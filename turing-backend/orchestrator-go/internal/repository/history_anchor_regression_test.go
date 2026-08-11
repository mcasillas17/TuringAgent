package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestListMessagesBeforeUsesSequenceAndRequiresSessionAnchor(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Causal ordering")
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.CreateSession(ctx, "Other")
	if err != nil {
		t.Fatal(err)
	}
	const createdAt = "2026-08-11T00:00:00.000000000Z"
	for _, message := range []struct {
		id       string
		sequence int
	}{
		{id: "message_z_first", sequence: 1},
		{id: "message_a_second", sequence: 2},
		{id: "message_q_third", sequence: 3},
		{id: "message_m_anchor", sequence: 4},
	} {
		if _, err := database.ExecContext(ctx, `
			INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
			VALUES (?, ?, 'user', ?, 'text', ?, ?)
		`, message.id, session.SessionID, message.id, message.sequence, createdAt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('message_other_anchor', ?, 'user', 'other', 'text', 1, ?)
	`, other.SessionID, createdAt); err != nil {
		t.Fatal(err)
	}

	messages, err := repo.ListMessagesBefore(ctx, session.SessionID, "message_m_anchor", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Sequence != 2 || messages[1].Sequence != 3 {
		t.Fatalf("predecessors = %+v, want sequences 2 then 3", messages)
	}
	for _, anchorID := range []string{"missing_anchor", "message_other_anchor"} {
		if _, err := repo.ListMessagesBefore(ctx, session.SessionID, anchorID, 10); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("anchor %q error = %v, want sql.ErrNoRows", anchorID, err)
		}
	}
}
