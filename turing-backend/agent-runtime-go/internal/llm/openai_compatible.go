package llm

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	maxOpenAIEventDataBytes                   = maxStreamTokenBytes
	maxOpenAIHTTPErrorBodyBytes               = maxOpenAIEventDataBytes
	maxOpenAIToolCalls                        = 128
	maxOpenAIToolCallArgumentBytes            = maxStreamTokenBytes
	maxOpenAIToolCallAggregateArgumentBytes   = maxStreamTokenBytes
	maxOpenAIToolCallAggregateIdentifierBytes = 64 * 1024
)

var errOpenAIPhysicalSSELineTooLong = errors.New("OpenAI-compatible SSE line exceeds maximum size")

// defaultOpenAIUsageDrainTimeout bounds how long the stream waits after the
// terminal event for the trailing usage chunk. Generous next to a conforming
// provider, which sends it in the same breath, and short enough that one which
// never closes costs a token count rather than the whole answer.
const defaultOpenAIUsageDrainTimeout = 2 * time.Second

type OpenAICompatible struct {
	baseURL             string
	apiKey              string
	client              *http.Client
	contextWindowTokens int
	maxOutputTokens     int
	// usageDrainTimeout is per-provider rather than a package variable so a
	// test can shorten it without reaching into shared state.
	usageDrainTimeout time.Duration
}

var _ Provider = (*OpenAICompatible)(nil)

func NewOpenAICompatible(baseURL string, apiKey string, client *http.Client) *OpenAICompatible {
	if client == nil {
		client = http.DefaultClient
	}
	return &OpenAICompatible{
		baseURL:             strings.TrimRight(baseURL, "/"),
		apiKey:              apiKey,
		client:              client,
		contextWindowTokens: DefaultContextWindowTokens,
		maxOutputTokens:     DefaultMaxOutputTokens,
		usageDrainTimeout:   defaultOpenAIUsageDrainTimeout,
	}
}

func NewOpenAICompatibleWithLimits(
	baseURL string,
	apiKey string,
	client *http.Client,
	contextWindowTokens int,
	maxOutputTokens int,
) (*OpenAICompatible, error) {
	if err := ValidateContextLimits(contextWindowTokens, maxOutputTokens); err != nil {
		return nil, err
	}
	provider := NewOpenAICompatible(baseURL, apiKey, client)
	provider.contextWindowTokens = contextWindowTokens
	provider.maxOutputTokens = maxOutputTokens
	return provider, nil
}

func (p *OpenAICompatible) ID() string { return "openai_compatible" }

func (p *OpenAICompatible) ContextWindowTokens() int { return p.contextWindowTokens }

func (p *OpenAICompatible) MaxOutputTokens() int { return p.maxOutputTokens }

func (p *OpenAICompatible) post(ctx context.Context, body []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("authorization", "Bearer "+p.apiKey)
	}
	return p.client.Do(httpReq)
}

// isOpenAIMalformedRequestStatus reports the two statuses a server uses to say
// the request body was not acceptable. Deliberately narrow: 401, 403 and 429
// are about who is asking and how often, and retrying those without
// stream_options would only spend a second request to fail the same way.
func isOpenAIMalformedRequestStatus(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusUnprocessableEntity
}

