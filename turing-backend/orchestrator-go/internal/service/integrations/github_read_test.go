package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/integrations/boundedjson"
)

// pagedGitHub answers issue-list requests from a fixed list of issues the way
// GitHub pages them, and records every request it is sent. Its Link header
// points somewhere GitHub never would, so a client that followed it instead of
// asking for the page by number would be caught.
type pagedGitHub struct {
	issues   []string
	requests []*url.URL
}

func (g *pagedGitHub) RoundTrip(request *http.Request) (*http.Response, error) {
	g.requests = append(g.requests, request.URL)
	query := request.URL.Query()
	page, pageErr := strconv.Atoi(query.Get("page"))
	perPage, perPageErr := strconv.Atoi(query.Get("per_page"))
	if pageErr != nil || perPageErr != nil || page < 1 || perPage < 1 || perPage > 100 {
		return githubAnswer(http.StatusUnprocessableEntity, `{"message":"Validation Failed"}`, nil), nil
	}
	start := min((page-1)*perPage, len(g.issues))
	end := min(start+perPage, len(g.issues))
	header := http.Header{}
	if end < len(g.issues) {
		header.Set("Link", fmt.Sprintf(`<https://evil.example/collect?page=%d>; rel="next", <https://evil.example/collect?page=999>; rel="last"`, page+1))
	}
	return githubAnswer(http.StatusOK, "["+strings.Join(g.issues[start:end], ",")+"]", header), nil
}

func (g *pagedGitHub) pagesRequested(t *testing.T) (pages []int, perPage []int) {
	t.Helper()
	for _, requested := range g.requests {
		if requested.Scheme != "https" || requested.Host != "api.github.com" || requested.Path != "/repos/octo/app/issues" {
			t.Fatalf("request went to %s, want only the issues of octo/app on api.github.com", requested)
		}
		page, _ := strconv.Atoi(requested.Query().Get("page"))
		size, _ := strconv.Atoi(requested.Query().Get("per_page"))
		pages, perPage = append(pages, page), append(perPage, size)
	}
	return pages, perPage
}

func githubAnswer(statusCode int, body string, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{StatusCode: statusCode, Body: io.NopCloser(strings.NewReader(body)), Header: header}
}

func readServer(transport http.RoundTripper) *Server {
	server := &Server{}
	server.SetHTTPClient(&http.Client{Transport: transport})
	return server
}

type listedPage struct {
	Issues []json.RawMessage `json:"issues"`
	Next   *struct {
		Page  int64 `json:"page"`
		Limit int   `json:"limit"`
	} `json:"next"`
	Complete bool `json:"complete"`
}

type listedRow struct {
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

// framedContent is what one framed result carries, failing the test when the
// result is over the frame's bound or the frame had to cut it.
func framedContent(t *testing.T, framed string) []byte {
	t.Helper()
	if len(framed) > backendegress.MaxFramedContentBytes {
		t.Fatalf("framed result is %d bytes, over %d", len(framed), backendegress.MaxFramedContentBytes)
	}
	if strings.Contains(framed, "Result truncated") {
		t.Fatal("the frame cut the result")
	}
	lines := strings.SplitN(framed, "\n", 3)
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "BEGIN ") {
		t.Fatalf("result is not framed: %q", framed)
	}
	content, ok := strings.CutSuffix(lines[2], "\nEND "+strings.TrimPrefix(lines[0], "BEGIN "))
	if !ok {
		t.Fatalf("frame does not end with its own delimiter: %q", framed)
	}
	return []byte(content)
}

