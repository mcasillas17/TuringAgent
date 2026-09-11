package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	egress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/mcpwire"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

const maxMCPResponseBytes int64 = 1024 * 1024

// maxMCPImportDocumentBytes bounds an entire mcp.json document's raw size,
// checked before it is ever handed to json.Decoder — the same reasoning
// maxMCPResponseBytes already applies to a single live HTTP response, but
// for the whole file a reimport reads instead. Without it, an
// arbitrarily large document (most of it outside any single server's
// "tools" snapshot, so mcpwire.MaxToolBytes alone never bounds it) could force
// this process to buffer and decode an unbounded amount of memory. The
// cap is tied to, not independent of, the existing per-snapshot limit:
// mcp.json commonly registers only a handful of servers, and a document
// giving eight of them a full mcpwire.MaxToolBytes-sized static snapshot would
// still comfortably fit inside this bound, with slack left over for the
// URL/header/JSON-syntax overhead none of that per-tool accounting counts.
const maxMCPImportDocumentBytes = 8 * mcpwire.MaxToolBytes

// errMCPImportDocumentTooLarge is the one fixed, generic reason both
// ImportJSON's in-memory size check and ReimportConfiguredJSON's own
// bounded file-read check use when a document exceeds
// maxMCPImportDocumentBytes, so a caller sees the identical message either
// way: a byte count and the fixed cap, never any of the document's own
// content or its on-disk path.
var errMCPImportDocumentTooLarge = fmt.Errorf("mcp.json exceeds the maximum supported document size of %d bytes", maxMCPImportDocumentBytes)

// maxMCPImportEntries bounds how many "mcpServers" entries a single
// mcp.json document may declare, checked before any entry is looked up or
// processed. It deliberately reuses repository.MaxNonBundledMCPServers —
// the same limit the registry itself enforces per non-bundled row — rather
// than an independent number: a document naming more entries than could
// ever actually fit in the registry would otherwise still cost one
// repository lookup per name before eventually being refused one entry at
// a time anyway, once the registry's own count cap was reached.
const maxMCPImportEntries = repository.MaxNonBundledMCPServers

// errMCPImportTooManyEntries is the one fixed, generic reason ImportJSON
// returns when a document's entry count exceeds maxMCPImportEntries.
// ReimportConfiguredJSON collapses it (like any other ImportJSON error)
// into a single bounded "_document" entry — never one row per named
// server — so mcp_import_issues and the ReimportMcpJson RPC's own Refused
// list stay bounded regardless of how many names an oversized document
// claims.
var errMCPImportTooManyEntries = fmt.Errorf("mcp.json declares more servers than the maximum supported entry count of %d", maxMCPImportEntries)

// errMCPResultCannotBeRedacted is the one fixed, generic reason
// c.request refuses a vendor's result outright, after redaction, rather
// than ever returning it: redactMCPSecret's per-scalar substitution
// cannot remove a token that is itself a piece of JSON syntax (e.g. the
// quote-colon-quote between an object's key and its value) — no matter
// what any single scalar's value is replaced with, that structural text
// survives around it. The safety-net scan below is what still finds
// such a token in the fully-redacted result; this is the fixed message
// it is refused with, never any part of the vendor's own response.
var errMCPResultCannotBeRedacted = errors.New("MCP response result could not be safely redacted")