func (p *OpenAICompatible) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	buildBody := func(includeUsage bool) ([]byte, openAIToolAliases, error) {
		body, aliases, err := p.marshalRequestWithUsage(req, includeUsage)
		if err != nil {
			return nil, openAIToolAliases{}, err
		}
		bounded, err := marshalProviderRequest("OpenAI-compatible", func() ([]byte, error) {
			return body, nil
		})
		return bounded, aliases, err
	}
	body, aliases, err := buildBody(true)
	if err != nil {
		return nil, err
	}
	resp, err := p.post(ctx, body)
	if err != nil {
		return nil, err
	}
	// Not every OpenAI-compatible server accepts an unknown body field: older
	// vLLM builds, some corporate proxies and older Azure API versions reject
	// the whole request rather than ignoring it. Those installations work
	// today, and losing every run on them to collect a number the user did not
	// ask for is the wrong trade — so a rejection that looks like "I do not
	// know that field" is retried once without it, and the run simply reports
	// no tokens.
	if isOpenAIMalformedRequestStatus(resp.StatusCode) {
		_ = resp.Body.Close()
		retryBody, _, retryErr := buildBody(false)
		if retryErr != nil {
			return nil, retryErr
		}
		resp, err = p.post(ctx, retryBody)
		if err != nil {
			return nil, err
		}
	}
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			code := providerHTTPErrorCode(resp.StatusCode)
			if resp.StatusCode == http.StatusTooManyRequests {
				body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxOpenAIHTTPErrorBodyBytes)+1))
				if err == nil && len(body) <= maxOpenAIHTTPErrorBodyBytes && isOpenAIQuotaErrorBody(body) {
					code = "model_quota_exceeded"
				}
			}
			sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: code, Message: fmt.Sprintf("OpenAI-compatible provider returned %d", resp.StatusCode)})
			return
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), maxStreamTokenBytes)
		scanner.Split(splitSSELines)
		state := newOpenAIStreamState(aliases.byAlias)
		// The completed event is held rather than sent the moment
		// finish_reason arrives, because the usage chunk OpenAI sends in
		// response to stream_options.include_usage arrives AFTER it. Returning
		// on finish_reason closed this channel before that chunk was ever read,
		// which is why nothing here could report tokens.
		//
		// Nothing else about the stream changes: the same events go out in the
		// same order, the completed one simply waits for the tail.
		var pendingCompleted *StreamEvent
		// Waiting for the tail must never cost the user the answer. A provider
		// that finishes, sends no [DONE], and leaves the connection open would
		// otherwise turn a completed response into a model timeout — the run
		// would fail on a change made to collect a token count. Closing the
		// body unblocks the read; the scanner error that follows is handled by
		// emitting the held completion.
		var drainDeadline *time.Timer
		defer func() {
			if drainDeadline != nil {
				drainDeadline.Stop()
			}
		}()
		holdCompleted := func(event StreamEvent) {
			pendingCompleted = &event
			if drainDeadline == nil {
				drainDeadline = time.AfterFunc(p.usageDrainTimeout, func() {
					_ = resp.Body.Close()
				})
			}
		}
		sendCompleted := func(event StreamEvent) {
			event.Usage = state.usage
			sendStreamEvent(ctx, out, event)
		}
		dispatch := func(data string) bool {
			trimmed := strings.TrimSpace(data)
			if trimmed == "" {
				return false
			}
			if trimmed == "[DONE]" {
				if pendingCompleted != nil {
					sendCompleted(*pendingCompleted)
					return true
				}
				if len(state.toolCalls) > 0 {
					sendStreamEvent(ctx, out, StreamEvent{
						Type:    "error",
						Code:    "model_bad_chunk",
						Message: fmt.Sprintf("[DONE] received with %d unfinished tool call(s)", len(state.toolCalls)),
					})
				} else {
					sendCompleted(StreamEvent{Type: "completed", FinishReason: "stop"})
				}
				return true
			}
			// Past the terminal event the stream has nothing left to say about
			// the answer, so this tail is mined for usage and otherwise
			// ignored rather than validated. A response that already finished
			// must not be turned into a failure by trailing bytes.
			if pendingCompleted != nil {
				state.absorbUsageFromChunk([]byte(data))
				return false
			}
			events, done, err := parseOpenAIData([]byte(data), state)
			if err != nil {
				sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: err.Error()})
				return true
			}
			for _, event := range events {
				if event.Type == "completed" {
					holdCompleted(event)
					continue
				}
				if !sendStreamEvent(ctx, out, event) {
					return true
				}
			}
			if pendingCompleted != nil {
				return false
			}
			return done
		}

		var dataLines []string
		dataBytes := 0
		firstLine := true
		for scanner.Scan() {
			line := scanner.Text()
			if firstLine {
				line = strings.TrimPrefix(line, "\uFEFF")
				firstLine = false
			}
			if line == "" {
				if len(dataLines) > 0 {
					data := strings.Join(dataLines, "\n")
					dataLines = nil
					dataBytes = 0
					if dispatch(data) {
						return
					}
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			field, value, found := strings.Cut(line, ":")
			if !found {
				value = ""
			}
			if field != "data" {
				continue
			}
			value = strings.TrimPrefix(value, " ")
			additionalBytes := len(value)
			if len(dataLines) > 0 {
				additionalBytes++
			}
			if additionalBytes > maxOpenAIEventDataBytes-dataBytes {
				// An oversized chunk arriving after the terminal event is a
				// trailing chunk, and the answer is already complete. Same rule
				// as everywhere past that point: it costs a token count, never
				// the response.
				if pendingCompleted != nil {
					sendCompleted(*pendingCompleted)
					return
				}
				sendStreamEvent(ctx, out, StreamEvent{
					Type:    "error",
					Code:    "model_bad_chunk",
					Message: fmt.Sprintf("SSE event data exceeds %d bytes", maxOpenAIEventDataBytes),
				})
				return
			}
			dataLines = append(dataLines, value)
			dataBytes += additionalBytes
		}
		if err := scanner.Err(); err != nil {
			// A read error in the tail after the answer already finished costs
			// a token count, not the response. Reporting it as a stream failure
			// would discard a complete answer over telemetry.
			if pendingCompleted != nil {
				sendCompleted(*pendingCompleted)
				return
			}
			if ctx.Err() == nil {
				code := "model_stream_error"
				if errors.Is(err, errOpenAIPhysicalSSELineTooLong) {
					code = "model_bad_chunk"
				}
				sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: code, Message: err.Error()})
			}
			return
		}
		// A stream that ends without [DONE] still finished if finish_reason
		// arrived, which is what many OpenAI-compatible servers do.
		if pendingCompleted != nil {
			sendCompleted(*pendingCompleted)
			return
		}
		if ctx.Err() != nil {
			return
		}
		message := "OpenAI-compatible stream ended before a terminal event"
		if len(state.toolCalls) > 0 {
			message += fmt.Sprintf(" with %d unfinished tool call(s)", len(state.toolCalls))
		}
		sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_stream_error", Message: message})
	}()
	return out, nil
}

