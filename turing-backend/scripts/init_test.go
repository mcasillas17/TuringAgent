package main

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInitRefreshesStaleAutomaticHostIdentity(t *testing.T) {
	result := runInit(t, "501", "20", `
HOST_UID=2000
HOST_GID=2000
`)

	assertEnvValue(t, result.env, "HOST_UID", "501")
	assertEnvValue(t, result.env, "HOST_GID", "20")
}

func TestInitUsesCurrentNonRootIdentityInsteadOfConfiguredOverrides(t *testing.T) {
	result := runInit(t, "501", "20", `
HOST_IDENTITY_MODE=manual
HOST_UID=1234
HOST_GID=2345
`)

	assertEnvValue(t, result.env, "HOST_UID", "501")
	assertEnvValue(t, result.env, "HOST_GID", "20")
	assertChownCalls(t, result)
}

func TestInitRejectsRootOrInvalidCurrentIdentityBeforeMutation(t *testing.T) {
	tests := []struct {
		name string
		uid  string
		gid  string
	}{
		{name: "root", uid: "0", gid: "0"},
		{name: "zero padded root", uid: "00", gid: "20"},
		{name: "zero padded positive", uid: "01", gid: "20"},
		{name: "negative", uid: "-1", gid: "20"},
		{name: "nonnumeric", uid: "not-a-number", gid: "20"},
		{name: "above portable maximum", uid: "2147483648", gid: "20"},
		{name: "far above portable maximum", uid: "99999999999999999999", gid: "20"},
		{name: "invalid group", uid: "20", gid: "01"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := executeInit(t, test.uid, test.gid, "", 0)
			if result.err == nil {
				t.Fatalf("init.sh succeeded for current identity %s:%s; output:\n%s", test.uid, test.gid, result.output)
			}
			if !strings.Contains(result.output, "must be run by a non-root host user with canonical UID/GID values") {
				t.Fatalf("failure did not explain the host identity requirement:\n%s", result.output)
			}
			if _, err := os.Lstat(result.sandbox); !os.IsNotExist(err) {
				t.Fatalf("sandbox was mutated before identity rejection: %v", err)
			}
			assertChownCalls(t, result)
		})
	}
}

func TestInitCreatesRealOwnedWritableTraversableSandboxWithoutChown(t *testing.T) {
	result := runInit(t, "501", "20", "")

	assertEnvValue(t, result.env, "HOST_UID", "501")
	assertEnvValue(t, result.env, "HOST_GID", "20")
	assertChownCalls(t, result)
	info, err := os.Lstat(result.sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("sandbox mode = %v, want a real directory", info.Mode())
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("fresh sandbox permissions = %04o, want 0700 independent of umask", info.Mode().Perm())
	}
}

func TestInitCreatesPrivateSkillsDirectory(t *testing.T) {
	result := runInit(t, "501", "20", "")

	info, err := os.Lstat(result.skills)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("skills mode = %v, want a real directory", info.Mode())
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("fresh skills permissions = %04o, want 0700", info.Mode().Perm())
	}
}

func TestInitRejectsSymlinkedSkillsDirectory(t *testing.T) {
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		target := filepath.Join(t.TempDir(), "skills")
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, "skills")); err != nil {
			t.Fatal(err)
		}
	})
	if result.err == nil {
		t.Fatal("init.sh accepted a symlinked skills directory")
	}
	if !strings.Contains(result.output, "skills must be a real directory, not a symlink") {
		t.Fatalf("failure did not explain the skills symlink rejection:\n%s", result.output)
	}
}

// The vault has to exist before the orchestrator mounts it, with the same
// private mode as skills, and with the two folders the brain is organised
// around already present so the user's first look in Obsidian is the real
// layout rather than an empty directory.
func TestInitCreatesPrivateMemoryVaultWithTierDirectories(t *testing.T) {
	result := runInit(t, "501", "20", "")

	for _, path := range []string{
		result.memory,
		filepath.Join(result.memory, "inbox"),
		filepath.Join(result.memory, "beliefs"),
	} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("%s mode = %v, want a real directory", path, info.Mode())
		}
		if info.Mode().Perm() != 0700 {
			t.Fatalf("fresh %s permissions = %04o, want 0700 independent of umask", path, info.Mode().Perm())
		}
	}
}

