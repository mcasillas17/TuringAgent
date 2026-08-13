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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type smokeConfig struct {
	addr           string
	token          string
	model          string
	attemptTimeout time.Duration
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
		var exitErr *smokeExitError
		if errors.As(err, &exitErr) {
			fmt.Fprintln(os.Stderr, err)
		} else {
			fmt.Fprintf(os.Stderr, "gRPC smoke failed: %v\n", err)
		}
		os.Exit(smokeExitCode(err))
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("grpc-smoke-client", flag.ContinueOnError)
	healthOnly := flags.Bool("health-only", false, "only run the HealthService.Check probe")
	modelDriven := flags.Bool("model-driven", false, "send a natural-language prompt and require the model to choose a tool")
	attempts := flags.Int("attempts", 3, "how many times to retry when the model does not call a tool")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *healthOnly && *modelDriven {
		return errors.New("-health-only and -model-driven cannot be used together")
	}
	if *attempts < 1 {
		return errors.New("-attempts must be at least 1")
	}

	cfg, err := loadConfig()
	if err != nil {
		if *modelDriven {
			return &smokeExitError{code: 2, message: "INCONCLUSIVE: " + err.Error()}
		}
		return err
	}

	timeout := 2 * time.Minute
	if *healthOnly {
		timeout = 2 * time.Second
	} else if *modelDriven {
		timeout = time.Duration(*attempts)*cfg.attemptTimeout + 30*time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// NewClient is lazy, so the old WithBlock behaviour (wait for the stack to
	// come up) is reproduced by Connect plus WaitForReady on the health probe
	// below. "passthrough:///" keeps the address opaque to the resolver exactly
	// as DialContext did, so dialLocalGRPC still receives host:port verbatim.
	conn, err := grpc.NewClient("passthrough:///"+cfg.addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(dialLocalGRPC))
	if err != nil {
		if *modelDriven {
			return &smokeExitError{code: 2, message: fmt.Sprintf("INCONCLUSIVE: create client for %s: %v", cfg.addr, err)}
		}
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
		if *modelDriven {
			return &smokeExitError{code: 2, message: "INCONCLUSIVE: " + err.Error()}
		}
		return err
	}
	if *healthOnly {
		fmt.Println("gRPC health check OK")
		return nil
	}
	if *modelDriven {
		verdict := client.runModelDrivenSmoke(ctx, cfg.model, *attempts, cfg.attemptTimeout)
		if verdict.status == verificationPass {
			fmt.Println(verdict.String())
			return nil
		}
		return &smokeExitError{code: verdict.exitCode(), message: verdict.String()}
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
	model := os.Getenv("TURING_VERIFY_MODEL")
	if model == "" {
		model = lookup("OLLAMA_MODEL")
	}
	if model == "" {
		model = "llama3.2"
	}
	jobTimeout := lookup("TURING_JOB_TIMEOUT_MS")
	if jobTimeout == "" {
		jobTimeout = "300000"
	}
	parsedJobTimeout, err := time.ParseDuration(jobTimeout + "ms")
	if err != nil || parsedJobTimeout <= 0 {
		return smokeConfig{}, fmt.Errorf("TURING_JOB_TIMEOUT_MS must be a positive millisecond duration, got %q", jobTimeout)
	}
	return smokeConfig{
		addr:           "localhost:" + port,
		token:          token,
		model:          model,
		attemptTimeout: parsedJobTimeout + 30*time.Second,
	}, nil
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

const modelDrivenPrompt = "What is the current time? Use the tools available to you and include the exact timestamp returned by the tool."

var (
	errToolPayloadContract = errors.New("tool lifecycle payload contract violation")
	errAnswerUncorrelated  = errors.New("final answer did not reflect the timestamp returned by the tool")
	errToolReportedFailure = errors.New("tool reported a model-caused failure")
	clockPattern           = regexp.MustCompile(`(?i)\b([0-9]{1,2}):([0-9]{2})(?::([0-9]{2}))?\s*(AM|PM)?\b`)
	rfc3339Pattern         = regexp.MustCompile(`[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})`)
	epochPattern           = regexp.MustCompile(`\b[0-9]{10,13}\b`)
)

type verificationStatus int

const (
	verificationPass verificationStatus = iota
	verificationFail
	verificationInconclusive
)

type modelDrivenAttempt struct {
	toolCalled    bool
	toolCallID    string
	toolName      string
	answer        string
	observedTools []string
}

type modelDrivenVerdict struct {
	status   verificationStatus
	model    string
	attempt  int
	attempts int
	toolName string
	answer   string
	replies  []string
	cause    error
}

func (v modelDrivenVerdict) exitCode() int {
	switch v.status {
	case verificationPass:
		return 0
	case verificationInconclusive:
		return 2
	default:
		return 1
	}
}

func (v modelDrivenVerdict) String() string {
	switch v.status {
	case verificationPass:
		return fmt.Sprintf(
			"PASS: model=%s chose %s on attempt %d/%d; answer=%q",
			v.model,
			v.toolName,
			v.attempt,
			v.attempts,
			v.answer,
		)
	case verificationInconclusive:
		var out strings.Builder
		fmt.Fprintf(
			&out,
			"INCONCLUSIVE: model=%s did not complete the verified system.time loop in %d attempts.\n",
			v.model,
			v.attempts,
		)
		for index, reply := range v.replies {
			if strings.HasPrefix(reply, "error: ") {
				fmt.Fprintf(&out, "  attempt %d error: %q\n", index+1, strings.TrimPrefix(reply, "error: "))
			} else {
				fmt.Fprintf(&out, "  attempt %d answer: %q\n", index+1, reply)
			}
		}
		out.WriteString("  This is a model-capability result, not necessarily a code defect. Re-run with\n")
		out.WriteString("  TURING_VERIFY_MODEL=<a stronger tool-calling model> to tell them apart.")
		return out.String()
	default:
		if v.toolName != "" {
			return fmt.Sprintf("FAIL: model=%s called %s but %v", v.model, v.toolName, v.cause)
		}
		return fmt.Sprintf("FAIL: model=%s verification failed: %v", v.model, v.cause)
	}
}

type smokeExitError struct {
	code    int
	message string
}

func (e *smokeExitError) Error() string {
	return e.message
}

func smokeExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *smokeExitError
	if errors.As(err, &exitErr) {
		return exitErr.code
	}
	return 1
}

func validateModelDrivenPrompt(prompt string) error {
	if strings.HasPrefix(strings.TrimSpace(prompt), "/tool") {
		return errors.New("model-driven prompt must not use the /tool debug shortcut")
	}
	return nil
}

func (c smokeClient) runModelDrivenSmoke(
	ctx context.Context,
	model string,
	attempts int,
	attemptTimeout time.Duration,
) modelDrivenVerdict {
	if err := validateModelDrivenPrompt(modelDrivenPrompt); err != nil {
		return modelDrivenVerdict{
			status:   verificationFail,
			model:    model,
			attempts: attempts,
			cause:    err,
		}
	}
	return verifyModelDrivenAttempts(model, attempts, func(attempt int) (modelDrivenAttempt, error) {
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		defer cancel()
		return c.runModelDrivenAttempt(attemptCtx, model, attempt)
	})
}

func (c smokeClient) runModelDrivenAttempt(ctx context.Context, model string, attempt int) (modelDrivenAttempt, error) {
	session, err := c.sessions.CreateSession(c.withAuth(ctx), &turingv1.CreateSessionRequest{
		Title: fmt.Sprintf("Live tool-loop verification %d", attempt),
	})
	if err != nil {
		return modelDrivenAttempt{}, fmt.Errorf("SessionService.CreateSession: %w", err)
	}
	sessionID := session.GetSessionId()
	if sessionID == "" {
		return modelDrivenAttempt{}, errors.New("SessionService.CreateSession returned an empty session_id")
	}

	stream, err := c.chat.SendMessage(c.withAuth(ctx), &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       modelDrivenPrompt,
		ContentType:   "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         model,
	})
	if err != nil {
		return modelDrivenAttempt{}, fmt.Errorf("ChatService.SendMessage: %w", err)
	}
	return readModelDrivenStream(stream)
}

func readModelDrivenStream(stream turingv1.ChatService_SendMessageClient) (modelDrivenAttempt, error) {
	var events []*turingv1.ChatStreamEvent
	var receiveErr error
	for {
		event, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			receiveErr = fmt.Errorf("receive ChatService.SendMessage event: %w", err)
			break
		}
		events = append(events, event)
		if event.GetRunCompleted() != nil || event.GetRunFailed() != nil || event.GetRunCancelled() != nil {
			break
		}
	}

	attempt, err := analyzeModelDrivenEvents(events)
	if receiveErr != nil {
		return attempt, receiveErr
	}
	return attempt, err
}

