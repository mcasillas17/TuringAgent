package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/encoding/protojson"
)

// A bearer sentinel deliberately shaped to contain both a double quote and
// a backslash — the two characters Go's %q verb (and, similarly, JSON
// string encoding) escapes. TestToolsListErrorCannotReflectRegisteredBearer
// already proves an ordinary token never reflects into a tools/list error;
// this one proves the narrower, previously-missed case: %q's escaping
// inserts extra backslash characters into the formatted error text, so the
// token no longer appears as one contiguous run of bytes for
// redactMCPErrorValue's plain strings.Contains/ReplaceAll to find and
// substitute — the escaped, still-reconstructible remnants of the token
// leak through instead. Both special characters sit well inside the first
// 16 bytes of the token so a leak survives even a truncated-prefix check.
const mcpCursorLeakSentinelToken = `mcp-"cursor\leak-sentinel-9f3c71a-do-not-leak`

// assertNoCursorTokenLeak asserts haystack carries neither the raw token
// nor a Go (%q) or JSON reconstructible escaped form of it. A pre-fix
// %q-interpolated cursor error fails the escaped-form checks specifically:
// the raw token itself is never contiguous in that text (see
// mcpCursorLeakSentinelToken's own doc comment), but the exact bytes %q (or
// json.Marshal) would have produced for it still are.
func assertNoCursorTokenLeak(t *testing.T, what string, haystack string, token string) {
	t.Helper()
	assertStringSentinelFree(t, what, haystack, token)
	goEscaped := fmt.Sprintf("%q", token)
	if strings.Contains(haystack, goEscaped[1:len(goEscaped)-1]) {
		t.Fatalf("%s carries the Go (%%q) escaped reconstructible form of the bearer token: %s", what, haystack)
	}
	jsonEscaped, err := json.Marshal(token)
	if err != nil {
		t.Fatal(err)
	}
	if trimmed := strings.Trim(string(jsonEscaped), `"`); strings.Contains(haystack, trimmed) {
		t.Fatalf("%s carries the JSON escaped reconstructible form of the bearer token: %s", what, haystack)
	}
}

// newRepeatedCursorVendor starts a tools/list server that always answers
// with an empty tools page and nextCursor equal to cursor, forcing
// listTools' pagination loop to see the identical cursor twice (the
// server's very first request has no cursor at all, so the *second*
// request — the first that carries "cursor": cursor in its params — is
// what trips the repeated-cursor check) and return its repeated-cursor
// error.
func newRepeatedCursorVendor(t *testing.T, cursor string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID,
			"result": map[string]any{"tools": []any{}, "nextCursor": cursor},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// TestListToolsRepeatedCursorErrorNeverLeaksBearerContainingQuoteOrBackslash
// is the direct, client-level proof of the fix: mcpClient.listTools must
// never interpolate a peer-controlled nextCursor value into its
// repeated-cursor error, regardless of what characters that cursor (here,
// the registered bearer token itself) contains.
func TestListToolsRepeatedCursorErrorNeverLeaksBearerContainingQuoteOrBackslash(t *testing.T) {
	const token = mcpCursorLeakSentinelToken
	server := newRepeatedCursorVendor(t, token)

	_, err := newMCPClient(server.URL, token, server.Client()).listTools(context.Background())
	if err == nil {
		t.Fatal("want a repeated-cursor error")
	}
	assertNoCursorTokenLeak(t, "listTools repeated-cursor error", err.Error(), token)
}

// TestListToolsInvalidCursorErrorNeverLeaksBearerContainingQuoteOrBackslash
// covers the sibling validation error (a non-string, non-null, non-absent
// nextCursor): it never carried the peer's value to begin with, but this
// pins that down as a regression test rather than leaving it implicit.
func TestListToolsInvalidCursorErrorNeverLeaksBearerContainingQuoteOrBackslash(t *testing.T) {
	const token = mcpCursorLeakSentinelToken
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID,
			"result": map[string]any{"tools": []any{}, "nextCursor": 42},
		})
	}))
	t.Cleanup(server.Close)

	_, err := newMCPClient(server.URL, token, server.Client()).listTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "nextCursor") {
		t.Fatalf("ListTools error = %v, want invalid nextCursor error", err)
	}
	assertNoCursorTokenLeak(t, "listTools invalid-cursor error", err.Error(), token)
}

