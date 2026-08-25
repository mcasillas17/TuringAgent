package memory

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// everyToolCall is the whole surface, in the smallest well-formed arguments
// each tool accepts. A gate that is supposed to be outer to everything has to
// be shown holding for all three, not for the convenient one.
func everyToolCall() map[string]map[string]any {
	return map[string]map[string]any{
		ToolSearch:   {"query": "coffee"},
		ToolRead:     {"belief_id": "note_whatever"},
		ToolRemember: {"title": "Coffee", "body": "They drink it black."},
	}
}

// A run id is an identity, and an identity is something the orchestrator's own
// tables either hold or do not. A runtime that made one up must not be able to
// search, read or write memory with it — and must not reach the vault on the
// way to being refused, because "it found nothing" is itself an answer about
// the user.
func TestMemoryDispatchRefusesARunItCannotFind(t *testing.T) {
	service, repo, vault, ctx := newMemoryService(t)
	_, sessionID := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")
	note := mustPromoteBelief(t, repo, ctx, sessionID, "Coffee", "They take their coffee black.")

	// A real belief exists, so a search or read that got through would answer
	// with something rather than with an empty vault's silence.
	for tool, args := range map[string]map[string]any{
		ToolSearch:   {"query": "coffee"},
		ToolRead:     {"belief_id": note.NoteID},
		ToolRemember: {"title": "Coffee", "body": "They drink it black."},
	} {
		t.Run(tool, func(t *testing.T) {
			response, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
				RunId: "run_01JQFABRICATEDRUNIDENTITY", ToolName: tool, Args: callArgs(t, args),
			})
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("%s on a fabricated run error = %v, want PermissionDenied", tool, err)
			}
			if response.GetResult() != nil {
				t.Fatalf("%s on a fabricated run returned %v, want nothing at all", tool, response.GetResult().AsMap())
			}
			if message := status.Convert(err).Message(); strings.Contains(message, "coffee black") {
				t.Fatalf("refusal leaked the vault's contents: %q", message)
			}
		})
	}

	// Nothing was written on the way to any of those refusals.
	candidates, err := repo.ListMemoryCandidates(ctx, repository.MemoryCandidateQuery{Limit: 10})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %d, want a fabricated run to file nothing", len(candidates))
	}
	inbox, err := os.ReadDir(filepath.Join(vault.Root(), memoryfiles.InboxDirName))
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("inbox = %d entries, want a fabricated run to reach no file", len(inbox))
	}
}

// The identity gate is outer to the toggle and to the policy alike: a run that
// does not exist is refused whether memory is on or off, and whatever class the
// tool was given.
func TestMemoryDispatchIdentityGateIsOuterToTheToggleAndThePolicy(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	setPolicyPerTool(t, repo, ctx, map[string]string{
		ToolSearch: "safe", ToolRead: "approval_required", ToolRemember: "safe",
	})
	for _, enabled := range []bool{true, false} {
		if _, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: enabled}); err != nil {
			t.Fatalf("SetMemoryEnabled(%v): %v", enabled, err)
		}
		for tool, args := range everyToolCall() {
			_, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
				RunId: "run_01JQNOSUCHRUNATALL", ToolName: tool, Args: callArgs(t, args),
			})
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("%s with memory enabled=%v error = %v, want PermissionDenied for an unknown run", tool, enabled, err)
			}
		}
	}
}

// Whether a run is unattended is read from automation_runs, and a caller has no
// field to claim otherwise with. Deleting the row is the only thing that can
// change the answer, so this asserts the derivation rather than the plumbing.
func TestMemoryAutomationStatusIsDerivedFromTheDatabase(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	setPolicies(t, repo, ctx, "safe")
	runID := newAutomationRun(t, repo, ctx)

	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolSearch, Args: callArgs(t, map[string]any{"query": "coffee"}),
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("automation dispatch error = %v, want PermissionDenied", err)
	}

	grant, unattended, err := repo.GetAutomationRunGrant(ctx, runID)
	if err != nil || !unattended {
		t.Fatalf("GetAutomationRunGrant = %+v unattended=%v err=%v, want the run known as unattended", grant, unattended, err)
	}
}

