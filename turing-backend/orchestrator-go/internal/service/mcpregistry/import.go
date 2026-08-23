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
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	mcpServerNamePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	localContainerHostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

// reservedMCPServerNames are names TuringAgent's own first-party servers
// own — the three bundled MCP servers plus the "integrations" pseudo-server
// that owns the `github.` tool namespace (see UpdateToolPolicyByName and
// ListPseudoServerTools) — and a caller cannot register or import over any
// of them regardless of tier.
var reservedMCPServerNames = map[string]struct{}{
	"system":       {},
	"files":        {},
	"skills":       {},
	"integrations": {},
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

// invalidMCPEntryNameMessage is the one fixed, generic reason ImportJSON
// records for an mcp.json entry whose own key/name fails
// validateMCPServerName — never validateMCPServerName's own, more specific
// error (pattern-invalid vs. reserved), and never anything derived from
// the entry's own name. Both of those would still be safe to return on
// their own, but the entry is recorded under a synthetic label instead of
// its own name (see invalidMCPEntryLabel) precisely because that name
// might not be safe to keep at all, and a reason that varied with which
// specific check failed would be one more surface for a future change to
// accidentally start interpolating it back in.
const invalidMCPEntryNameMessage = "server name is invalid or reserved"

// invalidMCPEntryLabel returns a bounded, deterministic, synthetic label
// for the n-th (1-indexed) mcp.json entry ImportJSON refuses for an
// invalid or reserved name, in the same sorted-by-name order ImportJSON
// already processes entries in (see invalidEntryOrdinal in ImportJSON):
// "_invalid_server_1", "_invalid_server_2", and so on. A leading "_"
// mirrors recordUnsupported's own reserved "_document" overflow key, and
// (like it) can never collide with a name that actually passed
// validateMCPServerName: mcpServerNamePattern requires a leading
// alphanumeric character, which "_" is not. This is the one label
// recordUnsupported records an invalid/reserved entry's refusal under
// instead of that entry's own untrusted raw name — see the doc comment at
// ImportJSON's own call site for why.
func invalidMCPEntryLabel(ordinal int) string {
	return fmt.Sprintf("_invalid_server_%d", ordinal)
}

// bundledServerRegistrationMessage is the wording used whenever the
// repository itself refuses a bundled-tier name collision, so an import and
// a direct registration say the same thing about the same refusal.
const bundledServerRegistrationMessage = "bundled server registration is managed by TuringAgent"

// mcpServerRegistryFullMessage is the wording used whenever the repository
// refuses to create a new non-bundled server row because
// repository.MaxNonBundledMCPServers has already been reached — the same
// message whether that happens through an mcp.json import (recorded as an
// Unsupported reason) or a direct RegisterMcpServer call (returned as a
// ResourceExhausted status), so neither path can drift from the other on
// what this specific refusal says.
const mcpServerRegistryFullMessage = "MCP server registry is full"

// mcpMissingIntegrationKeyMessage is the wording used whenever a bearer
// token is given but no integration key is configured to seal it with,
// whether that happens during an mcp.json import or a direct registration
// or rotation.
const mcpMissingIntegrationKeyMessage = "server token requires the TURING_INTEGRATION_KEY integration key so it can be stored sealed"

// mcpRegistryToolBudgetExceededMessage is the wording used whenever
// ImportMCPServer refuses an entry's static tools snapshot because
// accepting it would push the registry's aggregate present-tool byte
// total (repository.MaxMCPRegistryToolBytes, enforced transactionally by
// replaceServerToolsTx) over budget. Recorded as an ordinary per-entry
// Unsupported refusal — exactly like ErrMCPServerRegistryFull and
// ErrMCPToolNameCollision immediately below already are — rather than an
// error that aborts the rest of the document: a document naming several
// entries once the registry-wide tool budget is already exhausted must
// still report every one of them refused, deterministically, and must
// never discard an earlier entry in the same run that already committed
// successfully (the repository's own transaction for the offending entry
// itself has already rolled back completely by the time this runs — see
// ErrMCPRegistryToolBudgetExceeded's own doc comment — so there is never a
// partial row left behind for a corrected reimport to get stuck skipping).
const mcpRegistryToolBudgetExceededMessage = "MCP registry tool budget is exhausted"

// mcpThirdPartyToolBudgetExceededMessage is
// mcpRegistryToolBudgetExceededMessage's counterpart for
// repository.ErrMCPThirdPartyToolBudgetExceeded: an entry's static tools
// snapshot refused because accepting it would push the narrower
// third-party-only share of the aggregate (repository.
// MaxThirdPartyMCPRegistryToolBytes) over its own budget, distinct from
// the full aggregate being exhausted. Recorded the same ordinary,
// per-entry, never-document-aborting way for the same reason.
const mcpThirdPartyToolBudgetExceededMessage = "MCP registry third-party tool budget is exhausted"

type Server struct {
	turingv1.UnimplementedMcpRegistryServiceServer
	repo       *repository.Repository
	sealer     *secretbox.Sealer
	httpClient *http.Client
	approvals  ApprovalEnforcer
	notifier   RegistryChangeNotifier
	audit      AuditRecorder
	configRoot string
	// beforeConfigFileOpen, when set (test-only, nil in production), is
	// invoked by ReimportConfiguredJSON with mcp.json's full path
	// immediately before it opens that path. It lets a test win a
	// TOCTOU-shaped race deterministically — replacing a regular file
	// with a FIFO or a symlink at exactly the moment this call commits to
	// opening it — rather than relying on genuine goroutine-scheduling
	// timing to land in an already-narrow window.
	beforeConfigFileOpen func(path string)
	clientMu             sync.Mutex
	localClient          *http.Client
	remoteClient         *http.Client
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
	// importEntryBarrier, when set (test-only, nil/unused in
	// production), is invoked by ImportJSON immediately after an entry's
	// own repository transaction (ImportMCPServer) has committed a
	// freshly-created server — after it is appended to the in-memory
	// imported slice, so a test firing this can already see that name
	// reflected in whatever report a concurrent read observes. It lets a
	// test force a genuine interleaving exactly between two entries: a
	// context cancellation, or some other fatal interruption, landing
	// after the first entry has already durably committed but before the
	// next one is even attempted — rather than hoping a real cancellation
	// race happens to land there. Passed the committed entry's own name,
	// purely for a test's own bookkeeping/assertions; ImportJSON itself
	// does nothing with the return value (there is none).
	importEntryBarrier func(name string)
	// credentialLocksMu guards credentialLocks, the lazily-populated map
	// of per-server credential fences (see credentialLock). It is held
	// only for the map lookup/insert itself, never across any I/O or the
	// per-server lock it returns, so it is never a source of contention
	// between unrelated servers.
	credentialLocksMu sync.Mutex
	// credentialLocks is keyed by MCP server id, one *sync.RWMutex per
	// server that has ever needed one (see credentialLock). This replaces
	// an earlier single, process-global sync.RWMutex that fenced every
	// server's CallTool/discover/RotateMcpServerToken against every
	// other's: under that design, an in-flight call or discovery against
	// server A held the *same* lock a concurrent rotation for completely
	// unrelated server B needed, so B's rotation (and any call to B)
	// waited on A's traffic for no correctness reason — the fence only
	// ever needs to be per-server. A map entry is removed on that
	// server's successful DeleteMcpServer (see forgetCredentialLock), and
	// also by CallTool/discoverLocked/rotateServerTokenLocked themselves
	// whenever their own post-lock re-read discovers the server no
	// longer exists (see forgetCredentialLockIfCurrent) — needed because
	// any of those three may not call credentialLock for a given server
	// until after DeleteMcpServer has already forgotten it, and
	// credentialLock's own lazy-create behavior would otherwise reinstate
	// an entry DeleteMcpServer will never forget again. Together, steady-
	// state size tracks the registry's own row count — bounded by
	// repository.MaxNonBundledMCPServers plus the fixed, small number of
	// bundled servers — rather than growing without bound across
	// register/delete cycles over a long-running process's lifetime. A
	// goroutine that obtained a server's lock object just before its
	// entry was removed keeps operating on that same object safely: Go's
	// sync.RWMutex does not need to remain reachable from any particular
	// map to function, and a deleted server's id is never reused by a
	// later registration, so no future caller can ever be handed that
	// same object again once forgotten.
	credentialLocks map[string]*sync.RWMutex
	// rotateBarrier, when set (test-only, nil/unused in production), is
	// invoked by rotateServerTokenLocked while the target server's
	// credential lock is held for writing, immediately before the
	// rotation reads/reseals/replaces the server's token. It lets a test
	// prove the reverse direction of the fence — that a rotation
	// genuinely mid-flight blocks a concurrent CallTool/discover for the
	// *same* server from proceeding — the same way reimportBarrier
	// proves ReimportMcpJson's own interleaving, since
	// RotateMcpServerToken's real repository work is otherwise too fast
	// to reliably race against.
	rotateBarrier func()
	// callCredentialLockBarrier, when set (test-only, nil/unused in
	// production), is invoked by CallTool immediately before it calls
	// credentialLock — after every check that can refuse the call without
	// ever touching the credential lock, but before the lock is acquired
	// (or, if this is the first call for this server since it was last
	// forgotten, created). It lets a test force a genuine "deleted-id lock
	// recreation" interleaving: a concurrent DeleteMcpServer completing
	// (row gone, its own credentialLocks entry — if any existed —
	// forgotten) entirely before CallTool ever reaches credentialLock, so
	// CallTool's own call is what (re)creates the entry, proving
	// forgetCredentialLockIfCurrent cleans it back up once CallTool's own
	// post-lock re-read discovers the row is gone.
	callCredentialLockBarrier func()
	// discoverCredentialLockBarrier is discoverLocked's own counterpart to
	// callCredentialLockBarrier (test-only, nil/unused in production),
	// invoked immediately before discoverLocked calls credentialLock —
	// the very first thing discoverLocked does — for the same
	// deleted-id-recreation interleaving proof, via SetMcpServerEnabled's
	// enable-time discovery instead of CallTool.
	discoverCredentialLockBarrier func()
}

// credentialLock returns the one *sync.RWMutex that fences reading/using
// serverID's bearer token against RotateMcpServerToken replacing it —
// creating it on first use. CallTool and discoverLocked hold it for
// reading immediately before they (re-)read the server's current sealed
// token through their own network call and liveness/tool-status
// recording — deliberately never across an unbounded wait such as
// caller-side approval enforcement, which runs before this lock is taken.
// rotateServerTokenLocked holds it for writing across its own repo read,
// sealing, atomic sealed-token replace, and liveness-status reset. Go's
// sync.RWMutex already blocks new readers once a writer is waiting, so a
// rotation can never be starved by a steady stream of calls/discoveries
// against the same server, and every reader started after a rotation
// commits is guaranteed to observe the new token rather than one captured
// before the rotation began. Because the lock is keyed by server id,
// none of this ever blocks a concurrent call, discovery, or rotation
// against a *different* server.
func (s *Server) credentialLock(serverID string) *sync.RWMutex {
	s.credentialLocksMu.Lock()
	defer s.credentialLocksMu.Unlock()
	if lock, ok := s.credentialLocks[serverID]; ok {
		return lock
	}
	lock := &sync.RWMutex{}
	if s.credentialLocks == nil {
		s.credentialLocks = make(map[string]*sync.RWMutex)
	}
	s.credentialLocks[serverID] = lock
	return lock
}

// forgetCredentialLock removes serverID's entry from credentialLocks,
// called after DeleteMcpServer successfully deletes that server's row.
// See credentialLocks' own comment for why this is safe even if another
// goroutine is, at that moment, still using the same lock object it
// obtained just before this call.
func (s *Server) forgetCredentialLock(serverID string) {
	s.credentialLocksMu.Lock()
	defer s.credentialLocksMu.Unlock()
	delete(s.credentialLocks, serverID)
}

// forgetCredentialLockIfCurrent removes serverID's entry from
// credentialLocks only if it still maps to lock — the exact *sync.RWMutex
// the caller is holding — rather than forgetCredentialLock's own
// unconditional delete-by-key. CallTool, discoverLocked, and
// rotateServerTokenLocked each call this once their own post-lock re-read
// of the server discovers it no longer exists (repository.ErrMCPServerNotFound).
//
// This closes a leak forgetCredentialLock alone cannot: DeleteMcpServer
// forgets a server's entry the instant its own delete commits, but a
// concurrent CallTool/discoverLocked/rotateServerTokenLocked may not call
// credentialLock for that same server until sometime *after* that —
// credentialLock's own lazy-create behavior then installs a brand-new
// lock object for an id DeleteMcpServer will never forget again (it only
// ever runs once, on its own successful delete). Without this cleanup,
// that freshly (re-)created entry would remain in credentialLocks
// permanently — one leaked entry per race hit, growing the map without
// bound over a long-running process's register/delete/race cycles,
// contrary to credentialLocks' own doc comment that its steady-state size
// tracks the registry's own row count.
//
// The comparison is by identity (the exact *sync.RWMutex pointer), not
// merely "is some entry present for serverID": that is what keeps this
// safe to call even when another goroutine has, in the meantime,
// installed a genuinely different object for the same key — this only
// ever removes the caller's own object, never a fresher one it does not
// recognize, so it can never race away a legitimate concurrent use.
func (s *Server) forgetCredentialLockIfCurrent(serverID string, lock *sync.RWMutex) {
	s.credentialLocksMu.Lock()
	defer s.credentialLocksMu.Unlock()
	if s.credentialLocks[serverID] == lock {
		delete(s.credentialLocks, serverID)
	}
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

// maxMCPUnsupportedNameBytes bounds the "name" an mcp.json entry is filed
// under before recordUnsupported ever writes it into the in-memory report,
// mcp_import_issues, or (via ListMcpServers/ReimportMcpJson) a client/UI
// response. Every name recordUnsupported ever actually receives is already
// bounded well under this by construction — either it already passed
// validateMCPServerName (which mcpServerNamePattern itself caps at 64
// characters), or it is one of the handful of short, fixed synthetic
// labels this package assigns instead of an untrusted raw name (see
// invalidMCPEntryLabel and the "_document"/"_registry" reserved keys) —
// but a JSON object's keys carry no length limit of their own, so this
// stays as a defensive bound for any future caller rather than assuming
// "already validated or synthetic" can never be gotten wrong.
const maxMCPUnsupportedNameBytes = 64

// boundedMCPServerNameForDisplay bounds an mcp.json entry's name the same
// way boundedStatusMessage bounds its reason: trimmed to valid UTF-8, so a
// truncated but still-recognizable prefix survives rather than a raw byte
// cut that could split a multi-byte character. In practice every caller
// already passes a name well under this bound (see
// maxMCPUnsupportedNameBytes); this only ever matters for some future
// caller that stops being one.
func boundedMCPServerNameForDisplay(name string) string {
	return boundedUTF8(name, maxMCPUnsupportedNameBytes)
}

// recordUnsupported is the one place ImportJSON writes an unsupported
// reason, so every reason — whether a fixed short message or one built
// from attacker-controlled input such as a header name or a tool name — is
// bounded and valid UTF-8 the same way boundedStatusMessage already bounds
// a discovery/status error, rather than some call sites bounding it and
// others writing raw, unbounded strings. The name this is recorded under
// is bounded the same way (see maxMCPUnsupportedNameBytes), but by the
// time this is ever called it is never itself an untrusted, attacker-
// controlled value: it is either an mcp.json entry's own name that has
// already passed validateMCPServerName (so already well within the bound,
// verbatim), or, for an entry refused for an invalid or reserved name
// (ImportJSON's own call site, before this one is ever reached for that
// entry), the bounded, synthetic, per-document-deterministic label
// invalidMCPEntryLabel assigns instead of that entry's own raw name/key —
// see ImportJSON's doc comment at that call site for why an invalid key is
// never used here at all, not even bounded.
//
// This is also the one place that defensively bounds how many distinct
// names ever accumulate in the map at all — independent of, and in
// addition to, ImportJSON's own upfront maxMCPImportEntries document-level
// gate (see the len(document.Servers) check in ImportJSON): should some
// future caller or refactor ever invoke this more times than that gate
// allows for, the (maxMCPImportEntries+1)-th distinct name collapses into
// one additional, fixed "_document" summary entry — mirroring how an
// oversized or malformed whole document already collapses into that same
// key — rather than growing the map without bound. Neither "_document" nor
// "_registry" (ListMcpServers' own reserved degraded-notice key, before
// this package's own explicit registry_degraded/registry_degradation_reason
// fields replaced it — see mcpRegistryOverBudgetNoticeMessage) can ever
// collide with a name recorded here: every name reaching this function
// either already passed validateMCPServerName (which requires a leading
// alphanumeric character neither reserved key has) or is itself one of
// invalidMCPEntryLabel's own "_invalid_server_N" labels.
func recordUnsupported(unsupported map[string]string, name, reason string) {
	bounded := boundedMCPServerNameForDisplay(name)
	if _, exists := unsupported[bounded]; !exists && len(unsupported) >= maxMCPImportEntries {
		unsupported["_document"] = mcpUnsupportedOverflowMessage
		return
	}
	unsupported[bounded] = boundedStatusMessage(reason)
}

// mcpUnsupportedOverflowMessage is the fixed, generic reason recordUnsupported
// writes under the "_document" key once its defensive count bound is
// reached: it never echoes the overflowing entry's own name or reason,
// only that some refusals were collapsed rather than individually listed.
const mcpUnsupportedOverflowMessage = "additional entries were refused but are not individually listed; the entry count exceeds the maximum supported limit"

type DiscoveredTool struct {
	Name       string
	SchemaJSON string
}

// errMCPRootKeyInvalid is the one fixed, generic reason ImportJSON refuses
// a document whose root object does not declare the "mcpServers" key
// exactly once, spelled exactly that way. encoding/json's own built-in
// case-insensitive field-name fallback would otherwise accept a
// differently-cased key (e.g. "McpServers"/"MCPSERVERS") as if it were
// correctly spelled, and its map/struct decoding keeps only the last of
// two same- or differently-cased duplicate keys silently — either would
// let an attacker- or tooling-introduced second "mcpServers" object
// quietly win instead of the ambiguity being refused outright. This is
// deliberately scoped to the one root key ImportJSON reads: an unrelated
// sibling key (anything that is not itself a case-insensitive match for
// "mcpServers") is left alone.
var errMCPRootKeyInvalid = errors.New(`mcp.json must declare exactly one "mcpServers" object`)

// errMCPRootServersRequired is the fixed reason returned when the root
// object never declares a canonical "mcpServers" key at all (or declares
// it as an explicit JSON null, treated identically to absent) — distinct
// from errMCPRootKeyInvalid, which covers a case-variant or duplicate key
// actually being present.
var errMCPRootServersRequired = errors.New(`decode mcp.json: "mcpServers" object is required`)

// maxMCPRootFields bounds how many top-level members decodeMCPRootServers
// ever walks — independent of whether any given member turns out to be
// the one canonical "mcpServers" key it actually reads (see
// TestImportJSONIgnoresUnrelatedTopLevelKeys: an unrelated sibling key is
// deliberately *not* refused on its own). Without this, a root object
// packed with an unbounded number of distinct, otherwise-irrelevant
// sibling keys would force this loop to decode (and discard) every one of
// their values before ever reaching a well-formed document's single
// "mcpServers" key — the same class of parser-cardinality amplification
// maxMCPServerEntryFields/maxMCPToolObjectFields/maxMCPHeaderEntries close
// for the collections nested one level down. A small, fixed cap is enough
// headroom for any realistic mcp.json (a handful of sibling keys such as
// "$schema" alongside "mcpServers") without letting the count grow
// unbounded.
const maxMCPRootFields = 8

// errMCPRootTooManyFields is the one fixed, generic reason
// decodeMCPRootServers refuses a root object naming more than
// maxMCPRootFields top-level members — checked the instant the
// (maxMCPRootFields+1)-th member's key is seen, before its value is ever
// decoded, so an oversized or malformed excess member costs nothing to
// refuse. It never echoes any member's own name.
var errMCPRootTooManyFields = errors.New("decode mcp.json: root object has too many top-level fields")

// decodeMCPRootServers stream-parses mcp.json's root object with a
// token-level walk (mirroring decodeMCPEntryFields/decodeMCPHeaderEntries
// one level up) rather than json.Decoder.Decode into a struct, so this
// package — not encoding/json's own case-insensitive-fallback,
// last-key-wins struct/map decoding — decides what the root object's one
// significant key may look like. The canonical, exactly-cased
// "mcpServers" key must appear exactly once among the root object's
// members; every other member's value is still fully consumed (so the
// decoder's position stays correct) but otherwise ignored. Returns the raw,
// not-yet-decoded value of "mcpServers" for decodeMCPServerEntries to
// parse next.
func decodeMCPRootServers(data []byte) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	open, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("decode mcp.json: %w", err)
	}
	if delim, ok := open.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("decode mcp.json: root value must be an object")
	}
	var servers json.RawMessage
	seenMcpServersKey := false
	fieldCount := 0
	for decoder.More() {
		if fieldCount >= maxMCPRootFields {
			return nil, errMCPRootTooManyFields
		}
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("decode mcp.json: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("decode mcp.json: root object key must be a string")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("decode mcp.json: %w", err)
		}
		fieldCount++
		if !strings.EqualFold(key, "mcpServers") {
			continue
		}
		if key != "mcpServers" || seenMcpServersKey {
			return nil, errMCPRootKeyInvalid
		}
		seenMcpServersKey = true
		servers = value
	}
	if _, err := decoder.Token(); err != nil { // the closing '}'
		return nil, fmt.Errorf("decode mcp.json: %w", err)
	}
	if err := requireImportEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode mcp.json: %w", err)
	}
	if !seenMcpServersKey || string(bytes.TrimSpace(servers)) == "null" {
		return nil, errMCPRootServersRequired
	}
	return servers, nil
}