// persona.md is the only pinned document the agent can never write, so a
// fresh install ships an active starter persona rather than an empty file: a
// fresh install's remote-egress disclosure must be honest, and an empty
// persona would disclose nothing. Markdown's "#" makes a heading, not a
// comment, and nothing in this file is inert — every line, headings
// included, is pinned into every run exactly as written. The file must say
// so, and must not claim otherwise (no "commented out", no "uncomment").
func TestInitShipsAnActiveDefaultPersona(t *testing.T) {
	result := runInit(t, "501", "20", "")

	persona := filepath.Join(result.memory, "persona.md")
	info, err := os.Lstat(persona)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("persona.md mode = %v, want a regular file", info.Mode())
	}
	assertMode(t, persona, 0600)
	content, err := os.ReadFile(persona)
	if err != nil {
		t.Fatal(err)
	}
	body := string(content)
	if strings.TrimSpace(body) == "" {
		t.Fatal("the default persona is empty; a fresh install would pin nothing and disclose nothing")
	}
	lower := strings.ToLower(body)
	for _, forbidden := range []string{"uncomment", "commented out"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("the default persona falsely claims its lines are inert comments (found %q):\n%s", forbidden, body)
		}
	}
	if !strings.Contains(lower, "active") && !strings.Contains(lower, "pinned into every run") {
		t.Fatalf("the default persona does not state that its contents are active/pinned until edited:\n%s", body)
	}
	for _, want := range []string{
		"You are Turing, a careful assistant running on this machine.",
		"Answer briefly. Say when you are unsure rather than guessing.",
		"Ask before doing anything that changes files or leaves the machine.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the default persona is missing the intended default persona line %q:\n%s", want, body)
		}
	}
	// Initialization prints one secret on purpose, the client API key. The
	// user's own persona prose is not the script's to echo.
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if line = strings.TrimSpace(line); line != "" && strings.Contains(result.output, line) {
			t.Fatalf("init.sh printed persona content %q:\n%s", line, result.output)
		}
	}
}

// The default persona is active prose, pinned into every run exactly as
// written, not a commented-out placeholder the user must uncomment. That is
// tested against the persona content itself above; this test guards the
// *documentation* describing it (README.md and CLAUDE.md, which describe
// init.sh's behavior for humans) so the same false "commented default" /
// "uncomment" framing cannot silently return there even if the persona
// content and its own test stay honest.
func TestDocsDoNotClaimPersonaIsCommented(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	for _, relPath := range []string{"README.md", "CLAUDE.md"} {
		path := filepath.Join(repoRoot, relPath)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", relPath, err)
		}
		lower := strings.ToLower(string(content))
		for _, forbidden := range []string{"commented default", "uncomment"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%s falsely describes the default persona as a commented-out placeholder (found %q); "+
					"it must instead say init.sh writes an active starter persona.md, pinned exactly as written, "+
					"only when the file is absent", relPath, forbidden)
			}
		}
	}
}

// Re-running init.sh is routine — after a pull, after a reset, after a token
// rotation. It must never rewrite the persona the user has been editing.
func TestInitNeverOverwritesAnExistingPersona(t *testing.T) {
	const authored = "I am the user's own persona, hand written.\n"
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		memory := filepath.Join(root, "memory")
		if err := os.Mkdir(memory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(memory, "persona.md"), []byte(authored), 0600); err != nil {
			t.Fatal(err)
		}
	})
	if result.err != nil {
		t.Fatalf("init.sh failed: %v\n%s", result.err, result.output)
	}
	persona := filepath.Join(result.memory, "persona.md")
	content, err := os.ReadFile(persona)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != authored {
		t.Fatalf("persona.md = %q, want the user's own text preserved", content)
	}

	// A second run is the real idempotency claim: the first one created
	// nothing here, so only the second proves the guard is on the file's
	// existence rather than on the directory's.
	second := runInit(t, "501", "20", "")
	created, err := os.ReadFile(filepath.Join(second.memory, "persona.md"))
	if err != nil {
		t.Fatal(err)
	}
	third := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		memory := filepath.Join(root, "memory")
		if err := os.Mkdir(memory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(memory, "persona.md"), created, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(memory, "profile.md"), []byte("the user's prose\n"), 0600); err != nil {
			t.Fatal(err)
		}
	})
	if third.err != nil {
		t.Fatalf("init.sh failed on an initialized vault: %v\n%s", third.err, third.output)
	}
	profile, err := os.ReadFile(filepath.Join(third.memory, "profile.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(profile) != "the user's prose\n" {
		t.Fatalf("profile.md = %q, want the user's own text preserved", profile)
	}
}

