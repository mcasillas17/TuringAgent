package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/mcpwire"
)

const (
	wantMaxListToolsTotalCount   = 10_000
	wantMaxListToolsEncodedBytes = 4 * 1024 * 1024
	testChannelTimeout           = 2 * time.Second
)

func TestDecodeLimitedObjectRejectsTrailingJSONAndGarbage(t *testing.T) {
	for _, input := range []string{
		`{"value":1} {"other":2}`,
		`{"value":1} trailing`,
	} {
		t.Run(input, func(t *testing.T) {
			result, err := decodeLimitedObject(strings.NewReader(input), int64(len(input)))
			if err == nil || result != nil {
				t.Fatalf("decodeLimitedObject(%q) = %#v, %v; want trailing-data error", input, result, err)
			}
			if retryableFromError(err) {
				t.Fatalf("trailing-data error = %T %v, want non-retryable", err, err)
			}
		})
	}
}

func TestDecodeLimitedObjectAllowsTrailingWhitespaceAndUsesJSONNumbers(t *testing.T) {
	const input = "{\"value\":9007199254740993}\n\t "
	result, err := decodeLimitedObject(strings.NewReader(input), int64(len(input)))
	if err != nil {
		t.Fatalf("decodeLimitedObject returned error: %v", err)
	}
	number, ok := result["value"].(json.Number)
	if !ok || number.String() != "9007199254740993" {
		t.Fatalf("decoded value = %#v, want exact json.Number", result["value"])
	}
}

func TestListToolsPaginatesInOrder(t *testing.T) {
	var requests []listToolsRequest
	server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
		requests = append(requests, request)
		switch len(requests) {
		case 1:
			return http.StatusOK, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"first"},{"name":"second"}],"nextCursor":"page-2"}}`, request.ID), nil
		case 2:
			return http.StatusOK, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"third"}],"nextCursor":"page-3"}}`, request.ID), nil
		default:
			return http.StatusOK, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"fourth"}]}}`, request.ID), nil
		}
	})
	client := NewClient(server.URL, "", server.Client())

	tools, err := client.ListTools(context.Background())
	server.assertNoHandlerErrors(t)
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
			server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
				requests++
				return http.StatusOK, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, request.ID, result), nil
			})
			client := NewClient(server.URL, "", server.Client())

			tools, err := client.ListTools(context.Background())
			server.assertNoHandlerErrors(t)
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
	server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
		requests = append(requests, request)
		return http.StatusOK, fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%d,"result":{"tools":[],"nextCursor":""}}`,
			request.ID,
		), nil
	})
	client := NewClient(server.URL, "", server.Client())

	_, err := client.ListTools(context.Background())
	server.assertNoHandlerErrors(t)
	if err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("ListTools error = %v, want repeated cursor error", err)
	}
	assertListToolsRequests(t, requests, []map[string]any{{}, {"cursor": ""}})
}

func TestListToolsRejectsInvalidNextCursor(t *testing.T) {
	server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
		return http.StatusOK, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":[],"nextCursor":42}}`, request.ID), nil
	})
	client := NewClient(server.URL, "", server.Client())

	_, err := client.ListTools(context.Background())
	server.assertNoHandlerErrors(t)
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
			wantError: "page 1 must contain a tools array",
		},
		{
			name:      "null",
			firstPage: `{"tools":null}`,
			wantError: "page 1 must contain a tools array",
		},
		{
			name:      "wrong type",
			firstPage: `{"tools":{}}`,
			wantError: "page 1 must contain a tools array",
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
			server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
				requests++
				result := test.firstPage
				if requests == 2 {
					result = test.secondPage
				}
				return http.StatusOK, fmt.Sprintf(
					`{"jsonrpc":"2.0","id":%d,"result":%s}`,
					request.ID,
					result,
				), nil
			})
			client := NewClient(server.URL, "", server.Client())

			tools, err := client.ListTools(context.Background())
			server.assertNoHandlerErrors(t)
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
	server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
		requests = append(requests, request)
		return http.StatusOK, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":[],"nextCursor":"same"}}`, request.ID), nil
	})
	client := NewClient(server.URL, "", server.Client())

	_, err := client.ListTools(context.Background())
	server.assertNoHandlerErrors(t)
	if err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("ListTools error = %v, want repeated cursor error", err)
	}
	assertListToolsRequests(t, requests, []map[string]any{{}, {"cursor": "same"}})
}

