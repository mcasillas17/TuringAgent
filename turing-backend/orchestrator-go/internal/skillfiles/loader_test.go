package skillfiles

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestScanUsesRelativeFolderPathAsIdentityAndCapturesReferences(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "writing/tone", `---
name: Calm tone
description: Keeps prose measured and direct
version: "2.1"
author: Ada
license: MIT
requires:
  - files.read
  - files.update
---
Use short sentences.
`)
	referencePath := filepath.Join(root, "writing", "tone", "references", "examples.md")
	if err := os.MkdirAll(filepath.Dir(referencePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(referencePath, []byte("A measured example."), 0o600); err != nil {
		t.Fatal(err)
	}

	skills, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("skills = %+v, want one", skills)
	}
	skill := skills[0]
	if skill.ID != "writing/tone" || skill.Category != "writing" {
		t.Fatalf("identity = (%q, %q), want (writing/tone, writing)", skill.ID, skill.Category)
	}
	if skill.Name != "Calm tone" || skill.Description != "Keeps prose measured and direct" {
		t.Fatalf("metadata = (%q, %q)", skill.Name, skill.Description)
	}
	if skill.Version != "2.1" || skill.Author != "Ada" || skill.License != "MIT" {
		t.Fatalf("optional metadata = (%q, %q, %q)", skill.Version, skill.Author, skill.License)
	}
	if got := strings.Join(skill.Requires, ","); got != "files.read,files.update" {
		t.Fatalf("requires = %q", got)
	}
	if skill.Body != "Use short sentences." {
		t.Fatalf("body = %q", skill.Body)
	}
	if got := skill.References["references/examples.md"]; got != "A measured example." {
		t.Fatalf("reference = %q", got)
	}
}

func TestScanKeepsSameNamedSkillsDistinctByPath(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"writing/tone", "email/tone"} {
		writeTestSkill(t, root, id, "---\nname: Tone\ndescription: Shared display name\n---\nBody for "+id+".\n")
	}

	skills, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("skills = %+v, want two", skills)
	}
	if skills[0].ID != "email/tone" || skills[1].ID != "writing/tone" {
		t.Fatalf("ids = %q, %q", skills[0].ID, skills[1].ID)
	}
}

func TestScanListsMalformedSkillInsteadOfSilentlyDroppingIt(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "writing/broken", "---\nname: Broken\n---\nBody\n")

	skills, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("skills = %+v, want malformed entry", skills)
	}
	if skills[0].ID != "writing/broken" || !strings.Contains(skills[0].ParseError, "description") {
		t.Fatalf("malformed skill = %+v", skills[0])
	}
}

func TestScanRejectsUnknownFrontmatterInsteadOfAcceptingAnIDOverride(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "writing/tone", "---\nname: Tone\ndescription: Direct prose\nid: permanent-tone\n---\nBody\n")

	skills, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || !strings.Contains(skills[0].ParseError, "id") {
		t.Fatalf("skills = %+v, want unknown id field reported", skills)
	}
	if skills[0].ID != "writing/tone" {
		t.Fatalf("id = %q, want path identity", skills[0].ID)
	}
}

func TestScanTreatsMissingRootAsEmpty(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-created")
	skills, err := New(missing).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 {
		t.Fatalf("skills = %+v, want empty", skills)
	}
}

func TestScanTreatsEmptyRootAndFoldersWithoutSkillFilesAsEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "writing", "not-a-skill"), 0o700); err != nil {
		t.Fatal(err)
	}
	skills, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 {
		t.Fatalf("skills = %+v, want empty", skills)
	}
}

func TestScanListsUnreadableSkillWithItsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on Windows")
	}
	root := t.TempDir()
	writeTestSkill(t, root, "writing/locked", "---\nname: Locked\ndescription: Cannot read\n---\nBody\n")
	path := filepath.Join(root, "writing", "locked", "SKILL.md")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	skills, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || !strings.Contains(skills[0].ParseError, "read SKILL.md") {
		t.Fatalf("skills = %+v, want unreadable entry", skills)
	}
}

