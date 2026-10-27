package cli

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
)

// engineTimeout bounds one request for an engine's model list.
const engineTimeout = 3 * time.Second

const (
	engineOllama = "ollama"
	engineOpenAI = "openai"

	// These are the values the services fall back to when .env leaves a setting
	// empty.
	defaultEngineURL  = "http://host.docker.internal:11434"
	defaultLocalModel = "qwen2.5:7b"

	dockerInternalHost = "host.docker.internal"
	loopback           = "127.0.0.1"
	maxModelListBytes  = 4 << 20
)

// engineResult is what asking the local engine for its models found.
type engineResult struct {
	Engine string // "ollama" or "openai"
	// URL is the engine's address as the containers use it, without any
	// credentials in it.
	URL    string
	Model  string   // the model the runtime will ask for
	Models []string // the names the engine listed
	Listed bool     // Model is among Models
	// Warning is set when the engine answers, but not where the containers
	// reach it.
	Warning string
	// Problem is one sentence, ending in the next step, when the engine could
	// not be asked or did not give a model list.
	Problem string
}

func (r engineResult) ok() bool { return r.Problem == "" }

// engineSettings is the local engine as the services resolve it from .env.
type engineSettings struct {
	engine, urlKey, url, model, apiKey string
}

// resolveEngine reads the local engine's settings the way the runtime does:
// an empty engine is Ollama, an empty Ollama URL falls back to the deprecated
// OLLAMA_BASE_URL and then to the usual address, and the model falls back to
// OLLAMA_MODEL and then to a default. An OpenAI-compatible engine has no
// usual address, so it must name one.
func resolveEngine(file *envfile.File) (engineSettings, error) {
	get := func(name string) string {
		value, _ := file.Get(name)
		return value
	}
	settings := engineSettings{
		engine: get("TURING_LOCAL_ENGINE"),
		urlKey: "TURING_LOCAL_ENGINE_URL",
		apiKey: get("TURING_LOCAL_ENGINE_API_KEY"),
	}
	if settings.engine == "" {
		settings.engine = engineOllama
	}
	if settings.engine != engineOllama && settings.engine != engineOpenAI {
		return settings, errors.New("TURING_LOCAL_ENGINE must be ollama or openai; correct it in .env")
	}

	settings.url = get(settings.urlKey)
	if settings.engine == engineOllama && settings.url == "" && get("OLLAMA_BASE_URL") != "" {
		settings.urlKey, settings.url = "OLLAMA_BASE_URL", get("OLLAMA_BASE_URL")
	}
	if settings.url == "" {
		if settings.engine == engineOpenAI {
			return settings, errors.New("TURING_LOCAL_ENGINE_URL is required when TURING_LOCAL_ENGINE is openai; set it in .env")
		}
		settings.url = defaultEngineURL
	}

	settings.model = get("TURING_LOCAL_MODEL")
	if settings.model == "" {
		settings.model = get("OLLAMA_MODEL")
	}
	if settings.model == "" {
		settings.model = defaultLocalModel
	}
	return settings, nil
}

