package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/registry"
)

// engineCheckNote says what a model list from this machine does not prove.
const engineCheckNote = "(turing engine check, coming in a later version, proves it from inside the runtime)"

// statusReport prints the check lines as they finish, and keeps the warnings
// for the end.
type statusReport struct {
	out      io.Writer
	failed   bool
	warnings []string
}

func (r *statusReport) line(label, text string) {
	_, _ = fmt.Fprintf(r.out, "%-11s %s\n", label, text)
}

func (r *statusReport) ok(label, text string) { r.line(label, "ok: "+text) }

func (r *statusReport) fail(label, text string) {
	r.failed = true
	r.line(label, "FAILED: "+text)
}

func (r *statusReport) warn(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}

func (r *statusReport) printWarnings() {
	if len(r.warnings) == 0 {
		return
	}
	_, _ = fmt.Fprintf(r.out, "\nWarnings:\n")
	for _, warning := range r.warnings {
		_, _ = fmt.Fprintf(r.out, "  - %s\n", warning)
	}
}

// status prints one line per check, then the warnings, and exits 1 only when a
// check failed. It never prints a secret.
func (e Env) status(ctx context.Context, args []string) int {
	if code, ok := e.parseFlags(e.flagSet("status"), args); !ok {
		return code
	}
	file, fileErr := e.readDotEnv()
	report := &statusReport{out: e.Stdout}

	e.checkSetup(report, file, fileErr)
	dockerOK := e.checkDocker(ctx, report)
	servicesOK := e.checkServices(ctx, report, dockerOK)
	e.checkAppPort(ctx, report, file, fileErr, servicesOK)
	e.checkEngine(ctx, report, file, fileErr)
	checkAgentTeam(report, file, fileErr)
	e.warnAboutSettings(report, file, fileErr)
	report.printWarnings()

	if report.failed {
		return 1
	}
	return 0
}

const dotEnvUnreadable = "not checked: .env cannot be read, so see Setup"

func (e Env) checkSetup(r *statusReport, file *envfile.File, fileErr error) {
	if fileErr != nil {
		r.fail("Setup", oneLine(fileErr.Error()))
		return
	}
	path := filepath.Join(e.BackendDir, ".env")
	if info, err := os.Stat(path); err == nil && info.Mode().Perm() != 0o600 {
		r.warn(".env has mode %04o, not 0600, so other users on this machine may read its secrets; run chmod 600 %s", info.Mode().Perm(), path)
	}

	var missing []string
	for _, key := range registry.All() {
		if value, _ := file.Get(key.Name); key.Generated && value == "" {
			missing = append(missing, key.Name)
		}
	}
	if len(missing) > 0 {
		r.fail("Setup", fmt.Sprintf(".env has no value for %s; run ./scripts/init.sh to generate it", strings.Join(missing, ", ")))
		return
	}
	r.ok("Setup", ".env holds every generated key")
}

func (e Env) checkDocker(ctx context.Context, r *statusReport) bool {
	version, ok := e.dockerVersion(ctx)
	if ok {
		r.ok("Docker", strings.TrimSpace("Docker "+version+" is running"))
	} else {
		r.fail("Docker", fmt.Sprintf("Docker is not running; %s, then run ./turing start", e.startDockerHint()))
	}
	if e.GOOS == "linux" {
		e.warnIfDockerWillNotStartAtBoot(ctx, r)
	}
	return ok
}

// warnIfDockerWillNotStartAtBoot warns when systemd says Docker is not
// enabled. A host without systemctl says nothing.
func (e Env) warnIfDockerWillNotStartAtBoot(ctx context.Context, r *statusReport) {
	var out strings.Builder
	_, err := e.Runner.Run(ctx, host.Command{
		Dir:    e.BackendDir,
		Name:   "systemctl",
		Args:   []string{"is-enabled", "docker"},
		Env:    e.Environ,
		Stdout: &out,
		Stderr: io.Discard,
	})
	if err != nil {
		return
	}
	if word := strings.TrimSpace(out.String()); word != "enabled" {
		r.warn("Docker is not enabled to start at boot (systemctl says %q), so Turing will not come back after a reboot; run sudo systemctl enable docker",
			cmp.Or(word, "nothing"))
	}
}

// checkServices reports each service's state, and reports whether all four
// are running and healthy.
func (e Env) checkServices(ctx context.Context, r *statusReport, dockerOK bool) bool {
	if !dockerOK {
		r.fail("Services", "not checked: Docker is not running")
		return false
	}
	states, err := host.Ps(ctx, e.compose())
	if err != nil {
		r.fail("Services", fmt.Sprintf("could not list the services (%s); run ./turing start, which shows what is wrong", oneLine(err.Error())))
		return false
	}
	summary := serviceSummary(states)
	if service, _ := notHealthy(states); service != "" {
		// A service that was never created has no log to read.
		next := "./turing logs " + service
		if !hasContainer(states, service) {
			next = "./turing start"
		}
		r.fail("Services", fmt.Sprintf("%s; run %s", summary, next))
		return false
	}
	r.ok("Services", summary)
	return true
}

