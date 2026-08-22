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
	"strconv"
	"strings"
	"sync"
	"time"
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

// isReservedMCPServerName compares case-insensitively: mcpServerNamePattern
// itself accepts mixed-case names, so "Files", "SYSTEM", or "sKiLlS" would
// otherwise register or import successfully and shadow a bundled server's
// namespace under a differently-cased name.
func isReservedMCPServerName(name string) bool {
	_, reserved := reservedMCPServerNames[strings.ToLower(name)]
	return reserved
}

// validateMCPServerName is the one name check every entry point into the
// registry runs: the name must match mcpServerNamePattern and must not be
// one of the names TuringAgent reserves for its bundled servers. Both
// ImportJSON (applied per mcp.json entry, before that entry's body is even
// decoded, so a malformed body under a reserved/invalid key is still
// refused for the name rather than a decode error) and
// validateServerDefinition (used by RegisterMcpServer/RotateMcpServerToken)
// call this one implementation, so there is exactly one place either path
// could diverge from.
func validateMCPServerName(name string) error {
	if !mcpServerNamePattern.MatchString(name) {
		return errors.New("server name is invalid")
	}
	if isReservedMCPServerName(name) {
		return errMCPServerNameReserved
	}
	return nil
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
	// enableDiscoveryTimeoutOverride replaces enableDiscoveryTimeout when
	// set (test-only, zero value unused in production): it lets a test
	// prove the whole-operation timeout wrapper itself bounds discovery,
	// with a caller context that carries no deadline of its own, instead
	// of relying on a short caller-supplied deadline that would pass even
	// without the wrapper.
	enableDiscoveryTimeoutOverride time.Duration
	// reimportBarrier, when set (test-only, nil/unused in production), is
	// invoked by ReimportMcpJson after notify/audit have run and
	// immediately before the response is built from the in-memory
	// report. It lets a test force a genuine interleaving with a second,
	// concurrent ReimportMcpJson call sharing the same repository —
	// rather than hoping -race happens to catch one — and then assert
	// this call's response still reflects only its own report, never
	// whatever the other call did to the shared mcp_import_issues table
	// in the meantime.
	reimportBarrier func()
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

// auditRecordTimeout bounds the detached context auditMCPEvent derives so a
// slow or wedged audit sink cannot hang indefinitely after the mutation it
// is recording has already committed.
const auditRecordTimeout = 5 * time.Second

// postCommitStatusTimeout bounds the detached context SetMcpServerEnabled
// derives for a liveness-status write that happens after the enable/disable
// mutation has already committed. It uses the same duration as
// auditRecordTimeout for the same reason: a slow or wedged store must not
// hang indefinitely after the change it is recording already happened.
const postCommitStatusTimeout = auditRecordTimeout

// detachedBoundedContext derives a context for work that must run after an
// earlier mutation has already committed: it is detached from the caller's
// cancellation/deadline via context.WithoutCancel and given its own small
// bounded timeout, so a client that cancels (or any downstream cancellation
// racing the request, such as the registry-change notifier) can never skip
// or race a durable record of a change that already happened.
func detachedBoundedContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), timeout)
}

// discoveryTimeout returns enableDiscoveryTimeout, or
// enableDiscoveryTimeoutOverride when a test has set one.
func (s *Server) discoveryTimeout() time.Duration {
	if s.enableDiscoveryTimeoutOverride > 0 {
		return s.enableDiscoveryTimeoutOverride
	}
	return enableDiscoveryTimeout
}

// auditMCPEvent records a registry mutation and never lets a failure to
// record it fail the mutation that already happened: the change is real
// either way, and refusing to report it would just be a second problem. A
// failure logs only the action and target/server id — never the payload,
// which is the one place a token could otherwise reach a log.
//
// The write happens after the mutation it describes has already committed,
// so it must not inherit the client's request context as-is: a client that
// cancels (or a post-commit side effect, such as the registry-change
// notifier, that ends up cancelling it) must never race or skip a durable
// record of a change that already happened. The context is therefore
// detached from cancellation/deadline via detachedBoundedContext instead.
func (s *Server) auditMCPEvent(ctx context.Context, action, target string, payload map[string]any) {
	if s.audit == nil {
		return
	}
	auditCtx, cancel := detachedBoundedContext(ctx, auditRecordTimeout)
	defer cancel()
	if err := s.audit.Record(auditCtx, "", "client", "", action, target, payload); err != nil {
		log.Printf("record %s for %s failed", action, target)
	}
}

