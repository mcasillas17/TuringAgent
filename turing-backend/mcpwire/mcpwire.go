// Package mcpwire holds the Model Context Protocol wire details that both of
// Turing's MCP client paths — the agent runtime's client for the bundled
// servers, and the orchestrator's registry client for configured third-party
// endpoints — have to get identically right.
//
// It deliberately contains no policy, no credentials, and no redaction: each
// client keeps its own error classification and secret handling, because those
// differ. What is shared here is only what the protocol itself fixes.
package mcpwire

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"mime"
	"net/http"
	"strings"
)

const (
	// ProtocolVersion is the one MCP revision Turing implements.
	//
	// It is the latest *handshake-based* revision. The current revision at the
	// time of writing, 2026-07-28, removes `initialize`/`initialized` entirely
	// in favour of a stateless per-request core; 2025-11-25 is what the
	// specification's own versioning page calls a "legacy" revision, and it is
	// the one whose lifecycle Turing supports. A stock dual-era client probes
	// for the modern era first, receives a non-modern error, and falls back to
	// this revision, so pinning it here does not cut Turing off from current
	// clients.
	ProtocolVersion = "2025-11-25"

	// AcceptHeader is what every Streamable HTTP POST must send. The obligation
	// is the client's: the transport requires both media types on every POST,
	// and a peer may answer 400 to one that omits them. Stated as the client
	// rule rather than as server behaviour, because Turing's own bundled servers
	// deliberately enforce less — they refuse only a POST naming media types
	// they cannot answer with at all.
	AcceptHeader = "application/json, text/event-stream"

	// ProtocolVersionHeader carries the negotiated revision on every request
	// after initialization. SessionHeader carries a server-assigned session,
	// when the peer chose to use one; it is an addressing token, never a
	// credential.
	ProtocolVersionHeader = "Mcp-Protocol-Version"
	SessionHeader         = "Mcp-Session-Id"

	// MethodInitialize and friends are the lifecycle methods Turing speaks.
	MethodInitialize        = "initialize"
	MethodPing              = "ping"
	NotificationInitialized = "notifications/initialized"
	NotificationCancelled   = "notifications/cancelled"

	jsonMediaType = "application/json"
	sseMediaType  = "text/event-stream"
)

// ErrUnsupportedProtocolVersion reports that a peer answered `initialize` with
// a revision Turing does not implement. Per the lifecycle specification the
// server MUST answer with some version it supports, and a client that cannot
// speak it SHOULD disconnect: this is that disconnect, made explicit rather
// than degraded into a silent downgrade.
var ErrUnsupportedProtocolVersion = errors.New("unsupported MCP protocol version")

// ErrSessionLost reports the condition SessionLost detects: the peer no longer
// recognizes the session a request carried. It lives here, beside the check that
// produces it, so both client paths cannot drift on what it means.
var ErrSessionLost = errors.New("MCP session is no longer recognized by the server")

// ErrToolsNotAdvertised reports a peer whose initialize result declares no
// `tools` capability.
//
// Driving tools/list and tools/call at such a peer is Turing violating the
// handshake it just completed, and the failure that follows arrives as an opaque
// peer error recorded as that server's liveness status. Refusing names the real
// reason instead. CON-001 scopes capability negotiation, not only version
// negotiation.
var ErrToolsNotAdvertised = errors.New("MCP server advertised no tools capability")

// ErrCursorRepeated reports a peer that returned a nextCursor it had already
// returned, which would page forever.
//
// The cursor itself is never interpolated into either of these: a peer chooses
// it freely and at any length, and these errors are persisted as a server's
// liveness status and shown to an operator. Both live here so one client cannot
// reword them, or start echoing the peer's value, without the other.
var ErrCursorRepeated = errors.New("MCP tools/list returned a repeated nextCursor")

// ErrCursorInvalid reports a nextCursor that is neither a string, null, nor
// absent.
var ErrCursorInvalid = errors.New("MCP tools/list nextCursor must be a string, null, or absent")

// ErrCursorTooLong reports a nextCursor longer than this client will keep and
// send back. Nothing else bounds one: a cursor is limited only by the enclosing
// response cap, and a peer may serve a distinct one on every page, so without
// this a peer could make Turing retain and re-transmit a response cap's worth of
// opaque token per page, per discovery, while serving no tools at all.
var ErrCursorTooLong = errors.New("MCP tools/list nextCursor exceeds the length this client will echo")

// ErrUnusableSessionID reports a peer that assigned a session id the transport
// does not permit — too long, or outside visible ASCII. It lives beside
// AdoptSession, which is what refuses one.
var ErrUnusableSessionID = errors.New("MCP server assigned an unusable session id")

