package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// Reconcile audit actions. Every one of these is a change to what Turing
// remembers about the user, made on the strength of what a pass found in their
// files rather than on anything they asked for — so each one is recorded.
//
// The rows carry an id and a status and nothing else. A note's path carries its
// title, which is the user's own prose about themselves, and a candidate's
// session id names the conversation it came from: an audit log is not where
// either belongs. An id is enough to look the rest up while the row still
// exists, and means nothing once it does not.
const (
	memoryNoteIndexedAction         = "memory.note.indexed"
	memoryNoteWithdrawnAction       = "memory.note.withdrawn"
	memoryNoteRemovedAction         = "memory.note.removed"
	memoryNoteRefsRewrittenAction   = "memory.note.refs_rewritten"
	memoryCandidateOrphanedAction   = "memory.candidate.orphan_removed"
	memoryReservationReleasedAction = "memory.reservation.released"
)

// recordMemoryReconcileTx writes one redacted reconcile row inside the same
// transaction as the change it describes, for the same reason a candidate
// decision does: a record written separately can be lost while the change it
// describes survives, and then the user's memory has moved with nothing saying
// why.
func recordMemoryReconcileTx(ctx context.Context, tx *sql.Tx, action string, target string, status string) error {
	payload, err := json.Marshal(map[string]string{"status": status})
	if err != nil {
		return err
	}
	return recordAuditTx(ctx, tx, "", "system", "", action, target, string(payload))
}

// MemoryNoteIssue names one file the user needs to know about and why. The
// reason describes the file's structure — a broken frontmatter block, a
// contested identity — and never its contents.
type MemoryNoteIssue struct {
	RelPath string
	Reason  string
}

// memoryVaultRootArea names the vault root in an incompleteness report. It is
// not an area notes come from; it is the enumeration the other two rest on, so
// a root that could not be listed is reported alongside them rather than
// silently standing in for both.
const memoryVaultRootArea = "root"

// MemoryVaultAreaIssue names one area of the vault a pass could not enumerate
// in full, and why.
//
// It exists because the difference between "the walk found no beliefs" and
// "the walk could not read beliefs/" is invisible in a list of notes and is
// the difference between reconciling and erasing the user's memory. While an
// area is named here, nothing is retired for it.
type MemoryVaultAreaIssue struct {
	Area   string
	Reason string
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
	// Errors names every note this pass could not account for: one it could
	// not read or parse, and — in the writing pass — one it could not write.
	// A file the pass gave up on is one the user has to know about, because a
	// note that silently fails to appear in search is indistinguishable, to
	// them, from one Turing decided to forget.
	Errors  []MemoryNoteIssue
	Skipped []MemoryNoteIssue
	// UnmanagedInboxDrafts lists files the user dropped into the inbox
	// themselves. They are theirs, and nothing here touches them.
	UnmanagedInboxDrafts []string
	// OrphanInboxNotes lists candidate files with no candidate row — a
	// creation that crashed between the write and its transaction. The
	// reservation still names them, so they are tracked and cleanable; they
	// are reported rather than deleted, because a file the user may already
	// have read is not something to remove on a guess.
	OrphanInboxNotes []string
	// IncompleteAreas names every area this pass could not enumerate in full.
	// Nothing is retired for an area listed here: a walk that could not look
	// has not established that anything is missing.
	IncompleteAreas []MemoryVaultAreaIssue
	// ContestedPaths lists notes whose new name is still held by a row this
	// pass could not account for. Their files are in the vault and their rows
	// are intact under the previous name; the rename lands once the note
	// holding that name is resolved.
	ContestedPaths []MemoryNoteIssue
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
	// completeness says which areas the walk actually managed to enumerate.
	// It is what every deletion in this file is gated on, so it is carried
	// with the walk rather than re-derived: a caller that has the notes but
	// not this has no way to tell an empty area from an unreadable one.
	completeness memoryfiles.ScanCompleteness
}

