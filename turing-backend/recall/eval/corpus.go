package eval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxJSONBytes   = 1 << 20
	maxBodyBytes   = 16 << 10
	maxQueryBytes  = 512
	maxOutputBytes = 512 << 10
)

var mandatoryCategories = []string{
	"exact", "phrase", "operators", "paraphrase", "correction", "temporal",
	"synthesis", "cjk-whole", "cjk-partial", "cjk-short", "injection",
	"deletion", "abstention", "identity", "history-cutoff", "history-budget",
	"partial-excerpt", "recall-omission",
}
var fixtureID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func readBounded(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxJSONBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxJSONBytes {
		return nil, fmt.Errorf("JSON file byte bound")
	}
	return data, nil
}

type corpus struct {
	Version  int              `json:"version"`
	Sessions []fixtureSession `json:"sessions"`
	Messages []fixtureMessage `json:"messages"`
	Cases    []fixtureCase    `json:"cases"`
	Hash     string           `json:"-"`
}
type fixtureSession struct {
	ID    string `json:"id"`
	At    string `json:"at"`
	State string `json:"state"`
}
type fixtureMessage struct {
	ID      string `json:"id"`
	Session string `json:"session"`
	Role    string `json:"role"`
	At      string `json:"at"`
	Body    string `json:"body"`
}
type judgment struct {
	Message    string `json:"message"`
	Grade      *int   `json:"grade"`
	Span       string `json:"span"`
	ValidFrom  string `json:"valid_from"`
	ValidUntil string `json:"valid_until"`
	Exclude    string `json:"exclude"`
}
type fixtureCase struct {
	ID             string     `json:"id"`
	Categories     []string   `json:"categories"`
	Limitation     string     `json:"limitation"`
	CurrentSession string     `json:"current_session"`
	Anchor         string     `json:"anchor"`
	At             string     `json:"at"`
	AsOf           string     `json:"as_of"`
	Query          string     `json:"query"`
	Phrase         string     `json:"phrase"`
	Scope          string     `json:"scope"`
	ExcludeSession string     `json:"exclude_session"`
	Window         int        `json:"window"`
	Judgments      []judgment `json:"judgments"`
}

func utc(value string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339, value)
	if err != nil || !strings.HasSuffix(value, "Z") || at.UTC().Format(time.RFC3339) != value {
		return time.Time{}, fmt.Errorf("timestamp must be canonical UTC seconds")
	}
	return at, nil
}

