package mcpwire

import (
	"context"
	"errors"
	"net/http"
	"sync"
)

// Connection is the lifecycle state of one MCP connection: whether the
// handshake has completed, the revision it negotiated, and the session the peer
// assigned, if any.
//
// It lives here rather than in each client because both of Turing's MCP clients
// need exactly the same state machine, and the parts that genuinely differ
// between them — how a result is redacted, how a transport error is classified
// as retryable — are not part of it. Keeping one copy is what stops a fix to
// the handshake ordering or the session-loss rule from landing in one client
// and not the other.
//
// The zero value is a usable, uninitialized connection.
type Connection struct {
	// gate serializes handshakes and is held across their network round trips.
	// It is a one-slot channel rather than a mutex so a caller can abandon the
	// wait on its own context, and it is created lazily because the zero
	// Connection is usable. mu guards the state fields and is never held across
	// a round trip; the gate is read under it like every other field.
	gate        chan struct{}
	mu          sync.Mutex
	initialized bool
	version     string
	sessionID   string
	// generation increments on every Invalidate, so a handshake that was
	// invalidated while it was still running cannot publish its result over the
	// wipe. See EnsureInitialized.
	generation uint64
}

// Ready reports whether the handshake has completed, notification included.
func (c *Connection) Ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initialized
}

// headers returns the negotiated revision and session id to announce on the
// next request. Either may be empty: before negotiation there is no revision,
// and a sessionless peer never assigns a session.
func (c *Connection) headers() (version string, session string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.version, c.sessionID
}

// maxPeerHeaderBytes bounds a peer-controlled value Turing keeps and sends back
// in a header. Every other peer input on this path is already bounded — response
// bodies, tool descriptors, cursors, error text — and http.Transport would accept
// a response header of several megabytes, so without this a hostile or broken
// endpoint could make Turing retain and re-transmit one.
const maxPeerHeaderBytes = 512

// ConformingPeerHeaderValue reports whether a value the peer chose may be kept
// and sent back to it in a request header: visible ASCII, at most
// maxPeerHeaderBytes.
//
// The specification states this rule for session ids, and AdoptSession applies
// it there. Turing applies the same bound to every peer-chosen value it echoes,
// because the hazards are the same whatever the value means. A value net/http
// will not write does not corrupt a request — it fails the whole request, which
// for a best-effort teardown means the work it was meant to do silently does not
// happen. And nothing bounds what a peer may put in a response header but
// http.Transport's own megabyte ceiling.
//
// This is deliberately stricter than net/http, which refuses only control
// characters and DEL and accepts every byte above 0x7E. A revision or a session
// id is neither, so nothing legitimate is lost by refusing them here, and what
// is kept stays printable in a log or an error a person has to read.
func ConformingPeerHeaderValue(value string) bool {
	if len(value) > maxPeerHeaderBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7E {
			return false
		}
	}
	return true
}

// AdoptSession stores a peer-assigned session id, and reports whether it was
// usable.
//
// A session the transport would not permit is not adopted at all, rather than
// stored and echoed. The caller is told so it can fail with a reason: silently
// dropping it would send every later request without the header, and a stateful
// peer would answer a generic 400 that points nowhere near the cause.
func (c *Connection) AdoptSession(id string) bool {
	if !ConformingPeerHeaderValue(id) {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = id
	return true
}

// Invalidate drops every piece of negotiated state, so the next call re-runs
// the handshake from scratch. Nothing survives that a restarted, re-credentialed
// or disabled peer could be served from.
func (c *Connection) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initialized = false
	c.version = ""
	c.sessionID = ""
	c.generation++
}

// ErrConnectionInvalidated reports that the connection was invalidated while
// its handshake was still running, so that handshake's result was discarded.
//
// It is an error rather than a silent no-op because the caller is about to
// dispatch an operation-phase request: on a connection left uninitialized that
// request would carry no negotiated revision and no session, and would reach
// the peer before `notifications/initialized`. The caller must refuse; the next
// one runs a fresh handshake.
var ErrConnectionInvalidated = errors.New("MCP connection was invalidated during initialization")

