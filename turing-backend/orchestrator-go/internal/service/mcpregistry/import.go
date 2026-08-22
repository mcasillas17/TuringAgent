package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	toolpolicy "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	mcpServerNamePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	localContainerHostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

// reservedMCPServerNames are names TuringAgent's bundled servers own; a
// caller cannot register or import over them regardless of tier.
var reservedMCPServerNames = map[string]struct{}{
	"system": {},
	"files":  {},
	"skills": {},
}

func isReservedMCPServerName(name string) bool {
	_, reserved := reservedMCPServerNames[name]
	return reserved
}

// errMCPServerNameReserved is the one message used for a reserved name
// wherever it is refused, whether that is an mcp.json import (recorded as
// unsupported) or a direct RegisterMcpServer call (returned as a status).
var errMCPServerNameReserved = errors.New("server name is reserved by TuringAgent")

// bundledServerRegistrationMessage is the wording used whenever the
// repository itself refuses a bundled-tier name collision, so an import and
// a direct registration say the same thing about the same refusal.
const bundledServerRegistrationMessage = "bundled server registration is managed by TuringAgent"

// mcpMissingIntegrationKeyMessage is the wording used whenever a bearer
// token is given but no integration key is configured to seal it with,
// whether that happens during an mcp.json import or a direct registration
// or rotation.
const mcpMissingIntegrationKeyMessage = "server token requires the TURING_INTEGRATION_KEY integration key so it can be stored sealed"

type Server struct {
	turingv1.UnimplementedMcpRegistryServiceServer
	repo         *repository.Repository
	sealer       *secretbox.Sealer
	httpClient   *http.Client
	approvals    ApprovalEnforcer
	notifier     RegistryChangeNotifier
	audit        AuditRecorder
	configRoot   string
	clientMu     sync.Mutex
	localClient  *http.Client
	remoteClient *http.Client
}

// AuditRecorder is the audit service, narrowed to the one method this
// package needs. Registering a server and rotating its token are both
// consequential changes to standing access, so each leaves a record; the
// record names the server and, for a rotation, whether a token is now
// configured — never the token itself.
type AuditRecorder interface {
	Record(ctx context.Context, correlationID, actorType, actorID, action, target string, payload map[string]any) error
}

// SetAuditRecorder wires an audit sink for registration and rotation. A nil
// (unset) recorder is fine: mutations still succeed, they are simply not
// audited, the same way SetRegistryChangeNotifier's nil case works.
func (s *Server) SetAuditRecorder(recorder AuditRecorder) {
	s.audit = recorder
}

// SetMCPConfigRoot configures the directory ReimportConfiguredJSON reads
// mcp.json from. Leaving it unset means ReimportConfiguredJSON refuses to
// run rather than guessing a location.
func (s *Server) SetMCPConfigRoot(root string) {
	s.configRoot = root
}

// auditMCPEvent records a registry mutation and never lets a failure to
// record it fail the mutation that already happened: the change is real
// either way, and refusing to report it would just be a second problem. A
// failure logs only the action and target/server id — never the payload,
// which is the one place a token could otherwise reach a log.
func (s *Server) auditMCPEvent(ctx context.Context, action, target string, payload map[string]any) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Record(ctx, "", "client", "", action, target, payload); err != nil {
		log.Printf("record %s for %s failed", action, target)
	}
}

