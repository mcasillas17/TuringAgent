package memoryfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

// detachedEntry is one vault entry that has been taken off its name in a single
// atomic step and now lives under a reserved private name inside the same
// confined directory.
//
// Every deletion in this package goes through one, because an unlink names a
// name and the entry under a name is not something a caller can hold still.
// Obsidian, a sync client and Turing's own writer all replace a file by writing
// a new one beside it and renaming over the top, so between the check and the
// unlink the name can be somebody else's file — and an editor that already had
// the note open saves in place, which keeps the inode and changes every word.
// Detaching first collapses that window: what is verified afterwards is an entry
// nothing else can reach, because nothing else knows the name it is under.
//
// The rule this type exists to keep is that only a verified detached entry is
// ever unlinked. Anything else goes back under its own name; when it cannot, it
// is kept somewhere it can still be found and the caller is told where. Nothing
// here deletes a file it could not prove was its own.
type detachedEntry struct {
	vault   *Vault
	parent  *os.File
	leaf    string
	clean   string
	staging string
}

// detachEntry reserves a private name inside parent and renames leaf onto it.
//
// It answers (nil, nil) when the name was already gone. Every deletion here has
// to tolerate that: the caller may be retrying after a crash that already did
// the work, and a file the user asked not to have is a file that is not there.
func (v *Vault) detachEntry(ctx context.Context, parent *os.File, leaf string, clean string) (*detachedEntry, error) {
	v.detachBarrier(detachPhaseBeforeDetach, clean)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	staging, err := reserveStagingName(parent)
	if err != nil {
		return nil, fmt.Errorf("stage the removal of %q: %w", clean, err)
	}
	if err := unix.Renameat(int(parent.Fd()), leaf, int(parent.Fd()), staging); err != nil {
		_ = unix.Unlinkat(int(parent.Fd()), staging, 0)
		if errors.Is(err, unix.ENOENT) {
			return nil, nil
		}
		return nil, fmt.Errorf("detach %q before removing it: %w", clean, err)
	}
	v.detachBarrier(detachPhaseBeforeVerify, clean)
	return &detachedEntry{vault: v, parent: parent, leaf: leaf, clean: clean, staging: staging}, nil
}

// stagedRelPath is where the bytes are while they are off their name. It is
// named in every refusal that leaves them there, because a file somebody can
// still find is recoverable and a deleted one is not.
func (d *detachedEntry) stagedRelPath() string {
	return vaultRelPath(path.Dir(d.clean), d.staging)
}

// vaultRelPath joins a directory and an entry name the way the rest of this
// package spells a vault path, including at the root, where path.Dir answers
// "." and a caller would otherwise report "./profile.md".
func vaultRelPath(directory string, name string) string {
	if directory == "" || directory == "." || directory == "/" {
		return name
	}
	return directory + "/" + name
}

// stat inspects the detached entry through the private name it is under.
func (d *detachedEntry) stat() (unix.Stat_t, error) {
	var stat unix.Stat_t
	err := unix.Fstatat(int(d.parent.Fd()), d.staging, &stat, unix.AT_SYMLINK_NOFOLLOW)
	return stat, err
}

// open opens the detached entry and answers with the identity of the inode that
// was opened rather than of the name that opened it.
func (d *detachedEntry) open() (*os.File, unix.Stat_t, error) {
	return openConfinedEntry(d.parent, d.staging, d.stagedRelPath())
}

// objectionToOwnWrite answers whether the detached entry is still the write the
// caller made, and says in a sentence why not when it is not. An empty answer is
// the only thing that authorises the unlink.
//
// It re-opens the detached entry rather than holding a descriptor from before,
// which is what distinguishes it from objectionToDetachedEntry next door: a
// rejection is bound to a candidate it opened before touching anything, and a
// writer undoing its own work is bound to bytes it wrote and nothing else.
//
// Both halves are load-bearing and neither substitutes for the other. The inode
// says the name was not swapped for a different file; the bytes say nobody
// rewrote it in place, which is exactly what an editor with the note already
// open does. A removal authorised by identity alone deletes the user's newer
// words under the name of the older ones.
func (d *detachedEntry) objectionToOwnWrite(ctx context.Context, expected unix.Stat_t, expectedContent string) string {
	opened, openedStat, err := d.open()
	if err != nil {
		return fmt.Sprintf("it could not be read again before removal (%v)", err)
	}
	defer func() { _ = opened.Close() }()
	if openedStat.Dev != expected.Dev || openedStat.Ino != expected.Ino {
		return "another file had taken its name"
	}
	// The read is bounded by exactly what was written. Anything longer is not
	// those bytes, and the refusal below is the same either way.
	content, readErr := readEntryContent(ctx, opened, d.stagedRelPath(), len(expectedContent))
	if readErr != nil {
		return fmt.Sprintf("it could not be read again before removal (%v)", readErr)
	}
	if ContentHash(content) != ContentHash(expectedContent) {
		return "it was rewritten in place"
	}
	return ""
}

