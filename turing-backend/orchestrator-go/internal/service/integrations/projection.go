package integrations

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/integrations/boundedjson"
)

// What one GitHub answer may cost to read, and what one issue may cost the
// model once projected.
const (
	// maxGitHubElementBytes bounds one issue as GitHub sends it. GitHub caps
	// a body at 65,536 characters, at most 256 KiB, so no real issue nears it.
	maxGitHubElementBytes = 1 << 20
	// maxIssueRowBytes bounds one projected row, serialized. The field
	// bounds below keep every row within it, whatever the issue holds.
	maxIssueRowBytes = 1024
	// Field bounds are measured JSON-escaped, the cut marker included.
	maxTitleBytes     = 192
	maxLabelNameBytes = 48
	maxListedLabels   = 5
	maxStateBytes     = 32
	maxAuthorBytes    = 64
	maxTimestampBytes = 32
	maxURLBytes       = 200
	// maxListLimit is the largest page a list may ask for, GitHub's own.
	maxListLimit = 100
	// maxListPage is the highest page a list can name: the largest integer a
	// JSON number carries exactly, so a continuation's page reaches the next
	// call unchanged.
	maxListPage = 1 << 53
)

// githubIssueFields is what a row reads from one issue. It has no body field:
// a list decodes each issue into it, so every body is read past and dropped.
// GitHub marks a pull request listed among issues by its pull_request object,
// which decodes to non-nil with none of its members kept.
type githubIssueFields struct {
	Number      int64         `json:"number"`
	Title       string        `json:"title"`
	State       string        `json:"state"`
	PullRequest *struct{}     `json:"pull_request"`
	User        *githubUser   `json:"user"`
	Labels      []githubLabel `json:"labels"`
	Comments    int64         `json:"comments"`
	UpdatedAt   string        `json:"updated_at"`
	HTMLURL     string        `json:"html_url"`
}

// githubIssue is one issue with its body, as get_issue reads it.
type githubIssue struct {
	githubIssueFields
	Body string `json:"body"`
}

type githubUser struct {
	Login string `json:"login"`
}

// githubLabel is one label's name. GitHub writes a label as an object; its
// schema also allows a bare name.
type githubLabel struct{ name string }

func (l *githubLabel) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &l.name)
	}
	var label struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &label); err != nil {
		return err
	}
	l.name = label.Name
	return nil
}

// issueRow is one issue as a list shows it.
type issueRow struct {
	Number        int64    `json:"number"`
	Title         string   `json:"title"`
	State         string   `json:"state"`
	IsPullRequest bool     `json:"is_pull_request"`
	Author        string   `json:"author"`
	Labels        []string `json:"labels"`
	LabelCount    int      `json:"label_count"`
	Comments      int64    `json:"comments"`
	UpdatedAt     string   `json:"updated_at"`
	HTMLURL       string   `json:"html_url"`
}

// projectIssueRow keeps what a row shows of one issue, each string redacted
// of the credential and then cut to its bound.
func projectIssueRow(issue githubIssueFields, credential []byte) issueRow {
	shown := issue.Labels[:min(len(issue.Labels), maxListedLabels)]
	labels := make([]string, len(shown))
	for index, label := range shown {
		labels[index] = shownText(label.name, credential, maxLabelNameBytes)
	}
	author := ""
	if issue.User != nil {
		author = issue.User.Login
	}
	return issueRow{
		Number:        issue.Number,
		Title:         shownText(issue.Title, credential, maxTitleBytes),
		State:         shownText(issue.State, credential, maxStateBytes),
		IsPullRequest: issue.PullRequest != nil,
		Author:        shownText(author, credential, maxAuthorBytes),
		Labels:        labels,
		LabelCount:    len(issue.Labels),
		Comments:      issue.Comments,
		UpdatedAt:     shownText(issue.UpdatedAt, credential, maxTimestampBytes),
		HTMLURL:       shownText(issue.HTMLURL, credential, maxURLBytes),
	}
}

// issueRowJSON is one issue's row, serialized.
func issueRowJSON(issue githubIssueFields, credential []byte) (json.RawMessage, error) {
	row, err := json.Marshal(projectIssueRow(issue, credential))
	if err != nil {
		return nil, err
	}
	if len(row) > maxIssueRowBytes {
		// The field bounds make this unreachable. It is checked so that a
		// mistake in them fails the call instead of the frame cutting a page.
		return nil, fmt.Errorf("an issue row is %d bytes, over its %d-byte bound", len(row), maxIssueRowBytes)
	}
	return row, nil
}

// shownText is one decoded string as the model may see it: the credential
// replaced, then cut to maxBytes.
func shownText(value string, credential []byte, maxBytes int) string {
	return fitEscaped(redactString(value, credential), maxBytes)
}

