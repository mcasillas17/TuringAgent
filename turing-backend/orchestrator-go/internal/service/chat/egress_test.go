package chat

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestPrepareRemoteEgressDisclosesExactRemoteMaximumWithoutPersistence(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	before := chatPersistenceCounts(t, h)

	response, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId:      sessionID,
		Content:        "send remotely",
		ContentType:    "text",
		AgentId:        turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:          "gpt-4o-mini",
		IdempotencyKey: "remote_prepare_1",
	})
	if err != nil {
		t.Fatal(err)
	}

	disclosure := response.GetDisclosure()
	if disclosure == nil || disclosure.GetChallenge() == "" {
		t.Fatalf("disclosure = %+v", disclosure)
	}
	if disclosure.GetProvider() != turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE ||
		disclosure.GetModel() != "gpt-4o-mini" ||
		disclosure.GetEndpoint() != "https://api.openai.com/v1" ||
		disclosure.GetEndpointHost() != "api.openai.com" {
		t.Fatalf("destination = %+v", disclosure)
	}
	want := []turingv1.EgressDataCategory{
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CURRENT_MESSAGE,
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY,
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL,
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_SKILL_CONTENT,
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_SCHEMAS,
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS,
	}
	if got := disclosure.GetDataCategories(); len(got) != len(want) {
		t.Fatalf("categories = %v, want %v", got, want)
	} else {
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("categories = %v, want %v", got, want)
			}
		}
	}
	after := chatPersistenceCounts(t, h)
	if before != after {
		t.Fatalf("prepare changed persistence from %+v to %+v", before, after)
	}
}

func TestPrepareRemoteEgressDeletingSessionReturnsFailedPrecondition(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	if _, err := h.repo.BeginSessionDeletion(context.Background(), sessionID); err != nil {
		t.Fatal(err)
	}

	_, err := h.chatClient.PrepareRemoteEgress(
		h.clientContext(),
		&turingv1.PrepareRemoteEgressRequest{
			SessionId:      sessionID,
			Content:        "do not prepare",
			ContentType:    "text",
			ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
			Model:          "gpt-4o-mini",
			IdempotencyKey: "deleting_session",
		},
	)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("PrepareRemoteEgress error = %v, want FailedPrecondition", err)
	}
}

