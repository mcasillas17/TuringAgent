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

// MemorySnapshotFingerprint is the one-way binding between a consent and the
// pinned material it was granted over.
func MemorySnapshotFingerprint(snapshot MemorySnapshot) (string, error) {
	encoded, err := json.Marshal(snapshot)
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
func (s MemorySnapshot) HasPinnedContent() bool {
	return strings.TrimSpace(s.PersonaBody) != "" || strings.TrimSpace(s.ProfileBody) != ""
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
