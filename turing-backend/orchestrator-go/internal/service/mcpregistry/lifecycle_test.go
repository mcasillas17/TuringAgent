package mcpregistry

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
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/mcpwire"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// peerRequest is one message a lifecycle fixture peer saw, with the transport
// headers that carry the negotiated connection.
type peerRequest struct {
	Method          string
	ID              json.RawMessage
	Params          map[string]any
	ProtocolVersion string
	SessionID       string
	Accept          string
	Authorization   string
}

// answerMCPHandshake replies to the MCP lifecycle methods and passes every
// other message to next, so a fixture written against the operation phase does
// not have to restate the handshake.
func answerMCPHandshake(next http.Handler) http.Handler {
	return answerMCPHandshakeWithSession(next, "", nil)
}

// answerMCPHandshakeWithSession is answerMCPHandshake for a peer that assigns a
// session. The delete is answered in this wrapper rather than passed to next, so
// a test counting operation-phase requests is unaffected by it.
func answerMCPHandshakeWithSession(next http.Handler, session string, deleted chan<- string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if deleted != nil {
				select {
				case deleted <- r.Header.Get(mcpwire.SessionHeader):
				default:
				}
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "unreadable body", http.StatusBadRequest)
			return
		}
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &envelope)
		switch envelope.Method {
		case mcpwire.MethodInitialize:
			w.Header().Set("content-type", "application/json")
			if session != "" {
				w.Header().Set(mcpwire.SessionHeader, session)
			}
			fmt.Fprintf(w,
				`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}},"serverInfo":{"name":"peer","version":"1"}}}`,
				envelope.ID, mcpwire.ProtocolVersion)
		case mcpwire.NotificationInitialized, mcpwire.NotificationCancelled:
			w.WriteHeader(http.StatusAccepted)
		default:
			r.Body = io.NopCloser(bytes.NewReader(body))
			next.ServeHTTP(w, r)
		}
	})
}

// recordingPeer captures every message and answers the handshake plus whatever
// rest returns for the operation phase.
type recordingPeer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []peerRequest
	session  string
}

func newRecordingPeer(t *testing.T, session string, rest func(peerRequest) (int, string, string)) *recordingPeer {
	t.Helper()
	peer := &recordingPeer{session: session}
	peer.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		recorded := peerRequest{
			Method:          envelope.Method,
			ID:              envelope.ID,
			Params:          envelope.Params,
			ProtocolVersion: r.Header.Get(mcpwire.ProtocolVersionHeader),
			SessionID:       r.Header.Get(mcpwire.SessionHeader),
			Accept:          r.Header.Get("Accept"),
			Authorization:   r.Header.Get("Authorization"),
		}
		peer.mu.Lock()
		peer.requests = append(peer.requests, recorded)
		peer.mu.Unlock()

		var status int
		var contentType, body string
		switch envelope.Method {
		case mcpwire.MethodInitialize:
			if peer.session != "" {
				w.Header().Set(mcpwire.SessionHeader, peer.session)
			}
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
	t.Cleanup(peer.Close)
	return peer
}

func (p *recordingPeer) seen() []peerRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]peerRequest(nil), p.requests...)
}

func peerMethods(requests []peerRequest) []string {
	methods := make([]string, len(requests))
	for index, request := range requests {
		methods[index] = request.Method
	}
	return methods
}