func TestRemoteServerToolsEnterThePerRunEgressDecision(t *testing.T) {
	h := newHarness(t)
	server, err := h.repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor",
		URL:  "https://vendor.example/mcp",
		Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetMCPServerEnabled(context.Background(), server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.ReplaceMCPServerTools(context.Background(), server.ID, []repository.MCPServerTool{{
		Name: "vendor.lookup", Policy: "approval_required", SchemaJSON: `{"type":"object"}`,
	}}); err != nil {
		t.Fatal(err)
	}
	capabilities := defaultChatWorkerCapabilities(false)
	capabilities.Tools = append(capabilities.Tools, &turingv1.DiscoveredTool{
		ServerName: "vendor",
		ToolName:   "vendor.lookup",
		Schema:     &structpb.Struct{},
	})
	worker := connectChatTestWorker(t, h, capabilities)
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	request := &turingv1.PrepareRemoteEgressRequest{
		SessionId:      sessionID,
		Content:        "look it up",
		ContentType:    "text",
		AgentId:        turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:          "llama3.2",
		IdempotencyKey: "remote_mcp_prepare",
		RequestedTools: []string{"vendor/vendor.lookup"},
	}

	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	disclosure := prepared.GetDisclosure()
	if disclosure == nil {
		t.Fatal("remote MCP tool produced no egress disclosure")
	}
	destinations := disclosure.GetRemoteMcpServers()
	if len(destinations) != 1 ||
		destinations[0].GetServerName() != "vendor" ||
		destinations[0].GetEndpoint() != "https://vendor.example/mcp" {
		t.Fatalf("remote MCP destinations = %+v", destinations)
	}
	if !slices.Contains(disclosure.GetDataCategories(), turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS) ||
		!slices.Contains(disclosure.GetDataCategories(), turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS) {
		t.Fatalf("categories = %v, want tool arguments and results", disclosure.GetDataCategories())
	}

	err = sendMessageError(h, &turingv1.SendMessageRequest{
		SessionId:      request.GetSessionId(),
		Content:        request.GetContent(),
		ContentType:    request.GetContentType(),
		AgentId:        request.GetAgentId(),
		ModelProvider:  request.GetModelProvider(),
		Model:          request.GetModel(),
		IdempotencyKey: request.GetIdempotencyKey(),
		RequestedTools: request.GetRequestedTools(),
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("send without acknowledgement error = %v, want FailedPrecondition", err)
	}
}

func TestPrepareExternalAgentDisclosureOmitsCrossSessionRecall(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(true))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	agent, err := h.repo.CreateExternalAgent(context.Background(), repository.ExternalAgentInput{
		DisplayName: "External", Provider: "anthropic",
		BaseURL: "https://example.com/v1", Model: "external-model",
		CredentialRef: "external",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.SetSessionAgent(context.Background(), sessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	response, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: sessionID, Content: "external disclosure", ContentType: "text",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2", IdempotencyKey: "external_disclosure",
	})
	if err != nil {
		t.Fatal(err)
	}
	disclosure := response.GetDisclosure()
	if disclosure.GetExternalAgentId() != agent.AgentID ||
		disclosure.GetEndpointHost() != "example.com" {
		t.Fatalf("external disclosure = %+v", disclosure)
	}
	for _, category := range disclosure.GetDataCategories() {
		if category == turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL {
			t.Fatal("external agent disclosure included cross-session recall")
		}
	}
}

func TestSendMessageRejectsRemoteRunWithoutConsentBeforePersistence(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	before := chatPersistenceCounts(t, h)

	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:      sessionID,
		Content:        "must not persist",
		ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:          "gpt-4o-mini",
		IdempotencyKey: "remote_missing_consent",
	})
	if err == nil {
		_, err = stream.Recv()
	}

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("SendMessage error = %v, want FailedPrecondition", err)
	}
	after := chatPersistenceCounts(t, h)
	if before != after {
		t.Fatalf("rejected send changed persistence from %+v to %+v", before, after)
	}
}

func TestSendMessageRejectsOversizedRemoteContentBeforePersistence(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	before := chatPersistenceCounts(t, h)
	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId: sessionID, Content: strings.Repeat("x", maxEgressContentBytes+1),
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		RemoteEgressConsent: &turingv1.RemoteEgressConsent{
			Challenge: "bounded-before-parse", Acknowledged: true,
		},
	})
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversized remote send error = %v, want InvalidArgument", err)
	}
	if after := chatPersistenceCounts(t, h); after != before {
		t.Fatalf("oversized remote send changed persistence from %+v to %+v", before, after)
	}
}

func TestSendMessageRejectsOversizedRemoteContentBeforeConsent(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	before := chatPersistenceCounts(t, h)
	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId: sessionID, Content: strings.Repeat("x", maxEgressContentBytes+1),
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "gpt-4o-mini",
	})
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversized unconsented remote send error = %v, want InvalidArgument", err)
	}
	if after := chatPersistenceCounts(t, h); after != before {
		t.Fatalf("oversized unconsented send changed persistence from %+v to %+v", before, after)
	}
}

