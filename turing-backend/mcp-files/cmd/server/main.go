package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/project-turing/mcp-files/internal/approval"
	"github.com/project-turing/mcp-files/internal/auth"
	"github.com/project-turing/mcp-files/internal/jsonrpc"
	"github.com/project-turing/mcp-files/internal/tools"
)

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

	var req jsonrpc.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	switch req.Method {
	case "tools/list":
		writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": listTools()}})
	case "tools/call":
		name, _ := req.Params["name"].(string)
		args, _ := req.Params["arguments"].(map[string]any)
		if args == nil {
			args = map[string]any{}
		}
		approvalToken := approvalTokenFromParams(req.Params)
		result, err := filesTools.Call(name, args, approvalToken, agentID)
		if err != nil {
			writeJSONRPC(w, jsonrpc.Response{JSONRPC: "2.0", ID: req.ID, Error: map[string]any{"code": -32000, "message": err.Error()}})
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
			"inputSchema": objectSchema(map[string]any{"path": stringSchema()}, []any{}),
			"policy":      "safe",
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
			}, []any{}),
			"policy": "safe",
		},
		{
			"name":        "files.create",
			"description": "Create a UTF-8 file at a sandbox-relative path.",
			"inputSchema": objectSchema(map[string]any{"path": stringSchema(), "content": stringSchema()}, []any{"path", "content"}),
			"policy":      "approval_required",
		},
		{
			"name":        "files.update",
			"description": "Replace a UTF-8 file, optionally requiring its current SHA-256 hash.",
			"inputSchema": objectSchema(map[string]any{
				"path":         stringSchema(),
				"content":      stringSchema(),
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

func integerSchema(minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
}

func approvalTokenFromParams(params map[string]any) string {
	meta, _ := params["_meta"].(map[string]any)
	token, _ := meta["approvalToken"].(string)
	return token
}

func writeJSONRPC(w http.ResponseWriter, res jsonrpc.Response) {
	w.Header().Set("content-type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func envOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
