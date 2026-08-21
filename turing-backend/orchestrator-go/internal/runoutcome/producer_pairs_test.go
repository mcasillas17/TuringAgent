package runoutcome

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The approved mapping table says which pairs a normalizer accepts. It cannot
// say whether the runtime ever reports a pair that is not on it — and an
// off-matrix pair is silent: the failure still terminalizes the run, but its
// code is replaced by CodeUnknown and the reason falls back to the origin
// family's generic outcome. Nothing logs it, no test fails, and the operator
// loses the one diagnostic TUR-009 kept.
//
// So the producing call sites are read from source and every literal pair they
// report is required to survive normalization intact. The hand-written table
// below is the approved inventory; the scan is what stops a new call site from
// quietly joining it.
type producerPair struct {
	origin Origin
	code   string
}

// runtimeProducerPairs is every (origin, code) the agent runtime reports from a
// call site that spells both out. It is written by hand rather than derived
// from the scan, so adding a producer is a deliberate edit here as well as
// there.
func runtimeProducerPairs() map[producerPair]string {
	return map[producerPair]string{
		// internal/agent: context and provider selection.
		{OriginContextAssembly, "message_fetch_failed"}:             "message fetch",
		{OriginContextAssembly, "context_budget_exceeded"}:          "context budget",
		{OriginExternalProvider, "external_agent_unavailable"}:      "external agent routing",
		{OriginProviderConfiguration, "model_provider_unavailable"}: "unconfigured provider",

		// internal/agent: the model stream itself.
		{OriginProviderTransport, "model_timeout"}:                 "model deadline",
		{OriginProviderTransport, "model_stream_failed"}:           "stream could not start",
		{OriginProviderTransport, "model_stream_error"}:            "stream ended without a terminal event",
		{OriginProviderOutputGuard, "model_output_limit_exceeded"}: "output byte guard",

		// internal/agent: tools.
		{OriginToolInfrastructure, "tool_discovery_failed"}:   "tool discovery",
		{OriginToolInfrastructure, "tool_runner_unavailable"}: "debug tool has no MCP client",
		{OriginToolExecution, "tool_call_failed"}:             "debug tool execution",
		{OriginToolGuard, "tool_call_limit_exceeded"}:         "per-run tool call budget",
		{OriginToolGuard, "tool_result_limit_exceeded"}:       "per-run tool result budget",

		// internal/worker.
		{OriginDispatch, "worker_busy"}:        "worker at capacity",
		{OriginWorkerRuntime, "runtime_error"}: "executor error",
	}
}

// TestEveryRuntimeProducerPairNormalizesToAKnownCode is the pin. A pair that
// normalizes to CodeUnknown is off the matrix, whatever its origin looks like.
func TestEveryRuntimeProducerPairNormalizesToAKnownCode(t *testing.T) {
	for pair, site := range runtimeProducerPairs() {
		t.Run(fmt.Sprintf("%v_%s", pair.origin, pair.code), func(t *testing.T) {
			got := NormalizeFailure(pair.origin, pair.code, RetryClassNever)
			if got.Code() == CodeUnknown {
				t.Fatalf("%s reports (%v, %q), which normalization replaces with %q",
					site, pair.origin, pair.code, CodeUnknown)
			}
			if got.Code() != pair.code {
				t.Fatalf("%s reports (%v, %q), normalized to %q", site, pair.origin, pair.code, got.Code())
			}
			if got.Origin() != pair.origin {
				t.Fatalf("%s reports origin %v, normalized to %v", site, pair.origin, got.Origin())
			}
		})
	}
}

// TestRuntimeProducerPairInventoryMatchesTheCallSites reads the runtime's own
// source. Every literal typed failure report it finds must be in the inventory
// above, and every inventory entry must have a call site: a stale row would let
// a producer be deleted without anyone noticing the vocabulary shrank.
func TestRuntimeProducerPairInventoryMatchesTheCallSites(t *testing.T) {
	scanned := scanRuntimeProducerPairs(t)
	approved := runtimeProducerPairs()

	for pair, position := range scanned {
		if _, ok := approved[pair]; !ok {
			t.Errorf("%s reports the uninventoried pair (%v, %q)", position, pair.origin, pair.code)
		}
		if got := NormalizeFailure(pair.origin, pair.code, RetryClassNever); got.Code() == CodeUnknown {
			t.Errorf("%s reports (%v, %q), which normalization replaces with %q",
				position, pair.origin, pair.code, CodeUnknown)
		}
	}
	for pair := range approved {
		if _, ok := scanned[pair]; !ok {
			t.Errorf("inventory lists (%v, %q) but no runtime call site reports it", pair.origin, pair.code)
		}
	}
}

