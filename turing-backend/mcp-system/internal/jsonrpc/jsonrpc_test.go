package jsonrpc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeRequestPreservesValidIDWhenMethodIsInvalid(t *testing.T) {
	tests := []struct {
		name string
		body string
		want any
	}{
		{name: "missing method with string ID", body: `{"jsonrpc":"2.0","id":"request-1"}`, want: "request-1"},
		{name: "invalid method with numeric ID", body: `{"jsonrpc":"2.0","id":42,"method":7}`, want: json.Number("42")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, requestErr := DecodeRequest(strings.NewReader(test.body))
			if requestErr == nil {
				t.Fatal("DecodeRequest returned nil error")
			}
			if requestErr.ID != test.want {
				t.Fatalf("request error ID = %#v, want %#v", requestErr.ID, test.want)
			}
		})
	}
}

func TestDecodeRequestRejectsOversizedIDWithoutEchoingIt(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":"` + strings.Repeat("x", 1024) + `","method":"tools/list"}`
	_, requestErr := DecodeRequest(strings.NewReader(body))
	if requestErr == nil {
		t.Fatal("DecodeRequest accepted oversized id")
	}
	if requestErr.Code != -32600 || requestErr.ID != nil {
		t.Fatalf("request error = %+v, want invalid request with null id", requestErr)
	}
}
