package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMemoryToolsAreOfferedByDefaultWithTheirSeededPolicies(t *testing.T) {
	service, _, _, ctx := newMemoryService(t)

	response, err := service.ListMemoryTools(ctx, &turingv1.ListMemoryToolsRequest{})
	if err != nil {
		t.Fatalf("ListMemoryTools: %v", err)
	}
	got := toolNames(response)
	want := []string{ToolSearch, ToolRead, ToolRemember}
	if len(got) != len(want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}
	policies := map[string]turingv1.ToolPolicy{}
	for _, tool := range response.GetTools() {
		policies[tool.GetToolName()] = tool.GetPolicy()
		if !tool.GetEnabled() {
			t.Fatalf("%s is listed but not enabled", tool.GetToolName())
		}
		if tool.GetSchema() == nil || tool.GetDescription() == "" {
			t.Fatalf("%s has no schema or description", tool.GetToolName())
		}
	}
	for tool, want := range map[string]turingv1.ToolPolicy{
		ToolSearch:   turingv1.ToolPolicy_TOOL_POLICY_SAFE,
		ToolRead:     turingv1.ToolPolicy_TOOL_POLICY_SAFE,
		ToolRemember: turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
	} {
		if policies[tool] != want {
			t.Fatalf("%s policy = %v, want %v", tool, policies[tool], want)
		}
	}
}

// The schema is the model's whole description of what it may pass. A path or a
// scope in it would be an invitation to aim a memory tool at a file, which is
// the one thing the vault layer exists to make impossible.
func TestMemoryToolSchemasNameNoPathAndNoScope(t *testing.T) {
	service, _, _, ctx := newMemoryService(t)
	response, err := service.ListMemoryTools(ctx, &turingv1.ListMemoryToolsRequest{})
	if err != nil {
		t.Fatalf("ListMemoryTools: %v", err)
	}
	wantProperties := map[string][]string{
		ToolSearch:   {"query"},
		ToolRead:     {"belief_id"},
		ToolRemember: {"title", "body", "kind"},
	}
	for _, tool := range response.GetTools() {
		schema := tool.GetSchema().AsMap()
		if schema["additionalProperties"] != false {
			t.Fatalf("%s accepts arguments it does not describe", tool.GetToolName())
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no properties object", tool.GetToolName())
		}
		if len(properties) != len(wantProperties[tool.GetToolName()]) {
			t.Fatalf("%s properties = %v, want exactly %v", tool.GetToolName(), properties, wantProperties[tool.GetToolName()])
		}
		for _, name := range wantProperties[tool.GetToolName()] {
			if _, present := properties[name]; !present {
				t.Fatalf("%s is missing the %q argument", tool.GetToolName(), name)
			}
		}
		for _, forbidden := range []string{"path", "scope", "target", "dir", "file"} {
			if _, present := properties[forbidden]; present {
				t.Fatalf("%s exposes a %q argument", tool.GetToolName(), forbidden)
			}
		}
	}
}

func TestMemoryToggleSilencesDiscoveryWithoutARestart(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	notifier := &countingNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	settings, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: false})
	if err != nil {
		t.Fatalf("SetMemoryEnabled: %v", err)
	}
	if settings.GetEnabled() {
		t.Fatal("settings still report memory as on after it was turned off")
	}
	if notifier.calls != 1 {
		t.Fatalf("registry notifications = %d, want exactly one for a toggle that moved", notifier.calls)
	}

	response, err := service.ListMemoryTools(ctx, &turingv1.ListMemoryToolsRequest{})
	if err != nil {
		t.Fatalf("ListMemoryTools: %v", err)
	}
	if len(response.GetTools()) != 0 {
		t.Fatalf("tools = %v, want none while memory is off", toolNames(response))
	}

	// Writing the same value again is not a change, and re-announcing it would
	// make every client rebuild its tool list for nothing.
	if _, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: false}); err != nil {
		t.Fatalf("SetMemoryEnabled: %v", err)
	}
	if notifier.calls != 1 {
		t.Fatalf("registry notifications = %d, want no second announcement for an unchanged toggle", notifier.calls)
	}

	if _, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: true}); err != nil {
		t.Fatalf("SetMemoryEnabled: %v", err)
	}
	if notifier.calls != 2 {
		t.Fatalf("registry notifications = %d, want a second announcement when memory came back", notifier.calls)
	}
	response, err = service.ListMemoryTools(ctx, &turingv1.ListMemoryToolsRequest{})
	if err != nil || len(response.GetTools()) != 3 {
		t.Fatalf("tools = %v err=%v, want all three back without a restart", toolNames(response), err)
	}
	enabled, err := repo.MemoryEnabled(ctx)
	if err != nil || !enabled {
		t.Fatalf("stored toggle = %v err=%v, want the setting to have persisted", enabled, err)
	}
}

