package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
)

// keyRefusedMessage is what to tell a person whose .env key the running
// orchestrator does not accept.
const keyRefusedMessage = "the key in .env is not the one the running orchestrator started with; run ./turing restart"

type keyRefusedError struct{}

func (keyRefusedError) Error() string { return keyRefusedMessage }
func (keyRefusedError) Unwrap() error { return host.ErrUnauthenticated }

// UnhealthyError is the wait for health running out: Service, by its friendly
// name, is the first one that was not healthy.
type UnhealthyError struct {
	Service string
	Reason  string
	Waited  time.Duration
}

func (e *UnhealthyError) Error() string {
	return fmt.Sprintf("%s is not healthy after %ds (%s); run ./turing logs %s",
		e.Service, int(e.Waited.Seconds()), e.Reason, e.Service)
}

// flagSet is a FlagSet that reports errors through the command's own usage
// error rather than printing its own.
func (e Env) flagSet(command string) *flag.FlagSet {
	set := flag.NewFlagSet(command, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	return set
}

// parseFlags parses args, which must carry no positional arguments. A false
// ok means the exit code to return is already decided.
func (e Env) parseFlags(set *flag.FlagSet, args []string) (code int, ok bool) {
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = io.WriteString(e.Stdout, Usage)
			return 0, false
		}
		return e.usageError("turing %s: %v; run ./turing help", set.Name(), err), false
	}
	if set.NArg() > 0 {
		return e.usageError("turing %s takes no arguments, got %q; run ./turing help", set.Name(), set.Arg(0)), false
	}
	return 0, true
}

func (e Env) start(ctx context.Context, args []string) int {
	set := e.flagSet("start")
	build := set.Bool("build", false, "")
	if code, ok := e.parseFlags(set, args); !ok {
		return code
	}
	port, key, err := e.endpoint()
	if err != nil {
		return e.fail("%v", err)
	}
	return e.bringUp(ctx, port, key, *build)
}

// endpoint reads .env and returns the port and key to ask health with.
func (e Env) endpoint() (port int, key string, err error) {
	file, err := e.readDotEnv()
	if err != nil {
		return 0, "", err
	}
	return appEndpoint(file)
}

// bringUp starts the stack and waits for it to be healthy.
func (e Env) bringUp(ctx context.Context, port int, key string, build bool) int {
	args := []string{"up", "-d"}
	if build {
		args = append(args, "--build")
	}
	if code, err := e.compose().Run(ctx, nil, nil, e.Stdout, e.Stderr, args...); err != nil || code != 0 {
		return e.composeFailed(ctx, code, err, "./turing start")
	}
	_, _ = fmt.Fprintln(e.Stdout, "Waiting for Turing to become healthy...")
	if err := e.waitHealthy(ctx, port, key); err != nil {
		return e.fail("%v", err)
	}
	_, _ = fmt.Fprintln(e.Stdout, "Turing is running. To connect the app, run ./turing connect")
	return 0
}

func (e Env) stop(ctx context.Context, args []string) int {
	if code, ok := e.parseFlags(e.flagSet("stop"), args); !ok {
		return code
	}
	if code := e.takeDown(ctx, "./turing stop"); code != 0 {
		return code
	}
	_, _ = fmt.Fprintln(e.Stdout, "Turing is stopped. It stays stopped after a reboot; run ./turing start to start it again.")
	return 0
}

// takeDown stops the stack; retry is what to run if that fails. It needs no
// .env: stopping is what a person does when the install is broken.
func (e Env) takeDown(ctx context.Context, retry string) int {
	if code, err := e.compose().Run(ctx, nil, nil, e.Stdout, e.Stderr, "stop"); err != nil || code != 0 {
		return e.composeFailed(ctx, code, err, retry)
	}
	return 0
}

func (e Env) restart(ctx context.Context, args []string) int {
	set := e.flagSet("restart")
	build := set.Bool("build", false, "")
	yes := set.Bool("yes", false, "")
	if code, ok := e.parseFlags(set, args); !ok {
		return code
	}
	port, key, err := e.endpoint()
	if err != nil {
		return e.fail("%v", err)
	}
	if !e.confirm("Restarting stops a reply in progress. Restart now?", false, *yes) {
		_, _ = fmt.Fprintln(e.Stdout, "Not restarted.")
		return 1
	}
	if code := e.takeDown(ctx, "./turing restart"); code != 0 {
		return code
	}
	return e.bringUp(ctx, port, key, *build)
}

