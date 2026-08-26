package chat

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	egressChallengeVersion     = 3
	egressChallengeDomain      = "turing.remote-egress.challenge.v1"
	defaultEgressChallengeTTL  = 5 * time.Minute
	maxEgressChallengeBytes    = 32 * 1024
	maxEgressContentBytes      = 1024 * 1024
	maxEgressIDBytes           = 512
	maxEgressModelBytes        = 512
	maxEgressTools             = 256
	maxEgressSkills            = 256
	maxEgressToolNameBytes     = 512
	maxEgressSelectedToolBytes = 16 * 1024
)

// memoryDriftMessage is what a person gets when the vault moved between the
// disclosure they read and the send they made.
//
// It names both pinned files rather than guessing which one moved: the
// challenge carries one fingerprint over the pair, and inventing a specific
// tier from that would be a confident sentence this code cannot support.
//
// It names Obsidian because that is almost always the cause. The vault is a
// folder the user has open in an editor, and an autosave landing between
// consent and send is not a mistake they made — it is the product working as
// designed. The refusal has to read like an explanation, not an accusation.
const memoryDriftMessage = "your pinned memory changed since consent was prepared: " +
	"persona.md or profile.md was edited, which an open Obsidian editor can do by autosaving. " +
	"Re-read the pinned memory and prepare the send again"

type EgressConfig struct {
	OpenAIBaseURL string
	SigningSecret string
	ChallengeTTL  time.Duration
}

type liveToolSource interface {
	LiveToolNames() []string
}

type egressToolSource interface {
	EgressToolNames(repository.RoutingRequirements) []string
}

type egressContext struct {
	Provider                  string
	Model                     string
	ExternalAgentID           string
	ExternalCredentialRefHash string
	Endpoint                  string
	EndpointHost              string
	DataCategories            []turingv1.EgressDataCategory
	SelectedTools             []string
	SkillSnapshotFingerprint  string
	SkillInfo                 []repository.SkillEgressInfo
	RecallApplicable          bool
	MemoryProfileApplicable   bool
	MemorySnapshotFingerprint string
	MemorySnapshot            repository.MemoryEgressSnapshot
	RemoteMCPServers          []repository.RemoteMCPServerEgress
	IntegrationEndpoints      []repository.IntegrationEndpointEgress
}

type remoteMCPChallengeDestination struct {
	ServerName   string `json:"server_name"`
	Endpoint     string `json:"endpoint"`
	EndpointHost string `json:"endpoint_host"`
}

type egressChallengePayload struct {
	Version                   int                                    `json:"version"`
	Nonce                     string                                 `json:"nonce"`
	IssuedAtUnixNano          int64                                  `json:"issued_at_unix_nano"`
	ExpiresAtUnixNano         int64                                  `json:"expires_at_unix_nano"`
	SessionID                 string                                 `json:"session_id"`
	IdempotencyKey            string                                 `json:"idempotency_key"`
	RequestDigest             string                                 `json:"request_digest"`
	Provider                  string                                 `json:"provider"`
	Model                     string                                 `json:"model"`
	ExternalAgentID           string                                 `json:"external_agent_id,omitempty"`
	ExternalCredentialRefHash string                                 `json:"external_credential_ref_hash,omitempty"`
	Endpoint                  string                                 `json:"endpoint"`
	EndpointHost              string                                 `json:"endpoint_host"`
	DataCategories            []int32                                `json:"data_categories"`
	SelectedTools             []string                               `json:"selected_tools"`
	SkillSnapshotFingerprint  string                                 `json:"skill_snapshot_fingerprint"`
	RecallApplicable          bool                                   `json:"recall_applicable"`
	MemoryProfileApplicable   bool                                   `json:"memory_profile_applicable"`
	MemorySnapshotFingerprint string                                 `json:"memory_snapshot_fingerprint"`
	RemoteMCPServers          []remoteMCPChallengeDestination        `json:"remote_mcp_servers,omitempty"`
	IntegrationEndpoints      []repository.IntegrationEndpointEgress `json:"integration_endpoints,omitempty"`
}

