package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/internal/servicecmd"
)

// The probe's whole input. It is fixed text and no user data.
const (
	probeToolName   = "turing_engine_check"
	streamPrompt    = "Reply with the single word: ready"
	toolPrompt      = `Call the turing_engine_check tool with status "ok". Do not answer with text.`
	streamMaxTokens = 64
	toolMaxTokens   = 256
)

const (
	stepCount       = 4
	maxListingBytes = 4 << 20
	maxDetailRunes  = 200
	aliasNote       = "TURING_LOCAL_MODEL is not listed; treating it as an alias"
)

// What a step may take. The completions get far longer than the listings
// because a large model may still be loading when the first one arrives. They
// are variables so a test can shorten them.
var (
	listingTimeout    = 10 * time.Second
	completionTimeout = 300 * time.Second
)

// failure is a step's refusal: what was wrong, and what to change.
type failure struct{ reason, fix string }

func (f failure) Error() string { return f.reason + "; " + f.fix }

// engineProbe is the configured local engine, as the four steps see it.
type engineProbe struct {
	engine   string
	baseURL  string
	apiKey   string
	model    string
	client   *http.Client
	provider llm.Provider
}

// checkEngine proves the configured local engine is usable for Turing in four
// steps, stopping at the first that fails. Unlike check-config it reaches the
// network, but only the engine: it never dials the orchestrator.
func checkEngine(environ []string, progress io.Writer) ([]string, error) {
	env := servicecmd.EnvMap(environ)
	cfg, err := config.LoadFromEnv(func(key string) string { return env[key] })
	if err != nil {
		return nil, err
	}
	client := newEngineClient()
	defer client.CloseIdleConnections()
	provider, err := llm.NewLocal(llm.LocalOptions{
		Engine: cfg.LocalEngine, BaseURL: cfg.LocalEngineURL, APIKey: cfg.LocalEngineAPIKey,
		KeepAlive: cfg.OllamaKeepAlive, Client: client,
		ContextWindowTokens: cfg.LocalContextWindowTokens, MaxOutputTokens: cfg.LocalMaxOutputTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("configure the local engine: %w", err)
	}
	probe := &engineProbe{
		engine:   cfg.LocalEngine,
		baseURL:  strings.TrimRight(cfg.LocalEngineURL, "/"),
		apiKey:   cfg.LocalEngineAPIKey,
		model:    cfg.LocalModel,
		client:   backendegress.NoRedirectClient(client),
		provider: provider,
	}
	steps := []struct {
		name    string
		timeout time.Duration
		run     func(context.Context) (string, error)
	}{
		{"reachable", listingTimeout, probe.reachable},
		{"model present", listingTimeout, probe.modelPresent},
		{"streaming", completionTimeout, probe.streaming},
		{"tool calling", completionTimeout, probe.toolCalling},
	}
	for i, step := range steps {
		ctx, cancel := context.WithTimeout(context.Background(), step.timeout)
		note, err := step.run(ctx)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("step %d/%d (%s) failed: %w", i+1, stepCount, step.name, err)
		}
		if note != "" {
			note = " (" + note + ")"
		}
		_, _ = fmt.Fprintf(progress, "%d/%d %s: ok%s\n", i+1, stepCount, step.name, note)
	}
	return cfg.DeprecatedKeys, nil
}

// newEngineClient reaches the engine directly: a proxy configured for the
// container's other traffic must not stand between the runtime and an engine
// on this machine. It has no client-wide timeout, since a completion is
// bounded by its step's context.
func newEngineClient() *http.Client {
	transport := &http.Transport{}
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	}
	transport.Proxy = nil
	return &http.Client{Transport: transport}
}

// reachable asks the engine to answer at all.
func (p *engineProbe) reachable(ctx context.Context) (string, error) {
	path := "/models"
	if p.engine == llm.LocalEngineOllama {
		path = "/api/version"
	}
	_, err := p.get(ctx, path)
	return "", err
}

