package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/memory"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeSessions implements turingv1.SessionServiceClient by embedding the
// interface: only SearchMessages is exercised here, and any other call this
// package starts making will panic loudly rather than silently returning zeroes.
type fakeSessions struct {
	turingv1.SessionServiceClient

	gotCtx context.Context // recorded so the test can assert on the outgoing auth metadata
	gotReq *turingv1.SearchMessagesRequest
	resp   *turingv1.SearchMessagesResponse
	err    error
}

func (f *fakeSessions) SearchMessages(ctx context.Context, in *turingv1.SearchMessagesRequest, _ ...grpc.CallOption) (*turingv1.SearchMessagesResponse, error) {
	f.gotCtx = ctx
	f.gotReq = in
	return f.resp, f.err
}

// The row id is the thing recall dedupes on, and it was in fact dropped here
// once. Pin the whole mapping, field by field: the compile-time
// `var _ memory.Searcher = (*Client)(nil)` assertion only checks the signature,
// so nothing else notices when a field stops being carried across.
func TestSearchMessagesMapsEveryFieldIncludingTheRowID(t *testing.T) {
	createdAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	fake := &fakeSessions{resp: &turingv1.SearchMessagesResponse{Messages: []*turingv1.Message{{
		MessageId: "msg-1",
		SessionId: "session-1",
		Role:      turingv1.MessageRole_MESSAGE_ROLE_USER,
		Content:   "I fly out on the 14th",
		CreatedAt: timestamppb.New(createdAt),
	}}}}
	client := &Client{sessions: fake, token: "secret-token"}

	got, err := client.SearchMessages(context.Background(), "flight", 10)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	want := []memory.Excerpt{{
		MessageID: "msg-1",
		SessionID: "session-1",
		Role:      "user",
		Content:   "I fly out on the 14th",
		CreatedAt: createdAt,
	}}
	if len(got) != len(want) {
		t.Fatalf("got %d excerpts, want %d", len(got), len(want))
	}
	if got[0].MessageID != want[0].MessageID {
		t.Errorf("MessageID = %q, want %q (recall dedupes on the row id)", got[0].MessageID, want[0].MessageID)
	}
	if got[0].SessionID != want[0].SessionID {
		t.Errorf("SessionID = %q, want %q", got[0].SessionID, want[0].SessionID)
	}
	if got[0].Role != want[0].Role {
		t.Errorf("Role = %q, want %q", got[0].Role, want[0].Role)
	}
	if got[0].Content != want[0].Content {
		t.Errorf("Content = %q, want %q", got[0].Content, want[0].Content)
	}
	if !got[0].CreatedAt.Equal(want[0].CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got[0].CreatedAt, want[0].CreatedAt)
	}
}

// The empty SessionId is the whole point: scoping the search to the current
// session would recall only what the request already contains.
func TestSearchMessagesRequestsAllSessionsWithAuth(t *testing.T) {
	fake := &fakeSessions{resp: &turingv1.SearchMessagesResponse{}}
	client := &Client{sessions: fake, token: "secret-token"}

	if _, err := client.SearchMessages(context.Background(), "flight", 7); err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if fake.gotReq.GetQuery() != "flight" {
		t.Errorf("Query = %q, want %q", fake.gotReq.GetQuery(), "flight")
	}
	if fake.gotReq.GetSessionId() != "" {
		t.Errorf("SessionId = %q, want empty so the search spans earlier sessions", fake.gotReq.GetSessionId())
	}
	if fake.gotReq.GetLimit() != 7 {
		t.Errorf("Limit = %d, want 7", fake.gotReq.GetLimit())
	}
	md, ok := metadata.FromOutgoingContext(fake.gotCtx)
	if !ok || len(md.Get("authorization")) != 1 || md.Get("authorization")[0] != "Bearer secret-token" {
		t.Errorf("authorization metadata = %v, want [Bearer secret-token]", md.Get("authorization"))
	}
}

// Tool and unspecified rows carry no role the model can be shown, and a nil
// timestamp must not panic on the way through.
func TestSearchMessagesDropsRolesItCannotRender(t *testing.T) {
	fake := &fakeSessions{resp: &turingv1.SearchMessagesResponse{Messages: []*turingv1.Message{
		{MessageId: "a", Role: turingv1.MessageRole_MESSAGE_ROLE_TOOL, Content: "tool output"},
		{MessageId: "b", Role: turingv1.MessageRole_MESSAGE_ROLE_UNSPECIFIED, Content: "mystery"},
		{MessageId: "c", Role: turingv1.MessageRole_MESSAGE_ROLE_ASSISTANT, Content: "kept"},
	}}}
	client := &Client{sessions: fake}

	got, err := client.SearchMessages(context.Background(), "term", 10)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(got) != 1 || got[0].MessageID != "c" || got[0].Role != "assistant" {
		t.Fatalf("got %+v, want only the assistant message", got)
	}
	// A missing timestamp must not panic. AsTime() on a nil Timestamp is the Unix
	// epoch, which is harmless here: rank() sorts it last and Render dates it
	// 1970-01-01. Recorded so a change in that degradation is a visible one.
	if !got[0].CreatedAt.Equal(time.Unix(0, 0).UTC()) {
		t.Errorf("CreatedAt = %v, want the Unix epoch for a message with no timestamp", got[0].CreatedAt)
	}
}

