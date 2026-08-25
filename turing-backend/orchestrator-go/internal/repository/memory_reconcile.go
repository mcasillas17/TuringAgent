package repository

import (
	"context"
	"database/sql"
	"sort"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// MemoryNoteIssue names one file the user needs to know about and why. The
// reason describes the file's structure — a broken frontmatter block, a
// contested identity — and never its contents.
type MemoryNoteIssue struct {
	RelPath string
	Reason  string
}

// MemoryIndexReport is what one pass over the vault learned. Everything it
// could not index is named rather than swallowed: a note that silently fails
// to appear in search is indistinguishable, to the user, from one Turing
// decided to forget.
type MemoryIndexReport struct {
	Indexed   int
	Removed   int
	Healed    int
	Withdrawn int
	// AwaitingIdentity lists beliefs carrying no stable id. A read-only pass
	// reports them; only the file-writing pass may adopt one.
	AwaitingIdentity []string
	DuplicateNoteIDs []string
	Errors           []MemoryNoteIssue
	Skipped          []MemoryNoteIssue
	// UnmanagedInboxDrafts lists files the user dropped into the inbox
	// themselves. They are theirs, and nothing here touches them.
	UnmanagedInboxDrafts []string
	// OrphanInboxNotes lists candidate files with no candidate row — a
	// creation that crashed between the write and its transaction. The
	// reservation still names them, so they are tracked and cleanable; they
	// are reported rather than deleted, because a file the user may already
	// have read is not something to remove on a guess.
	OrphanInboxNotes []string
}

// MemoryReconcileReport adds what the file-writing pass changed on disk.
type MemoryReconcileReport struct {
	Index                   MemoryIndexReport
	IdentitiesAssigned      int
	RefsRewritten           int
	NotesHealed             int
	OrphanCandidatesRemoved int
	ReservationsCleared     int
}

// memoryVaultState is everything the database knows before a pass acts. It is
// read in one short transaction, outside the walk: SQLite must never be held
// open while the filesystem is being crawled.
type memoryVaultState struct {
	// anchor is captured before the scan. Rows newer than it were created
	// while this pass was already walking, so this pass knows nothing about
	// them and must not clean them up.
	anchor         string
	notesByID      map[string]MemoryNote
	evidenceByNote map[string][]string
	candidatePaths map[string]struct{}
}

// scannedVault is one walk, split into the shapes the passes need.
type scannedVault struct {
	beliefs     []memoryfiles.NoteRow
	inbox       []memoryfiles.NoteRow
	beliefPaths map[string]struct{}
	inboxPaths  map[string]struct{}
	duplicates  []string
	skipped     []memoryfiles.SkippedEntry
}

// RefreshMemoryIndex brings the note index up to date with the vault without
// writing a single byte to it.
//
// This is the pass that runs on a timer and behind a read. It may learn that a
// note carries no identity, that two files claim one, or that a frontmatter
// block no longer parses — and it reports all three rather than fixing them,
// because fixing them means writing to files the user has open in their editor.
func (r *Repository) RefreshMemoryIndex(ctx context.Context) (MemoryIndexReport, error) {
	vault, err := r.memoryVaultOrError()
	if err != nil {
		return MemoryIndexReport{}, err
	}
	r.memoryVaultMutex.Lock()
	defer r.memoryVaultMutex.Unlock()

	state, scanned, err := r.readVaultAndState(ctx, vault)
	if err != nil {
		return MemoryIndexReport{}, err
	}
	return r.applyMemoryIndex(ctx, state, scanned)
}

// ReconcileMemoryVault is the pass that is allowed to write.
//
// It runs at startup, after a deletion, and when a caller asks for it, under a
// vault-wide lock so two passes can never rewrite the same note from two
// different views of it. Its file writes are the four the plan allows: adopt a
// belief the user wrote by giving it an identity, and bring a managed note's
// citations back in line with the sidecar. Everything else it does is in the
// database — healing a note whose row was lost, retiring a candidate whose
// inbox entry is gone, and releasing the reservations that tracked them.
func (r *Repository) ReconcileMemoryVault(ctx context.Context) (MemoryReconcileReport, error) {
	vault, err := r.memoryVaultOrError()
	if err != nil {
		return MemoryReconcileReport{}, err
	}
	r.memoryVaultMutex.Lock()
	defer r.memoryVaultMutex.Unlock()

	state, scanned, err := r.readVaultAndState(ctx, vault)
	if err != nil {
		return MemoryReconcileReport{}, err
	}

	report := MemoryReconcileReport{}
	if report.IdentitiesAssigned, err = assignMissingIdentities(ctx, vault, scanned); err != nil {
		return MemoryReconcileReport{}, err
	}
	// The index runs before the citations are rewritten, not after. Healing a
	// note is what gives it a sidecar in the first place, and a rewrite driven
	// by a sidecar that does not exist yet would leave the file citing a
	// conversation the heal has already refused to link — converging only on
	// the pass after this one.
	if report.Index, err = r.applyMemoryIndex(ctx, state, scanned); err != nil {
		return MemoryReconcileReport{}, err
	}
	report.NotesHealed = report.Index.Healed
	if report.RefsRewritten, err = r.rewriteRefsFromSidecar(ctx, vault, scanned); err != nil {
		return MemoryReconcileReport{}, err
	}
	if report.OrphanCandidatesRemoved, report.ReservationsCleared, err = r.sweepVaultInbox(ctx, state, scanned); err != nil {
		return MemoryReconcileReport{}, err
	}
	return report, nil
}

// readVaultAndState captures the database's view and then walks the vault. The
// order matters: the anchor is taken first, so anything created while the walk
// is in progress is newer than it and out of this pass's scope.
func (r *Repository) readVaultAndState(ctx context.Context, vault *memoryfiles.Vault) (memoryVaultState, scannedVault, error) {
	state := memoryVaultState{anchor: now()}
	if r.memoryReconcileScanAnchor != "" {
		state.anchor = r.memoryReconcileScanAnchor
	}
	if err := r.loadMemoryVaultState(ctx, &state); err != nil {
		return memoryVaultState{}, scannedVault{}, err
	}
	result, err := vault.Scan(ctx)
	if err != nil {
		return memoryVaultState{}, scannedVault{}, err
	}
	scanned := scannedVault{
		beliefPaths: map[string]struct{}{},
		inboxPaths:  map[string]struct{}{},
		duplicates:  result.DuplicateNoteIDs,
		skipped:     result.Skipped,
	}
	for _, note := range result.Notes {
		switch note.Area {
		case memoryfiles.AreaBeliefs:
			scanned.beliefs = append(scanned.beliefs, note)
			scanned.beliefPaths[note.RelPath] = struct{}{}
		case memoryfiles.AreaInbox:
			scanned.inbox = append(scanned.inbox, note)
			scanned.inboxPaths[note.RelPath] = struct{}{}
		case memoryfiles.AreaOther:
		}
	}
	return state, scanned, nil
}

func (r *Repository) loadMemoryVaultState(ctx context.Context, state *memoryVaultState) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	state.notesByID = map[string]MemoryNote{}
	notes, err := tx.QueryContext(ctx, `
		SELECT id, path, content, content_hash, status, created_at, updated_at
		FROM memory_notes
	`)
	if err != nil {
		return err
	}
	for notes.Next() {
		note, err := scanMemoryNote(notes)
		if err != nil {
			return closeRowsWith(notes, err)
		}
		state.notesByID[note.NoteID] = note
	}
	if err := closeRows(notes); err != nil {
		return err
	}

	state.evidenceByNote = map[string][]string{}
	evidence, err := tx.QueryContext(ctx, `
		SELECT DISTINCT note_id, session_id FROM memory_evidence
	`)
	if err != nil {
		return err
	}
	for evidence.Next() {
		var noteID, sessionID string
		if err := evidence.Scan(&noteID, &sessionID); err != nil {
			return closeRowsWith(evidence, err)
		}
		state.evidenceByNote[noteID] = append(state.evidenceByNote[noteID], sessionID)
	}
	if err := closeRows(evidence); err != nil {
		return err
	}

	state.candidatePaths = map[string]struct{}{}
	candidates, err := tx.QueryContext(ctx, `SELECT inbox_path FROM memory_candidates`)
	if err != nil {
		return err
	}
	for candidates.Next() {
		var inboxPath string
		if err := candidates.Scan(&inboxPath); err != nil {
			return closeRowsWith(candidates, err)
		}
		state.candidatePaths[inboxPath] = struct{}{}
	}
	if err := closeRows(candidates); err != nil {
		return err
	}
	return tx.Commit()
}

