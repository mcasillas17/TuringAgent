// Package servicecmdtest checks a service image's Dockerfile against the
// checks its binary registers.
package servicecmdtest

import (
	"os"
	"strings"
	"testing"
)

// FinalStageLabel returns the value of `LABEL <key>="<value>"` in the final
// stage of the Dockerfile at dockerfilePath, the only stage whose labels the
// built image carries. It fails the test when the final stage has no such
// line, has more than one, or does not quote the value.
func FinalStageLabel(t testing.TB, dockerfilePath, key string) string {
	t.Helper()
	data, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("read %s: %v", dockerfilePath, err)
	}
	lines := strings.Split(string(data), "\n")
	finalStage := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "FROM ") {
			finalStage = i
		}
	}
	if finalStage == -1 {
		t.Fatalf("%s has no FROM line", dockerfilePath)
	}
	prefix := "LABEL " + key + "="
	var values []string
	for _, line := range lines[finalStage+1:] {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		raw := strings.TrimPrefix(trimmed, prefix)
		if len(raw) < 2 || !strings.HasPrefix(raw, `"`) || !strings.HasSuffix(raw, `"`) {
			t.Fatalf("%s: %s value must be double-quoted, got %s", dockerfilePath, key, raw)
		}
		values = append(values, raw[1:len(raw)-1])
	}
	if len(values) != 1 {
		t.Fatalf("%s: final stage has %d %s labels, want exactly 1", dockerfilePath, len(values), key)
	}
	return values[0]
}