func (p *OpenAICompatible) EstimateRequestTokens(req ChatRequest) (int, error) {
	body, _, err := p.marshalRequest(req)
	if err != nil {
		return 0, err
	}
	return len(body), nil
}

func (p *OpenAICompatible) marshalRequest(req ChatRequest) ([]byte, openAIToolAliases, error) {
	return p.marshalRequestWithUsage(req, true)
}

func (p *OpenAICompatible) marshalRequestWithUsage(
	req ChatRequest,
	includeUsage bool,
) ([]byte, openAIToolAliases, error) {
	aliases, err := buildOpenAIToolAliases(req.Tools)
	if err != nil {
		return nil, openAIToolAliases{}, err
	}
	tools := openAITools(req.Tools, aliases.byOriginal)
	converted, err := openAIMessages(req.Messages, aliases.byOriginal)
	if err != nil {
		return nil, openAIToolAliases{}, err
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = p.maxOutputTokens
	}
	request := openAIChatRequest{
		Model:               req.Model,
		Messages:            converted,
		Stream:              true,
		Temperature:         req.Temperature,
		MaxTokens:           legacyMaxTokens(req.Model, maxTokens),
		MaxCompletionTokens: reasoningMaxTokens(req.Model, maxTokens),
		Tools:               tools,
	}
	if includeUsage {
		request.StreamOptions = &openAIStreamOptions{IncludeUsage: true}
	}
	body, err := json.Marshal(request)
	return body, aliases, err
}