func TestInitRejectsSymlinkedMemoryVault(t *testing.T) {
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		target := filepath.Join(t.TempDir(), "memory")
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, "memory")); err != nil {
			t.Fatal(err)
		}
	})
	if result.err == nil {
		t.Fatal("init.sh accepted a symlinked memory vault")
	}
	if !strings.Contains(result.output, "memory must be a real directory, not a symlink") {
		t.Fatalf("failure did not explain the memory symlink rejection:\n%s", result.output)
	}
}

// A symlinked persona is the interesting one: writing the default through it
// would create or truncate a file anywhere the link points, under the host
// user's own identity.
func TestInitRejectsUnsafeMemoryVaultEntries(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*testing.T, string)
		wantOutput string
	}{
		{
			name: "persona symlink",
			setup: func(t *testing.T, root string) {
				memory := filepath.Join(root, "memory")
				if err := os.Mkdir(memory, 0700); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(t.TempDir(), "outside-persona.md")
				if err := os.WriteFile(target, []byte("outside\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(memory, "persona.md")); err != nil {
					t.Fatal(err)
				}
			},
			wantOutput: "memory/persona.md must be an owned regular file, not a symlink",
		},
		{
			name: "vault is a file",
			setup: func(t *testing.T, root string) {
				if err := os.WriteFile(filepath.Join(root, "memory"), []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			},
			wantOutput: "memory must be a real directory",
		},
		{
			name: "inbox is a file",
			setup: func(t *testing.T, root string) {
				memory := filepath.Join(root, "memory")
				if err := os.Mkdir(memory, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(memory, "inbox"), []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			},
			wantOutput: "memory/inbox must be a real directory",
		},
		{
			name: "profile symlink",
			setup: func(t *testing.T, root string) {
				memory := filepath.Join(root, "memory")
				if err := os.Mkdir(memory, 0700); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(t.TempDir(), "outside-profile.md")
				if err := os.WriteFile(target, []byte("outside\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(memory, "profile.md")); err != nil {
					t.Fatal(err)
				}
			},
			wantOutput: "memory/profile.md must be an owned regular file, not a symlink",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := executeInitWithSetup(t, "501", "20", "", 0, test.setup)
			if result.err == nil {
				t.Fatalf("init.sh accepted an unsafe memory vault:\n%s", result.output)
			}
			if !strings.Contains(result.output, test.wantOutput) {
				t.Fatalf("failure did not explain the unsafe memory vault:\n%s", result.output)
			}
		})
	}
}

// Both pinned documents are prose about the user, and persona.md is the one
// unframed instruction channel in the system. A copy restored from a backup, or
// written under a permissive umask, is tightened on the next run rather than
// left readable by everyone with an account on the machine.
func TestInitSecuresExistingPinnedDocuments(t *testing.T) {
	const persona = "my own persona\n"
	const profile = "my own profile\n"
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		memory := filepath.Join(root, "memory")
		if err := os.Mkdir(memory, 0700); err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string]string{
			"persona.md": persona,
			"profile.md": profile,
		} {
			path := filepath.Join(memory, name)
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0644); err != nil {
				t.Fatal(err)
			}
		}
	})
	if result.err != nil {
		t.Fatalf("init.sh failed: %v\n%s", result.err, result.output)
	}
	for name, want := range map[string]string{
		"persona.md": persona,
		"profile.md": profile,
	} {
		path := filepath.Join(result.memory, name)
		assertMode(t, path, 0600)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != want {
			t.Fatalf("%s = %q, want the user's own text preserved", name, content)
		}
	}
}

// profile.md is the user's to write, and the client creates it on first save.
// init.sh must not invent one: an empty file the user never wrote would pin
// nothing but would replace the visible "not written yet" state with silence.
func TestInitDoesNotCreateAProfile(t *testing.T) {
	result := runInit(t, "501", "20", "")

	if _, err := os.Lstat(filepath.Join(result.memory, "profile.md")); !os.IsNotExist(err) {
		t.Fatalf("init.sh created a profile.md the user did not write: %v", err)
	}
}

