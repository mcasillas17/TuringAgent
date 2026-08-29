package egress

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxFramedContentBytes is the default ceiling on one framed retrieval,
// delimiters and instructions included. Retrieved text is the largest thing a
// tool hands a model, and an unbounded one is how a single note displaces the
// conversation it was supposed to inform.
const MaxFramedContentBytes = 16 * 1024

// framingNonceBytes is the entropy in one delimiter. Content that cannot guess
// the delimiter cannot close the frame early and continue as if it were the
// orchestrator speaking, so the nonce is the whole spoof defence and is drawn
// from crypto/rand on every single call.
const framingNonceBytes = 16

// ErrFraming refuses a frame this package cannot honour exactly as asked. It is
// deliberately a refusal rather than a fallback: a caller that silently got an
// unlabelled frame, or a budget it did not ask for, would be handing a model
// content it cannot describe.
var ErrFraming = errors.New("retrieved content framing is not valid")

// Framing is one caller's description of what it is about to hand a model.
//
// Both fields are required. The label names the caller inside the delimiter
// itself, so two frames from different tools in one context can never be
// confused for each other; the instructions are the sentence the model reads
// before the content, which is the only channel that says the bytes below are
// data rather than something addressed to it.
type Framing struct {
	// Label names the retrieving caller — "GITHUB", "MEMORY_SEARCH". Uppercase
	// ASCII, digits and underscores only, so it can never contribute a byte
	// that looks like a delimiter boundary.
	Label string
	// Instructions is stated in the frame, above the content.
	Instructions string
	// MaxBytes bounds the entire frame. Zero means MaxFramedContentBytes.
	MaxBytes int
}

// FrameRetrievedContent wraps retrieved bytes in a per-call, unguessable frame.
//
// The content is never rewritten: text that forges a delimiter is passed
// through verbatim, because the frame's integrity comes from the nonce the
// content could not have known, not from scrubbing the content. Truncation cuts
// on a UTF-8 boundary and is announced outside the frame, in Turing's own
// voice, so a model can tell a fragment from a whole document.
func FrameRetrievedContent(framing Framing, raw []byte) (string, error) {
	if err := framing.validate(); err != nil {
		return "", err
	}
	limit := framing.MaxBytes
	if limit == 0 {
		limit = MaxFramedContentBytes
	}

	nonce := make([]byte, framingNonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("%w: no randomness for a framing delimiter", ErrFraming)
	}
	marker := "TURING_RETRIEVED_" + framing.Label + "_" + hex.EncodeToString(nonce)
	prefix := "BEGIN " + marker + "\n" + framing.Instructions + "\n"
	suffix := "\nEND " + marker
	// The notice reports the bytes actually kept, which are only known after
	// the rune-safe cut — so the budget reserves worst-case room for it using
	// the limit, whose digit count the kept figure can never exceed.
	noticeFor := func(kept int) string {
		return fmt.Sprintf("\n[Result truncated to %d bytes on a UTF-8 boundary.]", kept)
	}

	// Replacement is byte-for-byte length-preserving in the worst case only for
	// valid input, so the budget is measured after the repair, never before.
	valid := []byte(strings.ToValidUTF8(string(raw), "\uFFFD"))
	available := limit - len(prefix) - len(suffix)
	if available <= 0 {
		return "", fmt.Errorf("%w: %d bytes cannot hold the delimiters", ErrFraming, limit)
	}
	truncated := len(valid) > available
	if truncated {
		available -= len(noticeFor(limit))
		if available <= 0 {
			return "", fmt.Errorf("%w: %d bytes cannot hold a truncation notice", ErrFraming, limit)
		}
		// Cut at a rune boundary at or below the budget, the way
		// memoryfiles.truncateRunes does: step back to the nearest rune start
		// instead of re-validating the whole slice once per surplus byte.
		cut := available
		for cut > 0 && !utf8.RuneStart(valid[cut]) {
			cut--
		}
		if cut == 0 {
			// The budget admits some bytes but not one whole rune of this
			// content. Shipping an empty frame that claims "truncated to 0
			// bytes" would be a well-formed lie; refuse like the guards above.
			return "", fmt.Errorf("%w: %d bytes cannot hold any of the content", ErrFraming, limit)
		}
		valid = valid[:cut]
	}
	framed := prefix + string(valid) + suffix
	if truncated {
		framed += noticeFor(len(valid))
	}
	return framed, nil
}

func (f Framing) validate() error {
	if f.Label == "" {
		return fmt.Errorf("%w: a frame must name the caller that retrieved it", ErrFraming)
	}
	for _, symbol := range f.Label {
		if (symbol < 'A' || symbol > 'Z') && (symbol < '0' || symbol > '9') && symbol != '_' {
			return fmt.Errorf("%w: label must be uppercase ASCII, digits or underscores", ErrFraming)
		}
	}
	if strings.TrimSpace(f.Instructions) == "" {
		return fmt.Errorf("%w: a frame must state how its content is to be read", ErrFraming)
	}
	if strings.Contains(f.Instructions, "\n") {
		return fmt.Errorf("%w: instructions are one line", ErrFraming)
	}
	if f.MaxBytes < 0 {
		return fmt.Errorf("%w: byte budget is negative", ErrFraming)
	}
	return nil
}