func splitSSELines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		switch b {
		case '\n':
			return i + 1, data[:i], nil
		case '\r':
			if i+1 == len(data) && !atEOF {
				return 0, nil, nil
			}
			advance := i + 1
			if i+1 < len(data) && data[i+1] == '\n' {
				advance++
			}
			return advance, data[:i], nil
		}
	}
	if len(data) >= maxStreamTokenBytes {
		return 0, nil, errOpenAIPhysicalSSELineTooLong
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

type openAIChatRequest struct {
	Model               string               `json:"model"`
	Messages            []openAIMessage      `json:"messages"`
	Stream              bool                 `json:"stream"`
	Temperature         float64              `json:"temperature,omitempty"`
	MaxTokens           int                  `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                  `json:"max_completion_tokens,omitempty"`
	Tools               []openAITool         `json:"tools,omitempty"`
	StreamOptions       *openAIStreamOptions `json:"stream_options,omitempty"`
}

func legacyMaxTokens(model string, maxTokens int) int {
	if usesMaxCompletionTokens(model) {
		return 0
	}
	return maxTokens
}

func reasoningMaxTokens(model string, maxTokens int) int {
	if usesMaxCompletionTokens(model) {
		return maxTokens
	}
	return 0
}

func usesMaxCompletionTokens(model string) bool {
	name := strings.ToLower(strings.TrimSpace(model))
	if index := strings.LastIndexByte(name, '/'); index >= 0 {
		name = name[index+1:]
	}
	for _, family := range []string{"o1", "o3", "o4"} {
		if name == family || strings.HasPrefix(name, family+"-") {
			return true
		}
	}
	return name == "gpt-5" ||
		strings.HasPrefix(name, "gpt-5-") ||
		strings.HasPrefix(name, "gpt-5.")
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIMessage struct {
	Role       string                  `json:"role"`
	Content    *string                 `json:"content"`
	Name       string                  `json:"name,omitempty"`
	ToolCallID string                  `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIMessageToolCall `json:"tool_calls,omitempty"`
}

type openAIMessageToolCall struct {
	ID       string                    `json:"id"`
	Type     string                    `json:"type"`
	Function openAIMessageFunctionCall `json:"function"`
}

type openAIMessageFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func openAIMessages(messages []ChatMessage, aliases map[string]string) ([]openAIMessage, error) {
	converted := make([]openAIMessage, 0, len(messages))
	for _, message := range messages {
		content := message.Content
		result := openAIMessage{
			Role:    message.Role,
			Content: &content,
			Name:    message.Name,
		}
		switch message.Role {
		case "tool":
			result.Name = ""
			result.ToolCallID = message.ToolCallID
		case "assistant":
			if message.Content == "" && len(message.ToolCalls) > 0 {
				result.Content = nil
			}
			for _, call := range message.ToolCalls {
				callArguments := call.Arguments
				if callArguments == nil {
					callArguments = map[string]any{}
				}
				arguments, err := json.Marshal(callArguments)
				if err != nil {
					return nil, err
				}
				name := call.Name
				if alias, ok := aliases[name]; ok {
					name = alias
				}
				result.ToolCalls = append(result.ToolCalls, openAIMessageToolCall{
					ID:   call.ID,
					Type: "function",
					Function: openAIMessageFunctionCall{
						Name:      name,
						Arguments: string(arguments),
					},
				})
			}
		}
		converted = append(converted, result)
	}
	return converted, nil
}

type openAITool struct {
	Type     string                   `json:"type"`
	Function openAIFunctionDefinition `json:"function"`
}

type openAIFunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

func openAITools(definitions []ToolDefinition, aliases map[string]string) []openAITool {
	tools := make([]openAITool, 0, len(definitions))
	for _, definition := range definitions {
		parameters := definition.Parameters
		if parameters == nil {
			parameters = map[string]any{"type": "object"}
		}
		name := definition.Name
		if alias, ok := aliases[name]; ok {
			name = alias
		}
		tools = append(tools, openAITool{
			Type: "function",
			Function: openAIFunctionDefinition{
				Name:        name,
				Description: definition.Description,
				Parameters:  parameters,
			},
		})
	}

	return tools
}

type openAIToolAliases struct {
	byOriginal map[string]string
	byAlias    map[string]string
}

func buildOpenAIToolAliases(definitions []ToolDefinition) (openAIToolAliases, error) {
	aliases := openAIToolAliases{
		byOriginal: make(map[string]string, len(definitions)),
		byAlias:    make(map[string]string, len(definitions)),
	}
	used := make(map[string]struct{}, len(definitions))
	invalid := make([]string, 0, len(definitions))
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if _, duplicate := seen[definition.Name]; duplicate {
			return openAIToolAliases{}, fmt.Errorf("duplicate OpenAI tool definition %q", definition.Name)
		}
		seen[definition.Name] = struct{}{}
		if isOpenAIFunctionName(definition.Name) {
			aliases.byOriginal[definition.Name] = definition.Name
			aliases.byAlias[definition.Name] = definition.Name
			used[definition.Name] = struct{}{}
			continue
		}
		invalid = append(invalid, definition.Name)
	}

	sort.Strings(invalid)
	for _, original := range invalid {
		for attempt := 0; ; attempt++ {
			alias := openAIToolAlias(original, attempt)
			if _, collision := used[alias]; collision {
				continue
			}
			aliases.byOriginal[original] = alias
			aliases.byAlias[alias] = original
			used[alias] = struct{}{}
			break
		}
	}
	return aliases, nil
}

