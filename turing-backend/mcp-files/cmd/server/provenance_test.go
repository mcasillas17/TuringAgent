package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	filetools "github.com/project-turing/mcp-files/internal/tools"
	"google.golang.org/grpc"
)

// testProvenanceToken mints what the orchestrator would issue for one call, so
// the handler is exercised through the same capability check production uses.
func testProvenanceToken(t *testing.T, tool string, args map[string]any, logicalPath string) string {
	t.Helper()
	argsHash, err := filetools.CanonicalArgsHash(args)
	if err != nil {
		t.Fatal(err)
	}
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"iss": "turing.orchestrator", "aud": "mcp-files", "sub": "general_assistant",
		"jti": "prov_1", "kind": "provenance", "sid": "sess_1", "rid": "run_1", "gen": 0,
		"tool": tool, "args_hash": argsHash, "path": logicalPath,
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	input := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte("jwt-secret"))
	mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func provenanceCallBody(t *testing.T, tool string, args map[string]any, logicalPath string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": args,
			"_meta":     map[string]any{"provenanceToken": testProvenanceToken(t, tool, args, logicalPath)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestMcpHandlerRejectsToolCallWithoutProvenanceCapability(t *testing.T) {
	handler := testFilesHandler(t)

	status, response := callFilesMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.list","arguments":{}}}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	assertRPCErrorCode(t, response, -32602)
	if !strings.Contains(string(response), "provenance") {
		t.Fatalf("response = %s, want a refusal naming the missing capability", response)
	}
}

func TestMcpHandlerAcceptsProvenanceCapabilityForSafeTool(t *testing.T) {
	sandbox := t.TempDir()
	if err := os.WriteFile(filepath.Join(sandbox, "note.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(serverConfig{
		filesToken:           "files-token",
		approvalJwtSecret:    "jwt-secret",
		orchestratorGRPCAddr: startFakeOrchestrator(t, true),
		sandboxRoot:          sandbox,
	})
	args := map[string]any{"path": "note.txt"}

	status, response := callFilesMCP(t, handler, provenanceCallBody(t, "files.read", args, "note.txt"))

	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, response)
	}
	var envelope struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error != nil {
		t.Fatalf("response error = %v", envelope.Error)
	}
	if envelope.Result["content"] != "hello" {
		t.Fatalf("result = %+v, want the pre-existing root file read through the capability", envelope.Result)
	}
}

func TestMcpHandlerRejectsCapabilityIssuedForAnotherCall(t *testing.T) {
	handler := testFilesHandler(t)
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name":      "files.read",
			"arguments": map[string]any{"path": "note.txt"},
			"_meta": map[string]any{
				"provenanceToken": testProvenanceToken(t, "files.read", map[string]any{"path": "other.txt"}, "other.txt"),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, response := callFilesMCP(t, handler, string(body))

	if !strings.Contains(string(response), "provenance") {
		t.Fatalf("response = %s, want the capability refused for a different call", response)
	}
}

func TestMcpHandlerRejectsUnknownMetaKeys(t *testing.T) {
	handler := testFilesHandler(t)

	_, response := callFilesMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.list","arguments":{},"_meta":{"somethingElse":"x"}}}`)

	assertRPCErrorCode(t, response, -32602)
}

func TestMcpHandlerRejectsNonStringProvenanceToken(t *testing.T) {
	handler := testFilesHandler(t)

	_, response := callFilesMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.list","arguments":{},"_meta":{"provenanceToken":7}}}`)

	assertRPCErrorCode(t, response, -32602)
}

// fakeOrchestrator answers the one internal RPC a safe tool call now depends
// on. Reads reach it over the same channel production uses, so the handler is
// exercised with a real client rather than a stub.
type fakeOrchestrator struct {
	turingv1.UnimplementedApprovalServiceServer
	active bool
}

func (f *fakeOrchestrator) CheckSessionCapability(context.Context, *turingv1.CheckSessionCapabilityRequest) (*turingv1.SessionCapabilityState, error) {
	return &turingv1.SessionCapabilityState{Active: f.active}, nil
}

func startFakeOrchestrator(t *testing.T, active bool) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	turingv1.RegisterApprovalServiceServer(server, &fakeOrchestrator{active: active})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}

func TestSafeToolIsRefusedWhileTheSessionIsBeingWithdrawn(t *testing.T) {
	sandbox := t.TempDir()
	if err := os.WriteFile(filepath.Join(sandbox, "note.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(serverConfig{
		filesToken:           "files-token",
		approvalJwtSecret:    "jwt-secret",
		internalToken:        "internal-token",
		orchestratorGRPCAddr: startFakeOrchestrator(t, false),
		sandboxRoot:          sandbox,
	})

	_, response := callFilesMCP(t, handler, provenanceCallBody(t, "files.read", map[string]any{"path": "note.txt"}, "note.txt"))

	if !strings.Contains(string(response), "deletion") {
		t.Fatalf("response = %s, want the read refused for a withdrawing session", response)
	}
	if strings.Contains(string(response), "secret") {
		t.Fatalf("response = %s leaks the file's content", response)
	}
}

func TestSafeToolIsRefusedWhenTheSessionSubtreeIsNamedDirectly(t *testing.T) {
	sandbox := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sandbox, "sessions", "sess_2", "runs", "run_9", "files"), 0700); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(serverConfig{
		filesToken:           "files-token",
		approvalJwtSecret:    "jwt-secret",
		internalToken:        "internal-token",
		orchestratorGRPCAddr: startFakeOrchestrator(t, true),
		sandboxRoot:          sandbox,
	})

	_, response := callFilesMCP(t, handler, provenanceCallBody(t, "files.list", map[string]any{"path": "sessions"}, "sessions"))

	if strings.Contains(string(response), "sess_2") {
		t.Fatalf("response = %s enumerates other sessions", response)
	}
	assertRPCErrorCode(t, response, -32602)
}
