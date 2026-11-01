package machine

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const mib = uint64(1) << 20

// macReader answers the commands Gather runs on macOS from a table, the way
// `uname -m` and `sysctl -n` print them. A command missing from the table
// fails, as when the tool is absent.
func macReader(outputs map[string]string) Reader {
	return Reader{
		GOOS:   "darwin",
		GOARCH: "arm64",
		Output: func(name string, args ...string) (string, error) {
			out, ok := outputs[strings.Join(append([]string{name}, args...), " ")]
			if !ok {
				return "", errors.New("exec: " + name + ": executable file not found in $PATH")
			}
			return out, nil
		},
	}
}

func macOutputs(arch string, memBytes uint64, brand string) map[string]string {
	return map[string]string{
		"uname -m":                           arch + "\n",
		"sysctl -n hw.memsize":               strconv.FormatUint(memBytes, 10) + "\n",
		"sysctl -n machdep.cpu.brand_string": brand + "\n",
	}
}

// fixtureReader reads a Linux machine laid out under testdata/<name>, where
// /proc and /sys are the directories of that name.
func fixtureReader(t *testing.T, name string) Reader {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return Reader{
		GOOS:   "linux",
		GOARCH: "amd64",
		ReadFile: func(path string) ([]byte, error) {
			return os.ReadFile(filepath.Join(root, path))
		},
		Glob: func(pattern string) ([]string, error) {
			matches, err := filepath.Glob(filepath.Join(root, pattern))
			for i, match := range matches {
				relative, relErr := filepath.Rel(root, match)
				if relErr != nil {
					t.Fatal(relErr)
				}
				matches[i] = "/" + filepath.ToSlash(relative)
			}
			return matches, err
		},
		Output: func(name string, _ ...string) (string, error) {
			t.Errorf("a Linux read ran %s; it should read files only", name)
			return "", errors.New("no commands in this fixture")
		},
	}
}

// filesReader is a Linux machine made of the files named here.
func filesReader(files map[string]string) Reader {
	return Reader{
		GOOS:   "linux",
		GOARCH: "amd64",
		ReadFile: func(path string) ([]byte, error) {
			content, ok := files[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return []byte(content), nil
		},
		Glob: func(pattern string) ([]string, error) {
			var matches []string
			for path := range files {
				if ok, err := filepath.Match(pattern, path); err == nil && ok {
					matches = append(matches, path)
				}
			}
			slices.Sort(matches)
			return matches, nil
		},
	}
}

func hasProblem(f Facts, fragment string) bool {
	return slices.ContainsFunc(f.Problems, func(problem string) bool { return strings.Contains(problem, fragment) })
}

func TestGatherOnAMac(t *testing.T) {
	tests := []struct {
		name         string
		outputs      map[string]string
		wantMemory   uint64
		wantArch     string
		wantCPU      string
		wantApple    bool
		wantProblems int
	}{
		{name: "an M4 with 24 GiB", outputs: macOutputs("arm64", 24*gib, "Apple M4"), wantMemory: 24 * gib, wantArch: "arm64", wantCPU: "Apple M4", wantApple: true},
		{name: "an M4 Pro with 64 GiB", outputs: macOutputs("arm64", 64*gib, "Apple M4 Pro"), wantMemory: 64 * gib, wantArch: "arm64", wantCPU: "Apple M4 Pro", wantApple: true},
		{name: "an M3 Ultra with 128 GiB", outputs: macOutputs("arm64", 128*gib, "Apple M3 Ultra"), wantMemory: 128 * gib, wantArch: "arm64", wantCPU: "Apple M3 Ultra", wantApple: true},
		{name: "an Intel Mac", outputs: macOutputs("x86_64", 32*gib, "Intel(R) Core(TM) i9-9980HK CPU @ 2.40GHz"), wantMemory: 32 * gib, wantArch: "x86_64", wantCPU: "Intel(R) Core(TM) i9-9980HK CPU @ 2.40GHz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Gather(macReader(tt.outputs))

			if got.OS != "darwin" || got.Arch != tt.wantArch || got.CPU != tt.wantCPU || got.MemoryBytes != tt.wantMemory {
				t.Fatalf("facts = %+v, want darwin, %s, %q, %d bytes", got, tt.wantArch, tt.wantCPU, tt.wantMemory)
			}
			if got.AppleSilicon != tt.wantApple || got.StrixHalo || len(got.GPUs) != 0 {
				t.Fatalf("facts = %+v, want AppleSilicon=%v, not Strix Halo, no GPUs", got, tt.wantApple)
			}
			if len(got.Problems) != 0 {
				t.Fatalf("problems = %q, want none", got.Problems)
			}
		})
	}
}