// assignMissingIdentities adopts beliefs the user wrote by hand. Only the id is
// added; their prose is spliced around, never re-encoded, and an inbox draft is
// deliberately left alone — adopting one would decide, on the user's behalf,
// that a file they were still drafting is now memory.
func assignMissingIdentities(ctx context.Context, vault *memoryfiles.Vault, scanned scannedVault) (int, error) {
	assigned := 0
	for index := range scanned.beliefs {
		note := &scanned.beliefs[index]
		if !note.Indexable || note.NoteID != "" {
			continue
		}
		noteID, err := memoryfiles.NewNoteID()
		if err != nil {
			return assigned, err
		}
		rewritten, err := vault.RewriteFrontmatterRefs(ctx, memoryfiles.RewriteFrontmatterRefsRequest{
			RelPath:             note.RelPath,
			NoteID:              noteID,
			ExpectedContentHash: note.ContentHash,
		})
		if err != nil {
			return assigned, err
		}
		note.NoteID = noteID
		note.Content = rewritten.Content
		note.ContentHash = rewritten.ContentHash
		assigned++
	}
	return assigned, nil
}

// rewriteRefsFromSidecar is the one direction evidence travels. The database
// records what a note is grounded in and is what a session deletion acts on,
// so a file still citing a conversation that is gone is brought into line with
// the sidecar — never the reverse. An unmanaged note is the user's to edit, so
// it is left exactly as they wrote it.
//
// The sidecar is read again here rather than reused from the snapshot taken
// before the walk, because the index step in between is what gave a healed
// note its evidence, and rewriting from the older view would cite links that
// were already refused.
func (r *Repository) rewriteRefsFromSidecar(ctx context.Context, vault *memoryfiles.Vault, scanned scannedVault) (int, error) {
	current := memoryVaultState{}
	if err := r.loadMemoryVaultState(ctx, &current); err != nil {
		return 0, err
	}
	rewritten := make([]memoryfiles.NoteRow, 0)
	for index := range scanned.beliefs {
		note := &scanned.beliefs[index]
		if !note.Indexable || note.NoteID == "" || note.Status != memoryfiles.NoteStatusManaged {
			continue
		}
		if _, hasRow := current.notesByID[note.NoteID]; !hasRow {
			continue
		}
		desired := sortedUniqueStrings(current.evidenceByNote[note.NoteID])
		if equalStringSets(note.EvidenceRefs, desired) {
			continue
		}
		if desired == nil {
			// A non-nil empty slice is what clears the list; nil would mean
			// "leave the refs alone", which is the opposite instruction.
			desired = []string{}
		}
		updated, err := vault.RewriteFrontmatterRefs(ctx, memoryfiles.RewriteFrontmatterRefsRequest{
			RelPath:             note.RelPath,
			Refs:                desired,
			ExpectedContentHash: note.ContentHash,
		})
		if err != nil {
			return len(rewritten), err
		}
		note.Content = updated.Content
		note.ContentHash = updated.ContentHash
		note.EvidenceRefs = desired
		rewritten = append(rewritten, *note)
	}
	if len(rewritten) == 0 {
		return 0, nil
	}
	// The projection catches up with what was just written. A failure partway
	// through the loop above returns before this runs, so files already
	// rewritten are one pass ahead of the index until the next pass — which
	// indexes before it rewrites, and so brings them back in line.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, note := range rewritten {
		if err := updateMemoryNoteContentTx(ctx, tx, note.NoteID, note.Content, note.ContentHash); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(rewritten), nil
}

