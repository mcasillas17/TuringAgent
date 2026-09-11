package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc"
)

func decodeResult(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  map[string]any  `json:"result"`
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
	handler := testFilesHandler(t)
	status, body := callFilesMCP(t, handler, fmt.Sprintf(
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
	// Only what is implemented: no resources, prompts, logging, completions,
	// and no listChanged, because this server sends no notifications.
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

func TestInitializeAnswersAnUnsupportedRequestWithTheRevisionItSpeaks(t *testing.T) {
	// The lifecycle specification requires the server to answer with a version
	// it does support; a client that cannot speak it then disconnects. That is
	// the clear failure, and it is why nothing is silently downgraded here.
	handler := testFilesHandler(t)
	_, body := callFilesMCP(t, handler,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01","capabilities":{}}}`)
	result := decodeResult(t, body)
	if result["protocolVersion"] != supportedProtocolVersion {
		t.Fatalf("protocolVersion = %v, want the revision this server speaks (%q)", result["protocolVersion"], supportedProtocolVersion)
	}
}

func TestInitializeRefusesMalformedParams(t *testing.T) {
	handler := testFilesHandler(t)
	for name, params := range map[string]string{
		"missing version":     `{"capabilities":{}}`,
		"non-string version":  `{"protocolVersion":20251125}`,
		"unknown params key":  `{"protocolVersion":"2025-11-25","surprise":1}`,
		"non-object caps":     `{"protocolVersion":"2025-11-25","capabilities":[]}`,
		"non-object clientIn": `{"protocolVersion":"2025-11-25","clientInfo":"stock"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, body := callFilesMCP(t, handler,
				`{"jsonrpc":"2.0","id":1,"method":"initialize","params":`+params+`}`)
			assertRPCErrorCode(t, body, -32602)
		})
	}
}

func TestInitializedNotificationIsAcceptedWithoutABody(t *testing.T) {
	handler := testFilesHandler(t)
	status, body := callFilesMCP(t, handler, `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", status)
	}
	if len(body) != 0 {
		t.Fatalf("body = %s, want none", body)
	}
}

func TestPingAnswersAnEmptyResult(t *testing.T) {
	handler := testFilesHandler(t)
	status, body := callFilesMCP(t, handler, `{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	result := decodeResult(t, body)
	if len(result) != 0 {
		t.Fatalf("ping result = %#v, want an empty object", result)
	}
}

func TestPingRequiresTheBundledCredential(t *testing.T) {
	handler := testFilesHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: the lifecycle methods sit behind the same bearer as the tools", response.Code)
	}
}

func TestAnUnsupportedProtocolVersionHeaderIsRefused(t *testing.T) {
	handler := testFilesHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	request.Header.Set("Authorization", "Bearer files-token")
	request.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unsupported negotiated revision", response.Code)
	}
	if !strings.Contains(response.Body.String(), supportedProtocolVersion) {
		t.Fatalf("body = %s, want the supported revision named", response.Body.String())
	}
}

func TestASupportedProtocolVersionHeaderIsAccepted(t *testing.T) {
	handler := testFilesHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	request.Header.Set("Authorization", "Bearer files-token")
	request.Header.Set("Mcp-Protocol-Version", supportedProtocolVersion)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
}

func TestARequestCarryingAnOriginHeaderIsForbidden(t *testing.T) {
	// The transport requires servers to validate Origin against DNS rebinding.
	// These endpoints are only ever reached from inside the private Docker
	// network by the runtime, never by a browser, so any Origin at all is a
	// request this server should not be answering.
	handler := testFilesHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	request.Header.Set("Authorization", "Bearer files-token")
	request.Header.Set("Origin", "https://evil.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
}

func TestGetAndDeleteOnTheMCPEndpointAreRefused(t *testing.T) {
	// Both are optional in the transport: a server that offers neither an SSE
	// stream nor client-terminated sessions answers 405, and a stock client
	// carries on.
	handler := testFilesHandler(t)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		request := httptest.NewRequest(method, "/mcp", nil)
		request.Header.Set("Authorization", "Bearer files-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d, want 405", method, response.Code)
		}
	}
}

func TestNoSessionIsEverAssigned(t *testing.T) {
	// This server keeps no per-connection state, so it issues no session id:
	// there is nothing to bound, nothing to expire, and nothing a restart can
	// lose. A client re-initializes on its own connection and carries on.
	handler := testFilesHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q}}`, supportedProtocolVersion)))
	request.Header.Set("Authorization", "Bearer files-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if got := response.Header().Get("Mcp-Session-Id"); got != "" {
		t.Fatalf("Mcp-Session-Id = %q, want none", got)
	}
}

func TestUnknownMethodsStillFailAsProtocolErrors(t *testing.T) {
	handler := testFilesHandler(t)
	for _, method := range []string{"resources/list", "prompts/list", "sampling/createMessage", "server/discover", "logging/setLevel"} {
		_, body := callFilesMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"`+method+`"}`)
		assertRPCErrorCode(t, body, -32601)
	}
}

func TestReadOnlyToolsCarryAReadOnlyAnnotation(t *testing.T) {
	for _, tool := range listTools() {
		name, _ := tool["name"].(string)
		annotations, _ := tool["annotations"].(map[string]any)
		switch name {
		case "files.list", "files.search", "files.read":
			if annotations == nil || annotations["readOnlyHint"] != true {
				t.Errorf("%s annotations = %#v, want readOnlyHint", name, tool["annotations"])
			}
		case "files.create", "files.update":
			if annotations != nil && annotations["readOnlyHint"] == true {
				t.Errorf("%s claims to be read-only", name)
			}
		}
	}
}

func TestEveryFileToolDeclaresItselfClosedWorld(t *testing.T) {
	// openWorldHint defaults to true in the specification, so omitting it tells
	// a stock client that these tools may reach an open world of external
	// entities — the opposite of the sandbox they are confined to. mcp-system
	// already sets it; the file tools must agree, or the bundled servers
	// publish contradictory descriptions of the same guarantee.
	for _, tool := range listTools() {
		name, _ := tool["name"].(string)
		annotations, _ := tool["annotations"].(map[string]any)
		if annotations == nil {
			t.Errorf("%s carries no annotations", name)
			continue
		}
		if annotations["openWorldHint"] != false {
			t.Errorf("%s openWorldHint = %#v, want false: every file tool is sandbox-confined", name, annotations["openWorldHint"])
		}
	}
}

func TestFilesAcceptsAStockProgressTokenAndRefusesUnknownMetaKeys(t *testing.T) {
	// `_meta` is the protocol's own extensibility field, and progressToken is
	// the member a stock client attaches when it asks for progress. Refusing it
	// would fail a stock call on the very surface the acceptance criterion
	// covers. Accepting anything at all would drop the fail-closed property
	// that a capability this server does not expect is refused rather than
	// silently ignored. The orchestrator declines to issue a provenance
	// capability to any server but the file server, and relies on a refusal
	// here for the ones it never issues; that reasoning lives with it, in
	// orchestrator-go's internal/service/runtime, not in this module's own
	// provenance.go, which only verifies capabilities and issues none.
	//
	// This covers mcp-files only, and can cover nothing else: it lives in this
	// module, which has no import path to mcp-system. The two servers'
	// allowlists deliberately differ — mcp-system is issued no provenance
	// capability and refuses one — and its half is asserted by
	// TestSystemToolsStillWorkWhenTheirPolicyIsRaisedToApprovalRequired in
	// mcp-system/cmd/server/lifecycle_test.go.
	handler := testFilesHandler(t)

	_, accepted := callFilesMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.list","arguments":{},"_meta":{"progressToken":"p-1"}}}`)

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

	_, refused := callFilesMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.list","arguments":{},"_meta":{"unexpectedCapability":"x"}}}`)
	assertRPCErrorCode(t, refused, -32602)
}

// blockingOrchestrator holds every session-capability check open until its
// request context ends, which is the one place a bundled file call blocks long
// enough for a cancellation to matter.
type blockingOrchestrator struct {
	turingv1.UnimplementedApprovalServiceServer
	entered chan struct{}
	once    sync.Once
}

func (o *blockingOrchestrator) CheckSessionCapability(ctx context.Context, _ *turingv1.CheckSessionCapabilityRequest) (*turingv1.SessionCapabilityState, error) {
	o.once.Do(func() { close(o.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func startBlockingOrchestrator(t *testing.T) (addr string, entered chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	blocking := &blockingOrchestrator{entered: make(chan struct{})}
	server := grpc.NewServer()
	turingv1.RegisterApprovalServiceServer(server, blocking)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String(), blocking.entered
}

func postMCPAsync(handler http.Handler, body string) chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer files-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		done <- response
	}()
	return done
}

func TestCancellationStopsAMatchingInFlightRequest(t *testing.T) {
	addr, entered := startBlockingOrchestrator(t)
	handler := newHandler(serverConfig{
		filesToken:            "files-token",
		approvalJwtSecret:     "jwt-secret",
		approvalConsumerToken: "internal-token",
		orchestratorGRPCAddr:  addr,
		sandboxRoot:           t.TempDir(),
	})

	call := postMCPAsync(handler, provenanceCallBodyWithID(t, "slow-1", "files.list", map[string]any{}, ""))
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the tool call never reached the blocking orchestrator")
	}

	status, _ := callFilesMCP(t, handler,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"slow-1","reason":"user cancelled"}}`)
	if status != http.StatusAccepted {
		t.Fatalf("cancellation status = %d, want 202", status)
	}

	select {
	case response := <-call:
		// The call ends rather than hanging, and it ends as a failure. Asserting
		// only that it returned would leave a regression that answered a
		// cancelled mutation as a *completed* tool call looking correct — the
		// caller would be told work finished that was stopped.
		assertRPCErrorCode(t, response.Body.Bytes(), -32000)
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled call never returned")
	}
}

func TestCancellationFromAnotherIdentityDoesNotStopTheCall(t *testing.T) {
	// The in-flight registry is keyed by the authenticated caller, so a
	// cancellation only ever reaches a request that same caller issued.
	registry := newInflightRegistry(4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release, ok := registry.add("general_assistant", "req-1", cancel)
	if !ok {
		t.Fatal("add refused a first request")
	}
	defer release()

	if registry.cancel("someone_else", "req-1") {
		t.Fatal("a cancellation from another identity reached the request")
	}
	select {
	case <-ctx.Done():
		t.Fatal("the request was cancelled by another identity")
	default:
	}
	if !registry.cancel("general_assistant", "req-1") {
		t.Fatal("the owning identity could not cancel its own request")
	}
	<-ctx.Done()
}

func TestCancellationStopsARequestIdentifiedByANumericID(t *testing.T) {
	// The runtime numbers its requests, so a numeric id is the *only* shape a
	// real cancellation ever carries. JSON-RPC ids decode through UseNumber
	// while params decode without it, so the notification's requestId and the
	// registered request id arrive as different Go types for the same value;
	// nothing may fall through that gap.
	addr, entered := startBlockingOrchestrator(t)
	handler := newHandler(serverConfig{
		filesToken:            "files-token",
		approvalJwtSecret:     "jwt-secret",
		approvalConsumerToken: "internal-token",
		orchestratorGRPCAddr:  addr,
		sandboxRoot:           t.TempDir(),
	})

	call := postMCPAsync(handler, provenanceCallBodyWithID(t, 4242, "files.list", map[string]any{}, ""))
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the tool call never reached the blocking orchestrator")
	}

	status, _ := callFilesMCP(t, handler,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":4242,"reason":"user cancelled"}}`)
	if status != http.StatusAccepted {
		t.Fatalf("cancellation status = %d, want 202", status)
	}

	select {
	case response := <-call:
		// Same for the numeric id the runtime actually sends: stopped work is
		// reported as an error, never as a result.
		assertRPCErrorCode(t, response.Body.Bytes(), -32000)
	case <-time.After(5 * time.Second):
		t.Fatal("a numerically identified call was never cancelled; the cancellation path is inert for the ids the runtime actually sends")
	}
}

