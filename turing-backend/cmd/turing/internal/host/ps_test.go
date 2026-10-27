package host

import (
	"context"
	"slices"
	"testing"
)

func psRunner(stdout string, code int) *scriptedRunner {
	return &scriptedRunner{reply: func(Command) reply {
		return reply{stdout: stdout, stderr: "boom", code: code}
	}}
}

func TestPsParsesJSONLinesAndAnArray(t *testing.T) {
	want := []ServiceState{
		{Service: "turing-orchestrator", State: "running", Health: "healthy"},
		{Service: "turing-mcp-files", State: "exited", Health: ""},
	}
	lines := `{"Service":"turing-orchestrator","State":"running","Health":"healthy","Name":"x-1"}` + "\n\n" +
		`{"Service":"turing-mcp-files","State":"exited","Health":"","ExitCode":1}` + "\n"
	array := `[{"Service":"turing-orchestrator","State":"running","Health":"healthy"},` +
		`{"Service":"turing-mcp-files","State":"exited","Health":""}]`
	for name, output := range map[string]string{
		"json lines":          lines,
		"json lines no final": lines[:len(lines)-1],
		"array":               array,
		"padded array":        "\n  " + array + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			runner := psRunner(output, 0)
			got, err := Ps(context.Background(), Compose{Runner: runner, BackendDir: "/b"})
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("Ps = %+v, %v; want %+v", got, err, want)
			}
			wantArgs := []string{"ps", "--all", "--format", "json"}
			if !slices.Equal(runner.calls[0].Args, wantArgs) || runner.calls[0].Name != "./scripts/compose.sh" {
				t.Fatalf("command = %q %q", runner.calls[0].Name, runner.calls[0].Args)
			}
		})
	}
}

func TestPsHasNoServicesWhenComposeHasNoContainers(t *testing.T) {
	for _, output := range []string{"", "\n", "[]"} {
		got, err := Ps(context.Background(), Compose{Runner: psRunner(output, 0), BackendDir: "/b"})
		if err != nil || len(got) != 0 {
			t.Fatalf("Ps(%q) = %+v, %v; want none", output, got, err)
		}
	}
}

func TestPsFailsWhenComposeFailsOrPrintsSomethingElse(t *testing.T) {
	for name, runner := range map[string]*scriptedRunner{
		"exit status": psRunner("", 1),
		"not json":    psRunner("no configuration file provided\n", 0),
		"cut short":   psRunner(`{"Service":"turing-orch`, 0),
		"mixed":       psRunner(`{"Service":"a"}`+"\nnope\n", 0),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Ps(context.Background(), Compose{Runner: runner, BackendDir: "/b"}); err == nil {
				t.Fatal("Ps did not fail")
			}
		})
	}
}