// runtimeProducerDirectories are the packages that build a typed runtime
// failure report. They live in a sibling module directory, so the scan walks to
// them by path rather than importing them — orchestrator-go's internal packages
// are not importable from there, and this test must not become the reason they
// are.
func runtimeProducerDirectories() []string {
	return []string{
		filepath.Join("..", "..", "..", "agent-runtime-go", "internal", "agent"),
		filepath.Join("..", "..", "..", "agent-runtime-go", "internal", "worker"),
	}
}

// scanRuntimeProducerPairs collects every (origin, code) the runtime spells out
// literally, from either shape a producer uses: the agent's emitRunFailed
// helper, and a RuntimeRunFailed composite literal.
//
// A call site that passes a variable for either argument is deliberately not
// collected. There is exactly one — the agent forwarding a provider's own typed
// error event — and its vocabulary is the provider package's, pinned by that
// package's own tests; guessing at it here would only produce a table that
// looks complete and is not. Instead the scan counts those sites and the
// assertion below fixes how many may exist.
func scanRuntimeProducerPairs(t *testing.T) map[producerPair]string {
	t.Helper()
	pairs := map[producerPair]string{}
	forwarded := 0
	fileSet := token.NewFileSet()
	for _, directory := range runtimeProducerDirectories() {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatalf("read producer directory %s: %v", directory, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(directory, name)
			file, err := parser.ParseFile(fileSet, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				// emitRunFailed is the helper the call sites go through, so its
				// own body holds the one RuntimeRunFailed literal that is
				// supposed to take its code and origin from parameters.
				if !ok || function.Body == nil || function.Name.Name == "emitRunFailed" {
					continue
				}
				ast.Inspect(function.Body, func(node ast.Node) bool {
					code, origin, literal, ok := producerReport(node)
					if !ok {
						return true
					}
					if !literal {
						forwarded++
						return true
					}
					pairs[producerPair{origin: origin, code: code}] = fileSet.Position(node.Pos()).String()
					return true
				})
			}
		}
	}
	// One forwarding site: general_assistant.go's provider error arm.
	if forwarded != 1 {
		t.Fatalf("runtime has %d typed failure reports built from variables, want exactly the provider-forwarding one", forwarded)
	}
	return pairs
}

// producerReport reads one node as a typed failure report. literal is false
// when the node is a report whose code or origin is not spelled out at the call
// site.
func producerReport(node ast.Node) (code string, origin Origin, literal bool, ok bool) {
	switch value := node.(type) {
	case *ast.CallExpr:
		identifier, isIdentifier := value.Fun.(*ast.Ident)
		if !isIdentifier || identifier.Name != "emitRunFailed" || len(value.Args) < 4 {
			return "", 0, false, false
		}
		code, codeOK := stringLiteral(value.Args[2])
		origin, originOK := failureOriginSelector(value.Args[3])
		return code, origin, codeOK && originOK, true
	case *ast.CompositeLit:
		if selectorName(value.Type) != "turingv1.RuntimeRunFailed" {
			return "", 0, false, false
		}
		codeExpression, hasCode := compositeField(value, "Code")
		originExpression, hasOrigin := compositeField(value, "FailureOrigin")
		if !hasCode || !hasOrigin {
			return "", 0, false, false
		}
		code, codeOK := stringLiteral(codeExpression)
		origin, originOK := failureOriginSelector(originExpression)
		return code, origin, codeOK && originOK, true
	default:
		return "", 0, false, false
	}
}

func compositeField(composite *ast.CompositeLit, name string) (ast.Expr, bool) {
	for _, element := range composite.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := pair.Key.(*ast.Ident)
		if ok && key.Name == name {
			return pair.Value, true
		}
	}
	return nil, false
}

