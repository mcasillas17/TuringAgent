package repository

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
)

// quietAudit is one unattended.quiet.changed record as stored.
type quietAudit struct {
	actorType string
	target    string
	payload   map[string]any
}

func quietAuditRows(t *testing.T, ctx context.Context, repo *Repository) []quietAudit {
	t.Helper()
	rows, err := repo.db.QueryContext(ctx, `
		SELECT actor_type, COALESCE(target, ''), COALESCE(payload_json, '')
		FROM audit_logs WHERE action = 'unattended.quiet.changed' ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var audits []quietAudit
	for rows.Next() {
		var audit quietAudit
		var payload string
		if err := rows.Scan(&audit.actorType, &audit.target, &payload); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(payload), &audit.payload); err != nil {
			t.Fatalf("audit payload %q: %v", payload, err)
		}
		audits = append(audits, audit)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return audits
}

func mustSetQuiet(t *testing.T, ctx context.Context, repo *Repository, quiet bool, revision int64) UnattendedQuiet {
	t.Helper()
	setting, _, err := repo.SetUnattendedQuiet(ctx, quiet, revision)
	if err != nil {
		t.Fatalf("SetUnattendedQuiet(%v, %d): %v", quiet, revision, err)
	}
	return setting
}

func wantQuiet(t *testing.T, ctx context.Context, repo *Repository, want UnattendedQuiet) {
	t.Helper()
	got, err := repo.GetUnattendedQuiet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("quiet setting = %+v, want %+v", got, want)
	}
}

func wantFenced(t *testing.T, ctx context.Context, repo *Repository, want bool) {
	t.Helper()
	fenced, err := unattendedWorkFencedTx(ctx, repo.db)
	if err != nil {
		t.Fatal(err)
	}
	if fenced != want {
		t.Fatalf("unattended work fenced = %v, want %v", fenced, want)
	}
}

func drainPending(t *testing.T, ctx context.Context, repo *Repository) bool {
	t.Helper()
	var rows int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM settings WHERE key = 'unattended_quiet_drain'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return rows == 1
}

// TUR-028a: a missing row is the switch never set, which is off at revision
// 0, and nothing is fenced.
func TestTheQuietSwitchStartsOffAtRevisionZero(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	wantQuiet(t, ctx, repo, UnattendedQuiet{})
	wantFenced(t, ctx, repo, false)
}

// Turning quiet on commits the value at the next revision, the drain flag
// beside it and one content-free audit record, in one transaction. Turning it
// off writes the next revision and its own record, and leaves the drain
// pending, so unattended work stays fenced until the drain has finished.
func TestTurningQuietOnWritesTheNextRevisionTheDrainAndAnAuditRecord(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	on, changed, err := repo.SetUnattendedQuiet(ctx, true, 0)
	if err != nil || !changed || on != (UnattendedQuiet{Quiet: true, Revision: 1}) {
		t.Fatalf("turn quiet on = %+v, changed %v, %v; want on at revision 1", on, changed, err)
	}
	wantQuiet(t, ctx, repo, UnattendedQuiet{Quiet: true, Revision: 1})
	if !drainPending(t, ctx, repo) {
		t.Fatal("turning quiet on left no drain pending")
	}
	wantFenced(t, ctx, repo, true)

	off, changed, err := repo.SetUnattendedQuiet(ctx, false, 1)
	if err != nil || !changed || off != (UnattendedQuiet{Revision: 2}) {
		t.Fatalf("turn quiet off = %+v, changed %v, %v; want off at revision 2", off, changed, err)
	}
	if !drainPending(t, ctx, repo) {
		t.Fatal("turning quiet off cleared the drain it has not finished")
	}
	wantFenced(t, ctx, repo, true)

	audits := quietAuditRows(t, ctx, repo)
	if len(audits) != 2 {
		t.Fatalf("audit records = %+v, want one per change", audits)
	}
	for index, want := range []map[string]any{
		{"quiet": true, "revision": float64(1)},
		{"quiet": false, "revision": float64(2)},
	} {
		audit := audits[index]
		if audit.actorType != "client" || audit.target != "unattended_quiet" || len(audit.payload) != len(want) ||
			audit.payload["quiet"] != want["quiet"] || audit.payload["revision"] != want["revision"] {
			t.Fatalf("audit record %d = %+v, want the owner setting %v and nothing else", index, audit, want)
		}
	}
}

// With the current revision, setting the value already held writes nothing:
// no row, no revision, no audit record.
func TestSettingTheCurrentQuietValueWritesNothing(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	off, changed, err := repo.SetUnattendedQuiet(ctx, false, 0)
	if err != nil || changed || off != (UnattendedQuiet{}) {
		t.Fatalf("turn quiet off while off = %+v, changed %v, %v; want a no-op", off, changed, err)
	}
	var rows int
	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings WHERE key = 'unattended_quiet'`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("settings rows = %d, %v; want none written", rows, err)
	}
	mustSetQuiet(t, ctx, repo, true, 0)
	again, changed, err := repo.SetUnattendedQuiet(ctx, true, 1)
	if err != nil || changed || again != (UnattendedQuiet{Quiet: true, Revision: 1}) {
		t.Fatalf("turn quiet on while on = %+v, changed %v, %v; want a no-op at revision 1", again, changed, err)
	}
	if audits := quietAuditRows(t, ctx, repo); len(audits) != 1 {
		t.Fatalf("audit records = %d, want 1: a no-op records nothing", len(audits))
	}
}