type ImportReport struct {
	Imported    []string
	Skipped     []string
	Unsupported map[string]string
}

// maxMCPUnsupportedNameBytes bounds the untrusted "name" an mcp.json entry
// is filed under before recordUnsupported ever writes it into the
// in-memory report, mcp_import_issues, or (via ListMcpServers/
// ReimportMcpJson) a client/UI response. A JSON object's keys carry no
// length limit of their own — unlike a name that actually passed
// validateMCPServerName, which mcpServerNamePattern already caps at 64
// characters — so an invalid, arbitrarily long key must be bounded here
// rather than assumed short just because a *valid* one always would be.
// 64, not maxMCPStatusMessageBytes's 512, matches that same valid-name
// ceiling: nothing longer could ever have been a real, accepted name
// anyway, so preserving more of an invalid one adds no useful signal.
const maxMCPUnsupportedNameBytes = 64

// boundedMCPServerNameForDisplay bounds an mcp.json entry's own untrusted
// name/key the same way boundedStatusMessage bounds its reason: trimmed to
// valid UTF-8, so a truncated but still-recognizable prefix survives
// rather than a raw byte cut that could split a multi-byte character.
func boundedMCPServerNameForDisplay(name string) string {
	return boundedUTF8(name, maxMCPUnsupportedNameBytes)
}

// recordUnsupported is the one place ImportJSON writes an unsupported
// reason, so every reason — whether a fixed short message or one built
// from attacker-controlled input such as a header name or a tool name — is
// bounded and valid UTF-8 the same way boundedStatusMessage already bounds
// a discovery/status error, rather than some call sites bounding it and
// others writing raw, unbounded strings. The entry's own name/key is
// bounded too (see maxMCPUnsupportedNameBytes): it is exactly as
// untrusted and unbounded-by-anything-upstream as the reason text is, and
// this is the one place both ever get persisted or returned from.
//
// Using the bounded name as the map key means two distinct invalid names
// that happen to share the same maxMCPUnsupportedNameBytes-byte prefix
// collapse to one entry — the later one's reason silently wins the map
// write, and one refused-entry diagnostic (and, transitively, one count
// toward ListMcpServers/ReimportMcpJson's "refused" total) is lost. That
// is an accepted, diagnostic-only trade-off: neither colliding entry ever
// registers either way (both are already refused before this call), no
// secret or token is at stake, and preventing it would mean either
// growing the bound back toward the unbounded case this function exists
// to close, or introducing a disambiguating suffix that would itself need
// its own bound. It is not a correctness bug in whether an entry is
// accepted or refused — only in how precisely a pathological pair of
// refusals is reported back.
func recordUnsupported(unsupported map[string]string, name, reason string) {
	unsupported[boundedMCPServerNameForDisplay(name)] = boundedStatusMessage(reason)
}

type DiscoveredTool struct {
	Name       string
	SchemaJSON string
}

type mcpJSON struct {
	Servers map[string]json.RawMessage `json:"mcpServers"`
}