func emptyToolsPage(request peerRequest) (int, string, string) {
	return http.StatusOK, "application/json",
		fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}`, request.ID)
}

func TestRegistryClientInitializesBeforeDiscovery(t *testing.T) {
	peer := newRecordingPeer(t, "", emptyToolsPage)

	if _, err := newMCPClient(peer.URL, "vendor-token", peer.Client()).listTools(context.Background()); err != nil {
		t.Fatalf("listTools: %v", err)
	}

	requests := peer.seen()
	want := []string{mcpwire.MethodInitialize, mcpwire.NotificationInitialized, "tools/list"}
	if got := peerMethods(requests); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("methods = %v, want %v", got, want)
	}
	for index, request := range requests {
		if !strings.Contains(request.Accept, "application/json") ||
			!strings.Contains(request.Accept, "text/event-stream") {
			t.Errorf("request %d Accept = %q, want both Streamable HTTP media types", index, request.Accept)
		}
		if request.Authorization != "Bearer vendor-token" {
			t.Errorf("request %d Authorization = %q", index, request.Authorization)
		}
	}
	if requests[0].ProtocolVersion != "" {
		t.Errorf("initialize announced a version before negotiating one: %q", requests[0].ProtocolVersion)
	}
	for _, index := range []int{1, 2} {
		if requests[index].ProtocolVersion != mcpwire.ProtocolVersion {
			t.Errorf("request %d %s = %q", index, mcpwire.ProtocolVersionHeader, requests[index].ProtocolVersion)
		}
	}
}

func TestRegistryClientInitializeCarriesNoMetadata(t *testing.T) {
	peer := newRecordingPeer(t, "", emptyToolsPage)
	if _, err := newMCPClient(peer.URL, "", peer.Client()).listTools(context.Background()); err != nil {
		t.Fatalf("listTools: %v", err)
	}
	params := peer.seen()[0].Params
	if _, present := params["_meta"]; present {
		t.Fatal("initialize must never carry _meta: discovery consumes no approval and grants no capability")
	}
	capabilities, ok := params["capabilities"].(map[string]any)
	if !ok || len(capabilities) != 0 {
		t.Fatalf("initialize capabilities = %#v, want an empty object", params["capabilities"])
	}
}

func TestRegistryClientAnnouncesTheServerAssignedSession(t *testing.T) {
	peer := newRecordingPeer(t, "peer-session-77", emptyToolsPage)
	if _, err := newMCPClient(peer.URL, "", peer.Client()).listTools(context.Background()); err != nil {
		t.Fatalf("listTools: %v", err)
	}
	requests := peer.seen()
	if requests[0].SessionID != "" {
		t.Errorf("initialize carried a session before one was assigned: %q", requests[0].SessionID)
	}
	for _, index := range []int{1, 2} {
		if requests[index].SessionID != "peer-session-77" {
			t.Errorf("request %d (%s) session = %q", index, requests[index].Method, requests[index].SessionID)
		}
	}
}

func TestEachRegistryClientNegotiatesItsOwnConnection(t *testing.T) {
	// Two different servers must never share a session or a negotiated state:
	// the orchestrator builds one client per dispatch, so there is nothing to
	// leak between them.
	first := newRecordingPeer(t, "session-first", emptyToolsPage)
	second := newRecordingPeer(t, "session-second", emptyToolsPage)

	if _, err := newMCPClient(first.URL, "first-token", first.Client()).listTools(context.Background()); err != nil {
		t.Fatalf("first listTools: %v", err)
	}
	if _, err := newMCPClient(second.URL, "second-token", second.Client()).listTools(context.Background()); err != nil {
		t.Fatalf("second listTools: %v", err)
	}
	for _, request := range second.seen() {
		if request.SessionID == "session-first" {
			t.Fatal("the second server was addressed with the first server's session")
		}
		if strings.Contains(request.Authorization, "first-token") {
			t.Fatal("the second server was addressed with the first server's credential")
		}
	}
}

func TestRegistryClientRefusesAnUnsupportedNegotiatedVersion(t *testing.T) {
	unsupported := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&envelope)
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"1999-01-01","capabilities":{"tools":{}}}}`, envelope.ID)
	}))
	t.Cleanup(unsupported.Close)

	_, err := newMCPClient(unsupported.URL, "", unsupported.Client()).listTools(context.Background())
	if err == nil {
		t.Fatal("want a refusal when the peer negotiates a revision Turing does not implement")
	}
	if !errors.Is(err, mcpwire.ErrUnsupportedProtocolVersion) {
		t.Fatalf("error = %v, want ErrUnsupportedProtocolVersion", err)
	}
	if !strings.Contains(err.Error(), mcpwire.ProtocolVersion) {
		t.Fatalf("error %q should name the revision Turing speaks", err)
	}
}

func TestHandshakeFailuresStillRedactTheBearerToken(t *testing.T) {
	const token = "vendor-handshake-sentinel-4f2b9c7e"
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&envelope)
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":%q}}`, envelope.ID, "rejected "+token)
	}))
	t.Cleanup(peer.Close)

	_, err := newMCPClient(peer.URL, token, peer.Client()).listTools(context.Background())
	if err == nil {
		t.Fatal("want the handshake failure to surface")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("handshake error leaked the bearer token: %q", err)
	}
}

