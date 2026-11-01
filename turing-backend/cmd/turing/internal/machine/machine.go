// Package machine reads the facts about this computer that decide which local
// model engine suits it: its operating system, processor, memory and GPUs.
// Every read goes through a Reader, so tests give it fixture machines. A fact
// that cannot be read is reported and left empty; it is never an error,
// because the recommendation still has to be printed.
package machine

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// commandTimeout bounds one uname or sysctl call.
const commandTimeout = 5 * time.Second

// Reader is everything Gather touches outside its own variables.
type Reader struct {
	ReadFile func(path string) ([]byte, error)
	Glob     func(pattern string) ([]string, error)
	// Output runs a command and returns its standard output.
	Output func(name string, args ...string) (string, error)
	// GOOS and GOARCH are the operating system and architecture of this
	// process, as the runtime package names them.
	GOOS, GOARCH string
}

// OSReader is the Reader for this machine.
func OSReader() Reader {
	return Reader{
		ReadFile: os.ReadFile,
		Glob:     filepath.Glob,
		Output: func(name string, args ...string) (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
			defer cancel()
			out, err := exec.CommandContext(ctx, name, args...).Output()
			return string(out), err
		},
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
	}
}

var errNotRead = errors.New("this reader cannot do that")

func (r Reader) readFile(path string) ([]byte, error) {
	if r.ReadFile == nil {
		return nil, errNotRead
	}
	return r.ReadFile(path)
}

func (r Reader) glob(pattern string) ([]string, error) {
	if r.Glob == nil {
		return nil, errNotRead
	}
	return r.Glob(pattern)
}

func (r Reader) output(name string, args ...string) (string, error) {
	if r.Output == nil {
		return "", errNotRead
	}
	return r.Output(name, args...)
}

// Facts is what was read. A fact that could not be read is left zero and named
// in Problems.
type Facts struct {
	// OS and Arch are runtime.GOOS and the machine's architecture: "darwin"
	// or "linux", and "arm64", "x86_64" or "amd64".
	OS, Arch string
	CPU      string
	// MemoryBytes is the machine's memory, 0 when unknown. On an AMD Strix Halo
	// it includes the GPU's reserved share, which the BIOS takes out of what
	// the operating system reports.
	MemoryBytes  uint64
	AppleSilicon bool
	StrixHalo    bool
	// GPUs holds "amd", "nvidia" and "intel", once each, in the order the
	// kernel lists the cards.
	GPUs []string
	// Problems names each fact that could not be read, as a sentence.
	Problems []string
}

const (
	linuxMemInfo = "/proc/meminfo"
	linuxCPUInfo = "/proc/cpuinfo"
	drmVendors   = "/sys/class/drm/card*/device/vendor"
	drmVRAMTotal = "/sys/class/drm/card*/device/mem_info_vram_total"
)

var vendors = map[string]string{"0x1002": "amd", "0x10de": "nvidia", "0x8086": "intel"}

// Gather reads the facts for the Reader's operating system.
func Gather(r Reader) Facts {
	f := Facts{OS: r.GOOS, Arch: r.GOARCH}
	switch r.GOOS {
	case "darwin":
		f.gatherDarwin(r)
	case "linux":
		f.gatherLinux(r)
	default:
		f.problem("hardware facts are read only on macOS and Linux, so this machine's memory and GPU are unknown")
	}
	return f
}

func (f *Facts) problem(text string) { f.Problems = append(f.Problems, text) }

func (f *Facts) gatherDarwin(r Reader) {
	if arch, err := r.output("uname", "-m"); err == nil && strings.TrimSpace(arch) != "" {
		f.Arch = strings.TrimSpace(arch)
	} else if f.Arch == "" {
		f.problem("could not read the processor architecture (uname -m)")
	}
	f.AppleSilicon = f.Arch == "arm64"

	if out, err := r.output("sysctl", "-n", "hw.memsize"); err == nil {
		if bytes, parseErr := strconv.ParseUint(strings.TrimSpace(out), 10, 64); parseErr == nil {
			f.MemoryBytes = bytes
		}
	}
	if f.MemoryBytes == 0 {
		f.problem("could not read this Mac's memory (sysctl hw.memsize)")
	}

	if out, err := r.output("sysctl", "-n", "machdep.cpu.brand_string"); err == nil {
		f.CPU = strings.TrimSpace(out)
	}
	if f.CPU == "" {
		f.problem("could not read the CPU name (sysctl machdep.cpu.brand_string)")
	}
}

func (f *Facts) gatherLinux(r Reader) {
	if data, err := r.readFile(linuxMemInfo); err == nil {
		f.MemoryBytes = memTotal(data)
	}
	if f.MemoryBytes == 0 {
		f.problem("could not read this machine's memory (" + linuxMemInfo + ")")
	}

	if data, err := r.readFile(linuxCPUInfo); err == nil {
		f.CPU = modelName(data)
	}
	if f.CPU == "" {
		f.problem("could not read the CPU name (" + linuxCPUInfo + ")")
	}
	f.StrixHalo = strings.Contains(strings.ToLower(f.CPU), "ryzen ai max")

	f.readGPUs(r)

	// Memory the BIOS gave the GPU is not in MemTotal, and a Strix Halo can
	// give it most of the machine's memory. With MemTotal unknown there is
	// nothing to add it to.
	if f.StrixHalo && f.MemoryBytes > 0 {
		if carveOut, ok := vramTotal(r); ok {
			f.MemoryBytes += carveOut
		} else {
			f.problem("could not read the GPU memory carve-out (mem_info_vram_total), so the memory shown may be far lower than what is installed")
		}
	}
}

// readGPUs lists the vendors of the cards the kernel knows. A machine with no
// card, such as a server without a display adapter, has no GPU and no
// problem.
func (f *Facts) readGPUs(r Reader) {
	paths, err := r.glob(drmVendors)
	unreadable := err != nil
	for _, path := range paths {
		data, readErr := r.readFile(path)
		if readErr != nil {
			unreadable = true
			continue
		}
		gpu, known := vendors[strings.ToLower(strings.TrimSpace(string(data)))]
		if known && !slices.Contains(f.GPUs, gpu) {
			f.GPUs = append(f.GPUs, gpu)
		}
	}
	if unreadable {
		f.problem("could not read every GPU's vendor under /sys/class/drm")
	}
}

// memTotal is /proc/meminfo's MemTotal in bytes, or 0 when it is missing or
// not a size in kilobytes.
func memTotal(data []byte) uint64 {
	for _, line := range strings.Split(string(data), "\n") {
		rest, found := strings.CutPrefix(line, "MemTotal:")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) != 2 || fields[1] != "kB" {
			return 0
		}
		kilobytes, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil || kilobytes > math.MaxUint64/1024 {
			return 0
		}
		return kilobytes * 1024
	}
	return 0
}

// modelName is the first "model name" in /proc/cpuinfo, which x86 machines
// have and some others do not.
func modelName(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, ":")
		if found && strings.TrimSpace(key) == "model name" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// vramTotal sums every card's mem_info_vram_total, and reports whether at
// least one was read.
func vramTotal(r Reader) (total uint64, ok bool) {
	paths, err := r.glob(drmVRAMTotal)
	if err != nil {
		return 0, false
	}
	for _, path := range paths {
		data, readErr := r.readFile(path)
		if readErr != nil {
			continue
		}
		bytes, parseErr := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if parseErr != nil {
			continue
		}
		total += bytes
		ok = true
	}
	return total, ok
}