func (e Env) logs(ctx context.Context, args []string) int {
	follow, service := false, ""
	for _, arg := range args {
		switch {
		case arg == "-f":
			follow = true
		case strings.HasPrefix(arg, "-"):
			return e.usageError("turing logs: unknown option %q; run ./turing help", arg)
		case service != "":
			return e.usageError("turing logs takes one service, got %q and %q; run ./turing help", service, arg)
		default:
			service = arg
		}
	}
	composeArgs := []string{"logs", "--tail", logTailLines}
	if follow {
		composeArgs = append(composeArgs, "-f")
	}
	if service != "" {
		name, ok := composeName(service)
		if !ok {
			return e.usageError("unknown service %q; choose one of %s", service, strings.Join(friendlyNames(), ", "))
		}
		composeArgs = append(composeArgs, name)
	}
	code, err := e.compose().Run(ctx, nil, nil, e.Stdout, e.Stderr, composeArgs...)
	if err != nil {
		return e.fail("could not run ./scripts/compose.sh: %v; check that Docker is installed and this checkout is intact", err)
	}
	return code
}

func composeName(friendly string) (string, bool) {
	for _, service := range serviceNames {
		if service.Friendly == friendly {
			return service.Compose, true
		}
	}
	return "", false
}

// notHealthy returns the first service, by friendly name, that is not
// running with a health of "" or "healthy", and why. A service with several
// containers is fine when any one is.
func notHealthy(states []host.ServiceState) (service, reason string) {
	for _, want := range serviceNames {
		var first *host.ServiceState
		healthy := false
		for i, state := range states {
			if state.Service != want.Compose {
				continue
			}
			if first == nil {
				first = &states[i]
			}
			if state.State == "running" && (state.Health == "" || state.Health == "healthy") {
				healthy = true
			}
		}
		switch {
		case healthy:
		case first == nil:
			return want.Friendly, "it is not running"
		case first.State != "running":
			return want.Friendly, "it is " + first.State
		default:
			return want.Friendly, "its health check says " + first.Health
		}
	}
	return "", ""
}

// waitHealthy polls until the orchestrator answers its health check and every
// service is running and not unhealthy, for as long as HealthTimeout allows.
// It ends early when the orchestrator refuses the key, because waiting will
// not change that.
func (e Env) waitHealthy(ctx context.Context, port int, key string) error {
	parent := ctx
	ctx, cancel := context.WithTimeout(parent, e.HealthTimeout)
	defer cancel()
	compose := e.compose()
	last := &UnhealthyError{Service: "orchestrator", Reason: "it did not answer", Waited: e.HealthTimeout}

	for {
		healthErr := e.Health(ctx, port, key)
		if errors.Is(healthErr, host.ErrUnauthenticated) {
			return keyRefusedError{}
		}
		states, psErr := host.Ps(ctx, compose)

		service, reason := firstProblem(states, psErr, healthErr)
		if service == "" {
			return nil
		}
		// A call cut short by the deadline says nothing about the stack, so
		// the last full look is what gets reported.
		if ctx.Err() != nil {
			return waitEnded(parent, last)
		}
		last = &UnhealthyError{Service: service, Reason: reason, Waited: e.HealthTimeout}

		timer := time.NewTimer(e.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return waitEnded(parent, last)
		case <-timer.C:
		}
	}
}

// firstProblem names the first thing wrong, by service, or "" when the stack
// is healthy. A stack Compose cannot describe, or that Compose calls healthy
// while the orchestrator does not answer, is the orchestrator's problem.
func firstProblem(states []host.ServiceState, psErr, healthErr error) (service, reason string) {
	if psErr != nil {
		return "orchestrator", psErr.Error()
	}
	if service, reason = notHealthy(states); service != "" {
		return service, reason
	}
	if healthErr != nil {
		return "orchestrator", healthErr.Error()
	}
	return "", ""
}

// waitEnded is the error for a wait that stopped before the stack was
// healthy: the caller's own cancellation, or the timeout.
func waitEnded(parent context.Context, last *UnhealthyError) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	return last
}
