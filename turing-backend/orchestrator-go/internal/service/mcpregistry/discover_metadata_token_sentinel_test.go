package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	toolpolicy "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/tools"
	"google.golang.org/protobuf/encoding/protojson"
)

// This file is the live-discovery counterpart to import_atomic_test.go's
// own TestImportJSONTokenSentinelInToolNameRefusesEntryFailClosedAndSentinelFree
// family: a malicious or merely compromised live tools/list endpoint can
// echo the server's own configured bearer token back into a tool's name,
// description, or schema. Before this fix, discover() trusted that
// response outright — extracting and interpolating the tool's own name
// into an error before ever checking it, and handing the built
// DiscoveredTool list to RecordDiscovery regardless — so the only thing
// standing between an echoed bearer and a persisted, ListMcpServers-
// visible row was mcpClient's own marker-substitution redaction, which
// replaces a matched token with the fixed "[redacted]" text and then
// still lets discovery succeed with that (merely scrubbed, still
// attacker-shaped) metadata. This suite proves discover() now applies
// exactly the same fail-closed treatment buildImportTools already gives a
// static mcp.json "tools" snapshot: a raw tool object carrying the token
// anywhere is refused outright, before persistence, with the one fixed,
// generic mcpToolDefinitionRefusedMessage.

// discoverTokenRefusalCase is the one setup every "malicious tools/list
// echoes the bearer" scenario below shares: it registers a fresh
// remote-url server named "vendor" with sentinel sealed as its bearer
// token (via the repository directly, like enable_audit_test.go and
// rediscovery_policy_test.go already do, bypassing RegisterMcpServer's
// own front-end validation, which is unrelated to what this proves),
// points it at an httptest vendor whose tools/list reply is exactly
// toolsResult, and enables it. It asserts the one outcome finding
// requires regardless of *where* in the tool's raw metadata sentinel
// shows up: the enable RPC itself still succeeds (a refused discovery is
// not a refused enable), the server stays enabled and down with the one
// fixed, generic mcpToolDefinitionRefusedMessage — never a message built
// from the tool's own (sentinel-bearing) name or schema — the response's
// own Tools list (and, separately, ListMcpServers) stays empty (proving
// RecordDiscovery never ran), and the one audit record stays limited to
// its four allowed keys with discoverySucceeded=false. It returns the
// database and the audit recorder so a caller can layer a full sentinel
// sweep on top for a sentinel long and unique enough that one is
// meaningful (see runFullSentinelSweepDiscoverTokenRefusalCase).
func discoverTokenRefusalCase(t *testing.T, sentinel string, toolsResult map[string]any) (*db.DB, *turingv1.McpServerDescriptor, *recordingAuditRecorder) {
	t.Helper()
	database, service, repo := newSentinelSweepableRegistryService(t)
	vendor := newScalarEchoServer(t, toolsResult)
	service.httpClient = vendor.Client()

	recorder := &recordingAuditRecorder{}
	service.SetAuditRecorder(recorder)

	sealed, err := service.sealServerToken("vendor", sentinel)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: vendor.URL, SealedToken: sealed, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := registered.Server

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !descriptor.GetEnabled() {
		t.Fatal("a server refused for token-bearing discovery metadata must remain enabled")
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_DOWN {
		t.Fatalf("liveness = %v, want down", descriptor.GetLiveness())
	}
	if descriptor.GetStatusMessage() != mcpToolDefinitionRefusedMessage {
		t.Fatalf("status message = %q, want the fixed generic %q: an interpolated or specific message would leak (or hint at) the offending tool metadata", descriptor.GetStatusMessage(), mcpToolDefinitionRefusedMessage)
	}
	if len(descriptor.GetTools()) != 0 {
		t.Fatalf("response Tools = %+v, want none: RecordDiscovery must never run when discovery is refused", descriptor.GetTools())
	}

	tools, err := repo.ListMCPServerTools(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("repository tools = %+v, want none persisted after a refused discovery", tools)
	}

	listResponse, err := service.ListMcpServers(context.Background(), &turingv1.ListMcpServersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range listResponse.GetServers() {
		if entry.GetServerId() == server.ID && len(entry.GetTools()) != 0 {
			t.Fatalf("ListMcpServers server %q Tools = %+v, want none persisted after a refused discovery", server.ID, entry.GetTools())
		}
	}

	if len(recorder.records) != 1 {
		t.Fatalf("records = %+v, want exactly one", recorder.records)
	}
	record := recorder.records[0]
	if record.action != "mcp.server.enabled" {
		t.Fatalf("action = %q, want mcp.server.enabled", record.action)
	}
	onlyExpectedAuditKeys(t, record.payload, "name", "tier", "remoteDiscoveryAttempted", "discoverySucceeded")
	if record.payload["discoverySucceeded"] != false {
		t.Fatalf("payload discoverySucceeded = %v, want false", record.payload["discoverySucceeded"])
	}

	return database, descriptor, recorder
}

// runFullSentinelSweepDiscoverTokenRefusalCase is discoverTokenRefusalCase
// plus a full sentinel sweep of the response, the audit payload, the
// process log, and every non-BLOB database column (see
// assertDatabaseSentinelFreeExceptSealedToken) — appropriate only for a
// sentinel long and unique enough that finding it (or a substantial
// prefix of it) anywhere really would mean it leaked. It is deliberately
// not used for a short, common-shaped value like "true", "null", a plain
// digit string, or (especially) JSON structural punctuation like `":"`:
// see TestDiscoverRefusesStructurallySpanningTokenInDefaultedSchema's own
// comment for why that one stays narrower.
func runFullSentinelSweepDiscoverTokenRefusalCase(t *testing.T, sentinel string, toolsResult map[string]any) {
	t.Helper()
	var logged strings.Builder
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	database, descriptor, recorder := discoverTokenRefusalCase(t, sentinel, toolsResult)

	encodedDescriptor, err := protojson.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSentinelFree(t, "SetMcpServerEnabled response", string(encodedDescriptor), sentinel)
	assertStringSentinelFree(t, "audit payload", fmt.Sprintf("%+v", recorder.records[0].payload), sentinel)
	assertStringSentinelFree(t, "process log", logged.String(), sentinel)
	assertDatabaseSentinelFreeExceptSealedToken(t, database, sentinel)
}

// The one test to keep if every other one in this file were deleted: a
// long, unique sentinel embedded in a live tools/list tool's own name
// must refuse the whole discovery attempt fail-closed, and must never
// appear anywhere reachable — the enable response, ListMcpServers, the
// audit trail, the process log, or any plaintext database column.
func TestDiscoverRefusesToolNameEchoingConfiguredBearerFailClosedAndSentinelFree(t *testing.T) {
	const sentinel = "mcp-discover-sentinel-6f1a9c3e7b2d5084-do-not-leak"
	runFullSentinelSweepDiscoverTokenRefusalCase(t, sentinel, map[string]any{"tools": []any{map[string]any{
		"name": "vendor." + sentinel, "inputSchema": map[string]any{"type": "object"},
	}}})
}

// A tool's "description" is never stored or returned (DiscoveredTool has
// no field for it), but a configured bearer token appearing verbatim in
// one must still refuse the whole discovery attempt the same way a token
// in the name or schema does — the refusal signal must not depend on
// whether this package happens to persist the field it was found in.
// Reusing mcpRawMetadataContainsToken over the raw tool map (rather than
// checking only the fields DiscoveredTool stores) is what sweeps
// "description" in for free here.
func TestDiscoverRefusesToolDescriptionEchoingConfiguredBearerEvenThoughNeverStored(t *testing.T) {
	const sentinel = "mcp-discover-sentinel-2a7e4c9f1b6d3057-do-not-leak"
	runFullSentinelSweepDiscoverTokenRefusalCase(t, sentinel, map[string]any{"tools": []any{map[string]any{
		"name": "vendor.lookup", "description": sentinel, "inputSchema": map[string]any{"type": "object"},
	}}})
}

// The recursive raw-metadata scan must walk a schema map's own *keys*,
// not only its values.
func TestDiscoverRefusesSchemaMapKeyEchoingConfiguredBearer(t *testing.T) {
	const sentinel = "mcp-discover-sentinel-9c3f7a1e6b2d4085-do-not-leak"
	runFullSentinelSweepDiscoverTokenRefusalCase(t, sentinel, map[string]any{"tools": []any{map[string]any{
		"name": "vendor.lookup",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{sentinel: map[string]any{"type": "string"}},
		},
	}}})
}

// The same scan must also reach a string value nested inside a list,
// nested inside the schema — not only a schema's own top-level values.
func TestDiscoverRefusesSchemaStringValueEchoingConfiguredBearer(t *testing.T) {
	const sentinel = "mcp-discover-sentinel-4b8d2f6a9c1e7053-do-not-leak"
	runFullSentinelSweepDiscoverTokenRefusalCase(t, sentinel, map[string]any{"tools": []any{map[string]any{
		"name": "vendor.lookup",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"note": map[string]any{"enum": []any{"safe", sentinel}},
			},
		},
	}}})
}