// engineProbe asks the local engine for its models from this machine, at the
// address the containers use: host.docker.internal is this machine's loopback
// under Docker Desktop and Docker's bridge gateway on Linux.
func (e Env) engineProbe(ctx context.Context, file *envfile.File) engineResult {
	settings, err := resolveEngine(file)
	result := engineResult{Engine: settings.engine, Model: settings.model}
	if err != nil {
		result.Problem = err.Error()
		return result
	}
	configured, err := url.Parse(settings.url)
	if err != nil || (configured.Scheme != "http" && configured.Scheme != "https") || configured.Hostname() == "" {
		result.Problem = fmt.Sprintf("%s must be an http or https URL with a host; correct it in .env", settings.urlKey)
		return result
	}
	result.URL = withoutCredentials(configured)

	target := configured.Hostname()
	onLinuxBridge := target == dockerInternalHost && e.GOOS == "linux"
	switch {
	case onLinuxBridge:
		target, err = e.BridgeGateway(ctx)
		var noIPv4 noIPv4GatewayError
		if errors.As(err, &noIPv4) {
			result.Problem = fmt.Sprintf("%v, which %s leads to on Linux; give the default bridge an IPv4 subnet (docker network inspect bridge shows it), then run ./turing status",
				noIPv4, dockerInternalHost)
			return result
		}
		if err != nil {
			result.Problem = fmt.Sprintf("could not find Docker's bridge gateway, which %s leads to on Linux (%s); check that Docker is running, then run ./turing status",
				dockerInternalHost, oneLine(err.Error()))
			return result
		}
	case target == dockerInternalHost:
		target = loopback
	}

	models, failure := e.listModels(ctx, settings, configured, target)
	if failure == nil {
		result.Models = models
		result.Listed = modelListed(settings.engine, settings.model, models)
		return result
	}

	if onLinuxBridge && failure.noAnswer {
		if _, again := e.listModels(ctx, settings, configured, loopback); again == nil {
			result.Warning = fmt.Sprintf("the engine listens only on %s, which the containers cannot reach: it answered there but not on %s, where they reach this machine; %s",
				loopback, target, bindAdvice(settings.engine, target))
			result.Problem = fmt.Sprintf("the engine %s, though it answers on %s (the fix is under Warnings); then run ./turing status", failure.clause, loopback)
			return result
		}
	}
	result.Problem = fmt.Sprintf("the engine %s; start it on this machine, or correct TURING_LOCAL_ENGINE and %s in .env, then run ./turing status",
		failure.clause, settings.urlKey)
	return result
}

// bindAdvice is how to make the engine listen on address. Ollama has no
// listen flag: it reads OLLAMA_HOST. ds4's server takes --host.
func bindAdvice(engine, address string) string {
	if engine == engineOllama {
		return fmt.Sprintf("set OLLAMA_HOST=%s for the Ollama service (for example with sudo systemctl edit ollama) and restart it", address)
	}
	return fmt.Sprintf("start it with --host %s", address)
}

// engineFailure is why the engine's model list could not be had. clause
// finishes the sentence "the engine ..."; noAnswer is true when nothing
// answered at all, as opposed to an answer that was not a model list.
type engineFailure struct {
	clause   string
	noAnswer bool
}

// listModels asks the engine at address for its models.
func (e Env) listModels(ctx context.Context, settings engineSettings, configured *url.URL, address string) ([]string, *engineFailure) {
	probe := *configured
	probe.Host = joinHostPort(address, configured.Port())
	base := strings.TrimRight(configured.Path, "/")
	if settings.engine == engineOllama {
		probe.Path = base + "/api/tags"
	} else {
		probe.Path = base + "/models"
	}
	probe.RawPath, probe.RawQuery, probe.Fragment = "", "", ""
	shown := withoutCredentials(&probe)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, probe.String(), nil)
	if err != nil {
		return nil, &engineFailure{clause: fmt.Sprintf("could not be asked at %s", shown)}
	}
	request.Header.Set("Accept", "application/json")
	if settings.engine == engineOpenAI && settings.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+settings.apiKey)
	}

	response, err := e.EngineClient.Do(request)
	if err != nil {
		cause, timedOut := requestCause(err)
		if timedOut {
			return nil, &engineFailure{clause: fmt.Sprintf("did not answer in time at %s", shown), noAnswer: true}
		}
		return nil, &engineFailure{clause: fmt.Sprintf("did not answer at %s (%s)", shown, cause), noAnswer: true}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, &engineFailure{clause: fmt.Sprintf("answered HTTP %d at %s", response.StatusCode, shown)}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxModelListBytes))
	if err != nil {
		return nil, &engineFailure{clause: fmt.Sprintf("did not finish answering at %s", shown)}
	}
	models, ok := parseModelList(settings.engine, body)
	if !ok {
		return nil, &engineFailure{clause: fmt.Sprintf("did not answer with a model list at %s", shown)}
	}
	return models, nil
}

