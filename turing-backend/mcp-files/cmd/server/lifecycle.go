package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/mcpwire"
	"github.com/project-turing/mcp-files/internal/jsonrpc"
)

// supportedProtocolVersion is the one MCP revision this server implements.
// Turing advertises exactly one, and claiming a range it has not exercised
// would be the kind of unearned conformance claim CON-001 exists to remove.
//
// It is the shared constant, not a copy: this module already depends on the
// root module, so the compiler keeps the server and both client paths on one
// revision string. mcp-system keeps its own copy for the same reason
// internal/jsonrpc and internal/auth are duplicated there: its *shipped binary*
// imports only the standard library, a property two tests in that module pin.
// The module itself does carry one pinned test-only dependency.
const supportedProtocolVersion = mcpwire.ProtocolVersion

const (
	protocolVersionHeader = mcpwire.ProtocolVersionHeader
	serverImplementation  = "turing-mcp-files"
	serverVersion         = "1.0.0"

	// maxInflightCancellableRequests bounds the cancellation bookkeeping. It
	// is a ceiling on concurrently *cancellable* work, not on work: past it a
	// request is still served, it simply cannot be stopped by id.
	maxInflightCancellableRequests = 1024
)

// checkTransportHeaders applies the three Streamable HTTP header rules this
// server enforces, and reports whether the request may proceed.
//
// Origin: the transport requires servers to validate it against DNS rebinding.
// This endpoint is only ever reached from inside the private Docker network by
// the agent runtime and never by a browser, so any Origin at all names a
// request this server should not be answering.
//
// Accept: the transport obliges a client to list both media types a Streamable
// HTTP endpoint may answer with. What this server refuses is narrower — only a
// POST naming media types it cannot answer with at all — because refusing an
// absent header, or one naming just one of the two, would break callers that
// work today for no protocol benefit. The comparison folds case, media types
// being case-insensitive.
//
// Mcp-Protocol-Version: a header naming a revision this server does not
// implement must be refused with 400, so an unsupported combination fails
// loudly instead of being served under assumed semantics. An *absent* header is
// accepted, which is what the reference implementation does too: it keeps a
// client that has not yet been upgraded to send the header working, so
// deploying this server does not force a coordinated upgrade of its callers.
func checkTransportHeaders(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	// The transport obliges a client to list both media types. This server does
	// not enforce that obligation in full: it refuses only a POST that names
	// media types it cannot answer with at all. An absent header is accepted,
	// and so is one naming just one of the two — tightening either would break
	// callers that work today, for no protocol benefit.
	//
	// Media types are case-insensitive, so the comparison folds case rather than
	// refusing a conforming client that spells them differently.
	if accept := strings.ToLower(r.Header.Get("Accept")); accept != "" &&
		!strings.Contains(accept, "application/json") &&
		!strings.Contains(accept, "text/event-stream") &&
		!strings.Contains(accept, "*/*") {
		http.Error(w, "Accept must allow application/json or text/event-stream", http.StatusBadRequest)
		return false
	}
	if version := r.Header.Get(protocolVersionHeader); version != "" && version != supportedProtocolVersion {
		http.Error(w, "unsupported MCP protocol version; this server implements "+supportedProtocolVersion,
			http.StatusBadRequest)
		return false
	}
	return true
}

// initializeResult answers the `initialize` request.
//
// Version negotiation follows the lifecycle specification: a version this
// server supports is echoed back, and anything else is answered with the
// version it does support. A client that cannot speak that then disconnects,
// which is the clear failure — nothing is silently downgraded, and nothing
// about initialization grants any authority.
//
// The advertised capabilities are only what is implemented: tools, with no
// `listChanged` because this server sends no notifications at all, and no
// resources, prompts, logging, completions or experimental blocks.
func initializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": supportedProtocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo": map[string]any{
			"name":    serverImplementation,
			"version": serverVersion,
		},
	}
}

