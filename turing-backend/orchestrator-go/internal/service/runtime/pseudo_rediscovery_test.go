package runtime

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	mcpregistrysvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/mcpregistry"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestDisabledPseudoToolsStayOutOnCapabilityRereportAndCanReenter(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tools := []repository.DiscoveredTool{
		{ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{}`, Policy: "approval_required"},
		{ServerName: "skills", ToolName: "skills_list", SchemaJSON: `{}`, Policy: "approval_required"},
	}
	if err := h.repo.UpsertTools(ctx, tools); err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if err := h.repo.SetToolPolicyByName(ctx, tool.ServerName, tool.ToolName, "disabled"); err != nil {
			t.Fatal(err)
		}
	}
	capabilities := &registeredWorkerCapabilities{tools: map[string]struct{}{
		"integrations/github.list_issues": {}, "skills/skills_list": {},
	}}
	filteredCapabilities, filtered, err := h.service.filterRegisteredWorkerTools(ctx, capabilities, tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredCapabilities.tools) != 0 || len(filtered) != 0 {
		t.Fatalf("disabled tools survived capability re-report: capabilities=%v tools=%+v", filteredCapabilities.tools, filtered)
	}
	// The registry refresh persists the pruned union, which deliberately clears
	// present/enabled. Re-enabling must still allow the next report to bootstrap.
	if err := h.repo.UpsertTools(ctx, nil); err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if err := h.repo.SetToolPolicyByName(ctx, tool.ServerName, tool.ToolName, "approval_required"); err != nil {
			t.Fatal(err)
		}
	}
	filteredCapabilities, filtered, err = h.service.filterRegisteredWorkerTools(ctx, capabilities, tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredCapabilities.tools) != 2 || len(filtered) != 2 {
		t.Fatalf("re-enabled pseudo tools could not reenter: capabilities=%v tools=%+v", filteredCapabilities.tools, filtered)
	}
}

func TestPolicyUpdateNotifierFiltersStalePseudoToolRereports(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	policyService := mcpregistrysvc.New(h.repo, nil, nil)
	policyService.SetRegistryChangeNotifier(h.service)
	if _, err := h.repo.CreateConnection(ctx, repository.NewConnection{
		ConnectionID: "conn_pseudo_refresh", Provider: "github", DisplayName: "Policy GitHub",
		CredentialCiphertext: []byte{1}, CredentialHint: "••••token", GrantedScopes: []string{"Read repository data."},
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.UpsertTools(ctx, []repository.DiscoveredTool{
		{ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{}`, Policy: "approval_required"},
		{ServerName: "skills", ToolName: "skills_list", SchemaJSON: `{}`, Policy: "approval_required"},
	}); err != nil {
		t.Fatal(err)
	}
	stream := connectWorkerCapabilities(t, h, "worker-pseudo-refresh", "registration-pseudo-refresh", stalePseudoCapabilities("initial"))
	defer func() { _ = stream.CloseSend() }()
	route := repository.RoutingRequirements{AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2"}

	for index, target := range []struct{ server, tool string }{
		{server: "integrations", tool: "github.list_issues"},
		{server: "skills", tool: "skills_list"},
	} {
		if _, err := policyService.UpdateToolPolicyByName(ctx, &turingv1.UpdateToolPolicyByNameRequest{
			ServerName: target.server, ToolName: target.tool, Policy: turingv1.ToolPolicy_TOOL_POLICY_DISABLED,
		}); err != nil {
			t.Fatal(err)
		}
		changed := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
			return command.GetMcpRegistryChanged() != nil
		}).GetMcpRegistryChanged()
		marker := "system/rereport_marker_" + string(rune('a'+index))
		if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerCapabilitiesUpdated{
			WorkerCapabilitiesUpdated: &turingv1.RuntimeWorkerCapabilitiesUpdated{
				WorkerId: "worker-pseudo-refresh", RegistrationId: changed.GetRegistrationId(), Capabilities: stalePseudoCapabilities(marker),
			},
		}}); err != nil {
			t.Fatal(err)
		}
		eventually(t, time.Second, func() bool {
			tools := h.service.EgressToolNames(route)
			return slices.Contains(tools, marker) && !slices.Contains(tools, target.server+"/"+target.tool)
		})
	}
	tools := h.service.EgressToolNames(route)
	if slices.Contains(tools, "integrations/github.list_issues") || slices.Contains(tools, "skills/skills_list") {
		t.Fatalf("disabled pseudo tools resurrected after stale report: %v", tools)
	}
	endpoints, err := h.repo.IntegrationEndpointsForTools(ctx, tools)
	if err != nil || len(endpoints) != 0 {
		t.Fatalf("next disclosure source endpoints=%+v err=%v, want none", endpoints, err)
	}

	for index, target := range []struct{ server, tool string }{
		{server: "integrations", tool: "github.list_issues"},
		{server: "skills", tool: "skills_list"},
	} {
		if _, err := policyService.UpdateToolPolicyByName(ctx, &turingv1.UpdateToolPolicyByNameRequest{
			ServerName: target.server, ToolName: target.tool, Policy: turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
		}); err != nil {
			t.Fatal(err)
		}
		changed := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
			return command.GetMcpRegistryChanged() != nil
		}).GetMcpRegistryChanged()
		marker := "system/reenable_marker_" + string(rune('a'+index))
		if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerCapabilitiesUpdated{
			WorkerCapabilitiesUpdated: &turingv1.RuntimeWorkerCapabilitiesUpdated{
				WorkerId: "worker-pseudo-refresh", RegistrationId: changed.GetRegistrationId(), Capabilities: stalePseudoCapabilities(marker),
			},
		}}); err != nil {
			t.Fatal(err)
		}
		eventually(t, time.Second, func() bool {
			current := h.service.EgressToolNames(route)
			return slices.Contains(current, marker) && slices.Contains(current, target.server+"/"+target.tool)
		})
	}
}

func stalePseudoCapabilities(marker string) *turingv1.WorkerCapabilities {
	capabilities := modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1)
	capabilities.Tools = []*turingv1.DiscoveredTool{
		{ServerName: "integrations", ToolName: "github.list_issues", Schema: &structpb.Struct{}},
		{ServerName: "skills", ToolName: "skills_list", Schema: &structpb.Struct{}},
	}
	if marker != "initial" {
		server, tool, _ := strings.Cut(marker, "/")
		capabilities.Tools = append(capabilities.Tools, &turingv1.DiscoveredTool{ServerName: server, ToolName: tool, Schema: &structpb.Struct{}})
	}
	return capabilities
}