// Recall treats an error as "stop searching", so the error has to arrive rather
// than being flattened into an empty result set that looks like "nothing found".
func TestSearchMessagesReturnsTheRPCError(t *testing.T) {
	wantErr := errors.New("boom")
	client := &Client{sessions: &fakeSessions{err: wantErr}}

	got, err := client.SearchMessages(context.Background(), "term", 10)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if got != nil {
		t.Errorf("got %+v, want nil excerpts on error", got)
	}
}

func TestFetchMessagesPreservesChronologicalOrderAndExcludesOnlyExactIDs(t *testing.T) {
	sessions := &messageListClient{messages: []*turingv1.Message{
		{MessageId: "msg_system", Role: turingv1.MessageRole_MESSAGE_ROLE_SYSTEM, Content: "instructions"},
		{MessageId: "msg_older_user", Role: turingv1.MessageRole_MESSAGE_ROLE_USER, Content: "repeat me"},
		{MessageId: "msg_older_empty_assistant", Role: turingv1.MessageRole_MESSAGE_ROLE_ASSISTANT, Content: ""},
		{MessageId: "msg_current_user", Role: turingv1.MessageRole_MESSAGE_ROLE_USER, Content: "repeat me"},
		{MessageId: "msg_current_assistant", Role: turingv1.MessageRole_MESSAGE_ROLE_ASSISTANT, Content: ""},
	}}
	client := &Client{sessions: sessions}

	got, err := client.FetchMessages(
		context.Background(),
		"session_1",
		"msg_current_user",
		"msg_current_assistant",
	)
	if err != nil {
		t.Fatalf("FetchMessages returned error: %v", err)
	}
	want := []llm.ChatMessage{
		{Role: "system", Content: "instructions"},
		{Role: "user", Content: "repeat me"},
		{Role: "assistant", Content: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FetchMessages = %#v, want %#v", got, want)
	}
	if got := sessions.lastRequest.GetLimit(); got != 52 {
		t.Fatalf("ListMessages limit = %d, want 52", got)
	}
}

func TestFetchMessagesRetainsFiftyEntriesAfterExcludingCurrentTurn(t *testing.T) {
	messages := make([]*turingv1.Message, 0, 52)
	for i := 0; i < 52; i++ {
		messages = append(messages, &turingv1.Message{
			MessageId: fmt.Sprintf("msg_%02d", i),
			Role:      turingv1.MessageRole_MESSAGE_ROLE_USER,
			Content:   fmt.Sprintf("message %02d", i),
		})
	}
	sessions := &messageListClient{messages: messages}
	client := &Client{sessions: sessions}

	got, err := client.FetchMessages(context.Background(), "session_1", "msg_50", "msg_51")
	if err != nil {
		t.Fatalf("FetchMessages returned error: %v", err)
	}
	if len(got) != 50 {
		t.Fatalf("FetchMessages count = %d, want 50", len(got))
	}
	for i, message := range got {
		want := fmt.Sprintf("message %02d", i)
		if message.Role != "user" || message.Content != want {
			t.Fatalf("FetchMessages[%d] = %#v, want user %q", i, message, want)
		}
	}
	if got := sessions.lastRequest.GetLimit(); got != 52 {
		t.Fatalf("ListMessages limit = %d, want 52", got)
	}
}

func TestFetchMessagesDeduplicatesNonemptyExclusionsAndKeepsMostRecentFifty(t *testing.T) {
	messages := make([]*turingv1.Message, 0, 51)
	for i := 0; i < 51; i++ {
		messages = append(messages, &turingv1.Message{
			MessageId: fmt.Sprintf("msg_%02d", i),
			Role:      turingv1.MessageRole_MESSAGE_ROLE_ASSISTANT,
			Content:   fmt.Sprintf("message %02d", i),
		})
	}
	sessions := &messageListClient{messages: messages}
	client := &Client{sessions: sessions}

	got, err := client.FetchMessages(context.Background(), "session_1", "", "msg_missing", "msg_missing", "")
	if err != nil {
		t.Fatalf("FetchMessages returned error: %v", err)
	}
	if len(got) != 50 {
		t.Fatalf("FetchMessages count = %d, want 50", len(got))
	}
	for i, message := range got {
		want := fmt.Sprintf("message %02d", i+1)
		if message.Role != "assistant" || message.Content != want {
			t.Fatalf("FetchMessages[%d] = %#v, want assistant %q", i, message, want)
		}
	}
	if got := sessions.lastRequest.GetLimit(); got != 51 {
		t.Fatalf("ListMessages limit = %d, want 51", got)
	}
}

func TestWaitForApprovalTokenMarksDeniedAndExpiredRunsTerminal(t *testing.T) {
	tests := []struct {
		name    string
		status  turingv1.ApprovalStatus
		message string
	}{
		{name: "denied", status: turingv1.ApprovalStatus_APPROVAL_STATUS_DENIED, message: "approval denied"},
		{name: "expired", status: turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED, message: "approval expired"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &Client{approvals: &approvalStateClient{status: test.status}}

			_, err := client.WaitForApprovalToken(context.Background(), "approval_1", time.Millisecond, time.Second)

			if err == nil || err.Error() != test.message {
				t.Fatalf("WaitForApprovalToken error = %v, want %q", err, test.message)
			}
			var terminal interface{ RunTerminal() bool }
			if !errors.As(err, &terminal) || !terminal.RunTerminal() {
				t.Fatalf("WaitForApprovalToken error = %T %v, want terminal-run error", err, err)
			}
		})
	}
}

