package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	toolpolicy "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/tools"
)

var (
	mcpServerNamePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	localContainerHostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type Server struct {
	turingv1.UnimplementedMcpRegistryServiceServer
	repo         *repository.Repository
	sealer       *secretbox.Sealer
	httpClient   *http.Client
	approvals    ApprovalEnforcer
	notifier     RegistryChangeNotifier
	clientMu     sync.Mutex
	localClient  *http.Client
	remoteClient *http.Client
}

type ImportReport struct {
	Unsupported map[string]string
}

type DiscoveredTool struct {
	Name       string
	SchemaJSON string
}

type mcpJSON struct {
	Servers map[string]json.RawMessage `json:"mcpServers"`
}

type mcpJSONServer struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Tools   []mcpJSONTool     `json:"tools"`
}

type mcpJSONTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func New(repo *repository.Repository, sealer *secretbox.Sealer, httpClient *http.Client) *Server {
	return &Server{repo: repo, sealer: sealer, httpClient: httpClient}
}

func (s *Server) ImportJSON(ctx context.Context, data []byte) (ImportReport, error) {
	if s == nil || s.repo == nil {
		return ImportReport{}, errors.New("MCP registry repository is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var document mcpJSON
	if err := decoder.Decode(&document); err != nil {
		return ImportReport{}, fmt.Errorf("decode mcp.json: %w", err)
	}
	if err := requireImportEOF(decoder); err != nil {
		return ImportReport{}, fmt.Errorf("decode mcp.json: %w", err)
	}
	if document.Servers == nil {
		return ImportReport{}, errors.New("decode mcp.json: mcpServers object is required")
	}

	report := ImportReport{Unsupported: make(map[string]string)}
	for name, raw := range document.Servers {
		if !mcpServerNamePattern.MatchString(name) {
			report.Unsupported[name] = "server name is invalid"
			continue
		}
		if name == "system" || name == "files" || name == "skills" || name == "integrations" {
			report.Unsupported[name] = "server name is reserved by TuringAgent"
			continue
		}
		var entry mcpJSONServer
		entryDecoder := json.NewDecoder(bytes.NewReader(raw))
		entryDecoder.DisallowUnknownFields()
		if err := entryDecoder.Decode(&entry); err != nil {
			report.Unsupported[name] = "entry is invalid: " + err.Error()
			continue
		}
		var entryFields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entryFields); err != nil {
			report.Unsupported[name] = "entry is invalid"
			continue
		}
		_, toolsPresent := entryFields["tools"]
		if entry.Command != "" {
			report.Unsupported[name] = "stdio/command MCP servers are unsupported; run the server in a container or use an HTTPS URL"
			continue
		}
		tier, canonicalURL, err := classifyImportedURL(entry.URL)
		if err != nil {
			report.Unsupported[name] = err.Error()
			continue
		}
		token, err := bearerFromHeaders(entry.Headers)
		if err != nil {
			report.Unsupported[name] = err.Error()
			continue
		}
		var sealed []byte
		if token != "" {
			sealed, err = s.sealer.Seal([]byte(token), []byte(name))
			if err != nil {
				if errors.Is(err, secretbox.ErrNoKey) {
					report.Unsupported[name] = "server token requires the TURING_INTEGRATION_KEY integration key so it can be stored sealed"
					continue
				}
				return ImportReport{}, errors.New("seal MCP server token")
			}
		}
		server, err := s.repo.UpsertImportedMCPServer(ctx, repository.ImportedMCPServer{
			Name:        name,
			URL:         canonicalURL,
			SealedToken: sealed,
			Tier:        tier,
		})
		if err != nil {
			if errors.Is(err, repository.ErrMCPServerBundled) {
				report.Unsupported[name] = "bundled server registration is managed by TuringAgent"
				continue
			}
			if errors.Is(err, repository.ErrMCPServerImportSuppressed) {
				report.Unsupported[name] = "server was removed locally and remains suppressed; use a new name to import it again"
				continue
			}
			return ImportReport{}, fmt.Errorf("import MCP server %q: %w", name, err)
		}
		if toolsPresent {
			discovered := make([]DiscoveredTool, 0, len(entry.Tools))
			valid := true
			for index, tool := range entry.Tools {
				if strings.TrimSpace(tool.Name) == "" {
					report.Unsupported[name] = fmt.Sprintf("tool %d has an invalid name", index)
					valid = false
					break
				}
				schema := tool.InputSchema
				if schema == nil {
					schema = map[string]any{"type": "object"}
				}
				if rootType, present := schema["type"]; present && rootType != "object" {
					report.Unsupported[name] = fmt.Sprintf("tool %q inputSchema root type must be object", tool.Name)
					valid = false
					break
				}
				schema["type"] = "object"
				encoded, err := json.Marshal(schema)
				if err != nil {
					report.Unsupported[name] = fmt.Sprintf("tool %q inputSchema is invalid", tool.Name)
					valid = false
					break
				}
				discovered = append(discovered, DiscoveredTool{Name: tool.Name, SchemaJSON: string(encoded)})
			}
			if valid {
				if err := s.RecordDiscovery(ctx, server.ID, discovered); err != nil {
					report.Unsupported[name] = boundedStatusMessage(err.Error())
				}
			}
		}
	}
	if err := s.repo.ReplaceMCPImportIssues(ctx, report.Unsupported); err != nil {
		return ImportReport{}, fmt.Errorf("record mcp.json import issues: %w", err)
	}
	return report, nil
}

func (s *Server) RecordDiscovery(ctx context.Context, serverID string, discovered []DiscoveredTool) error {
	server, err := s.repo.GetMCPServer(ctx, serverID)
	if err != nil {
		return err
	}
	tools := make([]repository.MCPServerTool, 0, len(discovered))
	for _, tool := range discovered {
		if server.Tier != repository.MCPServerTierBundled {
			if owner, bundled := toolpolicy.BundledServerForTool(tool.Name); bundled {
				return fmt.Errorf("%w: %s is owned by bundled server %s", repository.ErrMCPToolNameCollision, tool.Name, owner)
			}
		}
		tools = append(tools, repository.MCPServerTool{
			Name:       tool.Name,
			Policy:     string(toolpolicy.DefaultPolicyFor(server.Name, tool.Name)),
			SchemaJSON: tool.SchemaJSON,
		})
	}
	return s.repo.ReplaceMCPServerTools(ctx, serverID, tools)
}

func classifyImportedURL(raw string) (repository.MCPServerTier, string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Host == "" {
		return "", "", errors.New("url must be an absolute HTTP(S) endpoint")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", errors.New("url must not contain userinfo, a query, or a fragment")
	}
	if parsed.Path == "" {
		parsed.Path = "/mcp"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	host := strings.ToLower(parsed.Hostname())
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		if host == "localhost" || net.ParseIP(host) != nil && isNonPublicIP(net.ParseIP(host)) {
			return "", "", errors.New("remote URL must use a public HTTPS host")
		}
		parsed.Scheme = "https"
		parsed.Host = canonicalHostPort(host, parsed.Port())
		endpoint, err := backendegress.ParseKeyedEndpoint(parsed.String())
		if err != nil {
			return "", "", errors.New("remote URL must be a canonical HTTPS endpoint")
		}
		return repository.MCPServerTierRemoteURL, endpoint.Canonical, nil
	case "http":
		if parsed.Port() == "" || !localContainerHostPattern.MatchString(host) ||
			host == "localhost" || host == "host.docker.internal" ||
			strings.HasPrefix(host, "turing-") || net.ParseIP(host) != nil {
			return "", "", errors.New("local container URL must use an explicit port and an isolated Docker service name")
		}
		parsed.Scheme = "http"
		parsed.Host = canonicalHostPort(host, parsed.Port())
		return repository.MCPServerTierLocalContainer, parsed.String(), nil
	default:
		return "", "", errors.New("url must use HTTPS remotely or HTTP for an isolated local container")
	}
}

func bearerFromHeaders(headers map[string]string) (string, error) {
	token := ""
	for name, value := range headers {
		if !strings.EqualFold(name, "authorization") {
			return "", fmt.Errorf("header %q is unsupported; only Authorization: Bearer is accepted", name)
		}
		const prefix = "Bearer "
		if !strings.HasPrefix(value, prefix) || strings.TrimSpace(strings.TrimPrefix(value, prefix)) == "" {
			return "", errors.New("authorization header must use a non-empty Bearer token")
		}
		token = strings.TrimSpace(strings.TrimPrefix(value, prefix))
		if strings.ContainsAny(token, "\r\n") {
			return "", errors.New("authorization bearer must not contain line breaks")
		}
	}
	return token, nil
}

func canonicalHostPort(host, port string) string {
	if port == "" {
		if strings.Contains(host, ":") {
			return "[" + host + "]"
		}
		return host
	}
	return net.JoinHostPort(host, port)
}

func isNonPublicIP(ip net.IP) bool {
	return !backendegress.IsPublicIP(ip)
}

func requireImportEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
