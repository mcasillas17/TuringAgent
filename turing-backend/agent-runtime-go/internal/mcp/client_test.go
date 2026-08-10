package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

const (
	wantMaxListToolsTotalCount   = 10_000
	wantMaxListToolsEncodedBytes = 4 * 1024 * 1024
)

func TestListToolsPaginatesInOrder(t *testing.T) {
	var requests []listToolsRequest
	server := newListToolsServer(t, func(request listToolsRequest) (int, string) {
		requests = append(requests, request)
		switch len(requests) {
		case 1:
			return http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"first"},{"name":"second"}],"nextCursor":"page-2"}}`
		case 2:
			return http.StatusOK, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"third"}],"nextCursor":"page-3"}}`
		default:
			return http.StatusOK, `{"jsonrpc":"2.0","id":3,"result":{"tools":[{"name":"fourth"}]}}`
		}
	})
	client := NewClient(server.URL, "", server.Client())

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if got, want := toolNames(tools), []string{"first", "second", "third", "fourth"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tool names = %v, want %v", got, want)
	}
	assertListToolsRequests(t, requests, []map[string]any{
		{},
		{"cursor": "page-2"},
		{"cursor": "page-3"},
	})
}

func TestListToolsTreatsAbsentAndNullNextCursorAsTerminal(t *testing.T) {
	tests := map[string]string{
		"absent": `{"tools":[{"name":"only"}]}`,
		"null":   `{"tools":[{"name":"only"}],"nextCursor":null}`,
	}
	for name, result := range tests {
		t.Run(name, func(t *testing.T) {
			requests := 0
			server := newListToolsServer(t, func(request listToolsRequest) (int, string) {
				requests++
				return http.StatusOK, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, request.ID, result)
			})
			client := NewClient(server.URL, "", server.Client())

			tools, err := client.ListTools(context.Background())
			if err != nil {
				t.Fatalf("ListTools returned error: %v", err)
			}
			if got := toolNames(tools); !reflect.DeepEqual(got, []string{"only"}) {
				t.Fatalf("tool names = %v, want [only]", got)
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
		})
	}
}

func TestListToolsRequestsEmptyCursorAndRejectsWhenRepeated(t *testing.T) {
	var requests []listToolsRequest
	server := newListToolsServer(t, func(request listToolsRequest) (int, string) {
		requests = append(requests, request)
		return http.StatusOK, fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%d,"result":{"tools":[],"nextCursor":""}}`,
			request.ID,
		)
	})
	client := NewClient(server.URL, "", server.Client())

	_, err := client.ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("ListTools error = %v, want repeated cursor error", err)
	}
	assertListToolsRequests(t, requests, []map[string]any{{}, {"cursor": ""}})
}

func TestListToolsRejectsInvalidNextCursor(t *testing.T) {
	server := newListToolsServer(t, func(request listToolsRequest) (int, string) {
		return http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{"tools":[],"nextCursor":42}}`
	})
	client := NewClient(server.URL, "", server.Client())

	_, err := client.ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "nextCursor") {
		t.Fatalf("ListTools error = %v, want invalid nextCursor error", err)
	}
}

func TestListToolsValidatesToolsOnEveryPage(t *testing.T) {
	tests := []struct {
		name       string
		firstPage  string
		secondPage string
		wantError  string
	}{
		{
			name:      "missing",
			firstPage: `{}`,
			wantError: "page 1 tools must be present and an array",
		},
		{
			name:      "null",
			firstPage: `{"tools":null}`,
			wantError: "page 1 tools must be present and an array",
		},
		{
			name:      "wrong type",
			firstPage: `{"tools":{}}`,
			wantError: "page 1 tools must be present and an array",
		},
		{
			name:       "bad entry on later page",
			firstPage:  `{"tools":[{"name":"valid"}],"nextCursor":"next"}`,
			secondPage: `{"tools":[{"name":"also-valid"},42]}`,
			wantError:  "page 2 tool 1 must be an object",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := newListToolsServer(t, func(request listToolsRequest) (int, string) {
				requests++
				result := test.firstPage
				if requests == 2 {
					result = test.secondPage
				}
				return http.StatusOK, fmt.Sprintf(
					`{"jsonrpc":"2.0","id":%d,"result":%s}`,
					request.ID,
					result,
				)
			})
			client := NewClient(server.URL, "", server.Client())

			tools, err := client.ListTools(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("ListTools error = %v, want %q", err, test.wantError)
			}
			if tools != nil {
				t.Fatalf("ListTools tools = %#v, want nil on error", tools)
			}
		})
	}
}