func TestWaitForApprovalTokenDoesNotAssumeConsumedApprovalIsTerminal(t *testing.T) {
	client := &Client{approvals: &approvalStateClient{
		status: turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED,
	}}

	_, err := client.WaitForApprovalToken(context.Background(), "approval_1", time.Millisecond, time.Second)

	if err == nil || err.Error() != "approval already consumed" {
		t.Fatalf("WaitForApprovalToken error = %v, want consumed error", err)
	}
	var terminal interface{ RunTerminal() bool }
	if errors.As(err, &terminal) && terminal.RunTerminal() {
		t.Fatalf("consumed approval error = %T %v, must not be terminal-run error", err, err)
	}
}

func TestWaitForApprovalTokenReturnsLazyExpiryBeforeWaitTimeout(t *testing.T) {
	clientState := &approvalStateClient{statuses: []turingv1.ApprovalStatus{
		turingv1.ApprovalStatus_APPROVAL_STATUS_PENDING,
		turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED,
	}}
	client := &Client{approvals: clientState}

	_, err := client.WaitForApprovalToken(context.Background(), "approval_1", time.Millisecond, time.Hour)

	if err == nil || err.Error() != "approval expired" {
		t.Fatalf("WaitForApprovalToken error = %v, want approval expired", err)
	}
	var terminal interface{ RunTerminal() bool }
	if !errors.As(err, &terminal) || !terminal.RunTerminal() {
		t.Fatalf("WaitForApprovalToken error = %T %v, want terminal-run error", err, err)
	}
	if got := clientState.callCount(); got != 2 {
		t.Fatalf("GetApprovalForRuntime calls = %d, want pending then expired", got)
	}
}

func TestWaitForApprovalTokenContextDeadlineIsNotTerminalDenial(t *testing.T) {
	client := &Client{approvals: &approvalStateClient{status: turingv1.ApprovalStatus_APPROVAL_STATUS_PENDING}}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := client.WaitForApprovalToken(ctx, "approval_1", time.Hour, time.Hour)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitForApprovalToken error = %v, want deadline exceeded", err)
	}
	var terminal interface{ RunTerminal() bool }
	if errors.As(err, &terminal) && terminal.RunTerminal() {
		t.Fatalf("WaitForApprovalToken error = %T %v, must not be terminal denial", err, err)
	}
}

type messageListClient struct {
	turingv1.SessionServiceClient
	messages    []*turingv1.Message
	lastRequest *turingv1.ListMessagesRequest
}

func (c *messageListClient) ListMessages(
	_ context.Context,
	request *turingv1.ListMessagesRequest,
	_ ...grpc.CallOption,
) (*turingv1.ListMessagesResponse, error) {
	c.lastRequest = request
	return &turingv1.ListMessagesResponse{Messages: c.messages}, nil
}

type approvalStateClient struct {
	mu       sync.Mutex
	status   turingv1.ApprovalStatus
	statuses []turingv1.ApprovalStatus
	calls    int
}

func (c *approvalStateClient) GetApprovalForRuntime(
	context.Context,
	*turingv1.GetApprovalForRuntimeRequest,
	...grpc.CallOption,
) (*turingv1.RuntimeApprovalState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	status := c.status
	if len(c.statuses) > 0 {
		status = c.statuses[0]
		c.statuses = c.statuses[1:]
	}
	return &turingv1.RuntimeApprovalState{Status: status}, nil
}

func (c *approvalStateClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (*approvalStateClient) ApproveApproval(
	context.Context,
	*turingv1.ApproveApprovalRequest,
	...grpc.CallOption,
) (*turingv1.ApprovalResponse, error) {
	panic("unexpected call")
}

func (*approvalStateClient) DenyApproval(
	context.Context,
	*turingv1.DenyApprovalRequest,
	...grpc.CallOption,
) (*turingv1.ApprovalResponse, error) {
	panic("unexpected call")
}

func (*approvalStateClient) ConsumeApproval(
	context.Context,
	*turingv1.ConsumeApprovalRequest,
	...grpc.CallOption,
) (*turingv1.ApprovalResponse, error) {
	panic("unexpected call")
}
