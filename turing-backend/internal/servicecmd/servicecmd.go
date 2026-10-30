// Package servicecmd is the command line both service binaries share. With no
// argument the binary runs its service, exactly as it always has. With
// exactly one check subcommand it runs that check and exits, starting
// nothing. Anything else is refused, so a caller can never start a second
// service by passing an argument the binary does not understand.
//
// An image says which checks its binary knows through the LabelKey label,
// because an image built before this package ignored its arguments and would
// have started the service instead of checking anything.
package servicecmd

import (
	"fmt"
	"io"
	"log"
	"strings"
)

// LabelKey is the image label listing the checks the image's binary knows,
// as LabelValue renders them.
const LabelKey = "org.turing.checks"

// Check is one check subcommand. Run receives the process environment as
// os.Environ returns it, and returns notices to print as warnings, or the
// error that refuses the configuration.
type Check struct {
	Name string
	Run  func(environ []string) (warnings []string, err error)
}

// Subcommand is the argument that runs the check called name.
func Subcommand(name string) string {
	return "check-" + name
}

// LabelValue is the LabelKey value for an image whose binary registers checks.
func LabelValue(checks []Check) string {
	names := make([]string, len(checks))
	for i, check := range checks {
		names[i] = check.Name
	}
	return strings.Join(names, ",")
}

// EnvMap indexes an os.Environ-style list. Each entry splits at its first
// '='; an entry without one is skipped, and a later duplicate wins.
func EnvMap(environ []string) map[string]string {
	env := make(map[string]string, len(environ))
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		env[key] = value
	}
	return env
}

// Main dispatches the command line and returns the process exit code: 0 when
// the service or check succeeded, 1 when it failed, 2 for an argument it does
// not know.
func Main(args, environ []string, checks []Check, serve func() error, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		if err := serve(); err != nil {
			log.New(stderr, "", log.LstdFlags).Print(err)
			return 1
		}
		return 0
	}
	for _, check := range checks {
		if args[0] != Subcommand(check.Name) {
			continue
		}
		if len(args) > 1 {
			return refuse(args[1], checks, stderr)
		}
		return runCheck(check, environ, stdout, stderr)
	}
	return refuse(args[0], checks, stderr)
}

func runCheck(check Check, environ []string, stdout, stderr io.Writer) int {
	warnings, err := check.Run(environ)
	for _, warning := range warnings {
		_, _ = fmt.Fprintf(stderr, "warning: %s\n", warning)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "configuration is valid")
	return 0
}

func refuse(argument string, checks []Check, stderr io.Writer) int {
	subcommands := make([]string, len(checks))
	for i, check := range checks {
		subcommands[i] = Subcommand(check.Name)
	}
	_, _ = fmt.Fprintf(stderr,
		"unknown argument %q; run with no arguments to start the service, or with one of: %s\n",
		argument, strings.Join(subcommands, ", "))
	return 2
}
