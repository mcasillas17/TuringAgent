package cli

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/machine"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/registry"
)

// maxMenuModels keeps an engine with a long list from filling the screen;
// "Another model name" takes any of the rest.
const maxMenuModels = 20

// localChoice is one way to set the local engine: the engine, its address as
// the containers reach it, the model, and the label the app shows ("" for the
// usual one).
type localChoice struct {
	engine, url, model, label string
}

func ollamaDefaults() localChoice {
	return localChoice{engine: engineOllama, url: defaultEngineURL, model: defaultLocalModel}
}

func foundChoice(found foundEngine, model string) localChoice {
	return localChoice{engine: found.Engine, url: found.ContainerURL, model: model, label: usualLabel(found.Engine, found.ContainerURL)}
}

// usualLabel is the label an engine gets unless one is given: ds4 for an
// OpenAI-compatible engine on ds4's port, otherwise none, which the app shows
// as Ollama or OpenAI-compatible.
func usualLabel(engine, containerURL string) string {
	if engine == engineOpenAI && urlPort(containerURL) == ds4Port {
		return labelDS4
	}
	return ""
}

// configuredEngine is the local engine .env sets, as the runtime resolves it.
// usable is false when the runtime would refuse it; the values are then .env's
// own.
func configuredEngine(file *envfile.File) (choice localChoice, usable bool) {
	label := envValue(file, engineLabelKey)
	settings, err := resolveEngine(file)
	if err != nil {
		return localChoice{engine: envValue(file, engineKindKey), url: envValue(file, engineURLKey), model: envValue(file, localModelKey), label: label}, false
	}
	return localChoice{engine: settings.engine, url: settings.url, model: settings.model, label: label}, true
}

// isCurrent reports whether found is the engine current names.
func isCurrent(found foundEngine, current *localChoice) bool {
	if current == nil || found.Engine != current.engine {
		return false
	}
	canonical, err := containerEngineURL(current.url)
	return err == nil && canonical == found.ContainerURL
}

// chooseLocalEngine shows what answers on this machine and what suits it,
// then takes the local engine from the flags, the person, the current
// choice, or the recommendation, in that order.
func (s *setupRun) chooseLocalEngine(ctx context.Context) error {
	facts := machine.Gather(s.Machine)
	recommendation := machine.Recommend(facts)
	route := s.route(ctx)
	// On a first run .env holds only the shipped example, which configures
	// nothing yet.
	var configured *envfile.File
	if !s.firstRun {
		configured = s.file
	}
	engines := s.discoverVia(ctx, configured, "", route)
	s.printf("\n")
	s.printDiscovery(facts, recommendation, engines, route, "")

	effective, usable := configuredEngine(s.file)
	var current *localChoice
	if !s.firstRun && usable {
		current = &effective
	}
	var choice localChoice
	switch {
	case s.flags.engine != "":
		choice = s.flagChoice()
	case s.interactive:
		picked, err := s.askEngine(ctx, recommendation, engines, route, current)
		if err != nil {
			return err
		}
		choice = picked
	case !s.firstRun:
		return nil
	default:
		choice = s.firstRunChoice(recommendation, engines)
	}
	s.setEngine(effective, choice)
	return nil
}

func (s *setupRun) flagChoice() localChoice {
	choice := localChoice{engine: s.flags.engine, url: s.flags.url, model: s.flags.model, label: s.flags.label}
	if choice.engine == engineOllama {
		choice.url = cmp.Or(choice.url, defaultEngineURL)
		choice.model = cmp.Or(choice.model, defaultLocalModel)
	}
	choice.label = cmp.Or(choice.label, usualLabel(choice.engine, choice.url))
	return choice
}