// mcpServerEntry is one member of mcp.json's "mcpServers" object, captured
// exactly as decodeMCPServerEntries found it on the wire — including a
// name that repeats another entry's name exactly. A
// map[string]json.RawMessage (what this package used to decode
// "mcpServers" into) cannot represent that: two JSON object members that
// share the exact same key decode into one map entry, last-value-wins,
// which would let requireUniqueMCPServerNames below never even see the
// duplicate it exists to refuse.
type mcpServerEntry struct {
	Name string
	Body json.RawMessage
}

// errMCPImportDuplicateServerName is the one fixed, generic reason
// ImportJSON refuses a whole mcp.json document that declares the exact
// same server name more than once. Plain map-based decoding would
// otherwise silently keep only the last of the two (or more) definitions
// — possibly with a different url or bearer token than an operator
// intended for the name — rather than the ambiguity being refused
// outright. This refuses the *whole document*, the same way
// errMCPImportTooManyEntries and errMCPImportDocumentTooLarge already do,
// rather than only one of the colliding entries: there is no principled
// way to pick a "winner" to still import, and refusing everything means
// neither definition can ever partly register.
var errMCPImportDuplicateServerName = errors.New("mcp.json declares the same server name more than once")

// decodeMCPServerEntries stream-parses the raw "mcpServers" object value
// with a token-level walk, preserving every member — including an exact
// name repeat — rather than collapsing into a map (see mcpServerEntry).
// The entry count is enforced the instant it would exceed
// maxMCPImportEntries, before that (maxMCPImportEntries+1)-th entry's key
// or body is even decoded: a document packed with far more (tiny) entries
// than could ever actually register is refused without first building a
// same-sized slice of them all in memory, the same way this package
// already bounds the whole document's raw byte size before decoding
// anything at all.
func decodeMCPServerEntries(raw json.RawMessage) ([]mcpServerEntry, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	open, err := decoder.Token()
	if err != nil {
		return nil, errors.New(`decode mcp.json: "mcpServers" must be an object`)
	}
	if delim, ok := open.(json.Delim); !ok || delim != '{' {
		return nil, errors.New(`decode mcp.json: "mcpServers" must be an object`)
	}
	var entries []mcpServerEntry
	for decoder.More() {
		if len(entries) >= maxMCPImportEntries {
			return nil, errMCPImportTooManyEntries
		}
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, errors.New(`decode mcp.json: "mcpServers" must be an object`)
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New(`decode mcp.json: "mcpServers" must be an object`)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errors.New(`decode mcp.json: "mcpServers" must be an object`)
		}
		entries = append(entries, mcpServerEntry{Name: key, Body: value})
	}
	if _, err := decoder.Token(); err != nil { // the closing '}'
		return nil, errors.New(`decode mcp.json: "mcpServers" must be an object`)
	}
	return entries, nil
}