// redactString replaces the credential in one decoded string, as
// redactCredential does in bytes. Running on decoded text, after parsing,
// catches a credential GitHub wrote with JSON escapes, and the newlines it
// writes never land inside a JSON string that is still to be parsed.
func redactString(value string, credential []byte) string {
	if len(credential) == 0 || !strings.Contains(value, string(credential)) {
		return value
	}
	redacted := []byte(value)
	redactCredential(redacted, credential)
	return string(redacted)
}

// jsonEscapedLen is how many bytes encoding/json writes for one rune of a
// string, with its default HTML escaping. Every bound on shown text is taken
// in this measure, because the model reads the text serialized.
func jsonEscapedLen(r rune, width int) int {
	switch {
	case r == utf8.RuneError && width == 1:
		return len(`\ufffd`)
	case r == '"' || r == '\\' || r == '\b' || r == '\f' || r == '\n' || r == '\r' || r == '\t':
		return len(`\n`)
	case r < 0x20 || r == '<' || r == '>' || r == '&' || r == '\u2028' || r == '\u2029':
		return len(`\u0000`)
	}
	return width
}

// fitEscaped returns value whole when it escapes to at most maxBytes, and
// otherwise its longest prefix, ending on a character boundary, that fits
// there followed by the visible cut marker.
func fitEscaped(value string, maxBytes int) string {
	if escapesWithin(value, maxBytes) {
		return value
	}
	// The marker's count is at most the whole value's length, so room for the
	// marker with that count is room for the one written.
	budget := maxBytes - len(cutMarker(len(value)))
	kept, used := 0, 0
	for kept < len(value) {
		r, width := utf8.DecodeRuneInString(value[kept:])
		cost := jsonEscapedLen(r, width)
		if used+cost > budget {
			break
		}
		kept, used = kept+width, used+cost
	}
	return value[:kept] + cutMarker(len(value)-kept)
}

func escapesWithin(value string, maxBytes int) bool {
	used := 0
	for offset := 0; offset < len(value); {
		r, width := utf8.DecodeRuneInString(value[offset:])
		if used += jsonEscapedLen(r, width); used > maxBytes {
			return false
		}
		offset += width
	}
	return true
}

// cutMarker is the visible cut: how many bytes of the text are not shown.
func cutMarker(omitted int) string {
	return " [cut: " + strconv.Itoa(omitted) + " bytes not shown]"
}

// issueListContinuation names the call that reads the next page: the page
// after this one, at the page size this call actually asked GitHub for, so
// following it reaches every row once whatever limit was first asked for.
type issueListContinuation struct {
	Page  int64 `json:"page"`
	Limit int   `json:"limit"`
}

// issueList is one list result: its rows, then exactly one continuation.
type issueList struct {
	Issues   []json.RawMessage      `json:"issues"`
	Next     *issueListContinuation `json:"next,omitempty"`
	Complete bool                   `json:"complete,omitempty"`
}

// githubFrameContentBytes is how much content one GitHub frame holds whole. A
// frame's delimiters and instructions are the same length on every call, so
// framing nothing measures them.
var githubFrameContentBytes = func() int {
	empty, err := backendegress.FrameRetrievedContent(githubFraming, nil)
	if err != nil {
		panic("integrations: the GitHub frame cannot be built: " + err.Error())
	}
	return githubFraming.MaxBytes - len(empty)
}()

// listEnvelopeBytes is what a list result holds besides its rows and the
// commas between them, at its longest: the longest continuation is the
// highest page at the largest limit.
var listEnvelopeBytes = func() int {
	envelope, err := json.Marshal(issueList{
		Issues: []json.RawMessage{},
		Next:   &issueListContinuation{Page: maxListPage, Limit: maxListLimit},
	})
	if err != nil {
		panic("integrations: the issue list envelope cannot be built: " + err.Error())
	}
	return len(envelope)
}()

// rowsPerFrame is how many rows at their bound fit one frame beside the
// longest continuation. A list asks GitHub for at most this many issues a
// page, so every page fits one frame whole and the frame never cuts it.
var rowsPerFrame = min(maxListLimit, (githubFrameContentBytes-listEnvelopeBytes+1)/(maxIssueRowBytes+1))

// issueListContent assembles one list result, refusing it rather than let the
// frame cut a row or the continuation.
func issueListContent(rows []json.RawMessage, next *issueListContinuation) ([]byte, error) {
	if rows == nil {
		rows = []json.RawMessage{}
	}
	content, err := json.Marshal(issueList{Issues: rows, Next: next, Complete: next == nil})
	if err != nil {
		return nil, err
	}
	if len(content) > githubFrameContentBytes {
		return nil, fmt.Errorf("an issue list is %d bytes, over the %d one frame holds", len(content), githubFrameContentBytes)
	}
	return content, nil
}

