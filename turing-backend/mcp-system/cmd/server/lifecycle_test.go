package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	systemtools "github.com/project-turing/mcp-system/internal/tools"
)

func decodeResult(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var envelope struct {
		JSONRPC string         `json:"jsonrpc"`
		Result  map[string]any `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	if envelope.Error != nil {
		t.Fatalf("response = %s, want a result", body)
	}
	if envelope.JSONRPC != "2.0" {
		t.Fatalf("jsonrpc = %q", envelope.JSONRPC)
	}
	return envelope.Result
}

func TestInitializeNegotiatesTheSupportedRevisionAndAdvertisesOnlyTools(t *testing.T) {
	handler := newHandler("system-token")
	status, body := callSystemMCP(t, handler, fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"stock","version":"1"}}}`,
		supportedProtocolVersion))
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	result := decodeResult(t, body)
	if result["protocolVersion"] != supportedProtocolVersion {
		t.Fatalf("protocolVersion = %v, want %q", result["protocolVersion"], supportedProtocolVersion)
	}
	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities = %#v", result["capabilities"])
	}
	if _, present := capabilities["tools"]; !present {
		t.Fatalf("capabilities = %#v, want a tools capability", capabilities)
	}
	for _, unimplemented := range []string{"resources", "prompts", "logging", "completions", "experimental", "tasks"} {
		if _, present := capabilities[unimplemented]; present {
			t.Errorf("capabilities advertise %q, which this server does not implement", unimplemented)
		}
	}
	tools, _ := capabilities["tools"].(map[string]any)
	if _, present := tools["listChanged"]; present {
		t.Error("tools.listChanged is advertised but no notification is ever sent")
	}
	info, ok := result["serverInfo"].(map[string]any)
	if !ok || info["name"] == "" || info["version"] == "" {
		t.Fatalf("serverInfo = %#v, want a named implementation", result["serverInfo"])
	}
}

func TestThisServerSpeaksThePinnedRevision(t *testing.T) {
	// Both operands are literals in this module, so this cannot see mcp-files or
	// mcpwire and must not claim to: it pins only that nobody edits this file's
	// constant by accident. The cross-module invariant — that mcpwire, mcp-files
	// and mcp-system name one revision — is enforced from the root module by
	// TestEveryModuleNamesTheSameMCPRevision in tools/docs/versions_test.go,
	// which is the only place that can read all three files.
	if supportedProtocolVersion != "2025-11-25" {
		t.Fatalf("supportedProtocolVersion = %q, want the pinned revision", supportedProtocolVersion)
	}
}

func TestInitializeAnswersAnUnsupportedRequestWithTheRevisionItSpeaks(t *testing.T) {
	handler := newHandler("system-token")
	_, body := callSystemMCP(t, handler,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01","capabilities":{}}}`)
	result := decodeResult(t, body)
	if result["protocolVersion"] != supportedProtocolVersion {
		t.Fatalf("protocolVersion = %v, want the revision this server speaks", result["protocolVersion"])
	}
}

func TestInitializeRefusesMalformedParams(t *testing.T) {
	handler := newHandler("system-token")
	for name, params := range map[string]string{
		"missing version":     `{"capabilities":{}}`,
		"non-string version":  `{"protocolVersion":20251125}`,
		"unknown params key":  `{"protocolVersion":"2025-11-25","surprise":1}`,
		"non-object caps":     `{"protocolVersion":"2025-11-25","capabilities":[]}`,
		"non-object clientIn": `{"protocolVersion":"2025-11-25","clientInfo":"stock"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, body := callSystemMCP(t, handler,
				`{"jsonrpc":"2.0","id":1,"method":"initialize","params":`+params+`}`)
			assertRPCErrorCode(t, body, -32602)
		})
	}
}

func TestInitializedNotificationIsAcceptedWithoutABody(t *testing.T) {
	handler := newHandler("system-token")
	status, body := callSystemMCP(t, handler, `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", status)
	}
	if len(body) != 0 {
		t.Fatalf("body = %s, want none", body)
	}
}

func TestPingAnswersAnEmptyResult(t *testing.T) {
	handler := newHandler("system-token")
	status, body := callSystemMCP(t, handler, `{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if result := decodeResult(t, body); len(result) != 0 {
		t.Fatalf("ping result = %#v, want an empty object", result)
	}
}

func TestPingRequiresTheBundledCredential(t *testing.T) {
	handler := newHandler("system-token")
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestCancellationIsAcceptedEvenThoughNothingCanBeStopped(t *testing.T) {
	// Every tool this server exposes computes its answer without blocking and
	// without a context, so there is nothing a cancellation could interrupt.
	// The specification lets a receiver ignore a cancellation it cannot act on;
	// what it must not do is fail, so the notification is accepted as one.
	handler := newHandler("system-token")
	status, body := callSystemMCP(t, handler,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1,"reason":"user cancelled"}}`)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", status)
	}
	if len(body) != 0 {
		t.Fatalf("body = %s, want none", body)
	}
}

