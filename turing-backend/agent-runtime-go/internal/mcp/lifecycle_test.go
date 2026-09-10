package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/mcpwire"
)

// recordedRequest is one POST a lifecycle fixture server saw, kept with the
// headers that carry the negotiated session so ordering can be asserted.
type recordedRequest struct {
	Method          string
	ID              json.RawMessage
	Params          map[string]any
	ProtocolVersion string
	SessionID       string
	Accept          string
	Authorization   string
}

type lifecycleServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
	// handle answers one decoded request. Returning an empty body means "202
	// Accepted with no body", which is what a conforming server sends for a
	// notification.
	handle func(index int, request recordedRequest) (status int, contentType string, body string)
}

func newLifecycleServer(t *testing.T, handle func(int, recordedRequest) (int, string, string)) *lifecycleServer {
	t.Helper()
	server := &lifecycleServer{handle: handle}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		recorded := recordedRequest{
			Method:          envelope.Method,
			ID:              envelope.ID,
			Params:          envelope.Params,
			ProtocolVersion: r.Header.Get(mcpwire.ProtocolVersionHeader),
			SessionID:       r.Header.Get(mcpwire.SessionHeader),
			Accept:          r.Header.Get("Accept"),
			Authorization:   r.Header.Get("Authorization"),
		}
		server.mu.Lock()
		index := len(server.requests)
		server.requests = append(server.requests, recorded)
		server.mu.Unlock()

		status, contentType, body := server.handle(index, recorded)
		if body == "" {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("content-type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *lifecycleServer) recorded() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedRequest(nil), s.requests...)
}

// answerLifecycle replies to the handshake methods and delegates everything
// else, so a test only writes the part it is about.
func answerLifecycle(rest func(int, recordedRequest) (int, string, string)) func(int, recordedRequest) (int, string, string) {
	return func(index int, request recordedRequest) (int, string, string) {
		switch request.Method {
		case mcpwire.MethodInitialize:
			body := fmt.Sprintf(
				`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}},"serverInfo":{"name":"fixture","version":"1"}}}`,
				request.ID, mcpwire.ProtocolVersion,
			)
			return http.StatusOK, "application/json", body
		case mcpwire.NotificationInitialized:
			return http.StatusAccepted, "", ""
		case mcpwire.NotificationCancelled:
			return http.StatusAccepted, "", ""
		default:
			return rest(index, request)
		}
	}
}

func emptyToolsResult(request recordedRequest) (int, string, string) {
	return http.StatusOK, "application/json",
		fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}`, request.ID)
}

func TestClientInitializesBeforeTheFirstToolsListAndThenAnnouncesTheNegotiatedVersion(t *testing.T) {
	server := newLifecycleServer(t, answerLifecycle(func(_ int, request recordedRequest) (int, string, string) {
		return emptyToolsResult(request)
	}))
	client := NewClient(server.URL, "secret-token", server.Client())

	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	requests := server.recorded()
	if len(requests) != 3 {
		t.Fatalf("requests = %d (%v), want initialize, notifications/initialized, tools/list", len(requests), methodsOf(requests))
	}
	wantMethods := []string{mcpwire.MethodInitialize, mcpwire.NotificationInitialized, "tools/list"}
	for index, want := range wantMethods {
		if requests[index].Method != want {
			t.Fatalf("request %d method = %q, want %q", index, requests[index].Method, want)
		}
		if !strings.Contains(requests[index].Accept, "application/json") ||
			!strings.Contains(requests[index].Accept, "text/event-stream") {
			t.Errorf("request %d Accept = %q, want both Streamable HTTP media types", index, requests[index].Accept)
		}
		if requests[index].Authorization != "Bearer secret-token" {
			t.Errorf("request %d Authorization = %q", index, requests[index].Authorization)
		}
	}
	if requests[0].ProtocolVersion != "" {
		t.Errorf("initialize carried %s = %q; the version is not negotiated yet", mcpwire.ProtocolVersionHeader, requests[0].ProtocolVersion)
	}
	for _, index := range []int{1, 2} {
		if requests[index].ProtocolVersion != mcpwire.ProtocolVersion {
			t.Errorf("request %d %s = %q, want %q", index, mcpwire.ProtocolVersionHeader, requests[index].ProtocolVersion, mcpwire.ProtocolVersion)
		}
	}
	if len(requests[0].ID) == 0 || string(requests[0].ID) == "null" {
		t.Error("initialize must be a request with an id, not a notification")
	}
	if len(requests[1].ID) != 0 {
		t.Errorf("notifications/initialized carried id %s; a notification has none", requests[1].ID)
	}
}

func TestInitializeAdvertisesNoClientCapabilityAndNoApprovalMetadata(t *testing.T) {
	server := newLifecycleServer(t, answerLifecycle(func(_ int, request recordedRequest) (int, string, string) {
		return emptyToolsResult(request)
	}))
	client := NewClient(server.URL, "", server.Client())
	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	params := server.recorded()[0].Params
	if params["protocolVersion"] != mcpwire.ProtocolVersion {
		t.Fatalf("initialize protocolVersion = %v", params["protocolVersion"])
	}
	capabilities, ok := params["capabilities"].(map[string]any)
	if !ok || len(capabilities) != 0 {
		t.Fatalf("initialize capabilities = %#v, want an empty object", params["capabilities"])
	}
	if _, present := params["_meta"]; present {
		t.Fatal("initialize must never carry _meta; it can consume no approval and grant no capability")
	}
}

func TestConcurrentCallsShareOneInitialization(t *testing.T) {
	var initializes int
	var mu sync.Mutex
	var announcing atomic.Int32
	var overtook atomic.Bool
	ready := make(chan struct{}, 1)
	server := newLifecycleServer(t, func(index int, request recordedRequest) (int, string, string) {
		if request.Method == mcpwire.MethodInitialize {
			mu.Lock()
			initializes++
			mu.Unlock()
			// Hold the handshake open briefly so every caller piles up on it.
			time.Sleep(20 * time.Millisecond)
		}
		switch request.Method {
		case mcpwire.NotificationInitialized:
			// Hold the notification open and let a *late* caller in. The eight
			// callers below all queue on the handshake lock before the
			// handshake starts, so none of them can exercise the fast path; a
			// caller arriving in this window is the one that would overtake the
			// notification if the connection became usable too early.
			announcing.Add(1)
			ready <- struct{}{}
			time.Sleep(50 * time.Millisecond)
			announcing.Add(-1)
		case mcpwire.MethodInitialize:
		default:
			// Arrival order alone proves nothing here: the notification is
			// recorded the moment it arrives, so an operation-phase request
			// dispatched while it is still in flight still lands after it in
			// the log. Overlap is the observable violation.
			if announcing.Load() != 0 {
				overtook.Store(true)
			}
		}
		return answerLifecycle(func(_ int, request recordedRequest) (int, string, string) {
			return emptyToolsResult(request)
		})(index, request)
	})
	client := NewClient(server.URL, "", server.Client())

	var wait sync.WaitGroup
	errs := make(chan error, 9)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := client.ListTools(context.Background()); err != nil {
				errs <- err
			}
		}()
	}
	// The late caller: it starts once the notification is on the wire, so it
	// takes the fast path rather than queueing behind the handshake.
	wait.Add(1)
	go func() {
		defer wait.Done()
		select {
		case <-ready:
		case <-time.After(testChannelTimeout):
			errs <- errors.New("the handshake never reached notifications/initialized")
			return
		}
		if _, err := client.ListTools(context.Background()); err != nil {
			errs <- err
		}
	}()
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent ListTools: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if initializes != 1 {
		t.Fatalf("initialize sent %d times, want exactly 1 across concurrent callers", initializes)
	}

	// Nothing may overtake the handshake: no operation-phase message may reach
	// the peer while notifications/initialized is still in flight.
	if overtook.Load() {
		t.Fatal("an operation-phase request reached the server before notifications/initialized was delivered")
	}
	announced := false
	for _, request := range server.recorded() {
		if request.Method == mcpwire.NotificationInitialized {
			announced = true
		}
	}
	if !announced {
		t.Fatal("the handshake never sent notifications/initialized")
	}
}

func TestInitializationIsRetriedAfterItFails(t *testing.T) {
	var attempts int
	server := newLifecycleServer(t, func(index int, request recordedRequest) (int, string, string) {
		if request.Method == mcpwire.MethodInitialize {
			attempts++
			if attempts == 1 {
				return http.StatusServiceUnavailable, "application/json", `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"warming up"}}`
			}
		}
		return answerLifecycle(func(_ int, request recordedRequest) (int, string, string) {
			return emptyToolsResult(request)
		})(index, request)
	})
	client := NewClient(server.URL, "", server.Client())

	if _, err := client.ListTools(context.Background()); err == nil {
		t.Fatal("first ListTools should surface the failed handshake")
	}
	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("second ListTools should retry initialization: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("initialize attempts = %d, want 2", attempts)
	}
}

func TestAnUnsupportedNegotiatedVersionFailsClearlyAndIsNotRetryable(t *testing.T) {
	server := newLifecycleServer(t, func(_ int, request recordedRequest) (int, string, string) {
		return http.StatusOK, "application/json", fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"1999-01-01","capabilities":{}}}`, request.ID)
	})
	client := NewClient(server.URL, "", server.Client())

	_, err := client.ListTools(context.Background())
	if err == nil {
		t.Fatal("want a refusal when the server negotiates a revision Turing does not implement")
	}
	if !errors.Is(err, mcpwire.ErrUnsupportedProtocolVersion) {
		t.Fatalf("error = %v, want ErrUnsupportedProtocolVersion", err)
	}
	if Retryable(err) {
		t.Fatal("a version mismatch is not a transient failure")
	}
	if !strings.Contains(err.Error(), mcpwire.ProtocolVersion) {
		t.Fatalf("error %q should name the revision Turing speaks", err)
	}
	if requests := server.recorded(); len(requests) != 1 {
		t.Fatalf("requests = %v, want the client to stop after the failed handshake", methodsOf(requests))
	}
}