// mcpClient speaks one MCP connection to one endpoint.
//
// It is deliberately built fresh for each discovery and each dispatch (see
// discover and CallTool) rather than cached. That costs one extra round trip —
// the `initialize` handshake — per operation, and buys three things worth more
// than the round trip: a rotated credential, a changed endpoint, or a disabled
// server can never be served out of connection state captured before the
// change; no session or negotiated state can be reused across two distinct
// servers, because no two operations share an instance; and there is no cache
// to invalidate, so there is no invalidation to get wrong.
//
// Deliberate ceiling: one handshake per third-party dispatch. If that round trip ever
// shows up in a profile, the upgrade is a cache keyed by (server id, endpoint,
// sealed-token fingerprint) that is dropped on rotation, disable and delete —
// not a longer-lived client with a mutable endpoint.
type mcpClient struct {
	endpoint   string
	token      string
	httpClient *http.Client
	mu         sync.Mutex
	nextID     int64
	// cancelNotices tracks the detached notifications/cancelled goroutines so a
	// session release cannot overtake one. Both are detached to keep a slow peer
	// off the credential lock, which leaves them unordered — and a DELETE that
	// lands first terminates the session the cancellation is about, so the peer
	// answers it 404 and is never told to stop the work.
	cancelNotices sync.WaitGroup
	// openedSession holds the id the peer named on an initialize response,
	// whatever becomes of that response afterwards — a refused status, a body
	// that cannot be read, a handshake that dies at the notification. The peer
	// opened the session in every one of those cases, Invalidate wipes the
	// Connection's copy, and releaseSession would otherwise see nothing to
	// delete and abandon it — once per attempt against a flaky peer, which is
	// the leak the delete exists to stop.
	//
	// The one id it never holds is one the transport does not permit: see
	// roundTrip for why such a session is left unreleased rather than echoed
	// back.
	openedSession string
	// openedVersion is the revision the peer named in that same handshake, kept
	// because the session is adopted from the response headers before the
	// envelope is read: a peer may assign a session and then answer with a
	// revision this client rejects. Releasing it under Turing's own revision
	// would claim a negotiation that never happened.
	openedVersion string
	// connection is the lifecycle state — handshake completion, negotiated
	// revision, assigned session — shared with the agent runtime's client so
	// the ordering and session-loss rules exist in one place.
	connection mcpwire.Connection
}

// mcpClientImplementationVersion is this client's own version, sent as
// clientInfo.version in the handshake. It is Turing's implementation version,
// not the protocol revision.
const mcpClientImplementationVersion = "1.0.0"

// mcpCancellationNotifyTimeout bounds the best-effort notifications/cancelled an
// abandoned dispatch sends on a context detached from the caller's own.
const mcpCancellationNotifyTimeout = 2 * time.Second

// mcpSessionReleaseTimeout bounds the best-effort session delete, which runs
// detached from the dispatch that opened the session.
const mcpSessionReleaseTimeout = 2 * time.Second

// idempotentMCPMethods may be replayed once after a lost session. This client
// only ever issues tools/list (discovery) and tools/call (dispatch), and
// tools/call is deliberately absent: a call whose delivery is ambiguous may
// already have run and committed on the peer, and repeating it automatically
// would risk doing the work twice while looking like recovery.
var idempotentMCPMethods = map[string]struct{}{
	"tools/list": {},
}

type redactedMCPError struct {
	cause   error
	message string
}

func (e redactedMCPError) Error() string { return e.message }
func (e redactedMCPError) Unwrap() error { return e.cause }

// newMCPClient wraps an already-hardened transport. A nil client is refused
// rather than defaulted: http.DefaultClient has no subnet-validating dialer and
// no timeouts, so defaulting to it would silently undo the per-tier hardening
// clientFor exists to apply, and the call would still succeed — just unhardened,
// with nothing failing. Every caller passes a client, and one that does not
// finds out immediately.
//
// Redirect refusal is applied here rather than left to the caller, matching the
// runtime client: this path's request bodies carry a registered server's sealed
// bearer, and a redirect target is chosen by the peer, so the invariant must not
// depend on which client a call site happened to pass. Copying leaves the
// caller's own value untouched.
func newMCPClient(endpoint string, token string, httpClient *http.Client) *mcpClient {
	if httpClient == nil {
		// Loud, because the alternative is silent: egress.NoRedirectClient
		// substitutes http.DefaultClient for a nil base, so without this the
		// call would succeed against an unhardened client and nothing would say
		// so. A nil here is a programming error at wiring time, not a runtime
		// condition a peer can cause.
		panic("mcpregistry: newMCPClient requires an http.Client; see clientFor")
	}
	return &mcpClient{
		endpoint:   endpoint,
		token:      token,
		httpClient: egress.NoRedirectClient(httpClient),
		// Not 1. This client is built fresh for every dispatch, so numbering
		// from a constant makes two concurrent calls to one endpoint issue the
		// same ids — and a peer that is sessionless keys an in-flight request by
		// the caller's identity plus that id, exactly as Turing's own bundled
		// servers do. A notifications/cancelled from one dispatch would then
		// stop the other's call. A peer that assigns sessions tells the two
		// apart already; one that does not now sees ids that do not overlap.
		nextID: mcpwire.StartingRequestID(),
	}
}

