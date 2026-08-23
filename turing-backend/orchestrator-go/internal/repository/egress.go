package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

const (
	GitHubIntegrationEndpoint        = "https://api.github.com"
	GitHubIntegrationEndpointHost    = "api.github.com"
	MaxIntegrationEndpoints          = 16
	MaxIntegrationEndpointEntryBytes = 768
	maxIntegrationDisplayNameRunes   = 64
)

type IntegrationEndpointEgress struct {
	Endpoint     string   `json:"endpoint"`
	EndpointHost string   `json:"endpoint_host"`
	ConnectionID string   `json:"connection_id"`
	DisplayName  string   `json:"display_name"`
	Tools        []string `json:"tools"`
}

type SkillEgressInfo struct {
	SkillID       string
	DisplayName   string
	BodyMayBeSent bool
}

func IntegrationEndpointEntrySize(entry IntegrationEndpointEgress) (int, error) {
	encoded, err := json.Marshal(entry)
	return len(encoded), err
}

const RunEgressDecisionVersion = backendegress.DecisionVersion

var (
	ErrRemoteEgressConsentRequired  = errors.New("remote run requires an egress decision")
	ErrLocalEgressDecisionForbidden = errors.New("local run must not carry an egress decision")
	ErrEgressChallengeAlreadyUsed   = errors.New("egress challenge was already used")
	ErrEgressDecisionInvalid        = errors.New("egress decision is invalid")
	ErrEgressSkillSnapshotChanged   = fmt.Errorf("egress skill snapshot changed: %w", ErrEgressDecisionInvalid)
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
	RemoteMCPServers          []RemoteMCPServerEgress
	IntegrationEndpoints      []IntegrationEndpointEgress
}

type RunEgressDecision struct {
	DecisionID                string                      `json:"decisionId"`
	RunID                     string                      `json:"runId"`
	Version                   int                         `json:"version"`
	ChallengeNonce            string                      `json:"challengeNonce"`
	ChallengeFingerprint      string                      `json:"challengeFingerprint"`
	RequestDigest             string                      `json:"requestDigest"`
	Provider                  string                      `json:"provider"`
	Model                     string                      `json:"model"`
	ExternalAgentID           string                      `json:"externalAgentId,omitempty"`
	ExternalCredentialRefHash string                      `json:"externalCredentialRefHash,omitempty"`
	Endpoint                  string                      `json:"endpoint"`
	EndpointHost              string                      `json:"endpointHost"`
	DataCategories            []string                    `json:"dataCategories"`
	SelectedTools             []string                    `json:"selectedTools"`
	SkillSnapshotFingerprint  string                      `json:"skillSnapshotFingerprint"`
	RecallApplicable          bool                        `json:"recallApplicable"`
	MemoryProfileApplicable   bool                        `json:"memoryProfileApplicable"`
	ConsentGrantedAt          string                      `json:"consentGrantedAt"`
	RemoteMCPServers          []RemoteMCPServerEgress     `json:"remoteMcpServers"`
	IntegrationEndpoints      []IntegrationEndpointEgress `json:"integrationEndpoints"`
}

