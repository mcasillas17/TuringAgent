package mcpwire

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProtocolVersionIsThePinnedRevision(t *testing.T) {
	// Both sides are literals in this package, so this establishes nothing about
	// the revision being latest, handshake-based, or supported by any peer — it
	// only makes a bump deliberate. What the revision actually is gets checked
	// where it can be: TestEveryModuleNamesTheSameMCPRevision in
	// tools/docs/versions_test.go compares it across all three modules, and the
	// SDK conformance suites negotiate it against an independent implementation.
	if ProtocolVersion != "2025-11-25" {
		t.Fatalf(
			"ProtocolVersion = %q; a bump must also move mcp-system's literal and the CLAUDE.md and NORTH_STAR rows",
			ProtocolVersion,
		)
	}
}

func TestAcceptHeaderListsBothStreamableHTTPContentTypes(t *testing.T) {
	// A stock 2025-11-25 server rejects a POST whose Accept header does not
	// list both media types with 400, so this constant is load-bearing.
	if !strings.Contains(AcceptHeader, "application/json") ||
		!strings.Contains(AcceptHeader, "text/event-stream") {
		t.Fatalf("AcceptHeader = %q, want both application/json and text/event-stream", AcceptHeader)
	}
}

func TestReadResponseMessageDecodesPlainJSON(t *testing.T) {
	body := io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":7,"result":{"ok":true}}`))
	message, err := ReadResponseMessage("application/json", body, 1024)
	if err != nil {
		t.Fatalf("ReadResponseMessage: %v", err)
	}
	if string(message) != `{"jsonrpc":"2.0","id":7,"result":{"ok":true}}` {
		t.Fatalf("message = %s", message)
	}
}

func TestReadResponseMessageDecodesSingleSSEEvent(t *testing.T) {
	body := io.NopCloser(strings.NewReader("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{}}\n\n"))
	message, err := ReadResponseMessage("text/event-stream", body, 1024)
	if err != nil {
		t.Fatalf("ReadResponseMessage: %v", err)
	}
	if string(message) != `{"jsonrpc":"2.0","id":7,"result":{}}` {
		t.Fatalf("message = %s", message)
	}
}

func TestReadResponseMessageSkipsPrimeAndNotificationEvents(t *testing.T) {
	// A 2025-11-25 server SHOULD prime the stream with an empty data event,
	// and MAY send notifications before the response. Neither is the answer.
	stream := "id: 1\ndata: \n\n" +
		"event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\n" +
		"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":9,\"result\":{\"done\":true}}\n\n"
	message, err := ReadResponseMessage("text/event-stream", io.NopCloser(strings.NewReader(stream)), 4096)
	if err != nil {
		t.Fatalf("ReadResponseMessage: %v", err)
	}
	if string(message) != `{"jsonrpc":"2.0","id":9,"result":{"done":true}}` {
		t.Fatalf("message = %s", message)
	}
}

func TestReadResponseMessageJoinsMultipleDataLines(t *testing.T) {
	stream := "event: message\ndata: {\"jsonrpc\":\"2.0\",\n" +
		"data: \"id\":3,\"result\":{}}\n\n"
	message, err := ReadResponseMessage("text/event-stream", io.NopCloser(strings.NewReader(stream)), 4096)
	if err != nil {
		t.Fatalf("ReadResponseMessage: %v", err)
	}
	if string(message) != "{\"jsonrpc\":\"2.0\",\n\"id\":3,\"result\":{}}" {
		t.Fatalf("message = %q", message)
	}
}

func TestReadResponseMessageStopsAtTheFirstResponseWithoutDrainingTheStream(t *testing.T) {
	// A peer that never terminates the stream must not be able to hold the
	// call open past its own response: decoding stops at the first response.
	blocked := &blockingReader{
		prefix: "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n",
		block:  make(chan struct{}),
	}
	message, err := ReadResponseMessage("text/event-stream", blocked, 4096)
	if err != nil {
		t.Fatalf("ReadResponseMessage: %v", err)
	}
	if string(message) != `{"jsonrpc":"2.0","id":1,"result":{}}` {
		t.Fatalf("message = %s", message)
	}
}

func TestReadResponseMessageRefusesAnOversizedBody(t *testing.T) {
	oversized := `{"jsonrpc":"2.0","id":1,"result":{"padding":"` + strings.Repeat("x", 512) + `"}}`
	if _, err := ReadResponseMessage("application/json", io.NopCloser(strings.NewReader(oversized)), 64); err == nil {
		t.Fatal("want an error for a body past the limit")
	}
	sse := "event: message\ndata: " + oversized + "\n\n"
	if _, err := ReadResponseMessage("text/event-stream", io.NopCloser(strings.NewReader(sse)), 64); err == nil {
		t.Fatal("want an error for an SSE stream past the limit")
	}
}