func stringLiteral(expression ast.Expr) (string, bool) {
	basic, ok := expression.(*ast.BasicLit)
	if !ok || basic.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(basic.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

// failureOriginSelector reads a generated FailureOrigin constant back onto this
// package's domain origin, by name. Reading the name rather than the numeric is
// what keeps the scan honest if the enum is ever renumbered.
func failureOriginSelector(expression ast.Expr) (Origin, bool) {
	name := selectorName(expression)
	const prefix = "turingv1.FailureOrigin_FAILURE_ORIGIN_"
	if !strings.HasPrefix(name, prefix) {
		return 0, false
	}
	origin, ok := originsByProtoSuffix()[strings.TrimPrefix(name, prefix)]
	return origin, ok
}

func originsByProtoSuffix() map[string]Origin {
	return map[string]Origin{
		"CONTEXT_ASSEMBLY":       OriginContextAssembly,
		"EXTERNAL_PROVIDER":      OriginExternalProvider,
		"PROVIDER_CONFIGURATION": OriginProviderConfiguration,
		"PROVIDER_PROTOCOL":      OriginProviderProtocol,
		"PROVIDER_TRANSPORT":     OriginProviderTransport,
		"PROVIDER_OUTPUT_GUARD":  OriginProviderOutputGuard,
		"TOOL_INFRASTRUCTURE":    OriginToolInfrastructure,
		"TOOL_EXECUTION":         OriginToolExecution,
		"TOOL_GUARD":             OriginToolGuard,
		"TOOL_POLICY":            OriginToolPolicy,
		"APPROVAL_TRANSPORT":     OriginApprovalTransport,
		"APPROVAL_EXPIRY":        OriginApprovalExpiry,
		"AUTOMATION_POLICY":      OriginAutomationPolicy,
		"WORKER_RUNTIME":         OriginWorkerRuntime,
		"DISPATCH":               OriginDispatch,
		"RECOVERY":               OriginRecovery,
		"ORCHESTRATOR_INTERNAL":  OriginOrchestratorInternal,
		"CLIENT_LIFECYCLE":       OriginClientLifecycle,
	}
}

func selectorName(expression ast.Expr) string {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return pkg.Name + "." + selector.Sel.Name
}

// approvedForwardedProviderPairs is every (origin, code) the llm package
// attaches to a provider error the agent then forwards verbatim. Like
// runtimeProducerPairs it is written by hand, so widening the provider
// vocabulary is a deliberate edit here as well as there.
func approvedForwardedProviderPairs() map[string]Origin {
	return map[string]Origin{
		"model_unavailable":    OriginProviderProtocol,
		"model_auth_failed":    OriginProviderProtocol,
		"model_request_failed": OriginProviderProtocol,
		"model_quota_exceeded": OriginProviderProtocol,
		"model_bad_chunk":      OriginProviderProtocol,
		"model_stream_error":   OriginProviderTransport,
		"model_timeout":        OriginProviderTransport,
		"model_error":          OriginExternalProvider,
	}
}

// The provider package chooses the origin for a forwarded provider error from
// its own code, so those pairs are producer pairs too even though the agent
// passes them through as variables.
//
// The inventory is checked against the llm package's own mapping table rather
// than trusted on its own: a hand-copy is exactly the thing that drifts, and
// the drift is silent — a provider code whose origin moved would still
// terminalize the run, just with its code replaced by CodeUnknown. The table
// lives in a sibling module directory, so it is read from source for the same
// reason the runtime producer scan is: orchestrator-go's internal packages are
// not importable from there, and this test must not become the reason they are.
func TestForwardedProviderErrorPairsNormalizeToKnownCodes(t *testing.T) {
	approved := approvedForwardedProviderPairs()
	scanned := scanProviderFailureOrigins(t)

	for code, origin := range scanned {
		if _, ok := approved[code]; !ok {
			t.Errorf("the llm package maps the uninventoried code %q to %v", code, origin)
			continue
		}
		if approved[code] != origin {
			t.Errorf("the llm package maps %q to %v, inventory says %v", code, origin, approved[code])
		}
	}
	for code := range approved {
		if _, ok := scanned[code]; !ok {
			t.Errorf("inventory lists %q but the llm package maps no such code", code)
		}
	}

	names := make([]string, 0, len(approved))
	for code := range approved {
		names = append(names, code)
	}
	sort.Strings(names)
	for _, code := range names {
		t.Run(code, func(t *testing.T) {
			got := NormalizeFailure(approved[code], code, RetryClassNever)
			if got.Code() != code {
				t.Fatalf("provider code %q normalized to %q", code, got.Code())
			}
			if got.Origin() != approved[code] {
				t.Fatalf("provider code %q normalized to origin %v, want %v", code, got.Origin(), approved[code])
			}
		})
	}
}

// providerOriginTableName is the llm package's single mapping from a provider
// error code to the origin that code always describes. Naming it here rather
// than scanning every switch in the package is deliberate: the point of the
// table existing is that there is one place to read.
const providerOriginTableName = "providerFailureOrigins"

// scanProviderFailureOrigins reads that table out of the llm package's source.
// An entry whose origin is not a spelled-out generated constant fails the scan
// rather than being skipped, because a computed origin is exactly the case a
// value-based pin cannot see.
func scanProviderFailureOrigins(t *testing.T) map[string]Origin {
	t.Helper()
	directory := filepath.Join("..", "..", "..", "agent-runtime-go", "internal", "llm")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read provider directory %s: %v", directory, err)
	}
	fileSet := token.NewFileSet()
	origins := map[string]Origin{}
	found := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(directory, name)
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.ValueSpec)
			if !ok || len(spec.Names) != 1 || spec.Names[0].Name != providerOriginTableName || len(spec.Values) != 1 {
				return true
			}
			composite, ok := spec.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s is not a composite literal", providerOriginTableName)
			}
			found = true
			for _, element := range composite.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					t.Fatalf("%s has a non key-value entry", providerOriginTableName)
				}
				code, ok := stringLiteral(pair.Key)
				if !ok {
					t.Fatalf("%s has an entry whose code is not a string literal", providerOriginTableName)
				}
				origin, ok := failureOriginSelector(pair.Value)
				if !ok {
					t.Fatalf("%s entry %q has an origin this scan cannot read", providerOriginTableName, code)
				}
				origins[code] = origin
			}
			return false
		})
	}
	if !found {
		t.Fatalf("the llm package no longer declares %s", providerOriginTableName)
	}
	return origins
}