// An automation is refused before the policy is consulted at all, so the
// refusal is the same for a safe tool, an approval-gated one, and every
// permutation in between. The gate is unconditional or it is not a gate.
func TestMemoryAutomationDispatchIsRefusedWhateverThePolicyClass(t *testing.T) {
	permutations := []map[string]string{
		{ToolSearch: "safe", ToolRead: "approval_required", ToolRemember: "safe"},
		{ToolSearch: "approval_required", ToolRead: "safe", ToolRemember: "approval_required"},
		{ToolSearch: "safe", ToolRead: "safe", ToolRemember: "safe"},
		{ToolSearch: "approval_required", ToolRead: "approval_required", ToolRemember: "approval_required"},
	}
	for index, policies := range permutations {
		service, repo, vault, ctx := newMemoryService(t)
		setPolicyPerTool(t, repo, ctx, policies)
		runID := newAutomationRun(t, repo, ctx)
		// No approval enforcer is wired, deliberately. If the automation gate
		// were removed, an approval_required tool would answer
		// FailedPrecondition ("enforcement is not configured") and a safe one
		// would simply run — so PermissionDenied for every tool in every
		// permutation is load-bearing for the gate being unconditional and
		// ahead of the policy, not merely for something having refused.
		for tool, args := range everyToolCall() {
			response, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
				RunId: runID, ToolName: tool, Args: callArgs(t, args),
			})
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("permutation %d: %s as %q on an automation run error = %v, want PermissionDenied",
					index, tool, policies[tool], err)
			}
			if response.GetResult() != nil {
				t.Fatalf("permutation %d: %s answered an automation run with %v", index, tool, response.GetResult().AsMap())
			}
		}
		candidates, err := repo.ListMemoryCandidates(ctx, repository.MemoryCandidateQuery{Limit: 10})
		if err != nil {
			t.Fatalf("ListMemoryCandidates: %v", err)
		}
		if len(candidates) != 0 {
			t.Fatalf("permutation %d: candidates = %d, want an automation to file nothing", index, len(candidates))
		}
		inbox, err := os.ReadDir(filepath.Join(vault.Root(), memoryfiles.InboxDirName))
		if err != nil {
			t.Fatalf("read inbox: %v", err)
		}
		if len(inbox) != 0 {
			t.Fatalf("permutation %d: inbox = %d entries, want an automation to reach no file", index, len(inbox))
		}
	}
}

// A refusal is written by Turing about the caller's mistake; it is not a place
// to repeat the caller's own bytes. An argument key is attacker-chosen — it can
// be a megabyte long, or it can be a secret dressed up as a field name — so the
// message names the tool and the arguments the tool declares, and nothing else.
func TestMemoryArgumentRefusalNeverEchoesWhatTheCallerSent(t *testing.T) {
	recorder := &recordingAudit{}
	service, repo, _, ctx := newMemoryServiceAt(t, filepath.Join(t.TempDir(), "turing.db"), newVaultRoot(t), recorder)
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")

	const secretKey = "aws_secret_access_key_AKIAIOSFODNN7EXAMPLE"
	const secretValue = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	huge := strings.Repeat("k", 64*1024)

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	for name, args := range map[string]map[string]any{
		"a secret-looking key": {"query": "coffee", secretKey: secretValue},
		"a huge key":           {"query": "coffee", huge: "x"},
		"both at once":         {"query": "coffee", secretKey: secretValue, huge: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
				RunId: runID, ToolName: ToolSearch, Args: callArgs(t, args),
			})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("error = %v, want InvalidArgument", err)
			}
			message := status.Convert(err).Message()
			for _, forbidden := range []string{secretKey, secretValue, "AKIA", "wJalrXUt", "kkkk"} {
				if strings.Contains(message, forbidden) {
					t.Fatalf("refusal echoed %q back: %q", forbidden, message)
				}
			}
			// Bounded by construction: the message is built from the tool's own
			// declared names, so its length cannot follow the caller's input.
			if len(message) > 256 {
				t.Fatalf("refusal is %d bytes; a fixed message built from the schema cannot be: %q", len(message), message)
			}
			if !strings.Contains(message, ToolSearch) || !strings.Contains(message, "query") {
				t.Fatalf("refusal %q does not say which tool and which arguments are allowed", message)
			}
		})
	}

	for _, forbidden := range []string{secretKey, secretValue, "kkkk"} {
		if strings.Contains(recorder.text(), forbidden) {
			t.Fatalf("the audit trail recorded %q", forbidden)
		}
		if strings.Contains(logs.String(), forbidden) {
			t.Fatalf("the log recorded %q", forbidden)
		}
	}
}

