package agent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
)

var errContextBudgetExceeded = errors.New("required context exceeds the configured model window")

type contextInput struct {
	skills  *llm.ChatMessage
	history []llm.ChatMessage
	recall  *llm.ChatMessage
	live    []llm.ChatMessage
}

type contextOmissions struct {
	HistoryMessages int
	RecallOmitted   bool
	ToolDefinitions int
}

func (o contextOmissions) Notice() string {
	var omitted []string
	if o.HistoryMessages > 0 {
		label := "older conversation messages"
		if o.HistoryMessages == 1 {
			label = "older conversation message"
		}
		omitted = append(omitted, fmt.Sprintf("%d %s", o.HistoryMessages, label))
	}
	if o.RecallOmitted {
		omitted = append(omitted, "recalled material")
	}
	if o.ToolDefinitions > 0 {
		label := "tool definitions"
		if o.ToolDefinitions == 1 {
			label = "tool definition"
		}
		omitted = append(omitted, fmt.Sprintf("%d %s", o.ToolDefinitions, label))
	}
	if len(omitted) == 0 {
		return ""
	}
	return "Context window limit: omitted " + joinNoticeItems(omitted) + " from this model request."
}

func joinNoticeItems(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
	}
}

type budgetedContext struct {
	Request    llm.ChatRequest
	Omissions  contextOmissions
	RecallUsed bool
	Estimate   int
}

func buildBudgetedContext(
	provider llm.Provider,
	model string,
	input contextInput,
	tools []llm.ToolDefinition,
) (budgetedContext, error) {
	selectedTools := make([]bool, len(tools))
	requiredNames := liveToolNames(input.live)
	for index, definition := range tools {
		if _, required := requiredNames[definition.Name]; required {
			selectedTools[index] = true
		}
	}

	selectedHistory := make([][]llm.ChatMessage, 0)
	recallUsed := false
	buildRequest := func() llm.ChatRequest {
		messages := make([]llm.ChatMessage, 0, len(input.history)+len(input.live)+2)
		if recallUsed && input.recall != nil {
			messages = append(messages, *input.recall)
		}
		if input.skills != nil {
			messages = append(messages, *input.skills)
		}
		for _, unit := range selectedHistory {
			messages = append(messages, unit...)
		}
		messages = append(messages, input.live...)

		selectedDefinitions := make([]llm.ToolDefinition, 0, len(tools))
		for index, definition := range tools {
			if selectedTools[index] {
				selectedDefinitions = append(selectedDefinitions, definition)
			}
		}
		return llm.ChatRequest{
			Model:    model,
			Messages: messages,
			Tools:    selectedDefinitions,
		}
	}
	estimate := func() (int, error) {
		return llm.EstimateRequestTokens(provider, buildRequest())
	}
	fits := func() (bool, int, error) {
		value, err := estimate()
		if err != nil {
			return false, 0, err
		}
		return value <= llm.ProviderContextWindowTokens(provider), value, nil
	}

	ok, mandatoryEstimate, err := fits()
	if err != nil {
		return budgetedContext{}, fmt.Errorf("estimate mandatory model context: %w", err)
	}
	if !ok {
		return budgetedContext{}, fmt.Errorf(
			"%w: conservative mandatory request estimate %d exceeds configured window %d",
			errContextBudgetExceeded,
			mandatoryEstimate,
			llm.ProviderContextWindowTokens(provider),
		)
	}

	omissions := contextOmissions{}
	for index := range tools {
		if selectedTools[index] {
			continue
		}
		selectedTools[index] = true
		ok, _, err := fits()
		if err != nil {
			return budgetedContext{}, fmt.Errorf("estimate model context with tool %q: %w", tools[index].Name, err)
		}
		if !ok {
			selectedTools[index] = false
			omissions.ToolDefinitions++
		}
	}

	if input.recall != nil {
		recallUsed = true
		ok, _, err := fits()
		if err != nil {
			return budgetedContext{}, fmt.Errorf("estimate model context with recall: %w", err)
		}
		if !ok {
			recallUsed = false
			omissions.RecallOmitted = true
		}
	}

	units := completeHistoryUnits(input.history)
	for index := len(units) - 1; index >= 0; index-- {
		selectedHistory = append([][]llm.ChatMessage{units[index]}, selectedHistory...)
		ok, _, err := fits()
		if err != nil {
			return budgetedContext{}, fmt.Errorf("estimate model context with history: %w", err)
		}
		if !ok {
			selectedHistory = selectedHistory[1:]
			omissions.HistoryMessages += len(units[index])
		}
	}

	request := buildRequest()
	finalEstimate, err := llm.EstimateRequestTokens(provider, request)
	if err != nil {
		return budgetedContext{}, fmt.Errorf("estimate final model context: %w", err)
	}
	return budgetedContext{
		Request:    request,
		Omissions:  omissions,
		RecallUsed: recallUsed,
		Estimate:   finalEstimate,
	}, nil
}

func liveToolNames(messages []llm.ChatMessage) map[string]struct{} {
	names := make(map[string]struct{})
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			if call.Name != "" {
				names[call.Name] = struct{}{}
			}
		}
	}
	return names
}

func completeHistoryUnits(messages []llm.ChatMessage) [][]llm.ChatMessage {
	var units [][]llm.ChatMessage
	for _, message := range messages {
		if message.Role == "user" || len(units) == 0 {
			units = append(units, nil)
		}
		last := len(units) - 1
		units[last] = append(units[last], message)
	}
	return units
}
