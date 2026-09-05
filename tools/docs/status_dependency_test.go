package docs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type runtimeModule struct {
	directory   string
	entries     []string
	tags        string
	environment []string
}

var runtimeModules = []runtimeModule{
	{".", []string{"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/cmd/server"}, "sqlite_fts5", []string{"GOOS=linux", "CGO_ENABLED=1"}},
	{".", []string{"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/cmd/runtime"}, "sqlite_fts5", []string{"GOOS=linux", "CGO_ENABLED=0"}},
	{"turing-backend/mcp-files", []string{"github.com/project-turing/mcp-files/cmd/server"}, "", []string{"GOOS=linux", "CGO_ENABLED=0"}},
	{"turing-backend/mcp-system", []string{"github.com/project-turing/mcp-system/cmd/server"}, "", []string{"GOOS=linux", "CGO_ENABLED=0"}},
}

var documentationConsumerModule = runtimeModule{".", []string{"./tools/docs"}, "sqlite_fts5", nil}

var offlineGraphEnvironment = []string{
	"GOTOOLCHAIN=auto", "GOWORK=off", "GO111MODULE=on", "GOFLAGS=-mod=readonly",
	"GOPROXY=off", "GONOPROXY=none", "GOSUMDB=off", "GOVCS=*:off",
}

func TestGoldmarkStaysOutsideRuntimeBuilds(t *testing.T) {
	var combined strings.Builder
	for _, module := range runtimeModules {
		combined.WriteString(offlineDependencyGraph(t, module, false))
		combined.WriteByte('\n')
	}
	if problem := runtimeDependencyProblem(combined.String()); problem != "" {
		t.Fatal(problem)
	}
	if !hasGoldmarkDependency(offlineDependencyGraph(t, documentationConsumerModule, true)) {
		t.Fatal("dependency graph positive control missed the documentation tests' Goldmark dependency")
	}
}

func dependencyArguments(module runtimeModule, modMode string, includeTests bool) []string {
	arguments := []string{"list", "-mod=" + modMode, "-deps"}
	if module.tags != "" {
		arguments = append(arguments, "-tags", module.tags)
	}
	if includeTests {
		arguments = append(arguments, "-test")
	}
	return append(arguments, module.entries...)
}

