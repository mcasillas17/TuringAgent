package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// maxGRPCMessageSizeForTest mirrors internal/app's unexported
// maxGRPCMessageSize (4 * 1024 * 1024): mcpregistry cannot import
// internal/app (app depends on mcpregistry, not the reverse), so this is
// the one place that constant's value is duplicated for a marshal-size
// assertion, rather than the gRPC configuration itself living here.
const maxGRPCMessageSizeForTest = 4 * 1024 * 1024

// shortBase62Key returns a unique, minimal-length string for index i (0,
// 1, ...) using bijective base-62 numbering (the scheme spreadsheet
// column names use: ...Y, Z, AA, AB, ...): every index gets a distinct
// key, and the key space never collides the way a naive fixed-width
// truncation could. Used below to build the "many minimal tools" shape:
// as many distinct tool name/schema pairs as possible for a fixed raw
// byte budget, which maximizes protobuf's fixed *per-message* framing
// overhead relative to each tool's own tiny payload — a real, but not
// the worst, adversarial shape (see manyMinimalTools and numberArrayTool).
func shortBase62Key(i int) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	n := len(alphabet)
	i++
	var buf []byte
	for i > 0 {
		i--
		buf = append([]byte{alphabet[i%n]}, buf...)
		i /= n
	}
	return string(buf)
}

// manyMinimalTools builds toolBudget raw bytes' worth of many small,
// distinct McpToolDescriptor messages (via the real toolDescriptor
// conversion), maximizing protobuf's fixed *per-message* framing overhead
// relative to each tool's own tiny payload. Measured to about a 1.55x
// wire expansion — a real adversarial shape, but not the worst one found
// (see numberArrayTool).
func manyMinimalTools(t *testing.T, toolBudget int) []*turingv1.McpToolDescriptor {
	t.Helper()
	var tools []*turingv1.McpToolDescriptor
	total := 0
	toolIndex := 0
	for total < toolBudget {
		name := shortBase62Key(toolIndex)
		schemaJSON := `{"type":"object"}`
		tool, err := toolDescriptor(repository.MCPServerTool{
			Name: name, Policy: "safe", SchemaJSON: schemaJSON, Enabled: true, Present: true,
		})
		if err != nil {
			t.Fatalf("toolDescriptor: %v", err)
		}
		tools = append(tools, tool)
		total += len(name) + len(schemaJSON)
		toolIndex++
	}
	return tools
}

// numberArraySchemaJSON builds the raw schema JSON text shaped
// `{"type":"object","x":[0,0,0,...]}` — an array of single-digit JSON
// numbers — sized so that len(name)+len(schema) equals exactly
// toolBudget raw bytes. Shared by numberArrayTool (which wraps it into a
// McpToolDescriptor for a hand-built response) and any test that instead
// needs to write this same worst-case shape through a real repository
// write path.
func numberArraySchemaJSON(name string, toolBudget int) string {
	const prefix = `{"type":"object","x":[`
	const suffix = `]}`
	targetSchemaBytes := toolBudget - len(name)
	buf := make([]byte, 0, targetSchemaBytes)
	buf = append(buf, prefix...)
	first := true
	for len(buf)+len(suffix) < targetSchemaBytes {
		if !first {
			buf = append(buf, ',')
		}
		buf = append(buf, '0')
		first = false
	}
	buf = append(buf, suffix...)
	return string(buf)
}