// modelPresent checks the engine lists TURING_LOCAL_MODEL.
func (p *engineProbe) modelPresent(ctx context.Context) (string, error) {
	path := "/models"
	if p.engine == llm.LocalEngineOllama {
		path = "/api/tags"
	}
	body, err := p.get(ctx, path)
	if err != nil {
		return "", err
	}
	names, err := listedNames(p.engine, body)
	if err != nil {
		return "", failure{
			reason: "the engine's model list is not what a " + p.engine + " server sends",
			fix:    "check that TURING_LOCAL_ENGINE matches the server at TURING_LOCAL_ENGINE_URL",
		}
	}
	listed, alias := modelListed(p.engine, p.model, names)
	switch {
	case listed:
		return "", nil
	case alias:
		return aliasNote, nil
	case p.engine == llm.LocalEngineOllama:
		return "", failure{
			reason: fmt.Sprintf("the model %q is not installed in Ollama", p.model),
			fix:    fmt.Sprintf("run ollama pull %s, or set TURING_LOCAL_MODEL to a model ollama list shows", p.model),
		}
	default:
		return "", failure{
			reason: "the engine lists no models",
			fix:    "load a model in the server and run the check again, or check TURING_LOCAL_ENGINE_URL",
		}
	}
}

// streaming asks for one short completion and needs text and a clean finish.
func (p *engineProbe) streaming(ctx context.Context) (string, error) {
	events, err := p.provider.StreamChat(ctx, llm.ChatRequest{
		Model:     p.model,
		Messages:  []llm.ChatMessage{{Role: "user", Content: streamPrompt}},
		MaxTokens: streamMaxTokens,
	})
	if err != nil {
		return "", p.unreachable(err, completionTimeout)
	}
	text := false
	for event := range events {
		switch event.Type {
		case "delta":
			text = text || strings.TrimSpace(event.Text) != ""
		case "error":
			return "", p.streamFailure(event, "see the engine's log for the cause, and check that TURING_LOCAL_MODEL is a model it can load")
		case "completed":
			if !text {
				return "", failure{
					reason: "the model streamed no text (only reasoning, or nothing)",
					fix:    "use a model or alias that answers directly, not one that only thinks within the token limit",
				}
			}
			return "", nil
		}
	}
	return "", p.streamEnded(ctx, completionTimeout)
}

// toolCalling offers one tool and needs the model to call it, which is what
// turns an engine that speaks the API into one usable for Turing.
func (p *engineProbe) toolCalling(ctx context.Context) (string, error) {
	events, err := p.provider.StreamChat(ctx, llm.ChatRequest{
		Model:     p.model,
		Messages:  []llm.ChatMessage{{Role: "user", Content: toolPrompt}},
		MaxTokens: toolMaxTokens,
		Tools: []llm.ToolDefinition{{
			Name:        probeToolName,
			Description: "Confirms that tool calling works.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status": map[string]any{"type": "string", "enum": []string{"ok"}},
				},
				"required": []string{"status"},
			},
		}},
	})
	if err != nil {
		return "", p.unreachable(err, completionTimeout)
	}
	var calls []llm.ToolCall
	for event := range events {
		switch event.Type {
		case "tool_call":
			calls = append(calls, event.ToolCalls...)
		case "error":
			return "", p.streamFailure(event, "see the engine's log; the server may be refusing tool definitions or the model's call")
		case "completed":
			calls = append(calls, event.ToolCalls...)
			return "", toolCallFailure(calls)
		}
	}
	return "", p.streamEnded(ctx, completionTimeout)
}

// toolCallFailure judges the calls the model made: nil when one names the
// probe's tool with parsed arguments.
func toolCallFailure(calls []llm.ToolCall) error {
	other := ""
	for _, call := range calls {
		if call.Name != probeToolName {
			if other == "" {
				other = call.Name
			}
			continue
		}
		if call.Arguments == nil {
			return failure{
				reason: "the tool call's arguments did not parse as a JSON object",
				fix:    "the server's tool-call parsing is not working; check its tool-calling option, or use another model",
			}
		}
		return nil
	}
	if len(calls) == 0 {
		return failure{
			reason: "the model or server does not do tool calling: it answered in text instead of calling the tool",
			fix:    "set TURING_LOCAL_MODEL to a model with tool support, and enable tool calling in the server if it has an option for it",
		}
	}
	return failure{
		reason: fmt.Sprintf("the model called %q instead of %s", oneLine(other, maxDetailRunes), probeToolName),
		fix:    "this model does not follow tool definitions reliably; set TURING_LOCAL_MODEL to another",
	}
}

// get is a listing request, which carries the API key when one is set.
func (p *engineProbe) get(ctx context.Context, path string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path, nil)
	if err != nil {
		return nil, p.unreachable(err, listingTimeout)
	}
	if p.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, p.unreachable(backendegress.RedactRedirectError(err), listingTimeout)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, p.refused(path, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxListingBytes))
	if err != nil {
		return nil, p.unreachable(err, listingTimeout)
	}
	return body, nil
}

