package agents

import (
	"context"
	"fmt"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	_ "github.com/mattn/go-sqlite3"
)

// The status code is the whole contract with the client: it is what tells a UI
// to say "that name is taken" rather than "something went wrong".
func TestAgentErrorMapsEachFailureToItsCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"missing agent", repository.ErrExternalAgentNotFound, codes.NotFound},
		{"missing session", repository.ErrSessionNotFound, codes.NotFound},
		{"duplicate name", repository.ErrExternalAgentNameTaken, codes.AlreadyExists},
		{"empty name", repository.ErrExternalAgentNameEmpty, codes.InvalidArgument},
		{"name too long", repository.ErrExternalAgentNameTooLong, codes.InvalidArgument},
		{"empty model", repository.ErrExternalAgentModelEmpty, codes.InvalidArgument},
		{"model too long", repository.ErrExternalAgentModelTooLong, codes.InvalidArgument},
		{"empty base URL", repository.ErrExternalAgentBaseURLEmpty, codes.InvalidArgument},
		{"malformed base URL", repository.ErrExternalAgentBaseURLInvalid, codes.InvalidArgument},
		{"plaintext remote base URL", repository.ErrExternalAgentBaseURLInsecure, codes.InvalidArgument},
		{"empty credential name", repository.ErrExternalAgentCredentialRefEmpty, codes.InvalidArgument},
		{"malformed credential name", repository.ErrExternalAgentCredentialRefFormat, codes.InvalidArgument},
		{"unsupported provider", repository.ErrExternalAgentProviderInvalid, codes.InvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := status.Code(agentError(tt.err, "fallback")); got != tt.want {
				t.Fatalf("code = %v, want %v", got, tt.want)
			}
		})
	}
}

// A storage error must not reach the caller with its text intact — it can name
// tables, paths, or the contents of a failing statement.
func TestAgentErrorHidesUnrecognisedFailures(t *testing.T) {
	err := agentError(fmt.Errorf("no such column: secret_column in /var/data/turing.db"), "create agent failed")

	if got := status.Code(err); got != codes.Internal {
		t.Fatalf("code = %v, want Internal", got)
	}
	message := status.Convert(err).Message()
	if message != "create agent failed" {
		t.Fatalf("message = %q, want the fixed fallback", message)
	}
	if strings.Contains(message, "secret_column") || strings.Contains(message, "turing.db") {
		t.Fatalf("storage detail leaked to the caller: %q", message)
	}
}

func newAgentServer(t *testing.T, credentialNames ...string) (*Server, *repository.Repository, *db.DB, context.Context) {
	t.Helper()
	database := openAgentTestDB(t)
	repo := repository.New(database)
	return New(repo, credentialNames), repo, database, context.Background()
}

func createRequest() *turingv1.CreateExternalAgentRequest {
	return &turingv1.CreateExternalAgentRequest{
		DisplayName:   "Claude",
		Provider:      turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_ANTHROPIC,
		BaseUrl:       "https://api.anthropic.com/v1",
		Model:         "claude-sonnet-4-5",
		CredentialRef: "claude",
	}
}

func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if status.Code(err) != want {
		t.Fatalf("code = %v (%v), want %v", status.Code(err), err, want)
	}
}