// requireUniqueMCPServerNames refuses the whole document (see
// errMCPImportDuplicateServerName) the moment any two entries share the
// exact same name. Bounded the same way decodeMCPServerEntries itself is:
// entries is already capped at maxMCPImportEntries by that call, so the
// map built here to detect a repeat is bounded too.
func requireUniqueMCPServerNames(entries []mcpServerEntry) error {
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if _, duplicate := seen[entry.Name]; duplicate {
			return errMCPImportDuplicateServerName
		}
		seen[entry.Name] = struct{}{}
	}
	return nil
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
	// Tools is captured as raw, undecoded JSON — not []mcpJSONTool — for
	// exactly the same reason Headers is: decodeMCPToolEntries walks each
	// array element's exact key/value tokens (via decodeMCPToolFields)
	// before anything collapses a tool object's own case-variant or
	// duplicate keys the way encoding/json's struct-tag decode otherwise
	// would (see decodeMCPToolEntries' own comment).
	Tools json.RawMessage `json:"tools"`
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
// step would apply to it. A second scan runs after that same
// json.Marshal, over the exact schemaJSON text this function is about to
// store: mcpRawMetadataContainsToken only ever inspects strings (a map
// key, or a string value), so a token equal to the literal text of a
// JSON number, boolean, or null schema value — none of which decode to a
// Go string — would otherwise pass it silently, and a token that only
// ever exists across a structural boundary json.Marshal itself
// introduces (the quote-colon-quote between a key and its value, for
// instance) can never be found by a scan of decoded values at all,
// regardless of type. Neither scan replaces the other.
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
		// Defense-in-depth only: decodeMCPToolEntries (see its own
		// errMCPStaticToolCountExceeded check) already refuses a static
		// snapshot naming more than maxMCPTools entries before rawTools
		// is ever built, so this should be unreachable in practice.
		return nil, errMCPStaticToolCountExceeded
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
		// A second, post-marshal scan of the exact schemaJSON text this
		// package is about to store — never a replacement for the
		// decoded scan above, which remains the only way to catch a
		// token containing a quote or backslash (see
		// mcpRawMetadataContainsToken's own doc comment). This one
		// catches what that recursive, strings-only walk structurally
		// cannot: a token equal to the literal serialized text of a JSON
		// number, boolean, or null schema value (none of those decode to
		// a Go string, so the scan above never inspects them), and a
		// token that only ever exists across a structural boundary
		// json.Marshal introduces — such as the quote-colon-quote
		// between a key and its value — that no single decoded string,
		// number, bool, or null value ever contains on its own.
		if token != "" && strings.Contains(schemaJSON, token) {
			return nil, errors.New(mcpToolDefinitionRefusedMessage)
		}
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

