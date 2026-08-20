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

	"github.com/project-turing/mcp-files/internal/approval"
	"github.com/project-turing/mcp-files/internal/auth"
	"github.com/project-turing/mcp-files/internal/jsonrpc"
	"github.com/project-turing/mcp-files/internal/tools"
)

const maxMCPRequestBytes = 6*tools.MaxMutationContentBytes + 64*1024
const maxMCPResponseBytes = 1024 * 1024
const healthcheckTimeout = time.Second

type serverConfig struct {
	filesToken            string
	approvalJwtSecret     string
	orchestratorGRPCAddr  string
	approvalConsumerToken string
	sandboxRoot           string
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
		defer cancel()
		if err := checkHealth(ctx, "http://127.0.0.1:"+envOrDefault("PORT", "7110")+"/healthz"); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.sandboxRoot, 0700); err != nil {
		log.Fatal(err)
	}

	addr := ":" + envOrDefault("PORT", "7110")
	log.Printf("starting mcp-files on %s", addr)
	if err := newHTTPServer(addr, newHandler(cfg)).ListenAndServe(); err != nil {
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

func loadConfig() (serverConfig, error) {
	approvalJWTSecret := os.Getenv("TURING_APPROVAL_JWT_SECRET")
	if approvalJWTSecret == "" {
		return serverConfig{}, errors.New("TURING_APPROVAL_JWT_SECRET is required")
	}
	return serverConfig{
		filesToken:            os.Getenv("MCP_FILES_TOKEN_GENERAL"),
		approvalJwtSecret:     approvalJWTSecret,
		orchestratorGRPCAddr:  envOrDefault("ORCHESTRATOR_GRPC_ADDR", "turing-orchestrator:3001"),
		approvalConsumerToken: os.Getenv("TURING_APPROVAL_CONSUMER_TOKEN"),
		sandboxRoot:           envOrDefault("FILES_SANDBOX_ROOT", "/sandbox"),
	}, nil
}

func newHandler(cfg serverConfig) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	filesTools := tools.NewFilesTools(cfg.sandboxRoot).WithApprovalValidator(approval.Consumer{
		OrchestratorGRPCAddr:  cfg.orchestratorGRPCAddr,
		ApprovalConsumerToken: cfg.approvalConsumerToken,
		JWTSecret:             cfg.approvalJwtSecret,
	})
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agentID, err := auth.AgentFromBearer(r, cfg.filesToken)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handleMCP(w, r, filesTools, agentID)
	}))
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

func handleMCP(w http.ResponseWriter, r *http.Request, filesTools tools.FilesTools, agentID string) {
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
			writeJSONRPCForRequest(w, req, jsonrpc.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]any{"code": paramsErr.Code, "message": paramsErr.Message},
			})
			return
		}
		writeJSONRPCForRequest(w, req, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": listTools()}})
	case "tools/call":
		name, args, approvalToken, paramsErr := parseToolCallParams(req)
		if paramsErr != nil {
			writeJSONRPCForRequest(w, req, jsonrpc.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]any{"code": paramsErr.Code, "message": paramsErr.Message},
			})
			return
		}
		result, err := filesTools.CallContext(r.Context(), name, args, approvalToken, agentID)
		if err != nil {
			code := -32000
			if tools.IsInvalidParams(err) {
				code = -32602
			}
			writeJSONRPCForRequest(w, req, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": code, "message": err.Error()}})
			return
		}
		writeJSONRPCForRequest(w, req, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: result})
	default:
		writeJSONRPCForRequest(w, req, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": -32601, "message": "method not found"}})
	}
}

