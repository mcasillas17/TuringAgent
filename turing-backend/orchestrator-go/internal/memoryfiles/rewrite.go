package memoryfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

// RewriteFrontmatterRefsRequest edits the two frontmatter fields Turing owns on
// an existing note: the evidence refs and the stable identity.
//
// Refs is tri-state on purpose. A nil slice leaves the existing list untouched;
// a non-nil slice — including an empty one — replaces it.
//
// Withdrawn is the third thing the refs key can say, and it is separate from an
// empty list on purpose: `refs: []` reads as a note nobody ever grounded, while
// the withdrawal marker says the conversations behind it were deleted. It wins
// over Refs when both are set, because a withdrawal is not a list.
type RewriteFrontmatterRefsRequest struct {
	RelPath             string
	Refs                []string
	Withdrawn           bool
	NoteID              string
	ExpectedContentHash string
}

// RewrittenNote is the note after the splice.
type RewrittenNote struct {
	RelPath     string
	Content     string
	ContentHash string
	Changed     bool
}

// requireRewritableRelPath is this primitive's own gate: only a belief under
// beliefs/ or a candidate under inbox/. persona.md and profile.md are refused
// here even though other primitives may touch them, because an evidence rewrite
// has no business anywhere near the pinned documents.
func requireRewritableRelPath(input string) (string, error) {
	if clean, err := requireBeliefsRelPath(input); err == nil {
		return clean, nil
	}
	if clean, err := requireInboxRelPath(input); err == nil {
		return clean, nil
	}
	if _, _, err := normalizeVaultPath(input); err != nil {
		return "", err
	}
	return "", confinementError(input, "only notes under "+BeliefsDirName+"/ or "+InboxDirName+"/ may have their frontmatter rewritten")
}

// RewriteFrontmatterRefs replaces a note's evidence refs and identity by
// splicing bytes, never by decoding the frontmatter into a struct and encoding
// it back. Re-encoding would silently reorder the user's keys, drop their
// comments and renormalise their quoting on every withdrawal — a memory system
// that quietly rewrites the user's own file is not one they can trust.
func (v *Vault) RewriteFrontmatterRefs(ctx context.Context, request RewriteFrontmatterRefsRequest) (RewrittenNote, error) {
	if err := ctx.Err(); err != nil {
		return RewrittenNote{}, err
	}
	clean, err := requireRewritableRelPath(request.RelPath)
	if err != nil {
		return RewrittenNote{}, err
	}
	if request.Refs == nil && request.NoteID == "" && !request.Withdrawn {
		return RewrittenNote{}, errors.New("rewrite requested with neither refs nor a withdrawal nor an identity to set")
	}

	unlock, err := v.locks.lockContext(ctx, v.pathLockKey(clean))
	if err != nil {
		return RewrittenNote{}, err
	}
	defer unlock()

	parent, leaf, err := v.openParent(ctx, clean, false)
	if err != nil {
		return RewrittenNote{}, err
	}
	defer func() { _ = parent.Close() }()

	fd, err := unix.Openat(int(parent.Fd()), leaf, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return RewrittenNote{}, fmt.Errorf("open %q: %w", clean, err)
	}
	file := os.NewFile(uintptr(fd), clean)
	if file == nil {
		_ = unix.Close(fd)
		return RewrittenNote{}, fmt.Errorf("open %q: invalid descriptor", clean)
	}
	defer func() { _ = file.Close() }()

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return RewrittenNote{}, fmt.Errorf("inspect %q: %w", clean, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return RewrittenNote{}, confinementError(clean, "entry is not a regular file")
	}
	currentBytes, err := readBounded(ctx, file, MaxNoteBytes)
	if err != nil {
		return RewrittenNote{}, fmt.Errorf("read %q: %w", clean, err)
	}
	if len(currentBytes) > MaxNoteBytes {
		return RewrittenNote{}, &LimitError{What: fmt.Sprintf("note %q", clean), Limit: MaxNoteBytes, Got: len(currentBytes)}
	}
	current := string(currentBytes)
	if request.ExpectedContentHash != "" && ContentHash(current) != request.ExpectedContentHash {
		return RewrittenNote{}, &StaleContentError{RelPath: clean}
	}

	updated, err := spliceNoteFrontmatter(clean, current, request)
	if err != nil {
		return RewrittenNote{}, err
	}
	if updated == current {
		return RewrittenNote{RelPath: clean, Content: current, ContentHash: ContentHash(current)}, nil
	}
	if len(updated) > MaxNoteBytes {
		return RewrittenNote{}, &LimitError{What: fmt.Sprintf("note %q after the rewrite", clean), Limit: MaxNoteBytes, Got: len(updated)}
	}
	if err := verifyRewrittenNote(clean, current, updated, request); err != nil {
		return RewrittenNote{}, err
	}
	if err := ctx.Err(); err != nil {
		return RewrittenNote{}, err
	}
	// In place through the same descriptor, for the same reason profile.md is:
	// the user may have this note open in Obsidian right now.
	if _, err := file.WriteAt([]byte(updated), 0); err != nil {
		return RewrittenNote{}, fmt.Errorf("write %q: %w", clean, err)
	}
	if err := file.Truncate(int64(len(updated))); err != nil {
		return RewrittenNote{}, fmt.Errorf("truncate %q: %w", clean, err)
	}
	if err := v.syncFile(file); err != nil {
		return RewrittenNote{}, fmt.Errorf("sync %q: %w", clean, err)
	}
	if err := v.syncDirectory(parent); err != nil {
		return RewrittenNote{}, fmt.Errorf("sync the directory holding %q: %w", clean, err)
	}
	if err := ctx.Err(); err != nil {
		return RewrittenNote{}, err
	}
	return RewrittenNote{
		RelPath:     clean,
		Content:     updated,
		ContentHash: ContentHash(updated),
		Changed:     true,
	}, nil
}