func TestScanListsOversizedSkillWithoutReadingIt(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "writing/huge", strings.Repeat("x", maxSkillFileBytes+1))

	skills, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || !strings.Contains(skills[0].ParseError, "exceeds") {
		t.Fatalf("skills = %+v, want size error", skills)
	}
}

func TestMisplacedSkillFileIsReportedWithoutHidingValidSkills(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "writing/tone", "---\nname: Tone\ndescription: Valid\n---\nBody\n")
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("hostile"), 0o600); err != nil {
		t.Fatal(err)
	}

	skills, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("skills = %+v, want misplaced entry and valid skill", skills)
	}
	var validFound, placementErrorFound bool
	for _, skill := range skills {
		validFound = validFound || skill.ID == "writing/tone" && skill.ParseError == ""
		placementErrorFound = placementErrorFound || strings.Contains(skill.ParseError, "category and skill folder")
	}
	if !validFound || !placementErrorFound {
		t.Fatalf("skills = %+v", skills)
	}
}

func TestScanReportsSymlinkedReferenceWithoutReadingOutsideTheSkill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	root := t.TempDir()
	writeTestSkill(t, root, "writing/tone", "---\nname: Tone\ndescription: Direct prose\n---\nBody\n")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("must not be read"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "writing", "tone", "references", "escape.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	skills, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || !strings.Contains(strings.ToLower(skills[0].ParseError), "symlink") {
		t.Fatalf("skills = %+v, want symlink error", skills)
	}
	if len(skills[0].References) != 0 {
		t.Fatalf("references = %+v, want no escaped content", skills[0].References)
	}
}

func TestScanIgnoresNonReferenceFilesInTheSkillFolder(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "writing/tone", "---\nname: Tone\ndescription: Direct prose\n---\nBody\n")
	script := filepath.Join(root, "writing", "tone", "scripts", "helper.bin")
	if err := os.MkdirAll(filepath.Dir(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte{0xff, 0xfe, 0xfd}, 0o700); err != nil {
		t.Fatal(err)
	}

	skills, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].ParseError != "" {
		t.Fatalf("skills = %+v, want valid skill", skills)
	}
	if len(skills[0].References) != 0 {
		t.Fatalf("references = %+v, want scripts excluded", skills[0].References)
	}
}

func TestReadRejectsFileSwappedForSymlinkAfterInspection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("O_NOFOLLOW is a Unix filesystem guarantee")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "SKILL.md")
	if err := os.WriteFile(path, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	inspected, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("outside secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}

	data, err := readBoundedRegularFileAfterInspect(path, 1024, inspected)
	if err == nil {
		t.Fatalf("read swapped symlink = %q, want rejection", data)
	}
	if strings.Contains(string(data), "outside secret") {
		t.Fatalf("outside content escaped through swapped symlink: %q", data)
	}
}

func TestReadRejectsParentDirectorySwappedForSymlinkAfterDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("O_NOFOLLOW is a Unix filesystem guarantee")
	}
	root := t.TempDir()
	path := filepath.Join(root, "writing", "tone", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	inspected, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "SKILL.md"), []byte("outside secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	tone := filepath.Join(root, "writing", "tone")
	parked := filepath.Join(root, "writing", "tone-original")
	if err := os.Rename(tone, parked); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, tone); err != nil {
		t.Fatal(err)
	}

	data, err := readBoundedRegularFileWithinRootAfterInspect(root, path, 1024, inspected)
	if err == nil {
		t.Fatalf("read through swapped parent = %q, want rejection", data)
	}
	if strings.Contains(string(data), "outside secret") {
		t.Fatalf("outside content escaped through swapped parent: %q", data)
	}
}

func TestUnreadableSubtreeDoesNotHideValidSkill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on Windows")
	}
	root := t.TempDir()
	writeTestSkill(t, root, "writing/tone", "---\nname: Tone\ndescription: Valid\n---\nBody\n")
	blocked := filepath.Join(root, "broken", "blocked")
	if err := os.MkdirAll(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })

	skills, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	var valid bool
	for _, skill := range skills {
		valid = valid || skill.ID == "writing/tone" && skill.ParseError == ""
	}
	if !valid {
		t.Fatalf("skills = %+v, want valid skill retained", skills)
	}
}

func writeTestSkill(t *testing.T, root, id, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(id), "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
