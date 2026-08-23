package runtime

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type LegacyCapabilityProfile struct {
	Models                      []*turingv1.ModelCapability
	AgentIds                    []turingv1.AgentId
	Tools                       []*turingv1.DiscoveredTool
	ExternalAgentCredentialRefs []string
	SupportsExternalAgents      bool
}

type registeredModelCapability struct {
	provider         string
	model            string
	maxContextTokens int
}

type registeredWorkerCapabilities struct {
	models                      []registeredModelCapability
	agentIDs                    map[string]struct{}
	tools                       map[string]struct{}
	externalAgentCredentialRefs map[string]struct{}
	maxConcurrentRuns           int
	remoteEgressDecisionVersion int
}

func cloneRegisteredWorkerCapabilities(input *registeredWorkerCapabilities) *registeredWorkerCapabilities {
	if input == nil {
		return nil
	}
	cloned := &registeredWorkerCapabilities{
		models:                      append([]registeredModelCapability(nil), input.models...),
		agentIDs:                    make(map[string]struct{}, len(input.agentIDs)),
		tools:                       make(map[string]struct{}, len(input.tools)),
		externalAgentCredentialRefs: make(map[string]struct{}, len(input.externalAgentCredentialRefs)),
		maxConcurrentRuns:           input.maxConcurrentRuns,
		remoteEgressDecisionVersion: input.remoteEgressDecisionVersion,
	}
	for value := range input.agentIDs {
		cloned.agentIDs[value] = struct{}{}
	}
	for value := range input.tools {
		cloned.tools[value] = struct{}{}
	}
	for value := range input.externalAgentCredentialRefs {
		cloned.externalAgentCredentialRefs[value] = struct{}{}
	}
	return cloned
}

func decodeWorkerCapabilities(snapshot *turingv1.WorkerCapabilities) (*registeredWorkerCapabilities, []repository.DiscoveredTool, error) {
	if snapshot == nil {
		return nil, nil, fmt.Errorf("capabilities are required")
	}
	if snapshot.GetMaxConcurrentRuns() < 1 || snapshot.GetMaxConcurrentRuns() > maxWorkerConcurrentRuns {
		return nil, nil, fmt.Errorf("max_concurrent_runs must be between 1 and %d", maxWorkerConcurrentRuns)
	}
	if len(snapshot.GetAgentIds()) == 0 {
		return nil, nil, fmt.Errorf("at least one agent_id is required")
	}
	agentIDs := make(map[string]struct{}, len(snapshot.GetAgentIds()))
	for _, agentID := range snapshot.GetAgentIds() {
		name := agentIDName(agentID)
		if name == "" {
			return nil, nil, fmt.Errorf("agent_id %d is unsupported", agentID)
		}
		agentIDs[name] = struct{}{}
	}
	if len(snapshot.GetModels()) == 0 && len(snapshot.GetExternalAgentCredentialRefs()) == 0 {
		return nil, nil, fmt.Errorf("at least one model or external-agent credential ref is required")
	}
	models := make([]registeredModelCapability, 0, len(snapshot.GetModels()))
	seenModels := make(map[string]struct{}, len(snapshot.GetModels()))
	for _, advertised := range snapshot.GetModels() {
		if advertised == nil {
			return nil, nil, fmt.Errorf("model capability is required")
		}
		provider := modelProviderName(advertised.GetProvider())
		model := strings.TrimSpace(advertised.GetModel())
		if provider == "" {
			return nil, nil, fmt.Errorf("model provider %d is unsupported", advertised.GetProvider())
		}
		if model == "" {
			return nil, nil, fmt.Errorf("model name is required")
		}
		if advertised.GetMaxContextTokens() < 0 {
			return nil, nil, fmt.Errorf("max_context_tokens must be non-negative")
		}
		key := provider + "\x00" + model
		if _, duplicate := seenModels[key]; duplicate {
			return nil, nil, fmt.Errorf("model capability %s/%s is duplicated", provider, model)
		}
		seenModels[key] = struct{}{}
		models = append(models, registeredModelCapability{
			provider:         provider,
			model:            model,
			maxContextTokens: int(advertised.GetMaxContextTokens()),
		})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].provider == models[j].provider {
			return models[i].model < models[j].model
		}
		return models[i].provider < models[j].provider
	})
	discovered, err := decodeDiscoveredTools(snapshot.GetTools())
	if err != nil {
		return nil, nil, err
	}
	tools := make(map[string]struct{}, len(discovered))
	for _, tool := range discovered {
		tools[tool.ServerName+"/"+tool.ToolName] = struct{}{}
	}
	credentialRefs := make(map[string]struct{}, len(snapshot.GetExternalAgentCredentialRefs()))
	for _, advertised := range snapshot.GetExternalAgentCredentialRefs() {
		credentialRef := strings.TrimSpace(advertised)
		if credentialRef == "" {
			return nil, nil, fmt.Errorf("external-agent credential ref is required")
		}
		if credentialRef != advertised {
			return nil, nil, fmt.Errorf("external-agent credential ref %q is not normalized", advertised)
		}
		if _, duplicate := credentialRefs[credentialRef]; duplicate {
			return nil, nil, fmt.Errorf("external-agent credential ref %q is duplicated", credentialRef)
		}
		credentialRefs[credentialRef] = struct{}{}
	}
	return &registeredWorkerCapabilities{
		models:                      models,
		agentIDs:                    agentIDs,
		tools:                       tools,
		externalAgentCredentialRefs: credentialRefs,
		maxConcurrentRuns:           int(snapshot.GetMaxConcurrentRuns()),
		remoteEgressDecisionVersion: int(snapshot.GetRemoteEgressDecisionVersion()),
	}, discovered, nil
}