// mcpRawMetadataContainsToken's recursive walk only ever inspects
// strings (a map key, or a string value) — a schema value that decodes
// to a Go float64 (a JSON number, never a string) passes through it
// silently, even though json.Marshal renders it, verbatim, as the
// token's own digits once discover() serializes the schema into the
// schemaJSON it is about to store. The post-marshal scan of that exact
// schemaJSON text is what still catches this. The token is chosen to
// look exactly like a JSON number so it can be embedded unquoted below.
//
// This deliberately uses discoverTokenRefusalCase, not the full-sweep
// wrapper: an arbitrary digit string is exactly the shape of a great
// many other legitimate, unrelated fields (ids, sizes, timestamps), so a
// database- or response-wide substring sweep for one risks a false
// positive unrelated to any real leak. discoverTokenRefusalCase's own
// assertions — the exact fixed StatusMessage, and an empty Tools list
// everywhere — already prove the refusal precisely without that risk.
func TestDiscoverRefusesSchemaNumericValueEchoingConfiguredBearer(t *testing.T) {
	const sentinel = "58217395104"
	discoverTokenRefusalCase(t, sentinel, map[string]any{"tools": []any{map[string]any{
		"name":        "vendor.lookup",
		"inputSchema": map[string]any{"type": "object", "minimum": json.Number(sentinel)},
	}}})
}