func TestRegistryClientReadsAnEventStreamResponse(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&envelope)
		switch envelope.Method {
		case mcpwire.NotificationInitialized:
			w.WriteHeader(http.StatusAccepted)
		case mcpwire.MethodInitialize:
			w.Header().Set("content-type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"protocolVersion\":%q,\"capabilities\":{\"tools\":{}}}}\n\n",
				envelope.ID, mcpwire.ProtocolVersion)
		default:
			w.Header().Set("content-type", "text/event-stream")
			fmt.Fprintf(w, "id: 1\ndata: \n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"tools\":[{\"name\":\"vendor.only\",\"inputSchema\":{\"type\":\"object\"}}]}}\n\n",
				envelope.ID)
		}
	}))
	t.Cleanup(peer.Close)

	tools, err := newMCPClient(peer.URL, "", peer.Client()).listTools(context.Background())
	if err != nil {
		t.Fatalf("listTools over SSE: %v", err)
	}
	if len(tools) != 1 || tools[0]["name"] != "vendor.only" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestAPeerErrorCarryingDataStillSurfacesItsCodeAndMessage(t *testing.T) {
	// `data` is an optional JSON-RPC error member, and the reference SDK
	// attaches one to its unsupported-protocol-version error. The envelope
	// decoder rejects unknown fields inside nested objects too, so an
	// undeclared `data` would turn a peer's valid protocol error into a decode
	// failure and lose the code and message the operator needs.
	peer := newRecordingPeer(t, "", func(request peerRequest) (int, string, string) {
		return http.StatusOK, "application/json", fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"vendor refused the cursor",`+
				`"data":{"supported":["2025-11-25"],"requested":"1999-01-01"}}}`, request.ID)
	})

	_, err := newMCPClient(peer.URL, "", peer.Client()).listTools(context.Background())
	if err == nil {
		t.Fatal("want the peer's protocol error surfaced")
	}
	if !strings.Contains(err.Error(), "vendor refused the cursor") {
		t.Fatalf("error = %v, want the peer's own message preserved", err)
	}
	if !strings.Contains(err.Error(), "-32602") {
		t.Fatalf("error = %v, want the peer's own code preserved", err)
	}
}

func TestAServerInitiatedRequestOnTheStreamIsNotMistakenForTheResponse(t *testing.T) {
	// A peer may send its own request on the event stream before answering.
	// It carries an id, so a reader that only looks for an id would hand it
	// back as the answer and fail the call on a mismatch.
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&envelope)
		switch envelope.Method {
		case mcpwire.NotificationInitialized:
			w.WriteHeader(http.StatusAccepted)
		case mcpwire.MethodInitialize:
			w.Header().Set("content-type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"protocolVersion\":%q,\"capabilities\":{\"tools\":{}}}}\n\n",
				envelope.ID, mcpwire.ProtocolVersion)
		default:
			w.Header().Set("content-type", "text/event-stream")
			fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":9001,\"method\":\"ping\"}\n\n")
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"tools\":[{\"name\":\"vendor.only\",\"inputSchema\":{\"type\":\"object\"}}]}}\n\n",
				envelope.ID)
		}
	}))
	t.Cleanup(peer.Close)

	tools, err := newMCPClient(peer.URL, "", peer.Client()).listTools(context.Background())
	if err != nil {
		t.Fatalf("listTools past a server-initiated request: %v", err)
	}
	if len(tools) != 1 || tools[0]["name"] != "vendor.only" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestCancellingADispatchNotifiesThePeerWithTheSameRequestID(t *testing.T) {
	// A third-party dispatch that its caller abandons should tell the peer to
	// stop working, on the same negotiated connection. It is advisory only: it
	// never asserts that a mutation the peer already committed was undone, and
	// nothing is re-dispatched.
	blocked := make(chan struct{})
	reached := make(chan struct{})
	var once sync.Once
	peer := newRecordingPeer(t, "", func(request peerRequest) (int, string, string) {
		if request.Method == "tools/call" {
			once.Do(func() { close(reached) })
			<-blocked
		}
		return emptyToolsPage(request)
	})
	client := newMCPClient(peer.URL, "", peer.Client())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.callTool(ctx, "vendor.write", map[string]any{"path": "x"})
		done <- err
	}()
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("the dispatch never reached the peer")
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("want the cancellation surfaced to the caller")
	}
	close(blocked)

	var cancellation *peerRequest
	var toolCallID string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && cancellation == nil {
		for _, request := range peer.seen() {
			switch request.Method {
			case "tools/call":
				toolCallID = string(request.ID)
			case mcpwire.NotificationCancelled:
				recorded := request
				cancellation = &recorded
			}
		}
		if cancellation == nil {
			time.Sleep(time.Millisecond)
		}
	}
	if cancellation == nil {
		t.Fatal("the peer was never told the dispatch was abandoned")
	}
	if len(cancellation.ID) != 0 {
		t.Errorf("notifications/cancelled carried id %s; it is a notification", cancellation.ID)
	}
	if got := jsonNumberText(t, cancellation.Params["requestId"]); got != toolCallID {
		t.Fatalf("cancelled requestId = %s, want the dispatch's own id (%s)", got, toolCallID)
	}
	if cancellation.ProtocolVersion != mcpwire.ProtocolVersion {
		t.Errorf("cancellation must ride the negotiated connection, got %q", cancellation.ProtocolVersion)
	}
}

func TestAnAbandonedHandshakeIsNeverCancelled(t *testing.T) {
	// The lifecycle specification forbids a client from cancelling `initialize`.
	release := make(chan struct{})
	reached := make(chan struct{})
	var once sync.Once
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&envelope)
		if envelope.Method == mcpwire.MethodInitialize {
			once.Do(func() { close(reached) })
			<-release
		}
		if envelope.Method == mcpwire.NotificationCancelled {
			t.Error("initialize must never be cancelled by a client")
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(peer.Close)
	client := newMCPClient(peer.URL, "", peer.Client())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = client.listTools(ctx)
	}()
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("the handshake never reached the peer")
	}
	cancel()
	<-done
	close(release)
}

func TestToolCallIsNeverReplayedAfterASessionIsLost(t *testing.T) {
	var calls int
	peer := newRecordingPeer(t, "peer-session", func(request peerRequest) (int, string, string) {
		if request.Method == "tools/call" {
			calls++
			return http.StatusNotFound, "text/plain", "session not found"
		}
		return emptyToolsPage(request)
	})

	if _, err := newMCPClient(peer.URL, "", peer.Client()).callTool(context.Background(), "vendor.write", nil); err == nil {
		t.Fatal("want the lost-session failure to surface")
	}
	if calls != 1 {
		t.Fatalf("tools/call dispatched %d times; a mutation whose delivery is ambiguous must never be replayed", calls)
	}
}

func TestDiscoveryDropsPeerSuppliedToolAnnotations(t *testing.T) {
	// readOnlyHint and friends are untrusted decoration. Discovery keeps only
	// the name and the input schema, so no annotation can reach policy.
	peer := newRecordingPeer(t, "", func(request peerRequest) (int, string, string) {
		return http.StatusOK, "application/json", fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"vendor.wipe","description":"d",`+
				`"inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true,"destructiveHint":false}}]}}`,
			request.ID)
	})
	service, repo := newRegistryTestService(t)
	service.httpClient = peer.Client()
	registered, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "annotated", URL: peer.URL, Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.discover(context.Background(), registered.Server.ID); err != nil {
		t.Fatalf("discover: %v", err)
	}

	tools, err := repo.ListMCPServerTools(context.Background(), registered.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %+v, want one", tools)
	}
	if strings.Contains(tools[0].SchemaJSON, "readOnlyHint") {
		t.Fatalf("stored schema kept a peer annotation: %s", tools[0].SchemaJSON)
	}
	if tools[0].Policy != "approval_required" {
		t.Fatalf("policy = %q; a readOnlyHint must not soften the default policy", tools[0].Policy)
	}
}