// strictJSON rejects duplicate keys before typed decoding, including escaped
// spellings of the same key. Null is not a valid fixture/baseline value.
func strictJSON(data []byte, target any) error {
	if len(data) > maxJSONBytes || !utf8.Valid(data) {
		return fmt.Errorf("JSON byte/UTF-8 bound")
	}
	scan := json.NewDecoder(bytes.NewReader(data))
	scan.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 24 {
			return fmt.Errorf("JSON nesting bound")
		}
		token, err := scan.Token()
		if err != nil {
			return err
		}
		if token == nil {
			return fmt.Errorf("null is not permitted")
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		keys := map[string]bool{}
		for scan.More() {
			if delim == '{' {
				key, err := scan.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || keys[name] {
					return fmt.Errorf("duplicate or malformed object key")
				}
				keys[name] = true
			}
			if err := value(depth + 1); err != nil {
				return err
			}
		}
		_, err = scan.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := scan.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	if err := exactFields(data, reflect.TypeOf(target)); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

// encoding/json otherwise accepts case-insensitive field aliases, which would
// make {"version":1,"Version":1} a second spelling of the same logical field.
func exactFields(data []byte, schema reflect.Type) error {
	if schema.Kind() == reflect.Pointer {
		return exactFields(data, schema.Elem())
	}
	switch schema.Kind() {
	case reflect.Struct, reflect.Map:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		known := map[string]reflect.Type{}
		if schema.Kind() == reflect.Struct {
			for i := 0; i < schema.NumField(); i++ {
				field := schema.Field(i)
				name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
				if name != "-" {
					known[name] = field.Type
				}
			}
		}
		for name, raw := range fields {
			var field reflect.Type
			if schema.Kind() == reflect.Map {
				field = schema.Elem()
			} else {
				field = known[name]
			}
			if field == nil {
				return fmt.Errorf("unknown JSON field")
			}
			if err := exactFields(raw, field); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		for _, raw := range values {
			if err := exactFields(raw, schema.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadCorpus(data []byte) (corpus, error) {
	var c corpus
	if err := strictJSON(data, &c); err != nil {
		return c, err
	}
	if c.Version != 1 || len(c.Cases) == 0 || len(c.Cases) > 64 || len(c.Sessions) > 64 ||
		len(c.Messages) == 0 || len(c.Messages) > 256 {
		return c, fmt.Errorf("schema or corpus count bound")
	}
	seen := map[string]bool{}
	checkID := func(id, prefix string) error {
		if !fixtureID.MatchString(id) || !strings.HasPrefix(id, prefix) || seen[id] {
			return fmt.Errorf("malformed or duplicate identifier")
		}
		seen[id] = true
		return nil
	}
	sessions := map[string]fixtureSession{}
	for _, s := range c.Sessions {
		if err := checkID(s.ID, "ses_"); err != nil {
			return c, err
		}
		if _, err := utc(s.At); err != nil {
			return c, err
		}
		switch s.State {
		case "active", "archived", "deleting", "deleted":
		default:
			return c, fmt.Errorf("invalid lifecycle")
		}
		sessions[s.ID] = s
	}
	messages := map[string]fixtureMessage{}
	last := map[string]time.Time{}
	for _, m := range c.Messages {
		if err := checkID(m.ID, "msg_"); err != nil {
			return c, err
		}
		s, ok := sessions[m.Session]
		if !ok {
			return c, fmt.Errorf("missing session reference")
		}
		at, err := utc(m.At)
		if err != nil {
			return c, err
		}
		start, _ := utc(s.At)
		if at.Before(start) || at.Before(last[m.Session]) {
			return c, fmt.Errorf("history timestamps out of sequence")
		}
		last[m.Session] = at
		if (m.Role != "user" && m.Role != "assistant" && m.Role != "system" && m.Role != "tool") ||
			len(m.Body) == 0 || len(m.Body) > maxBodyBytes {
			return c, fmt.Errorf("message role/body bound")
		}
		messages[m.ID] = m
	}
	categories := map[string]bool{}
	privateSpans := map[string]bool{}
	for _, test := range c.Cases {
		if err := checkID(test.ID, ""); err != nil {
			return c, err
		}
		if err := checkID(test.Anchor, "msg_"); err != nil {
			return c, err
		}
		if sessions[test.CurrentSession].State != "active" {
			return c, fmt.Errorf("current session must exist and be active")
		}
		for _, id := range []string{test.Scope, test.ExcludeSession} {
			if id != "" && sessions[id].ID == "" {
				return c, fmt.Errorf("missing search scope reference")
			}
		}
		at, err := utc(test.At)
		if err != nil {
			return c, err
		}
		asOf, err := utc(test.AsOf)
		if err != nil {
			return c, err
		}
		if at.Before(last[test.CurrentSession]) || asOf.After(at) {
			return c, fmt.Errorf("invalid anchor/as-of chronology")
		}
		if strings.TrimSpace(test.Query) == "" || strings.TrimSpace(test.Phrase) == "" ||
			len(test.Query) > maxQueryBytes || len(test.Phrase) > maxQueryBytes ||
			test.Window < 256 || test.Window > 65536 || test.Judgments == nil || len(test.Judgments) > 256 || len(test.Limitation) > 1024 {
			return c, fmt.Errorf("query, judgment, or context window bound")
		}
		localCategories := map[string]bool{}
		for _, category := range test.Categories {
			known := false
			for _, mandatory := range mandatoryCategories {
				if category == mandatory {
					known = true
				}
			}
			if !known || localCategories[category] {
				return c, fmt.Errorf("unknown or duplicate category")
			}
			localCategories[category], categories[category] = true, true
		}
		if len(localCategories) == 0 {
			return c, fmt.Errorf("missing case category")
		}
		judged := map[string]bool{}
		for _, j := range test.Judgments {
			m, ok := messages[j.Message]
			if !ok || judged[j.Message] || j.Grade == nil || *j.Grade < 0 || *j.Grade > 2 {
				return c, fmt.Errorf("invalid judgment reference/grade")
			}
			judged[j.Message] = true
			if j.Span == "" || !strings.Contains(m.Body, j.Span) || len(j.Span) > 1024 {
				return c, fmt.Errorf("missing evidence span")
			}
			from, err := utc(j.ValidFrom)
			if err != nil {
				return c, err
			}
			expired := asOf.Before(from)
			if j.ValidUntil != "" {
				until, err := utc(j.ValidUntil)
				if err != nil || !until.After(from) {
					return c, fmt.Errorf("invalid temporal interval")
				}
				expired = expired || !asOf.Before(until)
			}
			switch j.Exclude {
			case "", "stale", "scope", "privacy":
			default:
				return c, fmt.Errorf("unknown exclusion")
			}
			if (*j.Grade > 0 && (expired || j.Exclude != "" || sessions[m.Session].State == "deleted" || sessions[m.Session].State == "deleting")) ||
				(expired != (j.Exclude == "stale")) ||
				(j.Exclude == "privacy" && sessions[m.Session].State != "deleted" && sessions[m.Session].State != "deleting") {
				return c, fmt.Errorf("relevance contradicts temporal/lifecycle validity")
			}
			if j.Exclude == "privacy" {
				privateSpans[j.Message] = true
			}
		}
	}
	for _, m := range c.Messages {
		state := sessions[m.Session].State
		if (state == "deleting" || state == "deleted") && !privateSpans[m.ID] {
			return c, fmt.Errorf("withdrawn source requires explicit privacy span")
		}
	}
	for _, category := range mandatoryCategories {
		if !categories[category] {
			return c, fmt.Errorf("missing mandatory category: %s", category)
		}
	}
	sum := sha256.Sum256(data)
	c.Hash = hex.EncodeToString(sum[:])
	return c, nil
}