// discard unlinks the detached entry and flushes the directory both the detach
// and the unlink changed. It is the only unlink on this path, and every caller
// reaches it only after proving what it is about to remove.
//
// A failure says where the bytes actually are, so a detached file is never left
// unaccounted for in a sentence that reports it gone. The flush after a failed
// unlink is part of that sentence rather than a formality: if it fails too, the
// name the bytes are under is not one a crash is guaranteed to keep, and a
// caller pointing somebody at that name has to know.
func (d *detachedEntry) discard() error {
	if err := d.vault.unlinkStaging(d.parent, d.staging); err != nil && !errors.Is(err, unix.ENOENT) {
		if flushErr := d.vault.syncDirectory(d.parent); flushErr != nil {
			return fmt.Errorf(
				"remove the detached %q: %w; it is at %s, and the vault directory could not be flushed either (%v)",
				d.clean, err, d.stagedRelPath(), flushErr,
			)
		}
		return fmt.Errorf("remove the detached %q: %w; it is at %s", d.clean, err, d.stagedRelPath())
	}
	if err := d.vault.syncDirectory(d.parent); err != nil {
		return fmt.Errorf("sync vault directory after removing %q: %w", d.clean, err)
	}
	return nil
}

// detachRecovery names where the bytes of a detached entry may be kept when
// their own name cannot be taken back.
type detachRecovery int

const (
	// recoverAsVisibleDraft links the bytes under a name this server mints and
	// the vault walk indexes. It is the right answer for an inbox entry: a
	// draft nobody has read is exactly what the inbox is for, the user sees it
	// in the next listing, and they can delete it like any other file of theirs.
	recoverAsVisibleDraft detachRecovery = iota

	// recoverUnderReservedName leaves the bytes exactly where the detach put
	// them, under the private name the vault walk steps over.
	//
	// It is the right answer everywhere the visible alternative would be worse
	// than hiding — under beliefs/, and at the vault root beside the pinned
	// documents. A note the walk indexes there is one reconcile treats as an
	// accepted belief, so publishing a file this package could not even prove
	// was its own would fabricate a memory the user never accepted. The bytes
	// are still on disk and the refusal names the entry, which is what keeps
	// "recoverable" true without inventing a belief to make it so.
	recoverUnderReservedName
)

// recoveryFor is the single rule for where a detached entry may be kept when
// its own name cannot be taken back.
//
// Only the inbox can hold a visible one. A file that appears there is a draft
// waiting to be read, which is what the folder is for and what the user will
// recognise; the same file appearing under beliefs/ is a claim they are told
// Turing believes, and one at the vault root sits beside the pinned documents.
// So the visible rescue is scoped to the one area where being seen is the whole
// point, and everywhere else the bytes stay reserved and the caller says where.
func recoveryFor(clean string) detachRecovery {
	if strings.HasPrefix(clean, InboxDirName+"/") {
		return recoverAsVisibleDraft
	}
	return recoverUnderReservedName
}

