package mcpregistry

import (
	"context"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestTokenWithoutIntegrationKeyIsReportedInsteadOfFailingImport(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	service := New(repo, nil, nil)

	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer vendor-secret"}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("ImportJSON: %v", err)
	}
	if !strings.Contains(report.Unsupported["vendor"], "integration key") {
		t.Fatalf("unsupported = %v, want integration-key diagnostic", report.Unsupported)
	}
}
