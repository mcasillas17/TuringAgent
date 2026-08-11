package proto_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateRejectsUnsupportedProtocVersion(t *testing.T) {
	binDir := t.TempDir()
	writeTool(t, binDir, "protoc", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'libprotoc 99.0'; fi\n")
	writeTool(t, binDir, "protoc-gen-go", "#!/bin/sh\nexit 0\n")
	writeTool(t, binDir, "protoc-gen-go-grpc", "#!/bin/sh\nexit 0\n")
	writeTool(t, binDir, "dart", "#!/bin/sh\nexit 0\n")

	cmd := exec.Command("./generate.sh")
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+":/usr/bin:/bin",
		"PUB_CACHE="+filepath.Join(t.TempDir(), "pub-cache"),
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("generate.sh succeeded with unsupported protoc version")
	}
	want := "protoc 34.1 is required (found: libprotoc 99.0)"
	if !strings.Contains(string(output), want) {
		t.Fatalf("generate.sh output = %q, want it to contain %q", output, want)
	}
}

func writeTool(t *testing.T, dir, name, contents string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