// A vault carried over from an earlier install, or created by a user with a
// permissive umask, is secured rather than refused: init.sh owns provisioning,
// and persona.md is the one unframed instruction channel in the system, so
// leaving it group-readable is not an option.
func TestInitSecuresAnExistingPermissiveMemoryVault(t *testing.T) {
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		memory := filepath.Join(root, "memory")
		if err := os.Mkdir(memory, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(memory, 0755); err != nil {
			t.Fatal(err)
		}
		inbox := filepath.Join(memory, "inbox")
		if err := os.Mkdir(inbox, 0770); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(inbox, 0770); err != nil {
			t.Fatal(err)
		}
	})
	if result.err != nil {
		t.Fatalf("init.sh failed: %v\n%s", result.err, result.output)
	}
	assertMode(t, result.memory, 0700)
	assertMode(t, filepath.Join(result.memory, "inbox"), 0700)
	assertMode(t, filepath.Join(result.memory, "beliefs"), 0700)
}

func TestInitCreatesPrivateDataDirectoryWithoutChown(t *testing.T) {
	result := runInit(t, "501", "20", "")

	assertChownCalls(t, result)
	info, err := os.Lstat(result.data)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("data mode = %v, want a real directory", info.Mode())
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("fresh data permissions = %04o, want 0700 independent of umask", info.Mode().Perm())
	}
}

func TestInitSecuresOwnedLegacyDataAndSQLiteFiles(t *testing.T) {
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		data := filepath.Join(root, "data")
		if err := os.Mkdir(data, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(data, 0755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"turing.db", "turing.db-wal", "turing.db-shm"} {
			path := filepath.Join(data, name)
			if err := os.WriteFile(path, []byte("legacy"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0644); err != nil {
				t.Fatal(err)
			}
		}
	})
	if result.err != nil {
		t.Fatalf("init.sh failed: %v\n%s", result.err, result.output)
	}

	assertMode(t, result.data, 0700)
	for _, name := range []string{"turing.db", "turing.db-wal", "turing.db-shm"} {
		assertMode(t, filepath.Join(result.data, name), 0600)
	}
	assertChownCalls(t, result)
}

func TestInitRejectsUnsafeDataTypes(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*testing.T, string)
		wantOutput string
	}{
		{
			name: "data symlink",
			setup: func(t *testing.T, root string) {
				target := filepath.Join(root, "outside-data")
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(root, "data")); err != nil {
					t.Fatal(err)
				}
			},
			wantOutput: "data must be a real directory, not a symlink",
		},
		{
			name: "database symlink",
			setup: func(t *testing.T, root string) {
				data := filepath.Join(root, "data")
				if err := os.Mkdir(data, 0700); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(root, "outside.db")
				if err := os.WriteFile(target, []byte("outside"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(data, "turing.db")); err != nil {
					t.Fatal(err)
				}
			},
			wantOutput: "database file must be a regular file, not a symlink",
		},
		{
			name: "database directory",
			setup: func(t *testing.T, root string) {
				data := filepath.Join(root, "data")
				if err := os.Mkdir(data, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(data, "turing.db"), 0700); err != nil {
					t.Fatal(err)
				}
			},
			wantOutput: "database file must be a regular file",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := executeInitWithSetup(t, "501", "20", "", 0, test.setup)
			if result.err == nil {
				t.Fatalf("init.sh accepted unsafe data type; output:\n%s", result.output)
			}
			if !strings.Contains(result.output, test.wantOutput) {
				t.Fatalf("failure did not explain unsafe data type:\n%s", result.output)
			}
			if strings.Contains(result.output, "backend initialized") {
				t.Fatalf("init.sh claimed readiness with unsafe data:\n%s", result.output)
			}
			assertChownCalls(t, result)
		})
	}
}

