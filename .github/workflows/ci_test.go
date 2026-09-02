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

	goJob := requireIndentedBlock(t, workflow, "  go:", 2)
	requireContains(t, goJob, "go test -tags sqlite_fts5 -race ./... -count=1")
	requireContains(t, goJob, "go vet -tags sqlite_fts5 ./...")
	requireContains(t, workflow, "go build -tags sqlite_fts5 ./...")
	// The per-module commands below are asserted as `cd <dir>` + the command on
	// the next line, not as two independent substrings. The modules share command
	// text ("go test ./... -count=1" is identical in mcp-files and mcp-system), so
	// unpaired assertions are satisfied by whichever module still has the step and
	// deleting the other module's step would go unnoticed.
	requireRunsIn(t, workflow, "turing-backend/mcp-files", "go test -race ./... -count=1")
	requireRunsIn(t, workflow, "turing-backend/mcp-files", "go vet ./...")
	requireRunsIn(t, workflow, "turing-backend/mcp-files", "go build ./cmd/server")
	// mcp-system is a separate module; nothing else in CI compiles it.
	requireRunsIn(t, workflow, "turing-backend/mcp-system", "go test -race ./... -count=1")
	requireRunsIn(t, workflow, "turing-backend/mcp-system", "go vet ./...")
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

	protoJob := requireIndentedBlock(t, workflow, "  proto-and-scripts:", 2)
	requireContains(t, protoJob, "uses: bufbuild/buf-action@8c6a16e16f12ba20b6470afa9c2ba9b5ba8c97c3 # v1.5.0")
	requireContains(t, protoJob, `version: "1.72.0"`)
	requireContains(t, protoJob, "setup_only: true")
	requireContains(t, protoJob, `TURING_REQUIRE_BUF=1 go test ./tools/proto -run '^TestBreaking' -count=1`)
	requireContains(t, protoJob, `tools/proto/breaking.sh "origin/${GITHUB_BASE_REF:-main}"`)
	requireContains(t, protoJob, "tools/proto/check.sh")
	requireContains(t, protoJob, "bash -n tools/proto/breaking.sh turing-backend/scripts/compose.sh turing-backend/scripts/dev.sh turing-backend/scripts/init.sh turing-backend/scripts/reset.sh turing-backend/scripts/rotate-client-key.sh turing-backend/scripts/smoke-grpc.sh turing-backend/scripts/smoke.sh turing-backend/scripts/verify-tool-loop.sh")
	requireContains(t, workflow, "uses: dart-lang/setup-dart@v1")
	requireContains(t, workflow, "dart pub global activate protoc_plugin 23.0.0")
	requireContains(t, workflow, `echo "$HOME/.pub-cache/bin" >> "$GITHUB_PATH"`)
	requireContains(t, workflow, "go test -tags sqlite_fts5 ./.github/workflows")
	requireContains(t, workflow, "flutter analyze")
	requireContains(t, workflow, "flutter test")
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