func (s *Server) PrepareRemoteEgress(ctx context.Context, req *turingv1.PrepareRemoteEgressRequest) (*turingv1.PrepareRemoteEgressResponse, error) {
	input, err := s.prepareEgressInput(req)
	if err != nil {
		return nil, err
	}
	withdrawalState, err := s.repo.SessionWithdrawalState(ctx, input.SessionID)
	if err != nil {
		return nil, mapSessionError(ctx, err)
	}
	if !withdrawalState.Active {
		return nil, mapSessionError(ctx, repository.ErrSessionDeleting)
	}
	resolved, err := s.resolveEgressContext(ctx, input)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return &turingv1.PrepareRemoteEgressResponse{}, nil
	}
	if s.egress.SigningSecret == "" {
		return nil, status.Error(codes.FailedPrecondition, "remote egress consent is not configured")
	}
	now := s.now().UTC()
	ttl := s.egress.ChallengeTTL
	if ttl <= 0 {
		ttl = defaultEgressChallengeTTL
	}
	nonce, err := s.nonce()
	if err != nil {
		return nil, status.Error(codes.Internal, "create remote egress disclosure failed")
	}
	requestDigest, err := egressRequestDigest(input)
	if err != nil {
		return nil, status.Error(codes.Internal, "create remote egress disclosure failed")
	}
	payload := egressChallengePayload{
		Version:                   egressChallengeVersion,
		Nonce:                     nonce,
		IssuedAtUnixNano:          now.UnixNano(),
		ExpiresAtUnixNano:         now.Add(ttl).UnixNano(),
		SessionID:                 input.SessionID,
		IdempotencyKey:            input.IdempotencyKey,
		RequestDigest:             requestDigest,
		Provider:                  resolved.Provider,
		Model:                     resolved.Model,
		ExternalAgentID:           resolved.ExternalAgentID,
		ExternalCredentialRefHash: resolved.ExternalCredentialRefHash,
		Endpoint:                  resolved.Endpoint,
		EndpointHost:              resolved.EndpointHost,
		DataCategories:            categoryNumbers(resolved.DataCategories),
		SelectedTools:             append([]string(nil), resolved.SelectedTools...),
		SkillSnapshotFingerprint:  resolved.SkillSnapshotFingerprint,
		RecallApplicable:          resolved.RecallApplicable,
		MemoryProfileApplicable:   resolved.MemoryProfileApplicable,
		MemorySnapshotFingerprint: resolved.MemorySnapshotFingerprint,
		RemoteMCPServers:          toChallengeRemoteMCPServers(resolved.RemoteMCPServers),
		IntegrationEndpoints:      cloneChallengeIntegrationEndpoints(resolved.IntegrationEndpoints),
	}
	challenge, err := s.signEgressChallenge(payload)
	if err != nil {
		return nil, status.Error(codes.Internal, "create remote egress disclosure failed")
	}
	disclosedSkills := []*turingv1.SkillEgressDisclosure(nil)
	if slices.Contains(resolved.DataCategories, turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_SKILL_CONTENT) {
		disclosedSkills = toProtoSkillEgressDisclosures(resolved.SkillInfo)
	}
	return &turingv1.PrepareRemoteEgressResponse{
		Disclosure: &turingv1.RemoteEgressDisclosure{
			Challenge:            challenge,
			Provider:             providerToProto(resolved.Provider),
			Model:                resolved.Model,
			Endpoint:             resolved.Endpoint,
			EndpointHost:         resolved.EndpointHost,
			ExternalAgentId:      resolved.ExternalAgentID,
			DataCategories:       append([]turingv1.EgressDataCategory(nil), resolved.DataCategories...),
			ExpiresAt:            timestamppb.New(time.Unix(0, payload.ExpiresAtUnixNano).UTC()),
			RemoteMcpServers:     toProtoRemoteMCPServers(resolved.RemoteMCPServers),
			IntegrationEndpoints: toProtoIntegrationEndpoints(resolved.IntegrationEndpoints),
			SelectedTools:        append([]string(nil), resolved.SelectedTools...),
			Skills:               disclosedSkills,
			MemoryNotes:          toProtoMemoryEgressDisclosures(*resolved),
			// The flag and the rows agree by construction: both are gated on the
			// category actually being claimed, so a disclosure can never say
			// "memory may be sent" while naming nothing, or name a tier on a run
			// that will send none of it.
			MemoryProfileMayBeSent: resolved.MemoryProfileApplicable,
		},
	}, nil
}

// toProtoMemoryEgressDisclosures names the pinned tiers a person is consenting
// over. It carries the vault path, so they can go and read exactly what would
// be sent, and never the pinned bytes themselves: a disclosure is a list of
// what leaves, not a second copy of it. The fingerprint is absent for the same
// reason — it is the run's internal binding, not something a client acts on.
func toProtoMemoryEgressDisclosures(resolved egressContext) []*turingv1.MemoryEgressDisclosure {
	if !resolved.MemoryProfileApplicable {
		return nil
	}
	rows := make([]*turingv1.MemoryEgressDisclosure, 0, 3)
	for _, tier := range []struct {
		tier     turingv1.MemoryTier
		title    string
		document repository.MemoryPinnedDocument
	}{
		{turingv1.MemoryTier_MEMORY_TIER_PERSONA, "Persona", resolved.MemorySnapshot.Persona},
		{turingv1.MemoryTier_MEMORY_TIER_PROFILE, "Profile", resolved.MemorySnapshot.Profile},
	} {
		if !tier.document.Available || strings.TrimSpace(tier.document.Content) == "" {
			continue
		}
		rows = append(rows, &turingv1.MemoryEgressDisclosure{
			NoteId:        tier.document.RelPath,
			Title:         tier.title,
			VaultPath:     tier.document.RelPath,
			Tier:          tier.tier,
			BodyMayBeSent: true,
		})
	}
	// A memory tool is a second, larger door: nothing is pinned through it, but
	// whatever the model searches for and reads travels as tool arguments and
	// results. Naming the folder rather than its notes is the honest bound —
	// which notes get sent depends on what the model asks for, and this
	// disclosure is written before it asks.
	if backendegress.SelectedToolsIncludeMemory(resolved.SelectedTools) {
		rows = append(rows, &turingv1.MemoryEgressDisclosure{
			NoteId:        memoryfiles.BeliefsDirName,
			Title:         "Accepted memory reachable by the memory tools",
			VaultPath:     memoryfiles.BeliefsDirName,
			Tier:          turingv1.MemoryTier_MEMORY_TIER_BELIEF,
			BodyMayBeSent: true,
		})
	}
	return rows
}