func TestNegotiatedSessionIsAnnouncedOnEveryLaterRequest(t *testing.T) {
	server := newSessionServer(t, "session-abc", func(request recordedRequest) (int, string, string) {
		return emptyToolsResult(request)
	})
	client := NewClient(server.URL, "", server.Client())
	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	requests := server.recorded()
	if requests[0].SessionID != "" {
		t.Errorf("initialize carried a session id before one existed: %q", requests[0].SessionID)
	}
	for _, index := range []int{1, 2} {
		if requests[index].SessionID != "session-abc" {
			t.Errorf("request %d (%s) session = %q, want session-abc", index, requests[index].Method, requests[index].SessionID)
		}
	}
}

func TestSessionLossReinitializesAndRetriesOnlyDiscovery(t *testing.T) {
	var expired bool
	server := newSessionServer(t, "session-1", func(request recordedRequest) (int, string, string) {
		if request.Method == "tools/list" && !expired {
			expired = true
			return http.StatusNotFound, "text/plain", "session not found"
		}
		return emptyToolsResult(request)
	})
	client := NewClient(server.URL, "", server.Client())

	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools should recover from a lost session: %v", err)
	}
	methods := methodsOf(server.recorded())
	want := []string{
		mcpwire.MethodInitialize, mcpwire.NotificationInitialized, "tools/list",
		mcpwire.MethodInitialize, mcpwire.NotificationInitialized, "tools/list",
	}
	if strings.Join(methods, ",") != strings.Join(want, ",") {
		t.Fatalf("methods = %v, want %v", methods, want)
	}
}