func TestCancellationOfAnUnknownRequestIsAcceptedAndIgnored(t *testing.T) {
	handler := testFilesHandler(t)
	status, body := callFilesMCP(t, handler,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"never-existed","reason":"gone"}}`)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: an unknown request id is ignored, not an error", status)
	}
	if len(body) != 0 {
		t.Fatalf("body = %s, want none", body)
	}
}

func TestCancellationRefusesMalformedParams(t *testing.T) {
	handler := testFilesHandler(t)
	// A notification gets no error response, but a malformed one must not be
	// mistaken for a valid cancellation either: it is accepted and dropped.
	for _, params := range []string{`{}`, `{"requestId":{"a":1}}`, `{"requestId":"x","surprise":1}`} {
		status, _ := callFilesMCP(t, handler,
			`{"jsonrpc":"2.0","method":"notifications/cancelled","params":`+params+`}`)
		if status != http.StatusAccepted {
			t.Fatalf("status = %d for params %s, want 202", status, params)
		}
	}
}

func TestAToolsListCursorIsRefusedRatherThanSilentlyIgnored(t *testing.T) {
	handler := testFilesHandler(t)
	_, body := callFilesMCP(t, handler,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"cursor":"anything"}}`)
	assertRPCErrorCode(t, body, -32602)

	// An empty cursor is the absent case a client may still spell out.
	status, _ := callFilesMCP(t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"cursor":""}}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d for an empty cursor, want 200", status)
	}
}