// SetRequestHeaders applies the headers every MCP POST carries: both accepted
// media types, the bearer if there is one, and — once negotiated — the protocol
// version and the peer-assigned session.
//
// Shared because a header the two client paths disagree about is exactly the
// drift this package exists to prevent; what genuinely differs between them —
// redaction, retry classification, cancellation bookkeeping — stays with each
// client.
func SetRequestHeaders(req *http.Request, token string, connection *Connection) {
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", AcceptHeader)
	if token != "" {
		req.Header.Set("authorization", "Bearer "+token)
	}
	version, session := connection.headers()
	if version != "" {
		req.Header.Set(ProtocolVersionHeader, version)
	}
	if session != "" {
		req.Header.Set(SessionHeader, session)
	}
}

// InitializeParams builds the `initialize` params for a client that implements
// no client-side feature at all: no roots, no sampling, no elicitation. The
// capabilities object is present and empty, which is how the protocol spells
// "I offer nothing", and no `_meta` is attached — initialization must never
// carry an approval or provenance capability.
func InitializeParams(clientName string, clientVersion string) map[string]any {
	return map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": clientName, "version": clientVersion},
	}
}

// NegotiatedVersion accepts or refuses an `initialize` result, and returns the
// revision it negotiated.
//
// It refuses on two counts, not one: a revision Turing does not implement, and a
// peer that declares no `tools` capability — Turing drives only `tools/list` and
// `tools/call`, so a server that has just said it serves none has nothing this
// client can ask it for. Both are conditions of accepting the handshake, which
// is why they live together; callers that enumerate their failure modes name
// both.
func NegotiatedVersion(result map[string]any) (string, error) {
	raw, present := result["protocolVersion"]
	if !present {
		return "", fmt.Errorf("%w: the initialize result declared none, and Turing speaks %s", ErrUnsupportedProtocolVersion, ProtocolVersion)
	}
	version, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%w: the initialize result protocolVersion must be a string, and Turing speaks %s", ErrUnsupportedProtocolVersion, ProtocolVersion)
	}
	// Turing implements exactly one revision; claiming a range it has not
	// exercised would be the kind of unearned conformance claim CON-001 exists
	// to remove.
	if version != ProtocolVersion {
		// The peer's own version string is never interpolated: a peer chooses
		// it freely and at any length, and this error is persisted as a
		// server's liveness status and surfaced to an operator.
		return "", fmt.Errorf("%w: the server answered with a revision Turing does not implement, and Turing speaks %s", ErrUnsupportedProtocolVersion, ProtocolVersion)
	}
	if !advertisesTools(result) {
		// The peer's own capability object is never interpolated, for the same
		// reason its version string is not.
		return "", ErrToolsNotAdvertised
	}
	return version, nil
}

// advertisesTools reports whether the initialize result declares the one
// capability Turing uses. A peer that declares tools with no sub-options still
// declares them, so presence is what counts, not contents.
func advertisesTools(result map[string]any) bool {
	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		return false
	}
	_, advertised := capabilities["tools"]
	return advertised
}

// ReadResponseMessage extracts one JSON-RPC message from a Streamable HTTP
// response body. The transport lets a server answer a request either with a
// single `application/json` object or by opening a `text/event-stream`, and
// the specification requires a client to support both.
//
// For an event stream it stops at the first event carrying a JSON-RPC
// *response* — an object with an `id` — skipping the priming event and any
// notifications or requests the server sends first. Stopping there, rather
// than draining to EOF, is what keeps a peer that never terminates its stream
// from holding the call open past its own answer; the caller closes the body,
// which aborts the connection.
//
// maxBytes bounds the whole body either way.
func ReadResponseMessage(contentType string, body io.Reader, maxBytes int64) ([]byte, error) {
	mediaType := contentType
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil {
		mediaType = parsed
	}
	limited := io.LimitReader(body, maxBytes+1)
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case jsonMediaType:
		data, err := io.ReadAll(limited)
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > maxBytes {
			return nil, errors.New("MCP response too large")
		}
		return data, nil
	case sseMediaType:
		return readSSEResponse(limited, maxBytes)
	default:
		return nil, errors.New("MCP response content type must be application/json or text/event-stream")
	}
}

// readSSEResponse parses the Server-Sent Events framing far enough to find the
// response: `data:` lines accumulate into one payload, a blank line dispatches
// the event, and everything else (`event:`, `id:`, `retry:`, comments) is
// framing this client does not need. Only the `data` payload is JSON.
func readSSEResponse(body io.Reader, maxBytes int64) ([]byte, error) {
	reader := bufio.NewReader(body)
	var data []byte
	read := int64(0)
	dispatch := func() ([]byte, bool) {
		defer func() { data = nil }()
		if len(data) == 0 {
			return nil, false
		}
		var probe struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			return nil, false
		}
		// A response has an id and no method. A notification has no id; a
		// server-initiated request has both — and carries an id, so testing for
		// an id alone would hand the caller the peer's own request as if it
		// were the answer.
		if len(probe.ID) == 0 || probe.Method != "" {
			return nil, false
		}
		return data, true
	}
	for {
		line, err := reader.ReadString('\n')
		read += int64(len(line))
		if read > maxBytes {
			return nil, errors.New("MCP response too large")
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" && len(line) > 0 {
			if message, ok := dispatch(); ok {
				return message, nil
			}
		} else if payload, isData := strings.CutPrefix(trimmed, "data:"); isData {
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, strings.TrimPrefix(payload, " ")...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if message, ok := dispatch(); ok {
					return message, nil
				}
				return nil, errors.New("MCP event stream ended without a response")
			}
			return nil, err
		}
	}
}