func TestGatherOnAMacFallsBackToGOARCHWhenUnameFails(t *testing.T) {
	outputs := macOutputs("arm64", 24*gib, "Apple M4")
	delete(outputs, "uname -m")

	got := Gather(macReader(outputs))

	if got.Arch != "arm64" || !got.AppleSilicon || len(got.Problems) != 0 {
		t.Fatalf("facts = %+v, want arm64 from GOARCH, Apple Silicon, and no problem", got)
	}
}

func TestGatherOnAMacRecordsWhatItCouldNotReadAndStillAnswers(t *testing.T) {
	got := Gather(macReader(map[string]string{"uname -m": "arm64\n"}))

	if got.MemoryBytes != 0 || got.CPU != "" || !got.AppleSilicon {
		t.Fatalf("facts = %+v, want no memory and no CPU, but Apple Silicon from uname", got)
	}
	if !hasProblem(got, "memory") || !hasProblem(got, "CPU") {
		t.Fatalf("problems = %q, want one for the memory and one for the CPU", got.Problems)
	}
}

func TestGatherOnAMacRecordsAMemorySizeThatIsNotANumber(t *testing.T) {
	outputs := macOutputs("arm64", 0, "Apple M4")
	outputs["sysctl -n hw.memsize"] = "lots\n"

	got := Gather(macReader(outputs))

	if got.MemoryBytes != 0 || !hasProblem(got, "memory") {
		t.Fatalf("facts = %+v, want memory 0 and a problem naming the memory", got)
	}
}

func TestGatherOnAFrameworkDesktopAddsTheGPUCarveOutToMemTotal(t *testing.T) {
	got := Gather(fixtureReader(t, "framework-desktop"))

	if got.OS != "linux" || got.Arch != "amd64" || !got.StrixHalo || got.AppleSilicon {
		t.Fatalf("facts = %+v, want linux, amd64, Strix Halo", got)
	}
	if got.CPU != "AMD RYZEN AI MAX+ 395 w/ Radeon 8060S" {
		t.Fatalf("CPU = %q", got.CPU)
	}
	// The BIOS moved 96 GiB out of MemTotal, which says 31 GiB.
	if want := 31*gib + 96*gib; got.MemoryBytes != want {
		t.Fatalf("memory = %d, want %d (31 GiB MemTotal plus the 96 GiB carve-out)", got.MemoryBytes, want)
	}
	if !slices.Equal(got.GPUs, []string{"amd"}) || len(got.Problems) != 0 {
		t.Fatalf("GPUs = %q, problems = %q", got.GPUs, got.Problems)
	}
	if rec := Recommend(got); rec.Engine != "openai" || rec.Backend != "ROCm" {
		t.Fatalf("recommendation = %+v, want ds4 on ROCm for the 96 GB+ class", rec)
	}
}

func TestGatherOnAFrameworkDesktopWithASmallCarveOutAddsItAll(t *testing.T) {
	got := Gather(fixtureReader(t, "framework-desktop-small-carveout"))

	if !got.StrixHalo || got.CPU != "AMD Ryzen AI Max+ 395 w/ Radeon 8060S" {
		t.Fatalf("facts = %+v, want Strix Halo from a mixed-case CPU name", got)
	}
	if want := 123*gib + 512*mib; got.MemoryBytes != want {
		t.Fatalf("memory = %d, want %d (123 GiB MemTotal plus 512 MiB)", got.MemoryBytes, want)
	}
	if rec := Recommend(got); rec.Engine != "openai" || rec.Backend != "ROCm" {
		t.Fatalf("recommendation = %+v, want ds4 on ROCm", rec)
	}
}

func TestGatherOnALinuxBoxWithAnNVIDIAGPU(t *testing.T) {
	got := Gather(fixtureReader(t, "nvidia-128gib"))

	if got.StrixHalo || got.MemoryBytes != 128*gib || !slices.Equal(got.GPUs, []string{"nvidia"}) || len(got.Problems) != 0 {
		t.Fatalf("facts = %+v, want a plain 128 GiB box with an NVIDIA GPU and no problem", got)
	}
}

