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
// It answers whether the name the entry was taken off has actually let go of
// it, because the two outcomes lead callers in opposite directions and only
// this function knows which one happened.
//
// An unlink that fails leaves the bytes off their name, under a reserved
// private name nothing outside this package can guess — and every record that
// says somebody is answerable for them names the name they came off. Left
// there, that is how a file becomes unreachable: the record names a path
// holding nothing, the retry finds nothing and reports the removal done, and
// the bytes stay in the user's vault under a name no listing shows. So the
// entry goes back under its own name first, with a link that refuses to
// clobber whatever may have taken it, and the failure says where everything
// ended up. The retry then finds the file exactly where the record says it is
// and can prove ownership of it all over again.
//
// The flush after a successful unlink is not a formality either: an unlink
// that has not reached the disk is not an absence a crash will honour, and a
// caller that retires a durable record on the strength of one is retiring it
// for a file that can come back.
func (d *detachedEntry) discard() (bool, error) {
	if err := d.vault.unlinkStaging(d.parent, d.staging); err != nil && !errors.Is(err, unix.ENOENT) {
		placement := d.keepAfterFailedRemoval(err)
		// The name holds the entry again exactly when the link happened,
		// whether or not the flush that would survive a crash did.
		gone := !placement.restored && !placement.linkedBack
		return gone, withResidueMarker(placement, fmt.Errorf(
			"remove the detached %q: %w; %s", d.clean, err, placement.afterFailedRemoval(),
		))
	}
	if err := d.vault.syncDirectory(d.parent); err != nil {
		return true, fmt.Errorf("sync vault directory after removing %q: %w", d.clean, err)
	}
	return true, nil
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
	// keptReserved records that no restore was attempted at all, because the
	// entry's own name is one the vault walk indexes and nothing durable points
	// at it. It is not a restore that failed, and must not be described as one.
	keptReserved bool
	// flushErr is a directory fsync that failed after the bytes were placed. It
	// does not move them anywhere; it means the placement this describes is not
	// known to have survived a crash, and a caller that says where a file is
	// owes the reader that.
	flushErr error
}

// leavesResidue reports whether these bytes are reachable under a name only
// this package can spell — the reserved staging one, or a recovery name a
// rescue took. It is the question a caller with a record to retire has to ask:
// the path that record names may hold nothing while the file is still there.
func (p detachedPlacement) leavesResidue() bool {
	return p.recoveryRelPath != "" || (p.residueRelPath != "" && p.residueRemains)
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
	return d.putBackWith(recoveryFor(d.clean))
}

// keepAfterFailedRemoval decides where the bytes of an entry this call was
// entitled to delete are kept when the unlink refuses, and the decision is the
// area's rather than the caller's.
//
// Under inbox/, the entry's own name is what every record of it points at: a
// manifest row, a proposal the user is still being asked about. Putting it back
// there is what lets the retry find it, prove it, and finish.
//
// Under beliefs/ and at the vault root the same act would publish. The only
// removals that reach here in those areas are compensating ones — the rollback
// of a copy a promotion had just installed, the undo of a write that failed —
// and nothing durable names the file they are undoing. Linking it back would
// put a belief the user never accepted into the directory the app presents as
// what Turing holds, and the promotion that was abandoned could never be
// retried, because its destination name would be taken by the copy it rolled
// back. So the bytes stay under the reserved name, where the walk steps over
// them, and the failure says where they are.
func (d *detachedEntry) keepAfterFailedRemoval(cause error) detachedPlacement {
	if recoveryFor(d.clean) != recoverAsVisibleDraft {
		return d.stayStaged(detachedPlacement{cause: cause, keptReserved: true})
	}
	return d.putBackOrStage()
}

// putBackOrStage is the same restore, for an entry a removal had every right to
// delete and could not.
//
// It differs in one decision and only one: it never publishes. The rescue that
// puts a *refused* entry under a visible inbox name is for a proposal nobody
// has answered, and this entry is the opposite — its outcome is already
// recorded, and a second visible copy of it is a decided claim asked again. So
// where the name has been taken the bytes stay under the reserved name, both
// places are named, and the record that tracks the file is kept by the failure
// this returns.
func (d *detachedEntry) putBackOrStage() detachedPlacement {
	return d.putBackWith(recoverUnderReservedName)
}

