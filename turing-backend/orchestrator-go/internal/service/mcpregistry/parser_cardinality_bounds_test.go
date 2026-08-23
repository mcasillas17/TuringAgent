package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Every streaming collection decoder in import.go must enforce its own
// small, named maximum *before* it appends (or, for the tools array,
// before it even decodes) one more element — never only after building an
// arbitrarily large slice or map first. These tests prove each cap trips
// at exactly the right boundary (N elements still succeed; N+1 refuse
// deterministically with a fixed, generic reason that echoes no attacker
// -controlled field or key value). For decodeMCPRootServers,
// decodeMCPHeaderEntries, and decodeMCPToolEntries, the excess element's
// own value is deliberately malformed JSON, proving the decoder never even
// attempts to parse it — a raw JSON syntax error would otherwise surface
// in its place if the cap were ever removed. decodeMCPEntryFields and
// decodeMCPToolFields do not themselves validate field names or values
// (that is validateMCPEntryFields/validateMCPToolFields's job one layer
// up), so a malformed excess value there would be decoded into a
// json.RawMessage without error either way; those two tests instead give
// the excess field a well-formed value, so removing the cap would make
// the call return one field too many with a nil error instead of the
// fixed refusal these tests assert.

// decodeMCPRootServers must cap how many top-level root-object members it
// ever walks, independent of which of them (if any) is the canonical
// "mcpServers" key: an attacker-sized document packed with many distinct,
// otherwise-irrelevant sibling keys must be refused once maxMCPRootFields
// is reached, rather than this loop decoding (and discarding) an unbounded
// number of sibling values first.
func TestDecodeMCPRootServersCapsTopLevelFieldCount(t *testing.T) {
	t.Run("exactly at the cap still succeeds", func(t *testing.T) {
		fields := map[string]any{"mcpServers": map[string]any{}}
		for i := 0; len(fields) < maxMCPRootFields; i++ {
			fields[fmt.Sprintf("sibling-%d", i)] = i
		}
		if len(fields) != maxMCPRootFields {
			t.Fatalf("test setup: len(fields) = %d, want exactly maxMCPRootFields (%d)", len(fields), maxMCPRootFields)
		}
		document, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeMCPRootServers(document); err != nil {
			t.Fatalf("a document with exactly maxMCPRootFields top-level members must not be refused: %v", err)
		}
	})

	t.Run("one more than the cap refuses before decoding it", func(t *testing.T) {
		var buf bytes.Buffer
		buf.WriteString(`{"mcpServers":{}`)
		for i := 0; i < maxMCPRootFields; i++ {
			fmt.Fprintf(&buf, `,"sibling-%d":0`, i)
		}
		// The (maxMCPRootFields+1)-th member's own value is malformed JSON.
		// If decodeMCPRootServers ever attempted to decode it, this would
		// surface as a JSON syntax error instead of the fixed cap refusal
		// below — proving the cap is checked strictly before that.
		buf.WriteString(`,"overflow": {not valid json`)
		buf.WriteString(`}`)

		_, err := decodeMCPRootServers(buf.Bytes())
		if err == nil {
			t.Fatal("want an error: a document exceeding maxMCPRootFields top-level members must be refused")
		}
		if err != errMCPRootTooManyFields {
			t.Fatalf("err = %v, want the fixed errMCPRootTooManyFields reason", err)
		}
		if strings.Contains(err.Error(), "overflow") || strings.Contains(err.Error(), "sibling") {
			t.Fatalf("err = %q, must not echo any field name", err.Error())
		}
	})
}

