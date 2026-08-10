package workflows_test

import (
	"os"
	"strings"
	"testing"
)

func TestCIWorkflowCoversCoreChecks(t *testing.T) {
	data, err := os.ReadFile("ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)

	requireContains(t, workflow, "go test -tags sqlite_fts5 ./... -count=1")
	requireContains(t, workflow, "go build -tags sqlite_fts5 ./...")

	// The per-module commands below are asserted as `cd <dir>` + the command on
	// the next line, not as two independent substrings. The modules share command
	// text ("go test ./... -count=1" is identical in mcp-files and mcp-system), so
	// unpaired assertions are satisfied by whichever module still has the step and
	// deleting the other module's step would go unnoticed.
	requireRunsIn(t, workflow, "turing-backend/mcp-files", "go test ./... -count=1")
	requireRunsIn(t, workflow, "turing-backend/mcp-files", "go build ./cmd/server")
	// mcp-system is a separate module; nothing else in CI compiles it.
	requireRunsIn(t, workflow, "turing-backend/mcp-system", "go test ./... -count=1")
	requireRunsIn(t, workflow, "turing-backend/mcp-system", "go build ./...")

	// Lint must cover all three modules, with the repo config pinned explicitly
	// so a missed config lookup cannot silently downgrade this to defaults.
	requireContains(t, workflow, "go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@")
	// The trailing ./.github/workflows is load-bearing and is asserted as part of
	// the same string: `./...` skips dot-directories, so dropping it would leave
	// this very file unlinted while the command still looked correct.
	requireContains(t, workflow, `golangci-lint run --config "$GITHUB_WORKSPACE/.golangci.yml" --build-tags sqlite_fts5 ./... ./.github/workflows`)
	requireRunsIn(t, workflow, "turing-backend/mcp-files", `golangci-lint run --config "$GITHUB_WORKSPACE/.golangci.yml" ./...`)
	requireRunsIn(t, workflow, "turing-backend/mcp-system", `golangci-lint run --config "$GITHUB_WORKSPACE/.golangci.yml" ./...`)

	requireContains(t, workflow, "tools/proto/check.sh")
	requireContains(t, workflow, "uses: dart-lang/setup-dart@v1")
	requireContains(t, workflow, "dart pub global activate protoc_plugin 22.5.0")
	requireContains(t, workflow, `echo "$HOME/.pub-cache/bin" >> "$GITHUB_PATH"`)
	requireContains(t, workflow, "go test -tags sqlite_fts5 ./.github/workflows")
	requireContains(t, workflow, "flutter test")
	requireContains(t, workflow, "bash -n turing-backend/scripts/init.sh turing-backend/scripts/reset.sh turing-backend/scripts/smoke-grpc.sh")
}

// stepIndent is the column commands sit at inside ci.yml's `run: |` blocks
// (6 for the step, 2 for the block scalar, 2 for the script body).
const stepIndent = "\n          "

// requireRunsIn asserts that command is the line immediately after `cd dir`,
// so the assertion is pinned to one specific job step rather than being
// satisfiable by an identical command in some other module's job.
func requireRunsIn(t *testing.T, workflow string, dir string, command string) {
	t.Helper()
	requireContains(t, workflow, "cd "+dir+stepIndent+command)
}

func TestMCPFilesImagePreparesSandboxBeforeDroppingPrivileges(t *testing.T) {
	data, err := os.ReadFile("../../turing-backend/mcp-files/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(data)

	requireInOrder(t, dockerfile,
		"mkdir -p /sandbox",
		"chown 1000:1000 /sandbox",
		"USER mcp-files:mcp-files",
	)
}

func TestMCPSystemImageDropsPrivileges(t *testing.T) {
	data, err := os.ReadFile("../../turing-backend/mcp-system/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}

	requireInOrder(t, string(data),
		"addgroup -g 1000 -S mcp-system",
		"adduser -u 1000 -S -G mcp-system mcp-system",
		"USER mcp-system:mcp-system",
	)
}

func TestComposeRunsMCPFilesAsConfiguredHostIdentity(t *testing.T) {
	data, err := os.ReadFile("../../turing-backend/infra/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}

	compose := string(data)
	filesService := requireIndentedBlock(t, compose, "  turing-mcp-files:", 2)
	requireContains(t, filesService, `user: "${HOST_UID:-1000}:${HOST_GID:-1000}"`)
	for _, service := range []string{"turing-orchestrator", "turing-agent-runtime-general", "turing-mcp-system"} {
		block := requireIndentedBlock(t, compose, "  "+service+":", 2)
		if strings.Contains(block, "HOST_UID") || strings.Contains(block, "HOST_GID") {
			t.Fatalf("%s unexpectedly uses the mcp-files host identity:\n%s", service, block)
		}
	}
}

func TestInitConfiguresHostIdentityForBindMountedSandbox(t *testing.T) {
	envExample, err := os.ReadFile("../../turing-backend/.env.example")
	if err != nil {
		t.Fatal(err)
	}
	requireContains(t, string(envExample), "\nHOST_IDENTITY_MODE=auto\n")
	requireContains(t, string(envExample), "\nHOST_UID=\n")
	requireContains(t, string(envExample), "\nHOST_GID=\n")

	initScript, err := os.ReadFile("../../turing-backend/scripts/init.sh")
	if err != nil {
		t.Fatal(err)
	}
	requireContains(t, string(initScript), "configure_host_identity")
	requireContains(t, string(initScript), "is_positive_id")
}

func TestSandboxIsNotMadeWorldWritable(t *testing.T) {
	for _, path := range []string{
		"../../turing-backend/mcp-files/Dockerfile",
		"../../turing-backend/scripts/init.sh",
		"../../turing-backend/scripts/reset.sh",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, unsafe := range []string{"chmod 777", "chmod a+w", "chmod o+w"} {
			if strings.Contains(string(data), unsafe) {
				t.Fatalf("%s makes the sandbox world-writable with %q", path, unsafe)
			}
		}
	}
}

func requireContains(t *testing.T, text string, snippet string) {
	t.Helper()
	if !strings.Contains(text, snippet) {
		t.Fatalf("workflow missing %q", snippet)
	}
}

func requireInOrder(t *testing.T, text string, snippets ...string) {
	t.Helper()
	offset := 0
	for _, snippet := range snippets {
		index := strings.Index(text[offset:], snippet)
		if index == -1 {
			t.Fatalf("content missing %q in required order", snippet)
		}
		offset += index + len(snippet)
	}
}

func requireIndentedBlock(t *testing.T, text, header string, indent int) string {
	t.Helper()
	start := strings.Index(text, header+"\n")
	if start == -1 {
		t.Fatalf("content missing block %q", header)
	}
	lines := strings.Split(text[start:], "\n")
	block := []string{lines[0]}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) != "" && len(line)-len(strings.TrimLeft(line, " ")) <= indent {
			break
		}
		block = append(block, line)
	}
	return strings.Join(block, "\n")
}
