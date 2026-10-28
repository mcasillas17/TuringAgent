package main

import (
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
			output, err := exec.CommandContext(t.Context(), "go", args...).Output()
			if err != nil {
				t.Fatalf("go %s: %v", strings.Join(args, " "), err)
			}
			packages := strings.Fields(string(output))
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