func isOpenAIFunctionName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		if (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func openAIToolAlias(original string, attempt int) string {
	prefix := sanitizeOpenAIToolName(original)
	hashInput := original
	if attempt > 0 {
		hashInput = fmt.Sprintf("%s#%d", original, attempt)
	}
	sum := sha256.Sum256([]byte(hashInput))
	const suffixLength = 12
	suffix := fmt.Sprintf("%x", sum[:])[:suffixLength]
	const maxPrefixLength = 64 - 1 - suffixLength
	if len(prefix) > maxPrefixLength {
		prefix = prefix[:maxPrefixLength]
	}
	return prefix + "_" + suffix
}

func sanitizeOpenAIToolName(name string) string {
	var sanitized strings.Builder
	lastWasReplacement := false
	for _, character := range name {
		valid := (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '-'
		if valid {
			sanitized.WriteRune(character)
			lastWasReplacement = false
			continue
		}
		if !lastWasReplacement {
			sanitized.WriteByte('_')
			lastWasReplacement = true
		}
	}
	prefix := strings.Trim(sanitized.String(), "_-")
	if prefix == "" {
		return "tool"
	}
	return prefix
}

type openAIEnvelope struct {
	Choices json.RawMessage `json:"choices"`
	Error   json.RawMessage `json:"error"`
	Usage   json.RawMessage `json:"usage"`
}

// openAIUsage is the shape OpenAI-compatible providers report token counts in.
// Pointers so an omitted field stays unknown: several proxies send `usage`
// with only the field they happen to track.
type openAIUsage struct {
	PromptTokens     *int64 `json:"prompt_tokens"`
	CompletionTokens *int64 `json:"completion_tokens"`
}

type openAIChoice struct {
	Index        *int            `json:"index"`
	Delta        json.RawMessage `json:"delta"`
	FinishReason *string         `json:"finish_reason"`
}

type openAIErrorEnvelope struct {
	Message string          `json:"message"`
	Code    json.RawMessage `json:"code"`
	Type    json.RawMessage `json:"type"`
}

type openAIDelta struct {
	Content   *string               `json:"content"`
	ToolCalls []openAIToolCallDelta `json:"tool_calls"`
}

type openAIToolCallDelta struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type openAIToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

type openAIStreamState struct {
	toolCalls       map[int]*openAIToolCall
	argumentBytes   int
	identifierBytes int
	toolAliases     map[string]string
	usage           *TokenUsage
}

// absorbUsage records token counts from a chunk that carries them.
//
// Lenient by design, unlike everything else this file parses. A malformed
// `usage` object costs a number in a report; failing the stream over it would
// cost the user the answer they were waiting for. The last well-formed report
// wins, which is what a provider that re-sends a growing usage object means.
func (state *openAIStreamState) absorbUsage(raw json.RawMessage) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	var reported openAIUsage
	if err := json.Unmarshal(raw, &reported); err != nil {
		return
	}
	input := nonNegativeTokenCount(reported.PromptTokens)
	output := nonNegativeTokenCount(reported.CompletionTokens)
	if input == nil && output == nil {
		return
	}
	// Merged per field rather than replacing wholesale. A provider that sends
	// prompt and completion counts in one object and then a second object
	// carrying only one of them would otherwise lose the other — the later
	// report is more complete for what it mentions, not for what it omits.
	if state.usage == nil {
		state.usage = &TokenUsage{}
	}
	if input != nil {
		state.usage.InputTokens = input
	}
	if output != nil {
		state.usage.OutputTokens = output
	}
}

// absorbUsageFromChunk pulls usage out of a raw SSE payload without caring
// whether the rest of it parses. Used once the terminal event is already in
// hand, where the only thing still worth reading is the trailing usage chunk
// OpenAI sends after finish_reason.
func (state *openAIStreamState) absorbUsageFromChunk(data []byte) {
	var envelope openAIEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return
	}
	state.absorbUsage(envelope.Usage)
}

