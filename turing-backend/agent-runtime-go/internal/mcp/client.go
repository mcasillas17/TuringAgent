package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	egress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/safejson"
	"github.com/mcasillas17/TuringAgent/turing-backend/mcpwire"
)

const defaultMaxResponseBytes int64 = 1024 * 1024

type Client struct {
	endpoint         string
	token            string
	httpClient       *http.Client
	maxResponseBytes int64
	mu               sync.Mutex
	nextID           int64
	// connection is the lifecycle state — handshake completion, negotiated
	// revision, assigned session — shared with the orchestrator's registry
	// client so the ordering and session-loss rules exist in one place.
	connection mcpwire.Connection
}

// cancellationNotifyTimeout bounds the best-effort notifications/cancelled a
// cancelled call sends. It runs on a context detached from the caller's own,
// which is already cancelled by then, and is never allowed to keep the caller
// waiting for long.
const cancellationNotifyTimeout = 2 * time.Second

// clientImplementationVersion is this client's own version, sent as
// clientInfo.version in the handshake. It is Turing's implementation version,
// not the protocol revision.
const clientImplementationVersion = "1.0.0"

// idempotentMCPMethods may be replayed once after a lost session. tools/call
// is deliberately absent: a call whose delivery is ambiguous may already have
// executed and committed on the server, and repeating it automatically would
// risk performing the mutation twice while looking like recovery.
var idempotentMCPMethods = map[string]struct{}{
	"tools/list":       {},
	mcpwire.MethodPing: {},
}

type RetryableError interface {
	error
	Retryable() bool
}

type classifiedError struct {
	err       error
	retryable bool
}

func (e classifiedError) Error() string   { return e.err.Error() }
func (e classifiedError) Unwrap() error   { return e.err }
func (e classifiedError) Retryable() bool { return e.retryable }

type JSONRPCError struct {
	Code    int64
	Message string
}

func (e JSONRPCError) Error() string {
	return fmt.Sprintf("MCP error %d: %s", e.Code, e.Message)
}

type ToolCallError struct {
	Result map[string]any
}

func (e ToolCallError) Error() string {
	message := "MCP tool call failed"
	content, ok := e.Result["content"].([]any)
	if !ok {
		return message
	}
	for _, item := range content {
		block, ok := item.(map[string]any)
		if !ok || block["type"] != "text" {
			continue
		}
		text, ok := block["text"].(string)
		if ok && strings.TrimSpace(text) != "" {
			return message + ": " + text
		}
	}
	return message
}

func Retryable(err error) bool {
	var classified RetryableError
	return errors.As(err, &classified) && classified.Retryable()
}

// NewClient builds a client for one endpoint. httpClient is required and a nil
// one is refused rather than defaulted: egress.NoRedirectClient would otherwise
// substitute http.DefaultClient, which has no timeouts, and the call would
// succeed with nothing saying so. The registry client refuses it for the same
// reason.
func NewClient(endpoint string, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		panic("mcp: NewClient requires an http.Client")
	}
	// The redirect policy is applied here rather than at each construction site
	// so that no caller can build an MCP client that follows a peer-chosen
	// redirect. The bundled clients are built with http.DefaultClient, whose
	// CheckRedirect is nil — Go's follow-up-to-ten default — and a followed
	// redirect would replay the approval-bearing tools/call body to a URL the
	// peer picked. Copying the client leaves the caller's own value untouched.
	return &Client{
		endpoint:         endpoint,
		token:            token,
		httpClient:       egress.NoRedirectClient(httpClient),
		maxResponseBytes: defaultMaxResponseBytes,
		nextID:           mcpwire.StartingRequestID(),
	}
}

func (c *Client) ListTools(ctx context.Context) (tools []map[string]any, err error) {
	defer func() {
		err = classifyListToolsError(err)
	}()

	return mcpwire.CollectTools(func(params map[string]any) (map[string]any, error) {
		return c.request(ctx, "tools/list", params)
	})
}

