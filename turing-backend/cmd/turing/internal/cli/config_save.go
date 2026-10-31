package cli

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/registry"
	"github.com/mcasillas17/TuringAgent/turing-backend/internal/servicecmd"
)

const (
	applyPrompt  = "Apply now? Restarting stops a reply in progress."
	savedMessage = "Saved. It takes effect at the next ./turing start."
	imagesTooOld = "the images predate check-config; run ./turing start --build first"

	// configCheck is the check, in an image's label, that check-config needs.
	configCheck         = "config"
	internalPortKey     = "ORCHESTRATOR_INTERNAL_PORT"
	defaultInternalPort = 3001
)

// validatingServices are the services whose own check-config judges a change,
// in the order they are asked.
var validatingServices = []struct{ Friendly, Compose string }{
	{"orchestrator", "turing-orchestrator"},
	{"runtime", "turing-agent-runtime-general"},
}

// zeroBounds are the settings a running orchestrator refuses at 0 while a
// delegated task waits for consent, which only it can see.
var zeroBounds = []string{"TURING_QUEUE_MAX_WAIT_MS", "TURING_JOB_REAPER_INTERVAL_MS"}

// infrastructureMarkers in a check's output mean Docker or compose.sh failed,
// not the service: exit status 1 alone cannot tell them apart.
var infrastructureMarkers = []string{"Compose launch failed", "Cannot connect to the Docker daemon", "Error response from daemon"}

// saveOptions are answers the caller already has. Yes answers every question
// with its default, yes; NoApply saves without applying. SkipEngineCheck is
// for a caller that checks the engine itself once Turing is up, as setup
// does: a save that changes the local engine then neither checks it nor says
// to.
type saveOptions struct {
	Yes, NoApply, SkipEngineCheck bool
}

// settingsEdit returns the changes to save, given .env as it is under the
// lock. A nil value removes the key.
type settingsEdit func(current *envfile.File) (map[string]*string, error)

// saveSettings checks changes with the services, writes them to .env, and
// applies them. A nil value removes the key. A change to the local engine is
// then proved with engineCheck once Turing is healthy. It returns the exit
// code.
func (e Env) saveSettings(ctx context.Context, changes map[string]*string, opts saveOptions) int {
	return e.saveEdit(ctx, func(*envfile.File) (map[string]*string, error) { return changes, nil }, opts)
}

// saveEdit is saveSettings for changes that depend on what .env holds, worked
// out under the lock. The engine check runs after the lock is released: it
// changes nothing, and a large model can take minutes to load.
func (e Env) saveEdit(ctx context.Context, edit settingsEdit, opts saveOptions) int {
	code, applied, engineChanged := e.saveLocked(ctx, edit, opts)
	if code != 0 || opts.SkipEngineCheck || !engineChanged {
		return code
	}
	if !applied {
		_, _ = fmt.Fprintln(e.Stdout, engineCheckLater)
		return 0
	}
	// Health has passed, so there is nothing to restore: the engine is what
	// needs fixing.
	if e.engineCheck(ctx) != 0 {
		return e.fail("%s", engineCheckFailedAfterApply)
	}
	return 0
}