func (s *Server) applyRemoteEgress(
	ctx context.Context,
	req *turingv1.SendMessageRequest,
	input *repository.EnqueueUserMessageInput,
) (repository.EnqueueUserMessageResult, bool, error) {
	consent := req.GetRemoteEgressConsent()
	if consent == nil {
		idempotencyExists, err := s.repo.SendMessageIdempotencyExists(ctx, input.IdempotencyKey)
		if err != nil {
			return repository.EnqueueUserMessageResult{}, false,
				status.Error(codes.Internal, "look up idempotent send failed")
		}
		if idempotencyExists {
			fingerprint, err := repository.EnqueueRequestFingerprint(*input)
			if err != nil {
				return repository.EnqueueUserMessageResult{}, false,
					status.Error(codes.Internal, "validate idempotent send failed")
			}
			replayed, found, err := s.lookupSendReplay(ctx, input.IdempotencyKey, fingerprint)
			if err != nil || found {
				return replayed, found, err
			}
		}
		resolved, err := s.resolveEgressContext(ctx, *input)
		if err != nil {
			return repository.EnqueueUserMessageResult{}, false, err
		}
		if resolved != nil {
			if err := validateRemoteSendBounds(req, input.RequestedTools); err != nil {
				return repository.EnqueueUserMessageResult{}, false, err
			}
			return repository.EnqueueUserMessageResult{}, false,
				status.Error(codes.FailedPrecondition, "explicit consent is required for this remote run")
		}
		return repository.EnqueueUserMessageResult{}, false, nil
	}
	if !consent.GetAcknowledged() {
		return repository.EnqueueUserMessageResult{}, false,
			status.Error(codes.FailedPrecondition, "remote egress consent was not acknowledged")
	}
	if s.egress.SigningSecret == "" {
		return repository.EnqueueUserMessageResult{}, false,
			status.Error(codes.FailedPrecondition, "remote egress consent is not configured")
	}
	payload, payloadBytes, err := s.parseEgressChallenge(consent.GetChallenge())
	if err != nil {
		return repository.EnqueueUserMessageResult{}, false,
			status.Error(codes.FailedPrecondition, "remote egress challenge is invalid")
	}
	requestDigest, err := egressRequestDigest(*input)
	if err != nil {
		return repository.EnqueueUserMessageResult{}, false,
			status.Error(codes.Internal, "validate remote egress consent failed")
	}
	fingerprintSum := sha256.Sum256(payloadBytes)
	pending := &repository.PendingEgressDecision{
		Version:                   payload.Version,
		ChallengeNonce:            payload.Nonce,
		ChallengeFingerprint:      hex.EncodeToString(fingerprintSum[:]),
		RequestDigest:             payload.RequestDigest,
		Provider:                  payload.Provider,
		Model:                     payload.Model,
		ExternalAgentID:           payload.ExternalAgentID,
		ExternalCredentialRefHash: payload.ExternalCredentialRefHash,
		Endpoint:                  payload.Endpoint,
		EndpointHost:              payload.EndpointHost,
		DataCategories:            storageCategoryNames(payload.DataCategories),
		SelectedTools:             append([]string(nil), payload.SelectedTools...),
		SkillSnapshotFingerprint:  payload.SkillSnapshotFingerprint,
		RecallApplicable:          payload.RecallApplicable,
		MemoryProfileApplicable:   payload.MemoryProfileApplicable,
		MemorySnapshotFingerprint: payload.MemorySnapshotFingerprint,
		RemoteMCPServers:          fromChallengeRemoteMCPServers(payload.RemoteMCPServers),
		IntegrationEndpoints:      cloneChallengeIntegrationEndpoints(payload.IntegrationEndpoints),
		ConsentGrantedAt:          repository.FormatTimestamp(s.now().UTC()),
	}
	currentModel := input.Model
	currentExecutionModel := input.ExecutionModel
	input.Model = payload.Model
	input.ExecutionModel = payload.Model
	fingerprintDecision := *pending
	fingerprintDecision.DataCategories = storageCategoryNames(
		categoryNumbers(consent.GetAcknowledgedDataCategories()),
	)
	input.EgressDecision = &fingerprintDecision
	input.SelectedTools = append([]string(nil), payload.SelectedTools...)
	fingerprint, err := repository.EnqueueRequestFingerprint(*input)
	if err != nil {
		return repository.EnqueueUserMessageResult{}, false,
			status.Error(codes.Internal, "validate remote egress consent failed")
	}
	replayed, found, err := s.lookupSendReplay(ctx, input.IdempotencyKey, fingerprint)
	if err != nil || found {
		return replayed, found, err
	}
	if payload.SessionID != input.SessionID ||
		payload.IdempotencyKey != input.IdempotencyKey ||
		payload.RequestDigest != requestDigest ||
		!slices.Equal(payload.DataCategories, categoryNumbers(consent.GetAcknowledgedDataCategories())) {
		return repository.EnqueueUserMessageResult{}, false,
			status.Error(codes.FailedPrecondition, "remote egress consent does not match this request")
	}
	input.EgressDecision = pending
	if s.now().UTC().UnixNano() >= payload.ExpiresAtUnixNano {
		return repository.EnqueueUserMessageResult{}, false,
			status.Error(codes.FailedPrecondition, "remote egress challenge expired")
	}
	input.Model = currentModel
	input.ExecutionModel = currentExecutionModel
	resolved, err := s.resolveEgressContext(ctx, *input)
	if err != nil {
		return repository.EnqueueUserMessageResult{}, false, err
	}
	if resolved == nil || !payloadMatchesEgressContext(payload, *resolved) {
		if resolved != nil && payload.SkillSnapshotFingerprint != resolved.SkillSnapshotFingerprint {
			return repository.EnqueueUserMessageResult{}, false,
				status.Error(codes.FailedPrecondition, "the skill snapshot changed since consent was prepared; prepare the send again")
		}
		if resolved != nil && payload.MemorySnapshotFingerprint != resolved.MemorySnapshotFingerprint {
			return repository.EnqueueUserMessageResult{}, false,
				status.Error(codes.FailedPrecondition, memoryDriftMessage)
		}
		return repository.EnqueueUserMessageResult{}, false,
			status.Error(codes.FailedPrecondition, "remote egress context changed; prepare the send again")
	}
	input.Model = payload.Model
	input.ExecutionModel = payload.Model
	return repository.EnqueueUserMessageResult{}, false, nil
}