func TestAnUnsupportedProtocolVersionHeaderIsRefused(t *testing.T) {
	handler := newHandler("system-token")
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	request.Header.Set("Authorization", "Bearer system-token")
	request.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if !strings.Contains(response.Body.String(), supportedProtocolVersion) {
		t.Fatalf("body = %s, want the supported revision named", response.Body.String())
	}
}

func TestASupportedProtocolVersionHeaderIsAccepted(t *testing.T) {
	handler := newHandler("system-token")
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	request.Header.Set("Authorization", "Bearer system-token")
	request.Header.Set("Mcp-Protocol-Version", supportedProtocolVersion)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
}

func TestARequestCarryingAnOriginHeaderIsForbidden(t *testing.T) {
	handler := newHandler("system-token")
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	request.Header.Set("Authorization", "Bearer system-token")
	request.Header.Set("Origin", "https://evil.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
}

func TestGetAndDeleteOnTheMCPEndpointAreRefused(t *testing.T) {
	handler := newHandler("system-token")
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		request := httptest.NewRequest(method, "/mcp", nil)
		request.Header.Set("Authorization", "Bearer system-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d, want 405", method, response.Code)
		}
	}
}

func TestNoSessionIsEverAssigned(t *testing.T) {
	handler := newHandler("system-token")
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q}}`, supportedProtocolVersion)))
	request.Header.Set("Authorization", "Bearer system-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if got := response.Header().Get("Mcp-Session-Id"); got != "" {
		t.Fatalf("Mcp-Session-Id = %q, want none", got)
	}
}

func TestUnknownMethodsStillFailAsProtocolErrors(t *testing.T) {
	handler := newHandler("system-token")
	for _, method := range []string{"resources/list", "prompts/list", "sampling/createMessage", "server/discover"} {
		_, body := callSystemMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"`+method+`"}`)
		assertRPCErrorCode(t, body, -32601)
	}
}

func TestEverySystemToolCarriesAnAnnotation(t *testing.T) {
	for _, tool := range systemtools.List() {
		name, _ := tool["name"].(string)
		annotations, ok := tool["annotations"].(map[string]any)
		if !ok {
			t.Errorf("%s has no annotations", name)
			continue
		}
		if _, present := annotations["readOnlyHint"]; !present {
			t.Errorf("%s annotations = %#v, want a readOnlyHint", name, annotations)
		}
	}
}

func TestAToolsListCursorIsRefusedRatherThanSilentlyIgnored(t *testing.T) {
	handler := newHandler("system-token")
	_, body := callSystemMCP(t, handler,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"cursor":"anything"}}`)
	assertRPCErrorCode(t, body, -32602)

	// An empty cursor is the absent case a client may still spell out.
	status, _ := callSystemMCP(t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"cursor":""}}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d for an empty cursor, want 200", status)
	}
}

func TestOperationPhaseMethodsAreServedWithoutAPriorInitialize(t *testing.T) {
	// This server keeps no session and so cannot enforce handshake ordering.
	// That is deliberate and safe only because initialization confers no
	// authority — the bearer is the gate. This pins the documented behaviour in
	// both directions: it answers without a handshake, and it still refuses an
	// unauthenticated caller.
	handler := newHandler("system-token")
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"system.time","arguments":{}}}`,
	} {
		status, response := callSystemMCP(t, handler, body)
		if status != http.StatusOK {
			t.Fatalf("status = %d for %s without a handshake, want 200", status, body)
		}
		if bytes.Contains(response, []byte(`"error"`)) {
			t.Fatalf("response = %s, want a result", response)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d for an unauthenticated pre-initialize call, want 401", response.Code)
	}
}

func TestOnlyLifecycleNotificationsAreAccepted(t *testing.T) {
	// A tools/call with no id parses as a notification. Serving it would run
	// the tool and answer a bare 202 the caller cannot read; only the two
	// lifecycle notifications may arrive without an id.
	for method, want := range map[string]bool{
		"notifications/initialized": true,
		"notifications/cancelled":   true,
		"tools/call":                false,
		"tools/list":                false,
		"ping":                      false,
		"initialize":                false,
		"resources/list":            false,
	} {
		if got := notificationAllowed(method); got != want {
			t.Errorf("notificationAllowed(%q) = %t, want %t", method, got, want)
		}
	}
}

