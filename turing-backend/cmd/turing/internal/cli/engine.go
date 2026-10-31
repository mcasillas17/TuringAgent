package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
	"github.com/mcasillas17/TuringAgent/turing-backend/internal/servicecmd"
)

const (
	// engineCheckName is the check, in the runtime image's label, that
	// check-engine needs.
	engineCheckName = "engine"
	runtimeService  = "turing-agent-runtime-general"

	imagesTooOldForEngine       = "the images predate check-engine"
	rebuildFirst                = "run ./turing start --build first"
	fixThenCheckEngine          = "fix that, then run ./turing engine check"
	engineCheckFailedAfterApply = "The settings are saved and Turing is running, but the engine check failed; fix the engine, then run ./turing engine check."
	engineNotCheckedAfterApply  = "The settings are saved and Turing is running, but the engine was not checked; %s"
	engineCheckLater            = "After ./turing start, run ./turing engine check."
	// engineKeyRemoved is engine use's notice, and engineKeyRemovedConfigSet
	// setup's and config's, each naming a way to give the new server a key
	// that it has. remoteKeyRemoved is the notice for the remote model's key.
	engineKeyRemovedOpening   = "The previous engine's API key was removed; if this server needs one, "
	engineKeyRemoved          = engineKeyRemovedOpening + "add --api-key-stdin."
	engineKeyRemovedConfigSet = engineKeyRemovedOpening + "run ./turing config set " + engineAPIKeyKey + "."
	remoteKeyRemoved          = "The previous remote model's API key was removed; to use the new address, run ./turing config set " + remoteAPIKeyKey + "."

	engineAPIKeyKey  = "TURING_LOCAL_ENGINE_API_KEY"
	engineContextKey = "TURING_LOCAL_CONTEXT_WINDOW_TOKENS"

	// maxCheckOutput bounds what is kept of an engine check's output for its
	// last lines.
	maxCheckOutput = 64 << 10
)

// engine runs `turing engine <subcommand>`. A subcommand it does not know is
// not repeated back, in case it is a secret typed in the wrong place.
func (e Env) engine(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return e.usageError("turing engine needs a subcommand; run ./turing help")
	}
	switch args[0] {
	case "detect":
		return e.engineDetect(ctx, args[1:])
	case "use":
		return e.engineUse(ctx, args[1:])
	case "check":
		return e.engineCheckCommand(ctx, args[1:])
	default:
		return e.usageError("unknown engine command; run ./turing help")
	}
}

func (e Env) engineCheckCommand(ctx context.Context, args []string) int {
	if code, ok := e.parseFlags(e.flagSet("engine check"), args); !ok {
		return code
	}
	if _, err := e.readDotEnv(); err != nil {
		return e.fail("%v", err)
	}
	result := e.engineCheck(ctx)
	switch {
	case result.passed:
		return 0
	case result.stepFailed:
		return 1
	case result.problem != "":
		return e.fail("%s; %s", result.problem, result.next)
	default:
		return e.fail("%s", result.next)
	}
}

// checkEngineOnceRunning checks the engine once Turing is healthy with
// settings just saved, and ends with one sentence saying where that leaves
// them, which names one next step. It returns the exit code.
func (e Env) checkEngineOnceRunning(ctx context.Context) int {
	result := e.engineCheck(ctx)
	switch {
	case result.passed:
		return 0
	case result.stepFailed:
		return e.fail("%s", engineCheckFailedAfterApply)
	}
	if result.problem != "" {
		_, _ = fmt.Fprintln(e.Stderr, result.problem)
	}
	return e.fail(engineNotCheckedAfterApply, result.next)
}

// engineCheckResult is how an engine check ended. passed is every step
// passing; stepFailed is the engine failing one, whose sentence the runtime
// has already shown. Otherwise the engine was not checked: problem, when set,
// says why, any details are already shown, and next is the one thing to do so
// that it can be.
type engineCheckResult struct {
	passed, stepFailed bool
	problem, next      string
}