func (c *mcpClient) callTool(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}

	return c.request(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
}

func (c *mcpClient) listTools(ctx context.Context) (tools []map[string]any, err error) {
	defer func() {
		err = redactMCPErrorValue(err, c.token)
	}()
	return mcpwire.CollectTools(func(params map[string]any) (map[string]any, error) {
		// requestRaw, not request: discover() (this call's sole caller) must see
		// each tool exactly as the peer sent it, before request's own
		// marker-substitution redaction could replace a bearer echo with the
		// fixed "[redacted]" text and let the rest of discovery — and, past it,
		// RecordDiscovery — treat that still-attacker-shaped tool as if it were
		// clean. See requestRaw's own doc comment for why callTool, unlike this
		// method, still goes through request instead.
		return c.requestRaw(ctx, "tools/list", params)
	})
}

// request performs the JSON-RPC round trip via requestRaw and then
// applies this package's ordinary redact-or-refuse handling to a
// *successful* result: redactMCPSecret's per-scalar, per-key
// substitution first, then the whole-result structural safety net below.
// This is what every generic caller — currently only callTool, and
// through it CallRegisteredMcpTool — needs: a vendor's tool-call result
// can be arbitrary, untrusted data that still has to be returned to the
// caller in some form, so a matched bearer echo is redacted in place
// rather than refusing the whole call outright. listTools deliberately
// does not use this: see requestRaw's own doc comment.
func (c *mcpClient) request(ctx context.Context, method string, params map[string]any) (result map[string]any, err error) {
	defer func() {
		err = redactMCPErrorValue(err, c.token)
	}()
	result, err = c.requestRaw(ctx, method, params)
	if err != nil {
		return nil, err
	}
	redacted, ok := redactMCPSecret(result, c.token).(map[string]any)
	if !ok {
		return nil, errors.New("MCP response result must be an object")
	}
	// A final, whole-result safety net: redactMCPSecret's per-scalar
	// substitution above surgically replaces each string/number/bool/null
	// value that itself matched, but it cannot remove a token that is a
	// piece of JSON syntax rather than any single value's own content —
	// the quote-colon-quote between a key and its value exists
	// regardless of what that value is redacted to. Re-encoding the
	// already-redacted result and checking it one more time catches
	// exactly that residue; if the token is still there, the whole
	// result is refused with the one fixed, generic reason rather than
	// ever returned still carrying it. This never fires for an ordinary
	// string echo (already fully substituted above) or for the numeric/
	// bool/null cases (whose one matching scalar is now the fixed marker
	// string, not the original token). This scans the same
	// encoding/json text CallRegisteredMcpTool's eventual
	// structpb.NewStruct conversion is built from field-by-field — not
	// the wire bytes of any later protobuf or protojson re-encoding of
	// that Struct — so it is a check against the shape this function
	// itself is about to hand back, not a guarantee about every possible
	// downstream re-serialization.
	if c.token != "" {
		if encoded, err := json.Marshal(redacted); err == nil && strings.Contains(string(encoded), c.token) {
			return nil, errMCPResultCannotBeRedacted
		}
	}
	return redacted, nil
}