// CallTool sends one MCP tools/call. tokens are the server-issued capabilities
// for this call, in order: the approval token (empty for a safe tool), then the
// provenance capability (empty for a server that is issued none). They are
// forwarded verbatim under _meta; the runtime never mints or edits one, and
// omitting _meta entirely when there is nothing to send keeps servers that
// reject unknown _meta keys working unchanged.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any, tokens ...string) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	params := map[string]any{"name": name, "arguments": args}
	meta := map[string]any{}
	if len(tokens) > 0 && tokens[0] != "" {
		meta["approvalToken"] = tokens[0]
	}
	if len(tokens) > 1 && tokens[1] != "" {
		meta["provenanceToken"] = tokens[1]
	}
	if len(meta) > 0 {
		params["_meta"] = meta
	}
	result, err := c.request(ctx, "tools/call", params)
	if err != nil {
		return nil, err
	}
	if isError, ok := result["isError"].(bool); ok && isError {
		return nil, nonRetryableError(ToolCallError{Result: result})
	}
	return unwrapCallToolResult(result), nil
}

// unwrapCallToolResult reduces a conforming CallToolResult to the tool's own
// data.
//
// The result carries the payload twice: in `structuredContent`, and serialized
// into a sibling `content` text block for clients that read only unstructured
// content. Callers here want the structured form, and unwrapping it keeps every
// existing caller seeing exactly the map it saw before the bundled servers
// started answering in the protocol's own shape. A peer that sends no
// structuredContent is passed through untouched.
//
// Both client paths call this, and that is the point: a bundled tool and a
// third-party tool are the same kind of call to the model, so they must not
// disagree about the shape of the same protocol object. Handing one path the
// envelope would also spend the run's bounded tool-call summary on a duplicated,
// re-escaped copy of data the other path delivers once.
func unwrapCallToolResult(result map[string]any) map[string]any {
	// Non-empty only: a peer whose tool declares an output schema may send an
	// empty structured object beside a populated content block, and this helper
	// governs third-party peers as well as the bundled servers, so replacing the
	// result with {} would erase an answer the peer actually gave.
	if structured, ok := result["structuredContent"].(map[string]any); ok && len(structured) > 0 {
		return structured
	}
	return result
}

// Ping performs the MCP liveness round trip. A conforming peer answers with an
// empty result; anything else is a failure the caller can classify as usual.
//
// It exists for the protocol surface CON-001 scopes, and its callers today are
// the conformance fixtures, which use it to show this client can ping a stock
// server. Nothing in production calls it: bundled-server liveness comes from the
// containers' own healthchecks, not from here. Said plainly so the method is not
// mistaken for a liveness mechanism the product depends on.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.request(ctx, mcpwire.MethodPing, map[string]any{})
	return err
}

// request runs one operation-phase call: it makes sure the connection has been
// initialized, then performs the round trip, recovering from a lost session
// exactly once and only for a method that is safe to replay.
func (c *Client) request(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	if err := c.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	result, err := c.call(ctx, method, params)
	if err == nil || !errors.Is(err, mcpwire.ErrSessionLost) {
		return result, err
	}
	if _, replayable := idempotentMCPMethods[method]; !replayable {
		return nil, err
	}
	if err := c.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	return c.call(ctx, method, params)
}

// ensureInitialized performs the `initialize` request and the `initialized`
// notification once per connection.
//
// The ordering and the locking live in mcpwire.Connection, which both MCP
// clients share: the connection only becomes usable once the notification has
// been delivered, so a concurrent caller cannot slip a tool call in between.
// A failure leaves it uninitialized and the next call retries the whole
// handshake; nothing is cached that a restarted or re-credentialed peer could
// be served from. Failures include a revision this client does not implement and
// a peer that advertises no tools capability, both refused by NegotiatedVersion.
func (c *Client) ensureInitialized(ctx context.Context) error {
	// A handshake discarded because it raced a concurrent Invalidate is
	// transient: nothing was published, and the next caller runs a fresh one.
	// Classified at the source rather than at one consumer, so tools/list and
	// tools/call agree — unclassified it falls through to nonRetryableError and
	// fails the whole run's tool discovery permanently.
	err := c.connection.EnsureInitialized(
		ctx,
		func() (string, error) {
			result, err := c.call(ctx, mcpwire.MethodInitialize,
				mcpwire.InitializeParams("turing-agent-runtime", clientImplementationVersion))
			if err != nil {
				return "", err
			}
			version, err := mcpwire.NegotiatedVersion(result)
			if err != nil {
				return "", nonRetryableError(err)
			}
			return version, nil
		},
		func() error { return c.notify(ctx, mcpwire.NotificationInitialized, map[string]any{}) },
	)
	if errors.Is(err, mcpwire.ErrConnectionInvalidated) {
		return retryableError(err)
	}
	return err
}

