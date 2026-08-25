package memoryfiles

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const scannedNote = "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\ntitle: \"Prefers dark mode\"\n---\nbody\n"

// scanReadStub scripts what one note's read does. It is the seam these tests
// need: the two failures they are about — a pass cancelled while a note is
// being read, and a read that fails once and then works — are races against the
// filesystem and the clock that no arrangement of real files can schedule
// reliably. Everything it holds is mutex-guarded because a scan may read from
// several goroutines.
type scanReadStub struct {
	mutex        sync.Mutex
	reads        map[string]int
	failuresLeft map[string]int
	failure      func(ctx context.Context, relPath string) error
}

func newScanReadStub() *scanReadStub {
	return &scanReadStub{reads: map[string]int{}, failuresLeft: map[string]int{}}
}

// scriptFailures arms the next `times` reads of relPath to fail. Later reads of
// that path go to the real confined read, which is what makes "and then it
// works" a property of the scan rather than of the stub.
func (s *scanReadStub) scriptFailures(relPath string, times int, failure func(ctx context.Context, relPath string) error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.failuresLeft[relPath] = times
	s.failure = failure
}

func (s *scanReadStub) readCount(relPath string) int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.reads[relPath]
}

func (s *scanReadStub) hook() scanReadHook {
	return func(ctx context.Context, relPath string, read func() (string, unix.Stat_t, error)) (string, unix.Stat_t, error) {
		s.mutex.Lock()
		s.reads[relPath]++
		remaining := s.failuresLeft[relPath]
		failure := s.failure
		if remaining > 0 {
			s.failuresLeft[relPath] = remaining - 1
		}
		s.mutex.Unlock()
		if remaining > 0 && failure != nil {
			return "", unix.Stat_t{}, failure(ctx, relPath)
		}
		return read()
	}
}

func openScanStubVault(t *testing.T, stub *scanReadStub) *Vault {
	t.Helper()
	vault, err := openVaultWith(newTestVaultRoot(t), realSyncHooks(), stub.hook())
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	return vault
}

// A pass that ends while a note is being read has not decided anything about
// that note. Turning the cancellation into a per-note parse error would invent
// a verdict the read never reached, hand the caller a ScanResult that looks
// complete, and — because the row is cached under the file's unchanged
// (mtime, size) — keep serving that invention until the user edits a file they
// have no reason to think is broken.
func TestScanWithCacheReturnsTheContextErrorWhenAPassEndsMidRead(t *testing.T) {
	testCases := []struct {
		name string
		want error
		// arrange returns the context the pass runs under, together with the
		// failure the read reports once that context is done.
		arrange func(t *testing.T) (context.Context, func(ctx context.Context, relPath string) error)
	}{
		{
			name: "the caller cancels the scan",
			want: context.Canceled,
			arrange: func(t *testing.T) (context.Context, func(context.Context, string) error) {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				return ctx, func(ctx context.Context, relPath string) error {
					cancel()
					return fmt.Errorf("read %q: %w", relPath, ctx.Err())
				}
			},
		},
		{
			name: "the pass runs out of time",
			want: context.DeadlineExceeded,
			arrange: func(t *testing.T) (context.Context, func(context.Context, string) error) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
				t.Cleanup(cancel)
				return ctx, func(ctx context.Context, relPath string) error {
					<-ctx.Done()
					return fmt.Errorf("read %q: %w", relPath, ctx.Err())
				}
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stub := newScanReadStub()
			vault := openScanStubVault(t, stub)
			const relPath = "beliefs/note.md"
			writeVaultFile(t, vault, relPath, scannedNote)
			cache := NewMetadataCache()

			ctx, interrupt := testCase.arrange(t)
			stub.scriptFailures(relPath, 1, interrupt)

			result, err := vault.ScanWithCache(ctx, cache)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("an interrupted pass returned %v with %d notes", err, len(result.Notes))
			}
			if len(result.Notes) != 0 {
				t.Fatalf("an interrupted pass reported notes it never finished reading: %+v", result.Notes)
			}
			if cache.Len() != 0 {
				t.Fatal("an interrupted read was written to the cache; the next pass would serve the invention")
			}

			// Nothing about the file changed, so a pass that is allowed to
			// finish must read the user's note rather than a cached verdict
			// about the interruption.
			fresh, err := vault.ScanWithCache(context.Background(), cache)
			if err != nil {
				t.Fatalf("fresh scan: %v", err)
			}
			note := noteByPath(t, fresh, relPath)
			if !note.Indexable || note.ParseError != "" {
				t.Fatalf("the interrupted pass left the note poisoned: %+v", note)
			}
			if note.Content != scannedNote || note.Title != "Prefers dark mode" {
				t.Fatalf("the fresh pass did not read the file: %+v", note)
			}
		})
	}
}