// A worker reconnecting re-reports its capabilities, which rewrites every
// pseudo-server tool row. The toggle lives in settings, not in those rows, so
// the re-report must not quietly turn memory back on.
func TestMemoryToggleSurvivesACapabilityReReport(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	if _, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: false}); err != nil {
		t.Fatalf("SetMemoryEnabled: %v", err)
	}
	setPolicies(t, repo, ctx, "safe")

	response, err := service.ListMemoryTools(ctx, &turingv1.ListMemoryToolsRequest{})
	if err != nil {
		t.Fatalf("ListMemoryTools: %v", err)
	}
	if len(response.GetTools()) != 0 {
		t.Fatalf("tools = %v, want none: a capability report is not consent", toolNames(response))
	}
	settings, err := service.GetMemorySettings(ctx, &turingv1.GetMemorySettingsRequest{})
	if err != nil || settings.GetEnabled() {
		t.Fatalf("settings = %+v err=%v, want memory still off", settings, err)
	}
}

func TestMemoryToggleOffRefusesEveryDispatchWhateverThePolicySays(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, _ := newRun(t, repo, ctx)
	// Every tool marked safe, including the writing one: the toggle has to be
	// the outer gate, not something the policy can talk past.
	setPolicies(t, repo, ctx, "safe")
	if _, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: false}); err != nil {
		t.Fatalf("SetMemoryEnabled: %v", err)
	}

	for tool, args := range map[string]map[string]any{
		ToolSearch:   {"query": "coffee"},
		ToolRead:     {"belief_id": "note_whatever"},
		ToolRemember: {"title": "Coffee", "body": "They drink it black."},
	} {
		_, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
			RunId: runID, ToolName: tool, Args: callArgs(t, args),
		})
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("%s dispatch error = %v, want FailedPrecondition while memory is off", tool, err)
		}
	}
}

func TestMemoryDispatchRefusesUnknownAndDisabledTools(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, _ := newRun(t, repo, ctx)

	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: "memory.forget", Args: callArgs(t, map[string]any{}),
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown tool error = %v, want NotFound", err)
	}
	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		ToolName: ToolSearch, Args: callArgs(t, map[string]any{"query": "x"}),
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing run error = %v, want InvalidArgument", err)
	}

	setPolicies(t, repo, ctx, "disabled")
	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolSearch, Args: callArgs(t, map[string]any{"query": "x"}),
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("disabled tool error = %v, want FailedPrecondition", err)
	}
	response, err := service.ListMemoryTools(ctx, &turingv1.ListMemoryToolsRequest{})
	if err != nil || len(response.GetTools()) != 0 {
		t.Fatalf("tools = %v err=%v, want a disabled memory server to advertise nothing", toolNames(response), err)
	}
}

// An unattended run has nobody in front of it. Memory is what Turing believes
// about the person, so every memory tool is refused there — including one the
// user deliberately marked safe, which is exactly the case an allowlist check
// alone would miss, because a safe tool never reaches the allowlist.
func TestMemoryToolsRefuseAutomationRunsWhateverThePolicy(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	setPolicies(t, repo, ctx, "safe")
	runID := newAutomationRun(t, repo, ctx)

	for tool, args := range map[string]map[string]any{
		ToolSearch:   {"query": "coffee"},
		ToolRead:     {"belief_id": "note_whatever"},
		ToolRemember: {"title": "Coffee", "body": "They drink it black."},
	} {
		_, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
			RunId: runID, ToolName: tool, Args: callArgs(t, args),
		})
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("%s on an automation run error = %v, want PermissionDenied", tool, err)
		}
	}
}

