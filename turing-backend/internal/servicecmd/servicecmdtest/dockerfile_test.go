package servicecmdtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fatalRecorder stands in for a testing.TB so a test can watch
// FinalStageLabel fail without failing itself. Fatalf panics with the
// recorder to stop FinalStageLabel the way a real Fatalf would.
type fatalRecorder struct {
	testing.TB
	message string
}

func (f *fatalRecorder) Helper() {}

func (f *fatalRecorder) Fatalf(format string, args ...any) {
	f.message = fmt.Sprintf(format, args...)
	panic(f)
}

func writeDockerfile(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Dockerfile")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func labelOrFailure(path, key string) (value string, failure *fatalRecorder) {
	recorder := &fatalRecorder{}
	defer func() {
		if recovered := recover(); recovered != nil {
			if recovered != recorder {
				panic(recovered)
			}
			failure = recorder
		}
	}()
	return FinalStageLabel(recorder, path, key), nil
}

func TestFinalStageLabelReadsOnlyTheFinalStage(t *testing.T) {
	path := writeDockerfile(t,
		"FROM golang:1.27-bookworm AS build",
		`LABEL org.turing.checks="build-stage"`,
		"RUN go build ./...",
		"",
		"FROM debian:bookworm-slim",
		`LABEL org.turing.checks="config,engine"`,
		`ENTRYPOINT ["/app/binary"]`,
	)
	value, failure := labelOrFailure(path, "org.turing.checks")
	if failure != nil {
		t.Fatalf("FinalStageLabel failed: %s", failure.message)
	}
	if value != "config,engine" {
		t.Fatalf("FinalStageLabel = %q, want config,engine", value)
	}
}

func TestFinalStageLabelFailsWhenOnlyAnEarlierStageHasTheLabel(t *testing.T) {
	path := writeDockerfile(t,
		"FROM golang:1.27-bookworm AS build",
		`LABEL org.turing.checks="config"`,
		"FROM debian:bookworm-slim",
		`ENTRYPOINT ["/app/binary"]`,
	)
	if _, failure := labelOrFailure(path, "org.turing.checks"); failure == nil {
		t.Fatal("FinalStageLabel accepted a label outside the final stage")
	}
}

func TestFinalStageLabelFailsOnADuplicateInTheFinalStage(t *testing.T) {
	path := writeDockerfile(t,
		"FROM debian:bookworm-slim",
		`LABEL org.turing.checks="config"`,
		`LABEL org.turing.checks="config,engine"`,
	)
	if _, failure := labelOrFailure(path, "org.turing.checks"); failure == nil {
		t.Fatal("FinalStageLabel accepted two labels in the final stage")
	}
}

func TestFinalStageLabelFailsOnAnUnquotedValue(t *testing.T) {
	path := writeDockerfile(t,
		"FROM debian:bookworm-slim",
		`LABEL org.turing.checks=config`,
	)
	if _, failure := labelOrFailure(path, "org.turing.checks"); failure == nil {
		t.Fatal("FinalStageLabel accepted an unquoted value")
	}
}
