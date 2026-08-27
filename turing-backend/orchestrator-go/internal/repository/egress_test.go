package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

func remoteDecision() *PendingEgressDecision {
	skillFingerprint, _ := backendegress.SkillSnapshotFingerprint(nil)
	return &PendingEgressDecision{
		Version:              RunEgressDecisionVersion,
		ChallengeNonce:       "nonce_remote_1",
		ChallengeFingerprint: "fingerprint_remote_1",
		RequestDigest:        "request_digest_remote_1",
		Provider:             "openai_compatible",
		Model:                "gpt-5-mini",
		Endpoint:             "https://api.example.com/v1",
		EndpointHost:         "api.example.com",
		DataCategories: []string{
			"EGRESS_DATA_CATEGORY_CURRENT_MESSAGE",
			"EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY",
			"EGRESS_DATA_CATEGORY_TOOL_SCHEMAS",
		},
		SelectedTools:            []string{"files/files.read", "system/system.time"},
		SkillSnapshotFingerprint: skillFingerprint,
		RecallApplicable:         true,
		MemoryProfileApplicable:  false,
		ConsentGrantedAt:         "2026-08-20T01:02:03.000000000Z",
	}
}

func TestEgressCategoryPolicyCoversProtoEnum(t *testing.T) {
	expected := 0
	for name, value := range turingv1.EgressDataCategory_value {
		if value == int32(turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_UNSPECIFIED) {
			continue
		}
		expected++
		if _, ok := validEgressDataCategories[name]; !ok {
			t.Fatalf("repository category policy missing %s", name)
		}
		if label := egressCategoryLabel(name); label == "Unknown data category" {
			t.Fatalf("notice label missing %s", name)
		}
	}
	if len(validEgressDataCategories) != expected {
		t.Fatalf("repository category policy has %d entries, want %d", len(validEgressDataCategories), expected)
	}
}

func testRemoteEgressDecision(t *testing.T, model, endpoint, externalAgentID, credentialRef string) *PendingEgressDecision {
	t.Helper()
	decision := remoteDecision()
	decision.ChallengeNonce = ids.New("nonce")
	decision.ChallengeFingerprint = ids.New("fingerprint")
	decision.Model = model
	decision.Endpoint = endpoint
	decision.EndpointHost = ExternalAgentEndpointHost(endpoint)
	decision.ExternalAgentID = externalAgentID
	if externalAgentID != "" {
		decision.ExternalCredentialRefHash = backendegress.HashCredentialReference(credentialRef)
	}
	decision.RecallApplicable = externalAgentID == ""
	return decision
}