type ImportReport struct {
	Imported    []string
	Skipped     []string
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
	imported := make([]string, 0)
	skipped := make([]string, 0)
	for name, raw := range document.Servers {
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
		rawToken, err := bearerFromHeaders(entry.Headers)
		if err != nil {
			report.Unsupported[name] = err.Error()
			continue
		}
		// A file import never asks for a particular tier: whichever tier
		// the URL classifies as is the tier used.
		validated, err := validateServerDefinition(name, entry.URL, nil, rawToken)
		if err != nil {
			report.Unsupported[name] = err.Error()
			continue
		}
		tier, canonicalURL, token := validated.Tier, validated.URL, validated.Token

		// Reimport is create-only: detect an existing (or tombstoned)
		// name before sealing anything, so a reimport of a server that
		// already exists never needs a sealer at all, and never touches
		// its enabled state, endpoint, token, or tools snapshot. The one
		// narrow exception is a legacy placeholder from migration 0016
		// (non-bundled, url == ""): it was seeded disabled with no real
		// endpoint solely so a pre-registry runtime's tool policy and
		// schema survived, and it must be adoptable rather than skipped
		// forever, so it falls through to sealing/import below like a
		// brand-new name.
		existing, err := s.repo.GetMCPServerByName(ctx, name)
		switch {
		case err == nil:
			if existing.Tier == repository.MCPServerTierBundled {
				report.Unsupported[name] = bundledServerRegistrationMessage
				continue
			}
			if existing.URL != "" {
				skipped = append(skipped, name)
				continue
			}
		case errors.Is(err, repository.ErrMCPServerNotFound):
			tombstoned, terr := s.repo.MCPServerTombstoned(ctx, name)
			if terr != nil {
				return ImportReport{}, fmt.Errorf("check MCP server %q tombstone: %w", name, terr)
			}
			if tombstoned {
				report.Unsupported[name] = "server was removed locally and remains suppressed; use a new name to import it again"
				continue
			}
		default:
			return ImportReport{}, fmt.Errorf("look up MCP server %q: %w", name, err)
		}

		sealed, err := sealMCPServerToken(s.sealer, name, token)
		if err != nil {
			if errors.Is(err, secretbox.ErrNoKey) {
				report.Unsupported[name] = mcpMissingIntegrationKeyMessage
				continue
			}
			return ImportReport{}, errors.New("seal MCP server token")
		}
		result, err := s.repo.ImportMCPServer(ctx, repository.ImportedMCPServer{
			Name:        name,
			URL:         canonicalURL,
			SealedToken: sealed,
			Tier:        tier,
		})
		if err != nil {
			if errors.Is(err, repository.ErrMCPServerBundled) {
				report.Unsupported[name] = bundledServerRegistrationMessage
				continue
			}
			if errors.Is(err, repository.ErrMCPServerImportSuppressed) {
				report.Unsupported[name] = "server was removed locally and remains suppressed; use a new name to import it again"
				continue
			}
			return ImportReport{}, fmt.Errorf("import MCP server %q: %w", name, err)
		}
		if !result.Created {
			// Lost a race with a concurrent import/registration between
			// the disposition check above and this call: treat it like
			// the ordinary skip path rather than surfacing an error.
			skipped = append(skipped, name)
			continue
		}
		imported = append(imported, name)
		server := result.Server
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
	sort.Strings(imported)
	sort.Strings(skipped)
	report.Imported = imported
	report.Skipped = skipped
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
		if !strings.HasPrefix(value, prefix) {
			return "", errors.New("authorization header must use a non-empty Bearer token")
		}
		normalized, err := normalizeBearerToken(strings.TrimPrefix(value, prefix))
		if err != nil {
			return "", err
		}
		if normalized == "" {
			return "", errors.New("authorization header must use a non-empty Bearer token")
		}
		token = normalized
	}
	return token, nil
}

// normalizeBearerToken applies the one set of rules a bearer token must
// satisfy regardless of whether it arrived as an mcp.json Authorization
// header or a RegisterMcpServer/RotateMcpServerToken bearer_token field:
// surrounding whitespace is trimmed, and an empty result means "no token"
// rather than an error. A token that would corrupt the line-based MCP
// Authorization header it is eventually sent in — a line break or any other
// control character — is refused instead of silently truncated or escaped.
func normalizeBearerToken(raw string) (string, error) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", nil
	}
	if strings.ContainsAny(token, "\r\n") {
		return "", errors.New("authorization bearer must not contain line breaks")
	}
	for _, r := range token {
		if unicode.IsControl(r) {
			return "", errors.New("authorization bearer must not contain control characters")
		}
	}
	return token, nil
}

// validatedMCPServer is the outcome of validateServerDefinition: a name,
// canonical URL, normalized bearer token, and classified tier that are safe
// to hand to the repository. Sealing the token is deliberately left to the
// caller — it needs a sealer, and what a sealing failure should look like
// differs between an mcp.json import (recorded as unsupported) and a direct
// RegisterMcpServer/RotateMcpServerToken call (returned as a status).
type validatedMCPServer struct {
	Name  string
	URL   string
	Token string
	Tier  repository.MCPServerTier
}