func TestSessionLossNeverRedispatchesAToolCall(t *testing.T) {
	server := newSessionServer(t, "session-1", func(request recordedRequest) (int, string, string) {
		if request.Method == "tools/call" {
			return http.StatusNotFound, "text/plain", "session not found"
		}
		return emptyToolsResult(request)
	})
	client := NewClient(server.URL, "", server.Client())

	if _, err := client.CallTool(context.Background(), "files.create", map[string]any{"path": "x"}); err == nil {
		t.Fatal("want the lost-session failure to surface rather than a silent re-dispatch")
	}
	calls := 0
	for _, request := range server.recorded() {
		if request.Method == "tools/call" {
			calls++
		}
	}
	if calls != 1 {
		t.Fatalf("tools/call dispatched %d times; a mutation whose delivery is ambiguous must never be repeated automatically", calls)
	}
}

func TestResponsesArriveOverAnEventStream(t *testing.T) {
	server := newLifecycleServer(t, func(_ int, request recordedRequest) (int, string, string) {
		switch request.Method {
		case mcpwire.MethodInitialize:
			return http.StatusOK, "text/event-stream", fmt.Sprintf(
				"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"protocolVersion\":%q,\"capabilities\":{\"tools\":{}}}}\n\n",
				request.ID, mcpwire.ProtocolVersion)
		case mcpwire.NotificationInitialized:
			return http.StatusAccepted, "", ""
		default:
			return http.StatusOK, "text/event-stream", fmt.Sprintf(
				"id: 1\ndata: \n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"tools\":[{\"name\":\"only\"}]}}\n\n",
				request.ID)
		}
	})
	client := NewClient(server.URL, "", server.Client())

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools over SSE: %v", err)
	}
	if len(tools) != 1 || tools[0]["name"] != "only" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestPingAnswersAnEmptyResult(t *testing.T) {
	server := newLifecycleServer(t, answerLifecycle(func(_ int, request recordedRequest) (int, string, string) {
		if request.Method != mcpwire.MethodPing {
			return http.StatusOK, "application/json", fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}`, request.ID)
		}
		return http.StatusOK, "application/json", fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{}}`, request.ID)
	}))
	client := NewClient(server.URL, "", server.Client())

	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if last := server.recorded(); last[len(last)-1].Method != mcpwire.MethodPing {
		t.Fatalf("last method = %q, want ping", last[len(last)-1].Method)
	}
}