func TestPreparedRemoteSendPersistsOneRunDecision(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	request := &turingv1.PrepareRemoteEgressRequest{
		SessionId:      sessionID,
		Content:        "confirmed remote",
		ContentType:    "text",
		AgentId:        turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider:  turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:          "gpt-4o-mini",
		IdempotencyKey: "remote_confirmed_1",
	}
	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	disclosure := prepared.GetDisclosure()
	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId:      request.GetSessionId(),
		Content:        request.GetContent(),
		ContentType:    request.GetContentType(),
		AgentId:        request.GetAgentId(),
		ModelProvider:  request.GetModelProvider(),
		Model:          request.GetModel(),
		IdempotencyKey: request.GetIdempotencyKey(),
		RemoteEgressConsent: &turingv1.RemoteEgressConsent{
			Challenge:                  disclosure.GetChallenge(),
			AcknowledgedDataCategories: disclosure.GetDataCategories(),
			Acknowledged:               true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if queued.GetRunQueued() == nil {
		t.Fatalf("first event = %+v, want queued", queued)
	}
	decision, err := h.repo.GetRunEgressDecision(context.Background(), queued.GetRunQueued().GetRunId())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Endpoint != disclosure.GetEndpoint() || decision.ChallengeNonce == "" {
		t.Fatalf("stored decision = %+v", decision)
	}
}

func TestPreparedRemoteSendRejectsChangedPayloadAndTampering(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*turingv1.SendMessageRequest)
	}{
		{
			name: "changed content",
			mutate: func(request *turingv1.SendMessageRequest) {
				request.Content = "changed after disclosure"
			},
		},
		{
			name: "tampered challenge",
			mutate: func(request *turingv1.SendMessageRequest) {
				request.RemoteEgressConsent.Challenge += "x"
			},
		},
		{
			name: "missing category",
			mutate: func(request *turingv1.SendMessageRequest) {
				request.RemoteEgressConsent.AcknowledgedDataCategories =
					request.RemoteEgressConsent.AcknowledgedDataCategories[:1]
			},
		},
		{
			name: "false acknowledgment",
			mutate: func(request *turingv1.SendMessageRequest) {
				request.RemoteEgressConsent.Acknowledged = false
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
			defer func() { _ = worker.CloseSend() }()
			sessionID := h.createSession(t)
			prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
				SessionId: sessionID, Content: "original", ContentType: "text",
				ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
				Model:         "gpt-4o-mini", IdempotencyKey: "remote_tamper_" + strings.ReplaceAll(test.name, " ", "_"),
			})
			if err != nil {
				t.Fatal(err)
			}
			disclosure := prepared.GetDisclosure()
			request := &turingv1.SendMessageRequest{
				SessionId: sessionID, Content: "original", ContentType: "text",
				ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
				Model:         "gpt-4o-mini", IdempotencyKey: "remote_tamper_" + strings.ReplaceAll(test.name, " ", "_"),
				RemoteEgressConsent: &turingv1.RemoteEgressConsent{
					Challenge: disclosure.GetChallenge(), Acknowledged: true,
					AcknowledgedDataCategories: disclosure.GetDataCategories(),
				},
			}
			test.mutate(request)
			stream, err := h.chatClient.SendMessage(h.clientContext(), request)
			if err == nil {
				_, err = stream.Recv()
			}
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("SendMessage error = %v, want FailedPrecondition", err)
			}
			counts := chatPersistenceCounts(t, h)
			if counts.Messages != 0 || counts.Runs != 0 || counts.Decisions != 0 {
				t.Fatalf("rejected request persisted %+v", counts)
			}
		})
	}
}

func TestExpiredChallengeStillReplaysExactExistingIdempotentRun(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	h.service.now = func() time.Time {
		return time.Date(2026, time.August, 20, 1, 0, 0, 0, time.UTC)
	}
	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: sessionID, Content: "replay remote", ContentType: "text",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "gpt-4o-mini", IdempotencyKey: "remote_expired_replay",
	})
	if err != nil {
		t.Fatal(err)
	}
	disclosure := prepared.GetDisclosure()
	request := &turingv1.SendMessageRequest{
		SessionId: sessionID, Content: "replay remote", ContentType: "text",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "gpt-4o-mini", IdempotencyKey: "remote_expired_replay",
		RemoteEgressConsent: &turingv1.RemoteEgressConsent{
			Challenge: disclosure.GetChallenge(), Acknowledged: true,
			AcknowledgedDataCategories: disclosure.GetDataCategories(),
		},
	}
	first, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	firstEvent, err := first.Recv()
	if err != nil {
		t.Fatal(err)
	}
	h.service.now = func() time.Time {
		return disclosure.GetExpiresAt().AsTime().Add(time.Second)
	}
	replay, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	replayEvent, err := replay.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if replayEvent.GetRunQueued().GetRunId() != firstEvent.GetRunQueued().GetRunId() {
		t.Fatalf("replay run = %q, want %q", replayEvent.GetRunQueued().GetRunId(), firstEvent.GetRunQueued().GetRunId())
	}
	counts := chatPersistenceCounts(t, h)
	if counts.Runs != 1 || counts.Decisions != 1 {
		t.Fatalf("replay persisted %+v", counts)
	}
}