// applyMemoryIndex writes the projection. It runs in one transaction, over a
// walk that has already finished, so no SQLite transaction is ever held open
// across the filesystem.
func (r *Repository) applyMemoryIndex(ctx context.Context, state memoryVaultState, scanned scannedVault) (MemoryIndexReport, error) {
	report := MemoryIndexReport{DuplicateNoteIDs: scanned.duplicates}
	for _, entry := range scanned.skipped {
		report.Skipped = append(report.Skipped, MemoryNoteIssue{RelPath: entry.RelPath, Reason: entry.Reason})
	}
	for _, note := range scanned.inbox {
		switch {
		case note.ParseError != "":
			report.Errors = append(report.Errors, MemoryNoteIssue{RelPath: note.RelPath, Reason: note.ParseError})
		case note.Status == memoryfiles.NoteStatusUnmanaged:
			report.UnmanagedInboxDrafts = append(report.UnmanagedInboxDrafts, note.RelPath)
		default:
			if _, tracked := state.candidatePaths[note.RelPath]; !tracked {
				report.OrphanInboxNotes = append(report.OrphanInboxNotes, note.RelPath)
			}
		}
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MemoryIndexReport{}, err
	}
	defer func() { _ = tx.Rollback() }()

	indexedIDs := map[string]struct{}{}
	for _, note := range scanned.beliefs {
		if note.ParseError != "" {
			report.Errors = append(report.Errors, MemoryNoteIssue{RelPath: note.RelPath, Reason: note.ParseError})
			continue
		}
		if !note.Indexable {
			// Today every unindexable note also carries a parse error — a
			// contested identity is reported as one — so this is a second
			// gate rather than the branch that handles duplicates. It stands
			// because "not indexable" is the scan's word, and a future reason
			// to refuse a note must not become indexable by default.
			continue
		}
		if note.NoteID == "" {
			report.AwaitingIdentity = append(report.AwaitingIdentity, note.RelPath)
			continue
		}
		healed, withdrawn, err := indexMemoryNoteTx(ctx, tx, note)
		if err != nil {
			return MemoryIndexReport{}, err
		}
		indexedIDs[note.NoteID] = struct{}{}
		report.Indexed++
		if healed {
			report.Healed++
		}
		if withdrawn {
			report.Withdrawn++
		}
	}

	removed, err := removeVanishedNotesTx(ctx, tx, state.anchor, indexedIDs, scanned.beliefPaths)
	if err != nil {
		return MemoryIndexReport{}, err
	}
	report.Removed = removed
	if err := tx.Commit(); err != nil {
		return MemoryIndexReport{}, err
	}
	sort.Strings(report.AwaitingIdentity)
	sort.Strings(report.UnmanagedInboxDrafts)
	sort.Strings(report.OrphanInboxNotes)
	return report, nil
}

