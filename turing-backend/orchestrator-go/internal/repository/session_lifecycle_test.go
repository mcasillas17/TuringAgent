package repository

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRenameSessionNormalizesBoundsAndNoOps(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Original")
	if err != nil {
		t.Fatal(err)
	}
	title := strings.Repeat("日", 120)

	renamed, err := repo.RenameSession(ctx, session.SessionID, "  "+title+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if !renamed.Changed || renamed.Session.Title.String != title || renamed.Session.TitleOrigin != "explicit" {
		t.Fatalf("RenameSession = %+v", renamed)
	}
	payload := decodeSessionUpdatedPayload(t, renamed.Event)
	if payload.Title != title || payload.Status != "active" || payload.UpdatedAt != renamed.Session.UpdatedAt {
		t.Fatalf("rename payload = %+v, session = %+v", payload, renamed.Session)
	}

	noOp, err := repo.RenameSession(ctx, session.SessionID, "\t"+title+" ")
	if err != nil {
		t.Fatal(err)
	}
	if noOp.Changed || noOp.Event.EventID != "" || noOp.Session.UpdatedAt != renamed.Session.UpdatedAt {
		t.Fatalf("same-title rename = %+v, want no-op", noOp)
	}

	for _, invalid := range []string{" \n\t ", strings.Repeat("日", 121)} {
		if _, err := repo.RenameSession(ctx, session.SessionID, invalid); !errors.Is(err, ErrInvalidSessionTitle) {
			t.Fatalf("RenameSession(%q) error = %v, want ErrInvalidSessionTitle", invalid, err)
		}
	}
	afterInvalid, err := repo.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if afterInvalid.UpdatedAt != renamed.Session.UpdatedAt {
		t.Fatalf("invalid rename updated_at = %q, want %q", afterInvalid.UpdatedAt, renamed.Session.UpdatedAt)
	}
}

func TestRenameSessionPromotesMatchingAutomaticTitle(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "Derived title",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	})
	if err != nil {
		t.Fatal(err)
	}

	renamed, err := repo.RenameSession(ctx, session.SessionID, "Derived title")
	if err != nil {
		t.Fatal(err)
	}
	if !renamed.Changed || renamed.Session.TitleOrigin != "explicit" {
		t.Fatalf("matching derived rename = %+v, want explicit change", renamed)
	}
	if renamed.Session.UpdatedAt <= enqueued.SessionUpdatedEvent.CreatedAt {
		t.Fatalf("rename updated_at = %q, want after %q", renamed.Session.UpdatedAt, enqueued.SessionUpdatedEvent.CreatedAt)
	}
}

func TestArchiveAndRestoreSessionAreVisibleAndIdempotent(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Active run")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "Keep this run active while archiving",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}); err != nil {
		t.Fatal(err)
	}

	archived, err := repo.ArchiveSession(ctx, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Changed || archived.Session.Status != "archived" {
		t.Fatalf("ArchiveSession = %+v", archived)
	}
	if payload := decodeSessionUpdatedPayload(t, archived.Event); payload.Status != "archived" {
		t.Fatalf("archive payload = %+v", payload)
	}
	active, err := repo.ListSessionsPage(ctx, ListSessionsInput{Filter: SessionListActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertSessionIDs(t, active, []string{})
	archivedPage, err := repo.ListSessionsPage(ctx, ListSessionsInput{Filter: SessionListArchived, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertSessionIDs(t, archivedPage, []string{session.SessionID})

	archiveNoOp, err := repo.ArchiveSession(ctx, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if archiveNoOp.Changed || archiveNoOp.Event.EventID != "" || archiveNoOp.Session.UpdatedAt != archived.Session.UpdatedAt {
		t.Fatalf("second archive = %+v, want no-op", archiveNoOp)
	}

	restored, err := repo.RestoreSession(ctx, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Changed || restored.Session.Status != "active" || restored.Session.UpdatedAt <= archived.Session.UpdatedAt {
		t.Fatalf("RestoreSession = %+v", restored)
	}
	restoreNoOp, err := repo.RestoreSession(ctx, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if restoreNoOp.Changed || restoreNoOp.Event.EventID != "" || restoreNoOp.Session.UpdatedAt != restored.Session.UpdatedAt {
		t.Fatalf("second restore = %+v, want no-op", restoreNoOp)
	}
}

func TestSessionLifecycleMutationsKeepFutureTimestampMonotonic(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Future")
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(time.Hour)
	if _, err := repo.db.ExecContext(ctx, `
		UPDATE sessions SET updated_at = ? WHERE id = ?`,
		FormatTimestamp(future),
		session.SessionID,
	); err != nil {
		t.Fatal(err)
	}

	archived, err := repo.ArchiveSession(ctx, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if want := FormatTimestamp(future.Add(time.Nanosecond)); archived.Session.UpdatedAt != want {
		t.Fatalf("archive updated_at = %q, want %q", archived.Session.UpdatedAt, want)
	}
}

func TestSessionLifecycleMutationsRejectMissingSession(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	operations := []func() error{
		func() error {
			_, err := repo.RenameSession(ctx, "missing", "Title")
			return err
		},
		func() error {
			_, err := repo.ArchiveSession(ctx, "missing")
			return err
		},
		func() error {
			_, err := repo.RestoreSession(ctx, "missing")
			return err
		},
	}
	for _, operation := range operations {
		if err := operation(); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("lifecycle error = %v, want ErrSessionNotFound", err)
		}
	}
}

func TestConcurrentSessionLifecycleMutationsSerializeSnapshots(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Before")
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(time.Hour)
	if _, err := repo.db.ExecContext(ctx, `
		UPDATE sessions SET updated_at = ? WHERE id = ?`,
		FormatTimestamp(future),
		session.SessionID,
	); err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	wait.Add(2)
	errs := make(chan error, 2)
	go func() {
		defer wait.Done()
		_, err := repo.RenameSession(ctx, session.SessionID, "After")
		errs <- err
	}()
	go func() {
		defer wait.Done()
		_, err := repo.ArchiveSession(ctx, session.SessionID)
		errs <- err
	}()
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	stored, err := repo.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Title.String != "After" || stored.TitleOrigin != "explicit" || stored.Status != "archived" {
		t.Fatalf("final session = %+v", stored)
	}
	rows, err := repo.db.QueryContext(ctx, `
		SELECT created_at FROM events
		WHERE session_id = ? AND type = 'session.updated'
		ORDER BY sequence`,
		session.SessionID,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var timestamps []string
	for rows.Next() {
		var timestamp string
		if err := rows.Scan(&timestamp); err != nil {
			t.Fatal(err)
		}
		timestamps = append(timestamps, timestamp)
	}
	want := []string{
		FormatTimestamp(future.Add(time.Nanosecond)),
		FormatTimestamp(future.Add(2 * time.Nanosecond)),
	}
	if len(timestamps) != 2 || timestamps[0] != want[0] || timestamps[1] != want[1] {
		t.Fatalf("lifecycle event timestamps = %v, want %v", timestamps, want)
	}
}