func TestMemorySearchFramesEveryAnswerWithAFreshDelimiter(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, sessionID := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")
	mustPromoteBelief(t, repo, ctx, sessionID, "Coffee", "They take their coffee black.")

	first, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolSearch, Args: callArgs(t, map[string]any{"query": "coffee"}),
	})
	if err != nil {
		t.Fatalf("memory.search: %v", err)
	}
	second, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolSearch, Args: callArgs(t, map[string]any{"query": "coffee"}),
	})
	if err != nil {
		t.Fatalf("memory.search: %v", err)
	}
	firstBody, secondBody := frameBody(t, first.GetResult()), frameBody(t, second.GetResult())
	if frameMarker(t, firstBody) == frameMarker(t, secondBody) {
		t.Fatal("two searches reused one framing delimiter")
	}
	if !strings.Contains(firstBody, "coffee black") {
		t.Fatalf("search did not answer from the vault: %q", firstBody)
	}
	if len(firstBody) > 16*1024 {
		t.Fatalf("framed search result has %d bytes, want at most 16384", len(firstBody))
	}
}

func TestMemoryReadServesTheFileNotTheProjection(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	runID, sessionID := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")
	note := mustPromoteBelief(t, repo, ctx, sessionID, "Coffee", "They take their coffee black.")

	// The user edits the note in their own editor. A read has to answer with
	// what is on disk now, not with the copy the index made when it last looked.
	onDisk := filepath.Join(vault.Root(), note.Path)
	current, err := os.ReadFile(onDisk)
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	edited := strings.Replace(string(current), "coffee black", "coffee with oat milk", 1)
	if edited == string(current) {
		t.Fatalf("test could not edit the note body: %q", current)
	}
	if err := os.WriteFile(onDisk, []byte(edited), 0o600); err != nil {
		t.Fatalf("write note: %v", err)
	}

	response, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRead, Args: callArgs(t, map[string]any{"belief_id": note.NoteID}),
	})
	if err != nil {
		t.Fatalf("memory.read: %v", err)
	}
	body := frameBody(t, response.GetResult())
	if !strings.Contains(body, "oat milk") {
		t.Fatalf("read served a stale projection: %q", body)
	}

	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRead, Args: callArgs(t, map[string]any{"belief_id": "note_missing"}),
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown belief error = %v, want NotFound", err)
	}
}

// Confinement is a property of the API surface, not of the file layer alone: a
// crafted argument must be refused before it reaches anything that opens a file.
func TestMemoryToolsRefuseCraftedPathAndScopeArguments(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")

	for name, call := range map[string]struct {
		tool string
		args map[string]any
	}{
		"search with a path":       {ToolSearch, map[string]any{"query": "x", "path": "../../etc/passwd"}},
		"search with a scope":      {ToolSearch, map[string]any{"query": "x", "scope": "inbox"}},
		"read with a path":         {ToolRead, map[string]any{"belief_id": "note_x", "path": "beliefs/x.md"}},
		"read by path alone":       {ToolRead, map[string]any{"path": "beliefs/x.md"}},
		"remember with a path":     {ToolRemember, map[string]any{"title": "t", "body": "b", "path": "beliefs/x.md"}},
		"remember with a target":   {ToolRemember, map[string]any{"title": "t", "body": "b", "target": "profile.md"}},
		"remember with a bad kind": {ToolRemember, map[string]any{"title": "t", "body": "b", "kind": "persona_edit"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
				RunId: runID, ToolName: call.tool, Args: callArgs(t, call.args),
			}); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("error = %v, want InvalidArgument", err)
			}
		})
	}
}