func TestReadResponseMessageRefusesAnSSEStreamWithNoResponse(t *testing.T) {
	stream := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n"
	_, err := ReadResponseMessage("text/event-stream", io.NopCloser(strings.NewReader(stream)), 4096)
	if err == nil {
		t.Fatal("want an error when the stream ends without a response")
	}
}

func TestReadResponseMessageRefusesAnUnsupportedContentType(t *testing.T) {
	body := io.NopCloser(strings.NewReader("<html/>"))
	if _, err := ReadResponseMessage("text/html", body, 1024); err == nil {
		t.Fatal("want an error for an unsupported content type")
	}
}

func TestReadResponseMessageAcceptsContentTypeParameters(t *testing.T) {
	body := io.NopCloser(strings.NewReader("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"))
	if _, err := ReadResponseMessage("text/event-stream; charset=utf-8", body, 1024); err != nil {
		t.Fatalf("ReadResponseMessage: %v", err)
	}
}

func TestInitializeParamsAdvertiseNoClientFeatures(t *testing.T) {
	params := InitializeParams("turing-runtime", "1.2.3")
	version, _ := params["protocolVersion"].(string)
	if version != ProtocolVersion {
		t.Fatalf("protocolVersion = %v", params["protocolVersion"])
	}
	capabilities, ok := params["capabilities"].(map[string]any)
	if !ok || len(capabilities) != 0 {
		t.Fatalf("capabilities = %#v, want an empty object: Turing implements no client feature", params["capabilities"])
	}
	info, ok := params["clientInfo"].(map[string]any)
	if !ok || info["name"] != "turing-runtime" || info["version"] != "1.2.3" {
		t.Fatalf("clientInfo = %#v", params["clientInfo"])
	}
	if _, present := params["_meta"]; present {
		t.Fatal("initialize must never carry _meta capabilities")
	}
}

func TestNegotiatedVersionAcceptsOnlyTheSupportedRevision(t *testing.T) {
	version, err := NegotiatedVersion(map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
	})
	if err != nil {
		t.Fatalf("NegotiatedVersion: %v", err)
	}
	if version != ProtocolVersion {
		t.Fatalf("version = %q", version)
	}
	for _, result := range []map[string]any{
		// The stateless successor, two earlier revisions, and the shapes a peer
		// can answer with instead of a revision at all.
		{"protocolVersion": "2026-07-28"},
		{"protocolVersion": "2025-06-18"},
		{"protocolVersion": "2024-11-05"},
		{"protocolVersion": "nonsense"},
		{"protocolVersion": ""},
		{"protocolVersion": 20251125},
		{},
	} {
		if _, err := NegotiatedVersion(result); err == nil {
			t.Fatalf("NegotiatedVersion(%#v) = nil error, want a refusal", result)
		} else if !errors.Is(err, ErrUnsupportedProtocolVersion) {
			t.Fatalf("NegotiatedVersion(%#v) error = %v, want ErrUnsupportedProtocolVersion", result, err)
		}
	}
}

func TestNegotiatedVersionErrorNamesBothVersionsWithoutEchoingUnboundedPeerText(t *testing.T) {
	_, err := NegotiatedVersion(map[string]any{"protocolVersion": strings.Repeat("z", 4096)})
	if err == nil {
		t.Fatal("want a refusal")
	}
	if len(err.Error()) > 256 {
		t.Fatalf("error is %d bytes; a peer must not choose how long it is", len(err.Error()))
	}
	if !strings.Contains(err.Error(), ProtocolVersion) {
		t.Fatalf("error %q should name the supported revision", err)
	}
}

func TestReadResponseMessageSkipsAServerInitiatedRequest(t *testing.T) {
	// A server-initiated request carries an id too, so probing for an id alone
	// would return it as if it were the answer. The client would then fail the
	// call on a mismatched id instead of reading past it.
	stream := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":99,\"method\":\"ping\"}\n\n" +
		"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":5,\"result\":{\"ok\":true}}\n\n"
	message, err := ReadResponseMessage("text/event-stream", io.NopCloser(strings.NewReader(stream)), 4096)
	if err != nil {
		t.Fatalf("ReadResponseMessage: %v", err)
	}
	if string(message) != `{"jsonrpc":"2.0","id":5,"result":{"ok":true}}` {
		t.Fatalf("message = %s, want the response rather than the server's own request", message)
	}
}