func normalizePendingEgressDecision(input *PendingEgressDecision) (*PendingEgressDecision, error) {
	if input == nil {
		return nil, nil
	}
	normalized := *input
	normalized.DataCategories = append([]string(nil), input.DataCategories...)
	normalized.SelectedTools = append([]string(nil), input.SelectedTools...)
	normalized.RemoteMCPServers = append([]RemoteMCPServerEgress{}, input.RemoteMCPServers...)
	normalized.IntegrationEndpoints = cloneIntegrationEndpoints(input.IntegrationEndpoints)
	slices.Sort(normalized.SelectedTools)
	slices.SortFunc(normalized.RemoteMCPServers, func(left, right RemoteMCPServerEgress) int {
		return strings.Compare(left.ServerName, right.ServerName)
	})
	sortIntegrationEndpoints(normalized.IntegrationEndpoints)
	if normalized.Version != RunEgressDecisionVersion ||
		normalized.ChallengeNonce == "" ||
		normalized.ChallengeFingerprint == "" ||
		normalized.RequestDigest == "" ||
		(normalized.Provider != "openai_compatible" && normalized.Provider != "ollama") ||
		normalized.Model == "" ||
		(normalized.ExternalAgentID != "" && normalized.ExternalCredentialRefHash == "") ||
		(normalized.ExternalAgentID == "" && normalized.ExternalCredentialRefHash != "") ||
		normalized.SkillSnapshotFingerprint == "" ||
		normalized.ConsentGrantedAt == "" ||
		hasEmptyOrDuplicate(normalized.SelectedTools) {
		return nil, ErrEgressDecisionInvalid
	}
	if normalized.Provider == "openai_compatible" {
		if normalized.Endpoint == "" || normalized.EndpointHost == "" {
			return nil, ErrEgressDecisionInvalid
		}
	} else if normalized.Endpoint != "" || normalized.EndpointHost != "" ||
		normalized.ExternalAgentID != "" || normalized.ExternalCredentialRefHash != "" ||
		len(normalized.RemoteMCPServers) == 0 && len(normalized.IntegrationEndpoints) == 0 {
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
	if normalized.Endpoint != "" {
		endpoint, err := backendegress.ParseKeyedEndpoint(normalized.Endpoint)
		if err != nil ||
			endpoint.Canonical != normalized.Endpoint ||
			endpoint.Host != normalized.EndpointHost {
			return nil, ErrEgressDecisionInvalid
		}
	}
	for index, destination := range normalized.RemoteMCPServers {
		if destination.ServerName == "" ||
			(index > 0 && normalized.RemoteMCPServers[index-1].ServerName == destination.ServerName) {
			return nil, ErrEgressDecisionInvalid
		}
		endpoint, err := backendegress.ParseKeyedEndpoint(destination.Endpoint)
		if err != nil ||
			endpoint.Canonical != destination.Endpoint ||
			endpoint.Host != destination.EndpointHost {
			return nil, ErrEgressDecisionInvalid
		}
	}
	if len(normalized.IntegrationEndpoints) > MaxIntegrationEndpoints {
		return nil, ErrEgressDecisionInvalid
	}
	for index, destination := range normalized.IntegrationEndpoints {
		if !validIntegrationEndpoint(destination) ||
			(index > 0 && compareIntegrationEndpoint(normalized.IntegrationEndpoints[index-1], destination) >= 0) {
			return nil, ErrEgressDecisionInvalid
		}
		size, err := IntegrationEndpointEntrySize(destination)
		if err != nil || size > MaxIntegrationEndpointEntryBytes {
			return nil, ErrEgressDecisionInvalid
		}
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
	cloned.RemoteMCPServers = append([]RemoteMCPServerEgress{}, input.RemoteMCPServers...)
	cloned.IntegrationEndpoints = cloneIntegrationEndpoints(input.IntegrationEndpoints)
	slices.Sort(cloned.SelectedTools)
	slices.SortFunc(cloned.RemoteMCPServers, func(left, right RemoteMCPServerEgress) int {
		return strings.Compare(left.ServerName, right.ServerName)
	})
	sortIntegrationEndpoints(cloned.IntegrationEndpoints)
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

func (r *Repository) EgressSkillSnapshotFingerprint(ctx context.Context) (string, []SkillEgressInfo, error) {
	// go-sqlite3 does not enforce TxOptions.ReadOnly; the no-write guarantee is
	// provided by enabledSkillSnapshotsReadOnlyTx and its regression tests.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = tx.Rollback() }()
	snapshots, err := r.enabledSkillSnapshotsReadOnlyTx(ctx, tx)
	if err != nil {
		return "", nil, err
	}
	fingerprint, err := skillSnapshotFingerprint(snapshots)
	if err != nil {
		return "", nil, err
	}
	info := make([]SkillEgressInfo, len(snapshots))
	for index, snapshot := range snapshots {
		info[index] = SkillEgressInfo{
			SkillID:       snapshot.SkillID,
			DisplayName:   backendegress.SanitizeSkillDisplayName(snapshot.Name, snapshot.SkillID),
			BodyMayBeSent: !snapshot.Withheld,
		}
	}
	if err := tx.Commit(); err != nil {
		return "", nil, err
	}
	return fingerprint, info, nil
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
	remoteMCPServersJSON, err := json.Marshal(pending.RemoteMCPServers)
	if err != nil {
		return RunEgressDecision{}, err
	}
	integrationEndpointsJSON, err := json.Marshal(pending.IntegrationEndpoints)
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
		RemoteMCPServers:          append([]RemoteMCPServerEgress{}, pending.RemoteMCPServers...),
		IntegrationEndpoints:      cloneIntegrationEndpoints(pending.IntegrationEndpoints),
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO run_egress_decisions (
			decision_id, decision_version, run_id, challenge_nonce,
			challenge_fingerprint, request_digest, provider, model_name, external_agent_id,
			external_credential_ref_hash,
			endpoint, endpoint_host, data_categories_json, selected_tools_json,
			skill_snapshot_fingerprint, recall_applicable,
			memory_profile_applicable, consent_granted_at, remote_mcp_servers_json,
			integration_endpoints_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		string(remoteMCPServersJSON),
		string(integrationEndpointsJSON),
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
	var categoriesJSON, toolsJSON, remoteMCPServersJSON, integrationEndpointsJSON string
	err := r.db.QueryRowContext(ctx, `
		SELECT decision_id, run_id, decision_version, challenge_nonce,
			challenge_fingerprint, request_digest, provider, model_name, external_agent_id,
			external_credential_ref_hash,
			endpoint, endpoint_host, data_categories_json, selected_tools_json,
			skill_snapshot_fingerprint, recall_applicable,
			memory_profile_applicable, consent_granted_at, remote_mcp_servers_json,
			integration_endpoints_json
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
		&remoteMCPServersJSON,
		&integrationEndpointsJSON,
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
	if err := json.Unmarshal([]byte(remoteMCPServersJSON), &decision.RemoteMCPServers); err != nil {
		return RunEgressDecision{}, err
	}
	if err := json.Unmarshal([]byte(integrationEndpointsJSON), &decision.IntegrationEndpoints); err != nil {
		return RunEgressDecision{}, err
	}
	return decision, nil
}

func (r *Repository) RunAllowsIntegration(ctx context.Context, runID, endpoint, connectionID, toolName string) (bool, error) {
	decision, err := r.GetRunEgressDecision(ctx, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !slices.Contains(decision.SelectedTools, "integrations/"+toolName) ||
		!slices.Contains(decision.DataCategories, "EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS") ||
		!slices.Contains(decision.DataCategories, "EGRESS_DATA_CATEGORY_TOOL_RESULTS") {
		return false, nil
	}
	for _, destination := range decision.IntegrationEndpoints {
		if destination.Endpoint == endpoint && destination.ConnectionID == connectionID && slices.Contains(destination.Tools, toolName) {
			return true, nil
		}
	}
	return false, nil
}

func (r *Repository) IntegrationEndpointsForTools(ctx context.Context, selectedTools []string) ([]IntegrationEndpointEgress, error) {
	toolNames := make([]string, 0)
	for _, selected := range selectedTools {
		serverName, toolName, ok := strings.Cut(selected, "/")
		if !ok || serverName != "integrations" || toolName == "" {
			continue
		}
		available, err := r.PseudoServerToolAvailable(ctx, serverName, toolName)
		if err != nil {
			return nil, err
		}
		if available {
			toolNames = append(toolNames, toolName)
		}
	}
	slices.Sort(toolNames)
	toolNames = slices.Compact(toolNames)
	if len(toolNames) == 0 {
		return []IntegrationEndpointEgress{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, display_name FROM integration_connections
		WHERE provider = 'github' AND status = 'connected' AND credential_ciphertext IS NOT NULL
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]IntegrationEndpointEgress, 0)
	for rows.Next() {
		var connectionID, displayName string
		if err := rows.Scan(&connectionID, &displayName); err != nil {
			return nil, err
		}
		result = append(result, IntegrationEndpointEgress{
			Endpoint: GitHubIntegrationEndpoint, EndpointHost: GitHubIntegrationEndpointHost,
			ConnectionID: connectionID, DisplayName: integrationDisplayName(displayName, connectionID),
			Tools: append([]string{}, toolNames...),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortIntegrationEndpoints(result)
	return result, nil
}

func (r *Repository) IntegrationDispatchActive(ctx context.Context, runID, toolName, expectedPolicy string) (bool, error) {
	var active bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM tools tool
			JOIN agent_runs run ON run.id = ?
			JOIN sessions session ON session.id = run.session_id
			WHERE tool.server_name = 'integrations' AND tool.tool_name = ?
				AND tool.policy = ? AND tool.mcp_server_id IS NULL
				AND run.execution_active = 1 AND run.status = 'running'
				AND session.deletion_state = 'active'
		)
	`, runID, toolName, expectedPolicy).Scan(&active)
	return active, err
}

func cloneIntegrationEndpoints(input []IntegrationEndpointEgress) []IntegrationEndpointEgress {
	result := make([]IntegrationEndpointEgress, len(input))
	for index := range input {
		result[index] = input[index]
		result[index].Tools = append([]string{}, input[index].Tools...)
		slices.Sort(result[index].Tools)
	}
	return result
}

func sortIntegrationEndpoints(input []IntegrationEndpointEgress) {
	slices.SortFunc(input, compareIntegrationEndpoint)
}

func compareIntegrationEndpoint(left, right IntegrationEndpointEgress) int {
	if compared := strings.Compare(left.Endpoint, right.Endpoint); compared != 0 {
		return compared
	}
	return strings.Compare(left.ConnectionID, right.ConnectionID)
}

func validIntegrationEndpoint(destination IntegrationEndpointEgress) bool {
	if destination.Endpoint != GitHubIntegrationEndpoint || destination.EndpointHost != GitHubIntegrationEndpointHost ||
		destination.ConnectionID == "" || destination.DisplayName == "" || len(destination.Tools) == 0 ||
		hasEmptyOrDuplicate(destination.Tools) || !slices.IsSorted(destination.Tools) {
		return false
	}
	return utf8.RuneCountInString(destination.DisplayName) <= maxIntegrationDisplayNameRunes
}

func integrationDisplayName(displayName, connectionID string) string {
	runes := []rune(displayName)
	if len(runes) <= maxIntegrationDisplayNameRunes {
		return displayName
	}
	discriminator := connectionID
	if len(discriminator) > 8 {
		discriminator = discriminator[len(discriminator)-8:]
	}
	suffix := []rune("… (" + discriminator + ")")
	keep := maxIntegrationDisplayNameRunes - len(suffix)
	if keep < 0 {
		keep = 0
	}
	return string(runes[:keep]) + string(suffix)
}

func (r *Repository) RunAllowsRemoteMCP(
	ctx context.Context,
	runID string,
	serverName string,
	endpoint string,
	toolName string,
) (bool, error) {
	decision, err := r.GetRunEgressDecision(ctx, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !slices.Contains(decision.SelectedTools, serverName+"/"+toolName) ||
		!slices.Contains(decision.DataCategories, "EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS") ||
		!slices.Contains(decision.DataCategories, "EGRESS_DATA_CATEGORY_TOOL_RESULTS") {
		return false, nil
	}
	for _, destination := range decision.RemoteMCPServers {
		if destination.ServerName == serverName && destination.Endpoint == endpoint {
			return true, nil
		}
	}
	return false, nil
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