// notify sends a JSON-RPC notification. A conforming server answers 202 with
// no body, so there is nothing to decode and no id to match.
func (c *Client) notify(ctx context.Context, method string, params map[string]any) error {
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return nonRetryableError(err)
	}
	resp, err := c.send(ctx, payload)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.statusError(resp)
	}
	return nil
}

// send performs the HTTP POST that carries one JSON-RPC message, with the
// headers the Streamable HTTP transport requires: both accepted media types,
// the bearer credential, and — once negotiated — the protocol version and the
// server-assigned session.
func (c *Client) send(ctx context.Context, payload []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, nonRetryableError(err)
	}
	mcpwire.SetRequestHeaders(req, c.token, &c.connection)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := directContextError(ctx, err); ctxErr != nil {
			return nil, ctxErr
		}
		// A refused redirect is a deliberate security decision, not a transient
		// transport fault: the peer will redirect again on every attempt, so
		// reporting it retryable would tell a user to wait for something that
		// cannot change. Every other Do failure is genuinely worth retrying.
		var blocked *egress.RedirectBlockedError
		if errors.As(err, &blocked) {
			return nil, nonRetryableError(err)
		}
		return nil, retryableError(err)
	}
	return resp, nil
}

// statusError classifies a non-2xx response, and recognizes the one status the
// transport gives a specific meaning: 404 for a request carrying a session id
// means the server has terminated that session.
func (c *Client) statusError(resp *http.Response) error {
	if mcpwire.SessionLost(resp) {
		c.connection.Invalidate()
		return classifiedError{err: mcpwire.ErrSessionLost}
	}
	err := fmt.Errorf("MCP HTTP %d", resp.StatusCode)
	retryable := resp.StatusCode == http.StatusRequestTimeout ||
		resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode >= http.StatusInternalServerError && resp.StatusCode < 600)
	return classifiedError{err: err, retryable: retryable}
}