// The quiet setter, stated once: the named revision is compared first, so a
// write from a view older than the last change is refused before the
// equal-value check, writes nothing, and returns the setting as it stands.
// A stale quiet-off never undoes a quiet-on, and no unattended job queued
// before it is claimed.
func TestAStaleQuietWriteIsRefusedAndQuietStaysOn(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	queued := fireAutomation(t, ctx, repo, "Digest")
	wantQuiet(t, ctx, repo, UnattendedQuiet{})
	mustSetQuiet(t, ctx, repo, true, 0)

	for _, write := range []struct {
		quiet    bool
		revision int64
	}{{false, 0}, {true, 0}, {false, -1}, {false, 2}, {false, 1 << 40}} {
		current, changed, err := repo.SetUnattendedQuiet(ctx, write.quiet, write.revision)
		if !errors.Is(err, ErrUnattendedQuietStale) || changed || current != (UnattendedQuiet{Quiet: true, Revision: 1}) {
			t.Fatalf("SetUnattendedQuiet(%v, %d) = %+v, changed %v, %v; want stale with the current on at revision 1",
				write.quiet, write.revision, current, changed, err)
		}
	}
	wantQuiet(t, ctx, repo, UnattendedQuiet{Quiet: true, Revision: 1})
	if audits := quietAuditRows(t, ctx, repo); len(audits) != 1 {
		t.Fatalf("audit records = %d, want 1: a stale write records nothing", len(audits))
	}
	if claimed := claimNext(t, ctx, repo, 0); claimed.RunID != "" {
		t.Fatalf("claim after a refused stale quiet-off = %q, want nothing: the queued run %q stays fenced", claimed.RunID, queued.RunID)
	}
}

// A stored value that is neither boolean counts as on, so it fails closed,
// and reads as unavailable. Its revision stays readable, so the write that
// repairs it names that revision like any other, and is a change by
// definition.
func TestACorruptQuietValueCountsAsOnUntilAWriteRepairsIt(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO settings (key, value_json, revision, updated_at) VALUES ('unattended_quiet', 'maybe', 4, '2026-11-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	wantQuiet(t, ctx, repo, UnattendedQuiet{Quiet: true, Revision: 4, Unavailable: true})
	wantFenced(t, ctx, repo, true)
	automation := mustCreateDueAutomation(t, repo, ctx, "Digest", everyFiveMinutes())
	if fire, found, err := repo.ClaimDueAutomation(ctx, mustParse(t, automation.NextDueAt), attendedTestDefaults); err != nil || found {
		t.Fatalf("due claim with a corrupt quiet value = %+v, found %v, %v; want nothing fired", fire, found, err)
	}

	if _, _, err := repo.SetUnattendedQuiet(ctx, false, 3); !errors.Is(err, ErrUnattendedQuietStale) {
		t.Fatalf("repair naming an old revision = %v, want stale", err)
	}
	repaired, changed, err := repo.SetUnattendedQuiet(ctx, false, 4)
	if err != nil || !changed || repaired != (UnattendedQuiet{Revision: 5}) {
		t.Fatalf("repair = %+v, changed %v, %v; want off at revision 5", repaired, changed, err)
	}
	wantFenced(t, ctx, repo, false)
	if audits := quietAuditRows(t, ctx, repo); len(audits) != 1 {
		t.Fatalf("audit records = %d, want the repair's", len(audits))
	}
}

// The audit record is part of the change: if it cannot be written, nothing
// is.
func TestAFailedQuietAuditWriteRefusesTheChange(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	if _, err := repo.db.ExecContext(ctx, `
		CREATE TRIGGER refuse_quiet_audit BEFORE INSERT ON audit_logs
		WHEN NEW.action = 'unattended.quiet.changed'
		BEGIN SELECT RAISE(ABORT, 'audit insert refused'); END`); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := repo.SetUnattendedQuiet(ctx, true, 0); err == nil || changed {
		t.Fatalf("turn quiet on with the audit refused = changed %v, %v; want an error", changed, err)
	}
	wantQuiet(t, ctx, repo, UnattendedQuiet{})
	if drainPending(t, ctx, repo) {
		t.Fatal("a refused change left a drain pending")
	}
	wantFenced(t, ctx, repo, false)
}

// Two writers naming the same revision: the first commits the change, and the
// second, compared against the revision it now finds, is stale.
func TestConcurrentQuietWritersNamingOneRevisionChangeItOnce(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	const writers = 8
	var wait sync.WaitGroup
	results := make(chan error, writers)
	changes := make(chan bool, writers)
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, changed, err := repo.SetUnattendedQuiet(ctx, true, 0)
			results <- err
			changes <- changed
		}()
	}
	wait.Wait()
	close(results)
	close(changes)
	changedCount, staleCount := 0, 0
	for changed := range changes {
		if changed {
			changedCount++
		}
	}
	for err := range results {
		switch {
		case err == nil:
		case errors.Is(err, ErrUnattendedQuietStale):
			staleCount++
		default:
			t.Fatalf("concurrent write: %v", err)
		}
	}
	if changedCount != 1 || staleCount != writers-1 {
		t.Fatalf("changed %d, stale %d of %d writers; want exactly one change and every other stale", changedCount, staleCount, writers)
	}
	wantQuiet(t, ctx, repo, UnattendedQuiet{Quiet: true, Revision: 1})
}