// TestSetMcpServerEnabledCursorLeakNeverReachesResponseStatusRepoListAuditLogOrEvents
// is the end-to-end proof: a remote server whose tools/list repeats a
// nextCursor equal to the registered bearer (quote/backslash included)
// must still fail discovery closed, exactly as any other discovery error
// does, and the repeated-cursor error text — which discoverLocked persists
// verbatim (bounded) as the server's liveness status message — must never
// carry the token in any form anywhere this package could expose it: the
// SetMcpServerEnabled response, the repository row (status/error), the
// ListMcpServers response, the audit trail, the process log, or the
// (session-scoped, and so expected empty for MCP management) events table.
func TestSetMcpServerEnabledCursorLeakNeverReachesResponseStatusRepoListAuditLogOrEvents(t *testing.T) {
	const token = mcpCursorLeakSentinelToken
	database, service, repo := newSentinelSweepableRegistryService(t)
	vendor := newRepeatedCursorVendor(t, token)
	service.httpClient = vendor.Client()

	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	sealed, err := service.sealServerToken("vendor", token)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: vendor.URL, SealedToken: sealed, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := registered.Server

	var logged strings.Builder
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !descriptor.GetEnabled() {
		t.Fatal("a server whose discovery fails on a repeated cursor must remain enabled")
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_DOWN {
		t.Fatalf("liveness = %v, want down", descriptor.GetLiveness())
	}

	encodedDescriptor, err := protojson.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	// The protojson-encoded blob sweep below only guards the raw-token
	// case: JSON-encoding an already Go-%q-escaped status string a
	// second time re-escapes its backslashes, so a %q-escaped remnant of
	// the token would no longer be contiguous inside this blob even if
	// it were still present in the underlying field. The direct
	// GetStatusMessage()/StatusError/log checks below are what actually
	// prove the escaped-form leak this fix targets is gone; keep both
	// kinds of sweep rather than relying on the blob alone.
	assertNoCursorTokenLeak(t, "SetMcpServerEnabled response", string(encodedDescriptor), token)
	assertNoCursorTokenLeak(t, "SetMcpServerEnabled response status message", descriptor.GetStatusMessage(), token)

	updated, err := repo.GetMCPServer(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "down" {
		t.Fatalf("status = %q, want down", updated.Status)
	}
	assertNoCursorTokenLeak(t, "repository StatusError", updated.StatusError, token)

	listResponse, err := service.ListMcpServers(context.Background(), &turingv1.ListMcpServersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	encodedList, err := protojson.Marshal(listResponse)
	if err != nil {
		t.Fatal(err)
	}
	// See the encodedDescriptor comment above: this blob sweep is a
	// belt-and-suspenders raw-token check, not the escaped-form proof.
	assertNoCursorTokenLeak(t, "ListMcpServers response", string(encodedList), token)

	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want exactly one", recorder.records)
	}
	record := recorder.records[0]
	if record.action != "mcp.server.enabled" {
		t.Fatalf("action = %q, want mcp.server.enabled", record.action)
	}
	onlyExpectedAuditKeys(t, record.payload, "name", "tier", "remoteDiscoveryAttempted", "discoverySucceeded")
	if record.payload["discoverySucceeded"] != false {
		t.Fatalf("payload discoverySucceeded = %v, want false", record.payload["discoverySucceeded"])
	}
	assertNoCursorTokenLeak(t, "audit payload", fmt.Sprintf("%+v", record.payload), token)

	assertNoCursorTokenLeak(t, "process log", logged.String(), token)
	assertDatabaseSentinelFreeExceptSealedToken(t, database, token)

	var eventCount int
	if err := database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("event count = %d, want 0: MCP management must not emit session events", eventCount)
	}
}