// firstRunChoice is the engine --yes takes on a first run: the recommended
// one when it is the one engine answering and its model is clear, otherwise
// Ollama's defaults, with the command that switches to the recommendation.
func (s *setupRun) firstRunChoice(recommendation machine.Recommendation, engines []foundEngine) localChoice {
	if len(engines) == 1 {
		found := engines[0]
		if found.Problem == "" && found.Engine == recommendation.Engine && found.Label == recommendation.Label {
			if model, ok := modelToSuggest(found, recommendation.PreferredModel); ok {
				return foundChoice(found, model)
			}
		}
	}
	if recommendation.Engine != engineOllama {
		s.printf("Keeping Ollama with %s: --yes takes the recommended engine only when it is the one engine answering and its model is clear.\n", defaultLocalModel)
		s.printf("To use %s instead, run: %s\n", recommendation.Label, recommendedUse(recommendation, engines))
	}
	return ollamaDefaults()
}

// recommendedUse is the engine use command for the recommendation: the one
// detect suggests when it found the engine, otherwise one for its usual
// address and preferred model.
func recommendedUse(recommendation machine.Recommendation, engines []foundEngine) string {
	if example, ok := exampleUse(recommendation, engines); ok {
		return example
	}
	address := defaultEngineURL
	if recommendation.Label == labelDS4 {
		address = ds4EngineURL
	}
	return fmt.Sprintf("./turing engine use %s --url %s --model %s", recommendation.Engine, address, recommendation.PreferredModel)
}

// setEngine records the changes that make .env's local engine choice. What
// becomes of the engine's key is decided when the change is saved.
func (s *setupRun) setEngine(effective, choice localChoice) {
	s.set(engineKindKey, choice.engine, effective.engine)
	s.set(engineURLKey, choice.url, effective.url)
	s.set(localModelKey, choice.model, effective.model)
	s.set(engineLabelKey, choice.label, effective.label)
	s.engineChoice = &choice
}

// engineOption is one line of the engine menu: an engine that answered, whose
// models are offered next; a choice taken as it is; or, with neither, another
// address.
type engineOption struct {
	text  string
	marks []string
	found *foundEngine
	fixed *localChoice
}

// engineOptions are the engine menu and its default: the current choice on a
// rerun, else the recommended engine when it answered usably, else Ollama.
func engineOptions(recommendation machine.Recommendation, engines []foundEngine, current *localChoice) ([]engineOption, int) {
	var options []engineOption
	recommendedAt, currentAt, usableRecommendedAt := 0, 0, 0
	add := func(option engineOption, recommended, isCurrentChoice bool) {
		options = append(options, option)
		at := len(options)
		if recommended && recommendedAt == 0 {
			recommendedAt = at
			options[at-1].marks = append(options[at-1].marks, "recommended")
			if option.found == nil || option.found.Problem == "" {
				usableRecommendedAt = at
			}
		}
		if isCurrentChoice && currentAt == 0 {
			currentAt = at
			options[at-1].marks = append(options[at-1].marks, "current")
		}
		if option.found != nil && option.found.Problem != "" {
			options[at-1].marks = append(options[at-1].marks, "see the note above")
		}
	}
	for i := range engines {
		found := &engines[i]
		add(engineOption{text: found.Label + " at " + found.ContainerURL, found: found},
			found.Engine == recommendation.Engine && found.Label == recommendation.Label, isCurrent(*found, current))
	}
	ollama := ollamaDefaults()
	add(engineOption{text: "Ollama with " + defaultLocalModel, fixed: &ollama},
		recommendation.Engine == engineOllama, current != nil && *current == ollama)
	ollamaAt := len(options)
	if current != nil && currentAt == 0 {
		kept := *current
		address := "an address the runtime refuses"
		if canonical, err := containerEngineURL(kept.url); err == nil {
			address = canonical
		}
		label := cmp.Or(kept.label, engineLabel(kept.engine, urlPort(kept.url)))
		options = append(options, engineOption{text: fmt.Sprintf("%s at %s with %s", label, address, kept.model), marks: []string{"current; it did not answer"}, fixed: &kept})
		currentAt = len(options)
	}
	options = append(options, engineOption{text: "Another address"})
	return options, cmp.Or(currentAt, usableRecommendedAt, ollamaAt)
}