func TestListToolsRejectsRepeatedCursor(t *testing.T) {
	var requests []listToolsRequest
	server := newListToolsServer(t, func(request listToolsRequest) (int, string) {
		requests = append(requests, request)
		return http.StatusOK, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":[],"nextCursor":"same"}}`, request.ID)
	})
	client := NewClient(server.URL, "", server.Client())

	_, err := client.ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("ListTools error = %v, want repeated cursor error", err)
	}
	assertListToolsRequests(t, requests, []map[string]any{{}, {"cursor": "same"}})
}

func TestListToolsEnforcesPageLimit(t *testing.T) {
	const wantRequests = 100
	requests := 0
	server := newListToolsServer(t, func(request listToolsRequest) (int, string) {
		requests++
		return http.StatusOK, fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%d,"result":{"tools":[],"nextCursor":"page-%d"}}`,
			request.ID,
			requests,
		)
	})
	client := NewClient(server.URL, "", server.Client())

	_, err := client.ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "page limit") {
		t.Fatalf("ListTools error = %v, want page limit error", err)
	}
	if requests != wantRequests {
		t.Fatalf("requests = %d, want %d", requests, wantRequests)
	}
}

func TestListToolsEnforcesTotalToolCountLimit(t *testing.T) {
	t.Run("boundary", func(t *testing.T) {
		page := make([]map[string]any, wantMaxListToolsTotalCount)
		for index := range page {
			page[index] = map[string]any{}
		}
		server := newListToolsServer(t, func(request listToolsRequest) (int, string) {
			return http.StatusOK, listToolsResponse(t, request.ID, page, nil)
		})
		client := NewClient(server.URL, "", server.Client())

		tools, err := client.ListTools(context.Background())
		if err != nil {
			t.Fatalf("ListTools returned error at count boundary: %v", err)
		}
		if len(tools) != wantMaxListToolsTotalCount {
			t.Fatalf("ListTools returned %d tools, want %d", len(tools), wantMaxListToolsTotalCount)
		}
	})

	t.Run("overflow", func(t *testing.T) {
		firstPage := make([]map[string]any, wantMaxListToolsTotalCount)
		for index := range firstPage {
			firstPage[index] = map[string]any{}
		}
		requests := 0
		server := newListToolsServer(t, func(request listToolsRequest) (int, string) {
			requests++
			if requests == 1 {
				cursor := "overflow"
				return http.StatusOK, listToolsResponse(t, request.ID, firstPage, &cursor)
			}
			return http.StatusOK, listToolsResponse(t, request.ID, []map[string]any{{}}, nil)
		})
		client := NewClient(server.URL, "", server.Client())

		tools, err := client.ListTools(context.Background())
		wantError := fmt.Sprintf("page 2 total tool count exceeds limit of %d", wantMaxListToolsTotalCount)
		if err == nil || !strings.Contains(err.Error(), wantError) {
			t.Fatalf("ListTools error = %v, want %q", err, wantError)
		}
		if tools != nil {
			t.Fatalf("ListTools tools = %#v, want nil on error", tools)
		}
	})
}

func TestListToolsEnforcesAggregateEncodedToolBytesLimit(t *testing.T) {
	const pagesAtBoundary = 8
	boundaryTool := toolWithEncodedSize(t, wantMaxListToolsEncodedBytes/pagesAtBoundary)

	tests := []struct {
		name      string
		pageCount int
		wantError string
	}{
		{name: "boundary", pageCount: pagesAtBoundary},
		{
			name:      "overflow",
			pageCount: pagesAtBoundary + 1,
			wantError: fmt.Sprintf(
				"page 9 tool 0 makes aggregate encoded tool bytes exceed limit of %d",
				wantMaxListToolsEncodedBytes,
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := newListToolsServer(t, func(request listToolsRequest) (int, string) {
				requests++
				tool := boundaryTool
				if requests > pagesAtBoundary {
					tool = map[string]any{}
				}
				var cursor *string
				if requests < test.pageCount {
					next := fmt.Sprintf("page-%d", requests+1)
					cursor = &next
				}
				return http.StatusOK, listToolsResponse(t, request.ID, []map[string]any{tool}, cursor)
			})
			client := NewClient(server.URL, "", server.Client())

			tools, err := client.ListTools(context.Background())
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("ListTools returned error at byte boundary: %v", err)
				}
				if len(tools) != pagesAtBoundary {
					t.Fatalf("ListTools returned %d tools, want %d", len(tools), pagesAtBoundary)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("ListTools error = %v, want %q", err, test.wantError)
			}
			if tools != nil {
				t.Fatalf("ListTools tools = %#v, want nil on error", tools)
			}
		})
	}
}

func TestListToolsPropagatesLaterPageJSONRPCError(t *testing.T) {
	requests := 0
	server := newListToolsServer(t, func(request listToolsRequest) (int, string) {
		requests++
		if requests == 1 {
			return http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"first"}],"nextCursor":"next"}}`
		}
		return http.StatusOK, `{"jsonrpc":"2.0","id":2,"error":{"code":-32000,"message":"later page failed"}}`
	})
	client := NewClient(server.URL, "", server.Client())

	tools, err := client.ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "later page failed") {
		t.Fatalf("ListTools error = %v, want later page error", err)
	}
	if tools != nil {
		t.Fatalf("ListTools tools = %#v, want nil on error", tools)
	}
}

