package teamfiles

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"
)

const researchProfile = `---
name: Research
emoji: "🔬"
description: Researches topics against local knowledge and reports findings.
version: 1
model: ""
tools:
  - system.time
  - memory.search
  - memory.read
  - files.*
  - skills_list
  - skill_view
  - files.*
skills: []
memory: read
requires: []
max_tool_calls: 12
---
You are Research, a specialist working for Turing.

`

func writeProfile(t *testing.T, root, id, content string) string {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "AGENT.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func scanOne(t *testing.T, content string) Profile {
	t.Helper()
	root := t.TempDir()
	writeProfile(t, root, "research", content)
	profiles, err := New(root).Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("Scan() = %d profiles, want 1", len(profiles))
	}
	return profiles[0]
}

func TestScanParsesTheSpecExampleProfile(t *testing.T) {
	profile := scanOne(t, researchProfile)
	if profile.ParseError != "" {
		t.Fatalf("ParseError = %q", profile.ParseError)
	}
	if profile.ID != "research" || profile.Name != "Research" || profile.Emoji != "🔬" {
		t.Fatalf("identity = %q %q %q", profile.ID, profile.Name, profile.Emoji)
	}
	if profile.Description != "Researches topics against local knowledge and reports findings." {
		t.Fatalf("Description = %q", profile.Description)
	}
	// The spec writes version as a bare integer; it is a display string.
	if profile.Version != "1" {
		t.Fatalf("Version = %q, want 1", profile.Version)
	}
	wantTools := []string{"files.*", "memory.read", "memory.search", "skill_view", "skills_list", "system.time"}
	if !slices.Equal(profile.Tools, wantTools) {
		t.Fatalf("Tools = %v, want %v (sorted, deduplicated)", profile.Tools, wantTools)
	}
	if profile.Memory != MemoryRead || profile.MaxToolCalls != 12 || profile.Model != "" {
		t.Fatalf("authority = memory %q max %d model %q", profile.Memory, profile.MaxToolCalls, profile.Model)
	}
	if len(profile.Skills) != 0 || len(profile.Requires) != 0 {
		t.Fatalf("Skills = %v Requires = %v, want empty", profile.Skills, profile.Requires)
	}
	if profile.Instructions != "You are Research, a specialist working for Turing." {
		t.Fatalf("Instructions = %q", profile.Instructions)
	}
	if len(profile.Revision) != 64 {
		t.Fatalf("Revision = %q, want a sha256 hex digest", profile.Revision)
	}
}

func TestScanTreatsAMissingRootAsAnEmptyTeam(t *testing.T) {
	profiles, err := New(filepath.Join(t.TempDir(), "absent")).Scan()
	if err != nil || profiles != nil {
		t.Fatalf("Scan() = %v, %v; want nil, nil", profiles, err)
	}
}

func TestScanRefusesASymlinkedOrNonDirectoryRoot(t *testing.T) {
	real := t.TempDir()
	writeProfile(t, real, "research", researchProfile)
	link := filepath.Join(t.TempDir(), "team")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := New(link).Scan(); err == nil {
		t.Fatal("Scan() followed a symlinked team root")
	}
	file := filepath.Join(t.TempDir(), "team")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(file).Scan(); err == nil {
		t.Fatal("Scan() accepted a file as the team root")
	}
}

func TestScanSkipsHiddenEntriesFilesAndFoldersWithoutAnAgentFile(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root, "research", researchProfile)
	writeProfile(t, root, ".hidden", researchProfile)
	if err := os.WriteFile(filepath.Join(root, ".gitkeep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "drafts"), 0o700); err != nil {
		t.Fatal(err)
	}
	profiles, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].ID != "research" {
		t.Fatalf("Scan() = %+v, want only research", profiles)
	}
}