func TestConsumedChallengeWithoutIdempotencyKeyReturnsAlreadyExists(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: sessionID, Content: "one-time challenge", ContentType: "text",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "gpt-4o-mini",
	})
	if err != nil {
		t.Fatal(err)
	}

	disclosure := prepared.GetDisclosure()
	request := &turingv1.SendMessageRequest{
		SessionId: sessionID, Content: "one-time challenge", ContentType: "text",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "gpt-4o-mini",
		RemoteEgressConsent: &turingv1.RemoteEgressConsent{
			Challenge: disclosure.GetChallenge(), Acknowledged: true,
			AcknowledgedDataCategories: disclosure.GetDataCategories(),
		},
	}
	first, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Recv(); err != nil {
		t.Fatal(err)
	}
	second, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err == nil {
		_, err = second.Recv()
	}
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("second send error = %v, want AlreadyExists", err)
	}
	counts := chatPersistenceCounts(t, h)
	if counts.Runs != 1 || counts.Decisions != 1 {
		t.Fatalf("nonce reuse persisted %+v", counts)
	}
}

func TestConcurrentChallengeConsumptionCreatesOneRun(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: sessionID, Content: "concurrent challenge", ContentType: "text",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "gpt-4o-mini",
	})
	if err != nil {
		t.Fatal(err)
	}
	disclosure := prepared.GetDisclosure()
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			stream, callErr := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
				SessionId: sessionID, Content: "concurrent challenge", ContentType: "text",
				ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
				Model:         "gpt-4o-mini",
				RemoteEgressConsent: &turingv1.RemoteEgressConsent{
					Challenge: disclosure.GetChallenge(), Acknowledged: true,
					AcknowledgedDataCategories: disclosure.GetDataCategories(),
				},
			})
			if callErr == nil {
				_, callErr = stream.Recv()
			}
			results <- callErr
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	successes, conflicts := 0, 0
	for result := range results {
		switch status.Code(result) {
		case codes.OK:
			successes++
		case codes.AlreadyExists:
			conflicts++
		default:
			t.Fatalf("concurrent send error = %v", result)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent outcomes success/conflict = %d/%d", successes, conflicts)
	}
	counts := chatPersistenceCounts(t, h)
	if counts.Runs != 1 || counts.Decisions != 1 {
		t.Fatalf("concurrent challenge persisted %+v", counts)
	}
}

func TestExactLocalIdempotentReplaySurvivesLaterRemoteRouting(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(true))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	request := &turingv1.SendMessageRequest{
		SessionId: sessionID, Content: "local replay", ContentType: "text",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2", IdempotencyKey: "local_before_route",
	}
	first, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	firstEvent, err := first.Recv()
	if err != nil {
		t.Fatal(err)
	}
	agent, err := h.repo.CreateExternalAgent(context.Background(), repository.ExternalAgentInput{
		DisplayName: "External", Provider: "anthropic",
		BaseURL: "https://example.com/v1", Model: "external-model",
		CredentialRef: "external",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.SetSessionAgent(context.Background(), sessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	replay, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	replayEvent, err := replay.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if replayEvent.GetRunQueued().GetRunId() != firstEvent.GetRunQueued().GetRunId() {
		t.Fatalf("replay run = %q, want %q", replayEvent.GetRunQueued().GetRunId(), firstEvent.GetRunQueued().GetRunId())
	}
}

func TestChangedPayloadWithExistingRemoteIdempotencyKeyReturnsAlreadyExists(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: sessionID, Content: "original remote", ContentType: "text",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "gpt-4o-mini", IdempotencyKey: "remote_conflict_key",
	})
	if err != nil {
		t.Fatal(err)
	}
	disclosure := prepared.GetDisclosure()
	request := &turingv1.SendMessageRequest{
		SessionId: sessionID, Content: "original remote", ContentType: "text",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "gpt-4o-mini", IdempotencyKey: "remote_conflict_key",
		RemoteEgressConsent: &turingv1.RemoteEgressConsent{
			Challenge: disclosure.GetChallenge(), Acknowledged: true,
			AcknowledgedDataCategories: disclosure.GetDataCategories(),
		},
	}
	first, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Recv(); err != nil {
		t.Fatal(err)
	}
	request.Content = "changed remote"
	second, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err == nil {
		_, err = second.Recv()
	}
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("changed remote replay error = %v, want AlreadyExists", err)
	}
	request.Content = "original remote"
	request.Model = "different-model"
	third, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err == nil {
		_, err = third.Recv()
	}
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("changed-model replay error = %v, want AlreadyExists", err)
	}
}