func TestCancellingADispatchMidStreamStillNotifiesThePeer(t *testing.T) {
	// The other cancellation test blocks before headers, reaching only the
	// POST-failure branch. A long-running third-party tool call streams
	// instead: the peer answers, starts an event stream and stalls, so the
	// caller's cancellation surfaces from the body read. That path has to
	// announce the abandonment too.
	cancelled := make(chan peerRequest, 1)
	streaming := make(chan struct{})
	var once sync.Once
	var seen sync.Mutex
	var toolCallID string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&envelope)
		switch envelope.Method {
		case mcpwire.MethodInitialize:
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}}}}`,
				envelope.ID, mcpwire.ProtocolVersion)
		case mcpwire.NotificationInitialized:
			w.WriteHeader(http.StatusAccepted)
		case mcpwire.NotificationCancelled:
			select {
			case cancelled <- peerRequest{Method: envelope.Method, ID: envelope.ID, Params: envelope.Params,
				ProtocolVersion: r.Header.Get(mcpwire.ProtocolVersionHeader)}:
			default:
			}
			w.WriteHeader(http.StatusAccepted)
		default:
			seen.Lock()
			toolCallID = string(envelope.ID)
			seen.Unlock()
			w.Header().Set("content-type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			once.Do(func() { close(streaming) })
			<-r.Context().Done()
		}
	}))
	t.Cleanup(peer.Close)

	client := newMCPClient(peer.URL, "", peer.Client())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.callTool(ctx, "vendor.write", map[string]any{"path": "x"})
		done <- err
	}()
	select {
	case <-streaming:
	case <-time.After(5 * time.Second):
		t.Fatal("the peer never started streaming")
	}
	// The peer has flushed headers; give the client's Do() time to return so
	// the cancellation provably lands in the body read rather than the POST.
	time.Sleep(100 * time.Millisecond)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("want the cancellation surfaced to the caller")
	}

	select {
	case notification := <-cancelled:
		seen.Lock()
		want := toolCallID
		seen.Unlock()
		if got := jsonNumberText(t, notification.Params["requestId"]); got != want {
			t.Fatalf("cancelled requestId = %s, want the dispatch's own id (%s)", got, want)
		}
		if notification.ProtocolVersion != mcpwire.ProtocolVersion {
			t.Errorf("cancellation must ride the negotiated connection, got %q", notification.ProtocolVersion)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a dispatch abandoned mid-stream was never announced to the peer")
	}
}

func TestAPerDispatchClientTerminatesTheSessionItOpened(t *testing.T) {
	// This client is built fresh for every discovery and every dispatch, so a
	// peer that assigns sessions would accumulate one abandoned session per
	// operation — reclaimable only by its own timeout — if nothing deleted them.
	// The transport says a client that no longer needs a session SHOULD delete
	// it, and here the cost of not doing so lands on a third party's server.
	var deleted, deletedSession string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if got := r.Header.Get(mcpwire.ProtocolVersionHeader); got != mcpwire.ProtocolVersion {
				t.Errorf("delete carried protocol version %q, want %q", got, mcpwire.ProtocolVersion)
			}
			deleted = r.Method
			deletedSession = r.Header.Get(mcpwire.SessionHeader)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var req struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		if req.Method == "" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("content-type", "application/json")
		if req.Method == mcpwire.MethodInitialize {
			w.Header().Set(mcpwire.SessionHeader, "vendor-session-1")
			fmt.Fprintf(w,
				`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}},"serverInfo":{"name":"vendor","version":"1"}}}`,
				req.ID, mcpwire.ProtocolVersion)
			return
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}`, req.ID)
	}))
	defer peer.Close()

	client := newMCPClient(peer.URL, "", peer.Client())
	if _, err := client.listTools(context.Background()); err != nil {
		t.Fatalf("listTools: %v", err)
	}
	client.releaseSession()

	if deleted != http.MethodDelete {
		t.Fatal("the client never terminated the session the peer assigned it")
	}
	if deletedSession != "vendor-session-1" {
		t.Fatalf("delete carried session %q, want the one the peer assigned", deletedSession)
	}
}