func TestScanReportsEachBadProfileWithoutHidingTheOthers(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root, "research", researchProfile)
	writeProfile(t, root, "broken", "---\nname: [unclosed\n---\nbody\n")
	writeProfile(t, root, "typo", "---\nname: Typo\ndescription: d\ntool:\n  - files.read\n---\n")
	profiles, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		ids = append(ids, profile.ID)
	}
	if !slices.Equal(ids, []string{"broken", "research", "typo"}) {
		t.Fatalf("ids = %v, want sorted broken, research, typo", ids)
	}
	if profiles[0].ParseError == "" {
		t.Fatal("malformed YAML produced no parse error")
	}
	if profiles[1].ParseError != "" {
		t.Fatalf("a bad neighbour broke research: %q", profiles[1].ParseError)
	}
	// An unknown key would otherwise grant nothing silently.
	if !strings.Contains(profiles[2].ParseError, "tool") {
		t.Fatalf("unknown key error = %q, want it to name the key", profiles[2].ParseError)
	}
	if profiles[2].Revision != "" {
		t.Fatal("a profile that failed to parse carries a revision that could be granted")
	}
}

func TestScanRejectsInvalidAndReservedProfileIDs(t *testing.T) {
	for _, id := range []string{"turing", "Research", "x", "1dev", "dev_ops", strings.Repeat("a", 33)} {
		t.Run(id, func(t *testing.T) {
			root := t.TempDir()
			writeProfile(t, root, id, researchProfile)
			profiles, err := New(root).Scan()
			if err != nil {
				t.Fatal(err)
			}
			if len(profiles) != 1 || profiles[0].ParseError == "" {
				t.Fatalf("Scan() = %+v, want one profile with a parse error", profiles)
			}
			if profiles[0].ID != id {
				t.Fatalf("ID = %q, want the folder name %q so the error is attributable", profiles[0].ID, id)
			}
			if profiles[0].Revision != "" {
				t.Fatal("a profile with an invalid ID carries a grantable revision")
			}
		})
	}
	root := t.TempDir()
	writeProfile(t, root, "ab", researchProfile)
	writeProfile(t, root, "a-"+strings.Repeat("b", 30), researchProfile)
	profiles, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Fatalf("Scan() = %d profiles, want 2", len(profiles))
	}
	for _, profile := range profiles {
		if profile.ParseError != "" {
			t.Fatalf("valid ID %q rejected: %s", profile.ID, profile.ParseError)
		}
	}
}

func TestScanRefusesSymlinksAndNonRegularAgentFiles(t *testing.T) {
	outside := t.TempDir()
	outsideFile := writeProfile(t, outside, "elsewhere", researchProfile)

	t.Run("symlinked profile folder", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Symlink(filepath.Dir(outsideFile), filepath.Join(root, "research")); err != nil {
			t.Fatal(err)
		}
		assertSingleParseError(t, root, "research")
	})
	t.Run("AGENT.md symlink out of the root", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "research"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outsideFile, filepath.Join(root, "research", "AGENT.md")); err != nil {
			t.Fatal(err)
		}
		assertSingleParseError(t, root, "research")
	})
	t.Run("AGENT.md symlink inside the root", func(t *testing.T) {
		root := t.TempDir()
		target := writeProfile(t, root, "dev", researchProfile)
		if err := os.Mkdir(filepath.Join(root, "research"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, "research", "AGENT.md")); err != nil {
			t.Fatal(err)
		}
		profiles, err := New(root).Scan()
		if err != nil {
			t.Fatal(err)
		}
		if len(profiles) != 2 || profiles[1].ID != "research" || profiles[1].ParseError == "" {
			t.Fatalf("Scan() = %+v, want research refused", profiles)
		}
	})
	t.Run("AGENT.md is a directory", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "research", "AGENT.md"), 0o700); err != nil {
			t.Fatal(err)
		}
		assertSingleParseError(t, root, "research")
	})
}

func assertSingleParseError(t *testing.T, root, id string) {
	t.Helper()
	profiles, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].ID != id || profiles[0].ParseError == "" {
		t.Fatalf("Scan() = %+v, want %s with a parse error", profiles, id)
	}
	if profiles[0].Name == "Research" {
		t.Fatal("content was read through a refused path")
	}
}