// numberArrayTool builds a single McpToolDescriptor whose schema is
// exactly toolBudget raw bytes, shaped
// `{"type":"object","x":[0,0,0,...]}`: an array of single-digit JSON
// numbers. This is the true worst case found for wire expansion, not
// manyMinimalTools: each array element decodes to a Go float64 and
// converts, via structpb.NewStruct, to a google.protobuf.Value carrying a
// fixed 8-byte double -- costing roughly 9-11 wire bytes per element
// against as few as 2 raw JSON bytes ("0,"), an empirically measured
// ~5.5x expansion, versus manyMinimalTools' own ~1.55x. An earlier
// version of repository.MaxMCPRegistryToolBytes (1MiB) was sized only
// against the weaker shape and did not actually hold: a single tool
// consuming that whole 1MiB budget in this shape marshaled, by itself, to
// roughly 5.5MiB -- already past maxGRPCMessageSizeForTest before any
// server descriptor, Unsupported entry, or any other tool was even added.
func numberArrayTool(t *testing.T, toolBudget int) []*turingv1.McpToolDescriptor {
	t.Helper()
	const name = "t"
	tool, err := toolDescriptor(repository.MCPServerTool{
		Name: name, Policy: "safe", SchemaJSON: numberArraySchemaJSON(name, toolBudget), Enabled: true, Present: true,
	})
	if err != nil {
		t.Fatalf("toolDescriptor: %v", err)
	}
	return []*turingv1.McpToolDescriptor{tool}
}

// buildWorstCaseListResponse assembles a ListMcpServersResponse at the
// registry's own allowed bound — 256 non-bundled servers plus 3 bundled
// ones, each with a maximum-length url (maxMCPServerURLBytes), name
// (mcpServerNamePattern's 64-byte cap), and status message
// (maxMCPStatusMessageBytes), 256 maximally-sized Unsupported entries, and
// the entire repository.MaxMCPRegistryToolBytes aggregate tool-byte
// budget dumped onto a single server via toolsForBudget — using the real
// toolDescriptor conversion function (see manyMinimalTools/
// numberArrayTool), not a hand-rolled substitute, since that conversion is
// exactly the part whose wire-size behavior these tests exist to prove.
func buildWorstCaseListResponse(t *testing.T, toolsForBudget func(t *testing.T, toolBudget int) []*turingv1.McpToolDescriptor) *turingv1.ListMcpServersResponse {
	t.Helper()
	const (
		nonBundledServerCount = repository.MaxNonBundledMCPServers
		bundledServerCount    = 3
		unsupportedCount      = repository.MaxNonBundledMCPServers
	)
	longURL := "https://" + strings.Repeat("a", maxMCPServerURLBytes-len("https://"))
	longName := strings.Repeat("b", 64)
	longStatus := strings.Repeat("c", maxMCPStatusMessageBytes)

	response := &turingv1.ListMcpServersResponse{}
	for i := 0; i < nonBundledServerCount+bundledServerCount; i++ {
		tier := turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL
		if i >= nonBundledServerCount {
			tier = turingv1.McpServerTier_MCP_SERVER_TIER_BUNDLED
		}
		descriptor := &turingv1.McpServerDescriptor{
			ServerId:        "mcp_01ARZ3NDEKTSV4RRFFQ69G5FAV",
			Name:            longName,
			Transport:       "http",
			Url:             longURL,
			Tier:            tier,
			Enabled:         true,
			Liveness:        turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_UP,
			StatusMessage:   longStatus,
			SandboxConfined: tier == turingv1.McpServerTier_MCP_SERVER_TIER_BUNDLED,
		}
		if i == 0 {
			// Dump the entire aggregate tool budget onto one server.
			descriptor.Tools = toolsForBudget(t, repository.MaxMCPRegistryToolBytes)
		}
		response.Servers = append(response.Servers, descriptor)
	}
	for u := 0; u < unsupportedCount; u++ {
		response.Unsupported = append(response.Unsupported, &turingv1.UnsupportedMcpServer{
			Name:   fmt.Sprintf("unsupported-%d", u),
			Reason: strings.Repeat("d", maxMCPStatusMessageBytes),
		})
	}
	return response
}

