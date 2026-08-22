package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	runtimesvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type integrationNotifier struct{ calls atomic.Int32 }

func (n *integrationNotifier) NotifyMCPRegistryChanged(context.Context) error {
	n.calls.Add(1)
	return nil
}

func TestZeroConnectionsZeroToolsThenConnectRevokeReconnect(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)
	notifier := &integrationNotifier{}
	server.SetRegistryChangeNotifier(notifier)
	listed, err := server.ListIntegrationTools(ctx, &turingv1.ListIntegrationToolsRequest{})
	if err != nil || len(listed.GetTools()) != 0 {
		t.Fatalf("initial tools=%+v err=%v", listed, err)
	}
	request := &turingv1.ConnectAccountRequest{Provider: turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB, DisplayName: "First GitHub", Credential: "first-github-token", ConsentAcknowledged: true}
	first, err := server.ConnectAccount(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	listed, err = server.ListIntegrationTools(ctx, &turingv1.ListIntegrationToolsRequest{})
	if err != nil || len(listed.GetTools()) != 4 {
		t.Fatalf("connected tools=%+v err=%v", listed, err)
	}
	if _, err := server.RevokeConnection(ctx, &turingv1.RevokeConnectionRequest{ConnectionId: first.GetConnectionId()}); err != nil {
		t.Fatal(err)
	}
	listed, err = server.ListIntegrationTools(ctx, &turingv1.ListIntegrationToolsRequest{})
	if err != nil || len(listed.GetTools()) != 0 {
		t.Fatalf("revoked tools=%+v err=%v", listed, err)
	}
	request.DisplayName = "Second GitHub"
	request.Credential = "second-github-token"
	if _, err := server.ConnectAccount(ctx, request); err != nil {
		t.Fatal(err)
	}
	listed, err = server.ListIntegrationTools(ctx, &turingv1.ListIntegrationToolsRequest{})
	if err != nil || len(listed.GetTools()) != 4 {
		t.Fatalf("reconnected tools=%+v err=%v", listed, err)
	}
	if notifier.calls.Load() != 3 {
		t.Fatalf("notifications=%d,want 3", notifier.calls.Load())
	}
}

func TestListIntegrationToolsKeylessReturnsEmpty(t *testing.T) {
	database := openIntegrationTestDB(t)
	server := New(repository.New(database), nil, audit.New(repository.New(database)))
	response, err := server.ListIntegrationTools(context.Background(), &turingv1.ListIntegrationToolsRequest{})
	if err != nil || response == nil || len(response.GetTools()) != 0 {
		t.Fatalf("response=%+v err=%v, want successful empty discovery", response, err)
	}
}

type countingSealer struct {
	inner *secretbox.Sealer
	opens atomic.Int32
}

type openHookSealer struct {
	inner     CredentialSealer
	afterOpen func()
}

func (s *openHookSealer) Seal(plain, bound []byte) ([]byte, error) {
	return s.inner.Seal(plain, bound)
}
func (s *openHookSealer) Open(sealed, bound []byte) ([]byte, error) {
	plain, err := s.inner.Open(sealed, bound)
	if err == nil && s.afterOpen != nil {
		s.afterOpen()
	}
	return plain, err
}
func (s *openHookSealer) SealedWithThisKey(header []byte) bool {
	return s.inner.SealedWithThisKey(header)
}

type singleUseApproval struct {
	consumed     atomic.Int32
	connectionID string
	toolName     string
}

type approvalEnforcerFunc func(context.Context, string, string, string, string, map[string]any) error

func (f approvalEnforcerFunc) ConsumeApprovalForThirdParty(ctx context.Context, approvalID, runID, serverName, toolName string, args map[string]any) error {
	return f(ctx, approvalID, runID, serverName, toolName, args)
}

func (a *singleUseApproval) ConsumeApprovalForThirdParty(_ context.Context, approvalID, _ string, serverName, toolName string, args map[string]any) error {
	expectedTool := a.toolName
	if expectedTool == "" {
		expectedTool = "github.list_issues"
	}
	if approvalID != "approval_once" || serverName != "integrations" || toolName != expectedTool || args["connection_id"] != a.connectionID {
		return errors.New("approval context mismatch")
	}
	if a.consumed.Add(1) != 1 {
		return errors.New("approval already consumed")
	}
	return nil
}