// EnsureInitialized performs the handshake once: handshake sends `initialize`
// and returns the revision it negotiated, then announce sends the
// `initialized` notification.
//
// The negotiated revision is published before announce runs, because the
// notification itself has to carry it in a header — but the connection does not
// become Ready until announce has succeeded. That ordering is the point: a
// concurrent caller must not be able to slip an operation-phase request in
// between the two, which would let the peer see a tool call before the
// notification that ends initialization.
//
// A failure at either step leaves the connection uninitialized with no state
// behind it, so the next caller retries the whole handshake. An Invalidate that
// lands mid-handshake is also a failure, reported as ErrConnectionInvalidated:
// the result is discarded rather than published over the wipe, and this caller
// must not proceed on it. That holds for a wipe landing anywhere in the
// handshake, including while the notification is in flight — the peer's refusal
// of a notification whose headers had just been emptied says nothing useful. Nothing is downgraded and nothing is retried
// silently here; the caller decides what a failure means.
func (c *Connection) EnsureInitialized(
	ctx context.Context,
	handshake func() (string, error),
	announce func() error,
) error {
	if c.Ready() {
		return nil
	}
	// The gate is a channel rather than a mutex so a caller parked behind
	// somebody else's handshake can still be released by its own deadline. One
	// process-wide client is shared across concurrent runs, and the leader's
	// handshake is bounded only by the leader's context — so with a mutex, a run
	// the user just cancelled would sit here until a stranger's round trip
	// finished. The repository already joins an in-flight discovery this way one
	// layer up.
	gate := c.handshakeGate()
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if c.Ready() {
		return nil
	}

	// The generation is what makes the two-stage publish safe against an
	// Invalidate that lands mid-handshake. Invalidate only takes mu — it is
	// called from a concurrent request's error path, which must not block
	// behind a handshake's network round trips — so without this a wipe
	// arriving between the version publish and the ready publish would be
	// overwritten by `initialized = true`, leaving a connection that claims to
	// be ready while carrying no version and no session. Every later request
	// would then omit both headers, and the peer's resulting 404 would not even
	// look like a lost session, so nothing would ever re-initialize it.
	c.mu.Lock()
	generation := c.generation
	c.mu.Unlock()

	version, err := handshake()
	if err != nil {
		c.Invalidate()
		return err
	}
	if !c.publish(generation, func() { c.version = version }) {
		return c.discardStaleHandshake()
	}

	if err := announce(); err != nil {
		// An Invalidate can land after the version was published and before the
		// notification read its headers, so the peer saw a notification naming
		// no revision and no session and refused it — the repository's own
		// harness answers that shape 400, and the stock SDK server requires the
		// session id. Handing the caller the peer's refusal would report a
		// connection that merely needs re-initializing as a permanent failure:
		// a 400 is not retryable and a 404 on a request that carried no session
		// header is not even a lost session. The invalidation is named instead,
		// whether it landed before the notification or while it was in flight.
		if !c.current(generation) {
			return c.discardStaleHandshake()
		}
		c.Invalidate()
		return err
	}
	if !c.publish(generation, func() { c.initialized = true }) {
		return c.discardStaleHandshake()
	}
	return nil
}

// handshakeGate returns the one-slot channel that serializes handshakes,
// creating it on first use so the zero Connection needs no constructor.
func (c *Connection) handshakeGate() chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gate == nil {
		c.gate = make(chan struct{}, 1)
	}
	return c.gate
}

// discardStaleHandshake wipes whatever a handshake left behind after it was
// invalidated, and names the failure.
//
// Declining to publish is not enough on its own: AdoptSession is called from
// inside the handshake, as the initialize response is decoded, and it writes
// unguarded — so an invalidation landing *before* that point is followed by a
// session id being written after the wipe. Left there, the next handshake's own
// initialize POST would carry a stale Mcp-Session-Id on a connection with no
// negotiated revision, which is the half-published state the generation guard
// exists to prevent. The handshake gate is still held here — EnsureInitialized
// releases it only on return — so no other handshake can be running and this
// cannot wipe a newer one's state.
func (c *Connection) discardStaleHandshake() error {
	c.Invalidate()
	return ErrConnectionInvalidated
}

// current reports whether the connection has not been invalidated since
// generation was taken. publish answers the same question, but has to hold mu
// across the check and the change; this is for the points that only need to
// know.
func (c *Connection) current(generation uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation == generation
}

// publish applies one state change only if the connection has not been
// invalidated since generation was taken, and reports whether it did. A stale
// handshake leaves the connection uninitialized rather than half-published; its
// own caller is told so with ErrConnectionInvalidated, and the next caller runs
// a fresh handshake.
func (c *Connection) publish(generation uint64, apply func()) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != generation {
		return false
	}
	apply()
	return true
}

// CancelledMidRequest reports whether an in-flight request ended because its
// caller went away, and so should be announced to the peer with
// `notifications/cancelled`.
//
// It is shared rather than re-decided per call site because a request can end
// that way at two different points — the POST itself failing, and the response
// body failing partway through a stream the peer had already started — and
// covering only the first is a silent gap: a long-running tool call is exactly
// the case where the peer has already sent headers.
//
// `initialize` is excluded: the lifecycle specification forbids a client from
// cancelling it.
func CancelledMidRequest(ctx context.Context, method string) bool {
	return method != MethodInitialize && ctx != nil && ctx.Err() != nil
}

// SessionLost reports whether a response means the peer has terminated the
// session the request named. The transport gives HTTP 404 that specific
// meaning, but only for a request that actually carried a session id: a 404
// from a request without one is an ordinary not-found, not a lost session.
func SessionLost(response *http.Response) bool {
	return response != nil &&
		response.StatusCode == http.StatusNotFound &&
		response.Request != nil &&
		response.Request.Header.Get(SessionHeader) != ""
}