// listResult checks one list result's shape: rows of at most 1 KiB holding
// only the projected fields, then exactly one continuation.
func listResult(t *testing.T, framed string) (listedPage, []listedRow) {
	t.Helper()
	content := framedContent(t, framed)
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(content, &keys); err != nil {
		t.Fatalf("result is not one JSON object: %v", err)
	}
	_, hasNext := keys["next"]
	_, hasComplete := keys["complete"]
	if len(keys) != 2 || hasNext == hasComplete {
		t.Fatalf("result keys %v, want issues and exactly one continuation", slices.Sorted(maps.Keys(keys)))
	}
	var page listedPage
	if err := json.Unmarshal(content, &page); err != nil {
		t.Fatal(err)
	}
	if hasComplete && !page.Complete {
		t.Fatalf("complete is %s, want true", keys["complete"])
	}
	rows := make([]listedRow, len(page.Issues))
	for index, raw := range page.Issues {
		if len(raw) > maxIssueRowBytes {
			t.Fatalf("row is %d bytes serialized, over %d", len(raw), maxIssueRowBytes)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		got := slices.Sorted(maps.Keys(fields))
		want := []string{"author", "comments", "html_url", "is_pull_request", "label_count", "labels", "number", "state", "title", "updated_at"}
		if !slices.Equal(got, want) {
			t.Fatalf("row fields %v, want only %v", got, want)
		}
		if err := json.Unmarshal(raw, &rows[index]); err != nil {
			t.Fatal(err)
		}
	}
	return page, rows
}

// followNext reads a list the way a model is told to: the first call, then the
// call each result's continuation names, until a result says complete.
func followNext(t *testing.T, server *Server, args map[string]any, credential string) (rows []listedRow, pages []listedPage, framed []string) {
	t.Helper()
	for range 100 {
		result, err := server.callGitHub(context.Background(), "github.list_issues", args, []byte(credential))
		if err != nil {
			t.Fatal(err)
		}
		page, pageRows := listResult(t, result)
		rows, pages, framed = append(rows, pageRows...), append(pages, page), append(framed, result)
		if page.Next == nil {
			return rows, pages, framed
		}
		args = withArgs(args, map[string]any{"page": float64(page.Next.Page), "limit": float64(page.Next.Limit)})
	}
	t.Fatal("the list never said complete")
	return nil, nil, nil
}

func withArgs(base map[string]any, extra map[string]any) map[string]any {
	args := make(map[string]any, len(base)+len(extra))
	for key, value := range base {
		args[key] = value
	}
	for key, value := range extra {
		args[key] = value
	}
	return args
}

func listArgs(extra map[string]any) map[string]any {
	return withArgs(map[string]any{"connection_id": "conn", "owner": "octo", "repo": "app"}, extra)
}

func encodeIssue(t *testing.T, issue map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// worstCaseIssues is the design's worst case: 512-byte titles of characters
// that escape or take several bytes, 100 labels of 50 characters each and
// 64 KiB bodies, about 6.5 MiB in all.
func worstCaseIssues(t *testing.T, count int) []string {
	t.Helper()
	issues := make([]string, count)
	for index := range issues {
		number := index + 1
		labels := make([]map[string]any, 100)
		for label := range labels {
			labels[label] = map[string]any{
				"id": label, "node_id": "LA_kwDOQzynz88AAAACUDMzvQ", "url": "https://api.github.com/repos/octo/app/labels/x",
				"name": fmt.Sprintf("%03d", label) + strings.Repeat("<ü", 23) + "x", "color": "d73a4a", "default": false,
				"description": "A label description that is never shown",
			}
		}
		issues[index] = encodeIssue(t, map[string]any{
			"number":     number,
			"title":      fmt.Sprintf("%03d ", number) + strings.Repeat(`<"é`, 127),
			"state":      "open",
			"user":       map[string]any{"login": "octocat", "id": 583231, "avatar_url": "https://avatars.githubusercontent.com/u/583231?v=4"},
			"labels":     labels,
			"comments":   number,
			"updated_at": "2026-11-01T10:00:00Z",
			"html_url":   fmt.Sprintf("https://github.com/octo/app/issues/%d", number),
			"body":       "body-sentinel-not-for-lists " + strings.Repeat("b", 64<<10),
		})
	}
	return issues
}

func numbersOf(rows []listedRow) []int64 {
	numbers := make([]int64, len(rows))
	for index, row := range rows {
		numbers[index] = row.Number
	}
	return numbers
}

func oneToN(n int) []int64 {
	numbers := make([]int64, n)
	for index := range numbers {
		numbers[index] = int64(index + 1)
	}
	return numbers
}

func consecutivePages(n int) []int {
	pages := make([]int, n)
	for index := range pages {
		pages[index] = index + 1
	}
	return pages
}

func repeated(value, n int) []int {
	values := make([]int, n)
	for index := range values {
		values[index] = value
	}
	return values
}

func TestTheWorstCaseListProjectsCompletelyOnePageACall(t *testing.T) {
	github := &pagedGitHub{issues: worstCaseIssues(t, 100)}
	raw := 0
	for _, issue := range github.issues {
		raw += len(issue)
	}
	if raw < 6<<20 {
		t.Fatalf("fixture is %d bytes, want the worst case of about 6.5 MiB", raw)
	}
	rows, pages, framed := followNext(t, readServer(github), listArgs(nil), "")

	if got := numbersOf(rows); !slices.Equal(got, oneToN(100)) {
		t.Fatalf("rows reached %v, want each of 1..100 exactly once", got)
	}
	calls := (100 + rowsPerFrame - 1) / rowsPerFrame
	if len(pages) != calls || calls > 7 {
		t.Fatalf("calls = %d, want ceil(100 / %d) = %d and at most seven", len(pages), rowsPerFrame, calls)
	}
	requested, perPage := github.pagesRequested(t)
	if !slices.Equal(requested, consecutivePages(calls)) || !slices.Equal(perPage, repeated(rowsPerFrame, calls)) {
		t.Fatalf("pages %v of %v, want each page once at per_page=%d", requested, perPage, rowsPerFrame)
	}
	for _, result := range framed {
		if strings.Contains(result, "body-sentinel-not-for-lists") {
			t.Fatal("a body reached a list result")
		}
	}
	for _, row := range rows {
		if escaped := escapedSize(t, row.Title); escaped > maxTitleBytes || !strings.Contains(row.Title, "bytes not shown]") {
			t.Fatalf("title %q escapes to %d bytes, want a visible cut within %d", row.Title, escaped, maxTitleBytes)
		}
		if len(row.Labels) != 5 || row.LabelCount != 100 {
			t.Fatalf("labels listed %d of %d, want 5 of 100", len(row.Labels), row.LabelCount)
		}
		for _, label := range row.Labels {
			if escaped := escapedSize(t, label); escaped > maxLabelNameBytes || !strings.Contains(label, "bytes not shown]") {
				t.Fatalf("label %q escapes to %d bytes, want a visible cut within %d", label, escaped, maxLabelNameBytes)
			}
		}
	}
}

func escapedSize(t *testing.T, value string) int {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return len(encoded) - 2
}

func smallIssues(t *testing.T, count int) []string {
	t.Helper()
	issues := make([]string, count)
	for index := range issues {
		number := index + 1
		issues[index] = encodeIssue(t, map[string]any{
			"number": number, "title": fmt.Sprintf("Issue %d", number), "state": "open",
			"user": map[string]any{"login": "octocat"}, "labels": []any{}, "comments": 0,
			"updated_at": "2026-11-01T10:00:00Z", "html_url": fmt.Sprintf("https://github.com/octo/app/issues/%d", number),
			"body": "body",
		})
	}
	return issues
}

func TestASmallLimitIsThePageSizeAndItsContinuationCarriesIt(t *testing.T) {
	github := &pagedGitHub{issues: smallIssues(t, 12)}
	result, err := readServer(github).callGitHub(context.Background(), "github.list_issues", listArgs(map[string]any{"limit": float64(5)}), nil)
	if err != nil {
		t.Fatal(err)
	}
	page, rows := listResult(t, result)
	if got := numbersOf(rows); !slices.Equal(got, oneToN(5)) {
		t.Fatalf("rows %v, want 1..5", got)
	}
	if page.Next == nil || page.Next.Page != 2 || page.Next.Limit != 5 {
		t.Fatalf("continuation %+v, want next page 2 with limit 5", page.Next)
	}
	if _, perPage := github.pagesRequested(t); !slices.Equal(perPage, []int{5}) {
		t.Fatalf("per_page sent %v, want [5]", perPage)
	}
}

func TestALimitAboveOneFrameIsReadOneFrameAtATime(t *testing.T) {
	github := &pagedGitHub{issues: smallIssues(t, 120)}
	rows, pages, _ := followNext(t, readServer(github), listArgs(map[string]any{"limit": float64(50)}), "")
	if got := numbersOf(rows); !slices.Equal(got, oneToN(120)) {
		t.Fatalf("rows reached %v, want each of 1..120 exactly once", got)
	}
	for _, page := range pages[:len(pages)-1] {
		if page.Next == nil || page.Next.Limit != rowsPerFrame {
			t.Fatalf("continuation %+v, want limit %d", page.Next, rowsPerFrame)
		}
	}
	calls := (120 + rowsPerFrame - 1) / rowsPerFrame
	requested, perPage := github.pagesRequested(t)
	if !slices.Equal(requested, consecutivePages(calls)) || !slices.Equal(perPage, repeated(rowsPerFrame, calls)) {
		t.Fatalf("pages %v of %v, want each page once at per_page=%d", requested, perPage, rowsPerFrame)
	}
}

func recordedIssue(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/recorded_issue.json")
	if err != nil {
		t.Fatal(err)
	}
	var issue map[string]any
	if err := json.Unmarshal(raw, &issue); err != nil {
		t.Fatal(err)
	}
	return issue
}

func recordedPullRequest(t *testing.T, number int) map[string]any {
	t.Helper()
	issue := recordedIssue(t)
	issue["number"] = number
	issue["html_url"] = fmt.Sprintf("https://github.com/octo-org/octo-repo/pull/%d", number)
	issue["draft"] = false
	issue["pull_request"] = map[string]any{
		"url":       fmt.Sprintf("https://api.github.com/repos/octo-org/octo-repo/pulls/%d", number),
		"html_url":  fmt.Sprintf("https://github.com/octo-org/octo-repo/pull/%d", number),
		"diff_url":  fmt.Sprintf("https://github.com/octo-org/octo-repo/pull/%d.diff", number),
		"patch_url": fmt.Sprintf("https://github.com/octo-org/octo-repo/pull/%d.patch", number),
		"merged_at": nil,
	}
	return issue
}

// A page GitHub really sends can pass 1 MiB long before an element does; the
// decoder holds one element at a time, so the page still projects whole.
func TestARecordedShapePageOverOneMiBProjectsEveryRow(t *testing.T) {
	var page strings.Builder
	page.WriteString("[")
	for number := 1; number <= 100; number++ {
		issue := recordedIssue(t)
		if number%3 == 0 {
			issue = recordedPullRequest(t, number)
		}
		issue["number"] = number
		issue["body"] = strings.Repeat("Steps to reproduce the failure. ", 12<<10/32)
		if number > 1 {
			page.WriteString(",")
		}
		page.WriteString(encodeIssue(t, issue))
	}
	page.WriteString("]")
	if page.Len() <= 1<<20 {
		t.Fatalf("page is %d bytes, want over 1 MiB", page.Len())
	}
	rows, err := issueListRows(strings.NewReader(page.String()), 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 100 {
		t.Fatalf("projected %d rows, want all 100", len(rows))
	}
	for index, row := range rows {
		if len(row) > maxIssueRowBytes || strings.Contains(string(row), "Steps to reproduce") {
			t.Fatalf("row %d is %d bytes or holds the body: %s", index+1, len(row), row)
		}
		var decoded listedRow
		if err := json.Unmarshal(row, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Number != int64(index+1) || decoded.IsPullRequest != ((index+1)%3 == 0) {
			t.Fatalf("row %d = %+v, want number %d flagged as a pull request only when it is one", index+1, decoded, index+1)
		}
	}
}

func TestListRowsHoldOnlyTheProjectedFields(t *testing.T) {
	issue := recordedIssue(t)
	issue["body"] = "body-sentinel-not-for-lists"
	github := &pagedGitHub{issues: []string{encodeIssue(t, issue), encodeIssue(t, recordedPullRequest(t, 1348))}}
	result, err := readServer(github).callGitHub(context.Background(), "github.list_issues", listArgs(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := listResult(t, result)
	want := []string{
		`{"number":1347,"title":"Found a bug in the release pipeline","state":"open","is_pull_request":false,"author":"octocat","labels":["bug","release"],"label_count":2,"comments":3,"updated_at":"2026-11-21T09:56:27Z","html_url":"https://github.com/octo-org/octo-repo/issues/1347"}`,
		`{"number":1348,"title":"Found a bug in the release pipeline","state":"open","is_pull_request":true,"author":"octocat","labels":["bug","release"],"label_count":2,"comments":3,"updated_at":"2026-11-21T09:56:27Z","html_url":"https://github.com/octo-org/octo-repo/pull/1348"}`,
	}
	if len(page.Issues) != 2 || string(page.Issues[0]) != want[0] || string(page.Issues[1]) != want[1] || !page.Complete {
		t.Fatalf("result = %s", framedContent(t, result))
	}
	if strings.Contains(result, "body-sentinel-not-for-lists") {
		t.Fatal("a body reached a list result")
	}
}

// The credential is redacted from what was decoded, so it is caught however
// GitHub wrote it, and one issue holding it costs nothing but its own text.
func TestACredentialInOneTitleLeavesEveryOtherRowReachable(t *testing.T) {
	const credential = "ghp_int009aSentinelCredential0123456789"
	escaped := ""
	for _, character := range credential {
		escaped += fmt.Sprintf(`\u%04x`, character)
	}
	for name, write := range map[string]func(string) string{
		"as written":        func(encoded string) string { return encoded },
		"with JSON escapes": func(encoded string) string { return strings.Replace(encoded, credential, escaped, 1) },
		"beside other text": func(encoded string) string { return encoded },
	} {
		t.Run(name, func(t *testing.T) {
			issues := smallIssues(t, 100)
			title := credential
			if name == "beside other text" {
				title = "token " + credential + " leaked"
			}
			holding := encodeIssue(t, map[string]any{
				"number": 37, "title": title, "state": "open", "user": map[string]any{"login": "octocat"}, "labels": []any{},
				"comments": 0, "updated_at": "2026-11-01T10:00:00Z", "html_url": "https://github.com/octo/app/issues/37",
			})
			issues[36] = write(holding)
			if name == "with JSON escapes" && strings.Contains(issues[36], credential) {
				t.Fatal("fixture still holds the credential unescaped")
			}
			rows, _, framed := followNext(t, readServer(&pagedGitHub{issues: issues}), listArgs(nil), credential)
			if got := numbersOf(rows); !slices.Equal(got, oneToN(100)) {
				t.Fatalf("rows reached %v, want each of 1..100", got)
			}
			for _, result := range framed {
				if strings.Contains(result, credential) {
					t.Fatal("the credential reached a result")
				}
			}
			if got := rows[36].Title; !strings.Contains(got, strings.Repeat("\n", len(credential))) {
				t.Fatalf("title of the issue holding the credential = %q, want it replaced in place", got)
			}
		})
	}
}

func TestListIssuesRefusesAnAnswerItCannotProjectWhole(t *testing.T) {
	overBound := encodeIssue(t, map[string]any{"number": 2, "title": "big", "body": strings.Repeat("b", 1<<20)})
	for _, test := range []struct {
		name   string
		answer string
		limit  float64
		want   error
	}{
		{name: "an element over 1 MiB", answer: "[" + smallIssues(t, 1)[0] + "," + overBound + "]", limit: 5, want: boundedjson.ErrTooLarge},
		{name: "more elements than the per_page sent", answer: "[" + strings.Join(smallIssues(t, 4), ",") + "]", limit: 3, want: boundedjson.ErrTooManyElements},
		{name: "JSON cut short", answer: "[" + smallIssues(t, 1)[0] + `,{"number":2,"title":"cut`, limit: 5, want: boundedjson.ErrMalformed},
		{name: "an object instead of a list", answer: `{"message":"Not Found"}`, limit: 5, want: boundedjson.ErrMalformed},
		{name: "a field of another type", answer: `[{"number":"seven"}]`, limit: 5, want: boundedjson.ErrMalformed},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := readServer(roundTripFunc(func(*http.Request) (*http.Response, error) {
				return githubAnswer(http.StatusOK, test.answer, nil), nil
			}))
			result, err := server.callGitHub(context.Background(), "github.list_issues", listArgs(map[string]any{"limit": test.limit}), nil)
			if !errors.Is(err, test.want) || result != "" {
				t.Fatalf("result %q err %v, want no result and %v", result, err, test.want)
			}
		})
	}
}

// Every continuation names a page the next call accepts: the highest page a
// JSON number names exactly has no next page to offer, so GitHub saying there
// is one is refused rather than answered with a page that cannot be asked for.
func TestEveryContinuationNamesAPageTheNextCallAccepts(t *testing.T) {
	alwaysMore := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return githubAnswer(http.StatusOK, "[]", http.Header{"Link": []string{`<https://api.github.com/x>; rel="next"`}}), nil
	})
	server := readServer(alwaysMore)
	result, err := server.callGitHub(context.Background(), "github.list_issues", listArgs(map[string]any{"page": float64(maxListPage - 1)}), nil)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := listResult(t, result)
	if page.Next == nil || page.Next.Page != maxListPage {
		t.Fatalf("continuation %+v, want page %d", page.Next, int64(maxListPage))
	}
	if _, err := githubRequest("github.list_issues", listArgs(map[string]any{"page": float64(page.Next.Page)})); err != nil {
		t.Fatalf("the continuation's page was refused: %v", err)
	}
	if result, err := server.callGitHub(context.Background(), "github.list_issues", listArgs(map[string]any{"page": float64(maxListPage)}), nil); err == nil {
		t.Fatalf("result %q, want a refusal instead of a page past the last one that can be named", result)
	}
}

// Redaction runs on decoded text, so a credential only JSON escaping spells
// out, like `\u003c`, would slip past it; such a result is refused instead.
func TestACredentialThatEscapingWouldSpellIsNeverShown(t *testing.T) {
	const credential = `\u003c`
	server := readServer(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return githubAnswer(http.StatusOK, `[{"number":1,"title":"a < b"}]`, nil), nil
	}))
	result, err := server.callGitHub(context.Background(), "github.list_issues", listArgs(nil), []byte(credential))
	if err == nil || strings.Contains(result, credential) {
		t.Fatalf("result %q err %v, want a refusal that shows nothing", result, err)
	}
}