// decodeMCPEntryFields must cap an entry's own top-level field count at
// maxMCPServerEntryFields (the six canonical names: url, headers, command,
// args, env, tools) *before* appending a seventh field — validateMCPEntryFields
// would refuse any entry naming more than six distinct fields anyway, but
// only after this decode step had already appended every single one of
// them, however many an attacker supplied.
func TestDecodeMCPEntryFieldsCapsAtCanonicalFieldCount(t *testing.T) {
	t.Run("exactly six fields still decode", func(t *testing.T) {
		document := []byte(`{"url":"a","headers":{},"command":"","args":[],"env":{},"tools":[]}`)
		fields, err := decodeMCPEntryFields(document)
		if err != nil {
			t.Fatalf("an entry with exactly maxMCPServerEntryFields fields must not be refused: %v", err)
		}
		if len(fields) != maxMCPServerEntryFields {
			t.Fatalf("len(fields) = %d, want exactly maxMCPServerEntryFields (%d)", len(fields), maxMCPServerEntryFields)
		}
	})

	t.Run("a seventh field refuses before decoding it", func(t *testing.T) {
		// The seventh field's own value is well-formed JSON (unlike the
		// malformed-JSON probes used elsewhere in this file): without the
		// cap, decodeMCPEntryFields would happily decode it too and
		// return seven fields with a nil error, since this function does
		// not itself validate field names (that is validateMCPEntryFields's
		// job) — so this genuinely exercises the cap itself, not merely a
		// decode failure that would happen anyway.
		document := []byte(`{"url":"a","headers":{},"command":"","args":[],"env":{},"tools":[],"seventh":"x"}`)
		fields, err := decodeMCPEntryFields(document)
		if err != errMCPEntryFieldInvalid {
			t.Fatalf("err = %v, fields = %+v, want the fixed errMCPEntryFieldInvalid reason", err, fields)
		}
	})
}

// decodeMCPToolFields must cap a single tools[] element's own field count
// at maxMCPToolObjectFields (the three canonical names: name, description,
// inputSchema) *before* appending a fourth field.
func TestDecodeMCPToolFieldsCapsAtCanonicalFieldCount(t *testing.T) {
	t.Run("exactly three fields still decode", func(t *testing.T) {
		document := []byte(`{"name":"t","description":"d","inputSchema":{}}`)
		fields, err := decodeMCPToolFields(document)
		if err != nil {
			t.Fatalf("a tool object with exactly maxMCPToolObjectFields fields must not be refused: %v", err)
		}
		if len(fields) != maxMCPToolObjectFields {
			t.Fatalf("len(fields) = %d, want exactly maxMCPToolObjectFields (%d)", len(fields), maxMCPToolObjectFields)
		}
	})

	t.Run("a fourth field refuses before decoding it", func(t *testing.T) {
		// The fourth field's own value is well-formed JSON: without the
		// cap, decodeMCPToolFields would happily decode it too and return
		// four fields with a nil error, since this function does not
		// itself validate field names (that is validateMCPToolFields's
		// job) — so this genuinely exercises the cap itself, not merely a
		// decode failure that would happen anyway.
		document := []byte(`{"name":"t","description":"d","inputSchema":{},"fourth":"x"}`)
		fields, err := decodeMCPToolFields(document)
		if err == nil || err.Error() != mcpToolDefinitionRefusedMessage {
			t.Fatalf("err = %v, fields = %+v, want the fixed mcpToolDefinitionRefusedMessage reason", err, fields)
		}
	})
}

// decodeMCPHeaderEntries must cap the number of "headers" members it ever
// appends at a small, fixed maxMCPHeaderEntries — even though only a
// single case-insensitive Authorization key is ever actually supported
// (bearerFromHeaders) — so that a headers object packed with many
// distinct, tiny member names cannot force this decode loop to grow an
// unbounded slice before bearerFromHeaders ever gets a chance to refuse
// it as unsupported.
func TestDecodeMCPHeaderEntriesCapsAtHardLimit(t *testing.T) {
	t.Run("exactly at the cap still decodes", func(t *testing.T) {
		fields := make(map[string]string, maxMCPHeaderEntries)
		for i := 0; i < maxMCPHeaderEntries; i++ {
			fields[fmt.Sprintf("X-Header-%d", i)] = "v"
		}
		document, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := decodeMCPHeaderEntries(document)
		if err != nil {
			t.Fatalf("a headers object with exactly maxMCPHeaderEntries members must not be refused: %v", err)
		}
		if len(entries) != maxMCPHeaderEntries {
			t.Fatalf("len(entries) = %d, want exactly maxMCPHeaderEntries (%d)", len(entries), maxMCPHeaderEntries)
		}
	})

	t.Run("one more than the cap refuses before decoding its value", func(t *testing.T) {
		var buf bytes.Buffer
		buf.WriteString(`{`)
		for i := 0; i < maxMCPHeaderEntries; i++ {
			if i > 0 {
				buf.WriteString(`,`)
			}
			fmt.Fprintf(&buf, `"X-Header-%d":"v"`, i)
		}
		// The overflowing member's own value is not a string at all (an
		// object) — if decodeMCPHeaderEntries ever tried to decode it,
		// the error would be "header values must be strings" rather than
		// the fixed cap refusal.
		buf.WriteString(`,"overflow-header":{"nested":true}}`)

		_, err := decodeMCPHeaderEntries(buf.Bytes())
		if err == nil {
			t.Fatal("want an error: a headers object exceeding maxMCPHeaderEntries members must be refused")
		}
		if err.Error() == "header values must be strings" {
			t.Fatal("the overflowing member's value must never be decoded at all")
		}
		if strings.Contains(err.Error(), "overflow-header") {
			t.Fatalf("err = %q, must not echo a header name", err.Error())
		}
	})
}