// A proposal is not a memory. Until the user promotes it, memory.search must
// not match it and memory.read must not find it — otherwise remember/search is
// a scratchpad with a review queue painted on it.
func TestMemoryCandidateIsInertToSearchAndReadUntilItIsPromoted(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")
	const body = "They keep their spare key under the third flowerpot."

	filed, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRemember,
		Args: callArgs(t, map[string]any{"title": "Spare key", "body": body}),
	})
	if err != nil {
		t.Fatalf("memory.remember: %v", err)
	}
	candidateID, _ := filed.GetResult().AsMap()["candidate_id"].(string)
	if candidateID == "" {
		t.Fatalf("remember returned no candidate id: %v", filed.GetResult().AsMap())
	}

	searched, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolSearch, Args: callArgs(t, map[string]any{"query": "flowerpot"}),
	})
	if err != nil {
		t.Fatalf("memory.search: %v", err)
	}
	answer := frameBody(t, searched.GetResult())
	if strings.Contains(answer, "flowerpot") || strings.Contains(answer, candidateID) {
		t.Fatalf("search answered from an unreviewed proposal: %q", answer)
	}

	// The candidate's own identity is not a belief identity either.
	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRead, Args: callArgs(t, map[string]any{"belief_id": candidateID}),
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("read of a pending candidate error = %v, want NotFound", err)
	}

	// The user accepts it. Now — and only now — it is memory.
	note, err := repo.PromoteMemoryCandidate(ctx, candidateID)
	if err != nil {
		t.Fatalf("PromoteMemoryCandidate: %v", err)
	}
	searched, err = service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolSearch, Args: callArgs(t, map[string]any{"query": "flowerpot"}),
	})
	if err != nil {
		t.Fatalf("memory.search after promotion: %v", err)
	}
	answer = frameBody(t, searched.GetResult())
	if !strings.Contains(answer, "flowerpot") {
		t.Fatalf("search did not find the promoted belief: %q", answer)
	}
	read, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRead, Args: callArgs(t, map[string]any{"belief_id": note.NoteID}),
	})
	if err != nil {
		t.Fatalf("memory.read after promotion: %v", err)
	}
	if !strings.Contains(frameBody(t, read.GetResult()), "flowerpot") {
		t.Fatalf("read did not serve the promoted belief: %q", frameBody(t, read.GetResult()))
	}
}

