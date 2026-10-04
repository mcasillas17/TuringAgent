package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const teamTemplatesDirectory = "team-templates"

var seededProfiles = []string{"dev", "inbox", "research"}

// copyTeamTemplates mirrors the tracked templates beside the copied init.sh,
// because init.sh reads them relative to its own checkout.
func copyTeamTemplates(t *testing.T, destination string) {
	t.Helper()
	err := filepath.WalkDir(teamTemplatesDirectory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(teamTemplatesDirectory, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func teamEntries(t *testing.T, team string) []string {
	t.Helper()
	entries, err := os.ReadDir(team)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// A fresh install gets the three default specialists, copied byte for byte
// from the tracked templates, private to the host user. They arrive disabled:
// nothing here grants them anything.
func TestInitSeedsTheDefaultTeamIntoAPrivateDirectory(t *testing.T) {
	result := runInit(t, "501", "20", "")

	assertMode(t, result.team, 0o700)
	if got := teamEntries(t, result.team); !slices.Equal(got, seededProfiles) {
		t.Fatalf("team entries = %v, want %v", got, seededProfiles)
	}
	for _, id := range seededProfiles {
		assertMode(t, filepath.Join(result.team, id), 0o700)
		path := filepath.Join(result.team, id, "AGENT.md")
		assertMode(t, path, 0o600)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(teamTemplatesDirectory, id, "AGENT.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("team/%s/AGENT.md differs from its tracked template", id)
		}
	}
}

// The checkout tracks team/.gitkeep, so "empty" has to mean "no profiles yet",
// not "no entries at all", or no checkout would ever be seeded.
func TestInitSeedsATeamDirectoryHoldingOnlyHiddenFiles(t *testing.T) {
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		team := filepath.Join(root, "team")
		if err := os.Mkdir(team, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(team, ".gitkeep"), []byte("\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	if result.err != nil {
		t.Fatalf("init.sh failed: %v\n%s", result.err, result.output)
	}
	assertMode(t, result.team, 0o700)
	want := append([]string{".gitkeep"}, seededProfiles...)
	if got := teamEntries(t, result.team); !slices.Equal(got, want) {
		t.Fatalf("team entries = %v, want %v", got, want)
	}
}

// Seeding is a first-install act. A rerun must neither rewrite an edited
// profile nor bring back one the user deleted: a deleted specialist's grant
// survives in the database, so resurrecting the same file would silently make
// it active again.
func TestInitNeverRewritesOrResurrectsTeamProfiles(t *testing.T) {
	first := runInit(t, "501", "20", "")
	edited := filepath.Join(first.team, "research", "AGENT.md")
	const authored = "---\nname: Research\ndescription: Mine now.\n---\nMy own instructions.\n"
	if err := os.WriteFile(edited, []byte(authored), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(first.team, "dev")); err != nil {
		t.Fatal(err)
	}

	second := rerunInit(t, first)
	if second.err != nil {
		t.Fatalf("init.sh rerun failed: %v\n%s", second.err, second.output)
	}
	content, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != authored {
		t.Fatalf("research/AGENT.md = %q, want the user's edit preserved", content)
	}
	if _, err := os.Lstat(filepath.Join(second.team, "dev")); !os.IsNotExist(err) {
		t.Fatalf("a deleted default profile came back on rerun (err = %v)", err)
	}
}

// A user who brings their own team before the first init keeps exactly it.
func TestInitDoesNotSeedBesideTheUsersOwnProfile(t *testing.T) {
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		finance := filepath.Join(root, "team", "finance")
		if err := os.MkdirAll(finance, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(finance, "AGENT.md"), []byte("---\nname: Finance\ndescription: Money.\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if result.err != nil {
		t.Fatalf("init.sh failed: %v\n%s", result.err, result.output)
	}
	if got := teamEntries(t, result.team); !slices.Equal(got, []string{"finance"}) {
		t.Fatalf("team entries = %v, want only the user's profile", got)
	}
	// Nothing was copied, so the receipt must not say the defaults were.
	receipt, err := os.ReadFile(filepath.Join(result.data, "team-seeded"))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(receipt); strings.Contains(text, "has seeded") ||
		!strings.Contains(strings.Join(strings.Fields(text), " "), "or found profiles there first") {
		t.Fatalf("receipt overclaims seeding:\n%s", receipt)
	}

	if err := os.RemoveAll(filepath.Join(result.team, "finance")); err != nil {
		t.Fatal(err)
	}
	rerun := rerunInit(t, result)
	if rerun.err != nil {
		t.Fatalf("init.sh rerun failed: %v\n%s", rerun.err, rerun.output)
	}
	if got := teamEntries(t, rerun.team); len(got) != 0 {
		t.Fatalf("team entries = %v, want the team the user emptied left empty", got)
	}
}

// Emptying team/ is a decision too. Once a team has existed for this
// database, init.sh never seeds again, or every default specialist would
// return with whatever grant the database still holds for it.
func TestInitNeverReseedsATeamTheUserEmptied(t *testing.T) {
	for name, empty := range map[string]func(t *testing.T, team string){
		"every profile deleted": func(t *testing.T, team string) {
			for _, id := range seededProfiles {
				if err := os.RemoveAll(filepath.Join(team, id)); err != nil {
					t.Fatal(err)
				}
			}
		},
		"team directory deleted": func(t *testing.T, team string) {
			if err := os.RemoveAll(team); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			first := runInit(t, "501", "20", "")
			assertMode(t, filepath.Join(first.data, "team-seeded"), 0o600)
			empty(t, first.team)

			second := rerunInit(t, first)
			if second.err != nil {
				t.Fatalf("init.sh rerun failed: %v\n%s", second.err, second.output)
			}
			assertMode(t, second.team, 0o700)
			if got := teamEntries(t, second.team); len(got) != 0 {
				t.Fatalf("team entries = %v, want the emptied team left empty", got)
			}
		})
	}
}

// The receipt lives beside the database because it stands for the grants in
// it: a fresh data/ has none to bring back, so an empty team is seeded again.
func TestInitReseedsAnEmptyTeamOnceTheReceiptIsGone(t *testing.T) {
	first := runInit(t, "501", "20", "")
	for _, id := range seededProfiles {
		if err := os.RemoveAll(filepath.Join(first.team, id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(first.data, "team-seeded")); err != nil {
		t.Fatal(err)
	}

	second := rerunInit(t, first)
	if second.err != nil {
		t.Fatalf("init.sh rerun failed: %v\n%s", second.err, second.output)
	}
	if got := teamEntries(t, second.team); !slices.Equal(got, seededProfiles) {
		t.Fatalf("team entries = %v, want %v", got, seededProfiles)
	}
}

func TestInitRejectsSymlinkedTeamDirectory(t *testing.T) {
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		target := filepath.Join(t.TempDir(), "team")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, "team")); err != nil {
			t.Fatal(err)
		}
	})
	if result.err == nil {
		t.Fatal("init.sh accepted a symlinked team directory")
	}
	if !strings.Contains(result.output, "team must be a real directory, not a symlink") {
		t.Fatalf("failure did not explain the team symlink rejection:\n%s", result.output)
	}
}

// A seed that fails partway leaves no profile behind and no receipt, so the
// next run seeds the whole team instead of keeping the part that landed.
func TestInitRollsBackATeamSeedThatFailsPartway(t *testing.T) {
	var inbox string
	first := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		inbox = filepath.Join(root, "scripts", teamTemplatesDirectory, "inbox", "AGENT.md")
		if err := os.Chmod(inbox, 0o000); err != nil {
			t.Fatal(err)
		}
	})
	if first.err == nil {
		t.Fatal("init.sh succeeded though the inbox template could not be read")
	}
	if !strings.Contains(first.output, "could not seed team/inbox") {
		t.Fatalf("failure did not name the profile it could not seed:\n%s", first.output)
	}
	if got := teamEntries(t, first.team); len(got) != 0 {
		t.Fatalf("team entries = %v after a failed seed, want none", got)
	}
	if _, err := os.Lstat(filepath.Join(first.data, "team-seeded")); !os.IsNotExist(err) {
		t.Fatalf("a failed seed left a receipt (err = %v)", err)
	}

	if err := os.Chmod(inbox, 0o644); err != nil {
		t.Fatal(err)
	}
	second := rerunInit(t, first)
	if second.err != nil {
		t.Fatalf("init.sh rerun failed: %v\n%s", second.err, second.output)
	}
	if got := teamEntries(t, second.team); !slices.Equal(got, seededProfiles) {
		t.Fatalf("team entries = %v, want %v", got, seededProfiles)
	}
}

// init.sh counts what the loader counts: a stray file or a folder with no
// AGENT.md is not a profile, so it neither stops the first seed nor earns the
// receipt that would stop every later one.
func TestInitSeedsBesideEntriesThatAreNotProfiles(t *testing.T) {
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		team := filepath.Join(root, "team")
		if err := os.Mkdir(team, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(team, "README.md"), []byte("notes\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(team, "drafts"), 0o700); err != nil {
			t.Fatal(err)
		}
	})
	if result.err != nil {
		t.Fatalf("init.sh failed: %v\n%s", result.err, result.output)
	}
	want := []string{"README.md", "dev", "drafts", "inbox", "research"}
	if got := teamEntries(t, result.team); !slices.Equal(got, want) {
		t.Fatalf("team entries = %v, want %v", got, want)
	}
	assertMode(t, filepath.Join(result.data, "team-seeded"), 0o600)
}