func TestANotificationShapedToolCallIsDroppedNotServed(t *testing.T) {
	handler := newHandler("system-token")
	status, body := callSystemMCP(t, handler,
		`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"system.echo","arguments":{"text":"x"}}}`)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", status)
	}
	if len(body) != 0 {
		t.Fatalf("body = %s, want none", body)
	}
}

func TestLifecycleNotificationsSentAsRequestsAreProtocolErrors(t *testing.T) {
	// `notifications/*` methods have no request form. Answering an id-bearing
	// one with a result is an unsupported protocol shape quietly succeeding.
	handler := newHandler("system-token")
	for _, method := range []string{"notifications/initialized", "notifications/cancelled"} {
		_, body := callSystemMCP(t, handler,
			`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":{"requestId":1}}`)
		assertRPCErrorCode(t, body, -32601)
	}
}

func TestSystemAcceptsAStockProgressTokenAndRefusesUnknownMetaKeys(t *testing.T) {
	// The mcp-system half. This server previously accepted any `_meta` key, so
	// a capability it does not expect was ignored rather than refused. It
	// covers this server only — the mcp-files half is
	// TestFilesAcceptsAStockProgressTokenAndRefusesUnknownMetaKeys, in the other
	// module.
	handler := newHandler("system-token")

	_, accepted := callSystemMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"system.health","arguments":{},"_meta":{"progressToken":"p-1"}}}`)

	// The claim under test is narrow and deliberately so: progressToken must
	// not be rejected *as an unknown _meta key*. Whatever else the call needs
	// — a provenance capability, valid arguments — is a separate gate and is
	// asserted elsewhere.
	var envelope struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(accepted, &envelope); err != nil {
		t.Fatalf("decode %q: %v", accepted, err)
	}
	if envelope.Error != nil && envelope.Error.Message == "unknown _meta key" {
		t.Errorf("a stock client's progressToken was refused as an unknown _meta key: %s", accepted)
	}

	_, refused := callSystemMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"system.health","arguments":{},"_meta":{"unexpectedCapability":"x"}}}`)
	assertRPCErrorCode(t, refused, -32602)
}

func TestSystemToolsStillWorkWhenTheirPolicyIsRaisedToApprovalRequired(t *testing.T) {
	// A user may raise a system tool to approval_required — UpdateToolPolicyByName
	// blocks only *lowering* a bundled mutating tool to safe — and any newly
	// discovered tool defaults to approval_required. The runtime then forwards
	// the minted approval token to every non-caller-enforced client, this server
	// included, because runner.go hands CallTool the token for whatever client
	// the tool routes through.
	//
	// So refusing approvalToken here would take a supported configuration and
	// make it permanently unusable: the user is prompted, approves, the token is
	// minted, and the call is then rejected at the server. This server does not
	// verify approvals — the orchestrator's policy decision and consumption are
	// the gate — so the token is accepted and ignored.
	handler := newHandler("system-token")

	_, response := callSystemMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"system.health","arguments":{},"_meta":{"approvalToken":"minted-after-the-user-approved"}}}`)
	var envelope struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatalf("decode %q: %v", response, err)
	}
	if envelope.Error != nil {
		t.Fatalf("an approved system tool call was refused: %s", response)
	}

	// A provenance capability is still refused: this server writes nothing into
	// the sandbox and is issued none, which is exactly what provenance.go relies
	// on when it declines to send one here.
	_, provenance := callSystemMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"system.health","arguments":{},"_meta":{"provenanceToken":"not-for-this-server"}}}`)
	assertRPCErrorCode(t, provenance, -32602)
}

func TestAPostThatAcceptsNeitherMediaTypeIsRefused(t *testing.T) {
	// mcpwire documents that a conforming server refuses a POST which accepts
	// neither media type; this server has to be one. An absent Accept header is
	// left alone, and */* is honoured, so ordinary clients are unaffected.
	handler := newHandler("system-token")

	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	request.Header.Set("Authorization", "Bearer system-token")
	request.Header.Set("Accept", "text/plain")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d for an unsatisfiable Accept, want 400", response.Code)
	}

	// Media types are case-insensitive, so a client spelling them differently is
	// still naming types this server answers with.
	for _, accept := range []string{
		"", "application/json", "text/event-stream", "application/json, text/event-stream", "*/*",
		"Application/JSON, Text/Event-Stream", "APPLICATION/JSON", "Text/Event-Stream",
	} {
		request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		request.Header.Set("Authorization", "Bearer system-token")
		if accept != "" {
			request.Header.Set("Accept", accept)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code == http.StatusBadRequest {
			t.Errorf("Accept %q was refused; it names a type this server answers with", accept)
		}
	}
}