func (p *engineProbe) unreachable(err error, limit time.Duration) failure {
	if errors.Is(err, context.DeadlineExceeded) {
		return p.timedOut(limit)
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	return failure{
		reason: fmt.Sprintf("cannot reach the engine at %s: %s", p.baseURL, oneLine(err.Error(), maxDetailRunes)),
		fix:    "start the engine, and check that TURING_LOCAL_ENGINE_URL is an address the runtime container can reach",
	}
}

func (p *engineProbe) timedOut(limit time.Duration) failure {
	return failure{
		reason: fmt.Sprintf("no complete answer from %s within %s", p.baseURL, limit),
		fix:    "the engine may be busy or still loading the model; run the check again once it is ready, or check TURING_LOCAL_ENGINE_URL",
	}
}

func (p *engineProbe) refused(path string, status int) failure {
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return failure{
			reason: fmt.Sprintf("the engine answered %d to %s", status, path),
			fix:    "check that TURING_LOCAL_ENGINE matches the server, that TURING_LOCAL_ENGINE_URL is its address, and that it is healthy",
		}
	}
	switch {
	case p.engine != llm.LocalEngineOpenAI:
		return failure{
			reason: fmt.Sprintf("the engine answered %d, which Ollama itself does not", status),
			fix:    "check that TURING_LOCAL_ENGINE_URL points at Ollama, or set TURING_LOCAL_ENGINE=openai and TURING_LOCAL_ENGINE_API_KEY for a server that needs a key",
		}
	case p.apiKey == "":
		return failure{
			reason: fmt.Sprintf("the engine answered %d: it wants an API key", status),
			fix:    "set TURING_LOCAL_ENGINE_API_KEY to the key the server expects",
		}
	default:
		return failure{
			reason: fmt.Sprintf("the engine answered %d: it refused the API key", status),
			fix:    "correct TURING_LOCAL_ENGINE_API_KEY",
		}
	}
}

// streamFailure reports an error event, which the provider has classified:
// its code is the provider's own, its message the server's, so it is cut and
// made printable.
func (p *engineProbe) streamFailure(event llm.StreamEvent, fix string) failure {
	reason := "the engine's stream failed with " + event.Code
	if message := oneLine(event.Message, maxDetailRunes); message != "" {
		reason += ": " + message
	}
	if event.Code == "model_auth_failed" {
		fix = "set TURING_LOCAL_ENGINE_API_KEY to the key the engine expects"
	}
	return failure{reason: reason, fix: fix}
}

// streamEnded explains a stream that closed with neither a completion nor an
// error: the step's deadline, or a server that hung up.
func (p *engineProbe) streamEnded(ctx context.Context, limit time.Duration) failure {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return p.timedOut(limit)
	}
	return failure{
		reason: "the stream ended before the engine finished its answer",
		fix:    "see the engine's log for the cause",
	}
}

// listedNames is the model names an engine's listing reports: /api/tags for
// Ollama, /models for an OpenAI-compatible server.
func listedNames(engine string, body []byte) ([]string, error) {
	var names []string
	if engine == llm.LocalEngineOllama {
		var listing struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.Unmarshal(body, &listing); err != nil {
			return nil, err
		}
		for _, model := range listing.Models {
			names = append(names, model.Name)
		}
		return names, nil
	}
	var listing struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, err
	}
	for _, model := range listing.Data {
		names = append(names, model.ID)
	}
	return names, nil
}

// modelListed says whether the engine lists model. Ollama also lists an
// untagged name by its :latest tag. An OpenAI-compatible server's names are
// its own aliases, so a model it does not list still counts as an alias while
// it lists anything at all; an empty list is neither.
func modelListed(engine, model string, names []string) (listed bool, alias bool) {
	if slices.Contains(names, model) {
		return true, false
	}
	if engine == llm.LocalEngineOllama {
		untagged := !strings.Contains(model[strings.LastIndex(model, "/")+1:], ":")
		return untagged && slices.Contains(names, model+":latest"), false
	}
	return false, len(names) > 0
}

// oneLine makes text a server sent safe to print in one line: control
// characters become spaces, runs of space collapse, and a long text is cut.
func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)), " ")
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return text
}