func nonNegativeTokenCount(value *int64) *int64 {
	if value == nil || *value < 0 {
		return nil
	}
	return TokenCount(*value)
}

func newOpenAIStreamState(toolAliases ...map[string]string) *openAIStreamState {
	state := &openAIStreamState{toolCalls: make(map[int]*openAIToolCall)}
	if len(toolAliases) > 0 {
		state.toolAliases = toolAliases[0]
	}
	return state
}

func parseOpenAIData(data []byte, state *openAIStreamState) ([]StreamEvent, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var envelope openAIEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, false, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return nil, false, fmt.Errorf("malformed chunk: %w", err)
	}

	// Read before anything can reject the chunk: a provider that reports usage
	// alongside something this parser refuses still measured what it measured.
	state.absorbUsage(envelope.Usage)

	if len(envelope.Error) > 0 {
		if len(envelope.Choices) > 0 {
			return nil, false, fmt.Errorf("malformed error envelope: contains choices")
		}
		event, err := parseOpenAIErrorEnvelope(envelope.Error)
		if err != nil {
			return nil, false, err
		}
		return []StreamEvent{event}, true, nil
	}

	var choices []openAIChoice
	if len(envelope.Choices) == 0 {
		// A chunk whose only content is a usage report is not malformed, it is
		// how several providers deliver the counts requested by
		// stream_options.include_usage. It carries no choice because it
		// describes the response as a whole.
		if len(envelope.Usage) > 0 && string(envelope.Usage) != "null" {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("chunk must contain exactly one choice, got 0")
	}
	if err := json.Unmarshal(envelope.Choices, &choices); err != nil {
		return nil, false, fmt.Errorf("malformed choices: %w", err)
	}
	if len(choices) == 0 && choices != nil {
		return nil, false, nil
	}
	if len(choices) != 1 {
		return nil, false, fmt.Errorf("chunk must contain exactly one choice, got %d", len(choices))
	}
	choice := choices[0]
	if choice.Index != nil && *choice.Index != 0 {
		return nil, false, fmt.Errorf("choice has index %d, want 0", *choice.Index)
	}

	events := make([]StreamEvent, 0, 2)
	if len(choice.Delta) > 0 && string(choice.Delta) != "null" {
		delta, err := decodeOpenAIDelta(choice.Delta)
		if err != nil {
			return nil, false, err
		}
		for _, fragment := range delta.ToolCalls {
			if fragment.Index == nil {
				return nil, false, fmt.Errorf("tool call is missing index")
			}
			index := *fragment.Index
			if index < 0 {
				return nil, false, fmt.Errorf("tool call has negative index %d", index)
			}
			if fragment.Type != "" && fragment.Type != "function" {
				return nil, false, fmt.Errorf("tool call %d has unsupported type %q", index, fragment.Type)
			}
			if err := state.appendToolCallFragment(index, fragment); err != nil {
				return nil, false, err
			}
		}
		if delta.Content != nil && *delta.Content != "" {
			events = append(events, StreamEvent{Type: "delta", Text: *delta.Content})
		}
	}
	if choice.FinishReason != nil {
		if *choice.FinishReason == "tool_calls" {
			if len(state.toolCalls) == 0 {
				return nil, false, fmt.Errorf("finish_reason tool_calls received without any accumulated tool calls")
			}
			calls := make([]ToolCall, 0, len(state.toolCalls))
			ids := make(map[string]struct{}, len(state.toolCalls))
			for index := 0; index < len(state.toolCalls); index++ {
				call, ok := state.toolCalls[index]
				if !ok {
					return nil, false, fmt.Errorf("tool call indices are non-contiguous: missing index %d", index)
				}
				id := call.id
				if id == "" {
					return nil, false, fmt.Errorf("tool call %d is missing an ID", index)
				}
				if _, duplicate := ids[id]; duplicate {
					return nil, false, fmt.Errorf("tool call %d has duplicate ID %q", index, id)
				}
				ids[id] = struct{}{}
				name := call.name
				if name == "" {
					return nil, false, fmt.Errorf("tool call %d is missing a function name", index)
				}
				if original, ok := state.toolAliases[name]; ok {
					name = original
				}
				arguments := make(map[string]any)
				if call.arguments.Len() > 0 {
					decoder := json.NewDecoder(strings.NewReader(call.arguments.String()))
					decoder.UseNumber()
					var decoded any
					if err := decoder.Decode(&decoded); err != nil {
						return nil, false, fmt.Errorf("tool call %d arguments are malformed: %w", index, err)
					}
					if err := decoder.Decode(&struct{}{}); err != io.EOF {
						if err == nil {
							err = fmt.Errorf("multiple JSON values")
						}
						return nil, false, fmt.Errorf("tool call %d arguments are malformed: %w", index, err)
					}
					var ok bool
					arguments, ok = decoded.(map[string]any)
					if !ok {
						return nil, false, fmt.Errorf("tool call %d arguments must be a JSON object", index)
					}
				}
				calls = append(calls, ToolCall{ID: id, Name: name, Arguments: arguments})
			}
			events = append(events, StreamEvent{Type: "tool_call", ToolCalls: calls})
		} else if len(state.toolCalls) > 0 && *choice.FinishReason != "length" {
			return nil, false, fmt.Errorf("finish_reason %q received with unfinished tool calls", *choice.FinishReason)
		}
		events = append(events, StreamEvent{Type: "completed", FinishReason: *choice.FinishReason})
		return events, true, nil
	}
	return events, false, nil
}

