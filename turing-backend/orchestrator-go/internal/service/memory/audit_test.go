package memory

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// auditRowsFor reads the stored trail for one action straight back out of the
// repository, because the thing under test is what was written down — the
// correlation, the actor and the payload — not what a reader would project.
func auditRowsFor(t *testing.T, repo *repository.Repository, ctx context.Context, action string) []repository.AuditRecord {
	t.Helper()
	records, err := repo.ListAuditRecords(ctx, repository.AuditQuery{
		Action: sql.NullString{String: action, Valid: true},
		Order:  repository.AuditOrderAscending,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListAuditRecords(%s): %v", action, err)
	}
	return records
}

func onlyAuditRowFor(t *testing.T, repo *repository.Repository, ctx context.Context, action string) repository.AuditRecord {
	t.Helper()
	records := auditRowsFor(t, repo, ctx, action)
	if len(records) != 1 {
		t.Fatalf("%s rows = %d, want exactly one", action, len(records))
	}
	return records[0]
}

// deleteSession runs the whole withdrawal the user's delete performs, which is
// the only thing that scrubs an audit payload.
func deleteSession(t *testing.T, repo *repository.Repository, ctx context.Context, sessionID string) {
	t.Helper()
	if _, err := repo.BeginSessionDeletion(ctx, sessionID); err != nil {
		t.Fatalf("BeginSessionDeletion: %v", err)
	}
	receipt, err := repo.AdvanceSessionDeletion(ctx, sessionID)
	if err != nil {
		t.Fatalf("AdvanceSessionDeletion: %v", err)
	}
	if receipt.State != "completed" {
		t.Fatalf("deletion state = %q, want completed", receipt.State)
	}
}

func remember(t *testing.T, service *Server, ctx context.Context, runID string, title, body string) string {
	t.Helper()
	return rememberKind(t, service, ctx, runID, title, body, "")
}

func rememberKind(t *testing.T, service *Server, ctx context.Context, runID string, title, body, kind string) string {
	t.Helper()
	args := map[string]any{"title": title, "body": body}
	if kind != "" {
		args["kind"] = kind
	}
	response, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRemember, Args: callArgs(t, args),
	})
	if err != nil {
		t.Fatalf("memory.remember: %v", err)
	}
	candidateID, _ := response.GetResult().AsMap()["candidate_id"].(string)
	if candidateID == "" {
		t.Fatalf("remember returned no candidate id: %v", response.GetResult().AsMap())
	}
	return candidateID
}

// memory.remember is a model's move inside one conversation, not something the
// person sitting there did to their account. The row it leaves has to say so:
// correlated to the run, attributed to the runtime, and carrying the identity
// of the proposal rather than the claim.
func TestMemoryProposalIsAuditedAgainstTheRunThatMadeIt(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")
	const body = "Their bank card ends 4242 and they hate being asked about it."

	candidateID := remember(t, service, ctx, runID, "Card", body)

	record := onlyAuditRowFor(t, repo, ctx, "memory.tool.proposed")
	if !record.CorrelationID.Valid || record.CorrelationID.String != runID {
		t.Fatalf("correlation = %v, want the run %q that made the proposal", record.CorrelationID, runID)
	}
	if record.ActorType != "runtime" {
		t.Fatalf("actor type = %q, want runtime: no person asked for this row", record.ActorType)
	}
	if record.ActorID.Valid && record.ActorID.String != "" {
		t.Fatalf("actor id = %v, want an empty runtime identity", record.ActorID)
	}
	if !record.Target.Valid || record.Target.String != candidateID {
		t.Fatalf("target = %v, want the candidate %q", record.Target, candidateID)
	}

	// The payload names the proposal's kind and nothing else. A body is the
	// private half of a memory and an audit row is not where it goes.
	if !record.PayloadPresent || !record.PayloadJSON.Valid {
		t.Fatalf("payload = %+v, want the kind recorded", record)
	}
	if record.PayloadJSON.String != `{"kind":"belief"}` {
		t.Fatalf("payload = %s, want the kind and only the kind", record.PayloadJSON.String)
	}
	for _, forbidden := range []string{body, "4242", "Card"} {
		if strings.Contains(record.PayloadJSON.String, forbidden) {
			t.Fatalf("the audit payload carried %q", forbidden)
		}
	}
}

