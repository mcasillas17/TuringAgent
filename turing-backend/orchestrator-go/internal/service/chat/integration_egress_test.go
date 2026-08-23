package chat

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	mcpregistrysvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/mcpregistry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func integrationEgressCapabilities() *turingv1.WorkerCapabilities {
	capabilities := defaultChatWorkerCapabilities(false)
	capabilities.Tools = append(capabilities.Tools, &turingv1.DiscoveredTool{
		ServerName: "integrations", ToolName: "github.list_issues", Schema: &structpb.Struct{},
	})
	return capabilities
}

func integrationPrepareRequest(sessionID, idempotencyKey string) *turingv1.PrepareRemoteEgressRequest {
	return &turingv1.PrepareRemoteEgressRequest{
		SessionId: sessionID, Content: "list issues", ContentType: "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
		IdempotencyKey: idempotencyKey, RequestedTools: []string{"integrations/github.list_issues"},
	}
}

func createChatGitHubConnection(t *testing.T, repo *repository.Repository, index int) repository.Connection {
	t.Helper()
	connection, err := repo.CreateConnection(context.Background(), repository.NewConnection{
		ConnectionID: fmt.Sprintf("conn_chat_%03d", index), Provider: "github",
		DisplayName: fmt.Sprintf("GitHub %03d", index), CredentialCiphertext: []byte{1},
		CredentialHint: "••••token", GrantedScopes: []string{"Read repository data."},
	})
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func integrationEntryAtSize(t *testing.T, size int) repository.IntegrationEndpointEgress {
	t.Helper()
	entry := repository.IntegrationEndpointEgress{
		Endpoint: repository.GitHubIntegrationEndpoint, EndpointHost: repository.GitHubIntegrationEndpointHost,
		ConnectionID: "conn_boundary", DisplayName: "", Tools: []string{"github.list_issues"},
	}
	for {
		got, err := repository.IntegrationEndpointEntrySize(entry)
		if err != nil {
			t.Fatal(err)
		}
		if got == size {
			return entry
		}
		if got > size {
			t.Fatalf("cannot construct integration entry at %d bytes; reached %d", size, got)
		}
		entry.DisplayName += strings.Repeat("x", size-got)
	}
}

func TestLocalRunWithoutIntegrationToolsHasNoIntegrationEndpoints(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	createChatGitHubConnection(t, h.repo, 1)
	request := integrationPrepareRequest(h.createSession(t), "no_integration_tools")
	request.RequestedTools = []string{"system/system.time"}

	response, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	if endpoints := response.GetDisclosure().GetIntegrationEndpoints(); len(endpoints) != 0 {
		t.Fatalf("integration endpoints=%+v, want none when no integration tool is offered", endpoints)
	}
}

func TestDisabledIntegrationToolLeavesTheNextDisclosure(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, integrationEgressCapabilities())
	defer func() { _ = worker.CloseSend() }()
	createChatGitHubConnection(t, h.repo, 1)
	request := integrationPrepareRequest(h.createSession(t), "disclosure_before_disable")
	request.RequestedTools = []string{"system/system.time"}
	before, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), request)
	if err != nil || len(before.GetDisclosure().GetIntegrationEndpoints()) != 1 {
		t.Fatalf("before disclosure=%+v err=%v, want one integration endpoint", before.GetDisclosure(), err)
	}
	policyService := mcpregistrysvc.New(h.repo, nil, nil)
	policyService.SetRegistryChangeNotifier(h.runtime)
	if _, err := policyService.UpdateToolPolicyByName(context.Background(), &turingv1.UpdateToolPolicyByNameRequest{
		ServerName: "integrations", ToolName: "github.list_issues", Policy: turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
	}); err != nil {
		t.Fatal(err)
	}
	request.IdempotencyKey = "disclosure_after_disable"
	after, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	if endpoints := after.GetDisclosure().GetIntegrationEndpoints(); len(endpoints) != 0 {
		t.Fatalf("next disclosure endpoints=%+v, want none after policy notification", endpoints)
	}
}

