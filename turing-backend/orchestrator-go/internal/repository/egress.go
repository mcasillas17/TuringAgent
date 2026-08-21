package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

const RunEgressDecisionVersion = backendegress.DecisionVersion

var (
	ErrRemoteEgressConsentRequired  = errors.New("remote run requires an egress decision")
	ErrLocalEgressDecisionForbidden = errors.New("local run must not carry an egress decision")
	ErrEgressChallengeAlreadyUsed   = errors.New("egress challenge was already used")
	ErrEgressDecisionInvalid        = errors.New("egress decision is invalid")
)

var validEgressDataCategories = map[string]int{
	"EGRESS_DATA_CATEGORY_CURRENT_MESSAGE":      1,
	"EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY": 2,
	"EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL": 3,
	"EGRESS_DATA_CATEGORY_MEMORY_PROFILE":       4,
	"EGRESS_DATA_CATEGORY_SKILL_CONTENT":        5,
	"EGRESS_DATA_CATEGORY_TOOL_SCHEMAS":         6,
	"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS":       7,
	"EGRESS_DATA_CATEGORY_TOOL_RESULTS":         8,
	"EGRESS_DATA_CATEGORY_ATTACHMENTS":          9,
}

type PendingEgressDecision struct {
	Version                   int
	ChallengeNonce            string
	ChallengeFingerprint      string
	RequestDigest             string
	Provider                  string
	Model                     string
	ExternalAgentID           string
	ExternalCredentialRefHash string
	Endpoint                  string
	EndpointHost              string
	DataCategories            []string
	SelectedTools             []string
	SkillSnapshotFingerprint  string
	RecallApplicable          bool
	MemoryProfileApplicable   bool
	ConsentGrantedAt          string
}

type RunEgressDecision struct {
	DecisionID                string   `json:"decisionId"`
	RunID                     string   `json:"runId"`
	Version                   int      `json:"version"`
	ChallengeNonce            string   `json:"challengeNonce"`
	ChallengeFingerprint      string   `json:"challengeFingerprint"`
	RequestDigest             string   `json:"requestDigest"`
	Provider                  string   `json:"provider"`
	Model                     string   `json:"model"`
	ExternalAgentID           string   `json:"externalAgentId,omitempty"`
	ExternalCredentialRefHash string   `json:"externalCredentialRefHash,omitempty"`
	Endpoint                  string   `json:"endpoint"`
	EndpointHost              string   `json:"endpointHost"`
	DataCategories            []string `json:"dataCategories"`
	SelectedTools             []string `json:"selectedTools"`
	SkillSnapshotFingerprint  string   `json:"skillSnapshotFingerprint"`
	RecallApplicable          bool     `json:"recallApplicable"`
	MemoryProfileApplicable   bool     `json:"memoryProfileApplicable"`
	ConsentGrantedAt          string   `json:"consentGrantedAt"`
}

func normalizePendingEgressDecision(input *PendingEgressDecision) (*PendingEgressDecision, error) {
	if input == nil {
		return nil, nil
	}
	normalized := *input
	normalized.DataCategories = append([]string(nil), input.DataCategories...)
	normalized.SelectedTools = append([]string(nil), input.SelectedTools...)
	slices.Sort(normalized.SelectedTools)
	if normalized.Version != RunEgressDecisionVersion ||
		normalized.ChallengeNonce == "" ||
		normalized.ChallengeFingerprint == "" ||
		normalized.RequestDigest == "" ||
		normalized.Provider != "openai_compatible" ||
		normalized.Model == "" ||
		normalized.Endpoint == "" ||
		normalized.EndpointHost == "" ||
		(normalized.ExternalAgentID != "" && normalized.ExternalCredentialRefHash == "") ||
		(normalized.ExternalAgentID == "" && normalized.ExternalCredentialRefHash != "") ||
		normalized.SkillSnapshotFingerprint == "" ||
		normalized.ConsentGrantedAt == "" ||
		hasEmptyOrDuplicate(normalized.SelectedTools) {
		return nil, ErrEgressDecisionInvalid
	}
	if len(normalized.DataCategories) == 0 {
		return nil, ErrEgressDecisionInvalid
	}
	previousCategory := 0
	for _, category := range normalized.DataCategories {
		order, ok := validEgressDataCategories[category]
		if !ok || order <= previousCategory {
			return nil, ErrEgressDecisionInvalid
		}
		previousCategory = order
	}
	endpoint, err := backendegress.ParseKeyedEndpoint(normalized.Endpoint)
	if err != nil ||
		endpoint.Canonical != normalized.Endpoint ||
		endpoint.Host != normalized.EndpointHost {
		return nil, ErrEgressDecisionInvalid
	}
	if _, err := time.Parse(time.RFC3339Nano, normalized.ConsentGrantedAt); err != nil {
		return nil, ErrEgressDecisionInvalid
	}
	return &normalized, nil
}