// requestRaw performs the JSON-RPC round trip — transport, envelope
// decoding, response-size limits, and the peer's own JSON-RPC error
// object — and returns the result exactly as the peer sent it, without
// request's own redact-or-refuse handling of a *successful* result.
// request's generic callers (callTool) must never see an unredacted
// bearer echo, so they call request instead; listTools calls this
// directly so discover's own raw-metadata scan
// (mcpRawMetadataContainsToken, run before a tool's name is ever
// interpolated into an error or persisted via RecordDiscovery — see
// discover's own doc comment) can inspect each tool exactly as received.
// A marker already substituted in place of the token would hide a
// bearer echo from that scan just as effectively as never returning the
// tool at all: discover needs to *refuse* a token-bearing tool outright,
// the same way buildImportTools already refuses one in a static mcp.json
// snapshot, not merely have it redacted and still persisted.
//
// The peer's own JSON-RPC error object (envelope.Error.Message) is still
// redacted inline below, and the deferred wrapper still redacts whatever
// error this function itself returns: neither of those ever carries tool
// metadata this package would persist, so there is no raw-scan tradeoff
// for either — only a *successful* result's own redact-or-refuse
// handling moves to request, above.
func (c *mcpClient) requestRaw(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	if err := c.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	result, err := c.roundTrip(ctx, method, params)
	if err == nil || !errors.Is(err, mcpwire.ErrSessionLost) {
		return result, err
	}
	if _, replayable := idempotentMCPMethods[method]; !replayable {
		return nil, err
	}
	if err := c.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	return c.roundTrip(ctx, method, params)
}

// ensureInitialized performs the `initialize` request and the `initialized`
// notification once for this connection, before any operation-phase message.
//
// A failure leaves the connection uninitialized and is returned to the caller:
// discovery records it as the server's liveness status, and a dispatch refuses
// rather than proceeding on an unnegotiated connection. Nothing is retried
// silently and nothing is downgraded — an authentication failure, a transport
// failure, a version the peer picked that Turing does not implement, or a peer
// that advertises no tools capability all end the operation.
func (c *mcpClient) ensureInitialized(ctx context.Context) error {
	return c.connection.EnsureInitialized(
		ctx,
		func() (string, error) {
			result, err := c.roundTrip(ctx, mcpwire.MethodInitialize,
				mcpwire.InitializeParams("turing-orchestrator", mcpClientImplementationVersion))
			if err != nil {
				return "", err
			}
			version, err := mcpwire.NegotiatedVersion(result)
			if err != nil {
				return "", redactMCPErrorValue(err, c.token)
			}
			return version, nil
		},
		func() error { return c.notify(ctx, mcpwire.NotificationInitialized, map[string]any{}) },
	)
}

// notifyCancelled tells the peer that a dispatch its caller abandoned can stop.
//
// It is best effort, and call sites start it in its own goroutine. The caller's
// own context is already cancelled, so this runs on a short detached one and
// failing to deliver it changes nothing the caller sees — but the dispatch is
// still holding that server's credential read lock while it runs, and the peer
// chooses how slowly it answers. Running it inline would let a stalled peer hold
// that lock for the full timeout, blocking an operator rotating a leaked token
// and, because Go's RWMutex gives a waiting writer priority, every reader behind
// it. Whether the abandoned caller is still watching is beside the point.
//
// Cancellation asks a peer to stop working and free resources — it never asserts
// that a mutation the peer already committed has been undone, and nothing is
// re-dispatched.
func (c *mcpClient) notifyCancelled(requestID int64, reason string) {
	if !c.connection.Ready() {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), mcpCancellationNotifyTimeout)
	defer cancel()
	_ = c.notify(ctx, mcpwire.NotificationCancelled, map[string]any{
		"requestId": requestID,
		"reason":    reason,
	})
}

// startCancelNotice sends the cancellation on its own goroutine and records it,
// so releaseSession can wait for it rather than racing it to the peer.
func (c *mcpClient) startCancelNotice(requestID int64) {
	c.cancelNotices.Add(1)
	go func() {
		defer c.cancelNotices.Done()
		c.notifyCancelled(requestID, "the caller's context ended")
	}()
}

