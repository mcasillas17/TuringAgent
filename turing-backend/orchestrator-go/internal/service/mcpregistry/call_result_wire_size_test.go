package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// wireSizeCallHarness is a minimal registryCallHarness variant for
// CallRegisteredMcpTool's own wire-size guard: unlike registryCallHarness
// (call_test.go), the vendor's tools/call result is fully caller-controlled
// per test (via resultJSON), and the one registered tool's policy is
// "safe" — set explicitly after RecordDiscovery's own DefaultPolicyFor
// default (approval_required, see buildRepositoryTool) — so a test can
// call CallRegisteredMcpTool directly without also standing up an
// approval.
type wireSizeCallHarness struct {
	registry   *Server
	repo       *repository.Repository
	database   *db.DB
	serverID   string
	resultJSON string
}

func newWireSizeCallHarness(t *testing.T) *wireSizeCallHarness {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x37}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	h := &wireSizeCallHarness{repo: repo, database: database}
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode vendor request: %v", err)
		}
		w.Header().Set("content-type", "application/json")
		// h.resultJSON is embedded as a json.RawMessage so its bytes
		// reach the wire, and therefore the client's own
		// json.Unmarshal(envelope.Result, ...), completely unchanged —
		// exactly the same bytes callResultWireSize independently
		// measures a test fixture against.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result":  json.RawMessage(h.resultJSON),
		})
	}))
	t.Cleanup(vendor.Close)
	sealed, err := sealer.Seal([]byte("vendor-token"), []byte("vendor"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name:        "vendor",
		URL:         vendor.URL,
		SealedToken: sealed,
		Tier:        repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPServerEnabled(context.Background(), server.Server.ID, true); err != nil {
		t.Fatal(err)
	}
	h.serverID = server.Server.ID
	h.registry = New(repo, sealer, vendor.Client())
	if err := h.registry.RecordDiscovery(context.Background(), server.Server.ID, []DiscoveredTool{{
		Name:       "vendor.oversized_result",
		SchemaJSON: `{"type":"object"}`,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMCPToolPolicy(context.Background(), server.Server.ID, "vendor.oversized_result", "safe"); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *wireSizeCallHarness) runningRunID(t *testing.T) string {
	t.Helper()
	session, err := h.repo.CreateSession(context.Background(), "wire size test")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "call it", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkRunRunning(context.Background(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(
		context.Background(),
		`UPDATE agent_runs SET execution_active = 1 WHERE id = ?`,
		enqueued.RunID,
	); err != nil {
		t.Fatal(err)
	}
	return enqueued.RunID
}

// numberArrayResultRawJSON builds a raw JSON object text shaped
// `{"x":[0,0,0,...]}`, sized so its own raw byte length is exactly
// rawBytes — the same worst-case wire-expansion shape
// aggregate_response_budget_test.go's numberArraySchemaJSON already
// established for a tool *schema* (see its own doc comment for the
// ~5.5x expansion measurement), reused here for a tool *call result*
// instead: structpb.NewStruct converts a JSON number array identically
// regardless of which field of the response ultimately carries it.
func numberArrayResultRawJSON(rawBytes int) string {
	return numberArraySchemaJSON("", rawBytes)
}

// callResultWireSize mirrors exactly what CallRegisteredMcpTool itself
// computes: unmarshal the raw result JSON into a map, convert it through
// structpb.NewStruct, wrap it in the same response message, and measure
// proto.Size — so a test can determine a worst-case or boundary raw size
// without duplicating (and risking drift from) the real conversion.
func callResultWireSize(t *testing.T, rawResultJSON string) int {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(rawResultJSON), &result); err != nil {
		t.Fatalf("unmarshal number-array result fixture: %v", err)
	}
	value, err := structpb.NewStruct(result)
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	return proto.Size(&turingv1.CallRegisteredMcpToolResponse{Result: value})
}

// largestNumberArrayRawBytesAtOrUnderWireCap binary-searches the largest
// rawBytes such that numberArrayResultRawJSON(rawBytes)'s own converted
// wire size (see callResultWireSize) stays at or under limit, searching
// only within [0, maxMCPResponseBytes] — CallRegisteredMcpTool can never
// even observe a raw result larger than maxMCPResponseBytes, since
// mcpClient.request's own bounded read already refuses a larger HTTP
// response body before this package ever sees it.
func largestNumberArrayRawBytesAtOrUnderWireCap(t *testing.T, limit int) int {
	t.Helper()
	lo, hi := 0, int(maxMCPResponseBytes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if callResultWireSize(t, numberArrayResultRawJSON(mid)) <= limit {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// TestCallRegisteredMcpToolRefusesResultExceedingWireSizeCap proves the
// worst-measured case: a vendor's raw tools/call JSON-RPC result, well
// within maxMCPResponseBytes (1MiB) at the HTTP layer, can still convert
// (via structpb.NewStruct) to a protobuf message that would exceed
// maxGRPCMessageSize (4MiB, mirrored here as maxMCPToolResultWireBytes)
// once actually sent — a single large JSON number array is the same
// ~5.5x adversarial expansion shape already measured for a tool schema
// (see numberArraySchemaJSON). CallRegisteredMcpTool must catch this
// itself, with a fixed ResourceExhausted status, before ever handing the
// oversized response to gRPC's own send path.
func TestCallRegisteredMcpToolRefusesResultExceedingWireSizeCap(t *testing.T) {
	h := newWireSizeCallHarness(t)
	// 1,000,000 raw bytes comfortably clears maxMCPResponseBytes (1MiB)
	// with room for the JSON-RPC envelope around it, and — at the
	// measured ~5.5x number-array expansion — converts to roughly 5.5MB,
	// safely past the 4MiB wire cap.
	h.resultJSON = numberArrayResultRawJSON(1_000_000)
	if wire := callResultWireSize(t, h.resultJSON); wire <= maxMCPToolResultWireBytes {
		t.Fatalf("test setup is broken: fixture wire size %d must exceed maxMCPToolResultWireBytes (%d)", wire, maxMCPToolResultWireBytes)
	}
	runID := h.runningRunID(t)

	_, err := h.registry.CallRegisteredMcpTool(context.Background(), &turingv1.CallRegisteredMcpToolRequest{
		ServerId: h.serverID, RunId: runID, ToolName: "vendor.oversized_result",
		Args: &structpb.Struct{},
	})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("code = %v, want ResourceExhausted for a result whose converted wire size exceeds the cap", status.Code(err))
	}
	if len(err.Error()) > 256 {
		t.Fatalf("error message is %d bytes, want a short fixed message rather than anything sized against the result", len(err.Error()))
	}
	if bytesContainsAny(err.Error(), "vendor-token", "0,0,0") {
		t.Fatalf("error = %q, must never echo the result content or the server token", err.Error())
	}
}

// TestCallRegisteredMcpToolAllowsResultAtWireSizeBoundary proves the
// guard is not off-by-one: a result whose converted wire size lands
// exactly at maxMCPToolResultWireBytes must still be returned, and one
// byte more (via a slightly larger raw fixture) must be refused — using
// largestNumberArrayRawBytesAtOrUnderWireCap to find that exact raw-size
// boundary via the real conversion path rather than an estimate.
func TestCallRegisteredMcpToolAllowsResultAtWireSizeBoundary(t *testing.T) {
	boundaryRawBytes := largestNumberArrayRawBytesAtOrUnderWireCap(t, maxMCPToolResultWireBytes)
	boundaryJSON := numberArrayResultRawJSON(boundaryRawBytes)
	if wire := callResultWireSize(t, boundaryJSON); wire > maxMCPToolResultWireBytes {
		t.Fatalf("test setup is broken: boundary fixture wire size %d exceeds maxMCPToolResultWireBytes (%d)", wire, maxMCPToolResultWireBytes)
	}

	h := newWireSizeCallHarness(t)
	h.resultJSON = boundaryJSON
	runID := h.runningRunID(t)
	response, err := h.registry.CallRegisteredMcpTool(context.Background(), &turingv1.CallRegisteredMcpToolRequest{
		ServerId: h.serverID, RunId: runID, ToolName: "vendor.oversized_result",
		Args: &structpb.Struct{},
	})
	if err != nil {
		t.Fatalf("a result exactly at the wire-size boundary must be allowed: %v", err)
	}
	if response.GetResult() == nil {
		t.Fatal("response result is nil at the boundary")
	}

	// One raw byte more pushes the *converted* size over the cap (each
	// additional JSON number-array element costs far more than one wire
	// byte — see numberArrayTool's own ~5.5x measurement) — so growing
	// the raw fixture by even a modest, fixed margin must now be
	// refused.
	overH := newWireSizeCallHarness(t)
	overH.resultJSON = numberArrayResultRawJSON(boundaryRawBytes + 64)
	if wire := callResultWireSize(t, overH.resultJSON); wire <= maxMCPToolResultWireBytes {
		t.Fatalf("test setup is broken: fixture just beyond the boundary must itself convert to more than maxMCPToolResultWireBytes")
	}
	overRunID := overH.runningRunID(t)
	_, err = overH.registry.CallRegisteredMcpTool(context.Background(), &turingv1.CallRegisteredMcpToolRequest{
		ServerId: overH.serverID, RunId: overRunID, ToolName: "vendor.oversized_result",
		Args: &structpb.Struct{},
	})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("code = %v, want ResourceExhausted just beyond the boundary", status.Code(err))
	}
}

// TestCallToolInternalResultPersistenceSemanticsUnchanged proves the
// wire-size guard lives only in CallRegisteredMcpTool's own gRPC-response
// path: CallTool itself — the lower-level method whose map[string]any
// result the runtime persists directly into message/tool-call history,
// never through a marshaled CallRegisteredMcpToolResponse — must still
// return an oversized result exactly as before, uninspected by this new
// cap.
func TestCallToolInternalResultPersistenceSemanticsUnchanged(t *testing.T) {
	h := newWireSizeCallHarness(t)
	h.resultJSON = numberArrayResultRawJSON(1_000_000)
	runID := h.runningRunID(t)

	result, err := h.registry.CallTool(context.Background(), CallInput{
		ServerID: h.serverID, RunID: runID, ToolName: "vendor.oversized_result",
	})
	if err != nil {
		t.Fatalf("CallTool must not itself enforce the wire-size cap: %v", err)
	}
	array, ok := result["x"].([]any)
	if !ok || len(array) == 0 {
		t.Fatalf("result = %+v, want the full decoded number array untouched", result)
	}
}

func bytesContainsAny(s string, substrings ...string) bool {
	for _, substring := range substrings {
		if len(substring) > 0 && bytes.Contains([]byte(s), []byte(substring)) {
			return true
		}
	}
	return false
}
