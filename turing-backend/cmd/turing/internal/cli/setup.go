package cli

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/registry"
)

const (
	engineKindKey   = "TURING_LOCAL_ENGINE"
	localModelKey   = "TURING_LOCAL_MODEL"
	engineLabelKey  = "TURING_LOCAL_ENGINE_LABEL"
	remoteURLKey    = "OPENAI_BASE_URL"
	remoteModelKey  = "OPENAI_MODEL"
	remoteAPIKeyKey = "OPENAI_API_KEY"
	teamKey         = "TURING_AGENT_TEAM_ENABLED"

	setupNeedsATerminal = "setup asks questions; run it in a terminal, or pass --yes with the choices as flags"
	initScript          = "./scripts/init.sh"
)

// backendFiles are what make a directory a TuringAgent backend that setup can
// provision and build.
var backendFiles = []string{"scripts/init.sh", "infra/docker-compose.yml"}

var errAnswersEnded = errors.New("setup stopped because the answers ended, so nothing was saved; run ./turing setup again")

// setupFlags are setup's options. url is --url as the containers reach it,
// and remoteKey is what --remote-key-stdin read.
type setupFlags struct {
	yes                       bool
	engine, url, model, label string
	remoteURL, remoteModel    string
	remoteKeyFromStdin        bool
	remoteKey                 string
	team                      string
}

// setupRun is setup once .env exists: what it was given, .env as init.sh left
// it, and the changes chosen so far, each a value that differs from .env.
type setupRun struct {
	Env
	flags       setupFlags
	interactive bool
	firstRun    bool
	file        *envfile.File
	changes     map[string]*string
	// engineChoice is the local engine chosen, or nil when .env's is kept as
	// it is.
	engineChoice *localChoice
}

// setup provisions and builds Turing, walks through the choices that matter
// most, saves them through saveSettings, starts Turing, proves the engine and
// shows how the app connects. On a rerun the current choices are the
// defaults, and --yes keeps every one of them.
func (e Env) setup(ctx context.Context, args []string) int {
	flags, code, ok := e.parseSetupFlags(args)
	if !ok {
		return code
	}
	// Before anything runs: nobody could answer, and nothing would block.
	if !e.IsTerminal && !flags.yes {
		return e.usageError("%s", setupNeedsATerminal)
	}
	if flags.remoteKeyFromStdin {
		key, code, ok := e.readKeyOption(remoteAPIKeyKey, "./turing setup --yes --remote-key-stdin")
		if !ok {
			return code
		}
		flags.remoteKey = key
	}

	if !e.dockerRunning(ctx) {
		return e.fail("Docker is not running; %s, then run ./turing setup", e.startDockerHint())
	}
	if !e.isBackend() {
		return e.fail("%s is not a TuringAgent backend: it needs %s; run ./turing setup from a complete TuringAgent checkout",
			e.BackendDir, strings.Join(backendFiles, " and "))
	}
	_, err := os.Lstat(filepath.Join(e.BackendDir, ".env"))
	firstRun := errors.Is(err, fs.ErrNotExist)
	if code := e.provision(ctx); code != 0 {
		return code
	}
	// The images check every setting before it is saved, so they come first.
	e.printf("Building the images...\n")
	if code, err := e.compose().Run(ctx, nil, nil, e.Stdout, e.Stderr, "build"); err != nil || code != 0 {
		return e.composeFailed(ctx, code, err, "./turing setup")
	}
	file, err := e.readDotEnv()
	if err != nil {
		return e.fail("%v", err)
	}

	run := &setupRun{Env: e, flags: flags, interactive: e.IsTerminal && !flags.yes, firstRun: firstRun, file: file, changes: make(map[string]*string)}
	for _, step := range []func(context.Context) error{run.chooseLocalEngine, run.chooseRemote, run.chooseTeam} {
		if err := step(ctx); err != nil {
			return e.fail("%v", err)
		}
	}
	if code := run.save(ctx); code != 0 {
		return code
	}
	return e.startAndConnect(ctx)
}

func (e Env) parseSetupFlags(args []string) (setupFlags, int, bool) {
	var f setupFlags
	if namesOption(args, "remote-key", "api-key") {
		return f, e.usageError("the remote model's API key is never taken on the command line, where shell history and ps would keep it; pipe it in with --remote-key-stdin (see ./turing help)"), false
	}
	set := e.flagSet("setup")
	set.BoolVar(&f.yes, "yes", false, "")
	set.StringVar(&f.engine, "engine", "", "")
	engineURL := set.String("url", "", "")
	set.StringVar(&f.model, "model", "", "")
	set.StringVar(&f.label, "label", "", "")
	set.StringVar(&f.remoteURL, "remote-url", "", "")
	set.StringVar(&f.remoteModel, "remote-model", "", "")
	set.BoolVar(&f.remoteKeyFromStdin, "remote-key-stdin", false, "")
	set.StringVar(&f.team, "team", "", "")
	if code, ok := e.parseFlags(set, args); !ok {
		return f, code, false
	}

	// No value is repeated: a secret typed in the wrong place would be printed.
	switch {
	case f.engine != "" && f.engine != engineOllama && f.engine != engineOpenAI:
		return f, e.usageError("--engine takes ollama or openai; run ./turing help"), false
	case f.engine == "" && (*engineURL != "" || f.model != "" || f.label != ""):
		return f, e.usageError("--url, --model and --label go with --engine, which names the engine they are for; run ./turing help"), false
	case f.engine == engineOpenAI && (*engineURL == "" || f.model == ""):
		return f, e.usageError("openai needs --url and --model, because an OpenAI-compatible server has no usual address or model; run ./turing help"), false
	case f.team != "" && f.team != "on" && f.team != "off":
		return f, e.usageError("--team takes on or off; run ./turing help"), false
	}
	if *engineURL != "" {
		canonical, err := containerEngineURL(*engineURL)
		if err != nil {
			return f, e.fail("--url: %v; nothing was saved", err), false
		}
		f.url = canonical
	}
	return f, 0, true
}