func verifyModelDrivenAttempts(
	model string,
	attempts int,
	runAttempt func(attempt int) (modelDrivenAttempt, error),
) modelDrivenVerdict {
	if attempts < 1 {
		return modelDrivenVerdict{
			status:   verificationFail,
			model:    model,
			attempts: attempts,
			cause:    errors.New("attempt count must be at least 1"),
		}
	}

	replies := make([]string, 0, attempts)
	for attemptNumber := 1; attemptNumber <= attempts; attemptNumber++ {
		attempt, err := runAttempt(attemptNumber)
		if err != nil {
			if errors.Is(err, errToolPayloadContract) {
				return modelDrivenVerdict{
					status:   verificationFail,
					model:    model,
					attempt:  attemptNumber,
					attempts: attempts,
					toolName: attempt.toolName,
					answer:   attempt.answer,
					replies:  replies,
					cause:    err,
				}
			}
			if !attempt.toolCalled ||
				errors.Is(err, errAnswerUncorrelated) ||
				errors.Is(err, errToolReportedFailure) ||
				isModelGuardrailFailure(err) {
				reply := "error: " + err.Error()
				if attempt.answer != "" {
					reply += fmt.Sprintf("; answer=%q", attempt.answer)
				}
				replies = append(replies, reply)
				continue
			}
			return modelDrivenVerdict{
				status:   verificationFail,
				model:    model,
				attempt:  attemptNumber,
				attempts: attempts,
				toolName: attempt.toolName,
				answer:   attempt.answer,
				replies:  replies,
				cause:    err,
			}
		}
		if attempt.toolCalled {
			return modelDrivenVerdict{
				status:   verificationPass,
				model:    model,
				attempt:  attemptNumber,
				attempts: attempts,
				toolName: attempt.toolName,
				answer:   attempt.answer,
				replies:  replies,
			}
		}
		reply := attempt.answer
		if len(attempt.observedTools) > 0 {
			reply = fmt.Sprintf("called %s instead of system.time; answer=%q", strings.Join(attempt.observedTools, ", "), reply)
		}
		replies = append(replies, reply)
	}

	return modelDrivenVerdict{
		status:   verificationInconclusive,
		model:    model,
		attempts: attempts,
		replies:  replies,
	}
}

