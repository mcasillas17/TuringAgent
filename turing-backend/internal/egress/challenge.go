package egress

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// SignChallenge signs a consent challenge's payload with HMAC-SHA256 under a
// key derived from secret for domain, as base64url(payload) "."
// base64url(signature). Each consent flow has its own domain, so a challenge
// one issues is never accepted by another.
func SignChallenge(secret, domain string, payload []byte) string {
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(challengeSignature(secret, domain, payload))
}

// OpenChallenge verifies a token SignChallenge made for the same secret and
// domain and returns its payload. It refuses a token over twice maxBytes, a
// payload over maxBytes, and either half in anything but its canonical
// encoding.
func OpenChallenge(secret, domain, token string, maxBytes int) ([]byte, error) {
	if token == "" || len(token) > maxBytes*2 {
		return nil, errors.New("invalid challenge length")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, errors.New("invalid challenge shape")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(payload) > maxBytes || base64.RawURLEncoding.EncodeToString(payload) != parts[0] {
		return nil, errors.New("invalid challenge payload")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(signature) != parts[1] ||
		!hmac.Equal(signature, challengeSignature(secret, domain, payload)) {
		return nil, errors.New("invalid challenge signature")
	}
	return payload, nil
}

func challengeSignature(secret, domain string, payload []byte) []byte {
	deriver := hmac.New(sha256.New, []byte(secret))
	_, _ = deriver.Write([]byte(domain))
	signature := hmac.New(sha256.New, deriver.Sum(nil))
	_, _ = signature.Write(payload)
	return signature.Sum(nil)
}

// NewChallengeNonce is a challenge's single-use nonce: 32 random bytes,
// base64url.
func NewChallengeNonce() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