// call performs one JSON-RPC request/response round trip without any lifecycle
// handling of its own, so ensureInitialized can use it for the handshake
// itself without recursing.
func (c *Client) call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	id := c.nextRequestID()
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, nonRetryableError(err)
	}
	resp, err := c.send(ctx, payload)
	if err != nil {
		if mcpwire.CancelledMidRequest(ctx, method) {
			go c.notifyCancelled(id, "the caller's context ended")
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.statusError(resp)
	}
	if resp.StatusCode == http.StatusAccepted {
		return nil, nonRetryableError(errors.New("MCP request was accepted without a response"))
	}
	message, err := mcpwire.ReadResponseMessage(resp.Header.Get("Content-Type"), resp.Body, c.maxResponseBytes)
	if err != nil {
		if ctxErr := directContextError(ctx, err); ctxErr != nil {
			// A peer that has already sent headers and is streaming its answer
			// fails here rather than at the POST.
			if mcpwire.CancelledMidRequest(ctx, method) {
				go c.notifyCancelled(id, "the caller's context ended")
			}
			return nil, ctxErr
		}
		return nil, nonRetryableError(err)
	}
	obj, err := decodeLimitedObject(bytes.NewReader(message), c.maxResponseBytes)
	if err != nil {
		return nil, err
	}
	if session := resp.Header.Get(mcpwire.SessionHeader); session != "" && method == mcpwire.MethodInitialize {
		if !c.connection.AdoptSession(session) {
			return nil, nonRetryableError(mcpwire.ErrUnusableSessionID)
		}
	}
	if version, ok := obj["jsonrpc"].(string); !ok || version != "2.0" {
		return nil, nonRetryableError(errors.New("MCP response jsonrpc must be \"2.0\""))
	}
	responseID, ok := obj["id"].(json.Number)
	if !ok {
		return nil, nonRetryableError(errors.New("MCP response id must be a request ID"))
	}
	responseIDValue, err := responseID.Int64()
	if err != nil || responseIDValue != id {
		return nil, nonRetryableError(fmt.Errorf("MCP response id does not match request ID %d", id))
	}
	result, hasResult := obj["result"]
	rawErr, hasError := obj["error"]
	if hasResult && hasError {
		return nil, nonRetryableError(errors.New("MCP response must not contain both result and error"))
	}
	if hasError {
		errorObj, ok := rawErr.(map[string]any)
		if !ok {
			return nil, nonRetryableError(errors.New("MCP error"))
		}
		codeNumber, ok := errorObj["code"].(json.Number)
		if !ok {
			return nil, nonRetryableError(errors.New("MCP error code must be an integer"))
		}
		code, err := codeNumber.Int64()
		if err != nil {
			return nil, nonRetryableError(errors.New("MCP error code must be an integer"))
		}
		message, ok := errorObj["message"].(string)
		if !ok || message == "" {
			return nil, nonRetryableError(errors.New("MCP error"))
		}
		rpcErr := JSONRPCError{Code: code, Message: message}
		retryable := code == -32603 || (code >= -32099 && code <= -32000)
		return nil, classifiedError{err: rpcErr, retryable: retryable}
	}
	if !hasResult {
		return nil, nonRetryableError(errors.New("MCP response must contain result or error"))
	}
	resultObj, ok := result.(map[string]any)
	if !ok {
		return nil, nonRetryableError(errors.New("MCP response result must be an object"))
	}
	return resultObj, nil
}

// notifyCancelled tells the peer that an in-flight request has been abandoned.
//
// It is best effort, and call sites start it in its own goroutine. The caller's
// own context is already cancelled and a failure to deliver changes nothing it
// sees — but running it inline would hold the tool call open for the full
// timeout while the peer decides how slowly to answer, delaying the runner from
// recording the cancelled call's outcome and continuing the run. This client has
// no session to release, so nothing has to be ordered after it.
//
// Cancellation asks a server to stop working and free resources — it never
// asserts that a mutation the server already committed has been undone.
func (c *Client) notifyCancelled(requestID int64, reason string) {
	if !c.connection.Ready() {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), cancellationNotifyTimeout)
	defer cancel()
	_ = c.notify(ctx, mcpwire.NotificationCancelled, map[string]any{
		"requestId": requestID,
		"reason":    reason,
	})
}

func (c *Client) nextRequestID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

func decodeLimitedObject(reader io.Reader, maxBytes int64) (map[string]any, error) {
	limited := io.LimitReader(reader, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, retryableError(err)
	}
	if int64(len(data)) > maxBytes {
		return nil, nonRetryableError(errors.New("MCP response too large"))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	obj, err := safejson.DecodeObject(decoder)
	if err != nil {
		return nil, nonRetryableError(err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = errors.New("MCP response contains multiple JSON values")
		}
		return nil, nonRetryableError(fmt.Errorf("MCP response contains trailing data: %w", err))
	}
	return obj, nil
}

func classifyListToolsError(err error) error {
	if err == nil {
		return nil
	}
	// Background, deliberately: this runs deferred, where the caller's context
	// is out of scope, and the question here is only what the *error* is, not
	// whether the caller has since gone away. directContextError checks the
	// context first and then falls back to errors.Is, so a Background context
	// selects exactly that fallback. Every call site that does have the
	// caller's context passes it.
	if direct := directContextError(context.Background(), err); direct != nil {
		return direct
	}
	var classified RetryableError
	if errors.As(err, &classified) {
		return err
	}
	return nonRetryableError(err)
}

func directContextError(ctx context.Context, err error) error {
	if ctx != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func retryableError(err error) error {
	return classifiedError{err: err, retryable: true}
}

func nonRetryableError(err error) error {
	return classifiedError{err: err}
}