func TestCancellingACallNotifiesTheServerWithTheSameRequestID(t *testing.T) {
	cancelled := make(chan recordedRequest, 1)
	blocked := make(chan struct{})
	server := newLifecycleServer(t, func(index int, request recordedRequest) (int, string, string) {
		switch request.Method {
		case "tools/call":
			<-blocked
			return http.StatusOK, "application/json", fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{}}`, request.ID)
		case mcpwire.NotificationCancelled:
			select {
			case cancelled <- request:
			default:
			}
			return http.StatusAccepted, "", ""
		default:
			return answerLifecycle(func(_ int, request recordedRequest) (int, string, string) {
				return emptyToolsResult(request)
			})(index, request)
		}
	})
	client := NewClient(server.URL, "", server.Client())
	// Initialize up front so the cancelled request is the tool call itself.
	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.CallTool(ctx, "files.list", nil)
		done <- err
	}()
	// Wait until the call is actually in flight before cancelling.
	waitFor(t, func() bool {
		for _, request := range server.recorded() {
			if request.Method == "tools/call" {
				return true
			}
		}
		return false
	}, "the tool call to reach the server")
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("CallTool error = %v, want context.Canceled", err)
	}
	close(blocked)

	var notification recordedRequest
	select {
	case notification = <-cancelled:
	case <-time.After(testChannelTimeout):
		t.Fatal("timed out waiting for notifications/cancelled")
	}
	if len(notification.ID) != 0 {
		t.Errorf("notifications/cancelled carried id %s; it is a notification", notification.ID)
	}
	var toolCallID json.RawMessage
	for _, request := range server.recorded() {
		if request.Method == "tools/call" {
			toolCallID = request.ID
		}
	}
	// The id makes the round trip as a JSON number, which is the trip the
	// bundled server's safe-integer bound exists for: compare the value, not
	// fmt's rendering of a float64, which switches to exponent form well inside
	// the range an id may occupy.
	cancelledID, isNumber := notification.Params["requestId"].(float64)
	if !isNumber {
		t.Fatalf("cancelled requestId = %#v, want a JSON number", notification.Params["requestId"])
	}
	if got, want := strconv.FormatFloat(cancelledID, 'f', -1, 64), strings.Trim(string(toolCallID), `"`); got != want {
		t.Fatalf("cancelled requestId = %s, want the id of the tool call (%s)", got, want)
	}
	if notification.ProtocolVersion != mcpwire.ProtocolVersion {
		t.Errorf("cancellation must ride the same negotiated connection, got %s = %q", mcpwire.ProtocolVersionHeader, notification.ProtocolVersion)
	}
}

func TestCancellingInitializationNeverSendsACancellation(t *testing.T) {
	// The lifecycle specification forbids clients from cancelling `initialize`.
	release := make(chan struct{})
	server := newLifecycleServer(t, func(index int, request recordedRequest) (int, string, string) {
		if request.Method == mcpwire.MethodInitialize {
			<-release
		}
		return answerLifecycle(func(_ int, request recordedRequest) (int, string, string) {
			return emptyToolsResult(request)
		})(index, request)
	})
	client := NewClient(server.URL, "", server.Client())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = client.ListTools(ctx)
	}()
	waitFor(t, func() bool { return len(server.recorded()) > 0 }, "initialize to reach the server")
	cancel()
	<-done
	close(release)

	for _, request := range server.recorded() {
		if request.Method == mcpwire.NotificationCancelled {
			t.Fatal("initialize must never be cancelled by a client")
		}
	}
}

