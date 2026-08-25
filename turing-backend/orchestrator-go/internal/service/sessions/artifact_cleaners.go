package sessions

import (
	"context"
	"errors"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// Artifact cleanup scopes. A scope names one manifest and the one external
// store behind it. They are separate because they answer separate retention
// questions over separate roots — scratch output inside the tool sandbox, and
// user-visible notes inside the vault the user opens in their editor — and
// because either can fail while the other finishes.
const (
	ArtifactScopeSandbox = "sandbox"
	ArtifactScopeVault   = "vault"
)

// SessionArtifactCleaner withdraws the external files of exactly one manifest
// scope, and owns that scope end to end.
//
// Owning it end to end is the point. A cleaner that removed files while
// something else decided which manifest rows to drop could have one scope's
// success erase the other scope's evidence — the failing store's rows
// disappearing because a different store finished. Each cleaner removes its own
// files and forgets its own rows, so a pass where one scope fails leaves
// exactly that scope's rows behind and nobody else's.
type SessionArtifactCleaner interface {
	// ArtifactScope names the manifest this cleaner is responsible for.
	ArtifactScope() string
	// CleanupSessionArtifacts removes the external files this session owns in
	// this scope. It must be idempotent: it is retried after partial failures,
	// and a file that is already gone is the outcome that was wanted.
	CleanupSessionArtifacts(ctx context.Context, sessionID string, lifecycleVersion int64) error
	// ForgetCleanedArtifacts removes the manifest rows for the files this
	// cleaner just removed — its own scope's ids and no others.
	ForgetCleanedArtifacts(ctx context.Context, sessionID string) error
}

// vaultArtifactPurger is the only part of the repository the vault cleaner
// needs. The signature mirrors *repository.Repository exactly, so a stand-in
// cannot quietly answer a different question than production does.
type vaultArtifactPurger interface {
	PurgeSessionVaultArtifacts(ctx context.Context, sessionID string) (int, error)
}

// sandboxArtifactManifest is the only part of the repository the sandbox
// cleaner needs to forget what it removed.
type sandboxArtifactManifest interface {
	SessionSandboxArtifacts(ctx context.Context, sessionID string) ([]repository.SandboxArtifact, error)
	DeleteSandboxArtifact(ctx context.Context, artifactID string) error
}

type vaultArtifactCleaner struct {
	purger vaultArtifactPurger
}

// NewVaultArtifactCleaner withdraws the notes a session wrote into the user's
// vault.
//
// Every file it deletes goes through the repository purge, which removes each
// candidate through the inbox-only primitive. That is the whole confinement
// story: there is no second path to the vault from here, so a manifest row
// rewritten to name a belief or the persona document is refused rather than
// obeyed.
func NewVaultArtifactCleaner(purger vaultArtifactPurger) SessionArtifactCleaner {
	return &vaultArtifactCleaner{purger: purger}
}

func (c *vaultArtifactCleaner) ArtifactScope() string { return ArtifactScopeVault }

func (c *vaultArtifactCleaner) CleanupSessionArtifacts(ctx context.Context, sessionID string, _ int64) error {
	_, err := c.purger.PurgeSessionVaultArtifacts(ctx, sessionID)
	if errors.Is(err, repository.ErrVaultArtifactManifestFinalize) {
		// Every note was removed and only the rows naming them survived. That
		// is not a note this cleaner failed to delete, and returning it as one
		// would mark each surviving row delete_failed and file an audit entry
		// telling the user a note Turing did erase is still on their disk. The
		// outstanding work is real, so it is reported — as finalization, from
		// ForgetCleanedArtifacts, which is the question it actually answers.
		return nil
	}
	return err
}

// ForgetCleanedArtifacts finishes the bookkeeping the purge could not.
//
// The purge removes a manifest row in the same pass as the file it names, so
// after a clean pass there is nothing left to find and this costs one empty
// read. It is not a no-op only because that pass can remove every file and then
// fail to drop the rows: those rows are still the retry's worklist, removal is
// idempotent, and rerunning the purge over notes that are already gone drains
// them. It deliberately does not drop rows on its own — the ones a partial
// failure kept are still naming notes in the user's vault.
func (c *vaultArtifactCleaner) ForgetCleanedArtifacts(ctx context.Context, sessionID string) error {
	_, err := c.purger.PurgeSessionVaultArtifacts(ctx, sessionID)
	return err
}

// memoryVaultReconciler is the only part of the repository the completion
// needs. The signature mirrors *repository.Repository exactly.
type memoryVaultReconciler interface {
	ReconcileMemoryVault(ctx context.Context) (repository.MemoryReconcileReport, error)
}

// NewMemoryReconcileCompletion is the on-disk work a withdrawal owes once its
// rows are gone: a belief the user kept must stop citing the conversation
// Turing has just told them no longer exists.
//
// An install with no vault owes nothing. There are no belief files to rewrite,
// so an unattached vault is a completed obligation rather than a withdrawal
// that can never finish — treating it as a failure would leave every deletion
// on such an install permanently retryable over a promise it already kept.
// Every other failure is returned, and keeps the receipt retryable.
func NewMemoryReconcileCompletion(reconciler memoryVaultReconciler) repository.SessionDeletionCompletion {
	return func(ctx context.Context) error {
		if _, err := reconciler.ReconcileMemoryVault(ctx); err != nil &&
			!errors.Is(err, repository.ErrMemoryVaultUnavailable) {
			return err
		}
		return nil
	}
}