func TestIntegrationEgressEntryBudgetIsEnforcedWhileResolving(t *testing.T) {
	for _, test := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "exact", size: repository.MaxIntegrationEndpointEntryBytes},
		{name: "one over", size: repository.MaxIntegrationEndpointEntryBytes + 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			worker := connectChatTestWorker(t, h, integrationEgressCapabilities())
			defer func() { _ = worker.CloseSend() }()
			sessionID := h.createSession(t)
			entry := integrationEntryAtSize(t, test.size)
			h.service.integrationEndpointResolver = func(context.Context, []string) ([]repository.IntegrationEndpointEgress, error) {
				return []repository.IntegrationEndpointEgress{entry}, nil
			}
			response, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), integrationPrepareRequest(sessionID, "entry_budget_"+strings.ReplaceAll(test.name, " ", "_")))
			if test.wantErr {
				if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "entry is too large") {
					t.Fatalf("error=%v, want legible FailedPrecondition from resolution", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if response.GetDisclosure() == nil || response.GetDisclosure().GetChallenge() == "" {
				t.Fatal("exact-boundary entry was not prepared and signed")
			}
			payload, _, err := h.service.parseEgressChallenge(response.GetDisclosure().GetChallenge())
			if err != nil || len(payload.IntegrationEndpoints) != 1 {
				t.Fatalf("signed payload endpoints=%+v err=%v", payload.IntegrationEndpoints, err)
			}
		})
	}
}

func TestIntegrationEgressConnectionCountBudgetIsEnforcedWhileResolving(t *testing.T) {
	for _, test := range []struct {
		name    string
		count   int
		wantErr bool
	}{
		{name: "exact", count: repository.MaxIntegrationEndpoints},
		{name: "one over", count: repository.MaxIntegrationEndpoints + 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			worker := connectChatTestWorker(t, h, integrationEgressCapabilities())
			defer func() { _ = worker.CloseSend() }()
			for index := 0; index < test.count; index++ {
				createChatGitHubConnection(t, h.repo, index)
			}
			response, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), integrationPrepareRequest(h.createSession(t), "count_budget_"+strings.ReplaceAll(test.name, " ", "_")))
			if test.wantErr {
				if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "too many integration connections") {
					t.Fatalf("error=%v, want legible FailedPrecondition from resolution", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := len(response.GetDisclosure().GetIntegrationEndpoints()); got != repository.MaxIntegrationEndpoints {
				t.Fatalf("disclosure endpoints=%d, want %d", got, repository.MaxIntegrationEndpoints)
			}
			if _, _, err := h.service.parseEgressChallenge(response.GetDisclosure().GetChallenge()); err != nil {
				t.Fatalf("exact-boundary challenge did not verify: %v", err)
			}
		})
	}
}