// TestListToolsRepeatedCursorErrorNeverLeaksBearerContainingQuoteOrBackslash
// proves the repeated-cursor error never interpolates the peer-controlled
// nextCursor value: a peer picks nextCursor freely, including a value
// equal to (or containing) this client's own configured bearer token, and
// %q-formatting that value into the error text escapes any quote or
// backslash it contains — reproducing the token in an escaped,
// non-contiguous, but still fully reconstructible form. This bearer is
// deliberately shaped with both a quote and a backslash within its first
// 16 bytes so even a truncated-prefix leak check would catch a
// regression.
func TestListToolsRepeatedCursorErrorNeverLeaksBearerContainingQuoteOrBackslash(t *testing.T) {
	const token = `mcp-"tok\en-9f3c71a-do-not-leak`
	server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
		return http.StatusOK, fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%d,"result":{"tools":[],"nextCursor":%s}}`,
			request.ID, mustMarshalJSON(t, token),
		), nil
	})
	client := NewClient(server.URL, token, server.Client())

	_, err := client.ListTools(context.Background())
	server.assertNoHandlerErrors(t)
	if err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("ListTools error = %v, want repeated cursor error", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("ListTools error leaks the raw bearer token: %v", err)
	}
	goEscaped := fmt.Sprintf("%q", token)
	if strings.Contains(err.Error(), goEscaped[1:len(goEscaped)-1]) {
		t.Fatalf("ListTools error leaks the Go (%%q) escaped reconstructible form of the bearer token: %v", err)
	}
}

func mustMarshalJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %#v: %v", value, err)
	}
	return string(encoded)
}

func TestListToolsEnforcesPageLimit(t *testing.T) {
	const wantRequests = 100
	requests := 0
	server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
		requests++
		return http.StatusOK, fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%d,"result":{"tools":[],"nextCursor":"page-%d"}}`,
			request.ID,
			requests,
		), nil
	})
	client := NewClient(server.URL, "", server.Client())

	_, err := client.ListTools(context.Background())
	server.assertNoHandlerErrors(t)
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
		server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
			body, err := listToolsResponse(request.ID, page, nil)
			return http.StatusOK, body, err
		})
		client := NewClient(server.URL, "", server.Client())

		tools, err := client.ListTools(context.Background())
		server.assertNoHandlerErrors(t)
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
		server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
			requests++
			if requests == 1 {
				cursor := "overflow"
				body, err := listToolsResponse(request.ID, firstPage, &cursor)
				return http.StatusOK, body, err
			}
			body, err := listToolsResponse(request.ID, []map[string]any{{}}, nil)
			return http.StatusOK, body, err
		})
		client := NewClient(server.URL, "", server.Client())

		tools, err := client.ListTools(context.Background())
		server.assertNoHandlerErrors(t)
		wantError := fmt.Sprintf("page 2 exceeds limit of %d tools", wantMaxListToolsTotalCount)
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
				"page 9 tool 0 exceeds encoded descriptor limit of %d bytes",
				wantMaxListToolsEncodedBytes,
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
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
				body, err := listToolsResponse(request.ID, []map[string]any{tool}, cursor)
				return http.StatusOK, body, err
			})
			client := NewClient(server.URL, "", server.Client())

			tools, err := client.ListTools(context.Background())
			server.assertNoHandlerErrors(t)
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
	server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
		requests++
		if requests == 1 {
			return http.StatusOK, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"first"}],"nextCursor":"next"}}`, request.ID), nil
		}
		return http.StatusOK, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"code":-32000,"message":"later page failed"}}`, request.ID), nil
	})
	client := NewClient(server.URL, "", server.Client())

	tools, err := client.ListTools(context.Background())
	server.assertNoHandlerErrors(t)
	if err == nil || !strings.Contains(err.Error(), "later page failed") {
		t.Fatalf("ListTools error = %v, want later page error", err)
	}
	if tools != nil {
		t.Fatalf("ListTools tools = %#v, want nil on error", tools)
	}
}