type observedToolEvent struct {
	eventType    turingv1.TuringEventType
	toolCallID   string
	toolName     string
	errorMessage string
	observedAt   time.Time
}

type modelDrivenRunFailure struct {
	code    string
	message string
}

func (e *modelDrivenRunFailure) Error() string {
	return fmt.Sprintf("run failed code=%q message=%q", e.code, e.message)
}

func isModelGuardrailFailure(err error) bool {
	var failure *modelDrivenRunFailure
	if !errors.As(err, &failure) {
		return false
	}
	switch failure.code {
	case "tool_call_limit_exceeded",
		"tool_result_limit_exceeded",
		"model_output_limit_exceeded",
		"model_stream_failed",
		"model_stream_error",
		"model_unavailable",
		"model_request_failed",
		"model_auth_failed",
		"model_bad_chunk",
		"model_error",
		"model_quota_exceeded":
		return true
	default:
		return false
	}
}

func analyzeModelDrivenEvents(events []*turingv1.ChatStreamEvent) (modelDrivenAttempt, error) {
	var attempt modelDrivenAttempt
	var terminal bool
	var runErr error
	started := make(map[string]string)
	completed := make(map[string]string)
	failed := make(map[string]string)
	failedIndex := make(map[string]int)
	startedIndex := make(map[string]int)
	completedIndex := make(map[string]int)
	startedAt := make(map[string]time.Time)
	messageCompletedIndex := -1
	lastToolCompletedIndex := -1
	var lastToolCallID string
	var lastToolCompletedAt time.Time
	var postToolText strings.Builder
	var sawTokenDelta bool
	var targetCompleted bool
	observedTools := make(map[string]struct{})

	for index, event := range events {
		if delta := event.GetTokenDelta(); delta != nil {
			sawTokenDelta = true
			if targetCompleted {
				postToolText.WriteString(delta.GetDelta())
			}
		}
		if message := event.GetMessageCompleted(); message != nil {
			attempt.answer = message.GetContent()
			messageCompletedIndex = index
		}
		if event.GetRunCompleted() != nil {
			terminal = true
		}
		if failed := event.GetRunFailed(); failed != nil {
			terminal = true
			runErr = &modelDrivenRunFailure{code: failed.GetCode(), message: failed.GetMessage()}
		}
		if cancelled := event.GetRunCancelled(); cancelled != nil {
			terminal = true
			runErr = fmt.Errorf("run was cancelled: %s", cancelled.GetReason())
		}

		toolEvent, ok := modelDrivenToolEvent(event)
		if !ok {
			continue
		}
		if toolEvent.toolName != "" {
			observedTools[toolEvent.toolName] = struct{}{}
		}
		if toolEvent.toolCallID == "" {
			return attempt, fmt.Errorf("%w: empty toolCallId", errToolPayloadContract)
		}
		if toolEvent.toolName == "" {
			return attempt, fmt.Errorf("%w: empty toolName", errToolPayloadContract)
		}
		if toolEvent.toolName != "system.time" {
			continue
		}

		switch toolEvent.eventType {
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED:
			attempt.toolCalled = true
			if attempt.toolName == "" {
				attempt.toolName = toolEvent.toolName
			}
			started[toolEvent.toolCallID] = toolEvent.toolName
			startedIndex[toolEvent.toolCallID] = index
			startedAt[toolEvent.toolCallID] = toolEvent.observedAt
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED:
			attempt.toolCalled = true
			completed[toolEvent.toolCallID] = toolEvent.toolName
			completedIndex[toolEvent.toolCallID] = index
			lastToolCompletedIndex = index
			lastToolCallID = toolEvent.toolCallID
			lastToolCompletedAt = toolEvent.observedAt
			targetCompleted = true
			postToolText.Reset()
			if attempt.toolCallID == "" {
				attempt.toolCallID = toolEvent.toolCallID
				attempt.toolName = toolEvent.toolName
			}
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED:
			attempt.toolCalled = true
			attempt.toolName = toolEvent.toolName
			if _, duplicate := failedIndex[toolEvent.toolCallID]; duplicate {
				return attempt, fmt.Errorf("tool call %s emitted a duplicate failure", toolEvent.toolCallID)
			}
			failed[toolEvent.toolCallID] = toolEvent.errorMessage
			failedIndex[toolEvent.toolCallID] = index
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED:
			attempt.toolCalled = true
			attempt.toolCallID = toolEvent.toolCallID
			attempt.toolName = toolEvent.toolName
			return attempt, fmt.Errorf("tool call %s was denied", toolEvent.toolCallID)
		}
	}

	for toolName := range observedTools {
		attempt.observedTools = append(attempt.observedTools, toolName)
	}
	slices.Sort(attempt.observedTools)
	if !terminal {
		return attempt, errors.New("stream ended without a terminal run event")
	}
	if !attempt.toolCalled {
		if runErr != nil && attempt.answer == "" {
			attempt.answer = runErr.Error()
		}
		return attempt, nil
	}
	for toolCallID := range completed {
		if _, failedCall := failed[toolCallID]; failedCall {
			return attempt, fmt.Errorf("tool call %s reached conflicting terminal states", toolCallID)
		}
		startIndex, ok := startedIndex[toolCallID]
		if !ok {
			return attempt, fmt.Errorf("tool call %s completed without a matching start", toolCallID)
		}
		if completedIndex[toolCallID] <= startIndex {
			return attempt, fmt.Errorf("tool call %s completed before it started", toolCallID)
		}
	}
	for toolCallID, failIndex := range failedIndex {
		startIndex, ok := startedIndex[toolCallID]
		if !ok {
			return attempt, fmt.Errorf("tool call %s failed without a matching start", toolCallID)
		}
		if failIndex <= startIndex {
			return attempt, fmt.Errorf("tool call %s failed before it started", toolCallID)
		}
	}
	for toolCallID, toolName := range started {
		completedName, ok := completed[toolCallID]
		if !ok {
			if _, failedCall := failed[toolCallID]; failedCall {
				continue
			}
			return attempt, fmt.Errorf("tool call %s did not reach a terminal tool event", toolCallID)
		}
		if completedName != toolName {
			return attempt, fmt.Errorf(
				"tool call %s completed as %q after starting as %q",
				toolCallID,
				completedName,
				toolName,
			)
		}
	}
	var toolFailures error
	if len(failed) > 0 {
		toolFailures = toolFailuresError(failed)
		if !allToolFailuresRecoverable(failed) {
			return attempt, toolFailures
		}
	}
	if runErr != nil {
		return attempt, runErr
	}
	if len(completed) == 0 && toolFailures != nil {
		return attempt, fmt.Errorf("%w: %v", errToolReportedFailure, toolFailures)
	}
	for toolCallID, index := range failedIndex {
		if index > lastToolCompletedIndex {
			return attempt, fmt.Errorf(
				"%w: latest system.time call %s failed after the last successful completion: %v",
				errToolReportedFailure,
				toolCallID,
				toolFailures,
			)
		}
	}
	if strings.TrimSpace(attempt.answer) == "" {
		return attempt, errors.New("tool ran but the model produced no final answer")
	}
	if messageCompletedIndex <= lastToolCompletedIndex {
		return attempt, errors.New("model produced no final answer after tool completion")
	}
	correlationAnswer := attempt.answer
	if sawTokenDelta {
		correlationAnswer = postToolText.String()
		if strings.TrimSpace(correlationAnswer) == "" {
			return attempt, errors.New("model produced no answer content after tool completion")
		}
	}
	if !answerMatchesToolTime(correlationAnswer, startedAt[lastToolCallID], lastToolCompletedAt) {
		return attempt, errAnswerUncorrelated
	}
	return attempt, nil
}