// The switch is durable: a restart reads it back with its revision, still
// fencing unattended work.
func TestTheQuietSwitchSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "turing.db")
	open := func() *Repository {
		t.Helper()
		database, err := db.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })
		if err := db.ApplyMigrations(ctx, database); err != nil {
			t.Fatal(err)
		}
		return New(database)
	}
	first := open()
	mustSetQuiet(t, ctx, first, true, 0)
	if err := first.db.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := open()
	wantQuiet(t, ctx, restarted, UnattendedQuiet{Quiet: true, Revision: 1})
	wantFenced(t, ctx, restarted, true)
}

// The fence is up while quiet is on or a drain is pending, and a value of
// either key that cannot be read holds it up too: it fails closed.
func TestTheUnattendedFenceHoldsWhileQuietIsOnOrDraining(t *testing.T) {
	for name, test := range map[string]struct {
		quiet, drain string
		want         bool
	}{
		"neither key":            {want: false},
		"quiet off":              {quiet: "false", want: false},
		"quiet on":               {quiet: "true", want: true},
		"quiet off and draining": {quiet: "false", drain: "true", want: true},
		"draining only":          {drain: "true", want: true},
		"quiet unreadable":       {quiet: "yes", want: true},
		"drain unreadable":       {quiet: "false", drain: "1", want: true},
	} {
		t.Run(name, func(t *testing.T) {
			repo, ctx := newTitleTestRepo(t)
			for key, value := range map[string]string{"unattended_quiet": test.quiet, "unattended_quiet_drain": test.drain} {
				if value == "" {
					continue
				}
				if _, err := repo.db.ExecContext(ctx,
					`INSERT INTO settings (key, value_json, updated_at) VALUES (?, ?, '2026-11-01T00:00:00Z')`, key, value); err != nil {
					t.Fatal(err)
				}
			}
			wantFenced(t, ctx, repo, test.want)
		})
	}
}

// Repairing an unreadable value to on is a change like any other: the next
// revision, the drain raised, and an audit record.
func TestACorruptQuietValueCanBeRepairedToOn(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO settings (key, value_json, revision, updated_at) VALUES ('unattended_quiet', '1', 2, '2026-11-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	repaired, changed, err := repo.SetUnattendedQuiet(ctx, true, 2)
	if err != nil || !changed || repaired != (UnattendedQuiet{Quiet: true, Revision: 3}) {
		t.Fatalf("repair to on = %+v, changed %v, %v; want on at revision 3", repaired, changed, err)
	}
	if !drainPending(t, ctx, repo) {
		t.Fatal("repairing to on raised no drain")
	}
	audits := quietAuditRows(t, ctx, repo)
	if len(audits) != 1 || audits[0].payload["quiet"] != true || audits[0].payload["revision"] != float64(3) {
		t.Fatalf("audit records = %+v, want the repair's", audits)
	}
}

// Nothing on the attended probe: the claim reads the fence only before its
// second probe. With the settings table unreadable, an owner's turn is still
// claimed, and a claim that would reach unattended work fails rather than
// claiming it unfenced.
func TestTheAttendedProbeNeverReadsTheQuietFence(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	fire := fireAutomation(t, ctx, repo, "Digest")
	turn := enqueueLocalTurn(t, ctx, repo, "Owner")
	if _, err := repo.db.ExecContext(ctx, `ALTER TABLE settings RENAME TO settings_unreadable`); err != nil {
		t.Fatal(err)
	}
	if got := claimNext(t, ctx, repo, 0); got.RunID != turn.RunID {
		t.Fatalf("claim with the fence unreadable = %q, want the owner's turn %q", got.RunID, turn.RunID)
	}
	claimed, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-1", 0, time.Hour,
		localWorkerAt(0), func(RoutingRequirements) bool { return true })
	if err == nil || claimed.RunID != "" {
		t.Fatalf("claim reaching the automation run %s with the fence unreadable = %+v, %v; want an error and nothing claimed",
			fire.RunID, claimed, err)
	}
}
