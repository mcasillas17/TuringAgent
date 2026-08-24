package runtime

import (
	"context"
	"fmt"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/encoding/protojson"
)

// bundledDiscoveredToolsMirroringRealSchemas returns a *turingv1.DiscoveredTool
// for every tool TuringAgent's own bundled servers actually register today:
// "system" (turing-backend/mcp-system/internal/tools/system.go's List),
// "files" (turing-backend/mcp-files/cmd/server/main.go's listTools), and
// "skills" (turing-backend/agent-runtime-go/internal/agent/skill_tools.go's
// skillToolLister.ListTools). This package cannot import any of those —
// mcp-system and mcp-files are separate Go modules entirely, and
// agent-runtime-go/internal/agent is a different module-internal package
// tree this one is not rooted under — so these schemas are mirrored by
// hand, deliberately kept byte-for-byte faithful to those real
// definitions (including their real field names, descriptions, and
// numeric limits such as mcp-files' MaxSandboxPathBytes=4096,
// MaxSandboxComponentBytes=255, MaxSandboxPathDepth=64,
// MaxReadBytes=65536, and MaxMutationContentBytes=524288) rather than
// approximated or minimized, specifically so
// TestConnectWorkerSucceedsWhenThirdPartyToolsFillExactlyTheReservedSubBudget's
// own measurement of their combined byte total is a real proof, not an
// optimistic guess. Real tool *names* (system.health, files.read, and so
// on) are independently confirmed by
// internal/service/tools.DefaultPolicyFor's own seedPolicies map, which
// this package *can* (and does) import.
func bundledDiscoveredToolsMirroringRealSchemas(t *testing.T) []*turingv1.DiscoveredTool {
	t.Helper()
	emptySchema := map[string]any{
		"type": "object", "properties": map[string]any{}, "additionalProperties": false,
	}
	pathSchema := map[string]any{
		"type":      "string",
		"minLength": 1,
		"pattern":   `\S`,
		"description": fmt.Sprintf(
			"Sandbox-relative path limited to %d bytes, %d bytes per component, and %d components; byte limits are enforced by the server because JSON Schema maxLength counts characters.",
			4096, 255, 64,
		),
	}
	nonBlank := func(description string) map[string]any {
		return map[string]any{"type": "string", "minLength": 1, "pattern": `\S`, "description": description}
	}
	integerSchema := func(minimum, maximum int) map[string]any {
		return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
	}
	objectSchema := func(properties map[string]any, required []any) map[string]any {
		return map[string]any{
			"type": "object", "properties": properties, "required": required, "additionalProperties": false,
		}
	}
	contentSchema := map[string]any{
		"type":        "string",
		"description": fmt.Sprintf("UTF-8 content with a %d-byte server-enforced limit; maxLength is intentionally omitted because it counts characters.", 524288),
	}
	const encodedHashLength = len("sha256:") + 64
	expectedHashSchema := map[string]any{
		"type": "string", "minLength": encodedHashLength, "maxLength": encodedHashLength,
		"pattern": `^sha256:[0-9a-f]{64}$`, "description": "Exact lowercase sha256: digest of the expected current content.",
	}

	tools := []*turingv1.DiscoveredTool{
		// system (turing-backend/mcp-system/internal/tools/system.go).
		{ServerName: "system", ToolName: "system.health", Schema: mustStruct(t, emptySchema)},
		{ServerName: "system", ToolName: "system.time", Schema: mustStruct(t, emptySchema)},
		{ServerName: "system", ToolName: "system.echo", Schema: mustStruct(t, objectSchema(map[string]any{
			"text": map[string]any{
				"type": "string", "maxLength": 65536,
				"description": "Text to echo, limited to 65536 Unicode characters.",
			},
		}, []any{}))},
		{ServerName: "system", ToolName: "system.info", Schema: mustStruct(t, emptySchema)},
		// files (turing-backend/mcp-files/cmd/server/main.go).
		{ServerName: "files", ToolName: "files.list", Schema: mustStruct(t, objectSchema(map[string]any{
			"path": pathSchema, "limit": integerSchema(1, 1000),
		}, []any{}))},
		{ServerName: "files", ToolName: "files.search", Schema: mustStruct(t, objectSchema(map[string]any{
			"path": pathSchema, "query": nonBlank("Nonblank text to find."), "limit": integerSchema(1, 200),
		}, []any{"query"}))},
		{ServerName: "files", ToolName: "files.read", Schema: mustStruct(t, objectSchema(map[string]any{
			"path": pathSchema, "maxBytes": integerSchema(1, 65536),
		}, []any{"path"}))},
		{ServerName: "files", ToolName: "files.create", Schema: mustStruct(t, objectSchema(map[string]any{
			"path": pathSchema, "content": contentSchema,
		}, []any{"path", "content"}))},
		{ServerName: "files", ToolName: "files.update", Schema: mustStruct(t, objectSchema(map[string]any{
			"path": pathSchema, "content": contentSchema, "expectedHash": expectedHashSchema,
		}, []any{"path", "content"}))},
		// skills (turing-backend/agent-runtime-go/internal/agent/skill_tools.go).
		{ServerName: "skills", ToolName: "skills_list", Schema: mustStruct(t, map[string]any{
			"type": "object", "properties": map[string]any{}, "additionalProperties": false,
		})},
		{ServerName: "skills", ToolName: "skill_view", Schema: mustStruct(t, objectSchema(map[string]any{
			"id":   map[string]any{"type": "string", "description": "Exact skill id such as writing/tone"},
			"path": map[string]any{"type": "string", "description": "Optional relative reference path inside the skill"},
		}, []any{"id"}))},
	}
	return tools
}