func configureHarnessTool(t *testing.T, repo *repository.Repository, database *db.DB, runID, toolName string, endpoints []repository.IntegrationEndpointEgress) {
	t.Helper()
	if err := repo.UpsertTools(context.Background(), []repository.DiscoveredTool{{ServerName: "integrations", ToolName: toolName, SchemaJSON: `{}`, Policy: "approval_required"}}); err != nil {
		t.Fatal(err)
	}
	decision, err := repo.GetRunEgressDecision(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	decision.SelectedTools = []string{"integrations/" + toolName}
	decision.IntegrationEndpoints = endpoints
	selected, _ := json.Marshal(decision.SelectedTools)
	encodedEndpoints, _ := json.Marshal(decision.IntegrationEndpoints)
	if _, err := database.ExecContext(context.Background(), `UPDATE run_egress_decisions SET selected_tools_json=?, integration_endpoints_json=? WHERE run_id=?`, string(selected), string(encodedEndpoints), runID); err != nil {
		t.Fatal(err)
	}
}

func TestApprovalRequiredReadConsumesArgumentBoundApprovalExactlyOnce(t *testing.T) {
	server, repo, _, runID, connectionID := integrationCallHarness(t, "approval-read-token")
	if err := repo.SetToolPolicyByName(context.Background(), "integrations", "github.list_issues", "approval_required"); err != nil {
		t.Fatal(err)
	}
	enforcer := &singleUseApproval{connectionID: connectionID}
	server.SetApprovalEnforcer(enforcer)
	args, _ := structpb.NewStruct(map[string]any{"connection_id": connectionID, "owner": "owner", "repo": "repo"})
	request := &turingv1.CallIntegrationToolRequest{RunId: runID, ApprovalId: "approval_once", ToolName: "github.list_issues", Args: args}
	if _, err := server.CallIntegrationTool(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := server.CallIntegrationTool(context.Background(), request); err == nil {
		t.Fatal("consumed approval was accepted twice")
	}
	if enforcer.consumed.Load() != 2 {
		t.Fatalf("approval attempts=%d, want two with second refused", enforcer.consumed.Load())
	}
}

func TestIntegrationWriteRequiresOneArgumentBoundApprovalIncludingConnectionID(t *testing.T) {
	server, repo, database, runID, firstID := integrationCallHarness(t, "write-credential-a")
	second, err := server.ConnectAccount(context.Background(), &turingv1.ConnectAccountRequest{
		Provider: turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB, DisplayName: "Second GitHub",
		Credential: "write-credential-b", ConsentAcknowledged: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoints := []repository.IntegrationEndpointEgress{
		{Endpoint: repository.GitHubIntegrationEndpoint, EndpointHost: repository.GitHubIntegrationEndpointHost, ConnectionID: firstID, DisplayName: "Personal GitHub", Tools: []string{"github.create_comment"}},
		{Endpoint: repository.GitHubIntegrationEndpoint, EndpointHost: repository.GitHubIntegrationEndpointHost, ConnectionID: second.GetConnectionId(), DisplayName: "Second GitHub", Tools: []string{"github.create_comment"}},
	}
	configureHarnessTool(t, repo, database, runID, "github.create_comment", endpoints)
	argsFor := func(connectionID string) *structpb.Struct {
		args, structErr := structpb.NewStruct(map[string]any{
			"connection_id": connectionID, "owner": "owner", "repo": "repo", "issue_number": float64(7), "body": "complete body",
		})
		if structErr != nil {
			t.Fatal(structErr)
		}
		return args
	}
	request := &turingv1.CallIntegrationToolRequest{RunId: runID, ApprovalId: "approval_once", ToolName: "github.create_comment", Args: argsFor(firstID)}
	if _, err := server.CallIntegrationTool(context.Background(), request); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("write without caller-side approval error=%v", err)
	}

	enforcer := &singleUseApproval{connectionID: firstID, toolName: "github.create_comment"}
	server.SetApprovalEnforcer(enforcer)
	if _, err := server.CallIntegrationTool(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if got := enforcer.consumed.Load(); got != 1 {
		t.Fatalf("approval consumptions=%d, want one", got)
	}
	request.Args = argsFor(second.GetConnectionId())
	if _, err := server.CallIntegrationTool(context.Background(), request); err == nil {
		t.Fatal("approval bound to connection A authorized connection B")
	}
}

func (s *countingSealer) Seal(plain, bound []byte) ([]byte, error) { return s.inner.Seal(plain, bound) }
func (s *countingSealer) Open(sealed, bound []byte) ([]byte, error) {
	s.opens.Add(1)
	return s.inner.Open(sealed, bound)
}
func (s *countingSealer) SealedWithThisKey(header []byte) bool {
	return s.inner.SealedWithThisKey(header)
}

func TestSameConnectionIsUnsealedOncePerCall(t *testing.T) {
	server, repo, database, runID, connectionID := integrationCallHarness(t, "same-connection-token")
	args, _ := structpb.NewStruct(map[string]any{"connection_id": connectionID, "owner": "owner", "repo": "repo"})
	for range 2 {
		if _, err := server.CallIntegrationTool(context.Background(), &turingv1.CallIntegrationToolRequest{RunId: runID, ToolName: "github.list_issues", Args: args}); err != nil {
			t.Fatal(err)
		}
	}
	sealer := server.sealer.(*countingSealer)
	if got := sealer.opens.Load(); got != 2 {
		t.Fatalf("opens=%d, want exactly two for two same-connection calls", got)
	}
	assertPlaintextAbsentFromDatabase(t, context.Background(), database, "same-connection-token")
	_ = repo
}

func TestFullDispatchUsesCredentialNamedByConnectionID(t *testing.T) {
	server, repo, database, runID, firstID := integrationCallHarness(t, "credential-a")
	second, err := server.ConnectAccount(context.Background(), &turingv1.ConnectAccountRequest{Provider: turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB, DisplayName: "Work GitHub", AccountLabel: "work", Credential: "credential-b", ConsentAcknowledged: true})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := repo.GetRunEgressDecision(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	decision.IntegrationEndpoints = append(decision.IntegrationEndpoints, repository.IntegrationEndpointEgress{Endpoint: repository.GitHubIntegrationEndpoint, EndpointHost: repository.GitHubIntegrationEndpointHost, ConnectionID: second.GetConnectionId(), DisplayName: "Work GitHub", Tools: []string{"github.list_issues"}})
	encoded, err := json.Marshal(decision.IntegrationEndpoints)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `UPDATE run_egress_decisions SET integration_endpoints_json = ? WHERE run_id = ?`, string(encoded), runID); err != nil {
		t.Fatal(err)
	}
	server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("Authorization"); got != "Bearer credential-b" {
			t.Fatalf("authorization=%q, want B; first was %s", got, firstID)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[]`)), Header: make(http.Header)}, nil
	})})
	args, _ := structpb.NewStruct(map[string]any{"connection_id": second.GetConnectionId(), "owner": "owner", "repo": "repo"})
	if _, err := server.CallIntegrationTool(context.Background(), &turingv1.CallIntegrationToolRequest{RunId: runID, ToolName: "github.list_issues", Args: args}); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationDispatchRequiresAllFourDecisionLegsBeforeNetwork(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*repository.RunEgressDecision, string)
	}{
		{"selected tool", func(d *repository.RunEgressDecision, _ string) { d.SelectedTools = []string{} }},
		// One category removed at a time: wiping both lets either conjunct of
		// the category check be deleted from validation unnoticed.
		{"argument category alone", func(d *repository.RunEgressDecision, _ string) {
			d.DataCategories = slices.DeleteFunc(slices.Clone(d.DataCategories), func(c string) bool {
				return c == "EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS"
			})
		}},
		{"result category alone", func(d *repository.RunEgressDecision, _ string) {
			d.DataCategories = slices.DeleteFunc(slices.Clone(d.DataCategories), func(c string) bool {
				return c == "EGRESS_DATA_CATEGORY_TOOL_RESULTS"
			})
		}},
		{"connection half of the pair", func(d *repository.RunEgressDecision, _ string) {
			d.IntegrationEndpoints[0].ConnectionID = "conn_other_on_same_host"
		}},
		{"endpoint half of the pair", func(d *repository.RunEgressDecision, _ string) {
			d.IntegrationEndpoints[0].Endpoint = "https://ghe.example.com"
		}},
		{"tool in endpoint entry", func(d *repository.RunEgressDecision, _ string) {
			d.IntegrationEndpoints[0].Tools = []string{"github.get_issue"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, repo, database, runID, connectionID := integrationCallHarness(t, "decision-leg-token")
			decision, err := repo.GetRunEgressDecision(context.Background(), runID)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&decision, connectionID)
			selected, _ := json.Marshal(decision.SelectedTools)
			categories, _ := json.Marshal(decision.DataCategories)
			endpoints, _ := json.Marshal(decision.IntegrationEndpoints)
			if _, err := database.ExecContext(context.Background(), `UPDATE run_egress_decisions SET selected_tools_json=?, data_categories_json=?, integration_endpoints_json=? WHERE run_id=?`, string(selected), string(categories), string(endpoints), runID); err != nil {
				t.Fatal(err)
			}
			var network atomic.Int32
			server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				network.Add(1)
				t.Fatal("network touched before decision validation")
				return nil, nil
			})})
			args, _ := structpb.NewStruct(map[string]any{"connection_id": connectionID, "owner": "owner", "repo": "repo"})
			if _, err := server.CallIntegrationTool(context.Background(), &turingv1.CallIntegrationToolRequest{RunId: runID, ToolName: "github.list_issues", Args: args}); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("error=%v, want PermissionDenied", err)
			}
			if network.Load() != 0 {
				t.Fatalf("network calls=%d", network.Load())
			}
		})
	}
}

func TestAutomationIntegrationReadIsRefusedByMissingEgressDecision(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)
	repo := server.repo
	connection, err := server.ConnectAccount(ctx, &turingv1.ConnectAccountRequest{
		Provider:    turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB,
		DisplayName: "Automation GitHub", Credential: "automation-token", ConsentAcknowledged: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertTools(ctx, []repository.DiscoveredTool{{
		ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{}`, Policy: "approval_required",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetToolPolicyByName(ctx, "integrations", "github.list_issues", "safe"); err != nil {
		t.Fatal(err)
	}
	automation, err := repo.CreateAutomation(ctx, repository.AutomationInput{
		Name: "Read issues", Prompt: "List issues.", Schedule: repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	due, err := time.Parse(time.RFC3339Nano, automation.NextDueAt)
	if err != nil {
		t.Fatal(err)
	}
	fire, found, err := repo.ClaimDueAutomation(ctx, due, repository.AutomationRunDefaults{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil || !found {
		t.Fatalf("claim automation found=%v err=%v", found, err)
	}
	var network atomic.Int32
	server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		network.Add(1)
		return nil, errors.New("network must not be touched")
	})})
	args, _ := structpb.NewStruct(map[string]any{"connection_id": connection.GetConnectionId(), "owner": "owner", "repo": "repo"})
	_, err = server.CallIntegrationTool(ctx, &turingv1.CallIntegrationToolRequest{RunId: fire.RunID, ToolName: "github.list_issues", Args: args})
	if status.Code(err) != codes.PermissionDenied || !strings.Contains(err.Error(), "egress decision") || network.Load() != 0 {
		t.Fatalf("error=%v network=%d, want missing-egress refusal before policy or network", err, network.Load())
	}
}

func TestIntegrationDispatchRevalidatesRunAndConnectionLiveness(t *testing.T) {
	tests := []struct {
		name   string
		revoke func(*Server, *db.DB, string) error
	}{
		{"cancelled run", func(_ *Server, database *db.DB, runID string) error {
			_, err := database.ExecContext(context.Background(), `UPDATE agent_runs SET execution_active=0,status='cancelled' WHERE id=?`, runID)
			return err
		}},
		{"revoked connection", func(server *Server, _ *db.DB, connectionID string) error {
			_, err := server.RevokeConnection(context.Background(), &turingv1.RevokeConnectionRequest{ConnectionId: connectionID})
			return err
		}},
		{"deleted connection", func(server *Server, _ *db.DB, connectionID string) error {
			_, err := server.DeleteConnection(context.Background(), &turingv1.DeleteConnectionRequest{ConnectionId: connectionID})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _, database, runID, connectionID := integrationCallHarness(t, "liveness-token")
			target := runID
			if test.name != "cancelled run" {
				target = connectionID
			}
			if err := test.revoke(server, database, target); err != nil {
				t.Fatal(err)
			}
			var network atomic.Int32
			server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { network.Add(1); return nil, nil })})
			args, _ := structpb.NewStruct(map[string]any{"connection_id": connectionID, "owner": "owner", "repo": "repo"})
			if _, err := server.CallIntegrationTool(context.Background(), &turingv1.CallIntegrationToolRequest{RunId: runID, ToolName: "github.list_issues", Args: args}); err == nil {
				t.Fatal("dispatch succeeded after revocation")
			}
			if network.Load() != 0 {
				t.Fatalf("network calls=%d", network.Load())
			}
		})
	}
}

func TestIntegrationDispatchRevalidatesChangesMadeDuringApprovalWait(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(context.Context, *repository.Repository, *db.DB, string) error
	}{
		{name: "cancelled run", mutate: func(ctx context.Context, _ *repository.Repository, database *db.DB, runID string) error {
			_, err := database.ExecContext(ctx, `UPDATE agent_runs SET execution_active=0,status='cancelled' WHERE id=?`, runID)
			return err
		}},
		{name: "disabled tool", mutate: func(ctx context.Context, repo *repository.Repository, _ *db.DB, _ string) error {
			return repo.SetToolPolicyByName(ctx, "integrations", "github.list_issues", "disabled")
		}},
		{name: "deleting session", mutate: func(ctx context.Context, _ *repository.Repository, database *db.DB, runID string) error {
			_, err := database.ExecContext(ctx, `UPDATE sessions SET deletion_state='deleting' WHERE id=(SELECT session_id FROM agent_runs WHERE id=?)`, runID)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, repo, database, runID, connectionID := integrationCallHarness(t, "approval-race-token")
			if err := repo.SetToolPolicyByName(context.Background(), "integrations", "github.list_issues", "approval_required"); err != nil {
				t.Fatal(err)
			}
			server.SetApprovalEnforcer(approvalEnforcerFunc(func(ctx context.Context, _, gotRunID, _, _ string, _ map[string]any) error {
				return test.mutate(ctx, repo, database, gotRunID)
			}))
			var network atomic.Int32
			server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				network.Add(1)
				return nil, errors.New("network must not be touched")
			})})
			args, _ := structpb.NewStruct(map[string]any{"connection_id": connectionID, "owner": "owner", "repo": "repo"})
			_, err := server.CallIntegrationTool(context.Background(), &turingv1.CallIntegrationToolRequest{
				RunId: runID, ApprovalId: "approval", ToolName: "github.list_issues", Args: args,
			})
			if err == nil || network.Load() != 0 {
				t.Fatalf("error=%v network=%d, want post-approval refusal before network", err, network.Load())
			}
		})
	}
}