type mcpJSONServer struct {
	URL string `json:"url"`
	// Headers is captured as raw, undecoded JSON — not map[string]string
	// — so decodeMCPHeaderEntries can walk its exact key/value tokens
	// before anything collapses two JSON object members that share the
	// exact same key into a single, last-value-wins map entry.
	Headers json.RawMessage   `json:"headers"`
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

// mcpToolDefinitionRefusedMessage is the fixed, generic reason
// buildImportTools returns when a tool's metadata — its name, its
// description, or any part of its serialized schema — contains the
// entry's own configured bearer token verbatim, or when the same static
// snapshot names the same tool more than once. It deliberately never says
// why: naming the token, or even the word "token" or "metadata", would
// confirm to whoever controls that mcp.json entry (or is probing what
// refuses an import) exactly which check tripped and that a secret
// comparison happens at all — so every one of these cases reads exactly
// like any other malformed-entry refusal, including from each other.
const mcpToolDefinitionRefusedMessage = "server entry is invalid"

// mcpRawMetadataContainsToken recursively walks a decoded (not yet
// marshaled) JSON value — as produced by unmarshaling an mcp.json tool's
// inputSchema into map[string]any — looking for token as an exact
// substring of any string it finds: a map key, a string value, or a
// string nested inside a list, at any depth. Running this scan on the raw
// decoded value, before anything is marshaled back into JSON text, is
// what makes it able to catch a token containing a quote or backslash: had
// this instead scanned the marshaled JSON text (as buildImportTools used
// to), json.Marshal would have re-escaped exactly those two characters
// (`"` to `\"`, `\` to `\\`), and a plain substring search for the raw,
// unescaped token would never find it there.
func mcpRawMetadataContainsToken(value any, token string) bool {
	if token == "" {
		return false
	}
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, token)
	case map[string]any:
		for key, item := range typed {
			if strings.Contains(key, token) || mcpRawMetadataContainsToken(item, token) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if mcpRawMetadataContainsToken(item, token) {
				return true
			}
		}
	}
	return false
}

// buildImportTools fully validates an mcp.json entry's optional "tools"
// snapshot before ImportJSON ever touches the repository: every tool's
// name and schema must be well-formed, no two tools may share a name, no
// tool may name a bundled server's namespace, and the entry's own
// configured bearer token must never appear verbatim anywhere in a tool's
// metadata — its name, its description (never stored or returned, but
// still scanned: see mcpJSONTool.Description), or any map key or nested
// string/list value inside its schema. Any failure refuses the whole
// snapshot (returning it, and hence the entry, is left entirely to the
// caller — nothing here mutates anything), so an invalid, duplicated, or
// token-bearing definition can never leave a partial row a corrected
// reimport could only skip.
//
// The duplicate-name and token-metadata checks both run before any error
// message that would embed the tool's name (the schema-shape checks
// below, and buildRepositoryTool's own bundled-namespace-collision
// message) and use the one fixed, generic, sentinel-free
// mcpToolDefinitionRefusedMessage: a tool that is both token-bearing (or
// duplicated) and otherwise invalid must still be refused the same way,
// never a more specific message that would print the offending name or
// schema back out. The token-metadata scan itself runs over the raw,
// decoded schema value via mcpRawMetadataContainsToken — before
// json.Marshal below ever serializes it — precisely so a token containing
// a quote or backslash cannot hide behind whatever escaping that encoding
// step would apply to it.
//
// A static snapshot is bounded by the exact same maxMCPTools/
// maxMCPToolBytes limits mcpClient.listTools applies to a live
// tools/list response, counted the same way plus one addition: the tool
// count against maxMCPTools, and a running total of each tool's
// serialized name, its already-built schemaJSON, and its description
// (never stored, but still counted — otherwise an oversized description
// could inflate this call's real footprint while wholly evading the
// limit) against maxMCPToolBytes. Either limit being exceeded refuses the
// whole snapshot with a fixed, generic message — bounded and free of the
// offending name or schema, the same way an over-limit live discovery
// response never repeats it — before anything here has mutated the
// repository, so there is never a partial row a corrected, smaller
// snapshot could only skip.
func buildImportTools(tier repository.MCPServerTier, serverName string, rawTools []mcpJSONTool, token string) ([]repository.MCPServerTool, error) {
	if len(rawTools) > maxMCPTools {
		return nil, fmt.Errorf("static tools snapshot exceeds limit of %d tools", maxMCPTools)
	}
	tools := make([]repository.MCPServerTool, 0, len(rawTools))
	encodedBytes := 0
	seenNames := make(map[string]struct{}, len(rawTools))
	for index, tool := range rawTools {
		if strings.TrimSpace(tool.Name) == "" {
			return nil, fmt.Errorf("tool %d has an invalid name", index)
		}
		if _, duplicate := seenNames[tool.Name]; duplicate {
			return nil, errors.New(mcpToolDefinitionRefusedMessage)
		}
		seenNames[tool.Name] = struct{}{}

		schema := tool.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}

		// The token-metadata scan runs before the schema-shape check
		// below (and before it defaults/mutates schema["type"], so it
		// sees exactly what the entry supplied): a tool whose schema is
		// independently invalid (e.g. a non-object root type) must still
		// be refused with the one generic, sentinel-free reason if it is
		// also token-bearing, never the shape-check's own message, which
		// embeds the tool's name via %q and would print a sentinel-
		// bearing name back out.
		if token != "" && (strings.Contains(tool.Name, token) ||
			strings.Contains(tool.Description, token) ||
			mcpRawMetadataContainsToken(schema, token)) {
			return nil, errors.New(mcpToolDefinitionRefusedMessage)
		}

		if rootType, present := schema["type"]; present && rootType != "object" {
			return nil, fmt.Errorf("tool %q inputSchema root type must be object", tool.Name)
		}
		schema["type"] = "object"

		encoded, err := json.Marshal(schema)
		if err != nil {
			return nil, fmt.Errorf("tool %q inputSchema is invalid", tool.Name)
		}
		schemaJSON := string(encoded)
		size := len(tool.Name) + len(schemaJSON) + len(tool.Description)
		if size > maxMCPToolBytes-encodedBytes {
			return nil, fmt.Errorf("static tools snapshot exceeds encoded descriptor limit of %d bytes", maxMCPToolBytes)
		}
		encodedBytes += size
		built, err := buildRepositoryTool(tier, serverName, tool.Name, schemaJSON)
		if err != nil {
			return nil, err
		}
		tools = append(tools, built)
	}
	return tools, nil
}

