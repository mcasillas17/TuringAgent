package runoutcome

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// generatedReasonPrefix is the enum's own name prefix. It is spelled here rather
// than derived, so a proto rename has to be restated in this test.
const generatedReasonPrefix = "RUN_OUTCOME_REASON_"

// The persisted string and the public enum are two spellings of one closed
// vocabulary: a row written "provider_failure" is projected as
// RUN_OUTCOME_REASON_PROVIDER_FAILURE, and the client localizes from the enum.
// Production keeps hand-written constants on purpose — deriving them from the
// generated name maps would make a proto edit silently rewrite persisted
// history — so the two sides are pinned to each other here instead, where a
// rename, an addition, or a one-character typo on either side fails.
//
// UNSPECIFIED is the only exclusion: it is protobuf's absence value and is never
// a persisted reason.
func TestReasonConstantsPinTheGeneratedRunOutcomeReasonEnum(t *testing.T) {
	// Every persisted Reason, restated as a literal pair. This is deliberately
	// not generated: adding a constant must force an edit here.
	declared := map[string]Reason{
		"ReasonUnknown":                ReasonUnknown,
		"ReasonNone":                   ReasonNone,
		"ReasonCompletedNoContent":     ReasonCompletedNoContent,
		"ReasonUserCancelled":          ReasonUserCancelled,
		"ReasonAbandoned":              ReasonAbandoned,
		"ReasonExpired":                ReasonExpired,
		"ReasonContextLimit":           ReasonContextLimit,
		"ReasonProviderFailure":        ReasonProviderFailure,
		"ReasonToolFailure":            ReasonToolFailure,
		"ReasonPolicyDenied":           ReasonPolicyDenied,
		"ReasonRetriesExhausted":       ReasonRetriesExhausted,
		"ReasonRecoveryInterrupted":    ReasonRecoveryInterrupted,
		"ReasonSideEffectUncertain":    ReasonSideEffectUncertain,
		"ReasonApprovalDeliveryFailed": ReasonApprovalDeliveryFailed,
		"ReasonInternalFailure":        ReasonInternalFailure,
		"ReasonLegacyUnknown":          ReasonLegacyUnknown,
	}

	expected := make(map[string]Reason, len(turingv1.RunOutcomeReason_name))
	for number, enumName := range turingv1.RunOutcomeReason_name {
		if number == int32(turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_UNSPECIFIED) {
			continue
		}
		if got, ok := turingv1.RunOutcomeReason_value[enumName]; !ok || got != number {
			t.Fatalf("generated enum name %q maps to %d in the name map and %d in the value map",
				enumName, number, got)
		}
		suffix := strings.TrimPrefix(enumName, generatedReasonPrefix)
		if suffix == enumName || suffix == "" {
			t.Fatalf("generated enum name %q does not carry the %q prefix", enumName, generatedReasonPrefix)
		}
		constantName := reasonConstantName(t, suffix)
		if previous, clash := expected[constantName]; clash {
			t.Fatalf("enum name %q collides with an earlier reason %q on constant %s",
				enumName, previous, constantName)
		}
		expected[constantName] = Reason(strings.ToLower(suffix))
	}

	if !reflect.DeepEqual(declared, expected) {
		t.Fatalf("Reason constants = %v,\nwant the generated enum vocabulary %v", declared, expected)
	}

	// The map above only covers constants a human remembered to list. The source
	// scan is what catches the one that was added and not listed.
	literals := declaredReasonConstantLiterals(t)
	wantLiterals := make(map[string]string, len(declared))
	for name, reason := range declared {
		wantLiterals[name] = string(reason)
	}
	if !reflect.DeepEqual(literals, wantLiterals) {
		t.Fatalf("Reason constants declared in source = %v,\nwant exactly the pinned set %v", literals, wantLiterals)
	}
}

// Production must not reach the enum's text: the typed switches map numerics to
// domain values, and a generated name map would put provider- and worker-chosen
// enum spelling back into a durable payload the moment a new numeric arrives.
// The test may use those maps; the package may not.
func TestProductionSourcesNeverUseGeneratedEnumNameHelpers(t *testing.T) {
	for _, file := range productionSourceFiles(t, ".") {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if strings.HasSuffix(selector.Sel.Name, "_name") || strings.HasSuffix(selector.Sel.Name, "_value") {
				t.Errorf("%s uses the generated helper %s; map numerics with a typed switch instead",
					file, selector.Sel.Name)
			}
			return true
		})
	}
}

// reasonConstantName turns an enum suffix such as COMPLETED_NO_CONTENT into the
// Go constant name ReasonCompletedNoContent, so an identifier typo fails as
// loudly as a value typo.
func reasonConstantName(t *testing.T, enumSuffix string) string {
	t.Helper()
	name := "Reason"
	for _, part := range strings.Split(enumSuffix, "_") {
		if part == "" {
			t.Fatalf("enum suffix %q has an empty word", enumSuffix)
		}
		name += strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return name
}

// declaredReasonConstantLiterals reads every Reason-typed constant this package
// declares, from source rather than from a list a test author maintains.
func declaredReasonConstantLiterals(t *testing.T) map[string]string {
	t.Helper()
	literals := map[string]string{}
	for _, file := range productionSourceFiles(t, ".") {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, declaration := range parsed.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			for _, specification := range group.Specs {
				value, ok := specification.(*ast.ValueSpec)
				if !ok {
					continue
				}
				collectReasonConstants(t, file, value, literals)
			}
		}
	}
	return literals
}

func collectReasonConstants(t *testing.T, file string, value *ast.ValueSpec, literals map[string]string) {
	t.Helper()
	typeName := ""
	if identifier, ok := value.Type.(*ast.Ident); ok {
		typeName = identifier.Name
	}
	for index, name := range value.Names {
		if typeName != "Reason" {
			// An untyped string constant is assignable to Reason, so it would be
			// a persisted reason this pin never saw.
			if strings.HasPrefix(name.Name, "Reason") {
				t.Errorf("%s declares %s without the Reason type, which escapes the enum pin", file, name.Name)
			}
			continue
		}
		if index >= len(value.Values) {
			t.Fatalf("%s declares %s with no value", file, name.Name)
		}
		literal, ok := value.Values[index].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			t.Fatalf("%s declares %s from something other than a string literal", file, name.Name)
		}
		unquoted, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatalf("%s declares %s with an unreadable literal %s", file, name.Name, literal.Value)
		}
		literals[name.Name] = unquoted
	}
}

func productionSourceFiles(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, filepath.Join(directory, name))
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatalf("no production sources found in %s", directory)
	}
	return files
}