func writeJSONRPCForRequest(w http.ResponseWriter, req jsonrpc.Request, response jsonrpc.Response) {
	if req.Notification {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSONRPC(w, response)
}

func listTools() []map[string]any {
	return []map[string]any{
		{
			"name":        "files.list",
			"description": "List files and directories at a sandbox-relative path.",
			"inputSchema": objectSchema(map[string]any{
				"path":  pathStringSchema(),
				"limit": integerSchema(1, 1000),
			}, []any{}),
			"policy": "safe",
		},
		{
			"name":        "files.search",
			"description": "Search UTF-8 files for a query within a sandbox-relative path.",
			"inputSchema": objectSchema(map[string]any{
				"path":  pathStringSchema(),
				"query": nonBlankStringSchema("Nonblank text to find."),
				"limit": integerSchema(1, 200),
			}, []any{"query"}),
			"policy": "safe",
		},
		{
			"name":        "files.read",
			"description": "Read a UTF-8 file from a sandbox-relative path.",
			"inputSchema": objectSchema(map[string]any{
				"path":     pathStringSchema(),
				"maxBytes": integerSchema(1, tools.MaxReadBytes),
			}, []any{"path"}),
			"policy": "safe",
		},
		{
			"name":        "files.create",
			"description": "Create a UTF-8 file at a sandbox-relative path.",
			"inputSchema": objectSchema(map[string]any{"path": pathStringSchema(), "content": contentStringSchema()}, []any{"path", "content"}),
			"policy":      "approval_required",
		},
		{
			"name":        "files.update",
			"description": "Replace a UTF-8 file, optionally requiring its current SHA-256 hash.",
			"inputSchema": objectSchema(map[string]any{
				"path":         pathStringSchema(),
				"content":      contentStringSchema(),
				"expectedHash": expectedHashStringSchema(),
			}, []any{"path", "content"}),
			"policy": "approval_required",
		},
	}
}

func objectSchema(properties map[string]any, required []any) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func nonBlankStringSchema(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"minLength":   1,
		"pattern":     `\S`,
		"description": description,
	}
}

func pathStringSchema() map[string]any {
	return nonBlankStringSchema(fmt.Sprintf(
		"Sandbox-relative path limited to %d bytes, %d bytes per component, and %d components; byte limits are enforced by the server because JSON Schema maxLength counts characters.",
		tools.MaxSandboxPathBytes,
		tools.MaxSandboxComponentBytes,
		tools.MaxSandboxPathDepth,
	))
}

func contentStringSchema() map[string]any {
	return map[string]any{
		"type":        "string",
		"description": fmt.Sprintf("UTF-8 content with a %d-byte server-enforced limit; maxLength is intentionally omitted because it counts characters.", tools.MaxMutationContentBytes),
	}
}

func expectedHashStringSchema() map[string]any {
	const encodedHashLength = len("sha256:") + 64
	return map[string]any{
		"type":        "string",
		"minLength":   encodedHashLength,
		"maxLength":   encodedHashLength,
		"pattern":     `^sha256:[0-9a-f]{64}$`,
		"description": "Exact lowercase sha256: digest of the expected current content.",
	}
}

func integerSchema(minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
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
		res.ID = nil
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

func parseToolCallParams(req jsonrpc.Request) (string, map[string]any, string, *jsonrpc.RequestError) {
	if paramsErr := rejectUnknownParams(req, "name", "arguments", "_meta"); paramsErr != nil {
		return "", nil, "", paramsErr
	}
	name, valid := req.Params["name"].(string)
	if !valid || strings.TrimSpace(name) == "" {
		return "", nil, "", jsonrpc.InvalidParams(req.ID, "name must be a non-empty string")
	}
	args := map[string]any{}
	if value, present := req.Params["arguments"]; present {
		var object bool
		args, object = value.(map[string]any)
		if !object || args == nil {
			return "", nil, "", jsonrpc.InvalidParams(req.ID, "arguments must be an object")
		}
	}
	approvalToken := ""
	if value, present := req.Params["_meta"]; present {
		meta, object := value.(map[string]any)
		if !object || meta == nil {
			return "", nil, "", jsonrpc.InvalidParams(req.ID, "_meta must be an object")
		}
		for key := range meta {
			if key != "approvalToken" {
				return "", nil, "", jsonrpc.InvalidParams(req.ID, "unknown _meta key")
			}
		}
		if value, present := meta["approvalToken"]; present {
			var tokenString bool
			approvalToken, tokenString = value.(string)
			if !tokenString {
				return "", nil, "", jsonrpc.InvalidParams(req.ID, "_meta.approvalToken must be a string")
			}
		}
	}
	return name, args, approvalToken, nil
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
