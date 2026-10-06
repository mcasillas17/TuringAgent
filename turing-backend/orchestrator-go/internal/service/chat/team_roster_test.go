package chat

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/types/known/structpb"
)

var chatTestRoster = []repository.TeamRosterEntry{{
	ProfileID: "research", Revision: "rev-1", Name: "Research", Emoji: "🔬",
	Description: "Looks things up", Tools: []string{"system/system.time"},
}}

type fakeTeam struct {
	roster []repository.TeamRosterEntry
	err    error
	calls  int
	// continuation is what Continuation answers, and continuationErr its
	// failure; continuationRoutes records every route it was asked about.
	continuation       repository.TeamContinuation
	continuationErr    error
	continuationRoutes []repository.RoutingRequirements
}

func (f *fakeTeam) Roster(context.Context) ([]repository.TeamRosterEntry, error) {
	f.calls++
	return f.roster, f.err
}

func (f *fakeTeam) Continuation(_ context.Context, route repository.RoutingRequirements) (repository.TeamContinuation, error) {
	f.continuationRoutes = append(f.continuationRoutes, route)
	return f.continuation, f.continuationErr
}

var teamDelegateDescriptor = &turingv1.DiscoveredTool{ServerName: "team", ToolName: "team.delegate", Schema: &structpb.Struct{}}

// connectTeamWorker connects one worker at a team protocol version, with the
// default tools plus extra.
func connectTeamWorker(t *testing.T, h *harness, workerID string, version int32, extra ...*turingv1.DiscoveredTool) turingv1.RuntimeService_ConnectWorkerClient {
	t.Helper()
	capabilities := defaultChatWorkerCapabilities(false)
	capabilities.TeamProtocolVersion = version
	capabilities.Tools = append(capabilities.Tools, extra...)
	return connectWorkerWith(t, h, workerID, capabilities)
}

func connectWorkerWith(t *testing.T, h *harness, workerID string, capabilities *turingv1.WorkerCapabilities) turingv1.RuntimeService_ConnectWorkerClient {
	t.Helper()
	stream, err := turingv1.NewRuntimeServiceClient(h.conn).ConnectWorker(h.clientContext())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{
		WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: workerID, RegistrationId: "registration-" + workerID, Capabilities: capabilities},
	}}); err != nil {
		t.Fatal(err)
	}
	recvRuntimeCommand(t, stream, func(command *turingv1.RuntimeCommand) bool { return command.GetWorkerAccepted() != nil })
	t.Cleanup(func() { _ = stream.CloseSend() })
	return stream
}

func localTuringRequest(sessionID string) *turingv1.SendMessageRequest {
	return &turingv1.SendMessageRequest{
		SessionId: sessionID, Content: "prep me for the review", ContentType: "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
	}
}

// sendForRun sends and returns the queued run's ID.
func sendForRun(t *testing.T, h *harness, request *turingv1.SendMessageRequest) string {
	t.Helper()
	stream, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	event, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	runID := event.GetRunQueued().GetRunId()
	if runID == "" {
		t.Fatalf("first event = %+v, want run queued", event)
	}
	return runID
}

