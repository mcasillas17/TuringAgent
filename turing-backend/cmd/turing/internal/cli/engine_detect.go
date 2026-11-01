package cli

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/machine"
)

const (
	// ds4's documented default is 127.0.0.1:8000, which the containers reach as
	// host.docker.internal.
	ds4EngineURL = "http://" + dockerInternalHost + ":8000/v1"
	ds4Port      = "8000"

	labelOllama = "Ollama"
	labelDS4    = "ds4"
	labelOpenAI = "OpenAI-compatible"

	// maxModelsShown and maxModelRunes keep a long list, or an engine that
	// names a model at length, from filling the screen.
	maxModelsShown = 5
	maxModelRunes  = 80

	engineDocs = "docs/architecture/local-engines.md"

	// ds4CacheNote is the engine design's section 5.7.
	ds4CacheNote = "ds4's --kv-disk-dir keeps prompt text outside Turing's deletion lifecycle; " +
		"leave it off for Turing's engine, or accept that deleting a conversation in Turing does not clear it."
)

// foundEngine is a local engine that answered when asked for its models.
type foundEngine struct {
	// Engine is "ollama" or "openai"; Label is what to call it.
	Engine, Label string
	// HostURL is where this machine asked, and ContainerURL where the
	// containers reach it. Neither carries a credential.
	HostURL, ContainerURL string
	// Models are the names it listed.
	Models []string
	// LoopbackOnly is true on Linux when it answers on 127.0.0.1 but not on
	// Docker's bridge gateway, where the containers reach this machine.
	LoopbackOnly bool
	// Problem is one sentence saying why it is found but not yet usable.
	Problem string
}

// engineCandidate is one address to ask for models.
type engineCandidate struct {
	// engine is "" for an address given with --url, whose kind is not known.
	engine string
	// url is the container URL, canonical.
	url string
	// apiKey is sent only to the engine .env configures, never to a candidate
	// found by guessing.
	apiKey     string
	configured bool
	// explicit is true for the address given with --url, whose answer is
	// shown even when it is not a model list.
	explicit bool
}

// hostRoute is how this machine reaches host.docker.internal.
type hostRoute struct {
	target   string
	onBridge bool
	err      error
}

// engineCandidates lists, without repeats, the addresses worth asking: the
// engine .env configures (as status resolves it), Ollama and ds4 at their
// usual addresses, and the address given with --url. A URL the runtime would
// refuse is left out; status and engine use say why. --url naming an address
// already listed marks that one explicit.
func engineCandidates(current *envfile.File, extraURL string) []engineCandidate {
	var candidates []engineCandidate
	seen := make(map[string]int)
	add := func(candidate engineCandidate) {
		canonical, err := containerEngineURL(candidate.url)
		if err != nil {
			return
		}
		if i, ok := seen[canonical]; ok {
			candidates[i].explicit = candidates[i].explicit || candidate.explicit
			return
		}
		seen[canonical] = len(candidates)
		candidate.url = canonical
		candidates = append(candidates, candidate)
	}
	if current != nil {
		if settings, err := resolveEngine(current); err == nil {
			add(engineCandidate{engine: settings.engine, url: settings.url, apiKey: settings.apiKey, configured: true})
		}
	}
	add(engineCandidate{engine: engineOllama, url: defaultEngineURL})
	add(engineCandidate{engine: engineOpenAI, url: ds4EngineURL})
	if extraURL != "" {
		add(engineCandidate{url: extraURL, explicit: true})
	}
	return candidates
}

// discoverEngines asks each candidate for its models, from this machine at the
// address the containers use (see hostTarget), with the proxy-free client that
// refuses redirects. It returns the engines that answered with a model list,
// in the order of the candidates, and the engine .env configures even when it
// answered something else, with Problem saying what. An address nothing
// answers on is not an engine.
func (e Env) discoverEngines(ctx context.Context, current *envfile.File, extraURL string) []foundEngine {
	return e.discoverVia(ctx, current, extraURL, e.route(ctx))
}