// A model follows a continuation by passing its fields back as arguments, so
// each one must be an argument the tool's schema declares and accepts.
func TestAContinuationNamesOnlyArgumentsTheListSchemaAccepts(t *testing.T) {
	tool, ok := lookupIntegrationTool("github.list_issues")
	if !ok {
		t.Fatal("github.list_issues is not a tool")
	}
	properties := tool.schema["properties"].(map[string]any)
	encoded, err := json.Marshal(issueListContinuation{Page: maxListPage, Limit: rowsPerFrame})
	if err != nil {
		t.Fatal(err)
	}
	var continuation map[string]float64
	if err := json.Unmarshal(encoded, &continuation); err != nil {
		t.Fatal(err)
	}
	for name, value := range continuation {
		declared, ok := properties[name].(map[string]any)
		if !ok || declared["type"] != "integer" {
			t.Fatalf("continuation field %q is not an integer argument of the schema: %v", name, properties[name])
		}
		if minimum, ok := declared["minimum"].(int); !ok || value < float64(minimum) {
			t.Fatalf("continuation %s=%v is below the schema's minimum %v", name, value, declared["minimum"])
		}
		if maximum, ok := declared["maximum"].(int); ok && value > float64(maximum) {
			t.Fatalf("continuation %s=%v is above the schema's maximum %v", name, value, maximum)
		}
	}
}

