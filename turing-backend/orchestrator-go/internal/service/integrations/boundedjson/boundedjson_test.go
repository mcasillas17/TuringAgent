package boundedjson

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

type item struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

func collect(t *testing.T, input string, limits ArrayLimits) ([]item, error) {
	t.Helper()
	var got []item
	err := DecodeArray(strings.NewReader(input), limits, func(element item) error {
		got = append(got, element)
		return nil
	})
	return got, err
}

func TestDecodeArrayHandsOverEveryElementInOrder(t *testing.T) {
	got, err := collect(t, ` [ {"id":1,"text":"a","extra":{"x":[1,2]}} , {"id":2,"text":"b"} ] `, ArrayLimits{MaxElements: 2, MaxElementBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	want := []item{{ID: 1, Text: "a"}, {ID: 2, Text: "b"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDecodeArrayOfNothingCallsNothing(t *testing.T) {
	got, err := collect(t, `[]`, ArrayLimits{MaxElements: 0, MaxElementBytes: 16})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v err %v, want no elements and no error", got, err)
	}
}

// The bound is the element plus the separator and whitespace before it. An
// object that fits it exactly is decoded; one byte more is refused.
func TestDecodeArrayBoundsEachElementExactly(t *testing.T) {
	const object = `{"id":7,"text":"abcdefgh"}` // 26 bytes
	for _, test := range []struct {
		name  string
		input string
		bound int64
		err   error
	}{
		{name: "first element at the bound", input: `[` + object + `]`, bound: 26},
		{name: "first element over the bound", input: `[` + object + `]`, bound: 25, err: ErrTooLarge},
		{name: "second element and its comma at the bound", input: `[` + object + `,` + object + `]`, bound: 27},
		{name: "second element and its comma over the bound", input: `[` + object + `,` + object + `]`, bound: 26, err: ErrTooLarge},
		{name: "whitespace counts against the element after it", input: `[` + object + `,   ` + object + `]`, bound: 29, err: ErrTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := collect(t, test.input, ArrayLimits{MaxElements: 2, MaxElementBytes: test.bound})
			if !errors.Is(err, test.err) {
				t.Fatalf("err = %v, want %v", err, test.err)
			}
		})
	}
}

func TestDecodeArrayRefusesAnElementOverTheBoundWithoutReadingPastIt(t *testing.T) {
	huge := `{"id":2,"text":"` + strings.Repeat("x", 4<<20) + `"}`
	source := &countingSource{reader: strings.NewReader(`[{"id":1},` + huge + `]`)}
	var seen []int
	err := DecodeArray(source, ArrayLimits{MaxElements: 10, MaxElementBytes: 1 << 20}, func(element item) error {
		seen = append(seen, element.ID)
		return nil
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if source.read > 1<<20+64 {
		t.Fatalf("read %d bytes of a 4 MiB element, want no more than its 1 MiB bound", source.read)
	}
	if len(seen) != 1 || seen[0] != 1 {
		t.Fatalf("elements before the refusal = %v, want [1]", seen)
	}
}

func TestDecodeArrayRefusesMoreElementsThanAllowedBeforeDecodingTheExtraOne(t *testing.T) {
	input := `[{"id":1},{"id":2},{"id":3,"text":"` + strings.Repeat("y", 2<<20) + `"}]`
	source := &countingSource{reader: strings.NewReader(input)}
	calls := 0
	err := DecodeArray(source, ArrayLimits{MaxElements: 2, MaxElementBytes: 4 << 20}, func(item) error {
		calls++
		return nil
	})
	if !errors.Is(err, ErrTooManyElements) {
		t.Fatalf("err = %v, want ErrTooManyElements", err)
	}
	if calls != 2 {
		t.Fatalf("callbacks = %d, want the two allowed elements only", calls)
	}
	if source.read > 64<<10 {
		t.Fatalf("read %d bytes, want the extra element left unread", source.read)
	}
}

func TestDecodeArrayRefusesWhatIsNotOneArrayOfElements(t *testing.T) {
	for name, input := range map[string]string{
		"an object":                  `{"id":1}`,
		"null":                       `null`,
		"empty input":                ``,
		"cut after an element":       `[{"id":1}`,
		"cut inside an element":      `[{"id":1,"text":"ab`,
		"an element that is invalid": `[{"id":}]`,
		"an element of another type": `[{"id":"one"}]`,
		"a scalar element":           `[1]`,
		"trailing text":              `[{"id":1}] x`,
		"a second value":             `[{"id":1}][]`,
		"a missing comma":            `[{"id":1} {"id":2}]`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := collect(t, input, ArrayLimits{MaxElements: 5, MaxElementBytes: 1024})
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("err = %v, want ErrMalformed", err)
			}
		})
	}
}

// A refusal names where the input went wrong, never what it said, so text from
// a third party cannot travel out through the error.
func TestDecodeArrayRefusalsCarryNoInputText(t *testing.T) {
	_, err := collect(t, `[{"id":1,"text":"secret-sentinel"}, secret-sentinel]`, ArrayLimits{MaxElements: 5, MaxElementBytes: 1024})
	if !errors.Is(err, ErrMalformed) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "'s'") {
		t.Fatalf("err = %v, want ErrMalformed without input text", err)
	}
}

func TestDecodeArrayReturnsTheCallbacksErrorUnchanged(t *testing.T) {
	stop := errors.New("caller stopped")
	calls := 0
	err := DecodeArray(strings.NewReader(`[{"id":1},{"id":2}]`), ArrayLimits{MaxElements: 2, MaxElementBytes: 64}, func(item) error {
		calls++
		return stop
	})
	if err != stop || calls != 1 {
		t.Fatalf("err = %v after %d calls, want the callback's own error after one", err, calls)
	}
}

// A connection that fails mid-answer is the transport's failure, not a
// malformed answer: a caller retries one and distrusts the other.
func TestDecodeArrayPassesAReadFailureThroughUntyped(t *testing.T) {
	broken := errors.New("connection reset")
	reader := io.MultiReader(strings.NewReader(`[{"id":1},{"id":2,"te`), failingReader{err: broken})
	err := DecodeArray(reader, ArrayLimits{MaxElements: 5, MaxElementBytes: 64}, func(item) error { return nil })
	if !errors.Is(err, broken) {
		t.Fatalf("err = %v, want the read failure", err)
	}
	for _, typed := range []error{ErrMalformed, ErrTooLarge, ErrTooManyElements} {
		if errors.Is(err, typed) {
			t.Fatalf("err = %v, a read failure was typed as %v", err, typed)
		}
	}
}

// An answer far larger than the element bound decodes as long as each element
// fits, and the decoder never reads more than one bound past the end of the
// element before the one it hands over. The bound here is one element and a
// little room, which json.Decoder's own read-ahead would pass.
func TestDecodeArrayReadsALargeAnswerOneElementAtATime(t *testing.T) {
	const elements, bound = 100, 64<<10 + 64
	var input strings.Builder
	input.WriteString("[")
	ends := make([]int, elements)
	for index := range elements {
		if index > 0 {
			input.WriteString(",")
		}
		fmt.Fprintf(&input, `{"id":%d,"text":"%s"}`, index, strings.Repeat("z", 64<<10-32))
		ends[index] = input.Len()
	}
	input.WriteString("]")
	source := &countingSource{reader: strings.NewReader(input.String())}
	next := 0
	err := DecodeArray(source, ArrayLimits{MaxElements: elements, MaxElementBytes: bound}, func(element item) error {
		if element.ID != next {
			return fmt.Errorf("element %d arrived in place of %d", element.ID, next)
		}
		previousEnd := 1 // just past the opening bracket
		if next > 0 {
			previousEnd = ends[next-1]
		}
		if source.read > int64(previousEnd)+bound {
			return fmt.Errorf("read %d bytes by element %d, over %d past the element before it", source.read, next, bound)
		}
		next++
		return nil
	})
	if err != nil || next != elements {
		t.Fatalf("err = %v after %d elements, want all %d of a %d-byte answer", err, next, elements, input.Len())
	}
}

func TestDecodeArrayRefusesLimitsThatAdmitNothing(t *testing.T) {
	for _, limits := range []ArrayLimits{{MaxElements: -1, MaxElementBytes: 8}, {MaxElements: 1, MaxElementBytes: 0}} {
		if err := DecodeArray(strings.NewReader(`[]`), limits, func(item) error { return nil }); err == nil {
			t.Fatalf("limits %+v were accepted", limits)
		}
	}
}

func TestDecodeValueReadsOneValueUpToItsBound(t *testing.T) {
	const object = `{"id":9,"text":"whole"}` // 23 bytes
	got, err := DecodeValue[item](strings.NewReader(object), 23)
	if err != nil || got != (item{ID: 9, Text: "whole"}) {
		t.Fatalf("got %+v err %v", got, err)
	}
	if _, err := DecodeValue[item](strings.NewReader(object), 22); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge one byte over the bound", err)
	}
}

func TestDecodeValueRefusesMalformedAndPassesReadFailuresThrough(t *testing.T) {
	for name, input := range map[string]string{
		"cut":           `{"id":9`,
		"trailing text": `{"id":9} x`,
		"another type":  `["id"]`,
		"empty":         ``,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeValue[item](strings.NewReader(input), 64); !errors.Is(err, ErrMalformed) {
				t.Fatalf("err = %v, want ErrMalformed", err)
			}
		})
	}
	broken := errors.New("connection reset")
	_, err := DecodeValue[item](io.MultiReader(strings.NewReader(`{"id":`), failingReader{err: broken}), 64)
	if !errors.Is(err, broken) || errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want the untyped read failure", err)
	}
}

type countingSource struct {
	reader io.Reader
	read   int64
}

func (s *countingSource) Read(p []byte) (int, error) {
	n, err := s.reader.Read(p)
	s.read += int64(n)
	return n, err
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }
