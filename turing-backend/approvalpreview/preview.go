// Package approvalpreview defines the bounded, non-authorizing snapshot shared
// by the orchestrator and file server. Only an ordinary signed approval can
// authorize the mutation described here.
package approvalpreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

const (
	MaxTextBytes     = 64 * 1024
	MaxArgsBytes     = 128 * 1024
	MaxDiffBytes     = 256 * 1024
	MaxSnapshotBytes = 2 * 1024 * 1024
	TokenKind        = "approval-preview"
)

type Snapshot struct {
	State turingv1.ApprovalPreviewState `json:"state"`
	// ArgumentsJSON is optional sanitized display material, not canonical
	// execution arguments. READY file snapshots use {} and disclose the exact
	// mutation in File; non-file snapshots retain structured display arguments.
	ArgumentsJSON string                        `json:"arguments_json"`
	File          *turingv1.FileMutationPreview `json:"file,omitempty"`
}

// Binding is part of the existing approval JWT, not another authorization.
type Binding struct {
	PreviewHash  string `json:"preview_hash"`
	SessionID    string `json:"sid"`
	RunID        string `json:"rid"`
	ToolCallID   string `json:"tool_call_id"`
	Generation   int64  `json:"gen"`
	PhysicalPath string `json:"physical_path"`
	BeforeExists bool   `json:"before_exists"`
	BeforeHash   string `json:"before_hash"`
	AfterHash    string `json:"after_hash"`
}

type Request struct {
	Tool            string         `json:"tool"`
	AgentID         string         `json:"agent_id"`
	Args            map[string]any `json:"args"`
	ProvenanceToken string         `json:"provenance_token"`
}

func Hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

var secretPattern = regexp.MustCompile(`(?i)(bearer\s+\S+|-----BEGIN [A-Z ]*PRIVATE KEY|(?:gh[pousr]_|github_pat_|sk-|AKIA)[A-Za-z0-9_-]{8,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+|://[^/\s:]+:[^@\s]+@)`)
var secretKey = regexp.MustCompile(`(?i)(password|passwd|secret|token|authorization|^auths?$|credential|api.?key|private.?key|cookie)`)
var assignmentKeyPattern = regexp.MustCompile(`(?:"([^"\r\n]+)"|'([^'\r\n]+)'|([[:alnum:]_.-]+))\s*[:=]\s*\S`)

func SensitivePath(value string) bool {
	components := strings.Split(strings.ToLower(path.Clean(value)), "/")
	for index, component := range components {
		switch component {
		case ".ssh", ".aws", ".kube", "secrets", "credentials":
			return true
		case ".docker":
			if index+1 < len(components) && components[index+1] == "config.json" {
				return true
			}
		}
	}
	base := strings.ToLower(path.Base(value))
	return base == ".env" || strings.HasPrefix(base, ".env.") ||
		strings.Contains(base, "credential") || strings.Contains(base, "secret") ||
		base == "id_rsa" || base == "id_ed25519" || base == ".netrc" ||
		strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key")
}

func UnsafeText(value string) bool {
	if secretPattern.MatchString(value) {
		return true
	}
	// Match a complete assignment identifier, then apply the same policy as
	// structured keys. Suffixes such as secret_access_key must not bypass
	// redaction, while mentioning an identifier in prose is not an assignment.
	for _, assignment := range assignmentKeyPattern.FindAllStringSubmatch(value, -1) {
		for _, key := range assignment[1:] {
			if secretKey.MatchString(key) {
				return true
			}
		}
	}
	return false
}

func Binary(value string) bool {
	if !utf8.ValidString(value) {
		return true
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return true
		}
	}
	return false
}

// SanitizeArguments bounds both traversal and rendering. Sensitive strings are
// removed as a whole; redaction never leaves a suffix that could be a credential.
func SanitizeArguments(args map[string]any) (string, turingv1.ApprovalPreviewState) {
	raw, err := json.Marshal(args)
	if err != nil || len(raw) > MaxArgsBytes {
		return "{}", turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_OVERSIZED
	}
	if sensitiveArgumentPath(args) {
		return "{}", turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED
	}
	redacted := false
	binary := false
	var walk func(any, int) any
	walk = func(v any, depth int) any {
		if depth > 32 {
			redacted = true
			return "[redacted]"
		}
		switch x := v.(type) {
		case map[string]any:
			// A path identifies the sensitivity of its whole argument object,
			// including sibling content. Prose is not a filesystem path.
			if sensitiveArgumentPath(x) {
				redacted = true
				return "[redacted]"
			}
			m := make(map[string]any, len(x))
			for k, val := range x {
				if secretKey.MatchString(k) || UnsafeText(k) || Binary(k) {
					redacted = true
					m["[redacted key]"] = "[redacted]"
				} else {
					m[k] = walk(val, depth+1)
				}
			}
			return m
		case []any:
			a := make([]any, len(x))
			for i, val := range x {
				a[i] = walk(val, depth+1)
			}
			return a
		case string:
			if Binary(x) {
				binary = true
				return "[binary]"
			}
			if UnsafeText(x) {
				redacted = true
				return "[redacted]"
			}
		}
		return v
	}
	safe, _ := json.Marshal(walk(args, 0))
	if binary {
		return "{}", turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_BINARY
	}
	if redacted {
		return string(safe), turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED
	}
	return string(safe), turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY
}

func sensitiveArgumentPath(args map[string]any) bool {
	for key, value := range args {
		if strings.EqualFold(key, "path") {
			if filePath, ok := value.(string); ok && SensitivePath(filePath) {
				return true
			}
		}
	}
	return false
}

// Diff uses one replacement hunk, bounded linearly by the two input sizes.
// It is intentionally not a quadratic minimal-edit diff.
func Diff(before, after string) string {
	lines := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.SplitAfter(s, "\n")
	}
	bl, al := lines(before), lines(after)
	if len(bl) > 0 && bl[len(bl)-1] == "" {
		bl = bl[:len(bl)-1]
	}
	if len(al) > 0 && al[len(al)-1] == "" {
		al = al[:len(al)-1]
	}
	var b strings.Builder
	beforeStart, afterStart := 1, 1
	if len(bl) == 0 {
		beforeStart = 0
	}
	if len(al) == 0 {
		afterStart = 0
	}
	fmt.Fprintf(&b, "--- before\n+++ after\n@@ -%d,%d +%d,%d @@\n", beforeStart, len(bl), afterStart, len(al))
	for _, pair := range []struct {
		prefix string
		lines  []string
	}{{"-", bl}, {"+", al}} {
		for _, line := range pair.lines {
			b.WriteString(pair.prefix)
			b.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				b.WriteString("\n\\ No newline at end of file\n")
			}
		}
	}
	return b.String()
}