// buildRepositoryTool assembles a single repository.MCPServerTool from a
// name and an already-serialized schema, applying the one bundled-namespace
// collision guard and the one DefaultPolicyFor default that both an
// mcp.json static snapshot (buildImportTools) and live discovery
// (RecordDiscovery) share, so neither path can silently drift from the
// other on what a newly seen tool's policy or namespace rules are.
func buildRepositoryTool(tier repository.MCPServerTier, serverName, name, schemaJSON string) (repository.MCPServerTool, error) {
	if tier != repository.MCPServerTierBundled {
		if owner, bundled := toolpolicy.BundledServerForTool(name); bundled {
			return repository.MCPServerTool{}, fmt.Errorf("%w: %s is owned by bundled server %s", repository.ErrMCPToolNameCollision, name, owner)
		}
	}
	return repository.MCPServerTool{
		Name:       name,
		Policy:     string(toolpolicy.DefaultPolicyFor(serverName, name)),
		SchemaJSON: schemaJSON,
	}, nil
}

func New(repo *repository.Repository, sealer *secretbox.Sealer, httpClient *http.Client) *Server {
	return &Server{repo: repo, sealer: sealer, httpClient: httpClient}
}

func (s *Server) ImportJSON(ctx context.Context, data []byte) (ImportReport, error) {
	if s == nil || s.repo == nil {
		return ImportReport{}, errors.New("MCP registry repository is required")
	}
	// The whole document's raw size is bounded before it is ever handed
	// to json.Decoder, independent of (and prior to) any per-server or
	// per-tool limit below: most of a document sits outside any single
	// entry's own "tools" snapshot, so maxMCPToolBytes alone never bounds
	// how much this call would otherwise buffer and decode. The message
	// stays generic and bounded — a byte count and the fixed cap, never
	// any of the document's own content — and nothing below it, including
	// the repository, is ever touched for an oversized document.
	if len(data) > maxMCPImportDocumentBytes {
		return ImportReport{}, fmt.Errorf("mcp.json exceeds the maximum supported document size of %d bytes", maxMCPImportDocumentBytes)
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
	// Entries are processed in sorted name order, never Go's randomized
	// map iteration order: when two entries in the same document claim
	// the same tool name, whichever is processed first wins the
	// repository's inter-server collision check (see
	// replaceServerToolsTx) and the other is refused. Sorting first
	// makes that winner the lexicographically first server name, every
	// time, rather than a randomized one that could differ between two
	// imports of the identical file.
	names := make([]string, 0, len(document.Servers))
	for name := range document.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		raw := document.Servers[name]
		// The name check runs before the entry's body is even decoded:
		// a reserved or pattern-invalid name is refused for that reason
		// regardless of whether its body would otherwise fail strict
		// decoding, sharing the exact validateServerDefinition also
		// uses so the two paths can never independently drift apart.
		if err := validateMCPServerName(name); err != nil {
			recordUnsupported(report.Unsupported, name, err.Error())
			continue
		}
		var entry mcpJSONServer
		entryDecoder := json.NewDecoder(bytes.NewReader(raw))
		entryDecoder.DisallowUnknownFields()
		if err := entryDecoder.Decode(&entry); err != nil {
			recordUnsupported(report.Unsupported, name, "entry is invalid: "+err.Error())
			continue
		}
		var entryFields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entryFields); err != nil {
			recordUnsupported(report.Unsupported, name, "entry is invalid")
			continue
		}
		_, toolsPresent := entryFields["tools"]
		if entry.Command != "" {
			recordUnsupported(report.Unsupported, name, "stdio/command MCP servers are unsupported; run the server in a container or use an HTTPS URL")
			continue
		}
		headerEntries, err := decodeMCPHeaderEntries(entry.Headers)
		if err != nil {
			recordUnsupported(report.Unsupported, name, err.Error())
			continue
		}
		rawToken, err := bearerFromHeaders(headerEntries)
		if err != nil {
			recordUnsupported(report.Unsupported, name, err.Error())
			continue
		}
		// A file import never asks for a particular tier: whichever tier
		// the URL classifies as is the tier used.
		validated, err := validateServerDefinition(name, entry.URL, nil, rawToken)
		if err != nil {
			recordUnsupported(report.Unsupported, name, err.Error())
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
				recordUnsupported(report.Unsupported, name, bundledServerRegistrationMessage)
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
				recordUnsupported(report.Unsupported, name, "server was removed locally and remains suppressed; use a new name to import it again")
				continue
			}
		default:
			return ImportReport{}, fmt.Errorf("look up MCP server %q: %w", name, err)
		}

		// Every static tool this entry declares is fully validated —
		// name, schema, bundled-namespace collision, and the entry's own
		// token never appearing verbatim in a tool's name or schema —
		// before anything below touches the repository, so an invalid or
		// token-bearing snapshot refuses the whole entry (Unsupported
		// only, never also Imported) rather than leaving a partial row a
		// corrected reimport could only skip.
		var tools []repository.MCPServerTool
		if toolsPresent {
			tools, err = buildImportTools(tier, name, entry.Tools, token)
			if err != nil {
				recordUnsupported(report.Unsupported, name, err.Error())
				continue
			}
		}

		sealed, err := sealMCPServerToken(s.sealer, name, token)
		if err != nil {
			if errors.Is(err, secretbox.ErrNoKey) {
				recordUnsupported(report.Unsupported, name, mcpMissingIntegrationKeyMessage)
				continue
			}
			return ImportReport{}, errors.New("seal MCP server token")
		}
		result, err := s.repo.ImportMCPServer(ctx, repository.ImportedMCPServer{
			Name:        name,
			URL:         canonicalURL,
			SealedToken: sealed,
			Tier:        tier,
			Tools:       tools,
		})
		if err != nil {
			switch {
			case errors.Is(err, repository.ErrMCPServerBundled):
				recordUnsupported(report.Unsupported, name, bundledServerRegistrationMessage)
			case errors.Is(err, repository.ErrMCPServerImportSuppressed):
				recordUnsupported(report.Unsupported, name, "server was removed locally and remains suppressed; use a new name to import it again")
			case errors.Is(err, repository.ErrMCPToolNameCollision):
				// The repository's own transaction already rolled back
				// the row insert/adoption entirely (see
				// ImportMCPServer): no row remains for this name, so a
				// corrected reimport (dropping or renaming the
				// colliding tool) starts clean rather than skipping a
				// poisoned partial row.
				recordUnsupported(report.Unsupported, name, err.Error())
			default:
				return ImportReport{}, fmt.Errorf("import MCP server %q: %w", name, err)
			}
			continue
		}
		if !result.Created {
			// Lost a race with a concurrent import/registration between
			// the disposition check above and this call: treat it like
			// the ordinary skip path rather than surfacing an error.
			skipped = append(skipped, name)
			continue
		}
		imported = append(imported, name)
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
		built, err := buildRepositoryTool(server.Tier, server.Name, tool.Name, tool.SchemaJSON)
		if err != nil {
			return err
		}
		tools = append(tools, built)
	}
	return s.repo.ReplaceMCPServerTools(ctx, serverID, tools)
}

