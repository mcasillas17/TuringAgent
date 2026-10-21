package team

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"slices"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// delegationEgressChallengeDomain keeps this flow's challenges apart from
	// a send's: one signed for a task is never accepted by SendMessage, nor
	// one signed for a send here.
	delegationEgressChallengeDomain = "turing.delegation-egress.challenge.v1"
	delegationEgressChallengeTTL    = 5 * time.Minute
	maxDelegationEgressChallenge    = 32 * 1024
)

// A specialist's model is local, so only its tool calls may leave the
// machine: their arguments and their results.
var delegationEgressCategories = []turingv1.EgressDataCategory{
	turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
	turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS,
}

// delegationEgressChallenge is what a disclosure binds: the task, by its
// delegation and its own run, and everything the decision will freeze.
type delegationEgressChallenge struct {
	Version                   int                                    `json:"version"`
	Nonce                     string                                 `json:"nonce"`
	IssuedAtUnixNano          int64                                  `json:"issued_at_unix_nano"`
	ExpiresAtUnixNano         int64                                  `json:"expires_at_unix_nano"`
	DelegationID              string                                 `json:"delegation_id"`
	ChildRunID                string                                 `json:"child_run_id"`
	Model                     string                                 `json:"model"`
	SelectedTools             []string                               `json:"selected_tools"`
	DataCategories            []int32                                `json:"data_categories"`
	SkillSnapshotFingerprint  string                                 `json:"skill_snapshot_fingerprint"`
	MemorySnapshotFingerprint string                                 `json:"memory_snapshot_fingerprint"`
	RemoteMCPServers          []repository.RemoteMCPServerEgress     `json:"remote_mcp_servers"`
	IntegrationEndpoints      []repository.IntegrationEndpointEgress `json:"integration_endpoints"`
}

// SetEgressSigningSecret is TURING_EGRESS_SIGNING_SECRET. Without it no task
// can be consented to, as no send can.
func (s *Server) SetEgressSigningSecret(secret string) { s.egressSecret = secret }