func TestInitRejectsPreExistingSandboxSymlink(t *testing.T) {
	var target string
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		target = filepath.Join(root, "outside")
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, "sandbox")); err != nil {
			t.Fatal(err)
		}
	})

	if result.err == nil {
		t.Fatalf("init.sh succeeded; output:\n%s", result.output)
	}
	if !strings.Contains(result.output, "sandbox must be a real directory, not a symlink") {
		t.Fatalf("failure did not explain the sandbox symlink rejection:\n%s", result.output)
	}
	if strings.Contains(result.output, "backend initialized") {
		t.Fatalf("init.sh claimed readiness for a symlinked sandbox:\n%s", result.output)
	}
	assertChownCalls(t, result)
	if info, err := os.Lstat(result.sandbox); err != nil {
		t.Fatalf("sandbox symlink was removed: %v", err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("sandbox symlink was replaced: mode=%v", info.Mode())
	}
	if info, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target was removed: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("symlink target was mutated: mode=%v", info.Mode())
	}
}

func TestInitRejectsInaccessibleLegacySandboxEntries(t *testing.T) {
	tests := []struct {
		name       string
		create     func(t *testing.T, sandbox string)
		wantOutput string
	}{
		{
			name: "directory",
			create: func(t *testing.T, sandbox string) {
				path := filepath.Join(sandbox, "locked")
				if err := os.Mkdir(path, 0500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0700) })
			},
			wantOutput: "legacy sandbox directory is not readable, writable, and traversable: locked",
		},
		{
			name: "file",
			create: func(t *testing.T, sandbox string) {
				path := filepath.Join(sandbox, "locked.txt")
				if err := os.WriteFile(path, []byte("legacy"), 0000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0600) })
			},
			wantOutput: "legacy sandbox file is not readable and writable: locked.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
				sandbox := filepath.Join(root, "sandbox")
				if err := os.Mkdir(sandbox, 0700); err != nil {
					t.Fatal(err)
				}
				test.create(t, sandbox)
			})

			if result.err == nil {
				t.Fatalf("init.sh succeeded; output:\n%s", result.output)
			}
			if !strings.Contains(result.output, test.wantOutput) {
				t.Fatalf("failure did not identify the inaccessible entry:\n%s", result.output)
			}
			if strings.Contains(result.output, "backend initialized") {
				t.Fatalf("init.sh claimed readiness with inaccessible legacy content:\n%s", result.output)
			}
			assertChownCalls(t, result)
		})
	}
}

func TestInitRejectsGroupOrWorldWritableSandbox(t *testing.T) {
	for name, mode := range map[string]os.FileMode{
		"group writable": 0770,
		"world writable": 0702,
	} {
		t.Run(name, func(t *testing.T) {
			result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
				sandbox := filepath.Join(root, "sandbox")
				if err := os.Mkdir(sandbox, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(sandbox, mode); err != nil {
					t.Fatal(err)
				}
			})

			if result.err == nil {
				t.Fatalf("init.sh accepted sandbox mode %04o", mode)
			}
			if !strings.Contains(result.output, "sandbox must not be group- or world-writable") {
				t.Fatalf("failure did not explain unsafe sandbox permissions:\n%s", result.output)
			}
		})
	}
}

func TestInitSecuresExistingSandboxMode(t *testing.T) {
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		sandbox := filepath.Join(root, "sandbox")
		if err := os.Mkdir(sandbox, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(sandbox, 0755); err != nil {
			t.Fatal(err)
		}
	})

	if result.err != nil {
		t.Fatalf("init.sh failed to secure existing sandbox: %v\n%s", result.err, result.output)
	}
	assertMode(t, result.sandbox, 0700)
}

func TestInitRejectsGroupOrWorldWritableSandboxEntries(t *testing.T) {
	tests := []struct {
		name   string
		create func(*testing.T, string)
	}{
		{
			name: "directory",
			create: func(t *testing.T, sandbox string) {
				path := filepath.Join(sandbox, "shared")
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0770); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "file",
			create: func(t *testing.T, sandbox string) {
				path := filepath.Join(sandbox, "shared.txt")
				if err := os.WriteFile(path, []byte("shared"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0602); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
				sandbox := filepath.Join(root, "sandbox")
				if err := os.Mkdir(sandbox, 0700); err != nil {
					t.Fatal(err)
				}
				test.create(t, sandbox)
			})

			if result.err == nil {
				t.Fatalf("init.sh accepted unsafe %s permissions", test.name)
			}
			if !strings.Contains(result.output, "must not be group- or world-writable") {
				t.Fatalf("failure did not explain unsafe entry permissions:\n%s", result.output)
			}
		})
	}
}