// releaseSession terminates a peer-assigned session when this client is done
// with it.
//
// The transport says a client that no longer needs a session SHOULD delete it,
// and this client is built fresh for every discovery and every dispatch — so
// against a peer that assigns sessions, skipping this would abandon one per
// operation, reclaimable only by that peer's own timeout. Turing would be
// consuming a third party's resources at its own dispatch rate.
//
// Best effort, on a detached deadline: a peer that refuses the delete, answers
// 405, or has already dropped the session changes nothing. A sessionless peer —
// including both bundled servers — never reaches the request at all.
//
// Call sites start this in its own goroutine rather than deferring it inline.
// The peer chooses both whether a session exists and how slowly it answers, so
// a synchronous release would let it add the full timeout to every dispatch's
// tail latency — and, worse, that time would be spent still holding the
// server's credential read lock, delaying an operator rotating a leaked
// third-party token. The client is built fresh per operation and nothing else
// touches it once the call returns, so detaching is safe.
func (c *mcpClient) releaseSession() {
	// Ordering, not just politeness: a cancellation already in flight names this
	// session, and terminating it first would make the peer 404 the cancellation
	// and keep working on a call nobody is waiting for. Both goroutines are
	// bounded by their own timeouts, so this wait is too.
	c.cancelNotices.Wait()

	// What the peer said, not what the Connection kept. The Connection's copy is
	// the same value while the handshake holds and is gone once Invalidate runs,
	// so consulting it first added a branch that could only ever agree — and
	// keeping two mutable copies of one piece of peer state is what produced a
	// delete with no version header, and then a delete claiming a revision the
	// peer never named. One source cannot disagree with itself.
	c.mu.Lock()
	session, version := c.openedSession, c.openedVersion
	c.mu.Unlock()
	if session == "" {
		return
	}
	// The transport requires this header on every request after initialization,
	// so a peer that enforces it would answer a bare DELETE 400 and keep the
	// session — the very leak this delete exists to close. It carries whatever
	// revision the peer itself named, which need not be one this client
	// implements: the session is adopted from the response headers before the
	// envelope is read, so a peer may assign one and then answer with a revision
	// the handshake rejects. Turing's own revision is claimed only when the peer
	// named none at all — a transport or decode failure that produced no answer
	// to quote — where there is nothing better to send.
	if version == "" {
		version = mcpwire.ProtocolVersion
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), mcpSessionReleaseTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint, nil)
	if err != nil {
		return
	}
	if c.token != "" {
		req.Header.Set("authorization", "Bearer "+c.token)
	}
	req.Header.Set(mcpwire.ProtocolVersionHeader, version)
	req.Header.Set(mcpwire.SessionHeader, session)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// notify sends one JSON-RPC notification on the negotiated connection.
func (c *mcpClient) notify(ctx context.Context, method string, params map[string]any) (err error) {
	defer func() {
		err = redactMCPErrorValue(err, c.token)
	}()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	response, err := c.post(ctx, payload)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return c.statusError(response)
	}
	return nil
}

// post carries one JSON-RPC message with the headers the Streamable HTTP
// transport requires: both accepted media types, the sealed bearer, and — once
// negotiated — the protocol version and the peer's own session id. The session
// id is an addressing token the peer chose; it is never sent in place of, or
// treated as, the credential.
func (c *mcpClient) post(ctx context.Context, payload []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	mcpwire.SetRequestHeaders(request, c.token, &c.connection)
	return c.httpClient.Do(request)
}

// statusError turns a non-2xx into an error, recognizing the one status the
// transport gives a specific meaning: 404 for a request that carried a session
// id means the peer has terminated that session.
func (c *mcpClient) statusError(response *http.Response) error {
	if mcpwire.SessionLost(response) {
		c.connection.Invalidate()
		return mcpwire.ErrSessionLost
	}
	return fmt.Errorf("MCP HTTP %d", response.StatusCode)
}

