package jsonrpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type Request struct {
	JSONRPC string
	ID      any
	Method  string
	Params  map[string]any
}

type Response struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Result  any            `json:"result,omitempty"`
	Error   map[string]any `json:"error,omitempty"`
}

type RequestError struct {
	Code    int
	Message string
	ID      any
	Cause   error
}

func (e *RequestError) Error() string {
	return e.Message
}

func (e *RequestError) Unwrap() error {
	return e.Cause
}

type rawRequest struct {
	JSONRPC json.RawMessage `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  json.RawMessage `json:"method"`
	Params  json.RawMessage `json:"params"`
}

var integerIDPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

const maxIDBytes = 256

func DecodeRequest(reader io.Reader) (Request, *RequestError) {
	decoder := json.NewDecoder(reader)
	var envelope json.RawMessage
	if err := decoder.Decode(&envelope); err != nil {
		return Request{}, requestError(-32700, "parse error", nil, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return Request{}, requestError(-32600, "request must contain exactly one JSON object", nil, nil)
	} else if !errors.Is(err, io.EOF) {
		return Request{}, requestError(-32700, "parse error", nil, err)
	}
	strictDecoder := json.NewDecoder(bytes.NewReader(envelope))
	strictDecoder.DisallowUnknownFields()
	var raw rawRequest
	if err := strictDecoder.Decode(&raw); err != nil {
		return Request{}, requestError(-32600, "invalid request", nil, err)
	}

	var version string
	if len(raw.JSONRPC) == 0 || json.Unmarshal(raw.JSONRPC, &version) != nil || version != "2.0" {
		return Request{}, requestError(-32600, "jsonrpc must be \"2.0\"", nil, nil)
	}
	id, err := decodeID(raw.ID)
	if err != nil {
		return Request{}, requestError(-32600, err.Error(), nil, err)
	}
	var method string
	if len(raw.Method) == 0 || json.Unmarshal(raw.Method, &method) != nil || strings.TrimSpace(method) == "" {
		return Request{}, requestError(-32600, "method must be a non-empty string", id, nil)
	}
	params := map[string]any{}
	if len(raw.Params) > 0 {
		if string(raw.Params) == "null" {
			return Request{}, requestError(-32602, "params must be an object", id, nil)
		}
		if err := json.Unmarshal(raw.Params, &params); err != nil || params == nil {
			return Request{}, requestError(-32602, "params must be an object", id, err)
		}
	}
	return Request{JSONRPC: version, ID: id, Method: method, Params: params}, nil
}

func InvalidParams(id any, message string) *RequestError {
	return requestError(-32602, message, id, nil)
}

func requestError(code int, message string, id any, cause error) *RequestError {
	return &RequestError{Code: code, Message: message, ID: id, Cause: cause}
}

func decodeID(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("id must be a string or integer")
	}
	if len(raw) > maxIDBytes {
		return nil, fmt.Errorf("id exceeds %d-byte limit", maxIDBytes)
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid id: %w", err)
	}
	switch id := value.(type) {
	case string:
		return id, nil
	case json.Number:
		if !integerIDPattern.MatchString(id.String()) {
			return nil, errors.New("id must be a string or integer")
		}
		return id, nil
	default:
		return nil, errors.New("id must be a string or integer")
	}
}