// ImportJSON's own named return values (report, err) exist so that a
// fatal error discovered partway through the per-entry loop below — or
// after it, persisting the final Unsupported map — never has to discard
// whatever this call has already accumulated: see the deferred func
// immediately below, which is what actually populates report.Imported
// and report.Skipped (sorted) from the loop's own local slices,
// regardless of which return statement in this function ultimately
// fires. Before this, every one of those fatal paths returned a bare
// ImportReport{} — discarding every entry already recorded into
// report.Unsupported (via recordUnsupported) and, more importantly,
// every name already appended to imported/skipped for an entry whose own
// repository transaction (ImportMCPServer) had already committed one
// loop iteration earlier: a canceled context, or any other repository
// failure this function cannot attribute to a single entry (see the
// switch's own default case below), could make ImportJSON forget it had
// just durably registered N servers, even though those rows still exist
// and even though a concurrent ListMcpServers would already show them.
// ReimportConfiguredJSON's own recordDocumentRefusal is what turns a
// non-nil err here into a caller-visible outcome without ever pretending
// those already-committed entries did not happen.
func (s *Server) ImportJSON(ctx context.Context, data []byte) (report ImportReport, err error) {
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
		return ImportReport{}, errMCPImportDocumentTooLarge
	}
	rawServers, err := decodeMCPRootServers(data)
	if err != nil {
		return ImportReport{}, err
	}
	// The entry count itself is bounded during the streaming parse below
	// (see decodeMCPServerEntries) — the same repository.MaxNonBundledMCPServers
	// limit the registry itself enforces per non-bundled row (see
	// nonBundledMCPServerRegistryFullTx) — rather than only after a
	// full-sized map/slice of every entry has already been built: a
	// document naming more entries than could ever actually register
	// would otherwise still cost the memory and CPU to fully parse them
	// all first, and (absent any cap at all) would eventually be refused
	// one entry at a time only once the registry's own count cap is
	// reached — scattering what is really a single, document-level
	// problem across many per-entry Unsupported rows instead of the one
	// bounded refusal every other whole-document problem (size,
	// malformed JSON) already gets.
	entries, err := decodeMCPServerEntries(rawServers)
	if err != nil {
		return ImportReport{}, err
	}
	if err := requireUniqueMCPServerNames(entries); err != nil {
		return ImportReport{}, err
	}

	report = ImportReport{Unsupported: make(map[string]string)}
	imported := make([]string, 0)
	skipped := make([]string, 0)
	defer func() {
		sort.Strings(imported)
		sort.Strings(skipped)
		report.Imported = imported
		report.Skipped = skipped
	}()
	// Entries are processed in sorted name order, never the order they
	// happened to appear in the document (already deterministic here,
	// since decodeMCPServerEntries preserves wire order rather than a
	// map's own randomized iteration — but sorting explicitly still
	// matters for the reason below): when two entries in the same
	// document claim the same tool name, whichever is processed first
	// wins the repository's inter-server collision check (see
	// replaceServerToolsTx) and the other is refused. Sorting first makes
	// that winner the lexicographically first server name, every time,
	// rather than one that could differ depending on the order the two
	// entries happened to be written in.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	// invalidEntryOrdinal counts entries refused below for an invalid or
	// reserved name, in this same sorted-by-name order, so
	// invalidMCPEntryLabel's synthetic labels are assigned deterministically
	// — the same document always produces the same labels, regardless of
	// how many other, differently-named entries it also contains.
	invalidEntryOrdinal := 0
	for _, entry := range entries {
		name := entry.Name
		raw := entry.Body
		// The name check runs before the entry's body is even decoded:
		// a reserved or pattern-invalid name is refused for that reason
		// regardless of whether its body would otherwise fail strict
		// decoding, sharing the exact validateServerDefinition also
		// uses so the two paths can never independently drift apart.
		//
		// The entry's own raw name/key is never used to record this
		// refusal — unlike every other recordUnsupported call below,
		// which only ever runs for a name that has already passed this
		// same check. A key an operator's mcp.json names an entry under
		// is exactly as untrusted as any other value in the document,
		// and unlike a name that already passed validateMCPServerName
		// (bounded to mcpServerNamePattern's 64-byte, ASCII-only shape),
		// an invalid one might not even have been intended as a name at
		// all — a bearer token or other secret pasted into the wrong
		// JSON slot, for instance. Recording it under a bounded,
		// synthetic, deterministic label instead (invalidMCPEntryLabel)
		// with a fixed reason (invalidMCPEntryNameMessage, which never
		// distinguishes "invalid" from "reserved") keeps that value out
		// of the in-memory report, mcp_import_issues, every RPC response
		// that surfaces either, and the Flutter UI, exactly the same way
		// every other attacker-controlled reason below is already kept
		// out via boundedStatusMessage/errMCP*-fixed-message constants —
		// see TestImportInvalidEntryKeyEqualToBearerSentinelNeverLeaks.
		if err := validateMCPServerName(name); err != nil {
			invalidEntryOrdinal++
			recordUnsupported(report.Unsupported, invalidMCPEntryLabel(invalidEntryOrdinal), invalidMCPEntryNameMessage)
			continue
		}
		// The entry's own top-level field names are validated against a
		// raw, token-level parse before anything decodes it into
		// mcpJSONServer: this is what refuses a case variant (e.g.
		// "TOOLS") or a duplicate key (e.g. two "url" members)
		// deterministically, rather than letting encoding/json's own
		// case-insensitive struct-tag fallback (or its silent
		// last-one-wins handling of a duplicate key) decide instead. See
		// decodeMCPEntryFields/validateMCPEntryFields.
		fields, err := decodeMCPEntryFields(raw)
		if err != nil {
			recordUnsupported(report.Unsupported, name, err.Error())
			continue
		}
		if err := validateMCPEntryFields(fields); err != nil {
			recordUnsupported(report.Unsupported, name, err.Error())
			continue
		}
		// toolsPresent is computed from the same validated field list,
		// looking for the exact lowercase "tools" spelling — guaranteed
		// unique at this point — rather than a second, independent
		// lookup that could disagree with what the struct decode below
		// actually populates.
		var toolsPresent bool
		for _, field := range fields {
			if field.Name == "tools" {
				toolsPresent = true
				break
			}
		}
		var entry mcpJSONServer
		entryDecoder := json.NewDecoder(bytes.NewReader(raw))
		entryDecoder.DisallowUnknownFields()
		if err := entryDecoder.Decode(&entry); err != nil {
			// The fixed, generic errMCPEntryFieldInvalid reason — never
			// err.Error() — for every way this decode can fail: a type
			// mismatch (encoding/json's own message names the Go
			// struct/field involved, not attacker input, but is still
			// internal detail this response has no reason to expose).
			// This no longer needs to (and, since Headers and Tools are
			// both captured as raw json.RawMessage rather than decoded
			// structurally, no longer can) also catch an unknown key
			// nested inside a "headers" object or a "tools" array
			// element — decodeMCPHeaderEntries/bearerFromHeaders and
			// decodeMCPToolEntries/validateMCPToolFields below each own
			// that check for their own raw value now, with their own
			// fixed, generic, sentinel-free reasons, the same way
			// DisallowUnknownFields' own "unknown field %q" message
			// would otherwise have named the offending key verbatim —
			// and a JSON key is exactly as attacker-controlled as any
			// value, so echoing it back would leak it into every place
			// an ordinary Unsupported reason already reaches (ImportReport,
			// mcp_import_issues, the ReimportMcpJson RPC response,
			// ListMcpServers' own Unsupported list, and the
			// audit/event/log surfaces that mirror them) — the same
			// class of leak errMCPUnsupportedHeader already closes for
			// an unsupported header's own name.
			recordUnsupported(report.Unsupported, name, errMCPEntryFieldInvalid.Error())
			continue
		}
		// The "tools" array's own shape — every element's field names
		// exactly canonical, no duplicates — is decoded and validated
		// here, immediately after the entry's own top-level shape, and
		// therefore before the existing-row skip decision below: a
		// malformed tools shape must still be reported as a refusal even
		// for an entry that would otherwise be silently skipped as
		// already-registered, the same way it already was when this was
		// still part of entryDecoder.Decode's own single monolithic
		// struct decode above. Building the fully-validated
		// []repository.MCPServerTool snapshot (name/token/budget/
		// collision checks, via buildImportTools) still waits until
		// after that skip decision, since it needs this entry's own
		// normalized bearer token, computed below.
		var rawTools []mcpJSONTool
		if toolsPresent {
			rawTools, err = decodeMCPToolEntries(entry.Tools)
			if err != nil {
				recordUnsupported(report.Unsupported, name, err.Error())
				continue
			}
		}
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
			// existing.URL == "": this is a legacy placeholder adoption
			// candidate. Before this entry's token is ever sealed or the
			// placeholder's row touched, refuse it if it appears
			// verbatim in any tool this placeholder retained (present or
			// withdrawn) — see placeholderAdoptionTokenCollision's own
			// doc comment. No row mutation, no sealed token, and the
			// refusal is recorded exactly like any other Unsupported
			// entry: a corrected reimport with an unrelated token still
			// adopts/withdraws/reconciles as before. The reason recorded
			// is the one fixed, generic mcpToolDefinitionRefusedMessage
			// every other malformed/refused mcp.json entry already uses
			// — not the explicit errMCPTokenMatchesRetainedToolMetadata
			// reason RegisterMcpServer returns for the identical
			// collision — since a file import's refusal must never
			// confirm to whoever controls that file that a token/tool
			// metadata comparison is what tripped (see
			// placeholderAdoptionTokenCollision's own doc comment).
			collides, cerr := placeholderAdoptionTokenCollision(ctx, s.repo, existing.ID, token)
			if cerr != nil {
				return report, fmt.Errorf("list MCP server %q tools: %w", name, cerr)
			}
			if collides {
				recordUnsupported(report.Unsupported, name, mcpToolDefinitionRefusedMessage)
				continue
			}
		case errors.Is(err, repository.ErrMCPServerNotFound):
			tombstoned, terr := s.repo.MCPServerTombstoned(ctx, name)
			if terr != nil {
				return report, fmt.Errorf("check MCP server %q tombstone: %w", name, terr)
			}
			if tombstoned {
				recordUnsupported(report.Unsupported, name, "server was removed locally and remains suppressed; use a new name to import it again")
				continue
			}
		default:
			return report, fmt.Errorf("look up MCP server %q: %w", name, err)
		}

		// Every static tool this entry declares is fully validated —
		// name, schema, bundled-namespace collision, and the entry's own
		// token never appearing verbatim in a tool's name or schema —
		// before anything below touches the repository, so an invalid or
		// token-bearing snapshot refuses the whole entry (Unsupported
		// only, never also Imported) rather than leaving a partial row a
		// corrected reimport could only skip. rawTools was already
		// decoded and shape-validated above, before the skip decision.
		var tools []repository.MCPServerTool
		if toolsPresent {
			tools, err = buildImportTools(tier, name, rawTools, token)
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
			return report, errors.New("seal MCP server token")
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
			case errors.Is(err, repository.ErrMCPServerRegistryFull):
				// Recorded as an ordinary per-entry refusal — like every
				// other repository disposition here — rather than a hard
				// error that would abort the rest of the document: a
				// document naming several new servers once the registry
				// is already at repository.MaxNonBundledMCPServers must
				// still report every one of them refused, deterministically,
				// not just the first.
				recordUnsupported(report.Unsupported, name, mcpServerRegistryFullMessage)
			case errors.Is(err, repository.ErrMCPToolNameCollision):
				// The repository's own transaction already rolled back
				// the row insert/adoption entirely (see
				// ImportMCPServer): no row remains for this name, so a
				// corrected reimport (dropping or renaming the
				// colliding tool) starts clean rather than skipping a
				// poisoned partial row.
				recordUnsupported(report.Unsupported, name, err.Error())
			case errors.Is(err, repository.ErrMCPThirdPartyToolBudgetExceeded):
				// Checked before ErrMCPRegistryToolBudgetExceeded below,
				// mirroring replaceServerToolsTx's own check order: the
				// two are distinct sentinel errors (a given err matches
				// at most one of these two cases), so this ordering only
				// ever changes which case *would* have matched if both
				// were somehow satisfiable by the same err — it does not
				// change behavior today, but keeps the more specific
				// third-party reason preferred if that ever became
				// possible. Recorded as an ordinary per-entry refusal for
				// the same reason every other repository disposition
				// here is.
				recordUnsupported(report.Unsupported, name, mcpThirdPartyToolBudgetExceededMessage)
			case errors.Is(err, repository.ErrMCPRegistryToolBudgetExceeded):
				// Recorded as an ordinary per-entry refusal for the same
				// reason ErrMCPServerRegistryFull is immediately above:
				// the repository's own transaction has already rolled
				// back entirely (see replaceServerToolsTx), so this
				// entry leaves no partial row, and a document naming
				// several entries once the registry-wide aggregate tool
				// budget is already exhausted must still report every
				// one of them refused — never lose track of whatever
				// earlier entries in this same document already
				// committed successfully by returning an error from
				// ImportJSON itself.
				recordUnsupported(report.Unsupported, name, mcpRegistryToolBudgetExceededMessage)
			default:
				return report, fmt.Errorf("import MCP server %q: %w", name, err)
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
		if s.importEntryBarrier != nil {
			s.importEntryBarrier(name)
		}
	}
	if err := s.repo.ReplaceMCPImportIssues(ctx, report.Unsupported); err != nil {
		return report, fmt.Errorf("record mcp.json import issues: %w", err)
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

// maxMCPServerURLBytes bounds a server's canonical URL — checked after
// classifyImportedURL has already canonicalized it (lower-cased host,
// normalized port, defaulted/trimmed path), so this is the exact string
// that would otherwise be stored and returned in every descriptor/list
// response, not the caller's raw, pre-canonicalization input. Without a
// bound, an mcp.json entry or a direct RegisterMcpServer/RotateMcpServerToken
// call could store (and every later ListMcpServers response would then
// repeat) an arbitrarily long URL — most plausibly via an unbounded path,
// since host/port are already narrowly constrained by the tier-specific
// checks above. 2048 bytes comfortably covers any realistic MCP endpoint
// while keeping stored rows and list responses bounded.
const maxMCPServerURLBytes = 2048

// errMCPServerURLTooLong is the fixed, generic reason classifyImportedURL
// returns for a canonical URL exceeding maxMCPServerURLBytes, named once so
// both tiers, and both the file-import and direct-registration paths that
// eventually call classifyImportedURL (via validateServerDefinition), say
// the same thing about the same refusal.
var errMCPServerURLTooLong = fmt.Errorf("url exceeds the maximum supported length of %d bytes", maxMCPServerURLBytes)

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
		if len(endpoint.Canonical) > maxMCPServerURLBytes {
			return "", "", errMCPServerURLTooLong
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
		canonical := parsed.String()
		if len(canonical) > maxMCPServerURLBytes {
			return "", "", errMCPServerURLTooLong
		}
		return repository.MCPServerTierLocalContainer, canonical, nil
	default:
		return "", "", errors.New("url must use HTTPS remotely or HTTP for an isolated local container")
	}
}