// RefreshMemoryIndex brings the note index up to date with the vault without
// writing a single byte to it.
//
// This is the pass that runs on a timer and behind a read. It may learn that a
// note carries no identity, that two files claim one, or that a frontmatter
// block no longer parses — and it reports all three rather than fixing them,
// because fixing them means writing to files the user has open in their editor.
//
// Read-only is about the vault, not about the pass. It still walks the whole
// vault and still rewrites the index from what it saw, so it runs under the
// same vault-wide lock as the writing pass: see runVaultPass.
func (r *Repository) RefreshMemoryIndex(ctx context.Context) (MemoryIndexReport, error) {
	var report MemoryIndexReport
	err := r.runVaultPass(ctx, func(_ *memoryfiles.Vault, state memoryVaultState, scanned scannedVault) error {
		var applyErr error
		report, applyErr = r.applyMemoryIndex(ctx, state, scanned)
		return applyErr
	})
	if err != nil {
		return MemoryIndexReport{}, err
	}
	return report, nil
}

// ReconcileMemoryVault is the pass that is allowed to write.
//
// It runs at startup, after a deletion, and when a caller asks for it, under
// the same vault-wide lock as the read-only refresh so two passes can never
// rewrite the same note from two different views of it. Its file writes are the
// four the plan allows: adopt a belief the user wrote by giving it an identity,
// and bring a managed note's citations back in line with the sidecar.
// Everything else it does is in the database — healing a note whose row was
// lost, retiring a candidate whose inbox entry is gone, and releasing the
// reservations that tracked them.
func (r *Repository) ReconcileMemoryVault(ctx context.Context) (MemoryReconcileReport, error) {
	report := MemoryReconcileReport{}
	err := r.runVaultPass(ctx, func(vault *memoryfiles.Vault, state memoryVaultState, scanned scannedVault) error {
		var passErr error
		var adoptionIssues, rewriteIssues []MemoryNoteIssue
		report.IdentitiesAssigned, adoptionIssues, passErr = assignMissingIdentities(ctx, vault, scanned)
		if passErr != nil {
			return passErr
		}
		// The index runs before the citations are rewritten, not after.
		// Healing a note is what gives it a sidecar in the first place, and a
		// rewrite driven by a sidecar that does not exist yet would leave the
		// file citing a conversation the heal has already refused to link —
		// converging only on the pass after this one.
		if report.Index, passErr = r.applyMemoryIndex(ctx, state, scanned); passErr != nil {
			return passErr
		}
		report.NotesHealed = report.Index.Healed
		report.RefsRewritten, rewriteIssues, passErr = r.rewriteRefsFromSidecar(ctx, vault, scanned)
		if passErr != nil {
			return passErr
		}
		report.Index.Errors = append(report.Index.Errors, adoptionIssues...)
		report.Index.Errors = append(report.Index.Errors, rewriteIssues...)
		report.OrphanCandidatesRemoved, report.ReservationsCleared, passErr = r.sweepVaultInbox(ctx, state, scanned)
		return passErr
	})
	if err != nil {
		return MemoryReconcileReport{}, err
	}
	return report, nil
}

// runVaultPass is the one door both whole-vault passes go through, and the one
// place the vault-wide lock is taken.
//
// Two passes are not allowed to be inside the vault at the same time, and that
// is as true of the read-only refresh as it is of the writing reconcile: a
// refresh derives the whole index from one walk, so a reconcile assigning
// identities and rewriting citations underneath it would have it index bytes
// that no longer exist and then delete the rows for notes that do. Holding the
// lock in one shared place is deliberate — a second pass added later inherits
// the serialisation instead of having to remember it.
//
// The lock covers the state read, the walk, and whatever the caller does with
// the two. It never covers an unrelated call, and the database transaction the
// state read opens is closed before the walk starts: SQLite must not be held
// open across a filesystem crawl of a vault the user may be editing.
func (r *Repository) runVaultPass(
	ctx context.Context,
	act func(*memoryfiles.Vault, memoryVaultState, scannedVault) error,
) error {
	r.memoryVaultMutex.Lock()
	defer r.memoryVaultMutex.Unlock()

	// Read inside the lock, together. The vault and the cache filled from it
	// are one pair, and a pass that picked them up either side of a
	// SetMemoryVault would be reading one vault through another's cache.
	vault, cache := r.memoryVault, r.memoryScanCache
	if vault == nil {
		return ErrMemoryVaultUnavailable
	}
	state, scanned, err := r.readVaultAndState(ctx, vault, cache)
	if err != nil {
		return err
	}
	return act(vault, state, scanned)
}