// verifyRewrittenNote is the last thing between a splice and the user's file.
//
// Everything above it works on byte ranges: it locates one value, replaces it,
// and leaves the rest of the note alone. That is what keeps a user's key order,
// comments and quoting intact, and it is also why a splice can only ever be as
// correct as its idea of where the value ended and what may legally stand in
// its place. A frontmatter shape nobody anticipated — an anchor declared on the
// value another key aliases, a layout this package has not met — turns a
// faithful splice into a note that no longer reads back.
//
// So the spliced note is parsed whole, through the same lenient reader every
// other caller uses, and asked whether it says what the rewrite meant it to
// say. A note that does not is refused before a byte is written: the user keeps
// the file they had, and the refusal is per-note, which is what every caller of
// this primitive already expects.
//
// It is deliberately not a schema. Keys this package has never heard of are
// none of its business, and a vault the user annotates is full of them; the
// only things checked are the two fields the request asked for and the prose
// the splice was never supposed to touch.
func verifyRewrittenNote(relPath string, current string, updated string, request RewriteFrontmatterRefsRequest) error {
	parsed, err := ParseNote(relPath, updated)
	if err != nil {
		if _, before := ParseNote(relPath, current); before != nil {
			// The note did not read back before this rewrite either — a kind
			// this package does not recognise, say — so the splice is not what
			// broke it, and refusing here would make exactly those notes the
			// ones a withdrawal can never reach. The guard is about this
			// package's own splice, not about vetting the user's file.
			return nil
		}
		return noteParseError(relPath, "the rewritten frontmatter would not read back: %v", err)
	}
	if request.NoteID != "" && parsed.ID != request.NoteID {
		return noteParseError(relPath, "the rewritten frontmatter does not carry the identity it was given")
	}
	switch {
	case request.Withdrawn:
		if !parsed.Withdrawn {
			return noteParseError(relPath, "the rewritten frontmatter does not read as withdrawn")
		}
	case request.Refs != nil:
		if parsed.Withdrawn || !equalOrderedStrings(parsed.Refs, readableRefs(request.Refs)) {
			return noteParseError(relPath, "the rewritten frontmatter does not carry the citations it was given")
		}
	}
	before, err := splitFrontmatterAt(relPath, current)
	if err != nil {
		return err
	}
	if parsed.Body != before.Body {
		return noteParseError(relPath, "the rewrite would have changed the note's own text")
	}
	return nil
}