// decodeMCPToolEntries must cap the "tools" array at maxMCPTools *before*
// decoding (not merely before appending) the (maxMCPTools+1)-th element —
// buildImportTools's own len(rawTools) > maxMCPTools check runs only after
// every element already fully decoded, which would let an oversized (or
// deliberately malformed) inputSchema on tool maxMCPTools+1 be parsed for
// no possible benefit before that check ever ran.
func TestDecodeMCPToolEntriesCapsAtMaxMCPToolsBeforeDecodingOverCapElement(t *testing.T) {
	t.Run("exactly maxMCPTools elements still decode", func(t *testing.T) {
		tools := make([]map[string]any, maxMCPTools)
		for i := range tools {
			tools[i] = map[string]any{"name": fmt.Sprintf("tool-%d", i)}
		}
		document, err := json.Marshal(tools)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeMCPToolEntries(document)
		if err != nil {
			t.Fatalf("a tools array with exactly maxMCPTools elements must not be refused: %v", err)
		}
		if len(decoded) != maxMCPTools {
			t.Fatalf("len(decoded) = %d, want exactly maxMCPTools (%d)", len(decoded), maxMCPTools)
		}
	})

	t.Run("one more than maxMCPTools refuses before decoding its fields", func(t *testing.T) {
		var buf bytes.Buffer
		buf.WriteString(`[`)
		for i := 0; i < maxMCPTools; i++ {
			if i > 0 {
				buf.WriteString(`,`)
			}
			fmt.Fprintf(&buf, `{"name":"tool-%d"}`, i)
		}
		// The (maxMCPTools+1)-th element's inputSchema is malformed JSON.
		// If decodeMCPToolEntries ever tried to decode this element at
		// all, the error would surface as a raw JSON syntax error instead
		// of the fixed tool-count-exceeded reason.
		buf.WriteString(`,{"name":"overflow-tool","inputSchema":{not valid}}]`)

		_, err := decodeMCPToolEntries(buf.Bytes())
		if err == nil {
			t.Fatal("want an error: a tools array exceeding maxMCPTools elements must be refused")
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("%d", maxMCPTools)) {
			t.Fatalf("err = %q, want it to name the maxMCPTools limit", err.Error())
		}
		if strings.Contains(err.Error(), "overflow-tool") {
			t.Fatalf("err = %q, must not echo the overflowing tool's own name", err.Error())
		}
	})
}

// The full ImportJSON path must still refuse an over-maxMCPTools static
// snapshot with the same fixed, generic, limit-naming reason it always
// has (see static_snapshot_limits_test.go) — proving the earlier,
// decode-time cap in decodeMCPToolEntries did not change that
// user-visible outcome, only where in the pipeline it is enforced.
func TestImportJSONStaticSnapshotToolCountLimitStillRefusesTheSameWay(t *testing.T) {
	service, _ := newRegistryTestService(t)
	tools := make([]map[string]any, maxMCPTools+1)
	for i := range tools {
		tools[i] = map[string]any{"name": fmt.Sprintf("vendor.tool_%d", i)}
	}
	document, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"vendor": map[string]any{
				"url":   "https://vendor.example/mcp",
				"tools": tools,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := service.ImportJSON(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	reason, refused := report.Unsupported["vendor"]
	if !refused {
		t.Fatalf("Unsupported = %+v, want vendor refused", report.Unsupported)
	}
	if !strings.Contains(reason, fmt.Sprintf("%d", maxMCPTools)) {
		t.Fatalf("reason = %q, want it to name the maxMCPTools limit", reason)
	}
}