// newSessionServer answers the handshake with a server-assigned session id and
// then requires it on every later request, the way a stateful peer does.
func newSessionServer(t *testing.T, sessionID string, rest func(recordedRequest) (int, string, string)) *lifecycleServer {
	t.Helper()
	server := &lifecycleServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		recorded := recordedRequest{
			Method:          envelope.Method,
			ID:              envelope.ID,
			Params:          envelope.Params,
			ProtocolVersion: r.Header.Get(mcpwire.ProtocolVersionHeader),
			SessionID:       r.Header.Get(mcpwire.SessionHeader),
			Accept:          r.Header.Get("Accept"),
		}
		server.mu.Lock()
		server.requests = append(server.requests, recorded)
		server.mu.Unlock()

		var status int
		var contentType, body string
		switch envelope.Method {
		case mcpwire.MethodInitialize:
			w.Header().Set(mcpwire.SessionHeader, sessionID)
			status, contentType, body = http.StatusOK, "application/json", fmt.Sprintf(
				`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}}}}`,
				envelope.ID, mcpwire.ProtocolVersion)
		case mcpwire.NotificationInitialized, mcpwire.NotificationCancelled:
			status, contentType, body = http.StatusAccepted, "", ""
		default:
			status, contentType, body = rest(recorded)
		}
		if body == "" {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("content-type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func methodsOf(requests []recordedRequest) []string {
	methods := make([]string, len(requests))
	for index, request := range requests {
		methods[index] = request.Method
	}
	return methods
}

func waitFor(t *testing.T, condition func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(testChannelTimeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

func TestCancellingACallMidStreamStillNotifiesTheServer(t *testing.T) {
	// The existing cancellation test blocks before the peer writes headers, so
	// it only reaches the POST-failure branch. A long-running tool call is the
	// opposite shape: the peer answers, starts streaming, and stalls — the
	// caller's cancellation then surfaces from the body read, and that path has
	// to announce the abandonment too.
	cancelled := make(chan recordedRequest, 1)
	streaming := make(chan struct{})
	var once sync.Once
	server := newLifecycleServer(t, func(index int, request recordedRequest) (int, string, string) {
		if request.Method == mcpwire.NotificationCancelled {
			select {
			case cancelled <- request:
			default:
			}
			return http.StatusAccepted, "", ""
		}
		return answerLifecycle(func(_ int, request recordedRequest) (int, string, string) {
			return emptyToolsResult(request)
		})(index, request)
	})
	// Replace the handler so the tool call can hold an open stream: the
	// fixture's own handler returns a whole body at once.
	streamed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var envelope struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &envelope)
		if envelope.Method != "tools/call" {
			r.Body = io.NopCloser(bytes.NewReader(body))
			server.Config.Handler.ServeHTTP(w, r)
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		once.Do(func() { close(streaming) })
		<-r.Context().Done()
	}))
	t.Cleanup(streamed.Close)

	client := NewClient(streamed.URL, "", streamed.Client())
	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.CallTool(ctx, "files.list", nil)
		done <- err
	}()
	select {
	case <-streaming:
	case <-time.After(testChannelTimeout):
		t.Fatal("the peer never started streaming")
	}
	// The peer has flushed headers; give the client's Do() time to return so
	// the cancellation provably lands in the body read rather than the POST.
	time.Sleep(100 * time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("CallTool error = %v, want context.Canceled", err)
	}

	select {
	case notification := <-cancelled:
		if len(notification.ID) != 0 {
			t.Errorf("notifications/cancelled carried id %s; it is a notification", notification.ID)
		}
		if notification.Params["requestId"] == nil {
			t.Errorf("cancellation named no request: %#v", notification.Params)
		}
	case <-time.After(testChannelTimeout):
		t.Fatal("a call abandoned mid-stream was never announced to the peer")
	}
}

