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

func TestListToolsTreatsAbsentNullAndEmptyNextCursorAsTerminal(t *testing.T) {
	tests := map[string]string{
		"absent": `{"tools":[{"name":"only"}]}`,
		"null":   `{"tools":[{"name":"only"}],"nextCursor":null}`,
		"empty":  `{"tools":[{"name":"only"}],"nextCursor":""}`,
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

func toolNames(tools []map[string]any) []string {
	names := make([]string, len(tools))
	for index, tool := range tools {
		names[index], _ = tool["name"].(string)
	}
	return names
}
