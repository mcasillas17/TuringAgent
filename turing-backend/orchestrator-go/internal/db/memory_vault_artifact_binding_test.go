package db

import (
	"context"
	"testing"
)

// A manifest row is a licence to delete a file in somebody's vault, and until
// this round the only thing it named was a path. A path is not an owner: the
// user can move a candidate out of the inbox and save something of their own
// under the same name, and a cleaner following the row would unlink whatever it
// found there.
//
// So the row carries the bytes it is entitled to remove. The reservation is
// still taken before the write, so it starts with no hash at all — there is
// nothing to hash yet, and inventing one there would mean guessing what is
// about to be written. The hash arrives with the finalization that says the
// file is on disk, and after that a finalized row without one is a row that
// claims ownership it cannot prove.

// TestVaultArtifactReservationsCarryNoHashUntilTheyAreFinalized holds the
// reservation to being what it is: a record that bytes may land, taken before
// any exist.
func TestVaultArtifactReservationsCarryNoHashUntilTheyAreFinalized(t *testing.T) {
	ctx := context.Background()
	database := openMemoryVaultDB(t, ctx)
	insertTestSession(t, ctx, database, "session_binding")

	if _, err := database.ExecContext(ctx, `
		INSERT INTO vault_artifacts (
			id, session_id, vault_path, physical_path, state, created_at, expected_content_hash
		) VALUES (
			'vault_reserved', 'session_binding', 'inbox/cand_1.md', 'inbox/cand_1.md',
			'writing', '2026-08-24T00:00:00Z', NULL)`); err != nil {
		t.Fatalf("a reservation with no hash was refused: %v", err)
	}

	// A reservation that names bytes is a reservation that has guessed. The
	// write has not happened, so nothing on disk can match, and a later pass
	// reading that hash would be reading a claim nobody ever checked.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO vault_artifacts (
			id, session_id, vault_path, physical_path, state, created_at, expected_content_hash
		) VALUES (
			'vault_guessing', 'session_binding', 'inbox/cand_2.md', 'inbox/cand_2.md',
			'writing', '2026-08-24T00:00:00Z', 'a-hash-of-bytes-that-do-not-exist-yet')`,
	); err == nil {
		t.Fatal("a reservation taken before the write named the bytes it had not written")
	}
}

// TestFinalizedVaultArtifactsMustNameTheBytesTheyOwn is the other half: once a
// row says the file is there, it has to say which file.
func TestFinalizedVaultArtifactsMustNameTheBytesTheyOwn(t *testing.T) {
	ctx := context.Background()
	database := openMemoryVaultDB(t, ctx)
	insertTestSession(t, ctx, database, "session_binding")

	if _, err := database.ExecContext(ctx, `
		INSERT INTO vault_artifacts (
			id, session_id, vault_path, physical_path, state, created_at, finalized_at, expected_content_hash
		) VALUES (
			'vault_ready', 'session_binding', 'inbox/cand_1.md', 'inbox/cand_1.md',
			'ready', '2026-08-24T00:00:00Z', '2026-08-24T00:00:01Z', 'sha256:whatever-was-written')`,
	); err != nil {
		t.Fatalf("a finalized row naming its bytes was refused: %v", err)
	}

	if _, err := database.ExecContext(ctx, `
		INSERT INTO vault_artifacts (
			id, session_id, vault_path, physical_path, state, created_at, finalized_at, expected_content_hash
		) VALUES (
			'vault_unbound', 'session_binding', 'inbox/cand_2.md', 'inbox/cand_2.md',
			'ready', '2026-08-24T00:00:00Z', '2026-08-24T00:00:01Z', NULL)`,
	); err == nil {
		t.Fatal("a finalized artifact was accepted without naming the bytes it owns")
	}

	// The same rule under UPDATE, which is how finalization actually happens.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO vault_artifacts (
			id, session_id, vault_path, physical_path, state, created_at
		) VALUES (
			'vault_moving', 'session_binding', 'inbox/cand_3.md', 'inbox/cand_3.md',
			'writing', '2026-08-24T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE vault_artifacts
		SET state = 'ready', finalized_at = '2026-08-24T00:00:01Z'
		WHERE id = 'vault_moving'`,
	); err == nil {
		t.Fatal("a reservation was finalized without naming the bytes it owns")
	}
}

// TestFailedVaultArtifactDeletionsSurviveWithoutAHash keeps the manifest able
// to drain. A reservation whose write never landed can still be marked
// delete_failed by a cleanup pass — it has no hash and never will — and that
// row is the retry's worklist. A constraint that refused it would abort the
// whole withdrawal transaction and strand every sibling row with it.
func TestFailedVaultArtifactDeletionsSurviveWithoutAHash(t *testing.T) {
	ctx := context.Background()
	database := openMemoryVaultDB(t, ctx)
	insertTestSession(t, ctx, database, "session_binding")

	if _, err := database.ExecContext(ctx, `
		INSERT INTO vault_artifacts (
			id, session_id, vault_path, physical_path, state, created_at
		) VALUES (
			'vault_stuck', 'session_binding', 'inbox/cand_1.md', 'inbox/cand_1.md',
			'writing', '2026-08-24T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE vault_artifacts SET state = 'delete_failed' WHERE id = 'vault_stuck'`,
	); err != nil {
		t.Fatalf("an unfinalized row could not be marked delete_failed: %v", err)
	}
}