// issueListRows decodes one page of issues into rows, one issue at a time:
// the page is never held whole and every body is read past and dropped. An
// issue over maxGitHubElementBytes, more issues than perPage, or an answer
// that does not parse is a typed refusal, never a projected prefix.
func issueListRows(body io.Reader, perPage int, credential []byte) ([]json.RawMessage, error) {
	rows := make([]json.RawMessage, 0, perPage)
	limits := boundedjson.ArrayLimits{MaxElements: perPage, MaxElementBytes: maxGitHubElementBytes}
	if err := boundedjson.DecodeArray(body, limits, func(issue githubIssueFields) error {
		row, err := issueRowJSON(issue, credential)
		if err != nil {
			return err
		}
		rows = append(rows, row)
		return nil
	}); err != nil {
		return nil, err
	}
	return rows, nil
}

// readIssueList answers one list call: the page's rows and the continuation
// GitHub's Link header calls for.
func readIssueList(response *http.Response, plan githubPlan, credential []byte) (string, error) {
	rows, err := issueListRows(response.Body, plan.perPage, credential)
	if err != nil {
		return "", refusedAnswer(err, "a list of issues")
	}
	var next *issueListContinuation
	if linkHasNext(response.Header) {
		if plan.page >= maxListPage {
			return "", fmt.Errorf("GitHub has pages past %d, the last page this tool can name", int64(maxListPage))
		}
		next = &issueListContinuation{Page: plan.page + 1, Limit: plan.perPage}
	}
	content, err := issueListContent(rows, next)
	if err != nil {
		return "", err
	}
	return frameProjection(content, credential)
}

// issueView is one issue as get_issue shows it: its row, then its body.
type issueView struct {
	issueRow
	Body string `json:"body"`
}

// readIssue answers one get_issue call: the issue's row and as much of its
// body as the rest of one frame holds, cut visibly where it stops.
func readIssue(body io.Reader, credential []byte) (string, error) {
	issue, err := boundedjson.DecodeValue[githubIssue](body, maxGitHubElementBytes)
	if err != nil {
		return "", refusedAnswer(err, "an issue")
	}
	view := issueView{issueRow: projectIssueRow(issue.githubIssueFields, credential)}
	withoutBody, err := json.Marshal(view)
	if err != nil {
		return "", err
	}
	view.Body = shownText(issue.Body, credential, githubFrameContentBytes-len(withoutBody))
	content, err := json.Marshal(view)
	if err != nil {
		return "", err
	}
	if len(content) > githubFrameContentBytes {
		return "", fmt.Errorf("an issue is %d bytes, over the %d one frame holds", len(content), githubFrameContentBytes)
	}
	return frameProjection(content, credential)
}

// frameProjection frames a projection for the model. Every decoded string was
// redacted already; this catches a credential that escaping itself would
// spell, such as one written `\u003c`, and refuses rather than show it.
func frameProjection(content, credential []byte) (string, error) {
	if len(credential) > 0 && bytes.Contains(content, credential) {
		return "", errors.New("GitHub's answer cannot be shown without exposing the connection's credential")
	}
	return frameIntegrationResult(content)
}

// answerRefusal is a GitHub answer refused unread: a sentence the model can
// act on, and the typed reason beneath it for callers that check.
type answerRefusal struct {
	message string
	reason  error
}

func (r *answerRefusal) Error() string { return r.message }
func (r *answerRefusal) Unwrap() error { return r.reason }

func refusedAnswer(err error, wanted string) error {
	switch {
	case errors.Is(err, boundedjson.ErrTooLarge):
		return &answerRefusal{message: "GitHub answered with an issue over 1 MiB, so nothing of its answer is shown", reason: err}
	case errors.Is(err, boundedjson.ErrTooManyElements):
		return &answerRefusal{message: "GitHub answered with more issues than the page size asked for, so nothing of its answer is shown", reason: err}
	case errors.Is(err, boundedjson.ErrMalformed):
		return &answerRefusal{message: "GitHub's answer was not " + wanted, reason: err}
	}
	return errors.New("read GitHub response failed")
}

// linkHasNext reports whether GitHub's Link header names a next page. Only
// relations are read: no link target is parsed or followed, and the next page
// is asked for by number, from arguments this server validated. It reads
// leniently, so a header it misreads errs toward a next page: a needless
// continuation costs one call that answers complete, where a missed one would
// hide every later page.
func linkHasNext(header http.Header) bool {
	for _, value := range header.Values("Link") {
		// Each link is a <target> and then its ;-separated parameters. A
		// target holds no raw '<' or '>', so cutting at them skips each whole.
		for _, link := range strings.Split(value, "<")[1:] {
			_, parameters, found := strings.Cut(link, ">")
			if !found {
				continue
			}
			for _, parameter := range strings.Split(parameters, ";") {
				name, relations, _ := strings.Cut(parameter, "=")
				if !strings.EqualFold(strings.TrimSpace(name), "rel") {
					continue
				}
				for _, relation := range strings.Fields(strings.Trim(strings.TrimSpace(relations), `",`)) {
					if strings.EqualFold(relation, "next") {
						return true
					}
				}
			}
		}
	}
	return false
}
