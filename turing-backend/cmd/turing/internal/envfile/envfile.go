// Package envfile reads, edits and writes the backend's .env the way Compose
// reads it: a bare, single-quoted or double-quoted value per line, the last
// assignment of a key winning, and everything it does not understand kept
// byte for byte.
package envfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unicode"
)

// File is a parsed .env. It keeps every original line, so whatever an edit
// does not touch is written back unchanged.
type File struct {
	entries []entry
}

// entry is one line, or several when a quoted value spans lines, as Compose
// allows.
type entry struct {
	text   string // the bytes of the entry without its final line ending
	eol    string // "\n", "\r\n", or "" for a last line with no ending
	prefix string // what precedes the key: indentation and `export `
	key    string // empty unless the entry is an assignment
	value  string // the value Compose reads, before any interpolation
	quote  byte   // the value's opening quote, or 0 for a bare value
}

// Parse reads an .env. Unknown content is kept verbatim and is never an
// error; only a NUL byte is refused, since no reader treats it as text.
func Parse(data []byte) (*File, error) {
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, errors.New("the file contains a NUL byte")
	}
	s := string(data)
	file := &File{}
	for pos := 0; pos < len(s); {
		e, next := parseEntry(s, pos)
		file.entries = append(file.entries, e)
		pos = next
	}
	return file, nil
}

func parseEntry(s string, pos int) (entry, int) {
	end, eol := lineEnd(s, pos)
	e := entry{text: s[pos:end], eol: eol}
	next := end + len(eol)

	prefixLen, key, afterEquals, ok := splitAssignment(e.text)
	if !ok {
		return e, next
	}
	e.prefix, e.key = e.text[:prefixLen], key

	rest := e.text[afterEquals:]
	trimmed := strings.TrimLeft(rest, " \t")
	if trimmed == "" || (trimmed[0] != '\'' && trimmed[0] != '"') {
		e.value = decodeBare(trimmed)
		return e, next
	}

	open := pos + afterEquals + len(rest) - len(trimmed)
	e.quote = s[open]
	closing := closingQuote(s, open)
	if closing < 0 {
		// Compose would refuse this line; the rest of it is the best reading.
		e.value = trimmed[1:]
		return e, next
	}
	inner := s[open+1 : closing]
	if s[open] == '\'' {
		e.value = strings.ReplaceAll(inner, `\'`, "'")
	} else {
		e.value = decodeDoubleQuoted(inner)
	}
	// Anything after the closing quote is a comment Compose ignores.
	end, eol = lineEnd(s, closing+1)
	e.text, e.eol = s[pos:end], eol
	return e, end + len(eol)
}

// lineEnd returns where the line containing from ends, and its line ending.
func lineEnd(s string, from int) (int, string) {
	newline := strings.IndexByte(s[from:], '\n')
	if newline < 0 {
		return len(s), ""
	}
	end := from + newline
	if end > from && s[end-1] == '\r' {
		return end - 1, "\r\n"
	}
	return end, "\n"
}

// splitAssignment recognises `[export ]KEY=` with optional blanks around the
// key, and reports where the key starts and where the value text begins.
func splitAssignment(text string) (keyStart int, key string, afterEquals int, ok bool) {
	i := skipBlanks(text, 0)
	if strings.HasPrefix(text[i:], "export") && i+len("export") < len(text) && isBlank(text[i+len("export")]) {
		i = skipBlanks(text, i+len("export"))
	}
	start := i
	for i < len(text) && isKeyByte(text[i], i == start) {
		i++
	}
	if i == start {
		return 0, "", 0, false
	}
	equals := skipBlanks(text, i)
	if equals >= len(text) || text[equals] != '=' {
		return 0, "", 0, false
	}
	return start, text[start:i], equals + 1, true
}

func skipBlanks(s string, i int) int {
	for i < len(s) && isBlank(s[i]) {
		i++
	}
	return i
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' }

func isKeyByte(c byte, first bool) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_':
		return true
	case c >= '0' && c <= '9', c == '.', c == '-':
		return !first
	}
	return false
}

func validKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		if !isKeyByte(key[i], i == 0) {
			return false
		}
	}
	return true
}

// closingQuote finds the quote that ends the value opened at s[open], skipping
// an escaped character. It returns -1 when the value is never closed.
func closingQuote(s string, open int) int {
	quote := s[open]
	for i := open + 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case quote:
			return i
		}
	}
	return -1
}

// decodeBare reads an unquoted value: it ends at the first blank followed by
// `#`, and surrounding blanks are not part of it. Nothing is interpolated.
func decodeBare(trimmed string) string {
	for i := 0; i+1 < len(trimmed); i++ {
		if isBlank(trimmed[i]) && trimmed[i+1] == '#' {
			trimmed = trimmed[:i]
			break
		}
	}
	return strings.TrimRight(trimmed, " \t\r")
}

