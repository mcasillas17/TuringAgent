package team

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testEgressSecret = "egress-secret"

// consentTask is a task Research was given that waits for the user's consent
// to a GitHub tool, with GitHub connected; requires, when set, is the
// profile's requires pattern.
func (h *harness) consentTask(t *testing.T, requires string) (delegating, string) {
	t.Helper()
	h.routes.tools[defaultModel] = append(slices.Clone(builtinTools), "integrations/github.get_issue")
	frontmatter := strings.Replace(researchFrontmatter, "system.time]", "system.time, github.get_issue]", 1)
	if requires != "" {
		frontmatter += "requires: [" + requires + "]\n"
	}
	d := h.delegatingRunAs(t, "safe", frontmatter)
	h.connectGitHub(t, "conn_1")
	h.server.SetEgressSigningSecret(testEgressSecret)
	response, err := h.delegate(d, "call_1", "", researchTask)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := response.GetResult().AsMap()["delegation_id"].(string)
	return d, id
}

func (h *harness) connectGitHub(t *testing.T, connectionID string) {
	t.Helper()
	if _, err := h.repo.CreateConnection(context.Background(), repository.NewConnection{
		ConnectionID: connectionID, Provider: "github", DisplayName: "GitHub " + connectionID,
		CredentialCiphertext: []byte{1}, CredentialHint: "••••token", GrantedScopes: []string{"Read repository data."},
	}); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) prepare(t *testing.T, id string) *turingv1.RemoteEgressDisclosure {
	t.Helper()
	response, err := h.server.PrepareDelegationEgress(context.Background(), &turingv1.PrepareDelegationEgressRequest{DelegationId: id})
	if err != nil {
		t.Fatal(err)
	}
	return response.GetDisclosure()
}

func allow(disclosure *turingv1.RemoteEgressDisclosure) *turingv1.RemoteEgressConsent {
	return &turingv1.RemoteEgressConsent{
		Challenge: disclosure.GetChallenge(), Acknowledged: true,
		AcknowledgedDataCategories: slices.Clone(disclosure.GetDataCategories()),
	}
}

func (h *harness) decide(id string, consent *turingv1.RemoteEgressConsent) (*turingv1.Delegation, error) {
	return h.server.DecideDelegationEgress(context.Background(), &turingv1.DecideDelegationEgressRequest{DelegationId: id, Consent: consent})
}

func wantCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if status.Code(err) != code {
		t.Fatalf("error = %v, want %v", err, code)
	}
}

// The disclosure says where the task's tool calls may go — GitHub, through
// the connected account — which of its tools may reach it, and that only
// tool arguments and results may leave: the specialist's model is local.
func TestPreparingDisclosesWhereATasksToolCallsMayGo(t *testing.T) {
	h := newHarness(t)
	_, id := h.consentTask(t, "")
	h.routes.validated = nil

	disclosure := h.prepare(t, id)
	wantTools := []string{
		"files/files.read", "files/files.write", "integrations/github.get_issue", "memory/memory.read",
		"memory/memory.search", "skills/skill_view", "skills/skills_list", "system/system.time",
	}
	wantCategories := []turingv1.EgressDataCategory{
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS,
	}
	if disclosure.GetProvider() != turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA || disclosure.GetModel() != defaultModel ||
		disclosure.GetEndpoint() != "" || disclosure.GetChallenge() == "" ||
		!slices.Equal(disclosure.GetSelectedTools(), wantTools) ||
		!slices.Equal(disclosure.GetDataCategories(), wantCategories) ||
		len(disclosure.GetMemoryNotes()) != 0 || disclosure.GetMemoryProfileMayBeSent() {
		t.Fatalf("disclosure = %+v", disclosure)
	}
	if endpoints := disclosure.GetIntegrationEndpoints(); len(endpoints) != 1 ||
		endpoints[0].GetEndpointHost() != "api.github.com" || endpoints[0].GetConnectionId() != "conn_1" ||
		!slices.Equal(endpoints[0].GetTools(), []string{"github.get_issue"}) {
		t.Fatalf("integration endpoints = %+v", endpoints)
	}
	if left := time.Until(disclosure.GetExpiresAt().AsTime()); left <= 0 || left > 5*time.Minute {
		t.Fatalf("challenge expires in %v", left)
	}
	if len(h.routes.validated) != 1 || !h.routes.validated[0].RemoteEgressDecision ||
		!slices.Equal(h.routes.validated[0].SelectedTools, wantTools) || h.routes.validated[0].MinimumTeamProtocolVersion != 1 {
		t.Fatalf("validated routes = %+v, want the task's whole route with a decision", h.routes.validated)
	}
}

