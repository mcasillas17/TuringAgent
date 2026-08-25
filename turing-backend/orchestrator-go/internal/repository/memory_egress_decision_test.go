package repository

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// memoryEgressRepo is a repository with both a vault and the session/job tables
// the enqueue path needs, because the memory binding is only meaningful where
// both halves exist.
func memoryEgressRepo(t *testing.T) (*Repository, *memoryfiles.Vault, string) {
	t.Helper()
	repo, vault, _ := newMemoryTestRepo(t)
	session, err := repo.CreateSession(context.Background(), "Memory egress")
	if err != nil {
		t.Fatal(err)
	}
	return repo, vault, session.SessionID
}

func memoryRemoteDecision(t *testing.T, repo *Repository, selectedTools []string) *PendingEgressDecision {
	t.Helper()
	snapshot, err := repo.EgressMemorySnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := backendegress.MemorySnapshotFingerprint(snapshot.Preimage(selectedTools))
	if err != nil {
		t.Fatal(err)
	}
	decision := remoteDecision()
	decision.SelectedTools = append([]string(nil), selectedTools...)
	decision.MemorySnapshotFingerprint = fingerprint
	return decision
}

func TestRemoteEnqueueFreezesMemorySnapshotAndFingerprint(t *testing.T) {
	repo, vault, sessionID := memoryEgressRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak plainly.")
	writePin(t, vault, memoryfiles.ProfileFileName, "The user keeps chickens.")
	decision := memoryRemoteDecision(t, repo, []string{"files/files.read"})
	decision.MemoryProfileApplicable = true
	decision.DataCategories = append(decision.DataCategories, "EGRESS_DATA_CATEGORY_ATTACHMENTS")

	enqueued, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: sessionID, Content: "remote", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: decision.Model,
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	stored, err := repo.GetRunEgressDecision(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.MemorySnapshotFingerprint != decision.MemorySnapshotFingerprint {
		t.Fatalf("stored memory fingerprint = %q, want %q",
			stored.MemorySnapshotFingerprint, decision.MemorySnapshotFingerprint)
	}
	job, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker-memory")
	if err != nil {
		t.Fatal(err)
	}
	if job.MemorySnapshotFingerprint != decision.MemorySnapshotFingerprint {
		t.Fatalf("job memory fingerprint = %q, want %q",
			job.MemorySnapshotFingerprint, decision.MemorySnapshotFingerprint)
	}
	if job.PinnedPersona == nil || job.PinnedPersona.Body != "Speak plainly." || job.PinnedPersona.Withheld {
		t.Fatalf("job persona = %+v", job.PinnedPersona)
	}
	if job.PinnedProfile == nil || job.PinnedProfile.Body != "The user keeps chickens." || job.PinnedProfile.Withheld {
		t.Fatalf("job profile = %+v", job.PinnedProfile)
	}
}

// The fingerprint is the run's own binding. It must not appear in the notice
// the transcript carries or in the audit row, and neither must the pinned text.
func TestMemoryEgressNoticeAndAuditCarryNoPinnedContentOrFingerprint(t *testing.T) {
	repo, vault, sessionID := memoryEgressRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak plainly about badgers.")
	decision := memoryRemoteDecision(t, repo, nil)
	decision.MemoryProfileApplicable = true

	enqueued, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: sessionID, Content: "remote", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: decision.Model,
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	var auditPayload string
	if err := repo.db.QueryRowContext(context.Background(), `
		SELECT payload_json FROM audit_logs
		WHERE correlation_id = ? AND action = 'egress.consent.recorded'
	`, enqueued.RunID).Scan(&auditPayload); err != nil {
		t.Fatal(err)
	}
	surfaces := []string{auditPayload}
	for _, event := range enqueued.RoutingEvents {
		surfaces = append(surfaces, event.PayloadJSON)
	}
	for _, surface := range surfaces {
		for _, forbidden := range []string{"badgers", decision.MemorySnapshotFingerprint} {
			if strings.Contains(surface, forbidden) {
				t.Fatalf("memory surface leaked %q: %s", forbidden, surface)
			}
		}
	}
}

// The snapshot is recomputed inside the enqueue transaction against the frozen
// decision. An Obsidian autosave between consent and send has to come back as a
// legible "prepare the send again", not as a run that quietly sends bytes the
// user never saw disclosed.
func TestEnqueueRefusesWhenPersonaChangedAfterConsent(t *testing.T) {
	repo, vault, sessionID := memoryEgressRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak plainly.")
	decision := memoryRemoteDecision(t, repo, nil)
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak grandly.")

	_, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: sessionID, Content: "remote", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: decision.Model,
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
	})
	if !errors.Is(err, ErrEgressMemorySnapshotChanged) {
		t.Fatalf("enqueue error = %v, want ErrEgressMemorySnapshotChanged", err)
	}
	if !errors.Is(err, ErrEgressDecisionInvalid) {
		t.Fatalf("enqueue error = %v, want wrapped ErrEgressDecisionInvalid", err)
	}
}