// detachedPlacement says where the bytes of a detached entry ended up once the
// deletion decided it had no standing to remove them. It never says "deleted",
// because nothing on this path deletes.
//
// Every field exists because some caller has to be able to say a true sentence
// about a file it is holding. A restore that could not be flushed, a staging
// link that would not go away and a recovery name whose directory never reached
// the disk are three different facts, and collapsing them into "restored" is
// how a refusal ends up promising a tidiness nobody checked.
type detachedPlacement struct {
	// restored reports whether the bytes are back under their own name and
	// that link has reached the disk. It is never set on the strength of a
	// link alone: a link this process can see and a link a crash would see are
	// not the same thing.
	restored bool
	// linkedBack reports the in-between state: the entry is under its own name
	// as far as this process is concerned, and the flush that would make that
	// true after a crash failed. The staging name is deliberately still there,
	// so the bytes are reachable whichever way the directory lands.
	linkedBack bool
	// recoveryRelPath names the other place the bytes were kept: the only place
	// when restored is false, and a duplicate that could not be dropped when it
	// is true. It is empty exactly when the restore was clean.
	recoveryRelPath string
	// recoveryHidden reports whether that name is one the vault walk steps over,
	// so a caller can say which kind of "it is not lost" this is.
	recoveryHidden bool
	// cause is the failure that stopped the restore, carried rather than only
	// described so a caller can match on it.
	cause error
	// residueRelPath and residueErr name a second link to the entry that this
	// restore did not clear away cleanly, and what stopped it.
	residueRelPath string
	residueErr     error
	// residueRemains says which of the two that is. A link the unlink refused
	// to drop is on disk now and somebody can go and look at it. A link that
	// was dropped without the drop reaching the disk is not there at all until
	// a crash brings it back. Reporting the second as the first sends a person
	// after a file that is not there, which is the same class of untruth as
	// reporting a refusal as a removal.
	residueRemains bool
	// flushErr is a directory fsync that failed after the bytes were placed. It
	// does not move them anywhere; it means the placement this describes is not
	// known to have survived a crash, and a caller that says where a file is
	// owes the reader that.
	flushErr error
}

// clean reports whether the entry is back under its own name with nothing left
// to say about it. Every caller that phrases a plain "it was left alone" asks
// this rather than reading restored on its own, because a restore with a
// duplicate beside it or an fsync missing underneath it is not that sentence.
func (p detachedPlacement) clean() bool {
	return p.restored && !p.linkedBack &&
		p.recoveryRelPath == "" && p.residueRelPath == "" &&
		p.cause == nil && p.residueErr == nil && p.flushErr == nil
}

// failure is every reason this placement is not a clean restore, as one error a
// caller can match on. The sentence says what happened to a person; this is
// what an operator greps for, and EIO, ENOSPC and "another writer took the
// name" are the same sentence and different problems.
func (p detachedPlacement) failure() error {
	return errors.Join(p.cause, p.residueErr, p.flushErr)
}

// putBack returns a detached entry to its own name, and says where the bytes
// are when it cannot. It never unlinks them.
//
// The link refuses to overwrite, which is the whole point: whatever now holds
// the name is another writer's file and this one may not clobber it. Where the
// bytes go instead is not a caller's choice — it follows from the area the entry
// is in, so no caller can ask for a file to be published somewhere publishing it
// would be a lie.
//
// The order is the same one the rescue next door keeps, and for the same
// reason: link, flush, drop, flush. A rename and a link both live in the
// directory's page cache until an fsync, so dropping the staging name before
// the restore is on disk is the one ordering in which a crash can leave the
// bytes reachable from no name at all. Every step that fails is reported rather
// than swallowed — a caller that has to tell the user where their file is
// cannot do it from a placement that guessed.
func (d *detachedEntry) putBack() detachedPlacement {
	recovery := recoveryFor(d.clean)
	d.vault.detachBarrier(detachPhaseBeforeRestore, d.clean)
	if linkErr := d.vault.linkDetached(d.parent, d.staging, d.leaf); linkErr != nil {
		return d.preserve(recovery, linkErr)
	}
	if flushErr := d.vault.syncDirectory(d.parent); flushErr != nil {
		// Both names hold the entry and neither is known to be durable. The
		// staging one stays exactly where it is: it is the name the detach
		// already put the bytes under, and dropping it now to tidy up would
		// trade the one link a crash might still find for one it might not.
		return detachedPlacement{
			linkedBack:      true,
			cause:           flushErr,
			recoveryRelPath: d.stagedRelPath(),
			recoveryHidden:  true,
		}
	}
	if err := d.vault.unlinkStaging(d.parent, d.staging); err != nil && !errors.Is(err, unix.ENOENT) {
		// The entry is durably back under its own name, so what is left is a
		// second link to a file the user already has. That is residue to be
		// named, not a rescue to be published: a visible copy of a claim that
		// is already in the inbox is the same proposal twice, and the user
		// would have to decide about both.
		placement := detachedPlacement{
			restored:       true,
			residueRelPath: d.stagedRelPath(),
			residueErr:     err,
			residueRemains: true,
		}
		if flushErr := d.vault.syncDirectory(d.parent); flushErr != nil {
			placement.flushErr = flushErr
		}
		return placement
	}
	placement := detachedPlacement{restored: true}
	if flushErr := d.vault.syncDirectory(d.parent); flushErr != nil {
		// The restore is on disk; dropping the duplicate is not. After a crash
		// the staging name can come back, so it is named rather than assumed
		// gone.
		placement.residueRelPath = d.stagedRelPath()
		placement.residueErr = flushErr
	}
	return placement
}

