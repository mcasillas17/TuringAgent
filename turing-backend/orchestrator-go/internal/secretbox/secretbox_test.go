package secretbox

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func newTestSealer(t *testing.T) *Sealer {
	t.Helper()
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	sealer, err := New(key)
	if err != nil {
		t.Fatalf("new sealer: %v", err)
	}
	return sealer
}

func TestSealRoundTrips(t *testing.T) {
	sealer := newTestSealer(t)

	sealed, err := sealer.Seal([]byte("app-password-hunter2"), []byte("conn_1"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Contains(sealed, []byte("hunter2")) {
		t.Fatalf("sealed value contains the plaintext: %q", sealed)
	}
	opened, err := sealer.Open(sealed, []byte("conn_1"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(opened) != "app-password-hunter2" {
		t.Fatalf("opened = %q, want the original", opened)
	}
}

// Two connections that happen to use the same credential must not be visibly
// identical rows to somebody reading the database file.
func TestSealUsesAFreshNoncePerCall(t *testing.T) {
	sealer := newTestSealer(t)

	first, err := sealer.Seal([]byte("same-secret"), []byte("conn_1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := sealer.Seal([]byte("same-secret"), []byte("conn_1"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("sealing the same plaintext twice produced identical bytes")
	}
}

// The point of keeping the key out of the database: the ciphertext alone is
// not the credential.
func TestOpenFailsWithADifferentKey(t *testing.T) {
	sealed, err := newTestSealer(t).Seal([]byte("app-password"), []byte("conn_1"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = newTestSealer(t).Open(sealed, []byte("conn_1"))

	if !errors.Is(err, ErrCiphertextInvalid) {
		t.Fatalf("open with a different key = %v, want ErrCiphertextInvalid", err)
	}
}

func TestOpenRejectsTamperedAndTruncatedCiphertext(t *testing.T) {
	sealer := newTestSealer(t)
	sealed, err := sealer.Seal([]byte("app-password"), []byte("conn_1"))
	if err != nil {
		t.Fatal(err)
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := sealer.Open(tampered, []byte("conn_1")); !errors.Is(err, ErrCiphertextInvalid) {
		t.Fatalf("open tampered = %v, want ErrCiphertextInvalid", err)
	}
	if _, err := sealer.Open(sealed[:4], []byte("conn_1")); !errors.Is(err, ErrCiphertextInvalid) {
		t.Fatalf("open truncated = %v, want ErrCiphertextInvalid", err)
	}
}

// A failure to decrypt must not describe itself differently depending on why.
func TestOpenErrorTextDoesNotDependOnTheFailure(t *testing.T) {
	sealer := newTestSealer(t)
	sealed, err := sealer.Seal([]byte("app-password"), []byte("conn_1"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0xff

	_, tamperedErr := sealer.Open(tampered, []byte("conn_1"))
	_, wrongKeyErr := newTestSealer(t).Open(sealed, []byte("conn_1"))

	if tamperedErr.Error() != wrongKeyErr.Error() {
		t.Fatalf("distinguishable errors: %q vs %q", tamperedErr, wrongKeyErr)
	}
}

// Without this, a missing key would be indistinguishable from a configured
// one and the caller would have no way to refuse rather than store plaintext.
func TestNilSealerRefusesInsteadOfPassingThePlaintextThrough(t *testing.T) {
	var sealer *Sealer

	sealed, err := sealer.Seal([]byte("app-password"), []byte("conn_1"))
	if !errors.Is(err, ErrNoKey) {
		t.Fatalf("seal with no key = %v, want ErrNoKey", err)
	}
	if sealed != nil {
		t.Fatalf("seal with no key returned %q, want nothing", sealed)
	}
	if _, err := sealer.Open([]byte("anything"), []byte("conn_1")); !errors.Is(err, ErrNoKey) {
		t.Fatalf("open with no key = %v, want ErrNoKey", err)
	}
}

func TestParseKeyDistinguishesUnsetFromMalformed(t *testing.T) {
	if _, err := ParseKey(""); !errors.Is(err, ErrNoKey) {
		t.Fatalf("empty key = %v, want ErrNoKey", err)
	}
	for _, bad := range []string{"not-hex", hex.EncodeToString(make([]byte, 16)), hex.EncodeToString(make([]byte, 64))} {
		if _, err := ParseKey(bad); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("ParseKey(%q) = %v, want ErrInvalidKey", bad, err)
		}
	}
	key, err := ParseKey(hex.EncodeToString(make([]byte, KeySize)))
	if err != nil || len(key) != KeySize {
		t.Fatalf("ParseKey of a valid key = %v, %d bytes", err, len(key))
	}
}

func TestFromHexKeySealsWithTheDecodedKey(t *testing.T) {
	raw := make([]byte, KeySize)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	sealer, err := FromHexKey(hex.EncodeToString(raw))
	if err != nil {
		t.Fatalf("from hex key: %v", err)
	}
	sealed, err := sealer.Seal([]byte("app-password"), []byte("conn_1"))
	if err != nil {
		t.Fatal(err)
	}

	other, err := New(raw)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := other.Open(sealed, []byte("conn_1"))
	if err != nil || string(opened) != "app-password" {
		t.Fatalf("opened = %q / %v, want the original", opened, err)
	}

	if _, err := FromHexKey(strings.Repeat("z", 64)); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("FromHexKey with non-hex = %v, want ErrInvalidKey", err)
	}
}

// The reason Seal takes a binding at all: a sealed credential must not be a
// portable blob. Anyone able to write the database could otherwise move the
// GitHub token's ciphertext into a mail connection pointing at a server they
// control, and it would decrypt cleanly when a tool eventually opened it.
func TestASealedValueCannotBeMovedToAnotherConnection(t *testing.T) {
	sealer := newTestSealer(t)
	sealed, err := sealer.Seal([]byte("github-token"), []byte("conn_github"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = sealer.Open(sealed, []byte("conn_mail"))

	if !errors.Is(err, ErrCiphertextInvalid) {
		t.Fatalf("opening under another connection = %v, want ErrCiphertextInvalid", err)
	}
	// And still opens under its own.
	if _, err := sealer.Open(sealed, []byte("conn_github")); err != nil {
		t.Fatalf("open under its own binding: %v", err)
	}
}

// The fingerprint is how a reader tells "sealed with the key I have" from
// "sealed with a key that is gone", without decrypting anything.
func TestSealedWithThisKeyIdentifiesTheKeyWithoutOpening(t *testing.T) {
	sealer := newTestSealer(t)
	sealed, err := sealer.Seal([]byte("app-password"), []byte("conn_1"))
	if err != nil {
		t.Fatal(err)
	}

	if !sealer.SealedWithThisKey(sealed) {
		t.Fatal("sealer does not recognise its own output")
	}
	// The header alone is enough, which is what lets a list ask about every
	// row without reading a single credential out of the database.
	if !sealer.SealedWithThisKey(sealed[:HeaderSize]) {
		t.Fatal("the header alone should be enough to identify the key")
	}
	if newTestSealer(t).SealedWithThisKey(sealed) {
		t.Fatal("a different key claimed a value it cannot open")
	}
	var absent *Sealer
	if absent.SealedWithThisKey(sealed) {
		t.Fatal("a sealer with no key claimed a value")
	}
	for _, malformed := range [][]byte{nil, {}, {0x09, 0x01, 0x02, 0x03, 0x04}} {
		if sealer.SealedWithThisKey(malformed) {
			t.Fatalf("claimed a malformed value %v", malformed)
		}
	}
}

// A stored value written by a future format must not be opened as though it
// were this one.
func TestOpenRejectsAnUnknownSealFormat(t *testing.T) {
	sealer := newTestSealer(t)
	sealed, err := sealer.Seal([]byte("app-password"), []byte("conn_1"))
	if err != nil {
		t.Fatal(err)
	}
	sealed[0] = 0x09

	if _, err := sealer.Open(sealed, []byte("conn_1")); !errors.Is(err, ErrCiphertextInvalid) {
		t.Fatalf("open of an unknown version = %v, want ErrCiphertextInvalid", err)
	}
}