// The boolean counterpart: a token equal to the literal text "true" is
// exactly as findable in a schema's serialized JSON boolean value as it
// would be in a string value, but a Go bool never passes through
// mcpRawMetadataContainsToken's string-only scan either.
//
// This deliberately uses discoverTokenRefusalCase, not the full-sweep
// wrapper: "true" is also the literal JSON protojson renders for any
// *other*, unrelated populated boolean field — including this very
// response's own "enabled" field, since a refused discovery still
// leaves the server enabled — so a response/log/database-wide substring
// sweep for it would be a guaranteed false positive, not evidence of a
// leak. discoverTokenRefusalCase's own assertions already prove the
// refusal precisely without that risk.
func TestDiscoverRefusesSchemaBooleanValueEchoingConfiguredBearer(t *testing.T) {
	const sentinel = "true"
	discoverTokenRefusalCase(t, sentinel, map[string]any{"tools": []any{map[string]any{
		"name":        "vendor.lookup",
		"inputSchema": map[string]any{"type": "object", "readOnly": true},
	}}})
}

// The JSON-null counterpart: json.Unmarshal decodes a JSON null into an
// untyped nil interface, which (like the float64 and bool cases above)
// mcpRawMetadataContainsToken's recursive scan never inspects, even
// though json.Marshal renders it back out as the literal text "null"
// once the schema is serialized.
//
// This deliberately uses discoverTokenRefusalCase, not the full-sweep
// wrapper, for the same reason the boolean case above does: "null" is a
// common word that could plausibly appear in unrelated text (a log
// line, a schema's own placeholder copy) for reasons that have nothing
// to do with this fix.
func TestDiscoverRefusesSchemaNullValueEchoingConfiguredBearer(t *testing.T) {
	const sentinel = "null"
	discoverTokenRefusalCase(t, sentinel, map[string]any{"tools": []any{map[string]any{
		"name":        "vendor.lookup",
		"inputSchema": map[string]any{"type": "object", "default": nil},
	}}})
}