// bundledSchemaTotalRawBytes returns the same registry-wide raw
// (tool_name + schema_json) byte total repository.aggregateAllToolBytes/
// aggregateThirdPartyToolBytes would count for every tool
// bundledDiscoveredToolsMirroringRealSchemas returns, using the exact
// same protojson.Marshal conversion decodeDiscoveredTools itself applies
// (see capabilities.go) — not a hand-estimated size.
func bundledSchemaTotalRawBytes(t *testing.T, tools []*turingv1.DiscoveredTool) int {
	t.Helper()
	total := 0
	for _, tool := range tools {
		schemaJSON, err := protojson.Marshal(tool.GetSchema())
		if err != nil {
			t.Fatalf("protojson.Marshal: %v", err)
		}
		total += len(tool.GetToolName()) + len(schemaJSON)
	}
	return total
}

// TestFirstPartyBundledToolSchemasFitWithinReservedHeadroom measures the
// real, combined byte total of every tool TuringAgent's own bundled
// servers register today (see bundledDiscoveredToolsMirroringRealSchemas)
// and asserts it stays comfortably under the headroom
// repository.MaxThirdPartyMCPRegistryToolBytes guarantees UpsertTools —
// with generous margin for those schemas to grow, not merely scraping by.
// This is the measurement repository.MaxThirdPartyMCPRegistryToolBytes's
// own doc comment cites as its justification for 128KiB specifically:
// if this test ever failed, that constant's own margin claim would no
// longer hold and it would need to be revisited.
func TestFirstPartyBundledToolSchemasFitWithinReservedHeadroom(t *testing.T) {
	tools := bundledDiscoveredToolsMirroringRealSchemas(t)
	total := bundledSchemaTotalRawBytes(t, tools)
	headroom := repository.MaxMCPRegistryToolBytes - repository.MaxThirdPartyMCPRegistryToolBytes
	t.Logf("combined real bundled tool schema total = %d bytes, reserved headroom = %d bytes (%.2f%% used)",
		total, headroom, 100*float64(total)/float64(headroom))
	if total >= headroom {
		t.Fatalf("combined bundled tool schema total = %d bytes, want strictly under the reserved headroom (%d bytes)", total, headroom)
	}
	// A generous safety margin, not just "under the headroom": today's 11
	// real bundled tools total well under 2KiB combined, so requiring at
	// least 90% of the 128KiB headroom to remain free leaves enormous
	// room for that set to grow before this constant would need
	// revisiting.
	const minimumFreeFraction = 0.90
	free := headroom - total
	if float64(free)/float64(headroom) < minimumFreeFraction {
		t.Fatalf("only %.2f%% of the reserved headroom is free (%d of %d bytes), want at least %.0f%%",
			100*float64(free)/float64(headroom), free, headroom, 100*minimumFreeFraction)
	}
}