func TestASessionlessPeerIsNeverSentASessionDelete(t *testing.T) {
	// Both bundled servers are sessionless, and a peer that assigns nothing has
	// nothing to release: sending a bare DELETE would be a pointless request
	// against every such server on every dispatch.
	var methods []string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		var req struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		if req.Method == "" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("content-type", "application/json")
		if req.Method == mcpwire.MethodInitialize {
			fmt.Fprintf(w,
				`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}},"serverInfo":{"name":"vendor","version":"1"}}}`,
				req.ID, mcpwire.ProtocolVersion)
			return
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}`, req.ID)
	}))
	defer peer.Close()

	client := newMCPClient(peer.URL, "", peer.Client())
	if _, err := client.listTools(context.Background()); err != nil {
		t.Fatalf("listTools: %v", err)
	}
	client.releaseSession()

	for _, method := range methods {
		if method == http.MethodDelete {
			t.Fatal("a sessionless peer was sent a session delete")
		}
	}
}

func TestACancelledDispatchTellsThePeerToStopBeforeReleasingItsSession(t *testing.T) {
	// Both the cancellation and the session delete are detached, to keep a slow
	// peer off the credential lock. Detached means unordered, and the order
	// matters: the DELETE terminates the very session the cancellation names, so
	// a delete that lands first makes a conforming peer answer the cancellation
	// 404 and keep working on a call nobody is waiting for — the exact case
	// cancellation exists for.
	var mu sync.Mutex
	var order []string
	release := make(chan struct{})
	observed := make(chan struct{}, 4)

	peer := httptest.NewServer(answerMCPHandshakeWithSession(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The operation-phase call blocks until the caller gives up.
			body, _ := io.ReadAll(r.Body)
			var envelope struct {
				ID json.RawMessage `json:"id"`
			}
			_ = json.Unmarshal(body, &envelope)
			<-release
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{}}`, envelope.ID)
		}), "ordered-session-1", nil))
	defer peer.Close()

	recorder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mu.Lock()
			order = append(order, "delete")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			observed <- struct{}{}
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), mcpwire.NotificationCancelled) {
			mu.Lock()
			order = append(order, "cancelled")
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			observed <- struct{}{}
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		peer.Config.Handler.ServeHTTP(w, r)
	}))
	defer recorder.Close()

	client := newMCPClient(recorder.URL, "", recorder.Client())
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := client.callTool(ctx, "vendor.slow", nil)
	if err == nil {
		t.Fatal("callTool returned before its context was cancelled")
	}
	close(release)
	client.releaseSession()

	for received := 0; received < 2; received++ {
		select {
		case <-observed:
		case <-time.After(5 * time.Second):
			t.Fatalf("only saw %v", order)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "cancelled" || order[1] != "delete" {
		t.Fatalf("peer observed %v, want the cancellation before the session delete", order)
	}
}

func TestDiscoveryReinitializesAndReplaysAfterThePeerExpiresItsSession(t *testing.T) {
	// The runtime client has this test; the registry client did not — and this
	// is the path that actually meets it, because both bundled servers are
	// sessionless and can never produce a session-loss 404 at all. Deleting the
	// replay branch left every orchestrator test green while removing behaviour
	// the guide states for both paths.
	var mu sync.Mutex
	var initializes int
	var listSessions []string
	expired := true

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &envelope)
		switch envelope.Method {
		case mcpwire.MethodInitialize:
			mu.Lock()
			initializes++
			session := fmt.Sprintf("session-%d", initializes)
			mu.Unlock()
			w.Header().Set("content-type", "application/json")
			w.Header().Set(mcpwire.SessionHeader, session)
			fmt.Fprintf(w,
				`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}},"serverInfo":{"name":"peer","version":"1"}}}`,
				envelope.ID, mcpwire.ProtocolVersion)
		case mcpwire.NotificationInitialized:
			w.WriteHeader(http.StatusAccepted)
		default:
			mu.Lock()
			listSessions = append(listSessions, r.Header.Get(mcpwire.SessionHeader))
			drop := expired
			expired = false
			mu.Unlock()
			if drop {
				// The peer restarted: the session it assigned is gone.
				http.Error(w, "session not found", http.StatusNotFound)
				return
			}
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[]}}`, envelope.ID)
		}
	}))
	defer peer.Close()

	tools, err := newMCPClient(peer.URL, "", peer.Client()).listTools(context.Background())
	if err != nil {
		t.Fatalf("listTools did not recover from a lost session: %v", err)
	}
	if tools == nil {
		t.Fatal("listTools returned no result after recovery")
	}

	mu.Lock()
	defer mu.Unlock()
	if initializes != 2 {
		t.Fatalf("initialize count = %d, want a second handshake after the session was lost", initializes)
	}
	if len(listSessions) != 2 {
		t.Fatalf("tools/list attempts = %d, want the original and the replay", len(listSessions))
	}
	if listSessions[0] == listSessions[1] {
		t.Fatalf("the replay reused session %q; it must carry the newly assigned one", listSessions[0])
	}
	if listSessions[1] != "session-2" {
		t.Fatalf("replay carried session %q, want the one the second handshake assigned", listSessions[1])
	}
}

func TestAPeerAssigningAnUnusableSessionIDFailsWithAReason(t *testing.T) {
	// Refusing the id is right, but refusing it silently is not: every later
	// request would go unsessioned and a stateful peer would answer a generic
	// 400 that points nowhere near the cause. The handshake fails naming it, so
	// the message reaches the operator through the liveness status the registry
	// already persists.
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

	_, err := newMCPClient(peer.URL, "", peer.Client()).listTools(context.Background())
	if err == nil {
		t.Fatal("a peer assigned an unusable session id and the call proceeded anyway")
	}
	if !strings.Contains(err.Error(), "unusable session id") {
		t.Fatalf("error = %v, want one naming the unusable session id", err)
	}
}

func TestACancelledDispatchReturnsWhileTheCancellationNoticeIsStillInFlight(t *testing.T) {
	// The notice is detached because a dispatch runs holding that server's
	// credential read lock, so a stalled peer would block an operator rotating a
	// leaked token for the full timeout. Every other cancellation fixture in
	// this package answers the notice immediately and so cannot tell an inline
	// notice from a detached one; this one parks the peer's handler.
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
			<-parked
		}
	}))
	defer peer.Close()
	// Released before peer.Close so parked handlers cannot deadlock shutdown.
	defer close(parked)

	client := newMCPClient(peer.URL, "", peer.Client())
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		_, _ = client.callTool(ctx, "vendor.slow", nil)
	}()

	select {
	case <-returned:
	case <-time.After(mcpCancellationNotifyTimeout):
		t.Fatal("callTool did not return while the peer was still holding the cancellation notice; the notice is not detached")
	}
	select {
	case <-notified:
	case <-time.After(5 * time.Second):
		t.Fatal("the peer never received the cancellation notice")
	}
}

func TestARegisteredPeerAdvertisingNoToolsCapabilityIsRefused(t *testing.T) {
	// The registry half of the same rule. This path meets peers Turing does not
	// control, so a peer that declares resources and no tools must be refused by
	// name rather than driven into a generic failure recorded as its liveness.
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
			`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"resources":{}}}}`,
			envelope.ID, mcpwire.ProtocolVersion)
	}))
	defer peer.Close()

	_, err := newMCPClient(peer.URL, "", peer.Client()).listTools(context.Background())
	if err == nil {
		t.Fatal("discovery proceeded against a peer that advertised no tools capability")
	}
	if !errors.Is(err, mcpwire.ErrToolsNotAdvertised) {
		t.Fatalf("error = %v, want one wrapping ErrToolsNotAdvertised", err)
	}
}

func TestASessionOpenedByAFailedHandshakeIsStillReleased(t *testing.T) {
	// The peer assigns a session on initialize and then fails the
	// notifications/initialized POST. Invalidate wipes the Connection's copy, so
	// without remembering it separately releaseSession would see nothing to
	// delete — and every retry against a flaky peer would abandon another
	// session, the exact leak the delete exists to prevent.
	deleted := make(chan string, 4)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			// The version header matters as much as the session: the transport
			// requires it on every post-initialization request, so a peer that
			// enforces it would refuse a delete without one and keep the session.
			if got := r.Header.Get(mcpwire.ProtocolVersionHeader); got != mcpwire.ProtocolVersion {
				t.Errorf("delete carried protocol version %q, want %q", got, mcpwire.ProtocolVersion)
			}
			select {
			case deleted <- r.Header.Get(mcpwire.SessionHeader):
			default:
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &envelope)
		switch envelope.Method {
		case mcpwire.MethodInitialize:
			w.Header().Set("content-type", "application/json")
			w.Header().Set(mcpwire.SessionHeader, "orphaned-session-1")
			fmt.Fprintf(w,
				`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}}}}`,
				envelope.ID, mcpwire.ProtocolVersion)
		case mcpwire.NotificationInitialized:
			// The handshake dies here, after the session exists.
			http.Error(w, "no", http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer peer.Close()

	client := newMCPClient(peer.URL, "", peer.Client())
	if _, err := client.listTools(context.Background()); err == nil {
		t.Fatal("listTools succeeded although the handshake failed")
	}
	client.releaseSession()

	select {
	case session := <-deleted:
		if session != "orphaned-session-1" {
			t.Fatalf("delete carried session %q, want the one the peer assigned", session)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a session opened by a failed handshake was abandoned on the peer")
	}
}

func TestASessionReleaseQuotesTheRevisionThePeerAnswered(t *testing.T) {
	// The session is adopted from the response headers before the envelope is
	// read, so a peer can assign one and then answer with a revision this client
	// rejects. Releasing it under Turing's own revision would tell the peer the
	// session was negotiated under a version it never agreed to.
	const peerRevision = "2025-06-18"
	released := make(chan string, 4)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			select {
			case released <- r.Header.Get(mcpwire.ProtocolVersionHeader):
			default:
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
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
		w.Header().Set(mcpwire.SessionHeader, "session-under-another-revision")
		fmt.Fprintf(w,
			`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}}}}`,
			envelope.ID, peerRevision)
	}))
	defer peer.Close()

	client := newMCPClient(peer.URL, "", peer.Client())
	if _, err := client.listTools(context.Background()); err == nil {
		t.Fatal("discovery proceeded against a peer that answered an unsupported revision")
	}
	client.releaseSession()

	select {
	case version := <-released:
		if version != peerRevision {
			t.Fatalf("delete carried protocol version %q, want the revision the peer answered (%q)",
				version, peerRevision)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the session the peer assigned was never released")
	}
}

func TestASessionReleaseFallsBackToTuringsRevisionWhenThePeerNamedNone(t *testing.T) {
	// A peer can assign a session and then answer with no usable revision at
	// all — here a numeric protocolVersion, which the handshake rejects and
	// which leaves nothing to quote back. The delete still has to carry a
	// revision, because the transport requires the header on every request
	// after initialization and a peer enforcing it would refuse a bare delete
	// and keep the session. Turing's own is the only one left to claim.
	released := make(chan string, 4)
	sessions := make(chan string, 4)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			select {
			case released <- r.Header.Get(mcpwire.ProtocolVersionHeader):
				sessions <- r.Header.Get(mcpwire.SessionHeader)
			default:
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
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
		w.Header().Set(mcpwire.SessionHeader, "session-without-a-revision")
		fmt.Fprintf(w,
			`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":20251125,"capabilities":{"tools":{}}}}`,
			envelope.ID)
	}))
	defer peer.Close()

	client := newMCPClient(peer.URL, "", peer.Client())
	if _, err := client.listTools(context.Background()); err == nil {
		t.Fatal("discovery proceeded against a peer that named no usable revision")
	}
	client.releaseSession()

	select {
	case version := <-released:
		if version != mcpwire.ProtocolVersion {
			t.Fatalf("delete carried protocol version %q, want %q", version, mcpwire.ProtocolVersion)
		}
		if session := <-sessions; session != "session-without-a-revision" {
			t.Fatalf("delete carried session %q, want the one the peer assigned", session)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the session the peer assigned was never released")
	}
}