func classifyImportedURL(raw string) (repository.MCPServerTier, string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Host == "" {
		return "", "", errors.New("url must be an absolute HTTP(S) endpoint")
	}
	// ForceQuery is url.Parse's own signal for a bare trailing "?" with no
	// query string after it (e.g. ".../mcp?"): RawQuery stays empty in
	// that case — there is nothing after the "?" to be empty or not — so
	// checking RawQuery != "" alone would let it silently through even
	// though the URL still names an (empty) query, same as the leading
	// checks below refuse a query with real key/value content.
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", "", errors.New("url must not contain userinfo, a query, or a fragment")
	}
	// url.Parse's own Port() only guarantees a decimal-digit string (or
	// empty); it never checks that string names a usable 16-bit TCP port.
	// Applied once here, before either tier branch, so an explicit port
	// outside 1-65535 (":0", or one that overflows a uint16) is refused
	// identically for a remote-URL or local-container endpoint — an
	// absent port is unaffected and is still handled per-tier below.
	if port := parsed.Port(); port != "" && !validMCPPort(port) {
		return "", "", errors.New("url port must be a number between 1 and 65535")
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

// errMultipleAuthorizationHeaders is the fixed reason bearerFromHeaders
// returns when an mcp.json entry's headers object carries more than one
// case-insensitive "authorization" key (e.g. both "Authorization" and
// "authorization" present as distinct JSON keys, which decode into
// distinct map entries). Go map iteration order is randomized, so
// silently taking whichever one happens to be visited last — even when
// both carry the identical value — would make the accepted token a
// randomized winner rather than a deterministic outcome. Refusing
// outright instead means the result can never depend on that order.
var errMultipleAuthorizationHeaders = errors.New("only a single authorization header is accepted")

// mcpHeaderEntry is a single key/value pair from an mcp.json entry's raw
// "headers" JSON object, preserved exactly as decodeMCPHeaderEntries found
// it on the wire — including a name that repeats another entry's, whether
// by an exact spelling match or only a case-insensitive one. A
// map[string]string cannot represent that: two JSON object members that
// share the exact same key decode into one map entry, last-value-wins,
// which would let bearerFromHeaders's duplicate-Authorization check never
// even see the duplicate it exists to refuse.
type mcpHeaderEntry struct {
	Name  string
	Value string
}

// decodeMCPHeaderEntries parses an mcp.json entry's raw "headers" value
// with a token-level walk (json.Decoder.Token/More), rather than
// json.Unmarshal into a map[string]string, specifically so that two JSON
// object members sharing the exact same key are preserved as two separate
// entries instead of being silently collapsed into one before
// bearerFromHeaders ever sees them. A missing or JSON-null "headers" value
// is not an error — both mean "no headers", matching what an absent or
// null map[string]string field decoded to previously — but any other
// non-object value, or any non-string member value, is refused: this
// function is the one place that decides what a "headers" value may
// shape as, now that the field itself is captured as raw, undecoded
// json.RawMessage precisely so this can inspect it before anything
// collapses it into a map.
func decodeMCPHeaderEntries(raw json.RawMessage) ([]mcpHeaderEntry, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	open, err := decoder.Token()
	if err != nil {
		return nil, errors.New("headers must be an object")
	}
	if delim, ok := open.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("headers must be an object")
	}
	var entries []mcpHeaderEntry
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, errors.New("headers must be an object")
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("headers must be an object")
		}
		var value string
		if err := decoder.Decode(&value); err != nil {
			return nil, errors.New("header values must be strings")
		}
		entries = append(entries, mcpHeaderEntry{Name: key, Value: value})
	}
	if _, err := decoder.Token(); err != nil { // the closing '}'
		return nil, errors.New("headers must be an object")
	}
	return entries, nil
}