func TestCIWorkflowRunsPinnedLintInRootAndNestedModules(t *testing.T) {
	data, err := os.ReadFile("ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	if got := strings.Count(workflow, "\n  lint:\n"); got != 1 {
		t.Fatalf("lint job count = %d, want exactly one", got)
	}
	lintJob := requireIndentedBlock(t, workflow, "  lint:", 2)
	requireContains(t, lintJob, `go-version: "1.25.x"`)
	// Every Go job must pin the same toolchain, and each is asserted inside its
	// own job block rather than by counting occurrences across the file — a
	// count is satisfied by any five, including five copies in one job and none
	// in another. The root module and mcp-files both declare `go 1.25.0`, so a
	// job that drifted back to 1.23.x would not fail outright: with the default
	// GOTOOLCHAIN=auto it would silently switch toolchains and pass, which is
	// exactly how the pins fell out of step before #92 and #115 repaired them.
	// Only this assertion makes that drift visible.
	for _, job := range []string{"  go:", "  mcp-files:", "  mcp-system:", "  proto-and-scripts:"} {
		requireContains(t, requireIndentedBlock(t, workflow, job, 2), `go-version: "1.25.x"`)
	}
	requireContains(t, lintJob, "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2")
	requireContains(t, lintJob, `golangci-lint run --config "$GITHUB_WORKSPACE/.golangci.yml" --build-tags sqlite_fts5 ./... ./.github/workflows`)
	requireRunsIn(t, lintJob, "turing-backend/mcp-files", `golangci-lint run --config "$GITHUB_WORKSPACE/.golangci.yml" ./...`)
	requireRunsIn(t, lintJob, "turing-backend/mcp-system", `golangci-lint run --config "$GITHUB_WORKSPACE/.golangci.yml" ./...`)
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

func TestOrchestratorImageDropsPrivilegesWithSafeStandaloneDataDirectory(t *testing.T) {
	data, err := os.ReadFile("../../turing-backend/orchestrator-go/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}

	requireInOrder(t, string(data),
		"groupadd --gid 1000 turing-orchestrator",
		"useradd --uid 1000 --gid 1000",
		"install -d -o turing-orchestrator -g turing-orchestrator -m 0700 /app/data",
		"USER turing-orchestrator:turing-orchestrator",
	)
}

func TestMCPSystemSubdirectoryBuildContextHasEffectiveIgnoreRules(t *testing.T) {
	composeData, err := os.ReadFile("../../turing-backend/infra/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	systemService := requireIndentedBlock(t, string(composeData), "  turing-mcp-system:", 2)
	requireContains(t, systemService, "build: ../mcp-system")

	ignoreData, err := os.ReadFile("../../turing-backend/mcp-system/.dockerignore")
	if err != nil {
		t.Fatalf("mcp-system subdirectory context has no .dockerignore: %v", err)
	}
	ignore := string(ignoreData)
	for _, pattern := range []string{
		".env",
		".env.*",
		"*.pem",
		"*.key",
		"*.cert",
		"server",
		"*.test",
		"*.prof",
		"coverage",
	} {
		requireContains(t, ignore, pattern)
	}
}

func TestComposeRunsBindMountWritersAsValidatedHostIdentity(t *testing.T) {
	data, err := os.ReadFile("../../turing-backend/infra/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}

	compose := string(data)
	for _, service := range []string{"turing-orchestrator", "turing-mcp-files"} {
		block := requireIndentedBlock(t, compose, "  "+service+":", 2)
		requireContains(t, block, `user: "${HOST_UID:?Use scripts/compose.sh to launch}:${HOST_GID:?Use scripts/compose.sh to launch}"`)
	}
	for _, service := range []string{"turing-agent-runtime-general", "turing-mcp-system"} {
		block := requireIndentedBlock(t, compose, "  "+service+":", 2)
		if strings.Contains(block, "HOST_UID") || strings.Contains(block, "HOST_GID") {
			t.Fatalf("%s unexpectedly uses the bind-mount writer identity:\n%s", service, block)
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

// TestContainerGoBuildersMatchTheirRuntimeDistroAndModuleFloor pins the two
// image facts nothing else checks — no CI job builds an image, and Dependabot
// bumps every FROM line independently of every other:
//
//   - the golang builder's distro suffix matches its runtime stage's distro.
//     The orchestrator build is CGO and links the builder's glibc, so a
//     newer-distro builder (trixie) over a bookworm-slim runtime produces a
//     binary that fails at container start, not at image build.
//   - the builder's Go version covers the module's `go` directive. Below the
//     floor, GOTOOLCHAIN=auto quietly downloads a conforming toolchain inside
//     every image build — which is exactly how a 1.23 builder kept serving a
//     1.25.0 module unnoticed after the Dependabot sweep.
func TestContainerGoBuildersMatchTheirRuntimeDistroAndModuleFloor(t *testing.T) {
	cases := []struct {
		dockerfile string
		gomod      string
	}{
		{"../../turing-backend/orchestrator-go/Dockerfile", "../../go.mod"},
		{"../../turing-backend/agent-runtime-go/Dockerfile", "../../go.mod"},
		{"../../turing-backend/mcp-files/Dockerfile", "../../turing-backend/mcp-files/go.mod"},
		{"../../turing-backend/mcp-system/Dockerfile", "../../turing-backend/mcp-system/go.mod"},
	}
	runtimeFor := map[string]string{
		"bookworm": "FROM debian:bookworm-slim",
		"alpine":   "FROM alpine:",
	}
	for _, test := range cases {
		data, err := os.ReadFile(test.dockerfile)
		if err != nil {
			t.Fatal(err)
		}
		dockerfile := string(data)
		builderVersion, distro := "", ""
		for _, line := range strings.Split(dockerfile, "\n") {
			if !strings.HasPrefix(line, "FROM golang:") {
				continue
			}
			tag := strings.Fields(strings.TrimPrefix(line, "FROM golang:"))[0]
			version, suffix, found := strings.Cut(tag, "-")
			if !found {
				t.Fatalf("%s builder tag %q has no distro suffix; an unsuffixed golang tag floats to whatever distro is current", test.dockerfile, tag)
			}
			builderVersion, distro = version, suffix
			break
		}
		if builderVersion == "" {
			t.Fatalf("%s has no golang builder stage", test.dockerfile)
		}
		runtime, known := runtimeFor[distro]
		if !known {
			t.Fatalf("%s builder distro %q is not one this repo pairs with a runtime base", test.dockerfile, distro)
		}
		if !strings.Contains(dockerfile, runtime) {
			t.Fatalf("%s builds on golang:*-%s but its runtime stage is not %q", test.dockerfile, distro, runtime)
		}
		floor := goDirective(t, test.gomod)
		if compareGoMinors(t, builderVersion, floor) < 0 {
			t.Fatalf("%s builder go %s is below the module floor go %s in %s: image builds would download a toolchain via GOTOOLCHAIN=auto instead of using the pinned image", test.dockerfile, builderVersion, floor, test.gomod)
		}
	}
}

func goDirective(t *testing.T, gomod string) string {
	t.Helper()
	data, err := os.ReadFile(gomod)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if version, found := strings.CutPrefix(line, "go "); found {
			return strings.TrimSpace(version)
		}
	}
	t.Fatalf("%s has no go directive", gomod)
	return ""
}

// compareGoMinors orders two Go versions by major.minor only — the granularity
// FROM tags carry.
func compareGoMinors(t *testing.T, left, right string) int {
	t.Helper()
	parse := func(version string) [2]int {
		parts := strings.Split(version, ".")
		if len(parts) < 2 {
			t.Fatalf("go version %q does not carry major.minor", version)
		}
		var out [2]int
		for index := range out {
			value := 0
			for _, digit := range parts[index] {
				if digit < '0' || digit > '9' {
					t.Fatalf("go version %q is not numeric", version)
				}
				value = value*10 + int(digit-'0')
			}
			out[index] = value
		}
		return out
	}
	a, b := parse(left), parse(right)
	for index := range a {
		if a[index] != b[index] {
			if a[index] < b[index] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func requireIndentedBlock(t *testing.T, text, header string, indent int) string {
	t.Helper()
	allLines := strings.Split(text, "\n")
	start := -1
	for index, line := range allLines {
		if line == header {
			start = index
			break
		}
	}
	if start < 0 {
		t.Fatalf("content missing block %q", header)
	}
	block := []string{allLines[start]}
	for _, line := range allLines[start+1:] {
		if strings.TrimSpace(line) != "" && len(line)-len(strings.TrimLeft(line, " ")) <= indent {
			break
		}
		block = append(block, line)
	}
	return strings.Join(block, "\n")
}
