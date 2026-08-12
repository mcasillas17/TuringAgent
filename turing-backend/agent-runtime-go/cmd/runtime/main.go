package main

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/agent"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/mcp"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/orchestrator"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/worker"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := orchestrator.Dial(ctx, cfg.OrchestratorGRPCAddr, cfg.InternalToken)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	providers := map[turingv1.ModelProvider]llm.Provider{
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: llm.NewOllama(cfg.OllamaBaseURL, http.DefaultClient),
	}
	if cfg.OpenAIAPIKey != "" {
		providers[turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE] = llm.NewOpenAICompatible(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, http.DefaultClient)
	}
	toolRunner := &tools.Runner{WaitApproval: func(ctx context.Context, approvalID string) (string, error) {
		return client.WaitForApprovalToken(ctx, approvalID, time.Second, cfg.ApprovalTimeout)
	}}
	toolset := &agent.GeneralAssistantTools{
		SystemMCP:          mcp.NewClient(cfg.MCPSystemBaseURL, cfg.MCPSystemToken, http.DefaultClient),
		FilesMCP:           mcp.NewClient(cfg.MCPFilesBaseURL, cfg.MCPFilesToken, http.DefaultClient),
		Runner:             toolRunner,
		MaxToolCallsPerRun: cfg.MaxToolCallsPerRun,
		ModelTimeout:       cfg.ModelTimeout,
		ToolTimeout:        cfg.ToolTimeout,
		TotalToolTimeout:   cfg.TotalToolTimeout,
	}
	executor := agent.NewGeneralAssistant(providers, client, toolset)
	runtimeWorker := worker.New(worker.Options{
		WorkerID:                 cfg.WorkerID,
		AgentID:                  turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		MaxConcurrentRuns:        cfg.MaxConcurrentRuns,
		HeartbeatInterval:        cfg.HeartbeatInterval,
		DisconnectCleanupTimeout: cfg.TotalToolTimeout,
	}, runtimeClientAdapter{client: client}, executor)
	return serve(ctx, runtimeWorker)
}

const (
	initialBackoff = 500 * time.Millisecond
	maxBackoff     = 30 * time.Second
	// A stream that served this long was healthy, so whatever ended it is a
	// fresh problem: retry promptly rather than inherit the delay an earlier bad
	// patch had climbed to.
	healthyStreamDuration = time.Minute
)

// serve keeps the worker connected.
//
// Worker.Run owns exactly one stream: it connects, registers, serves, and on the
// way out cancels active runs, drains them and stops the outbound writer.
// Retrying around it reuses that teardown as written, and leaves Run testable as
// the single-stream function every existing test drives.
func serve(ctx context.Context, runtimeWorker *worker.Worker) error {
	return serveWith(ctx, runtimeWorker.Run, healthyStreamDuration, time.After)
}

// serveWith is serve's loop over an arbitrary attempt function. healthyFor and
// sleep are injected so a test can exercise the backoff growth and the
// reset-after-a-healthy-stream branch without waiting real seconds — otherwise
// both are unreachable and could be deleted with the suite still green.
func serveWith(
	ctx context.Context,
	run func(context.Context) error,
	healthyFor time.Duration,
	sleep func(time.Duration) <-chan time.Time,
) error {
	backoff := initialBackoff
	for {
		started := time.Now()
		err := run(ctx)
		if !shouldReconnect(ctx, err) {
			if ctx.Err() != nil {
				log.Printf("runtime worker stopping: %v", ctx.Err())
			} else {
				log.Print("runtime worker stopped at the orchestrator's request")
			}
			return nil
		}
		if time.Since(started) >= healthyFor {
			backoff = initialBackoff
		}
		delay := jitter(backoff)
		log.Printf("runtime worker disconnected (%v); reconnecting in %s", err, delay.Round(time.Millisecond))
		select {
		case <-ctx.Done():
			// Shutting down mid-backoff is a clean exit, not a failure.
			return nil
		case <-sleep(delay):
		}
		backoff = nextBackoff(backoff)
	}
}

// shouldReconnect reports whether a Worker.Run return warrants another attempt.
//
// A nil error means the worker stopped deliberately — the orchestrator sent a
// shutdown command — and must not be restarted. A cancelled context means the
// process was signalled. Everything else is a transport or orchestrator problem
// the runtime is expected to ride out.
func shouldReconnect(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	// Worker.Run validates its configuration before touching the network. Those
	// failures cannot succeed on retry, and looping them forever would keep the
	// process alive, never exit non-zero, and so never let the restart policy or
	// an operator see the fault.
	if errors.Is(err, worker.ErrInvalidConfig) {
		return false
	}
	return err != nil
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}

// jitter spreads retries over [d/2, d] so a runtime and an orchestrator
// restarting together do not re-collide on a fixed rhythm. The floor keeps a
// tight failure loop from spinning. This is scheduling, not secrets.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	half := d / 2
	return half + time.Duration(rand.Int63n(int64(half)+1))
}

type runtimeClientAdapter struct{ client *orchestrator.Client }

func (a runtimeClientAdapter) ConnectWorker(ctx context.Context) (worker.RuntimeStream, error) {
	return a.client.ConnectWorker(ctx)
}