func allToolFailuresRecoverable(failed map[string]string) bool {
	if len(failed) == 0 {
		return false
	}
	for _, message := range failed {
		normalized := strings.ToLower(message)
		if !strings.Contains(normalized, "invalid params") &&
			!strings.Contains(normalized, "invalid argument") &&
			!strings.Contains(normalized, "unknown argument") &&
			!strings.Contains(normalized, "does not accept arguments") {
			return false
		}
	}
	return true
}

func toolFailuresError(failed map[string]string) error {
	details := make([]string, 0, len(failed))
	for toolCallID, message := range failed {
		if message == "" {
			message = "tool call failed"
		}
		details = append(details, fmt.Sprintf("%s: %s", toolCallID, message))
	}
	slices.Sort(details)
	return errors.New(strings.Join(details, "; "))
}

func answerMatchesToolTime(answer string, startedAt, completedAt time.Time) bool {
	if startedAt.IsZero() || completedAt.IsZero() || completedAt.Before(startedAt) {
		return false
	}
	windowStart := startedAt.Add(-2 * time.Second)
	windowEnd := completedAt.Add(2 * time.Second)
	for _, candidate := range rfc3339Pattern.FindAllString(answer, -1) {
		parsed, err := time.Parse(time.RFC3339Nano, candidate)
		if err == nil && !parsed.Before(windowStart) && !parsed.After(windowEnd) {
			return true
		}
	}
	for _, candidate := range epochPattern.FindAllString(answer, -1) {
		epoch, err := strconv.ParseInt(candidate, 10, 64)
		if err != nil {
			continue
		}
		var epochTime time.Time
		if epoch > 100_000_000_000 {
			epochTime = time.UnixMilli(epoch)
		} else {
			epochTime = time.Unix(epoch, 0)
		}
		if !epochTime.Before(windowStart) && !epochTime.After(windowEnd) {
			return true
		}
	}
	for _, field := range strings.Fields(answer) {
		candidate := strings.Trim(field, "`*_()[]{}<>,;.!?\"'")
		parsed, err := time.Parse(time.RFC3339Nano, candidate)
		if err == nil && !parsed.Before(windowStart) && !parsed.After(windowEnd) {
			return true
		}
		epoch, err := strconv.ParseInt(candidate, 10, 64)
		if err != nil {
			continue
		}
		var epochTime time.Time
		if epoch > 100_000_000_000 {
			epochTime = time.UnixMilli(epoch)
		} else {
			epochTime = time.Unix(epoch, 0)
		}
		if !epochTime.Before(windowStart) && !epochTime.After(windowEnd) {
			return true
		}
	}
	for _, match := range clockPattern.FindAllStringSubmatch(answer, -1) {
		hour, hourErr := strconv.Atoi(match[1])
		minute, minuteErr := strconv.Atoi(match[2])
		if hourErr != nil || minuteErr != nil || minute > 59 {
			continue
		}
		second := -1
		if match[3] != "" {
			var secondErr error
			second, secondErr = strconv.Atoi(match[3])
			if secondErr != nil || second > 59 {
				continue
			}
		}
		period := strings.ToUpper(match[4])
		if period != "" {
			if hour < 1 || hour > 12 {
				continue
			}
			hour %= 12
			if period == "PM" {
				hour += 12
			}
		} else if hour > 23 {
			continue
		}
		for candidate := windowStart; !candidate.After(windowEnd); candidate = candidate.Add(time.Second) {
			candidate = candidate.UTC()
			if candidate.Hour() == hour &&
				candidate.Minute() == minute &&
				(second < 0 || candidate.Second() == second) {
				return true
			}
		}
	}
	return false
}

