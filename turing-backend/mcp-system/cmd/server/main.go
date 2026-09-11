package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/project-turing/mcp-system/internal/auth"
	"github.com/project-turing/mcp-system/internal/jsonrpc"
	"github.com/project-turing/mcp-system/internal/tools"
)

const maxMCPRequestBytes = 1024 * 1024
const maxMCPResponseBytes = 1024 * 1024
const healthcheckTimeout = time.Second

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
		defer cancel()
		if err := checkHealth(ctx, "http://127.0.0.1:"+envOrDefault("PORT", "7100")+"/healthz"); err != nil {
			log.Fatal(err)
		}
		return
	}

	addr := ":" + envOrDefault("PORT", "7100")
	log.Printf("starting mcp-system on %s", addr)
	if err := newHTTPServer(addr, newHandler(os.Getenv("MCP_SYSTEM_TOKEN_GENERAL"))).ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}
}

func newHandler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/mcp", auth.RequireBearer(token, http.HandlerFunc(handleMCP)))
	return mux
}

func checkHealth(ctx context.Context, endpoint string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("MCP health endpoint returned %s", response.Status)
	}
	return nil
}

func handleMCP(w http.ResponseWriter, r *http.Request) {
	// GET (open a server-to-client SSE stream) and DELETE (terminate a session)
	// are both optional in the Streamable HTTP transport. This server offers
	// neither — it never pushes to a client and keeps no session — so 405 is
	// the answer the transport defines, and a stock client carries on.
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !checkTransportHeaders(w, r) {
		return
	}

	if r.ContentLength > maxMCPRequestBytes {
		writeJSONRPCStatus(w, http.StatusRequestEntityTooLarge, jsonrpc.Response{
			JSONRPC: "2.0",
			Error:   map[string]any{"code": -32600, "message": "request body too large"},
		})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMCPRequestBytes)
	req, requestErr := jsonrpc.DecodeRequest(r.Body)
	if requestErr != nil {
		statusCode := http.StatusOK
		var maxBytesErr *http.MaxBytesError
		if errors.As(requestErr, &maxBytesErr) {
			statusCode = http.StatusRequestEntityTooLarge
			requestErr = &jsonrpc.RequestError{
				Code:    -32600,
				Message: "request body too large",
			}
		}
		writeJSONRPCStatus(w, statusCode, jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      requestErr.ID,
			Error:   map[string]any{"code": requestErr.Code, "message": requestErr.Message},
		})
		return
	}

	if req.Notification && !notificationAllowed(req.Method) {
		// A request that is not one of the lifecycle notifications, sent
		// without an id, is dropped without doing any work. The transport
		// answers every notification 202 with no body, so serving it would
		// perform the method and discard both its result and its error.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if !req.Notification && notificationAllowed(req.Method) {
		// And the inverse: a `notifications/*` method has no request form, so
		// an id-bearing one is an unsupported shape and gets the same
		// -32601 every other unsupported method gets. Answering it with a
		// result would let a caller drive the cancellation through a shape the
		// protocol does not define.
		writeJSONRPC(w, jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   map[string]any{"code": -32601, "message": "method not found"},
		})
		return
	}

	switch req.Method {
	case "initialize":
		if paramsErr := validateInitializeParams(req); paramsErr != nil {
			writeJSONRPC(w, jsonrpc.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]any{"code": paramsErr.Code, "message": paramsErr.Message},
			})
			return
		}
		writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: initializeResult()})
	case "notifications/initialized":
		// Nothing to record: this server keeps no session, so the operation
		// phase needs no state to enter. The 202 is the whole answer.
		acceptNotification(w)
	case "notifications/cancelled":
		// Accepted and ignored, deliberately. Every tool this server exposes
		// computes its answer without blocking and without a context, so there
		// is nothing in flight a cancellation could stop; the specification
		// lets a receiver ignore a cancellation whose request cannot be
		// cancelled, and pretending otherwise with a registry that cancels
		// nothing would be theatre. mcp-files, whose calls do block on the
		// orchestrator, keeps a real one.
		acceptNotification(w)
	case "ping":
		if paramsErr := rejectUnknownParams(req, "_meta"); paramsErr != nil {
			writeJSONRPC(w, jsonrpc.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]any{"code": paramsErr.Code, "message": paramsErr.Message},
			})
			return
		}
		writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
	case "tools/list":
		if paramsErr := validateToolsListParams(req); paramsErr != nil {
			writeJSONRPC(w, jsonrpc.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]any{"code": paramsErr.Code, "message": paramsErr.Message},
			})
			return
		}
		writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": tools.List()}})
	case "tools/call":
		name, args, paramsErr := parseToolCallParams(req)
		if paramsErr != nil {
			writeJSONRPC(w, jsonrpc.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]any{"code": paramsErr.Code, "message": paramsErr.Message},
			})
			return
		}
		result, err := tools.Call(name, args)
		if err != nil {
			code := -32000
			if tools.IsInvalidParams(err) {
				code = -32602
			}
			writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": code, "message": err.Error()}})
			return
		}
		writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: callToolResult(result)})
	default:
		writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": -32601, "message": "method not found"}})
	}
}