// validateServerDefinition applies the one set of rules a server definition
// must satisfy regardless of whether it arrived through an mcp.json import
// or a direct RegisterMcpServer call: the name must match
// mcpServerNamePattern and must not be one of the names TuringAgent reserves
// for its bundled servers; the URL must classify as exactly one tier (a
// stdio-shaped value such as "npx vendor" has no host and is refused the
// same way a malformed URL is); and, when the caller already knows which
// tier it asked for, that tier must agree with the URL's classification
// rather than silently overriding it. The bearer token, if any, is
// normalized the same way for both callers.
func validateServerDefinition(name, rawURL string, requestedTier *repository.MCPServerTier, token string) (validatedMCPServer, error) {
	if !mcpServerNamePattern.MatchString(name) {
		return validatedMCPServer{}, errors.New("server name is invalid")
	}
	if isReservedMCPServerName(name) {
		return validatedMCPServer{}, errMCPServerNameReserved
	}
	tier, canonicalURL, err := classifyImportedURL(rawURL)
	if err != nil {
		return validatedMCPServer{}, err
	}
	if requestedTier != nil && *requestedTier != tier {
		return validatedMCPServer{}, errors.New("requested tier does not match the url's classification")
	}
	normalizedToken, err := normalizeBearerToken(token)
	if err != nil {
		return validatedMCPServer{}, err
	}
	return validatedMCPServer{Name: name, URL: canonicalURL, Token: normalizedToken, Tier: tier}, nil
}

// sealMCPServerToken seals a plaintext bearer token for storage, bound to
// name so a stored ciphertext cannot be moved to another server's row and
// still decrypt. An empty token seals to nothing — a server with no token
// configured never needs a sealer at all.
func sealMCPServerToken(sealer *secretbox.Sealer, name, token string) ([]byte, error) {
	if token == "" {
		return nil, nil
	}
	return sealer.Seal([]byte(token), []byte(name))
}

// sealServerToken is sealMCPServerToken shaped for the RPC surface: a
// missing integration key becomes the fixed FailedPrecondition status used
// by both RegisterMcpServer and RotateMcpServerToken, and any other sealing
// failure becomes a generic Internal status rather than a raw error.
func (s *Server) sealServerToken(name, token string) ([]byte, error) {
	sealed, err := sealMCPServerToken(s.sealer, name, token)
	if err != nil {
		if errors.Is(err, secretbox.ErrNoKey) {
			return nil, status.Error(codes.FailedPrecondition, mcpMissingIntegrationKeyMessage)
		}
		return nil, status.Error(codes.Internal, "seal MCP server token failed")
	}
	return sealed, nil
}

// errMCPConfigRootNotConfigured is returned by ReimportConfiguredJSON when
// no config root has been set; the public RPC maps it to FailedPrecondition
// rather than guessing a location or silently no-op'ing.
var errMCPConfigRootNotConfigured = errors.New("MCP config root is not configured")

// ReimportConfiguredJSON re-reads mcp.json from the configured config root
// and imports it, the same way app startup does. An absent file is not a
// failure: it clears any previously recorded import issues and reports an
// empty ImportReport, so a client that reimports after deleting mcp.json
// sees a clean slate rather than a stale error. A malformed document is
// recorded as a bounded "_document" entry in mcp_import_issues and returned
// inside the ImportReport rather than as an error, so a client can display
// why nothing imported the same way it displays any other refused entry.
// Any other failure to read the file is returned as a fixed error that
// never repeats the file's contents or path.
func (s *Server) ReimportConfiguredJSON(ctx context.Context) (ImportReport, error) {
	if s.configRoot == "" {
		return ImportReport{}, errMCPConfigRootNotConfigured
	}
	data, err := os.ReadFile(filepath.Join(s.configRoot, "mcp.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
		if clearErr := s.repo.ReplaceMCPImportIssues(ctx, map[string]string{}); clearErr != nil {
			return ImportReport{}, fmt.Errorf("clear mcp.json import issues: %w", clearErr)
		}
		return ImportReport{}, nil
	case err != nil:
		log.Printf("read mcp.json: %v", err)
		return ImportReport{}, errors.New("read mcp.json failed")
	}
	report, err := s.ImportJSON(ctx, data)
	if err != nil {
		message := boundedStatusMessage(err.Error())
		if recordErr := s.repo.ReplaceMCPImportIssues(ctx, map[string]string{"_document": message}); recordErr != nil {
			return ImportReport{}, fmt.Errorf("record mcp.json import failure: %w", recordErr)
		}
		return ImportReport{Unsupported: map[string]string{"_document": message}}, nil
	}
	return report, nil
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
	address, ok := netip.AddrFromSlice(ip)
	return !ok || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() ||
		isSpecialUseMCPAddress(address.Unmap())
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