func TestConnectionInitializesOnceAndOnlyBecomesReadyAfterTheNotification(t *testing.T) {
	var connection Connection
	if connection.Ready() {
		t.Fatal("a fresh connection reports ready")
	}
	var readyDuringAnnounce bool
	var handshakes, announces int
	err := connection.EnsureInitialized(
		context.Background(),
		func() (string, error) { handshakes++; return ProtocolVersion, nil },
		func() error {
			announces++
			// The version must already be published — the notification carries
			// it in a header — but the connection must not yet be usable, or a
			// concurrent caller could overtake the notification.
			if version, _ := connection.headers(); version != ProtocolVersion {
				t.Errorf("negotiated version not published before the notification: %q", version)
			}
			readyDuringAnnounce = connection.Ready()
			return nil
		},
	)
	if err != nil {
		t.Fatalf("EnsureInitialized: %v", err)
	}
	if readyDuringAnnounce {
		t.Fatal("the connection reported ready before notifications/initialized was delivered")
	}
	if !connection.Ready() {
		t.Fatal("the connection is not ready after a successful handshake")
	}
	if err := connection.EnsureInitialized(
		context.Background(),
		func() (string, error) { handshakes++; return ProtocolVersion, nil },
		func() error { announces++; return nil },
	); err != nil {
		t.Fatalf("second EnsureInitialized: %v", err)
	}
	if handshakes != 1 || announces != 1 {
		t.Fatalf("handshakes = %d, announces = %d, want exactly one of each", handshakes, announces)
	}
}

func TestConnectionIsNotReadyAfterAFailedHandshakeOrNotification(t *testing.T) {
	for name, test := range map[string]struct {
		handshake func() (string, error)
		announce  func() error
	}{
		"handshake fails": {
			handshake: func() (string, error) { return "", errors.New("refused") },
			announce:  func() error { t.Fatal("announce ran after a failed handshake"); return nil },
		},
		"notification fails": {
			handshake: func() (string, error) { return ProtocolVersion, nil },
			announce:  func() error { return errors.New("refused") },
		},
	} {
		t.Run(name, func(t *testing.T) {
			var connection Connection
			connection.AdoptSession("stale-session")
			if err := connection.EnsureInitialized(context.Background(), test.handshake, test.announce); err == nil {
				t.Fatal("want the failure surfaced")
			}
			if connection.Ready() {
				t.Fatal("a failed handshake left the connection usable")
			}
			if version, session := connection.headers(); version != "" || session != "" {
				t.Fatalf("failed handshake left state behind: version=%q session=%q", version, session)
			}
		})
	}
}

func TestConcurrentCallersShareOneHandshake(t *testing.T) {
	var connection Connection
	var mu sync.Mutex
	handshakes := 0
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_ = connection.EnsureInitialized(context.Background(), func() (string, error) {
				mu.Lock()
				handshakes++
				mu.Unlock()
				time.Sleep(5 * time.Millisecond)
				return ProtocolVersion, nil
			}, func() error { return nil })
		}()
	}
	wait.Wait()
	mu.Lock()
	defer mu.Unlock()
	if handshakes != 1 {
		t.Fatalf("handshakes = %d, want exactly 1 across concurrent callers", handshakes)
	}
}

func TestInvalidateDuringAHandshakeIsNotOverwrittenByIt(t *testing.T) {
	// Invalidate is called from a concurrent request's error path and takes
	// only the state lock, so it can land mid-handshake. The handshake must not
	// then publish over the wipe: a connection that reported ready while
	// carrying no version and no session would omit both headers on every later
	// request, and the peer's resulting 404 would not even look like a lost
	// session — so nothing would ever re-initialize it.
	var connection Connection
	err := connection.EnsureInitialized(
		context.Background(),
		func() (string, error) { connection.AdoptSession("s-1"); return ProtocolVersion, nil },
		func() error {
			connection.Invalidate() // a concurrent request saw its session go away
			return nil
		},
	)
	// The handshake must FAIL, not quietly return success: its caller is about
	// to dispatch an operation-phase request, and on an invalidated connection
	// that request would carry no negotiated version and no session, and would
	// precede notifications/initialized.
	if !errors.Is(err, ErrConnectionInvalidated) {
		t.Fatalf("EnsureInitialized error = %v, want ErrConnectionInvalidated", err)
	}
	if connection.Ready() {
		t.Fatal("a handshake invalidated while it ran still marked the connection ready")
	}
	if version, session := connection.headers(); version != "" || session != "" {
		t.Fatalf("state survived the invalidation: version=%q session=%q", version, session)
	}

	// And the connection is still usable: the next caller runs a fresh
	// handshake rather than being stuck.
	if err := connection.EnsureInitialized(
		context.Background(),
		func() (string, error) { return ProtocolVersion, nil },
		func() error { return nil },
	); err != nil {
		t.Fatalf("second EnsureInitialized: %v", err)
	}
	if !connection.Ready() {
		t.Fatal("the connection never recovered after an invalidated handshake")
	}
}