func assertUnderGRPCMessageSizeWithMargin(t *testing.T, what string, encoded []byte, minimumMarginBytes int) {
	t.Helper()
	t.Logf("%s: wire=%d bytes (%.3f MiB), limit=%d bytes (%.3f MiB), margin=%d bytes (%.1f%%)",
		what, len(encoded), float64(len(encoded))/(1024*1024),
		maxGRPCMessageSizeForTest, float64(maxGRPCMessageSizeForTest)/(1024*1024),
		maxGRPCMessageSizeForTest-len(encoded), 100*float64(maxGRPCMessageSizeForTest-len(encoded))/float64(maxGRPCMessageSizeForTest))
	if len(encoded) >= maxGRPCMessageSizeForTest {
		t.Fatalf("%s marshaled to %d bytes, want strictly under maxGRPCMessageSizeForTest (%d)",
			what, len(encoded), maxGRPCMessageSizeForTest)
	}
	// A generous safety margin, not just "under the limit": this fails if
	// some future change erodes the margin down to a sliver instead of
	// silently blowing through it outright.
	if maxGRPCMessageSizeForTest-len(encoded) < minimumMarginBytes {
		t.Fatalf("%s margin is only %d bytes, want at least %d", what, maxGRPCMessageSizeForTest-len(encoded), minimumMarginBytes)
	}
}

// TestListMcpServersResponseWorstCaseStaysUnderGRPCMessageSize proves
// repository.MaxMCPRegistryToolBytes (see its own doc comment for the
// full accounting) is conservative enough in practice, not merely in
// theory, against the true worst-measured shape: a single tool whose
// schema is one large array of minimal JSON scalars (numberArrayTool),
// which converts far less efficiently than many small tools do.
func TestListMcpServersResponseWorstCaseStaysUnderGRPCMessageSize(t *testing.T) {
	response := buildWorstCaseListResponse(t, numberArrayTool)
	encoded, err := proto.Marshal(response)
	if err != nil {
		t.Fatalf("marshal worst-case ListMcpServersResponse: %v", err)
	}
	assertUnderGRPCMessageSizeWithMargin(t, "worst-case ListMcpServersResponse (number-array shape)", encoded, 512*1024)
}

// TestListMcpServersResponseManyMinimalToolsShapeStaysUnderGRPCMessageSize
// is the secondary regression case: the same aggregate budget, spread
// across many minimal, distinct tools instead of one large schema. This
// was mistakenly treated as the worst case in an earlier version of this
// budget; it is kept as its own test (rather than folded away) precisely
// because it is a materially different shape from numberArrayTool, and a
// future change could regress one without regressing the other.
func TestListMcpServersResponseManyMinimalToolsShapeStaysUnderGRPCMessageSize(t *testing.T) {
	response := buildWorstCaseListResponse(t, manyMinimalTools)
	encoded, err := proto.Marshal(response)
	if err != nil {
		t.Fatalf("marshal worst-case ListMcpServersResponse: %v", err)
	}
	assertUnderGRPCMessageSizeWithMargin(t, "worst-case ListMcpServersResponse (many-minimal-tools shape)", encoded, 512*1024)
}

