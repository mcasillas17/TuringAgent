package sessions

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"unicode"
	"unicode/utf8"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
)

const (
	sessionCursorDomain        = "turing.session-list.cursor.v1\x00"
	sessionCursorMagic         = "TSLC"
	sessionCursorVersion  byte = 1
	maxSessionCursorBytes      = 2048
	maxSessionIDBytes          = 256

	sessionCursorMagicSize       = len(sessionCursorMagic)
	sessionCursorTimestampSize   = len("2006-01-02T15:04:05.000000000Z")
	sessionCursorTimestampOffset = sessionCursorMagicSize + 2
	sessionCursorIDLengthOffset  = sessionCursorTimestampOffset + sessionCursorTimestampSize
	sessionCursorIDOffset        = sessionCursorIDLengthOffset + 2
)

var errInvalidSessionCursor = errors.New("invalid session cursor")

type sessionFilter byte

const (
	sessionFilterActive   sessionFilter = 1
	sessionFilterArchived sessionFilter = 2
	sessionFilterAll      sessionFilter = 3
)

type sessionCursor struct {
	Filter    sessionFilter
	UpdatedAt string
	SessionID string
}

type sessionCursorCodec struct {
	key [sha256.Size]byte
}

func newSessionCursorCodec(key [sha256.Size]byte) sessionCursorCodec {
	return sessionCursorCodec{key: key}
}

func (c sessionCursorCodec) encode(cursor sessionCursor) (string, error) {
	if !validSessionFilter(cursor.Filter) || !validSessionID(cursor.SessionID) {
		return "", errInvalidSessionCursor
	}
	if _, err := persisttime.ParseCanonical(cursor.UpdatedAt); err != nil {
		return "", errInvalidSessionCursor
	}

	body := make([]byte, sessionCursorIDOffset+len(cursor.SessionID))
	copy(body, sessionCursorMagic)
	body[sessionCursorMagicSize] = sessionCursorVersion
	body[sessionCursorMagicSize+1] = byte(cursor.Filter)
	copy(body[sessionCursorTimestampOffset:sessionCursorIDLengthOffset], cursor.UpdatedAt)
	binary.BigEndian.PutUint16(body[sessionCursorIDLengthOffset:sessionCursorIDOffset], uint16(len(cursor.SessionID)))
	copy(body[sessionCursorIDOffset:], cursor.SessionID)

	return base64.RawURLEncoding.EncodeToString(append(body, c.tag(body)...)), nil
}

func (c sessionCursorCodec) decode(encoded string, expectedFilter sessionFilter) (sessionCursor, error) {
	if encoded == "" || len(encoded) > maxSessionCursorBytes {
		return sessionCursor{}, errInvalidSessionCursor
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.RawURLEncoding.EncodeToString(data) != encoded {
		return sessionCursor{}, errInvalidSessionCursor
	}
	if len(data) < sessionCursorIDOffset+1+sha256.Size {
		return sessionCursor{}, errInvalidSessionCursor
	}

	body := data[:len(data)-sha256.Size]
	tag := data[len(data)-sha256.Size:]
	if !hmac.Equal(tag, c.tag(body)) {
		return sessionCursor{}, errInvalidSessionCursor
	}

	if !bytes.Equal(body[:sessionCursorMagicSize], []byte(sessionCursorMagic)) ||
		body[sessionCursorMagicSize] != sessionCursorVersion {
		return sessionCursor{}, errInvalidSessionCursor
	}
	filter := sessionFilter(body[sessionCursorMagicSize+1])
	if !validSessionFilter(filter) || filter != expectedFilter {
		return sessionCursor{}, errInvalidSessionCursor
	}
	updatedAt := string(body[sessionCursorTimestampOffset:sessionCursorIDLengthOffset])
	if _, err := persisttime.ParseCanonical(updatedAt); err != nil {
		return sessionCursor{}, errInvalidSessionCursor
	}
	idLength := int(binary.BigEndian.Uint16(body[sessionCursorIDLengthOffset:sessionCursorIDOffset]))
	if idLength == 0 || idLength > maxSessionIDBytes || len(body) != sessionCursorIDOffset+idLength {
		return sessionCursor{}, errInvalidSessionCursor
	}
	sessionID := string(body[sessionCursorIDOffset:])
	if !validSessionID(sessionID) {
		return sessionCursor{}, errInvalidSessionCursor
	}
	return sessionCursor{
		Filter:    filter,
		UpdatedAt: updatedAt,
		SessionID: sessionID,
	}, nil
}

func (c sessionCursorCodec) tag(body []byte) []byte {
	mac := hmac.New(sha256.New, c.key[:])
	_, _ = mac.Write([]byte(sessionCursorDomain))
	_, _ = mac.Write(body)
	return mac.Sum(nil)
}

func validSessionFilter(filter sessionFilter) bool {
	return filter == sessionFilterActive ||
		filter == sessionFilterArchived ||
		filter == sessionFilterAll
}

func validSessionID(sessionID string) bool {
	if sessionID == "" || len(sessionID) > maxSessionIDBytes || !utf8.ValidString(sessionID) {
		return false
	}
	for _, value := range sessionID {
		if unicode.IsControl(value) {
			return false
		}
	}
	return true
}