// saveLocked checks, writes and applies the changes under the lock. applied
// is true when they are running and healthy, and engineChanged when they
// touch the local engine.
func (e Env) saveLocked(ctx context.Context, edit settingsEdit, opts saveOptions) (code int, applied, engineChanged bool) {
	path := filepath.Join(e.BackendDir, ".env")
	// A missing .env is said before the lock file is made.
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return e.fail("%v", errNoDotEnv), false, false
	}
	unlock, err := envfile.Lock(filepath.Join(e.BackendDir, ".env.lock"))
	if errors.Is(err, envfile.ErrLocked) {
		return e.fail("%v; wait for it to finish, then run this command again", err), false, false
	}
	if err != nil {
		return e.fail("cannot lock .env: %v; check that %s is writable by you, then run this command again", err, e.BackendDir), false, false
	}
	defer unlock()

	previous, current, err := e.readDotEnvBytes()
	if err != nil {
		return e.fail("%v", err), false, false
	}
	changes, err := edit(current)
	if err != nil {
		return e.fail("%v", err), false, false
	}
	candidate, changed, err := buildCandidate(previous, changes)
	if err != nil {
		return e.fail("%v", err), false, false
	}
	if bytes.Equal(candidate.Bytes(), previous) {
		_, _ = fmt.Fprintln(e.Stdout, "Nothing to change: .env already holds that.")
		return 0, false, false
	}
	if err := checkAppPort(current, candidate, changed); err != nil {
		return e.fail("%v", err), false, false
	}
	if code, ok := e.validate(ctx, changed); !ok {
		return code, false, false
	}
	if err := envfile.WriteAtomic(path, candidate.Bytes()); err != nil {
		return e.fail("cannot save .env: %v; check .env, then run this command again", err), false, false
	}
	code, applied = e.applySaved(ctx, path, previous, current, candidate, changed, opts)
	return code, applied, changesLocalEngine(changes)
}

// changesLocalEngine reports whether changes name a setting in the registry's
// local engine section. A deprecated name the save moves to its new one is
// not in changes, and does not count: the services read the same value either
// way.
func changesLocalEngine(changes map[string]*string) bool {
	anchor, _ := registry.Lookup("TURING_LOCAL_ENGINE")
	for name := range changes {
		if key, ok := registry.Lookup(name); ok && key.Section == anchor.Section {
			return true
		}
	}
	return false
}

// buildCandidate is .env with the changes made and the deprecated names
// migrated, and every name that changed, with the value the services will
// read for it: "" for one that is removed.
func buildCandidate(data []byte, changes map[string]*string) (*envfile.File, map[string]string, error) {
	candidate, err := envfile.Parse(data)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot parse .env: %w; fix it and try again", err)
	}
	changed := make(map[string]string)
	for _, name := range slices.Sorted(maps.Keys(changes)) {
		if key, ok := registry.Lookup(name); !ok || !key.Editable {
			return nil, nil, fmt.Errorf("%s is not a setting turing config changes; ./turing config show lists the ones it does", name)
		}
		value := changes[name]
		if value == nil {
			candidate.Delete(name)
			changed[name] = ""
			continue
		}
		if err := candidate.Set(name, *value); err != nil {
			return nil, nil, fmt.Errorf("cannot save %w; choose another value and run the command again", err)
		}
		changed[name] = *value
	}
	if err := migrateDeprecated(candidate, changed); err != nil {
		return nil, nil, err
	}
	return candidate, changed, nil
}

// migrateDeprecated follows the services' own fallback against the
// candidate: an old name's value moves to its new name only where the
// services would read it, and the old line goes either way, since nothing
// else reads it. An old value Compose interpolates is left alone, with its
// new name: Set would write ${VAR} as literal text.
func migrateDeprecated(candidate *envfile.File, changed map[string]string) error {
	engine := cmp.Or(envValue(candidate, "TURING_LOCAL_ENGINE"), engineOllama)
	for _, key := range registry.All() {
		for _, old := range key.Deprecated {
			value, present := candidate.Get(old)
			if !present || candidate.Interpolates(old) {
				continue
			}
			// The runtime ignores an old Ollama URL for any other engine, so
			// moving it would point the local route at the wrong server.
			read := old != "OLLAMA_BASE_URL" || engine == engineOllama
			if value != "" && read && envValue(candidate, key.Name) == "" {
				if err := candidate.Set(key.Name, value); err != nil {
					return fmt.Errorf("cannot move %s to %w; set %s with ./turing config set, which removes %s", old, err, key.Name, old)
				}
				changed[key.Name] = value
			}
			candidate.Delete(old)
			changed[old] = ""
		}
	}
	return nil
}