func TestScanRefusesMalformedFiles(t *testing.T) {
	cases := map[string]string{
		"oversized":             "---\nname: R\ndescription: d\n---\n" + strings.Repeat("x", maxAgentFileBytes),
		"not UTF-8":             "---\nname: R\ndescription: d\n---\n\xff\xfe",
		"no frontmatter":        "name: R\n",
		"unclosed frontmatter":  "---\nname: R\ndescription: d\n",
		"two documents":         "---\nname: R\ndescription: d\n--- \nname: S\n---\n",
		"missing name":          "---\ndescription: d\n---\n",
		"missing description":   "---\nname: R\n---\n",
		"long name":             "---\nname: " + strings.Repeat("n", 121) + "\ndescription: d\n---\n",
		"long description":      "---\nname: R\ndescription: " + strings.Repeat("d", 501) + "\n---\n",
		"long emoji":            "---\nname: R\ndescription: d\nemoji: " + strings.Repeat("e", 17) + "\n---\n",
		"unknown memory":        "---\nname: R\ndescription: d\nmemory: write\n---\n",
		"negative tool calls":   "---\nname: R\ndescription: d\nmax_tool_calls: -1\n---\n",
		"tool calls past int32": "---\nname: R\ndescription: d\nmax_tool_calls: 2147483648\n---\n",
		"fractional tool calls": "---\nname: R\ndescription: d\nmax_tool_calls: 12.5\n---\n",
		"binary name":           "---\nname: !!binary /w==\ndescription: d\n---\n",
		"binary tool":           "---\nname: R\ndescription: d\ntools: [!!binary /w==]\n---\n",
		"binary requires":       "---\nname: R\ndescription: d\nrequires: [!!binary /w==]\n---\n",
		"negative fraction":     "---\nname: R\ndescription: d\nmax_tool_calls: -0.5\n---\n",
		"float tool calls":      "---\nname: R\ndescription: d\nmax_tool_calls: 12.0\n---\n",
		"quoted tool calls":     "---\nname: R\ndescription: d\nmax_tool_calls: '12'\n---\n",
		"bare star tool":        "---\nname: R\ndescription: d\ntools: ['*']\n---\n",
		"inner star tool":       "---\nname: R\ndescription: d\ntools: ['fi*les']\n---\n",
		"empty tool":            "---\nname: R\ndescription: d\ntools: ['  ']\n---\n",
		"spaced tool":           "---\nname: R\ndescription: d\ntools: ['files read']\n---\n",
		"control char requires": "---\nname: R\ndescription: d\nrequires: [\"gmail.\\a\"]\n---\n",
		"bare star skill":       "---\nname: R\ndescription: d\nskills: ['*']\n---\n",
		"long pattern":          "---\nname: R\ndescription: d\ntools: ['" + strings.Repeat("t", 129) + "']\n---\n",
		"spaced model":          "---\nname: R\ndescription: d\nmodel: qwen 2.5\n---\n",
		"long model":            "---\nname: R\ndescription: d\nmodel: " + strings.Repeat("m", 129) + "\n---\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			profile := scanOne(t, content)
			if profile.ParseError == "" {
				t.Fatalf("accepted %s: %+v", name, profile)
			}
			if profile.Revision != "" {
				t.Fatalf("refused profile carries revision %q", profile.Revision)
			}
		})
	}
}

func TestScanDefaultsAbsentMemoryToNone(t *testing.T) {
	profile := scanOne(t, "---\nname: R\ndescription: d\n---\n")
	if profile.ParseError != "" || profile.Memory != MemoryNone {
		t.Fatalf("Memory = %q err = %q, want none", profile.Memory, profile.ParseError)
	}
	explicit := scanOne(t, "---\nname: R\ndescription: d\nmemory: none\n---\n")
	if explicit.Revision != profile.Revision {
		t.Fatal("an absent memory key and memory: none produced different revisions")
	}
}

