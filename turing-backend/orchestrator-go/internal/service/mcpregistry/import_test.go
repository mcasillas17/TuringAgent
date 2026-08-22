package mcpregistry

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
)

func TestImportingMcpJSONLeavesEverythingOff(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"vendor": {
				"url": "https://vendor.example/mcp",
				"headers": {"Authorization": "Bearer vendor-secret"},
				"tools": [{
					"name": "vendor.lookup",
					"inputSchema": {"type": "object"}
				}]
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unsupported) != 0 {
		t.Fatalf("unsupported = %v, want none", report.Unsupported)
	}

	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vendor := findRepositoryServer(t, servers, "vendor")
	if vendor.Enabled {
		t.Fatal("an imported server must arrive disabled")
	}
	if vendor.Tier != repository.MCPServerTierRemoteURL {
		t.Fatalf("tier = %q, want remote URL", vendor.Tier)
	}
	if bytes.Contains(vendor.SealedToken, []byte("vendor-secret")) {
		t.Fatal("the imported bearer was stored in plaintext")
	}

	tools, err := repo.ListMCPServerTools(context.Background(), vendor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Policy != "approval_required" || tools[0].Enabled {
		t.Fatalf("imported tools = %+v, want disabled and approval_required", tools)
	}
}

func TestStdioEntriesAreReportedAsUnsupported(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"local": {"command": "npx", "args": ["x"]}
		}

	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Unsupported["local"], "stdio") {
		t.Fatalf("report = %q, want it to say why stdio is unsupported", report.Unsupported["local"])
	}
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.Name == "local" {
			t.Fatal("a stdio entry must not be registered")
		}
	}
}