func decodeOpenAIDelta(data []byte) (openAIDelta, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var delta openAIDelta
	if err := decoder.Decode(&delta); err != nil {
		return openAIDelta{}, fmt.Errorf("malformed delta: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return openAIDelta{}, fmt.Errorf("delta contains trailing data: %w", err)
	}
	return delta, nil
}

func parseOpenAIErrorEnvelope(data []byte) (StreamEvent, error) {
	var providerError openAIErrorEnvelope
	if err := json.Unmarshal(data, &providerError); err != nil {
		return StreamEvent{}, fmt.Errorf("malformed error envelope: %w", err)
	}
	if strings.TrimSpace(providerError.Message) == "" {
		return StreamEvent{}, fmt.Errorf("malformed error envelope: missing message")
	}

	providerCode, err := openAIErrorString(providerError.Code, "code")
	if err != nil {
		return StreamEvent{}, err
	}
	providerType, err := openAIErrorString(providerError.Type, "type")
	if err != nil {
		return StreamEvent{}, err
	}
	message := "OpenAI-compatible provider error"
	if providerCode != "" {
		message += fmt.Sprintf(" (%s)", providerCode)
	}
	message += ": " + providerError.Message
	return StreamEvent{
		Type:    "error",
		Code:    classifyOpenAIError(providerType, providerCode, providerError.Message),
		Message: message,
	}, nil
}

func openAIErrorString(raw json.RawMessage, field string) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("malformed error envelope: %s must be a non-empty string", field)
	}
	return value, nil
}

func classifyOpenAIError(errorType string, errorCode string, message string) string {
	if isOpenAIQuotaError(errorType, errorCode) {
		return "model_quota_exceeded"
	}
	details := normalizeOpenAIErrorDetails(errorType + " " + errorCode + " " + message)
	if containsOpenAIErrorIndicator(details,
		"authentication", "unauthorized", "api key", "permission", "forbidden",
		"access denied", "account deactivated", "organization deactivated",
	) {
		return "model_auth_failed"
	}
	if containsOpenAIErrorIndicator(details,
		"invalid request", "bad request", "model not found", "invalid model",
		"context length", "context window", "maximum context", "invalid input",
		"invalid parameter", "invalid value", "invalid prompt", "malformed request", "unsupported model",
	) {
		return "model_request_failed"
	}
	if containsOpenAIErrorIndicator(details,
		"rate limit", "too many requests", "quota", "server error", "internal error",
		"internal server", "overload", "service unavailable", "temporarily unavailable",
		"timeout", "capacity",
	) {
		return "model_unavailable"
	}
	return "model_error"
}