// PrepareDelegationEgress is the disclosure for a task waiting for consent:
// where its tool calls may go and what may leave the machine, read from the
// task as it stands now. It writes nothing.
func (s *Server) PrepareDelegationEgress(ctx context.Context, req *turingv1.PrepareDelegationEgressRequest) (*turingv1.PrepareDelegationEgressResponse, error) {
	id, err := requireField("delegation_id", req.GetDelegationId())
	if err != nil {
		return nil, err
	}
	challenge, err := s.resolveDelegationEgress(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.egressSecret == "" {
		return nil, status.Error(codes.FailedPrecondition, "remote egress consent is not configured")
	}
	issued := s.now().UTC()
	if challenge.Nonce, err = s.nonce(); err != nil {
		return nil, status.Error(codes.Internal, "create the disclosure failed")
	}
	challenge.IssuedAtUnixNano = issued.UnixNano()
	challenge.ExpiresAtUnixNano = issued.Add(delegationEgressChallengeTTL).UnixNano()
	payload, err := json.Marshal(challenge)
	if err != nil || len(payload) > maxDelegationEgressChallenge {
		return nil, status.Error(codes.Internal, "create the disclosure failed")
	}
	return &turingv1.PrepareDelegationEgressResponse{Disclosure: &turingv1.RemoteEgressDisclosure{
		Challenge:            backendegress.SignChallenge(s.egressSecret, delegationEgressChallengeDomain, payload),
		Provider:             turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:                challenge.Model,
		DataCategories:       slices.Clone(delegationEgressCategories),
		ExpiresAt:            timestamppb.New(time.Unix(0, challenge.ExpiresAtUnixNano).UTC()),
		RemoteMcpServers:     repository.RemoteMCPServersProto(challenge.RemoteMCPServers),
		SelectedTools:        slices.Clone(challenge.SelectedTools),
		IntegrationEndpoints: repository.IntegrationEndpointsProto(challenge.IntegrationEndpoints),
	}}, nil
}

// DecideDelegationEgress records the user's decision for one task. A consent
// is accepted only as the disclosure it answers, still describing the task;
// none declines.
func (s *Server) DecideDelegationEgress(ctx context.Context, req *turingv1.DecideDelegationEgressRequest) (*turingv1.Delegation, error) {
	id, err := requireField("delegation_id", req.GetDelegationId())
	if err != nil {
		return nil, err
	}
	var result repository.DelegationEgressResult
	if consent := req.GetConsent(); consent == nil {
		result, err = s.repo.DeclineDelegationEgress(ctx, id)
	} else {
		pending, consentErr := s.consentedDecision(ctx, id, consent)
		if consentErr != nil {
			return nil, consentErr
		}
		result, err = s.repo.GrantDelegationEgress(ctx, id, pending)
	}
	if err != nil {
		return nil, delegationEgressError(err)
	}
	if s.events != nil {
		for _, event := range result.Events {
			s.events.Publish(events.FromRepositoryEvent(event))
		}
	}
	// A worker may take the task now, or a declined one's continuation.
	if s.routes != nil {
		if err := s.routes.DispatchPending(context.WithoutCancel(ctx)); err != nil {
			log.Printf("team: dispatch after consent for %s: %v", id, err)
		}
		if err := s.routes.RefreshPendingRoutingState(context.WithoutCancel(ctx), "delegation consent decided"); err != nil {
			log.Printf("team: refresh routing state after consent for %s: %v", id, err)
		}
	}
	return s.GetDelegation(ctx, &turingv1.GetDelegationRequest{DelegationId: id})
}

// consentedDecision opens the consent's challenge and turns it into the
// decision the task's run will carry, refusing one that does not answer its
// own disclosure or no longer describes the task.
func (s *Server) consentedDecision(ctx context.Context, id string, consent *turingv1.RemoteEgressConsent) (*repository.PendingEgressDecision, error) {
	refused := func(message string) error { return status.Error(codes.FailedPrecondition, message) }
	if !consent.GetAcknowledged() {
		return nil, refused("consent was not acknowledged")
	}
	if s.egressSecret == "" {
		return nil, refused("remote egress consent is not configured")
	}
	payload, err := backendegress.OpenChallenge(s.egressSecret, delegationEgressChallengeDomain, consent.GetChallenge(), maxDelegationEgressChallenge)
	if err != nil {
		return nil, refused("the consent challenge is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var challenge delegationEgressChallenge
	if err := decoder.Decode(&challenge); err != nil || decoder.More() {
		return nil, refused("the consent challenge is invalid")
	}
	acknowledged := make([]int32, 0, len(consent.GetAcknowledgedDataCategories()))
	for _, category := range consent.GetAcknowledgedDataCategories() {
		acknowledged = append(acknowledged, int32(category))
	}
	if challenge.DelegationID != id || !slices.Equal(challenge.DataCategories, acknowledged) {
		return nil, refused("the consent does not answer this task's disclosure")
	}
	if s.now().UTC().UnixNano() >= challenge.ExpiresAtUnixNano {
		return nil, refused("the consent challenge expired; review the task's tools again")
	}
	current, err := s.resolveDelegationEgress(ctx, id)
	if err != nil {
		return nil, err
	}
	current.Version, current.Nonce = challenge.Version, challenge.Nonce
	current.IssuedAtUnixNano, current.ExpiresAtUnixNano = challenge.IssuedAtUnixNano, challenge.ExpiresAtUnixNano
	if again, err := json.Marshal(current); err != nil || !bytes.Equal(again, payload) {
		return nil, refused("the task's tools changed since they were disclosed; review them again")
	}
	fingerprint := sha256.Sum256(payload)
	digest, err := json.Marshal(map[string]string{"delegation_id": id, "child_run_id": challenge.ChildRunID})
	if err != nil {
		return nil, status.Error(codes.Internal, "record consent failed")
	}
	requestDigest := sha256.Sum256(digest)
	categories := make([]string, len(delegationEgressCategories))
	for index, category := range delegationEgressCategories {
		categories[index] = category.String()
	}
	return &repository.PendingEgressDecision{
		Version:                   challenge.Version,
		ChallengeNonce:            challenge.Nonce,
		ChallengeFingerprint:      hex.EncodeToString(fingerprint[:]),
		RequestDigest:             hex.EncodeToString(requestDigest[:]),
		Provider:                  "ollama",
		Model:                     challenge.Model,
		DataCategories:            categories,
		SelectedTools:             challenge.SelectedTools,
		SkillSnapshotFingerprint:  challenge.SkillSnapshotFingerprint,
		MemorySnapshotFingerprint: challenge.MemorySnapshotFingerprint,
		ConsentGrantedAt:          repository.FormatTimestamp(s.now().UTC()),
		RemoteMCPServers:          challenge.RemoteMCPServers,
		IntegrationEndpoints:      challenge.IntegrationEndpoints,
	}, nil
}

// resolveDelegationEgress is the task as a decision would freeze it now:
// its whole set, where the tools that leave the machine would go, and the
// bindings, with no nonce or time. A task whose route a worker that can carry
// the decision would not run, or whose tools no longer go anywhere, is not
// offered for consent.
func (s *Server) resolveDelegationEgress(ctx context.Context, id string) (delegationEgressChallenge, error) {
	egress, err := s.repo.DelegationEgressContext(ctx, id)
	if err != nil {
		return delegationEgressChallenge{}, delegationEgressError(err)
	}
	remotes, err := s.repo.RemoteMCPServersForTools(ctx, egress.EgressTools)
	if err != nil {
		return delegationEgressChallenge{}, status.Error(codes.Internal, "resolve remote MCP egress failed")
	}
	integrations, err := s.repo.IntegrationEndpointsForTools(ctx, egress.EgressTools)
	if err != nil {
		return delegationEgressChallenge{}, status.Error(codes.Internal, "resolve integration egress failed")
	}
	if len(remotes) == 0 && len(integrations) == 0 {
		return delegationEgressChallenge{}, status.Error(codes.FailedPrecondition,
			"the tools this task asked for no longer leave the machine; run it without them")
	}
	if len(integrations) > repository.MaxIntegrationEndpoints {
		return delegationEgressChallenge{}, status.Error(codes.FailedPrecondition, "too many integration connections; revoke connections or run the task without them")
	}
	for _, destination := range integrations {
		if size, err := repository.IntegrationEndpointEntrySize(destination); err != nil || size > repository.MaxIntegrationEndpointEntryBytes {
			return delegationEgressChallenge{}, status.Error(codes.FailedPrecondition, "integration egress entry is too large; shorten the connection name or run the task without it")
		}
	}
	if s.routes == nil {
		return delegationEgressChallenge{}, status.Error(codes.FailedPrecondition, "no worker can run this task")
	}
	if err := s.routes.ValidateRouting(ctx, repository.RoutingRequirements{
		AgentID: childAgentID, ModelProvider: childProvider, Model: egress.Model,
		RequestedTools: egress.SelectedTools, SelectedTools: egress.SelectedTools,
		RemoteEgressDecision: true, MinimumTeamProtocolVersion: 1,
	}); err != nil {
		return delegationEgressChallenge{}, err
	}
	categories := make([]int32, len(delegationEgressCategories))
	for index, category := range delegationEgressCategories {
		categories[index] = int32(category)
	}
	return delegationEgressChallenge{
		Version:                   backendegress.DecisionVersion,
		DelegationID:              egress.DelegationID,
		ChildRunID:                egress.ChildRunID,
		Model:                     egress.Model,
		SelectedTools:             egress.SelectedTools,
		DataCategories:            categories,
		SkillSnapshotFingerprint:  egress.SkillSnapshotFingerprint,
		MemorySnapshotFingerprint: egress.MemorySnapshotFingerprint,
		RemoteMCPServers:          remotes,
		IntegrationEndpoints:      integrations,
	}, nil
}

func delegationEgressError(err error) error {
	switch {
	case errors.Is(err, repository.ErrDelegationNotFound):
		return status.Error(codes.NotFound, "delegation not found")
	case errors.Is(err, repository.ErrDelegationEgressNotPending), errors.Is(err, repository.ErrEgressChallengeAlreadyUsed):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, repository.ErrEgressDecisionInvalid):
		return status.Error(codes.FailedPrecondition, "the task's tools changed since they were disclosed; review them again")
	}
	return status.Error(codes.Internal, "record consent failed")
}