// Allowing records the decision on the task's own run, lets a worker take it,
// and tells the turn's stream.
func TestAllowingLetsTheTaskRunWithItsTools(t *testing.T) {
	h := newHarness(t)
	d, id := h.consentTask(t, "")
	disclosure := h.prepare(t, id)
	d.published.published = nil
	dispatched := h.routes.dispatched

	delegation, err := h.decide(id, allow(disclosure))
	if err != nil {
		t.Fatal(err)
	}
	if delegation.GetEgressConsent() != "granted" || delegation.GetState() != "queued" {
		t.Fatalf("delegation = %+v, want consent granted", delegation)
	}
	decision, err := h.repo.GetRunEgressDecision(context.Background(), delegation.GetChildRunId())
	if err != nil || !slices.Equal(decision.SelectedTools, disclosure.GetSelectedTools()) ||
		len(decision.IntegrationEndpoints) != 1 || decision.IntegrationEndpoints[0].ConnectionID != "conn_1" {
		t.Fatalf("decision = %+v, %v; want the disclosed set on the task's run", decision, err)
	}
	if h.routes.dispatched == dispatched {
		t.Fatal("allowing did not ask a worker to take the task")
	}
	updated := false
	for _, event := range d.published.published {
		updated = updated || event.Type == "delegation.updated"
	}
	if !updated {
		t.Fatal("allowing was not reported on the turn's stream")
	}
}