// validateInitializeParams checks the handshake's own arguments. A malformed
// handshake is a protocol error, not something to paper over with defaults.
func validateInitializeParams(req jsonrpc.Request) *jsonrpc.RequestError {
	if paramsErr := rejectUnknownParams(req, "protocolVersion", "capabilities", "clientInfo", "_meta"); paramsErr != nil {
		return paramsErr
	}
	version, present := req.Params["protocolVersion"]
	if !present {
		return jsonrpc.InvalidParams(req.ID, "protocolVersion is required")
	}
	if _, isString := version.(string); !isString {
		return jsonrpc.InvalidParams(req.ID, "protocolVersion must be a string")
	}
	for _, key := range []string{"capabilities", "clientInfo", "_meta"} {
		value, present := req.Params[key]
		if !present {
			continue
		}
		if object, isObject := value.(map[string]any); !isObject || object == nil {
			return jsonrpc.InvalidParams(req.ID, key+" must be an object")
		}
	}
	return nil
}

// cancelledRequestID reads the request id a `notifications/cancelled` names.
// A malformed notification is dropped rather than answered: the transport
// answers every notification with 202, and the specification says an invalid
// cancellation should simply be ignored.
func cancelledRequestID(req jsonrpc.Request) (any, bool) {
	if paramsErr := rejectUnknownParams(req, "requestId", "reason", "_meta"); paramsErr != nil {
		return nil, false
	}
	id, present := req.Params["requestId"]
	if !present {
		return nil, false
	}
	// Whether the id is one the registry can key is inflightRegistry.cancel's
	// question, and it already refuses the ones it cannot. Re-deriving a key
	// here under an agent id that is never real would couple this params check
	// to the registry's key encoding for no observable difference: an unusable
	// requestId is dropped either way, and the notification is answered 202
	// either way.
	return id, true
}

// readOnlyAnnotations is the standard hint block for a tool that only reads.
//
// It is advisory metadata for a client's own display and nothing more. Turing's
// own policy for these tools comes from the `policy` field the orchestrator
// reads, never from an annotation — and the reverse is enforced in the other
// direction too: a third-party server's annotations are dropped during
// discovery, so no peer can soften a policy by claiming to be read-only.
func readOnlyAnnotations() map[string]any {
	return map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
}

// mutatingAnnotations is the hint block for a tool that writes. Like every
// annotation it is display metadata: a mutating file tool is gated by the
// argument-bound approval token and its preview preconditions, and no hint here
// relaxes any of that.
func mutatingAnnotations(destructive bool) map[string]any {
	return map[string]any{"readOnlyHint": false, "destructiveHint": destructive, "idempotentHint": false, "openWorldHint": false}
}

// notificationAllowed reports whether a method may legitimately arrive without
// a JSON-RPC id.
//
// Only the two lifecycle notifications may. Anything else without an id — most
// dangerously a `tools/call` — would otherwise be executed and then answered
// with a bare 202 the caller cannot read: a mutation would run, its one-time
// approval token would be spent, and neither the result nor the failure would
// reach anyone. Such a request is also uncancellable, since a notification has
// no id for `notifications/cancelled` to name. It is dropped instead.
func notificationAllowed(method string) bool {
	return method == "notifications/initialized" || method == "notifications/cancelled"
}

// callToolResult shapes a tool's own return value as the CallToolResult the
// protocol defines, so a stock client can actually read the answer.
//
// A bare JSON-RPC result carrying the tool's own keys decodes into an EMPTY
// CallToolResult in a conforming client: `content` and `structuredContent` are
// the only fields it looks at. The tool's map therefore moves into
// `structuredContent`, and the specification's backwards-compatibility advice
// — a tool returning structured content SHOULD also return the serialized JSON
// in a text block — supplies `content`.
//
// The duplication is real and is measured, not waved away: both
// representations of the same payload travel together, and the serialized copy
// is escaped a second time, so the worst case — a 4 KiB path plus 64 KiB of
// content, every byte of which escapes to six — is 905,282 bytes against the
// 1 MiB limit writeJSONRPCStatus enforces, where the bare map would have been
// 417,806. This module's own response-budget test builds its envelope from
// callToolResult and additionally asserts that both copies are still there, so
// dropping either one, or measuring the bare map again, fails that test rather
// than silently narrowing the margin at runtime.
//
// Turing's own runtime is unaffected: its client unwraps `structuredContent`
// back to this same map, so every existing caller sees exactly what it saw
// before.
func callToolResult(result map[string]any) map[string]any {
	encoded, err := json.Marshal(result)
	if err != nil {
		// Unreachable for the maps these tools build, and a tool result that
		// cannot be encoded has nothing to send either way.
		encoded = []byte("{}")
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": string(encoded)}},
		"structuredContent": result,
		"isError":           false,
	}
}
