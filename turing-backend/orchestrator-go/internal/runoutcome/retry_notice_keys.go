package runoutcome

// reservedRetryNoticeKeys are the payload keys only the repository's retry and
// recovery notice writer ever sets on an agent.run.step row. Their presence —
// not whether their values still parse — is what marks a row as a
// retry-notice attempt rather than a governed non-retry step, so a value this
// build cannot trust must still resolve to a bounded typed notice instead of
// being left as an unrewritten row that keeps republishing its raw note and
// reason forever.
//
// Both the migration boundary (rewriting a legacy row) and the public-read
// boundary (projecting a stored row for a client) answer this exact presence
// question, so it is asked through the one predicate below rather than
// through two lists that could silently drift apart. The list itself is
// never exported: a caller that needs the vocabulary asks a question of it
// (HasReservedRetryNoticeKey, DeleteReservedRetryNoticeKeys) instead of
// holding a slice it could reorder, truncate, or append to.
var reservedRetryNoticeKeys = []string{"category", "attempt", "attempts", "maxAttempts", "stateVersion"}

// HasReservedRetryNoticeKey reports whether payload carries any key only the
// repository's retry/recovery notice writer ever sets on an agent.run.step
// row. Presence, not parse success, is the signal a caller should gate on: a
// worker's generic step content never carries these keys (they are stripped
// at ingress by DeleteReservedRetryNoticeKeys), so seeing one at all — with a
// valid value or a corrupted one — means the row is claiming to be a
// repository-authored notice.
func HasReservedRetryNoticeKey(payload map[string]any) bool {
	for _, key := range reservedRetryNoticeKeys {
		if _, ok := payload[key]; ok {
			return true
		}
	}
	return false
}

// DeleteReservedRetryNoticeKeys removes every reserved retry/recovery notice
// key from fields. It exists for a caller that strips repository-authored
// projections from a worker-authored event at ingress, so that boundary and
// HasReservedRetryNoticeKey above ask about the identical vocabulary rather
// than risking a second, hand-maintained copy of the key names.
//
// It is generic over the field value type so a caller can pass either a plain
// decoded payload (map[string]any) or a protobuf struct's field map
// (map[string]*structpb.Value) without this package taking on a protobuf
// dependency it otherwise has no use for.
func DeleteReservedRetryNoticeKeys[V any](fields map[string]V) {
	for _, key := range reservedRetryNoticeKeys {
		delete(fields, key)
	}
}
