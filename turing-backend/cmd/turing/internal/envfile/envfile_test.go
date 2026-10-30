package envfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustParse(t *testing.T, content string) *File {
	t.Helper()
	file, err := Parse([]byte(content))
	if err != nil {
		t.Fatalf("Parse(%q): %v", content, err)
	}
	return file
}

func TestParseKeepsEveryByteOfWhatItDoesNotUnderstand(t *testing.T) {
	for name, content := range map[string]string{
		"empty":                   "",
		"only a newline":          "\n",
		"no trailing newline":     "A=1\nB=2",
		"comments and blanks":     "# header\n\nA=1\n   \n# A=2\n",
		"crlf":                    "A=1\r\n# c\r\nB=2\r\n",
		"mixed endings":           "A=1\r\nB=2\nC=3",
		"lone carriage return":    "A=1\rB=2\n",
		"garbage lines":           "!!!\n=oops\nBARE\n1BAD=x\nA B=c\n",
		"invalid utf-8":           "A=\xff\xfe\n",
		"multi-line double quote": "A=\"one\ntwo\r\nthree\"\nB=2\n",
		"multi-line single quote": "A='one\ntwo'\nB=2\n",
		"unterminated quote":      "A='never closed\nB=2\n",
		"export and spacing":      "  export\tA = 1  # note\nB =2\n",
		"trailing text":           "A='x' trailing\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := string(mustParse(t, content).Bytes()); got != content {
				t.Fatalf("Bytes() = %q, want %q", got, content)
			}
		})
	}
}

func TestParseRefusesOnlyANULByte(t *testing.T) {
	if _, err := Parse([]byte("A=1\x00\n")); err == nil {
		t.Fatal("Parse accepted a NUL byte")
	}
	for _, content := range []string{"A=\xff\n", "\r\r\r", "===", "'", "\"\n\""} {
		if _, err := Parse([]byte(content)); err != nil {
			t.Fatalf("Parse(%q) = %v, want it to keep unknown content", content, err)
		}
	}
}