func validateRemoteSendBounds(req *turingv1.SendMessageRequest, requestedTools []string) error {
	if len(req.GetSessionId()) > maxEgressIDBytes ||
		len(req.GetIdempotencyKey()) > maxEgressIDBytes ||
		len(req.GetModel()) > maxEgressModelBytes {
		return status.Error(codes.InvalidArgument, "request metadata is too long")
	}
	if len(req.GetContent()) > maxEgressContentBytes {
		return status.Error(codes.InvalidArgument, "content is too long")
	}
	if len(requestedTools) > maxEgressTools {
		return status.Error(codes.InvalidArgument, "too many requested_tools entries")
	}
	for _, tool := range requestedTools {
		if len(tool) > maxEgressToolNameBytes {
			return status.Error(codes.InvalidArgument, "requested tool name is too long")
		}
	}
	return nil
}

func (s *Server) lookupSendReplay(
	ctx context.Context,
	idempotencyKey string,
	fingerprint string,
) (repository.EnqueueUserMessageResult, bool, error) {
	replayed, found, err := s.repo.LookupSendMessageReplay(ctx, idempotencyKey, fingerprint)
	if errors.Is(err, repository.ErrIdempotencyConflict) {
		return repository.EnqueueUserMessageResult{}, false,
			status.Error(codes.AlreadyExists, "idempotency key was already used for a different request")
	}
	if err != nil {
		return repository.EnqueueUserMessageResult{}, false,
			status.Error(codes.Internal, "look up idempotent send failed")
	}
	return replayed, found, nil
}