// The loader lists a folder it cannot look inside as a profile it cannot read,
// so init.sh, unable to tell either, treats it as one and seeds nothing.
func TestInitCountsAFolderItCannotInspectAsAProfile(t *testing.T) {
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		custom := filepath.Join(root, "team", "custom")
		if err := os.MkdirAll(custom, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(custom, "AGENT.md"), []byte("---\nname: C\ndescription: d\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(custom, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(custom, 0o700) })
	})
	if result.err != nil {
		t.Fatalf("init.sh failed: %v\n%s", result.err, result.output)
	}
	if got := teamEntries(t, result.team); !slices.Equal(got, []string{"custom"}) {
		t.Fatalf("team entries = %v, want only the folder it could not inspect", got)
	}
	assertMode(t, filepath.Join(result.data, "team-seeded"), 0o600)
}

// The loader lists a symlinked profile folder, and a folder whose AGENT.md is
// a symlink, as profiles it refuses to read. Even dangling, each is a profile
// the user put there, so nothing is seeded beside it.
func TestInitCountsSymlinkedProfilesAsProfiles(t *testing.T) {
	for name, place := range map[string]func(t *testing.T, team string){
		"dangling profile folder": func(t *testing.T, team string) {
			if err := os.Symlink(filepath.Join(team, "nowhere"), filepath.Join(team, "custom")); err != nil {
				t.Fatal(err)
			}
		},
		"dangling AGENT.md": func(t *testing.T, team string) {
			custom := filepath.Join(team, "custom")
			if err := os.Mkdir(custom, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(team, "nowhere.md"), filepath.Join(custom, "AGENT.md")); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
				team := filepath.Join(root, "team")
				if err := os.Mkdir(team, 0o700); err != nil {
					t.Fatal(err)
				}
				place(t, team)
			})
			if result.err != nil {
				t.Fatalf("init.sh failed: %v\n%s", result.err, result.output)
			}
			if got := teamEntries(t, result.team); !slices.Equal(got, []string{"custom"}) {
				t.Fatalf("team entries = %v, want only the user's profile", got)
			}
			assertMode(t, filepath.Join(result.data, "team-seeded"), 0o600)
		})
	}
}

