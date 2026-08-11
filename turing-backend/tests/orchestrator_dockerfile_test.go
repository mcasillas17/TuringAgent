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
	dockerfile := string(data)
	if !strings.Contains(dockerfile, "ARG GOFLAGS=-tags=sqlite_fts5") {
		t.Fatal("orchestrator Dockerfile must default GOFLAGS to -tags=sqlite_fts5 so direct builds support migration 0003")
	}
	if !strings.Contains(dockerfile, "go build $GOFLAGS") {
		t.Fatal("orchestrator Dockerfile must pass GOFLAGS to go build")
	}

	composeData, err := os.ReadFile(filepath.Join("..", "infra", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	compose := string(composeData)
	start := strings.Index(compose, "  turing-orchestrator:\n")
	end := strings.Index(compose, "\n  turing-agent-runtime-general:\n")
	if start == -1 || end == -1 || end <= start {
		t.Fatal("could not locate turing-orchestrator service in docker-compose.yml")
	}
	orchestratorService := compose[start:end]
	if !strings.Contains(orchestratorService, "GOFLAGS: -tags=sqlite_fts5") {
		t.Fatal("orchestrator Compose build must pass GOFLAGS=-tags=sqlite_fts5")
	}
}