func TestIntegrationDispatchRevalidatesAfterUnsealImmediatelyBeforeNetwork(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(context.Context, *Server, *repository.Repository, string) error
	}{
		{name: "revoked connection", mutate: func(ctx context.Context, server *Server, _ *repository.Repository, connectionID string) error {
			_, err := server.RevokeConnection(ctx, &turingv1.RevokeConnectionRequest{ConnectionId: connectionID})
			return err
		}},
		{name: "disabled tool", mutate: func(ctx context.Context, _ *Server, repo *repository.Repository, _ string) error {
			return repo.SetToolPolicyByName(ctx, "integrations", "github.list_issues", "disabled")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, repo, _, runID, connectionID := integrationCallHarness(t, "post-unseal-race-token")
			var mutateErr error
			server.sealer = &openHookSealer{inner: server.sealer, afterOpen: func() {
				mutateErr = test.mutate(context.Background(), server, repo, connectionID)
			}}
			var network atomic.Int32
			server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				network.Add(1)
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[]`)), Header: make(http.Header)}, nil
			})})
			args, _ := structpb.NewStruct(map[string]any{"connection_id": connectionID, "owner": "owner", "repo": "repo"})
			_, err := server.CallIntegrationTool(context.Background(), &turingv1.CallIntegrationToolRequest{
				RunId: runID, ToolName: "github.list_issues", Args: args,
			})
			if mutateErr != nil {
				t.Fatal(mutateErr)
			}
			if err == nil || network.Load() != 0 {
				t.Fatalf("error=%v network=%d, want post-unseal refusal immediately before network", err, network.Load())
			}
		})
	}
}

func integrationCallHarness(t *testing.T, credential string) (*Server, *repository.Repository, *db.DB, string, string) {
	t.Helper()
	database := openIntegrationTestDB(t)
	repo := repository.New(database)
	realSealer, err := secretbox.New(bytes.Repeat([]byte{0x52}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	server := New(repo, &countingSealer{inner: realSealer}, audit.New(repo))
	connection, err := server.ConnectAccount(context.Background(), &turingv1.ConnectAccountRequest{Provider: turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB, DisplayName: "Personal GitHub", AccountLabel: "me", Credential: credential, ConsentAcknowledged: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertTools(context.Background(), []repository.DiscoveredTool{{ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{}`, Policy: "approval_required"}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetToolPolicyByName(context.Background(), "integrations", "github.list_issues", "safe"); err != nil {
		t.Fatal(err)
	}
	session, err := repo.CreateSession(context.Background(), "integration call")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := repo.EgressSkillSnapshotFingerprint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pending := &repository.PendingEgressDecision{Version: repository.RunEgressDecisionVersion, ChallengeNonce: "nonce-" + connection.GetConnectionId(), ChallengeFingerprint: "fingerprint", RequestDigest: "digest", Provider: "ollama", Model: "llama", DataCategories: []string{"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS", "EGRESS_DATA_CATEGORY_TOOL_RESULTS"}, SelectedTools: []string{"integrations/github.list_issues"}, SkillSnapshotFingerprint: fingerprint, ConsentGrantedAt: repository.FormatTimestamp(time.Now().UTC()), IntegrationEndpoints: []repository.IntegrationEndpointEgress{{Endpoint: repository.GitHubIntegrationEndpoint, EndpointHost: repository.GitHubIntegrationEndpointHost, ConnectionID: connection.GetConnectionId(), DisplayName: connection.GetDisplayName(), Tools: []string{"github.list_issues"}}}}
	enqueued, err := repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{SessionID: session.SessionID, Content: "test", ContentType: "text", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama", SelectedTools: []string{"integrations/github.list_issues"}, EgressDecision: pending})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(context.Background(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `UPDATE agent_runs SET execution_active = 1 WHERE id = ?`, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[]`)), Header: make(http.Header)}, nil
	})})
	return server, repo, database, enqueued.RunID, connection.GetConnectionId()
}