func TestMemoryRememberFilesAnInboxCandidateAndNeverEchoesItsBody(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	runID, sessionID := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")
	const body = "Their bank card ends 4242 and they hate being asked about it."

	response, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRemember,
		Args: callArgs(t, map[string]any{"title": "Card", "body": body}),
	})
	if err != nil {
		t.Fatalf("memory.remember: %v", err)
	}
	result := response.GetResult().AsMap()
	candidateID, _ := result["candidate_id"].(string)
	if candidateID == "" {
		t.Fatalf("remember returned no candidate id: %v", result)
	}
	if result["title"] != "Card" || result["status"] != "pending" {
		t.Fatalf("remember result = %v, want the title and a pending status", result)
	}
	if len(result) != 3 {
		t.Fatalf("remember result = %v, want exactly candidate_id, title and status", result)
	}
	for key, value := range result {
		if text, ok := value.(string); ok && strings.Contains(text, "4242") {
			t.Fatalf("remember echoed the body back through %q", key)
		}
	}

	// The claim is in the inbox, waiting for the user, and nowhere else.
	candidate, err := repo.MemoryCandidateByID(ctx, candidateID)
	if err != nil {
		t.Fatalf("MemoryCandidateByID: %v", err)
	}
	if candidate.SourceSessionID != sessionID {
		t.Fatalf("candidate session = %q, want the run's own session %q", candidate.SourceSessionID, sessionID)
	}
	if candidate.Body != body || candidate.State != "pending" {
		t.Fatalf("candidate = %+v, want the proposal stored pending", candidate)
	}
	if !strings.HasPrefix(candidate.InboxPath, memoryfiles.InboxDirName+"/") {
		t.Fatalf("candidate landed at %q, outside the inbox", candidate.InboxPath)
	}
	beliefs, err := os.ReadDir(filepath.Join(vault.Root(), memoryfiles.BeliefsDirName))
	if err != nil {
		t.Fatalf("read beliefs: %v", err)
	}
	if len(beliefs) != 0 {
		t.Fatalf("beliefs = %d entries, want a proposal to reach only the inbox", len(beliefs))
	}
}

func TestMemoryRememberRefusesAnOversizeBodyRatherThanTruncatingIt(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")

	oversize := strings.Repeat("a", memoryfiles.MaxCandidateBodyBytes+1)
	_, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRemember,
		Args: callArgs(t, map[string]any{"title": "Long", "body": oversize}),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversize body error = %v, want InvalidArgument", err)
	}
	if !strings.Contains(status.Convert(err).Message(), "16384") {
		t.Fatalf("refusal %q does not say what the limit is", status.Convert(err).Message())
	}
	if strings.Contains(status.Convert(err).Message(), "aaaa") {
		t.Fatalf("refusal echoed the body: %q", status.Convert(err).Message())
	}
	candidates, err := repo.ListMemoryCandidates(ctx, repository.MemoryCandidateQuery{Limit: 10})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %d, want an over-limit proposal to store nothing at all", len(candidates))
	}
}

// A tool call is not a licence to rewrite the user's files. Search runs the
// read-only index pass, never the reconcile pass that adopts a hand-written
// note by writing an identity into it — because the user very likely has that
// file open, and a question a model asked is not a reason to edit it.
func TestMemorySearchRefreshesTheIndexWithoutWritingToTheVault(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")

	// A belief the user wrote by hand, carrying no identity of its own. Only
	// the writing pass may adopt one.
	handWritten := filepath.Join(vault.Root(), memoryfiles.BeliefsDirName, "their-own-belief.md")
	const original = "---\ntitle: Bikes\n---\n\nThey bike to work every day.\n"
	if err := os.WriteFile(handWritten, []byte(original), 0o600); err != nil {
		t.Fatalf("write hand-written belief: %v", err)
	}

	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolSearch, Args: callArgs(t, map[string]any{"query": "bike"}),
	}); err != nil {
		t.Fatalf("memory.search: %v", err)
	}

	after, err := os.ReadFile(handWritten)
	if err != nil {
		t.Fatalf("read hand-written belief: %v", err)
	}
	if string(after) != original {
		t.Fatalf("a search rewrote the user's own file:\n%q\nwant\n%q", after, original)
	}
}

