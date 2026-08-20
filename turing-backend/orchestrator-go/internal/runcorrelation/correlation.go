// Package runcorrelation owns the single rule that decides whether a run and
// an assistant message are one linked pair.
//
// The link is circular — the run stores its assistant message ID and the
// message stores its run ID — so a second, slightly different rule elsewhere
// would let one reader accept a pairing another rejects. Migration preflight,
// atomic enqueue, and the joined history reader all call Validate instead, and
// the package deliberately depends on neither the database nor protobuf so
// nothing can be tempted to reimplement it locally.
package runcorrelation

import "errors"

// AssistantRole is the only message role a run may own. It is exported because
// the readers that detect a duplicate claimant have to spell the same predicate
// as idx_messages_assistant_run_unique, and a role literal retyped in a SQL
// string is exactly the second opinion this package exists to prevent. Roles
// are stored lowercase by the writer, so comparisons against it are exact
// rather than folded.
const AssistantRole = "assistant"

// ErrConflict is the only error this package returns. Correlation problems are
// reported to operators and surfaced at the public read boundary, so the error
// names the class of problem and never the row values behind it.
var ErrConflict = errors.New("run/message correlation conflict")

// Link is one candidate run/message pairing, read from the run row and the
// message row.
type Link struct {
	RunID, RunSessionID, RunAssistantMessageID string
	MessageID, MessageSessionID, MessageRunID  string
	MessageRole                                string
}

// Validate accepts only a complete, mutually consistent assistant link: both
// directions name each other, both rows agree on the session, and the message
// is the assistant turn. Absent identity is rejected rather than treated as a
// weak match, because a half-written link cannot prove ownership either.
func Validate(link Link) error {
	if link.RunID == "" || link.RunSessionID == "" || link.RunAssistantMessageID == "" ||
		link.MessageID == "" || link.MessageSessionID == "" || link.MessageRunID == "" {
		return ErrConflict
	}
	if link.MessageRunID != link.RunID {
		return ErrConflict
	}
	if link.RunAssistantMessageID != link.MessageID {
		return ErrConflict
	}
	if link.MessageSessionID != link.RunSessionID {
		return ErrConflict
	}
	if link.MessageRole != AssistantRole {
		return ErrConflict
	}
	return nil
}