// A non-profile folder that takes a template's name stops the seed. Nothing
// the run created survives and no receipt is written, so the next run can
// seed once the folder is moved.
func TestInitLeavesAFolderInATemplatesPlaceAlone(t *testing.T) {
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		if err := os.MkdirAll(filepath.Join(root, "team", "inbox"), 0o700); err != nil {
			t.Fatal(err)
		}
	})
	if result.err == nil {
		t.Fatal("init.sh seeded over a folder named after a template")
	}
	if !strings.Contains(result.output, "team/inbox is in the way: it is not a profile (no AGENT.md); move it aside") {
		t.Fatalf("failure did not say what is in the way or how to clear it:\n%s", result.output)
	}
	if got := teamEntries(t, result.team); !slices.Equal(got, []string{"inbox"}) {
		t.Fatalf("team entries = %v, want only the user's folder", got)
	}
	if entries, err := os.ReadDir(filepath.Join(result.team, "inbox")); err != nil || len(entries) != 0 {
		t.Fatalf("the user's folder was changed: %v, %v", entries, err)
	}
	if _, err := os.Lstat(filepath.Join(result.data, "team-seeded")); !os.IsNotExist(err) {
		t.Fatalf("a failed seed left a receipt (err = %v)", err)
	}
}

// A checkout missing a template is broken, and init.sh says which file rather
// than seeding a partial team.
func TestInitFailsWhenATeamTemplateIsMissing(t *testing.T) {
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		if err := os.Remove(filepath.Join(root, "scripts", teamTemplatesDirectory, "inbox", "AGENT.md")); err != nil {
			t.Fatal(err)
		}
	})
	if result.err == nil {
		t.Fatal("init.sh succeeded without the inbox template")
	}
	if !strings.Contains(result.output, "scripts/team-templates/inbox/AGENT.md") {
		t.Fatalf("failure did not name the missing template:\n%s", result.output)
	}
	if _, err := os.Lstat(filepath.Join(result.team, "dev")); !os.IsNotExist(err) {
		t.Fatalf("a partial team was seeded (err = %v)", err)
	}
}

