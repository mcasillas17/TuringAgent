package runoutcome

import "testing"

// TestHasReservedRetryNoticeKeyRecognizesEachKeyAlone pins the full
// vocabulary one key at a time. db and events both gate their retry-notice
// rewrite on this predicate, and a payload that carries only one reserved
// key — no others — still has to be recognized: a table that silently
// dropped one of the five (say, by only checking "attempt"/"attempts"/
// "maxAttempts" and forgetting "category" or "stateVersion") would let a row
// that named only a reserved key of its own keep its raw note and reason
// forever instead of being resolved to a bounded notice.
func TestHasReservedRetryNoticeKeyRecognizesEachKeyAlone(t *testing.T) {
	for _, key := range []string{"category", "attempt", "attempts", "maxAttempts", "stateVersion"} {
		t.Run(key, func(t *testing.T) {
			payload := map[string]any{key: "anything", "note": "raw", "reason": "raw"}
			if !HasReservedRetryNoticeKey(payload) {
				t.Fatalf("HasReservedRetryNoticeKey(%#v) = false, want true: %q alone must be recognized", payload, key)
			}
		})
	}
}

// TestHasReservedRetryNoticeKeyRejectsGenericStepContent proves the predicate
// does not widen past the five reserved keys: an unrelated worker step's own
// keys must not be mistaken for a repository-authored retry/recovery notice.
func TestHasReservedRetryNoticeKeyRejectsGenericStepContent(t *testing.T) {
	payload := map[string]any{
		"note": "Sending to Claude — this message leaves your machine", "externalAgent": "Claude", "endpoint": "api.anthropic.com",
	}
	if HasReservedRetryNoticeKey(payload) {
		t.Fatalf("HasReservedRetryNoticeKey(%#v) = true, want false: no reserved key is present", payload)
	}
	if HasReservedRetryNoticeKey(map[string]any{}) {
		t.Fatal("HasReservedRetryNoticeKey(empty) = true, want false")
	}
}

// TestDeleteReservedRetryNoticeKeysRemovesExactlyTheVocabulary pins
// DeleteReservedRetryNoticeKeys against the same five-key vocabulary
// HasReservedRetryNoticeKey answers presence for, so the ingress strip and
// the rewrite predicate cannot silently drift onto two different lists.
func TestDeleteReservedRetryNoticeKeysRemovesExactlyTheVocabulary(t *testing.T) {
	fields := map[string]any{
		"category": "dispatch_retry", "attempt": 2, "attempts": 3, "maxAttempts": 3, "stateVersion": 5,
		"note": "keep me", "reason": "keep me too",
	}
	DeleteReservedRetryNoticeKeys(fields)
	want := map[string]any{"note": "keep me", "reason": "keep me too"}
	if len(fields) != len(want) {
		t.Fatalf("fields after delete = %#v, want %#v", fields, want)
	}
	for key, value := range want {
		if fields[key] != value {
			t.Fatalf("fields[%q] = %#v, want %#v (deletion touched a key it should not have)", key, fields[key], value)
		}
	}
	for _, reserved := range []string{"category", "attempt", "attempts", "maxAttempts", "stateVersion"} {
		if _, present := fields[reserved]; present {
			t.Fatalf("fields still carries reserved key %q after DeleteReservedRetryNoticeKeys", reserved)
		}
	}
}