// readableRefs is the list a rewrite can expect to read back, given the one it
// was handed. The reader drops blank citations, so comparing against the raw
// request would refuse a write that was in fact faithful.
func readableRefs(refs []string) []string {
	readable := make([]string, 0, len(refs))
	for _, ref := range refs {
		if trimmed := strings.TrimSpace(ref); trimmed != "" {
			readable = append(readable, trimmed)
		}
	}
	return readable
}

func equalOrderedStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func spliceNoteFrontmatter(relPath string, content string, request RewriteFrontmatterRefsRequest) (string, error) {
	split, err := splitFrontmatterAt(relPath, content)
	if err != nil {
		return "", err
	}
	if !split.Present {
		// A note the user wrote by hand has no frontmatter to splice, so one is
		// prepended. Their prose is carried across untouched.
		newline := detectNewline(content)
		block, err := spliceFrontmatterKeys(relPath, "", newline, request)
		if err != nil {
			return "", err
		}
		return frontmatterFence + newline + block + frontmatterFence + newline + split.Body, nil
	}
	updated, err := spliceFrontmatterKeys(relPath, split.Raw, detectNewline(split.Raw), request)
	if err != nil {
		return "", err
	}
	if updated == split.Raw {
		return content, nil
	}
	// Spliced back into the note's own bytes, so both fences and the body keep
	// exactly the line endings the user's editor wrote.
	return content[:split.RawStart] + updated + content[split.RawStart+len(split.Raw):], nil
}

