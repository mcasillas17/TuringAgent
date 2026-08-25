package chat

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func attachChatVault(t *testing.T, h *harness) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{memoryfiles.InboxDirName, memoryfiles.BeliefsDirName} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatalf("prepare vault dir %q: %v", dir, err)
		}
	}
	vault, err := memoryfiles.Open(root)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	h.repo.SetMemoryVault(vault)
	return root
}

func writeChatPin(t *testing.T, root string, name string, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func memoryChatSend(sessionID, content, key string) *turingv1.SendMessageRequest {
	return &turingv1.SendMessageRequest{
		SessionId: sessionID, Content: content, ContentType: "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "gpt-4o-mini", IdempotencyKey: key,
	}
}

func prepareMemoryDisclosure(
	t *testing.T,
	h *harness,
	request *turingv1.SendMessageRequest,
) *turingv1.RemoteEgressDisclosure {
	t.Helper()
	prepared, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: request.GetSessionId(), Content: request.GetContent(), ContentType: request.GetContentType(),
		AgentId: request.GetAgentId(), ModelProvider: request.GetModelProvider(), Model: request.GetModel(),
		IdempotencyKey: request.GetIdempotencyKey(), RequestedTools: request.GetRequestedTools(),
	})
	if err != nil {
		t.Fatalf("PrepareRemoteEgress: %v", err)
	}
	return prepared.GetDisclosure()
}

func hasMemoryCategory(disclosure *turingv1.RemoteEgressDisclosure) bool {
	return slices.Contains(disclosure.GetDataCategories(),
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_MEMORY_PROFILE)
}