// mcpEntryField is a single top-level key/value member of an mcp.json
// server entry's raw JSON object, preserved exactly as
// decodeMCPEntryFields found it on the wire — including a name that
// repeats another member's, whether by an exact spelling match or only a
// case-insensitive one. A map[string]json.RawMessage (or letting
// mcpJSONServer's own struct tags decode the object directly) cannot
// represent or detect that: encoding/json resolves a struct tag
// case-insensitively whenever no exact-case match exists, and unmarshaling
// into either a map or a struct silently keeps only the last of two
// members that share the same key — exactly the failure mode this type
// and decodeMCPEntryFields exist to make detectable instead.
type mcpEntryField struct {
	Name  string
	Value json.RawMessage
}

// canonicalMCPEntryFieldNames are the only top-level key spellings an
// mcp.json server entry may use. Every one is lowercase; validateMCPEntryFields
// refuses the same field spelled with any other case (e.g. "Tools", "URL")
// rather than silently accepting it through encoding/json's own
// case-insensitive fallback matching.
var canonicalMCPEntryFieldNames = map[string]struct{}{
	"url":     {},
	"headers": {},
	"command": {},
	"args":    {},
	"env":     {},
	"tools":   {},
}

// maxMCPServerEntryFields bounds how many top-level fields
// decodeMCPEntryFields ever appends before refusing further growth —
// exactly len(canonicalMCPEntryFieldNames): no well-formed entry could
// ever declare more than that many distinct top-level keys.
// validateMCPEntryFields already refuses any entry naming more than one of
// each, but only after decodeMCPEntryFields had already appended every
// field an attacker supplied; checking here first means an entry with an
// unbounded number of (however invalid, however many times repeated)
// top-level keys is refused the instant the seventh one is seen, before
// its value is even decoded.
var maxMCPServerEntryFields = len(canonicalMCPEntryFieldNames)

// errMCPEntryFieldInvalid is the one fixed, generic reason
// decodeMCPEntryFields/validateMCPEntryFields return for every way an
// entry's own top-level field names can be malformed: not an object at
// all, a name that does not match one of canonicalMCPEntryFieldNames at
// all (case-insensitively), a canonical name spelled with the wrong case
// (e.g. "Tools" instead of "tools"), the same canonical name repeated
// more than once (an exact-spelling repeat, which JSON itself permits
// inside one object, or a case-insensitive one such as "tools" and
// "Tools" both present), or more than maxMCPServerEntryFields fields
// present at all. It deliberately never says which of those tripped, the
// same reasoning mcpToolDefinitionRefusedMessage documents.
var errMCPEntryFieldInvalid = errors.New("entry is invalid")

// decodeMCPEntryFields parses an mcp.json server entry's raw JSON object
// with the same token-level walk (json.Decoder.Token/More)
// decodeMCPHeaderEntries already uses for the "headers" value, and for the
// same reason: two JSON object members that share the exact same key, or
// differ only in case, must be preserved as separate entries rather than
// silently collapsed — by json.Unmarshal into a map, or by
// mcpJSONServer's own struct tags via encoding/json's built-in
// case-insensitive fallback matching — before validateMCPEntryFields below
// ever sees the conflict it exists to refuse. Called before entry's body
// is decoded into mcpJSONServer at all, so a case-variant or duplicate key
// is refused deterministically instead of quietly feeding a struct field
// that an independent exact-lowercase-key check (such as ImportJSON's own
// toolsPresent flag) would then disagree with.
func decodeMCPEntryFields(raw json.RawMessage) ([]mcpEntryField, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	open, err := decoder.Token()
	if err != nil {
		return nil, errMCPEntryFieldInvalid
	}
	if delim, ok := open.(json.Delim); !ok || delim != '{' {
		return nil, errMCPEntryFieldInvalid
	}
	var fields []mcpEntryField
	for decoder.More() {
		if len(fields) >= maxMCPServerEntryFields {
			return nil, errMCPEntryFieldInvalid
		}
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, errMCPEntryFieldInvalid
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errMCPEntryFieldInvalid
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errMCPEntryFieldInvalid
		}
		fields = append(fields, mcpEntryField{Name: key, Value: value})
	}
	if _, err := decoder.Token(); err != nil { // the closing '}'
		return nil, errMCPEntryFieldInvalid
	}
	return fields, nil
}

// validateMCPEntryFields requires every one of an entry's top-level field
// names to exactly match one of canonicalMCPEntryFieldNames — never
// merely case-insensitively, the way mcpJSONServer's own struct decoding
// would otherwise accept via encoding/json's built-in case-insensitive
// fallback whenever no exact-case match exists. Without this, an entry
// naming "TOOLS" instead of "tools" would decode into mcpJSONServer.Tools
// exactly as if spelled correctly, yet ImportJSON's own independent
// exact-lowercase-key presence check (deliberately strict, so it can never
// be fooled into treating a same-cased "tools" the same as a wrong-cased
// near-miss) would then treat that populated field as absent — silently
// discarding a "tools" snapshot rather than refusing the entry the way
// every other malformed shape is. An exact-duplicate or case-insensitive
// duplicate of any canonical name (e.g. two "url" members) is refused the
// same way: JSON itself permits a duplicate key in one object, and plain
// encoding/json decoding (struct or map) would otherwise silently keep
// only the last one.
func validateMCPEntryFields(fields []mcpEntryField) error {
	seenCanonical := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		lower := strings.ToLower(field.Name)
		if _, isCanonical := canonicalMCPEntryFieldNames[lower]; !isCanonical {
			return errMCPEntryFieldInvalid
		}
		if field.Name != lower {
			return errMCPEntryFieldInvalid
		}
		if _, duplicate := seenCanonical[lower]; duplicate {
			return errMCPEntryFieldInvalid
		}
		seenCanonical[lower] = struct{}{}
	}
	return nil
}

// mcpToolField is a single top-level key/value member of a "tools" array
// element's raw JSON object, preserved exactly as decodeMCPToolFields
// found it on the wire — including a name that repeats another member's,
// whether by an exact spelling match or only a case-insensitive one. This
// is the tool-object counterpart of mcpEntryField (an mcp.json entry's own
// top-level fields) and mcpHeaderEntry (a "headers" object's members): a
// map or mcpJSONTool's own former struct tags cannot represent or detect a
// repeated or case-variant key, since encoding/json resolves a struct tag
// case-insensitively whenever no exact-case match exists and silently
// keeps only the last of two members that share the same key.
type mcpToolField struct {
	Name  string
	Value json.RawMessage
}

// canonicalMCPToolFieldNames maps a tool object's only permitted key
// spellings, lowercased, to their exact canonical form. Unlike
// canonicalMCPEntryFieldNames (whose six canonical spellings are already
// all lowercase), "inputSchema" is deliberately not: mcp.json itself, and
// every MCP tool definition, spells it in camelCase, so
// validateMCPToolFields must compare a field's exact Name against this
// map's *value* (the true canonical spelling), not merely confirm its
// lowercased form names a recognized key.
var canonicalMCPToolFieldNames = map[string]string{
	"name":        "name",
	"description": "description",
	"inputschema": "inputSchema",
}

// maxMCPToolObjectFields bounds how many top-level fields
// decodeMCPToolFields ever appends before refusing further growth —
// exactly len(canonicalMCPToolFieldNames): no well-formed tool object
// could ever declare more than that many distinct top-level keys.
// validateMCPToolFields already refuses any element naming more than one
// of each, but only after decodeMCPToolFields had already appended every
// field an attacker supplied; checking here first means a tool element
// with an unbounded number of (however invalid) top-level keys is refused
// the instant the fourth one is seen, before its value — potentially a
// large inputSchema-shaped payload under some other name — is even
// decoded.
var maxMCPToolObjectFields = len(canonicalMCPToolFieldNames)

// validateMCPToolFields requires every one of a "tools" array element's
// top-level field names to exactly match one of canonicalMCPToolFieldNames'
// canonical spellings — never merely case-insensitively, the way
// mcpJSONTool's own former struct-tag decode accepted "Name" or "NAME" as
// interchangeable with "name" via encoding/json's built-in case-insensitive
// fallback matching. An exact-duplicate or case-insensitive duplicate of
// any canonical name (e.g. both "inputSchema" and "InputSchema") is
// refused the same way: JSON permits a duplicate key in one object, and
// plain encoding/json decoding would otherwise silently keep only the last
// one. Every failure uses the one fixed, generic
// mcpToolDefinitionRefusedMessage — never distinguishing which of the two
// tripped, or naming the offending key — the same reasoning
// validateMCPEntryFields/errMCPEntryFieldInvalid already document for the
// sibling top-level check.
func validateMCPToolFields(fields []mcpToolField) error {
	seenLower := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		lower := strings.ToLower(field.Name)
		canonical, isKnown := canonicalMCPToolFieldNames[lower]
		if !isKnown || field.Name != canonical {
			return errors.New(mcpToolDefinitionRefusedMessage)
		}
		if _, duplicate := seenLower[lower]; duplicate {
			return errors.New(mcpToolDefinitionRefusedMessage)
		}
		seenLower[lower] = struct{}{}
	}
	return nil
}