// parseModelList reads the names out of Ollama's /api/tags or an
// OpenAI-compatible /models answer. ok is false for anything else.
func parseModelList(engine string, body []byte) (models []string, ok bool) {
	if engine == engineOllama {
		var list struct {
			Models *[]struct{ Name, Model string } `json:"models"`
		}
		if json.Unmarshal(body, &list) != nil || list.Models == nil {
			return nil, false
		}
		for _, entry := range *list.Models {
			models = append(models, cmp.Or(entry.Name, entry.Model))
		}
		return models, true
	}
	var list struct {
		Data *[]struct{ ID string } `json:"data"`
	}
	if json.Unmarshal(body, &list) != nil || list.Data == nil {
		return nil, false
	}
	for _, entry := range *list.Data {
		models = append(models, entry.ID)
	}
	return models, true
}

// modelListed reports whether the engine lists want. Ollama also lists an
// untagged name as its latest tag.
func modelListed(engine, want string, models []string) bool {
	for _, model := range models {
		if model == want || (engine == engineOllama && !strings.Contains(want, ":") && model == want+":latest") {
			return true
		}
	}
	return false
}

// requestCause is why a request failed, without the client's wrapper around
// the URL, and whether it ran out of time.
func requestCause(err error) (cause string, timedOut bool) {
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "", true
	}
	var wrapped *url.Error
	if errors.As(err, &wrapped) {
		err = wrapped.Err
	}
	return oneLine(err.Error()), false
}

// withoutCredentials is u as text with any user and password left out.
func withoutCredentials(u *url.URL) string {
	plain := *u
	plain.User = nil
	return plain.String()
}

// joinHostPort is host and port as a URL's host, which brackets an IPv6 host
// even when there is no port.
func joinHostPort(address, port string) string {
	if port != "" {
		return net.JoinHostPort(address, port)
	}
	if strings.Contains(address, ":") {
		return "[" + address + "]"
	}
	return address
}

// oneLine folds every run of whitespace, newlines included, into one space.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// noIPv4GatewayError is Docker answering, but with no IPv4 gateway for its
// default bridge: host.docker.internal resolves to the IPv4 one.
type noIPv4GatewayError struct {
	reported []string
}

func (e noIPv4GatewayError) Error() string {
	if len(e.reported) == 0 {
		return "docker reports no gateway for its bridge network"
	}
	return fmt.Sprintf("docker reports no IPv4 gateway for its bridge network (only %s)", strings.Join(e.reported, " "))
}

// dockerBridgeGateway returns a lookup of the address of Docker's default
// bridge network, which is where host.docker.internal leads on Linux. A bridge
// with both IPv4 and IPv6 subnets lists both gateways; the IPv4 one is the
// one host-gateway resolves to.
func dockerBridgeGateway(runner host.Runner, backendDir string, environ []string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		var stdout, stderr bytes.Buffer
		code, err := runner.Run(ctx, host.Command{
			Dir:    backendDir,
			Name:   "docker",
			Args:   []string{"network", "inspect", "bridge", "--format", "{{range .IPAM.Config}}{{.Gateway}} {{end}}"},
			Env:    environ,
			Stdout: &stdout,
			Stderr: &stderr,
		})
		switch {
		case err != nil:
			return "", fmt.Errorf("could not run docker: %w", err)
		case code != 0:
			return "", fmt.Errorf("docker network inspect bridge exited %d: %s", code, oneLine(stderr.String()))
		}
		fields := strings.Fields(stdout.String())
		var first netip.Addr
		for _, field := range fields {
			address, err := netip.ParseAddr(field)
			if err != nil {
				return "", fmt.Errorf("docker reports %q as its bridge gateway, which is not an address", field)
			}
			if address.Is4() && !first.IsValid() {
				first = address
			}
		}
		if !first.IsValid() {
			return "", noIPv4GatewayError{reported: fields}
		}
		return first.String(), nil
	}
}