func TestEnqueueRefusesWhenProfileChangedAfterConsent(t *testing.T) {
	repo, vault, sessionID := memoryEgressRepo(t)
	writePin(t, vault, memoryfiles.ProfileFileName, "The user keeps chickens.")
	decision := memoryRemoteDecision(t, repo, nil)
	writePin(t, vault, memoryfiles.ProfileFileName, "The user keeps bees.")

	_, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: sessionID, Content: "remote", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: decision.Model,
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
	})
	if !errors.Is(err, ErrEgressMemorySnapshotChanged) {
		t.Fatalf("enqueue error = %v, want ErrEgressMemorySnapshotChanged", err)
	}
}

// Flipping the toggle between consent and send changes what would be sent, so
// it is the same refusal as an edit.
func TestEnqueueRefusesWhenMemoryToggleFlippedAfterConsent(t *testing.T) {
	repo, vault, sessionID := memoryEgressRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak plainly.")
	decision := memoryRemoteDecision(t, repo, nil)
	if _, err := repo.SetMemoryEnabled(context.Background(), false); err != nil {
		t.Fatal(err)
	}

	_, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: sessionID, Content: "remote", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: decision.Model,
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
	})
	if !errors.Is(err, ErrEgressMemorySnapshotChanged) {
		t.Fatalf("enqueue error = %v, want ErrEgressMemorySnapshotChanged", err)
	}
}

// A decision with no memory binding at all is not a decision this repository
// will freeze: an unbound run could be replayed against any vault.
func TestPendingDecisionWithoutMemoryFingerprintIsInvalid(t *testing.T) {
	decision := remoteDecision()
	decision.MemorySnapshotFingerprint = ""

	if _, err := normalizePendingEgressDecision(decision); !errors.Is(err, ErrEgressDecisionInvalid) {
		t.Fatalf("normalize error = %v, want ErrEgressDecisionInvalid", err)
	}
}

// The idempotency key is not a licence to replay a send against different
// pinned material.
func TestEnqueueFingerprintCoversTheMemorySnapshot(t *testing.T) {
	repo, vault, sessionID := memoryEgressRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak plainly.")
	decision := memoryRemoteDecision(t, repo, nil)
	input := EnqueueUserMessageInput{
		SessionID: sessionID, Content: "remote", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: decision.Model,
		IdempotencyKey: "send_memory_1",
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
	}
	before, err := EnqueueRequestFingerprint(input)
	if err != nil {
		t.Fatal(err)
	}
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak grandly.")
	changed := memoryRemoteDecision(t, repo, nil)
	changed.ChallengeNonce = decision.ChallengeNonce
	changed.ChallengeFingerprint = decision.ChallengeFingerprint
	shifted := input
	shifted.EgressDecision = changed
	after, err := EnqueueRequestFingerprint(shifted)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("the enqueue fingerprint ignores the memory snapshot")
	}
}