// decodeMCPToolFields parses a single "tools" array element's raw JSON
// object with the same token-level walk (json.Decoder.Token/More)
// decodeMCPEntryFields/decodeMCPHeaderEntries already use, and for the
// same reason: two JSON object members that share the exact same key, or
// differ only in case, must be preserved as separate entries rather than
// silently collapsed — by json.Unmarshal into a map, or by mcpJSONTool's
// own former struct tags via encoding/json's case-insensitive fallback
// matching — before validateMCPToolFields ever sees the conflict it exists
// to refuse. A non-object element, or malformed JSON, is refused with the
// one fixed, generic mcpToolDefinitionRefusedMessage.
func decodeMCPToolFields(raw json.RawMessage) ([]mcpToolField, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	open, err := decoder.Token()
	if err != nil {
		return nil, errors.New(mcpToolDefinitionRefusedMessage)
	}
	if delim, ok := open.(json.Delim); !ok || delim != '{' {
		return nil, errors.New(mcpToolDefinitionRefusedMessage)
	}
	var fields []mcpToolField
	for decoder.More() {
		if len(fields) >= maxMCPToolObjectFields {
			return nil, errors.New(mcpToolDefinitionRefusedMessage)
		}
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, errors.New(mcpToolDefinitionRefusedMessage)
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New(mcpToolDefinitionRefusedMessage)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errors.New(mcpToolDefinitionRefusedMessage)
		}
		fields = append(fields, mcpToolField{Name: key, Value: value})
	}
	if _, err := decoder.Token(); err != nil { // the closing '}'
		return nil, errors.New(mcpToolDefinitionRefusedMessage)
	}
	return fields, nil
}

// mcpToolFromFields converts a validated (exact-canonical-key,
// no-duplicate) field list into an mcpJSONTool, decoding each field's raw
// value into the type buildImportTools expects: Name and Description as
// strings, InputSchema as a raw JSON object (map[string]any). A field
// whose value does not match that type (e.g. a numeric "name") is refused
// with the one fixed mcpToolDefinitionRefusedMessage rather than echoing
// encoding/json's own type-mismatch message. A field never present in
// fields at all (validateMCPToolFields already guarantees at most one of
// each) simply leaves the corresponding mcpJSONTool field at its zero
// value — nil InputSchema, empty Description — matching exactly what an
// absent JSON member decoded to under the former struct-tag decode.
func mcpToolFromFields(fields []mcpToolField) (mcpJSONTool, error) {
	var tool mcpJSONTool
	for _, field := range fields {
		var err error
		switch field.Name {
		case "name":
			err = json.Unmarshal(field.Value, &tool.Name)
		case "description":
			err = json.Unmarshal(field.Value, &tool.Description)
		case "inputSchema":
			err = json.Unmarshal(field.Value, &tool.InputSchema)
		}
		if err != nil {
			return mcpJSONTool{}, errors.New(mcpToolDefinitionRefusedMessage)
		}
	}
	return tool, nil
}

// errMCPStaticToolCountExceeded is the one fixed, generic reason a static
// "tools" snapshot naming more than maxMCPTools entries is refused —
// shared by decodeMCPToolEntries (checked before the
// (maxMCPTools+1)-th element is ever decoded) and buildImportTools' own
// defense-in-depth len(rawTools) > maxMCPTools check, so both report the
// exact same wording regardless of which one actually trips. It names the
// fixed limit itself, never the offending entry's own tool count or any
// tool's name.
var errMCPStaticToolCountExceeded = fmt.Errorf("static tools snapshot exceeds limit of %d tools", maxMCPTools)