// A reserved or pattern-invalid name must be refused for that reason alone,
// even when its raw JSON body would otherwise fail strict decoding (an
// unknown field here). This proves the name check runs before the entry is
// decoded, sharing the one validateMCPServerName implementation import and
// RegisterMcpServer both use — not a second, decode-order-dependent check
// that a future edit could silently diverge from mapMCPValidationError's
// reserved-name handling.
func TestImportJSONRefusesReservedAndInvalidNamesBeforeDecodingTheEntryBody(t *testing.T) {
	service, repo := newRegistryTestService(t)
	report, err := service.ImportJSON(context.Background(), []byte(`{
		"mcpServers": {
			"files": {"unknown_field_that_fails_strict_decoding": true},
			"not a valid name!": {"unknown_field_that_fails_strict_decoding": true}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if reason := report.Unsupported["files"]; !strings.Contains(reason, "reserved") {
		t.Fatalf("files reason = %q, want it refused for being reserved, not a decode failure", reason)
	}
	if reason := report.Unsupported["not a valid name!"]; !strings.Contains(reason, "invalid") {
		t.Fatalf("not a valid name! reason = %q, want it refused for being an invalid name, not a decode failure", reason)
	}
	if strings.Contains(report.Unsupported["files"], "entry is invalid") {
		t.Fatalf("files reason = %q, must not be the decode-failure message", report.Unsupported["files"])
	}
	if strings.Contains(report.Unsupported["not a valid name!"], "entry is invalid") {
		t.Fatalf("not a valid name! reason = %q, must not be the decode-failure message", report.Unsupported["not a valid name!"])
	}

	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.Name == "not a valid name!" {
			t.Fatal("an invalid name must never create a row")
		}
	}
}

// validateMCPServerName is the one shared reserved/pattern check. This
// fails if that sharing is ever undone — e.g. if ImportJSON grows its own
// separate name check that drifts from validateServerDefinition's.
func TestValidateMCPServerNameIsSharedByImportAndValidateServerDefinition(t *testing.T) {
	for _, name := range []string{"files", "system", "skills", "not a valid name!", ""} {
		nameErr := validateMCPServerName(name)
		if nameErr == nil {
			t.Fatalf("validateMCPServerName(%q) = nil, want an error", name)
		}
		_, defErr := validateServerDefinition(name, "https://vendor.example/mcp", nil, "")
		if defErr == nil {
			t.Fatalf("validateServerDefinition(%q, ...) = nil, want an error", name)
		}
		if defErr.Error() != nameErr.Error() {
			t.Fatalf("validateServerDefinition(%q) = %q, want the same reason as validateMCPServerName: %q",
				name, defErr.Error(), nameErr.Error())
		}
	}
}

func newRegistryTestService(t *testing.T) (*Server, *repository.Repository) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x41}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	return New(repo, sealer, nil), repo
}

func findRepositoryServer(t *testing.T, servers []repository.MCPServerRecord, name string) repository.MCPServerRecord {
	t.Helper()
	for _, server := range servers {
		if server.Name == name {
			return server
		}
	}
	t.Fatalf("server %q not found in %+v", name, servers)
	return repository.MCPServerRecord{}
}

// An unsupported header name or tool name in mcp.json is attacker
// controlled — an operator's own file, but one that could still be
// arbitrarily long or (via headers, which are a JSON object's keys) not
// otherwise length-limited by anything upstream. Every ImportJSON
// unsupported reason must go through the same boundedStatusMessage bound
// RecordDiscovery's own failures already used, both in the report handed
// back to the caller and in what gets persisted to mcp_import_issues.
func TestImportJSONBoundsLongAttackerControlledReasonsAndKeepsValidUTF8(t *testing.T) {
	service, repo := newRegistryTestService(t)
	longHeaderName := strings.Repeat("x-évil-header-", 60)
	longToolName := strings.Repeat("évil-tool-name-", 60)
	if len(longHeaderName) <= maxMCPStatusMessageBytes || len(longToolName) <= maxMCPStatusMessageBytes {
		t.Fatalf("test setup is broken: names must exceed maxMCPStatusMessageBytes (%d) on their own", maxMCPStatusMessageBytes)
	}

	document := fmt.Sprintf(`{
		"mcpServers": {
			"vendor-header": {
				"url": "https://vendor-header.example/mcp",
				"headers": {%q: "Bearer x"}
			},
			"vendor-tool": {
				"url": "https://vendor-tool.example/mcp",
				"tools": [{"name": %q, "inputSchema": {"type": "array"}}]
			}
		}
	}`, longHeaderName, longToolName)

	report, err := service.ImportJSON(context.Background(), []byte(document))
	if err != nil {
		t.Fatal(err)
	}

	headerReason, ok := report.Unsupported["vendor-header"]
	if !ok {
		t.Fatalf("report = %+v, want vendor-header refused", report.Unsupported)
	}
	assertBoundedUTF8(t, "header reason", headerReason)

	toolReason, ok := report.Unsupported["vendor-tool"]
	if !ok {
		t.Fatalf("report = %+v, want vendor-tool refused", report.Unsupported)
	}
	assertBoundedUTF8(t, "tool reason", toolReason)

	issues, err := repo.ListMCPImportIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, issue := range issues {
		assertBoundedUTF8(t, "persisted reason for "+issue.Name, issue.Reason)
		found[issue.Name] = true
	}
	if !found["vendor-header"] || !found["vendor-tool"] {
		t.Fatalf("issues = %+v, want both vendor-header and vendor-tool persisted", issues)
	}
}

func assertBoundedUTF8(t *testing.T, what, reason string) {
	t.Helper()
	if len(reason) > maxMCPStatusMessageBytes {
		t.Fatalf("%s length = %d, want <= %d", what, len(reason), maxMCPStatusMessageBytes)
	}
	if !utf8.ValidString(reason) {
		t.Fatalf("%s is not valid UTF-8: %q", what, reason)
	}
}

// validatedMCPServer carries a plaintext bearer Token until the caller
// seals it. Nothing in this package intentionally formats one with fmt,
// but %v/%+v/%#v are exactly what a stray log.Printf, an error wrap, or a
// test failure message reaches for by default — so String/GoString must
// redact Token themselves rather than relying on every call site to
// remember not to print the struct directly.
func TestValidatedMCPServerNeverPrintsTokenViaFmt(t *testing.T) {
	const sentinel = "validated-mcp-server-token-should-never-print-abc123"
	validated := validatedMCPServer{
		Name:  "vendor",
		URL:   "https://vendor.example/mcp",
		Token: sentinel,
		Tier:  repository.MCPServerTierRemoteURL,
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		rendered := fmt.Sprintf(format, validated)
		if strings.Contains(rendered, sentinel) {
			t.Fatalf("fmt %q rendered the token: %s", format, rendered)
		}
	}
}