func TestASessionNamedByAnUnusableIDIsNeverEchoedBack(t *testing.T) {
	// The peer opened a session and named it with an id the transport does not
	// permit. Turing refuses the id — and deliberately does not release the
	// session, because the delete would have to write that id back out. The
	// bound exists so a hostile endpoint cannot make Turing retain and
	// re-transmit a header of megabytes; a teardown request is re-transmission.
	for _, unusable := range []struct {
		name string
		id   string
	}{
		{"one byte over the bound", strings.Repeat("s", 513)},
		{"the size the bound exists for", strings.Repeat("s", 128*1024)},
		{"outside visible ASCII", "session with spaces"},
	} {
		t.Run(unusable.name, func(t *testing.T) {
			deleted := make(chan string, 4)
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					select {
					case deleted <- r.Header.Get(mcpwire.SessionHeader):
					default:
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}
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
				w.Header().Set(mcpwire.SessionHeader, unusable.id)
				fmt.Fprintf(w,
					`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}}}}`,
					envelope.ID, mcpwire.ProtocolVersion)
			}))
			defer peer.Close()

			client := newMCPClient(peer.URL, "", peer.Client())
			_, err := client.listTools(context.Background())
			if !errors.Is(err, mcpwire.ErrUnusableSessionID) {
				t.Fatalf("error = %v, want one wrapping ErrUnusableSessionID", err)
			}
			client.releaseSession()

			select {
			case session := <-deleted:
				t.Fatalf("the refused id was echoed back in a delete (%d bytes)", len(session))
			default:
			}
		})
	}
}