// A remote MCP server's tool is disclosed by its server and endpoint, and
// allowing it freezes that destination onto the task's run with the tool in
// the run's set; the run's own tools stay out of it until then.
func TestAllowingARemoteMCPServerTool(t *testing.T) {
	h := newHarness(t)
	h.registerRemoteServer(t, "notes", "notes.search")
	h.routes.tools[defaultModel] = append(slices.Clone(builtinTools), "notes/notes.search")
	d := h.delegatingRunAs(t, "safe", strings.Replace(researchFrontmatter, "system.time]", "system.time, notes.search]", 1))
	h.server.SetEgressSigningSecret(testEgressSecret)
	response, err := h.delegate(d, "call_1", "", researchTask)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := response.GetResult().AsMap()["delegation_id"].(string)
	record, err := h.repo.GetDelegation(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if selection, err := h.repo.RunToolSelection(context.Background(), record.ChildRunID); err != nil ||
		!selection.Enforced || selection.Allows("notes/notes.search") {
		t.Fatalf("selection before consent = %+v, %v; want it enforced without the remote tool", selection, err)
	}

	disclosure := h.prepare(t, id)
	if remotes := disclosure.GetRemoteMcpServers(); len(remotes) != 1 || remotes[0].GetServerName() != "notes" ||
		remotes[0].GetEndpointHost() != "notes.example.test" || remotes[0].GetEndpoint() != "https://notes.example.test/mcp" ||
		len(disclosure.GetIntegrationEndpoints()) != 0 || !slices.Contains(disclosure.GetSelectedTools(), "notes/notes.search") {
		t.Fatalf("disclosure = %+v, want the notes server and its tool", disclosure)
	}
	if _, err := h.decide(id, allow(disclosure)); err != nil {
		t.Fatal(err)
	}
	decision, err := h.repo.GetRunEgressDecision(context.Background(), record.ChildRunID)
	if err != nil || len(decision.RemoteMCPServers) != 1 || decision.RemoteMCPServers[0].ServerName != "notes" ||
		decision.RemoteMCPServers[0].EndpointHost != "notes.example.test" {
		t.Fatalf("decision = %+v, %v; want the notes server frozen on the task's run", decision, err)
	}
	if selection, err := h.repo.RunToolSelection(context.Background(), record.ChildRunID); err != nil ||
		!selection.Allows("notes/notes.search") || !selection.Allows("system/system.time") {
		t.Fatalf("selection after consent = %+v, %v; want the remote tool beside the local ones", selection, err)
	}
}

// A consent is accepted only as the disclosure it answers: acknowledged, for
// this task, unexpired, signed by this flow, over exactly the categories it
// disclosed, and describing the task as it still stands. Anything else is
// refused, and the task keeps waiting.
func TestAllowingRefusesAConsentThatDoesNotAnswerItsDisclosure(t *testing.T) {
	type refusal = func(t *testing.T, h *harness, id string, disclosure *turingv1.RemoteEgressDisclosure) *turingv1.RemoteEgressConsent
	// Each refusal says why, so the user knows whether to review the task
	// again; the message is what the card shows.
	messages := map[string]string{
		"not acknowledged": "not acknowledged", "fewer categories": "does not answer", "garbage": "invalid",
		"expired": "expired", "another task": "does not answer", "a send's domain": "invalid",
		"a second connection since": "changed since", "no secret, forged": "not configured",
		"an unknown field": "invalid",
	}
	cases := map[string]refusal{
		"no secret, forged": func(t *testing.T, h *harness, _ string, disclosure *turingv1.RemoteEgressDisclosure) *turingv1.RemoteEgressConsent {
			h.server.SetEgressSigningSecret("")
			payload, err := json.Marshal(openTestChallenge(t, disclosure.GetChallenge()))
			if err != nil {
				t.Fatal(err)
			}
			consent := allow(disclosure)
			consent.Challenge = backendegress.SignChallenge("", delegationEgressChallengeDomain, payload)
			return consent
		},
		"not acknowledged": func(_ *testing.T, _ *harness, _ string, disclosure *turingv1.RemoteEgressDisclosure) *turingv1.RemoteEgressConsent {
			consent := allow(disclosure)
			consent.Acknowledged = false
			return consent
		},
		"fewer categories": func(_ *testing.T, _ *harness, _ string, disclosure *turingv1.RemoteEgressDisclosure) *turingv1.RemoteEgressConsent {
			consent := allow(disclosure)
			consent.AcknowledgedDataCategories = consent.AcknowledgedDataCategories[:1]
			return consent
		},
		"garbage": func(_ *testing.T, _ *harness, _ string, disclosure *turingv1.RemoteEgressDisclosure) *turingv1.RemoteEgressConsent {
			consent := allow(disclosure)
			consent.Challenge = "not.a-challenge"
			return consent
		},
		"expired": func(_ *testing.T, h *harness, _ string, disclosure *turingv1.RemoteEgressDisclosure) *turingv1.RemoteEgressConsent {
			h.server.now = func() time.Time { return time.Now().Add(time.Hour) }
			return allow(disclosure)
		},
		"another task": func(t *testing.T, h *harness, id string, disclosure *turingv1.RemoteEgressDisclosure) *turingv1.RemoteEgressConsent {
			payload := openTestChallenge(t, disclosure.GetChallenge())
			payload["delegation_id"] = "dlg_other"
			return signTestChallenge(t, disclosure, payload, delegationEgressChallengeDomain)
		},
		"an unknown field": func(t *testing.T, _ *harness, _ string, disclosure *turingv1.RemoteEgressDisclosure) *turingv1.RemoteEgressConsent {
			payload := openTestChallenge(t, disclosure.GetChallenge())
			payload["remote_egress"] = true
			return signTestChallenge(t, disclosure, payload, delegationEgressChallengeDomain)
		},
		"a send's domain": func(t *testing.T, h *harness, id string, disclosure *turingv1.RemoteEgressDisclosure) *turingv1.RemoteEgressConsent {
			return signTestChallenge(t, disclosure, openTestChallenge(t, disclosure.GetChallenge()), "turing.remote-egress.challenge.v1")
		},
		"a second connection since": func(t *testing.T, h *harness, _ string, disclosure *turingv1.RemoteEgressDisclosure) *turingv1.RemoteEgressConsent {
			h.connectGitHub(t, "conn_2")
			return allow(disclosure)
		},
	}
	for name, consentFor := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			_, id := h.consentTask(t, "")
			disclosure := h.prepare(t, id)
			_, err := h.decide(id, consentFor(t, h, id, disclosure))
			wantCode(t, err, codes.FailedPrecondition)
			if message := status.Convert(err).Message(); !strings.Contains(message, messages[name]) {
				t.Fatalf("refusal = %q, want it to say %q", message, messages[name])
			}
			record, err := h.repo.GetDelegation(context.Background(), id)
			if err != nil || record.EgressConsent != "pending" {
				t.Fatalf("delegation = %+v, %v; want consent still pending", record, err)
			}
		})
	}
}

