package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRequiresTheBackendDirectoryFirstAndAbsolute(t *testing.T) {
	for name, args := range map[string][]string{
		"no arguments":     nil,
		"only a command":   {"help"},
		"flag not first":   {"help", "--backend-dir", "/srv/turing-backend"},
		"no directory":     {"--backend-dir"},
		"relative path":    {"--backend-dir", "turing-backend", "help"},
		"empty path":       {"--backend-dir", "", "help"},
		"another flag":     {"--other", "/srv/turing-backend", "help"},
		"wrong spelling":   {"-backend-dir", "/srv/turing-backend", "help"},
		"directory in two": {"--backend-dir=/srv/turing-backend", "help"},
	} {
		t.Run(name, func(t *testing.T) {
			var stderr bytes.Buffer
			if code := run(args, &stderr); code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			got := stderr.String()
			if !strings.Contains(got, "--backend-dir") || !strings.Contains(got, "Usage:") {
				t.Fatalf("stderr = %q, want the requirement and the usage", got)
			}
		})
	}
}