func TestAConformingCallToolResultIsUnwrappedForCallers(t *testing.T) {
	// The bundled servers answer tools/call in the protocol's own shape so a
	// stock client can read it. Turing's own callers want the tool's data, not
	// the envelope, so the client hands back structuredContent — which is what
	// every caller saw before the servers started conforming.
	server := newLifecycleServer(t, answerLifecycle(func(_ int, request recordedRequest) (int, string, string) {
		return http.StatusOK, "application/json", fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"serialized"}],`+
				`"structuredContent":{"path":"note.txt","bytesRead":5},"isError":false}}`, request.ID)
	}))
	client := NewClient(server.URL, "", server.Client())

	result, err := client.CallTool(context.Background(), "files.read", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result["path"] != "note.txt" {
		t.Fatalf("result = %#v, want the tool's own map unwrapped from structuredContent", result)
	}
	if _, wrapped := result["structuredContent"]; wrapped {
		t.Fatalf("result = %#v, want the envelope unwrapped, not passed through", result)
	}
}

func TestAPeerResultWithoutStructuredContentIsPassedThrough(t *testing.T) {
	// A third-party peer that sends no structuredContent must not be mangled.
	server := newLifecycleServer(t, answerLifecycle(func(_ int, request recordedRequest) (int, string, string) {
		return http.StatusOK, "application/json", fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"plain"}]}}`, request.ID)
	}))
	client := NewClient(server.URL, "", server.Client())

	result, err := client.CallTool(context.Background(), "vendor.tool", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if _, present := result["content"]; !present {
		t.Fatalf("result = %#v, want the peer's own result untouched", result)
	}
}

func TestACancelledCallReturnsWhileTheCancellationNoticeIsStillInFlight(t *testing.T) {
	// The notice is detached so a stalled peer cannot hold the tool call open
	// for the full cancellationNotifyTimeout, which would delay the runner from
	// recording the cancelled call's outcome. Every other cancellation fixture
	// answers the notice immediately, so none of them can tell an inline notice
	// from a detached one — this parks the peer's handler instead.
	parked := make(chan struct{})
	notified := make(chan struct{}, 1)

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &envelope)
		switch envelope.Method {
		case mcpwire.MethodInitialize:
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w,
				`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}},"serverInfo":{"name":"peer","version":"1"}}}`,
				envelope.ID, mcpwire.ProtocolVersion)
		case mcpwire.NotificationInitialized:
			w.WriteHeader(http.StatusAccepted)
		case mcpwire.NotificationCancelled:
			select {
			case notified <- struct{}{}:
			default:
			}
			<-parked
			w.WriteHeader(http.StatusAccepted)
		default:
			// The operation-phase call never answers; the caller gives up.
			<-parked
		}
	}))
	defer peer.Close()
	// Released before peer.Close so parked handlers cannot deadlock shutdown.
	defer close(parked)

	client := NewClient(peer.URL, "", peer.Client())
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		_, _ = client.CallTool(ctx, "files.read", nil)
	}()

	select {
	case <-returned:
	case <-time.After(cancellationNotifyTimeout):
		t.Fatal("CallTool did not return while the peer was still holding the cancellation notice; the notice is not detached")
	}
	select {
	case <-notified:
	case <-time.After(5 * time.Second):
		t.Fatal("the peer never received the cancellation notice")
	}
}

func TestABundledPeerAssigningAnUnusableSessionIDFailsWithAReason(t *testing.T) {
	// The registry client has this test; this is the bundled-server half of the
	// same wiring. Refusing the id is right, but the caller has to be told why:
	// otherwise every later request goes unsessioned and the failure that
	// eventually surfaces points nowhere near the cause.
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &envelope)
		if envelope.Method != mcpwire.MethodInitialize {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("content-type", "application/json")
		w.Header().Set(mcpwire.SessionHeader, strings.Repeat("s", 4096))
		fmt.Fprintf(w,
			`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}},"serverInfo":{"name":"peer","version":"1"}}}`,
			envelope.ID, mcpwire.ProtocolVersion)
	}))
	defer peer.Close()

	_, err := NewClient(peer.URL, "", peer.Client()).ListTools(context.Background())
	if err == nil {
		t.Fatal("a peer assigned an unusable session id and discovery proceeded anyway")
	}
	if !errors.Is(err, mcpwire.ErrUnusableSessionID) {
		t.Fatalf("error = %v, want one wrapping mcpwire.ErrUnusableSessionID", err)
	}
}