func openTestChallenge(t *testing.T, challenge string) map[string]any {
	t.Helper()
	payload, err := backendegress.OpenChallenge(testEgressSecret, delegationEgressChallengeDomain, challenge, 32*1024)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func signTestChallenge(t *testing.T, disclosure *turingv1.RemoteEgressDisclosure, fields map[string]any, domain string) *turingv1.RemoteEgressConsent {
	t.Helper()
	payload, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	consent := allow(disclosure)
	consent.Challenge = backendegress.SignChallenge(testEgressSecret, domain, payload)
	return consent
}

// Declining runs a task that does not require the tools without them, and
// ends one that does.
func TestDecliningRunsOrEndsTheTask(t *testing.T) {
	t.Run("optional", func(t *testing.T) {
		h := newHarness(t)
		_, id := h.consentTask(t, "")
		delegation, err := h.decide(id, nil)
		if err != nil || delegation.GetEgressConsent() != "declined" || delegation.GetState() != "queued" ||
			!slices.Equal(delegation.GetEgressTools(), []string{"integrations/github.get_issue"}) {
			t.Fatalf("delegation = %+v, %v; want it queued without the tools it names", delegation, err)
		}
	})
	t.Run("required by a local tool", func(t *testing.T) {
		h := newHarness(t)
		_, id := h.consentTask(t, "system.*")
		delegation, err := h.decide(id, nil)
		if err != nil || delegation.GetEgressRequired() || delegation.GetState() != "queued" {
			t.Fatalf("delegation = %+v, %v; want it queued without the tools", delegation, err)
		}
	})
	t.Run("required", func(t *testing.T) {
		h := newHarness(t)
		_, id := h.consentTask(t, "github.*")
		delegation, err := h.decide(id, nil)
		if err != nil || delegation.GetState() != "cancelled" || delegation.GetErrorCode() != "egress_consent_declined" ||
			!delegation.GetEgressRequired() {
			t.Fatalf("delegation = %+v, %v; want it ended", delegation, err)
		}
	})
}

// Only a task waiting for consent has a disclosure or takes a decision.
func TestConsentIsOnlyForATaskWaitingForIt(t *testing.T) {
	t.Run("no id", func(t *testing.T) {
		h := newHarness(t)
		_, err := h.server.PrepareDelegationEgress(context.Background(), &turingv1.PrepareDelegationEgressRequest{})
		wantCode(t, err, codes.InvalidArgument)
		_, err = h.decide("", nil)
		wantCode(t, err, codes.InvalidArgument)
	})
	t.Run("unknown", func(t *testing.T) {
		h := newHarness(t)
		_, err := h.server.PrepareDelegationEgress(context.Background(), &turingv1.PrepareDelegationEgressRequest{DelegationId: "dlg_missing"})
		wantCode(t, err, codes.NotFound)
	})
	t.Run("needed none", func(t *testing.T) {
		h := newHarness(t)
		d := h.delegatingRun(t, "safe")
		response, err := h.delegate(d, "call_1", "", researchTask)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := response.GetResult().AsMap()["delegation_id"].(string)
		_, err = h.server.PrepareDelegationEgress(context.Background(), &turingv1.PrepareDelegationEgressRequest{DelegationId: id})
		wantCode(t, err, codes.FailedPrecondition)
		_, err = h.decide(id, nil)
		wantCode(t, err, codes.FailedPrecondition)
	})
	t.Run("already decided", func(t *testing.T) {
		h := newHarness(t)
		_, id := h.consentTask(t, "")
		disclosure := h.prepare(t, id)
		if _, err := h.decide(id, nil); err != nil {
			t.Fatal(err)
		}
		_, err := h.decide(id, allow(disclosure))
		wantCode(t, err, codes.FailedPrecondition)
	})
	t.Run("not configured", func(t *testing.T) {
		h := newHarness(t)
		_, id := h.consentTask(t, "")
		h.server.SetEgressSigningSecret("")
		_, err := h.server.PrepareDelegationEgress(context.Background(), &turingv1.PrepareDelegationEgressRequest{DelegationId: id})
		wantCode(t, err, codes.FailedPrecondition)
	})
	t.Run("account disconnected", func(t *testing.T) {
		h := newHarness(t)
		_, id := h.consentTask(t, "")
		if _, err := h.repo.RevokeConnection(context.Background(), "conn_1"); err != nil {
			t.Fatal(err)
		}
		_, err := h.server.PrepareDelegationEgress(context.Background(), &turingv1.PrepareDelegationEgressRequest{DelegationId: id})
		wantCode(t, err, codes.FailedPrecondition)
	})
}