func TestInitRejectsEnvSymlinkBeforeMutation(t *testing.T) {
	var target string
	const original = "EXTERNAL=keep\n"
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		if err := os.Remove(filepath.Join(root, ".env")); err != nil {
			t.Fatal(err)
		}
		target = filepath.Join(root, "outside.env")
		if err := os.WriteFile(target, []byte(original), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, ".env")); err != nil {
			t.Fatal(err)
		}
	})

	if result.err == nil {
		t.Fatalf("init.sh accepted .env symlink:\n%s", result.output)
	}
	if !strings.Contains(result.output, ".env must be a regular file, not a symlink") {
		t.Fatalf("failure did not explain .env symlink rejection:\n%s", result.output)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != original {
		t.Fatalf(".env symlink target changed: content=%q err=%v", content, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Fatalf(".env symlink target mode changed to %04o", info.Mode().Perm())
	}
}

func TestInitRejectsNonRegularEnvBeforeChmod(t *testing.T) {
	var envDirectory string
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		if err := os.Remove(filepath.Join(root, ".env")); err != nil {
			t.Fatal(err)
		}
		envDirectory = filepath.Join(root, ".env")
		if err := os.Mkdir(envDirectory, 0700); err != nil {
			t.Fatal(err)
		}
	})

	if result.err == nil {
		t.Fatalf("init.sh accepted non-regular .env:\n%s", result.output)
	}
	if !strings.Contains(result.output, ".env must be a regular file") {
		t.Fatalf("failure did not explain non-regular .env rejection:\n%s", result.output)
	}
	info, err := os.Stat(envDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("non-regular .env was chmodded to %04o", info.Mode().Perm())
	}
}

type initResult struct {
	// root is the checkout init.sh ran in, so a test can run it again in the
	// same place. Idempotence is about a second run over the first one's own
	// files, which a fresh directory cannot express.
	root     string
	sandbox  string
	skills   string
	memory   string
	data     string
	env      string
	envErr   error
	output   string
	chownLog string
	err      error
}

func runInit(t *testing.T, uid, gid, identityConfig string) initResult {
	t.Helper()
	result := executeInit(t, uid, gid, identityConfig, 0)
	if result.err != nil {
		t.Fatalf("init.sh failed: %v\n%s", result.err, result.output)
	}
	if result.envErr != nil {
		t.Fatalf("read initialized .env: %v", result.envErr)
	}
	return result
}

func executeInit(t *testing.T, uid, gid, identityConfig string, chownExit int) initResult {
	t.Helper()
	return executeInitWithSetup(t, uid, gid, identityConfig, chownExit, nil)
}

func executeInitWithSetup(t *testing.T, uid, gid, identityConfig string, chownExit int, setup func(*testing.T, string)) initResult {
	t.Helper()
	return executeInitIn(t, t.TempDir(), uid, gid, identityConfig, chownExit, setup)
}

// executeInitInDirectory runs init.sh under a checkout directory of the test's
// choosing. Every other caller does not care where the checkout is; the one
// that does cares because a path with a space in it is what most of these
// helpers would quietly get wrong.
func executeInitInDirectory(t *testing.T, uid, gid, identityConfig string, directory string) initResult {
	t.Helper()
	root := filepath.Join(t.TempDir(), directory)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	return executeInitIn(t, root, uid, gid, identityConfig, 0, nil)
}

// rerunInit runs init.sh again over the checkout a previous run left behind,
// with that run's own .env in place. It is how idempotence is actually asked
// about: a second run in a fresh directory answers a different question.
func rerunInit(t *testing.T, previous initResult) initResult {
	t.Helper()
	command := exec.Command("bash", filepath.Join(previous.root, "scripts", "init.sh"))
	command.Env = append(os.Environ(),
		"PATH="+filepath.Join(previous.root, "bin")+":"+os.Getenv("PATH"),
		"CHOWN_LOG="+previous.chownLog,
		"CHOWN_EXIT=0",
	)
	output, commandErr := command.CombinedOutput()
	updated, err := os.ReadFile(filepath.Join(previous.root, ".env"))
	rerun := previous
	rerun.env = string(updated)
	rerun.envErr = err
	rerun.output = string(output)
	rerun.err = commandErr
	return rerun
}