func offlineDependencyGraph(t *testing.T, module runtimeModule, includeTests bool) string {
	t.Helper()
	directory := module.directory
	if !filepath.IsAbs(directory) {
		directory = filepath.Join("../..", directory)
	}
	modMode := "readonly"
	if _, err := os.Stat(filepath.Join(directory, "vendor/modules.txt")); err == nil {
		modMode = "vendor"
	} else if !os.IsNotExist(err) {
		t.Fatalf("%s: cannot inspect vendor mode", module.directory)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	arguments := dependencyArguments(module, modMode, includeTests)
	command := exec.CommandContext(ctx, "go", arguments...)
	command.WaitDelay = time.Second
	command.Dir = directory
	command.Env = append(append(os.Environ(), offlineGraphEnvironment...), module.environment...)
	output, err := command.Output()
	if err == nil {
		return string(output)
	}
	t.Fatal(dependencyFailureMessage(err, ctx.Err() != nil, module, arguments))
	return ""
}

func dependencyFailureMessage(err error, timedOut bool, module runtimeModule, arguments []string) string {
	settings := append(append([]string{}, offlineGraphEnvironment...), module.environment...)
	reproduce := "inside " + module.directory + ", set [" + strings.Join(settings, ", ") + "], then run: go " + strings.Join(arguments, " ")
	if errors.Is(err, exec.ErrNotFound) {
		return "Go tool unavailable on PATH; " + reproduce
	}
	if timedOut {
		return "offline Go dependency graph timed out; " + reproduce
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return "offline go list exited " + strconv.Itoa(exited.ExitCode()) +
			"; first run go mod download inside " + module.directory +
			" with normal network access, or resolve package errors; then " + reproduce
	}
	return "offline go list could not execute; " + reproduce
}

func hasGoldmarkDependency(output string) bool {
	for _, imported := range strings.Fields(output) {
		if imported == "github.com/yuin/goldmark" || strings.HasPrefix(imported, "github.com/yuin/goldmark/") {
			return true
		}
	}
	return false
}

func runtimeDependencyProblem(output string) string {
	if hasGoldmarkDependency(output) {
		return "Goldmark entered an application runtime build graph; remove the dependency or reconcile the documented test-only boundary"
	}
	found := make(map[string]bool)
	for _, imported := range strings.Fields(output) {
		found[imported] = true
	}
	for _, module := range runtimeModules {
		for _, entry := range module.entries {
			if !found[entry] {
				return "runtime dependency graph is missing expected entry point " + entry
			}
		}
	}
	return ""
}

func TestDependencyArgumentsMatchDeclaredProfiles(t *testing.T) {
	for _, module := range runtimeModules {
		t.Run(module.entries[0], func(t *testing.T) {
			want := []string{"list", "-mod=readonly", "-deps"}
			if module.directory == "." {
				want = append(want, "-tags", "sqlite_fts5")
			}
			want = append(want, module.entries...)
			if got := dependencyArguments(module, "readonly", false); !reflect.DeepEqual(got, want) {
				t.Fatalf("runtime graph arguments = %v, want %v", got, want)
			}
		})
	}
	want := []string{"list", "-mod=vendor", "-deps", "-tags", "sqlite_fts5", "-test", "./tools/docs"}
	if got := dependencyArguments(documentationConsumerModule, "vendor", true); !reflect.DeepEqual(got, want) {
		t.Fatalf("positive-control arguments = %v, want %v", got, want)
	}
}

func TestDependencyGraphObservesNoCGOBuildConstraint(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":           "module fixture\n\ngo 1.23\n\nrequire github.com/yuin/goldmark v0.0.0\nreplace github.com/yuin/goldmark => ./goldmark\n",
		"main.go":          "package main\nfunc main() {}\n",
		"nocgo.go":         "//go:build !cgo\n\npackage main\nimport _ \"github.com/yuin/goldmark\"\n",
		"goldmark/go.mod":  "module github.com/yuin/goldmark\n\ngo 1.23\n",
		"goldmark/mark.go": "package goldmark\n",
	} {
		name = filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, cgo := range []string{"0", "1"} {
		t.Run("CGO_ENABLED="+cgo, func(t *testing.T) {
			module := runtimeModule{root, []string{"fixture"}, "", []string{"GOOS=linux", "CGO_ENABLED=" + cgo}}
			if got := hasGoldmarkDependency(offlineDependencyGraph(t, module, false)); got != (cgo == "0") {
				t.Fatalf("build-constrained dependency presence = %v for CGO_ENABLED=%s", got, cgo)
			}
		})
	}
}

func TestDependencyFailureClassifications(t *testing.T) {
	exit := exec.Command("go", "version", "-doc-guard-invalid-flag").Run()
	var exited *exec.ExitError
	if !errors.As(exit, &exited) {
		t.Fatal("expected the Go tool to reject an invalid flag")
	}
	module := runtimeModules[0]
	arguments := dependencyArguments(module, "readonly", false)
	for _, fixture := range []struct {
		name     string
		err      error
		timedOut bool
		want     string
	}{
		{"missing Go", exec.ErrNotFound, false, "unavailable on PATH"},
		{"timeout", context.DeadlineExceeded, true, "timed out"},
		{"exit status", exit, false, "exited " + strconv.Itoa(exited.ExitCode())},
		{"start failure", errors.New("private-error-value"), false, "could not execute"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			message := dependencyFailureMessage(fixture.err, fixture.timedOut, module, arguments)
			for _, want := range []string{fixture.want, "inside .", "GOPROXY=off", "GOOS=linux", "CGO_ENABLED=1", "then run: go list"} {
				if !strings.Contains(message, want) {
					t.Errorf("diagnostic %q missing %q", message, want)
				}
			}
			if strings.Contains(message, "private-error-value") {
				t.Fatal("diagnostic leaked error content")
			}
		})
	}
}

func TestRuntimeDependencyFixtures(t *testing.T) {
	var entries []string
	for _, module := range runtimeModules {
		entries = append(entries, module.entries...)
	}
	baseline := strings.Join(entries, "\n") + "\nstrings\n"
	for _, fixture := range []struct{ name, output, want string }{
		{"baseline", baseline, ""},
		{"direct or transitive dependency", baseline + "github.com/yuin/goldmark\n", "Goldmark entered"},
		{"subpackage dependency", baseline + "github.com/yuin/goldmark/ast\n", "Goldmark entered"},
		{"unrelated prefix", baseline + "github.com/yuin/goldmarker\n", ""},
		{"empty result", "", "missing expected entry point"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			problem := runtimeDependencyProblem(fixture.output)
			if (fixture.want == "" && problem != "") || !strings.Contains(problem, fixture.want) {
				t.Fatalf("got %q, want %q", problem, fixture.want)
			}
		})
	}
	for _, entry := range entries {
		t.Run("missing "+entry, func(t *testing.T) {
			problem := runtimeDependencyProblem(strings.Replace(baseline, entry+"\n", "", 1))
			if !strings.Contains(problem, "missing expected entry point "+entry) {
				t.Fatalf("missing runtime not identified: %s", problem)
			}
		})
	}
}