// The token-metadata scan must win regardless of what ELSE is wrong with
// the tool: a tool whose name carries the sentinel AND whose inputSchema
// is independently invalid (a non-object root type) must still refuse
// with the one generic, sentinel-free reason — never the schema-
// validation message, which would embed the tool's (sentinel-bearing)
// name verbatim via %q. This is the discriminating case a valid-schema-
// only sentinel test cannot catch: it proves the token check runs before
// the tool's name is ever extracted for interpolation into another
// error, not merely that it runs at some point.
func TestDiscoverTokenInNameWithInvalidSchemaStillRefusesGenericallyAndSentinelFree(t *testing.T) {
	const sentinel = "mcp-discover-sentinel-7e1b4a8f2c6d9031-do-not-leak"
	runFullSentinelSweepDiscoverTokenRefusalCase(t, sentinel, map[string]any{"tools": []any{map[string]any{
		"name": "vendor." + sentinel, "inputSchema": map[string]any{"type": "array"},
	}}})
}

// TestDiscoverRefusesStructurallySpanningTokenInDefaultedSchema proves
// the schema scan catches a token that never exists as a substring of
// any single decoded string, number, bool, or null value at all — only
// across the structural JSON syntax json.Marshal emits around them. A
// tool with no inputSchema at all still gets discover()'s own default
// schema{"type": "object"} injected and marshaled, which already
// contains the quote-colon-quote between "type" and "object" — so a
// token equal to exactly that (`":"`) is present the moment *any* tool
// is discovered without an explicit schema, structurally, regardless of
// what the tool's own name or any other field says.
//
// This intentionally does not use the full sentinel sweep the other
// scenarios in this file do: `":"` is ordinary JSON punctuation that
// appears in essentially every stored JSON column this package writes
// (an audit payload, another tool's own ordinary schema_json) for
// entirely unrelated reasons, so a database- or log-wide substring sweep
// for it would be a near-guaranteed false positive, not evidence of a
// leak. discoverTokenRefusalCase's own assertions — the exact fixed
// StatusMessage, and an empty Tools list everywhere — already prove the
// refusal precisely without that risk.
func TestDiscoverRefusesStructurallySpanningTokenInDefaultedSchema(t *testing.T) {
	const sentinel = `":"`
	discoverTokenRefusalCase(t, sentinel, map[string]any{"tools": []any{map[string]any{
		"name": "vendor.lookup",
	}}})
}

