package runoutcome

import (
	"crypto/sha256"
	"encoding/hex"
)

// HasDisplayableContent reports whether persisted assistant content contains at
// least one Unicode scalar outside the approved whitespace set.
//
// The set is written out here on purpose. Delegating to strings.TrimSpace or
// unicode.IsSpace would tie a durable, user-visible decision — blank bubble or
// explanatory card — to a runtime's own notion of whitespace, and the Dart
// client cannot share that notion. The boolean is the only output: callers
// persist the original bytes unchanged.
func HasDisplayableContent(content string) bool {
	for _, scalar := range content {
		if !approvedWhitespace(scalar) {
			return true
		}
	}
	return false
}

func approvedWhitespace(scalar rune) bool {
	switch scalar {
	case '\u0009', '\u000A', '\u000B', '\u000C', '\u000D',
		'\u0020',
		'\u0085',
		'\u00A0',
		'\u1680',
		'\u2000', '\u2001', '\u2002', '\u2003', '\u2004',
		'\u2005', '\u2006', '\u2007', '\u2008', '\u2009', '\u200A',
		'\u2028', '\u2029', '\u202F',
		'\u205F',
		'\u3000':
		return true
	default:
		return false
	}
}

// ContentSHA256 is the internal lowercase identity of exact persisted content
// bytes. A duplicate terminal report is a no-op only when this matches, so the
// digest hashes the content as given: no trimming, normalization, or
// re-encoding.
func ContentSHA256(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}