// The runtime bound is bytes, and the boundary is exact. A multibyte argument
// is the case where a character count and a byte count disagree, which is
// precisely where a wrong unit would show up as either a refused valid input or
// an accepted oversize one.
func TestMemoryByteBoundsAreEnforcedOnTheExactMultibyteBoundary(t *testing.T) {
	service, repo, _, ctx := newMemoryService(t)
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")

	// 256 two-byte runes: exactly 512 bytes, and exactly 256 characters.
	atLimit := strings.Repeat("é", maxMemoryQueryBytes/2)
	if len(atLimit) != maxMemoryQueryBytes {
		t.Fatalf("test query is %d bytes, want exactly %d", len(atLimit), maxMemoryQueryBytes)
	}
	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolSearch, Args: callArgs(t, map[string]any{"query": atLimit}),
	}); err != nil {
		t.Fatalf("a query of exactly %d bytes was refused: %v", maxMemoryQueryBytes, err)
	}
	overLimit := atLimit + "a"
	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolSearch, Args: callArgs(t, map[string]any{"query": overLimit}),
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a query of %d bytes error = %v, want InvalidArgument", len(overLimit), err)
	}

	bodyAtLimit := strings.Repeat("é", memoryfiles.MaxCandidateBodyBytes/2)
	if len(bodyAtLimit) != memoryfiles.MaxCandidateBodyBytes {
		t.Fatalf("test body is %d bytes, want exactly %d", len(bodyAtLimit), memoryfiles.MaxCandidateBodyBytes)
	}
	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRemember,
		Args: callArgs(t, map[string]any{"title": "At the limit", "body": bodyAtLimit}),
	}); err != nil {
		t.Fatalf("a body of exactly %d bytes was refused: %v", memoryfiles.MaxCandidateBodyBytes, err)
	}
	// One more rune is two more bytes, and only a byte count refuses it: this
	// body is 8193 characters, well under any plausible character limit.
	bodyOverLimit := bodyAtLimit + "é"
	_, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRemember,
		Args: callArgs(t, map[string]any{"title": "Over the limit", "body": bodyOverLimit}),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a body of %d bytes error = %v, want InvalidArgument", len(bodyOverLimit), err)
	}
	if message := status.Convert(err).Message(); !strings.Contains(message, "bytes") {
		t.Fatalf("refusal %q does not name the unit the limit is measured in", message)
	}
	candidates, err := repo.ListMemoryCandidates(ctx, repository.MemoryCandidateQuery{Limit: 10})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want only the one at the limit to have been filed", len(candidates))
	}

	// belief_id carries its own, much smaller budget. It shares the helper the
	// two above pin, but not the constant, so a read wired to the wrong number
	// would slip past both of them.
	idAtLimit := strings.Repeat("é", maxBeliefIDBytes/2)
	if len(idAtLimit) != maxBeliefIDBytes {
		t.Fatalf("test belief id is %d bytes, want exactly %d", len(idAtLimit), maxBeliefIDBytes)
	}
	// No such belief exists, so at the limit the bound is passed and the read
	// gets as far as looking: NotFound is the proof it was not refused here.
	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRead, Args: callArgs(t, map[string]any{"belief_id": idAtLimit}),
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("a belief id of exactly %d bytes error = %v, want NotFound rather than a bounds refusal",
			maxBeliefIDBytes, err)
	}
	idOverLimit := idAtLimit + "é"
	if _, err := service.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRead, Args: callArgs(t, map[string]any{"belief_id": idOverLimit}),
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a belief id of %d bytes error = %v, want InvalidArgument", len(idOverLimit), err)
	}
}

// JSON Schema's maxLength counts characters, and three of these arguments are
// bounded in bytes. Advertising a byte budget as maxLength would tell a model
// it may send 16384 characters of Japanese — three times what the runtime will
// take — so the descriptor states the unit it actually means and carries the
// number in a vendor extension instead. The title is genuinely rune-bounded, so
// it keeps maxLength and must not sprout a byte claim beside it.
func TestMemoryToolSchemasDescribeByteBoundsWithoutMisusingMaxLength(t *testing.T) {
	service, _, _, ctx := newMemoryService(t)
	response, err := service.ListMemoryTools(ctx, &turingv1.ListMemoryToolsRequest{})
	if err != nil {
		t.Fatalf("ListMemoryTools: %v", err)
	}
	byteBounded := map[string]map[string]int{
		ToolSearch:   {"query": maxMemoryQueryBytes},
		ToolRead:     {"belief_id": maxBeliefIDBytes},
		ToolRemember: {"body": memoryfiles.MaxCandidateBodyBytes},
	}
	seen := 0
	for _, tool := range response.GetTools() {
		properties, ok := tool.GetSchema().AsMap()["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no properties object", tool.GetToolName())
		}
		for name, limit := range byteBounded[tool.GetToolName()] {
			property, ok := properties[name].(map[string]any)
			if !ok {
				t.Fatalf("%s.%s is not an object", tool.GetToolName(), name)
			}
			if _, present := property["maxLength"]; present {
				t.Fatalf("%s.%s advertises maxLength for a bound measured in bytes", tool.GetToolName(), name)
			}
			declared, ok := property[schemaMaxBytesKeyword].(float64)
			if !ok || int(declared) != limit {
				t.Fatalf("%s.%s %s = %v, want %d", tool.GetToolName(), name, schemaMaxBytesKeyword, property[schemaMaxBytesKeyword], limit)
			}
			description, _ := property["description"].(string)
			if !strings.Contains(description, "bytes") {
				t.Fatalf("%s.%s description %q does not say the limit is in bytes", tool.GetToolName(), name, description)
			}
			if strings.Contains(description, "characters") {
				t.Fatalf("%s.%s description %q contradicts itself about the unit", tool.GetToolName(), name, description)
			}
			seen++
		}
		if tool.GetToolName() != ToolRemember {
			continue
		}
		title, ok := properties["title"].(map[string]any)
		if !ok {
			t.Fatal("memory.remember has no title property")
		}
		if maxLength, ok := title["maxLength"].(float64); !ok || int(maxLength) != maxMemoryTitleRunes {
			t.Fatalf("title maxLength = %v, want the rune bound %d it is actually enforced with", title["maxLength"], maxMemoryTitleRunes)
		}
		if _, present := title[schemaMaxBytesKeyword]; present {
			t.Fatalf("title carries a byte claim it is not enforced with: %v", title[schemaMaxBytesKeyword])
		}
		if description, _ := title["description"].(string); strings.Contains(description, "bytes") {
			t.Fatalf("title description %q claims a byte bound", description)
		}
	}
	if seen != 3 {
		t.Fatalf("checked %d byte-bounded properties, want 3", seen)
	}
}