// detectNewline matches the line ending the file already uses, so a rewrite
// does not leave a vault synced from Windows with mixed endings.
func detectNewline(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func spliceFrontmatterKeys(relPath string, raw string, newline string, request RewriteFrontmatterRefsRequest) (string, error) {
	updated := raw
	var err error
	if request.NoteID != "" {
		updated, err = spliceFrontmatterValue(relPath, updated, FrontmatterKeyID, newline, func(style valueStyle) string {
			return style.renderScalar(yamlQuote(request.NoteID))
		})
		if err != nil {
			return "", err
		}
	}
	switch {
	case request.Withdrawn:
		// A withdrawal replaces the whole value, list or not: the note now says
		// its evidence is gone rather than listing what is left of it.
		updated, err = spliceFrontmatterValue(relPath, updated, FrontmatterKeyRefs, newline, func(style valueStyle) string {
			return style.renderScalar(yamlQuote(WithdrawnRefsMarker))
		})
		if err != nil {
			return "", err
		}
	case request.Refs != nil:
		updated, err = spliceFrontmatterValue(relPath, updated, FrontmatterKeyRefs, newline, func(style valueStyle) string {
			return style.renderSequence(request.Refs)
		})
		if err != nil {
			return "", err
		}
	}
	return updated, nil
}

// nestedIndentWidth is how far past its key a value is indented when the key's
// own column is all this package has to go on.
const nestedIndentWidth = 2

// valueStyle carries how the existing value was written, so the replacement
// lands in the same shape the user's file already uses.
type valueStyle struct {
	// inline is true when the value began on the same line as its key.
	inline bool
	// separator is the space needed before an inline value when the original
	// had none, as happens for a key with no value at all.
	separator string
	// indent is the column the first block item starts at, minus one.
	indent int
	// keyIndent is the column the mapping key starts at, minus one. It is what
	// decides where a non-inline value may legally begin; see scalarPad.
	keyIndent int
	// appended is true when the key did not exist and the rendering is being
	// added at the end of the frontmatter.
	appended bool
	// newline is the line ending the file already uses.
	newline string
	// trailing is an inline comment the user wrote after the value on the same
	// line. It sits inside the replaced range, so it is carried across
	// explicitly rather than being swallowed by the new value.
	trailing string
}

// scalarPad is the indentation a non-inline scalar or flow value has to add to
// what the replaced range already leaves in front of it.
//
// A block sequence is the one value YAML lets sit at its own key's column —
// `refs:` followed by `- "sess"` at the left margin is the specification's
// standard form and what most editors emit. Nothing else may: a scalar there is
// read as the *next key*, so writing the withdrawal marker at the sequence's
// column hands the user a note their own editor can no longer open. So a
// replacement that is not a block sequence is pushed past the key, and the
// value's own column is kept whenever the user had already put it there — which
// is every note this package writes itself.
func (s valueStyle) scalarPad() int {
	if s.indent > s.keyIndent {
		return 0
	}
	return s.keyIndent + nestedIndentWidth - s.indent
}

func (s valueStyle) renderScalar(value string) string {
	switch {
	case s.appended:
		return value + s.newline
	case s.inline:
		return s.separator + value + s.trailing + s.newline
	default:
		return strings.Repeat(" ", s.scalarPad()) + value + s.newline
	}
}

func (s valueStyle) renderSequence(items []string) string {
	if s.appended {
		if len(items) == 0 {
			return "[]" + s.newline
		}
		return s.newline + strings.Repeat(" ", nestedIndentWidth) + renderBlockSequence(items, nestedIndentWidth, s.newline)
	}
	if s.inline {
		return s.separator + renderFlowSequence(items) + s.trailing + s.newline
	}
	if len(items) == 0 {
		// `[]` is a flow value rather than a block sequence, so it is indented
		// like a scalar for exactly the reason scalarPad gives.
		return strings.Repeat(" ", s.scalarPad()) + "[]" + s.newline
	}
	return renderBlockSequence(items, s.indent, s.newline)
}

func renderBlockSequence(items []string, indent int, newline string) string {
	if len(items) == 0 {
		return "[]" + newline
	}
	prefix := strings.Repeat(" ", indent)
	var builder strings.Builder
	for index, item := range items {
		if index > 0 {
			builder.WriteString(prefix)
		}
		builder.WriteString("- " + yamlQuote(item) + newline)
	}
	return builder.String()
}

func renderFlowSequence(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, yamlQuote(item))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// spliceFrontmatterValue replaces the byte range one key's value occupies and
// leaves every other byte of the frontmatter exactly as the user wrote it.
func spliceFrontmatterValue(relPath string, raw string, key string, newline string, render func(valueStyle) string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return raw + key + ": " + render(valueStyle{appended: true, newline: newline}), nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &document); err != nil {
		return "", noteParseError(relPath, "frontmatter is not valid YAML: %v", err)
	}
	mapping, err := frontmatterMapping(relPath, &document)
	if err != nil {
		return "", err
	}
	if err := refuseFlowMapping(relPath, key, mapping); err != nil {
		return "", err
	}
	index, err := singleKeyIndex(relPath, mapping, key)
	if err != nil {
		return "", err
	}
	lineStarts := lineStartOffsets(raw)
	if index < 0 {
		appended := raw
		if !strings.HasSuffix(appended, "\n") {
			appended += newline
		}
		return appended + key + ": " + render(valueStyle{appended: true, newline: newline}), nil
	}
	keyNode := mapping.Content[index]
	valueNode := mapping.Content[index+1]
	start, err := byteOffset(lineStarts, len(raw), valueNode.Line, valueNode.Column)
	if err != nil {
		return "", noteParseError(relPath, "frontmatter key %q could not be located: %v", key, err)
	}
	end := len(raw)
	if index+2 < len(mapping.Content) {
		nextKey := mapping.Content[index+2]
		if end, err = byteOffset(lineStarts, len(raw), nextKey.Line, 1); err != nil {
			return "", noteParseError(relPath, "frontmatter key after %q could not be located: %v", key, err)
		}
	}
	end = trimTrailingCommentaryLines(raw, start, end)
	if start > end {
		return "", noteParseError(relPath, "frontmatter key %q spans an unreadable range", key)
	}
	style := valueStyle{
		inline:    keyNode.Line == valueNode.Line,
		indent:    valueNode.Column - 1,
		keyIndent: keyNode.Column - 1,
		newline:   newline,
	}
	if style.inline && (start == 0 || raw[start-1] != ' ') {
		style.separator = " "
	}
	if style.inline {
		style.trailing = inlineTrailingComment(raw[start:end])
	}
	return raw[:start] + render(style) + raw[end:], nil
}

// inlineTrailingComment returns the whitespace-plus-comment the user wrote after
// an inline value on the same line, so replacing the value does not delete the
// note they left beside it.
func inlineTrailingComment(segment string) string {
	if lineEnd := strings.IndexByte(segment, '\n'); lineEnd >= 0 {
		segment = segment[:lineEnd]
	}
	offset := commentOffset(segment)
	if offset < 0 {
		return ""
	}
	for offset > 0 && (segment[offset-1] == ' ' || segment[offset-1] == '\t') {
		offset--
	}
	return strings.TrimRight(segment[offset:], "\r")
}

