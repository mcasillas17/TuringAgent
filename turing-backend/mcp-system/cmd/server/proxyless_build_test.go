package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTheShippedBinaryBuildsWithNoModuleProxy pins the invariant this module's
// Dockerfile depends on: it has no `go mod download` layer and builds with
// GOPROXY=off, so `./cmd/server` must resolve with nothing in the module cache.
//
// That used to be trivially true — the module had no requirements at all. It
// stopped being trivial when the MCP conformance fixtures added a pinned
// test-only SDK, and it now rests on module-graph pruning: the shipped binary
// imports only the standard library, so the loader never needs the SDK's go.mod
// to build it. TestTheShippedBinaryImportsOnlyTheStandardLibrary guards the
// import graph by parsing sources; only actually running the loader with an
// empty cache proves the build itself needs no proxy.
//
// Only GOMODCACHE is isolated. The build cache is deliberately shared, because
// the module proxy — not compilation — is what this asserts, and rebuilding the
// standard library from cold would cost far more than the invariant is worth.
//
// The child runs the toolchain already running this test, resolved through
// GOROOT. Go keeps downloaded toolchains in the module cache, so the isolation
// hides those too: comparing this module's directive against the PATH `go`
// instead would make the guard skip whenever that binary is older —including on
// CI, which pins go-version 1.25.x while the directive moves independently with
// Dependabot, so the next bump would turn the assertion inert on the one machine
// meant to run it. The running toolchain necessarily satisfies the directive, or
// this test binary would not exist, so there is no version to compare.
//
// Nothing here skips. A machine that cannot produce a `go` command cannot verify
// the invariant, and says so loudly rather than passing quietly.
func TestTheShippedBinaryBuildsWithNoModuleProxy(t *testing.T) {
	pathGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("no go tool on PATH, so this guard cannot run the toolchain it needs: %v", err)
	}
	located, err := exec.Command(pathGo, "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("reading GOROOT: %v", err)
	}
	goRoot := strings.TrimSpace(string(located))
	if goRoot == "" {
		t.Fatal("go env GOROOT is empty; this guard needs the running toolchain's own go binary")
	}
	goTool := filepath.Join(goRoot, "bin", "go")
	if _, err := os.Stat(goTool); err != nil {
		t.Fatalf("no go binary at %s: %v", goTool, err)
	}

	isolated := t.TempDir()
	command := exec.Command(goTool, "build", "-o", filepath.Join(isolated, "server"), "./cmd/server")
	command.Dir = filepath.Join("..", "..")
	command.Env = append(os.Environ(),
		"GOPROXY=off",
		"GOFLAGS=",
		"GOTOOLCHAIN=local",
		"GOMODCACHE="+filepath.Join(isolated, "mod"),
	)

	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf(
			"building ./cmd/server with an empty module cache and GOPROXY=off failed, so the Dockerfile's "+
				"proxyless build is broken for anyone with a cold cache: %v\n%s",
			err, output,
		)
	}
}

// TestTheModuleDeclaresNoToolchainDirective closes the one gap GOTOOLCHAIN=local
// opens in the guard above.
//
// The image builds under the default GOTOOLCHAIN=auto, which honours a
// `toolchain` directive; the guard pins `local`, which ignores one. The go
// command writes that directive into go.mod by itself when a contributor with a
// newer Go runs `go get` or `go mod tidy`, and under GOPROXY=off the image build
// would then try to download that toolchain and fail — while the guard stayed
// green. So its absence is asserted directly.
func TestTheModuleDeclaresNoToolchainDirective(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if match := regexp.MustCompile(`(?m)^toolchain\s+(\S+)$`).FindStringSubmatch(string(source)); match != nil {
		t.Fatalf(
			"mcp-system's go.mod declares `toolchain %s`; the image builds under GOTOOLCHAIN=auto with "+
				"GOPROXY=off, so it would try to download that toolchain and fail",
			match[1],
		)
	}
}