func decodeLegacyWorkerCapabilities(profile *LegacyCapabilityProfile, ready *turingv1.RuntimeWorkerReady) (*registeredWorkerCapabilities, []repository.DiscoveredTool, error) {
	if profile == nil {
		return nil, nil, fmt.Errorf("legacy capability profile is not configured")
	}
	if ready == nil {
		return nil, nil, fmt.Errorf("worker_ready is required")
	}
	if agentIDName(ready.GetAgentId()) == "" {
		return nil, nil, fmt.Errorf("worker ready agent_id %d is unsupported", ready.GetAgentId())
	}
	agentSupported := false
	for _, agentID := range profile.AgentIds {
		if agentID == ready.GetAgentId() {
			agentSupported = true
			break
		}
	}
	if !agentSupported {
		return nil, nil, fmt.Errorf("worker ready agent_id %d is not in the legacy capability profile", ready.GetAgentId())
	}
	var discovered []repository.DiscoveredTool
	switch ready.GetToolDiscoveryStatus() {
	case turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_UNSPECIFIED:
		if len(ready.GetTools()) > 0 {
			var err error
			discovered, err = decodeDiscoveredTools(ready.GetTools())
			if err != nil {
				return nil, nil, err
			}
		} else if len(profile.Tools) > 0 {
			var err error
			discovered, err = decodeDiscoveredTools(profile.Tools)
			if err != nil {
				return nil, nil, err
			}
		}
	case turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE:
		var err error
		discovered, err = decodeDiscoveredTools(ready.GetTools())
		if err != nil {
			return nil, nil, err
		}
	case turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_FAILED:
		return nil, nil, fmt.Errorf("worker tool discovery failed")
	default:
		return nil, nil, fmt.Errorf("worker tool discovery status is invalid")
	}
	maxConcurrentRuns := int(ready.GetMaxConcurrentRuns())
	if maxConcurrentRuns <= 0 {
		maxConcurrentRuns = defaultMaxConcurrentRuns
	}
	if maxConcurrentRuns > maxWorkerConcurrentRuns {
		maxConcurrentRuns = maxWorkerConcurrentRuns
	}
	tools := make([]*turingv1.DiscoveredTool, 0, len(discovered))
	for _, tool := range discovered {
		tools = append(tools, &turingv1.DiscoveredTool{
			ServerName: tool.ServerName,
			ToolName:   tool.ToolName,
			Schema:     &structpb.Struct{},
		})
	}
	capabilities, _, err := decodeWorkerCapabilities(&turingv1.WorkerCapabilities{
		Models:                      profile.Models,
		AgentIds:                    profile.AgentIds,
		Tools:                       tools,
		MaxConcurrentRuns:           int32(maxConcurrentRuns),
		SupportsExternalAgents:      profile.SupportsExternalAgents || len(profile.ExternalAgentCredentialRefs) > 0,
		ExternalAgentCredentialRefs: profile.ExternalAgentCredentialRefs,
	})
	if err != nil {
		return nil, nil, err
	}
	return capabilities, discovered, nil
}