// The idempotency key is the client's handle on one send. Reusing it after the
// persona changed is a different request, and it must be refused rather than
// answered with the earlier run.
func TestReplayWithTheSameKeyIsRefusedAfterThePersonaChanged(t *testing.T) {
	repo, vault, sessionID := memoryEgressRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak plainly.")
	decision := memoryRemoteDecision(t, repo, nil)
	input := EnqueueUserMessageInput{
		SessionID: sessionID, Content: "remote", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: decision.Model,
		IdempotencyKey: "send_memory_replay",
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
	}
	if _, err := repo.EnqueueUserMessage(context.Background(), input); err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak grandly.")
	changed := memoryRemoteDecision(t, repo, nil)
	changed.ChallengeNonce = "nonce_memory_replay"
	replay := input
	replay.EgressDecision = changed
	fingerprint, err := EnqueueRequestFingerprint(replay)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.LookupSendMessageReplay(
		context.Background(), replay.IdempotencyKey, fingerprint,
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("replay lookup error = %v, want ErrIdempotencyConflict", err)
	}
}

// A local run has no consent and no fingerprint to check, but it still carries
// the pinned snapshot the runtime will inject, frozen at enqueue.
func TestLocalEnqueueFreezesPinnedSnapshotWithoutADecision(t *testing.T) {
	repo, vault, sessionID := memoryEgressRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak plainly.")

	if _, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: sessionID, Content: "local", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	job, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker-local")
	if err != nil {
		t.Fatal(err)
	}
	if job.PinnedPersona == nil || job.PinnedPersona.Body != "Speak plainly." {
		t.Fatalf("job persona = %+v", job.PinnedPersona)
	}
	if job.PinnedProfile == nil || !job.PinnedProfile.Withheld && job.PinnedProfile.Body != "" {
		t.Fatalf("job profile = %+v", job.PinnedProfile)
	}
}

// A queued job keeps the snapshot it was given. Turning memory off while the
// job waits does not rewrite what the user already consented to send.
func TestQueuedJobKeepsItsSnapshotAcrossAToggleFlip(t *testing.T) {
	repo, vault, sessionID := memoryEgressRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak plainly.")

	if _, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: sessionID, Content: "local", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	if _, err := repo.SetMemoryEnabled(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak grandly.")

	job, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker-frozen")
	if err != nil {
		t.Fatal(err)
	}
	if job.PinnedPersona == nil || job.PinnedPersona.Body != "Speak plainly." {
		t.Fatalf("job persona = %+v, want the snapshot taken at enqueue", job.PinnedPersona)
	}
}

// An edit lands on the next run, not the one already accepted. Each job carries
// the persona that was on disk when its message was taken.
func TestAVaultEditReachesTheNextRunAndNotTheQueuedOne(t *testing.T) {
	repo, vault, firstSession := memoryEgressRepo(t)
	secondSession := newMemoryTestSession(t, repo)
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak plainly.")
	enqueueLocal := func(sessionID string, content string) {
		t.Helper()
		if _, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
			SessionID: sessionID, Content: content, ContentType: "text",
			AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
		}); err != nil {
			t.Fatalf("EnqueueUserMessage(%s): %v", content, err)
		}
	}
	enqueueLocal(firstSession, "first")
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak grandly.")
	enqueueLocal(secondSession, "second")

	first, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker-b")
	if err != nil {
		t.Fatal(err)
	}
	if first.PinnedPersona.Body != "Speak plainly." {
		t.Fatalf("first job persona = %+v, want the pre-edit words", first.PinnedPersona)
	}
	if second.PinnedPersona.Body != "Speak grandly." {
		t.Fatalf("second job persona = %+v, want the post-edit words", second.PinnedPersona)
	}
	if first.MemorySnapshotFingerprint == second.MemorySnapshotFingerprint {
		t.Fatal("two different personas produced the same job fingerprint")
	}
}