// indexMemoryNoteTx projects one note. Whether the row exists is decided inside
// this transaction rather than from the snapshot taken before the walk, because
// a promotion may have committed in between — and copying frontmatter refs into
// a note that already has evidence would duplicate what the sidecar knows.
func indexMemoryNoteTx(ctx context.Context, tx *sql.Tx, note memoryfiles.NoteRow) (bool, bool, error) {
	existing, err := memoryNoteByID(ctx, tx, note.NoteID)
	switch {
	case err == ErrMemoryNoteNotFound:
		status := string(note.Status)
		live, err := liveSessionRefsTx(ctx, tx, note.EvidenceRefs)
		if err != nil {
			return false, false, err
		}
		// A note whose every citation names a conversation that no longer
		// exists has lost its support. It is kept — the user accepted it —
		// and marked, so nothing answers with it as if it were still grounded.
		withdrawn := len(note.EvidenceRefs) > 0 && len(live) == 0
		if withdrawn {
			status = MemoryNoteStatusWithdrawn
		}
		// The row is written before its evidence: evidence belongs to the note,
		// and the foreign key that says so is what keeps an orphaned citation
		// from existing at all.
		if err := upsertMemoryNoteTx(ctx, tx, memoryNoteFromRow(note, status)); err != nil {
			return false, false, err
		}
		if err := linkMemoryEvidenceTx(ctx, tx, note.NoteID, live); err != nil {
			return false, false, err
		}
		return true, withdrawn, nil
	case err != nil:
		return false, false, err
	}

	status := string(note.Status)
	withdrawn := false
	switch {
	case existing.Status == MemoryNoteStatusWithdrawn:
		// Withdrawal is terminal. The conversations that grounded this note are
		// deleted, and a deleted conversation does not come back.
		status = MemoryNoteStatusWithdrawn
	case note.Status == memoryfiles.NoteStatusManaged && len(note.EvidenceRefs) > 0:
		sidecar, err := memoryNoteEvidenceSessions(ctx, tx, note.NoteID)
		if err != nil {
			return false, false, err
		}
		if len(sidecar) == 0 {
			status = MemoryNoteStatusWithdrawn
			withdrawn = true
		}
	}
	if err := upsertMemoryNoteTx(ctx, tx, memoryNoteFromRow(note, status)); err != nil {
		return false, false, err
	}
	return false, withdrawn, nil
}

