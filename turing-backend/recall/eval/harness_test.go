package eval

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	runtimekit "github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/testkit"
	storekit "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/testkit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

func connectStore(t testing.TB, store *storekit.RecallStore) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := store.Server()
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///recall-eval",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

type caseHarness struct {
	store *storekit.RecallStore
	conn  *grpc.ClientConn
}

func openCaseStore(ctx context.Context, path string, cleanup func(func())) (*storekit.RecallStore, error) {
	store, err := storekit.OpenRecallStore(ctx, path)
	if err != nil {
		return nil, err
	}
	// Capture this successful handle, never the caller's reassigned variable.
	cleanup(func() { _ = store.Close() })
	return store, nil
}

func newCaseHarness(t testing.TB, c corpus, test fixtureCase) caseHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "recall.sqlite")
	store, err := openCaseStore(ctx, path, t.Cleanup)
	if err != nil {
		t.Fatal(err)
	}
	var sessions []storekit.RecallSession
	var messages []storekit.RecallMessage
	for _, s := range c.Sessions {
		at, _ := utc(s.At)
		sessions = append(sessions, storekit.RecallSession{ID: s.ID, At: at})
	}
	for _, m := range c.Messages {
		at, _ := utc(m.At)
		messages = append(messages, storekit.RecallMessage{ID: m.ID, SessionID: m.Session, Role: m.Role, Content: m.Body, At: at})
	}
	at, _ := utc(test.At)
	messages = append(messages, storekit.RecallMessage{ID: test.Anchor, SessionID: test.CurrentSession, Role: "user", Content: test.Query, At: at})
	if err := store.Seed(ctx, sessions, messages); err != nil {
		t.Fatal(err)
	}
	// First prove both projections could read the rows being withdrawn.
	for _, hits := range []bool{false, true} {
		ids, err := store.SearchIDs(ctx, "visibility", "", "", hits)
		if err != nil || !slices.Contains(ids, "msg_private_pending") || !slices.Contains(ids, "msg_private_deleted") {
			t.Fatalf("withdrawal precondition: ids=%v err=%v", ids, err)
		}
	}
	for _, s := range c.Sessions {
		switch s.State {
		case "archived":
			if err := store.Archive(ctx, s.ID); err != nil {
				t.Fatal(err)
			}
		case "deleting", "deleted":
			if err := store.BeginWithdrawal(ctx, s.ID); err != nil {
				t.Fatal(err)
			}
			if s.State == "deleted" {
				state, err := store.Delete(ctx, s.ID)
				if err != nil || state != "SESSION_DELETION_STATE_COMPLETED" {
					t.Fatalf("delete: %s %v", state, err)
				}
			}
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = openCaseStore(ctx, path, t.Cleanup)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range c.Sessions {
		if s.State == "deleting" || s.State == "deleted" {
			state, err := store.DeletionState(ctx, s.ID)
			want := "quiescing"
			if s.State == "deleted" {
				want = "completed"
			}
			if err != nil || state != want {
				t.Fatalf("reopened withdrawal: %s %v, want %s", state, err, want)
			}
		}
	}
	return caseHarness{store: store, conn: connectStore(t, store)}
}

func caseJob(test fixtureCase) *turingv1.AgentJob {
	return &turingv1.AgentJob{SessionId: test.CurrentSession, UserMessageId: test.Anchor, UserText: test.Query,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "fixture-model"}
}

func runCase(t testing.TB, c corpus, test fixtureCase) observation {
	t.Helper()
	h := newCaseHarness(t, c, test)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	o := observation{RPC: []string{}, Legacy: []string{}, Snippets: map[string]string{}, Bodies: map[string]string{}}
	client := turingv1.NewSessionServiceClient(h.conn)
	req := &turingv1.SearchMessagesRequest{Query: test.Phrase, SessionId: test.Scope, ExcludeSessionId: test.ExcludeSession, Limit: 5,
		ResponseFormat: turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_HITS}
	hits, err := client.SearchMessages(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	req.ResponseFormat = turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES
	legacy, err := client.SearchMessages(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits.Messages) != 0 || len(legacy.Hits) != 0 || len(hits.Hits) != len(legacy.Messages) {
		t.Fatal("RPC response projection shapes disagree")
	}
	for i, hit := range hits.Hits {
		id := hit.GetMessage().GetMessageId()
		o.RPC = append(o.RPC, id)
		o.Snippets[id] = hit.Snippet
		o.Bodies[id] = hit.GetMessage().GetContent()
		if !proto.Equal(hit.Message, legacy.Messages[i]) {
			t.Fatal("RPC projections disagree on message payload/order")
		}
		if len(hit.Snippet) > 800 || utf8.RuneCountInString(hit.Snippet) > 200 || strings.ContainsAny(hit.Snippet, "\r\n") {
			t.Fatal("RPC snippet bound/framing")
		}
	}
	for _, message := range legacy.Messages {
		o.Legacy = append(o.Legacy, message.MessageId)
	}
	o.RepositoryHits, err = h.store.SearchIDs(ctx, test.Phrase, test.Scope, test.ExcludeSession, true)
	if err != nil {
		t.Fatal(err)
	}
	o.RepositoryLegacy, err = h.store.SearchIDs(ctx, test.Phrase, test.Scope, test.ExcludeSession, false)
	if err != nil {
		t.Fatal(err)
	}
	o.Execution, err = runtimekit.ExecuteRecall(ctx, h.conn, caseJob(test), test.Window)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func cloneObservation(t testing.TB, o observation) observation {
	t.Helper()
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	var result observation
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestFailedReopenKeepsOriginalCleanup(t *testing.T) {
	var cleanups []func()
	register := func(cleanup func()) { cleanups = append(cleanups, cleanup) }
	path := filepath.Join(t.TempDir(), "recall.sqlite")
	ctx := context.Background()
	store, err := openCaseStore(ctx, path, register)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	store, err = openCaseStore(cancelled, path, register)
	if err == nil || store != nil {
		t.Fatal("cancelled reopen must fail without returning a store")
	}
	if len(cleanups) != 1 {
		t.Fatal("failed reopen registered a cleanup for an invalid handle")
	}
	// Cleanup must still refer to the successfully opened handle, not the
	// caller's now-nil variable. A nil dereference here fails this test.
	cleanups[0]()
}

func TestRealRecallAdapters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "recall.sqlite")
	store, err := openCaseStore(ctx, path, t.Cleanup)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := store.Seed(ctx,
		[]storekit.RecallSession{{ID: "ses_old", At: at}, {ID: "ses_now", At: at}},
		[]storekit.RecallMessage{
			{ID: "msg_fact", SessionID: "ses_old", Role: "user", Content: "quartz identifier QX771", At: at},
			{ID: "msg_anchor", SessionID: "ses_now", Role: "user", Content: "quartz", At: at.Add(time.Hour)},
		}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = openCaseStore(ctx, path, t.Cleanup)
	if err != nil {
		t.Fatal(err)
	}
	conn := connectStore(t, store)
	job := &turingv1.AgentJob{SessionId: "ses_now", UserMessageId: "msg_anchor", UserText: "quartz",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "fixture-model"}
	got, err := runtimekit.ExecuteRecall(ctx, conn, job, 32768)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestCount != 1 || got.Queries != 2 || got.Rows != 2 || len(got.Passes) != 1 {
		t.Fatalf("real execution observations: %+v", got)
	}
	pass := got.Passes[0]
	if len(pass.Selected) != 1 || pass.Selected[0].MessageID != "msg_fact" ||
		len(pass.InContext) != 1 || pass.InContext[0].MessageID != "msg_anchor" {
		t.Fatalf("exact identity or admitted context lost: %+v", pass)
	}
	if !strings.Contains(pass.Rendered, "QX771") || got.PromptTokens <= got.PromptBytes || got.UsageReported {
		t.Fatalf("render, real wire estimator, or absent provider usage: %+v", got)
	}
	if !reflect.DeepEqual(got.Request[0].Content, pass.Rendered) {
		t.Fatal("render never reached actual StreamChat")
	}
}