// TestListMcpServersResponseWorstCaseViaUpsertToolsPathStaysUnderGRPCMessageSize
// is the UpsertTools-path counterpart to
// TestListMcpServersResponseWorstCaseStaysUnderGRPCMessageSize above: that
// test hand-builds a ListMcpServersResponse directly (bypassing the
// repository and the service entirely) to measure the worst case cheaply
// across many dimensions at once. This test instead writes the same
// worst-measured single-tool shape (numberArraySchemaJSON) through the
// real repository.UpsertTools — the bundled/skills/legacy path that,
// before this fix, enforced no aggregate budget of its own at all — at
// exactly the registry-wide MaxMCPRegistryToolBytes cap, attributed to
// "system" (a real, migration-seeded bundled server row; "skills" tools
// carry no mcp_servers row at all — see schema/0016_mcp_registry.sql —
// and so are deliberately excluded from ListMcpServers' response
// entirely, making them unsuitable for proving *this* response's wire
// size), then calls the real service.ListMcpServers and marshals its
// real response. This is narrower than the hand-built worst case (one
// server, no Unsupported entries) but proves the actual code path end to
// end: real UpsertTools enforcement, real serverDescriptor/toolDescriptor
// conversion, and a real marshal size, rather than only ever modeling
// that path synthetically.
func TestListMcpServersResponseWorstCaseViaUpsertToolsPathStaysUnderGRPCMessageSize(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	const name = "system.worst_case"
	if err := repo.UpsertTools(ctx, []repository.DiscoveredTool{{
		ServerName: "system", ToolName: name, Policy: "safe",
		SchemaJSON: numberArraySchemaJSON(name, repository.MaxMCPRegistryToolBytes),
	}}); err != nil {
		t.Fatalf("UpsertTools at exactly MaxMCPRegistryToolBytes must not be refused: %v", err)
	}

	response, err := service.ListMcpServers(ctx, &turingv1.ListMcpServersRequest{})
	if err != nil {
		t.Fatalf("ListMcpServers: %v", err)
	}
	found := false
	for _, server := range response.GetServers() {
		if server.GetName() != "system" {
			continue
		}
		for _, tool := range server.GetTools() {
			if tool.GetToolName() == name {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("test setup is broken: the worst-case tool written via UpsertTools does not appear under system's descriptor")
	}
	encoded, err := proto.Marshal(response)
	if err != nil {
		t.Fatalf("marshal ListMcpServersResponse built from a real UpsertTools write: %v", err)
	}
	assertUnderGRPCMessageSizeWithMargin(t, "ListMcpServersResponse via the real UpsertTools path", encoded, 512*1024)
}

// TestReimportMcpJsonResponseWorstCaseStaysUnderGRPCMessageSize builds a
// ReimportMcpJsonResponse at its own allowed bound. Unlike
// ListMcpServersResponse, this response carries no tool schemas at all
// (see the proto definition) — only name lists and Unsupported
// name/reason pairs, each already bounded by maxMCPImportEntries and
// maxMCPStatusMessageBytes/maxMCPUnsupportedNameBytes — so its worst case
// is dramatically smaller, but this still measures the real thing rather
// than assuming it.
func TestReimportMcpJsonResponseWorstCaseStaysUnderGRPCMessageSize(t *testing.T) {
	response := &turingv1.ReimportMcpJsonResponse{}
	longName := strings.Repeat("a", maxMCPUnsupportedNameBytes)
	for i := 0; i < maxMCPImportEntries; i++ {
		response.Imported = append(response.Imported, longName)
		response.Skipped = append(response.Skipped, longName)
		response.Refused = append(response.Refused, &turingv1.UnsupportedMcpServer{
			Name:   longName,
			Reason: strings.Repeat("b", maxMCPStatusMessageBytes),
		})
	}
	encoded, err := proto.Marshal(response)
	if err != nil {
		t.Fatalf("marshal worst-case ReimportMcpJsonResponse: %v", err)
	}
	t.Logf("worst-case ReimportMcpJsonResponse: wire=%d bytes, limit=%d bytes", len(encoded), maxGRPCMessageSizeForTest)
	if len(encoded) >= maxGRPCMessageSizeForTest {
		t.Fatalf("worst-case ReimportMcpJsonResponse marshaled to %d bytes, want strictly under maxGRPCMessageSizeForTest (%d)",
			len(encoded), maxGRPCMessageSizeForTest)
	}
}

// A sanity check on the measurement technique itself: structpb.NewStruct
// must actually succeed on the minimal schema used above, so the
// worst-case test above is exercising the real conversion path rather
// than silently short-circuiting on an error some future schema-shape
// change could introduce.
func TestToolDescriptorHandlesMinimalSchemaUsedByWorstCaseTest(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"type":"object"}`), &schema); err != nil {
		t.Fatal(err)
	}
	if _, err := structpb.NewStruct(schema); err != nil {
		t.Fatalf("structpb.NewStruct on the minimal schema failed: %v", err)
	}
}
