package config

import (
	"strings"
	"testing"
)

// The orchestrator opens the vault at MEMORY_ROOT, and inside Docker that is
// `/memory` — a path that exists only inside the container. The desktop client
// showed it to the user as "your vault is here", which is a path they cannot
// open, cannot find, and cannot even usefully search for.
//
// So there are two settings now, and they answer different questions.
// MEMORY_ROOT is where the bytes are, and every open, every confinement check
// and every refusal is about it. MEMORY_DISPLAY_ROOT is a string the client may
// show a person, and nothing else ever reads it. On a native run they are the
// same path and the display one does not need setting; under Compose they
// differ, and the host path is the only one worth showing.

// TestMemoryDisplayRootDefaultsToTheVaultItDescribes keeps the native case
// working without configuration: one path, one meaning, nothing to get wrong.
func TestMemoryDisplayRootDefaultsToTheVaultItDescribes(t *testing.T) {
	cfg, err := LoadFromMap(requiredEnv())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MemoryDisplayRoot != cfg.MemoryRoot {
		t.Fatalf("MemoryDisplayRoot = %q, want it to default to MemoryRoot %q",
			cfg.MemoryDisplayRoot, cfg.MemoryRoot)
	}

	env := requiredEnv()
	env["MEMORY_ROOT"] = "/srv/test-memory"
	cfg, err = LoadFromMap(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MemoryDisplayRoot != "/srv/test-memory" {
		t.Fatalf("MemoryDisplayRoot = %q, want it to follow MEMORY_ROOT", cfg.MemoryDisplayRoot)
	}
}

// The Docker case. The two paths are independent: the container opens /memory
// and the person is told about the folder on their own disk.
func TestMemoryDisplayRootIsIndependentOfTheVaultItDescribes(t *testing.T) {
	env := requiredEnv()
	env["MEMORY_ROOT"] = "/memory"
	env["MEMORY_DISPLAY_ROOT"] = "/Users/someone/turing/turing-backend/memory"
	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MemoryRoot != "/memory" {
		t.Fatalf("MemoryRoot = %q, want the container path the vault is opened at", cfg.MemoryRoot)
	}
	if cfg.MemoryDisplayRoot != "/Users/someone/turing/turing-backend/memory" {
		t.Fatalf("MemoryDisplayRoot = %q, want the host path", cfg.MemoryDisplayRoot)
	}

	// Paths with spaces are ordinary on both desktop platforms this ships on,
	// and a setting that could not carry one would silently be wrong for every
	// user whose checkout lives under "My Documents".
	env = requiredEnv()
	env["MEMORY_DISPLAY_ROOT"] = "/Users/some one/My Vaults/turing memory"
	cfg, err = LoadFromMap(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MemoryDisplayRoot != "/Users/some one/My Vaults/turing memory" {
		t.Fatalf("MemoryDisplayRoot = %q, want the spaces kept", cfg.MemoryDisplayRoot)
	}
}

// A display path is a string handed to a person, but it is still a path, and it
// is refused on exactly the terms the real one is. A value nobody validated is
// how a relative fragment or a traversal ends up rendered as "your memory is
// here" — and how somebody starts treating it as a path to act on.
func TestMemoryDisplayRootIsRefusedUnlessItIsACleanAbsolutePath(t *testing.T) {
	for _, bad := range []string{
		"relative/memory",
		"memory",
		"../../etc",
		"/srv/../memory",
		"/srv/memory/",
		"/srv//memory",
		"/srv/./memory",
	} {
		env := requiredEnv()
		env["MEMORY_DISPLAY_ROOT"] = bad
		if _, err := LoadFromMap(env); err == nil {
			t.Fatalf("LoadFromMap accepted MEMORY_DISPLAY_ROOT = %q", bad)
		} else if !strings.Contains(err.Error(), "MEMORY_DISPLAY_ROOT") {
			t.Fatalf("error = %v, want it to name MEMORY_DISPLAY_ROOT", err)
		}
	}

	// Unset is not "bad": it is the native case, and the default stands.
	env := requiredEnv()
	env["MEMORY_DISPLAY_ROOT"] = ""
	cfg, err := LoadFromMap(env)
	if err != nil || cfg.MemoryDisplayRoot != cfg.MemoryRoot {
		t.Fatalf("empty MEMORY_DISPLAY_ROOT = %q err=%v, want the MemoryRoot default", cfg.MemoryDisplayRoot, err)
	}
}

// The display path is never the one anything opens. MEMORY_ROOT is validated
// and used on its own terms whatever the display setting says, so a display
// value can never widen, redirect or otherwise reach the confinement domain.
func TestMemoryDisplayRootNeverChangesWhereTheVaultIsOpened(t *testing.T) {
	env := requiredEnv()
	env["MEMORY_ROOT"] = "/memory"
	env["MEMORY_DISPLAY_ROOT"] = "/etc"
	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MemoryRoot != "/memory" {
		t.Fatalf("MemoryRoot = %q, want the display setting to have no reach into it", cfg.MemoryRoot)
	}

	// And the reverse: a refused MEMORY_ROOT is refused whatever display path
	// is beside it, rather than one standing in for the other.
	env = requiredEnv()
	env["MEMORY_ROOT"] = "relative/memory"
	env["MEMORY_DISPLAY_ROOT"] = "/Users/someone/memory"
	if _, err := LoadFromMap(env); err == nil || !strings.Contains(err.Error(), "MEMORY_ROOT") {
		t.Fatalf("error = %v, want MEMORY_ROOT refused on its own terms", err)
	}
}