// engineCheck runs the runtime's check-engine in a one-off runtime container,
// so the engine is proved with the settings, network and provider code the
// runtime itself uses: reachable, the model present, streaming and tool
// calling. Its steps reach the terminal as they happen. An image that cannot
// say, by its label, that it knows check-engine is never run with it. The
// caller says what the result means for what it was doing.
func (e Env) engineCheck(ctx context.Context) engineCheckResult {
	if !e.dockerRunning(ctx) {
		return engineCheckResult{problem: "Docker is not running", next: e.startDockerHint() + ", then run ./turing engine check"}
	}
	compose := e.compose()
	checks, err := host.ImageChecks(ctx, compose, runtimeService)
	switch {
	case err != nil && !errors.Is(err, host.ErrImageMissing):
		return e.engineNotChecked("the runtime image's checks could not be read", err.Error())
	case err != nil || !slices.Contains(checks, engineCheckName):
		return engineCheckResult{problem: imagesTooOldForEngine, next: rebuildFirst}
	}

	_, _ = fmt.Fprintln(e.Stdout, "Checking the engine from inside the runtime; a model that is still loading can take a few minutes...")
	output := &tailBuffer{}
	code, err := compose.Run(ctx, nil, nil, io.MultiWriter(e.Stdout, output), io.MultiWriter(e.Stderr, output),
		"run", "--rm", "--no-deps", runtimeService, servicecmd.Subcommand(engineCheckName))
	switch {
	case err != nil:
		return e.engineNotChecked("the engine check could not start", err.Error())
	case code == 0:
		return engineCheckResult{passed: true}
	case code == 1 && !containsAny(output.String(), infrastructureMarkers):
		// The runtime's own sentence, naming the step and what to change, is
		// already on the terminal.
		return engineCheckResult{stepFailed: true}
	default:
		return e.engineNotChecked(fmt.Sprintf("the engine check did not run to the end (exit status %d)", code), output.String())
	}
}

// engineNotChecked shows why an engine check could not run, with the last
// lines of what it printed.
func (e Env) engineNotChecked(what, detail string) engineCheckResult {
	_, _ = fmt.Fprintf(e.Stderr, "not checked: %s:\n", what)
	e.indented(host.LastLines(detail, 5))
	return engineCheckResult{next: fixThenCheckEngine}
}