// The other kind a model may propose is a profile edit, and it is the more
// sensitive of the two: the profile is what Turing says about the user in
// every conversation. Its row records the kind and, still, nothing else.
func TestMemoryProfileEditProposalRecordsOnlyItsKind(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")
	const body = "They are being treated for a condition they have not told anyone about."

	candidateID := rememberKind(t, service, ctx, runID, "Profile", body, "profile_edit")

	record := onlyAuditRowFor(t, repo, ctx, "memory.tool.proposed")
	if !record.CorrelationID.Valid || record.CorrelationID.String != runID {
		t.Fatalf("correlation = %v, want the run %q that made the proposal", record.CorrelationID, runID)
	}
	if record.ActorType != "runtime" {
		t.Fatalf("actor type = %q, want runtime", record.ActorType)
	}
	if !record.Target.Valid || record.Target.String != candidateID {
		t.Fatalf("target = %v, want the candidate %q", record.Target, candidateID)
	}
	if record.PayloadJSON.String != `{"kind":"profile_edit"}` {
		t.Fatalf("payload = %s, want the kind and only the kind", record.PayloadJSON.String)
	}
	for _, forbidden := range []string{body, "condition", "Profile"} {
		if strings.Contains(record.PayloadJSON.String, forbidden) {
			t.Fatalf("the audit payload carried %q", forbidden)
		}
	}
}

// A memory proposal is a statement about one conversation, so deleting that
// conversation has to take the audit payload with it. Before this row was
// correlated to its run, the scrub could not find it and the trail kept a
// claim about a conversation the user had withdrawn.
func TestDeletingTheConversationScrubsItsMemoryProposalAudit(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, sessionID := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")

	remember(t, service, ctx, runID, "Card", "They drink it black.")
	deleteSession(t, repo, ctx, sessionID)

	record := onlyAuditRowFor(t, repo, ctx, "memory.tool.proposed")
	if !record.PayloadJSON.Valid || record.PayloadJSON.String != `{"scrubbed":true}` {
		t.Fatalf("payload after deletion = %v, want the scrub tombstone", record.PayloadJSON)
	}
}

// The two records a person's own decision leaves are account-level and stay
// that way: they are not scoped to any run, and attributing them to the
// runtime would erase the fact that a human chose them.
func TestMemoryAccountDecisionsStayClientRecordsWithNoRun(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	_, sessionID := newRun(t, repo, ctx)

	if _, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: false}); err != nil {
		t.Fatalf("SetMemoryEnabled: %v", err)
	}
	candidate, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: repository.MemoryCandidateKindProfileEdit,
		Title: "Profile", Body: "Prefers short answers.",
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	if _, err := service.ApplyMemoryProfile(ctx, &turingv1.ApplyMemoryProfileRequest{
		CandidateId: candidate.CandidateID, Content: "Prefers short answers.",
	}); err != nil {
		t.Fatalf("ApplyMemoryProfile: %v", err)
	}

	for action, target := range map[string]string{
		"memory.enabled_changed": ServerName,
		"memory.profile.applied": candidate.CandidateID,
	} {
		record := onlyAuditRowFor(t, repo, ctx, action)
		if record.CorrelationID.Valid && record.CorrelationID.String != "" {
			t.Fatalf("%s correlation = %v, want no run: this is an account decision", action, record.CorrelationID)
		}
		if record.ActorType != "client" {
			t.Fatalf("%s actor type = %q, want client", action, record.ActorType)
		}
		if !record.Target.Valid || record.Target.String != target {
			t.Fatalf("%s target = %v, want %q", action, record.Target, target)
		}
	}
}

// racingAudit deletes the proposal's conversation in the moment between the
// candidate landing and its audit row being written, which is the one ordering
// where a run-scoped insert can find nothing to correlate to.
type racingAudit struct {
	inner *audit.Server
	race  func()
}

func (a *racingAudit) Record(ctx context.Context, correlationID string, actorType string, actorID string, action string, target string, payload map[string]any) error {
	return a.inner.Record(ctx, correlationID, actorType, actorID, action, target, payload)
}

