package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type Client struct {
	conn      *grpc.ClientConn
	token     string
	runtime   turingv1.RuntimeServiceClient
	sessions  turingv1.SessionServiceClient
	approvals turingv1.ApprovalServiceClient
}

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

func (c *Client) FetchMessages(ctx context.Context, sessionID string) ([]llm.ChatMessage, error) {
	resp, err := c.sessions.ListMessages(c.withAuth(ctx), &turingv1.ListMessagesRequest{SessionId: sessionID, Limit: 50})
	if err != nil {
		return nil, err
	}
	messages := resp.GetMessages()
	out := make([]llm.ChatMessage, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		role, ok := chatRole(message.GetRole())
		if !ok {
			continue
		}
		out = append(out, llm.ChatMessage{Role: role, Content: message.GetContent()})
	}
	return out, nil
}

func (c *Client) GetApprovalState(ctx context.Context, approvalID string) (*turingv1.RuntimeApprovalState, error) {
	return c.approvals.GetApprovalForRuntime(c.withAuth(ctx), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
}

func (c *Client) WaitForApprovalToken(ctx context.Context, approvalID string, pollInterval time.Duration, timeout time.Duration) (string, error) {
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	if timeout <= 0 {
		timeout = 65 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		state, err := c.GetApprovalState(ctx, approvalID)
		if err != nil {
			return "", err
		}
		switch state.GetStatus() {
		case turingv1.ApprovalStatus_APPROVAL_STATUS_APPROVED:
			if state.GetApprovalToken() == "" {
				return "", errors.New("approval token is missing")
			}
			return state.GetApprovalToken(), nil
		case turingv1.ApprovalStatus_APPROVAL_STATUS_DENIED:
			return "", errors.New("approval denied")
		case turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED:
			return "", errors.New("approval expired")
		case turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED:
			return "", errors.New("approval already consumed")
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("approval timed out: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

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
