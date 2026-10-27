package host

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func imageRunner(inspect reply) *scriptedRunner {
	return &scriptedRunner{reply: func(cmd Command) reply {
		if cmd.Name == "docker" {
			return inspect
		}
		return reply{stdout: "infra-turing-orchestrator\n"}
	}}
}

func TestImageChecksReadsTheLabelOfTheImageComposeNames(t *testing.T) {
	runner := imageRunner(reply{stdout: "config\n"})
	compose := Compose{Runner: runner, BackendDir: "/b"}

	checks, err := ImageChecks(context.Background(), compose, "turing-orchestrator")
	if err != nil || !slices.Equal(checks, []string{"config"}) {
		t.Fatalf("ImageChecks = %q, %v", checks, err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %d, want 2: %+v", len(runner.calls), runner.calls)
	}
	first, second := runner.calls[0], runner.calls[1]
	if first.Name != "./scripts/compose.sh" || !slices.Equal(first.Args, []string{"config", "--images", "turing-orchestrator"}) {
		t.Fatalf("first command = %q %q", first.Name, first.Args)
	}
	wantInspect := []string{"image", "inspect", "--format", `{{ index .Config.Labels "org.turing.checks" }}`, "infra-turing-orchestrator"}
	if second.Name != "docker" || !slices.Equal(second.Args, wantInspect) {
		t.Fatalf("second command = %q %q, want docker %q", second.Name, second.Args, wantInspect)
	}
}

func TestImageChecksSplitsAListAndTrimsIt(t *testing.T) {
	checks, err := ImageChecks(context.Background(), Compose{Runner: imageRunner(reply{stdout: " config, engine \n"}), BackendDir: "/b"}, "svc")
	if err != nil || !slices.Equal(checks, []string{"config", "engine"}) {
		t.Fatalf("ImageChecks = %q, %v", checks, err)
	}
}

func TestImageChecksHasNothingToReportForAnImageWithoutTheLabel(t *testing.T) {
	for name, stdout := range map[string]string{
		"no value marker": "<no value>\n",
		"empty":           "\n",
		"nothing":         "",
	} {
		t.Run(name, func(t *testing.T) {
			checks, err := ImageChecks(context.Background(), Compose{Runner: imageRunner(reply{stdout: stdout}), BackendDir: "/b"}, "svc")
			if err != nil || checks != nil {
				t.Fatalf("ImageChecks = %q, %v; want nil, nil", checks, err)
			}
		})
	}
}

func TestImageChecksSaysWhenTheImageIsNotBuilt(t *testing.T) {
	_, err := ImageChecks(context.Background(), Compose{Runner: imageRunner(reply{stderr: "Error: No such image", code: 1}), BackendDir: "/b"}, "svc")
	if !errors.Is(err, ErrImageMissing) {
		t.Fatalf("err = %v, want ErrImageMissing", err)
	}
}

func TestImageChecksDoesNotCallAMissingNameAMissingImage(t *testing.T) {
	failing := &scriptedRunner{reply: func(Command) reply {
		return reply{stderr: "no such service: svc", code: 1}
	}}
	_, err := ImageChecks(context.Background(), Compose{Runner: failing, BackendDir: "/b"}, "svc")
	if err == nil || errors.Is(err, ErrImageMissing) {
		t.Fatalf("err = %v, want a compose error that is not ErrImageMissing", err)
	}
	if !strings.Contains(err.Error(), "no such service") {
		t.Fatalf("err = %q does not carry compose's message", err)
	}
	if len(failing.calls) != 1 {
		t.Fatalf("docker was asked about an image compose never named: %+v", failing.calls)
	}
}

func TestImageChecksRefusesAnAnswerThatIsNotOneImage(t *testing.T) {
	runner := &scriptedRunner{reply: func(Command) reply { return reply{stdout: "\n"} }}
	if _, err := ImageChecks(context.Background(), Compose{Runner: runner, BackendDir: "/b"}, "svc"); err == nil {
		t.Fatal("an empty image name was accepted")
	}
}
