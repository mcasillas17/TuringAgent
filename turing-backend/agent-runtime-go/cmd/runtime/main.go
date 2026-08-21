package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os/signal"
	"sort"
	"syscall"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/agent"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/mcp"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/memory"
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
	client, err := orchestrator.Dial(ctx, cfg.OrchestratorGRPCAddr, cfg.RuntimeToken)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	ollamaProvider, err := llm.NewOllamaWithLimits(
		cfg.OllamaBaseURL,
		http.DefaultClient,
		cfg.OllamaContextWindowTokens,
		cfg.OllamaMaxOutputTokens,
	)
	if err != nil {
		return fmt.Errorf("configure Ollama provider: %w", err)
	}
	ollamaProvider.WithKeepAlive(cfg.OllamaKeepAlive)
	providers := map[turingv1.ModelProvider]llm.Provider{
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: ollamaProvider,
	}
	if cfg.OpenAIAPIKey != "" {
		openAIProvider, err := llm.NewOpenAICompatibleWithLimits(
			cfg.OpenAIBaseURL,
			cfg.OpenAIAPIKey,
			http.DefaultClient,
			cfg.OpenAIContextWindowTokens,
			cfg.OpenAIMaxOutputTokens,
		)
		if err != nil {
			return fmt.Errorf("configure OpenAI-compatible provider: %w", err)
		}
		providers[turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE] = openAIProvider
	}
	toolRunner := &tools.Runner{
		WaitApproval: func(ctx context.Context, approvalID string) (string, error) {
			return client.WaitForApprovalToken(ctx, approvalID, time.Second, cfg.ApprovalTimeout)
		},
		// One budget covers waiting for the decision and waiting for the
		// orchestrator to accept the resume it permits.
		ApprovalWaitTimeout: cfg.ApprovalTimeout,
	}
	toolset := &agent.GeneralAssistantTools{
		SystemMCP: mcp.NewClient(cfg.MCPSystemBaseURL, cfg.MCPSystemToken, http.DefaultClient),
		// The orchestrator client is the Searcher: recall queries SearchMessages
		// across the user's earlier sessions. NewRecaller rather than a struct
		// literal, because an unset budget would silently recall nothing.
		Recall:             memory.NewRecaller(client),
		FilesMCP:           mcp.NewClient(cfg.MCPFilesBaseURL, cfg.MCPFilesToken, http.DefaultClient),
		Runner:             toolRunner,
		MaxToolCallsPerRun: cfg.MaxToolCallsPerRun,
		ModelTimeout:       cfg.ModelTimeout,
		ToolTimeout:        cfg.ToolTimeout,
		TotalToolTimeout:   cfg.TotalToolTimeout,
	}
	executor := agent.NewGeneralAssistant(providers, client, toolset)
	// The only place a third-party API key exists at runtime. It is read from
	// this process's environment and used to build a per-job client; nothing
	// puts it back on a job, an event, or a response.
	executor.SetExternalAgentProvider(agent.NewExternalAgentProviderFunc(
		cfg.AgentAPIKeys,
		cfg.OpenAIContextWindowTokens,
		cfg.OpenAIMaxOutputTokens,
		http.DefaultClient,
	))
	runtimeWorker := worker.New(worker.Options{
		WorkerID:                    cfg.WorkerID,
		AgentID:                     turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		MaxConcurrentRuns:           cfg.MaxConcurrentRuns,
		HeartbeatInterval:           cfg.HeartbeatInterval,
		DisconnectCleanupTimeout:    cfg.TotalToolTimeout,
		Models:                      advertisedModels(cfg),
		ExternalAgentCredentialRefs: agentCredentialRefs(cfg.AgentAPIKeys),
		SupportsExternalAgents:      len(cfg.AgentAPIKeys) > 0,
		DiscoverTools:               executor.AdvertisedTools,
	}, runtimeClientAdapter{client: client}, executor)
	return serve(ctx, runtimeWorker)
}

func agentCredentialRefs(keys map[string]string) []string {
	refs := make([]string, 0, len(keys))
	for ref := range keys {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

func advertisedModels(cfg config.Config) []*turingv1.ModelCapability {
	models := []*turingv1.ModelCapability{{
		Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:            cfg.OllamaModel,
		MaxContextTokens: int32(cfg.OllamaContextWindowTokens),
	}}
	if cfg.OpenAIAPIKey != "" {
		models = append(models, &turingv1.ModelCapability{
			Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
			Model:            cfg.OpenAIModel,
			MaxContextTokens: int32(cfg.OpenAIContextWindowTokens),
		})
	}
	return models
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

// discoverToolsFor adapts the agent's tool snapshot to the handshake. The
// orchestrator persists it as its registry and derives policy from it, so it
// must come from the same discovery the agent executes against.
type runtimeClientAdapter struct{ client *orchestrator.Client }

func (a runtimeClientAdapter) ConnectWorker(ctx context.Context) (worker.RuntimeStream, error) {
	return a.client.ConnectWorker(ctx)
}