func TestGetReadsTheValueComposeWouldRead(t *testing.T) {
	tests := []struct {
		name    string
		content string
		key     string
		want    string
		wantOK  bool
	}{
		{name: "bare", content: "K=abc\n", key: "K", want: "abc", wantOK: true},
		{name: "bare with inner space", content: "K=a b\n", key: "K", want: "a b", wantOK: true},
		{name: "bare with url", content: "K=http://h:8/v1\n", key: "K", want: "http://h:8/v1", wantOK: true},
		{name: "inline comment after space", content: "K=abc # comment\n", key: "K", want: "abc", wantOK: true},
		{name: "a tab then a hash is not a comment", content: "K=abc\t# not a comment\n", key: "K", want: "abc\t# not a comment", wantOK: true},
		{name: "a space after a tab starts the comment", content: "K=abc\t #x\n", key: "K", want: "abc", wantOK: true},
		{name: "stops at the first comment marker", content: "K=a #b #c\n", key: "K", want: "a", wantOK: true},
		{name: "hash without a space is part of the value", content: "K=a#b\n", key: "K", want: "a#b", wantOK: true},
		{name: "leading hash is part of the value", content: "K=#x\n", key: "K", want: "#x", wantOK: true},
		{name: "space then hash is read after trimming", content: "K= #x\n", key: "K", want: "#x", wantOK: true},
		{name: "surrounding spaces are trimmed", content: "K=   spaced  \n", key: "K", want: "spaced", wantOK: true},
		{name: "spaces around the equals sign", content: "K = v\n", key: "K", want: "v", wantOK: true},
		{name: "single quoted escaped apostrophe", content: `K='it\'s'` + "\n", key: "K", want: "it's", wantOK: true},
		{name: "single quoted is literal", content: `K='a $B ${C} #d \n \\ "e"'` + "\n", key: "K", want: `a $B ${C} #d \n \\ "e"`, wantOK: true},
		{name: "single quoted keeps its spaces", content: "K='  x  '\n", key: "K", want: "  x  ", wantOK: true},
		{name: "single quoted with trailing comment", content: "K='x' # tail\n", key: "K", want: "x", wantOK: true},
		{name: "single quoted empty", content: "K=''\n", key: "K", want: "", wantOK: true},
		{name: "double quoted escaped quote", content: `K="a\"b"` + "\n", key: "K", want: `a"b`, wantOK: true},
		{name: "double quoted escapes", content: `K="a\\b\nc\td"` + "\n", key: "K", want: "a\\b\nc\td", wantOK: true},
		{name: "double quoted ending in an escaped backslash", content: `K="a\\" # c` + "\n", key: "K", want: `a\`, wantOK: true},
		{name: "double quoted is not interpolated", content: `K="$X ${Y}"` + "\n", key: "K", want: "$X ${Y}", wantOK: true},
		{name: "double quoted keeps an unknown escape", content: `K="a\qb"` + "\n", key: "K", want: `a\qb`, wantOK: true},
		{name: "double quoted keeps a hash", content: `K="a #b"` + "\n", key: "K", want: "a #b", wantOK: true},
		{name: "double quoted spanning lines", content: "K=\"one\ntwo\"\nL=3\n", key: "K", want: "one\ntwo", wantOK: true},
		{name: "a key after a multi-line value", content: "K=\"one\ntwo\"\nL=3\n", key: "L", want: "3", wantOK: true},
		{name: "a line inside a multi-line value is not a key", content: "K=\"one\nL=3\"\n", key: "L", wantOK: false},
		{name: "unterminated quote reads the rest of the line", content: "K='open\n", key: "K", want: "open", wantOK: true},
		{name: "export prefix", content: "export K=v\n", key: "K", want: "v", wantOK: true},
		{name: "export prefix with tab and indent", content: "  export\tK=v\n", key: "K", want: "v", wantOK: true},
		{name: "a key named export", content: "export=v\n", key: "export", want: "v", wantOK: true},
		{name: "duplicates: last wins", content: "K=1\nK=2\nK=3\n", key: "K", want: "3", wantOK: true},
		{name: "duplicates: last wins over a different quoting", content: "K=1\nK='2'\n", key: "K", want: "2", wantOK: true},
		{name: "empty value", content: "K=\n", key: "K", want: "", wantOK: true},
		{name: "empty value without a newline", content: "K=", key: "K", want: "", wantOK: true},
		{name: "empty last value wins", content: "K=1\nK=\n", key: "K", want: "", wantOK: true},
		{name: "absent", content: "A=1\n", key: "K", wantOK: false},
		{name: "empty file", content: "", key: "K", wantOK: false},
		{name: "a longer key is not the key", content: "KK=1\nK_=2\n", key: "K", wantOK: false},
		{name: "a commented key is not a key", content: "# K=1\n", key: "K", wantOK: false},
		{name: "crlf bare", content: "K=v\r\n", key: "K", want: "v", wantOK: true},
		{name: "crlf single quoted", content: "K='a b'\r\nL=2\r\n", key: "K", want: "a b", wantOK: true},
		{name: "crlf empty", content: "K=\r\n", key: "K", want: "", wantOK: true},
		{name: "crlf with a comment", content: "K=v # c\r\n", key: "K", want: "v", wantOK: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := mustParse(t, test.content).Get(test.key)
			if ok != test.wantOK || got != test.want {
				t.Fatalf("Get(%q) = (%q, %v), want (%q, %v)", test.key, got, ok, test.want, test.wantOK)
			}
		})
	}
}

func TestInterpolatesIsTrueOnlyWhereComposeExpandsTheLastAssignment(t *testing.T) {
	tests := []struct {
		name, content, key string
		want               bool
	}{
		{"bare with a brace", "K=${X}\n", "K", true},
		{"bare with a dollar", "K=$X\n", "K", true},
		{"bare with an escaped dollar, which Compose also rewrites", "K=a$$b\n", "K", true},
		{"double quoted", `K="$X"` + "\n", "K", true},
		{"double quoted with a default", `K="${X:-d}"` + "\n", "K", true},
		{"export prefix", "export K=$X\n", "K", true},
		{"single quoted", "K='${X}'\n", "K", false},
		{"bare without a dollar", "K=abc\n", "K", false},
		{"double quoted without a dollar", `K="abc"` + "\n", "K", false},
		{"a dollar only in a comment", "K=abc # $X\n", "K", false},
		{"a dollar only after the closing quote", "K='abc' $X\n", "K", false},
		{"the last assignment decides: single quoted", "K=$X\nK='$X'\n", "K", false},
		{"the last assignment decides: bare", "K='$X'\nK=$X\n", "K", true},
		{"absent", "A=$X\n", "K", false},
		{"empty key", "K=$X\n", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mustParse(t, tt.content).Interpolates(tt.key); got != tt.want {
				t.Fatalf("Interpolates(%q) in %q = %v, want %v", tt.key, tt.content, got, tt.want)
			}
		})
	}
}

func TestInterpolatesIsFalseForAValueSetWrote(t *testing.T) {
	file := mustParse(t, "K=$OLD\n")
	if err := file.Set("K", "${X}"); err != nil {
		t.Fatal(err)
	}
	if file.Interpolates("K") {
		t.Fatalf("Interpolates(K) = true after Set wrote %q", file.Bytes())
	}
	if err := file.Set("NEW", "$Y"); err != nil {
		t.Fatal(err)
	}
	if file.Interpolates("NEW") {
		t.Fatalf("Interpolates(NEW) = true after Set appended %q", file.Bytes())
	}
}

func TestKeysListsDistinctKeysInFileOrder(t *testing.T) {
	file := mustParse(t, "# c\nB=1\nA=2\nexport C=3\nB=4\nnot a key\nA=5\n")
	got := strings.Join(file.Keys(), ",")
	if got != "B,A,C" {
		t.Fatalf("Keys() = %s, want B,A,C", got)
	}
	if keys := mustParse(t, "").Keys(); len(keys) != 0 {
		t.Fatalf("Keys() on an empty file = %v", keys)
	}
}

func TestSetReplacesTheLastOccurrenceAndDropsTheEarlierOnes(t *testing.T) {
	file := mustParse(t, "# top\nK=1\nA=a\nK=2\n# mid\nB=b\nK=3\n# end\n")
	if err := file.Set("K", "new"); err != nil {
		t.Fatal(err)
	}
	want := "# top\nA=a\n# mid\nB=b\nK=new\n# end\n"
	if got := string(file.Bytes()); got != want {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}
	if got, ok := file.Get("K"); !ok || got != "new" {
		t.Fatalf("Get(K) = (%q, %v)", got, ok)
	}
}

func TestSetAppendsAMissingKey(t *testing.T) {
	file := mustParse(t, "A=1\n")
	if err := file.Set("B", "2"); err != nil {
		t.Fatal(err)
	}
	if got, want := string(file.Bytes()), "A=1\nB=2\n"; got != want {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}
}

func TestSetAddsTheMissingNewlineBeforeAppending(t *testing.T) {
	file := mustParse(t, "A=1")
	if err := file.Set("B", "2"); err != nil {
		t.Fatal(err)
	}
	if got, want := string(file.Bytes()), "A=1\nB=2\n"; got != want {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}
}

func TestSetOnAnEmptyFileWritesOneLine(t *testing.T) {
	file := mustParse(t, "")
	if err := file.Set("A", "1"); err != nil {
		t.Fatal(err)
	}
	if got, want := string(file.Bytes()), "A=1\n"; got != want {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}
}

func TestSetKeepsCommentsOrderAndUnknownContentByteForByte(t *testing.T) {
	original := "# header — keep me\n\nB='x y' # note\n  \n!!! not a setting\nTARGET=old\n#TARGET=commented\nA=1\r\n"
	file := mustParse(t, original)
	if err := file.Set("TARGET", "new"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(original, "TARGET=old", "TARGET=new", 1)
	if got := string(file.Bytes()); got != want {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}
}

func TestSetKeepsTheLineEndingOfTheFile(t *testing.T) {
	t.Run("replacing", func(t *testing.T) {
		file := mustParse(t, "A=1\r\nK=old\r\nB=2\r\n")
		if err := file.Set("K", "new"); err != nil {
			t.Fatal(err)
		}
		if got, want := string(file.Bytes()), "A=1\r\nK=new\r\nB=2\r\n"; got != want {
			t.Fatalf("Bytes() = %q, want %q", got, want)
		}
	})
	t.Run("appending", func(t *testing.T) {
		file := mustParse(t, "A=1\r\n")
		if err := file.Set("B", "2"); err != nil {
			t.Fatal(err)
		}
		if got, want := string(file.Bytes()), "A=1\r\nB=2\r\n"; got != want {
			t.Fatalf("Bytes() = %q, want %q", got, want)
		}
	})
	t.Run("appending after a last line without an ending", func(t *testing.T) {
		file := mustParse(t, "A=1\r\nB=2")
		if err := file.Set("C", "3"); err != nil {
			t.Fatal(err)
		}
		if got, want := string(file.Bytes()), "A=1\r\nB=2\r\nC=3\r\n"; got != want {
			t.Fatalf("Bytes() = %q, want %q", got, want)
		}
	})
	t.Run("replacing a last line without an ending", func(t *testing.T) {
		file := mustParse(t, "A=1\nK=old")
		if err := file.Set("K", "new"); err != nil {
			t.Fatal(err)
		}
		if got, want := string(file.Bytes()), "A=1\nK=new"; got != want {
			t.Fatalf("Bytes() = %q, want %q", got, want)
		}
	})
}

func TestSetKeepsTheExportPrefixOfTheLineItReplaces(t *testing.T) {
	file := mustParse(t, "  export K=old # note\n")
	if err := file.Set("K", "new"); err != nil {
		t.Fatal(err)
	}
	if got, want := string(file.Bytes()), "  export K=new\n"; got != want {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}
}

func TestSetReplacesAMultiLineValueAsAWhole(t *testing.T) {
	file := mustParse(t, "A=1\nK=\"one\ntwo\nthree\"\nB=2\n")
	if err := file.Set("K", "new"); err != nil {
		t.Fatal(err)
	}
	if got, want := string(file.Bytes()), "A=1\nK=new\nB=2\n"; got != want {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}
}

func TestSetWritesTheEncodedValueAndReadsItBack(t *testing.T) {
	for _, value := range []string{
		"", "abc", "http://h:8/v1", "a b", "$X", "${X}", "a #b", `{"k":"v"}`, "it's",
		"  padded  ", `a\b`, "héllo ☃", "a$$b", `say "hi"`, "'", "''",
	} {
		file := mustParse(t, "# c\nK=old\n")
		if err := file.Set("K", value); err != nil {
			t.Fatalf("Set(%q): %v", value, err)
		}
		reparsed := mustParse(t, string(file.Bytes()))
		if got, ok := reparsed.Get("K"); !ok || got != value {
			t.Fatalf("value %q read back as (%q, %v) from %q", value, got, ok, file.Bytes())
		}
	}
}

func TestSetRefusesAValueWithoutChangingTheFile(t *testing.T) {
	const original = "K=old\n"
	file := mustParse(t, original)
	err := file.Set("K", "secret-ending-in-backslash\\")
	if err == nil {
		t.Fatal("Set accepted a value Encode refuses")
	}
	if strings.Contains(err.Error(), "secret-ending") {
		t.Fatalf("the error contains the value: %v", err)
	}
	if got := string(file.Bytes()); got != original {
		t.Fatalf("the file changed after a refused Set: %q", got)
	}
}

func TestSetRefusesAKeyThatCannotBeWrittenAsOne(t *testing.T) {
	for _, key := range []string{"", "A B", "A=B", "A\nB", "1A", "A#", "é"} {
		file := mustParse(t, "")
		if err := file.Set(key, "v"); err == nil {
			t.Fatalf("Set(%q) was accepted", key)
		}
		if len(file.Bytes()) != 0 {
			t.Fatalf("Set(%q) changed the file: %q", key, file.Bytes())
		}
	}
}

func TestDeleteRemovesEveryOccurrence(t *testing.T) {
	file := mustParse(t, "# c\nK=1\nA=a\nexport K=2\nB=b\nK=\"x\ny\"\n")
	file.Delete("K")
	if got, want := string(file.Bytes()), "# c\nA=a\nB=b\n"; got != want {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}
	if _, ok := file.Get("K"); ok {
		t.Fatal("K is still present after Delete")
	}
}

func TestDeleteOfAnAbsentKeyChangesNothing(t *testing.T) {
	const original = "A=1\n# K=2\nKK=3"
	file := mustParse(t, original)
	file.Delete("K")
	if got := string(file.Bytes()); got != original {
		t.Fatalf("Bytes() = %q, want %q", got, original)
	}
}

func TestEncodeLeavesPlainValuesBare(t *testing.T) {
	for _, value := range []string{"", "abc", "http://h:8/v1", "a@b+c", "A-b_c.D:9/x", "tk_0123abcdef"} {
		got, err := Encode(value)
		if err != nil || got != value {
			t.Fatalf("Encode(%q) = (%q, %v), want it unchanged", value, got, err)
		}
	}
}

func TestEncodeQuotesEverythingElse(t *testing.T) {
	tests := map[string]string{
		"a b":         `'a b'`,
		"$X":          `'$X'`,
		"${X}":        `'${X}'`,
		"a #b":        `'a #b'`,
		`{"k":"v"}`:   `'{"k":"v"}'`,
		"it's":        `'it\'s'`,
		"'":           `'\''`,
		" lead":       `' lead'`,
		"trail ":      `'trail '`,
		"a#b":         `'a#b'`,
		`a\b`:         `'a\b'`,
		"héllo":       `'héllo'`,
		"a=b":         `'a=b'`,
		"a,b":         `'a,b'`,
		`"x"`:         `'"x"'`,
		"/Users/o'b/": `'/Users/o\'b/'`,
	}
	for value, want := range tests {
		got, err := Encode(value)
		if err != nil || got != want {
			t.Fatalf("Encode(%q) = (%q, %v), want %q", value, got, err, want)
		}
	}
}

func TestEncodeRefusesWhatAComposeLiteralCannotCarry(t *testing.T) {
	for name, value := range map[string]string{
		"trailing backslash":                  "supersecret\\",
		"backslash before an apostrophe":      "super\\'secret",
		"backslash apostrophe at the end":     "supersecret\\'",
		"newline":                             "super\nsecret",
		"carriage return":                     "super\rsecret",
		"tab":                                 "super\tsecret",
		"NUL":                                 "super\x00secret",
		"escape":                              "super\x1bsecret",
		"delete":                              "super\x7fsecret",
		"a C1 control character (next line)":  "super\u0085secret",
		"a lone backslash":                    `\`,
		"double backslash at the end":         `ab\\`,
		"an apostrophe after a double escape": `a\\'b`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Encode(value)
			if err == nil {
				t.Fatalf("Encode accepted %q as %q", value, got)
			}
			if got != "" {
				t.Fatalf("Encode returned %q together with an error", got)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "super") {
				t.Fatalf("the error contains the value: %v", err)
			}
		})
	}
}