// preserve is the one exit for bytes that could not go back under their own
// name. It never unlinks: it moves them somewhere findable if it may, and says
// where they are if it may not.
func (d *detachedEntry) preserve(recovery detachRecovery, cause error) detachedPlacement {
	placement := detachedPlacement{cause: cause}
	if recovery == recoverUnderReservedName {
		if flushErr := d.vault.syncDirectory(d.parent); flushErr != nil {
			placement.flushErr = flushErr
		}
		placement.recoveryRelPath = d.stagedRelPath()
		placement.recoveryHidden = true
		return placement
	}
	name, staged, rescueErr := d.vault.rescueDetachedEntry(d.parent, d.staging)
	if name == "" {
		placement.recoveryRelPath = d.stagedRelPath()
		placement.recoveryHidden = true
		placement.residueErr = rescueErr
		return placement
	}
	placement.recoveryRelPath = vaultRelPath(path.Dir(d.clean), name)
	if rescueErr != nil {
		// The visible name exists and holds the bytes; only the tidying after
		// it failed. The reserved name is reported either way, and staged says
		// whether it is a link somebody can find today or one a crash can
		// bring back.
		placement.residueRelPath = d.stagedRelPath()
		placement.residueErr = rescueErr
		placement.residueRemains = staged
	}
	return placement
}

// explain says where the bytes are, in the caller's own words plus this one's
// facts. Every branch ends in a place a person can go and look.
//
// The caller supplies only why it declined to delete; how that reads once the
// file could not go back is this type's business, because it is the only thing
// that knows whether the name was taken, the tidying afterwards was what
// failed, or the directory never reached the disk.
func (p detachedPlacement) explain(why string) string {
	if p.linkedBack {
		return fmt.Sprintf(
			"%s and was put back under its own name, but the vault directory could not be flushed (%v), so that is not known to have survived a crash; it is not lost — it is also at %s",
			why, p.cause, p.recoveryRelPath,
		)
	}
	if p.restored {
		return p.note(p.withResidue(why + ", so it was left alone"))
	}
	reason := why + " and could not be put back under its own name"
	switch {
	case p.recoveryHidden && p.residueErr != nil:
		return p.note(fmt.Sprintf(
			"%s (%v), and no recovery name could be taken for it (%v); it is not lost — it is at %s",
			reason, p.cause, p.residueErr, p.recoveryRelPath,
		))
	case p.recoveryHidden:
		return p.note(fmt.Sprintf(
			"%s (%v); it is not lost — it was kept for recovery at %s, where nothing indexes it as a note",
			reason, p.cause, p.recoveryRelPath,
		))
	default:
		return p.note(p.withResidue(fmt.Sprintf(
			"%s (%v); it is not lost — it was kept for recovery at %s",
			reason, p.cause, p.recoveryRelPath,
		)))
	}
}

// withResidue adds the second link this restore did not clear away, in the
// tense it is actually in. There are two of those and they send a person to
// different places: one is a file to go and look at, the other is a name that
// is gone until a crash brings it back.
func (p detachedPlacement) withResidue(sentence string) string {
	switch {
	case p.residueRelPath == "":
		return sentence
	case p.residueRemains:
		return fmt.Sprintf("%s, but a second link to it could not be dropped (%v); that link is at %s", sentence, p.residueErr, p.residueRelPath)
	default:
		return fmt.Sprintf(
			"%s; the second link at %s was dropped, but that removal was not flushed (%v), so the name can come back after a crash",
			sentence, p.residueRelPath, p.residueErr,
		)
	}
}

// note appends the one fact that qualifies every other sentence here: the
// directory holding these names never reached the disk, so what was just said
// is true of this process and not yet of a crash.
func (p detachedPlacement) note(sentence string) string {
	if p.flushErr == nil {
		return sentence
	}
	return fmt.Sprintf("%s; the vault directory could not be flushed afterwards (%v), so that placement is not known to have survived a crash", sentence, p.flushErr)
}