func executeInitIn(t *testing.T, root string, uid, gid, identityConfig string, chownExit int, setup func(*testing.T, string)) initResult {
	t.Helper()
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scriptsDir, 0700); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("init.sh")
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(scriptsDir, "init.sh")
	if err := os.WriteFile(scriptPath, script, 0700); err != nil {
		t.Fatal(err)
	}
	env := "TURING_CLIENT_API_KEY=client\n" +
		"MCP_SYSTEM_TOKEN_GENERAL=system\n" +
		"MCP_FILES_TOKEN_GENERAL=files\n" +
		"TURING_APPROVAL_JWT_SECRET=approval\n" +
		strings.TrimSpace(identityConfig) + "\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(env), 0600); err != nil {
		t.Fatal(err)
	}
	if setup != nil {
		setup(t, root)
	}

	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	fakeID := "#!/bin/sh\ncase \"$1\" in\n-u) printf '%s\\n' '" + uid + "' ;;\n-g) printf '%s\\n' '" + gid + "' ;;\n*) exit 2 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(binDir, "id"), []byte(fakeID), 0700); err != nil {
		t.Fatal(err)
	}
	chownLog := filepath.Join(root, "chown.log")
	fakeChown := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CHOWN_LOG\"\nexit \"${CHOWN_EXIT:-0}\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "chown"), []byte(fakeChown), 0700); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("bash", scriptPath)
	command.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"CHOWN_LOG="+chownLog,
		"CHOWN_EXIT="+strconv.Itoa(chownExit),
	)
	output, commandErr := command.CombinedOutput()
	updated, err := os.ReadFile(filepath.Join(root, ".env"))
	return initResult{
		root:     root,
		sandbox:  filepath.Join(root, "sandbox"),
		skills:   filepath.Join(root, "skills"),
		memory:   filepath.Join(root, "memory"),
		data:     filepath.Join(root, "data"),
		env:      string(updated),
		envErr:   err,
		output:   string(output),
		chownLog: chownLog,
		err:      commandErr,
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s permissions = %04o, want %04o", path, got, want)
	}
}

func assertChownCalls(t *testing.T, result initResult, want ...string) {
	t.Helper()
	data, err := os.ReadFile(result.chownLog)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(data))
	wantText := strings.Join(want, "\n")
	if got != wantText {
		t.Fatalf("chown calls = %q, want %q", got, wantText)
	}
}

func assertEnvValue(t *testing.T, env, name, want string) {
	t.Helper()
	prefix := name + "="
	for _, line := range strings.Split(env, "\n") {
		if strings.HasPrefix(line, prefix) {
			if got := strings.TrimPrefix(line, prefix); got != want {
				t.Fatalf("%s = %q, want %q\n.env:\n%s", name, got, want, env)
			}
			return
		}
	}
	t.Fatalf("%s missing from .env:\n%s", name, env)
}

// The key that seals third-party credentials has to exist before anyone can
// connect an account, and it belongs in .env with the other secrets rather
// than in the database it protects.
func TestInitGeneratesAnIntegrationKeyOfTheRightShape(t *testing.T) {
	result := runInit(t, "501", "20", "")

	value := envValue(t, result.env, "TURING_INTEGRATION_KEY")
	if len(value) != 64 {
		t.Fatalf("TURING_INTEGRATION_KEY = %q (%d chars), want 64 hex characters", value, len(value))
	}
	if _, err := hex.DecodeString(value); err != nil {
		t.Fatalf("TURING_INTEGRATION_KEY is not hex: %v", err)
	}
}

// Re-running init.sh must not rotate it: a new key would make every stored
// credential unreadable, and the accounts would silently stop working.
func TestInitKeepsAnExistingIntegrationKey(t *testing.T) {
	existing := strings.Repeat("ab", 32)

	result := runInit(t, "501", "20", "TURING_INTEGRATION_KEY="+existing+"\n")

	assertEnvValue(t, result.env, "TURING_INTEGRATION_KEY", existing)
}