// roundTrip performs one JSON-RPC request/response exchange with no lifecycle
// handling of its own, so ensureInitialized can use it for the handshake
// without recursing.
func (c *mcpClient) roundTrip(ctx context.Context, method string, params map[string]any) (result map[string]any, err error) {
	defer func() {
		err = redactMCPErrorValue(err, c.token)
	}()
	id := c.nextRequestID()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}

	response, err := c.post(ctx, payload)
	if err != nil {
		if mcpwire.CancelledMidRequest(ctx, method) {
			c.startCancelNotice(id)
		}
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	// Read before the status check and the body read, because both can fail
	// after the peer has already opened the session: the header is the only
	// evidence it exists, and releaseSession is the only thing that closes it.
	// An id the transport does not permit is not kept — see the adoption below.
	assignedSession := ""
	if method == mcpwire.MethodInitialize {
		assignedSession = response.Header.Get(mcpwire.SessionHeader)
		if assignedSession != "" && mcpwire.ConformingPeerHeaderValue(assignedSession) {
			c.mu.Lock()
			c.openedSession = assignedSession
			c.mu.Unlock()
		}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, c.statusError(response)
	}
	data, err := mcpwire.ReadResponseMessage(
		response.Header.Get("Content-Type"), response.Body, maxMCPResponseBytes)
	if err != nil {
		// A peer that has already sent headers and is streaming its answer
		// fails here rather than at the POST, which is the normal shape for a
		// long-running third-party tool call.
		if mcpwire.CancelledMidRequest(ctx, method) {
			c.startCancelNotice(id)
		}
		return nil, err
	}
	if assignedSession != "" {
		// Refusing the id also means the session behind it is deliberately left
		// to the peer's own timeout, because it was never recorded above.
		//
		// That is a real cost — this client is built fresh per operation, so a
		// peer stuck on a bad id abandons one session per attempt — and it is
		// still the right trade. The bound exists precisely so a hostile or
		// broken endpoint cannot make Turing retain and re-transmit a header of
		// megabytes (http.Transport hands back up to 10 MB of response headers),
		// and echoing the id in a teardown request is retaining and
		// re-transmitting it. A peer that violates the id rule has already
		// failed every call Turing will make to it.
		if !c.connection.AdoptSession(assignedSession) {
			return nil, mcpwire.ErrUnusableSessionID
		}
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int64           `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int64  `json:"code"`
			Message string `json:"message"`
			// Declared, never read. `data` is an optional JSON-RPC error
			// member a conforming peer may attach — the reference SDK does,
			// for its unsupported-protocol-version error — and the decoder's
			// DisallowUnknownFields applies to nested objects too, so leaving
			// it out turns a peer's perfectly valid protocol error into a
			// decode failure and loses its code and message.
			Data json.RawMessage `json:"data"`
		} `json:"error"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, err
	}
	if envelope.JSONRPC != "2.0" || envelope.ID != id {
		return nil, errors.New("MCP response does not match request")
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf(
			"MCP error %d: %s",
			envelope.Error.Code,
			redactMCPSecretString(envelope.Error.Message, c.token),
		)
	}
	if len(envelope.Result) == 0 {
		return nil, errors.New("MCP response result is required")
	}
	result = make(map[string]any)
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		return nil, errors.New("MCP response result must be an object")
	}
	if result == nil {
		return nil, errors.New("MCP response result must be an object")
	}
	if method == mcpwire.MethodInitialize {
		// Recorded beside the session for releaseSession, before the caller gets
		// to reject this revision — and under the same bound as the session id,
		// because it is echoed the same way.
		//
		// "net/http will refuse to write a bad one" is not enough: a refused
		// header value fails the whole teardown request, which abandons the
		// session this client had already recorded. Turing's own revision is a
		// safe thing to name there, so a revision it cannot send is simply not
		// kept, and releaseSession falls back exactly as it does for a peer that
		// named none.
		if version, named := result["protocolVersion"].(string); named &&
			mcpwire.ConformingPeerHeaderValue(version) {
			c.mu.Lock()
			c.openedVersion = version
			c.mu.Unlock()
		}
	}
	return result, nil
}

func redactMCPErrorValue(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	redacted := redactMCPSecretString(err.Error(), secret)
	if redacted == err.Error() {
		return err
	}
	return redactedMCPError{cause: err, message: redacted}
}

func (c *mcpClient) nextRequestID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

