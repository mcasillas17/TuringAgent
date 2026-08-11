package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type smokeConfig struct {
	addr  string
	token string
}

type smokeClient struct {
	token    string
	health   turingv1.HealthServiceClient
	sessions turingv1.SessionServiceClient
	chat     turingv1.ChatServiceClient
	events   turingv1.EventServiceClient
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "gRPC smoke failed: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("grpc-smoke-client", flag.ContinueOnError)
	healthOnly := flags.Bool("health-only", false, "only run the HealthService.Check probe")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	timeout := 2 * time.Minute
	if *healthOnly {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// NewClient is lazy, so the old WithBlock behaviour (wait for the stack to
	// come up) is reproduced by Connect plus WaitForReady on the health probe
	// below. "passthrough:///" keeps the address opaque to the resolver exactly
	// as DialContext did, so dialLocalGRPC still receives host:port verbatim.
	conn, err := grpc.NewClient("passthrough:///"+cfg.addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(dialLocalGRPC))
	if err != nil {
		return fmt.Errorf("create client for %s: %w", cfg.addr, err)
	}
	conn.Connect()
	defer func() { _ = conn.Close() }()

	client := smokeClient{
		token:    cfg.token,
		health:   turingv1.NewHealthServiceClient(conn),
		sessions: turingv1.NewSessionServiceClient(conn),
		chat:     turingv1.NewChatServiceClient(conn),
		events:   turingv1.NewEventServiceClient(conn),
	}
	if err := client.checkHealth(ctx); err != nil {
		return err
	}
	if *healthOnly {
		fmt.Println("gRPC health check OK")
		return nil
	}
	return client.runFullSmoke(ctx)
}

func dialLocalGRPC(ctx context.Context, addr string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "tcp4", addr)
}

func loadConfig() (smokeConfig, error) {
	dotenv, err := readDotEnv(".env")
	if err != nil {
		return smokeConfig{}, err
	}
	lookup := func(name string) string {
		if value := os.Getenv(name); value != "" {
			return value
		}
		return dotenv[name]
	}

	port := lookup("ORCHESTRATOR_PUBLIC_PORT")
	if port == "" {
		port = "3000"
	}
	token := lookup("TURING_CLIENT_API_KEY")
	if token == "" {
		return smokeConfig{}, errors.New("TURING_CLIENT_API_KEY is required")
	}
	return smokeConfig{addr: "localhost:" + port, token: token}, nil
}

func readDotEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return parseDotEnv(file)
}

func parseDotEnv(r io.Reader) (map[string]string, error) {
	values := map[string]string{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "\"'")
		if name != "" {
			values[name] = value
		}
	}
	return values, scanner.Err()
}

func (c smokeClient) withAuth(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.token)
}

func (c smokeClient) checkHealth(ctx context.Context) error {
	// WaitForReady replaces the removed WithBlock: block until the channel is
	// ready or the caller's timeout fires, rather than failing fast while the
	// orchestrator is still starting.
	resp, err := c.health.Check(c.withAuth(ctx), &turingv1.HealthCheckRequest{}, grpc.WaitForReady(true))
	if err != nil {
		return fmt.Errorf("HealthService.Check: %w", err)
	}
	if !resp.GetOk() {
		return errors.New("HealthService.Check returned ok=false")
	}
	return nil
}

func (c smokeClient) runFullSmoke(ctx context.Context) error {
	session, err := c.sessions.CreateSession(c.withAuth(ctx), &turingv1.CreateSessionRequest{Title: "gRPC smoke test"})
	if err != nil {
		return fmt.Errorf("SessionService.CreateSession: %w", err)
	}
	sessionID := session.GetSessionId()
	if sessionID == "" {
		return errors.New("SessionService.CreateSession returned an empty session_id")
	}

	stream, err := c.chat.SendMessage(c.withAuth(ctx), &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "/tool system.time",
		ContentType:   "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
	})
	if err != nil {
		return fmt.Errorf("ChatService.SendMessage: %w", err)
	}

	streamResult, err := readChatStream(stream)
	if err != nil {
		return err
	}
	if err := streamResult.validate(); err != nil {
		return err
	}
	if err := c.validatePersistedEvents(ctx, sessionID, streamResult.runID); err != nil {
		return err
	}

	fmt.Printf("gRPC smoke OK: session=%s run=%s\n", sessionID, streamResult.runID)
	return nil
}

type chatResult struct {
	runID          string
	tokenDelta     bool
	terminalEvent  bool
	terminalFailed error
}

func readChatStream(stream turingv1.ChatService_SendMessageClient) (chatResult, error) {
	var result chatResult
	for {
		event, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, fmt.Errorf("receive ChatService.SendMessage event: %w", err)
		}
		if event.GetRunId() != "" {
			result.runID = event.GetRunId()
		}
		switch typed := event.GetEvent().(type) {
		case *turingv1.ChatStreamEvent_RunQueued:
			if typed.RunQueued.GetRunId() != "" {
				result.runID = typed.RunQueued.GetRunId()
			}
		case *turingv1.ChatStreamEvent_TokenDelta:
			if typed.TokenDelta.GetDelta() != "" {
				result.tokenDelta = true
			}
		case *turingv1.ChatStreamEvent_RunCompleted:
			result.terminalEvent = true
			if typed.RunCompleted.GetRunId() != "" {
				result.runID = typed.RunCompleted.GetRunId()
			}
			return result, nil
		case *turingv1.ChatStreamEvent_RunFailed:
			result.terminalEvent = true
			if typed.RunFailed.GetRunId() != "" {
				result.runID = typed.RunFailed.GetRunId()
			}
			result.terminalFailed = fmt.Errorf("run_failed code=%q message=%q", typed.RunFailed.GetCode(), typed.RunFailed.GetMessage())
			return result, nil
		}
	}
	return result, nil
}

func (r chatResult) validate() error {
	var problems []string
	if r.runID == "" {
		problems = append(problems, "no run_id was observed")
	}
	if !r.tokenDelta {
		problems = append(problems, "no token_delta event was observed")
	}
	if !r.terminalEvent {
		problems = append(problems, "no run_completed or run_failed event was observed")
	}
	if r.terminalFailed != nil {
		problems = append(problems, r.terminalFailed.Error())
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (c smokeClient) validatePersistedEvents(ctx context.Context, sessionID string, runID string) error {
	resp, err := c.events.ListEvents(c.withAuth(ctx), &turingv1.ListEventsRequest{
		SessionId:     sessionID,
		AfterSequence: 0,
		Limit:         500,
	})
	if err != nil {
		return fmt.Errorf("EventService.ListEvents: %w", err)
	}
	if len(resp.GetEvents()) == 0 {
		return errors.New("EventService.ListEvents returned no events")
	}

	var tokenDelta bool
	var terminalEvent bool
	for _, event := range resp.GetEvents() {
		if runID != "" && event.GetRunId() != "" && event.GetRunId() != runID {
			continue
		}
		switch event.GetType() {
		case turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA:
			tokenDelta = true
		case turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_COMPLETED:
			terminalEvent = true
		case turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED:
			return errors.New("EventService.ListEvents observed agent.run.failed")
		}
	}

	var problems []string
	if !tokenDelta {
		problems = append(problems, "EventService.ListEvents returned no message_delta event")
	}
	if !terminalEvent {
		problems = append(problems, "EventService.ListEvents returned no terminal run event")
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}