func (e Env) route(ctx context.Context) hostRoute {
	var route hostRoute
	route.target, route.onBridge, route.err = e.hostTarget(ctx, dockerInternalHost)
	return route
}

func (e Env) discoverVia(ctx context.Context, current *envfile.File, extraURL string, route hostRoute) []foundEngine {
	var found []foundEngine
	for _, candidate := range engineCandidates(current, extraURL) {
		if engine, ok := e.probeCandidate(ctx, candidate, route); ok {
			found = append(found, engine)
		}
	}
	return found
}

// listing is one try at an engine's model list.
type listing struct {
	models       []string
	address      string
	loopbackOnly bool
	failure      *engineFailure // nil when the models were listed
}

// attempt lists the engine's models at the address the containers use. On
// Linux an engine that does not answer there is asked on 127.0.0.1 too: any
// answer there, a model list or not, means it listens where the containers
// cannot reach it.
func (e Env) attempt(ctx context.Context, settings engineSettings, base *url.URL, route hostRoute) listing {
	address := route.target
	if route.err != nil {
		address = loopback
	}
	models, failure := e.listModels(ctx, settings, base, address)
	if failure == nil {
		return listing{models: models, address: address}
	}
	if route.onBridge && route.err == nil && failure.noAnswer {
		models, again := e.listModels(ctx, settings, base, loopback)
		if again == nil {
			return listing{models: models, address: loopback, loopbackOnly: true}
		}
		if !again.noAnswer {
			return listing{address: loopback, loopbackOnly: true, failure: again}
		}
	}
	return listing{address: address, failure: failure}
}

// probeCandidate asks one candidate. An address given with --url might be
// either kind of engine, so Ollama's list is tried first, and OpenAI's only if
// something answered that was not Ollama's list. The configured engine and the
// --url address are kept when they answer with something else, with Problem
// saying what; a guessed address that does is not an engine.
func (e Env) probeCandidate(ctx context.Context, candidate engineCandidate, route hostRoute) (foundEngine, bool) {
	base, err := url.Parse(candidate.url)
	if err != nil {
		return foundEngine{}, false
	}
	kinds := []string{candidate.engine}
	if candidate.engine == "" {
		kinds = []string{engineOllama, engineOpenAI}
	}
	var lastKind string
	var last *listing
	for _, kind := range kinds {
		got := e.attempt(ctx, engineSettings{engine: kind, apiKey: candidate.apiKey}, base, route)
		if got.failure == nil {
			return foundFrom(kind, candidate.url, base, got, route), true
		}
		if got.failure.noAnswer {
			break
		}
		lastKind, last = kind, &got
	}
	if (candidate.configured || candidate.explicit) && last != nil {
		found := foundFrom(lastKind, candidate.url, base, *last, route)
		problems := []string{"the engine " + last.failure.clause}
		if found.Problem != "" {
			problems = append(problems, found.Problem)
		}
		found.Problem = strings.Join(problems, "; ")
		return found, true
	}
	return foundEngine{}, false
}

func foundFrom(kind, containerURL string, base *url.URL, got listing, route hostRoute) foundEngine {
	found := foundEngine{
		Engine:       kind,
		Label:        engineLabel(kind, base.Port()),
		ContainerURL: containerURL,
		HostURL:      hostURL(base, got.address),
		Models:       cleanNames(got.models),
		LoopbackOnly: got.loopbackOnly,
	}
	switch {
	case got.loopbackOnly:
		found.Problem = "it answers only on " + loopback + ", which the containers cannot reach"
	case route.err != nil:
		found.Problem = "whether the containers can reach it is unknown, because Docker's bridge gateway was not found (" + oneLine(route.err.Error()) + ")"
	}
	return found
}

