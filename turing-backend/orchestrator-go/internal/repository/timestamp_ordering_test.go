package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestFormatTimestampUsesFixedUTCWidthAndOrdersFractionalPrefixes(t *testing.T) {
	base := time.Date(2030, 1, 2, 3, 4, 5, 0, time.FixedZone("non-UTC", -7*60*60))
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{name: "whole_second", at: base, want: "2030-01-02T10:04:05.000000000Z"},
		{name: "first_nanosecond", at: base.Add(time.Nanosecond), want: "2030-01-02T10:04:05.000000001Z"},
		{name: "tenth_second", at: base.Add(100 * time.Millisecond), want: "2030-01-02T10:04:05.100000000Z"},
	}
	var previous string
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := FormatTimestamp(test.at)
			if got != test.want {
				t.Fatalf("FormatTimestamp(%s) = %q, want %q", test.at, got, test.want)
			}
			if previous != "" && got <= previous {
				t.Fatalf("timestamp order %q then %q is not chronological", previous, got)
			}
			previous = got
		})
	}
}

func TestRecoverStaleAssignmentsOrdersLegacyFractionPrefixesChronologically(t *testing.T) {
	tests := []struct {
		name          string
		leaseExpires  string
		cutoff        string
		wantRecovered bool
	}{
		{
			name:          "whole_second_before_fractional_cutoff",
			leaseExpires:  "2030-01-02T03:04:05Z",
			cutoff:        "2030-01-02T03:04:05.000000001Z",
			wantRecovered: true,
		},
		{
			name:          "fractional_expiry_after_whole_second_cutoff",
			leaseExpires:  "2030-01-02T03:04:05.000000001Z",
			cutoff:        "2030-01-02T03:04:05Z",
			wantRecovered: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openTestDB(t)
			repo := New(database)
			ctx := context.Background()
			session, err := repo.CreateSession(ctx, "Fractional lease ordering")
			if err != nil {
				t.Fatal(err)
			}
			enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
				SessionID: session.SessionID, Content: test.name, AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.ClaimNextJobWithLimit(ctx, "general_assistant", "worker-fractional", 1, time.Hour); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(ctx, `
				UPDATE agent_runs
				SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = NULL
				WHERE id = ?
			`, test.leaseExpires, enqueued.RunID); err != nil {
				t.Fatal(err)
			}
			cutoff, err := time.Parse(time.RFC3339Nano, test.cutoff)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.RecoverStaleAssignments(ctx, cutoff); err != nil {
				t.Fatal(err)
			}
			run, err := repo.GetRun(ctx, enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if test.wantRecovered && (run.Status != "queued" || run.ExecutionActive) {
				t.Fatalf("recovery result = %+v, want queued inactive run", run)
			}
			if !test.wantRecovered && (run.Status != "running" || !run.ExecutionActive) {
				t.Fatalf("recovery result = %+v, want running active run", run)
			}
		})
	}
}

func TestTerminalRunOrdersLegacyDependentTimestampsChronologically(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Legacy dependent ordering")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "cancel legacy dependents", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJobWithLimit(ctx, "general_assistant", "worker-legacy", 1, time.Hour); err != nil {
		t.Fatal(err)
	}

	type dependent struct {
		toolCallID string
		createdAt  string
		approvalID string
	}
	dependents := []dependent{
		{toolCallID: "call_legacy_early", createdAt: "2030-01-02T03:04:05.1Z"},
		{toolCallID: "call_legacy_late", createdAt: "2030-01-02T03:04:05.100000001Z"},
	}
	for index := range dependents {
		item := &dependents[index]
		if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
			ToolCallID: item.toolCallID, RunID: enqueued.RunID, Status: "approval_required",
		}, "general_assistant", "files", "files.update", `{}`, "sha256:test"); err != nil {
			t.Fatal(err)
		}
		approval, err := repo.CreateApproval(
			ctx, enqueued.RunID, item.toolCallID, "general_assistant", "files.update",
			`{}`, "sha256:test", "2099-01-01T00:00:00Z",
		)
		if err != nil {
			t.Fatal(err)
		}
		item.approvalID = approval.ApprovalID
		if _, err := database.ExecContext(ctx, `UPDATE tool_calls SET created_at = ? WHERE id = ?`, item.createdAt, item.toolCallID); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx, `UPDATE approvals SET created_at = ? WHERE id = ?`, item.createdAt, item.approvalID); err != nil {
			t.Fatal(err)
		}
	}

	events, err := repo.CancelRunWithEvent(ctx, enqueued.RunID, "client_cancelled", `{"reason":"client_cancelled"}`)
	if err != nil {
		t.Fatal(err)
	}
	var approvalOrder, toolOrder []string
	for _, event := range events {
		var payload map[string]any
		if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
			t.Fatal(err)
		}
		switch event.Type {
		case "approval.expired":
			approvalOrder = append(approvalOrder, payload["approvalId"].(string))
		case "tool.call.failed":
			toolOrder = append(toolOrder, payload["toolCallId"].(string))
		}
	}
	wantApprovals := []string{dependents[0].approvalID, dependents[1].approvalID}
	if !reflect.DeepEqual(approvalOrder, wantApprovals) {
		t.Fatalf("approval event order = %v, want %v", approvalOrder, wantApprovals)
	}
	wantTools := []string{dependents[0].toolCallID, dependents[1].toolCallID}
	if !reflect.DeepEqual(toolOrder, wantTools) {
		t.Fatalf("tool event order = %v, want %v", toolOrder, wantTools)
	}
}

func TestRecordAuditUsesDeterministicallySortedFixedWidthTimestampsAtHighVolume(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	const records = 500
	for index := 0; index < records; index++ {
		if err := repo.RecordAudit(ctx, "trace_audit", "system", "", fmt.Sprintf("audit.%03d", index), "audit-target", "{}"); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := database.QueryContext(ctx, `SELECT created_at FROM audit_logs WHERE target = ? ORDER BY created_at, id`, "audit-target")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var previous time.Time
	count := 0
	for rows.Next() {
		var createdAt string
		if err := rows.Scan(&createdAt); err != nil {
			t.Fatal(err)
		}
		if len(createdAt) != len("2030-01-02T03:04:05.000000000Z") {
			t.Fatalf("audit timestamp %q is not fixed width", createdAt)
		}
		parsed, err := time.Parse("2006-01-02T15:04:05.000000000Z", createdAt)
		if err != nil {
			t.Fatalf("parse fixed-width audit timestamp %q: %v", createdAt, err)
		}
		if !previous.IsZero() && parsed.Before(previous) {
			t.Fatalf("audit timestamp order moved backward from %s to %s", previous, parsed)
		}
		previous = parsed
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != records {
		t.Fatalf("ordered audit records = %d, want %d", count, records)
	}
}
