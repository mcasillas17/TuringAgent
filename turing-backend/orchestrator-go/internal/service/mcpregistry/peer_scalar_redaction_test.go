package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
)

// newScalarEchoServer starts an httptest JSON-RPC server whose "result" is
// exactly resultValue, wrapped the same minimal envelope every other
// redaction test in this package uses (see review_regressions_test.go's
// TestMCPResponseCannotReflectRegisteredBearer).
func newScalarEchoServer(t *testing.T, resultValue any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID,
			"result": resultValue,
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// TestPeerResultRedactsNumericBearerValue proves redactMCPSecret's
// recursion catches a token whose only matching representation in a
// vendor's result is a JSON *number*, not a string: before this fix, the
// switch in redactMCPSecret had no case for the float64 a JSON number
// decodes to, so the value passed through unredacted, into the return
// value and (via CallRegisteredMcpTool) the gRPC response.
func TestPeerResultRedactsNumericBearerValue(t *testing.T) {
	const token = "48217395104"
	server := newScalarEchoServer(t, map[string]any{
		"content": []any{map[string]any{"type": "number", "value": json.Number(token)}},
	})

	result, err := newMCPClient(server.URL, token, server.Client()).callTool(
		context.Background(), "vendor.echo", map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), token) {
		t.Fatalf("MCP result reflected registered bearer as a numeric value: %s", encoded)
	}
}

// TestPeerResultRedactsScalarNestedDirectlyInsideAListOfLists proves the
// []any case's own recursion still reaches a bare scalar with no
// intervening map — a list element that is itself another list, whose
// own element is the token-bearing number — rather than only ever being
// exercised (elsewhere in this file) with a map sitting between the
// outer list and the scalar. redactMCPSecret's []any branch calls itself
// recursively on every element regardless of that element's own type, so
// this passes today without any additional change, but it closes a real
// gap in what this file's other tests actually cover.
func TestPeerResultRedactsScalarNestedDirectlyInsideAListOfLists(t *testing.T) {
	const token = "73914026855"
	server := newScalarEchoServer(t, map[string]any{
		"content": []any{[]any{json.Number(token)}},
	})

	result, err := newMCPClient(server.URL, token, server.Client()).callTool(
		context.Background(), "vendor.echo", map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), token) {
		t.Fatalf("MCP result reflected registered bearer as a scalar nested directly inside a list of lists: %s", encoded)
	}
}

// The boolean counterpart: a token equal to the literal text "true" or
// "false", returned as a genuine JSON boolean rather than a string.
func TestPeerResultRedactsBooleanBearerValue(t *testing.T) {
	const token = "true"
	server := newScalarEchoServer(t, map[string]any{
		"content": []any{map[string]any{"type": "flag", "value": true}},
	})

	result, err := newMCPClient(server.URL, token, server.Client()).callTool(
		context.Background(), "vendor.echo", map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), token) {
		t.Fatalf("MCP result reflected registered bearer as a boolean value: %s", encoded)
	}
}

// The JSON-null counterpart: a token equal to the literal text "null",
// returned as a genuine JSON null rather than a string.
func TestPeerResultRedactsNullBearerValue(t *testing.T) {
	const token = "null"
	server := newScalarEchoServer(t, map[string]any{
		"content": []any{map[string]any{"type": "empty", "value": nil}},
	})

	result, err := newMCPClient(server.URL, token, server.Client()).callTool(
		context.Background(), "vendor.echo", map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), token) {
		t.Fatalf("MCP result reflected registered bearer as a null value: %s", encoded)
	}
}

// TestPeerResultRefusesWhenTokenIsStructurallyUnredactable proves the
// other half of the fix: a token that is itself a piece of JSON syntax —
// here, the quote-colon-quote between every object key and its value —
// can never be removed from a result by substituting any single scalar's
// own value, no matter what it is replaced with: the surrounding
// key:value colon survives regardless. redactMCPSecret's per-scalar
// substitution alone cannot make the result token-free in this case, so
// callTool must refuse the whole result outright with a fixed, generic
// error instead of ever returning a result that still, unavoidably,
// contains the token.
func TestPeerResultRefusesWhenTokenIsStructurallyUnredactable(t *testing.T) {
	const token = `":"`
	server := newScalarEchoServer(t, map[string]any{
		"content": []any{map[string]any{"type": "text", "text": "unrelated"}},
	})

	result, err := newMCPClient(server.URL, token, server.Client()).callTool(
		context.Background(), "vendor.echo", map[string]any{},
	)
	if err == nil {
		t.Fatalf("want a refusal for a structurally unredactable token, got result: %+v", result)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("refusal error must never itself carry the token: %v", err)
	}
}

// TestCallRegisteredMcpToolRefusesStructurallyUnredactableResultEndToEnd
// is the full-service counterpart: the structural-token refusal above,
// proven all the way through Server.CallTool/CallRegisteredMcpTool (using
// the same registryCallHarness/runningToolCall setup call_test.go's own
// dispatch tests use, so the call actually reaches an active dispatch
// state rather than being refused earlier for an unrelated reason), with
// the process log, the audit trail, and the server's own persisted
// liveness status all swept for the token too — not just the mcpClient
// return value the narrower test above checks.
func TestCallRegisteredMcpToolRefusesStructurallyUnredactableResultEndToEnd(t *testing.T) {
	const token = `":"`
	h := newRegistryCallHarness(t)
	h.setResult(map[string]any{
		"content": []any{map[string]any{"type": "text", "text": "unrelated"}},
	})
	// Re-seal the harness's own server row with the structural token as
	// its bearer, in place of the harness's default "vendor-token": the
	// redaction this test is about is keyed to whatever token CallTool
	// decrypts and sends as the vendor's bearer, not to the harness's own
	// unrelated default value.
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x42}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := sealer.Seal([]byte(token), []byte("vendor"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.ReplaceMCPServerToken(context.Background(), h.serverID, sealed); err != nil {
		t.Fatal(err)
	}
	runID := h.runningToolCall(t, "call_structural_token", map[string]any{"path": "x"})
	if err := h.repo.SetMCPToolPolicy(context.Background(), h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}
	recorder := &recordingAuditRecorder{}
	h.registry.SetAuditRecorder(recorder)

	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	response, err := h.registry.CallRegisteredMcpTool(context.Background(), &turingv1.CallRegisteredMcpToolRequest{
		ServerId: h.serverID, RunId: runID, ToolName: "vendor.write",
	})
	if err == nil {
		t.Fatalf("want a refusal for a structurally unredactable result, got response: %+v", response)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("RPC error must never itself carry the token: %v", err)
	}
	if got := h.reached.Load(); got != 1 {
		t.Fatalf("vendor requests = %d, want exactly 1: the vendor really was called", got)
	}

	for _, record := range recorder.records {
		for _, value := range record.payload {
			if s, ok := value.(string); ok && strings.Contains(s, token) {
				t.Fatalf("audit payload carries the token: %+v", record.payload)
			}
		}
	}
	assertStringSentinelFree(t, "process log", logged.String(), token)

	current, err := h.repo.GetMCPServer(context.Background(), h.serverID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(current.StatusError, token) {
		t.Fatalf("persisted status_error carries the token: %q", current.StatusError)
	}
}