// TestDiscoverRefusesToolNameEchoForLocalContainerTierToo proves the fix
// applies to a local-container server's enable-time discovery exactly as
// it does a remote-url one: discover() is the one shared function both
// tiers call from SetMcpServerEnabled, so this is deliberately the same
// scenario as TestDiscoverRefusesToolNameEchoingConfiguredBearerFailClosedAndSentinelFree
// above, only with Tier: MCPServerTierLocalContainer.
func TestDiscoverRefusesToolNameEchoForLocalContainerTierToo(t *testing.T) {
	const sentinel = "mcp-discover-sentinel-local-3d8f1a6c9e2b4075-do-not-leak"
	database, service, repo := newSentinelSweepableRegistryService(t)
	vendor := newScalarEchoServer(t, map[string]any{"tools": []any{map[string]any{
		"name": "vendor-local." + sentinel, "inputSchema": map[string]any{"type": "object"},
	}}})
	service.httpClient = vendor.Client()

	sealed, err := service.sealServerToken("vendor-local", sentinel)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor-local", URL: vendor.URL, SealedToken: sealed, Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := registered.Server

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !descriptor.GetEnabled() {
		t.Fatal("a local-container server refused for token-bearing discovery metadata must remain enabled")
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_DOWN {
		t.Fatalf("liveness = %v, want down", descriptor.GetLiveness())
	}
	if descriptor.GetStatusMessage() != mcpToolDefinitionRefusedMessage {
		t.Fatalf("status message = %q, want the fixed generic %q", descriptor.GetStatusMessage(), mcpToolDefinitionRefusedMessage)
	}
	if len(descriptor.GetTools()) != 0 {
		t.Fatalf("response Tools = %+v, want none", descriptor.GetTools())
	}
	tools, err := repo.ListMCPServerTools(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools = %+v, want none: local-container discovery must fail closed exactly like remote-url", tools)
	}
	assertDatabaseSentinelFreeExceptSealedToken(t, database, sentinel)
}

// TestDiscoverRefusalPreservesPriorSnapshotAndEditedPolicy mirrors
// rediscovery_policy_test.go's own
// TestImportedRemoteServerDiscoveryFailurePreservesSnapshotAndPolicy, but
// the enable-time discovery attempt fails because the vendor's tools/list
// reply now echoes the configured bearer rather than because the vendor
// returns a JSON-RPC error: a prior snapshot (and its edited policy) from
// an earlier, legitimate import/discovery must be left exactly as it
// was — RecordDiscovery is never called on a refused discover() — while
// the server itself is still marked enabled and down.
func TestDiscoverRefusalPreservesPriorSnapshotAndEditedPolicy(t *testing.T) {
	const sentinel = "mcp-discover-sentinel-preserve-8b3e6a1f9c2d5074-do-not-leak"
	service, repo := newRegistryTestService(t)
	vendor := newScalarEchoServer(t, map[string]any{"tools": []any{map[string]any{
		"name": "vendor." + sentinel, "inputSchema": map[string]any{"type": "object"},
	}}})
	service.httpClient = vendor.Client()

	sealed, err := service.sealServerToken("vendor", sentinel)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repo.ImportMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: vendor.URL, SealedToken: sealed, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := result.Server
	const snapshotSchema = `{"type":"object","properties":{"snapshot":{}}}`
	if err := service.RecordDiscovery(context.Background(), server.ID, []DiscoveredTool{
		{Name: "vendor.lookup", SchemaJSON: snapshotSchema},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateMcpToolPolicy(context.Background(), &turingv1.UpdateMcpToolPolicyRequest{
		ServerId: server.ID, ToolName: "vendor.lookup",
		Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE,
	}); err != nil {
		t.Fatal(err)
	}

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !descriptor.GetEnabled() {
		t.Fatal("a server refused for token-bearing discovery metadata must remain enabled")
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_DOWN {
		t.Fatalf("liveness = %v, want down", descriptor.GetLiveness())
	}

	tools, err := repo.ListMCPServerTools(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %+v, want exactly the prior snapshot preserved (RecordDiscovery must never run for a refused discovery)", tools)
	}
	tool := tools[0]
	if tool.Name != "vendor.lookup" || tool.Policy != "safe" || tool.SchemaJSON != snapshotSchema || !tool.Present {
		t.Fatalf("tool = %+v, want the prior snapshot and edited policy fully preserved", tool)
	}
}

// TestDiscoverEnableCleanToolsListSucceedsWithConfiguredUnrelatedTokenAndDefaultPolicy
// is the negative-space proof every fail-closed scan above needs: an
// ordinary, legitimate tools/list response that never mentions the
// server's own configured bearer token must still discover and persist
// its tool exactly as before, with the vendor's own schema untouched and
// the usual newly-discovered-tool default policy assigned — the new
// checks must never trip on metadata that simply doesn't contain the
// token.
func TestDiscoverEnableCleanToolsListSucceedsWithConfiguredUnrelatedTokenAndDefaultPolicy(t *testing.T) {
	const token = "vendor-real-secret-unrelated-9f2c8b3e"
	service, repo := newRegistryTestService(t)
	vendor := newScalarEchoServer(t, map[string]any{"tools": []any{map[string]any{
		"name": "vendor.lookup", "description": "looks things up",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
	}}})
	service.httpClient = vendor.Client()

	sealed, err := service.sealServerToken("vendor", token)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := repo.RegisterMCPServer(context.Background(), repository.ImportedMCPServer{
		Name: "vendor", URL: vendor.URL, SealedToken: sealed, Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := registered.Server

	descriptor, err := service.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: server.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.GetLiveness() != turingv1.McpServerLiveness_MCP_SERVER_LIVENESS_UP {
		t.Fatalf("liveness = %v, want up: a clean discovery unrelated to the configured token must still succeed", descriptor.GetLiveness())
	}
	tools, err := repo.ListMCPServerTools(context.Background(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %+v, want exactly one discovered tool", tools)
	}
	tool := tools[0]
	if tool.Name != "vendor.lookup" {
		t.Fatalf("tool name = %q, want vendor.lookup", tool.Name)
	}
	if tool.Policy != string(toolpolicy.DefaultPolicyFor("vendor", "vendor.lookup")) {
		t.Fatalf("tool policy = %q, want the default policy for a newly discovered tool", tool.Policy)
	}
	if !strings.Contains(tool.SchemaJSON, "query") {
		t.Fatalf("tool schema = %q, want the vendor's own schema preserved untouched", tool.SchemaJSON)
	}
}