func (a *racingAudit) RecordForExistingRun(ctx context.Context, runID string, actorType string, actorID string, action string, target string, payload map[string]any) (bool, error) {
	a.race()
	return a.inner.RecordForExistingRun(ctx, runID, actorType, actorID, action, target, payload)
}

// If the conversation is gone by the time the row is written, there is nothing
// honest left to report. The candidate went with the session's cascade, so a
// success carrying its id would name a proposal that no longer exists — and
// falling back to an account-level row would leave a permanent, uncorrelated
// statement about a conversation the user just deleted.
func TestMemoryProposalFailsRatherThanLeaveAnUncorrelatedAuditRow(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, sessionID := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")

	deletions := 0
	service.audit = &racingAudit{inner: audit.New(repo), race: func() {
		if deletions > 0 {
			return
		}
		deletions++
		deleteSession(t, repo, ctx, sessionID)
	}}

	response, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRemember,
		Args: callArgs(t, map[string]any{"title": "Card", "body": "They drink it black."}),
	})
	if err == nil {
		t.Fatalf("remember succeeded with no conversation to correlate to: %v", response.GetResult().AsMap())
	}
	if code := status.Code(err); code != codes.FailedPrecondition {
		t.Fatalf("error = %v (%s), want FailedPrecondition", err, code)
	}
	if message := status.Convert(err).Message(); strings.Contains(message, sessionID) || strings.Contains(message, runID) {
		t.Fatalf("refusal %q named the run or session back", message)
	}

	if records := auditRowsFor(t, repo, ctx, "memory.tool.proposed"); len(records) != 0 {
		t.Fatalf("memory.tool.proposed rows = %d, want none: an uncorrelated row is residue", len(records))
	}
}

// failingAudit is a trail that is there but cannot be written to right now —
// the run is fine, the proposal is fine, the insert is not.
type failingAudit struct {
	calls int
	err   error
}

func (a *failingAudit) Record(context.Context, string, string, string, string, string, map[string]any) error {
	return a.err
}

func (a *failingAudit) RecordForExistingRun(context.Context, string, string, string, string, string, map[string]any) (bool, error) {
	a.calls++
	return false, a.err
}

// A trail that cannot be written to is not the same thing as a conversation
// that is gone, and the two must not be answered the same way. Here the run is
// still there and the proposal is already durable in the user's inbox, so
// refusing the call would only invite a retry that files the same claim a
// second time. The row is lost and logged; the vault is not made a mess of.
func TestMemoryProposalSurvivesAnAuditWriteFailureWithoutFilingItselfTwice(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, sessionID := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")

	recorder := &failingAudit{err: errors.New("audit_logs is unwritable")}
	service.audit = recorder

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	first := remember(t, service, ctx, runID, "Card", "They drink it black.")
	if recorder.calls != 1 {
		t.Fatalf("run-scoped audit writes = %d, want exactly one attempt", recorder.calls)
	}
	if !strings.Contains(logs.String(), "memory.tool.proposed") || !strings.Contains(logs.String(), first) {
		t.Fatalf("the lost row was not logged: %q", logs.String())
	}

	// One call, one proposal. The failure is in the trail, not in the vault.
	candidates, err := repo.ListMemoryCandidates(ctx, repository.MemoryCandidateQuery{
		SessionID: sessionID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].CandidateID != first {
		t.Fatalf("candidates = %d, want the single proposal the one call filed", len(candidates))
	}
}

// A memory server with nowhere to record is not a server that records less; it
// is one whose proposals would be untraceable. remember refuses rather than
// filing a claim about the user that nothing can account for.
func TestMemoryProposalRefusesWhenThereIsNoTrailToRecordItIn(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, sessionID := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")
	service.audit = nil

	_, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRemember,
		Args: callArgs(t, map[string]any{"title": "Card", "body": "They drink it black."}),
	})
	if code := status.Code(err); code != codes.Internal {
		t.Fatalf("error = %v (%s), want Internal", err, code)
	}
	if message := status.Convert(err).Message(); strings.Contains(message, sessionID) || strings.Contains(message, runID) {
		t.Fatalf("refusal %q named the run or session back", message)
	}
}
