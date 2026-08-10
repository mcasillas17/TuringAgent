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
	requireContains(t, workflow, "tools/proto/check.sh")
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