func TestEncodeAllowsABackslashThatIsNeitherLastNorBeforeAnApostrophe(t *testing.T) {
	got, err := Encode(`C:\dir\file`)
	if err != nil || got != `'C:\dir\file'` {
		t.Fatalf("Encode = (%q, %v)", got, err)
	}
}

func TestReadRegularReadsARegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRegular(path)
	if err != nil || string(got) != "A=1\n" {
		t.Fatalf("ReadRegular = (%q, %v)", got, err)
	}
}

func TestReadRegularRefusesASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	if err := os.WriteFile(target, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".env")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, err := ReadRegular(link)
	if err == nil {
		t.Fatal("ReadRegular followed a symlink")
	}
	if !strings.Contains(err.Error(), "symlink") || !strings.Contains(err.Error(), link) {
		t.Fatalf("the error does not say what is wrong with the path: %v", err)
	}
}

func TestReadRegularRefusesADirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadRegular(dir)
	if err == nil {
		t.Fatal("ReadRegular read a directory")
	}
	if !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("the error does not say what is wrong with the path: %v", err)
	}
}

func TestReadRegularReportsAMissingFileAsNotExist(t *testing.T) {
	_, err := ReadRegular(filepath.Join(t.TempDir(), ".env"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadRegular of a missing file = %v, want fs.ErrNotExist", err)
	}
}
