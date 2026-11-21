package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// pendingStatement is one SQL statement that sets a job back to pending,
// and the function whose source holds it ("" at package level).
type pendingStatement struct {
	file     string
	function string
}

var (
	whitespaceRun   = regexp.MustCompile(`\s+`)
	jobsUpdate      = regexp.MustCompile(`(?i)\bUPDATE jobs SET (.*?) WHERE\b`)
	pendingAssigned = regexp.MustCompile(`(?i)\bstatus\s*=\s*'pending'`)
)

// pendingStatementsIn finds, in each string literal of one parsed file with
// its whitespace runs collapsed to one space, every UPDATE jobs SET … WHERE
// whose SET clause assigns status = 'pending'.
func pendingStatementsIn(t *testing.T, name string, file *ast.File) []pendingStatement {
	t.Helper()
	var found []pendingStatement
	scan := func(function string, node ast.Node) {
		ast.Inspect(node, func(n ast.Node) bool {
			literal, ok := n.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatalf("%s: unquote %s: %v", name, literal.Value, err)
			}
			for _, match := range jobsUpdate.FindAllStringSubmatch(whitespaceRun.ReplaceAllString(text, " "), -1) {
				if pendingAssigned.MatchString(match[1]) {
					found = append(found, pendingStatement{file: name, function: function})
				}
			}
			return true
		})
	}
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			scan(function.Name.Name, function)
			continue
		}
		scan("", declaration)
	}
	return found
}

func parseRepositorySources(t *testing.T) map[string]*ast.File {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*ast.File{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = file
	}
	if len(files) == 0 {
		t.Fatal("no repository sources were parsed")
	}
	return files
}

// TUR-028a: releaseJobToPendingTx is the one way back to pending, because it
// holds the quiet check every requeue must pass. Exactly one statement in the
// package's sources sets a job pending, and it is inside that function: the
// guard fails if a second appears, and also if it finds none, so it cannot
// pass by matching nothing.
func TestOnlyReleaseJobToPendingTxSetsAJobBackToPending(t *testing.T) {
	var found []pendingStatement
	for name, file := range parseRepositorySources(t) {
		found = append(found, pendingStatementsIn(t, name, file)...)
	}
	if len(found) != 1 || found[0].function != "releaseJobToPendingTx" {
		t.Fatalf("statements setting a job pending = %+v, want exactly one, inside releaseJobToPendingTx", found)
	}
}

// The guard reads the statements as the code writes them: across lines,
// whatever the case, in the SET clause only. Run on the two multi-line
// statements the package held before TUR-028a, it finds both, so it fails
// on that code.
func TestThePendingStatementGuardFindsMultiLineStatements(t *testing.T) {
	const source = "package repository\n" +
		"func releaseJobToPendingTx() string { return `\n\t\tUPDATE jobs\n\t\tSET status = 'pending',\n\t\t\tlease_owner = NULL,\n\t\t\tattempt = attempt + ?\n\t\tWHERE run_id = ?\n\t\t\tAND status = 'in_progress'` }\n" +
		"func requeueAssignment() string { return `\n\t\tUPDATE jobs\n\t\tSET status = 'pending',\n\t\t\tassignment_attempt_id = NULL\n\t\tWHERE id = ? AND run_id = ? AND status = 'in_progress'` }\n" +
		"func shouting() string { return \"update JOBS set Status='PENDING' where id = ?\" }\n" +
		"func claim() string { return `UPDATE jobs SET status = 'in_progress' WHERE id = ? AND status = 'pending'` }\n" +
		"var cancelled = `UPDATE jobs SET status = 'cancelled' WHERE run_id = ? AND status IN ('pending','in_progress')`\n"
	file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := pendingStatementsIn(t, "fixture.go", file)
	functions := make([]string, 0, len(found))
	for _, statement := range found {
		functions = append(functions, statement.function)
	}
	if strings.Join(functions, ",") != "releaseJobToPendingTx,requeueAssignment,shouting" {
		t.Fatalf("statements found in = %v, want the two multi-line statements and the shouted one, and neither statement whose only pending is in its WHERE", functions)
	}
}

// What only the deleted RequeueClaimedJob reached is gone with it:
// requeueAssignment always names a delivery state and never increments the
// attempt, so it takes exactly three parameters, and it never takes the
// uncertain recovery edge.
func TestRequeueClaimedJobIsGoneAndRequeueAssignmentNeverIncrementsTheAttempt(t *testing.T) {
	var requeueAssignment *ast.FuncDecl
	for name, file := range parseRepositorySources(t) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			switch function.Name.Name {
			case "RequeueClaimedJob":
				t.Fatalf("%s still declares RequeueClaimedJob", name)
			case "requeueAssignment":
				requeueAssignment = function
			}
		}
	}
	if requeueAssignment == nil {
		t.Fatal("requeueAssignment is missing")
	}
	var parameters []string
	for _, field := range requeueAssignment.Type.Params.List {
		for _, name := range field.Names {
			parameters = append(parameters, name.Name)
		}
	}
	if strings.Join(parameters, ",") != "ctx,assignment,requiredExecutionState" {
		t.Fatalf("requeueAssignment parameters = %v, want ctx, assignment, requiredExecutionState", parameters)
	}
	// Its proof is that no worker received the assignment, so it takes only
	// the unsent edge; the uncertain recovery edge was RequeueClaimedJob's.
	ast.Inspect(requeueAssignment.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callee, ok := call.Fun.(*ast.Ident); ok && callee.Name == "requeueRunLifecycleTx" {
			t.Fatal("requeueAssignment still reaches requeueRunLifecycleTx, the uncertain recovery edge")
		}
		return true
	})
}