func TestGatherOnALinuxBoxWithNoDRMDirectoryHasNoGPUsAndNoProblem(t *testing.T) {
	got := Gather(fixtureReader(t, "no-drm"))

	if got.MemoryBytes != 128*gib || len(got.GPUs) != 0 || got.StrixHalo || len(got.Problems) != 0 {
		t.Fatalf("facts = %+v, want 128 GiB, no GPUs and no problem", got)
	}
	if got.CPU != "Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz" {
		t.Fatalf("CPU = %q", got.CPU)
	}
}

func TestGatherWithAnUnreadableMemInfoRecordsAProblemAndGivesMemoryZero(t *testing.T) {
	got := Gather(fixtureReader(t, "no-meminfo"))

	if got.MemoryBytes != 0 || !hasProblem(got, "memory") {
		t.Fatalf("facts = %+v, want memory 0 and a problem naming the memory", got)
	}
	if !slices.Equal(got.GPUs, []string{"intel"}) || got.CPU != "Intel(R) Core(TM) i7-1165G7 @ 2.80GHz" {
		t.Fatalf("facts = %+v, want the GPU and CPU read anyway", got)
	}
	if rec := Recommend(got); rec.Engine != "ollama" {
		t.Fatalf("recommendation = %+v, want Ollama when the memory is unknown", rec)
	}
}

func TestGatherReadsEachKnownVendorOnceAndIgnoresTheRest(t *testing.T) {
	got := Gather(filesReader(map[string]string{
		"/proc/meminfo":                      "MemTotal: 16777216 kB\n",
		"/proc/cpuinfo":                      "model name\t: Some CPU\n",
		"/sys/class/drm/card0/device/vendor": "0x8086\n",
		"/sys/class/drm/card1/device/vendor": "0x10DE\n",
		"/sys/class/drm/card2/device/vendor": "0x10de\n",
		"/sys/class/drm/card3/device/vendor": "0x1af4\n",
		"/sys/class/drm/card4/device/vendor": "0x1002\n",
	}))

	if want := []string{"intel", "nvidia", "amd"}; !slices.Equal(got.GPUs, want) {
		t.Fatalf("GPUs = %q, want %q", got.GPUs, want)
	}
	if got.StrixHalo || len(got.Problems) != 0 {
		t.Fatalf("facts = %+v", got)
	}
}

func TestGatherSumsEveryCardsCarveOut(t *testing.T) {
	got := Gather(filesReader(map[string]string{
		"/proc/meminfo":                                   "MemTotal: 33554432 kB\n",
		"/proc/cpuinfo":                                   "model name\t: AMD RYZEN AI MAX+ PRO 395\n",
		"/sys/class/drm/card0/device/vendor":              "0x1002\n",
		"/sys/class/drm/card0/device/mem_info_vram_total": strconv.FormatUint(64*gib, 10) + "\n",
		"/sys/class/drm/card1/device/vendor":              "0x1002\n",
		"/sys/class/drm/card1/device/mem_info_vram_total": strconv.FormatUint(32*gib, 10) + "\n",
	}))

	if want := 32*gib + 64*gib + 32*gib; !got.StrixHalo || got.MemoryBytes != want {
		t.Fatalf("facts = %+v, want Strix Halo with %d bytes", got, want)
	}
}

func TestGatherOnAStrixHaloWhoseCarveOutCannotBeReadSaysTheMemoryMayBeLow(t *testing.T) {
	got := Gather(filesReader(map[string]string{
		"/proc/meminfo":                      "MemTotal: 32505856 kB\n",
		"/proc/cpuinfo":                      "model name\t: AMD RYZEN AI MAX+ 395\n",
		"/sys/class/drm/card0/device/vendor": "0x1002\n",
	}))

	if !got.StrixHalo || got.MemoryBytes != 31*gib {
		t.Fatalf("facts = %+v, want Strix Halo with MemTotal alone", got)
	}
	if !hasProblem(got, "carve-out") {
		t.Fatalf("problems = %q, want one saying the GPU's reserved memory could not be read", got.Problems)
	}
}