// ScanMemoryVault reads the vault the way a pass does, through the same lock
// and the same cache, and returns what it saw without writing anything.
//
// It exists for the one caller that renders a page immediately after
// reconciling it. That caller needs the notes themselves, which the reconcile
// report does not carry, and a second cold walk of the user's whole vault to
// draw one page is a walk too many. Going through here means the second pass
// reuses everything the first one just read.
func (r *Repository) ScanMemoryVault(ctx context.Context) (memoryfiles.ScanResult, error) {
	r.memoryVaultMutex.Lock()
	defer r.memoryVaultMutex.Unlock()
	vault, cache := r.memoryVault, r.memoryScanCache
	if vault == nil {
		return memoryfiles.ScanResult{}, ErrMemoryVaultUnavailable
	}
	return vault.ScanWithCache(ctx, cache)
}

// readVaultAndState captures the database's view and then walks the vault. The
// order matters: the anchor is taken first, so anything created while the walk
// is in progress is newer than it and out of this pass's scope.
func (r *Repository) readVaultAndState(
	ctx context.Context,
	vault *memoryfiles.Vault,
	cache *memoryfiles.MetadataCache,
) (memoryVaultState, scannedVault, error) {
	state := memoryVaultState{anchor: now()}
	if r.memoryReconcileScanAnchor != "" {
		state.anchor = r.memoryReconcileScanAnchor
	}
	if err := r.loadMemoryVaultState(ctx, &state); err != nil {
		return memoryVaultState{}, scannedVault{}, err
	}
	// The seam sits here, between the closed state transaction and the walk:
	// inside the vault-wide lock, holding no database transaction. A test
	// parked here is a pass that is provably in the middle of the vault.
	if r.memoryVaultPassBarrier != nil {
		r.memoryVaultPassBarrier()
	}
	result, err := vault.ScanWithCache(ctx, cache)
	if err != nil {
		return memoryVaultState{}, scannedVault{}, err
	}
	scanned := scannedVault{
		beliefPaths:  map[string]struct{}{},
		inboxPaths:   map[string]struct{}{},
		duplicates:   result.DuplicateNoteIDs,
		skipped:      result.Skipped,
		completeness: result.Completeness,
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
//
// One note it cannot write is one note. A frontmatter written as a YAML flow
// mapping, a file the user has open read-only, a permission the sync client
// changed: each is a fact about that file and about nothing else, and each is
// reported and stepped over. Aborting the pass on the first of them let a
// single note the user happened to write on one line stop every other note from
// being adopted, keep an interrupted deletion from ever finishing, and take the
// whole app down at startup.
func assignMissingIdentities(
	ctx context.Context,
	vault *memoryfiles.Vault,
	scanned scannedVault,
) (int, []MemoryNoteIssue, error) {
	assigned := 0
	var issues []MemoryNoteIssue
	for index := range scanned.beliefs {
		note := &scanned.beliefs[index]
		if !note.Indexable || note.NoteID != "" {
			continue
		}
		noteID, err := memoryfiles.NewNoteID()
		if err != nil {
			// Not a fact about this note: the identity source itself failed,
			// so the next note would fail the same way and a pass that carried
			// on would be reporting a vault it never adopted anything in.
			return assigned, issues, err
		}
		rewritten, err := vault.RewriteFrontmatterRefs(ctx, memoryfiles.RewriteFrontmatterRefsRequest{
			RelPath:             note.RelPath,
			NoteID:              noteID,
			ExpectedContentHash: note.ContentHash,
		})
		if err != nil {
			if fatal := untrustworthyPassError(ctx, err); fatal != nil {
				return assigned, issues, fatal
			}
			issues = append(issues, MemoryNoteIssue{
				RelPath: note.RelPath,
				Reason:  "this note could not be given a stable identity: " + err.Error(),
			})
			continue
		}
		note.NoteID = noteID
		note.Content = rewritten.Content
		note.ContentHash = rewritten.ContentHash
		assigned++
	}
	return assigned, issues, nil
}

// untrustworthyPassError separates "this file" from "this pass".
//
// A failure to write one note says something about that note. A cancelled or
// expired context says the pass never got to look at the rest of the vault, and
// a report built on top of it would describe a walk that did not happen — so
// that one is returned and the pass fails as a whole. Everything else is
// per-note, visible in the report, and stepped over.
func untrustworthyPassError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
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
func (r *Repository) rewriteRefsFromSidecar(
	ctx context.Context,
	vault *memoryfiles.Vault,
	scanned scannedVault,
) (int, []MemoryNoteIssue, error) {
	current := memoryVaultState{}
	if err := r.loadMemoryVaultState(ctx, &current); err != nil {
		return 0, nil, err
	}
	rewritten := make([]memoryfiles.NoteRow, 0)
	var issues []MemoryNoteIssue
	for index := range scanned.beliefs {
		note := &scanned.beliefs[index]
		if !note.Indexable || note.NoteID == "" || note.Status != memoryfiles.NoteStatusManaged {
			continue
		}
		if _, hasRow := current.notesByID[note.NoteID]; !hasRow {
			continue
		}
		desired := sortedUniqueStrings(current.evidenceByNote[note.NoteID])
		request, needed := refsRewriteFor(*note, desired)
		if !needed {
			continue
		}
		updated, err := vault.RewriteFrontmatterRefs(ctx, request)
		if err != nil {
			// Same rule as the adoption step above, and it matters more here:
			// this is the pass a session deletion waits on, and one note whose
			// citations cannot be rewritten must not hold the withdrawal of
			// every other note open forever.
			if fatal := untrustworthyPassError(ctx, err); fatal != nil {
				return len(rewritten), issues, fatal
			}
			issues = append(issues, MemoryNoteIssue{
				RelPath: note.RelPath,
				Reason:  "this note's citations could not be brought back in line with the record: " + err.Error(),
			})
			continue
		}
		note.Content = updated.Content
		note.ContentHash = updated.ContentHash
		note.EvidenceRefs = desired
		note.EvidenceWithdrawn = request.Withdrawn
		rewritten = append(rewritten, *note)
	}
	if len(rewritten) == 0 {
		return 0, issues, nil
	}
	// The projection catches up with what was just written. A failure partway
	// through the loop above returns before this runs, so files already
	// rewritten are one pass ahead of the index until the next pass — which
	// indexes before it rewrites, and so brings them back in line.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, issues, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, note := range rewritten {
		if err := updateMemoryNoteContentTx(ctx, tx, note.NoteID, note.Content, note.ContentHash); err != nil {
			return 0, issues, err
		}
		status := "refs_updated"
		if note.EvidenceWithdrawn {
			status = MemoryNoteStatusWithdrawn
		}
		if err := recordMemoryReconcileTx(ctx, tx, memoryNoteRefsRewrittenAction, note.NoteID, status); err != nil {
			return 0, issues, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, issues, err
	}
	return len(rewritten), issues, nil
}

// refsRewriteFor decides what one note's refs value has to say, given what the
// sidecar knows, and whether saying it means writing to the user's file at all.
//
// Three outcomes, and the third is the one that matters: a note still citing
// conversations the sidecar no longer has is *withdrawn*, in words. Handing it
// an empty list instead would say something different and untrue — that nobody
// ever grounded it — and the marker, unlike a list, cannot be read back as a
// citation, so the pass after this one cannot undo the withdrawal. A note that
// genuinely never carried citations is left exactly as the user wrote it.
func refsRewriteFor(note memoryfiles.NoteRow, desired []string) (memoryfiles.RewriteFrontmatterRefsRequest, bool) {
	request := memoryfiles.RewriteFrontmatterRefsRequest{
		RelPath:             note.RelPath,
		ExpectedContentHash: note.ContentHash,
	}
	if len(desired) > 0 {
		if equalStringSets(note.EvidenceRefs, desired) {
			return request, false
		}
		request.Refs = desired
		return request, true
	}
	if len(note.EvidenceRefs) == 0 {
		return request, false
	}
	request.Withdrawn = true
	return request, true
}

// applyMemoryIndex writes the projection. It runs in one transaction, over a
// walk that has already finished, so no SQLite transaction is ever held open
// across the filesystem.
func (r *Repository) applyMemoryIndex(ctx context.Context, state memoryVaultState, scanned scannedVault) (MemoryIndexReport, error) {
	report := MemoryIndexReport{
		DuplicateNoteIDs: scanned.duplicates,
		IncompleteAreas:  incompleteVaultAreas(scanned.completeness),
	}
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

	// The notes are sorted into what this pass will write and what it will
	// only report on before anything is written, because deciding who owns a
	// contested path needs the whole list: a note the pass is about to move is
	// a note whose old name is about to be free, and one it is not is a note
	// whose name has to be left alone.
	writable := make([]memoryfiles.NoteRow, 0, len(scanned.beliefs))
	scannedIDs := map[string]struct{}{}
	for _, note := range scanned.beliefs {
		if note.NoteID != "" {
			// Recorded even for a note this pass refuses to index: an identity
			// the walk saw is an identity whose row must not be treated as an
			// abandoned name.
			scannedIDs[note.NoteID] = struct{}{}
		}
		switch {
		case note.ParseError != "":
			report.Errors = append(report.Errors, MemoryNoteIssue{RelPath: note.RelPath, Reason: note.ParseError})
		case !note.Indexable:
			// Today every unindexable note also carries a parse error — a
			// contested identity is reported as one — so this is a second
			// gate rather than the branch that handles duplicates. It stands
			// because "not indexable" is the scan's word, and a future reason
			// to refuse a note must not become indexable by default.
		case note.NoteID == "":
			report.AwaitingIdentity = append(report.AwaitingIdentity, note.RelPath)
		default:
			writable = append(writable, note)
		}
	}

	beliefsComplete := scanned.completeness.Area(memoryfiles.AreaBeliefs).Complete
	plan, err := planMemoryNotePaths(ctx, tx, writable, scannedIDs, state.anchor, beliefsComplete)
	if err != nil {
		return MemoryIndexReport{}, err
	}
	if err := parkMemoryNotePathsTx(ctx, tx, plan.park); err != nil {
		return MemoryIndexReport{}, err
	}
	if r.memoryIndexParkBarrier != nil {
		if err := r.memoryIndexParkBarrier(); err != nil {
			return MemoryIndexReport{}, err
		}
	}

	for _, note := range writable {
		if _, waiting := plan.deferred[note.NoteID]; waiting {
			report.ContestedPaths = append(report.ContestedPaths, MemoryNoteIssue{
				RelPath: note.RelPath,
				Reason:  memoryNotePathContestedReason,
			})
			continue
		}
		healed, withdrawn, err := indexMemoryNoteTx(ctx, tx, note)
		if err != nil {
			return MemoryIndexReport{}, err
		}
		report.Indexed++
		if healed {
			report.Healed++
		}
		if withdrawn {
			report.Withdrawn++
		}
	}

	// The one deletion this pass performs, and the only thing gated on the
	// walk having actually read beliefs/. Everything above is an upsert, which
	// is safe to run on whatever the walk did manage to see; a removal is not,
	// because it is inferred from what the walk did *not* see, and a walk that
	// could not look did not fail to find anything.
	if beliefsComplete {
		// Every identity the walk saw is spared, not only the ones this pass
		// wrote: a note it refused to index because two files claim it is a
		// note that is present, and a note whose rename it held back is
		// present under a name the projection cannot take yet. Both would
		// otherwise read as files that vanished, because their rows still
		// name paths the vault no longer has.
		kept := make(map[string]struct{}, len(scannedIDs)+len(plan.deferred))
		for noteID := range scannedIDs {
			kept[noteID] = struct{}{}
		}
		for noteID := range plan.deferred {
			kept[noteID] = struct{}{}
		}
		removed, err := removeVanishedNotesTx(ctx, tx, state.anchor, kept, scanned.beliefPaths)
		if err != nil {
			return MemoryIndexReport{}, err
		}
		report.Removed = removed
	}
	if err := tx.Commit(); err != nil {
		return MemoryIndexReport{}, err
	}
	sort.Strings(report.AwaitingIdentity)
	sort.Strings(report.UnmanagedInboxDrafts)
	sort.Strings(report.OrphanInboxNotes)
	return report, nil
}

// memoryNotePathContestedReason is what the user is told when a rename could
// not be applied this pass. It names no other note, because the answer to "why
// is my note still under the old name" is that the pass declined to guess, not
// which of their other memories it declined to guess about.
const memoryNotePathContestedReason = "another note still holds this path; the rename will be applied once that note is resolved"

// memoryNotePathParkPrefix is what a contested path is set to for the moment
// between vacating it and writing the real one.
//
// It has to be something the vault could never produce, or a pass would be
// able to leave a value behind that later reads back as a file the user has:
// it opens with a NUL byte, which normalizeVaultPath refuses outright, so it
// cannot be a path, cannot be typed, and cannot be created. The note's id
// follows, because the path column is UNIQUE and two notes vacating at once
// must not collide on the way out of one collision.
const memoryNotePathParkPrefix = "\x00memory-note-path-parked\x00"

func parkedMemoryNotePath(noteID string) string { return memoryNotePathParkPrefix + noteID }

// memoryNotePathPlan is one pass's answer to every contested path: which rows
// have to let go of their name before the real names are written, and which
// notes are not being written at all this time because nobody could establish
// that the name they want is free.
type memoryNotePathPlan struct {
	park     []string
	deferred map[string]struct{}
}

// planMemoryNotePaths works out, before a single path is written, who owns each
// name this pass wants to claim.
//
// The projection keys notes by identity but the path column is UNIQUE, so a
// rename in Obsidian is an update that collides with whatever row still holds
// the new name. A row is only pushed off its name when this pass can say what
// becomes of it: either the walk found its file under a different name, and it
// is being written there in this same transaction, or the walk read the whole
// of beliefs/, never saw its identity, and its row predates the walk — which
// makes it a name nothing in the vault answers to any more.
//
// Anything else, and the *claimant* waits instead. That is the conservative
// direction: a note that keeps its old path for one more pass has lost nothing,
// while a row pushed off its name with nothing to move it to would be a memory
// vacated on a guess. Waiting cascades — a note that stays put keeps its own
// name occupied — so the decision is taken to a fixpoint rather than in one
// sweep.
func planMemoryNotePaths(
	ctx context.Context,
	tx *sql.Tx,
	writes []memoryfiles.NoteRow,
	scannedIDs map[string]struct{},
	anchor string,
	removalEnabled bool,
) (memoryNotePathPlan, error) {
	plan := memoryNotePathPlan{deferred: map[string]struct{}{}}
	if len(writes) == 0 {
		return plan, nil
	}

	// Read inside the transaction rather than from the snapshot taken before
	// the walk: a promotion may have committed a new note in between, and a
	// row this pass cannot see is a collision it cannot avoid.
	rows, err := tx.QueryContext(ctx, `SELECT id, path, created_at FROM memory_notes`)
	if err != nil {
		return memoryNotePathPlan{}, err
	}
	holderOf := map[string]string{}
	createdAt := map[string]string{}
	for rows.Next() {
		var noteID, path, created string
		if err := rows.Scan(&noteID, &path, &created); err != nil {
			return memoryNotePathPlan{}, closeRowsWith(rows, err)
		}
		holderOf[path] = noteID
		createdAt[noteID] = created
	}
	if err := closeRows(rows); err != nil {
		return memoryNotePathPlan{}, err
	}

	writing := make(map[string]struct{}, len(writes))
	for _, note := range writes {
		writing[note.NoteID] = struct{}{}
	}

	// vacates answers the only question that matters about an incumbent: will
	// this pass have moved it off this name by the time the transaction ends?
	vacates := func(incumbent string) bool {
		if _, queued := writing[incumbent]; queued {
			_, waiting := plan.deferred[incumbent]
			return !waiting
		}
		if !removalEnabled {
			return false
		}
		if _, seen := scannedIDs[incumbent]; seen {
			return false
		}
		return createdAt[incumbent] < anchor
	}

	for {
		settled := true
		for _, note := range writes {
			if _, waiting := plan.deferred[note.NoteID]; waiting {
				continue
			}
			incumbent, held := holderOf[note.RelPath]
			if !held || incumbent == note.NoteID || vacates(incumbent) {
				continue
			}
			plan.deferred[note.NoteID] = struct{}{}
			settled = false
		}
		if settled {
			break
		}
	}

	for _, note := range writes {
		if _, waiting := plan.deferred[note.NoteID]; waiting {
			continue
		}
		if incumbent, held := holderOf[note.RelPath]; held && incumbent != note.NoteID {
			plan.park = append(plan.park, incumbent)
		}
	}
	plan.park = sortedUniqueStrings(plan.park)
	return plan, nil
}

// parkMemoryNotePathsTx vacates each contested name. Only the path moves:
// updated_at is left alone because nothing about the note itself changed, and a
// note that reads as freshly touched every time a neighbour is renamed is a
// note whose history stops meaning anything.
func parkMemoryNotePathsTx(ctx context.Context, tx *sql.Tx, noteIDs []string) error {
	for _, noteID := range noteIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE memory_notes SET path = ? WHERE id = ?
		`, parkedMemoryNotePath(noteID), noteID); err != nil {
			return err
		}
	}
	return nil
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
		// exists has lost its support, and so has one whose file already says
		// its evidence was withdrawn. It is kept — the user accepted it — and
		// marked, so nothing answers with it as if it were still grounded.
		withdrawn := note.EvidenceWithdrawn || (len(note.EvidenceRefs) > 0 && len(live) == 0)
		if withdrawn {
			status = MemoryNoteStatusWithdrawn
		}
		// The row is written before its evidence: evidence belongs to the note,
		// and the foreign key that says so is what keeps an orphaned citation
		// from existing at all.
		if err := upsertMemoryNoteTx(ctx, tx, memoryNoteFromRow(note, status)); err != nil {
			return false, false, err
		}
		if err := linkMemoryEvidenceTx(ctx, tx, note.NoteID, note.ContentHash, live); err != nil {
			return false, false, err
		}
		if err := recordMemoryReconcileTx(ctx, tx, memoryNoteIndexedAction, note.NoteID, status); err != nil {
			return false, false, err
		}
		if withdrawn {
			if err := recordMemoryReconcileTx(ctx, tx, memoryNoteWithdrawnAction, note.NoteID, "evidence_gone"); err != nil {
				return false, false, err
			}
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
	// A pass that found nothing different writes nothing. The row is the same
	// row, but an UPDATE is not free here: memory_notes_fts is an
	// external-content index maintained by trigger, so every no-op write
	// deletes and reinserts the note's tokens, and updated_at moves — telling
	// the user their memory changed every time anything looked at it.
	if desired := memoryNoteFromRow(note, status); !sameIndexedNote(existing, desired) {
		if err := upsertMemoryNoteTx(ctx, tx, desired); err != nil {
			return false, false, err
		}
	}
	// Only the transition is recorded, not every pass over an unchanged note:
	// a log that fills with "reconcile looked at this and left it alone" is one
	// nobody can read the real events out of.
	if withdrawn {
		if err := recordMemoryReconcileTx(ctx, tx, memoryNoteWithdrawnAction, note.NoteID, "evidence_gone"); err != nil {
			return false, false, err
		}
	}
	return false, withdrawn, nil
}

// sameIndexedNote compares the four columns a pass may rewrite. Identity is the
// key and is equal by construction; created_at is never rewritten; updated_at is
// the thing being decided about and so cannot be part of the decision.
func sameIndexedNote(existing MemoryNote, desired MemoryNote) bool {
	return existing.Path == desired.Path &&
		existing.Content == desired.Content &&
		existing.ContentHash == desired.ContentHash &&
		existing.Status == desired.Status
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
// on. keptIDs is every identity the walk saw, not only the ones this pass
// wrote, because a note it read and declined to index is still a note the user
// has. Rows newer than this pass's anchor are left alone: they were written
// while the walk was already in progress, so this pass never saw their files.
func removeVanishedNotesTx(
	ctx context.Context,
	tx *sql.Tx,
	anchor string,
	keptIDs map[string]struct{},
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
		if _, kept := keptIDs[noteID]; kept {
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
		if err := recordMemoryReconcileTx(ctx, tx, memoryNoteRemovedAction, noteID, "file_missing"); err != nil {
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
//
// Both are also inferred from absence, so both are gated on the walk having
// read the inbox in full. An inbox the walk could not enumerate holds every
// candidate it ever did, as far as anyone here knows.
func (r *Repository) sweepVaultInbox(ctx context.Context, state memoryVaultState, scanned scannedVault) (int, int, error) {
	if inbox := scanned.completeness.Area(memoryfiles.AreaInbox); !inbox.Complete {
		return 0, 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	// A profile edit the user moved into beliefs/ is not a finished promotion:
	// nothing promoted it, nothing may, and the file is still in the vault. Its
	// candidate row and its reservation are what let the user move it back or
	// reject it, so an inbox that no longer holds the path is not evidence
	// that either is stale. The identity is the link — it travels with the
	// file, the path does not.
	held, err := profileEditsToRetainTx(ctx, tx, scanned)
	if err != nil {
		return 0, 0, err
	}
	retain := func(vaultPath string) bool {
		if len(held) == 0 {
			return false
		}
		noteID := memoryfiles.NoteIDFromInboxRelPath(vaultPath)
		if noteID == "" {
			return false
		}
		_, misplaced := held[noteID]
		return misplaced
	}

	orphans, err := staleRowsTx(ctx, tx, `
		SELECT id, inbox_path FROM memory_candidates WHERE created_at < ?
	`, scanned.inboxPaths, retain, state.anchor)
	if err != nil {
		return 0, 0, err
	}
	for _, candidateID := range orphans {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_candidates WHERE id = ?`, candidateID); err != nil {
			return 0, 0, err
		}
		if err := recordMemoryReconcileTx(ctx, tx, memoryCandidateOrphanedAction, candidateID, "inbox_file_missing"); err != nil {
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
	`, scanned.inboxPaths, retain, state.anchor)
	if err != nil {
		return 0, 0, err
	}
	for _, artifactID := range reservations {
		if _, err := tx.ExecContext(ctx, `DELETE FROM vault_artifacts WHERE id = ?`, artifactID); err != nil {
			return 0, 0, err
		}
		if err := recordMemoryReconcileTx(ctx, tx, memoryReservationReleasedAction, artifactID, "inbox_file_missing"); err != nil {
			return 0, 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return len(orphans), len(reservations), nil
}

// incompleteVaultAreas turns the walk's own account of what it could read into
// the report the caller sees. The order is fixed — root, then the two areas —
// so a report is stable to compare across passes.
func incompleteVaultAreas(completeness memoryfiles.ScanCompleteness) []MemoryVaultAreaIssue {
	var issues []MemoryVaultAreaIssue
	for _, area := range []struct {
		name string
		scan memoryfiles.AreaScan
	}{
		{memoryVaultRootArea, completeness.Root},
		{string(memoryfiles.AreaBeliefs), completeness.Beliefs},
		{string(memoryfiles.AreaInbox), completeness.Inbox},
	} {
		if area.scan.Complete {
			continue
		}
		issues = append(issues, MemoryVaultAreaIssue{Area: area.name, Reason: area.scan.Reason})
	}
	return issues
}

// profileEditsToRetainTx names the proposals the inbox sweep must not retire,
// by identity — the one thing that travels with a file the user moved.
//
// When the walk read beliefs/ in full, that is exactly the profile edits it
// found sitting there: the scan already refuses to index them and says why, and
// this is the same fact in the shape the sweep needs, so the two cannot drift
// into disagreeing about which files are still awaiting a decision.
//
// When it did not, the answer has to be every profile edit still on the books.
// A folder the walk could not open may be holding one, and "the walk found no
// misplaced proposal" read off a folder nobody could list is the same mistake
// as "the note was deleted" read off one — which is why the belief removal is
// gated on the very same completeness. Retaining too much costs one more pass;
// retaining too little leaves the user a proposal in their vault with no row
// saying it was ever proposed, and no way to apply or reject it.
func profileEditsToRetainTx(ctx context.Context, tx *sql.Tx, scanned scannedVault) (map[string]struct{}, error) {
	held := map[string]struct{}{}
	if scanned.completeness.Area(memoryfiles.AreaBeliefs).Complete {
		for _, note := range scanned.beliefs {
			if note.Kind != memoryfiles.KindProfileEdit || note.NoteID == "" {
				continue
			}
			held[note.NoteID] = struct{}{}
		}
		return held, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT inbox_path FROM memory_candidates WHERE kind = ?
	`, MemoryCandidateKindProfileEdit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var inboxPath string
		if err := rows.Scan(&inboxPath); err != nil {
			return nil, closeRowsWith(rows, err)
		}
		if noteID := memoryfiles.NoteIDFromInboxRelPath(inboxPath); noteID != "" {
			held[noteID] = struct{}{}
		}
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	return held, nil
}

// staleRowsTx returns the ids of rows whose recorded path is no longer an inbox
// entry. The query is a fixed code constant and every value is bound.
//
// retain is the one exception: a row whose file the walk found elsewhere in the
// vault is not stale, and the caller is the only thing that can say so.
func staleRowsTx(
	ctx context.Context,
	tx *sql.Tx,
	query string,
	present map[string]struct{},
	retain func(string) bool,
	args ...any,
) ([]string, error) {
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
		if retain != nil && retain(path) {
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