func TestListToolsPropagatesCancellationDuringPagination(t *testing.T) {
	secondPageStarted := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := decodeListToolsRequest(t, r)
		if request.Params["cursor"] == nil {
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[],"nextCursor":"next"}}`, request.ID)
			return
		}
		once.Do(func() { close(secondPageStarted) })
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, "", server.Client())
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := client.ListTools(ctx)
		errCh <- err
	}()
	<-secondPageStarted
	cancel()

	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("ListTools error = %v, want context.Canceled", err)
	}
}

func TestCallToolReturnsJSONRPCErrorMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"denied"}}`))
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, "token", server.Client())
	_, err := client.CallTool(context.Background(), "files.read", map[string]any{"path": "note.txt"})
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("CallTool error = %v, want denied", err)
	}
}

type listToolsRequest struct {
	ID     int64          `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

func newListToolsServer(
	t *testing.T,
	response func(listToolsRequest) (status int, body string),
) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := decodeListToolsRequest(t, r)
		status, body := response(request)
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func decodeListToolsRequest(t *testing.T, r *http.Request) listToolsRequest {
	t.Helper()
	var request listToolsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if request.Method != "tools/list" {
		t.Fatalf("method = %q, want tools/list", request.Method)
	}
	return request
}

func assertListToolsRequests(t *testing.T, requests []listToolsRequest, params []map[string]any) {
	t.Helper()
	if len(requests) != len(params) {
		t.Fatalf("requests = %d, want %d", len(requests), len(params))
	}
	for index, request := range requests {
		if request.ID != int64(index+1) {
			t.Errorf("request %d ID = %d, want %d", index, request.ID, index+1)
		}
		if !reflect.DeepEqual(request.Params, params[index]) {
			t.Errorf("request %d params = %#v, want %#v", index, request.Params, params[index])
		}
	}
}

func listToolsResponse(t *testing.T, id int64, tools []map[string]any, nextCursor *string) string {
	t.Helper()
	result := map[string]any{"tools": tools}
	if nextCursor != nil {
		result["nextCursor"] = *nextCursor
	}
	response, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return string(response)
}

func toolWithEncodedSize(t *testing.T, size int) map[string]any {
	t.Helper()
	const emptyToolBytes = len(`{"data":""}`)
	if size < emptyToolBytes {
		t.Fatalf("tool size %d is smaller than minimum %d", size, emptyToolBytes)
	}
	tool := map[string]any{"data": strings.Repeat("x", size-emptyToolBytes)}
	encoded, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal sized tool: %v", err)
	}
	if len(encoded) != size {
		t.Fatalf("encoded tool size = %d, want %d", len(encoded), size)
	}
	return tool
}

func toolNames(tools []map[string]any) []string {
	names := make([]string, len(tools))
	for index, tool := range tools {
		names[index], _ = tool["name"].(string)
	}
	return names
}
