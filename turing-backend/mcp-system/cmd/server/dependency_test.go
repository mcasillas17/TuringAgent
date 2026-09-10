package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheShippedBinaryImportsOnlyTheStandardLibrary pins the property that lets
// this module take a pinned MCP SDK dependency for conformance evidence without
// changing what actually ships: the SDK is imported by test files only, so the
// server binary's dependency graph is still the standard library alone.
//
// This walks the module's own sources rather than shelling out to `go list`, so
// it stays offline and needs no module cache beyond what the build already has.
func TestTheShippedBinaryImportsOnlyTheStandardLibrary(t *testing.T) {
	moduleRoot := filepath.Join("..", "..")
	const modulePath = "github.com/project-turing/mcp-system/"

	fileSet := token.NewFileSet()
	err := filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			imported := strings.Trim(spec.Path.Value, `"`)
			if strings.HasPrefix(imported, modulePath) {
				continue
			}
			// A standard-library path has no dot in its first segment.
			if first, _, _ := strings.Cut(imported, "/"); strings.Contains(first, ".") {
				t.Errorf("%s imports %q; the shipped mcp-system binary must stay standard-library only", path, imported)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module sources: %v", err)
	}
}
