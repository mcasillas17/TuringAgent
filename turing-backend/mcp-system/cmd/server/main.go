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
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("MCP health endpoint returned %s", response.Status)
	}
	return nil
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
			code := -32000
			if tools.IsInvalidParams(err) {
				code = -32602
			}
			writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": code, "message": err.Error()}})
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
	var payload bytes.Buffer
	if err := json.NewEncoder(&payload).Encode(res); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
	if payload.Len() > maxMCPResponseBytes {
		payload.Reset()
		res.Result = nil
		res.Error = map[string]any{"code": -32603, "message": "response body too large"}
		if err := json.NewEncoder(&payload).Encode(res); err != nil {
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
			return
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
	}
	return name, args, nil
}

func validateToolsListParams(req jsonrpc.Request) *jsonrpc.RequestError {
	if paramsErr := rejectUnknownParams(req, "cursor", "_meta"); paramsErr != nil {
		return paramsErr
	}
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