func agentIDName(agentID turingv1.AgentId) string {
	switch agentID {
	case turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT:
		return "general_assistant"
	default:
		return ""
	}
}

func modelProviderName(provider turingv1.ModelProvider) string {
	switch provider {
	case turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA:
		return "ollama"
	case turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE:
		return "openai_compatible"
	default:
		return ""
	}
}

type routingCandidate struct {
	capabilities *registeredWorkerCapabilities
}

type unavailableRoutingState struct {
	fingerprint   string
	lossPublished bool
}

const (
	pendingRoutingPageSize       = 128
	pendingRoutingRefreshTimeout = 5 * time.Second
)

func workerCapabilitiesSupportRoute(capabilities *registeredWorkerCapabilities, route repository.RoutingRequirements) bool {
	if capabilities == nil {
		return false
	}
	if (route.ModelProvider == "openai_compatible" || route.ExternalAgent) &&
		capabilities.remoteEgressDecisionVersion < repository.RunEgressDecisionVersion {
		return false
	}
	if _, ok := capabilities.agentIDs[route.AgentID]; !ok {
		return false
	}
	if route.ExternalAgent {
		if _, ok := capabilities.externalAgentCredentialRefs[route.ExternalAgentCredentialRef]; !ok ||
			route.RequiredContextTokens > 0 {
			return false
		}
	} else {
		modelSupported := false
		for _, model := range capabilities.models {
			if model.provider == route.ModelProvider &&
				model.model == route.Model &&
				(route.RequiredContextTokens <= 0 || model.maxContextTokens >= route.RequiredContextTokens) {
				modelSupported = true
				break
			}
		}
		if !modelSupported {
			return false
		}
	}
	for _, requestedTool := range route.RequestedTools {
		if _, ok := capabilities.tools[requestedTool]; !ok {
			return false
		}
	}
	for _, selectedTool := range route.SelectedTools {
		if _, ok := capabilities.tools[selectedTool]; !ok {
			return false
		}
	}
	minimumCapacity := route.MinimumWorkerMaxConcurrentRuns
	if minimumCapacity <= 0 {
		minimumCapacity = 1
	}
	return capabilities.maxConcurrentRuns >= minimumCapacity
}

