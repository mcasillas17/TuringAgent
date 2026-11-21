// Package attention is the Needs-you queue's projection: the closed stage
// table every owner obligation is classified by. It is pure, so the table is
// tested on its own, and both the repository's reads and the public service
// use it without either reasoning about stages itself.
package attention

// StageID names one row of the closed stage table.
type StageID string

// The table's stages, in its order, and the one answer for a row none of
// them describes.
const (
	PersonApproval StageID = "person/approval"
	PersonConsent  StageID = "person/consent"
	PersonBlocked  StageID = "person/blocked"
	PersonDeletion StageID = "person/deletion"
	PersonProposal StageID = "person/proposal"
	// Unavailable is a row the projection cannot classify. It is listed, never
	// dropped: an empty list must be a fact, not a guess.
	Unavailable StageID = "unavailable"
)

// Conditions are the qualifying conditions one subject meets. A subject that
// meets several is classified once, by the first stage in the table.
type Conditions struct {
	// A pending, unexpired approval on any run.
	PendingApproval bool
	// A delegated task waiting for the owner's consent to tools that leave
	// the machine.
	PendingConsent bool
	// An enabled automation whose latest occurrence failed or was blocked,
	// and that occurrence is not the one the owner dismissed.
	BlockedAutomation bool
	// An enabled watch suspended by a source failure. No source sets it until
	// PRO-001.
	SuspendedWatch bool
	// An owner's deletion receipt held failed and retryable.
	RetryableDeletion bool
	// An open watch proposal. No source sets it until PRO-001.
	OpenProposal bool
}

// table is read top to bottom; the first stage whose condition holds wins.
var table = []struct {
	stage StageID
	holds func(Conditions) bool
}{
	{PersonApproval, func(c Conditions) bool { return c.PendingApproval }},
	{PersonConsent, func(c Conditions) bool { return c.PendingConsent }},
	{PersonBlocked, func(c Conditions) bool { return c.BlockedAutomation || c.SuspendedWatch }},
	{PersonDeletion, func(c Conditions) bool { return c.RetryableDeletion }},
	{PersonProposal, func(c Conditions) bool { return c.OpenProposal }},
}

// Stage classifies one subject by the closed table, first match wins.
func Stage(conditions Conditions) StageID {
	for _, row := range table {
		if row.holds(conditions) {
			return row.stage
		}
	}
	return Unavailable
}

// Rank is a stage's place in the table, from 1, and the first key of the
// queue's order. Unavailable has no rank of its own: an unclassified row
// keeps its source's place.
func Rank(stage StageID) int {
	for index, row := range table {
		if row.stage == stage {
			return index + 1
		}
	}
	return 0
}

// Kind is the source a row comes from. Its value is the third key of the
// queue's order, so the order of these constants is part of the contract.
type Kind int

const (
	KindApproval Kind = iota + 1
	KindDelegation
	KindAutomation
	KindDeletion
	KindWatch
	KindProposal
)

// SourceStage is the stage a source's rows sit in, which fixes where they
// sort even when one of them cannot be classified.
func SourceStage(kind Kind) StageID {
	switch kind {
	case KindApproval:
		return PersonApproval
	case KindDelegation:
		return PersonConsent
	case KindAutomation, KindWatch:
		return PersonBlocked
	case KindDeletion:
		return PersonDeletion
	case KindProposal:
		return PersonProposal
	default:
		return Unavailable
	}
}