// askEngine asks which engine to use, then its model.
func (s *setupRun) askEngine(ctx context.Context, recommendation machine.Recommendation, engines []foundEngine, route hostRoute, current *localChoice) (localChoice, error) {
	options, preset := engineOptions(recommendation, engines, current)
	for {
		s.printf("\nLocal engine:\n")
		for i, option := range options {
			s.printf("  %d) %s%s\n", i+1, option.text, marked(option.marks))
		}
		n, ok := s.pickNumber(len(options), preset)
		if !ok {
			return localChoice{}, errAnswersEnded
		}
		option := options[n-1]
		switch {
		case option.fixed != nil:
			return *option.fixed, nil
		case option.found != nil:
			return s.askModel(*option.found, current)
		}
		found, ok, err := s.askAddress(ctx, route)
		if err != nil {
			return localChoice{}, err
		}
		if ok {
			return s.askModel(found, current)
		}
	}
}

func marked(marks []string) string {
	if len(marks) == 0 {
		return ""
	}
	return "  (" + strings.Join(marks, ", ") + ")"
}

// askAddress asks for an engine's address and what answers there. ok is
// false when nothing does, and the menu is shown again.
func (s *setupRun) askAddress(ctx context.Context, route hostRoute) (foundEngine, bool, error) {
	for {
		typed, ok := s.askText("Engine address (such as http://localhost:8000/v1)", "")
		if !ok {
			return foundEngine{}, false, errAnswersEnded
		}
		// The rule is shown, never the address: it could carry a password.
		canonical, err := containerEngineURL(typed)
		if err != nil {
			s.printf("%v.\n", err)
			continue
		}
		found, ok := s.probeCandidate(ctx, engineCandidate{url: canonical, explicit: true}, route)
		if !ok {
			s.printf("Nothing answered at %s; start the engine, then choose it again.\n", canonical)
			return foundEngine{}, false, nil
		}
		if found.Problem != "" {
			s.printf("Note: %s\n", found.Problem)
		}
		return found, true, nil
	}
}

// askModel asks which of found's models to use. Keeping the engine .env names
// keeps its address as written and its label.
func (s *setupRun) askModel(found foundEngine, current *localChoice) (localChoice, error) {
	choice := foundChoice(found, "")
	currentModel := ""
	if isCurrent(found, current) {
		choice.url, choice.label, currentModel = current.url, current.label, current.model
	}
	model, ok := s.pickModel(found.Models, machine.PreferredModel(found.Label), currentModel)
	if !ok {
		return localChoice{}, errAnswersEnded
	}
	choice.model = model
	return choice, nil
}

// pickModel offers the engine's models, the preferred one first when it is
// listed, and any other name. The default is current when there is one, else
// the first offered.
func (s *setupRun) pickModel(models []string, preferred, current string) (string, bool) {
	type modelOption struct {
		name  string
		marks []string
	}
	var options []modelOption
	if preferred != "" && slices.Contains(models, preferred) {
		options = append(options, modelOption{name: preferred, marks: []string{"recommended"}})
	}
	for _, model := range models[:min(len(models), maxMenuModels)] {
		if model != preferred {
			options = append(options, modelOption{name: model})
		}
	}
	preset := 1
	if current != "" {
		at := slices.IndexFunc(options, func(option modelOption) bool { return option.name == current })
		if at < 0 {
			mark := "current"
			if !slices.Contains(models, current) {
				mark = "current; not listed by the engine"
			}
			options = append(options, modelOption{name: current})
			at = len(options) - 1
			options[at].marks = append(options[at].marks, mark)
		} else {
			options[at].marks = append(options[at].marks, "current")
		}
		preset = at + 1
	}
	if len(options) == 0 {
		return s.askText("Model name", preferred)
	}

	s.printf("\nModel:\n")
	for i, option := range options {
		s.printf("  %d) %s%s\n", i+1, option.name, marked(option.marks))
	}
	s.printf("  %d) Another model name\n", len(options)+1)
	n, ok := s.pickNumber(len(options)+1, preset)
	switch {
	case !ok:
		return "", false
	case n > len(options):
		return s.askText("Model name", "")
	default:
		return options[n-1].name, true
	}
}