// tailBuffer keeps the last maxCheckOutput bytes a command prints on both its
// streams, which os/exec copies from two goroutines.
type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if over := len(b.data) - maxCheckOutput; over > 0 {
		b.data = b.data[over:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

// engineUse points the runtime at a local engine through saveSettings, whose
// check-config judges what this does not. It never takes the engine's key as
// an argument, and never repeats an argument it does not know.
func (e Env) engineUse(ctx context.Context, args []string) int {
	if namesOption(args, "api-key") {
		return e.usageError("the engine's API key is never taken on the command line, where shell history and ps would keep it; pipe it in with --api-key-stdin (see ./turing help)")
	}
	set := e.flagSet("engine use")
	engineURL := set.String("url", "", "")
	model := set.String("model", "", "")
	contextTokens := set.String("context", "", "")
	label := set.String("label", "", "")
	keyFromStdin := set.Bool("api-key-stdin", false, "")
	yes := set.Bool("yes", false, "")
	noApply := set.Bool("no-apply", false, "")
	positional, code, ok := e.parseInterspersed(set, args)
	if !ok {
		return code
	}

	switch {
	case len(positional) == 0:
		return e.usageError("turing engine use needs an engine, ollama or openai; run ./turing help")
	case len(positional) > 1:
		return e.usageError("turing engine use takes one engine, ollama or openai, and its options; run ./turing help")
	}
	engine := positional[0]
	switch {
	case engine != engineOllama && engine != engineOpenAI:
		return e.usageError("turing engine use takes ollama or openai; run ./turing help")
	case engine == engineOpenAI && (*engineURL == "" || *model == ""):
		return e.usageError("openai needs --url and --model, because an OpenAI-compatible server has no usual address or model; run ./turing help")
	case engine == engineOllama && *keyFromStdin:
		return e.usageError("Ollama takes no API key, so --api-key-stdin is for openai only; run ./turing help")
	}

	// No --url clears the setting, which leaves Ollama at its usual address;
	// openai always has one.
	containerURL := ""
	if *engineURL != "" {
		canonical, err := containerEngineURL(*engineURL)
		if err != nil {
			return e.fail("--url: %v; nothing was saved", err)
		}
		containerURL = canonical
	}
	if *label == "" {
		*label = usualLabel(engine, containerURL)
	}

	// A key given here replaces the one in .env.
	var key *string
	if *keyFromStdin {
		read, code, ok := e.readEngineKey()
		if !ok {
			return code
		}
		key = &read
	}

	keyRemoved := false
	edit := func(current *envfile.File) (map[string]*string, error) {
		changes := map[string]*string{
			"TURING_LOCAL_ENGINE":       &engine,
			engineURLKey:                &containerURL,
			"TURING_LOCAL_MODEL":        model,
			"TURING_LOCAL_ENGINE_LABEL": label,
		}
		if *contextTokens != "" {
			changes[engineContextKey] = contextTokens
		}
		if key != nil {
			changes[engineAPIKeyKey] = key
		} else if value, removed := engineKeyChange(current, engine, containerURL); value != nil {
			changes[engineAPIKeyKey] = value
			// Ollama takes no key, so dropping one is not news, and
			// --api-key-stdin, which the notice names, is for openai only.
			keyRemoved = removed && engine == engineOpenAI
		}
		return changes, nil
	}
	// The notice is for a removal that was saved, not one a check refused.
	saved := func() {
		if keyRemoved {
			_, _ = fmt.Fprintln(e.Stdout, engineKeyRemoved)
		}
	}
	return e.saveEdit(ctx, edit, saveOptions{Yes: *yes, NoApply: *noApply, Saved: saved})
}

// engineKeyChange is what becomes of the engine's API key in .env when the
// local engine becomes engine at containerURL. The key belongs to the server
// .env names now and is never sent to another: Ollama takes none, so it is
// always cleared, and an OpenAI-compatible engine keeps it only at the same
// address, compared in canonical form. value is nil when the key is left as
// it is; removed is true when a key that was set is cleared.
func engineKeyChange(current *envfile.File, engine, containerURL string) (value *string, removed bool) {
	held := envValue(current, engineAPIKeyKey)
	if engine == engineOpenAI && (held == "" || keyBelongsTo(current, containerURL)) {
		return nil, false
	}
	cleared := ""
	return &cleared, held != ""
}

// keyBelongsTo reports whether the engine key in .env was set for the server
// at containerURL: .env names an openai engine at that same address, each in
// canonical form.
func keyBelongsTo(current *envfile.File, containerURL string) bool {
	if envValue(current, "TURING_LOCAL_ENGINE") != engineOpenAI {
		return false
	}
	configured, err := containerEngineURL(envValue(current, engineURLKey))
	wanted, wantedErr := containerEngineURL(containerURL)
	return err == nil && wantedErr == nil && configured == wanted
}

// parseInterspersed parses set's options wherever they are among args, and
// returns the arguments that are not options.
func (e Env) parseInterspersed(set *flag.FlagSet, args []string) (positional []string, code int, ok bool) {
	for {
		if err := set.Parse(args); err != nil {
			return nil, e.flagError(set, err), false
		}
		rest := set.Args()
		if len(rest) == 0 {
			return positional, 0, true
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// readEngineKey reads the engine's API key for --api-key-stdin.
func (e Env) readEngineKey() (string, int, bool) {
	return e.readKeyOption(engineAPIKeyKey, "./turing engine use openai ... --api-key-stdin")
}

// readKeyOption reads the value of the secret setting name for an option
// that reads it rather than take it as an argument: with typing hidden at a
// terminal, otherwise one line from standard input. pipeInto is the command
// to pipe it into.
func (e Env) readKeyOption(name, pipeInto string) (string, int, bool) {
	var key string
	if e.IsTerminal {
		read, err := e.ReadSecret(fmt.Sprintf("Value for %s (typing is hidden): ", name))
		if err != nil {
			return "", e.fail("could not read the key: %v; run the command again", err), false
		}
		key = read
	} else {
		line, err := e.readLine()
		if err != nil && line == "" {
			if errors.Is(err, io.EOF) {
				return "", e.usageError("nothing was read from standard input for %s; pipe the key in, as in printf '%%s\\n' KEY | %s",
					name, pipeInto), false
			}
			return "", e.fail("could not read standard input: %v; run the command again", err), false
		}
		key = line
	}
	if key == "" {
		return "", e.usageError("the key read from standard input is empty, so nothing changed; to remove %s, run ./turing config unset %s", name, name), false
	}
	return key, 0, true
}

// namesOption reports whether any of args is one of the named options, as
// -name, --name or --name=value. A secret given that way is refused before
// the flags are parsed, which could only call it unknown.
func namesOption(args []string, names ...string) bool {
	return slices.ContainsFunc(args, func(arg string) bool {
		name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		return strings.HasPrefix(arg, "-") && slices.Contains(names, name)
	})
}

// urlPort is the port a URL names, or "" when it names none.
func urlPort(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Port()
}
