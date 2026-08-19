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
	ToolResults     int
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
	if o.ToolResults > 0 {
		label := "tool results"
		if o.ToolResults == 1 {
			label = "tool result"
		}
		omitted = append(omitted, fmt.Sprintf("content from %d %s", o.ToolResults, label))
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
	liveMessages := cloneChatMessages(input.live)
	selectedTools := make([]bool, len(tools))
	requiredNames := liveToolNames(liveMessages)
	for index, definition := range tools {
		if _, required := requiredNames[definition.Name]; required {
			selectedTools[index] = true
		}
	}

	selectedHistory := make([][]llm.ChatMessage, 0)
	recallUsed := false
	omissions := contextOmissions{}
	buildRequest := func() llm.ChatRequest {
		messages := make([]llm.ChatMessage, 0, len(input.history)+len(liveMessages)+2)
		if recallUsed && input.recall != nil {
			messages = append(messages, *input.recall)
		}
		if input.skills != nil {
			messages = append(messages, *input.skills)
		}
		for _, unit := range selectedHistory {
			messages = append(messages, unit...)
		}
		messages = append(messages, liveMessages...)

		selectedDefinitions := make([]llm.ToolDefinition, 0, len(tools))
		for index, definition := range tools {
			if selectedTools[index] {
				selectedDefinitions = append(selectedDefinitions, definition)
			}
		}
		return llm.ChatRequest{
			Model:     model,
			Messages:  messages,
			MaxTokens: provider.MaxOutputTokens(),
			Tools:     selectedDefinitions,
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
		inputBudget := provider.ContextWindowTokens() - provider.MaxOutputTokens()
		return value <= inputBudget, value, nil
	}

	ok, mandatoryEstimate, err := fits()
	if err != nil {
		return budgetedContext{}, fmt.Errorf("estimate mandatory model context: %w", err)
	}
	for index := range liveMessages {
		if ok {
			break
		}
		if liveMessages[index].Role != "tool" || isCompactedToolResult(liveMessages[index].Content) {
			continue
		}
		liveMessages[index].Content = compactedToolResult(liveMessages[index].Content)
		omissions.ToolResults++
		ok, mandatoryEstimate, err = fits()
		if err != nil {
			return budgetedContext{}, fmt.Errorf("estimate compacted live tool protocol: %w", err)
		}
	}
	if !ok {
		return budgetedContext{}, fmt.Errorf(
			"%w: conservative mandatory request estimate %d plus %d reserved output tokens exceeds configured window %d",
			errContextBudgetExceeded,
			mandatoryEstimate,
			provider.MaxOutputTokens(),
			provider.ContextWindowTokens(),
		)
	}

	optionalTools := make([]int, 0, len(tools))
	for index := range tools {
		if !selectedTools[index] {
			optionalTools = append(optionalTools, index)
		}
	}
	setOptionalPrefix := func(count int) {
		for optionalIndex, toolIndex := range optionalTools {
			selectedTools[toolIndex] = optionalIndex < count
		}
	}
	best := 0
	for low, high := 0, len(optionalTools); low <= high; {
		middle := low + (high-low)/2
		setOptionalPrefix(middle)
		candidateFits, _, err := fits()
		if err != nil {
			return budgetedContext{}, fmt.Errorf("estimate model context with %d optional tools: %w", middle, err)
		}
		if candidateFits {
			best = middle
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	setOptionalPrefix(best)
	omissions.ToolDefinitions = len(optionalTools) - best

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
			for omittedIndex := 0; omittedIndex <= index; omittedIndex++ {
				omissions.HistoryMessages += len(units[omittedIndex])
			}
			break
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

func compactedToolResult(content string) string {
	return fmt.Sprintf(
		`{"contextBudget":{"omitted":true,"originalBytes":%d,"message":"Tool result content omitted to fit the configured context window."}}`,
		len(content),
	)
}

func isCompactedToolResult(content string) bool {
	return strings.Contains(content, `"contextBudget":{"omitted":true`)
}

func cloneChatMessages(messages []llm.ChatMessage) []llm.ChatMessage {
	cloned := make([]llm.ChatMessage, len(messages))
	copy(cloned, messages)
	for index := range cloned {
		cloned[index].ToolCalls = append([]llm.ToolCall(nil), cloned[index].ToolCalls...)
	}
	return cloned
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