// TestRuntimeToolFailureCategoryMatchesTheApprovedVocabulary pins the one word
// the agent writes on a tool.call.failed event.
//
// The orchestrator writes that event too, from an after-beacon, and takes its
// category from ToolCallFailureCategory. The agent cannot call that function —
// this package is not importable from agent-runtime-go — so it spells the
// category out as a constant, and a spelled-out copy is a second vocabulary
// waiting to happen. The constant is read back out of the source here and
// required to be exactly the category the orchestrator uses, so a client can
// never tell which of the two producers wrote a tool.call.failed.
func TestRuntimeToolFailureCategoryMatchesTheApprovedVocabulary(t *testing.T) {
	want, isFailure := ToolCallFailureCategory("tool.call.failed")
	if !isFailure {
		t.Fatal("tool.call.failed is no longer a categorized failure event")
	}
	got := scanRuntimeStringConstant(t,
		filepath.Join("..", "..", "..", "agent-runtime-go", "internal", "agent"), "toolFailureCategory")
	if got != string(want) {
		t.Fatalf("the runtime writes category %q on tool.call.failed, the orchestrator writes %q", got, want)
	}
}

// scanRuntimeStringConstant reads one spelled-out string constant out of a
// sibling module directory.
func scanRuntimeStringConstant(t *testing.T, directory string, name string) string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read directory %s: %v", directory, err)
	}
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		fileName := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(fileName, ".go") || strings.HasSuffix(fileName, "_test.go") {
			continue
		}
		path := filepath.Join(directory, fileName)
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		value := ""
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.ValueSpec)
			if !ok || len(spec.Names) != 1 || spec.Names[0].Name != name || len(spec.Values) != 1 {
				return true
			}
			literal, ok := stringLiteral(spec.Values[0])
			if !ok {
				t.Fatalf("%s in %s is not a spelled-out string literal", name, path)
			}
			value = literal
			return false
		})
		if value != "" {
			return value
		}
	}
	t.Fatalf("no directory file declares the constant %s", name)
	return ""
}