func (d *detachedEntry) putBackWith(recovery detachRecovery) detachedPlacement {
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
//
// Both roads can end under the reserved staging name — one by design, one when
// the rescue could not take a visible one — and they owe the same fsync. The
// detach that put the bytes there was a rename, and a rename lives in the
// directory's page cache: without the flush, "it is at ..." names a place a
// crash can take away, with the entry back under the name the same sentence
// says it left. So the flush happens before the placement is described, and a
// flush that fails is carried rather than dropped, because the caller is about
// to send somebody to that name.
func (d *detachedEntry) preserve(recovery detachRecovery, cause error) detachedPlacement {
	placement := detachedPlacement{cause: cause}
	if recovery == recoverUnderReservedName {
		return d.stayStaged(placement)
	}
	rescue := d.vault.rescueDetachedEntry(d.parent, d.staging)
	if rescue.name == "" {
		// No visible name was ever linked, so nothing has moved: the bytes are
		// exactly where the detach left them, and this is the same placement
		// the reserved road above makes.
		placement.residueErr = rescue.nameErr
		return d.stayStaged(placement)
	}
	placement.recoveryRelPath = vaultRelPath(path.Dir(d.clean), rescue.name)
	if rescue.flushErr != nil {
		// The visible name holds the bytes as this process sees it, and the
		// fsync that would make that true after a crash did not happen. The
		// rescue stopped there rather than going on to drop the staging name,
		// so both names are on disk and neither is known to be durable. That
		// is two facts and neither is a failed drop: nothing was dropped, and
		// the placement being reported is a placement nobody flushed.
		placement.flushErr = rescue.flushErr
		placement.residueRelPath = d.stagedRelPath()
		placement.residueRemains = true
		return placement
	}
	if rescue.residueErr != nil {
		// The visible name is durable and holds the bytes; only the tidying
		// after it failed. residueRemains says whether the reserved name is a
		// link somebody can find today or one a crash can bring back.
		placement.residueRelPath = d.stagedRelPath()
		placement.residueErr = rescue.residueErr
		placement.residueRemains = rescue.residueRemains
	}
	return placement
}

// stayStaged records that the bytes are staying under the reserved name, and
// flushes the directory that holds it first. It is one function because the two
// roads that end here have to be indistinguishable afterwards: a caller reading
// a placement cannot tell which one it came from, and must not need to.
func (d *detachedEntry) stayStaged(placement detachedPlacement) detachedPlacement {
	if flushErr := d.vault.syncDirectory(d.parent); flushErr != nil {
		placement.flushErr = flushErr
	}
	placement.recoveryRelPath = d.stagedRelPath()
	placement.recoveryHidden = true
	return placement
}

// afterFailedRemoval says where the bytes are when the unlink a verified entry
// had already earned did not happen.
//
// It is separate from explain() because the sentence explain() builds is about
// a refusal — "it was left alone" — and this is not one. The removal was
// entitled to happen and the filesystem would not do it, so what a caller owes
// the reader is where the file is now and whether that is a place a crash will
// keep.
func (p detachedPlacement) afterFailedRemoval() string {
	switch {
	case p.linkedBack:
		return fmt.Sprintf(
			"it is back under its own name, but the vault directory could not be flushed (%v), so that is not known to have survived a crash; it is not lost — it is also at %s",
			p.cause, p.recoveryRelPath,
		)
	case p.restored:
		return p.note(p.withResidue("it is back under its own name"))
	case p.keptReserved:
		return p.note(fmt.Sprintf(
			"it is not lost — it was kept at %s rather than put back under its own name, which the vault indexes as a note nobody accepted",
			p.recoveryRelPath,
		))
	default:
		return p.note(fmt.Sprintf(
			"it could not be put back under its own name (%v); it is not lost — it is at %s, where nothing indexes it as a note",
			p.cause, p.recoveryRelPath,
		))
	}
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
// tense it is actually in. There are three of those and they send a person to
// different places: one is a file to go and look at, one is a name that is gone
// until a crash brings it back, and one is a link nobody tried to drop at all.
//
// That last one is not a failure and must not read like one. It is the rescue
// stopping the moment its flush failed, keeping the name the bytes were already
// under rather than trading a link a crash might find for one it might not.
// Reporting it as a drop that failed blames a step that never ran; the caveat
// note() adds is what says the placement is not known to be durable.
func (p detachedPlacement) withResidue(sentence string) string {
	switch {
	case p.residueRelPath == "":
		return sentence
	case p.residueErr == nil && p.residueRemains:
		return fmt.Sprintf("%s, and it is also still at %s, which was deliberately left in place rather than dropped", sentence, p.residueRelPath)
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
// What it answers with is a rescuePlacement rather than a name and a bool,
// because the three steps that can fail leave three different states behind and
// a caller has to be able to tell them apart. Collapsing them is how the first
// flush ended up reported as a drop that failed — a step that had not run.
func (v *Vault) rescueDetachedEntry(parent *os.File, staging string) rescuePlacement {
	for attempt := 0; attempt < 16; attempt++ {
		name, err := v.mintRecoveryName()
		if err != nil {
			return rescuePlacement{nameErr: err}
		}
		linkErr := v.linkDetached(parent, staging, name)
		if errors.Is(linkErr, unix.EEXIST) {
			continue
		}
		if linkErr != nil {
			return rescuePlacement{nameErr: linkErr}
		}
		if err := v.syncDirectory(parent); err != nil {
			// The staging name is deliberately left alone. Dropping it now
			// would trade the name the detach already put the bytes under —
			// which a crash might still find — for one no fsync has
			// established.
			return rescuePlacement{name: name, flushErr: err}
		}
		if err := v.unlinkStaging(parent, staging); err != nil && !errors.Is(err, unix.ENOENT) {
			return rescuePlacement{name: name, residueErr: err, residueRemains: true}
		}
		if err := v.syncDirectory(parent); err != nil {
			return rescuePlacement{name: name, residueErr: err}
		}
		return rescuePlacement{name: name}
	}
	return rescuePlacement{nameErr: errors.New("could not allocate a recovery name")}
}

// rescuePlacement is how far a rescue got, in the four states it can stop in.
//
// Each field answers a question a caller has to be able to answer about
// somebody's file, and no two of them mean the same thing. The name is where
// the bytes were published. nameErr is why they were not. flushErr says the
// publishing happened but is not known to have survived a crash, and that the
// reserved name is still there because of it. residueErr says the publishing is
// durable and the tidying afterwards is what failed — and residueRemains says
// whether that leftover link is one somebody can go and open today.
//
// The reason this is a struct and not (string, bool, error) is the first flush.
// As one error it was indistinguishable from a failed drop, so the refusal that
// came out of it blamed a step that never ran and promised a location nobody
// had flushed.
type rescuePlacement struct {
	// name is the visible recovery name the bytes are linked under, empty
	// exactly when no visible name was ever taken.
	name string
	// nameErr is why no visible name was taken. Set only when name is empty.
	nameErr error
	// flushErr is the failure of the fsync that would have established the
	// visible link. When it is set the staging name is still on disk, on
	// purpose, and no drop was attempted.
	flushErr error
	// residueErr is what stopped the staging name being dropped cleanly once
	// the visible link was durable.
	residueErr error
	// residueRemains distinguishes a link that would not go away from one that
	// went away without the removal reaching the disk.
	residueRemains bool
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
	// removalKept: the entry was proved to be the caller's and the unlink
	// failed, so it is back under its own name and nothing was deleted. It is
	// separate from a refusal because nothing is wrong with the file — the
	// removal is simply still owed — and separate from a removal because a
	// caller compensating for a move it was making must not act as though the
	// move had happened.
	removalKept
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
		placement := detached.putBack()
		return removalRefused, placement, endedBeforeVerification,
			endedRemoval(clean, placement, endedBeforeVerification, ended)
	}
	if objection != "" {
		placement := detached.putBack()
		// And the same question again, because the restore is where a request
		// most often runs out: it is three directory operations and up to
		// three fsyncs long, and a deadline that expires inside it arrives
		// after the check above and before this answer is chosen. Reporting
		// the objection then would tell a caller that had already gone that
		// the user's own file had moved — a claim about the vault made on
		// nobody's behalf, and one a retry loop matching on staleness would go
		// round again on.
		if ended := ctx.Err(); ended != nil {
			return removalRefused, placement, objection,
				endedRemoval(clean, placement, objection, ended)
		}
		return removalRefused, placement, objection, nil
	}
	if gone, err := detached.discard(); err != nil {
		if !gone {
			// The name holds the entry again, so nothing was removed. A caller
			// undoing half a move has to hear that as "the move did not
			// happen", or it keeps a copy of a file that is still where it
			// started.
			return removalKept, detachedPlacement{}, "", err
		}
		// The name is gone, so the caller must not treat this as a removal
		// that never happened.
		return removalDone, detachedPlacement{}, "", err
	}
	return removalDone, detachedPlacement{}, "", nil
}

// withResidueMarker adds the one fact a caller holding a record has to be able
// to match on: these bytes are still somewhere, under a name nothing outside
// this package can produce. Without it, a caller that reads only "the removal
// failed" retires the record that names the file — and the path that record
// names is exactly the one that now holds nothing.
//
// It adds nothing to the sentence: the placement has already said where the
// bytes are, in words. This is for errors.Is.
func withResidueMarker(placement detachedPlacement, err error) error {
	if err == nil || !placement.leavesResidue() {
		return err
	}
	return &residueError{err: err}
}

// residueError carries ErrVaultResidue beside a failure without spending a word
// on it.
type residueError struct{ err error }

func (e *residueError) Error() string { return e.err.Error() }

func (e *residueError) Unwrap() []error { return []error{ErrVaultResidue, e.err} }

// endedRemoval is the cancellation a compensating removal answers with, carrying
// where it left the bytes.
//
// Both halves travel together and neither may be dropped. What a caller matches
// on is the context error — the request is what ended, and nothing about the
// file was decided — and what rides along is the bounded, content-free sentence
// about where the bytes are, because a caller that has gone still leaves an
// operator who has to find the file.
func endedRemoval(clean string, placement detachedPlacement, reason string, ended error) error {
	detail := boundRefusalDetail(reason + ", so it was left alone")
	if !placement.clean() {
		detail = boundRefusalDetail(placement.explain(reason))
	}
	// A request that ended while the bytes were off their name leaves them
	// exactly where a failed unlink does, so the caller holding a record of the
	// file is owed the same marker. It rides beside the cancellation rather
	// than replacing it: what ended is still the request.
	return &EndedRequestError{RelPath: clean, Detail: detail, Cause: withResidueMarker(placement, ended)}
}
