package proto_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBreakingMaterializesArchiveAndChecksProducerExit(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, exit := range []string{"0", "37"} {
		t.Run("archive_exit_"+exit, func(t *testing.T) {
			repo := newCompatibilityRepo(t, "additive", false)
			bin := t.TempDir()
			bufLog := filepath.Join(t.TempDir(), "buf-called")
			writeTool(t, bin, "git", `#!/bin/sh
if [ "$3" = archive ]; then
  "$REAL_GIT" "$@" || exit $?
  # A reader can close a tar pipe after its end markers, before the producer
  # finishes padding. Require a completed file rather than racing that close.
  if [ -p /dev/fd/1 ]; then exit 73; fi
  exit "$ARCHIVE_EXIT"
fi
exec "$REAL_GIT" "$@"
`)
			writeTool(t, bin, "buf", `#!/bin/sh
if [ "$1" = --version ]; then echo 1.72.0; exit 0; fi
test -r "$4/turing/v1/example.proto" || exit 42
printf called > "$BUF_LOG"
`)
			command := exec.Command(filepath.Join(repo, "tools", "proto", "breaking.sh"), "origin/main")
			command.Dir = repo
			command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"REAL_GIT="+realGit, "ARCHIVE_EXIT="+exit, "BUF_LOG="+bufLog)
			output, err := command.CombinedOutput()
			if exit == "0" {
				if err != nil {
					t.Fatalf("valid completed archive failed: %v\n%s", err, output)
				}
				if _, err := os.Stat(bufLog); err != nil {
					t.Fatalf("compatibility check did not run: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(string(output), "failed to extract protobuf schema") {
					t.Fatalf("archive producer failure was not reported: %v\n%s", err, output)
				}
				if _, err := os.Stat(bufLog); !os.IsNotExist(err) {
					t.Fatal("compatibility check ran after archive producer failure")
				}
			}
		})
	}
}