func memoryNoteFromRow(note memoryfiles.NoteRow, status string) MemoryNote {
	return MemoryNote{
		NoteID:      note.NoteID,
		Path:        note.RelPath,
		Content:     note.Content,
		ContentHash: note.ContentHash,
		Status:      status,
	}
}

// removeVanishedNotesTx retires rows for files that are no longer in the vault.
// A row survives if either its identity or its path was seen, so a note whose
// frontmatter temporarily fails to parse keeps its row and its evidence — a
// typo in a YAML block is not a reason to destroy the citations a memory rests
// on. Rows newer than this pass's anchor are left alone: they were written
// while the walk was already in progress, so this pass never saw their files.
func removeVanishedNotesTx(
	ctx context.Context,
	tx *sql.Tx,
	anchor string,
	indexedIDs map[string]struct{},
	seenPaths map[string]struct{},
) (int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, path FROM memory_notes WHERE created_at < ?
	`, anchor)
	if err != nil {
		return 0, err
	}
	var vanished []string
	for rows.Next() {
		var noteID, path string
		if err := rows.Scan(&noteID, &path); err != nil {
			return 0, closeRowsWith(rows, err)
		}
		if _, indexed := indexedIDs[noteID]; indexed {
			continue
		}
		if _, seen := seenPaths[path]; seen {
			continue
		}
		vanished = append(vanished, noteID)
	}
	if err := closeRows(rows); err != nil {
		return 0, err
	}
	for _, noteID := range vanished {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_notes WHERE id = ?`, noteID); err != nil {
			return 0, err
		}
	}
	return len(vanished), nil
}

// sweepVaultInbox retires what the inbox no longer holds: a candidate row whose
// file is gone, and a reservation for a path that is no longer an inbox entry.
// Both are what a promotion, a rejection or a crash leaves behind, and both are
// bounded by the anchor so a candidate written during the walk is not mistaken
// for one whose file vanished.
func (r *Repository) sweepVaultInbox(ctx context.Context, state memoryVaultState, scanned scannedVault) (int, int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	orphans, err := staleRowsTx(ctx, tx, `
		SELECT id, inbox_path FROM memory_candidates WHERE created_at < ?
	`, scanned.inboxPaths, state.anchor)
	if err != nil {
		return 0, 0, err
	}
	for _, candidateID := range orphans {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_candidates WHERE id = ?`, candidateID); err != nil {
			return 0, 0, err
		}
	}

	// A reservation is taken before the bytes exist, so its own age says
	// nothing about whether the file was there to be walked. What does is when
	// the write was confirmed: only a reservation finalized before this walk
	// started can be judged by what the walk did or did not find. One taken or
	// confirmed while the walk was in flight — or one still unconfirmed after
	// a crash — is left alone, because clearing it would leave the bytes that
	// landed a moment later with nothing in the manifest naming them. A
	// lingering unconfirmed reservation is the intended outcome: it names a
	// path that may hold a file, which is exactly what the session cleaner
	// needs to be told.
	reservations, err := staleRowsTx(ctx, tx, `
		SELECT id, vault_path FROM vault_artifacts
		WHERE finalized_at IS NOT NULL AND finalized_at < ?
	`, scanned.inboxPaths, state.anchor)
	if err != nil {
		return 0, 0, err
	}
	for _, artifactID := range reservations {
		if _, err := tx.ExecContext(ctx, `DELETE FROM vault_artifacts WHERE id = ?`, artifactID); err != nil {
			return 0, 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return len(orphans), len(reservations), nil
}

// staleRowsTx returns the ids of rows whose recorded path is no longer an inbox
// entry. The query is a fixed code constant and every value is bound.
func staleRowsTx(ctx context.Context, tx *sql.Tx, query string, present map[string]struct{}, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var stale []string
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, closeRowsWith(rows, err)
		}
		if _, found := present[path]; found {
			continue
		}
		stale = append(stale, id)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	return stale, nil
}

func closeRows(rows *sql.Rows) error {
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	return rows.Close()
}

func closeRowsWith(rows *sql.Rows, err error) error {
	_ = rows.Close()
	return err
}