func TestGatherOnAMachineThatIsNotAStrixHaloIgnoresACarveOutFile(t *testing.T) {
	got := Gather(filesReader(map[string]string{
		"/proc/meminfo":                                   "MemTotal: 16777216 kB\n",
		"/proc/cpuinfo":                                   "model name\t: AMD Ryzen 7 7840U\n",
		"/sys/class/drm/card0/device/vendor":              "0x1002\n",
		"/sys/class/drm/card0/device/mem_info_vram_total": strconv.FormatUint(8*gib, 10) + "\n",
	}))

	if got.StrixHalo || got.MemoryBytes != 16*gib {
		t.Fatalf("facts = %+v, want 16 GiB with no carve-out added", got)
	}
}

func TestGatherRecordsAMissingCPUNameButStillAnswers(t *testing.T) {
	got := Gather(filesReader(map[string]string{
		"/proc/meminfo": "MemTotal: 16777216 kB\n",
		"/proc/cpuinfo": "processor\t: 0\nBogoMIPS\t: 50.00\n",
	}))

	if got.CPU != "" || got.MemoryBytes != 16*gib || !hasProblem(got, "CPU") {
		t.Fatalf("facts = %+v, want memory read, no CPU name and a problem naming the CPU", got)
	}
}

func TestGatherReadsAMemTotalThatIsNotInKilobytesAsAProblem(t *testing.T) {
	for _, meminfo := range []string{"", "Buffers: 5 kB\n", "MemTotal: lots kB\n", "MemTotal:\n"} {
		got := Gather(filesReader(map[string]string{"/proc/meminfo": meminfo, "/proc/cpuinfo": "model name\t: X\n"}))

		if got.MemoryBytes != 0 || !hasProblem(got, "memory") {
			t.Errorf("meminfo %q: facts = %+v, want memory 0 and a problem", meminfo, got)
		}
	}
}

func TestGatherRecordsAGlobThatFailsAndAVendorThatCannotBeRead(t *testing.T) {
	reader := filesReader(map[string]string{"/proc/meminfo": "MemTotal: 16777216 kB\n", "/proc/cpuinfo": "model name\t: X\n"})
	reader.Glob = func(string) ([]string, error) { return nil, filepath.ErrBadPattern }

	if got := Gather(reader); !hasProblem(got, "GPU") || got.MemoryBytes != 16*gib {
		t.Fatalf("a failing glob: facts = %+v, want a GPU problem and the memory still read", got)
	}

	reader = filesReader(map[string]string{"/proc/meminfo": "MemTotal: 16777216 kB\n", "/proc/cpuinfo": "model name\t: X\n"})
	reader.Glob = func(string) ([]string, error) { return []string{"/sys/class/drm/card0/device/vendor"}, nil }
	if got := Gather(reader); !hasProblem(got, "GPU") || len(got.GPUs) != 0 {
		t.Fatalf("an unreadable vendor: facts = %+v, want a GPU problem and no GPU", got)
	}
}

func TestGatherWithAReaderThatReadsNothingNeverPanics(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "freebsd"} {
		got := Gather(Reader{GOOS: goos, GOARCH: "amd64"})

		if got.OS != goos || got.MemoryBytes != 0 || len(got.Problems) == 0 {
			t.Errorf("%s: facts = %+v, want memory 0 with problems", goos, got)
		}
		if rec := Recommend(got); rec.Engine != "ollama" {
			t.Errorf("%s: recommendation = %+v, want Ollama", goos, rec)
		}
	}
}

func TestGatherOnAnOperatingSystemItDoesNotKnowSaysSo(t *testing.T) {
	got := Gather(Reader{GOOS: "windows", GOARCH: "amd64"})

	if got.OS != "windows" || got.Arch != "amd64" || !hasProblem(got, "macOS and Linux") {
		t.Fatalf("facts = %+v, want a problem naming the operating systems that are read", got)
	}
}

func TestOSReaderHasEveryReader(t *testing.T) {
	r := OSReader()

	if r.ReadFile == nil || r.Glob == nil || r.Output == nil || r.GOOS == "" || r.GOARCH == "" {
		t.Fatalf("OSReader = %+v, want every field set", r)
	}
	if _, err := r.Output("definitely-not-a-command-turing-runs"); err == nil {
		t.Fatal("running a command that does not exist succeeded")
	}
}