func (s *Server) replaceWorkerCapabilities(
	ctx context.Context,
	workerID string,
	connectedWorker *worker,
	update *turingv1.RuntimeWorkerCapabilitiesUpdated,
) error {
	s.registryMu.RLock()
	defer s.registryMu.RUnlock()
	if update.GetWorkerId() == "" || update.GetRegistrationId() == "" || update.GetCapabilities() == nil {
		return status.Error(codes.InvalidArgument, "worker_id, registration_id, and capabilities are required")
	}
	if update.GetWorkerId() != workerID || update.GetRegistrationId() != connectedWorker.registrationID {
		return status.Error(codes.PermissionDenied, "capability update does not match the connected registration")
	}
	capabilities, discovered, err := decodeWorkerCapabilities(update.GetCapabilities())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "worker capabilities are invalid: %v", err)
	}
	capabilities, discovered, err = s.filterRegisteredWorkerTools(ctx, capabilities, discovered)
	if err != nil {
		return status.Error(codes.Internal, "filter worker tool capabilities")
	}

	s.mu.Lock()
	ownsRegistration := s.workers[workerID] == connectedWorker
	s.mu.Unlock()
	if !ownsRegistration {
		return status.Error(codes.PermissionDenied, "capability update registration is stale")
	}
	connectedWorker.mu.Lock()
	if connectedWorker.closed {
		connectedWorker.mu.Unlock()
		return status.Error(codes.Canceled, "worker is disconnected")
	}
	connectedWorker.mu.Unlock()

	if err := s.persistDiscoveredTools(ctx, workerID, connectedWorker, discovered); err != nil {
		return status.Error(codes.Internal, "persist worker tool capabilities")
	}
	connectedWorker.mu.Lock()
	if connectedWorker.closed {
		connectedWorker.mu.Unlock()
		return status.Error(codes.Canceled, "worker is disconnected")
	}
	connectedWorker.capabilities = capabilities
	connectedWorker.maxConcurrent = capabilities.maxConcurrentRuns
	connectedWorker.mu.Unlock()
	s.refreshPendingCapabilityStateAdvisory(ctx, "worker capabilities changed", workerID, true, true)
	return s.DispatchPending(ctx)
}

func (s *Server) ProviderCapabilities() map[turingv1.ModelProvider][]*turingv1.ModelCapability {
	type modelKey struct {
		provider turingv1.ModelProvider
		model    string
	}
	advertised := make(map[modelKey]int32)
	for _, candidate := range s.liveRoutingCandidates(time.Now().UTC()) {
		for _, model := range candidate.capabilities.models {
			provider := modelProviderProto(model.provider)
			if provider == turingv1.ModelProvider_MODEL_PROVIDER_UNSPECIFIED {
				continue
			}
			if provider == turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE &&
				candidate.capabilities.remoteEgressDecisionVersion < repository.RunEgressDecisionVersion {
				continue
			}
			key := modelKey{provider: provider, model: model.model}
			current, present := advertised[key]
			if limit := int32(model.maxContextTokens); !present || limit > current {
				advertised[key] = limit
			}
		}
	}
	keys := make([]modelKey, 0, len(advertised))
	for key := range advertised {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].provider == keys[j].provider {
			return keys[i].model < keys[j].model
		}
		return keys[i].provider < keys[j].provider
	})
	out := make(map[turingv1.ModelProvider][]*turingv1.ModelCapability)
	for _, key := range keys {
		out[key.provider] = append(out[key.provider], &turingv1.ModelCapability{
			Provider:         key.provider,
			Model:            key.model,
			MaxContextTokens: advertised[key],
		})
	}
	return out
}

func (s *Server) RoutableDefaultModel(provider string, configured string) string {
	advertised := s.ProviderCapabilities()[modelProviderProto(provider)]
	for _, model := range advertised {
		if model.GetModel() == configured {
			return configured
		}
	}
	if len(advertised) > 0 {
		return advertised[0].GetModel()
	}
	return ""
}

