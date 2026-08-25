package memoryfiles

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Frontmatter key names. They are plain words because the vault is meant to be
// opened and edited by a human in Obsidian, not decoded by one.
const (
	FrontmatterKeyID        = "id"
	FrontmatterKeyKind      = "kind"
	FrontmatterKeyTitle     = "title"
	FrontmatterKeyCreatedAt = "created_at"
	FrontmatterKeyManaged   = "managed"
	FrontmatterKeyRefs      = "refs"
)

const frontmatterFence = "---"

// noteFrontmatter is the frontmatter Turing writes. It is deliberately not the
// type used to read a note back: reading is lenient and byte-preserving, and
// round-tripping through this struct would discard everything a user added.
type noteFrontmatter struct {
	ID        string
	Kind      NoteKind
	Title     string
	CreatedAt time.Time
	Managed   bool
	Refs      []string
}

func renderNote(front noteFrontmatter, body string) string {
	var builder strings.Builder
	builder.WriteString(frontmatterFence + "\n")
	builder.WriteString(FrontmatterKeyID + ": " + yamlQuote(front.ID) + "\n")
	builder.WriteString(FrontmatterKeyKind + ": " + yamlQuote(string(front.Kind)) + "\n")
	builder.WriteString(FrontmatterKeyTitle + ": " + yamlQuote(front.Title) + "\n")
	builder.WriteString(FrontmatterKeyCreatedAt + ": " + yamlQuote(front.CreatedAt.UTC().Format(time.RFC3339)) + "\n")
	fmt.Fprintf(&builder, "%s: %t\n", FrontmatterKeyManaged, front.Managed)
	if len(front.Refs) == 0 {
		builder.WriteString(FrontmatterKeyRefs + ": []\n")
	} else {
		builder.WriteString(FrontmatterKeyRefs + ":\n  " + renderBlockSequence(front.Refs, 2, "\n"))
	}
	builder.WriteString(frontmatterFence + "\n")
	if body != "" {
		builder.WriteString("\n")
		builder.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

// yamlQuote emits a double-quoted YAML scalar. Everything Turing writes is
// quoted so a title that happens to look like YAML stays a title.
func yamlQuote(value string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, symbol := range value {
		switch symbol {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		default:
			if symbol < 0x20 || symbol == 0x7f {
				fmt.Fprintf(&builder, `\x%02x`, symbol)
				continue
			}
			builder.WriteRune(symbol)
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

// ErrNoteParse marks a note whose frontmatter could not be read. It is always
// per-note: one broken file makes that file unavailable and leaves the rest of
// the vault readable.
var ErrNoteParse = errors.New("note frontmatter could not be parsed")

// NoteParseError names the note and the reason, so the client can show the user
// which file in their own vault needs their attention.
type NoteParseError struct {
	RelPath string
	Reason  string
}

func (e *NoteParseError) Error() string {
	return fmt.Sprintf("%s: %s", e.RelPath, e.Reason)
}

func (e *NoteParseError) Unwrap() error { return ErrNoteParse }

func noteParseError(relPath string, format string, args ...any) error {
	return &NoteParseError{RelPath: relPath, Reason: fmt.Sprintf(format, args...)}
}

// ParsedNote is a note read leniently. RawFrontmatter and Body are the file's
// own bytes: nothing this package writes back is produced by re-encoding them,
// so a user's key order, comments and quoting survive a Turing edit.
type ParsedNote struct {
	ID             string
	Kind           NoteKind
	Title          string
	Managed        bool
	Refs           []string
	HasFrontmatter bool
	RawFrontmatter string
	Body           string
}

// ParseNote reads a note's frontmatter without insisting the note is Turing's.
// A file with no frontmatter is a perfectly good note the user wrote by hand:
// it parses, carries no identity, and is unmanaged until reconcile assigns one.
// Only a frontmatter block that opens and then cannot be read is an error.
func ParseNote(relPath string, content string) (ParsedNote, error) {
	rawFrontmatter, body, hasFrontmatter, err := splitFrontmatter(relPath, content)
	if err != nil {
		return ParsedNote{}, err
	}
	parsed := ParsedNote{
		HasFrontmatter: hasFrontmatter,
		RawFrontmatter: rawFrontmatter,
		Body:           body,
	}
	if hasFrontmatter && strings.TrimSpace(rawFrontmatter) != "" {
		var document yaml.Node
		if err := yaml.Unmarshal([]byte(rawFrontmatter), &document); err != nil {
			return ParsedNote{}, noteParseError(relPath, "frontmatter is not valid YAML: %v", err)
		}
		if document.Kind != 0 {
			mapping, err := frontmatterMapping(relPath, &document)
			if err != nil {
				return ParsedNote{}, err
			}
			readFrontmatterFields(&parsed, mapping)
		}
	}
	if parsed.Title == "" {
		parsed.Title = headingTitle(parsed.Body)
	}
	return parsed, nil
}

func frontmatterMapping(relPath string, document *yaml.Node) (*yaml.Node, error) {
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil, noteParseError(relPath, "frontmatter must be a single YAML document")
	}
	mapping := document.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return nil, noteParseError(relPath, "frontmatter must be a mapping of keys to values")
	}
	return mapping, nil
}

// readFrontmatterFields takes only what Turing needs and ignores everything
// else. An unrecognised value is dropped rather than promoted to an error: the
// user's own keys are none of this parser's business.
func readFrontmatterFields(parsed *ParsedNote, mapping *yaml.Node) {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		key := mapping.Content[index]
		value := mapping.Content[index+1]
		if key.Kind != yaml.ScalarNode {
			continue
		}
		switch key.Value {
		case FrontmatterKeyID:
			if value.Kind == yaml.ScalarNode {
				parsed.ID = strings.TrimSpace(value.Value)
			}
		case FrontmatterKeyKind:
			if value.Kind == yaml.ScalarNode {
				if kind := NoteKind(strings.TrimSpace(value.Value)); kind.Valid() {
					parsed.Kind = kind
				}
			}
		case FrontmatterKeyTitle:
			if value.Kind == yaml.ScalarNode {
				parsed.Title = strings.TrimSpace(value.Value)
			}
		case FrontmatterKeyManaged:
			if value.Kind == yaml.ScalarNode {
				parsed.Managed = strings.EqualFold(value.Value, "true")
			}
		case FrontmatterKeyRefs:
			parsed.Refs = scalarSequence(value)
		}
	}
}

func scalarSequence(value *yaml.Node) []string {
	if value.Kind != yaml.SequenceNode {
		return nil
	}
	items := make([]string, 0, len(value.Content))
	for _, item := range value.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
			continue
		}
		if trimmed := strings.TrimSpace(item.Value); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

// frontmatterSplit locates the frontmatter block inside a note's own bytes.
// RawStart is kept so a writer can splice the block back into the original
// content instead of rebuilding it, which would normalise the fences and quietly
// rewrite the line endings of a vault synced from Windows.
type frontmatterSplit struct {
	Raw      string
	Body     string
	Present  bool
	RawStart int
}

// splitFrontmatter returns the bytes between the fences, the bytes after the
// closing fence, and whether a fence was present at all.
func splitFrontmatter(relPath string, content string) (string, string, bool, error) {
	split, err := splitFrontmatterAt(relPath, content)
	if err != nil {
		return "", "", false, err
	}
	return split.Raw, split.Body, split.Present, nil
}

func splitFrontmatterAt(relPath string, content string) (frontmatterSplit, error) {
	rest, ok := trimFenceLine(content)
	if !ok {
		return frontmatterSplit{Body: content}, nil
	}
	rawStart := len(content) - len(rest)
	for cursor := 0; cursor <= len(rest); {
		lineEnd := strings.IndexByte(rest[cursor:], '\n')
		var line string
		var next int
		if lineEnd < 0 {
			line = rest[cursor:]
			next = len(rest)
		} else {
			line = rest[cursor : cursor+lineEnd]
			next = cursor + lineEnd + 1
		}
		if strings.TrimRight(line, "\r") == frontmatterFence {
			return frontmatterSplit{
				Raw:      rest[:cursor],
				Body:     rest[next:],
				Present:  true,
				RawStart: rawStart,
			}, nil
		}
		if next == cursor {
			break
		}
		cursor = next
	}
	return frontmatterSplit{}, noteParseError(relPath, "frontmatter opens with %q but is never closed", frontmatterFence)
}

func trimFenceLine(content string) (string, bool) {
	switch {
	case strings.HasPrefix(content, frontmatterFence+"\n"):
		return content[len(frontmatterFence)+1:], true
	case strings.HasPrefix(content, frontmatterFence+"\r\n"):
		return content[len(frontmatterFence)+2:], true
	default:
		return content, false
	}
}

// headingTitle falls back to the note's first Markdown heading, which is what a
// user reading their own vault would call the note.
func headingTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
		if trimmed != "" {
			return ""
		}
	}
	return ""
}