func TestAnInvalidationBeforeTheSessionIsAdoptedLeavesNothingBehind(t *testing.T) {
	// AdoptSession is called from inside the handshake, as the initialize
	// response is decoded, and it writes unguarded. If the invalidation lands
	// BEFORE that point the wipe happens first and the session id is written
	// after it, so refusing to publish is not enough on its own: the next
	// handshake's own initialize POST would carry a stale Mcp-Session-Id for a
	// connection that has no negotiated revision.
	var connection Connection
	err := connection.EnsureInitialized(
		context.Background(),
		func() (string, error) {
			connection.Invalidate() // a concurrent request saw its session go away
			connection.AdoptSession("s-stale")
			return ProtocolVersion, nil
		},
		func() error {
			t.Error("announce ran for a handshake that was already invalidated")
			return nil
		},
	)
	if !errors.Is(err, ErrConnectionInvalidated) {
		t.Fatalf("EnsureInitialized error = %v, want ErrConnectionInvalidated", err)
	}
	if connection.Ready() {
		t.Fatal("an invalidated handshake left the connection ready")
	}
	if version, session := connection.headers(); version != "" || session != "" {
		t.Fatalf("state survived the invalidation: version=%q session=%q", version, session)
	}
}

func TestConnectionCarriesTheSessionTheServerAssigned(t *testing.T) {
	var connection Connection
	if err := connection.EnsureInitialized(
		context.Background(),
		func() (string, error) { connection.AdoptSession("s-1"); return ProtocolVersion, nil },
		func() error { return nil },
	); err != nil {
		t.Fatalf("EnsureInitialized: %v", err)
	}
	if version, session := connection.headers(); version != ProtocolVersion || session != "s-1" {
		t.Fatalf("headers = %q/%q", version, session)
	}
	connection.Invalidate()
	if connection.Ready() {
		t.Fatal("Invalidate left the connection ready")
	}
	if version, session := connection.headers(); version != "" || session != "" {
		t.Fatalf("Invalidate left state behind: version=%q session=%q", version, session)
	}
}

func TestSessionLostIsOnlyA404ThatNamedASession(t *testing.T) {
	withSession := &http.Request{Header: http.Header{SessionHeader: []string{"s-1"}}}
	withoutSession := &http.Request{Header: http.Header{}}
	for name, test := range map[string]struct {
		response *http.Response
		want     bool
	}{
		"404 naming a session": {&http.Response{StatusCode: 404, Request: withSession}, true},
		"404 with no session":  {&http.Response{StatusCode: 404, Request: withoutSession}, false},
		"410 naming a session": {&http.Response{StatusCode: 410, Request: withSession}, false},
		"404 with no request":  {&http.Response{StatusCode: 404}, false},
		"200 naming a session": {&http.Response{StatusCode: 200, Request: withSession}, false},
	} {
		if got := SessionLost(test.response); got != test.want {
			t.Errorf("SessionLost(%s) = %t, want %t", name, got, test.want)
		}
	}
}

// blockingReader serves prefix and then blocks forever, standing in for a peer
// that sends its response and holds the SSE stream open.
type blockingReader struct {
	prefix string
	offset int
	block  chan struct{}
	closed bool
}

func (r *blockingReader) Read(p []byte) (int, error) {
	if r.offset < len(r.prefix) {
		n := copy(p, r.prefix[r.offset:])
		r.offset += n
		return n, nil
	}
	<-r.block
	return 0, io.EOF
}

func (r *blockingReader) Close() error {
	if !r.closed {
		r.closed = true
		close(r.block)
	}
	return nil
}

