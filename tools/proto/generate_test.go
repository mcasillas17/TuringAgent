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

func TestGenerateExecutesWindowsPubCacheShimViaPATH(t *testing.T) {
	binDir := t.TempDir()
	protocLog := filepath.Join(t.TempDir(), "protoc.log")
	dartPluginLog := filepath.Join(t.TempDir(), "dart-plugin.log")
	writeTool(t, binDir, "protoc", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo 'libprotoc 34.1'\n  exit 0\nfi\nprintf '%s\\n' \"$*\" >> \"$PROTO_LOG\"\ncase \"$*\" in\n  *--dart_out=*)\n    case \"$*\" in *--plugin=protoc-gen-dart=*) exit 41 ;; esac\n    old_ifs=$IFS\n    IFS=:\n    for directory in $PATH; do\n      candidate=\"$directory/protoc-gen-dart.bat\"\n      if [ -f \"$candidate\" ]; then\n        IFS=$old_ifs\n        /bin/sh \"$candidate\"\n        exit $?\n      fi\n    done\n    IFS=$old_ifs\n    exit 42\n    ;;\nesac\n")
	writeTool(t, binDir, "protoc-gen-go", "#!/bin/sh\nexit 0\n")
	writeTool(t, binDir, "protoc-gen-go-grpc", "#!/bin/sh\nexit 0\n")
	writeTool(t, binDir, "dart", "#!/bin/sh\nif [ \"$1 $2 $3\" = 'pub global list' ]; then echo 'protoc_plugin 22.5.0'; fi\n")

	localAppData := t.TempDir()
	pubCache := filepath.Join(localAppData, "Pub", "Cache")
	plugin := filepath.Join(pubCache, "bin", "protoc-gen-dart.bat")
	if err := os.MkdirAll(filepath.Dir(plugin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plugin, []byte("#!/bin/sh\nprintf 'invoked\\n' > \"$DART_PLUGIN_LOG\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("./generate.sh")
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+":/usr/bin:/bin",
		"PUB_CACHE=",
		"OS=Windows_NT",
		"LOCALAPPDATA="+localAppData,
		"PROTO_LOG="+protocLog,
		"DART_PLUGIN_LOG="+dartPluginLog,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate.sh: %v\n%s", err, output)
	}
	logData, err := os.ReadFile(protocLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logData), "--plugin=protoc-gen-dart=") {
		t.Fatalf("Windows protoc call used an explicit batch-file mapping: %q", logData)
	}
	pluginData, err := os.ReadFile(dartPluginLog)
	if err != nil {
		t.Fatalf("read Dart plugin execution log: %v", err)
	}
	if got := strings.TrimSpace(string(pluginData)); got != "invoked" {
		t.Fatalf("Dart plugin execution log = %q, want invoked", got)
	}
}

func TestGenerateRejectsWindowsDrivePathWithoutCygpath(t *testing.T) {
	binDir := t.TempDir()
	writeTool(t, binDir, "dirname", "#!/bin/sh\nexec /usr/bin/dirname \"$@\"\n")
	writeTool(t, binDir, "protoc", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'libprotoc 34.1'; fi\n")
	writeTool(t, binDir, "protoc-gen-go", "#!/bin/sh\nexit 0\n")
	writeTool(t, binDir, "protoc-gen-go-grpc", "#!/bin/sh\nexit 0\n")
	writeTool(t, binDir, "dart", "#!/bin/sh\nexit 0\n")

	cmd := exec.Command("bash", "./generate.sh")
	cmd.Env = append(os.Environ(),
		"PATH="+binDir,
		"PUB_CACHE=",
		"OS=Windows_NT",
		`LOCALAPPDATA=C:\Users\developer\AppData\Local`,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("generate.sh succeeded with an unresolvable Windows drive path")
	}
	want := "requires cygpath; install Git for Windows or set PUB_CACHE to a POSIX-style absolute path"
	if !strings.Contains(string(output), want) {
		t.Fatalf("generate.sh output = %q, want it to contain %q", output, want)
	}
}

func TestGenerateExcludesUnpinnedOptionalGenerators(t *testing.T) {
	data, err := os.ReadFile("generate.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--swift_out", "--csharp_out", "--java_out", "--grpc-swift_out", "--grpc-java_out"} {
		if strings.Contains(string(data), flag) {
			t.Fatalf("canonical generation contains unpinned optional flag %q", flag)
		}
	}
}

func writeTool(t *testing.T, dir, name, contents string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
