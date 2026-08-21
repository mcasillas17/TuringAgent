package sessions

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
)

func TestSessionCursorRoundTrip(t *testing.T) {
	if sessionCursorDomain != "turing.session-list.cursor.v1\x00" {
		t.Fatalf("session cursor domain = %q", sessionCursorDomain)
	}
	codec := newSessionCursorCodec([32]byte{1})
	want := sessionCursor{
		Filter:    sessionFilterActive,
		UpdatedAt: "2026-08-20T04:00:00.000000001Z",
		SessionID: "sess_01K34EXAMPLE",
	}
	encoded, err := codec.encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.decode(encoded, sessionFilterActive)
	if err != nil || got != want {
		t.Fatalf("decode = %+v, %v, want %+v", got, err, want)
	}
}

func TestSessionCursorRejectsForeignFilterAndSigningKey(t *testing.T) {
	codec := newSessionCursorCodec([32]byte{1})
	encoded, err := codec.encode(sessionCursor{
		Filter:    sessionFilterActive,
		UpdatedAt: "2026-08-20T04:00:00.000000001Z",
		SessionID: "session-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertInvalidSessionCursor(t, codec, encoded, sessionFilterArchived)
	assertInvalidSessionCursor(t, newSessionCursorCodec([32]byte{2}), encoded, sessionFilterActive)
}

func TestSessionCursorRejectsMalformedEncodings(t *testing.T) {
	key := [32]byte{1}
	codec := newSessionCursorCodec(key)
	valid, err := codec.encode(sessionCursor{
		Filter:    sessionFilterAll,
		UpdatedAt: "2026-08-20T04:00:00.000000001Z",
		SessionID: "session-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(valid)
	if err != nil {
		t.Fatal(err)
	}
	tamperedTag := append([]byte(nil), decoded...)
	tamperedTag[len(tamperedTag)-1] ^= 1
	trailingBody := append(append([]byte(nil), decoded[:len(decoded)-sha256.Size]...), 0)
	trailingBody = appendSessionCursorTag(key, trailingBody)

	testCases := map[string]string{
		"empty":                  "",
		"padding":                valid + "=",
		"noncanonical tail bits": noncanonicalBase64Tail(t, valid),
		"truncated":              valid[:len(valid)-1],
		"tampered tag":           base64.RawURLEncoding.EncodeToString(tamperedTag),
		"trailing body byte":     base64.RawURLEncoding.EncodeToString(trailingBody),
		"oversize":               strings.Repeat("A", maxSessionCursorBytes+1),
	}
	for name, encoded := range testCases {
		t.Run(name, func(t *testing.T) {
			assertInvalidSessionCursor(t, codec, encoded, sessionFilterAll)
		})
	}
}

func TestSessionCursorAuthenticatesBeforeSemanticValidation(t *testing.T) {
	key := [32]byte{1}
	codec := newSessionCursorCodec(key)
	body := validSessionCursorBody(t, codec)

	testCases := map[string]func([]byte){
		"magic": func(candidate []byte) {
			candidate[0] ^= 1
		},
		"version": func(candidate []byte) {
			candidate[sessionCursorMagicSize]++
		},
		"filter": func(candidate []byte) {
			candidate[sessionCursorMagicSize+1] = 0xff
		},
		"timestamp": func(candidate []byte) {
			candidate[sessionCursorMagicSize+2] = 'x'
		},
		"empty id": func(candidate []byte) {
			binary.BigEndian.PutUint16(candidate[sessionCursorIDLengthOffset:sessionCursorIDOffset], 0)
			clear(candidate[sessionCursorIDOffset:])
		},
		"bad id length": func(candidate []byte) {
			binary.BigEndian.PutUint16(candidate[sessionCursorIDLengthOffset:sessionCursorIDOffset], 256)
		},
		"control id": func(candidate []byte) {
			candidate[sessionCursorIDOffset] = '\n'
		},
	}
	for name, mutate := range testCases {
		t.Run(name, func(t *testing.T) {
			candidate := append([]byte(nil), body...)
			mutate(candidate)
			encoded := base64.RawURLEncoding.EncodeToString(appendSessionCursorTag(key, candidate))
			assertInvalidSessionCursor(t, codec, encoded, sessionFilterActive)
		})
	}
}

func TestSessionCursorEncodeRejectsInvalidAnchors(t *testing.T) {
	codec := newSessionCursorCodec([32]byte{1})
	testCases := []sessionCursor{
		{
			Filter:    sessionFilterActive,
			UpdatedAt: "2026-08-20T04:00:00Z",
			SessionID: "session-1",
		},
		{
			Filter:    0xff,
			UpdatedAt: "2026-08-20T04:00:00.000000001Z",
			SessionID: "session-1",
		},
		{
			Filter:    sessionFilterActive,
			UpdatedAt: "2026-08-20T04:00:00.000000001Z",
			SessionID: "",
		},
		{
			Filter:    sessionFilterActive,
			UpdatedAt: "2026-08-20T04:00:00.000000001Z",
			SessionID: strings.Repeat("s", 257),
		},
		{
			Filter:    sessionFilterActive,
			UpdatedAt: "2026-08-20T04:00:00.000000001Z",
			SessionID: string([]byte{0xff}),
		},
	}
	for _, testCase := range testCases {
		if _, err := codec.encode(testCase); err != errInvalidSessionCursor {
			t.Fatalf("encode(%+v) error = %v, want errInvalidSessionCursor", testCase, err)
		}
	}
}

func validSessionCursorBody(t *testing.T, codec sessionCursorCodec) []byte {
	t.Helper()
	encoded, err := codec.encode(sessionCursor{
		Filter:    sessionFilterActive,
		UpdatedAt: "2026-08-20T04:00:00.000000001Z",
		SessionID: "session-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return decoded[:len(decoded)-sha256.Size]
}

func appendSessionCursorTag(key [32]byte, body []byte) []byte {
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write([]byte(sessionCursorDomain))
	_, _ = mac.Write(body)
	return append(body, mac.Sum(nil)...)
}

func assertInvalidSessionCursor(t *testing.T, codec sessionCursorCodec, encoded string, filter sessionFilter) {
	t.Helper()
	if _, err := codec.decode(encoded, filter); err != errInvalidSessionCursor {
		t.Fatalf("decode error = %v, want errInvalidSessionCursor", err)
	}
}

func noncanonicalBase64Tail(t *testing.T, encoded string) string {
	t.Helper()
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	index := strings.IndexByte(alphabet, encoded[len(encoded)-1])
	if index < 0 || index%4 != 0 {
		t.Fatalf("cursor last base64 character index = %d, want zero tail bits", index)
	}
	return encoded[:len(encoded)-1] + string(alphabet[index+1])
}
