// Package boundedjson decodes JSON from a reader it does not trust without
// holding more of it than its caller allows.
//
// DecodeArray streams one top-level array, decoding one element at a time, so
// an answer of any length costs one element's worth of memory, and each
// element is bounded on its own. DecodeValue reads one value whole up to a
// bound. Both refuse, rather than return part of what they read, with one of
// three typed errors: ErrTooLarge, ErrTooManyElements or ErrMalformed. A
// failure to read is passed through untyped, so a caller can tell a broken
// connection from an answer it must not trust. No error carries input text.
package boundedjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var (
	// ErrTooLarge refuses a value, or one array element, over its byte bound.
	ErrTooLarge = errors.New("boundedjson: value is over its byte bound")
	// ErrTooManyElements refuses an array holding more elements than allowed.
	ErrTooManyElements = errors.New("boundedjson: array holds more elements than allowed")
	// ErrMalformed refuses input that is not the JSON asked for: invalid or
	// cut-off JSON, text after the value, or a value of another shape.
	ErrMalformed = errors.New("boundedjson: malformed JSON")
)

// ArrayLimits bound one array.
type ArrayLimits struct {
	// MaxElements is the most elements the array may hold. Zero admits only
	// an empty array.
	MaxElements int
	// MaxElementBytes bounds each element, counted with the separator and
	// whitespace before it. The bound is exact for an object or an array; a
	// scalar is seen whole only once the byte after it is read.
	MaxElementBytes int64
}

// DecodeArray decodes the one JSON array r holds, handing each element to
// each, decoded into a T, before it reads the next one.
//
// It never reads more than limits.MaxElementBytes past the end of the
// previous element, so an element over that bound is refused before it is
// read whole, and it refuses an element past limits.MaxElements before
// decoding it. Elements handed over before a refusal are not taken back: a
// caller that must not act on part of an array keeps what it was handed
// until DecodeArray returns nil. An error each returns stops the decode and
// is returned unchanged.
func DecodeArray[T any](r io.Reader, limits ArrayLimits, each func(T) error) error {
	if limits.MaxElements < 0 || limits.MaxElementBytes <= 0 {
		return errors.New("boundedjson: array limits must be positive")
	}
	source := &boundedSource{reader: r}
	decoder := json.NewDecoder(source)

	source.allow(decoder.InputOffset() + limits.MaxElementBytes)
	opening, err := decoder.Token()
	if err != nil {
		return source.refusal(decoder, limits.MaxElementBytes)
	}
	if opening != json.Delim('[') {
		return malformed(decoder.InputOffset())
	}
	for count := 0; ; count++ {
		source.allow(decoder.InputOffset() + limits.MaxElementBytes)
		if !decoder.More() {
			break
		}
		if count == limits.MaxElements {
			return fmt.Errorf("%w (%d)", ErrTooManyElements, limits.MaxElements)
		}
		var element T
		if err := decoder.Decode(&element); err != nil {
			return source.refusal(decoder, limits.MaxElementBytes)
		}
		if err := each(element); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return source.refusal(decoder, limits.MaxElementBytes)
	}
	// One array is the whole answer: anything after it but whitespace, a
	// second value included, refuses it.
	source.allow(decoder.InputOffset() + limits.MaxElementBytes)
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return malformed(decoder.InputOffset())
		}
		return source.refusal(decoder, limits.MaxElementBytes)
	}
	return nil
}

// DecodeValue reads the one JSON value r holds, at most maxBytes of it, and
// decodes it into a T.
func DecodeValue[T any](r io.Reader, maxBytes int64) (T, error) {
	var value T
	if maxBytes <= 0 {
		return value, errors.New("boundedjson: the byte bound must be positive")
	}
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return value, fmt.Errorf("boundedjson: read failed: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return value, fmt.Errorf("%w (%d bytes)", ErrTooLarge, maxBytes)
	}
	if err := json.Unmarshal(data, &value); err != nil {
		var zero T
		var syntax *json.SyntaxError
		var mismatch *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntax):
			return zero, malformed(syntax.Offset)
		case errors.As(err, &mismatch):
			return zero, malformed(mismatch.Offset)
		}
		return zero, ErrMalformed
	}
	return value, nil
}

// malformed names where the input went wrong and never what it said.
func malformed(offset int64) error {
	return fmt.Errorf("%w near byte %d", ErrMalformed, offset)
}

// boundedSource hands its decoder at most limit bytes in all. The decoder
// buffers ahead, so a bound on what one Decode call consumes would not bound
// what it reads; a bound on the source does.
type boundedSource struct {
	reader    io.Reader
	delivered int64
	limit     int64
	exceeded  bool
	failure   error
}

// allow raises the limit to an absolute offset; it never lowers it, because
// bytes already handed over cannot be taken back.
func (s *boundedSource) allow(limit int64) {
	if limit > s.limit {
		s.limit = limit
	}
}

func (s *boundedSource) Read(p []byte) (int, error) {
	remaining := s.limit - s.delivered
	if remaining <= 0 {
		s.exceeded = true
		return 0, ErrTooLarge
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := s.reader.Read(p)
	s.delivered += int64(n)
	if err != nil && err != io.EOF && s.failure == nil {
		s.failure = err
	}
	return n, err
}

// refusal says why the decoder stopped. The decoder's own error is not
// consulted for that, because it can quote the input.
func (s *boundedSource) refusal(decoder *json.Decoder, bound int64) error {
	switch {
	case s.exceeded:
		return fmt.Errorf("%w (%d bytes per element)", ErrTooLarge, bound)
	case s.failure != nil:
		return fmt.Errorf("boundedjson: read failed: %w", s.failure)
	default:
		return malformed(decoder.InputOffset())
	}
}
