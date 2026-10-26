package egress

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

// A challenge opens only under the secret and domain it was signed for, so a
// token one consent flow issues can never be accepted by another.
func TestAChallengeOpensOnlyForItsSecretAndDomain(t *testing.T) {
	payload := []byte(`{"version":1}`)
	token := SignChallenge("secret", "domain.a", payload)
	opened, err := OpenChallenge("secret", "domain.a", token, 1024)
	if err != nil || string(opened) != string(payload) {
		t.Fatalf("open = %q, %v", opened, err)
	}
	for name, open := range map[string]func() error{
		"other domain": func() error { _, err := OpenChallenge("secret", "domain.b", token, 1024); return err },
		"other secret": func() error { _, err := OpenChallenge("other", "domain.a", token, 1024); return err },
		"tampered": func() error {
			parts := strings.Split(token, ".")
			forged := base64.RawURLEncoding.EncodeToString([]byte(`{"version":2}`)) + "." + parts[1]
			_, err := OpenChallenge("secret", "domain.a", forged, 1024)
			return err
		},
		"oversized": func() error { _, err := OpenChallenge("secret", "domain.a", token, 4); return err },
		"shape":     func() error { _, err := OpenChallenge("secret", "domain.a", token+".x", 1024); return err },
		"empty":     func() error { _, err := OpenChallenge("secret", "domain.a", "", 1024); return err },
	} {
		if open() == nil {
			t.Fatalf("%s: a challenge opened that must not", name)
		}
	}
}

// The token format is the one remote-egress challenges have always used, so
// a send prepared before this code moved still verifies after it.
func TestAChallengeIsTheExistingTokenFormat(t *testing.T) {
	payload := []byte(`{"version":1}`)
	deriver := hmac.New(sha256.New, []byte("secret"))
	_, _ = deriver.Write([]byte("domain.a"))
	signature := hmac.New(sha256.New, deriver.Sum(nil))
	_, _ = signature.Write(payload)
	want := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature.Sum(nil))
	if got := SignChallenge("secret", "domain.a", payload); got != want {
		t.Fatalf("token = %q, want %q", got, want)
	}
}

// A nonce is 32 random bytes in canonical base64url, and never repeats.
func TestAChallengeNonceIsRandomAndCanonical(t *testing.T) {
	first, err := NewChallengeNonce()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewChallengeNonce()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil || len(raw) != 32 || first == second {
		t.Fatalf("nonces %q and %q, want two distinct 32-byte base64url values", first, second)
	}
}