func TestExternalCredentialReferenceChangeRequiresNewDisclosure(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(true))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	agent, err := h.repo.CreateExternalAgent(context.Background(), repository.ExternalAgentInput{
		DisplayName: "External", Provider: "anthropic",
		BaseURL: "https://example.com/v1", Model: "external-model",
		CredentialRef: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.SetSessionAgent(context.Background(), sessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: sessionID, Content: "credential bound", ContentType: "text",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2", IdempotencyKey: "credential_change",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.UpdateExternalAgent(context.Background(), agent.AgentID, repository.ExternalAgentInput{
		DisplayName: agent.DisplayName, Provider: agent.Provider,
		BaseURL: agent.BaseURL, Model: agent.Model, CredentialRef: "external",
	}); err != nil {
		t.Fatal(err)
	}
	disclosure := prepared.GetDisclosure()
	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId: sessionID, Content: "credential bound", ContentType: "text",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2", IdempotencyKey: "credential_change",
		RemoteEgressConsent: &turingv1.RemoteEgressConsent{
			Challenge: disclosure.GetChallenge(), Acknowledged: true,
			AcknowledgedDataCategories: disclosure.GetDataCategories(),
		},
	})
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("credential change error = %v, want FailedPrecondition", err)
	}
}

type persistenceCounts struct {
	Messages           int
	Runs               int
	Jobs               int
	Decisions          int
	Audits             int
	Events             int
	IdempotencyRecords int
}

func chatPersistenceCounts(t *testing.T, h *harness) persistenceCounts {
	t.Helper()
	var counts persistenceCounts
	for _, item := range []struct {
		table string
		dest  *int
	}{
		{"messages", &counts.Messages},
		{"agent_runs", &counts.Runs},
		{"jobs", &counts.Jobs},
		{"run_egress_decisions", &counts.Decisions},
		{"audit_logs", &counts.Audits},
		{"events", &counts.Events},
		{"send_message_idempotency", &counts.IdempotencyRecords},
	} {
		if err := h.database.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+item.table).Scan(item.dest); err != nil {
			t.Fatal(err)
		}
	}
	return counts
}

func consentRemoteRequest(t *testing.T, h *harness, request *turingv1.SendMessageRequest) {
	t.Helper()
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = "consent_" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	}
	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: request.GetSessionId(), Content: request.GetContent(),
		ContentType: request.GetContentType(), AgentId: request.GetAgentId(),
		ModelProvider: request.GetModelProvider(), Model: request.GetModel(),
		IdempotencyKey:                 request.GetIdempotencyKey(),
		RequestedTools:                 request.GetRequestedTools(),
		RequiredContextTokens:          request.GetRequiredContextTokens(),
		MinimumWorkerMaxConcurrentRuns: request.GetMinimumWorkerMaxConcurrentRuns(),
	})
	if err != nil {
		t.Fatal(err)
	}
	disclosure := prepared.GetDisclosure()
	if disclosure == nil {
		t.Fatal("remote request produced no disclosure")
	}
	request.RemoteEgressConsent = &turingv1.RemoteEgressConsent{
		Challenge: disclosure.GetChallenge(), Acknowledged: true,
		AcknowledgedDataCategories: disclosure.GetDataCategories(),
	}
}