// bearerFromHeaders never lets Go's randomized map iteration influence its
// outcome: it sorts the header entries by name first, so which unsupported
// header is named in the error (when more than one is present) is always
// the lexicographically first one, not whichever one iteration happened to
// visit first, and the fixed precedence between the two possible refusals
// — an unsupported header name versus more than one case-insensitive
// Authorization key — is the same regardless of how many of either are
// present: an unsupported header, if any, is always reported first, the
// same way the original single-pass loop always failed on the first
// non-Authorization key it happened to visit before it could ever reach
// the duplicate-Authorization check below. Neither the chosen unsupported
// name nor errMultipleAuthorizationHeaders' fixed message ever includes a
// header's value. Taking entries (preserving every occurrence, including
// an exact-spelling repeat of the same name) rather than a
// map[string]string is what lets the duplicate-Authorization check below
// ever see two entries both literally named "Authorization" in the first
// place — decodeMCPHeaderEntries is what makes that possible; a map could
// never have held them both.
func bearerFromHeaders(headers []mcpHeaderEntry) (string, error) {
	sorted := make([]mcpHeaderEntry, len(headers))
	copy(sorted, headers)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var unsupportedName string
	var authValue string
	authCount := 0
	for _, entry := range sorted {
		if !strings.EqualFold(entry.Name, "authorization") {
			if unsupportedName == "" {
				unsupportedName = entry.Name
			}
			continue
		}
		authCount++
		authValue = entry.Value
	}
	if unsupportedName != "" {
		return "", fmt.Errorf("header %q is unsupported; only Authorization: ****** accepted", unsupportedName)
	}
	if authCount > 1 {
		return "", errMultipleAuthorizationHeaders
	}
	if authCount == 0 {
		return "", nil
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(authValue, prefix) {
		return "", errors.New("authorization header must use a non-empty ******")
	}
	normalized, err := normalizeBearerToken(strings.TrimPrefix(authValue, prefix))
	if err != nil {
		return "", err
	}
	if normalized == "" {
		return "", errors.New("authorization header must use a non-empty ******")
	}
	return normalized, nil
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

// String and GoString redact Token so that %v, %+v, %#v, or %s on a
// validatedMCPServer — via a stray log.Printf, an error wrap, or a test
// failure message — can never print the plaintext bearer it carries before
// the caller seals it.
func (v validatedMCPServer) String() string {
	return fmt.Sprintf("validatedMCPServer{Name:%q, URL:%q, Tier:%q, Token:REDACTED}", v.Name, v.URL, v.Tier)
}

func (v validatedMCPServer) GoString() string {
	return v.String()
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
	if err := validateMCPServerName(name); err != nil {
		return validatedMCPServer{}, err
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

// validMCPPort reports whether portStr — as returned by url.URL.Port(), so
// already guaranteed to contain only decimal digits, or be empty — names a
// usable TCP port: 1-65535. url.Parse itself never rejects an
// out-of-range or zero port on its own; Port() only guarantees a
// decimal-digit string, not that it fits the actual 16-bit port space.
// ParseUint with a 16-bit size bound rejects both a non-numeric string
// (defensively, in case a caller ever passes one) and one that overflows
// that range in the same call; port == 0 is the one in-range-looking value
// ParseUint would otherwise accept, so it is refused explicitly.
func validMCPPort(portStr string) bool {
	port, err := strconv.ParseUint(portStr, 10, 16)
	return err == nil && port >= 1
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