func TestGitHubCredentialTravelsOnlyInAuthorizationHeader(t *testing.T) {
	server, _, _ := newIntegrationServer(t)
	credential := "credential-for-connection-b"
	server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("Authorization"); got != "Bearer "+credential {
			t.Fatalf("authorization = %q", got)
		}
		if strings.Contains(request.URL.String(), credential) {
			t.Fatalf("credential leaked into URL %q", request.URL.String())
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[]`)), Header: make(http.Header)}, nil
	})})
	result, err := server.callGitHub(context.Background(), "github.list_issues", map[string]any{
		"connection_id": "conn_b", "owner": "owner", "repo": "repo",
	}, []byte(credential))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result, credential) {
		t.Fatalf("credential leaked into result %q", result)
	}
}

func TestSuccessfulGitHubResponseCannotReflectCredentialIntoToolResult(t *testing.T) {
	for _, credential := range []string{"successful-response-reflection-secret", "[REDACTED]", strings.Repeat("*", 16)} {
		t.Run(credential, func(t *testing.T) {
			server, _, _ := newIntegrationServer(t)
			server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body := `{"authorization":` + strconv.Quote(request.Header.Get("Authorization")) + `,"token":` + strconv.Quote(credential) + `}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})})
			result, err := server.callGitHub(context.Background(), "github.list_issues", map[string]any{
				"connection_id": "conn", "owner": "owner", "repo": "repo",
			}, []byte(credential))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result, credential) {
				t.Fatalf("provider reflected credential into result %q", result)
			}
		})
	}
}