// Turning memory off is a decision about Turing, not a report about the disk.
// A user who switched memory off and whose vault has since gone missing must
// still be told memory is off — the toggle is the thing they can act on, and a
// row that swapped it for a folder problem would invite them to fix the folder
// and expect memory back.
func TestSettingsStayDisabledWhenTheVaultIsAlsoUnavailable(t *testing.T) {
	t.Run("no vault at all", func(t *testing.T) {
		service, repo, _, ctx := newMemoryService(t)
		if _, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: false}); err != nil {
			t.Fatalf("SetMemoryEnabled: %v", err)
		}
		// The vault the app could not open at startup: memory still has to
		// describe itself, and the toggle is what it describes first.
		vaultless := New(repo, nil, nil)
		settings, err := vaultless.GetMemorySettings(ctx, &turingv1.GetMemorySettingsRequest{})
		if err != nil {
			t.Fatalf("GetMemorySettings: %v", err)
		}
		if settings.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_DISABLED {
			t.Fatalf("settings reason = %v, want DISABLED", settings.GetUnavailableReason())
		}
		state, err := vaultless.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
		if err != nil {
			t.Fatalf("ListMemoryState: %v", err)
		}
		if state.GetSettings().GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_DISABLED {
			t.Fatalf("state settings reason = %v, want DISABLED", state.GetSettings().GetUnavailableReason())
		}
		if state.GetSettings().GetEnabled() {
			t.Fatal("state settings report memory as on")
		}
	})

	t.Run("an unreadable vault", func(t *testing.T) {
		service, _, vault, ctx := newMemoryService(t)
		if _, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: false}); err != nil {
			t.Fatalf("SetMemoryEnabled: %v", err)
		}
		beliefs := filepath.Join(vault.Root(), memoryfiles.BeliefsDirName)
		if err := os.Chmod(beliefs, 0o000); err != nil {
			t.Fatalf("chmod beliefs: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(beliefs, 0o700) })

		state, err := service.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
		if err != nil {
			t.Fatalf("ListMemoryState: %v", err)
		}
		if state.GetSettings().GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_DISABLED {
			t.Fatalf("state settings reason = %v, want DISABLED even with the vault unreadable",
				state.GetSettings().GetUnavailableReason())
		}
		if state.GetSettings().GetEnabled() {
			t.Fatal("state settings report memory as on")
		}
		// The vault problem is not swallowed: a tier row is where it belongs,
		// beside the tier it actually stops from being read.
		var beliefTier *turingv1.MemoryTierState
		for _, tier := range state.GetTiers() {
			if tier.GetTier() == turingv1.MemoryTier_MEMORY_TIER_BELIEF {
				beliefTier = tier
			}
		}
		if beliefTier == nil {
			t.Fatal("state has no belief tier")
		}
		if beliefTier.GetEnabled() {
			t.Fatal("belief tier reports itself enabled while memory is off")
		}
		if beliefTier.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_UNREADABLE {
			t.Fatalf("belief tier reason = %v, want VAULT_UNREADABLE so the folder problem is still visible",
				beliefTier.GetUnavailableReason())
		}
	})

	// The other direction of the same switch, and the one the app actually
	// boots into: memory defaults to on, so a vault the app could not open at
	// startup leaves an enabled service with no vault. DISABLED outranking the
	// folder must not mean the folder stops being reported when nothing
	// outranks it.
	t.Run("on, but the vault never opened", func(t *testing.T) {
		_, repo, _, ctx := newMemoryService(t)
		vaultless := New(repo, nil, nil)

		settings, err := vaultless.GetMemorySettings(ctx, &turingv1.GetMemorySettingsRequest{})
		if err != nil {
			t.Fatalf("GetMemorySettings: %v", err)
		}
		if !settings.GetEnabled() {
			t.Fatal("memory did not default to on, so this test is no longer covering the boot state")
		}
		if settings.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING {
			t.Fatalf("settings reason = %v, want VAULT_MISSING when memory is on and the vault never opened",
				settings.GetUnavailableReason())
		}
		if settings.GetVaultRoot() != "" || settings.GetVaultWritable() {
			t.Fatalf("settings describe a vault that does not exist: root=%q writable=%v",
				settings.GetVaultRoot(), settings.GetVaultWritable())
		}

		state, err := vaultless.ListMemoryState(ctx, &turingv1.ListMemoryStateRequest{})
		if err != nil {
			t.Fatalf("ListMemoryState: %v", err)
		}
		if state.GetSettings().GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING {
			t.Fatalf("state settings reason = %v, want VAULT_MISSING",
				state.GetSettings().GetUnavailableReason())
		}
	})
}

