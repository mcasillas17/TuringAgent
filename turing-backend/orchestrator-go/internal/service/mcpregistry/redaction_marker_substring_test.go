package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"google.golang.org/protobuf/encoding/protojson"
)

// markerSubstringTokens is finding #4's own named set: a token equal to, or
// found as a substring of, mcpRedactedMarker ("[redacted]") itself — the
// pathological case where naively substituting the marker for a matched
// occurrence cannot actually remove the token, because the marker's own
// fixed replacement text still contains it afterward.
var markerSubstringTokens = []string{"e", "red", "ed]", "[redacted]"}

// TestRedactMCPSecretStringNeverLeavesMarkerSubstringTokenPresent is the
// unit-level determining test for finding #4: redactMCPSecretString must
// guarantee its output never contains secret, even when secret is a
// substring of (or equal to) mcpRedactedMarker itself. After the naive
// strings.ReplaceAll, if the result still contains secret, the function
// must fall back to a proven-safe representation — empty is universal —
// rather than ever returning text that still carries it. An ordinary,
// unrelated secret is unaffected: it still redacts exactly as before.
func TestRedactMCPSecretStringNeverLeavesMarkerSubstringTokenPresent(t *testing.T) {
	for _, test := range []struct {
		name  string
		token string
		value string
		want  string
	}{
		{name: "single letter e", token: "e", value: "there", want: ""},
		{name: "red prefix of marker", token: "red", value: "my favorite color is red", want: ""},
		{name: "ed] suffix of marker", token: "ed]", value: "trailed] here", want: ""},
		{name: "whole marker", token: "[redacted]", value: "status [redacted] seen", want: ""},
		{name: "unrelated token still redacts normally", token: "ordinary-secret-token", value: "use ordinary-secret-token now", want: "use " + mcpRedactedMarker + " now"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := redactMCPSecretString(test.value, test.token)
			if got != test.want {
				t.Fatalf("redactMCPSecretString(%q, %q) = %q, want %q", test.value, test.token, got, test.want)
			}
			if strings.Contains(got, test.token) {
				t.Fatalf("redactMCPSecretString output still contains the secret: %q", got)
			}
		})
	}
}

// TestRedactMCPErrorValueNeverLeavesMarkerSubstringTokenPresent proves
// redactMCPErrorValue's error path benefits from the same guarantee: since
// it is built entirely from redactMCPSecretString, an error whose message
// contains a marker-substring token must never surface that token either,
// falling back to an empty redacted message rather than a partially-marked
// one that still carries it.
func TestRedactMCPErrorValueNeverLeavesMarkerSubstringTokenPresent(t *testing.T) {
	for _, token := range markerSubstringTokens {
		t.Run(token, func(t *testing.T) {
			original := errors.New("here is an error containing " + token)
			redacted := redactMCPErrorValue(original, token)
			if redacted == nil {
				t.Fatal("redactMCPErrorValue returned nil")
			}
			if strings.Contains(redacted.Error(), token) {
				t.Fatalf("redacted error still contains the token: %v", redacted)
			}
			if redacted.Error() == original.Error() {
				t.Fatalf("redactMCPErrorValue left the token-bearing message unchanged: %v", redacted)
			}
		})
	}
}

// TestPeerToolsCallResultRedactsOrdinaryStringForMarkerSubstringTokens
// proves finding #4's own acceptance criterion at the mcpClient level: an
// ordinary vendor string value containing a marker-substring token must
// succeed (redacted or emptied), never refuse with
// errMCPResultCannotBeRedacted — that refusal is reserved for a token that
// is itself a piece of JSON syntax (see
// TestPeerResultRefusesWhenTokenIsStructurallyUnredactable in
// peer_scalar_redaction_test.go), not for this narrower marker-collision
// case, which redactMCPSecretString's own proven-safe fallback can always
// resolve without refusing the whole result. The result key ("info")
// deliberately shares no character sequence with any of the four tested
// tokens, so only the *value*'s own redaction is under test here.
func TestPeerToolsCallResultRedactsOrdinaryStringForMarkerSubstringTokens(t *testing.T) {
	for _, token := range markerSubstringTokens {
		t.Run(token, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					ID int64 `json:"id"`
				}
				_ = json.NewDecoder(r.Body).Decode(&request)
				w.Header().Set("content-type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0", "id": request.ID,
					"result": map[string]any{"info": "value containing " + token + " inline"},
				})
			}))
			t.Cleanup(server.Close)

			result, err := newMCPClient(server.URL, token, server.Client()).callTool(
				context.Background(), "vendor.echo", map[string]any{},
			)
			if err != nil {
				t.Fatalf("an ordinary echo must succeed rather than refuse with errMCPResultCannotBeRedacted: %v", err)
			}
			encoded, encodeErr := json.Marshal(result)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			if strings.Contains(string(encoded), token) {
				t.Fatalf("MCP result leaked the marker-substring token %q: %s", token, encoded)
			}
		})
	}
}