func TestListToolsPropagatesCancellationDuringPagination(t *testing.T) {
	secondPageStarted := make(chan struct{})
	secondPageDone := make(chan struct{})
	handlerErrors := make(chan error, 1)
	var once sync.Once
	server := httptest.NewServer(answerHandshake(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request, err := decodeListToolsRequest(r)
		if err != nil {
			reportHandlerError(handlerErrors, err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if request.Params["cursor"] == nil {
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[],"nextCursor":"next"}}`, request.ID)
			return
		}
		once.Do(func() { close(secondPageStarted) })
		defer close(secondPageDone)
		select {
		case <-r.Context().Done():
		case <-time.After(testChannelTimeout):
			reportHandlerError(handlerErrors, errors.New("timed out waiting for second page request cancellation"))
		}
	})))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, "", server.Client())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, err := client.ListTools(ctx)
		errCh <- err
	}()
	waitForTestSignal(t, secondPageStarted, handlerErrors, "second page request")
	cancel()

	err := waitForTestError(t, errCh, handlerErrors, "ListTools result")
	if err != context.Canceled {
		t.Fatalf("ListTools error = %T %v, want context.Canceled directly", err, err)
	}
	waitForTestSignal(t, secondPageDone, handlerErrors, "second page handler completion")
	assertNoHandlerErrors(t, handlerErrors)
}

func TestListToolsClassifiesRetryableFailures(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		retryable bool
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, retryable: false},
		{name: "request timeout", status: http.StatusRequestTimeout, retryable: true},
		{name: "rate limited", status: http.StatusTooManyRequests, retryable: true},
		{name: "server failure", status: http.StatusInternalServerError, retryable: true},
		{name: "invalid HTTP status", status: 600, retryable: false},
		{name: "malformed JSON", status: http.StatusOK, body: `{not-json`, retryable: false},
		{
			name:      "malformed protocol",
			status:    http.StatusOK,
			body:      `{"jsonrpc":"1.0","id":%d,"result":{"tools":[]}}`,
			retryable: false,
		},
		{
			name:      "missing response ID",
			status:    http.StatusOK,
			body:      `{"jsonrpc":"2.0","result":{"tools":[]}}`,
			retryable: false,
		},
		{
			name:   "mismatched response ID",
			status: http.StatusOK,
			// Negative: a client numbers from 1 upwards, so this can never be
			// the id it sent. A small positive literal would only *probably*
			// mismatch now that numbering starts at a random point.
			body:      `{"jsonrpc":"2.0","id":-1,"result":{"tools":[]}}`,
			retryable: false,
		},
		{
			name:      "JSON-RPC server error",
			status:    http.StatusOK,
			body:      `{"jsonrpc":"2.0","id":%d,"error":{"code":-32000,"message":"failed"}}`,
			retryable: true,
		},
		{
			name:      "JSON-RPC internal error",
			status:    http.StatusOK,
			body:      `{"jsonrpc":"2.0","id":%d,"error":{"code":-32603,"message":"internal"}}`,
			retryable: true,
		},
		{
			name:      "JSON-RPC invalid params",
			status:    http.StatusOK,
			body:      `{"jsonrpc":"2.0","id":%d,"error":{"code":-32602,"message":"invalid"}}`,
			retryable: false,
		},
		{
			name:      "JSON-RPC other protocol error",
			status:    http.StatusOK,
			body:      `{"jsonrpc":"2.0","id":%d,"error":{"code":-31999,"message":"other"}}`,
			retryable: false,
		},
		{
			name:      "malformed tools page",
			status:    http.StatusOK,
			body:      `{"jsonrpc":"2.0","id":%d,"result":{"tools":"invalid"}}`,
			retryable: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
				body := test.body
				if strings.Contains(body, "%d") {
					body = fmt.Sprintf(body, request.ID)
				}
				return test.status, body, nil
			})
			client := NewClient(server.URL, "", server.Client())

			_, err := client.ListTools(context.Background())
			server.assertNoHandlerErrors(t)

			if err == nil {
				t.Fatal("ListTools returned nil error")
			}
			if got := retryableFromError(err); got != test.retryable {
				t.Fatalf("retryable(%T %v) = %t, want %t", err, err, got, test.retryable)
			}
			if strings.HasPrefix(test.name, "JSON-RPC") {
				var rpcErr JSONRPCError
				if !errors.As(err, &rpcErr) || !strings.Contains(err.Error(), fmt.Sprint(rpcErr.Code)) {
					t.Fatalf("ListTools error = %T %v, want preserved JSON-RPC code", err, err)
				}
			}
		})
	}
}

func TestListToolsClassifiesNetworkFailureAsRetryable(t *testing.T) {
	transportErr := errors.New("network unavailable")
	client := NewClient("http://mcp.invalid", "", &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportErr
		}),
	})

	_, err := client.ListTools(context.Background())

	if !errors.Is(err, transportErr) || !retryableFromError(err) {
		t.Fatalf("ListTools error = %T %v, want retryable network failure", err, err)
	}
}

func TestListToolsReturnsBodyReadCancellationDirectly(t *testing.T) {
	bodyStarted := make(chan struct{})
	client := NewClient("http://mcp.invalid", "", &http.Client{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: &cancelingResponseBody{
					ctx:     request.Context(),
					started: bodyStarted,
				},
				// A real peer always declares one, and the client now picks
				// its decoder from it.
				Header: http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.ListTools(ctx)
		result <- err
	}()
	<-bodyStarted
	cancel()

	err := <-result

	if err != context.Canceled {
		t.Fatalf("ListTools error = %T %v, want context.Canceled directly", err, err)
	}
}

func TestCallToolReturnsJSONRPCErrorMessage(t *testing.T) {
	server := httptest.NewServer(answerHandshake(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"error":{"code":-32000,"message":"denied"}}`, answeredID(r))
	})))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, "token", server.Client())
	_, err := client.CallTool(context.Background(), "files.read", map[string]any{"path": "note.txt"})
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("CallTool error = %v, want denied", err)
	}
}