// acceptNotification is the whole answer to a notification: 202, no body. The
// two id-shape guards at the top of handleMCP are what make this correct
// without a per-call check — a notification reaches the switch only for a
// method notificationAllowed permits, and such a method never reaches it
// carrying an id. Any method added to notificationAllowed must answer here.
func acceptNotification(w http.ResponseWriter) {
	w.WriteHeader(http.StatusAccepted)
}

func writeJSONRPC(w http.ResponseWriter, res jsonrpc.Response) {
	writeJSONRPCStatus(w, http.StatusOK, res)
}

func writeJSONRPCStatus(w http.ResponseWriter, statusCode int, res jsonrpc.Response) {
	var payload bytes.Buffer
	if err := json.NewEncoder(&payload).Encode(res); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
	if payload.Len() > maxMCPResponseBytes {
		// Discard the result, but keep the id: a conforming client matches
		// responses by id, so a null one turns a bounded failure into a call
		// that never resolves. The id cannot be what overran the cap — decodeID
		// refuses anything over maxIDBytes — so it is only dropped if the error
		// envelope is somehow still too large, which needs an id that never
		// came off the wire.
		payload.Reset()
		res.Result = nil
		res.Error = map[string]any{"code": -32603, "message": "response body too large"}
		if err := json.NewEncoder(&payload).Encode(res); err != nil {
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
			return
		}
		if payload.Len() > maxMCPResponseBytes {
			payload.Reset()
			res.ID = nil
			if err := json.NewEncoder(&payload).Encode(res); err != nil {
				http.Error(w, "failed to encode response", http.StatusInternalServerError)
				return
			}
		}
	}
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(payload.Bytes())
}

func parseToolCallParams(req jsonrpc.Request) (string, map[string]any, *jsonrpc.RequestError) {
	if paramsErr := rejectUnknownParams(req, "name", "arguments", "_meta"); paramsErr != nil {
		return "", nil, paramsErr
	}
	name, valid := req.Params["name"].(string)
	if !valid || strings.TrimSpace(name) == "" {
		return "", nil, jsonrpc.InvalidParams(req.ID, "name must be a non-empty string")
	}
	args := map[string]any{}
	if value, present := req.Params["arguments"]; present {
		var object bool
		args, object = value.(map[string]any)
		if !object || args == nil {
			return "", nil, jsonrpc.InvalidParams(req.ID, "arguments must be an object")
		}
	}
	if value, present := req.Params["_meta"]; present {
		meta, object := value.(map[string]any)
		if !object || meta == nil {
			return "", nil, jsonrpc.InvalidParams(req.ID, "_meta must be an object")
		}
		// Fail-closed like mcp-files, but scoped to what can legitimately arrive
		// here. approvalToken can: a user may raise a system tool to
		// approval_required, and the runtime then forwards the minted token to
		// whichever client the tool routes through, this server included. This
		// server verifies no approval — the orchestrator's policy decision and
		// consumption are the gate — so the token is accepted and ignored, and
		// refusing it would make an approved call fail after the user had
		// already approved it. provenanceToken cannot legitimately arrive:
		// nothing here writes into the sandbox, so a provenance capability is a
		// misrouted one and is refused. The orchestrator relies on that refusal
		// when it declines to issue a provenance capability to any server but
		// the file server; the reasoning lives with it, in orchestrator-go's
		// internal/service/runtime, since this module cannot see across the
		// boundary.
		// progressToken is the protocol's own member, accepted and ignored
		// because progress notifications are not implemented.
		for key := range meta {
			if key != "progressToken" && key != "approvalToken" {
				return "", nil, jsonrpc.InvalidParams(req.ID, "unknown _meta key")
			}
		}
	}
	return name, args, nil
}

func validateToolsListParams(req jsonrpc.Request) *jsonrpc.RequestError {
	if paramsErr := rejectUnknownParams(req, "cursor", "_meta"); paramsErr != nil {
		return paramsErr
	}
	if cursor, present := req.Params["cursor"]; present {
		text, valid := cursor.(string)
		if !valid {
			return jsonrpc.InvalidParams(req.ID, "cursor must be a string")
		}
		// This server returns its whole tool list in one page and never emits a
		// nextCursor, so no client can hold a cursor this server issued.
		// Honouring one by silently returning the first page again would be an
		// unsupported protocol feature quietly succeeding.
		if text != "" {
			return jsonrpc.InvalidParams(req.ID, "cursor is not supported: this server returns a single page")
		}
	}
	if meta, present := req.Params["_meta"]; present {
		if object, valid := meta.(map[string]any); !valid || object == nil {
			return jsonrpc.InvalidParams(req.ID, "_meta must be an object")
		}
	}
	return nil
}

func rejectUnknownParams(req jsonrpc.Request, allowed ...string) *jsonrpc.RequestError {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key := range req.Params {
		if _, ok := allowedSet[key]; !ok {
			return jsonrpc.InvalidParams(req.ID, "unknown params key")
		}
	}
	return nil
}

func envOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
