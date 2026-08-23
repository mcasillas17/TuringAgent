package telemetry

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Pinned, so nothing below depends on when the suite runs.
var asOf = time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)

func TestGetTelemetrySummaryDefaultsToAWeekAndSaysSo(t *testing.T) {
	server, _ := newTestServer(t)

	response, err := server.GetTelemetrySummary(context.Background(), &turingv1.GetTelemetrySummaryRequest{})
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}

	// The window comes back with the answer so a client labels the numbers
	// with the span the server used rather than the one it assumed.
	if response.GetWindow().GetDays() != DefaultWindowDays {
		t.Fatalf("window days = %d, want %d", response.GetWindow().GetDays(), DefaultWindowDays)
	}
	if got := response.GetWindow().GetEnd().AsTime(); !got.Equal(asOf) {
		t.Fatalf("window end = %s, want %s", got, asOf)
	}
	// Snapped to a UTC day boundary so the daily series has exactly one bucket
	// per day the label promises.
	wantStart := time.Date(2026, time.March, 9, 0, 0, 0, 0, time.UTC)
	if got := response.GetWindow().GetStart().AsTime(); !got.Equal(wantStart) {
		t.Fatalf("window start = %s, want %s", got, wantStart)
	}
	if len(response.GetDaily()) != DefaultWindowDays {
		t.Fatalf("daily entries = %d, want one per day of the window", len(response.GetDaily()))
	}
}

func TestGetTelemetrySummaryHonoursTheRequestedWindow(t *testing.T) {
	server, _ := newTestServer(t)

	response, err := server.GetTelemetrySummary(context.Background(), &turingv1.GetTelemetrySummaryRequest{WindowDays: 30})
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}

	if response.GetWindow().GetDays() != 30 {
		t.Fatalf("window days = %d, want 30", response.GetWindow().GetDays())
	}
	if len(response.GetDaily()) != 30 {
		t.Fatalf("daily entries = %d, want one per day of the window", len(response.GetDaily()))
	}
}

// A window the server cannot answer is refused rather than quietly narrowed:
// a page that says "last 400 days" over numbers covering 365 is a lie the
// client cannot detect.
func TestGetTelemetrySummaryRejectsAnImpossibleWindowAsInvalidArgument(t *testing.T) {
	server, _ := newTestServer(t)

	for _, days := range []int32{-1, repository.MaxTelemetryWindowDays + 1} {
		_, err := server.GetTelemetrySummary(context.Background(), &turingv1.GetTelemetrySummaryRequest{WindowDays: days})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("window %d error code = %s, want InvalidArgument", days, status.Code(err))
		}
		if !strings.Contains(status.Convert(err).Message(), "365") {
			t.Fatalf("window %d message = %q, want the allowed range", days, status.Convert(err).Message())
		}
	}
}

func TestGetTelemetrySummaryRejectsANilRequest(t *testing.T) {
	server, _ := newTestServer(t)

	if _, err := server.GetTelemetrySummary(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("nil request error code = %s, want InvalidArgument", status.Code(err))
	}
}

// A storage failure must not hand its text to the caller, and must not come
// back as a client error the client would try to "fix" by asking differently.
func TestGetTelemetrySummaryReportsStorageFailuresAsInternalWithoutDetail(t *testing.T) {
	server, database := newTestServer(t)
	if _, err := database.ExecContext(context.Background(), `DROP TABLE tool_calls`); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	_, err := server.GetTelemetrySummary(context.Background(), &turingv1.GetTelemetrySummaryRequest{WindowDays: 7})
	if status.Code(err) != codes.Internal {
		t.Fatalf("error code = %s, want Internal", status.Code(err))
	}
	message := status.Convert(err).Message()
	if message != "telemetry summary failed" {
		t.Fatalf("message = %q, want the fixed text with no storage detail", message)
	}
	if strings.Contains(strings.ToLower(message), "tool_calls") {
		t.Fatalf("message = %q, want no schema detail", message)
	}
}

// The provenance the whole feature turns on has to survive the proto boundary:
// an unreported token count must arrive at the client ABSENT, so it can say
// "not reported" instead of drawing a zero.
func TestGetTelemetrySummaryCarriesUnknownTokenCountsThroughAsAbsent(t *testing.T) {
	server, database := newTestServer(t)
	ctx := context.Background()
	repo := repository.New(database)
	session, err := repo.CreateSession(ctx, "Chat")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	silent := enqueueAndComplete(t, repo, session.SessionID, nil)
	reported := enqueueAndComplete(t, repo, session.SessionID, &repository.RunTokenUsage{
		InputTokens: tokenCount(70), OutputTokens: tokenCount(8),
	})
	if silent == reported {
		t.Fatal("the two runs share an id")
	}

	// A window wide enough to hold rows written at time.Now, since the runs
	// above go through the real enqueue path and are stamped by it.
	server.now = func() time.Time { return time.Now().UTC() }
	response, err := server.GetTelemetrySummary(ctx, &turingv1.GetTelemetrySummaryRequest{WindowDays: 1})
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}

	tokens := response.GetTokens()
	if tokens.GetRunsWithUsage() != 1 || tokens.GetRunsWithoutUsage() != 1 {
		t.Fatalf("token provenance = %+v, want one of each", tokens)
	}
	if tokens.InputTokens == nil || tokens.GetInputTokens() != 70 {
		t.Fatalf("input tokens = %v, want 70", tokens.InputTokens)
	}
	// These runs did start and finish, so their average IS measurable and must
	// be present — a fast run is not an unmeasured one. The absent case is
	// covered where it actually arises, over a window with no finished runs.
	if response.GetRuns().AverageDurationMs == nil {
		t.Fatal("average duration is absent for runs that recorded both timestamps")
	}
}

func TestGetTelemetrySummaryLeavesTheAverageAbsentWithNothingToAverage(t *testing.T) {
	server, _ := newTestServer(t)

	response, err := server.GetTelemetrySummary(context.Background(), &turingv1.GetTelemetrySummaryRequest{WindowDays: 7})
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}

	if response.GetRuns().AverageDurationMs != nil {
		t.Fatalf("average duration = %d, want absent rather than zero", response.GetRuns().GetAverageDurationMs())
	}
	if response.GetTokens().InputTokens != nil {
		t.Fatalf("input tokens = %d, want absent", response.GetTokens().GetInputTokens())
	}
}

func newTestServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "turing.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return NewWithClock(repository.New(database), func() time.Time { return asOf }), database
}

func enqueueAndComplete(t *testing.T, repo *repository.Repository, sessionID string, usage *repository.RunTokenUsage) string {
	t.Helper()
	ctx := context.Background()
	enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: sessionID, Content: "hi", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "qwen2.5:7b",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatalf("read run state: %v", err)
	}
	if _, err := repo.CompleteRunCanonical(ctx, repository.CompleteRunInput{
		RunID:                enqueued.RunID,
		AssistantMessageID:   enqueued.AssistantMessageID,
		Content:              "done",
		ExpectedStateVersion: state.StateVersion,
		Usage:                usage,
	}); err != nil {
		t.Fatalf("complete run: %v", err)
	}
	return enqueued.RunID
}

func tokenCount(value int64) *int64 { return &value }