// A read that fails for a reason outside the file — a descriptor exhausted, an
// I/O error, an entry swapped underneath the walk — says nothing about what the
// note contains. It is reported so the user is never left guessing why a note
// is missing, and it is deliberately not cached: the same file with the same
// (mtime, size) has to be tried again on the next pass, or one transient error
// silently retires a memory until its bytes happen to change.
func TestScanWithCacheRetriesAFailedReadOnAnUnchangedFile(t *testing.T) {
	stub := newScanReadStub()
	vault := openScanStubVault(t, stub)
	const relPath = "beliefs/note.md"
	writeVaultFile(t, vault, relPath, scannedNote)
	cache := NewMetadataCache()

	stub.scriptFailures(relPath, 1, func(ctx context.Context, relPath string) error {
		return fmt.Errorf("read %q: %w", relPath, unix.EIO)
	})

	first, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("a single unreadable note failed the whole pass: %v", err)
	}
	unreadable := noteByPath(t, first, relPath)
	if unreadable.Indexable || unreadable.Status != NoteStatusError {
		t.Fatalf("a note that could not be read was indexed: %+v", unreadable)
	}
	if unreadable.ParseError == "" {
		t.Fatal("a note that could not be read was reported without a reason")
	}
	if _, reusable := cache.reusableRow(relPath, unreadable.ModTimeUnix, unreadable.SizeBytes); reusable {
		t.Fatal("a failed read was cached; the next pass would reuse a verdict the read never reached")
	}

	second, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if got := stub.readCount(relPath); got != 2 {
		t.Fatalf("the failed read was not retried: the note was read %d time(s)", got)
	}
	note := noteByPath(t, second, relPath)
	if !note.Indexable || note.ParseError != "" {
		t.Fatalf("a transient read failure outlived the read that failed: %+v", note)
	}
	if note.Content != scannedNote {
		t.Fatalf("the retry did not read the file: %q", note.Content)
	}
	if _, reusable := cache.reusableRow(relPath, note.ModTimeUnix, note.SizeBytes); !reusable {
		t.Fatal("the successful retry was not cached")
	}
}

// The other half of the same line: a note whose bytes were read and turned out
// to be malformed is a verdict about content, so it is cached and the file is
// not read again until its (mtime, size) says the user changed it.
func TestScanWithCacheKeepsMalformedFrontmatterCachedUntilTheFileChanges(t *testing.T) {
	stub := newScanReadStub()
	vault := openScanStubVault(t, stub)
	const relPath = "beliefs/broken.md"
	writeVaultFile(t, vault, relPath, "---\nid: \"unterminated\n---\nbody\n")
	cache := NewMetadataCache()

	first, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	broken := noteByPath(t, first, relPath)
	if broken.Indexable || broken.ParseError == "" {
		t.Fatalf("malformed frontmatter was indexed: %+v", broken)
	}
	if got := stub.readCount(relPath); got != 1 {
		t.Fatalf("the first pass read the note %d time(s)", got)
	}

	cached, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if got := stub.readCount(relPath); got != 1 {
		t.Fatalf("an unchanged malformed note was read again: %d reads", got)
	}
	if again := noteByPath(t, cached, relPath); again.Indexable || again.ParseError != broken.ParseError {
		t.Fatalf("the cached verdict changed: %+v", again)
	}

	writeVaultFile(t, vault, relPath, scannedNote)
	fixed, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("third scan: %v", err)
	}
	if got := stub.readCount(relPath); got != 2 {
		t.Fatalf("the user fixed the note and it was not read again: %d reads", got)
	}
	repaired := noteByPath(t, fixed, relPath)
	if !repaired.Indexable || repaired.ParseError != "" {
		t.Fatalf("the fixed note is still refused: %+v", repaired)
	}
}

// An over-limit note is the one refusal that comes out of the read step and is
// still a statement about content: the bytes were there to be counted. It is
// cached like any other content verdict, so a vault holding a half-megabyte
// note is not re-read from disk on every timer tick.
func TestScanWithCacheKeepsAnOverLimitNoteCachedUntilTheFileChanges(t *testing.T) {
	stub := newScanReadStub()
	vault := openScanStubVault(t, stub)
	const relPath = "beliefs/huge.md"
	oversized := "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\n" + string(make([]byte, MaxNoteBytes))
	writeVaultFile(t, vault, relPath, oversized)
	cache := NewMetadataCache()

	first, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	huge := noteByPath(t, first, relPath)
	if huge.Indexable || huge.ParseError == "" {
		t.Fatalf("an over-limit note was indexed: %+v", huge)
	}

	if _, err := vault.ScanWithCache(context.Background(), cache); err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if got := stub.readCount(relPath); got != 1 {
		t.Fatalf("an unchanged over-limit note was read from disk again: %d reads", got)
	}
}