// The toggle is a setting in the database, not a fact about a process. A
// restart is a second service over the same database: it has to come back off,
// offer nothing, refuse everything — and come back on when the user says so.
func TestMemoryToggleSurvivesARestartOfTheWholeService(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "turing.db")
	root := newVaultRoot(t)

	service, repo, _, ctx := newMemoryServiceAt(t, dbPath, root, nil)
	runID, _ := newRun(t, repo, ctx)
	setPolicies(t, repo, ctx, "safe")
	if _, err := service.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: false}); err != nil {
		t.Fatalf("SetMemoryEnabled: %v", err)
	}

	// Everything below this line is a different repository, a different vault
	// handle and a different service, over the same bytes on disk.
	restarted, restartedRepo, _, ctx := newMemoryServiceAt(t, dbPath, root, nil)
	settings, err := restarted.GetMemorySettings(ctx, &turingv1.GetMemorySettingsRequest{})
	if err != nil {
		t.Fatalf("GetMemorySettings after restart: %v", err)
	}
	if settings.GetEnabled() {
		t.Fatal("memory came back on after a restart")
	}
	if settings.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_DISABLED {
		t.Fatalf("settings reason after restart = %v, want DISABLED", settings.GetUnavailableReason())
	}
	tools, err := restarted.ListMemoryTools(ctx, &turingv1.ListMemoryToolsRequest{})
	if err != nil {
		t.Fatalf("ListMemoryTools after restart: %v", err)
	}
	if len(tools.GetTools()) != 0 {
		t.Fatalf("tools after restart = %v, want none", toolNames(tools))
	}
	for tool, args := range everyToolCall() {
		if _, err := restarted.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
			RunId: runID, ToolName: tool, Args: callArgs(t, args),
		}); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("%s after restart error = %v, want FailedPrecondition while memory is off", tool, err)
		}
	}

	// Turned back on through the restarted service, memory works again — the
	// setting is the only thing that ever decided this.
	if _, err := restarted.SetMemoryEnabled(ctx, &turingv1.SetMemoryEnabledRequest{Enabled: true}); err != nil {
		t.Fatalf("SetMemoryEnabled(true) after restart: %v", err)
	}
	tools, err = restarted.ListMemoryTools(ctx, &turingv1.ListMemoryToolsRequest{})
	if err != nil || len(tools.GetTools()) != 3 {
		t.Fatalf("tools = %v err=%v, want all three back", toolNames(tools), err)
	}
	if _, err := restarted.CallMemoryTool(ctx, &turingv1.CallMemoryToolRequest{
		RunId: runID, ToolName: ToolRemember,
		Args: callArgs(t, map[string]any{"title": "Coffee", "body": "They take their coffee black."}),
	}); err != nil {
		t.Fatalf("memory.remember after being turned back on: %v", err)
	}
	candidates, err := restartedRepo.ListMemoryCandidates(ctx, repository.MemoryCandidateQuery{Limit: 10})
	if err != nil {
		t.Fatalf("ListMemoryCandidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want the proposal the restarted service filed", len(candidates))
	}
}
