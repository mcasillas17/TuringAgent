// Package secretbox seals the one class of secret TuringAgent stores in its
// database: third-party credentials the user pastes in when connecting an
// account.
//
// The key is TURING_INTEGRATION_KEY from turing-backend/.env — the same place
// the API key, the internal token and the approval signing secret already
// live, held at chmod 600 and gitignored. It is deliberately NOT in
// data/turing.db, because a key stored beside the ciphertext it protects is
// theatre. What this buys is that a copy of the database on its own — a
// backup directory, a synced folder, a file attached to a bug report — is not
// a copy of the user's credentials. It buys nothing against someone who can
// read the backend directory, the process memory, or the environment.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

// KeySize is the AES-256 key length, in bytes. TURING_INTEGRATION_KEY is that
// many bytes hex-encoded, which is exactly what init.sh's `openssl rand -hex
// 32` produces for every other secret in .env.
const KeySize = 32

// The sealed form is version || fingerprint || nonce || ciphertext.
//
// The version byte exists so the format can change without every stored row
// becoming ambiguous. The fingerprint is four bytes of SHA-256 over the key,
// and it is what lets a reader answer "was this sealed with the key I have?"
// without decrypting anything — which is how a connection whose key was
// rotated or lost can say so instead of sitting there claiming to be
// connected. Four bytes identify a key among the handful a person will ever
// have; they are not a secret, and they are not enough to attack one.
const (
	sealVersion     byte = 1
	FingerprintSize      = 4
	HeaderSize           = 1 + FingerprintSize
)

var (
	// ErrNoKey reports that sealing was attempted without a configured key.
	// Callers turn this into "integrations are not configured" rather than
	// falling back to storing the secret in the clear.
	ErrNoKey = errors.New("no integration key configured")

	ErrInvalidKey        = errors.New("integration key must be 64 hex characters (32 bytes)")
	ErrCiphertextInvalid = errors.New("sealed value could not be opened")
)

// Sealer seals and opens credentials. The zero value is unusable on purpose:
// there is no implicit "no encryption" mode.
type Sealer struct {
	aead        cipher.AEAD
	fingerprint [FingerprintSize]byte
}

// ParseKey decodes the hex-encoded key from configuration. An empty key is
// reported as ErrNoKey so the caller can distinguish "not set up" from
// "set up wrong".
func ParseKey(hexKey string) ([]byte, error) {
	if hexKey == "" {
		return nil, ErrNoKey
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	return key, nil
}

// New builds a sealer from raw key bytes.
func New(key []byte) (*Sealer, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	sealer := &Sealer{aead: aead}
	digest := sha256.Sum256(key)
	copy(sealer.fingerprint[:], digest[:FingerprintSize])
	return sealer, nil
}

// FromHexKey is the configuration path: hex string in, sealer out.
func FromHexKey(hexKey string) (*Sealer, error) {
	key, err := ParseKey(hexKey)
	if err != nil {
		return nil, err
	}
	return New(key)
}

// Seal returns version || fingerprint || nonce || ciphertext.
//
// boundTo is authenticated but not encrypted: pass the identity of the row
// this secret belongs to. Without it, a sealed credential is a portable blob
// — anyone able to write the database could move the GitHub token into a mail
// connection pointing at a server they control, and it would decrypt cleanly.
// Binding makes that a decryption failure.
//
// A fresh random nonce per call is what keeps two connections that share a
// credential from being visibly identical rows.
func (s *Sealer) Seal(plaintext, boundTo []byte) ([]byte, error) {
	if s == nil || s.aead == nil {
		return nil, ErrNoKey
	}
	header := make([]byte, 0, HeaderSize+s.aead.NonceSize()+len(plaintext)+s.aead.Overhead())
	header = append(header, sealVersion)
	header = append(header, s.fingerprint[:]...)
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	header = append(header, nonce...)
	return s.aead.Seal(header, nonce, plaintext, boundTo), nil
}

// Open reverses Seal, and fails unless boundTo matches what was sealed.
//
// Nothing on the read side of the integrations API calls this, because no API
// response carries a credential. It is here for the tool path that will
// eventually use one, and its signature forces that caller to say which
// connection it believes it is opening.
func (s *Sealer) Open(sealed, boundTo []byte) ([]byte, error) {
	if s == nil || s.aead == nil {
		return nil, ErrNoKey
	}
	if len(sealed) < HeaderSize+s.aead.NonceSize() {
		return nil, ErrCiphertextInvalid
	}
	if sealed[0] != sealVersion {
		return nil, ErrCiphertextInvalid
	}
	if !s.SealedWithThisKey(sealed) {
		return nil, ErrCiphertextInvalid
	}
	nonce := sealed[HeaderSize : HeaderSize+s.aead.NonceSize()]
	ciphertext := sealed[HeaderSize+s.aead.NonceSize():]
	plaintext, err := s.aead.Open(nil, nonce, ciphertext, boundTo)
	if err != nil {
		// GCM's own error names the failure mode; callers get a fixed one so a
		// decryption failure cannot be turned into an oracle.
		return nil, ErrCiphertextInvalid
	}
	return plaintext, nil
}

// SealedWithThisKey reports whether a stored value carries this sealer's key
// fingerprint. It reads five bytes and decrypts nothing, so a caller can ask
// it about every row in a list without the key touching a credential.
//
// A false answer means the credential can never be opened again — the key in
// .env is not the one it was sealed with. That is worth saying out loud
// rather than leaving a connection to claim it still works.
func (s *Sealer) SealedWithThisKey(sealedPrefix []byte) bool {
	if s == nil || s.aead == nil {
		return false
	}
	if len(sealedPrefix) < HeaderSize || sealedPrefix[0] != sealVersion {
		return false
	}
	for i := 0; i < FingerprintSize; i++ {
		if sealedPrefix[1+i] != s.fingerprint[i] {
			return false
		}
	}
	return true
}