func TestASessionNamedOnAResponseThatCannotBeReadIsStillReleased(t *testing.T) {
	// The peer names a session and then answers with something the client
	// cannot read — a refused status, or a body the reader rejects. It opened
	// the session in both cases, and the header is the only evidence of it, so
	// the record has to be taken before either failure is noticed.
	for _, unreadable := range []struct {
		name  string
		reply func(w http.ResponseWriter, id json.RawMessage)
	}{
		{"a refused status", func(w http.ResponseWriter, id json.RawMessage) {
			http.Error(w, "gone", http.StatusGone)
		}},
		{"a content type the reader refuses", func(w http.ResponseWriter, id json.RawMessage) {
			w.Header().Set("content-type", "text/plain")
			fmt.Fprint(w, "not an MCP message")
		}},
	} {
		t.Run(unreadable.name, func(t *testing.T) {
			deleted := make(chan string, 4)
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					select {
					case deleted <- r.Header.Get(mcpwire.SessionHeader):
					default:
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}
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
				w.Header().Set(mcpwire.SessionHeader, "session-behind-a-bad-answer")
				unreadable.reply(w, envelope.ID)
			}))
			defer peer.Close()

			client := newMCPClient(peer.URL, "", peer.Client())
			if _, err := client.listTools(context.Background()); err == nil {
				t.Fatal("discovery proceeded against a peer whose answer could not be read")
			}
			client.releaseSession()

			select {
			case session := <-deleted:
				if session != "session-behind-a-bad-answer" {
					t.Fatalf("delete carried session %q, want the one the peer assigned", session)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("a session named on an unreadable response was abandoned on the peer")
			}
		})
	}
}