func TestAnEmptyStructuredContentDoesNotEraseThePeersAnswer(t *testing.T) {
	// The unwrap replaces the whole result with structuredContent. A conforming
	// peer whose tool declares an output schema but has nothing structured to
	// say can send an empty object beside a populated content block — and this
	// helper now governs third-party peers too, so erasing their answer would
	// hand the model {} for a call that actually returned something.
	result := map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": "the vendor's answer"}},
		"structuredContent": map[string]any{},
		"isError":           false,
	}
	unwrapped := unwrapCallToolResult(result)
	if _, present := unwrapped["content"]; !present {
		t.Fatalf("result = %#v, want the peer's content preserved when structuredContent is empty", unwrapped)
	}
}

func TestAPeerAdvertisingNoToolsCapabilityIsRefused(t *testing.T) {
	// CON-001 scopes capability negotiation, not only version negotiation.
	// Driving tools/list at a peer that just told us it has no tools is Turing
	// violating the handshake it completed, and the failure would otherwise
	// arrive as an opaque peer error naming nothing.
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &envelope)
		if envelope.Method != mcpwire.MethodInitialize {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w,
			`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"resources":{}},"serverInfo":{"name":"peer","version":"1"}}}`,
			envelope.ID, mcpwire.ProtocolVersion)
	}))
	defer peer.Close()

	_, err := NewClient(peer.URL, "", peer.Client()).ListTools(context.Background())
	if err == nil {
		t.Fatal("discovery proceeded against a peer that advertised no tools capability")
	}
	if !errors.Is(err, mcpwire.ErrToolsNotAdvertised) {
		t.Fatalf("error = %v, want one wrapping ErrToolsNotAdvertised", err)
	}
}

func TestARefusedRedirectIsNotReportedAsRetryable(t *testing.T) {
	// The refusal is a security decision, not a transport hiccup: the peer will
	// redirect again on every attempt. Reporting it retryable tells a user to
	// wait for something that cannot change, and the run's failure carries the
	// wrong retry class. The repository already types this refusal —
	// egress.RedirectBlockedError.Retryable() is false — which is what makes the
	// distinction possible here.
	var elsewhereReached atomic.Bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhereReached.Store(true)
		w.Header().Set("content-type", "application/json")
		// A literal id is safe here and only here: reaching this target fails the
		// test outright. A fixture a client actually reads has to echo the id the
		// request carried.
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer elsewhere.Close()

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer peer.Close()

	_, err := NewClient(peer.URL, "", peer.Client()).ListTools(context.Background())
	// Checked first, and separately from the error: a followed redirect fails
	// this call for its own reasons — a mismatched response id, an unusable
	// revision — so asserting only that ListTools errored, and that the error is
	// non-retryable, holds just as well when the refusal has been removed.
	if elsewhereReached.Load() {
		t.Fatal("the client followed a peer-chosen redirect")
	}
	if err == nil {
		t.Fatal("a redirecting peer was followed")
	}
	if Retryable(err) {
		t.Fatalf("error = %v, reported retryable; a refused redirect can never succeed on retry", err)
	}
}

func TestTheRuntimeClientRefusesANilHTTPClient(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("NewClient accepted a nil http.Client")
		}
	}()
	NewClient("http://127.0.0.1:1", "", nil)
}

func TestTwoClientsDoNotNumberTheirRequestsAlike(t *testing.T) {
	// The bundled servers key an in-flight request by identity plus the id the
	// client chose, and that identity is one shared token per agent kind. Two
	// runtimes numbering from the same place would make one's
	// notifications/cancelled stop the other's call — for a mutating file tool,
	// aborting a write whose one-time approval was already spent.
	const clients = 64
	starts := make(map[int64]bool, clients)
	for index := 0; index < clients; index++ {
		client := NewClient("http://127.0.0.1:1", "", http.DefaultClient)
		start := client.nextRequestID()
		if starts[start] {
			t.Fatalf("client %d began numbering at %d, which another client had already taken", index, start)
		}
		starts[start] = true

		// Whatever the start, every id the client can issue has to stay inside
		// the range the servers' registry accepts, or a cancellation could not
		// name it at all.
		if start < 1 || start > mcpwire.MaxStartingRequestID {
			t.Fatalf("starting id %d is outside [1, %d]", start, int64(mcpwire.MaxStartingRequestID))
		}
		if next := client.nextRequestID(); next != start+1 {
			t.Fatalf("second id = %d, want %d: ids must stay consecutive", next, start+1)
		}
	}
}