// hasContainer reports whether Compose lists any container for the service,
// by its friendly name.
func hasContainer(states []host.ServiceState, friendly string) bool {
	name, _ := composeName(friendly)
	return slices.ContainsFunc(states, func(state host.ServiceState) bool { return state.Service == name })
}

// serviceSummary lists the four services with their state, and their health
// where they report one. A service with several containers shows a healthy
// one when there is one.
func serviceSummary(states []host.ServiceState) string {
	parts := make([]string, 0, len(serviceNames))
	for _, want := range serviceNames {
		var shown *host.ServiceState
		for i, state := range states {
			if state.Service != want.Compose {
				continue
			}
			if shown == nil || (state.State == "running" && (state.Health == "" || state.Health == "healthy")) {
				shown = &states[i]
			}
		}
		switch {
		case shown == nil:
			parts = append(parts, want.Friendly+" not created")
		case shown.Health == "":
			parts = append(parts, want.Friendly+" "+shown.State)
		default:
			parts = append(parts, fmt.Sprintf("%s %s (%s)", want.Friendly, shown.State, shown.Health))
		}
	}
	return strings.Join(parts, ", ")
}

func (e Env) checkAppPort(ctx context.Context, r *statusReport, file *envfile.File, fileErr error, servicesOK bool) {
	if fileErr != nil {
		r.fail("App port", dotEnvUnreadable)
		return
	}
	port, key, err := appEndpoint(file)
	if err != nil {
		r.fail("App port", oneLine(err.Error()))
		return
	}
	err = e.Health(ctx, port, key)
	switch {
	case err == nil:
		r.ok("App port", fmt.Sprintf("the orchestrator answers on 127.0.0.1:%d with the key in .env", port))
	case errors.Is(err, host.ErrUnauthenticated):
		r.fail("App port", keyRefusedMessage)
	default:
		next := "./turing start"
		if servicesOK {
			next = "./turing logs orchestrator"
		}
		r.fail("App port", fmt.Sprintf("the orchestrator did not answer on 127.0.0.1:%d (%s); run %s", port, oneLine(err.Error()), next))
	}
}

func (e Env) checkEngine(ctx context.Context, r *statusReport, file *envfile.File, fileErr error) {
	if fileErr != nil {
		r.fail("Engine", dotEnvUnreadable)
		return
	}
	result := e.engineProbe(ctx, file)
	if result.Warning != "" {
		r.warn("%s", result.Warning)
	}
	if !result.ok() {
		r.fail("Engine", result.Problem)
		return
	}
	count := fmt.Sprintf("%d models", len(result.Models))
	if len(result.Models) == 1 {
		count = "1 model"
	}
	verdict := fmt.Sprintf("and %s is listed", result.Model)
	if !result.Listed {
		verdict = fmt.Sprintf("but %s is not listed", result.Model)
		fix := "correct TURING_LOCAL_MODEL in .env to a model it lists"
		if result.Engine == engineOllama {
			fix = fmt.Sprintf("run ollama pull %s, or correct TURING_LOCAL_MODEL in .env", result.Model)
		}
		r.warn("the local model %s is not among the models the engine lists, so a reply will fail; %s", result.Model, fix)
	}
	r.ok("Engine", fmt.Sprintf("%s at %s lists %s, %s %s", result.Engine, result.URL, count, verdict, engineCheckNote))
}

func checkAgentTeam(r *statusReport, file *envfile.File, fileErr error) {
	switch {
	case fileErr != nil:
		r.line("Agent team", "not known: .env cannot be read")
	case envValue(file, "TURING_AGENT_TEAM_ENABLED") == "true":
		r.line("Agent team", "on (experimental)")
	default:
		r.line("Agent team", "off")
	}
}

func envValue(file *envfile.File, name string) string {
	value, _ := file.Get(name)
	return value
}

// warnAboutSettings warns about deprecated names in .env, and about settings
// the shell exports: Compose prefers an exported variable to .env, so the
// owner's own docker compose calls would not match what turing runs.
func (e Env) warnAboutSettings(r *statusReport, file *envfile.File, fileErr error) {
	if fileErr == nil {
		for _, key := range registry.All() {
			for _, old := range key.Deprecated {
				if envValue(file, old) != "" {
					r.warn("%s in .env is deprecated; rename it to %s (the services read it only while %s is empty)", old, key.Name, key.Name)
				}
			}
		}
	}

	exported := make(map[string]bool)
	for _, entry := range e.Environ {
		name, _, _ := strings.Cut(entry, "=")
		exported[name] = true
	}
	var named []string
	for _, name := range registry.StripNames() {
		if exported[name] && !slices.Contains(named, name) {
			named = append(named, name)
			r.warn("%s: your shell exports it, and your own docker compose calls would prefer it to .env (turing's own calls ignore it)", name)
		}
	}
}
