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
	requireContains(t, workflow, "cd turing-backend/mcp-files")
	requireContains(t, workflow, "go test ./... -count=1")
	requireContains(t, workflow, "go build ./cmd/server")
	// mcp-system is a separate module; nothing else in CI compiles it.
	requireContains(t, workflow, "cd turing-backend/mcp-system")
	requireContains(t, workflow, "go build ./...")
	// Lint must cover all three modules, with the repo config pinned explicitly
	// so a missed config lookup cannot silently downgrade this to defaults.
	requireContains(t, workflow, "go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@")
	requireContains(t, workflow, `golangci-lint run --config "$GITHUB_WORKSPACE/.golangci.yml" --build-tags sqlite_fts5 ./...`)
	requireContains(t, workflow, `golangci-lint run --config "$GITHUB_WORKSPACE/.golangci.yml" ./...`)
	requireContains(t, workflow, "tools/proto/check.sh")
	requireContains(t, workflow, "uses: dart-lang/setup-dart@v1")
	requireContains(t, workflow, "dart pub global activate protoc_plugin 22.5.0")
	requireContains(t, workflow, `echo "$HOME/.pub-cache/bin" >> "$GITHUB_PATH"`)
	requireContains(t, workflow, "go test -tags sqlite_fts5 ./.github/workflows")
	requireContains(t, workflow, "flutter test")
	requireContains(t, workflow, "bash -n turing-backend/scripts/init.sh turing-backend/scripts/reset.sh turing-backend/scripts/smoke-grpc.sh")
}

func requireContains(t *testing.T, text string, snippet string) {
	t.Helper()
	if !strings.Contains(text, snippet) {
		t.Fatalf("workflow missing %q", snippet)
	}
}