func TestASessionIsStillReleasedWhenThePeerNamesAnUnsendableRevision(t *testing.T) {
	// A revision carrying a control character is one net/http will not write,
	// and it refuses the whole request rather than the header — so keeping it
	// would drop the teardown entirely and abandon the session. Turing's own
	// revision is named instead, exactly as for a peer that named none.
	released := make(chan string, 4)
	sessions := make(chan string, 4)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			select {
			case released <- r.Header.Get(mcpwire.ProtocolVersionHeader):
				sessions <- r.Header.Get(mcpwire.SessionHeader)
			default:
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
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
		w.Header().Set(mcpwire.SessionHeader, "session-behind-a-bad-revision")
		fmt.Fprintf(w,
			`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-11-25\r\nX-Injected: yes","capabilities":{"tools":{}}}}`,
			envelope.ID)
	}))
	defer peer.Close()

	client := newMCPClient(peer.URL, "", peer.Client())
	if _, err := client.listTools(context.Background()); err == nil {
		t.Fatal("discovery proceeded against a peer that named an unusable revision")
	}
	client.releaseSession()

	select {
	case version := <-released:
		if version != mcpwire.ProtocolVersion {
			t.Fatalf("delete carried protocol version %q, want %q", version, mcpwire.ProtocolVersion)
		}
		if session := <-sessions; session != "session-behind-a-bad-revision" {
			t.Fatalf("delete carried session %q, want the one the peer assigned", session)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the session was abandoned because the peer named a revision that could not be sent")
	}
}

func TestTheRegistryClientRefusesANilHTTPClient(t *testing.T) {
	// egress.NoRedirectClient substitutes http.DefaultClient for a nil base, so
	// without the constructor's own check this would quietly produce the
	// unhardened client its doc comment says must never exist.
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("newMCPClient accepted a nil http.Client")
		}
	}()
	newMCPClient("http://127.0.0.1:1", "", nil)
}

// jsonNumberText renders a JSON-decoded request id as the integer it is.
//
// The id crosses the wire as a JSON number and decodes to float64, whose fmt
// rendering switches to exponent form well inside the range an id may occupy —
// which is only invisible while every client starts numbering at 1.
func jsonNumberText(t *testing.T, value any) string {
	t.Helper()
	number, isNumber := value.(float64)
	if !isNumber {
		t.Fatalf("requestId = %#v, want a JSON number", value)
	}
	return strconv.FormatFloat(number, 'f', -1, 64)
}

func TestTwoDispatchesDoNotNumberTheirRequestsAlike(t *testing.T) {
	// This client is built fresh for every discovery and every dispatch, so a
	// constant start means every concurrent call to one endpoint issues the same
	// ids. A sessionless peer keys an in-flight request by the caller's identity
	// plus that id — the design Turing's own bundled servers use — so one
	// dispatch's notifications/cancelled would stop another's call.
	const dispatches = 64
	starts := make(map[int64]bool, dispatches)
	for index := 0; index < dispatches; index++ {
		client := newMCPClient("http://127.0.0.1:1", "", http.DefaultClient)
		start := client.nextRequestID()
		if starts[start] {
			t.Fatalf("dispatch %d began numbering at %d, which another had already taken", index, start)
		}
		starts[start] = true
		if start < 1 || start > mcpwire.MaxStartingRequestID {
			t.Fatalf("starting id %d is outside [1, %d]", start, int64(mcpwire.MaxStartingRequestID))
		}
		if next := client.nextRequestID(); next != start+1 {
			t.Fatalf("second id = %d, want %d: ids must stay consecutive", next, start+1)
		}
	}
}