func routeChatSessionToExternalAgent(t *testing.T, h *harness, sessionID string) {
	t.Helper()
	agent, err := h.repo.CreateExternalAgent(context.Background(), repository.ExternalAgentInput{
		DisplayName: "External", Provider: "anthropic", BaseURL: "https://example.com/v1",
		Model: "external-model", CredentialRef: "external",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.SetSessionAgent(context.Background(), sessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
}

// A run that never leaves the machine discloses no memory category, however
// much the vault holds: there is no destination for it to be a statement about.
func TestLocalRunNeverClaimsTheMemoryCategory(t *testing.T) {
	h := newHarness(t)
	root := attachChatVault(t, h)
	writeChatPin(t, root, memoryfiles.PersonaFileName, "Speak plainly.")
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()

	response, err := h.chatClient.PrepareRemoteEgress(h.clientContext(), &turingv1.PrepareRemoteEgressRequest{
		SessionId: h.createSession(t), Content: "local", ContentType: "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "llama3.2", IdempotencyKey: "memory_local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetDisclosure() != nil {
		t.Fatalf("a local run produced a disclosure: %+v", response.GetDisclosure())
	}
}

func TestRemoteRunClaimsMemoryForPersonaAloneAndProfileAlone(t *testing.T) {
	for _, tier := range []struct {
		name string
		file string
	}{
		{"persona", memoryfiles.PersonaFileName},
		{"profile", memoryfiles.ProfileFileName},
	} {
		t.Run(tier.name, func(t *testing.T) {
			h := newHarness(t)
			root := attachChatVault(t, h)
			writeChatPin(t, root, tier.file, "Something the user wrote.")
			worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
			defer func() { _ = worker.CloseSend() }()

			disclosure := prepareMemoryDisclosure(t, h,
				memoryChatSend(h.createSession(t), "remote "+tier.name, "memory_"+tier.name))
			if !hasMemoryCategory(disclosure) {
				t.Fatalf("categories = %v, want memory profile", disclosure.GetDataCategories())
			}
			if !disclosure.GetMemoryProfileMayBeSent() {
				t.Fatal("disclosure does not say the memory profile may be sent")
			}
			tiers := make([]turingv1.MemoryTier, 0, len(disclosure.GetMemoryNotes()))
			for _, note := range disclosure.GetMemoryNotes() {
				tiers = append(tiers, note.GetTier())
				if strings.Contains(note.GetTitle(), "Something the user wrote") {
					t.Fatalf("disclosure leaked pinned content: %+v", note)
				}
			}
			if len(tiers) == 0 {
				t.Fatal("disclosure names no memory tier")
			}
		})
	}
}

// Whitespace is not content. A persona of nothing but blank lines contributes
// no instruction, so it must not put a category on the disclosure.
func TestRemoteRunWithWhitespaceOnlyPinsClaimsNoMemory(t *testing.T) {
	h := newHarness(t)
	root := attachChatVault(t, h)
	writeChatPin(t, root, memoryfiles.PersonaFileName, "   \n\t\n")
	writeChatPin(t, root, memoryfiles.ProfileFileName, "\n\n  ")
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()

	disclosure := prepareMemoryDisclosure(t, h,
		memoryChatSend(h.createSession(t), "blank pins", "memory_blank"))
	if hasMemoryCategory(disclosure) {
		t.Fatalf("categories = %v, want no memory profile", disclosure.GetDataCategories())
	}
	if disclosure.GetMemoryProfileMayBeSent() || len(disclosure.GetMemoryNotes()) != 0 {
		t.Fatalf("disclosure claims memory it will not send: %+v", disclosure)
	}
}

// An unreadable pin sends nothing, so it claims nothing — and it must not be
// reported as a category the user then consents to.
func TestRemoteRunWithUnreadablePinClaimsNoMemory(t *testing.T) {
	h := newHarness(t)
	root := attachChatVault(t, h)
	if err := os.Symlink("/etc/passwd", filepath.Join(root, memoryfiles.PersonaFileName)); err != nil {
		t.Fatalf("symlink persona: %v", err)
	}
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()

	disclosure := prepareMemoryDisclosure(t, h,
		memoryChatSend(h.createSession(t), "symlinked persona", "memory_symlink"))
	if hasMemoryCategory(disclosure) {
		t.Fatalf("categories = %v, want no memory profile", disclosure.GetDataCategories())
	}
}

// Memory tools are memory leaving the machine even when nothing is pinned: the
// arguments and results carry the user's own notes.
func TestRemoteRunWithMemoryToolsAndEmptyPinsStillClaimsMemory(t *testing.T) {
	h := newHarness(t)
	attachChatVault(t, h)
	capabilities := defaultChatWorkerCapabilities(false)
	capabilities.Tools = append(capabilities.Tools, &turingv1.DiscoveredTool{
		ServerName: "memory", ToolName: "memory.search", Schema: &structpb.Struct{},
	})
	worker := connectChatTestWorker(t, h, capabilities)
	defer func() { _ = worker.CloseSend() }()

	disclosure := prepareMemoryDisclosure(t, h,
		memoryChatSend(h.createSession(t), "memory tools", "memory_tools"))
	if !slices.Contains(disclosure.GetSelectedTools(), "memory/memory.search") {
		t.Fatalf("selected tools = %v, want the memory tool", disclosure.GetSelectedTools())
	}
	if !hasMemoryCategory(disclosure) {
		t.Fatalf("categories = %v, want memory profile", disclosure.GetDataCategories())
	}
	// Nothing is pinned, so the disclosure must not name a persona or a profile;
	// what it has to say is that the memory tools can reach accepted notes.
	tiers := make([]turingv1.MemoryTier, 0, len(disclosure.GetMemoryNotes()))
	for _, note := range disclosure.GetMemoryNotes() {
		tiers = append(tiers, note.GetTier())
	}
	if len(tiers) != 1 || tiers[0] != turingv1.MemoryTier_MEMORY_TIER_BELIEF {
		t.Fatalf("memory notes = %+v, want only the belief tier the tools reach", disclosure.GetMemoryNotes())
	}
}

// Recall is withheld from an external agent because the run belongs to someone
// else's model. The persona is not: the user pointed this conversation at that
// agent and the persona is how they want to be spoken to there too. So the
// category applies, and this divergence from RecallApplicable is deliberate.
func TestExternalAgentRunClaimsMemoryEvenThoughRecallIsWithheld(t *testing.T) {
	h := newHarness(t)
	root := attachChatVault(t, h)
	writeChatPin(t, root, memoryfiles.PersonaFileName, "Speak plainly.")
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(true))
	defer func() { _ = worker.CloseSend() }()
	sessionID := h.createSession(t)
	routeChatSessionToExternalAgent(t, h, sessionID)

	disclosure := prepareMemoryDisclosure(t, h,
		memoryChatSend(sessionID, "external agent", "memory_external"))
	if slices.Contains(disclosure.GetDataCategories(),
		turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL) {
		t.Fatalf("categories = %v, want no cross-session recall", disclosure.GetDataCategories())
	}
	if !hasMemoryCategory(disclosure) {
		t.Fatalf("categories = %v, want memory profile", disclosure.GetDataCategories())
	}
}

// The disclosure is what a person reads before consenting. It names the tiers
// and the tools, and it carries neither the pinned bytes nor the fingerprint
// the run is bound with.
func TestMemoryDisclosureNamesTiersWithoutContentOrFingerprint(t *testing.T) {
	h := newHarness(t)
	root := attachChatVault(t, h)
	writeChatPin(t, root, memoryfiles.PersonaFileName, "Call me Ishmael and nothing else.")
	writeChatPin(t, root, memoryfiles.ProfileFileName, "The user keeps chickens.")
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()

	disclosure := prepareMemoryDisclosure(t, h,
		memoryChatSend(h.createSession(t), "disclosure detail", "memory_detail"))
	fingerprint, _, err := h.repo.EgressMemorySnapshotFingerprint(
		context.Background(), disclosure.GetSelectedTools())
	if err != nil {
		t.Fatal(err)
	}
	rendered := disclosure.String()
	for _, forbidden := range []string{"Ishmael", "chickens", fingerprint} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("disclosure leaked %q: %s", forbidden, rendered)
		}
	}
	tiers := map[turingv1.MemoryTier]string{}
	for _, note := range disclosure.GetMemoryNotes() {
		tiers[note.GetTier()] = note.GetVaultPath()
	}
	if tiers[turingv1.MemoryTier_MEMORY_TIER_PERSONA] != memoryfiles.PersonaFileName ||
		tiers[turingv1.MemoryTier_MEMORY_TIER_PROFILE] != memoryfiles.ProfileFileName {
		t.Fatalf("memory notes = %+v, want both pinned tiers named", disclosure.GetMemoryNotes())
	}
}

// An Obsidian autosave between prepare and send has to come back as something a
// person can act on. It names the file, says to re-read it, and says to prepare
// the send again — never a bare "invalid decision".
func TestSendMessageNamesMemoryDriftPerTier(t *testing.T) {
	for _, tier := range []struct {
		name string
		file string
	}{
		{"persona", memoryfiles.PersonaFileName},
		{"profile", memoryfiles.ProfileFileName},
	} {
		t.Run(tier.name, func(t *testing.T) {
			h := newHarness(t)
			root := attachChatVault(t, h)
			writeChatPin(t, root, tier.file, "Original words.")
			worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
			defer func() { _ = worker.CloseSend() }()
			request := memoryChatSend(h.createSession(t), "drift "+tier.name, "memory_drift_"+tier.name)
			disclosure := prepareMemoryDisclosure(t, h, request)

			writeChatPin(t, root, tier.file, "Edited words.")
			request.RemoteEgressConsent = &turingv1.RemoteEgressConsent{
				Challenge: disclosure.GetChallenge(), Acknowledged: true,
				AcknowledgedDataCategories: disclosure.GetDataCategories(),
			}
			err := sendMessageError(h, request)
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("send error = %v, want FailedPrecondition", err)
			}
			message := err.Error()
			for _, want := range []string{
				memoryfiles.PersonaFileName, memoryfiles.ProfileFileName,
				"changed", "Re-read", "prepare the send again", "Obsidian",
			} {
				if !strings.Contains(message, want) {
					t.Fatalf("send error = %q, missing %q", message, want)
				}
			}
		})
	}
}

// Non-memory drift keeps the wording it already had. A skill edit is a
// different problem with a different fix.
func TestSendMessageKeepsSkillDriftWordingWhenMemoryIsUntouched(t *testing.T) {
	h := newHarness(t)
	root := attachChatVault(t, h)
	writeChatPin(t, root, memoryfiles.PersonaFileName, "Speak plainly.")
	writeChatSkill(t, h, "writing/tone", "Tone", "Brief", nil, "Original body.")
	if _, err := h.repo.SetSkillEnabled(context.Background(), "writing/tone", true); err != nil {
		t.Fatal(err)
	}
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	request := memoryChatSend(h.createSession(t), "skill drift", "memory_skill_drift")
	disclosure := prepareMemoryDisclosure(t, h, request)

	writeChatSkill(t, h, "writing/tone", "Tone", "Brief", nil, "Edited body.")
	request.RemoteEgressConsent = &turingv1.RemoteEgressConsent{
		Challenge: disclosure.GetChallenge(), Acknowledged: true,
		AcknowledgedDataCategories: disclosure.GetDataCategories(),
	}
	err := sendMessageError(h, request)
	if !strings.Contains(err.Error(), "skill snapshot changed") {
		t.Fatalf("send error = %v, want the skill drift wording", err)
	}
	if strings.Contains(err.Error(), memoryfiles.PersonaFileName) {
		t.Fatalf("send error = %v, wrongly blames memory", err)
	}
}

// The sentinel the enqueue transaction raises has to survive the mapping. It
// wraps ErrEgressDecisionInvalid, so an ordering that checked the generic
// sentinel first would answer every vault edit with "invalid decision".
func TestMapEnqueueErrorNamesMemorySnapshotRaceBeforeGenericInvalidity(t *testing.T) {
	err := mapEnqueueError(context.Background(), repository.ErrEgressMemorySnapshotChanged)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("status = %v, want FailedPrecondition", status.Code(err))
	}
	for _, want := range []string{"memory", "prepare the send again"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, missing %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "remote egress decision is invalid for this request") {
		t.Fatalf("error = %v, want the memory-specific message", err)
	}
}