// commentOffset finds a YAML comment marker outside quotes and flow collections,
// so a '#' inside a value such as ["a#1"] is not mistaken for one.
func commentOffset(segment string) int {
	singleQuoted := false
	doubleQuoted := false
	depth := 0
	for index := 0; index < len(segment); index++ {
		symbol := segment[index]
		switch {
		case singleQuoted:
			if symbol == '\'' {
				singleQuoted = false
			}
		case doubleQuoted:
			if symbol == '\\' {
				index++
				continue
			}
			if symbol == '"' {
				doubleQuoted = false
			}
		case symbol == '\'':
			singleQuoted = true
		case symbol == '"':
			doubleQuoted = true
		case symbol == '[' || symbol == '{':
			depth++
		case symbol == ']' || symbol == '}':
			if depth > 0 {
				depth--
			}
		case symbol == '#' && depth == 0 && index > 0 && (segment[index-1] == ' ' || segment[index-1] == '\t'):
			return index
		}
	}
	return -1
}

// refuseFlowMapping is the one frontmatter shape a byte splice cannot edit.
//
// Every replacement here is bounded by the start of the next key or by the end
// of the block, and inside `{a: 1, b: 2}` neither bound is a boundary: the last
// value's range swallows the closing brace, an earlier value's range ends where
// the next key begins and the replacement drops a newline into the middle of the
// mapping, and a key that is absent is appended after the closing brace where it
// is not part of the mapping at all. All three hand the user back a note their
// own editor can no longer read.
//
// The alternative is re-encoding the mapping, which is exactly what this
// primitive exists not to do: it would reorder the user's keys, drop their
// comments and renormalise their quoting. So the write is refused before it
// happens, by name, with the one edit that makes the note editable again.
func refuseFlowMapping(relPath string, key string, mapping *yaml.Node) error {
	if mapping.Style&yaml.FlowStyle == 0 {
		return nil
	}
	return noteParseError(
		relPath,
		"frontmatter is written as a YAML flow mapping ({...}), where every key shares one bracketed range; %q is edited by splicing that one value's bytes, which cannot be done inside braces without re-encoding the whole mapping and rewriting the rest of the frontmatter with it. Write the frontmatter one key per line and this edit will apply",
		key,
	)
}

// singleKeyIndex refuses a frontmatter that defines the same key twice: which
// one Turing should edit is genuinely ambiguous, and guessing would drop the
// other silently.
func singleKeyIndex(relPath string, mapping *yaml.Node, key string) (int, error) {
	found := -1
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		node := mapping.Content[index]
		if node.Kind != yaml.ScalarNode || node.Value != key {
			continue
		}
		if found >= 0 {
			return -1, noteParseError(relPath, "frontmatter defines %q more than once", key)
		}
		found = index
	}
	return found, nil
}

func lineStartOffsets(raw string) []int {
	starts := []int{0}
	for index := 0; index < len(raw); index++ {
		if raw[index] == '\n' {
			starts = append(starts, index+1)
		}
	}
	return starts
}

func byteOffset(lineStarts []int, length int, line int, column int) (int, error) {
	if line < 1 || line > len(lineStarts) || column < 1 {
		return 0, fmt.Errorf("position %d:%d is outside the frontmatter", line, column)
	}
	offset := lineStarts[line-1] + column - 1
	if offset > length {
		return 0, fmt.Errorf("position %d:%d is past the end of the frontmatter", line, column)
	}
	return offset, nil
}

// trimTrailingCommentaryLines pulls the replacement range back off any blank or
// comment-only lines that sit between this value and the next key, so a comment
// the user wrote about the following field survives the splice.
func trimTrailingCommentaryLines(raw string, start int, end int) int {
	for end > start {
		lineStart := strings.LastIndexByte(raw[:end-1], '\n') + 1
		if lineStart <= start {
			return end
		}
		line := strings.TrimSpace(raw[lineStart:end])
		if line != "" && !strings.HasPrefix(line, "#") {
			return end
		}
		end = lineStart
	}
	return end
}
