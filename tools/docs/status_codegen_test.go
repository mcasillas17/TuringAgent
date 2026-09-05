package docs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type codegenChange struct {
	operation string
	target    string
}

func TestProtoCheckRejectsGeneratedDrift(t *testing.T) {
	script := repoFile(t, "tools/proto/check.sh")
	changes := []codegenChange{{}, {"fail", ""}}
	for _, tree := range []string{"proto", "gen", "turing-client/turing_app/lib/generated"} {
		changes = append(changes,
			codegenChange{"write", tree + "/output.txt"},
			codegenChange{"write", tree + "/added.txt"},
			codegenChange{"write", tree + "/nested/added.txt"},
			codegenChange{"remove", tree + "/output.txt"},
		)
	}
	for _, changed := range changes {
		t.Run(changed.operation+"="+changed.target, func(t *testing.T) {
			if problem := protoCheckBehaviorProblem(t, script, changed); problem != "" {
				t.Error(problem)
			}
		})
	}
}

func TestProtoCheckSuppressionFixtures(t *testing.T) {
	baseline := repoFile(t, "tools/proto/check.sh")
	const generation = `"$ROOT/tools/proto/generate.sh"`
	const gate = `if [[ "$changed" -ne 0 ]]; then`
	for _, fixture := range []struct{ name, old, replacement, changed string }{
		{"reset failure flag", gate, "changed=0\n" + gate, "gen/output.txt"},
		{"early success", gate, "exit 0\n" + gate, "gen/output.txt"},
		{"skip loop body", "lib/generated; do", "lib/generated; do\n  continue", "gen/output.txt"},
		{"suppress diff status", ">/dev/null; then", ">/dev/null && false; then", "gen/output.txt"},
		{"unreachable change flag", "changed=1", "if false; then\n      changed=1\n    fi", "gen/output.txt"},
		{"restore output after generation", generation, generation + "\n" + `cp -R "$SNAPSHOT/gen/." "$ROOT/gen"`, "gen/output.txt"},
		{"ignore Dart output", "proto gen turing-client/turing_app/lib/generated; do", "proto gen; do", "turing-client/turing_app/lib/generated/output.txt"},
		{"compare only existing files", "proto gen turing-client/turing_app/lib/generated; do", "proto/output.txt gen/output.txt turing-client/turing_app/lib/generated/output.txt; do", "gen/nested/added.txt"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if !strings.Contains(baseline, fixture.old) {
				t.Fatal("codegen fixture anchor moved; reconcile the behavioral probe")
			}
			mutated := strings.Replace(baseline, fixture.old, fixture.replacement, 1)
			if problem := protoCheckBehaviorProblem(t, mutated, codegenChange{"write", fixture.changed}); !strings.Contains(problem, "did not reject generated drift") {
				t.Fatalf("suppression must be detected for the intended reason: %s", problem)
			}
		})
	}
	t.Run("whole gate unreachable", func(t *testing.T) {
		problem := protoCheckBehaviorProblem(t, "if false; then\n"+baseline+"\nfi\n", codegenChange{"write", "gen/output.txt"})
		if !strings.Contains(problem, "did not reject generated drift") {
			t.Fatalf("unreachable gate must be detected: %s", problem)
		}
	})
	t.Run("generation after comparison", func(t *testing.T) {
		mutated := strings.Replace(baseline, generation, "", 1)
		mutated = strings.Replace(mutated, gate, generation+"\n"+gate, 1)
		if problem := protoCheckBehaviorProblem(t, mutated, codegenChange{"write", "gen/output.txt"}); !strings.Contains(problem, "did not reject generated drift") {
			t.Fatalf("late generation must be detected: %s", problem)
		}
	})
	t.Run("generator failure ignored without errexit", func(t *testing.T) {
		mutated := strings.Replace(baseline, "set -euo pipefail", "set -uo pipefail", 1)
		problem := protoCheckBehaviorProblem(t, mutated, codegenChange{"fail", ""})
		if !strings.Contains(problem, "failed generator was not propagated") {
			t.Fatalf("ignored generator failure must be detected: %s", problem)
		}
	})
}

// Exercise the real check script with a deterministic fake generator, not
// protoc. Every input, output and mktemp snapshot stays in this test's fixture.
func protoCheckBehaviorProblem(t *testing.T, script string, changed codegenChange) string {
	t.Helper()
	root := t.TempDir()
	generator := `#!/usr/bin/env bash
set -eu
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
case "$DOC_GUARD_OPERATION" in
  "") ;;
  fail) exit 42 ;;
  write)
    mkdir -p "$(dirname "$ROOT/$DOC_GUARD_CHANGE")"
    printf 'changed\n' > "$ROOT/$DOC_GUARD_CHANGE"
    ;;
  remove) rm -- "$ROOT/$DOC_GUARD_CHANGE" ;;
  *) exit 2 ;;
esac
`
	for _, file := range []struct{ path, content string }{
		{"proto/output.txt", "before\n"},
		{"gen/output.txt", "before\n"},
		{"turing-client/turing_app/lib/generated/output.txt", "before\n"},
		{"tools/proto/check.sh", script},
		{"tools/proto/generate.sh", generator},
	} {
		path := filepath.Join(root, file.path)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(file.content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	tmp := filepath.Join(root, "tmp")
	if err := os.Mkdir(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "bash", filepath.Join(root, "tools/proto/check.sh"))
	command.WaitDelay = time.Second
	command.Dir = root
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"), "TMPDIR=" + tmp, "LC_ALL=C",
		"DOC_GUARD_CHANGE=" + changed.target, "DOC_GUARD_OPERATION=" + changed.operation,
	}
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return "tools/proto/check.sh: probe timed out; reconcile the generation/diff gate"
	}
	if changed.operation == "fail" {
		var exited *exec.ExitError
		if errors.As(err, &exited) && exited.ExitCode() == 42 {
			return ""
		}
		return "tools/proto/check.sh: failed generator was not propagated"
	}
	if changed.target == "" {
		if err == nil {
			return ""
		}
		return "tools/proto/check.sh: unchanged fixture failed; reconcile the generation/diff gate and Bash prerequisites"
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) && exited.ExitCode() == 1 &&
		bytes.Contains(output, []byte("generated proto output is not deterministic or not committed")) {
		return ""
	}
	if err == nil {
		return "tools/proto/check.sh: did not reject generated drift in " + changed.target + "; restore the generation/diff failure gate"
	}
	return "tools/proto/check.sh: probe failed outside the drift gate; check Bash/file utility prerequisites"
}
