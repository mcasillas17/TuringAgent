package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

const maxMCPResponseBytes int64 = 1024 * 1024
const maxMCPTools = 10_000
const maxMCPToolPages = 100
const maxMCPToolBytes = 4 * 1024 * 1024

// maxMCPImportDocumentBytes bounds an entire mcp.json document's raw size,
// checked before it is ever handed to json.Decoder — the same reasoning
// maxMCPResponseBytes already applies to a single live HTTP response, but
// for the whole file a reimport reads instead. Without it, an
// arbitrarily large document (most of it outside any single server's
// "tools" snapshot, so maxMCPToolBytes alone never bounds it) could force
// this process to buffer and decode an unbounded amount of memory. The
// cap is tied to, not independent of, the existing per-snapshot limit:
// mcp.json commonly registers only a handful of servers, and a document
// giving eight of them a full maxMCPToolBytes-sized static snapshot would
// still comfortably fit inside this bound, with slack left over for the
// URL/header/JSON-syntax overhead none of that per-tool accounting counts.
const maxMCPImportDocumentBytes = 8 * maxMCPToolBytes

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

// errMCPCursorRepeated is the one fixed, generic reason listTools refuses
// a peer's tools/list response whose nextCursor repeats one already seen
// earlier in the same pagination loop. The cursor's own value — whatever
// it is — must never be interpolated into this error: a peer chooses
// nextCursor freely, including a value equal to (or containing) this
// client's own configured bearer token, and %q-formatting that value
// escapes any quote or backslash it contains. That escaping breaks the
// token into a non-contiguous run of bytes, so redactMCPErrorValue's
// later plain strings.Contains/ReplaceAll redaction can no longer find
// (and so cannot remove) it — the escaped, still fully reconstructible
// remnants of the token would leak through instead, including into the
// liveness status message discoverLocked persists from this error
// verbatim. Kept fixed and cursor-free, this error can never carry that
// risk regardless of what any peer's cursor contains.
var errMCPCursorRepeated = errors.New("MCP tools/list returned a repeated nextCursor")

// errMCPCursorInvalid is errMCPCursorRepeated's sibling for a nextCursor
// whose JSON type is not a string, null, or absent: for the identical
// reason, the peer's actual value is never interpolated into this error.
var errMCPCursorInvalid = errors.New("MCP tools/list nextCursor must be a string, null, or absent")

type mcpClient struct {
	endpoint   string
	token      string
	httpClient *http.Client
	mu         sync.Mutex
	nextID     int64
}

type redactedMCPError struct {
	cause   error
	message string
}

func (e redactedMCPError) Error() string { return e.message }
func (e redactedMCPError) Unwrap() error { return e.cause }

func newMCPClient(endpoint string, token string, httpClient *http.Client) *mcpClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &mcpClient{
		endpoint:   endpoint,
		token:      token,
		httpClient: httpClient,
		nextID:     1,
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
	tools = make([]map[string]any, 0)
	params := map[string]any{}
	seenCursors := make(map[string]struct{})
	encodedBytes := 0
	for page := 0; page < maxMCPToolPages; page++ {
		// requestRaw, not request: discover() (this call's sole caller)
		// must see each tool exactly as the peer sent it, before
		// request's own marker-substitution redaction could replace a
		// bearer echo with the fixed "[redacted]" text and let the rest
		// of this loop — and, past it, RecordDiscovery — treat that
		// still-attacker-shaped tool as if it were clean. See
		// requestRaw's own doc comment for why callTool, unlike this
		// method, still goes through request instead.
		result, err := c.requestRaw(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		raw, ok := result["tools"].([]any)
		if !ok {
			return nil, errors.New("MCP tools/list result must contain a tools array")
		}
		if len(raw) > maxMCPTools-len(tools) {
			return nil, fmt.Errorf("MCP tools/list exceeds limit of %d tools", maxMCPTools)
		}
		for index, value := range raw {
			tool, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("MCP tools/list page %d tool %d must be an object", page+1, index)
			}
			encoded, err := json.Marshal(tool)
			if err != nil || len(encoded) > maxMCPToolBytes-encodedBytes {
				return nil, fmt.Errorf("MCP tools/list exceeds encoded descriptor limit of %d bytes", maxMCPToolBytes)
			}
			encodedBytes += len(encoded)
			tools = append(tools, tool)
		}
		cursorValue, present := result["nextCursor"]
		if !present || cursorValue == nil {
			return tools, nil
		}
		cursor, ok := cursorValue.(string)
		if !ok {
			return nil, errMCPCursorInvalid
		}
		if _, repeated := seenCursors[cursor]; repeated {
			return nil, errMCPCursorRepeated
		}
		seenCursors[cursor] = struct{}{}
		params = map[string]any{"cursor": cursor}
	}
	return nil, fmt.Errorf("MCP tools/list exceeded page limit of %d", maxMCPToolPages)
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
func (c *mcpClient) requestRaw(ctx context.Context, method string, params map[string]any) (result map[string]any, err error) {
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

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("content-type", "application/json")
	if c.token != "" {
		request.Header.Set("authorization", "Bearer "+c.token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("MCP HTTP %d", response.StatusCode)
	}
	limited := io.LimitReader(response.Body, maxMCPResponseBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxMCPResponseBytes {
		return nil, errors.New("MCP response too large")
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int64           `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int64  `json:"code"`
			Message string `json:"message"`
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