// The read frame carries the same guarantees the search frame does: a fresh
// delimiter each time, a hard byte ceiling, and a truncation that cuts on a
// rune boundary and says so.
func TestMemoryReadFramesAreBoundedFreshAndValidUTF8(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, sessionID := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")
	// Multibyte, inside what a proposal may be, and once its frontmatter is
	// added, larger than what one frame may carry.
	note := mustPromoteBelief(t, repo, ctx, sessionID, "Long", strings.Repeat("é", 8100))

	args := map[string]any{"belief_id": note.NoteID}
	first, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRead, Args: callArgs(t, args),
	})
	if err != nil {
		t.Fatalf("memory.read: %v", err)
	}
	second, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRead, Args: callArgs(t, args),
	})
	if err != nil {
		t.Fatalf("memory.read: %v", err)
	}
	firstBody, secondBody := frameBody(t, first.GetResult()), frameBody(t, second.GetResult())
	if frameMarker(t, firstBody) == frameMarker(t, secondBody) {
		t.Fatal("two reads reused one framing delimiter")
	}
	if len(firstBody) > 16*1024 {
		t.Fatalf("framed read has %d bytes, want at most 16384", len(firstBody))
	}
	if !utf8.ValidString(firstBody) {
		t.Fatal("truncation split a multibyte rune")
	}
	if !strings.Contains(firstBody, "truncated") {
		t.Fatal("a truncated read did not say it was truncated")
	}
}

// The inbox listing is the inbox: proposals Turing filed and drafts the user
// dropped in. Applying either filter drops the drafts, because they carry
// neither a state nor a kind and pretending otherwise would be an invention.
func TestUnfilteredCandidateListingIsTheWholeInbox(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	_, sessionID := newRun(t, repo, ctx)
	if _, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: repository.MemoryCandidateKindBelief,
		Title: "Coffee", Body: "They take their coffee black.",
	}); err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	draft := filepath.Join(vault.Root(), memoryfiles.InboxDirName, "my-own-note.md")
	if err := os.WriteFile(draft, []byte("I bike to work.\n"), 0o600); err != nil {
		t.Fatalf("write draft: %v", err)
	}

	all, err := service.ListMemoryCandidates(ctx, &turingv1.ListMemoryCandidatesRequest{})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	managed, unmanaged := 0, 0
	for _, candidate := range all.GetCandidates() {
		if candidate.GetManaged() {
			managed++
			continue
		}
		unmanaged++
	}
	if managed != 1 || unmanaged != 1 {
		t.Fatalf("unfiltered listing = %d managed, %d unmanaged, want one of each", managed, unmanaged)
	}

	pending, err := service.ListMemoryCandidates(ctx, &turingv1.ListMemoryCandidatesRequest{
		State: turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PENDING,
	})
	if err != nil {
		t.Fatalf("ListMemoryCandidates(pending): %v", err)
	}
	if len(pending.GetCandidates()) != 1 || !pending.GetCandidates()[0].GetManaged() {
		t.Fatalf("filtered listing = %+v, want only the proposal Turing filed", pending.GetCandidates())
	}
}

// The title bound is the one the schema advertises and the one the vault
// enforces: characters, not bytes. Counting bytes here would refuse a short
// title in Japanese, Greek or Cyrillic that every layer underneath accepts —
// on exactly the personal, non-Latin prose this feature exists to keep.
func TestMemoryRememberBoundsTheTitleInCharactersNotBytes(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")

	// 100 characters, 300 bytes: well inside the advertised 200-character limit.
	multibyte := strings.Repeat("好", 100)
	response, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRemember,
		Args: callArgs(t, map[string]any{"title": multibyte, "body": "They read in Chinese."}),
	})
	if err != nil {
		t.Fatalf("a %d-character title was refused: %v", utf8.RuneCountInString(multibyte), err)
	}
	if response.GetResult().AsMap()["title"] != multibyte {
		t.Fatalf("title = %v, want it round-tripped whole", response.GetResult().AsMap()["title"])
	}

	// One character past the limit is refused, and the refusal names the unit.
	_, err = service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRemember,
		Args: callArgs(t, map[string]any{"title": strings.Repeat("好", 201), "body": "b"}),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("over-long title error = %v, want InvalidArgument", err)
	}
	message := status.Convert(err).Message()
	if !strings.Contains(message, "201 characters") || !strings.Contains(message, "200 characters") {
		t.Fatalf("refusal %q does not say what the limit is in the unit it is measured in", message)
	}
	if strings.Contains(message, "好") {
		t.Fatalf("refusal echoed the title: %q", message)
	}
}