func TestRemoteEnqueuePersistsAndFreezesEgressDecision(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Remote")
	if err != nil {
		t.Fatal(err)
	}
	input := EnqueueUserMessageInput{
		SessionID:      session.SessionID,
		Content:        "remote request",
		ContentType:    "text",
		AgentID:        "general_assistant",
		ModelProvider:  "openai_compatible",
		Model:          "gpt-5-mini",
		IdempotencyKey: "send_remote_1",
		EgressDecision: remoteDecision(),
		SelectedTools:  []string{"system/system.time", "files/files.read"},
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}

	decision, err := repo.GetRunEgressDecision(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if decision.RunID != enqueued.RunID || decision.DecisionID == "" ||
		decision.ChallengeNonce != "nonce_remote_1" ||
		decision.ChallengeFingerprint != "fingerprint_remote_1" ||
		decision.Provider != "openai_compatible" ||
		decision.Endpoint != "https://api.example.com/v1" ||
		decision.EndpointHost != "api.example.com" ||
		!decision.RecallApplicable || decision.MemoryProfileApplicable {
		t.Fatalf("decision = %+v", decision)
	}
	if !reflect.DeepEqual(decision.DataCategories, []string{
		"EGRESS_DATA_CATEGORY_CURRENT_MESSAGE",
		"EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY",
		"EGRESS_DATA_CATEGORY_TOOL_SCHEMAS",
	}) {
		t.Fatalf("categories = %v", decision.DataCategories)
	}
	if !reflect.DeepEqual(decision.SelectedTools, []string{"files/files.read", "system/system.time"}) {
		t.Fatalf("selected tools = %v", decision.SelectedTools)
	}

	job, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-egress")
	if err != nil {
		t.Fatal(err)
	}
	if job.EgressDecision == nil || job.EgressDecision.DecisionID != decision.DecisionID {
		t.Fatalf("job decision = %+v", job.EgressDecision)
	}
	if !reflect.DeepEqual(job.SelectedTools, decision.SelectedTools) {
		t.Fatalf("job selected tools = %v, want %v", job.SelectedTools, decision.SelectedTools)
	}
	if len(enqueued.RoutingEvents) != 1 {
		t.Fatalf("routing events = %+v, want one disclosure notice", enqueued.RoutingEvents)
	}
	var notice map[string]any
	if err := json.Unmarshal([]byte(enqueued.RoutingEvents[0].PayloadJSON), &notice); err != nil {
		t.Fatal(err)
	}
	if notice["endpoint"] != "api.example.com" || notice["provider"] != "openai_compatible" {
		t.Fatalf("notice = %+v", notice)
	}
	var auditPayload string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT payload_json FROM audit_logs
		WHERE correlation_id = ? AND action = 'egress.consent.recorded'
	`, enqueued.RunID).Scan(&auditPayload); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"nonce_remote_1", "fingerprint_remote_1", "remote request", "challenge"} {
		if strings.Contains(auditPayload, forbidden) {
			t.Fatalf("audit payload leaked %q: %s", forbidden, auditPayload)
		}
	}
	var audit map[string]any
	if err := json.Unmarshal([]byte(auditPayload), &audit); err != nil {
		t.Fatal(err)
	}
	if audit["provider"] != "openai_compatible" || audit["endpointHost"] != "api.example.com" ||
		audit["decisionVersion"] != float64(RunEgressDecisionVersion) ||
		audit["consentGrantedAt"] != "2026-08-20T01:02:03.000000000Z" {
		t.Fatalf("audit payload = %+v", audit)
	}
}

func TestRemoteEnqueueRequiresDecisionBeforePersistence(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "No consent")
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "must not persist", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: "gpt-5-mini",
	})
	if !errors.Is(err, ErrRemoteEgressConsentRequired) {
		t.Fatalf("enqueue error = %v, want ErrRemoteEgressConsentRequired", err)
	}
	assertSessionRunCounts(t, repo, ctx, session.SessionID, 0, 0)
}

func TestLocalEnqueueRejectsRemoteDecision(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Local")
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "stay local", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
		EgressDecision: remoteDecision(),
	})
	if !errors.Is(err, ErrLocalEgressDecisionForbidden) {
		t.Fatalf("enqueue error = %v, want ErrLocalEgressDecisionForbidden", err)
	}
	assertSessionRunCounts(t, repo, ctx, session.SessionID, 0, 0)
}

func TestEgressChallengeNonceCreatesAtMostOneRun(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Nonce")
	if err != nil {
		t.Fatal(err)
	}
	first := remoteDecision()
	first.ChallengeFingerprint = "first"
	_, err = repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "first", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: "gpt-5-mini",
		IdempotencyKey: "nonce_first", EgressDecision: first,
	})
	if err != nil {
		t.Fatal(err)
	}
	second := remoteDecision()
	second.ChallengeFingerprint = "second"
	_, err = repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "second", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: "gpt-5-mini",
		IdempotencyKey: "nonce_second", EgressDecision: second,
	})
	if !errors.Is(err, ErrEgressChallengeAlreadyUsed) {
		t.Fatalf("second enqueue error = %v, want ErrEgressChallengeAlreadyUsed", err)
	}
	assertSessionRunCounts(t, repo, ctx, session.SessionID, 2, 1)
}

func TestSessionCascadeDeletesRunEgressDecision(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Delete")
	if err != nil {
		t.Fatal(err)
	}

	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "remote", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: "gpt-5-mini",
		IdempotencyKey: "delete_remote", EgressDecision: remoteDecision(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, session.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetRunEgressDecision(ctx, enqueued.RunID); err == nil {
		t.Fatal("egress decision survived session delete")
	}
}

func TestSessionDeletionScrubsEgressAuditMetadata(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Delete audit")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "remote audit", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: "gpt-5-mini",
		EgressDecision: remoteDecision(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE agent_runs SET status = 'completed', finished_at = ? WHERE id = ?`,
		FormatTimestamp(time.Now().UTC()), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE jobs SET status = 'completed', finished_at = ? WHERE run_id = ?`,
		FormatTimestamp(time.Now().UTC()), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteSession(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := repo.db.QueryRowContext(ctx,
		`SELECT payload_json FROM audit_logs WHERE action = 'egress.consent.recorded' AND correlation_id = ?`,
		enqueued.RunID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != scrubbedAuditPayload {
		t.Fatalf("egress audit payload after delete = %s", payload)
	}
}

func TestLegacyExternalAgentURLIsCanonicalizedWhenFrozenIntoJob(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Legacy route")
	if err != nil {
		t.Fatal(err)
	}

	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	const legacyURL = "https://API.Anthropic.com:443/v1/"
	if _, err := repo.db.ExecContext(ctx, `UPDATE external_agents SET base_url = ? WHERE id = ?`, legacyURL, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	decision := testRemoteEgressDecision(t, agent.Model, "https://api.anthropic.com/v1", agent.AgentID, agent.CredentialRef)
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "legacy", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "ignored",
		EgressDecision: decision,
	})
	if err != nil {
		t.Fatal(err)
	}
	target := payloadExternalAgent(t, ctx, repo, enqueued.JobID)
	if target == nil || target.BaseURL != "https://api.anthropic.com/v1" {
		t.Fatalf("frozen target = %+v", target)
	}
}

func TestEgressSkillFingerprintMatchesEnqueueAfterSkillEditWithoutPrepareWrites(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", []string{"files.update"}, "Be brief.")
	grantRepositoryCapability(t, repo, "writing/tone", "files.update")
	enableRepositorySkill(t, repo, "writing/tone")
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", []string{"files.update"}, "Changed body.")

	var grantsBefore int
	if err := repo.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM skill_capability_grants WHERE skill_id = 'writing/tone'`).Scan(&grantsBefore); err != nil {
		t.Fatal(err)
	}
	fingerprint, _, err := repo.EgressSkillSnapshotFingerprint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var grantsAfter int
	if err := repo.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM skill_capability_grants WHERE skill_id = 'writing/tone'`).Scan(&grantsAfter); err != nil {
		t.Fatal(err)
	}
	if grantsAfter != grantsBefore {
		t.Fatalf("prepare changed grant count from %d to %d", grantsBefore, grantsAfter)
	}

	session, err := repo.CreateSession(context.Background(), "Skill drift")
	if err != nil {
		t.Fatal(err)
	}
	decision := remoteDecision()
	decision.SkillSnapshotFingerprint = fingerprint
	if _, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "remote after edit", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: decision.Model,
		EgressDecision: decision,
	}); err != nil {
		t.Fatalf("enqueue after pure prepare fingerprint: %v", err)
	}
}

func TestEgressSkillSnapshotFingerprintReturnsBoundDisclosureInfoFromOneRead(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", nil, "Be brief.")
	enableRepositorySkill(t, repo, "writing/tone")

	firstFingerprint, firstInfo, err := repo.EgressSkillSnapshotFingerprint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstInfo, []SkillEgressInfo{{
		SkillID: "writing/tone", DisplayName: "Tone", BodyMayBeSent: true,
	}}) {
		t.Fatalf("first disclosure info = %+v", firstInfo)
	}

	writeRepositorySkill(t, root, "writing/tone", "Clear Tone", "Brief prose", nil, "Be clearer.")
	secondFingerprint, secondInfo, err := repo.EgressSkillSnapshotFingerprint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if secondFingerprint == firstFingerprint {
		t.Fatal("edited skill did not change fingerprint")
	}
	if !reflect.DeepEqual(secondInfo, []SkillEgressInfo{{
		SkillID: "writing/tone", DisplayName: "Clear Tone", BodyMayBeSent: true,
	}}) {
		t.Fatalf("second disclosure info = %+v", secondInfo)
	}
}

func TestEgressSkillDisclosureBodyMayBeSentUsesSnapshotWithheldBit(t *testing.T) {
	t.Run("all declared capabilities granted", func(t *testing.T) {
		repo, root := newSkillRepository(t)
		writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief", []string{"files.update"}, "Body.")
		grantRepositoryCapability(t, repo, "writing/tone", "files.update")
		enableRepositorySkill(t, repo, "writing/tone")
		_, info, err := repo.EgressSkillSnapshotFingerprint(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(info) != 1 || !info[0].BodyMayBeSent {
			t.Fatalf("disclosure info = %+v, want body may be sent", info)
		}
	})

	t.Run("revoked grant withholds body", func(t *testing.T) {
		repo, root := newSkillRepository(t)
		writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief", []string{"files.update"}, "Body.")
		grantRepositoryCapability(t, repo, "writing/tone", "files.update")
		enableRepositorySkill(t, repo, "writing/tone")
		if _, err := repo.SetSkillGrant(context.Background(), "writing/tone", "files.update", false); err != nil {
			t.Fatal(err)
		}
		_, info, err := repo.EgressSkillSnapshotFingerprint(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(info) != 1 || info[0].BodyMayBeSent {
			t.Fatalf("disclosure info = %+v, want metadata only", info)
		}
	})

	t.Run("zero capabilities permits body", func(t *testing.T) {
		repo, root := newSkillRepository(t)
		writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief", nil, "Body.")
		enableRepositorySkill(t, repo, "writing/tone")
		_, info, err := repo.EgressSkillSnapshotFingerprint(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(info) != 1 || !info[0].BodyMayBeSent {
			t.Fatalf("disclosure info = %+v, want body may be sent", info)
		}
	})

	t.Run("stale grant scope after body edit with unchanged requires withholds body", func(t *testing.T) {
		repo, root := newSkillRepository(t)
		writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief", []string{"files.update"}, "Original body.")
		grantRepositoryCapability(t, repo, "writing/tone", "files.update")
		enableRepositorySkill(t, repo, "writing/tone")
		writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief", []string{"files.update"}, "Edited body.")

		var storedGrantCount int
		if err := repo.db.QueryRowContext(context.Background(), `
			SELECT COUNT(*) FROM skill_capability_grants
			WHERE skill_id = 'writing/tone' AND capability = 'files.update'
		`).Scan(&storedGrantCount); err != nil {
			t.Fatal(err)
		}
		if storedGrantCount != 1 {
			t.Fatalf("stored grant count = %d, want 1 to preserve the divergence", storedGrantCount)
		}
		_, info, err := repo.EgressSkillSnapshotFingerprint(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(info) != 1 || info[0].BodyMayBeSent {
			t.Fatalf("disclosure info = %+v, want stale snapshot metadata only", info)
		}
	})
}

func TestEgressSkillFingerprintTreatsNewUnreconciledSkillAsDisabled(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/new", "New", "Not reconciled", nil, "Body.")
	fingerprint, _, err := repo.EgressSkillSnapshotFingerprint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	emptyFingerprint, err := backendegress.SkillSnapshotFingerprint(nil)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint != emptyFingerprint {
		t.Fatalf("new disabled skill fingerprint = %q, want empty %q", fingerprint, emptyFingerprint)
	}
	var settings int
	if err := repo.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM skill_settings WHERE skill_id = 'writing/new'`).Scan(&settings); err != nil {
		t.Fatal(err)
	}
	if settings != 0 {
		t.Fatal("prepare reconciled a new skill")
	}
}