// decodeDoubleQuoted decodes the four escapes Compose documents and leaves any
// other backslash alone. Interpolation is deliberately not evaluated.
func decodeDoubleQuoted(inner string) string {
	var out strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			decoded := byte(0)
			switch inner[i+1] {
			case '\\', '"':
				decoded = inner[i+1]
			case 'n':
				decoded = '\n'
			case 't':
				decoded = '\t'
			}
			if decoded != 0 {
				out.WriteByte(decoded)
				i++
				continue
			}
		}
		out.WriteByte(inner[i])
	}
	return out.String()
}

// Get returns the value Compose reads for key: the one on its last assignment.
func (f *File) Get(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	for i := len(f.entries) - 1; i >= 0; i-- {
		if f.entries[i].key == key {
			return f.entries[i].value, true
		}
	}
	return "", false
}

// Interpolates reports whether Compose expands variables in key's last
// assignment: it is bare or double-quoted, and its value holds a $. Get
// returns such a value as written, not as Compose expands it.
func (f *File) Interpolates(key string) bool {
	if key == "" {
		return false
	}
	for i := len(f.entries) - 1; i >= 0; i-- {
		if e := f.entries[i]; e.key == key {
			return e.quote != '\'' && strings.Contains(e.value, "$")
		}
	}
	return false
}

// Set writes value for key. It replaces the last assignment, which is the one
// Compose reads, drops any earlier ones, and appends the key when absent. A
// value that cannot be written is refused before anything changes.
func (f *File) Set(key, value string) error {
	if !validKey(key) {
		return fmt.Errorf("%q is not a name .env can hold", key)
	}
	encoded, err := Encode(value)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}

	last := -1
	for i := range f.entries {
		if f.entries[i].key == key {
			last = i
		}
	}
	if last < 0 {
		ending := f.lineEnding()
		if n := len(f.entries); n > 0 && f.entries[n-1].eol == "" {
			f.entries[n-1].eol = ending
		}
		f.entries = append(f.entries, entry{
			text: key + "=" + encoded, eol: ending, key: key, value: value, quote: encodedQuote(encoded),
		})
		return nil
	}

	replaced := &f.entries[last]
	replaced.text = replaced.prefix + key + "=" + encoded
	replaced.value, replaced.quote = value, encodedQuote(encoded)
	f.removeKeyExcept(key, last)
	return nil
}

// encodedQuote is the opening quote of what Encode returned: ' or none.
func encodedQuote(encoded string) byte {
	if strings.HasPrefix(encoded, "'") {
		return '\''
	}
	return 0
}

// Delete removes every assignment of key.
func (f *File) Delete(key string) {
	if key != "" {
		f.removeKeyExcept(key, -1)
	}
}

func (f *File) removeKeyExcept(key string, keep int) {
	kept := f.entries[:0]
	for i, e := range f.entries {
		if e.key == key && i != keep {
			continue
		}
		kept = append(kept, e)
	}
	clear(f.entries[len(kept):])
	f.entries = kept
}

// lineEnding is the file's own line ending, so an appended line matches it.
func (f *File) lineEnding() string {
	for _, e := range f.entries {
		if e.eol != "" {
			return e.eol
		}
	}
	return "\n"
}

// Keys lists each assigned key once, in the order of its first appearance.
func (f *File) Keys() []string {
	var keys []string
	seen := make(map[string]bool)
	for _, e := range f.entries {
		if e.key != "" && !seen[e.key] {
			seen[e.key] = true
			keys = append(keys, e.key)
		}
	}
	return keys
}

// Bytes is the file as it would be written.
func (f *File) Bytes() []byte {
	var out bytes.Buffer
	for _, e := range f.entries {
		out.WriteString(e.text)
		out.WriteString(e.eol)
	}
	return out.Bytes()
}

// Encode returns value as a .env right-hand side that Compose reads back byte
// for byte: bare when every character is plain, otherwise a single-quoted
// literal where nothing is interpolated and the only escape is \' for an
// apostrophe. It matches init.sh's compose_literal. A value that form cannot
// carry is refused, and the error never contains the value, which may be a
// secret.
func Encode(value string) (string, error) {
	if strings.HasSuffix(value, `\`) {
		return "", errors.New("a value ending in a backslash cannot be written to .env")
	}
	if strings.Contains(value, `\'`) {
		return "", errors.New("a value containing a backslash before an apostrophe cannot be written to .env")
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", errors.New("a value containing a control character cannot be written to .env")
	}
	if isBare(value) {
		return value, nil
	}
	return "'" + strings.ReplaceAll(value, "'", `\'`) + "'", nil
}

// isBare reports whether value matches ^[A-Za-z0-9._:/@+-]*$.
func isBare(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case strings.IndexByte("._:/@+-", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// ReadRegular reads path, refusing a symlink or anything that is not a regular
// file, so a link planted at .env cannot redirect a read.
func ReadRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := requireRegular(path, info); err != nil {
		return nil, err
	}
	// O_NOFOLLOW and O_NONBLOCK keep a swap after the Lstat from following a
	// link or blocking on a pipe.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return io.ReadAll(file)
}

func requireRegular(path string, info os.FileInfo) error {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%s is a symlink, not a regular file", path)
	case !info.Mode().IsRegular():
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}