// chooseRemote takes the remote model from the flags, or asks at a terminal.
// Otherwise it is left as it is. Saying no removes its key, which turns it
// off.
func (s *setupRun) chooseRemote(context.Context) error {
	savedKey := envValue(s.file, remoteAPIKeyKey)
	if f := s.flags; f.remoteURL != "" || f.remoteModel != "" || f.remoteKeyFromStdin {
		if f.remoteURL != "" {
			s.set(remoteURLKey, f.remoteURL, envValue(s.file, remoteURLKey))
		}
		if f.remoteModel != "" {
			s.set(remoteModelKey, f.remoteModel, envValue(s.file, remoteModelKey))
		}
		if f.remoteKeyFromStdin {
			s.set(remoteAPIKeyKey, f.remoteKey, savedKey)
		}
		return nil
	}
	if !s.interactive {
		return nil
	}
	if !s.confirm("Add a remote model? Each send to it asks you first.", savedKey != "", false) {
		s.set(remoteAPIKeyKey, "", savedKey)
		return nil
	}
	example := s.readExample()
	address, ok := s.askText("Remote base URL", cmp.Or(envValue(s.file, remoteURLKey), registry.Shipped(example, remoteURLKey)))
	if !ok {
		return errAnswersEnded
	}
	model, ok := s.askText("Remote model", cmp.Or(envValue(s.file, remoteModelKey), registry.Shipped(example, remoteModelKey)))
	if !ok {
		return errAnswersEnded
	}
	prompt := "API key for the remote model (typing is hidden): "
	if savedKey != "" {
		prompt = "API key for the remote model (typing is hidden; Enter keeps the saved one): "
	}
	key, err := s.ReadSecret(prompt)
	if err != nil {
		return fmt.Errorf("could not read the key (%v), so nothing was saved; run ./turing setup again", err)
	}
	s.set(remoteURLKey, address, envValue(s.file, remoteURLKey))
	s.set(remoteModelKey, model, envValue(s.file, remoteModelKey))
	switch {
	case key != "":
		s.set(remoteAPIKeyKey, key, savedKey)
	case savedKey == "":
		s.printf("No key was given, so the remote model stays off.\n")
	}
	return nil
}

// chooseTeam takes the agent team from --team, or asks at a terminal, with
// .env's value as the default.
func (s *setupRun) chooseTeam(context.Context) error {
	saved := envValue(s.file, teamKey)
	on := saved == "true"
	switch {
	case s.flags.team != "":
		on = s.flags.team == "on"
	case s.interactive:
		on = s.confirm("Turn on the agent team (experimental)?", on, false)
	}
	if on != (saved == "true") {
		s.set(teamKey, strconv.FormatBool(on), saved)
	}
	return nil
}

// pickNumber asks for a number from 1 to n, where Enter is preset. ok is
// false when the answers end.
func (e Env) pickNumber(n, preset int) (int, bool) {
	for {
		e.printf("Choose [%d]: ", preset)
		line, err := e.readLine()
		answer := strings.TrimSpace(line)
		if number, convErr := strconv.Atoi(answer); convErr == nil && number >= 1 && number <= n {
			return number, true
		}
		if err != nil {
			e.printf("\n")
			return 0, false
		}
		if answer == "" {
			return preset, true
		}
		e.printf("Type a number from 1 to %d, or press Enter for %d.\n", n, preset)
	}
}

// askText asks for one line, where Enter is preset; with no preset it asks
// until something is typed. ok is false when the answers end.
func (e Env) askText(prompt, preset string) (string, bool) {
	for {
		if preset != "" {
			e.printf("%s [%s]: ", prompt, preset)
		} else {
			e.printf("%s: ", prompt)
		}
		line, err := e.readLine()
		answer := strings.TrimSpace(line)
		switch {
		case answer != "":
			return answer, true
		case err != nil:
			e.printf("\n")
			return "", false
		case preset != "":
			return preset, true
		}
	}
}