// The receipt says what deleting it does, including that a kept database
// brings each default back with the setting and grant it had.
func TestInitReceiptSaysWhatDeletingItDoes(t *testing.T) {
	result := runInit(t, "501", "20", "")
	text, err := os.ReadFile(filepath.Join(result.data, "team-seeded"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Delete this file to have the next init.sh seed an empty team/ again.",
		"with the on/off setting and grant it had",
		"data/ is the whole database, every conversation included: deleting it is a full reset",
		"delete its row from agent_profile_settings",
	} {
		if !strings.Contains(strings.Join(strings.Fields(string(text)), " "), want) {
			t.Errorf("receipt does not say %q:\n%s", want, text)
		}
	}
}

// A template that is a symlink is refused: cp would copy whatever it points
// at into team/ as a specialist.
func TestInitRefusesASymlinkedTemplate(t *testing.T) {
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		template := filepath.Join(root, "scripts", teamTemplatesDirectory, "inbox", "AGENT.md")
		elsewhere := filepath.Join(root, "elsewhere.md")
		if err := os.WriteFile(elsewhere, []byte("---\nname: X\ndescription: x\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(template); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(elsewhere, template); err != nil {
			t.Fatal(err)
		}
	})
	if result.err == nil {
		t.Fatal("init.sh seeded from a symlinked template")
	}
	if !strings.Contains(result.output, "scripts/team-templates/inbox/AGENT.md") {
		t.Fatalf("failure did not name the template:\n%s", result.output)
	}
	if got := teamEntries(t, result.team); len(got) != 0 {
		t.Fatalf("team entries = %v, want none", got)
	}
	if _, err := os.Lstat(filepath.Join(result.data, "team-seeded")); !os.IsNotExist(err) {
		t.Fatalf("a refused seed left a receipt (err = %v)", err)
	}
}

// A receipt that is a dangling symlink still counts, so init.sh neither seeds
// nor writes through the link to wherever it points.
func TestInitHonoursADanglingReceiptLink(t *testing.T) {
	var target string
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		data := filepath.Join(root, "data")
		if err := os.MkdirAll(data, 0o700); err != nil {
			t.Fatal(err)
		}
		target = filepath.Join(root, "outside-receipt")
		if err := os.Symlink(target, filepath.Join(data, "team-seeded")); err != nil {
			t.Fatal(err)
		}
	})
	if result.err != nil {
		t.Fatalf("init.sh failed: %v\n%s", result.err, result.output)
	}
	if got := teamEntries(t, result.team); len(got) != 0 {
		t.Fatalf("team entries = %v, want none", got)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("init.sh wrote through the receipt link (err = %v)", err)
	}
}

// So is a template folder that is a symlink, and the templates folder itself:
// a regular AGENT.md reached through either is still not the checkout's.
func TestInitRefusesASymlinkedTemplateFolder(t *testing.T) {
	for name, link := range map[string]func(t *testing.T, templates string){
		"one template folder": func(t *testing.T, templates string) {
			moved := templates + "-inbox"
			if err := os.Rename(filepath.Join(templates, "inbox"), moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(moved, filepath.Join(templates, "inbox")); err != nil {
				t.Fatal(err)
			}
		},
		"the templates folder": func(t *testing.T, templates string) {
			moved := templates + "-moved"
			if err := os.Rename(templates, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(moved, templates); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
				link(t, filepath.Join(root, "scripts", teamTemplatesDirectory))
			})
			if result.err == nil {
				t.Fatal("init.sh seeded through a symlinked template folder")
			}
			if !strings.Contains(result.output, "must not be a symlink") {
				t.Fatalf("failure did not name the symlink:\n%s", result.output)
			}
			if got := teamEntries(t, result.team); len(got) != 0 {
				t.Fatalf("team entries = %v, want none", got)
			}
			if _, err := os.Lstat(filepath.Join(result.data, "team-seeded")); !os.IsNotExist(err) {
				t.Fatalf("a refused seed left a receipt (err = %v)", err)
			}
		})
	}
}