func wantRunRoster(t *testing.T, h *harness, runID string, want []repository.TeamRosterEntry) {
	t.Helper()
	roster, err := h.repo.RunTeamRoster(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 && len(roster) == 0 {
		return
	}
	if !reflect.DeepEqual(roster, want) {
		t.Fatalf("run roster = %+v, want %+v", roster, want)
	}
}

// An attended Turing turn on a local model, with a team-protocol worker on
// its route, is enqueued with the team as it stands.
func TestALocalTuringTurnIsOfferedTheTeam(t *testing.T) {
	h := newHarness(t)
	connectTeamWorker(t, h, "team-worker", 1)
	h.service.SetTeamRoster(&fakeTeam{roster: chatTestRoster})

	runID := sendForRun(t, h, localTuringRequest(h.createSession(t)))
	wantRunRoster(t, h, runID, chatTestRoster)
}

// What the turn's continuation may use is frozen with its team, read for the
// same team-protocol route the roster was checked on.
func TestALocalTuringTurnFreezesItsContinuationWithTheTeam(t *testing.T) {
	h := newHarness(t)
	connectTeamWorker(t, h, "team-worker", 1)
	continuation := repository.TeamContinuation{Tools: []string{"system/system.time"}, ResultMaxBytes: 4096}
	team := &fakeTeam{roster: chatTestRoster, continuation: continuation}
	h.service.SetTeamRoster(team)

	runID := sendForRun(t, h, localTuringRequest(h.createSession(t)))
	frozen, err := h.repo.RunTeamContinuation(context.Background(), runID)
	if err != nil || !reflect.DeepEqual(frozen, continuation) {
		t.Fatalf("frozen continuation = %+v, %v; want %+v", frozen, err, continuation)
	}
	if len(team.continuationRoutes) == 0 {
		t.Fatal("the continuation was never asked for")
	}
	for _, route := range team.continuationRoutes {
		if route.MinimumTeamProtocolVersion != 1 || route.Model != "llama3.2" || route.ModelProvider != "ollama" {
			t.Fatalf("continuation route = %+v, want the turn's route at team protocol 1", route)
		}
	}
}

// A team whose continuation cannot be worked out is not offered: delegating
// would leave the results nowhere to go.
func TestATeamWithoutAContinuationIsNotOffered(t *testing.T) {
	h := newHarness(t)
	connectTeamWorker(t, h, "team-worker", 1)
	h.service.SetTeamRoster(&fakeTeam{roster: chatTestRoster, continuationErr: errors.New("tool catalog unreadable")})

	runID := sendForRun(t, h, localTuringRequest(h.createSession(t)))
	wantRunRoster(t, h, runID, nil)
}

// Only a worker that honors the team protocol can run a parent with the
// team, so without one the team is not even read.
func TestATurnWithoutATeamProtocolWorkerIsNotOfferedTheTeam(t *testing.T) {
	h := newHarness(t)
	connectTeamWorker(t, h, "old-worker", 0)
	team := &fakeTeam{roster: chatTestRoster}
	h.service.SetTeamRoster(team)

	runID := sendForRun(t, h, localTuringRequest(h.createSession(t)))
	wantRunRoster(t, h, runID, nil)
	if team.calls != 0 {
		t.Fatalf("team read %d times, want none", team.calls)
	}
}

// The team is optional: when it cannot be read, Turing still answers, just
// without it.
func TestAnUnreadableTeamLeavesTheTurnWithoutIt(t *testing.T) {
	h := newHarness(t)
	connectTeamWorker(t, h, "team-worker", 1)
	h.service.SetTeamRoster(&fakeTeam{err: errors.New("team root unreadable")})

	runID := sendForRun(t, h, localTuringRequest(h.createSession(t)))
	wantRunRoster(t, h, runID, nil)
}

// The static gates: only an attended Turing run on a local model, in a
// conversation that is not routed to an external agent, is offered the team.
func TestTheTeamIsOfferedOnlyWhereARunCanDelegate(t *testing.T) {
	h := newHarness(t)
	connectTeamWorker(t, h, "team-worker", 1)
	local := h.createSession(t)
	routed := h.createSession(t)
	agent, err := h.repo.CreateExternalAgent(context.Background(), repository.ExternalAgentInput{
		DisplayName: "External", Provider: "anthropic", BaseURL: "https://example.com",
		Model: "external-model", CredentialRef: "external",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.SetSessionAgent(context.Background(), routed, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	base := repository.EnqueueUserMessageInput{
		SessionID: local, AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2", ExecutionModel: "llama3.2",
	}
	for _, test := range []struct {
		name   string
		source teamRosterSource
		mutate func(*repository.EnqueueUserMessageInput)
		want   bool
	}{
		{"a local Turing run", &fakeTeam{roster: chatTestRoster}, func(*repository.EnqueueUserMessageInput) {}, true},
		{"the team is off", nil, func(*repository.EnqueueUserMessageInput) {}, false},
		{"a remote model", &fakeTeam{roster: chatTestRoster}, func(input *repository.EnqueueUserMessageInput) {
			input.ModelProvider, input.Model, input.ExecutionModel = "openai_compatible", "gpt-4o-mini", "gpt-4o-mini"
		}, false},
		{"an external agent", &fakeTeam{roster: chatTestRoster}, func(input *repository.EnqueueUserMessageInput) {
			input.SessionID = routed
		}, false},
		{"a model no team-protocol worker serves", &fakeTeam{roster: chatTestRoster}, func(input *repository.EnqueueUserMessageInput) {
			input.Model, input.ExecutionModel = "llama3.1:8b", "llama3.1:8b"
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			h.service.SetTeamRoster(test.source)
			input := base
			test.mutate(&input)
			roster, _, err := h.service.teamRosterFor(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(roster) > 0; got != test.want {
				t.Fatalf("offered the team = %v (%+v), want %v", got, roster, test.want)
			}
		})
	}
}

// The team is offered only where a team-protocol worker can serve the whole
// turn. When only an older worker meets the turn's tools, context or
// capacity, the turn goes to it exactly as before, without the team, rather
// than being refused for a minimum the team added.
func TestATurnOnlyAnOlderWorkerCanServeIsNotOfferedTheTeam(t *testing.T) {
	for _, test := range []struct {
		name    string
		older   func(*turingv1.WorkerCapabilities)
		request func(*turingv1.SendMessageRequest)
	}{
		{"a requested tool", func(capabilities *turingv1.WorkerCapabilities) {
			capabilities.Tools = append(capabilities.Tools,
				&turingv1.DiscoveredTool{ServerName: "files", ToolName: "read_file", Schema: &structpb.Struct{}})
		}, func(request *turingv1.SendMessageRequest) {
			request.RequestedTools = []string{"files/read_file"}
		}},
		{"the context", func(capabilities *turingv1.WorkerCapabilities) {
			capabilities.Models[0].MaxContextTokens = 32768
		}, func(request *turingv1.SendMessageRequest) {
			request.RequiredContextTokens = 16384
		}},
		{"the capacity", func(capabilities *turingv1.WorkerCapabilities) {
			capabilities.MaxConcurrentRuns = 8
		}, func(request *turingv1.SendMessageRequest) {
			request.MinimumWorkerMaxConcurrentRuns = 4
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			connectTeamWorker(t, h, "team-worker", 1)
			older := defaultChatWorkerCapabilities(false)
			test.older(older)
			connectWorkerWith(t, h, "old-worker", older)
			team := &fakeTeam{roster: chatTestRoster}
			h.service.SetTeamRoster(team)

			request := localTuringRequest(h.createSession(t))
			test.request(request)
			runID := sendForRun(t, h, request)
			wantRunRoster(t, h, runID, nil)
			if team.calls != 0 {
				t.Fatalf("team read %d times, want none", team.calls)
			}
		})
	}
}

// A local parent whose tools need consent has its tools frozen. Asking every
// worker on the route would let an older one strip team.delegate from the
// set, so the team-protocol workers are asked first; the consent covers that
// set, and the consented send keeps the team.
func TestAConsentedParentAsksTheTeamProtocolWorkersFirst(t *testing.T) {
	h := newHarness(t)
	github := &turingv1.DiscoveredTool{ServerName: "integrations", ToolName: "github.list_issues", Schema: &structpb.Struct{}}
	connectTeamWorker(t, h, "old-worker", 0, github)
	connectTeamWorker(t, h, "team-worker", 1, github, teamDelegateDescriptor)
	createChatGitHubConnection(t, h.repo, 1)
	h.service.SetTeamRoster(&fakeTeam{roster: chatTestRoster})

	request := localTuringRequest(h.createSession(t))
	request.RequestedTools = []string{"integrations/github.list_issues"}
	consentRemoteRequest(t, h, request)
	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: request.GetSessionId(), Content: request.GetContent(), ContentType: request.GetContentType(),
		AgentId: request.GetAgentId(), ModelProvider: request.GetModelProvider(), Model: request.GetModel(),
		IdempotencyKey: request.GetIdempotencyKey(), RequestedTools: request.GetRequestedTools(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(prepared.GetDisclosure().GetSelectedTools(), repository.TeamDelegateTool) {
		t.Fatalf("disclosed tools = %v, want team.delegate from the team-protocol workers", prepared.GetDisclosure().GetSelectedTools())
	}
	runID := sendForRun(t, h, request)
	wantRunRoster(t, h, runID, chatTestRoster)
}

// A consented send derives its tools from what the consent covered, not from a
// second reading of the team: the team is optional, so a read that fails on
// either side of the consent leaves the run without it rather than refusing a
// send the user already agreed to.
func TestAConsentedSendDoesNotRereadTheTeamToPickItsTools(t *testing.T) {
	for _, test := range []struct {
		name              string
		atPrepare, atSend error
	}{
		{"read at prepare, unreadable at send", nil, errors.New("team root unreadable")},
		{"unreadable at prepare, read at send", errors.New("team root unreadable"), nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			github := &turingv1.DiscoveredTool{ServerName: "integrations", ToolName: "github.list_issues", Schema: &structpb.Struct{}}
			connectTeamWorker(t, h, "old-worker", 0, github)
			connectTeamWorker(t, h, "team-worker", 1, github, teamDelegateDescriptor)
			createChatGitHubConnection(t, h.repo, 1)
			team := &fakeTeam{roster: chatTestRoster, err: test.atPrepare}
			h.service.SetTeamRoster(team)

			request := localTuringRequest(h.createSession(t))
			request.RequestedTools = []string{"integrations/github.list_issues"}
			consentRemoteRequest(t, h, request)
			team.err = test.atSend
			runID := sendForRun(t, h, request)
			wantRunRoster(t, h, runID, nil)
		})
	}
}

// The team's tool set is useful only to a worker that can carry the run's
// egress decision. A team-protocol worker too old to validate one is left out
// of it, so the turn goes to the worker that can, without the team, instead of
// being refused for a set nobody can run.
func TestATeamWorkerThatCannotCarryTheDecisionLeavesTheTurnWithoutTheTeam(t *testing.T) {
	h := newHarness(t)
	github := &turingv1.DiscoveredTool{ServerName: "integrations", ToolName: "github.list_issues", Schema: &structpb.Struct{}}
	connectTeamWorker(t, h, "egress-worker", 0, github)
	stale := defaultChatWorkerCapabilities(false)
	stale.TeamProtocolVersion = 1
	stale.RemoteEgressDecisionVersion = 0
	stale.Tools = append(stale.Tools, github, teamDelegateDescriptor)
	connectWorkerWith(t, h, "team-worker", stale)
	createChatGitHubConnection(t, h.repo, 1)
	h.service.SetTeamRoster(&fakeTeam{roster: chatTestRoster})

	request := localTuringRequest(h.createSession(t))
	request.RequestedTools = []string{"integrations/github.list_issues"}
	consentRemoteRequest(t, h, request)
	runID := sendForRun(t, h, request)
	wantRunRoster(t, h, runID, nil)
}

// When no team-protocol worker names team.delegate, the frozen set is today's
// and the parent cannot delegate, so it carries no team.
func TestAConsentedParentWithoutTeamDelegateLosesTheTeam(t *testing.T) {
	h := newHarness(t)
	github := &turingv1.DiscoveredTool{ServerName: "integrations", ToolName: "github.list_issues", Schema: &structpb.Struct{}}
	connectTeamWorker(t, h, "team-worker", 1, github)
	createChatGitHubConnection(t, h.repo, 1)
	h.service.SetTeamRoster(&fakeTeam{roster: chatTestRoster})

	request := localTuringRequest(h.createSession(t))
	request.RequestedTools = []string{"integrations/github.list_issues"}
	consentRemoteRequest(t, h, request)
	runID := sendForRun(t, h, request)
	wantRunRoster(t, h, runID, nil)
}

// A keyed resend replays the run it already made, team and all, even after
// the team changed.
func TestAKeyedResendReplaysTheOriginalTeam(t *testing.T) {
	h := newHarness(t)
	connectTeamWorker(t, h, "team-worker", 1)
	team := &fakeTeam{roster: chatTestRoster}
	h.service.SetTeamRoster(team)
	request := localTuringRequest(h.createSession(t))
	request.IdempotencyKey = "team_replay"

	first := sendForRun(t, h, request)
	team.roster = nil
	second := sendForRun(t, h, request)
	if second != first {
		t.Fatalf("resend queued %q, want the replay of %q", second, first)
	}
	wantRunRoster(t, h, first, chatTestRoster)
}