func TestAnOversizedOrNonConformingSessionIDIsNotAdopted(t *testing.T) {
	// The session id is the one peer-controlled value this package keeps and
	// re-sends on every later request. Everything else a peer supplies is
	// already bounded, and net/http would accept a header of several megabytes,
	// so an unbounded id would let a hostile endpoint make Turing retain and
	// re-transmit one. The transport restricts it to visible ASCII.
	for name, id := range map[string]string{
		"oversized":  strings.Repeat("s", maxPeerHeaderBytes+1),
		"space":      "session id",
		"control":    "session\x01id",
		"newline":    "session\nid",
		"high byte":  "session\x80id",
		"empty byte": "session\x00id",
	} {
		var connection Connection
		if connection.AdoptSession(id) {
			t.Errorf("%s session id was reported as adopted", name)
		}
		if _, session := connection.headers(); session != "" {
			t.Errorf("%s session id was adopted (%d bytes); it would be echoed on every later request", name, len(session))
		}
	}

	var connection Connection
	if !connection.AdoptSession("conforming-session-1") {
		t.Fatal("a conforming session id was refused")
	}
	if _, session := connection.headers(); session != "conforming-session-1" {
		t.Fatalf("session = %q, want the conforming id adopted", session)
	}
}

func TestACallerParkedBehindAnotherHandshakeIsReleasedByItsOwnContext(t *testing.T) {
	// One client is shared across concurrent runs, and the leader's handshake is
	// bounded only by the leader's context. With a mutex here, a run the user
	// just cancelled would sit behind a stranger's network round trip until that
	// stranger finished. The caller's own deadline has to be able to release it.
	var connection Connection
	leaderParked := make(chan struct{})
	releaseLeader := make(chan struct{})

	go func() {
		_ = connection.EnsureInitialized(
			context.Background(),
			func() (string, error) {
				close(leaderParked)
				<-releaseLeader
				return ProtocolVersion, nil
			},
			func() error { return nil },
		)
	}()
	<-leaderParked
	defer close(releaseLeader)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	returned := make(chan error, 1)
	go func() {
		returned <- connection.EnsureInitialized(
			ctx,
			func() (string, error) { return ProtocolVersion, nil },
			func() error { return nil },
		)
	}()

	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("second caller returned %v, want its own context's error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a caller with an already-cancelled context stayed parked behind another handshake")
	}
}

func TestNegotiatedVersionRefusesAPeerThatAdvertisesNoTools(t *testing.T) {
	// The package that owns the rule witnesses it, rather than leaving the only
	// evidence at the two client call sites. The revision is correct in every
	// case here: what is refused is a peer with nothing this client can ask for.
	for name, result := range map[string]map[string]any{
		"another capability":      {"protocolVersion": ProtocolVersion, "capabilities": map[string]any{"resources": map[string]any{}}},
		"empty capabilities":      {"protocolVersion": ProtocolVersion, "capabilities": map[string]any{}},
		"no capabilities":         {"protocolVersion": ProtocolVersion},
		"capabilities wrong type": {"protocolVersion": ProtocolVersion, "capabilities": "tools"},
	} {
		if _, err := NegotiatedVersion(result); !errors.Is(err, ErrToolsNotAdvertised) {
			t.Errorf("%s: error = %v, want ErrToolsNotAdvertised", name, err)
		}
	}

	// A peer that declares tools with no sub-options has still declared them.
	if _, err := NegotiatedVersion(map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
	}); err != nil {
		t.Fatalf("a peer advertising tools was refused: %v", err)
	}
}

func TestAnInvalidationWhileTheNotificationIsInFlightIsReportedAsAnInvalidation(t *testing.T) {
	// The window the generation guard did not cover: the revision is published
	// before the notification runs, because the notification has to carry it —
	// so a concurrent request's error path can wipe it after the publish and
	// before the headers are read. The peer then sees a notification naming no
	// revision and no session, and refuses it. Reporting that refusal would tell
	// the caller a re-initializable connection had failed permanently.
	var connection Connection
	announced := 0
	err := connection.EnsureInitialized(
		context.Background(),
		func() (string, error) { return ProtocolVersion, nil },
		func() error {
			announced++
			connection.Invalidate()
			version, session := connection.headers()
			if version != "" || session != "" {
				t.Fatalf("the invalidation left headers behind: version %q, session %q", version, session)
			}
			// What a conforming peer answers a notification with no revision.
			return errors.New("400 unsupported MCP protocol version")
		},
	)
	if !errors.Is(err, ErrConnectionInvalidated) {
		t.Fatalf("EnsureInitialized error = %v, want one wrapping ErrConnectionInvalidated", err)
	}
	if announced != 1 {
		t.Fatalf("announce ran %d times, want 1", announced)
	}
	if connection.Ready() {
		t.Fatal("the connection is ready after an invalidated handshake")
	}
	if version, session := connection.headers(); version != "" || session != "" {
		t.Fatalf("state survived: version %q, session %q", version, session)
	}
}
