package repository

import (
	"context"
	"database/sql"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// memoryUnavailableDisabled and memoryUnavailableVaultMissing are the two typed
// reasons this layer produces on its own, for the cases memoryfiles never sees:
// the user turned memory off, or the orchestrator has no vault to ask.
const (
	memoryUnavailableDisabled     = "disabled"
	memoryUnavailableVaultMissing = string(memoryfiles.UnavailableVaultMissing)
)

// MemoryPinnedDocument is persona.md or profile.md as one run would carry it.
//
// Content is the post-truncation bytes with the truncation notice already
// attached — what a model would actually be shown — because everything
// downstream (the fingerprint, the prompt, the disclosure) is a statement about
// that and not about the file on disk.
//
// Available and Reason are kept apart from Content on purpose: a document that
// could not be read pins nothing and says why, and an empty body is a different
// fact from an absent one.
type MemoryPinnedDocument struct {
	RelPath     string
	Content     string
	ContentHash string
	Truncated   bool
	Available   bool
	Reason      string
	Detail      string
}

// MemoryEgressSnapshot is the whole of the pinned memory one send is decided
// over: the toggle, and the two pinned documents as they read at that moment.
type MemoryEgressSnapshot struct {
	Enabled bool
	Persona MemoryPinnedDocument
	Profile MemoryPinnedDocument
}

// Preimage projects the snapshot into the shape all three parties hash. The
// selected tools come from the caller because they are frozen elsewhere — by
// the routing decision, not by the vault — and the memory category depends on
// them as much as on the pins.
//
// Canonical is called here, at the one place this repository turns a read of
// the vault into the preimage everything downstream trusts: the fingerprint,
// and the frozen job body a worker will inject. An unavailable document should
// never carry Content today, but should is not a guarantee this projection
// gets to lean on for what a withheld tier is allowed to say it pinned.
func (s MemoryEgressSnapshot) Preimage(selectedTools []string) backendegress.MemorySnapshot {
	return backendegress.MemorySnapshot{
		PersonaID:           s.Persona.RelPath,
		PersonaDisplayName:  s.Persona.RelPath,
		PersonaBody:         s.Persona.Content,
		PersonaContentHash:  s.Persona.ContentHash,
		PersonaWithheld:     !s.Persona.Available,
		ProfileID:           s.Profile.RelPath,
		ProfileBody:         s.Profile.Content,
		ProfileContentHash:  s.Profile.ContentHash,
		ProfileWithheld:     !s.Profile.Available,
		MemoryToolsSelected: backendegress.SelectedToolsIncludeMemory(selectedTools),
	}.Canonical()
}

// EgressMemorySnapshot reads the toggle and the two pinned documents in one
// operation, so the bytes a consent is granted over are the bytes that were on
// disk when the toggle was read.
//
// Exactly two files are opened. There is no walk, no index and no scan here:
// this runs on the send path and, in its transactional form, inside the enqueue
// transaction, where an unbounded traversal of a vault the user may be editing
// would hold a write lock for as long as their vault is large.
func (r *Repository) EgressMemorySnapshot(ctx context.Context) (MemoryEgressSnapshot, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return MemoryEgressSnapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()
	snapshot, err := r.egressMemorySnapshotTx(ctx, tx)
	if err != nil {
		return MemoryEgressSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return MemoryEgressSnapshot{}, err
	}
	return snapshot, nil
}

// EgressMemorySnapshotFingerprint is the send path's one entry point: it reads
// the toggle and both pinned documents once and returns the fingerprint bound
// to the tools this run would carry, alongside the snapshot the disclosure is
// written from.
//
// One read, one fingerprint. A caller that read the vault and then hashed a
// second read could disclose one persona and bind another.
func (r *Repository) EgressMemorySnapshotFingerprint(
	ctx context.Context,
	selectedTools []string,
) (string, MemoryEgressSnapshot, error) {
	snapshot, err := r.EgressMemorySnapshot(ctx)
	if err != nil {
		return "", MemoryEgressSnapshot{}, err
	}
	fingerprint, err := backendegress.MemorySnapshotFingerprint(snapshot.Preimage(selectedTools))
	if err != nil {
		return "", MemoryEgressSnapshot{}, err
	}
	return fingerprint, snapshot, nil
}

func (r *Repository) egressMemorySnapshotTx(ctx context.Context, q rowQuerier) (MemoryEgressSnapshot, error) {
	enabled, err := memoryEnabledTx(ctx, q)
	if err != nil {
		return MemoryEgressSnapshot{}, err
	}
	snapshot := MemoryEgressSnapshot{Enabled: enabled}
	if !enabled {
		snapshot.Persona = withheldPinnedDocument(memoryfiles.PersonaFileName, memoryUnavailableDisabled)
		snapshot.Profile = withheldPinnedDocument(memoryfiles.ProfileFileName, memoryUnavailableDisabled)
		return snapshot, nil
	}
	if r.memoryVault == nil {
		snapshot.Persona = withheldPinnedDocument(memoryfiles.PersonaFileName, memoryUnavailableVaultMissing)
		snapshot.Profile = withheldPinnedDocument(memoryfiles.ProfileFileName, memoryUnavailableVaultMissing)
		return snapshot, nil
	}
	snapshot.Persona = pinnedDocument(r.memoryVault.LoadPersona(ctx))
	snapshot.Profile = pinnedDocument(r.memoryVault.LoadProfile(ctx))
	return snapshot, nil
}

func pinnedDocument(document memoryfiles.PinnedDocument) MemoryPinnedDocument {
	return MemoryPinnedDocument{
		RelPath:     document.RelPath,
		Content:     document.Content,
		ContentHash: document.ContentHash,
		Truncated:   document.Truncated,
		Available:   document.Available,
		Reason:      string(document.Reason),
		Detail:      document.Detail,
	}
}

// withheldPinnedDocument is the row for a tier that was never opened. It says
// which document it is about and why nothing came back, and it deliberately
// carries no hash: hashing the empty string here would produce a content hash
// for content that was never read.
func withheldPinnedDocument(relPath string, reason string) MemoryPinnedDocument {
	return MemoryPinnedDocument{RelPath: relPath, Available: false, Reason: reason}
}