func (s *Server) LiveToolNames() []string {
	unique := map[string]struct{}{}
	for _, candidate := range s.liveRoutingCandidates(time.Now().UTC()) {
		for tool := range candidate.capabilities.tools {
			unique[tool] = struct{}{}
		}
	}
	tools := make([]string, 0, len(unique))
	for tool := range unique {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	return tools
}

func (s *Server) EgressToolNames(route repository.RoutingRequirements) []string {
	candidates := s.liveRoutingCandidates(time.Now().UTC())
	candidates = filterRoutingCandidates(candidates, func(capabilities *registeredWorkerCapabilities) bool {
		if _, ok := capabilities.agentIDs[route.AgentID]; !ok {
			return false
		}
		if (route.ModelProvider == "openai_compatible" || route.ExternalAgent) &&
			capabilities.remoteEgressDecisionVersion < repository.RunEgressDecisionVersion {
			return false
		}
		if route.ExternalAgent {
			if _, ok := capabilities.externalAgentCredentialRefs[route.ExternalAgentCredentialRef]; !ok {
				return false
			}
		} else {
			matched := false
			for _, model := range capabilities.models {
				if model.provider == route.ModelProvider && model.model == route.Model &&
					(route.RequiredContextTokens <= 0 || model.maxContextTokens >= route.RequiredContextTokens) {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
		for _, requested := range route.RequestedTools {
			if _, ok := capabilities.tools[requested]; !ok {
				return false
			}
		}
		minimumCapacity := route.MinimumWorkerMaxConcurrentRuns
		if minimumCapacity <= 0 {
			minimumCapacity = 1
		}
		return capabilities.maxConcurrentRuns >= minimumCapacity
	})
	if len(candidates) == 0 {
		return nil
	}
	common := make(map[string]struct{}, len(candidates[0].capabilities.tools))
	for tool := range candidates[0].capabilities.tools {
		common[tool] = struct{}{}
	}
	for _, candidate := range candidates[1:] {
		for tool := range common {
			if _, ok := candidate.capabilities.tools[tool]; !ok {
				delete(common, tool)
			}
		}
	}
	tools := make([]string, 0, len(common))
	for tool := range common {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	return tools
}

func (s *Server) AgentAvailable(agentID turingv1.AgentId) bool {
	name := agentIDName(agentID)
	if name == "" {
		return false
	}
	for _, candidate := range s.liveRoutingCandidates(time.Now().UTC()) {
		if _, ok := candidate.capabilities.agentIDs[name]; ok {
			return true
		}
	}
	return false
}

func modelProviderProto(provider string) turingv1.ModelProvider {
	switch provider {
	case "ollama":
		return turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA
	case "openai_compatible":
		return turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE
	default:
		return turingv1.ModelProvider_MODEL_PROVIDER_UNSPECIFIED
	}
}

func (s *Server) refreshPendingCapabilityState(
	ctx context.Context,
	cause string,
	workerID string,
	publishLosses bool,
	publishRestorations bool,
) error {
	ctx, cancel := context.WithTimeout(ctx, pendingRoutingRefreshTimeout)
	defer cancel()
	s.availabilityMu.Lock()
	defer s.availabilityMu.Unlock()

	nextUnavailable := make(map[string]unavailableRoutingState)
	previousPending := make(map[string]struct{}, len(s.unavailablePending))
	cursor := repository.PendingRoutingCursor{}
	for {
		work, nextCursor, err := s.repo.ListPendingRoutingWorkPage(ctx, cursor, pendingRoutingPageSize)
		if err != nil {
			return err
		}
		for _, item := range work {
			if _, tracked := s.unavailablePending[item.RunID]; tracked {
				previousPending[item.RunID] = struct{}{}
			}
			routingErr := s.ValidateRouting(ctx, item.Requirements)
			if routingErr == nil {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			detail := routingDetail(routingErr)
			if detail == nil {
				return routingErr
			}
			fingerprint := routingRequirementsFingerprint(item.Requirements)
			previous := s.unavailablePending[item.RunID]
			state := unavailableRoutingState{
				fingerprint:   fingerprint,
				lossPublished: previous.fingerprint == fingerprint && previous.lossPublished,
			}
			nextUnavailable[item.RunID] = state
			if !publishLosses || state.lossPublished {
				continue
			}
			label := routingRequirementLabel(detail.GetKind())
			requested := strings.Join(strings.Fields(detail.GetRequested()), " ")
			event, appended, err := s.repo.AppendPendingRunNotice(
				ctx,
				item.RunID,
				fmt.Sprintf("Waiting for a compatible worker: %s %q is unavailable", label, requested),
				map[string]any{
					"reason":                         "routing_capability_unavailable",
					"cause":                          cause,
					"workerId":                       workerID,
					"agentId":                        item.Requirements.AgentID,
					"provider":                       item.Requirements.ModelProvider,
					"model":                          item.Requirements.Model,
					"requiredTools":                  append([]string(nil), item.Requirements.RequestedTools...),
					"requiredContextTokens":          item.Requirements.RequiredContextTokens,
					"minimumWorkerMaxConcurrentRuns": item.Requirements.MinimumWorkerMaxConcurrentRuns,
					"externalAgent":                  item.Requirements.ExternalAgent,
					"unavailableCapability":          detail.GetKind().String(),
					"requested":                      detail.GetRequested(),
					"available":                      append([]string(nil), detail.GetAvailable()...),
				},
			)
			if err != nil {
				return err
			}
			if !appended {
				delete(nextUnavailable, item.RunID)
				delete(s.unavailablePending, item.RunID)
				continue
			}
			s.publishEvent(event)
			state.lossPublished = true
			nextUnavailable[item.RunID] = state
			s.unavailablePending[item.RunID] = state
		}
		if len(work) < pendingRoutingPageSize {
			break
		}
		cursor = nextCursor
	}
	if publishRestorations {
		restoredRunIDs := make([]string, 0)
		for runID := range s.unavailablePending {
			if _, stillUnavailable := nextUnavailable[runID]; stillUnavailable {
				continue
			}
			if _, stillPending := previousPending[runID]; stillPending {
				restoredRunIDs = append(restoredRunIDs, runID)
			}
		}
		sort.Strings(restoredRunIDs)
		for _, runID := range restoredRunIDs {
			event, appended, err := s.repo.AppendPendingRunNotice(
				ctx,
				runID,
				"Compatible worker available; retrying dispatch",
				map[string]any{
					"reason":   "routing_capability_restored",
					"cause":    cause,
					"workerId": workerID,
				},
			)
			if err != nil {
				return err
			}
			if appended {
				s.publishEvent(event)
			}
			delete(s.unavailablePending, runID)
		}
	} else {
		for runID, state := range s.unavailablePending {
			if !state.lossPublished {
				continue
			}
			if _, stillPending := previousPending[runID]; !stillPending {
				continue
			}
			if _, stillUnavailable := nextUnavailable[runID]; !stillUnavailable {
				nextUnavailable[runID] = state
			}
		}
	}
	s.unavailablePending = nextUnavailable
	return nil
}

func routingDetail(err error) *turingv1.RoutingUnavailableDetail {
	for _, detail := range status.Convert(err).Details() {
		if unavailable, ok := detail.(*turingv1.RoutingUnavailableDetail); ok {
			return unavailable
		}
	}
	return nil
}

func routingRequirementLabel(kind turingv1.RoutingRequirementKind) string {
	switch kind {
	case turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_PROVIDER:
		return "provider"
	case turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_MODEL:
		return "model"
	case turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_TOOL:
		return "tool"
	case turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_AGENT:
		return "agent"
	case turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_CONTEXT:
		return "context limit"
	case turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_CAPACITY:
		return "capacity"
	case turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_EXTERNAL_AGENT_CREDENTIAL:
		return "external-agent credential"
	default:
		return "routing capability"
	}
}

func routingRequirementsFingerprint(route repository.RoutingRequirements) string {
	return strings.Join([]string{
		route.AgentID,
		route.ModelProvider,
		route.Model,
		strings.Join(route.RequestedTools, "\x1f"),
		strings.Join(route.SelectedTools, "\x1f"),
		strconv.Itoa(route.RequiredContextTokens),
		strconv.Itoa(route.MinimumWorkerMaxConcurrentRuns),
		strconv.FormatBool(route.ExternalAgent),
		route.ExternalAgentCredentialRef,
	}, "\x00")
}

func (s *Server) liveRoutingCandidates(now time.Time) []routingCandidate {
	workers := s.snapshotWorkers()
	candidates := make([]routingCandidate, 0, len(workers))
	for _, entry := range workers {
		entry.worker.mu.Lock()
		live := !entry.worker.closed &&
			!entry.worker.lastHeartbeat.IsZero() &&
			now.Before(entry.worker.lastHeartbeat.Add(s.dispatch.LeaseDuration))
		capabilities := entry.worker.capabilities
		entry.worker.mu.Unlock()
		if live && capabilities != nil {
			candidates = append(candidates, routingCandidate{capabilities: capabilities})
		}
	}
	return candidates
}

func (s *Server) ValidateRouting(ctx context.Context, route repository.RoutingRequirements) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	candidates := s.liveRoutingCandidates(time.Now().UTC())
	candidates = filterRoutingCandidates(candidates, func(capabilities *registeredWorkerCapabilities) bool {
		_, ok := capabilities.agentIDs[route.AgentID]
		return ok
	})
	if len(candidates) == 0 {
		return routingUnavailable(
			turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_AGENT,
			route.AgentID,
			s.availableAgentIDs(),
		)
	}
	if route.ModelProvider == "openai_compatible" || route.ExternalAgent {
		candidates = filterRoutingCandidates(candidates, func(capabilities *registeredWorkerCapabilities) bool {
			return capabilities.remoteEgressDecisionVersion >= repository.RunEgressDecisionVersion
		})
		if len(candidates) == 0 {
			return routingUnavailable(
				turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_PROVIDER,
				fmt.Sprintf("remote egress decision v%d", repository.RunEgressDecisionVersion),
				nil,
			)
		}
	}
	if route.ExternalAgent {
		candidates = filterRoutingCandidates(candidates, func(capabilities *registeredWorkerCapabilities) bool {
			_, ok := capabilities.externalAgentCredentialRefs[route.ExternalAgentCredentialRef]
			return ok
		})
		if len(candidates) == 0 {
			return routingUnavailable(
				turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_EXTERNAL_AGENT_CREDENTIAL,
				route.ExternalAgentCredentialRef,
				nil,
			)
		}
		if route.RequiredContextTokens > 0 {
			return routingUnavailable(
				turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_CONTEXT,
				strconv.Itoa(route.RequiredContextTokens),
				nil,
			)
		}
	} else {
		candidates = filterRoutingCandidates(candidates, func(capabilities *registeredWorkerCapabilities) bool {
			for _, model := range capabilities.models {
				if model.provider == route.ModelProvider {
					return true
				}
			}
			return false
		})
		if len(candidates) == 0 {
			return routingUnavailable(
				turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_PROVIDER,
				route.ModelProvider,
				s.availableProviders(),
			)
		}
		candidates = filterRoutingCandidates(candidates, func(capabilities *registeredWorkerCapabilities) bool {
			for _, model := range capabilities.models {
				if model.provider == route.ModelProvider && model.model == route.Model {
					return true
				}
			}
			return false
		})
		if len(candidates) == 0 {
			return routingUnavailable(
				turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_MODEL,
				route.ModelProvider+"/"+route.Model,
				s.availableModels(route.ModelProvider),
			)
		}
		if route.RequiredContextTokens > 0 {
			candidates = filterRoutingCandidates(candidates, func(capabilities *registeredWorkerCapabilities) bool {
				for _, model := range capabilities.models {
					if model.provider == route.ModelProvider &&
						model.model == route.Model &&
						model.maxContextTokens >= route.RequiredContextTokens {
						return true
					}
				}
				return false
			})
			if len(candidates) == 0 {
				return routingUnavailable(
					turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_CONTEXT,
					strconv.Itoa(route.RequiredContextTokens),
					s.availableContextLimits(route.ModelProvider, route.Model),
				)
			}
		}
	}
	for _, requestedTool := range route.RequestedTools {
		candidates = filterRoutingCandidates(candidates, func(capabilities *registeredWorkerCapabilities) bool {
			_, ok := capabilities.tools[requestedTool]
			return ok
		})
		if len(candidates) == 0 {
			return routingUnavailable(
				turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_TOOL,
				requestedTool,
				s.availableTools(),
			)
		}
	}
	for _, selectedTool := range route.SelectedTools {
		candidates = filterRoutingCandidates(candidates, func(capabilities *registeredWorkerCapabilities) bool {
			_, ok := capabilities.tools[selectedTool]
			return ok
		})
		if len(candidates) == 0 {
			return routingUnavailable(
				turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_TOOL,
				selectedTool,
				s.availableTools(),
			)
		}
	}
	minimumCapacity := route.MinimumWorkerMaxConcurrentRuns
	if minimumCapacity <= 0 {
		minimumCapacity = 1
	}
	candidates = filterRoutingCandidates(candidates, func(capabilities *registeredWorkerCapabilities) bool {
		return capabilities.maxConcurrentRuns >= minimumCapacity
	})
	if len(candidates) == 0 {
		return routingUnavailable(
			turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_CAPACITY,
			strconv.Itoa(minimumCapacity),
			s.availableCapacities(),
		)
	}
	return nil
}

func filterRoutingCandidates(candidates []routingCandidate, matches func(*registeredWorkerCapabilities) bool) []routingCandidate {
	filtered := candidates[:0]
	for _, candidate := range candidates {
		if matches(candidate.capabilities) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func routingUnavailable(kind turingv1.RoutingRequirementKind, requested string, available []string) error {
	base := status.New(codes.FailedPrecondition, "no connected worker supports the requested route")
	detailed, err := base.WithDetails(&turingv1.RoutingUnavailableDetail{
		Kind:      kind,
		Requested: requested,
		Available: available,
	})
	if err != nil {
		return status.Errorf(codes.Internal, "encode routing unavailable detail: %v", err)
	}
	return detailed.Err()
}

func (s *Server) availableAgentIDs() []string {
	return collectAvailable(s, func(capabilities *registeredWorkerCapabilities) []string {
		values := make([]string, 0, len(capabilities.agentIDs))
		for agentID := range capabilities.agentIDs {
			values = append(values, agentID)
		}
		return values
	})
}

func (s *Server) availableProviders() []string {
	return collectAvailable(s, func(capabilities *registeredWorkerCapabilities) []string {
		values := make([]string, 0, len(capabilities.models))
		for _, model := range capabilities.models {
			values = append(values, model.provider)
		}
		return values
	})
}

func (s *Server) availableModels(provider string) []string {
	return collectAvailable(s, func(capabilities *registeredWorkerCapabilities) []string {
		values := make([]string, 0, len(capabilities.models))
		for _, model := range capabilities.models {
			if model.provider == provider {
				values = append(values, model.model)
			}
		}
		return values
	})
}

func (s *Server) availableContextLimits(provider string, modelName string) []string {
	return collectAvailable(s, func(capabilities *registeredWorkerCapabilities) []string {
		values := make([]string, 0, len(capabilities.models))
		for _, model := range capabilities.models {
			if model.provider == provider && model.model == modelName {
				values = append(values, strconv.Itoa(model.maxContextTokens))
			}
		}
		return values
	})
}

func (s *Server) availableTools() []string {
	return collectAvailable(s, func(capabilities *registeredWorkerCapabilities) []string {
		values := make([]string, 0, len(capabilities.tools))
		for tool := range capabilities.tools {
			values = append(values, tool)
		}
		return values
	})
}

func (s *Server) availableCapacities() []string {
	return collectAvailable(s, func(capabilities *registeredWorkerCapabilities) []string {
		return []string{strconv.Itoa(capabilities.maxConcurrentRuns)}
	})
}

func collectAvailable(s *Server, values func(*registeredWorkerCapabilities) []string) []string {
	unique := map[string]struct{}{}
	for _, candidate := range s.liveRoutingCandidates(time.Now().UTC()) {
		for _, value := range values(candidate.capabilities) {
			unique[value] = struct{}{}
		}
	}
	available := make([]string, 0, len(unique))
	for value := range unique {
		available = append(available, value)
	}
	sort.Strings(available)
	return available
}