// checkAppPort applies the host's rules to a changed ORCHESTRATOR_PUBLIC_PORT,
// none of which the orchestrator's integer parser checks.
func checkAppPort(current, candidate *envfile.File, changed map[string]string) error {
	value, ok := changed[portKey]
	if !ok {
		return nil
	}
	port := defaultPort
	if value != "" {
		number, err := strconv.Atoi(value)
		if err != nil || strings.TrimLeft(value, "0123456789") != "" || number < 1 || number > 65535 {
			return fmt.Errorf("%s must be a whole number from 1 to 65535; choose another port", portKey)
		}
		port = number
	}
	internal := defaultInternalPort
	if number, err := strconv.Atoi(envValue(candidate, internalPortKey)); err == nil {
		internal = number
	}
	if port == internal {
		return fmt.Errorf("%s must differ from %s (%d); choose another port", portKey, internalPortKey, internal)
	}
	// The port Turing publishes now is held by Turing itself.
	if was, err := publicPort(current); err == nil && was == port {
		return nil
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if errors.Is(err, syscall.EADDRINUSE) {
		return fmt.Errorf("port %d is already in use on 127.0.0.1; choose another port for %s, or stop what holds it", port, portKey)
	}
	if err != nil {
		return fmt.Errorf("port %d cannot be opened on 127.0.0.1 (%v); choose another port for %s", port, err, portKey)
	}
	_ = listener.Close()
	return nil
}

// validate runs check-config in the orchestrator and runtime images with the
// changed values in the environment, which Compose prefers to .env. Only an
// image whose label lists the check is run with an argument: an older binary
// would ignore it and start a second service on the live database.
func (e Env) validate(ctx context.Context, changed map[string]string) (int, bool) {
	// docker image inspect fails the same way for a stopped Docker as for a
	// missing image, so Docker is asked first.
	if !e.dockerRunning(ctx) {
		return e.fail("Docker is not running, so the change was not checked and nothing was saved; %s, then run this command again", e.startDockerHint()), false
	}
	compose := e.compose()
	for _, service := range validatingServices {
		checks, err := host.ImageChecks(ctx, compose, service.Compose)
		switch {
		case errors.Is(err, host.ErrImageMissing):
			return e.fail("%s", imagesTooOld), false
		case err != nil:
			return e.notChecked(fmt.Sprintf("the %s image's checks could not be read", service.Friendly), err.Error()), false
		case !slices.Contains(checks, configCheck):
			return e.fail("%s", imagesTooOld), false
		}
	}

	_, _ = fmt.Fprintln(e.Stdout, "Checking the change with the orchestrator and the runtime...")
	for _, service := range validatingServices {
		stdout, stderr, code, err := compose.Output(ctx, changed, "run", "--rm", "--no-deps", service.Compose, servicecmd.Subcommand(configCheck))
		output := stdout + "\n" + stderr
		switch {
		case err != nil:
			return e.notChecked(fmt.Sprintf("the %s's check could not start", service.Friendly), err.Error()), false
		case code == 0:
			for _, line := range strings.Split(stderr, "\n") {
				if strings.HasPrefix(line, "warning: ") {
					_, _ = fmt.Fprintf(e.Stdout, "%s: %s\n", service.Friendly, strings.TrimRight(line, "\r"))
				}
			}
		case code == 1 && !containsAny(output, infrastructureMarkers):
			return e.refused(service.Friendly, cmp.Or(strings.TrimSpace(stderr), strings.TrimSpace(stdout))), false
		default:
			return e.notChecked(fmt.Sprintf("the %s's check did not run (exit status %d)", service.Friendly, code), output), false
		}
	}
	return 0, true
}

func containsAny(text string, markers []string) bool {
	return slices.ContainsFunc(markers, func(marker string) bool { return strings.Contains(text, marker) })
}

// refused shows a service's refusal, which names the key, and returns exit
// code 1.
func (e Env) refused(service, message string) int {
	_, _ = fmt.Fprintf(e.Stderr, "the %s refused the change, so nothing was saved:\n", service)
	e.indented(message)
	return e.fail("choose another value, then run this command again")
}

// notChecked reports a check that could not run, with the last lines of what
// it printed, and returns exit code 1.
func (e Env) notChecked(what, detail string) int {
	_, _ = fmt.Fprintf(e.Stderr, "not checked: %s, so nothing was saved:\n", what)
	e.indented(host.LastLines(detail, 5))
	return e.fail("fix that, then run this command again; ./turing status shows the state of Docker and the services")
}

func (e Env) indented(text string) {
	for _, line := range strings.Split(text, "\n") {
		_, _ = fmt.Fprintf(e.Stderr, "  %s\n", line)
	}
}

// applySaved applies a saved change if the person agrees, keeping the
// previous bytes until the stack is healthy, so that a change only a running
// service can refuse can be undone. applied is true when the new settings are
// running and healthy.
func (e Env) applySaved(ctx context.Context, path string, previous []byte, current, candidate *envfile.File, changed map[string]string, opts saveOptions) (code int, applied bool) {
	if !opts.NoApply {
		// Ctrl-C at the prompt ends the process, so the save is said first.
		_, _ = fmt.Fprintln(e.Stdout, "Saved in .env.")
	}
	if opts.NoApply || !e.confirm(applyPrompt, true, opts.Yes) {
		_, _ = fmt.Fprintln(e.Stdout, savedMessage)
		for _, name := range zeroBounds {
			if value, ok := changed[name]; ok && isZero(value) {
				_, _ = fmt.Fprintf(e.Stdout, "Warning: the next start may refuse %s=0, because only a running orchestrator can see whether a delegated task is waiting for consent; if it does, run ./turing config set %s with a value above 0.\n",
					name, name)
			}
		}
		return 0, false
	}

	port, key, err := appEndpoint(candidate)
	if err != nil {
		return e.fail("the change is saved in .env, but it cannot be applied: %v", err), false
	}
	service, err := e.applyAndWait(ctx, port, key)
	if err == nil {
		_, _ = fmt.Fprintln(e.Stdout, "Turing is running with the new settings.")
		return 0, true
	}
	_, _ = fmt.Fprintln(e.Stderr, err)

	subject, question, logs := "Turing", "Turing", "./turing logs"
	if service != "" {
		subject, question, logs = "the "+service, "The "+service, "./turing logs "+service
	}
	if !e.confirm(question+" did not come up healthy. Restore the previous settings and apply again?", true, opts.Yes) {
		return e.fail("the new settings are saved in .env, but %s did not come up healthy; run %s", subject, logs), false
	}
	_, _ = fmt.Fprintln(e.Stdout, "Restoring the previous settings...")
	if err := envfile.WriteAtomic(path, previous); err != nil {
		return e.fail("could not restore the previous settings: %v; the new ones are still in .env, and %s shows why they did not come up healthy", err, logs), false
	}
	if port, key, err = appEndpoint(current); err != nil {
		return e.fail("restored the previous settings, but %v", err), false
	}
	if _, err := e.applyAndWait(ctx, port, key); err != nil {
		return e.fail("restored the previous settings, but they did not come up healthy either: %v", err), false
	}
	return e.fail("restored the previous settings, and Turing is running with them; %s shows why the change did not come up healthy", logs), false
}

// applyAndWait has Compose recreate the containers whose settings changed,
// then waits for health. On failure it names the service to blame, or ""
// when Compose itself failed.
func (e Env) applyAndWait(ctx context.Context, port int, key string) (string, error) {
	code, err := e.compose().Run(ctx, nil, nil, e.Stdout, e.Stderr, "up", "-d")
	if err != nil {
		return "", fmt.Errorf("could not run ./scripts/compose.sh: %w", err)
	}
	if code != 0 {
		return "", fmt.Errorf("compose up -d failed (exit status %d); its output is above", code)
	}
	_, _ = fmt.Fprintln(e.Stdout, "Waiting for Turing to become healthy...")
	err = e.waitHealthy(ctx, port, key)
	var unhealthy *UnhealthyError
	switch {
	case err == nil:
		return "", nil
	case errors.As(err, &unhealthy):
		return unhealthy.Service, err
	default:
		return "orchestrator", err
	}
}

func isZero(value string) bool {
	number, err := strconv.Atoi(value)
	return err == nil && number == 0
}