func TestEnqueueReturnsSpecificWrappedSentinelWhenSkillSnapshotChanged(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief", nil, "Original body.")
	enableRepositorySkill(t, repo, "writing/tone")
	fingerprint, _, err := repo.EgressSkillSnapshotFingerprint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief", nil, "Edited body.")
	session, err := repo.CreateSession(context.Background(), "Skill enqueue race")
	if err != nil {
		t.Fatal(err)
	}
	decision := remoteDecision()
	decision.SkillSnapshotFingerprint = fingerprint
	_, err = repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "remote", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: decision.Model,
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
	})
	if !errors.Is(err, ErrEgressSkillSnapshotChanged) {
		t.Fatalf("enqueue error = %v, want ErrEgressSkillSnapshotChanged", err)
	}
	if !errors.Is(err, ErrEgressDecisionInvalid) {
		t.Fatalf("enqueue error = %v, want wrapped ErrEgressDecisionInvalid", err)
	}
}

func withSkillCategory(decision *PendingEgressDecision) {
	decision.DataCategories = append(
		append([]string(nil), decision.DataCategories[:2]...),
		append([]string{"EGRESS_DATA_CATEGORY_SKILL_CONTENT"}, decision.DataCategories[2:]...)...,
	)
}

func TestRemoteEgressNoticeNamesOnlyCategoryDisclosedSkills(t *testing.T) {
	t.Run("remote skill category names skills", func(t *testing.T) {
		repo, root := newSkillRepository(t)
		writeRepositorySkill(t, root, "writing/tone", "Tone Guide", "Brief", nil, "Body.")
		enableRepositorySkill(t, repo, "writing/tone")
		fingerprint, _, err := repo.EgressSkillSnapshotFingerprint(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		decision := remoteDecision()
		decision.SkillSnapshotFingerprint = fingerprint
		withSkillCategory(decision)
		note := enqueueEgressNoticeNote(t, repo, decision, "openai_compatible")
		if !strings.Contains(note, "Skills that may be sent: Tone Guide") {
			t.Fatalf("notice = %q, want skill name", note)
		}
	})

	t.Run("remote without skill category has no skill line", func(t *testing.T) {
		repo, _ := newSkillRepository(t)
		note := enqueueEgressNoticeNote(t, repo, remoteDecision(), "openai_compatible")
		if strings.Contains(note, "Skills that may be sent:") {
			t.Fatalf("notice = %q, want no skill line", note)
		}
	})

	t.Run("local remote MCP with enabled skill has no skill line", func(t *testing.T) {
		repo, root := newSkillRepository(t)
		writeRepositorySkill(t, root, "writing/tone", "Tone Guide", "Brief", nil, "Body.")
		enableRepositorySkill(t, repo, "writing/tone")
		fingerprint, _, err := repo.EgressSkillSnapshotFingerprint(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		decision := remoteDecision()
		decision.Provider = "ollama"
		decision.Model = "local"
		decision.Endpoint = ""
		decision.EndpointHost = ""
		decision.RecallApplicable = false
		decision.DataCategories = []string{
			"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS",
			"EGRESS_DATA_CATEGORY_TOOL_RESULTS",
		}
		decision.SkillSnapshotFingerprint = fingerprint
		decision.RemoteMCPServers = []RemoteMCPServerEgress{{
			ServerName: "vendor", Endpoint: "https://vendor.example/mcp", EndpointHost: "vendor.example",
		}}
		note := enqueueEgressNoticeNote(t, repo, decision, "ollama")
		if strings.Contains(note, "Skills that may be sent:") || strings.Contains(note, "Tone Guide") {
			t.Fatalf("notice = %q, want no skill line", note)
		}
	})

	t.Run("remote skill category over empty snapshot has no dangling skill line", func(t *testing.T) {
		repo, _ := newSkillRepository(t)
		fingerprint, _, err := repo.EgressSkillSnapshotFingerprint(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		decision := remoteDecision()
		decision.SkillSnapshotFingerprint = fingerprint
		withSkillCategory(decision)
		note := enqueueEgressNoticeNote(t, repo, decision, "openai_compatible")
		if strings.Contains(note, "Skills that may be sent:") {
			t.Fatalf("notice = %q, want no dangling skill line", note)
		}
		if !strings.Contains(note, "Enabled skill content") ||
			!strings.Contains(note, "Data leaves your machine") {
			t.Fatalf("notice = %q, want category line and egress warning intact", note)
		}
	})
}

func TestRemoteEgressNoticeTruncatesSkillNamesAtEight(t *testing.T) {
	repo, root := newSkillRepository(t)
	for index := 1; index <= 12; index++ {
		id := fmt.Sprintf("skills/s%02d", index)
		name := fmt.Sprintf("Skill %02d", index)
		writeRepositorySkill(t, root, id, name, "Brief", nil, "Body.")
		enableRepositorySkill(t, repo, id)
	}
	fingerprint, _, err := repo.EgressSkillSnapshotFingerprint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	decision := remoteDecision()
	decision.SkillSnapshotFingerprint = fingerprint
	decision.DataCategories = append(
		append([]string(nil), decision.DataCategories[:2]...),
		append([]string{"EGRESS_DATA_CATEGORY_SKILL_CONTENT"}, decision.DataCategories[2:]...)...,
	)
	note := enqueueEgressNoticeNote(t, repo, decision, "openai_compatible")
	for index := 1; index <= 8; index++ {
		if !strings.Contains(note, fmt.Sprintf("Skill %02d", index)) {
			t.Fatalf("notice = %q, missing Skill %02d", note, index)
		}
	}
	if strings.Contains(note, "Skill 09") || !strings.Contains(note, "+4 more") {
		t.Fatalf("notice = %q, want eight names and +4 more", note)
	}
}

func enqueueEgressNoticeNote(t *testing.T, repo *Repository, decision *PendingEgressDecision, provider string) string {
	t.Helper()
	session, err := repo.CreateSession(context.Background(), "Egress notice")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "notice", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: provider, Model: decision.Model,
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(enqueued.RoutingEvents) != 1 {
		t.Fatalf("routing events = %+v, want one", enqueued.RoutingEvents)
	}
	var payload struct {
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(enqueued.RoutingEvents[0].PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Note
}

func assertSessionRunCounts(t *testing.T, repo *Repository, ctx context.Context, sessionID string, messages, runs int) {
	t.Helper()
	var gotMessages, gotRuns int
	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID).Scan(&gotMessages); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runs WHERE session_id = ?`, sessionID).Scan(&gotRuns); err != nil {
		t.Fatal(err)
	}
	if gotMessages != messages || gotRuns != runs {
		t.Fatalf("counts messages/runs = %d/%d, want %d/%d", gotMessages, gotRuns, messages, runs)
	}
}
