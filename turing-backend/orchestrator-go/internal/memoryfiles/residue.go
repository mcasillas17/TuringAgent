package memoryfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// maxInboxResidueEntries bounds one residue sweep. It is the same bound the
// vault walk runs within, for the same reason: a directory somebody has filled
// is a directory this server reads a bounded slice of and then refuses, rather
// than one it stalls on.
const maxInboxResidueEntries = MaxVaultIndexedFiles

// RemoveInboxResidue removes the reserved entries in the inbox whose whole
// bytes are exactly ones the caller is entitled to delete, and reports which of
// those it could not.
//
// It exists because a removal that cannot unlink does not stop being
// answerable for the bytes. The entry is put back under its own name so the
// record naming it still finds it — but the reserved name it was detached to
// cannot always be dropped, and then the same inode has two names. The retry
// removes the entry under the visible one, which is the only name it knows, and
// the record is retired for a file that is still in the user's vault under a
// name no listing shows.
//
// What entitles this to remove one is the same thing that entitles every other
// removal here: the bytes. A reserved entry whose content hashes to something
// the caller was already allowed to delete is that file under a name nobody can
// spell; anything else is somebody else's — a half-written note another writer
// is staging, a file this package never made — and is left exactly where it is.
// Each removal still goes through the detach-and-verify path, so what is
// unlinked is the entry that was checked and never whatever took its name
// afterwards.
//
// Visible entries are never touched. They are the files the caller's own
// removals are about, decided under their own locks, with their own rules about
// what may be deleted and what must be put back.
func (v *Vault) RemoveInboxResidue(ctx context.Context, expectedHashes []string) (map[string]error, error) {
	wanted := make(map[string]struct{}, len(expectedHashes))
	for _, hash := range expectedHashes {
		if hash != "" {
			wanted[hash] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parent, err := v.openDirectory(ctx, []string{InboxDirName}, false)
	if err != nil {
		if errors.Is(err, unix.ENOENT) || errors.Is(err, os.ErrNotExist) {
			// No inbox, so nothing is under a reserved name in it.
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = parent.Close() }()

	failures := map[string]error{}
	examined := 0
	for {
		if err := ctx.Err(); err != nil {
			return failures, err
		}
		names, listErr := v.listDirectoryBatch(parent)
		if listErr != nil {
			// Half a listing proves nothing about the half it did not return,
			// and this sweep's whole job is to be able to say "there is no
			// second copy". So it reports rather than concluding.
			return failures, fmt.Errorf("list %s/ for reserved entries: %w", InboxDirName, listErr)
		}
		if len(names) == 0 {
			return failures, nil
		}
		for _, name := range names {
			if !strings.HasPrefix(name, stagingPrefix) {
				continue
			}
			examined++
			if examined > maxInboxResidueEntries {
				return failures, fmt.Errorf(
					"%s/ holds more than %d reserved entries, past the bound this sweep runs within",
					InboxDirName, maxInboxResidueEntries,
				)
			}
			hash, err := v.removeResidueEntry(ctx, parent, name, wanted)
			if hash == "" {
				// Not bytes this caller can name, or gone already. Either way
				// there is nothing to report against a hash.
				if err != nil && ctx.Err() != nil {
					return failures, err
				}
				continue
			}
			if err != nil {
				failures[hash] = err
			}
		}
	}
}

// removeResidueEntry considers one reserved entry and answers with the hash it
// matched, if any, and what stopped the removal.
//
// An entry that cannot be read, or that is bigger than a note can be, matches
// nothing: this sweep only ever removes bytes it has read and hashed itself.
func (v *Vault) removeResidueEntry(
	ctx context.Context,
	parent *os.File,
	name string,
	wanted map[string]struct{},
) (string, error) {
	clean := vaultRelPath(InboxDirName, name)
	unlock, err := v.locks.lockContext(ctx, v.pathLockKey(clean))
	if err != nil {
		return "", err
	}
	defer unlock()

	opened, stat, err := openConfinedEntry(parent, name, clean)
	if err != nil {
		return "", nil
	}
	defer func() { _ = opened.Close() }()
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return "", nil
	}
	content, err := readEntryContent(ctx, opened, clean, MaxNoteBytes)
	if err != nil {
		return "", nil
	}
	hash := ContentHash(content)
	if _, ok := wanted[hash]; !ok {
		return "", nil
	}
	outcome, placement, reason, err := v.removeVerifiedEntry(ctx, parent, name, clean, stat, content)
	switch outcome {
	case removalMissing, removalDone:
		if err != nil {
			return hash, err
		}
		return hash, nil
	default:
		if err != nil {
			return hash, err
		}
		return hash, errors.New(boundRefusalDetail(placement.explain(reason)))
	}
}