// existingThirdPartyRawBytes sums, through exported repository methods
// only, the same raw (tool_name + schema_json) byte total
// repository.aggregateThirdPartyToolBytes computes internally for every
// non-bundled server — present and withdrawn rows alike (ListMCPServerTools
// already returns both). Used so this test's own "fill to exactly the
// cap" arithmetic does not need to hardcode (and risk drifting from)
// whatever a shared test harness like newHarness happens to seed ahead of
// time (e.g. its own small "custom" server).
func existingThirdPartyRawBytes(t *testing.T, repo *repository.Repository) int {
	t.Helper()
	servers, err := repo.ListMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, server := range servers {
		if server.Tier == repository.MCPServerTierBundled {
			continue
		}
		tools, err := repo.ListMCPServerTools(context.Background(), server.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range tools {
			total += len(tool.Name) + len(tool.SchemaJSON)
		}
	}
	return total
}

// thirdPartyToolOfExactRawSize returns a single ImportedMCPServer tool
// whose raw (len(Name) + len(SchemaJSON)) byte size is exactly n, mirroring
// the repository package's own toolOfExactRawSize (unreachable from this
// package).
func thirdPartyToolOfExactRawSize(t *testing.T, name string, n int) repository.MCPServerTool {
	t.Helper()
	const prefix = `{"type":"object","d":"`
	const suffix = `"}`
	pad := n - len(name) - len(prefix) - len(suffix)
	if pad < 0 {
		t.Fatalf("thirdPartyToolOfExactRawSize(%q, %d): target size too small", name, n)
	}
	padding := make([]byte, pad)
	for i := range padding {
		padding[i] = 'x'
	}
	return repository.MCPServerTool{
		Name: name, Policy: "safe", SchemaJSON: prefix + string(padding) + suffix, Enabled: true, Present: true,
	}
}

// TestConnectWorkerSucceedsWhenThirdPartyToolsFillExactlyTheReservedSubBudget
// is the end-to-end proof that repository.MaxThirdPartyMCPRegistryToolBytes
// genuinely guarantees a connecting worker its own share of the registry-
// wide aggregate, regardless of how many third-party servers already
// exist: a third-party server's own tools are imported directly through
// the repository (mirroring what an operator's mcp.json import or direct
// registration could already have committed before any worker ever
// connects) up to *exactly* the third-party sub-budget's own cap, and a
// worker then connects carrying every real bundled tool capability
// ("system", "files", "skills" — see
// bundledDiscoveredToolsMirroringRealSchemas) as its own
// WorkerReady.Capabilities.Tools. ConnectWorker's own persistDiscoveredTools
// (which calls repository.UpsertTools, the path that retains the full,
// separate registry-wide aggregate cap) must still succeed: the
// third-party sub-budget's whole purpose is to leave UpsertTools' own
// half of the aggregate untouched no matter how full the third-party
// half already is. One more third-party byte, attempted afterward, must
// then be refused.
func TestConnectWorkerSucceedsWhenThirdPartyToolsFillExactlyTheReservedSubBudget(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	existing := existingThirdPartyRawBytes(t, h.repo)
	remaining := repository.MaxThirdPartyMCPRegistryToolBytes - existing
	if remaining <= 0 {
		t.Fatalf("test setup is broken: existing third-party bytes (%d) already meet or exceed the sub-budget", existing)
	}
	if _, err := h.repo.ImportMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor-cap-fill", URL: "http://vendor-cap-fill:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
		Tools: []repository.MCPServerTool{thirdPartyToolOfExactRawSize(t, "vendor-cap-fill.a", remaining)},
	}); err != nil {
		t.Fatalf("filling the third-party sub-budget to exactly its own cap must succeed: %v", err)
	}

	capabilities := modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 8192, 1,
	)
	capabilities.Tools = bundledDiscoveredToolsMirroringRealSchemas(t)
	stream := connectWorkerCapabilities(t, h, "worker-third-party-headroom", "registration-third-party-headroom", capabilities)
	defer func() { _ = stream.CloseSend() }()

	// The worker's own bundled tools genuinely persisted: UpsertTools
	// really ran, using its own, separate share of the aggregate.
	available, err := h.repo.MCPToolAvailable(ctx, "system", "system.health")
	if err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("system.health is not available after ConnectWorker despite the third-party sub-budget being exactly full")
	}

	// One more third-party byte, on top of the already-exact sub-budget,
	// must be refused.
	_, err = h.repo.ImportMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor-one-too-many", URL: "http://vendor-one-too-many:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
		Tools: []repository.MCPServerTool{{Name: "x", Policy: "safe", SchemaJSON: `{"type":"object"}`, Enabled: true, Present: true}},
	})
	if err == nil {
		t.Fatal("a third-party import one byte beyond the exactly-full sub-budget must be refused")
	}
}