// mcpRedactedMarker is the one fixed, generic replacement text every
// redaction in this file uses in place of a secret it finds: a
// substring occurrence inside a string (redactMCPSecretString), or the
// entire value of a scalar whose canonical JSON text contains the secret
// (redactMCPSecretScalar). Sharing one constant means a caller can never
// see two different placeholder spellings depending on which of the two
// paths happened to redact a given value.
const mcpRedactedMarker = "[redacted]"

// redactMCPSecret recursively redacts secret out of value, which is
// whatever a vendor's JSON-RPC result decoded into: a string, a
// []any/map[string]any (walked recursively, map keys included), or any
// other JSON scalar (redactMCPSecretScalar — a number, a bool, or JSON
// null, none of which json.Unmarshal ever decodes into a Go string).
func redactMCPSecret(value any, secret string) any {
	if secret == "" {
		return value
	}
	switch typed := value.(type) {
	case string:
		return redactMCPSecretString(typed, secret)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = redactMCPSecret(item, secret)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[redactMCPSecretString(key, secret)] = redactMCPSecret(item, secret)
		}
		return result
	default:
		return redactMCPSecretScalar(typed, secret)
	}
}

// mcpSecretFreeOrEmpty is the one proven-safe fallback every redaction
// primitive in this file falls back to when its own ordinary substitution
// cannot guarantee secret is actually gone: it returns candidate unchanged
// if candidate no longer contains secret, or "" otherwise. Empty is a
// universal safe answer — an empty string can never contain a non-empty
// secret — chosen deliberately over any other fixed placeholder, because
// mcpRedactedMarker itself is exactly the kind of candidate this guards
// against: a short or unlucky secret (equal to, or a substring of, "e",
// "red", "ed]", or the whole marker "[redacted]") can survive inside the
// very text meant to redact it, since strings.ReplaceAll only ever
// replaces the *matched* occurrence(s) of secret with mcpRedactedMarker
// and has no way to know that its own replacement text might reintroduce
// the exact thing it just removed. Without this guard, a result or error
// containing such a secret would either keep leaking it (a single-letter
// secret like "e" appears twice inside "[redacted]" alone) or, at the
// whole-result safety net in request() below, be refused outright with
// errMCPResultCannotBeRedacted even for an ordinary, otherwise perfectly
// redactable echo — a false availability refusal this function is what
// prevents.
func mcpSecretFreeOrEmpty(candidate, secret string) string {
	if secret != "" && strings.Contains(candidate, secret) {
		return ""
	}
	return candidate
}

func redactMCPSecretString(value string, secret string) string {
	if secret == "" {
		return value
	}
	return mcpSecretFreeOrEmpty(strings.ReplaceAll(value, secret, mcpRedactedMarker), secret)
}

// redactMCPSecretScalar handles every JSON scalar redactMCPSecret's own
// switch does not already: a number (decoded as float64), a bool, or
// JSON null (decoded as an untyped nil interface — value == nil here).
// None of these has a substring of its own a partial, in-place
// replacement could target the way redactMCPSecretString's
// strings.ReplaceAll does for a string: encoding/json never re-quotes a
// number, a bool, or null, so there is no way to redact only part of
// one. Its own canonical JSON wire text is instead checked for secret as
// a substring — exactly the text json.Marshal would otherwise still be
// about to emit for it, once whatever holds it is finally serialized —
// and a match replaces the whole scalar with the fixed redaction
// marker, changing its wire type (e.g. a number becomes a string)
// rather than ever letting a secret-bearing number, boolean, or null
// reach a caller. That replacement marker itself goes through
// mcpSecretFreeOrEmpty too, for the identical reason redactMCPSecretString
// does: a short or unlucky secret occurring only in "1e10"-shaped
// scientific notation, or in "true"/"false", can still be a substring of
// "[redacted]" itself, and no wire-type change can fix that on its own.
func redactMCPSecretScalar(value any, secret string) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	if !strings.Contains(string(encoded), secret) {
		return value
	}
	return mcpSecretFreeOrEmpty(mcpRedactedMarker, secret)
}