func TestCallToolRejectsResponseWithResultAndError(t *testing.T) {
	server := httptest.NewServer(answerHandshake(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[]},"error":{"code":-32000,"message":"failed"}}`, answeredID(r))
	})))
	t.Cleanup(server.Close)

	result, err := NewClient(server.URL, "", server.Client()).CallTool(context.Background(), "system.echo", nil)

	if err == nil || !strings.Contains(err.Error(), "both result and error") {
		t.Fatalf("CallTool error = %v, want conflicting response envelope error", err)
	}
	if result != nil {
		t.Fatalf("CallTool result = %#v, want nil", result)
	}
}

func TestCallToolRejectsNonObjectResult(t *testing.T) {
	server := httptest.NewServer(answerHandshake(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":["not","an","object"]}`, answeredID(r))
	})))
	t.Cleanup(server.Close)

	result, err := NewClient(server.URL, "", server.Client()).CallTool(context.Background(), "system.echo", nil)

	if err == nil || !strings.Contains(err.Error(), "result must be an object") {
		t.Fatalf("CallTool error = %v, want non-object result error", err)
	}
	if result != nil {
		t.Fatalf("CallTool result = %#v, want nil", result)
	}
}

func TestCallToolReturnsTypedErrorForToolFailureResult(t *testing.T) {
	server := httptest.NewServer(answerHandshake(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"tool failed"}],"isError":true}}`, answeredID(r))
	})))
	t.Cleanup(server.Close)

	result, err := NewClient(server.URL, "", server.Client()).CallTool(context.Background(), "system.echo", nil)

	var toolErr ToolCallError
	if !errors.As(err, &toolErr) || !strings.Contains(err.Error(), "tool failed") {
		t.Fatalf("CallTool error = %T %v, want typed tool failure", err, err)
	}
	if result != nil {
		t.Fatalf("CallTool result = %#v, want nil", result)
	}
	if toolErr.Result["isError"] != true {
		t.Fatalf("ToolCallError result = %#v, want original failure result", toolErr.Result)
	}
}

func TestCallToolAcceptsObjectResult(t *testing.T) {
	server := httptest.NewServer(answerHandshake(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[],"isError":false,"value":42}}`, answeredID(r))
	})))
	t.Cleanup(server.Close)

	result, err := NewClient(server.URL, "", server.Client()).CallTool(context.Background(), "system.echo", nil)

	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if result["value"] != json.Number("42") {
		t.Fatalf("CallTool result = %#v, want preserved object", result)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type cancelingResponseBody struct {
	ctx     context.Context
	started chan<- struct{}
}

func (b *cancelingResponseBody) Read([]byte) (int, error) {
	close(b.started)
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (*cancelingResponseBody) Close() error { return nil }

func retryableFromError(err error) bool {
	var classified interface{ Retryable() bool }
	return errors.As(err, &classified) && classified.Retryable()
}

var _ io.ReadCloser = (*cancelingResponseBody)(nil)

type listToolsRequest struct {
	ID     int64          `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

type listToolsTestServer struct {
	*httptest.Server
	handlerErrors <-chan error
}

func newListToolsServer(
	t *testing.T,
	response func(listToolsRequest) (status int, body string, err error),
) *listToolsTestServer {
	t.Helper()
	handlerErrors := make(chan error, 1)
	server := httptest.NewServer(answerHandshake(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request, err := decodeListToolsRequest(r)
		if err != nil {
			reportHandlerError(handlerErrors, err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		status, body, err := response(request)
		if err != nil {
			reportHandlerError(handlerErrors, fmt.Errorf("build response: %w", err))
			http.Error(w, "invalid response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})))
	t.Cleanup(server.Close)
	return &listToolsTestServer{Server: server, handlerErrors: handlerErrors}
}

// answerHandshake replies to the MCP lifecycle methods and passes everything
// else through, so a fixture written against the operation phase does not have
// to restate the handshake, which consumes the client's first request id.
//
// A fixture must answer the id the request carried — answeredID reads it, and
// the body is restored below so an inner handler still can. Never a literal: a
// client begins numbering at a random point, so a hardcoded id matches nothing.
func answerHandshake(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "unreadable body", http.StatusBadRequest)
			return
		}
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &envelope)
		switch envelope.Method {
		case "initialize":
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w,
				`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}}}}`,
				envelope.ID, mcpwire.ProtocolVersion)
		case "notifications/initialized", "notifications/cancelled":
			w.WriteHeader(http.StatusAccepted)
		default:
			r.Body = io.NopCloser(bytes.NewReader(body))
			next.ServeHTTP(w, r)
		}
	})
}

func decodeListToolsRequest(r *http.Request) (listToolsRequest, error) {
	var request listToolsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return listToolsRequest{}, fmt.Errorf("decode request: %w", err)
	}
	if request.Method != "tools/list" {
		return listToolsRequest{}, fmt.Errorf("method = %q, want tools/list", request.Method)
	}
	return request, nil
}

func (server *listToolsTestServer) assertNoHandlerErrors(t *testing.T) {
	t.Helper()
	assertNoHandlerErrors(t, server.handlerErrors)
}

func assertNoHandlerErrors(t *testing.T, handlerErrors <-chan error) {
	t.Helper()
	select {
	case err := <-handlerErrors:
		t.Fatalf("HTTP handler assertion: %v", err)
	default:
	}
}

func reportHandlerError(handlerErrors chan<- error, err error) {
	select {
	case handlerErrors <- err:
	default:
	}
}

func waitForTestSignal(t *testing.T, signal <-chan struct{}, handlerErrors <-chan error, description string) {
	t.Helper()
	select {
	case <-signal:
	case err := <-handlerErrors:
		t.Fatalf("HTTP handler assertion while waiting for %s: %v", description, err)
	case <-time.After(testChannelTimeout):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForTestError(t *testing.T, result <-chan error, handlerErrors <-chan error, description string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case err := <-handlerErrors:
		t.Fatalf("HTTP handler assertion while waiting for %s: %v", description, err)
	case <-time.After(testChannelTimeout):
		t.Fatalf("timed out waiting for %s", description)
	}
	return nil
}

func assertListToolsRequests(t *testing.T, requests []listToolsRequest, params []map[string]any) {
	t.Helper()
	if len(requests) != len(params) {
		t.Fatalf("requests = %d, want %d", len(requests), len(params))
	}
	// Ids are consecutive from wherever this client began numbering, which is a
	// random point: two runtimes sharing one bundled token must not number their
	// requests alike, or one's cancellation reaches the other's call. The
	// handshake took the id before the first tools/list, so the run starts one
	// past it.
	first := requests[0].ID
	for index, request := range requests {
		if request.ID != first+int64(index) {
			t.Errorf("request %d ID = %d, want %d", index, request.ID, first+int64(index))
		}
		if !reflect.DeepEqual(request.Params, params[index]) {
			t.Errorf("request %d params = %#v, want %#v", index, request.Params, params[index])
		}
	}
}

func listToolsResponse(id int64, tools []map[string]any, nextCursor *string) (string, error) {
	result := map[string]any{"tools": tools}
	if nextCursor != nil {
		result["nextCursor"] = *nextCursor
	}
	response, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	if err != nil {
		return "", fmt.Errorf("marshal response: %w", err)
	}
	return string(response), nil
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

func TestABundledServerCannotRedirectTheApprovalBearingCallElsewhere(t *testing.T) {
	// CallTool's body carries the one-time approval token and the run's
	// provenance capability under params._meta, and net/http replays a request
	// body verbatim on a 307/308 (bytes.NewReader populates GetBody). The
	// redirect target is chosen entirely by the peer, so following one would
	// POST an approval capability to an arbitrary URL — the endpoint being a
	// fixed internal address does not help, because the redirect leaves it.
	var elsewhereGotBody string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		elsewhereGotBody = string(body)
		w.Header().Set("content-type", "application/json")
		// A literal id is safe here and only here: this target must never be
		// reached, and the test fails if it is. A fixture a client actually reads
		// has to echo the id the request carried.
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer elsewhere.Close()

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		if req.Method == mcpwire.MethodInitialize {
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w,
				`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}},"serverInfo":{"name":"peer","version":"1"}}}`,
				req.ID, mcpwire.ProtocolVersion)
			return
		}
		if req.Method == "" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer peer.Close()

	client := NewClient(peer.URL, "bundled-token", peer.Client())
	_, err := client.CallTool(context.Background(), "files.read", map[string]any{
		"_meta": map[string]any{"approvalToken": "one-time-capability"},
	})
	if err == nil {
		t.Fatal("CallTool followed a peer-chosen redirect; it must refuse")
	}
	if strings.Contains(elsewhereGotBody, "one-time-capability") {
		t.Fatalf("the approval capability was replayed to the redirect target: %s", elsewhereGotBody)
	}
}

func TestNewClientRefusesRedirectsWithoutDisturbingTheCallersClient(t *testing.T) {
	// The redirect refusal is applied once, to the client every request goes
	// through, so asserting it here covers the handshake, notifications,
	// ping, tools/list and tools/call alike — there is no per-method path that
	// could miss it. The end-to-end proof that a redirect is actually refused
	// lives in TestABundledServerCannotRedirectTheApprovalBearingCallElsewhere.
	//
	// The copy must also be faithful: NewClient hands back a different client
	// value, and silently dropping a caller's Transport, Jar or Timeout would
	// remove hardening the caller thought it had configured.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{MaxIdleConns: 7}
	caller := &http.Client{Transport: transport, Jar: jar, Timeout: 11 * time.Second}

	client := NewClient("http://mcp.internal", "token", caller)

	if client.httpClient == caller {
		t.Fatal("NewClient stored the caller's client; mutating it would change the caller's own value")
	}
	if client.httpClient.CheckRedirect == nil {
		t.Fatal("NewClient left CheckRedirect nil, which is net/http's follow-up-to-ten default")
	}
	if err := refusedRedirect(client.httpClient); err == nil {
		t.Fatal("the client's redirect policy permits a redirect")
	}
	if client.httpClient.Transport != transport {
		t.Error("the caller's Transport was dropped")
	}
	if client.httpClient.Jar != jar {
		t.Error("the caller's Jar was dropped")
	}
	if client.httpClient.Timeout != 11*time.Second {
		t.Errorf("the caller's Timeout became %v", client.httpClient.Timeout)
	}
	if caller.CheckRedirect != nil {
		t.Error("NewClient mutated the caller's own client")
	}
}

func TestAMidHandshakeInvalidationIsRetryableNotAPermanentDiscoveryFailure(t *testing.T) {
	// mcpwire.ErrConnectionInvalidated is transient by design: the handshake
	// result is discarded rather than published over a concurrent wipe, and the
	// next caller runs a fresh one. Classified non-retryable it becomes
	// permanentToolDiscoveryError, and the whole run fails tool discovery for a
	// benign race the design expects to recover from.
	var invalidatingClient *Client
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		if req.Method == "" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if req.Method == mcpwire.MethodInitialize {
			// The wipe lands while this handshake is still running.
			invalidatingClient.connection.Invalidate()
		}
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w,
			`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}},"serverInfo":{"name":"peer","version":"1"}}}`,
			req.ID, mcpwire.ProtocolVersion)
	}))
	defer peer.Close()
	invalidatingClient = NewClient(peer.URL, "", peer.Client())

	// Drive the real discovery path, not the state machine directly: the
	// classification has to hold where ListTools actually sees it.
	_, err := invalidatingClient.ListTools(context.Background())
	if err == nil {
		t.Fatal("ListTools succeeded on a connection invalidated mid-handshake")
	}
	if !errors.Is(err, mcpwire.ErrConnectionInvalidated) {
		t.Fatalf("ListTools error = %v, want ErrConnectionInvalidated", err)
	}
	if !Retryable(err) {
		t.Fatal("a mid-handshake invalidation is classified as a permanent tool-discovery failure; the next handshake would have recovered")
	}
}

// refusedRedirect exercises a client's redirect policy the way net/http does,
// with a real request and history, and returns whatever it refuses with.
func refusedRedirect(client *http.Client) error {
	if client.CheckRedirect == nil {
		return nil
	}
	from, _ := http.NewRequest(http.MethodPost, "http://mcp.internal/mcp", nil)
	to, _ := http.NewRequest(http.MethodPost, "http://elsewhere.invalid/mcp", nil)
	return client.CheckRedirect(to, []*http.Request{from})
}

func TestListToolsBoundsTheCursorItEchoesBack(t *testing.T) {
	// The cursor is the one discovery value Turing keeps and sends back, and a
	// peer may serve a distinct one on every page. Unbounded, a peer serving no
	// tools at all could still make the client retain and re-transmit a response
	// cap's worth of opaque token per page.
	for _, test := range []struct {
		name      string
		length    int
		wantError error
	}{
		{name: "boundary", length: mcpwire.MaxCursorBytes},
		{name: "overflow", length: mcpwire.MaxCursorBytes + 1, wantError: mcpwire.ErrCursorTooLong},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := newListToolsServer(t, func(request listToolsRequest) (int, string, error) {
				requests++
				if requests == 1 {
					cursor := strings.Repeat("c", test.length)
					body, err := listToolsResponse(request.ID, []map[string]any{}, &cursor)
					return http.StatusOK, body, err
				}
				body, err := listToolsResponse(request.ID, []map[string]any{}, nil)
				return http.StatusOK, body, err
			})
			client := NewClient(server.URL, "", server.Client())

			_, err := client.ListTools(context.Background())
			server.assertNoHandlerErrors(t)
			if test.wantError == nil {
				if err != nil {
					t.Fatalf("ListTools returned error at the cursor boundary: %v", err)
				}
				return
			}
			if !errors.Is(err, test.wantError) {
				t.Fatalf("ListTools error = %v, want one wrapping %v", err, test.wantError)
			}
			if strings.Contains(err.Error(), "cccc") {
				t.Fatal("the error echoed the peer's cursor back")
			}
		})
	}
}

// answeredID reports the JSON-RPC id the client put on this request, so a
// fixture answers the request it received instead of assuming where the client
// began numbering. answerHandshake restores the body before delegating, so the
// inner handler can still read it.
func answeredID(r *http.Request) int64 {
	body, _ := io.ReadAll(r.Body)
	var envelope struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(body, &envelope)
	return envelope.ID
}