// Tool-collection bounds. A peer chooses how many pages it serves, how many
// tools each page carries, how large each descriptor is, and how long the cursor
// that fetches the next page is, so discovery is bounded on all four before any
// of it is stored, echoed back, or shown to a model.
//
// MaxCursorBytes is generous for an opaque pagination token — real ones are tens
// of bytes — and exists because a cursor is the one value here Turing keeps and
// sends back to the peer, which is the same reason session ids and revisions are
// bounded in connection.go.
const (
	MaxToolPages   = 100
	MaxTools       = 10_000
	MaxToolBytes   = 4 * 1024 * 1024
	MaxCursorBytes = 4 * 1024
)

// CollectTools walks a peer's paginated tools/list and returns every tool it
// served, within the bounds above.
//
// fetch performs one page's request with the params it is given: the clients
// differ in how they do that — one redacts its errors, the other deliberately
// does not, so discovery scans exactly what the peer sent — but the bounds, the
// cursor rules and the shapes they refuse are one rule, kept here for the reason
// this package exists. Two copies drifted apart is the failure mode; the caps
// are the part most worth not duplicating.
func CollectTools(fetch func(params map[string]any) (map[string]any, error)) ([]map[string]any, error) {
	tools := make([]map[string]any, 0)
	params := map[string]any{}
	seenCursors := make(map[string]struct{})
	encodedBytes := 0
	for page := 0; page < MaxToolPages; page++ {
		result, err := fetch(params)
		if err != nil {
			return nil, err
		}
		values, ok := result["tools"].([]any)
		if !ok {
			return nil, fmt.Errorf("MCP tools/list page %d must contain a tools array", page+1)
		}
		if len(values) > MaxTools-len(tools) {
			return nil, fmt.Errorf("MCP tools/list page %d exceeds limit of %d tools", page+1, MaxTools)
		}
		for index, value := range values {
			tool, isObject := value.(map[string]any)
			if !isObject {
				return nil, fmt.Errorf("MCP tools/list page %d tool %d must be an object", page+1, index)
			}
			encoded, err := json.Marshal(tool)
			if err != nil {
				return nil, fmt.Errorf("MCP tools/list page %d tool %d cannot be encoded: %w", page+1, index, err)
			}
			if len(encoded) > MaxToolBytes-encodedBytes {
				return nil, fmt.Errorf(
					"MCP tools/list page %d tool %d exceeds encoded descriptor limit of %d bytes",
					page+1, index, MaxToolBytes)
			}
			encodedBytes += len(encoded)
			tools = append(tools, tool)
		}
		cursorValue, present := result["nextCursor"]
		if !present || cursorValue == nil {
			return tools, nil
		}
		cursor, isString := cursorValue.(string)
		if !isString {
			return nil, ErrCursorInvalid
		}
		if len(cursor) > MaxCursorBytes {
			return nil, ErrCursorTooLong
		}
		if _, repeated := seenCursors[cursor]; repeated {
			return nil, ErrCursorRepeated
		}
		seenCursors[cursor] = struct{}{}
		params = map[string]any{"cursor": cursor}
	}
	return nil, fmt.Errorf("MCP tools/list exceeded page limit of %d", MaxToolPages)
}

// MaxStartingRequestID bounds where a client begins numbering its requests.
//
// A server keys an in-flight request by the caller's identity plus the id the
// client chose, and an identity can cover more than one client: Turing's bundled
// servers authenticate one token per agent kind rather than per process, and a
// sessionless third-party peer has nothing but its bearer to tell two callers
// apart. Numbering from 1 everywhere therefore makes two clients issue the same
// ids, and a notifications/cancelled naming one stops the other's call — which
// for a mutating tool means aborting work whose one-time approval was already
// spent.
//
// The bound keeps every id a client can go on to issue inside the safe-integer
// range those registries accept, with room for far more requests than any one
// client will make.
const MaxStartingRequestID = 1 << 32

// StartingRequestID returns the id a new client should number from: a random
// point rather than a constant, so two clients sharing one identity do not
// issue the ids that would let either cancel the other's request.
func StartingRequestID() int64 {
	return rand.Int64N(MaxStartingRequestID) + 1
}
