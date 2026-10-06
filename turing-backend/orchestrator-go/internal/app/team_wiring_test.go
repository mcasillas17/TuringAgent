package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func newTeamTestApp(t *testing.T, enabled bool) *App {
	t.Helper()
	return newTeamTestAppWithProfile(t, enabled, "---\nname: Research\ndescription: Looks things up\ntools: [system.time]\nmemory: none\n---\nYou research.\n")
}

func newTeamTestAppWithProfile(t *testing.T, enabled bool, profile string) *App {
	t.Helper()
	teamRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(teamRoot, "research"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(teamRoot, "research", "AGENT.md"), []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := New(config.Config{
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
		ApprovalJWTSecret:    "approval-secret",
		DatabasePath:         t.TempDir() + "/turing.db",
		OllamaModel:          "llama3.2",
		TeamRoot:             teamRoot,
		AgentTeamEnabled:     enabled,
		MaxDelegationsPerRun: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Stop)
	return app
}

// The public facet manages profiles and the internal facet serves the team
// tool, and the runtime identity reaches only the latter.
func TestTeamServiceFacetAndRuntimeIdentityWiring(t *testing.T) {
	app := newTeamTestApp(t, true)
	public := turingv1.NewTeamServiceClient(newBufconnClient(t, app.PublicServer))
	internal := turingv1.NewTeamServiceClient(newBufconnClient(t, app.InternalServer))
	publicCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer client")
	internalCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer internal")

	if _, err := public.ListTeamTools(publicCtx, &turingv1.ListTeamToolsRequest{RunId: "run_missing"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public ListTeamTools error = %v, want PermissionDenied", err)
	}
	if _, err := internal.ListTeamTools(internalCtx, &turingv1.ListTeamToolsRequest{RunId: "run_missing"}); status.Code(err) != codes.NotFound {
		t.Fatalf("internal ListTeamTools error = %v, want NotFound from the service", err)
	}
	if _, err := internal.ListAgentProfiles(internalCtx, &turingv1.ListAgentProfilesRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("internal ListAgentProfiles error = %v, want PermissionDenied", err)
	}
	if _, err := public.CallTeamTool(publicCtx, &turingv1.CallTeamToolRequest{RunId: "run_missing"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public CallTeamTool error = %v, want PermissionDenied", err)
	}
	if _, err := internal.CallTeamTool(internalCtx, &turingv1.CallTeamToolRequest{RunId: "run_missing"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("internal CallTeamTool error = %v, want InvalidArgument from the service", err)
	}
}

// With the team on, a local Turing turn served by a team-protocol worker is
// offered the active specialists; with it off, the same turn is offered none.
func TestALocalTurnIsOfferedTheTeamOnlyWhileItIsOn(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "on", false: "off"}[enabled], func(t *testing.T) {
			app := newTeamTestApp(t, enabled)
			publicConn := newBufconnClient(t, app.PublicServer)
			internalConn := newBufconnClient(t, app.InternalServer)
			publicCtx, cancelPublic := context.WithCancel(metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer client"))
			defer cancelPublic()
			internalCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer internal")

			worker, err := turingv1.NewRuntimeServiceClient(internalConn).ConnectWorker(internalCtx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = worker.CloseSend() }()
			if err := worker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
				WorkerId: "team-worker", RegistrationId: "registration-team-worker",
				Capabilities: &turingv1.WorkerCapabilities{
					Models:   []*turingv1.ModelCapability{{Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2", MaxContextTokens: 8192}},
					AgentIds: []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
					Tools: []*turingv1.DiscoveredTool{
						{ServerName: "system", ToolName: "system.time", Schema: &structpb.Struct{}},
						{ServerName: "team", ToolName: "team.delegate", Schema: &structpb.Struct{}},
					},
					MaxConcurrentRuns:   1,
					TeamProtocolVersion: 1,
				},
			}}}); err != nil {
				t.Fatal(err)
			}
			recvRuntimeCommand(t, worker, func(command *turingv1.RuntimeCommand) bool { return command.GetWorkerAccepted() != nil })

			team := turingv1.NewTeamServiceClient(publicConn)
			profiles, err := team.ListAgentProfiles(publicCtx, &turingv1.ListAgentProfilesRequest{})
			if err != nil || len(profiles.GetProfiles()) != 1 {
				t.Fatalf("profiles = %+v, %v", profiles, err)
			}
			if _, err := team.GrantAgentProfile(publicCtx, &turingv1.GrantAgentProfileRequest{
				ProfileId: "research", Revision: profiles.GetProfiles()[0].GetRevision(),
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := team.SetAgentProfileEnabled(publicCtx, &turingv1.SetAgentProfileEnabledRequest{ProfileId: "research", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			session, err := app.Repository.CreateSession(context.Background(), "Team")
			if err != nil {
				t.Fatal(err)
			}
			stream, err := turingv1.NewChatServiceClient(publicConn).SendMessage(publicCtx, &turingv1.SendMessageRequest{
				SessionId: session.SessionID, Content: "prep me", ContentType: "text",
				AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
				ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
				IdempotencyKey: "team_wiring",
			})
			if err != nil {
				t.Fatal(err)
			}
			queued, err := stream.Recv()
			if err != nil {
				t.Fatal(err)
			}

			tools, err := turingv1.NewTeamServiceClient(internalConn).ListTeamTools(internalCtx,
				&turingv1.ListTeamToolsRequest{RunId: queued.GetRunQueued().GetRunId()})
			if err != nil {
				t.Fatal(err)
			}
			if !enabled {
				if len(tools.GetTools()) != 0 {
					t.Fatalf("tools = %+v, want none while the team is off", tools.GetTools())
				}
				return
			}
			if len(tools.GetTools()) != 1 {
				t.Fatalf("tools = %+v, want team.delegate", tools.GetTools())
			}
			agent := tools.GetTools()[0].GetSchema().AsMap()["properties"].(map[string]any)["agent"].(map[string]any)
			if enum, _ := agent["enum"].([]any); !slices.Equal(enum, []any{"research"}) {
				t.Fatalf("agent enum = %v, want the active specialist", agent["enum"])
			}

			// The worker holding the run delegates through the internal facet,
			// and the specialist's hidden conversation opens with the brief.
			assigned := recvRuntimeCommand(t, worker, func(command *turingv1.RuntimeCommand) bool { return command.GetRunAssigned() != nil })
			args, err := structpb.NewStruct(map[string]any{"agent": "research", "task": "Gather the notes"})
			if err != nil {
				t.Fatal(err)
			}
			delegated, err := turingv1.NewTeamServiceClient(internalConn).CallTeamTool(internalCtx, &turingv1.CallTeamToolRequest{
				RunId: queued.GetRunQueued().GetRunId(), AssignmentAttemptId: assigned.GetRunAssigned().GetAssignmentAttemptId(),
				ToolCallId: "call_1", ToolName: "team.delegate", Args: args,
			})
			if err != nil {
				t.Fatal(err)
			}
			if delegated.GetResult().AsMap()["agent"] != "research" || delegated.GetResult().AsMap()["state"] != "queued" {
				t.Fatalf("delegation = %v", delegated.GetResult().AsMap())
			}
			delegation, found, err := app.Repository.DelegationForToolCall(context.Background(), queued.GetRunQueued().GetRunId(), "call_1")
			if err != nil || !found {
				t.Fatalf("delegation = %+v, %v, %v", delegation, found, err)
			}
			messages, err := turingv1.NewSessionServiceClient(publicConn).ListMessages(publicCtx,
				&turingv1.ListMessagesRequest{SessionId: delegation.ChildSessionID})
			if err != nil {
				t.Fatal(err)
			}
			if len(messages.GetMessages()) == 0 || !strings.Contains(messages.GetMessages()[0].GetContent(), "Task:\nGather the notes") {
				t.Fatalf("child transcript = %+v, want the brief first", messages.GetMessages())
			}
		})
	}
}