func isOpenAIQuotaErrorBody(data []byte) bool {
	events, done, err := parseOpenAIData(data, newOpenAIStreamState())
	return err == nil && done && len(events) == 1 &&
		events[0].Type == "error" && events[0].Code == "model_quota_exceeded"
}

func isOpenAIQuotaError(errorType string, errorCode string) bool {
	for _, signal := range []string{errorType, errorCode} {
		switch strings.TrimSpace(normalizeOpenAIErrorDetails(signal)) {
		case "insufficient quota", "quota exceeded", "quota exhausted", "exceeded quota",
			"billing hard limit reached", "billing hard limit exceeded",
			"credit balance exhausted", "credits exhausted":
			return true
		}
	}
	return false
}

func normalizeOpenAIErrorDetails(details string) string {
	details = strings.ToLower(details)
	return strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(details)
}

func containsOpenAIErrorIndicator(details string, indicators ...string) bool {
	for _, indicator := range indicators {
		if strings.Contains(details, indicator) {
			return true
		}
	}
	return false
}

func (state *openAIStreamState) appendToolCallFragment(index int, fragment openAIToolCallDelta) error {
	call := state.toolCalls[index]
	if call == nil && len(state.toolCalls) >= maxOpenAIToolCalls {
		return fmt.Errorf("tool call count exceeds %d", maxOpenAIToolCalls)
	}
	if call != nil && fragment.ID != "" && call.id != "" && fragment.ID != call.id {
		return fmt.Errorf("tool call %d has conflicting ID %q after %q", index, fragment.ID, call.id)
	}
	if call != nil && fragment.Function.Name != "" && call.name != "" && fragment.Function.Name != call.name {
		return fmt.Errorf(
			"tool call %d has conflicting function name %q after %q",
			index,
			fragment.Function.Name,
			call.name,
		)
	}

	argumentFragment := ""
	if len(fragment.Function.Arguments) > 0 {
		var decoded any
		if err := json.Unmarshal(fragment.Function.Arguments, &decoded); err != nil {
			return fmt.Errorf("tool call %d arguments must be a string: %w", index, err)
		}
		// A JSON null carries no fragment, which is what a zero-argument call
		// looks like from servers that send the field anyway. It means the same
		// as omitting the field, which this function already accepts, so leave
		// the fragment empty rather than failing the run before the tool is ever
		// dispatched. Anything else non-string is still a protocol error.
		if decoded != nil {
			var ok bool
			argumentFragment, ok = decoded.(string)
			if !ok {
				return fmt.Errorf("tool call %d arguments must be a string", index)
			}
		}
	}

	identifierBytes := state.identifierBytes
	if fragment.ID != "" {
		if call != nil {
			identifierBytes -= len(call.id)
		}
		identifierBytes += len(fragment.ID)
	}
	if fragment.Function.Name != "" {
		if call != nil {
			identifierBytes -= len(call.name)
		}
		identifierBytes += len(fragment.Function.Name)
	}
	if identifierBytes > maxOpenAIToolCallAggregateIdentifierBytes {
		return fmt.Errorf("tool call ID and name bytes exceed %d", maxOpenAIToolCallAggregateIdentifierBytes)
	}

	argumentBytes := len(argumentFragment)
	currentCallArgumentBytes := 0
	if call != nil {
		currentCallArgumentBytes = call.arguments.Len()
	}
	if argumentBytes > maxOpenAIToolCallArgumentBytes-currentCallArgumentBytes {
		return fmt.Errorf("tool call %d arguments exceed %d bytes", index, maxOpenAIToolCallArgumentBytes)
	}
	if argumentBytes > maxOpenAIToolCallAggregateArgumentBytes-state.argumentBytes {
		return fmt.Errorf("aggregate arguments exceed %d bytes", maxOpenAIToolCallAggregateArgumentBytes)
	}

	if call == nil {
		call = &openAIToolCall{}
		state.toolCalls[index] = call
	}
	if fragment.ID != "" {
		call.id = fragment.ID
	}
	if fragment.Function.Name != "" {
		call.name = fragment.Function.Name
	}
	call.arguments.WriteString(argumentFragment)
	state.identifierBytes = identifierBytes
	state.argumentBytes += argumentBytes
	return nil
}
