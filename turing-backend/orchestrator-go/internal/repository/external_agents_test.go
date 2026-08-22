package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func anthropicAgent() ExternalAgentInput {
	return ExternalAgentInput{
		DisplayName:   "Claude",
		Provider:      "anthropic",
		BaseURL:       "https://api.anthropic.com/v1",
		Model:         "claude-sonnet-4-5",
		CredentialRef: "claude",
	}
}

func mustCreateAgent(t *testing.T, ctx context.Context, repo *Repository, input ExternalAgentInput) ExternalAgent {
	t.Helper()
	agent, err := repo.CreateExternalAgent(ctx, input)
	if err != nil {
		t.Fatalf("create agent %q: %v", input.DisplayName, err)
	}
	return agent
}

func TestCreateExternalAgentTrimsAndStoresNoSecret(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	input := anthropicAgent()
	input.DisplayName = "  Claude  "
	input.CredentialRef = "  claude  "

	agent := mustCreateAgent(t, ctx, repo, input)
	if agent.DisplayName != "Claude" || agent.CredentialRef != "claude" {
		t.Fatalf("stored %q / %q, want both trimmed", agent.DisplayName, agent.CredentialRef)
	}

	// The point of the whole credential design: nothing in this table can hold
	// an API key, because there is no column that could.
	var columns []string
	rows, err := repo.db.QueryContext(ctx, `SELECT name FROM pragma_table_info('external_agents')`)
	if err != nil {
		t.Fatalf("read columns: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	for _, name := range columns {
		lowered := strings.ToLower(name)
		if lowered == "api_key" || lowered == "apikey" || lowered == "secret" || lowered == "credential" {
			t.Fatalf("external_agents has a %q column; third-party keys must never be stored (columns = %v)", name, columns)
		}
	}
}

func TestCreateExternalAgentRejectsDuplicateNamesRegardlessOfCase(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	mustCreateAgent(t, ctx, repo, anthropicAgent())

	if _, err := repo.CreateExternalAgent(ctx, anthropicAgent()); !errors.Is(err, ErrExternalAgentNameTaken) {
		t.Fatalf("duplicate error = %v, want ErrExternalAgentNameTaken", err)
	}
	lowered := anthropicAgent()
	lowered.DisplayName = "claude"
	if _, err := repo.CreateExternalAgent(ctx, lowered); !errors.Is(err, ErrExternalAgentNameTaken) {
		t.Fatalf("case-different duplicate error = %v, want ErrExternalAgentNameTaken", err)
	}
}

func TestCreateExternalAgentValidatesEveryField(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	cases := []struct {
		name    string
		mutate  func(*ExternalAgentInput)
		wantErr error
	}{
		{"blank name", func(in *ExternalAgentInput) { in.DisplayName = "  " }, ErrExternalAgentNameEmpty},
		{"long name", func(in *ExternalAgentInput) { in.DisplayName = strings.Repeat("n", maxExternalAgentNameRunes+1) }, ErrExternalAgentNameTooLong},
		{"blank model", func(in *ExternalAgentInput) { in.Model = " " }, ErrExternalAgentModelEmpty},
		{"long model", func(in *ExternalAgentInput) { in.Model = strings.Repeat("m", maxExternalAgentModelRunes+1) }, ErrExternalAgentModelTooLong},
		{"blank base URL", func(in *ExternalAgentInput) { in.BaseURL = "" }, ErrExternalAgentBaseURLEmpty},
		{"relative base URL", func(in *ExternalAgentInput) { in.BaseURL = "/v1" }, ErrExternalAgentBaseURLInvalid},
		{"malformed IPv6 base URL", func(in *ExternalAgentInput) { in.BaseURL = "http://[::1" }, ErrExternalAgentBaseURLInvalid},
		{"non-http scheme", func(in *ExternalAgentInput) { in.BaseURL = "ftp://example.com/v1" }, ErrExternalAgentBaseURLInvalid},
		{"query string", func(in *ExternalAgentInput) { in.BaseURL = "https://example.com/v1?key=abc" }, ErrExternalAgentBaseURLInvalid},
		// The whole conversation travels over this URL. Plaintext to somewhere
		// off this machine would carry it in the clear.
		{"plaintext remote", func(in *ExternalAgentInput) { in.BaseURL = "http://api.anthropic.com/v1" }, ErrExternalAgentBaseURLInsecure},
		{"long base URL", func(in *ExternalAgentInput) {
			in.BaseURL = "https://example.com/" + strings.Repeat("p", maxExternalAgentBaseURLRunes)
		}, ErrExternalAgentBaseURLTooLong},
		{"blank credential", func(in *ExternalAgentInput) { in.CredentialRef = " " }, ErrExternalAgentCredentialRefEmpty},
		{"long credential", func(in *ExternalAgentInput) {
			in.CredentialRef = strings.Repeat("c", maxExternalAgentCredentialRefRunes+1)
		}, ErrExternalAgentCredentialRefLong},
		{"credential with a space", func(in *ExternalAgentInput) { in.CredentialRef = "my key" }, ErrExternalAgentCredentialRefFormat},
		{"credential with a slash", func(in *ExternalAgentInput) { in.CredentialRef = "../secret" }, ErrExternalAgentCredentialRefFormat},
		{"unknown provider", func(in *ExternalAgentInput) { in.Provider = "skynet" }, ErrExternalAgentProviderInvalid},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			input := anthropicAgent()
			testCase.mutate(&input)
			if _, err := repo.CreateExternalAgent(ctx, input); !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

// A URL is the one field that can smuggle a secret past a design built to keep
// secrets out of the database: Go's HTTP client turns userinfo into an
// Authorization header, so https://user:sk-secret@host/v1 IS a credential — and
// it passes every other check, being absolute, https, and free of query and
// fragment. Accepting it would put a third-party key in base_url, and from
// there into every job payload, the runtime stream, and any client that lists
// agents.
func TestCreateExternalAgentRefusesCredentialsSmuggledIntoTheURL(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	existing := mustCreateAgent(t, ctx, repo, anthropicAgent())

	for _, baseURL := range []string{
		"https://user:sk-secret@api.anthropic.com/v1",
		"https://sk-secret@api.anthropic.com/v1",
		"http://user:sk-secret@localhost:4000/v1",
	} {
		input := anthropicAgent()
		input.DisplayName = "Smuggler"
		input.BaseURL = baseURL
		if _, err := repo.CreateExternalAgent(ctx, input); !errors.Is(err, ErrExternalAgentBaseURLCredentials) {
			t.Fatalf("create with %q = %v, want ErrExternalAgentBaseURLCredentials", baseURL, err)
		}
		if _, err := repo.UpdateExternalAgent(ctx, existing.AgentID, input); !errors.Is(err, ErrExternalAgentBaseURLCredentials) {
			t.Fatalf("update with %q = %v, want ErrExternalAgentBaseURLCredentials", baseURL, err)
		}
	}

	// And nothing key-shaped reached the table by any route.
	var stored int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM external_agents WHERE base_url LIKE '%@%'`).Scan(&stored); err != nil {
		t.Fatalf("scan stored URLs: %v", err)
	}
	if stored != 0 {
		t.Fatalf("%d stored base URLs carry userinfo", stored)
	}
}

// Renaming one agent onto another's name is a different statement from
// creating a duplicate, and reaches a different unique-violation branch.
func TestUpdateExternalAgentRejectsRenamingOntoAnotherName(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	mustCreateAgent(t, ctx, repo, anthropicAgent())
	other := anthropicAgent()
	other.DisplayName = "ChatGPT"
	second := mustCreateAgent(t, ctx, repo, other)

	renamed := other
	renamed.DisplayName = "claude"
	if _, err := repo.UpdateExternalAgent(ctx, second.AgentID, renamed); !errors.Is(err, ErrExternalAgentNameTaken) {
		t.Fatalf("rename onto an existing name = %v, want ErrExternalAgentNameTaken", err)
	}
}

func TestCreateExternalAgentAllowsPlaintextOnlyForLiteralLoopback(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	for index, host := range []string{"localhost", "127.0.0.1", "[::1]"} {
		input := anthropicAgent()
		input.DisplayName = host
		input.Provider = "other"
		input.BaseURL = "http://" + host + ":4000/v1"
		if _, err := repo.CreateExternalAgent(ctx, input); err != nil {
			t.Fatalf("case %d: create with %s: %v", index, input.BaseURL, err)
		}
	}
}

func TestCreateExternalAgentRejectsPlaintextDockerHostAlias(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	input := anthropicAgent()
	input.BaseURL = "http://host.docker.internal:4000/v1"
	if _, err := repo.CreateExternalAgent(ctx, input); !errors.Is(err, ErrExternalAgentBaseURLInsecure) {
		t.Fatalf("create with Docker host alias = %v, want ErrExternalAgentBaseURLInsecure", err)
	}
}

func TestUpdateAndDeleteExternalAgentReportMissingAgents(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	if _, err := repo.UpdateExternalAgent(ctx, "agent_nope", anthropicAgent()); !errors.Is(err, ErrExternalAgentNotFound) {
		t.Fatalf("update error = %v, want ErrExternalAgentNotFound", err)
	}
	if err := repo.DeleteExternalAgent(ctx, "agent_nope"); !errors.Is(err, ErrExternalAgentNotFound) {
		t.Fatalf("delete error = %v, want ErrExternalAgentNotFound", err)
	}
	if _, err := repo.GetExternalAgent(ctx, "agent_nope"); !errors.Is(err, ErrExternalAgentNotFound) {
		t.Fatalf("get error = %v, want ErrExternalAgentNotFound", err)
	}
}

func TestUpdateExternalAgentRewritesEveryFieldAndStillValidates(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())

	updated, err := repo.UpdateExternalAgent(ctx, agent.AgentID, ExternalAgentInput{
		DisplayName:   "ChatGPT",
		Provider:      "openai",
		BaseURL:       "https://api.openai.com/v1",
		Model:         "gpt-4o",
		CredentialRef: "openai",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.DisplayName != "ChatGPT" || updated.Provider != "openai" ||
		updated.Model != "gpt-4o" || updated.CredentialRef != "openai" ||
		updated.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("updated agent = %+v, want every field rewritten", updated)
	}

	invalid := anthropicAgent()
	invalid.BaseURL = "http://api.openai.com/v1"
	if _, err := repo.UpdateExternalAgent(ctx, agent.AgentID, invalid); !errors.Is(err, ErrExternalAgentBaseURLInsecure) {
		t.Fatalf("update with plaintext remote URL = %v, want ErrExternalAgentBaseURLInsecure", err)
	}
}

func TestListExternalAgentsOrdersByNameCaseInsensitively(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	for _, name := range []string{"grok", "Claude", "chatgpt"} {
		input := anthropicAgent()
		input.DisplayName = name
		mustCreateAgent(t, ctx, repo, input)
	}

	agents, err := repo.ListExternalAgents(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var names []string
	for _, agent := range agents {
		names = append(names, agent.DisplayName)
	}
	want := []string{"chatgpt", "Claude", "grok"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("names = %v, want %v", names, want)
	}
}

func TestSessionAgentRoundTripAndReplacement(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	claude := mustCreateAgent(t, ctx, repo, anthropicAgent())
	openaiInput := anthropicAgent()
	openaiInput.DisplayName = "ChatGPT"
	openaiInput.Provider = "openai"
	openaiInput.CredentialRef = "openai"
	chatgpt := mustCreateAgent(t, ctx, repo, openaiInput)

	// A conversation nobody routed is local, and that is not an error.
	if _, routed, err := repo.GetSessionAgent(ctx, session.SessionID); err != nil || routed {
		t.Fatalf("fresh session routed = %v (err %v), want false", routed, err)
	}

	if _, err := repo.SetSessionAgent(ctx, session.SessionID, claude.AgentID); err != nil {
		t.Fatalf("route to claude: %v", err)
	}
	// Routing somewhere new replaces where it went; it does not add a second
	// recipient.
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, chatgpt.AgentID); err != nil {
		t.Fatalf("re-route to chatgpt: %v", err)
	}
	agent, routed, err := repo.GetSessionAgent(ctx, session.SessionID)
	if err != nil || !routed || agent.AgentID != chatgpt.AgentID {
		t.Fatalf("destination = %+v routed %v err %v, want chatgpt", agent, routed, err)
	}

	if err := repo.ClearSessionAgent(ctx, session.SessionID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, routed, err := repo.GetSessionAgent(ctx, session.SessionID); err != nil || routed {
		t.Fatalf("cleared session routed = %v (err %v), want false", routed, err)
	}
	// Clearing an already-local conversation asks for a state it is already
	// in, which is not a failure.
	if err := repo.ClearSessionAgent(ctx, session.SessionID); err != nil {
		t.Fatalf("second clear: %v", err)
	}
}

// Pointing a conversation at a third party is the most consequential thing the
// app lets someone do. Without a record, "when did this start going to
// Anthropic?" has no answer.
func TestRoutingAConversationLeavesAnAuditRecord(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())

	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatalf("route: %v", err)
	}

	var payloadJSON string
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COALESCE(payload_json, '') FROM audit_logs WHERE action = 'session.routed' AND target = ?`,
		session.SessionID).Scan(&payloadJSON); err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	var payload struct {
		Agent    string `json:"agent"`
		AgentID  string `json:"agentId"`
		Endpoint string `json:"endpoint"`
		Model    string `json:"model"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode audit payload: %v", err)
	}
	if payload.Agent != "Claude" || payload.AgentID != agent.AgentID ||
		payload.Endpoint != "api.anthropic.com" || payload.Model != "claude-sonnet-4-5" {
		t.Fatalf("audit payload = %+v, want the destination it recorded", payload)
	}

	if err := repo.ClearSessionAgent(ctx, session.SessionID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	var unrouted int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'session.unrouted' AND target = ?`,
		session.SessionID).Scan(&unrouted); err != nil {
		t.Fatalf("count unroute rows: %v", err)
	}
	if unrouted != 1 {
		t.Fatalf("session.unrouted rows = %d, want 1", unrouted)
	}

	// Clearing an already-local conversation changed nothing, so it records
	// nothing — otherwise the log fills with no-ops and buries the decisions.
	if err := repo.ClearSessionAgent(ctx, session.SessionID); err != nil {
		t.Fatalf("second clear: %v", err)
	}
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'session.unrouted' AND target = ?`,
		session.SessionID).Scan(&unrouted); err != nil {
		t.Fatalf("re-count unroute rows: %v", err)
	}
	if unrouted != 1 {
		t.Fatalf("session.unrouted rows after a no-op clear = %d, want still 1", unrouted)
	}
}

// The foreign key does not say which side is missing, and telling someone
// their agent is gone when the conversation went stale sends them to look in
// the wrong place.
func TestSessionAgentNamesWhichSideIsMissing(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())

	if _, err := repo.SetSessionAgent(ctx, session.SessionID, "agent_nope"); !errors.Is(err, ErrExternalAgentNotFound) {
		t.Fatalf("unknown agent error = %v, want ErrExternalAgentNotFound", err)
	}
	if _, err := repo.SetSessionAgent(ctx, "sess_nope", agent.AgentID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("unknown session error = %v, want ErrSessionNotFound", err)
	}
	if _, _, err := repo.GetSessionAgent(ctx, "sess_nope"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("get unknown session error = %v, want ErrSessionNotFound", err)
	}
	if err := repo.ClearSessionAgent(ctx, "sess_nope"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("clear unknown session error = %v, want ErrSessionNotFound", err)
	}
}

// Deleting an agent must fail towards "stays on this machine". A dangling
// route would either error on every send or, worse, be silently ignored.
func TestDeletingAnAgentReturnsItsConversationsToTheLocalAssistant(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatalf("route: %v", err)
	}

	if err := repo.DeleteExternalAgent(ctx, agent.AgentID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, routed, err := repo.GetSessionAgent(ctx, session.SessionID); err != nil || routed {
		t.Fatalf("after deleting the agent, routed = %v (err %v), want false", routed, err)
	}
}

func TestEnqueueUserMessageRoutesToTheConversationsAgent(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatalf("route: %v", err)
	}

	// The request asks for the local model. The conversation's configured
	// destination wins, or the bar above the messages would be lying.
	result, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:      session.SessionID,
		Content:        "hello",
		AgentID:        "general_assistant",
		ModelProvider:  "ollama",
		Model:          "qwen2.5:7b",
		EgressDecision: testRemoteEgressDecision(t, agent.Model, agent.BaseURL, agent.AgentID, agent.CredentialRef),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var provider, model string
	if err := repo.db.QueryRowContext(ctx,
		`SELECT model_provider, model_name FROM agent_runs WHERE id = ?`, result.RunID).
		Scan(&provider, &model); err != nil {
		t.Fatalf("read run: %v", err)
	}
	if provider != "openai_compatible" || model != "claude-sonnet-4-5" {
		t.Fatalf("run recorded %q/%q, want openai_compatible/claude-sonnet-4-5", provider, model)
	}

	target := payloadExternalAgent(t, ctx, repo, result.JobID)
	if target == nil || target.DisplayName != "Claude" ||
		target.BaseURL != "https://api.anthropic.com/v1" || target.CredentialRef != "claude" {
		t.Fatalf("payload target = %+v, want the routed agent", target)
	}
	// The reference, never the key: the job payload is one of the places a
	// secret could plausibly end up.
	payload := jobPayloadJSON(t, ctx, repo, result.JobID)
	if strings.Contains(payload, "apiKey") || strings.Contains(payload, "api_key") {
		t.Fatalf("job payload mentions an API key: %s", payload)
	}
}

// A conversation nobody routed keeps taking whatever the request asked for.
func TestEnqueueUserMessageLeavesUnroutedConversationsAlone(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	result, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "hello",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var provider, model string
	if err := repo.db.QueryRowContext(ctx,
		`SELECT model_provider, model_name FROM agent_runs WHERE id = ?`, result.RunID).
		Scan(&provider, &model); err != nil {
		t.Fatalf("read run: %v", err)
	}
	if provider != "ollama" || model != "qwen2.5:7b" {
		t.Fatalf("run recorded %q/%q, want the requested ollama/qwen2.5:7b", provider, model)
	}
	if target := payloadExternalAgent(t, ctx, repo, result.JobID); target != nil {
		t.Fatalf("payload target = %+v, want nil for an unrouted conversation", target)
	}
	if len(result.RoutingEvents) != 0 {
		t.Fatalf("routing events = %d, want none: nothing left the machine", len(result.RoutingEvents))
	}
}

// The transcript is where a person will look afterwards to answer "did that go
// anywhere?", so the answer has to be written there when it happens.
func TestEnqueueUserMessageRecordsThatTheMessageLeftTheMachine(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	input := anthropicAgent()
	// A name spanning two lines must not break the sentence in half.
	input.DisplayName = "Claude\nvia Anthropic"
	agent := mustCreateAgent(t, ctx, repo, input)
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatalf("route: %v", err)
	}

	result, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello",
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
		EgressDecision: testRemoteEgressDecision(t, agent.Model, agent.BaseURL, agent.AgentID, agent.CredentialRef),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if len(result.RoutingEvents) != 1 {
		t.Fatalf("routing events = %d, want exactly one notice", len(result.RoutingEvents))
	}
	notice := result.RoutingEvents[0]
	if notice.Type != "agent.run.step" {
		t.Fatalf("notice type = %q, want agent.run.step so the client renders it", notice.Type)
	}
	// After the queued event, so the stream that is already replaying from the
	// queued sequence still delivers it.
	if notice.Sequence <= result.QueuedEvent.Sequence {
		t.Fatalf("notice sequence %d, want after the queued event's %d", notice.Sequence, result.QueuedEvent.Sequence)
	}
	var payload struct {
		Note     string `json:"note"`
		Endpoint string `json:"endpoint"`
		Model    string `json:"model"`
	}
	if err := json.Unmarshal([]byte(notice.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode notice payload: %v", err)
	}
	if !strings.Contains(payload.Note, "leaves your machine") {
		t.Fatalf("note = %q, want it to say the message leaves the machine", payload.Note)
	}
	// Prospective, never past tense. Nothing has left at enqueue time, and the
	// designed failure paths (missing key, unwired runtime, a debug tool
	// command answered locally, a cancelled run) mean it may never leave at
	// all — a past-tense claim would then be a lie sitting in the transcript.
	if strings.Contains(payload.Note, "left your machine") ||
		strings.HasPrefix(payload.Note, "Sent ") {
		t.Fatalf("note = %q, want a prospective claim: nothing has left the machine at enqueue time", payload.Note)
	}
	if strings.Contains(payload.Note, "\n") {
		t.Fatalf("note = %q, want the agent name flattened to one line", payload.Note)
	}
	if payload.Endpoint != "api.anthropic.com" || payload.Model != "claude-sonnet-4-5" {
		t.Fatalf("notice payload endpoint/model = %q/%q, want api.anthropic.com/claude-sonnet-4-5", payload.Endpoint, payload.Model)
	}
}

// Re-pointing or deleting the agent while the job waits must not redirect a
// message the user already sent to a different company.
func TestQueuedJobKeepsTheAgentItWasEnqueuedWith(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatalf("route: %v", err)
	}
	result, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello",
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
		EgressDecision: testRemoteEgressDecision(t, agent.Model, agent.BaseURL, agent.AgentID, agent.CredentialRef),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if _, err := repo.UpdateExternalAgent(ctx, agent.AgentID, ExternalAgentInput{
		DisplayName:   "Somewhere else",
		Provider:      "openai",
		BaseURL:       "https://api.openai.com/v1",
		Model:         "gpt-4o",
		CredentialRef: "openai",
	}); err != nil {
		t.Fatalf("update agent: %v", err)
	}
	if err := repo.ClearSessionAgent(ctx, session.SessionID); err != nil {
		t.Fatalf("clear route: %v", err)
	}

	target := payloadExternalAgent(t, ctx, repo, result.JobID)
	if target == nil || target.BaseURL != "https://api.anthropic.com/v1" {
		t.Fatalf("payload target = %+v, want the endpoint captured at enqueue", target)
	}
}

// The claimed job is what the runtime actually acts on, so the target has to
// survive the round trip through payload_json.
func TestClaimNextJobCarriesTheRoutedAgent(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatalf("route: %v", err)
	}
	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello",
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
		EgressDecision: testRemoteEgressDecision(t, agent.Model, agent.BaseURL, agent.AgentID, agent.CredentialRef),
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	job, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if job.ExternalAgent == nil || job.ExternalAgent.CredentialRef != "claude" {
		t.Fatalf("claimed job target = %+v, want the routed agent", job.ExternalAgent)
	}
	if job.ModelProvider != "openai_compatible" || job.Model != "claude-sonnet-4-5" {
		t.Fatalf("claimed job = %q/%q, want openai_compatible/claude-sonnet-4-5", job.ModelProvider, job.Model)
	}
}

// A retry re-reads payload_json through the same claim path, so the second
// attempt is a second chance to lose the destination — and losing it there
// would answer a routed conversation locally on attempt two, silently.
func TestARequeuedRoutedJobIsStillRoutedOnItsNextAttempt(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatalf("route: %v", err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello",
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
		EgressDecision: testRemoteEgressDecision(t, agent.Model, agent.BaseURL, agent.AgentID, agent.CredentialRef),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	first, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-1")
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if first.EgressDecision == nil {
		t.Fatal("first claim lost egress decision")
	}

	decision, err := repo.RequeueOrFailRetryableRun(ctx, RetryableRunFailureInput{
		RunID: enqueued.RunID, Failure: dispatchCondition("worker_busy"), MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("requeue: %v", err)
	}
	if !decision.Requeued {
		t.Fatal("run was not requeued, so this test proves nothing about attempt two")
	}

	retried, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-2")
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if retried.Attempt < 2 {
		t.Fatalf("claimed attempt = %d, want the retry", retried.Attempt)
	}
	if retried.ExternalAgent == nil || retried.ExternalAgent.BaseURL != "https://api.anthropic.com/v1" {
		t.Fatalf("retried job target = %+v, want the destination frozen at enqueue", retried.ExternalAgent)
	}
	if retried.ModelProvider != "openai_compatible" || retried.Model != "claude-sonnet-4-5" {
		t.Fatalf("retried job = %q/%q, want the routed provider and model", retried.ModelProvider, retried.Model)
	}
	if retried.EgressDecision == nil ||
		retried.EgressDecision.DecisionID != first.EgressDecision.DecisionID ||
		retried.EgressDecision.ChallengeFingerprint != first.EgressDecision.ChallengeFingerprint {
		t.Fatalf("retry decision = %+v, want original %+v", retried.EgressDecision, first.EgressDecision)
	}
	var decisions int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM run_egress_decisions WHERE run_id = ?`, enqueued.RunID).Scan(&decisions); err != nil {
		t.Fatal(err)
	}
	if decisions != 1 {
		t.Fatalf("run egress decisions = %d, want 1", decisions)
	}
}

func payloadExternalAgent(t *testing.T, ctx context.Context, repo *Repository, jobID string) *ExternalAgentTarget {
	t.Helper()
	var payload struct {
		ExternalAgent *ExternalAgentTarget `json:"externalAgent"`
	}
	if err := json.Unmarshal([]byte(jobPayloadJSON(t, ctx, repo, jobID)), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return payload.ExternalAgent
}

// Deleting a session lets the FK graph do the deleting, so a table added later
// is only cleaned up if its foreign key says so. Without this, a deleted
// conversation would leave a row behind recording where it used to send things.
func TestDeletingASessionRemovesItsRouting(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatalf("route: %v", err)
	}

	if err := repo.DeleteSession(ctx, session.SessionID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	var remaining int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_external_agent WHERE session_id = ?`, session.SessionID).
		Scan(&remaining); err != nil {
		t.Fatalf("count routing rows: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("routing rows left after deleting the conversation = %d, want 0", remaining)
	}
	// The agent itself is the user's configuration and outlives one
	// conversation.
	if _, err := repo.GetExternalAgent(ctx, agent.AgentID); err != nil {
		t.Fatalf("agent was removed along with the conversation: %v", err)
	}
}

func jobPayloadJSON(t *testing.T, ctx context.Context, repo *Repository, jobID string) string {
	t.Helper()
	var payloadJSON string
	if err := repo.db.QueryRowContext(ctx, `SELECT payload_json FROM jobs WHERE id = ?`, jobID).Scan(&payloadJSON); err != nil {
		t.Fatalf("read job payload: %v", err)
	}
	return payloadJSON
}