func modelDrivenToolEvent(event *turingv1.ChatStreamEvent) (observedToolEvent, bool) {
	if started := event.GetToolCallStarted(); started != nil {
		return observedToolEvent{
			eventType:  turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED,
			toolCallID: started.GetToolCallId(),
			toolName:   started.GetToolName(),
		}, true
	}
	if completed := event.GetToolCallCompleted(); completed != nil {
		return observedToolEvent{
			eventType:  turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED,
			toolCallID: completed.GetToolCallId(),
			toolName:   completed.GetToolName(),
		}, true
	}
	if failed := event.GetToolCallFailed(); failed != nil {
		return observedToolEvent{
			eventType:    turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			toolCallID:   failed.GetToolCallId(),
			toolName:     failed.GetToolName(),
			errorMessage: failed.GetPayload().GetFields()["error"].GetStringValue(),
		}, true
	}

	persisted := event.GetPersistedEvent()
	if persisted == nil {
		return observedToolEvent{}, false
	}
	switch persisted.GetType() {
	case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_EXPIRED:
		return observedToolEvent{
			eventType:    persisted.GetType(),
			toolCallID:   persisted.GetPayload().GetFields()["toolCallId"].GetStringValue(),
			toolName:     persisted.GetPayload().GetFields()["toolName"].GetStringValue(),
			errorMessage: persisted.GetPayload().GetFields()["error"].GetStringValue(),
			observedAt:   persisted.GetCreatedAt().AsTime(),
		}, true
	default:
		return observedToolEvent{}, false
	}
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