func TestIntegrationChallengeRefusesWhenOneOfTwoConnectionsIsRevoked(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, integrationEgressCapabilities())
	defer func() { _ = worker.CloseSend() }()
	first := createChatGitHubConnection(t, h.repo, 1)
	createChatGitHubConnection(t, h.repo, 2)
	sessionID := h.createSession(t)
	prepare := integrationPrepareRequest(sessionID, "two_connections_world_change")
	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), prepare)
	if err != nil {
		t.Fatal(err)
	}
	disclosure := prepared.GetDisclosure()
	if len(disclosure.GetIntegrationEndpoints()) != 2 || len(disclosure.GetSelectedTools()) == 0 {
		t.Fatalf("initial disclosure=%+v", disclosure)
	}
	names := make([]string, 0, 2)
	for _, endpoint := range disclosure.GetIntegrationEndpoints() {
		names = append(names, endpoint.GetDisplayName())
	}
	sort.Strings(names)
	if names[0] != "GitHub 001" || names[1] != "GitHub 002" {
		t.Fatalf("disclosure display names = %v, want the connections' names", names)
	}
	if _, err := h.repo.RevokeConnection(context.Background(), first.ConnectionID); err != nil {
		t.Fatal(err)
	}
	current := integrationPrepareRequest(sessionID, "survivor_snapshot_probe")
	current.Content = prepare.Content
	currentDisclosure, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), current)
	if err != nil {
		t.Fatal(err)
	}
	if got := currentDisclosure.GetDisclosure().GetSelectedTools(); fmt.Sprint(got) != fmt.Sprint(disclosure.GetSelectedTools()) {
		t.Fatalf("selected tools changed after one-of-two revoke: before=%v after=%v", disclosure.GetSelectedTools(), got)
	}
	if got := len(currentDisclosure.GetDisclosure().GetIntegrationEndpoints()); got != 1 {
		t.Fatalf("current endpoints=%d, want surviving connection", got)
	}
	request := &turingv1.SendMessageRequest{
		SessionId: prepare.GetSessionId(), Content: prepare.GetContent(), ContentType: prepare.GetContentType(),
		AgentId: prepare.GetAgentId(), ModelProvider: prepare.GetModelProvider(), Model: prepare.GetModel(),
		IdempotencyKey: prepare.GetIdempotencyKey(), RequestedTools: prepare.GetRequestedTools(),
		RemoteEgressConsent: &turingv1.RemoteEgressConsent{
			Challenge: disclosure.GetChallenge(), Acknowledged: true,
			AcknowledgedDataCategories: disclosure.GetDataCategories(),
		},
	}
	stream, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "context changed") {
		t.Fatalf("send error=%v, want context-changed refusal", err)
	}
}

func TestIntegrationConnectionSetChangeConflictsWithExistingIdempotencyKey(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, integrationEgressCapabilities())
	defer func() { _ = worker.CloseSend() }()
	createChatGitHubConnection(t, h.repo, 1)
	sessionID := h.createSession(t)
	prepare := integrationPrepareRequest(sessionID, "integration_connection_conflict")
	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), prepare)
	if err != nil {
		t.Fatal(err)
	}
	disclosure := prepared.GetDisclosure()
	send := &turingv1.SendMessageRequest{
		SessionId: prepare.GetSessionId(), Content: prepare.GetContent(), ContentType: prepare.GetContentType(),
		AgentId: prepare.GetAgentId(), ModelProvider: prepare.GetModelProvider(), Model: prepare.GetModel(),
		IdempotencyKey: prepare.GetIdempotencyKey(), RequestedTools: prepare.GetRequestedTools(),
		RemoteEgressConsent: &turingv1.RemoteEgressConsent{Challenge: disclosure.GetChallenge(), Acknowledged: true, AcknowledgedDataCategories: disclosure.GetDataCategories()},
	}
	first, err := h.chatClient.SendMessage(h.clientContext(), send)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Recv(); err != nil {
		t.Fatal(err)
	}
	createChatGitHubConnection(t, h.repo, 2)
	changed, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), prepare)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(changed.GetDisclosure().GetIntegrationEndpoints()); got != 2 {
		t.Fatalf("changed endpoints=%d, want 2", got)
	}
	send.RemoteEgressConsent = &turingv1.RemoteEgressConsent{
		Challenge: changed.GetDisclosure().GetChallenge(), Acknowledged: true,
		AcknowledgedDataCategories: changed.GetDisclosure().GetDataCategories(),
	}
	second, err := h.chatClient.SendMessage(h.clientContext(), send)
	if err == nil {
		_, err = second.Recv()
	}
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("changed connection-set replay error=%v, want AlreadyExists", err)
	}
	counts := chatPersistenceCounts(t, h)
	if counts.Runs != 1 || counts.Decisions != 1 {
		t.Fatalf("connection-set conflict persisted fresh work: %+v", counts)
	}
}
