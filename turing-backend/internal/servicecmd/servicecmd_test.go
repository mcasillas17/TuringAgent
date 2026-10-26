package servicecmd

import (
	"bytes"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
)

type recorder struct {
	served  bool
	checked bool
	environ []string
}

func (r *recorder) serve(err error) func() error {
	return func() error {
		r.served = true
		return err
	}
}

func (r *recorder) check(warnings []string, err error) Check {
	return Check{Name: "config", Run: func(environ []string) ([]string, error) {
		r.checked = true
		r.environ = environ
		return warnings, err
	}}
}

func TestMainWithNoArgumentsServes(t *testing.T) {
	var r recorder
	var stdout, stderr bytes.Buffer
	code := Main(nil, nil, []Check{r.check(nil, nil)}, r.serve(nil), &stdout, &stderr)
	if code != 0 || !r.served || r.checked {
		t.Fatalf("code = %d, served = %v, checked = %v; want 0, true, false", code, r.served, r.checked)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q; want both empty", stdout.String(), stderr.String())
	}
}

func TestMainReportsAServeErrorWithTheStandardLogFormat(t *testing.T) {
	var r recorder
	var stdout, stderr bytes.Buffer
	code := Main(nil, nil, []Check{r.check(nil, nil)}, r.serve(errors.New("boom")), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} boom\n$`).MatchString(stderr.String()) {
		t.Fatalf("stderr = %q, want the standard logger's timestamp and the error", stderr.String())
	}
}

func TestMainRunsARegisteredCheck(t *testing.T) {
	var r recorder
	var stdout, stderr bytes.Buffer
	environ := []string{"A=1", "B=2"}
	code := Main([]string{"check-config"}, environ, []Check{r.check(nil, nil)}, r.serve(nil), &stdout, &stderr)
	if code != 0 || r.served || !r.checked {
		t.Fatalf("code = %d, served = %v, checked = %v; want 0, false, true", code, r.served, r.checked)
	}
	if stdout.String() != "configuration is valid\n" || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
	if !slices.Equal(r.environ, environ) {
		t.Fatalf("check saw environ %q, want %q", r.environ, environ)
	}
}

func TestMainReportsACheckRefusal(t *testing.T) {
	var r recorder
	var stdout, stderr bytes.Buffer
	check := r.check(nil, errors.New("TURING_X must be a number"))
	code := Main([]string{"check-config"}, nil, []Check{check}, r.serve(nil), &stdout, &stderr)
	if code != 1 || r.served {
		t.Fatalf("code = %d, served = %v; want 1, false", code, r.served)
	}
	if stderr.String() != "TURING_X must be a number\n" || stdout.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestMainPrintsWarningsOnSuccess(t *testing.T) {
	var r recorder
	var stdout, stderr bytes.Buffer
	code := Main([]string{"check-config"}, nil, []Check{r.check([]string{"a", "b"}, nil)}, r.serve(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if stderr.String() != "warning: a\nwarning: b\n" || stdout.String() != "configuration is valid\n" {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestMainRefusesUnknownArguments(t *testing.T) {
	for _, tc := range []struct {
		args      []string
		offending string
	}{
		{args: []string{"-h"}, offending: "-h"},
		{args: []string{"--help"}, offending: "--help"},
		{args: []string{"check-engine"}, offending: "check-engine"},
		{args: []string{"check-config", "--verbose"}, offending: "--verbose"},
		{args: []string{"serve"}, offending: "serve"},
		{args: []string{""}, offending: `""`},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var r recorder
			var stdout, stderr bytes.Buffer
			code := Main(tc.args, nil, []Check{r.check(nil, nil)}, r.serve(nil), &stdout, &stderr)
			if code != 2 || r.served || r.checked {
				t.Fatalf("code = %d, served = %v, checked = %v; want 2, false, false", code, r.served, r.checked)
			}
			if !strings.Contains(stderr.String(), tc.offending) || !strings.Contains(stderr.String(), "check-config") {
				t.Fatalf("stderr = %q, want it to name %s and list check-config", stderr.String(), tc.offending)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestMainListsEveryRegisteredCheckWhenRefusing(t *testing.T) {
	var r recorder
	var stderr bytes.Buffer
	checks := []Check{{Name: "config"}, {Name: "engine"}}
	if code := Main([]string{"nope"}, nil, checks, r.serve(nil), &bytes.Buffer{}, &stderr); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "check-config, check-engine") {
		t.Fatalf("stderr = %q, want both subcommands in order", stderr.String())
	}
}

func TestLabelValueJoinsNamesInOrder(t *testing.T) {
	if got := LabelValue([]Check{{Name: "config"}, {Name: "engine"}}); got != "config,engine" {
		t.Fatalf("LabelValue = %q, want config,engine", got)
	}
	if got := LabelValue([]Check{{Name: "engine"}}); got != "engine" {
		t.Fatalf("LabelValue = %q, want engine", got)
	}
}

func TestSubcommandPrefixesTheName(t *testing.T) {
	if got := Subcommand("config"); got != "check-config" {
		t.Fatalf("Subcommand = %q, want check-config", got)
	}
}

func TestEnvMapSplitsAtTheFirstEqualsAndKeepsTheLastDuplicate(t *testing.T) {
	env := EnvMap([]string{"A=b=c", "EMPTY=", "NOEQUALS", "D=1", "D=2"})
	want := map[string]string{"A": "b=c", "EMPTY": "", "D": "2"}
	if len(env) != len(want) {
		t.Fatalf("EnvMap = %q, want %q", env, want)
	}
	for key, value := range want {
		if got, ok := env[key]; !ok || got != value {
			t.Fatalf("EnvMap[%q] = %q (present %v), want %q", key, got, ok, value)
		}
	}
}
