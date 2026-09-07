package testkit

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/agent"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/memory"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/orchestrator"
	"google.golang.org/grpc"
)

type RecallText struct {
	MessageID string `json:"message_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
}

type RecallSelection struct {
	MessageID string    `json:"message_id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	At        time.Time `json:"at"`
}

type RecallPass struct {
	InContext []RecallText      `json:"in_context"`
	Selected  []RecallSelection `json:"selected"`
	Rendered  string            `json:"rendered"`
}

type RecallExecution struct {
	Passes         []RecallPass `json:"passes"`
	Request        []RecallText `json:"request"`
	RequestCount   int          `json:"request_count"`
	Queries        int          `json:"queries"`
	Rows           int          `json:"rows"`
	PromptRunes    int          `json:"prompt_runes"`
	PromptBytes    int          `json:"prompt_bytes"`
	PromptTokens   int          `json:"prompt_estimated_tokens"`
	UsageReported  bool         `json:"usage_reported"`
	RecallOmitted  bool         `json:"recall_omitted"`
	HistoryOmitted int          `json:"history_omitted"`
}

type observedSearcher struct {
	client *orchestrator.Client
	result *RecallExecution
}

func (s observedSearcher) SearchMessages(ctx context.Context, query, sessionID, excludedID string, limit int) ([]memory.Excerpt, error) {
	s.result.Queries++
	rows, err := s.client.SearchMessages(ctx, query, sessionID, excludedID, limit)
	s.result.Rows += len(rows)
	return rows, err
}

type observedRecaller struct {
	real     *memory.Recaller
	result   *RecallExecution
	selected []RecallSelection
}

func (r *observedRecaller) PrepareRecall(ctx context.Context, sessionID, text string) func(context.Context, []llm.ChatMessage) (llm.ChatMessage, bool) {
	prepare := r.real.PrepareRecall(ctx, sessionID, text)
	return func(ctx context.Context, inContext []llm.ChatMessage) (llm.ChatMessage, bool) {
		r.selected = nil
		block, ok := prepare(ctx, inContext)
		r.result.Passes = append(r.result.Passes, RecallPass{
			InContext: observedTexts(inContext), Selected: r.selected, Rendered: block.Content,
		})
		return block, ok
	}
}

func observedTexts(messages []llm.ChatMessage) []RecallText {
	result := make([]RecallText, 0, len(messages))
	for _, message := range messages {
		result = append(result, RecallText{MessageID: message.MessageID, Role: message.Role, Content: message.Content})
	}
	return result
}

// The embedded real Ollama provider supplies its exact wire serializer and
// conservative estimator. Only StreamChat is replaced; no socket/model/judge
// is used and no provider-reported token usage is fabricated.
type recallCaptureProvider struct {
	*llm.Ollama
	result *RecallExecution
}

func (p *recallCaptureProvider) StreamChat(_ context.Context, request llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	p.result.RequestCount++
	p.result.Request = observedTexts(request.Messages)
	for _, message := range request.Messages {
		p.result.PromptRunes += utf8.RuneCountInString(message.Content)
		p.result.PromptBytes += len(message.Content)
	}
	estimate, err := p.EstimateRequestTokens(request)
	if err != nil {
		return nil, err
	}
	p.result.PromptTokens = estimate
	events := make(chan llm.StreamEvent, 1)
	events <- llm.StreamEvent{Type: "completed", FinishReason: "stop"}
	close(events)
	return events, nil
}

// ExecuteRecall runs GeneralAssistant.Execute with the real runtime client and
// recaller. Every recorded pass contains the actual budget-admitted inContext,
// including convergence/fallback passes, not a separately fetched history.
func ExecuteRecall(ctx context.Context, conn *grpc.ClientConn, job *turingv1.AgentJob, window int) (RecallExecution, error) {
	var result RecallExecution
	ollama, err := llm.NewOllamaWithLimits("http://127.0.0.1:1", nil, window, 64)
	if err != nil {
		return result, err
	}
	client := orchestrator.New(conn, "")
	recaller := &observedRecaller{real: memory.NewRecaller(observedSearcher{client, &result}), result: &result}
	recaller.real.ObserveSelection = func(excerpts []memory.Excerpt) {
		for _, excerpt := range excerpts {
			recaller.selected = append(recaller.selected, RecallSelection{
				MessageID: excerpt.MessageID, SessionID: excerpt.SessionID, Role: excerpt.Role,
				Content: excerpt.Content, At: excerpt.CreatedAt,
			})
		}
	}
	provider := &recallCaptureProvider{Ollama: ollama, result: &result}
	assistant := agent.NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
	}, client, &agent.GeneralAssistantTools{Recall: recaller})
	err = assistant.Execute(ctx, job, func(update *turingv1.RuntimeUpdate) error {
		if update.GetRunFailed() != nil {
			return fmt.Errorf("evaluation execution failed: %s", update.GetRunFailed().GetCode())
		}
		event := update.GetEvent()
		if event.GetType() == turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED {
			return fmt.Errorf("evaluation execution failed: %s", event.GetType())
		}
		payload := event.GetPayload().GetFields()
		if payload["recallOmitted"].GetBoolValue() {
			result.RecallOmitted = true
		}
		if value := int(payload["historyMessagesOmitted"].GetNumberValue()); value > result.HistoryOmitted {
			result.HistoryOmitted = value
		}
		return nil
	})
	if err == nil && result.RequestCount != 1 {
		err = fmt.Errorf("evaluation expected one provider dispatch, got %d", result.RequestCount)
	}
	return result, err
}
