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
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	egressChallengeVersion     = 1
	egressChallengeDomain      = "turing.remote-egress.challenge.v1"
	defaultEgressChallengeTTL  = 5 * time.Minute
	maxEgressChallengeBytes    = 32 * 1024
	maxEgressContentBytes      = 1024 * 1024
	maxEgressIDBytes           = 512
	maxEgressModelBytes        = 512
	maxEgressTools             = 256
	maxEgressToolNameBytes     = 512
	maxEgressSelectedToolBytes = 16 * 1024
)

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
	RecallApplicable          bool
	MemoryProfileApplicable   bool
}

type egressChallengePayload struct {
	Version                   int      `json:"version"`
	Nonce                     string   `json:"nonce"`
	IssuedAtUnixNano          int64    `json:"issued_at_unix_nano"`
	ExpiresAtUnixNano         int64    `json:"expires_at_unix_nano"`
	SessionID                 string   `json:"session_id"`
	IdempotencyKey            string   `json:"idempotency_key"`
	RequestDigest             string   `json:"request_digest"`
	Provider                  string   `json:"provider"`
	Model                     string   `json:"model"`
	ExternalAgentID           string   `json:"external_agent_id,omitempty"`
	ExternalCredentialRefHash string   `json:"external_credential_ref_hash,omitempty"`
	Endpoint                  string   `json:"endpoint"`
	EndpointHost              string   `json:"endpoint_host"`
	DataCategories            []int32  `json:"data_categories"`
	SelectedTools             []string `json:"selected_tools"`
	SkillSnapshotFingerprint  string   `json:"skill_snapshot_fingerprint"`
	RecallApplicable          bool     `json:"recall_applicable"`
	MemoryProfileApplicable   bool     `json:"memory_profile_applicable"`
}

func (s *Server) PrepareRemoteEgress(ctx context.Context, req *turingv1.PrepareRemoteEgressRequest) (*turingv1.PrepareRemoteEgressResponse, error) {
	input, err := s.prepareEgressInput(req)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.GetSession(ctx, input.SessionID); err != nil {
		return nil, mapSessionError(ctx, err)
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
	}
	challenge, err := s.signEgressChallenge(payload)
	if err != nil {
		return nil, status.Error(codes.Internal, "create remote egress disclosure failed")
	}
	return &turingv1.PrepareRemoteEgressResponse{
		Disclosure: &turingv1.RemoteEgressDisclosure{
			Challenge:       challenge,
			Provider:        providerToProto(resolved.Provider),
			Model:           resolved.Model,
			Endpoint:        resolved.Endpoint,
			EndpointHost:    resolved.EndpointHost,
			ExternalAgentId: resolved.ExternalAgentID,
			DataCategories:  append([]turingv1.EgressDataCategory(nil), resolved.DataCategories...),
			ExpiresAt:       timestamppb.New(time.Unix(0, payload.ExpiresAtUnixNano).UTC()),
		},
	}, nil
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
	agent, routed, err := s.repo.GetSessionAgent(ctx, input.SessionID)
	if err != nil {
		return nil, status.Error(codes.Internal, "resolve remote egress destination failed")
	}
	if routed {
		resolved.Provider = "openai_compatible"
		resolved.Model = agent.Model
		resolved.ExternalAgentID = agent.AgentID
		resolved.ExternalCredentialRefHash = backendegress.HashCredentialReference(agent.CredentialRef)
		rawEndpoint = agent.BaseURL
		externalCredentialRef = agent.CredentialRef
	} else if input.ModelProvider == "openai_compatible" {
		resolved.Provider = input.ModelProvider
		resolved.Model = input.Model
		if input.ExecutionModel != "" {
			resolved.Model = input.ExecutionModel
		}
		rawEndpoint = s.egress.OpenAIBaseURL
		resolved.RecallApplicable = true
	} else {
		return nil, nil
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
	if s.runtime != nil {
		if err := s.runtime.ValidateRouting(ctx, route); err != nil {
			return nil, err
		}
	}
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
	resolved.SkillSnapshotFingerprint, err = s.repo.EgressSkillSnapshotFingerprint(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "resolve remote egress skill context failed")
	}
	resolved.DataCategories = []turingv1.EgressDataCategory{
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CURRENT_MESSAGE,
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY,
	}
	if resolved.RecallApplicable {
		resolved.DataCategories = append(resolved.DataCategories,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL)
	}
	resolved.DataCategories = append(resolved.DataCategories,
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_SKILL_CONTENT)
	if len(resolved.SelectedTools) > 0 {
		resolved.DataCategories = append(resolved.DataCategories,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_SCHEMAS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS)
	}
	slices.Sort(resolved.DataCategories)
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
		payload.Provider != "openai_compatible" ||
		payload.Model == "" || len(payload.Model) > maxEgressModelBytes ||
		payload.Endpoint == "" || payload.EndpointHost == "" ||
		(payload.ExternalAgentID != "" && payload.ExternalCredentialRefHash == "") ||
		(payload.ExternalAgentID == "" && payload.ExternalCredentialRefHash != "") ||
		payload.ExpiresAtUnixNano <= payload.IssuedAtUnixNano ||
		len(payload.SelectedTools) > maxEgressTools ||
		!slices.IsSorted(payload.SelectedTools) ||
		!slices.IsSorted(payload.DataCategories) {
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
	endpoint, err := backendegress.ParseKeyedEndpoint(payload.Endpoint)
	return err == nil && endpoint.Canonical == payload.Endpoint && endpoint.Host == payload.EndpointHost
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
		slices.Equal(payload.SelectedTools, resolved.SelectedTools) &&
		slices.Equal(payload.DataCategories, categoryNumbers(resolved.DataCategories))
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
	return turingv1.ModelProvider_MODEL_PROVIDER_UNSPECIFIED
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
