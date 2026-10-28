package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// TestTheTuringCommandImportsNoSQLitePackage pins the design's promise that the
// command never opens the database (host-command design, sections 6.1 and 7):
// the orchestrator owns state, and a host command that held SQLite open beside
// it could fence or corrupt the runs in flight.
//
// It asks the go tool for the whole transitive graph rather than parsing this
// package's imports, so a SQLite driver pulled in by a shared package is caught
// too. It runs with and without the build tag the rest of the repo needs: the
// command is documented as needing no tags, so a driver that appears only under
// sqlite_fts5, or only without it, must still be seen. database/sql is refused
// as well, since a driver is only reachable through it.
func TestTheTuringCommandImportsNoSQLitePackage(t *testing.T) {
	for name, args := range map[string][]string{
		"without build tags": {"list", "-deps", "."},
		"with sqlite_fts5":   {"list", "-deps", "-tags", "sqlite_fts5", "."},
	} {
		t.Run(name, func(t *testing.T) {
			output, err := goTool(t.Context(), args...)
			if err != nil {
				t.Fatal(err)
			}
			packages := strings.Fields(output)
			if len(packages) == 0 {
				t.Fatalf("go %s listed no packages; this guard reads its answer from there", strings.Join(args, " "))
			}
			for _, pkg := range packages {
				if strings.Contains(strings.ToLower(pkg), "sqlite") || pkg == "database/sql" {
					t.Errorf("the turing command depends on %q; it must never open the database, so it may not import a SQLite package", pkg)
				}
			}
		})
	}
}

func TestGoToolReportsWhatGoPrintedWhenItFails(t *testing.T) {
	_, err := goTool(t.Context(), "list", "-deps", "./no-such-package")
	if err == nil {
		t.Fatal("go list of a missing package succeeded")
	}
	if !strings.Contains(err.Error(), "directory not found") {
		t.Fatalf("error = %q, want what go printed on stderr", err)
	}
}

// goTool runs the go tool and returns its standard output. When it fails, the
// error carries what it printed on standard error, which says why.
func goTool(ctx context.Context, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, "go", args...).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return "", fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(exit.Stderr)))
	}
	if err != nil {
		return "", fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return string(output), nil
}