// TestPeerToolsCallResultRedactsMapKeyContainingMarkerSubstringToken proves
// the same guarantee reaches a map *key*, not only a value: redactMCPSecret
// redacts keys through the identical redactMCPSecretString primitive (see
// its own map branch), so a key built from a marker-substring token must be
// handled exactly as safely as a value carrying one.
func TestPeerToolsCallResultRedactsMapKeyContainingMarkerSubstringToken(t *testing.T) {
	for _, token := range markerSubstringTokens {
		t.Run(token, func(t *testing.T) {
			key := "safe-" + token + "-key"
			server := newScalarEchoServer(t, map[string]any{key: "value"})

			result, err := newMCPClient(server.URL, token, server.Client()).callTool(
				context.Background(), "vendor.echo", map[string]any{},
			)
			if err != nil {
				t.Fatalf("an ordinary echo must succeed rather than refuse: %v", err)
			}
			encoded, encodeErr := json.Marshal(result)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			if strings.Contains(string(encoded), token) {
				t.Fatalf("MCP result leaked the marker-substring token %q via a map key: %s", token, encoded)
			}
		})
	}
}

// TestPeerToolsCallResultEmptiesBooleanScalarWhenMarkerWouldStillLeak
// covers the scalar half of finding #4's own "map/list keys/scalars as
// relevant" instruction: redactMCPSecretScalar substitutes a whole
// number/bool/null value with mcpRedactedMarker when its own canonical
// JSON text contains secret, so it needs the identical proven-safe
// fallback redactMCPSecretString now has. Only "e" among
// markerSubstringTokens can ever appear inside a *non-string* JSON
// scalar's canonical text at all — "true"/"false"/"null" and ordinary
// number literals are built only from digits, '.', '-', and the letters
// t/r/u/e/f/a/l/s/n — so this is deliberately narrower than the
// string-value tests above, which do cover all four tokens.
func TestPeerToolsCallResultEmptiesBooleanScalarWhenMarkerWouldStillLeak(t *testing.T) {
	const token = "e"
	server := newScalarEchoServer(t, map[string]any{"flag": true})

	result, err := newMCPClient(server.URL, token, server.Client()).callTool(
		context.Background(), "vendor.echo", map[string]any{},
	)
	if err != nil {
		t.Fatalf("an ordinary boolean echo must succeed rather than refuse: %v", err)
	}
	encoded, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if strings.Contains(string(encoded), token) {
		t.Fatalf("MCP result leaked the marker-substring token %q via a boolean scalar: %s", token, encoded)
	}
}

// TestPeerErrorNeverLeavesMarkerSubstringToken proves the "peer error"
// half of finding #4: a vendor's own JSON-RPC error message containing a
// marker-substring token must never surface it in the error this package
// returns, across the double redaction path (the inline
// redactMCPSecretString call inside request's own envelope.Error handling,
// then the deferred redactMCPErrorValue wrapping the whole result) —
// mirroring TestMCPErrorCannotReflectRegisteredBearer (token_error_test.go)
// but for these specific tokens.
func TestPeerErrorNeverLeavesMarkerSubstringToken(t *testing.T) {
	for _, token := range markerSubstringTokens {
		t.Run(token, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					ID int64 `json:"id"`
				}
				_ = json.NewDecoder(r.Body).Decode(&request)
				w.Header().Set("content-type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0", "id": request.ID,
					"error": map[string]any{
						"code": -32000, "message": "failed near " + token + " boundary",
					},
				})
			}))
			t.Cleanup(server.Close)

			_, err := newMCPClient(server.URL, token, server.Client()).callTool(
				context.Background(), "vendor.fail", map[string]any{},
			)
			if err == nil {
				t.Fatal("want a non-nil error from the vendor's own JSON-RPC error envelope")
			}
			if strings.Contains(err.Error(), token) {
				t.Fatalf("MCP error leaked the marker-substring token %q: %v", token, err)
			}
		})
	}
}

// TestCallRegisteredMcpToolSucceedsAndStaysTokenFreeForMarkerSubstringTokenEndToEnd
// is the full-service counterpart, mirroring
// TestCallRegisteredMcpToolRefusesStructurallyUnredactableResultEndToEnd
// (peer_scalar_redaction_test.go) but for the opposite, success outcome: a
// marker-substring bearer ("red") must let an ordinary vendor echo
// (unrelated to the bearer beyond coincidentally sharing that word)
// dispatch and return normally — response, process log, audit payloads,
// and the server's own persisted liveness status all swept for the token
// too, not just the mcpClient return value the narrower tests above check.
func TestCallRegisteredMcpToolSucceedsAndStaysTokenFreeForMarkerSubstringTokenEndToEnd(t *testing.T) {
	const token = "red"
	h := newRegistryCallHarness(t)
	h.setResult(map[string]any{"info": "shade of red observed"})
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
	runID := h.runningToolCall(t, "call_marker_substring_token", map[string]any{"path": "x"})
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
	if err != nil {
		t.Fatalf("an ordinary echo containing a marker-substring token must succeed, not refuse: %v", err)
	}
	encoded, err := protojson.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), token) {
		t.Fatalf("response carries the marker-substring token: %s", encoded)
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
