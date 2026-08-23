package repository

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var (
	agentRunSetClause = regexp.MustCompile(
		`(?is)\bUPDATE\s+(?:OR\s+(?:ROLLBACK|ABORT|REPLACE|FAIL|IGNORE)\s+)?` +
			`(?:[a-z_][a-z0-9_]*\.)?agent_runs\b` +
			`(?:\s+(?:AS\s+)?[a-z_][a-z0-9_]*)?\s+SET\s+` +
			`(.*?)(?:\s+WHERE\b|;|"|` + "`" + `)`,
	)
	runStatusAssignment = regexp.MustCompile(`(?is)(?:^|,)\s*status\s*=`)
	runStatusNoop       = regexp.MustCompile(`(?is)^status\s*=\s*status$`)
)

func writesAgentRunStatus(source string) bool {
	for _, match := range agentRunSetClause.FindAllStringSubmatch(source, -1) {
		assignments := strings.TrimSpace(match[1])
		if runStatusNoop.MatchString(assignments) {
			continue
		}
		if runStatusAssignment.MatchString(assignments) {
			return true
		}
	}
	return false
}

func TestRunStatusWriteMatcherCatchesStatusAnywhereInSetClause(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   bool
	}{
		{
			name:   "status follows another assignment",
			source: "UPDATE agent_runs SET execution_state = 'uncertain', status = 'recovering' WHERE id = ?",
			want:   true,
		},
		{
			name:   "status appears only in the predicate",
			source: "UPDATE agent_runs SET execution_state = 'uncertain' WHERE id = ? AND status = 'failed'",
			want:   false,
		},
		{
			name:   "liveness no-op is not lifecycle authority",
			source: "UPDATE agent_runs SET status = status WHERE id = ? AND status = 'running'",
			want:   false,
		},
		{
			name:   "aliased table",
			source: "UPDATE agent_runs AS r SET status = 'recovering' WHERE r.id = ?",
			want:   true,
		},
		{
			name:   "conflict clause",
			source: "UPDATE OR REPLACE agent_runs SET status = 'failed' WHERE id = ?",
			want:   true,
		},
		{
			name:   "schema qualified table",
			source: "UPDATE main.agent_runs SET status = 'cancelled' WHERE id = ?",
			want:   true,
		},
		{
			name:   "statement without predicate",
			source: "UPDATE agent_runs SET status = 'failed';",
			want:   true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := writesAgentRunStatus(testCase.source); got != testCase.want {
				t.Fatalf("status write match = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestCanonicalTransitionOwnsEveryProductionRunStatusWrite(t *testing.T) {
	for _, path := range packageSourceFiles(t) {
		if path == "run_state.go" {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if writesAgentRunStatus(string(body)) {
			t.Errorf("%s writes agent_runs.status outside the canonical transition core", path)
		}
	}
}