func TestRevisionCoversAuthorityFieldsOnly(t *testing.T) {
	base := scanOne(t, researchProfile).Revision
	authority := map[string][2]string{
		"tools":          {"  - system.time\n", "  - system.*\n"},
		"skills":         {"skills: []", "skills: ['research/*']"},
		"memory":         {"memory: read", "memory: propose"},
		"model":          {`model: ""`, "model: llama3.1:8b"},
		"requires":       {"requires: []", "requires: ['files.read']"},
		"max_tool_calls": {"max_tool_calls: 12", "max_tool_calls: 4"},
	}
	for field, edit := range authority {
		edited := strings.Replace(researchProfile, edit[0], edit[1], 1)
		if edited == researchProfile {
			t.Fatalf("%s edit did not apply", field)
		}
		got := scanOne(t, edited)
		if got.ParseError != "" {
			t.Fatalf("%s edit failed to parse: %s", field, got.ParseError)
		}
		if got.Revision == base {
			t.Fatalf("editing %s left the revision unchanged, so a stale grant would still apply", field)
		}
	}
	display := map[string][2]string{
		"name":         {"name: Research", "name: Researcher"},
		"emoji":        {`emoji: "🔬"`, `emoji: "📚"`},
		"description":  {"description: Researches", "description: Studies"},
		"version":      {"version: 1", "version: 2"},
		"instructions": {"working for Turing.", "working for Turing. Be brief."},
		"tool order":   {"  - system.time\n  - memory.search\n", "  - memory.search\n  - system.time\n"},
	}
	for field, edit := range display {
		edited := strings.Replace(researchProfile, edit[0], edit[1], 1)
		if edited == researchProfile {
			t.Fatalf("%s edit did not apply", field)
		}
		if got := scanOne(t, edited); got.Revision != base {
			t.Fatalf("editing %s changed the revision; only authority fields may", field)
		}
	}
}

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"files.read", "files.read", true},
		{"files.read", "files.reader", false},
		{"files.*", "files.read", true},
		{"files.*", "file.read", false},
		{"github.*", "github.get_issue", true},
		{"research/*", "research/papers", true},
	}
	for _, tc := range cases {
		if got := MatchPattern(tc.pattern, tc.name); got != tc.want {
			t.Errorf("MatchPattern(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

// The wire field is int32, and the hash covers the parsed value: a larger
// limit would be granted under one number and shown in review as another.
func TestScanAcceptsTheLargestToolCallLimitTheWireCarries(t *testing.T) {
	profile := scanOne(t, "---\nname: R\ndescription: d\nmax_tool_calls: 2147483647\n---\n")
	if profile.ParseError != "" || profile.MaxToolCalls != math.MaxInt32 {
		t.Fatalf("max_tool_calls = %d, parse error %q", profile.MaxToolCalls, profile.ParseError)
	}
}

// A profile saved with Windows line endings, or checked out under autocrlf,
// is the same profile and keeps its grant.
func TestScanReadsCRLFProfilesAsTheSameProfile(t *testing.T) {
	lf := scanOne(t, researchProfile)
	crlf := scanOne(t, strings.ReplaceAll(researchProfile, "\n", "\r\n"))
	if crlf.ParseError != "" {
		t.Fatalf("CRLF profile did not parse: %s", crlf.ParseError)
	}
	if crlf.Revision != lf.Revision || crlf.Instructions != lf.Instructions {
		t.Fatalf("CRLF revision %q instructions %q, want %q %q", crlf.Revision, crlf.Instructions, lf.Revision, lf.Instructions)
	}
}

func TestScanAcceptsFrontmatterClosedAtTheEndOfTheFile(t *testing.T) {
	profile := scanOne(t, "---\nname: R\ndescription: d\n---")
	if profile.ParseError != "" || profile.Instructions != "" || profile.Name != "R" {
		t.Fatalf("profile = %+v", profile)
	}
}

// A name that is not UTF-8 cannot travel in a protobuf string, and one such
// entry would fail the whole list. It is quoted instead, which keeps names
// that differ in their bytes apart. Linux allows such names; APFS refuses
// them, so the test skips there.
func TestScanQuotesFolderNamesThatAreNotUTF8(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root, "research", researchProfile)
	folder := "caf\xe9"
	if err := os.Mkdir(filepath.Join(root, folder), 0o700); err != nil {
		t.Skipf("this filesystem refuses names that are not UTF-8: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, folder, "AGENT.md"), []byte(researchProfile), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "research"), filepath.Join(root, "link\xff")); err != nil {
		t.Fatal(err)
	}

	profiles, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 3 {
		t.Fatalf("profiles = %+v, want the valid one and both bad names", profiles)
	}
	for _, profile := range profiles {
		if !utf8.ValidString(profile.ID) || !utf8.ValidString(profile.ParseError) {
			t.Fatalf("profile %q carries text that is not UTF-8: %+v", profile.ID, profile)
		}
		if profile.ID != "research" && profile.ParseError == "" {
			t.Fatalf("a name that is not UTF-8 loaded as a profile: %+v", profile)
		}
	}
}

func TestDisplayIDQuotesOnlyNamesThatAreNotUTF8(t *testing.T) {
	cases := map[string]string{
		"research":    "research",
		"café":        "café",
		"caf\xe9":     `"caf\xe9"`,
		"caf\xe9\xe9": `"caf\xe9\xe9"`,
	}
	for raw, want := range cases {
		if got := displayID(raw); got != want || !utf8.ValidString(got) {
			t.Errorf("displayID(%q) = %q, want %q", raw, got, want)
		}
	}
}

// The UI shows tools as server/tool, but a pattern names the tool alone. A
// copied qualified name would otherwise match nothing and look like a broken
// specialist, so it is refused with the form to write instead.
func TestScanRefusesServerQualifiedToolPatterns(t *testing.T) {
	for field, content := range map[string]string{
		"tools":    "---\nname: R\ndescription: d\ntools: [files/files.read]\n---\n",
		"requires": "---\nname: R\ndescription: d\ntools: [files.*]\nrequires: [files/files.read]\n---\n",
	} {
		t.Run(field, func(t *testing.T) {
			profile := scanOne(t, content)
			if !strings.Contains(profile.ParseError, "write files.read") {
				t.Fatalf("parse error = %q, want it to name the tool-only form", profile.ParseError)
			}
		})
	}
	if profile := scanOne(t, "---\nname: R\ndescription: d\nskills: [research/*]\n---\n"); profile.ParseError != "" {
		t.Fatalf("a skill folder pattern was refused: %s", profile.ParseError)
	}
}

// withoutPath drops the container path an fs.PathError carries from lstat,
// stat or read, and leaves a pathless error unchanged.
func TestWithoutPathKeepsOnlyTheCause(t *testing.T) {
	err := withoutPath(&fs.PathError{Op: "read", Path: "/team/custom/AGENT.md", Err: syscall.EIO})
	if err != syscall.EIO {
		t.Fatalf("withoutPath = %v, want the bare cause", err)
	}
	plain := errors.New("file changed while it was being opened")
	if withoutPath(plain) != plain {
		t.Fatal("withoutPath changed an error that carries no path")
	}
}

// A folder the loader cannot look inside is still listed, as a profile it
// cannot read, so it is never silently missing. init.sh relies on this.
func TestScanListsAFolderItCannotOpenAsAParseError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any folder")
	}
	root := t.TempDir()
	writeProfile(t, root, "custom", researchProfile)
	custom := filepath.Join(root, "custom")
	if err := os.Chmod(custom, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(custom, 0o700) })

	profiles, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].ID != "custom" || profiles[0].ParseError == "" || profiles[0].Revision != "" {
		t.Fatalf("profiles = %+v, want custom listed as a parse error", profiles)
	}
	// The Agents page shows this text as written, so it must not name the
	// container mount the desktop user cannot see.
	if strings.Contains(profiles[0].ParseError, root) || !strings.Contains(profiles[0].ParseError, "permission denied") {
		t.Fatalf("ParseError = %q, want the cause without the path under %s", profiles[0].ParseError, root)
	}
}