// isBackend reports whether BackendDir holds the files setup runs and builds
// from.
func (e Env) isBackend() bool {
	for _, name := range backendFiles {
		info, err := os.Stat(filepath.Join(e.BackendDir, name))
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// provision runs init.sh as it is, which makes .env on a first run and fills
// in only what is missing on a rerun, so no secret is ever regenerated. Its
// output, the app key line included, goes straight to the terminal.
func (e Env) provision(ctx context.Context) int {
	e.printf("Provisioning (%s)...\n", initScript)
	code, err := e.Runner.Run(ctx, host.Command{Dir: e.BackendDir, Name: initScript, Env: e.Environ, Stdout: e.Stdout, Stderr: e.Stderr})
	switch {
	case err != nil:
		return e.fail("could not run %s: %v; check that this checkout is intact, then run ./turing setup again", initScript, err)
	case code != 0:
		return e.fail("%s failed (exit status %d), so setup stopped; its output above says why; fix that, then run ./turing setup again", initScript, code)
	}
	return 0
}

// set records value for name when it differs from what .env holds now.
func (s *setupRun) set(name, value, now string) {
	if value != now {
		s.changes[name] = &value
	}
}

// save shows the changes and, once they are confirmed, checks and saves them
// without applying them: setup starts Turing and checks the engine itself
// next. The engine's key is decided as engine use decides it, against .env as
// it is under the lock; the list shown is that decision against .env as it
// was read.
func (s *setupRun) save(ctx context.Context) int {
	shown, _ := s.changesFor(s.file)
	if len(shown) == 0 {
		s.printf("\nNo changes.\n")
		return 0
	}
	s.printf("\nSettings to save:\n")
	for _, key := range registry.All() {
		if value, ok := shown[key.Name]; ok {
			s.printf("  %s\n", shownChange(key, *value))
		}
	}
	if !s.confirm("Save these settings?", true, s.flags.yes) {
		return s.fail("nothing was saved, and Turing was not started; run ./turing setup again to choose again")
	}
	keyRemoved := false
	edit := func(current *envfile.File) (map[string]*string, error) {
		changes, removed := s.changesFor(current)
		keyRemoved = removed
		return changes, nil
	}
	// The notice is for a removal that was saved, not one a check refused.
	saved := func() {
		if keyRemoved {
			s.printf("%s\n", setupKeyRemoved)
		}
	}
	return s.saveEdit(ctx, edit, saveOptions{Yes: s.flags.yes, NoApply: true, CallerStarts: true, SkipEngineCheck: true, Saved: saved})
}

// changesFor is the changes chosen, with what becomes of the engine's key
// when .env is current; removed is a key that was set being cleared.
func (s *setupRun) changesFor(current *envfile.File) (changes map[string]*string, removed bool) {
	changes = maps.Clone(s.changes)
	if s.engineChoice == nil {
		return changes, false
	}
	value, removed := engineKeyChange(current, s.engineChoice.engine, s.engineChoice.url)
	if value == nil || *value == envValue(current, engineAPIKeyKey) {
		return changes, false
	}
	changes[engineAPIKeyKey] = value
	return changes, removed
}

// shownChange is a change as it may be shown: a secret only as set or
// removed, and an address that could carry a credential not at all.
func shownChange(key registry.Key, value string) string {
	switch {
	case key.IsSecret() && value == "":
		return key.Name + ": removed"
	case key.IsSecret():
		return key.Name + ": set"
	case value == "":
		return key.Name + ": cleared"
	case key.Kind == registry.URL && strings.ContainsAny(value, "@?#"):
		return key.Name + ": set (not shown, because it has a user, password, query or fragment)"
	default:
		return key.Name + "=" + value
	}
}

// startAndConnect starts Turing with what .env holds, waits for health,
// proves the engine, and shows how the app connects. An engine that fails the
// check still leaves Turing running, so the connection is shown either way.
func (e Env) startAndConnect(ctx context.Context) int {
	file, err := e.readDotEnv()
	if err != nil {
		return e.fail("%v", err)
	}
	port, key, err := appEndpoint(file)
	if err != nil {
		return e.fail("%v", err)
	}
	e.printf("\nStarting Turing...\n")
	if code := e.upAndWait(ctx, port, key, false); code != 0 {
		return code
	}
	code := e.checkEngineOnceRunning(ctx)
	if code == 0 {
		e.printf("\nTuring is running, and its engine works. To connect the app:\n")
	} else {
		e.printf("\nTo connect the app:\n")
	}
	e.printConnection(port, key)
	return code
}