func hasEmptyOrDuplicate(values []string) bool {
	for index, value := range values {
		if strings.TrimSpace(value) == "" || (index > 0 && values[index-1] == value) {
			return true
		}
	}
	return false
}

func clonePendingEgressDecision(input *PendingEgressDecision) *PendingEgressDecision {
	if input == nil {
		return nil
	}
	cloned := *input
	cloned.DataCategories = append([]string(nil), input.DataCategories...)
	cloned.SelectedTools = append([]string(nil), input.SelectedTools...)
	slices.Sort(cloned.SelectedTools)
	return &cloned
}

func egressDecisionSelectedTools(decision *PendingEgressDecision, fallback []string) []string {
	if decision != nil {
		return append([]string(nil), decision.SelectedTools...)
	}
	selected := append([]string(nil), fallback...)
	slices.Sort(selected)
	return slices.Compact(selected)
}

func skillSnapshotFingerprint(snapshots []SkillSnapshot) (string, error) {
	canonical := make([]backendegress.SkillSnapshot, len(snapshots))
	for index, snapshot := range snapshots {
		instructions := snapshot.Body
		if instructions == "" {
			instructions = snapshot.Instructions
		}
		canonical[index] = backendegress.SkillSnapshot{
			SkillID: snapshot.SkillID, Name: snapshot.Name,
			Description: snapshot.Description, Category: snapshot.Category,
			Instructions: instructions, References: snapshot.References,
			Withheld:            snapshot.Withheld,
			MissingCapabilities: snapshot.MissingCapabilities,
		}
	}
	return backendegress.SkillSnapshotFingerprint(canonical)
}

func (r *Repository) EgressSkillSnapshotFingerprint(ctx context.Context) (string, error) {
	// go-sqlite3 does not enforce TxOptions.ReadOnly; the no-write guarantee is
	// provided by enabledSkillSnapshotsReadOnlyTx and its regression tests.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	snapshots, err := r.enabledSkillSnapshotsReadOnlyTx(ctx, tx)
	if err != nil {
		return "", err
	}
	fingerprint, err := skillSnapshotFingerprint(snapshots)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return fingerprint, nil
}

