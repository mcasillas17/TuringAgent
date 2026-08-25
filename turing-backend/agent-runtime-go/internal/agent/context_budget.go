package agent

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
)

var errContextBudgetExceeded = errors.New("required context exceeds the configured model window")

type contextInput struct {
	// memory is the pinned persona and profile. It sits beside skills as
	// upstream, pre-decision context: both are built before the budget runs,
	// because both are all-or-nothing units the budget can only accept or drop.
	memory                    []llm.ChatMessage
	skills                    []llm.ChatMessage
	history                   []llm.ChatMessage
	recall                    *llm.ChatMessage
	live                      []llm.ChatMessage
	requiredToolNames         map[string]struct{}
	excludedOptionalToolNames map[string]struct{}
	skillIndexOmitted         bool
	memoryOmitted             bool
	// minimalToolResults marks synthetic protocol placeholders supplied by the
	// caller for preflight. Real tool content is never identified by sniffing
	// untrusted bytes.
	minimalToolResults map[int]struct{}
}

type contextOmissions struct {
	HistoryMessages   int
	RecallOmitted     bool
	SkillIndexOmitted bool
	MemoryOmitted     bool
	ToolDefinitions   int
	ToolResults       int
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
	if o.SkillIndexOmitted {
		omitted = append(omitted, "enabled skill metadata")
	}
	if o.MemoryOmitted {
		omitted = append(omitted, "pinned memory")
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

// EventPayload is the structured half of the same statement Notice makes in
// prose. It exists as one method rather than as a literal at the emit site so
// that a field added to this struct and forgotten here is a test failure rather
// than an omission the client is never told about: the exhaustiveness guard
// counts keys against fields.
func (o contextOmissions) EventPayload() map[string]any {
	return map[string]any{
		"historyMessagesOmitted": o.HistoryMessages,
		"recallOmitted":          o.RecallOmitted,
		"skillIndexOmitted":      o.SkillIndexOmitted,
		"memoryOmitted":          o.MemoryOmitted,
		"toolDefinitionsOmitted": o.ToolDefinitions,
		"toolResultsOmitted":     o.ToolResults,
	}
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
	for name := range input.requiredToolNames {
		requiredNames[name] = struct{}{}
	}
	for index, definition := range tools {
		if _, required := requiredNames[definition.Name]; required {
			selectedTools[index] = true
		}
	}

	selectedHistory := make([][]llm.ChatMessage, 0)
	recallUsed := false
	omissions := contextOmissions{
		SkillIndexOmitted: input.skillIndexOmitted,
		MemoryOmitted:     input.memoryOmitted,
	}
	buildRequest := func() llm.ChatRequest {
		messages := make([]llm.ChatMessage, 0, len(input.memory)+len(input.skills)+len(input.history)+len(liveMessages)+1)
		// The persona leads, because it is the user's statement of who is
		// answering and everything after it is read in that voice.
		messages = append(messages, input.memory...)
		if recallUsed && input.recall != nil {
			messages = append(messages, *input.recall)
		}
		messages = append(messages, input.skills...)
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
	if !ok {
		type toolResultCandidate struct {
			index   int
			savings int
			marker  string
		}
		candidates := make([]toolResultCandidate, 0)
		for index, message := range liveMessages {
			if message.Role != "tool" {
				continue
			}
			if _, minimal := input.minimalToolResults[index]; minimal {
				continue
			}
			marker := compactedToolResultForBytes(len(message.Content))
			liveMessages[index].Content = marker
			candidateEstimate, err := estimate()
			liveMessages[index].Content = message.Content
			if err != nil {
				return budgetedContext{}, fmt.Errorf("estimate compacted tool result %d: %w", index, err)
			}
			savings := mandatoryEstimate - candidateEstimate
			if savings <= 0 {
				continue
			}
			candidates = append(candidates, toolResultCandidate{
				index:   index,
				savings: savings,
				marker:  marker,
			})
		}
		sort.Slice(candidates, func(left, right int) bool {
			if candidates[left].savings != candidates[right].savings {
				return candidates[left].savings > candidates[right].savings
			}
			return candidates[left].index < candidates[right].index
		})
		for _, candidate := range candidates {
			if ok {
				break
			}
			liveMessages[candidate.index].Content = candidate.marker
			omissions.ToolResults++
			ok, mandatoryEstimate, err = fits()
			if err != nil {
				return budgetedContext{}, fmt.Errorf("estimate compacted live tool protocol: %w", err)
			}
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
	for index, definition := range tools {
		if selectedTools[index] {
			continue
		}
		if _, excluded := input.excludedOptionalToolNames[definition.Name]; excluded {
			continue
		}
		optionalTools = append(optionalTools, index)
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
	for _, selected := range selectedTools {
		if !selected {
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
	bestHistoryUnits := 0
	for low, high := 0, len(units); low <= high; {
		middle := low + (high-low)/2
		selectedHistory = units[len(units)-middle:]
		candidateFits, _, err := fits()
		if err != nil {
			return budgetedContext{}, fmt.Errorf("estimate model context with %d history units: %w", middle, err)
		}
		if candidateFits {
			bestHistoryUnits = middle
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	selectedHistory = units[len(units)-bestHistoryUnits:]
	for omittedIndex := 0; omittedIndex < len(units)-bestHistoryUnits; omittedIndex++ {
		omissions.HistoryMessages += len(units[omittedIndex])
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

func compactedToolResultForBytes(originalBytes int) string {
	return fmt.Sprintf(
		`{"contextBudget":{"omitted":true,"originalBytes":%d,"message":"Tool result content omitted to fit the configured context window."}}`,
		originalBytes,
	)
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