func TestGetIssueOverOneMiBIsRefused(t *testing.T) {
	answer := encodeIssue(t, map[string]any{"number": 7, "title": "big", "body": strings.Repeat("b", 1<<20)})
	server := readServer(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return githubAnswer(http.StatusOK, answer, nil), nil
	}))
	result, err := server.callGitHub(context.Background(), "github.get_issue", listArgs(map[string]any{"issue_number": float64(7)}), nil)
	if !errors.Is(err, boundedjson.ErrTooLarge) || result != "" {
		t.Fatalf("result %q err %v, want no result and ErrTooLarge", result, err)
	}
}

func TestGetIssueShowsItsRowAndAsMuchBodyAsOneFrameHolds(t *testing.T) {
	const credential = "ghp_int009aSentinelCredential0123456789"
	issue := recordedIssue(t)
	issue["body"] = "Credential " + credential + " pasted here. " + strings.Repeat("<é", 100<<10)
	server := readServer(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/repos/octo/app/issues/1347" {
			t.Fatalf("requested %s", request.URL)
		}
		return githubAnswer(http.StatusOK, encodeIssue(t, issue), nil), nil
	}))
	result, err := server.callGitHub(context.Background(), "github.get_issue", listArgs(map[string]any{"issue_number": float64(1347)}), []byte(credential))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result, credential) {
		t.Fatal("the credential reached the result")
	}
	var got struct {
		listedRow
		Body string `json:"body"`
	}
	content := framedContent(t, result)
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatal(err)
	}
	if got.Number != 1347 || got.Author != "octocat" || !slices.Equal(got.Labels, []string{"bug", "release"}) || got.IsPullRequest {
		t.Fatalf("issue fields = %+v", got.listedRow)
	}
	if !strings.HasPrefix(got.Body, "Credential "+strings.Repeat("\n", len(credential))+" pasted here. <é") || !strings.HasSuffix(got.Body, " bytes not shown]") {
		t.Fatalf("body = %q, want the redacted start and a visible cut", got.Body)
	}
}