// decodeMCPToolEntries parses an mcp.json entry's raw "tools" value —
// captured as json.RawMessage (see mcpJSONServer.Tools) precisely so this
// can inspect it before anything collapses a tool object's fields into a
// struct — as a JSON array, validating each element's own field names
// strictly (decodeMCPToolFields/validateMCPToolFields) before converting
// it to an mcpJSONTool. A missing or JSON-null "tools" value is not an
// error — both mean "no tools" — matching what an absent or null
// []mcpJSONTool field decoded to under the former struct-tag decode;
// ImportJSON's own independent toolsPresent flag (derived from the
// entry's validated top-level field list, not from this function) is what
// actually decides whether buildImportTools runs at all. Any other
// non-array value, or any non-object element, is refused with the one
// fixed, generic mcpToolDefinitionRefusedMessage.
//
// The element count is enforced (errMCPStaticToolCountExceeded) the
// instant it would exceed maxMCPTools — the same maxMCPTools limit
// buildImportTools itself already enforces, checked here first, before
// that (maxMCPTools+1)-th element's own raw bytes are even decoded, let
// alone its (potentially large) inputSchema or description parsed by
// decodeMCPToolFields/mcpToolFromFields. Without this, an attacker-sized
// static snapshot could force every excess tool's full shape to be
// decoded for no possible benefit before buildImportTools' own
// after-the-fact count check ever ran.
func decodeMCPToolEntries(raw json.RawMessage) ([]mcpJSONTool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	open, err := decoder.Token()
	if err != nil {
		return nil, errors.New(mcpToolDefinitionRefusedMessage)
	}
	if delim, ok := open.(json.Delim); !ok || delim != '[' {
		return nil, errors.New(mcpToolDefinitionRefusedMessage)
	}
	var tools []mcpJSONTool
	for decoder.More() {
		if len(tools) >= maxMCPTools {
			return nil, errMCPStaticToolCountExceeded
		}
		var element json.RawMessage
		if err := decoder.Decode(&element); err != nil {
			return nil, errors.New(mcpToolDefinitionRefusedMessage)
		}
		fields, err := decodeMCPToolFields(element)
		if err != nil {
			return nil, err
		}
		if err := validateMCPToolFields(fields); err != nil {
			return nil, err
		}
		tool, err := mcpToolFromFields(fields)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	if _, err := decoder.Token(); err != nil { // the closing ']'
		return nil, errors.New(mcpToolDefinitionRefusedMessage)
	}
	return tools, nil
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

// errMCPUnsupportedHeader is the one fixed, generic reason bearerFromHeaders
// refuses any header other than a single case-insensitive Authorization
// key. It deliberately never includes the offending header's own name (an
// earlier version of this message did, via fmt.Errorf("header %q is
// unsupported...", name)): a JSON object's key is exactly as
// attacker-controlled as its value, so a header could be named with the
// literal value of a bearer token used elsewhere in the very same entry —
// whether by an operator's templating mistake or a deliberate attempt to
// exploit an error message that echoes it back — and that name would then
// flow into every place an ordinary Unsupported reason already reaches:
// the in-memory ImportReport, the mcp_import_issues table, the
// ReimportMcpJson RPC response, and any client/UI display built from it.
// A fixed, bounded message closes that regardless of what the header is
// named.
var errMCPUnsupportedHeader = errors.New("only a single Authorization bearer header is supported")

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

// maxMCPHeaderEntries bounds how many "headers" members decodeMCPHeaderEntries
// ever appends before refusing further growth — even though
// bearerFromHeaders ultimately supports only a single case-insensitive
// Authorization key, at most one entry beyond that is enough to still
// classify a headers object deterministically as carrying either an
// unsupported header name or more than one Authorization key (see
// header_hardening_test.go's own duplicate/unsupported classification
// tests, none of which needs more than a handful of headers to exercise).
// Without a cap here, a headers object packed with an unbounded number of
// distinct, tiny member names could force this decode loop to grow an
// unbounded slice before bearerFromHeaders ever gets a chance to refuse
// it — the same class of parser-cardinality amplification
// maxMCPServerEntryFields/maxMCPToolObjectFields/maxMCPRootFields close
// for the collections above and below this one.
const maxMCPHeaderEntries = 8

// errMCPTooManyHeaderEntries is the one fixed, generic reason
// decodeMCPHeaderEntries refuses a "headers" object naming more than
// maxMCPHeaderEntries members — checked the instant the
// (maxMCPHeaderEntries+1)-th member's key is seen, before its value is
// ever decoded. It never echoes any header's own name or value.
var errMCPTooManyHeaderEntries = errors.New("headers object has too many entries")

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
		if len(entries) >= maxMCPHeaderEntries {
			return nil, errMCPTooManyHeaderEntries
		}
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

// bearerFromHeaders enforces the one shape an mcp.json entry's headers may
// take: at most a single case-insensitive Authorization key, and nothing
// else. Any other header name is refused with the one fixed, content-free
// errMCPUnsupportedHeader reason — deliberately never echoing that
// header's own name back (see errMCPUnsupportedHeader's own comment for
// why: a header's key is exactly as untrusted as its value, and could
// itself be set to the entry's own bearer token or any other secret) —
// and always takes precedence over the separate
// errMultipleAuthorizationHeaders refusal below. Neither outcome depends
// on Go's randomized map iteration order: "does an unsupported header
// exist at all" and "is there more than one case-insensitive Authorization
// key" are both yes/no facts about the whole set, unlike the discarded
// "which one gets named", so no sorting is needed to make either
// deterministic. Taking entries (preserving every occurrence, including
// an exact-spelling repeat of the same name) rather than a
// map[string]string is what lets the duplicate-Authorization check below
// ever see two entries both literally named "Authorization" in the first
// place — decodeMCPHeaderEntries is what makes that possible; a map could
// never have held them both.
func bearerFromHeaders(headers []mcpHeaderEntry) (string, error) {
	hasUnsupported := false
	var authValue string
	authCount := 0
	for _, entry := range headers {
		if !strings.EqualFold(entry.Name, "authorization") {
			hasUnsupported = true
			continue
		}
		authCount++
		authValue = entry.Value
	}
	if hasUnsupported {
		return "", errMCPUnsupportedHeader
	}
	if authCount > 1 {
		return "", errMultipleAuthorizationHeaders
	}
	if authCount == 0 {
		return "", nil
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(authValue, prefix) {
		return "", errMCPAuthorizationHeaderMalformed
	}
	normalized, err := normalizeBearerToken(strings.TrimPrefix(authValue, prefix))
	if err != nil {
		return "", err
	}
	if normalized == "" {
		return "", errMCPAuthorizationHeaderMalformed
	}
	return normalized, nil
}

// errMCPAuthorizationHeaderMalformed is the fixed reason bearerFromHeaders
// returns when an mcp.json entry's single Authorization header is present
// but does not carry a usable bearer credential: missing the required
// "Bearer " prefix entirely, or carrying nothing but whitespace after it.
// The wording names what an operator must fix (a non-empty bearer
// credential) in plain, operator-meaningful language — never the header's
// own value, and never anything that could be mistaken for a redaction
// artifact or a content-filter placeholder.
var errMCPAuthorizationHeaderMalformed = errors.New("authorization header must contain a non-empty bearer credential")

// maxMCPBearerTokenBytes bounds a bearer token's raw byte length —
// checked in normalizeBearerToken, so it applies identically regardless
// of whether the token arrived as an mcp.json Authorization header or a
// RegisterMcpServer/RotateMcpServerToken bearer_token field. 4096 matches
// maxCredentialBytes in internal/service/integrations: long enough for
// anything a real MCP server issues, short enough that neither an
// mcp.json entry nor a direct registration/rotation request can use the
// sealed_token column (or the request itself) to smuggle in an
// arbitrarily large blob under the guise of a credential.
const maxMCPBearerTokenBytes = 4096

// errMCPBearerTokenTooLong is the fixed, generic reason normalizeBearerToken
// returns when a token's raw byte length exceeds maxMCPBearerTokenBytes.
// It deliberately never states the token's own (attacker-controlled)
// length, only the fixed limit — consistent with never echoing any part
// of the token itself.
var errMCPBearerTokenTooLong = fmt.Errorf("authorization bearer must not exceed %d bytes", maxMCPBearerTokenBytes)

// normalizeBearerToken applies the one set of rules a bearer token must
// satisfy regardless of whether it arrived as an mcp.json Authorization
// header or a RegisterMcpServer/RotateMcpServerToken bearer_token field:
// surrounding whitespace is trimmed, and an empty result means "no token"
// rather than an error. A token that would corrupt the line-based MCP
// Authorization header it is eventually sent in — a line break or any other
// control character — is refused instead of silently truncated or escaped.
// The length check uses Go's len() on the string directly — real UTF-8
// byte count, not a rune count that would undercount a multibyte token
// relative to what actually gets sealed and stored — so a token built
// entirely from multibyte characters cannot slip past the cap by having
// fewer runes than bytes.
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
	if len(token) > maxMCPBearerTokenBytes {
		return "", errMCPBearerTokenTooLong
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

// errMCPTokenMatchesPublicMetadata is the one fixed, generic reason a
// bearer token is refused when it appears, verbatim, inside metadata that
// is not secret at all: the server's own name, or its canonical URL
// (including that URL's own decoded path representation — see
// tokenAppearsInPublicMetadata). A name or a canonical URL is returned by
// every list/register/rotate response, recorded in every audit row for
// that server, and visible in the Flutter MCPs page, so a token that
// equals or is contained in either can never actually be secret: anyone
// who can already see the name or URL already has it. This intentionally
// may refuse a short, ambiguous token that only coincidentally shares
// characters with an unrelated name/URL substring — the secrecy
// invariant (a token must never be recoverable from public metadata
// alone) wins over convenience for that edge case. The message
// deliberately never says which of the two matched, or where: naming
// that would confirm to whoever controls the input exactly which
// comparison tripped, the same reasoning mcpToolDefinitionRefusedMessage
// and errMCPUnsupportedHeader already document for their own checks.
var errMCPTokenMatchesPublicMetadata = errors.New("server token must not appear in the server's own name or url")

// tokenAppearsInPublicMetadata reports whether the non-empty token
// appears, verbatim, anywhere in name or canonicalURL. canonicalURL is
// checked two ways: as the raw string this package actually stores and
// returns (catching a token in the host or an unescaped path segment),
// and via its independently re-parsed, decoded Path (catching a token
// containing a character url.URL.String() would percent-encode — a
// space, a quote, a backslash — that would otherwise hide behind
// whatever escaping that encoding step applies; see
// mcpRawMetadataContainsToken's own doc comment for the same reasoning
// applied to a tool's schema). canonicalURL is always this package's own
// already-canonicalized output (classifyImportedURL's return value, or a
// stored server's own URL column), so re-parsing it here can never fail
// in practice; a parse failure is treated as "no additional decoded
// match" rather than an error, since the raw-string check above still
// runs regardless.
func tokenAppearsInPublicMetadata(token, name, canonicalURL string) bool {
	if token == "" {
		return false
	}
	if strings.Contains(name, token) || strings.Contains(canonicalURL, token) {
		return true
	}
	if parsed, err := url.Parse(canonicalURL); err == nil && strings.Contains(parsed.Path, token) {
		return true
	}
	return false
}

// errMCPTokenMatchesRetainedToolMetadata is the one fixed, generic reason
// rotateServerTokenLocked refuses a new bearer token that appears,
// verbatim, in this server's own retained tool metadata — any present or
// withdrawn tool's name, or its exact stored schema_json representation
// (including a numeric/structural case only a raw-text scan of that stored
// text, not a decoded-value walk, can catch — see
// tokenAppearsInRetainedToolMetadata's own doc comment). A tool descriptor
// is exactly as public as a server's own name/url (returned in every
// list/register/rotate response and recorded in every audit row for that
// server — see tokenAppearsInPublicMetadata for the identical reasoning
// applied there), so a token recoverable from one can never actually be
// secret. The message never says which tool, or which of name/schema
// matched, the same reasoning errMCPTokenMatchesPublicMetadata and
// mcpToolDefinitionRefusedMessage already document for their own,
// structurally identical checks.
var errMCPTokenMatchesRetainedToolMetadata = errors.New("server token must not appear in this server's own retained tool metadata")

// tokenAppearsInRetainedToolMetadata reports whether the non-empty token
// appears, verbatim, in the name or stored schema_json of any tool in
// tools — present or withdrawn alike. Retained (present = false) rows
// matter here, not just a documentation nuance: serverDescriptor/
// buildServerDescriptor read every tool ListMCPServerTools returns, with
// no present filter, so a withdrawn tool's own name/schema is returned in
// every List/Get server descriptor exactly as a present tool's is.
//
// Two independent scans run against each tool's schema — the same pairing
// buildImportTools' own token check runs at import time (see
// mcpRawMetadataContainsToken's own doc comment), just applied to an
// already-*stored* schema_json string instead of freshly-decoded caller
// input, since that is all rotation ever has to compare against:
//
//   - The raw scan (strings.Contains against the exact stored text) catches
//     a token equal to the literal serialized text of a JSON number,
//     boolean, or null value, or one that only spans a structural boundary
//     json.Marshal itself introduced (a quote, a colon) — cases the decoded
//     scan below structurally cannot see, since none of those decode to a
//     Go string.
//   - The decoded scan (mcpRawMetadataContainsToken, run against
//     schema_json re-parsed back into map[string]any/[]any/string) catches
//     the opposite case: a token containing a quote or backslash character,
//     escaped in the stored text in a way a plain substring search of that
//     raw text alone would never find, but visible again once the text is
//     unmarshaled back into its original runtime value.
//
// Neither scan replaces the other. A schema_json that fails to unmarshal
// (never expected in practice — every stored row was itself produced by a
// json.Marshal call before being stored) is treated as no additional
// decoded match, exactly like tokenAppearsInPublicMetadata's own
// url.Parse failure path: the raw scan above still runs regardless and is
// never skipped.
//
// Both scans compare against schema_json's own stored text, not against
// whatever a later McpToolDescriptor.Schema (structpb.NewStruct, then
// protojson) would re-serialize it as: a number's stored text and its
// canonicalized wire form can differ (json.Marshal never re-emits
// "1e2" as "100" the way structpb/protojson's own numeric formatting
// might). A token equal only to that re-canonicalized form and absent
// from the stored text this function actually inspects would not be
// caught here — but the secrecy invariant this check exists for still
// holds regardless: a token that already equals part of a tool's own
// schema, in any of its representations, was never actually secret to
// begin with, the same reasoning tokenAppearsInPublicMetadata's own
// short-token-collision tradeoff documents.
func tokenAppearsInRetainedToolMetadata(token string, tools []repository.MCPServerTool) bool {
	if token == "" {
		return false
	}
	for _, tool := range tools {
		if strings.Contains(tool.Name, token) {
			return true
		}
		if strings.Contains(tool.SchemaJSON, token) {
			return true
		}
		var decoded any
		if err := json.Unmarshal([]byte(tool.SchemaJSON), &decoded); err == nil {
			if mcpRawMetadataContainsToken(decoded, token) {
				return true
			}
		}
	}
	return false
}

// placeholderAdoptionTokenCollision is rotateServerTokenLocked's retained-
// tool check (see tokenAppearsInRetainedToolMetadata's own doc comment),
// applied to the one other place a bearer token gets paired with a
// server's pre-existing retained tools instead of a fresh, toolless row:
// adopting a migration-0016 (or otherwise legacy) placeholder — a
// disabled, non-bundled row with url == "" — via direct RegisterMcpServer
// or a file ImportJSON reimport. Both callers already look the candidate
// row up by name (GetMCPServerByName) before sealing or mutating anything;
// once one identifies a url-empty, non-bundled row as its adoption
// candidate, it calls this with that row's id and the new (already
// normalized) token, before ever calling sealServerToken or the
// repository's own Register/ImportMCPServer. A true result must refuse the
// whole call, leaving the placeholder's row, and its retained tools,
// completely untouched: without this, a chosen token could round-trip
// straight back out through the very register/import response (or a
// later List) whose descriptor still carries the adopted placeholder's
// own retained tool. The two callers differ in exactly what they say
// about the refusal, though: RegisterMcpServer is an authenticated RPC,
// so it returns the explicit, package-shared
// errMCPTokenMatchesRetainedToolMetadata reason, the same one rotation
// uses. ImportJSON instead folds the identical refusal into the one
// fixed, generic mcpToolDefinitionRefusedMessage every other malformed or
// refused mcp.json entry already uses — never naming the token, the
// tool, or the word "metadata" — so an mcp.json entry (or whoever
// controls it) cannot distinguish this collision from any other reason
// that same file's entry might be refused.
//
// Skipped entirely for an empty token, mirroring
// rotateServerTokenLocked's own optimization for the identical reason:
// tokenAppearsInRetainedToolMetadata already returns false for one, so the
// repository read below would be pure overhead for a call that can never
// collide with anything.
func placeholderAdoptionTokenCollision(ctx context.Context, repo *repository.Repository, placeholderID, token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	retainedTools, err := repo.ListMCPServerTools(ctx, placeholderID)
	if err != nil {
		return false, err
	}
	return tokenAppearsInRetainedToolMetadata(token, retainedTools), nil
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
// normalized the same way for both callers, and — once normalized — must
// not appear verbatim in the name or canonical URL it is about to be
// paired with (see tokenAppearsInPublicMetadata).
//
// rawURL is trimmed of leading/trailing whitespace before it ever reaches
// classifyImportedURL, matching what the Flutter client's own registration
// form already does to the URL text field before submitting it
// (`_url.text.trim()` in workspace_pages.dart). Without this, the two
// callers would disagree in ways url.Parse itself makes worse, not just
// cosmetically different: trailing whitespace survives as part of
// url.Parse's Path and comes back out percent-encoded (a trailing space
// becomes a literal "%20" in the stored/returned canonical URL) rather
// than being classified the same as the same URL with no trailing space,
// and leading whitespace makes url.Parse fail outright ("first path
// segment in URL cannot contain colon") for an otherwise well-formed
// https://... value — refusing an entry a client that trims first would
// have accepted. Trimming once, here, before either tier's own checks
// run, keeps a direct RegisterMcpServer/RotateMcpServerToken call and an
// mcp.json import in exact agreement with each other and with the
// client's own pre-trimmed input, for both the remote-URL and
// local-container tiers.
func validateServerDefinition(name, rawURL string, requestedTier *repository.MCPServerTier, token string) (validatedMCPServer, error) {
	if err := validateMCPServerName(name); err != nil {
		return validatedMCPServer{}, err
	}
	tier, canonicalURL, err := classifyImportedURL(strings.TrimSpace(rawURL))
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
	if tokenAppearsInPublicMetadata(normalizedToken, name, canonicalURL) {
		return validatedMCPServer{}, errMCPTokenMatchesPublicMetadata
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

// errMCPConfigNotRegularFile is openRegularMCPConfigFile's own internal
// signal that a successfully opened descriptor is not a regular file —
// used only for an fstat-confirmed non-regular result (a FIFO opened
// non-blockingly with no writer, a device, a directory); ENOENT and
// ELOOP are the raw OS errors from open(2) itself. Every caller collapses
// all of these into the one fixed, path-free "read mcp.json failed"
// message regardless of which one occurred.
var errMCPConfigNotRegularFile = errors.New("mcp.json is not a regular file")

// openRegularMCPConfigFile opens path for reading using O_NOFOLLOW and
// O_NONBLOCK — the same two flags, and the same open-then-fstat shape, the
// mcp-files sandbox already uses for exactly this reason (see
// FilesTools.openRegularFileContext in
// turing-backend/mcp-files/internal/tools/safe_fs.go) — replacing a
// separate os.Lstat-then-plain-os.Open pair that left a TOCTOU gap
// between the two syscalls: path could in principle be replaced with
// something non-regular in the moment between them, and a plain os.Open
// reached in that window would either silently follow a symlink or block
// indefinitely on a FIFO's read-side open waiting for a writer that may
// never come — no bounded read afterward can help, since the read never
// even starts. O_NOFOLLOW makes open(2) itself fail (ELOOP) rather than
// resolving a symlink as the final path component; O_NONBLOCK makes a
// FIFO's open return immediately regardless of whether a writer is
// connected, rather than blocking the calling goroutine. A missing path
// (ENOENT) is returned as-is for the caller to treat as "no mcp.json".
// Once open, the descriptor — not the path a second time — is fstat-ed:
// this is the same open file description any subsequent read would use,
// so nothing about the path can change underneath this check the way it
// could between two path-based syscalls. Only a confirmed regular file is
// returned; every other outcome (FIFO, socket, device, directory) closes
// the descriptor and returns errMCPConfigNotRegularFile, never anything
// that could repeat path in a message a caller might log or return
// as-is (this function itself never logs).
func openRegularMCPConfigFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = file.Close()
		return nil, errMCPConfigNotRegularFile
	}
	return file, nil
}

// ReimportConfiguredJSON re-reads mcp.json from the configured config root
// and imports it, the same way app startup does. An absent file is not a
// failure: it clears any previously recorded import issues and reports an
// empty ImportReport, so a client that reimports after deleting mcp.json
// sees a clean slate rather than a stale error. A malformed document, or a
// fatal error ImportJSON itself cannot attribute to a single entry (a
// canceled context, or some other repository failure between entries — see
// ImportJSON's own switch default case), is recorded as a bounded
// "_document" entry in mcp_import_issues via recordDocumentRefusal and
// returned inside the ImportReport rather than as an error, so a client
// can display why the run did not fully complete the same way it displays
// any other refused entry — alongside, not instead of, every entry that
// really did commit earlier in the same run (see recordDocumentRefusal's
// own doc comment). Any other failure to read the file itself is returned
// as a fixed error that never repeats the file's contents or path.
//
// mcp.json is opened through openRegularMCPConfigFile, whose own doc
// comment covers exactly what makes that safe against a FIFO, a symlink,
// a socket, a device, or a directory in its place — including one swapped
// in at the last possible moment, since there is no longer a separate
// check-then-open pair of path-based syscalls for such a swap to land
// between. A directory is refused the same fixed way every other
// non-regular result is, matching this function's own prior behavior.
//
// Only once the descriptor is confirmed to be a regular file is it read,
// through io.LimitReader(maxMCPImportDocumentBytes+1) — the same
// bounded-read shape mcpClient.request already applies to a live HTTP
// response body: a plain, unbounded read (e.g. os.ReadFile) has no size
// bound of its own and would buffer the entire file — however large, or
// however slow a legitimate regular file might be to fully materialize —
// before ImportJSON's own in-memory size check ever got a chance to run.
// Reading through a bounded LimitReader instead means at most
// maxMCPImportDocumentBytes+1 bytes are ever allocated, and the read
// completes (with its own synthesized io.EOF) without waiting for the
// underlying source to actually end.
func (s *Server) ReimportConfiguredJSON(ctx context.Context) (ImportReport, error) {
	if s.configRoot == "" {
		return ImportReport{}, errMCPConfigRootNotConfigured
	}
	path := filepath.Join(s.configRoot, "mcp.json")
	if s.beforeConfigFileOpen != nil {
		s.beforeConfigFileOpen(path)
	}
	file, err := openRegularMCPConfigFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if clearErr := s.repo.ReplaceMCPImportIssues(ctx, map[string]string{}); clearErr != nil {
			return ImportReport{}, fmt.Errorf("clear mcp.json import issues: %w", clearErr)
		}
		return ImportReport{}, nil
	case err != nil:
		log.Printf("open mcp.json: %v", err)
		return ImportReport{}, errors.New("read mcp.json failed")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxMCPImportDocumentBytes+1))
	closeErr := file.Close()
	if err != nil {
		log.Printf("read mcp.json: %v", err)
		return ImportReport{}, errors.New("read mcp.json failed")
	}
	if closeErr != nil {
		log.Printf("close mcp.json: %v", closeErr)
		return ImportReport{}, errors.New("read mcp.json failed")
	}
	if len(data) > maxMCPImportDocumentBytes {
		return s.recordDocumentRefusal(ctx, ImportReport{}, errMCPImportDocumentTooLarge)
	}
	report, err := s.ImportJSON(ctx, data)
	if err != nil {
		return s.recordDocumentRefusal(ctx, report, err)
	}
	return report, nil
}

// recordDocumentRefusal folds a whole-document-level failure — a
// document too large to import, or a fatal error ImportJSON itself could
// not attribute to a single entry — into report's own Unsupported map as
// a bounded "_document" entry, and persists the merged issues list.
//
// report may already carry real Imported/Skipped/Unsupported entries from
// ImportJSON's own per-entry transactions, each of which has already
// durably committed by the time this runs (see ImportJSON's own doc
// comment on why its named returns preserve exactly that). This never
// discards them the way returning a fresh
// ImportReport{Unsupported: {"_document": ...}} in their place used to:
// the persisted issues list is the merged map (every already-collected
// reason plus "_document"), not the "_document" entry alone, for the same
// reason.
//
// The merged issues are persisted through a context detached from ctx
// (see detachedBoundedContext), the same way auditMCPEvent already
// detaches its own post-commit write: by the time this runs, every
// Imported entry has already committed, so recording that state is
// exactly the kind of post-commit bookkeeping that must not be skippable
// merely because the caller's own context is canceled — which, for a
// canceled-context cause, is definitionally already true of ctx itself.
// A client that cancels a reimport must not also erase the record of what
// already happened.
//
// Persisting can itself still fail (a genuine repository error,
// independent of anything already committed): that failure is returned
// as-is, but report — with "_document" already folded in — is still
// returned alongside it rather than discarded, so a caller that must
// ultimately surface Internal (see ReimportMcpJson) still has
// report.Imported/Skipped/Unsupported to notify/audit before it does.
func (s *Server) recordDocumentRefusal(ctx context.Context, report ImportReport, cause error) (ImportReport, error) {
	if report.Unsupported == nil {
		report.Unsupported = make(map[string]string)
	}
	report.Unsupported["_document"] = boundedStatusMessage(cause.Error())
	recordCtx, cancel := detachedBoundedContext(ctx, auditRecordTimeout)
	defer cancel()
	if err := s.repo.ReplaceMCPImportIssues(recordCtx, report.Unsupported); err != nil {
		return report, fmt.Errorf("record mcp.json import failure: %w", err)
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
