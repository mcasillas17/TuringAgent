package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	runtimetestkit "github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/testkit"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/metadata"
)

// fakeLocalEngine is an OpenAI-compatible server on this machine playing one
// delegation: Turing calls the team, then answers once the call returns; the
// specialist answers its brief; Turing's continuation answers from the
// results. The OpenAI wire renames team.delegate, so the engine reads the
// name it was offered.
type fakeLocalEngine struct {
	mu             sync.Mutex
	specialistSeen bool
	continuation   bool
	requests       int
	authorization  []string
	maxTokens      bool
}

func (f *fakeLocalEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var body struct {
		MaxTokens *int `json:"max_tokens"`
		Tools     []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	_ = json.Unmarshal(raw, &body)
	request := string(raw)
	delegate := ""
	for _, tool := range body.Tools {
		if strings.Contains(tool.Function.Name, "delegate") {
			delegate = tool.Function.Name
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	f.authorization = append(f.authorization, r.Header.Get("authorization"))
	f.maxTokens = f.maxTokens || body.MaxTokens != nil
	w.Header().Set("Content-Type", "text/event-stream")
	answer := func(text string) {
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", text)
	}
	switch {
	case strings.Contains(request, "TURING_RETRIEVED_DELEGATION_RESULTS"):
		f.continuation = true
		answer("Research found the notes.")
	case strings.Contains(request, "Gather the notes") && delegate == "":
		f.specialistSeen = true
		answer("Found the notes.")
	case strings.Contains(request, `"role":"tool"`):
		answer("Asked Research to gather the notes.")
	case delegate != "":
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\","+
			"\"function\":{\"name\":%q,\"arguments\":\"{\\\"agent\\\":\\\"research\\\",\\\"task\\\":\\\"Gather the notes\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", delegate)
	default:
		answer("No team.")
	}
}

// The local route on an OpenAI-compatible engine is the local route: a send
// needs no egress consent, Turing delegates through it, the specialist runs
// on it, and the continuation answers from the results. The provider list
// names the engine.
func TestATurnAndADelegatedTaskRunOnAnOpenAICompatibleLocalEngine(t *testing.T) {
	app := newTeamTestAppWithProfile(t, true, "---\nname: Research\ndescription: Looks things up\ntools: [skills_list]\nmemory: none\n---\nYou research.\n")
	engine := &fakeLocalEngine{}
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	publicConn := newBufconnClient(t, app.PublicServer)
	internalConn := newBufconnClient(t, app.InternalServer)
	publicCtx, cancelPublic := context.WithCancel(metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer client"))
	defer cancelPublic()

	workerCtx, stopWorker := context.WithCancel(context.Background())
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- runtimetestkit.RunWorker(workerCtx, runtimetestkit.WorkerConfig{
			Conn: internalConn, RuntimeToken: "internal", WorkerID: "local-engine-runtime",
			MaxConcurrentRuns: 1, MaxToolCallsPerRun: 10,
			OpenAIBaseURL: "http://127.0.0.1:1", OpenAIModel: "unused",
			LocalOpenAIBaseURL: server.URL + "/v1", LocalModel: "llama3.2", LocalEngineLabel: "fake-engine",
		})
	}()
	t.Cleanup(func() {
		stopWorker()
		<-workerDone
	})
	waitFor(t, "the runtime worker to serve the local model", func() bool {
		return app.RuntimeService.ValidateRouting(context.Background(), repository.RoutingRequirements{
			AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
			RequestedTools: []string{repository.TeamDelegateTool}, MinimumTeamProtocolVersion: 1,
		}) == nil
	})

	config, err := turingv1.NewSessionServiceClient(publicConn).GetConfig(publicCtx, &turingv1.GetConfigRequest{})
	if err != nil {
		t.Fatal(err)
	}
	local := config.GetProviders()[0]
	if local.GetProvider() != turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA || len(local.GetModels()) != 1 ||
		local.GetModels()[0].GetEngine() != "fake-engine" || local.GetRequiresPerRunConsent() {
		t.Fatalf("local provider = %+v, want the engine named and no consent", local)
	}

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
	session, err := app.Repository.CreateSession(context.Background(), "Local engine")
	if err != nil {
		t.Fatal(err)
	}
	// No PrepareRemoteEgress and no consent: nothing leaves the machine.
	stream, err := turingv1.NewChatServiceClient(publicConn).SendMessage(publicCtx, &turingv1.SendMessageRequest{
		SessionId: session.SessionID, Content: "prep me for the design review", ContentType: "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
		IdempotencyKey: "local_engine_e2e",
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	parentRunID := queued.GetRunQueued().GetRunId()
	waitFor(t, "the parent to complete", func() bool {
		run, err := app.Repository.GetRun(context.Background(), parentRunID)
		return err == nil && run.Status == "completed"
	})
	if count, err := app.Repository.DelegationCount(context.Background(), parentRunID); err != nil || count != 1 {
		t.Fatalf("delegations = %d, %v; want the model's one call", count, err)
	}
	waitFor(t, "Turing's continuation to answer", func() bool {
		var status string
		err := app.database.QueryRowContext(context.Background(),
			`SELECT status FROM agent_runs WHERE continues_run_id = ?`, parentRunID).Scan(&status)
		return err == nil && status == "completed"
	})
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if !engine.specialistSeen || !engine.continuation || !engine.maxTokens {
		t.Fatalf("engine saw specialist %v, continuation %v, max_tokens %v", engine.specialistSeen, engine.continuation, engine.maxTokens)
	}
	for _, header := range engine.authorization {
		if header != "" {
			t.Fatalf("authorization = %q, want none without a key", header)
		}
	}
}