func TestCreateExternalAgentReportsWhetherItsKeyExists(t *testing.T) {
	server, _, _, ctx := newAgentServer(t)

	agent, err := server.CreateExternalAgent(ctx, createRequest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Said before the user sends anything. Reporting an agent as ready when
	// its key is missing is exactly the pretending this project refuses.
	if agent.GetCredentialAvailable() {
		t.Fatal("credential_available = true with no keys configured")
	}
	if agent.GetCredentialRef() != "claude" {
		t.Fatalf("credential_ref = %q, want the name back", agent.GetCredentialRef())
	}
}

func TestCreateExternalAgentReportsAKeyItCanFind(t *testing.T) {
	server, _, _, ctx := newAgentServer(t, "claude")

	agent, err := server.CreateExternalAgent(ctx, createRequest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !agent.GetCredentialAvailable() {
		t.Fatal("credential_available = false with a matching key configured")
	}
}

func TestExternalAgentRequestsMapToActionableCodes(t *testing.T) {
	server, repo, _, ctx := newAgentServer(t)
	existing, err := server.CreateExternalAgent(ctx, createRequest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	cases := []struct {
		name string
		call func() error
		want codes.Code
	}{
		{"nil create request", func() error {
			_, err := server.CreateExternalAgent(ctx, nil)
			return err
		}, codes.InvalidArgument},
		{"nil update request", func() error {
			_, err := server.UpdateExternalAgent(ctx, nil)
			return err
		}, codes.InvalidArgument},
		{"nil delete request", func() error {
			_, err := server.DeleteExternalAgent(ctx, nil)
			return err
		}, codes.InvalidArgument},
		{"nil get request", func() error {
			_, err := server.GetSessionAgent(ctx, nil)
			return err
		}, codes.InvalidArgument},
		{"nil set request", func() error {
			_, err := server.SetSessionAgent(ctx, nil)
			return err
		}, codes.InvalidArgument},
		{"nil clear request", func() error {
			_, err := server.ClearSessionAgent(ctx, nil)
			return err
		}, codes.InvalidArgument},
		{"duplicate name", func() error {
			_, err := server.CreateExternalAgent(ctx, createRequest())
			return err
		}, codes.AlreadyExists},
		{"blank name", func() error {
			request := createRequest()
			request.DisplayName = "  "
			_, err := server.CreateExternalAgent(ctx, request)
			return err
		}, codes.InvalidArgument},
		{"blank model", func() error {
			request := createRequest()
			request.DisplayName = "Other"
			request.Model = ""
			_, err := server.CreateExternalAgent(ctx, request)
			return err
		}, codes.InvalidArgument},
		{"plaintext remote endpoint", func() error {
			request := createRequest()
			request.DisplayName = "Other"
			request.BaseUrl = "http://api.anthropic.com/v1"
			_, err := server.CreateExternalAgent(ctx, request)
			return err
		}, codes.InvalidArgument},
		{"credential name with a slash", func() error {
			request := createRequest()
			request.DisplayName = "Other"
			request.CredentialRef = "../secret"
			_, err := server.CreateExternalAgent(ctx, request)
			return err
		}, codes.InvalidArgument},
		// A provider the client did not choose must not be defaulted: the
		// label names the company receiving the conversation.
		{"unspecified provider on create", func() error {
			request := createRequest()
			request.DisplayName = "Other"
			request.Provider = turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_UNSPECIFIED
			_, err := server.CreateExternalAgent(ctx, request)
			return err
		}, codes.InvalidArgument},
		{"unspecified provider on update", func() error {
			_, err := server.UpdateExternalAgent(ctx, &turingv1.UpdateExternalAgentRequest{
				AgentId:       existing.GetAgentId(),
				DisplayName:   "Claude",
				BaseUrl:       "https://api.anthropic.com/v1",
				Model:         "claude-sonnet-4-5",
				CredentialRef: "claude",
			})
			return err
		}, codes.InvalidArgument},
		{"update unknown agent", func() error {
			_, err := server.UpdateExternalAgent(ctx, &turingv1.UpdateExternalAgentRequest{
				AgentId:       "agent_nope",
				DisplayName:   "Nope",
				Provider:      turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_OPENAI,
				BaseUrl:       "https://api.openai.com/v1",
				Model:         "gpt-4o",
				CredentialRef: "openai",
			})
			return err
		}, codes.NotFound},
		{"delete unknown agent", func() error {
			_, err := server.DeleteExternalAgent(ctx, &turingv1.DeleteExternalAgentRequest{AgentId: "agent_nope"})
			return err
		}, codes.NotFound},
		{"route unknown conversation", func() error {
			_, err := server.SetSessionAgent(ctx, &turingv1.SetSessionAgentRequest{
				SessionId: "sess_nope", AgentId: existing.GetAgentId(),
			})
			return err
		}, codes.NotFound},
		{"route to unknown agent", func() error {
			_, err := server.SetSessionAgent(ctx, &turingv1.SetSessionAgentRequest{
				SessionId: session.SessionID, AgentId: "agent_nope",
			})
			return err
		}, codes.NotFound},
		{"read unknown conversation", func() error {
			_, err := server.GetSessionAgent(ctx, &turingv1.GetSessionAgentRequest{SessionId: "sess_nope"})
			return err
		}, codes.NotFound},
		{"clear unknown conversation", func() error {
			_, err := server.ClearSessionAgent(ctx, &turingv1.ClearSessionAgentRequest{SessionId: "sess_nope"})
			return err
		}, codes.NotFound},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assertCode(t, testCase.call(), testCase.want)
		})
	}
}

func TestSessionAgentRoundTripThroughTheService(t *testing.T) {
	server, repo, _, ctx := newAgentServer(t, "claude")
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	agent, err := server.CreateExternalAgent(ctx, createRequest())
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// An absent agent is the local assistant, not a missing field.
	local, err := server.GetSessionAgent(ctx, &turingv1.GetSessionAgentRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if local.GetAgent() != nil {
		t.Fatalf("fresh conversation destination = %v, want none", local.GetAgent())
	}

	routed, err := server.SetSessionAgent(ctx, &turingv1.SetSessionAgentRequest{
		SessionId: session.SessionID, AgentId: agent.GetAgentId(),
	})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if routed.GetAgent().GetAgentId() != agent.GetAgentId() || !routed.GetAgent().GetCredentialAvailable() {
		t.Fatalf("set returned %v, want the routed agent with its credential found", routed.GetAgent())
	}

	read, err := server.GetSessionAgent(ctx, &turingv1.GetSessionAgentRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("get after set: %v", err)
	}
	if read.GetAgent().GetAgentId() != agent.GetAgentId() {
		t.Fatalf("read destination = %v, want the routed agent", read.GetAgent())
	}

	cleared, err := server.ClearSessionAgent(ctx, &turingv1.ClearSessionAgentRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if cleared.GetAgent() != nil {
		t.Fatalf("cleared destination = %v, want none", cleared.GetAgent())
	}
}

func TestListExternalAgentsReturnsEveryFieldButNeverASecret(t *testing.T) {
	server, _, _, ctx := newAgentServer(t, "claude")
	if _, err := server.CreateExternalAgent(ctx, createRequest()); err != nil {
		t.Fatalf("create: %v", err)
	}

	listed, err := server.ListExternalAgents(ctx, &turingv1.ListExternalAgentsRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed.GetAgents()) != 1 {
		t.Fatalf("agents = %d, want 1", len(listed.GetAgents()))
	}
	agent := listed.GetAgents()[0]
	if agent.GetDisplayName() != "Claude" || agent.GetModel() != "claude-sonnet-4-5" ||
		agent.GetBaseUrl() != "https://api.anthropic.com/v1" ||
		agent.GetProvider() != turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_ANTHROPIC {
		t.Fatalf("agent = %v, want every configured field back", agent)
	}
	// The message has no field that could hold a key, and this asserts the
	// wire form stays that way if one is ever added.
	if strings.Contains(agent.String(), "sk-") {
		t.Fatalf("agent message carries something key-shaped: %v", agent)
	}
	fields := agent.ProtoReflect().Descriptor().Fields()
	for index := 0; index < fields.Len(); index++ {
		name := string(fields.Get(index).Name())
		if name == "api_key" || name == "credential" || name == "secret" {
			t.Fatalf("ExternalAgent has a %q field; a third-party key must never reach a client", name)
		}
	}
}

// A row written by a newer build must not be relabelled as a vendor someone
// did not pick.
func TestUnknownStoredProviderIsReportedAsUnspecified(t *testing.T) {
	server, repo, database, ctx := newAgentServer(t)
	if _, err := repo.CreateExternalAgent(ctx, repository.ExternalAgentInput{
		DisplayName: "Claude", Provider: "anthropic",
		BaseURL: "https://api.anthropic.com/v1", Model: "claude-sonnet-4-5", CredentialRef: "claude",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE external_agents SET provider = 'from-the-future'`); err != nil {
		t.Fatalf("rewrite provider: %v", err)
	}

	listed, err := server.ListExternalAgents(ctx, &turingv1.ListExternalAgentsRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := listed.GetAgents()[0].GetProvider(); got != turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_UNSPECIFIED {
		t.Fatalf("provider = %v, want UNSPECIFIED for a value this build does not know", got)
	}
}

func openAgentTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/turing.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}