// Turning memory off is a complete answer: nothing is pinned, no memory tool is
// listed, and the disclosure claims none of it however full the vault is.
func TestRemoteRunWithMemoryOffClaimsNoMemory(t *testing.T) {
	h := newHarness(t)
	root := attachChatVault(t, h)
	writeChatPin(t, root, memoryfiles.PersonaFileName, "Speak plainly.")
	writeChatPin(t, root, memoryfiles.ProfileFileName, "The user keeps chickens.")
	if _, err := h.repo.SetMemoryEnabled(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()

	disclosure := prepareMemoryDisclosure(t, h,
		memoryChatSend(h.createSession(t), "memory off", "memory_off"))
	if hasMemoryCategory(disclosure) {
		t.Fatalf("categories = %v, want no memory profile", disclosure.GetDataCategories())
	}
	if disclosure.GetMemoryProfileMayBeSent() || len(disclosure.GetMemoryNotes()) != 0 {
		t.Fatalf("a disabled vault produced a memory disclosure: %+v", disclosure)
	}
}

// A persona past the pin budget is an ordinary truncation, never a failed send:
// the budget bounds what reaches a prompt, and the user may write as much as
// they like.
func TestOversizedPersonaStillSendsAsATruncatedPin(t *testing.T) {
	h := newHarness(t)
	root := attachChatVault(t, h)
	writeChatPin(t, root, memoryfiles.PersonaFileName, strings.Repeat("a", memoryfiles.MaxPersonaBytes*4))
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	request := memoryChatSend(h.createSession(t), "oversized persona", "memory_oversized")
	disclosure := prepareMemoryDisclosure(t, h, request)
	if !hasMemoryCategory(disclosure) {
		t.Fatalf("categories = %v, want memory profile", disclosure.GetDataCategories())
	}

	request.RemoteEgressConsent = &turingv1.RemoteEgressConsent{
		Challenge: disclosure.GetChallenge(), Acknowledged: true,
		AcknowledgedDataCategories: disclosure.GetDataCategories(),
	}
	stream, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := stream.Recv()
	if err != nil {
		t.Fatalf("an over-budget persona blocked the send: %v", err)
	}
	if queued.GetRunQueued().GetRunId() == "" {
		t.Fatalf("no run was queued: %+v", queued)
	}
}
func TestConsentCarriesTheMemoryFingerprintAndRefusesAMismatch(t *testing.T) {
	h := newHarness(t)
	root := attachChatVault(t, h)
	writeChatPin(t, root, memoryfiles.PersonaFileName, "Speak plainly.")
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	request := memoryChatSend(h.createSession(t), "bound consent", "memory_bound")
	disclosure := prepareMemoryDisclosure(t, h, request)
	payload, _, err := h.service.parseEgressChallenge(disclosure.GetChallenge())
	if err != nil {
		t.Fatal(err)
	}
	expected, _, err := h.repo.EgressMemorySnapshotFingerprint(
		context.Background(), disclosure.GetSelectedTools())
	if err != nil {
		t.Fatal(err)
	}
	if payload.MemorySnapshotFingerprint != expected {
		t.Fatalf("challenge memory fingerprint = %q, want %q",
			payload.MemorySnapshotFingerprint, expected)
	}

	request.RemoteEgressConsent = &turingv1.RemoteEgressConsent{
		Challenge: disclosure.GetChallenge(), Acknowledged: true,
		AcknowledgedDataCategories: disclosure.GetDataCategories(),
	}
	stream, err := h.chatClient.SendMessage(h.clientContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := h.repo.GetRunEgressDecision(context.Background(), queued.GetRunQueued().GetRunId())
	if err != nil {
		t.Fatal(err)
	}
	if stored.MemorySnapshotFingerprint != expected {
		t.Fatalf("stored fingerprint = %q, want %q", stored.MemorySnapshotFingerprint, expected)
	}
	if !stored.MemoryProfileApplicable {
		t.Fatal("the frozen decision does not carry the memory flag it disclosed")
	}
}