func TestGitHubHTTPFailureDoesNotEchoCredential(t *testing.T) {
	server, _, _ := newIntegrationServer(t)
	credential := "status-failure-secret"
	server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(credential)), Header: make(http.Header)}, nil
	})})
	_, err := server.callGitHub(context.Background(), "github.list_issues", map[string]any{
		"connection_id": "conn", "owner": "owner", "repo": "repo",
	}, []byte(credential))
	if err == nil || strings.Contains(err.Error(), credential) {
		t.Fatalf("error = %v", err)
	}
}

func TestFullCallFailuresNeverPersistReportOrLogPlaintextCredential(t *testing.T) {
	credential := "full-cycle-credential-hygiene-secret"
	server, _, database, runID, connectionID := integrationCallHarness(t, credential)
	args, _ := structpb.NewStruct(map[string]any{"connection_id": connectionID, "owner": "owner", "repo": "repo"})
	request := &turingv1.CallIntegrationToolRequest{RunId: runID, ToolName: "github.list_issues", Args: args}

	var logs bytes.Buffer
	originalWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(originalWriter) })

	server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(credential)), Header: make(http.Header)}, nil
	})})
	statusResponse, statusErr := server.CallIntegrationTool(context.Background(), request)
	if statusErr == nil || statusResponse != nil || strings.Contains(statusErr.Error(), credential) {
		t.Fatalf("status response=%+v error=%v", statusResponse, statusErr)
	}

	server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"token":` + strconv.Quote(credential) + `}`)), Header: make(http.Header)}, nil
	})})
	successResponse, successErr := server.CallIntegrationTool(context.Background(), request)
	if successErr != nil || successResponse == nil || strings.Contains(successResponse.GetResult().String(), credential) {
		t.Fatalf("success response=%+v error=%v leaked provider-reflected credential", successResponse, successErr)
	}

	server.SetHTTPClient(&http.Client{Transport: &http.Transport{Proxy: nil, DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("dial failed before TLS")
	}}})
	dialResponse, dialErr := server.CallIntegrationTool(context.Background(), request)
	if dialErr == nil || dialResponse != nil || strings.Contains(dialErr.Error(), credential) {
		t.Fatalf("dial response=%+v error=%v", dialResponse, dialErr)
	}
	if strings.Contains(logs.String(), credential) {
		t.Fatalf("credential reached logs: %q", logs.String())
	}
	assertPlaintextAbsentFromDatabase(t, context.Background(), database, credential)
}

func TestProviderReflectedCredentialCannotReachRuntimeEventOrAudit(t *testing.T) {
	const credential = "runtime-result-reflection-secret"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	database := openIntegrationTestDB(t)
	repo := repository.New(database)
	sealer, err := secretbox.New(bytes.Repeat([]byte{0x62}, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	integrationServer := New(repo, sealer, audit.New(repo))
	connection, err := integrationServer.ConnectAccount(ctx, &turingv1.ConnectAccountRequest{
		Provider: turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB, DisplayName: "Runtime GitHub",
		Credential: credential, ConsentAcknowledged: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertTools(ctx, []repository.DiscoveredTool{{
		ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{}`, Policy: "approval_required",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetToolPolicyByName(ctx, "integrations", "github.list_issues", "safe"); err != nil {
		t.Fatal(err)
	}
	session, err := repo.CreateSession(ctx, "credential event hygiene")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := repo.EgressSkillSnapshotFingerprint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pending := &repository.PendingEgressDecision{
		Version: repository.RunEgressDecisionVersion, ChallengeNonce: "event-hygiene-nonce",
		ChallengeFingerprint: "event-hygiene-fingerprint", RequestDigest: "event-hygiene-digest",
		Provider: "ollama", Model: "llama3.2", ConsentGrantedAt: repository.FormatTimestamp(time.Now().UTC()),
		DataCategories: []string{"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS", "EGRESS_DATA_CATEGORY_TOOL_RESULTS"},
		SelectedTools:  []string{"integrations/github.list_issues"}, SkillSnapshotFingerprint: fingerprint,
		IntegrationEndpoints: []repository.IntegrationEndpointEgress{{
			Endpoint: repository.GitHubIntegrationEndpoint, EndpointHost: repository.GitHubIntegrationEndpointHost,
			ConnectionID: connection.GetConnectionId(), DisplayName: connection.GetDisplayName(), Tools: []string{"github.list_issues"},
		}},
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "list issues", ContentType: "text",
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
		SelectedTools: []string{"integrations/github.list_issues"}, EgressDecision: pending,
	})
	if err != nil {
		t.Fatal(err)
	}
	runtimeServer := runtimesvc.NewWithConfig(repo, events.NewBus(16), runtimesvc.DispatchConfig{})
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	turingv1.RegisterRuntimeServiceServer(grpcServer, runtimeServer)
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		runtimeServer.WaitForWorkerStreams()
	})
	grpcConn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = grpcConn.Close() })
	stream, err := turingv1.NewRuntimeServiceClient(grpcConn).ConnectWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "worker-event-hygiene", RegistrationId: "registration-event-hygiene",
		Capabilities: &turingv1.WorkerCapabilities{
			Models:   []*turingv1.ModelCapability{{Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2", MaxContextTokens: 8192}},
			AgentIds: []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT}, MaxConcurrentRuns: 1,
			RemoteEgressDecisionVersion: repository.RunEgressDecisionVersion,
			Tools:                       []*turingv1.DiscoveredTool{{ServerName: "integrations", ToolName: "github.list_issues", Schema: &structpb.Struct{}}},
		},
	}}}); err != nil {
		t.Fatal(err)
	}
	var job *turingv1.AgentJob
	for job == nil {
		command, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		job = command.GetRunAssigned()
	}
	if job.GetRunId() != enqueued.RunID {
		t.Fatalf("assigned run=%q, want %q", job.GetRunId(), enqueued.RunID)
	}
	args, _ := structpb.NewStruct(map[string]any{
		"connection_id": connection.GetConnectionId(), "owner": "owner", "repo": "repo",
	})
	before := &turingv1.ToolCallBeacon{
		RunId: job.GetRunId(), TraceId: job.GetTraceId(), ToolCallId: "call_event_hygiene",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "integrations",
		ToolName: "github.list_issues", Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: args,
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: before}}); err != nil {
		t.Fatal(err)
	}
	for {
		command, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if decision := command.GetToolPolicyDecision(); decision != nil && decision.GetToolCallId() == before.GetToolCallId() {
			if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW || !decision.GetReadOnly() {
				t.Fatalf("before decision=%+v, want safe read", decision)
			}
			break
		}
	}
	integrationServer.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"authorization":` + strconv.Quote(request.Header.Get("Authorization")) + `}`)), Header: make(http.Header)}, nil
	})})
	response, err := integrationServer.CallIntegrationTool(ctx, &turingv1.CallIntegrationToolRequest{
		RunId: job.GetRunId(), ToolName: "github.list_issues", Args: args,
	})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := json.Marshal(response.GetResult().AsMap())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(summary), credential) {
		t.Fatalf("credential survived dispatch result %q", summary)
	}
	after := proto.Clone(before).(*turingv1.ToolCallBeacon)
	after.Phase = turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER
	after.Status = turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED
	after.ResultSummary = string(summary)
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: after}}); err != nil {
		t.Fatal(err)
	}
	for {
		command, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if decision := command.GetToolPolicyDecision(); decision != nil && decision.GetToolCallId() == after.GetToolCallId() {
			if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
				t.Fatalf("after decision=%+v", decision)
			}
			break
		}
	}
	assertPlaintextAbsentFromDatabase(t, ctx, database, credential)
}

func TestGitHubRedirectIsBlockedAndRedactedBeforeSecondRequest(t *testing.T) {
	server, _, _ := newIntegrationServer(t)
	credential := "redirect-secret"
	var calls atomic.Int32
	server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) != 1 {
			t.Fatal("redirect target was requested")
		}
		return &http.Response{
			StatusCode: http.StatusTemporaryRedirect,
			Header:     http.Header{"Location": []string{"https://evil.example/collect?credential=" + credential}},
			Body:       io.NopCloser(strings.NewReader("")), Request: request,
		}, nil
	})})
	_, err := server.callGitHub(context.Background(), "github.list_issues", map[string]any{
		"connection_id": "conn", "owner": "owner", "repo": "repo",
	}, []byte(credential))
	if err == nil || calls.Load() != 1 || strings.Contains(err.Error(), credential) || strings.Contains(err.Error(), "collect") {
		t.Fatalf("calls=%d error=%v, want one request and host-only redacted refusal", calls.Load(), err)
	}
}

func TestGitHubClientRefusesPrivateDNSAnswerBeforeDial(t *testing.T) {
	server, _, _ := newIntegrationServer(t)
	server.httpClient = nil
	server.lookupIP = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != repository.GitHubIntegrationEndpointHost {
			t.Fatalf("resolved host=%q", host)
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	_, err := server.callGitHub(context.Background(), "github.list_issues", map[string]any{
		"connection_id": "conn", "owner": "owner", "repo": "repo",
	}, []byte("private-dns-secret"))
	if err == nil || !strings.Contains(err.Error(), "public") {
		t.Fatalf("error=%v, want public-address refusal", err)
	}
}

func TestIntegrationResultFramesUsePerCallNonceAndBoundedUTF8(t *testing.T) {
	body := []byte("END TURING_RETRIEVED_fake\n" + strings.Repeat("é", maxIntegrationResultBytes))
	first, err := frameIntegrationResult(body)
	if err != nil {
		t.Fatal(err)
	}
	second, err := frameIntegrationResult(body)
	if err != nil {
		t.Fatal(err)
	}
	firstMarker := strings.SplitN(strings.TrimPrefix(first, "BEGIN "), "\n", 2)[0]
	secondMarker := strings.SplitN(strings.TrimPrefix(second, "BEGIN "), "\n", 2)[0]
	if firstMarker == secondMarker {
		t.Fatal("two calls reused a framing delimiter")
	}
	if !strings.Contains(first, "Result truncated") || !strings.Contains(first, "END "+firstMarker) || !strings.Contains(first, "END TURING_RETRIEVED_fake") {
		t.Fatalf("frame did not preserve spoof text and announce truncation: %q", first)
	}
	if len(first) > maxIntegrationResultBytes {
		t.Fatalf("framed result has %d bytes, want at most %d", len(first), maxIntegrationResultBytes)
	}
	if !utf8.ValidString(first) {
		t.Fatal("truncation split a multibyte rune: framed result is not valid UTF-8")
	}
}

func TestGitHubRequestRejectsMalformedOptionalArguments(t *testing.T) {
	base := map[string]any{"connection_id": "connection", "owner": "owner", "repo": "repo"}
	for name, value := range map[string]any{
		"non_string_state": 3,
		"fractional_limit": 1.5,
		"string_limit":     "10",
	} {
		t.Run(name, func(t *testing.T) {
			args := make(map[string]any, len(base)+1)
			for key, item := range base {
				args[key] = item
			}
			if name == "non_string_state" {
				args["state"] = value
			} else {
				args["limit"] = value
			}
			if _, _, _, err := githubRequest("github.list_issues", args); err == nil {
				t.Fatal("malformed optional argument was treated as absent")
			}
		})
	}
}

// The tool descriptions are how the model learns valid connection ids; an
// implementation that enumerated a fake or nothing at all previously passed
// every test because the Execute-path test wrote its own description.
func TestListIntegrationToolsNamesConnectionsAndOmitsDisabled(t *testing.T) {
	server, database, ctx := newIntegrationServer(t)
	repo := repository.New(database)
	connected, err := server.ConnectAccount(ctx, &turingv1.ConnectAccountRequest{
		Provider:    turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB,
		DisplayName: "Work GitHub", Credential: "work-token", ConsentAcknowledged: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	personal, err := server.ConnectAccount(ctx, &turingv1.ConnectAccountRequest{
		Provider:    turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB,
		DisplayName: "Personal GitHub", Credential: "personal-token", ConsentAcknowledged: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := server.ListIntegrationTools(ctx, &turingv1.ListIntegrationToolsRequest{})
	if err != nil || len(listed.GetTools()) != 4 {
		t.Fatalf("tools=%+v err=%v", listed, err)
	}
	// Two connections, so a lister that emits only pairs[0] fails.
	for _, want := range []string{
		"(" + connected.GetConnectionId() + ", Work GitHub)",
		"(" + personal.GetConnectionId() + ", Personal GitHub)",
	} {
		for _, tool := range listed.GetTools() {
			if !strings.Contains(tool.GetDescription(), "Available connections: ") ||
				!strings.Contains(tool.GetDescription(), want) {
				t.Fatalf("tool %s description %q does not enumerate %q",
					tool.GetToolName(), tool.GetDescription(), want)
			}
		}
	}
	if err := repo.UpsertTools(ctx, []repository.DiscoveredTool{{
		ServerName: "integrations", ToolName: "github.get_file", SchemaJSON: `{}`, Policy: "approval_required",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetToolPolicyByName(ctx, "integrations", "github.get_file", "disabled"); err != nil {
		t.Fatal(err)
	}
	listed, err = server.ListIntegrationTools(ctx, &turingv1.ListIntegrationToolsRequest{})
	if err != nil || len(listed.GetTools()) != 3 {
		t.Fatalf("tools after disable=%+v err=%v, want the disabled tool filtered", listed, err)
	}
	for _, tool := range listed.GetTools() {
		if tool.GetToolName() == "github.get_file" {
			t.Fatal("disabled tool still offered by ListIntegrationTools")
		}
	}
}

// A missing TURING_INTEGRATION_KEY and a rotated key are different failures
// with different remedies; telling a keyless operator to "reconnect" loops
// them through a flow that cannot succeed.
func TestIntegrationDispatchWithoutKeyNamesTheMissingKeyNotReconnect(t *testing.T) {
	server, _, _, runID, connectionID := integrationCallHarness(t, "keyless-token")
	server.sealer = nil
	args, _ := structpb.NewStruct(map[string]any{"connection_id": connectionID, "owner": "owner", "repo": "repo"})
	_, err := server.CallIntegrationTool(context.Background(), &turingv1.CallIntegrationToolRequest{
		RunId: runID, ToolName: "github.list_issues", Args: args,
	})
	if err == nil || !strings.Contains(err.Error(), "TURING_INTEGRATION_KEY") {
		t.Fatalf("error=%v, want the unconfigured-key message", err)
	}
	if err != nil && strings.Contains(err.Error(), "reconnect") {
		t.Fatalf("error=%v, told a keyless operator to reconnect", err)
	}
}
