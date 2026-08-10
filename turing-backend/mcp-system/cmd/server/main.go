package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/project-turing/mcp-system/internal/auth"
	"github.com/project-turing/mcp-system/internal/jsonrpc"
	"github.com/project-turing/mcp-system/internal/tools"
)

const maxMCPRequestBytes = 1024 * 1024

func main() {
	addr := ":" + envOrDefault("PORT", "7100")
	log.Printf("starting mcp-system on %s", addr)
	if err := http.ListenAndServe(addr, newHandler(os.Getenv("MCP_SYSTEM_TOKEN_GENERAL"))); err != nil {
		log.Fatal(err)
	}
}

func newHandler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/mcp", auth.RequireBearer(token, http.HandlerFunc(handleMCP)))
	return mux
}

func handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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

	switch req.Method {
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
			writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": -32000, "message": err.Error()}})
			return
		}
		writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: result})
	default:
		writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": -32601, "message": "method not found"}})
	}
}

func writeJSONRPC(w http.ResponseWriter, res jsonrpc.Response) {
	writeJSONRPCStatus(w, http.StatusOK, res)
}

func writeJSONRPCStatus(w http.ResponseWriter, statusCode int, res jsonrpc.Response) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func parseToolCallParams(req jsonrpc.Request) (string, map[string]any, *jsonrpc.RequestError) {
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
	}
	return name, args, nil
}

func validateToolsListParams(req jsonrpc.Request) *jsonrpc.RequestError {
	if cursor, present := req.Params["cursor"]; present {
		if _, valid := cursor.(string); !valid {
			return jsonrpc.InvalidParams(req.ID, "cursor must be a string")
		}
	}
	if meta, present := req.Params["_meta"]; present {
		if object, valid := meta.(map[string]any); !valid || object == nil {
			return jsonrpc.InvalidParams(req.ID, "_meta must be an object")
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