// A queued job's profile is fixed the same way its persona is: turning memory
// off while the job waits does not rewrite either half of what the user
// already consented to send.
func TestQueuedJobKeepsItsProfileSnapshotAcrossAToggleFlip(t *testing.T) {
	repo, vault, sessionID := memoryEgressRepo(t)
	writePin(t, vault, memoryfiles.ProfileFileName, "The user keeps chickens.")

	if _, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: sessionID, Content: "local", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	if _, err := repo.SetMemoryEnabled(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	writePin(t, vault, memoryfiles.ProfileFileName, "The user keeps bees.")

	job, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker-frozen-profile")
	if err != nil {
		t.Fatal(err)
	}
	if job.PinnedProfile == nil || job.PinnedProfile.Body != "The user keeps chickens." {
		t.Fatalf("job profile = %+v, want the snapshot taken at enqueue", job.PinnedProfile)
	}
}

// An edit to the profile lands on the next run, not the one already accepted —
// the same guarantee TestAVaultEditReachesTheNextRunAndNotTheQueuedOne makes
// for the persona, exercised here for the other tier. Each job carries the
// profile that was on disk when its message was taken.
func TestAProfileEditReachesTheNextRunAndNotTheQueuedOne(t *testing.T) {
	repo, vault, firstSession := memoryEgressRepo(t)
	secondSession := newMemoryTestSession(t, repo)
	writePin(t, vault, memoryfiles.ProfileFileName, "The user keeps chickens.")
	enqueueLocal := func(sessionID string, content string) {
		t.Helper()
		if _, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
			SessionID: sessionID, Content: content, ContentType: "text",
			AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
		}); err != nil {
			t.Fatalf("EnqueueUserMessage(%s): %v", content, err)
		}
	}
	enqueueLocal(firstSession, "first")
	writePin(t, vault, memoryfiles.ProfileFileName, "The user keeps bees.")
	enqueueLocal(secondSession, "second")

	first, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker-profile-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker-profile-b")
	if err != nil {
		t.Fatal(err)
	}
	if first.PinnedProfile.Body != "The user keeps chickens." {
		t.Fatalf("first job profile = %+v, want the pre-edit words", first.PinnedProfile)
	}
	if second.PinnedProfile.Body != "The user keeps bees." {
		t.Fatalf("second job profile = %+v, want the post-edit words", second.PinnedProfile)
	}
	if first.MemorySnapshotFingerprint == second.MemorySnapshotFingerprint {
		t.Fatal("two different profiles produced the same job fingerprint")
	}
}

// The enqueue transaction opens the two pinned documents and nothing else. A
// scan or an index refresh in here would hold a write lock for as long as the
// user's vault is large, on the path a person is waiting on — and it would do
// it while they are typing into that same vault.
func TestEnqueueDoesNotWalkTheVault(t *testing.T) {
	repo, vault, sessionID := memoryEgressRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak plainly.")
	for index := range 5 {
		writePin(t, vault, filepath.Join(memoryfiles.BeliefsDirName,
			fmt.Sprintf("note-%d.md", index)), "---\nid: belief_x\n---\nA note.\n")
	}

	if _, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: sessionID, Content: "local", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	var indexed int
	if err := repo.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM memory_notes`).Scan(&indexed); err != nil {
		t.Fatal(err)
	}
	if indexed != 0 {
		t.Fatalf("enqueue indexed %d vault notes; it must read only the two pinned files", indexed)
	}
	job, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker-walk")
	if err != nil {
		t.Fatal(err)
	}
	if job.PinnedPersona == nil || job.PinnedPersona.Body != "Speak plainly." {
		t.Fatalf("job persona = %+v", job.PinnedPersona)
	}
}

// The pin budget bounds what reaches a prompt, not what the user may write. A
// persona far past it truncates, carries a notice, and still enqueues.
func TestAnOversizedPersonaTruncatesRatherThanBlockingTheEnqueue(t *testing.T) {
	repo, vault, sessionID := memoryEgressRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, strings.Repeat("a", memoryfiles.MaxPersonaBytes*8))
	decision := memoryRemoteDecision(t, repo, nil)
	decision.MemoryProfileApplicable = true

	if _, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: sessionID, Content: "remote", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: decision.Model,
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
	}); err != nil {
		t.Fatalf("an over-budget persona blocked the enqueue: %v", err)
	}
	job, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker-truncated")
	if err != nil {
		t.Fatal(err)
	}
	if job.PinnedPersona == nil || job.PinnedPersona.Withheld {
		t.Fatalf("an over-budget persona was withheld: %+v", job.PinnedPersona)
	}
	if !strings.Contains(job.PinnedPersona.Body, "are pinned") {
		t.Fatal("the truncated pin carries no notice saying so")
	}
	if len(job.PinnedPersona.Body) > memoryfiles.MaxPersonaBytes+512 {
		t.Fatalf("pinned body is %d bytes, want the budget plus a notice", len(job.PinnedPersona.Body))
	}
}
