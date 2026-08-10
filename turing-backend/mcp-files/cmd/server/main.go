package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/project-turing/mcp-files/internal/approval"
	"github.com/project-turing/mcp-files/internal/auth"
	"github.com/project-turing/mcp-files/internal/jsonrpc"
	"github.com/project-turing/mcp-files/internal/tools"
)

const maxMCPRequestBytes = 6*tools.MaxMutationContentBytes + 64*1024

type serverConfig struct {
	filesToken           string
	approvalJwtSecret    string
	orchestratorGRPCAddr string
	internalToken        string
	sandboxRoot          string
}

func main() {
	cfg := loadConfig()
	if err := os.MkdirAll(cfg.sandboxRoot, 0700); err != nil {
		log.Fatal(err)
	}

	addr := ":" + envOrDefault("PORT", "7110")
	log.Printf("starting mcp-files on %s", addr)
	if err := http.ListenAndServe(addr, newHandler(cfg)); err != nil {
		log.Fatal(err)
	}
}

func loadConfig() serverConfig {
	return serverConfig{
		filesToken:           os.Getenv("MCP_FILES_TOKEN_GENERAL"),
		approvalJwtSecret:    os.Getenv("TURING_APPROVAL_JWT_SECRET"),
		orchestratorGRPCAddr: envOrDefault("ORCHESTRATOR_GRPC_ADDR", "turing-orchestrator:3001"),
		internalToken:        os.Getenv("TURING_INTERNAL_TOKEN"),
		sandboxRoot:          envOrDefault("FILES_SANDBOX_ROOT", "/sandbox"),
	}
}

func newHandler(cfg serverConfig) http.Handler {
	mux := http.NewServeMux()
	filesTools := tools.NewFilesTools(cfg.sandboxRoot).WithApprovalValidator(approval.Consumer{
		OrchestratorGRPCAddr: cfg.orchestratorGRPCAddr,
		InternalToken:        cfg.internalToken,
		JWTSecret:            cfg.approvalJwtSecret,
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
			writeJSONRPC(w, jsonrpc.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   map[string]any{"code": paramsErr.Code, "message": paramsErr.Message},
			})
			return
		}
		writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": listTools()}})
	case "tools/call":
		name, args, approvalToken, paramsErr := parseToolCallParams(req)
		if paramsErr != nil {
			writeJSONRPC(w, jsonrpc.Response{
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
			writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": code, "message": err.Error()}})
			return
		}
		writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: result})
	default:
		writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": -32601, "message": "method not found"}})
	}
}

func listTools() []map[string]any {
	return []map[string]any{
		{
			"name":        "files.list",
			"description": "List files and directories at a sandbox-relative path.",
			"inputSchema": objectSchema(map[string]any{
				"path":  stringSchema(),
				"limit": integerSchema(1, 1000),
			}, []any{}),
			"policy": "safe",
		},
		{
			"name":        "files.search",
			"description": "Search UTF-8 files for a query within a sandbox-relative path.",
			"inputSchema": objectSchema(map[string]any{
				"path":  stringSchema(),
				"query": stringSchema(),
				"limit": integerSchema(1, 200),
			}, []any{"query"}),
			"policy": "safe",
		},
		{
			"name":        "files.read",
			"description": "Read a UTF-8 file from a sandbox-relative path.",
			"inputSchema": objectSchema(map[string]any{
				"path":     stringSchema(),
				"maxBytes": integerSchema(1, 524288),
			}, []any{"path"}),
			"policy": "safe",
		},
		{
			"name":        "files.create",
			"description": "Create a UTF-8 file at a sandbox-relative path.",
			"inputSchema": objectSchema(map[string]any{"path": stringSchema(), "content": contentStringSchema()}, []any{"path", "content"}),
			"policy":      "approval_required",
		},
		{
			"name":        "files.update",
			"description": "Replace a UTF-8 file, optionally requiring its current SHA-256 hash.",
			"inputSchema": objectSchema(map[string]any{
				"path":         stringSchema(),
				"content":      contentStringSchema(),
				"expectedHash": stringSchema(),
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

func stringSchema() map[string]any {
	return map[string]any{"type": "string"}
}

func contentStringSchema() map[string]any {
	return map[string]any{"type": "string", "maxLength": tools.MaxMutationContentBytes}
}

func integerSchema(minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
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