func TestInitGeneratesAndPreservesEgressSigningSecret(t *testing.T) {
	generated := runInit(t, "501", "20", "")
	value := envValue(t, generated.env, "TURING_EGRESS_SIGNING_SECRET")
	if len(value) != 64 {
		t.Fatalf("TURING_EGRESS_SIGNING_SECRET has %d chars, want 64", len(value))
	}
	if _, err := hex.DecodeString(value); err != nil {
		t.Fatalf("TURING_EGRESS_SIGNING_SECRET is not hex: %v", err)
	}

	existing := strings.Repeat("cd", 32)
	restarted := runInit(t, "501", "20", "TURING_EGRESS_SIGNING_SECRET="+existing+"\n")
	assertEnvValue(t, restarted.env, "TURING_EGRESS_SIGNING_SECRET", existing)
}

func TestInitGeneratesACursorHMACSecretOfTheRightShape(t *testing.T) {
	result := runInit(t, "501", "20", "")

	value := envValue(t, result.env, "TURING_CURSOR_HMAC_SECRET")
	if len(value) != 64 {
		t.Fatalf("TURING_CURSOR_HMAC_SECRET = %q (%d chars), want 64 hex characters", value, len(value))
	}
	if _, err := hex.DecodeString(value); err != nil {
		t.Fatalf("TURING_CURSOR_HMAC_SECRET is not hex: %v", err)
	}
	if value != strings.ToLower(value) {
		t.Fatalf("TURING_CURSOR_HMAC_SECRET = %q, want lowercase hex", value)
	}
}

func TestInitKeepsAnExistingCursorHMACSecret(t *testing.T) {
	existing := strings.Repeat("cd", 32)

	result := runInit(t, "501", "20", "TURING_CURSOR_HMAC_SECRET="+existing+"\n")
	assertEnvValue(t, result.env, "TURING_CURSOR_HMAC_SECRET", existing)
	assertEnvValue(t, result.env, "TURING_CURSOR_HMAC_SECRET", existing)
}

// The runtime and approval-consumer identities must never collide: a shared
// secret would let a compromised approval consumer (mcp-files) present the
// runtime's own credential and reach RuntimeService/SessionService, which is
// exactly the privilege escalation TUR-006 removes.
func TestInitGeneratesDistinctRuntimeAndApprovalConsumerTokens(t *testing.T) {
	result := runInit(t, "501", "20", "")

	runtimeToken := envValue(t, result.env, "TURING_RUNTIME_TOKEN")
	approvalConsumerToken := envValue(t, result.env, "TURING_APPROVAL_CONSUMER_TOKEN")
	for name, value := range map[string]string{
		"TURING_RUNTIME_TOKEN":           runtimeToken,
		"TURING_APPROVAL_CONSUMER_TOKEN": approvalConsumerToken,
	} {
		if len(value) != 64 {
			t.Fatalf("%s has %d chars, want 64 hex characters", name, len(value))
		}
		if _, err := hex.DecodeString(value); err != nil {
			t.Fatalf("%s is not hex", name)
		}
	}
	if runtimeToken == approvalConsumerToken {
		t.Fatal("TURING_RUNTIME_TOKEN and TURING_APPROVAL_CONSUMER_TOKEN were generated equal")
	}
}

// Restarting the stack (re-running init.sh) must not rotate either identity's
// token out from under services that already hold it: rotation is a distinct,
// deliberate action, not a side effect of an ordinary restart.
func TestInitKeepsExistingRuntimeAndApprovalConsumerTokensAcrossRestart(t *testing.T) {
	existingRuntime := strings.Repeat("11", 32)
	existingApprovalConsumer := strings.Repeat("22", 32)

	result := runInit(t, "501", "20",
		"TURING_RUNTIME_TOKEN="+existingRuntime+"\n"+
			"TURING_APPROVAL_CONSUMER_TOKEN="+existingApprovalConsumer+"\n")

	assertEnvValue(t, result.env, "TURING_RUNTIME_TOKEN", existingRuntime)
	assertEnvValue(t, result.env, "TURING_APPROVAL_CONSUMER_TOKEN", existingApprovalConsumer)
}

func envValue(t *testing.T, env, name string) string {
	t.Helper()
	prefix := name + "="
	for _, line := range strings.Split(env, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("%s missing from .env:\n%s", name, env)
	return ""
}