func (s *Server) prepareEgressInput(req *turingv1.PrepareRemoteEgressRequest) (repository.EnqueueUserMessageInput, error) {
	if req == nil {
		return repository.EnqueueUserMessageInput{}, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetSessionId() == "" || len(req.GetSessionId()) > maxEgressIDBytes {
		return repository.EnqueueUserMessageInput{}, status.Error(codes.InvalidArgument, "session_id is required and must be bounded")
	}
	if req.GetContent() == "" || len(req.GetContent()) > maxEgressContentBytes {
		return repository.EnqueueUserMessageInput{}, status.Error(codes.InvalidArgument, "content is required and must be bounded")
	}
	if len(req.GetIdempotencyKey()) > maxEgressIDBytes || len(req.GetModel()) > maxEgressModelBytes {
		return repository.EnqueueUserMessageInput{}, status.Error(codes.InvalidArgument, "request metadata is too long")
	}
	if err := requestContentType(req.GetContentType()); err != nil {
		return repository.EnqueueUserMessageInput{}, err
	}
	requestedTools, err := requestTools(req.GetRequestedTools())
	if err != nil {
		return repository.EnqueueUserMessageInput{}, err
	}
	if len(requestedTools) > maxEgressTools {
		return repository.EnqueueUserMessageInput{}, status.Error(codes.InvalidArgument, "too many requested_tools entries")
	}
	for _, tool := range requestedTools {
		if len(tool) > maxEgressToolNameBytes {
			return repository.EnqueueUserMessageInput{}, status.Error(codes.InvalidArgument, "requested tool name is too long")
		}
	}
	if req.GetRequiredContextTokens() < 0 || req.GetMinimumWorkerMaxConcurrentRuns() < 0 {
		return repository.EnqueueUserMessageInput{}, status.Error(codes.InvalidArgument, "routing limits must be non-negative")
	}
	agentID, err := requestAgentID(req.GetAgentId())
	if err != nil {
		return repository.EnqueueUserMessageInput{}, err
	}
	provider, err := requestModelProvider(req.GetModelProvider())
	if err != nil {
		return repository.EnqueueUserMessageInput{}, err
	}
	model := req.GetModel()
	executionModel := model
	if model == "" {
		configured := s.ollamaModel
		if provider == "openai_compatible" {
			configured = s.openAIModel
		}
		model = configured
		executionModel = configured
		if s.runtime != nil {
			executionModel = s.runtime.RoutableDefaultModel(provider, configured)
		}
	}
	return repository.EnqueueUserMessageInput{
		SessionID: req.GetSessionId(), Content: req.GetContent(), ContentType: "text",
		AgentID: agentID, ModelProvider: provider, Model: model, ExecutionModel: executionModel,
		RequestedModel: req.GetModel(),
		IdempotencyKey: req.GetIdempotencyKey(), RequestedTools: requestedTools,
		RequiredContextTokens:          int(req.GetRequiredContextTokens()),
		MinimumWorkerMaxConcurrentRuns: int(req.GetMinimumWorkerMaxConcurrentRuns()),
	}, nil
}

func (s *Server) resolveEgressContext(ctx context.Context, input repository.EnqueueUserMessageInput) (*egressContext, error) {
	resolved := &egressContext{}
	externalCredentialRef := ""
	rawEndpoint := ""
	providerEgress := false
	agent, routed, err := s.repo.GetSessionAgent(ctx, input.SessionID)
	if err != nil {
		return nil, mapSessionError(ctx, err)
	}
	if routed {
		resolved.Provider = "openai_compatible"
		resolved.Model = agent.Model
		resolved.ExternalAgentID = agent.AgentID
		resolved.ExternalCredentialRefHash = backendegress.HashCredentialReference(agent.CredentialRef)
		rawEndpoint = agent.BaseURL
		externalCredentialRef = agent.CredentialRef
		providerEgress = true
	} else if input.ModelProvider == "openai_compatible" {
		resolved.Provider = input.ModelProvider
		resolved.Model = input.Model
		if input.ExecutionModel != "" {
			resolved.Model = input.ExecutionModel
		}
		rawEndpoint = s.egress.OpenAIBaseURL
		resolved.RecallApplicable = true
		providerEgress = true
	} else {
		resolved.Provider = input.ModelProvider
		resolved.Model = input.Model
		if input.ExecutionModel != "" {
			resolved.Model = input.ExecutionModel
		}
	}
	if len(resolved.Model) == 0 || len(resolved.Model) > maxEgressModelBytes {
		return nil, status.Error(codes.FailedPrecondition, "remote egress model name is too long")
	}
	route := repository.RoutingRequirements{
		AgentID: input.AgentID, ModelProvider: resolved.Provider, Model: resolved.Model,
		RequestedTools:                 input.RequestedTools,
		RequiredContextTokens:          input.RequiredContextTokens,
		MinimumWorkerMaxConcurrentRuns: input.MinimumWorkerMaxConcurrentRuns,
		ExternalAgent:                  routed, ExternalAgentCredentialRef: externalCredentialRef,
	}
	// A first pass, on what is known before anything has looked at where the
	// frozen tools go. It is worth making early — an unknown model or a tool no
	// worker has is a better answer than one arrived at after reading the vault
	// — but it is provisional by construction: this route says nothing yet
	// about whether the run will carry an egress decision, and a worker too old
	// to validate one satisfies it. The authoritative check is below, once that
	// is known.
	if s.runtime != nil {
		if err := s.runtime.ValidateRouting(ctx, route); err != nil {
			return nil, err
		}
	}
	if source, ok := s.runtime.(egressToolSource); ok {
		resolved.SelectedTools = source.EgressToolNames(route)
	} else if source, ok := s.runtime.(liveToolSource); ok {
		resolved.SelectedTools = source.LiveToolNames()
	}
	slices.Sort(resolved.SelectedTools)
	resolved.SelectedTools = slices.Compact(resolved.SelectedTools)
	if len(resolved.SelectedTools) > maxEgressTools {
		return nil, status.Error(codes.FailedPrecondition, "remote egress tool snapshot has too many entries")
	}
	selectedToolBytes := 0
	for _, tool := range resolved.SelectedTools {
		if len(tool) > maxEgressToolNameBytes {
			return nil, status.Error(codes.FailedPrecondition, "remote egress tool snapshot contains an oversized name")
		}
		selectedToolBytes += len(tool)
	}
	if selectedToolBytes > maxEgressSelectedToolBytes {
		return nil, status.Error(codes.FailedPrecondition, "remote egress tool snapshot is too large")
	}
	resolved.RemoteMCPServers, err = s.repo.RemoteMCPServersForTools(ctx, resolved.SelectedTools)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "resolve remote MCP egress failed")
	}
	resolved.IntegrationEndpoints, err = s.integrationEndpointResolver(ctx, resolved.SelectedTools)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "resolve integration egress failed")
	}
	if len(resolved.IntegrationEndpoints) > repository.MaxIntegrationEndpoints {
		return nil, status.Error(codes.FailedPrecondition, "too many integration connections; revoke connections or disable integration tools")
	}
	for _, destination := range resolved.IntegrationEndpoints {
		size, sizeErr := repository.IntegrationEndpointEntrySize(destination)
		if sizeErr != nil || size > repository.MaxIntegrationEndpointEntryBytes {
			return nil, status.Error(codes.FailedPrecondition, "integration egress entry is too large; shorten the connection name or disable integration tools")
		}
	}
	if !providerEgress && len(resolved.RemoteMCPServers) == 0 && len(resolved.IntegrationEndpoints) == 0 {
		return nil, nil
	}
	// Everything the enqueue will freeze is now known, so the route is rebuilt
	// on it and validated again — and this is the one that decides whether a
	// challenge is issued at all.
	//
	// Two things are added that the first pass could not know. The decision:
	// this run will carry one, of whatever shape, and the worker that executes
	// it has to be able to validate one. A local model calling a remote MCP
	// server or an integration carries exactly the decision a remote model
	// does — the user's tool arguments and results leave the machine either
	// way — so gating on the model's destination let a pre-decision worker look
	// like a home for this run when the dispatch it is queued for will never
	// hand it one. And the tools: the snapshot below is the slice that goes
	// into the signed challenge, so it is the slice validated here rather than
	// a second reading of the registry that could have moved in between.
	//
	// A challenge that goes out is a promise that the run it describes can
	// happen. Where no connected worker can execute it the caller gets the
	// routing refusal — which names what is missing — instead of a consent
	// dialog for a run that would sit in the queue forever.
	route.SelectedTools = resolved.SelectedTools
	route.RemoteEgressDecision = true
	if s.runtime != nil {
		if err := s.runtime.ValidateRouting(ctx, route); err != nil {
			return nil, err
		}
	}
	if providerEgress {
		endpoint, parseErr := backendegress.ParseKeyedEndpoint(rawEndpoint)
		if parseErr != nil {
			message := "configured OpenAI endpoint is insecure"
			if routed {
				message = "configured external agent endpoint is insecure"
			}
			return nil, status.Error(codes.FailedPrecondition, message)
		}
		resolved.Endpoint = endpoint.Canonical
		resolved.EndpointHost = endpoint.Host
	}
	resolved.SkillSnapshotFingerprint, resolved.SkillInfo, err = s.repo.EgressSkillSnapshotFingerprint(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "resolve remote egress skill context failed")
	}
	if providerEgress && len(resolved.SkillInfo) > maxEgressSkills {
		return nil, status.Error(codes.FailedPrecondition, "too many enabled skills for remote egress disclosure; disable skills and try again")
	}
	// One read of the vault serves the fingerprint, the applicability decision
	// and the disclosure. Reading it again for any of the three would let a run
	// disclose one persona and bind another.
	resolved.MemorySnapshotFingerprint, resolved.MemorySnapshot, err =
		s.repo.EgressMemorySnapshotFingerprint(ctx, resolved.SelectedTools)
	if err != nil {
		return nil, status.Error(codes.Internal, "resolve remote egress memory context failed")
	}
	// Memory is applicable when something of the user's own would actually
	// travel: pinned words that survive a trim, or a memory tool whose
	// arguments and results are their notes even when nothing is pinned.
	//
	// External agent runs are included, and that is a deliberate divergence
	// from RecallApplicable. Recall is withheld there because the transcript
	// belongs to a conversation the user pointed elsewhere; the persona is not,
	// because it is how they asked to be spoken to, and they asked it of this
	// conversation.
	resolved.MemoryProfileApplicable = providerEgress &&
		(resolved.MemorySnapshot.Preimage(resolved.SelectedTools).HasPinnedContent() ||
			backendegress.SelectedToolsIncludeMemory(resolved.SelectedTools))
	if providerEgress {
		resolved.DataCategories = []turingv1.EgressDataCategory{
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CURRENT_MESSAGE,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY,
		}
		if len(resolved.SkillInfo) > 0 {
			resolved.DataCategories = append(resolved.DataCategories,
				turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_SKILL_CONTENT)
		}
	}
	if resolved.RecallApplicable {
		resolved.DataCategories = append(resolved.DataCategories,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL)
	}
	if resolved.MemoryProfileApplicable {
		resolved.DataCategories = append(resolved.DataCategories,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_MEMORY_PROFILE)
	}
	if providerEgress && len(resolved.SelectedTools) > 0 {
		resolved.DataCategories = append(resolved.DataCategories,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_SCHEMAS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS)
	}
	if len(resolved.RemoteMCPServers) > 0 {
		resolved.DataCategories = append(resolved.DataCategories,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS)
	}
	if len(resolved.IntegrationEndpoints) > 0 {
		resolved.DataCategories = append(resolved.DataCategories,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS)
	}
	slices.Sort(resolved.DataCategories)
	resolved.DataCategories = slices.Compact(resolved.DataCategories)
	return resolved, nil
}