func insertRunEgressDecisionTx(ctx context.Context, tx *sql.Tx, runID string, pending *PendingEgressDecision) (RunEgressDecision, error) {
	categoriesJSON, err := json.Marshal(pending.DataCategories)
	if err != nil {
		return RunEgressDecision{}, err
	}
	toolsJSON, err := json.Marshal(pending.SelectedTools)
	if err != nil {
		return RunEgressDecision{}, err
	}
	decision := RunEgressDecision{
		DecisionID:                ids.New("egress"),
		RunID:                     runID,
		Version:                   pending.Version,
		ChallengeNonce:            pending.ChallengeNonce,
		ChallengeFingerprint:      pending.ChallengeFingerprint,
		RequestDigest:             pending.RequestDigest,
		Provider:                  pending.Provider,
		Model:                     pending.Model,
		ExternalAgentID:           pending.ExternalAgentID,
		ExternalCredentialRefHash: pending.ExternalCredentialRefHash,
		Endpoint:                  pending.Endpoint,
		EndpointHost:              pending.EndpointHost,
		DataCategories:            append([]string(nil), pending.DataCategories...),
		SelectedTools:             append([]string(nil), pending.SelectedTools...),
		SkillSnapshotFingerprint:  pending.SkillSnapshotFingerprint,
		RecallApplicable:          pending.RecallApplicable,
		MemoryProfileApplicable:   pending.MemoryProfileApplicable,
		ConsentGrantedAt:          pending.ConsentGrantedAt,
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO run_egress_decisions (
			decision_id, decision_version, run_id, challenge_nonce,
			challenge_fingerprint, request_digest, provider, model_name, external_agent_id,
			external_credential_ref_hash,
			endpoint, endpoint_host, data_categories_json, selected_tools_json,
			skill_snapshot_fingerprint, recall_applicable,
			memory_profile_applicable, consent_granted_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		decision.DecisionID,
		decision.Version,
		decision.RunID,
		decision.ChallengeNonce,
		decision.ChallengeFingerprint,
		decision.RequestDigest,
		decision.Provider,
		decision.Model,
		nullableText(decision.ExternalAgentID),
		decision.ExternalCredentialRefHash,
		decision.Endpoint,
		decision.EndpointHost,
		string(categoriesJSON),
		string(toolsJSON),
		decision.SkillSnapshotFingerprint,
		decision.RecallApplicable,
		decision.MemoryProfileApplicable,
		decision.ConsentGrantedAt,
	)
	if err != nil {
		if isUniqueViolation(err) &&
			strings.Contains(err.Error(), "run_egress_decisions.challenge_nonce") {
			return RunEgressDecision{}, ErrEgressChallengeAlreadyUsed
		}
		return RunEgressDecision{}, err
	}
	return decision, nil
}

func (r *Repository) GetRunEgressDecision(ctx context.Context, runID string) (RunEgressDecision, error) {
	var decision RunEgressDecision
	var externalAgentID sql.NullString
	var categoriesJSON, toolsJSON string
	err := r.db.QueryRowContext(ctx, `
		SELECT decision_id, run_id, decision_version, challenge_nonce,
			challenge_fingerprint, request_digest, provider, model_name, external_agent_id,
			external_credential_ref_hash,
			endpoint, endpoint_host, data_categories_json, selected_tools_json,
			skill_snapshot_fingerprint, recall_applicable,
			memory_profile_applicable, consent_granted_at
		FROM run_egress_decisions
		WHERE run_id = ?
	`, runID).Scan(
		&decision.DecisionID,
		&decision.RunID,
		&decision.Version,
		&decision.ChallengeNonce,
		&decision.ChallengeFingerprint,
		&decision.RequestDigest,
		&decision.Provider,
		&decision.Model,
		&externalAgentID,
		&decision.ExternalCredentialRefHash,
		&decision.Endpoint,
		&decision.EndpointHost,
		&categoriesJSON,
		&toolsJSON,
		&decision.SkillSnapshotFingerprint,
		&decision.RecallApplicable,
		&decision.MemoryProfileApplicable,
		&decision.ConsentGrantedAt,
	)
	if err != nil {
		return RunEgressDecision{}, err
	}

	if externalAgentID.Valid {
		decision.ExternalAgentID = externalAgentID.String
	}
	if err := json.Unmarshal([]byte(categoriesJSON), &decision.DataCategories); err != nil {
		return RunEgressDecision{}, err
	}
	if err := json.Unmarshal([]byte(toolsJSON), &decision.SelectedTools); err != nil {
		return RunEgressDecision{}, err
	}
	return decision, nil
}

func EnqueueRequestFingerprint(input EnqueueUserMessageInput) (string, error) {
	input = normalizeEnqueueUserMessageInput(input)
	return enqueueRequestFingerprint(input)
}

func (r *Repository) SendMessageIdempotencyExists(ctx context.Context, idempotencyKey string) (bool, error) {
	if idempotencyKey == "" {
		return false, nil
	}
	var exists int
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM send_message_idempotency WHERE idempotency_key = ?
		)
	`, idempotencyKey).Scan(&exists); err != nil {
		return false, err
	}
	return exists == 1, nil
}

func (r *Repository) LookupSendMessageReplay(ctx context.Context, idempotencyKey, fingerprint string) (EnqueueUserMessageResult, bool, error) {
	if idempotencyKey == "" {
		return EnqueueUserMessageResult{}, false, nil
	}
	// Intent-only with go-sqlite3; this path contains SELECTs exclusively.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return EnqueueUserMessageResult{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	record, found, err := findSendMessageIdempotencyTx(ctx, tx, idempotencyKey)
	if err != nil {
		return EnqueueUserMessageResult{}, false, err
	}
	if !found {
		if err := tx.Commit(); err != nil {
			return EnqueueUserMessageResult{}, false, err
		}
		return EnqueueUserMessageResult{}, false, nil
	}
	if record.RequestFingerprint != fingerprint {
		return EnqueueUserMessageResult{}, false, ErrIdempotencyConflict
	}
	if err := tx.Commit(); err != nil {
		return EnqueueUserMessageResult{}, false, err
	}
	return record.result(), true, nil
}