// rescueDetachedEntry moves bytes a deletion is holding out of the private
// staging name and under a visible one in the same confined directory.
//
// The name is minted rather than derived from anything in the file: a rescued
// entry may be a claim about the user nobody has read, and a name built from its
// contents would publish some of it into a directory listing. The link refuses
// to overwrite and a taken name is retried, so a rescue can never clobber
// another rescue or anything else the user has.
//
// The order is what makes it durable: link, flush, then drop the staging name,
// then flush again. A crash anywhere in it leaves the bytes reachable under at
// least one name, which is the property the whole path exists for.
//
// The second answer is whether the staging name is still a link on disk. It is
// not a detail: a caller reporting where a file is has to know the difference
// between a reserved name somebody can go and open and one this call dropped
// without the drop reaching the disk.
func (v *Vault) rescueDetachedEntry(parent *os.File, staging string) (string, bool, error) {
	for attempt := 0; attempt < 16; attempt++ {
		name, err := v.mintRecoveryName()
		if err != nil {
			return "", true, err
		}
		linkErr := v.linkDetached(parent, staging, name)
		if errors.Is(linkErr, unix.EEXIST) {
			continue
		}
		if linkErr != nil {
			return "", true, linkErr
		}
		if err := v.syncDirectory(parent); err != nil {
			return name, true, err
		}
		if err := v.unlinkStaging(parent, staging); err != nil && !errors.Is(err, unix.ENOENT) {
			return name, true, err
		}
		if err := v.syncDirectory(parent); err != nil {
			return name, false, err
		}
		return name, false, nil
	}
	return "", true, errors.New("could not allocate a recovery name")
}

// reserveStagingName takes a random private name inside the entry's own
// directory, exclusively, so the detach has somewhere to move the file that
// nothing else can be holding. The name is created rather than merely
// generated: O_EXCL is what makes "unused" a fact instead of a probability.
func reserveStagingName(parent *os.File) (string, error) {
	staging, name, err := createStagingFile(parent, 0o600)
	if err != nil {
		return "", err
	}
	if err := staging.Close(); err != nil {
		_ = unix.Unlinkat(int(parent.Fd()), name, 0)
		return "", err
	}
	return name, nil
}

// removalOutcome says what a compensating removal actually did to the name it
// was pointed at. It is three answers rather than a bool because "there was
// nothing there" and "what was there was not mine" lead callers in opposite
// directions.
type removalOutcome int

const (
	// removalMissing: the name was already gone before anything was detached.
	removalMissing removalOutcome = iota
	// removalDone: the verified entry left its name and was unlinked.
	removalDone
	// removalRefused: what was under the name could not be proved to be the
	// caller's, so it was put back or kept, and nothing was deleted.
	removalRefused
)

// removeVerifiedEntry is the compensating deletion every writer in this package
// shares: it takes the entry off its name atomically, proves the detached entry
// is both the inode and the bytes the caller wrote, and unlinks only that.
//
// It is one function rather than one per caller because the unsafe part is
// identical in all of them, and a second copy of it is a second place for the
// window between "check" and "unlink" to reopen.
func (v *Vault) removeVerifiedEntry(
	ctx context.Context,
	parent *os.File,
	leaf string,
	clean string,
	expected unix.Stat_t,
	expectedContent string,
) (removalOutcome, detachedPlacement, string, error) {
	detached, err := v.detachEntry(ctx, parent, leaf, clean)
	if err != nil {
		return removalMissing, detachedPlacement{}, "", err
	}
	if detached == nil {
		return removalMissing, detachedPlacement{}, "", nil
	}
	objection := detached.objectionToOwnWrite(ctx, expected, expectedContent)
	// The file is off its name and nothing has decided yet whether it may go. A
	// request that has ended by now is not a decision, however the verification
	// came out: a read that stopped because the caller had gone proves nothing,
	// and one that finished was answered for somebody who is no longer there.
	// Either way the bytes go back, and the cancellation is what this reports.
	//
	// This is the single place the question is asked, so the branch is the one
	// a test reaches by cancelling the request at all.
	if ended := ctx.Err(); ended != nil {
		return removalRefused, detached.putBack(), "the request ended before the removal could be verified", ended
	}
	if objection != "" {
		return removalRefused, detached.putBack(), objection, nil
	}
	if err := detached.discard(); err != nil {
		// The name is gone either way, so the caller must not treat this as a
		// removal that never happened.
		return removalDone, detachedPlacement{}, "", err
	}
	return removalDone, detachedPlacement{}, "", nil
}