func egressRequestDigest(input repository.EnqueueUserMessageInput) (string, error) {
	canonical, err := json.Marshal(struct {
		Version                        int      `json:"version"`
		SessionID                      string   `json:"session_id"`
		Content                        string   `json:"content"`
		ContentType                    string   `json:"content_type"`
		AgentID                        string   `json:"agent_id"`
		ModelProvider                  string   `json:"model_provider"`
		RequestedModel                 string   `json:"requested_model"`
		IdempotencyKey                 string   `json:"idempotency_key"`
		RequestedTools                 []string `json:"requested_tools"`
		RequiredContextTokens          int      `json:"required_context_tokens"`
		MinimumWorkerMaxConcurrentRuns int      `json:"minimum_worker_max_concurrent_runs"`
	}{
		Version: 1, SessionID: input.SessionID, Content: input.Content,
		ContentType: input.ContentType, AgentID: input.AgentID,
		ModelProvider: input.ModelProvider, RequestedModel: input.RequestedModel,
		IdempotencyKey:                 input.IdempotencyKey,
		RequestedTools:                 append([]string(nil), input.RequestedTools...),
		RequiredContextTokens:          input.RequiredContextTokens,
		MinimumWorkerMaxConcurrentRuns: input.MinimumWorkerMaxConcurrentRuns,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Server) signEgressChallenge(payload egressChallengePayload) (string, error) {
	if !validChallengePayload(payload) {
		return "", errors.New("egress challenge payload is invalid")
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if len(canonical) > maxEgressChallengeBytes {
		return "", errors.New("egress challenge payload is too large")
	}
	signature := hmac.New(sha256.New, s.egressSigningKey())
	_, _ = signature.Write(canonical)
	return base64.RawURLEncoding.EncodeToString(canonical) + "." +
		base64.RawURLEncoding.EncodeToString(signature.Sum(nil)), nil
}

func (s *Server) parseEgressChallenge(token string) (egressChallengePayload, []byte, error) {
	if token == "" || len(token) > maxEgressChallengeBytes*2 {
		return egressChallengePayload{}, nil, errors.New("invalid challenge length")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return egressChallengePayload{}, nil, errors.New("invalid challenge shape")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(payloadBytes) > maxEgressChallengeBytes ||
		base64.RawURLEncoding.EncodeToString(payloadBytes) != parts[0] {
		return egressChallengePayload{}, nil, errors.New("invalid challenge payload")
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(providedSignature) != parts[1] {
		return egressChallengePayload{}, nil, errors.New("invalid challenge signature")
	}
	expectedSignature := hmac.New(sha256.New, s.egressSigningKey())
	_, _ = expectedSignature.Write(payloadBytes)
	if !hmac.Equal(providedSignature, expectedSignature.Sum(nil)) {
		return egressChallengePayload{}, nil, errors.New("invalid challenge signature")
	}
	decoder := json.NewDecoder(bytes.NewReader(payloadBytes))
	decoder.DisallowUnknownFields()
	var payload egressChallengePayload
	if err := decoder.Decode(&payload); err != nil {
		return egressChallengePayload{}, nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return egressChallengePayload{}, nil, err
	}
	canonical, err := json.Marshal(payload)
	if err != nil || !bytes.Equal(canonical, payloadBytes) || !validChallengePayload(payload) {
		return egressChallengePayload{}, nil, errors.New("challenge payload is not canonical")
	}
	return payload, payloadBytes, nil
}

func (s *Server) egressSigningKey() []byte {
	deriver := hmac.New(sha256.New, []byte(s.egress.SigningSecret))
	_, _ = deriver.Write([]byte(egressChallengeDomain))
	return deriver.Sum(nil)
}

func validChallengePayload(payload egressChallengePayload) bool {
	if payload.Version != egressChallengeVersion ||
		payload.Nonce == "" || len(payload.Nonce) > maxEgressIDBytes ||
		payload.SessionID == "" || len(payload.SessionID) > maxEgressIDBytes ||
		len(payload.IdempotencyKey) > maxEgressIDBytes ||
		payload.RequestDigest == "" ||
		payload.MemorySnapshotFingerprint == "" ||
		(payload.Provider != "openai_compatible" && payload.Provider != "ollama") ||
		payload.Model == "" || len(payload.Model) > maxEgressModelBytes ||
		(payload.ExternalAgentID != "" && payload.ExternalCredentialRefHash == "") ||
		(payload.ExternalAgentID == "" && payload.ExternalCredentialRefHash != "") ||
		payload.ExpiresAtUnixNano <= payload.IssuedAtUnixNano ||
		len(payload.SelectedTools) > maxEgressTools ||
		!slices.IsSorted(payload.SelectedTools) ||
		!slices.IsSorted(payload.DataCategories) {
		return false
	}
	if payload.Provider == "openai_compatible" {
		if payload.Endpoint == "" || payload.EndpointHost == "" {
			return false
		}
	} else if payload.Endpoint != "" || payload.EndpointHost != "" ||
		payload.ExternalAgentID != "" || payload.ExternalCredentialRefHash != "" ||
		len(payload.RemoteMCPServers) == 0 && len(payload.IntegrationEndpoints) == 0 {
		return false
	}
	for index, category := range payload.DataCategories {
		if category <= int32(turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_UNSPECIFIED) ||
			category > int32(turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_ATTACHMENTS) ||
			(index > 0 && payload.DataCategories[index-1] == category) {
			return false
		}
	}
	selectedToolBytes := 0
	for index, tool := range payload.SelectedTools {
		if tool == "" || len(tool) > maxEgressToolNameBytes ||
			(index > 0 && payload.SelectedTools[index-1] == tool) {
			return false
		}
		selectedToolBytes += len(tool)
	}
	if selectedToolBytes > maxEgressSelectedToolBytes {
		return false
	}
	if payload.Endpoint != "" {
		endpoint, err := backendegress.ParseKeyedEndpoint(payload.Endpoint)
		if err != nil || endpoint.Canonical != payload.Endpoint || endpoint.Host != payload.EndpointHost {
			return false
		}
	}
	for index, destination := range payload.RemoteMCPServers {
		if destination.ServerName == "" ||
			(index > 0 && payload.RemoteMCPServers[index-1].ServerName >= destination.ServerName) {
			return false
		}
		endpoint, err := backendegress.ParseKeyedEndpoint(destination.Endpoint)
		if err != nil || endpoint.Canonical != destination.Endpoint || endpoint.Host != destination.EndpointHost {
			return false
		}
	}
	if len(payload.IntegrationEndpoints) > repository.MaxIntegrationEndpoints {
		return false
	}
	for index, destination := range payload.IntegrationEndpoints {
		if destination.Endpoint != repository.GitHubIntegrationEndpoint ||
			destination.EndpointHost != repository.GitHubIntegrationEndpointHost ||
			destination.ConnectionID == "" || destination.DisplayName == "" || len(destination.Tools) == 0 ||
			!slices.IsSorted(destination.Tools) {
			return false
		}
		if index > 0 {
			previous := payload.IntegrationEndpoints[index-1]
			if previous.Endpoint > destination.Endpoint ||
				(previous.Endpoint == destination.Endpoint && previous.ConnectionID >= destination.ConnectionID) {
				return false
			}
		}
		for toolIndex, tool := range destination.Tools {
			if tool == "" || (toolIndex > 0 && destination.Tools[toolIndex-1] == tool) {
				return false
			}
		}
		size, err := repository.IntegrationEndpointEntrySize(destination)
		if err != nil || size > repository.MaxIntegrationEndpointEntryBytes {
			return false
		}
	}
	return true
}

func payloadMatchesEgressContext(payload egressChallengePayload, resolved egressContext) bool {
	return payload.Provider == resolved.Provider &&
		payload.Model == resolved.Model &&
		payload.ExternalAgentID == resolved.ExternalAgentID &&
		payload.ExternalCredentialRefHash == resolved.ExternalCredentialRefHash &&
		payload.Endpoint == resolved.Endpoint &&
		payload.EndpointHost == resolved.EndpointHost &&
		payload.SkillSnapshotFingerprint == resolved.SkillSnapshotFingerprint &&
		payload.RecallApplicable == resolved.RecallApplicable &&
		payload.MemoryProfileApplicable == resolved.MemoryProfileApplicable &&
		payload.MemorySnapshotFingerprint == resolved.MemorySnapshotFingerprint &&
		slices.Equal(payload.SelectedTools, resolved.SelectedTools) &&
		slices.Equal(payload.RemoteMCPServers, toChallengeRemoteMCPServers(resolved.RemoteMCPServers)) &&
		slices.EqualFunc(payload.IntegrationEndpoints, resolved.IntegrationEndpoints, func(left, right repository.IntegrationEndpointEgress) bool {
			return left.Endpoint == right.Endpoint && left.EndpointHost == right.EndpointHost &&
				left.ConnectionID == right.ConnectionID && left.DisplayName == right.DisplayName && slices.Equal(left.Tools, right.Tools)
		}) &&
		slices.Equal(payload.DataCategories, categoryNumbers(resolved.DataCategories))
}

func cloneChallengeIntegrationEndpoints(destinations []repository.IntegrationEndpointEgress) []repository.IntegrationEndpointEgress {
	result := make([]repository.IntegrationEndpointEgress, len(destinations))
	for index := range destinations {
		result[index] = destinations[index]
		result[index].Tools = append([]string{}, destinations[index].Tools...)
	}
	return result
}

func categoryNumbers(categories []turingv1.EgressDataCategory) []int32 {
	values := make([]int32, len(categories))
	for index, category := range categories {
		values[index] = int32(category)
	}
	return values
}

func storageCategoryNames(categories []int32) []string {
	values := make([]string, len(categories))
	for index, category := range categories {
		values[index] = turingv1.EgressDataCategory(category).String()
	}
	return values
}

func providerToProto(provider string) turingv1.ModelProvider {
	if provider == "openai_compatible" {
		return turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE
	}
	if provider == "ollama" {
		return turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA
	}
	return turingv1.ModelProvider_MODEL_PROVIDER_UNSPECIFIED
}

func toChallengeRemoteMCPServers(destinations []repository.RemoteMCPServerEgress) []remoteMCPChallengeDestination {
	result := make([]remoteMCPChallengeDestination, len(destinations))
	for index, destination := range destinations {
		result[index] = remoteMCPChallengeDestination{
			ServerName:   destination.ServerName,
			Endpoint:     destination.Endpoint,
			EndpointHost: destination.EndpointHost,
		}
	}
	return result
}

func fromChallengeRemoteMCPServers(destinations []remoteMCPChallengeDestination) []repository.RemoteMCPServerEgress {
	result := make([]repository.RemoteMCPServerEgress, len(destinations))
	for index, destination := range destinations {
		result[index] = repository.RemoteMCPServerEgress{
			ServerName:   destination.ServerName,
			Endpoint:     destination.Endpoint,
			EndpointHost: destination.EndpointHost,
		}
	}
	return result
}

func toProtoRemoteMCPServers(destinations []repository.RemoteMCPServerEgress) []*turingv1.RemoteMcpEgressDestination {
	result := make([]*turingv1.RemoteMcpEgressDestination, len(destinations))
	for index, destination := range destinations {
		result[index] = &turingv1.RemoteMcpEgressDestination{
			ServerName:   destination.ServerName,
			Endpoint:     destination.Endpoint,
			EndpointHost: destination.EndpointHost,
		}
	}
	return result
}

func toProtoSkillEgressDisclosures(skills []repository.SkillEgressInfo) []*turingv1.SkillEgressDisclosure {
	result := make([]*turingv1.SkillEgressDisclosure, len(skills))
	for index, skill := range skills {
		result[index] = &turingv1.SkillEgressDisclosure{
			SkillId: skill.SkillID, DisplayName: skill.DisplayName,
			BodyMayBeSent: skill.BodyMayBeSent,
		}
	}
	return result
}

func toProtoIntegrationEndpoints(destinations []repository.IntegrationEndpointEgress) []*turingv1.IntegrationEgressDestination {
	result := make([]*turingv1.IntegrationEgressDestination, len(destinations))
	for index, destination := range destinations {
		result[index] = &turingv1.IntegrationEgressDestination{
			Endpoint: destination.Endpoint, EndpointHost: destination.EndpointHost,
			ConnectionId: destination.ConnectionID, DisplayName: destination.DisplayName,
			Tools: append([]string(nil), destination.Tools...),
		}
	}
	return result
}

func newEgressNonce() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}