func engineLabel(kind, port string) string {
	switch {
	case kind == engineOllama:
		return labelOllama
	case port == ds4Port:
		return labelDS4
	default:
		return labelOpenAI
	}
}

// hostURL is base as this machine asks for it at address.
func hostURL(base *url.URL, address string) string {
	host := *base
	host.Host = joinHostPort(address, base.Port())
	return host.String()
}

// cleanNames drops what a terminal would act on rather than show, from names
// an engine chose.
func cleanNames(names []string) []string {
	var clean []string
	for _, name := range names {
		name = strings.Map(func(r rune) rune {
			if unicode.IsPrint(r) {
				return r
			}
			return -1
		}, name)
		if name != "" {
			clean = append(clean, name)
		}
	}
	return clean
}

// engineDetect shows what this machine is, which engines answer on it, and
// which one suits it. It changes nothing, so it exits 0 whatever it finds; a
// --url the runtime would refuse is the one failure.
func (e Env) engineDetect(ctx context.Context, args []string) int {
	set := e.flagSet("engine detect")
	extraURL := set.String("url", "", "")
	if code, ok := e.parseFlags(set, args); !ok {
		return code
	}
	urlGiven := false
	set.Visit(func(f *flag.Flag) { urlGiven = urlGiven || f.Name == "url" })
	if urlGiven {
		if _, err := containerEngineURL(*extraURL); err != nil {
			return e.fail("--url: %v", err)
		}
	}

	facts := machine.Gather(e.Machine)
	recommendation := machine.Recommend(facts)
	current, dotEnvNote := e.detectDotEnv()
	route := e.route(ctx)
	engines := e.discoverVia(ctx, current, *extraURL, route)

	e.printDiscovery(facts, recommendation, engines, route, dotEnvNote)
	_, _ = fmt.Fprintln(e.Stdout, "To use it: ./turing engine use <engine> --url <url> --model <model>, or ./turing setup")
	if example, ok := exampleUse(recommendation, engines); ok {
		_, _ = fmt.Fprintf(e.Stdout, "For example: %s\n", example)
	}
	return 0
}

// printDiscovery shows what this machine is, the engines that answered and
// the one that suits it, with Linux's bridge advice and the ds4 cache note:
// what detect shows, and what setup shows before it asks. dotEnvNote, when
// set, says why the engine .env configures was not asked.
func (e Env) printDiscovery(facts machine.Facts, recommendation machine.Recommendation, engines []foundEngine, route hostRoute, dotEnvNote string) {
	say := func(format string, args ...any) { _, _ = fmt.Fprintf(e.Stdout, format+"\n", args...) }
	say("This machine: %s", describeMachine(facts))
	for _, problem := range facts.Problems {
		say("  Note: %s", problem)
	}
	if dotEnvNote != "" {
		say("  Note: %s; the engine it configures was not asked", dotEnvNote)
	}

	if len(engines) == 0 {
		say("Engines answering: none")
	} else {
		say("Engines answering:")
		for _, engine := range engines {
			line := fmt.Sprintf("  %s: %s (%s)", engine.Label, engine.ContainerURL, modelsText(engine.Models))
			if engine.Problem != "" {
				line += "; " + engine.Problem
			}
			say("%s", line)
		}
	}

	backend := ""
	if recommendation.Backend != "" {
		backend = " (" + recommendation.Backend + ")"
	}
	say("Recommended: %s%s with %s (see %s)", recommendation.Label, backend, recommendation.ModelHint, engineDocs)
	say("  Why: %s", recommendation.Reason)

	if e.GOOS == "linux" {
		printBridgeAdvice(say, route, engines)
	}
	if recommendation.Label == labelDS4 || slices.ContainsFunc(engines, func(f foundEngine) bool { return f.Engine == engineOpenAI }) {
		say("Note: %s", ds4CacheNote)
	}
}

