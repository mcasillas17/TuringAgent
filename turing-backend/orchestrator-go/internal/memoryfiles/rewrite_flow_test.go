package memoryfiles

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// A flow mapping — the whole frontmatter written as `{key: value, ...}` inside
// braces — is legal YAML that Obsidian reads and this splice cannot survive.
// The byte range one value occupies is bounded by the next key or by the end of
// the block, and neither bound is a boundary inside braces: replacing the last
// value swallows the closing brace, replacing an earlier one drops a newline
// into the middle of the mapping, and appending a missing key writes it outside
// the braces entirely. Each one hands the user back a note their own editor can
// no longer parse, so the rewrite refuses before it writes a byte.
func TestRewriteFrontmatterRefsRefusesAFlowMappingAndLeavesTheFileExactlyAsItWas(t *testing.T) {
	const noteID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	testCases := []struct {
		name    string
		content string
		request RewriteFrontmatterRefsRequest
	}{
		{
			name:    "refs is the last key in the mapping",
			content: "---\n{id: \"" + noteID + "\", refs: [\"sess_a\"]}\n---\nbody\n",
			request: RewriteFrontmatterRefsRequest{Refs: []string{"sess_b"}},
		},
		{
			name:    "refs is followed by another key",
			content: "---\n{refs: [\"sess_a\"], title: keep}\n---\nbody\n",
			request: RewriteFrontmatterRefsRequest{Refs: []string{"sess_b"}},
		},
		{
			name:    "refs is absent and would be appended",
			content: "---\n{title: keep}\n---\nbody\n",
			request: RewriteFrontmatterRefsRequest{Refs: []string{"sess_a"}},
		},
		{
			name:    "an identity is assigned to a flow mapping",
			content: "---\n{title: keep}\n---\nbody\n",
			request: RewriteFrontmatterRefsRequest{NoteID: noteID},
		},
		{
			name:    "the mapping is spread over several lines",
			content: "---\n{\n  id: \"" + noteID + "\",\n  refs: [\"sess_a\"]\n}\n---\nbody\n",
			request: RewriteFrontmatterRefsRequest{Refs: []string{"sess_b"}},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			vault := newTestVault(t)
			path := writeVaultFile(t, vault, "beliefs/flow.md", testCase.content)
			request := testCase.request
			request.RelPath = "beliefs/flow.md"

			result, err := vault.RewriteFrontmatterRefs(context.Background(), request)
			if !errors.Is(err, ErrNoteParse) {
				t.Fatalf("expected a typed parse refusal, got error %v and content %q", err, result.Content)
			}
			var parseError *NoteParseError
			if !errors.As(err, &parseError) {
				t.Fatalf("refusal is not a *NoteParseError: %v", err)
			}
			if !strings.Contains(parseError.Reason, "flow mapping") {
				t.Fatalf("the refusal does not say which shape it cannot edit: %q", parseError.Reason)
			}
			if result != (RewrittenNote{}) {
				t.Fatalf("a refused rewrite still reported a result: %+v", result)
			}
			onDisk, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read note: %v", readErr)
			}
			if string(onDisk) != testCase.content {
				t.Fatalf("a refused rewrite changed the file:\nwant %q\ngot  %q", testCase.content, onDisk)
			}
		})
	}
}

// The refusal is about braces around the whole mapping, not about flow syntax
// anywhere in the file. A block mapping whose refs value happens to be a flow
// sequence is the ordinary Obsidian shape, and it keeps being spliced byte for
// byte.
func TestRewriteFrontmatterRefsStillSplicesAFlowSequenceInsideABlockMapping(t *testing.T) {
	vault := newTestVault(t)
	content := "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\nrefs: [\"sess_a\", \"sess_b\"] # kept\ntitle: keep\n---\nbody\n"
	path := writeVaultFile(t, vault, "beliefs/flow-sequence.md", content)

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/flow-sequence.md",
		Refs:    []string{"sess_b"},
	})
	if err != nil {
		t.Fatalf("rewrite refs: %v", err)
	}
	want := strings.Replace(content, "[\"sess_a\", \"sess_b\"]", "[\"sess_b\"]", 1)
	if result.Content != want {
		t.Fatalf("flow sequence rewrite:\nwant %q\ngot  %q", want, result.Content)
	}
	onDisk, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read note: %v", readErr)
	}
	if string(onDisk) != want {
		t.Fatalf("on-disk bytes differ from the reported content:\nwant %q\ngot  %q", want, onDisk)
	}
}
