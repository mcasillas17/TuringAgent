package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOrchestratorDockerfileDefaultsGOFLAGSToSQLiteFTS5(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "orchestrator-go", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ARG GOFLAGS=-tags=sqlite_fts5") {
		t.Fatal("orchestrator Dockerfile must default GOFLAGS to -tags=sqlite_fts5 so direct builds support migration 0003")
	}
}
