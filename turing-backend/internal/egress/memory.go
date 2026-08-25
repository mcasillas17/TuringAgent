package egress

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// MemoryServerName is the pseudo-server every memory tool is registered under.
// It lives here rather than in the memory service because three components
// that never import each other — the consent resolver, the enqueue and the
// runtime's independent re-derivation — all have to agree on which selected
// tool names mean "memory is in play".
const MemoryServerName = "memory"

// MemorySnapshot is the exact pinned material one run is allowed to send, in
// the shape all three parties hash.
//
// It is the post-truncation bytes, notice included, not the file on disk: the
// fingerprint has to be a statement about what a model was actually shown, so
// hashing the vault's own bytes would bind the run to something nobody saw.
//
// Nothing here is omitempty, deliberately. A withheld persona and an empty one
// are different facts — the first says the tier was off or unreadable, the
// second says the user wrote nothing — and a preimage that dropped either
// field would let those two hash identically.
//
// The memory toggle is not a field. It is bound through the snapshot it
// produces: turning memory off withholds both documents and empties both
// bodies, which is a different preimage. Carrying the toggle as its own claim
// would put a fact on the wire that the runtime cannot check against the job it
// was handed, and an unverifiable claim in a binding is worse than no claim.
type MemorySnapshot struct {
	PersonaID           string `json:"persona_id"`
	PersonaDisplayName  string `json:"persona_display_name"`
	PersonaBody         string `json:"persona_body"`
	PersonaContentHash  string `json:"persona_content_hash"`
	PersonaWithheld     bool   `json:"persona_withheld"`
	ProfileID           string `json:"profile_id"`
	ProfileBody         string `json:"profile_body"`
	ProfileContentHash  string `json:"profile_content_hash"`
	ProfileWithheld     bool   `json:"profile_withheld"`
	MemoryToolsSelected bool   `json:"memory_tools_selected"`
}

// Canonical clears a withheld tier's body and hash.
//
// A tier is either read or it is not; "withheld but here is what was in it
// anyway" is not a fact this snapshot is allowed to state. No producer today
// builds that contradiction on purpose, but Canonical is the one place that
// forecloses it, rather than leaving every reader — the fingerprint, the
// applicability check, a future one — to remember to check Withheld before it
// trusts Body. Called once here, both the consent binding and the "would
// anything reach a prompt" answer are computed against the same bytes, and a
// withheld tier can never smuggle content through either by disagreeing with
// its own flag.
func (s MemorySnapshot) Canonical() MemorySnapshot {
	if s.PersonaWithheld {
		s.PersonaBody = ""
		s.PersonaContentHash = ""
	}
	if s.ProfileWithheld {
		s.ProfileBody = ""
		s.ProfileContentHash = ""
	}
	return s
}

// MemorySnapshotFingerprint is the one-way binding between a consent and the
// pinned material it was granted over. It hashes the canonical form so a
// withheld tier's leftover body — which a producer must never create, but
// which this function does not have to trust it avoided — cannot shift the
// binding away from what a withheld tier actually contributes: nothing.
func MemorySnapshotFingerprint(snapshot MemorySnapshot) (string, error) {
	encoded, err := json.Marshal(snapshot.Canonical())
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// HasPinnedContent answers whether anything from the vault would actually reach
// a prompt. The trim is here, once, because the consent decision and the
// runtime's re-derivation must not hold two different ideas of "empty": a
// persona of nothing but spaces contributes no instruction and must not put a
// memory category on a disclosure.
//
// A withheld tier contributes nothing regardless of what its body holds, for
// the same reason the runtime's own prompt assembly ignores a withheld body
// before it looks at it: withheld and empty are different facts, but both are
// "nothing reaches the prompt", and this is the one place the disclosure asks
// that question.
func (s MemorySnapshot) HasPinnedContent() bool {
	canonical := s.Canonical()
	return strings.TrimSpace(canonical.PersonaBody) != "" || strings.TrimSpace(canonical.ProfileBody) != ""
}

// IsMemoryToolName reports whether a frozen selected-tool name belongs to the
// memory pseudo-server. Prefix matching is exact on the separator so a
// third-party server called "memoryx" cannot borrow the category.
func IsMemoryToolName(name string) bool {
	prefix := MemoryServerName + "/"
	return strings.HasPrefix(name, prefix) && len(name) > len(prefix)
}

// SelectedToolsIncludeMemory is the tool half of the memory applicability rule.
func SelectedToolsIncludeMemory(selectedTools []string) bool {
	for _, name := range selectedTools {
		if IsMemoryToolName(name) {
			return true
		}
	}
	return false
}