func TestOperationPhaseMethodsAreServedWithoutAPriorInitialize(t *testing.T) {
	// This server keeps no session and so cannot enforce handshake ordering.
	// That is deliberate and safe only because initialization confers no
	// authority — the bearer, the approval token and the provenance capability
	// are the gates. This pins the documented behaviour in both directions: it
	// answers without a handshake, and it still refuses an unauthenticated
	// caller.
	handler := testFilesHandler(t)
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	} {
		status, response := callFilesMCP(t, handler, body)
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

func TestANotificationShapedToolCallDoesNotExecuteTheTool(t *testing.T) {
	// A tools/call with no id parses as a JSON-RPC notification, which the
	// transport answers 202 with no body. Executing the tool anyway would run a
	// mutation, spend its one-time approval token and leave the caller unable
	// to observe either outcome — and the call would not even be cancellable,
	// since a notification has no id to name. It must be dropped, not served.
	//
	// The blocking orchestrator is the discriminator: every file tool asks it
	// about the session before touching the filesystem, so reaching it at all
	// proves the tool body ran.
	addr, entered := startBlockingOrchestrator(t)
	handler := newHandler(serverConfig{
		filesToken:            "files-token",
		approvalJwtSecret:     "jwt-secret",
		approvalConsumerToken: "internal-token",
		orchestratorGRPCAddr:  addr,
		sandboxRoot:           t.TempDir(),
	})

	body := provenanceCallBodyWithID(t, 1, "files.list", map[string]any{}, "")
	notification := strings.Replace(body, `"id":1,`, "", 1)
	if notification == body {
		t.Fatalf("could not build a notification-shaped call from %s", body)
	}

	done := postMCPAsync(handler, notification)

	// Reaching the orchestrator at all means the tool body ran.
	select {
	case <-entered:
		t.Fatal("a notification-shaped tools/call executed the tool")
	case response := <-done:
		if response.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", response.Code)
		}
		if response.Body.Len() != 0 {
			t.Fatalf("body = %s, want none", response.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the notification-shaped call neither ran nor returned")
	}
}

func TestOnlyLifecycleNotificationsAreAccepted(t *testing.T) {
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

func TestLifecycleNotificationsSentAsRequestsAreProtocolErrors(t *testing.T) {
	// `notifications/*` methods have no request form. Answering an id-bearing
	// one with a result is an unsupported protocol shape quietly succeeding —
	// and for the cancellation it would also do the work.
	handler := testFilesHandler(t)
	for _, method := range []string{"notifications/initialized", "notifications/cancelled"} {
		_, body := callFilesMCP(t, handler,
			`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":{"requestId":"x"}}`)
		assertRPCErrorCode(t, body, -32601)
	}
}

func TestAnIDBearingCancellationDoesNotStopAnInFlightCall(t *testing.T) {
	// The refusal above must be a refusal, not merely a different response
	// shape wrapped around the same side effect.
	addr, entered := startBlockingOrchestrator(t)
	handler := newHandler(serverConfig{
		filesToken:            "files-token",
		approvalJwtSecret:     "jwt-secret",
		approvalConsumerToken: "internal-token",
		orchestratorGRPCAddr:  addr,
		sandboxRoot:           t.TempDir(),
	})

	call := postMCPAsync(handler, provenanceCallBodyWithID(t, "slow-2", "files.list", map[string]any{}, ""))
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the tool call never reached the blocking orchestrator")
	}

	_, body := callFilesMCP(t, handler,
		`{"jsonrpc":"2.0","id":9,"method":"notifications/cancelled","params":{"requestId":"slow-2"}}`)
	assertRPCErrorCode(t, body, -32601)

	select {
	case <-call:
		t.Fatal("an id-bearing notifications/cancelled stopped the call it named")
	case <-time.After(250 * time.Millisecond):
	}
}

func TestAPostThatAcceptsNeitherMediaTypeIsRefused(t *testing.T) {
	// mcpwire documents that a conforming server refuses a POST which accepts
	// neither media type; this server has to be one. An absent Accept header is
	// left alone, and */* is honoured, so ordinary clients are unaffected.
	handler := testFilesHandler(t)

	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	request.Header.Set("Authorization", "Bearer files-token")
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
		request.Header.Set("Authorization", "Bearer files-token")
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
