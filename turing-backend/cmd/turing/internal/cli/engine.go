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

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
	"github.com/mcasillas17/TuringAgent/turing-backend/internal/servicecmd"
)

const (
	// engineCheckName is the check, in the runtime image's label, that
	// check-engine needs.
	engineCheckName = "engine"
	runtimeService  = "turing-agent-runtime-general"

	imagesTooOldForEngine       = "the images predate check-engine; run ./turing start --build first"
	engineCheckFailedAfterApply = "The settings are saved and Turing is running, but the engine check failed; fix the engine, then run ./turing engine check."
	engineCheckLater            = "After ./turing start, run ./turing engine check."

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
	return e.engineCheck(ctx)
}

// engineCheck runs the runtime's check-engine in a one-off runtime container,
// so the engine is proved with the settings, network and provider code the
// runtime itself uses: reachable, the model present, streaming and tool
// calling. Its steps reach the terminal as they happen, and a failure is its
// own sentence. An image that cannot say, by its label, that it knows
// check-engine is never run with it. It returns the exit code.
func (e Env) engineCheck(ctx context.Context) int {
	if !e.dockerRunning(ctx) {
		return e.fail("Docker is not running, so the engine was not checked; %s, then run ./turing engine check", e.startDockerHint())
	}
	compose := e.compose()
	checks, err := host.ImageChecks(ctx, compose, runtimeService)
	switch {
	case errors.Is(err, host.ErrImageMissing):
		return e.fail("%s", imagesTooOldForEngine)
	case err != nil:
		return e.engineNotChecked("the runtime image's checks could not be read", err.Error())
	case !slices.Contains(checks, engineCheckName):
		return e.fail("%s", imagesTooOldForEngine)
	}

	_, _ = fmt.Fprintln(e.Stdout, "Checking the engine from inside the runtime; a model that is still loading can take a few minutes...")
	output := &tailBuffer{}
	code, err := compose.Run(ctx, nil, nil, io.MultiWriter(e.Stdout, output), io.MultiWriter(e.Stderr, output),
		"run", "--rm", "--no-deps", runtimeService, servicecmd.Subcommand(engineCheckName))
	switch {
	case err != nil:
		return e.engineNotChecked("the engine check could not start", err.Error())
	case code == 0:
		return 0
	case code == 1 && !containsAny(output.String(), infrastructureMarkers):
		// The runtime's own sentence, naming the step and what to change, is
		// already on the terminal.
		return 1
	default:
		return e.engineNotChecked(fmt.Sprintf("the engine check did not run to the end (exit status %d)", code), output.String())
	}
}

// engineNotChecked reports an engine check that could not run, with the last
// lines of what it printed, and returns exit code 1.
func (e Env) engineNotChecked(what, detail string) int {
	_, _ = fmt.Fprintf(e.Stderr, "not checked: %s:\n", what)
	e.indented(host.LastLines(detail, 5))
	return e.fail("fix that, then run ./turing engine check; ./turing status shows the state of Docker and the services")
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
	for _, arg := range args {
		if name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "="); strings.HasPrefix(arg, "-") && name == "api-key" {
			return e.usageError("the engine's API key is never taken on the command line, where shell history and ps would keep it; pipe it in with --api-key-stdin (see ./turing help)")
		}
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
	if *label == "" && engine == engineOpenAI && urlPort(containerURL) == ds4Port {
		*label = labelDS4
	}

	changes := map[string]*string{
		"TURING_LOCAL_ENGINE":       &engine,
		engineURLKey:                &containerURL,
		"TURING_LOCAL_MODEL":        model,
		"TURING_LOCAL_ENGINE_LABEL": label,
	}
	if *contextTokens != "" {
		changes[engineContextKey] = contextTokens
	}
	switch {
	case engine == engineOllama:
		// The runtime refuses a key with Ollama.
		empty := ""
		changes[engineAPIKeyKey] = &empty
	case *keyFromStdin:
		key, code, ok := e.readEngineKey()
		if !ok {
			return code
		}
		changes[engineAPIKeyKey] = &key
	}
	return e.saveSettings(ctx, changes, saveOptions{Yes: *yes, NoApply: *noApply})
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

// readEngineKey reads the engine's API key: with typing hidden at a terminal,
// otherwise one line from standard input.
func (e Env) readEngineKey() (string, int, bool) {
	var key string
	if e.IsTerminal {
		read, err := e.ReadSecret(fmt.Sprintf("Value for %s (typing is hidden): ", engineAPIKeyKey))
		if err != nil {
			return "", e.fail("could not read the key: %v; run the command again", err), false
		}
		key = read
	} else {
		line, err := e.readLine()
		if err != nil && line == "" {
			if errors.Is(err, io.EOF) {
				return "", e.usageError("nothing was read from standard input for %s; pipe the key in, as in printf '%%s\\n' KEY | ./turing engine use openai ... --api-key-stdin",
					engineAPIKeyKey), false
			}
			return "", e.fail("could not read standard input: %v; run the command again", err), false
		}
		key = line
	}
	if key == "" {
		return "", e.usageError("the key read from standard input is empty, so nothing changed; to remove the engine's key, run ./turing config unset %s", engineAPIKeyKey), false
	}
	return key, 0, true
}

// urlPort is the port a URL names, or "" when it names none.
func urlPort(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Port()
}
