package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/memory"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Client struct {
	conn      *grpc.ClientConn
	token     string
	runtime   turingv1.RuntimeServiceClient
	sessions  turingv1.SessionServiceClient
	approvals turingv1.ApprovalServiceClient
}

const defaultApprovalWaitTimeout = 71 * time.Second

// Dial builds the orchestrator client. The dial is lazy: the connection is
// established on the first RPC, and the worker loop already retries, so no
// blocking wait is needed here.
//
// "passthrough:///" preserves DialContext's resolver behaviour — addr is the
// Docker service name in ORCHESTRATOR_GRPC_ADDR and must reach the dialer
// verbatim rather than going through NewClient's default DNS resolver.
func Dial(_ context.Context, addr string, token string) (*Client, error) {
	conn, err := grpc.NewClient("passthrough:///"+addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	// NewClient starts the channel IDLE; DialContext connected eagerly in the
	// background. Connect() keeps startup behaviour unchanged so the first RPC
	// does not also pay for the handshake.
	conn.Connect()
	return New(conn, token), nil
}

func New(conn *grpc.ClientConn, token string) *Client {
	return &Client{
		conn:      conn,
		token:     token,
		runtime:   turingv1.NewRuntimeServiceClient(conn),
		sessions:  turingv1.NewSessionServiceClient(conn),
		approvals: turingv1.NewApprovalServiceClient(conn),
	}
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) ConnectWorker(ctx context.Context) (turingv1.RuntimeService_ConnectWorkerClient, error) {
	return c.runtime.ConnectWorker(c.withAuth(ctx))
}

func (c *Client) FetchMessages(ctx context.Context, sessionID string, beforeMessageID string) ([]llm.ChatMessage, error) {
	const historyLimit = 50
	resp, err := c.sessions.ListMessages(c.withAuth(ctx), &turingv1.ListMessagesRequest{
		SessionId:       sessionID,
		BeforeMessageId: beforeMessageID,
		Limit:           int32(historyLimit),
	})
	if err != nil {
		return nil, err
	}
	messages := messagesBeforeAnchor(resp.GetMessages(), beforeMessageID)
	out := make([]llm.ChatMessage, 0, len(messages))
	for _, message := range messages {
		role, ok := chatRole(message.GetRole())
		if !ok {
			continue
		}
		out = append(out, llm.ChatMessage{
			MessageID: message.GetMessageId(),
			Role:      role,
			Content:   message.GetContent(),
		})
	}
	if len(out) > historyLimit {
		out = out[len(out)-historyLimit:]
	}
	return out, nil
}

// cmd/runtime passes this client as the recaller's Searcher. Asserted here so a
// change to either side is caught at compile time in this package, rather than
// only where they are wired together.
var _ memory.Searcher = (*Client)(nil)

// Note the orchestrator treats the query as an exact phrase, so callers should
// pass a single term rather than a sentence (see the memory package).
func (c *Client) SearchMessages(
	ctx context.Context,
	query string,
	sessionID string,
	excludedSessionID string,
	limit int,
) ([]memory.Excerpt, error) {
	resp, err := c.sessions.SearchMessages(c.withAuth(ctx), &turingv1.SearchMessagesRequest{
		Query:            query,
		SessionId:        sessionID,
		Limit:            int32(limit),
		ExcludeSessionId: excludedSessionID,
	})
	if err != nil {
		return nil, err
	}
	messages := resp.GetMessages()
	out := make([]memory.Excerpt, 0, len(messages))
	for _, message := range messages {
		role, ok := chatRole(message.GetRole())
		if !ok {
			continue
		}
		out = append(out, memory.Excerpt{
			// Carry the row id through: recall dedupes on it, and two distinct rows
			// can share a session, a timestamp and a body.
			MessageID: message.GetMessageId(),
			SessionID: message.GetSessionId(),
			Role:      role,
			Content:   message.GetContent(),
			CreatedAt: message.GetCreatedAt().AsTime(),
		})
	}
	return out, nil
}

func messagesBeforeAnchor(messages []*turingv1.Message, beforeMessageID string) []*turingv1.Message {
	if beforeMessageID == "" {
		return messages
	}
	for index, message := range messages {
		if message.GetMessageId() == beforeMessageID {
			return messages[:index]
		}
	}
	return messages
}

func (c *Client) GetApprovalState(ctx context.Context, approvalID string) (*turingv1.RuntimeApprovalState, error) {
	return c.approvals.GetApprovalForRuntime(c.withAuth(ctx), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
}

func (c *Client) WaitForApprovalToken(ctx context.Context, approvalID string, pollInterval time.Duration, timeout time.Duration) (string, error) {
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	if timeout <= 0 {
		timeout = defaultApprovalWaitTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		state, err := c.GetApprovalState(ctx, approvalID)
		if err != nil {
			if !isTransientApprovalPollError(ctx, err) {
				return "", err
			}
		} else {
			switch state.GetStatus() {
			case turingv1.ApprovalStatus_APPROVAL_STATUS_APPROVED:
				if state.GetApprovalToken() == "" {
					return "", errors.New("approval token is missing")
				}
				return state.GetApprovalToken(), nil
			case turingv1.ApprovalStatus_APPROVAL_STATUS_DENIED:
				return "", terminalApprovalError{message: "approval denied"}
			case turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED:
				return "", terminalApprovalError{message: "approval expired"}
			case turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED:
				return "", errors.New("approval already consumed")
			}
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("approval timed out: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func isTransientApprovalPollError(ctx context.Context, err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.ResourceExhausted, codes.Aborted:
		return true
	case codes.DeadlineExceeded:
		return ctx.Err() == nil
	default:
		return false
	}
}

type terminalApprovalError struct {
	message string
}

func (e terminalApprovalError) Error() string     { return e.message }
func (e terminalApprovalError) RunTerminal() bool { return true }

func (c *Client) ConsumeApproval(ctx context.Context, approvalID string) error {
	_, err := c.approvals.ConsumeApproval(c.withAuth(ctx), &turingv1.ConsumeApprovalRequest{ApprovalId: approvalID})
	return err
}

func (c *Client) withAuth(ctx context.Context) context.Context {
	if c.token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.token)
}

func chatRole(role turingv1.MessageRole) (string, bool) {
	switch role {
	case turingv1.MessageRole_MESSAGE_ROLE_SYSTEM:
		return "system", true
	case turingv1.MessageRole_MESSAGE_ROLE_USER:
		return "user", true
	case turingv1.MessageRole_MESSAGE_ROLE_ASSISTANT:
		return "assistant", true
	default:
		return "", false
	}
}