// detectDotEnv reads .env if there is one. detect works before the first
// setup, when there is none; one it cannot read is reported, not fatal.
func (e Env) detectDotEnv() (file *envfile.File, note string) {
	file, err := e.readDotEnv()
	switch {
	case err == nil:
		return file, ""
	case errors.Is(err, errNoDotEnv):
		return nil, ""
	default:
		return nil, err.Error()
	}
}

// printBridgeAdvice says where, on Linux, an engine must listen to be reached
// from the containers, and how to make each one that does not.
func printBridgeAdvice(say func(string, ...any), route hostRoute, engines []foundEngine) {
	if route.err != nil {
		say("Docker's bridge gateway was not found (%s); start Docker, then run this again to see where an engine must listen.", oneLine(route.err.Error()))
		return
	}
	say("Docker's bridge gateway is %s: on Linux the containers reach this machine there, as %s, so an engine must listen on it.", route.target, dockerInternalHost)
	for _, engine := range engines {
		if engine.LoopbackOnly {
			say("  To reach %s from the containers, %s.", engine.Label, bindAdvice(engine.Engine, route.target))
		}
	}
}

// exampleUse is the `engine use` command for the engine found that matches the
// recommendation, by kind and by label, when one is usable. It names the
// recommended model when the engine lists it, the engine's only model when it
// lists one, and otherwise leaves the choice to the owner. A name the engine
// chose goes into the command only when a shell would take it as one plain
// word.
func exampleUse(recommendation machine.Recommendation, engines []foundEngine) (string, bool) {
	for _, engine := range engines {
		if engine.Engine != recommendation.Engine || engine.Label != recommendation.Label || engine.Problem != "" {
			continue
		}
		model, ok := modelToSuggest(engine, recommendation.PreferredModel)
		if !ok || !safeModelName.MatchString(model) {
			model = "<model>"
		}
		return fmt.Sprintf("./turing engine use %s --url %s --model %s", engine.Engine, engine.ContainerURL, model), true
	}
	return "", false
}

// modelToSuggest is the model to name for an engine without asking: the
// preferred one when it lists it, or its only model when it lists one.
func modelToSuggest(engine foundEngine, preferred string) (string, bool) {
	switch {
	case preferred != "" && slices.Contains(engine.Models, preferred):
		return preferred, true
	case len(engine.Models) == 1:
		return engine.Models[0], true
	default:
		return "", false
	}
}

// safeModelName is a model name that can be pasted into a shell as it is.
var safeModelName = regexp.MustCompile(`^[A-Za-z0-9._:/@-]{1,80}$`)

// describeMachine is the one line `engine detect` opens with.
func describeMachine(f machine.Facts) string {
	system := cmp.Or(map[string]string{"darwin": "macOS", "linux": "Linux"}[f.OS], f.OS, "an unknown operating system")
	processor := f.CPU
	switch {
	case processor != "":
	case f.AppleSilicon:
		processor = "Apple Silicon"
	default:
		processor = cmp.Or(f.Arch, "an unknown processor")
	}
	memory := "memory unknown"
	if f.MemoryBytes > 0 {
		const gib = uint64(1) << 30
		memory = fmt.Sprintf("%d GB", (f.MemoryBytes+gib/2)/gib)
	}
	return strings.Join([]string{system, processor, memory}, ", ")
}

// modelsText names up to five of an engine's models.
func modelsText(models []string) string {
	shown := make([]string, 0, maxModelsShown)
	for _, model := range models[:min(len(models), maxModelsShown)] {
		if runes := []rune(model); len(runes) > maxModelRunes {
			model = string(runes[:maxModelRunes]) + "..."
		}
		shown = append(shown, model)
	}
	switch {
	case len(models) == 0:
		return "no models listed"
	case len(models) > maxModelsShown:
		return fmt.Sprintf("models: %s, and %d more", strings.Join(shown, ", "), len(models)-maxModelsShown)
	default:
		return "models: " + strings.Join(shown, ", ")
	}
}
