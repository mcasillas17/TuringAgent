package safejson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"unicode/utf8"

	"google.golang.org/protobuf/types/known/structpb"
)

func DecodeObject(decoder *json.Decoder) (map[string]any, error) {
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("expected JSON object")
	}
	return obj, nil
}

func Normalize(value any) (any, error) {
	switch v := value.(type) {
	case nil, bool, string:
		return v, nil
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, nil
		}
		f, err := v.Float64()
		if err != nil {
			return nil, err
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, errors.New("unsupported non-finite number")
		}
		return f, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, errors.New("unsupported non-finite number")
		}
		return v, nil
	case float32:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, errors.New("unsupported non-finite number")
		}
		return f, nil
	case int, int8, int16, int32, int64, uint8, uint16, uint32:
		return v, nil
	case uint:
		if uint64(v) > math.MaxInt64 {
			return nil, errors.New("uint exceeds supported range")
		}
		return v, nil
	case uint64:
		if v > math.MaxInt64 {
			return nil, errors.New("uint64 exceeds supported range")
		}
		return v, nil
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			normalized, err := Normalize(item)
			if err != nil {
				return nil, err
			}
			out = append(out, normalized)
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			normalized, err := Normalize(item)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			out[key] = normalized
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported JSON value %T", value)
	}
}

func MarshalCanonical(value any) ([]byte, error) {
	normalized, err := Normalize(value)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(normalized); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func ToStruct(value map[string]any) (*structpb.Struct, error) {
	normalized, err := Normalize(value)
	if err != nil {
		return nil, err
	}
	obj, ok := normalized.(map[string]any)
	if !ok {
		return nil, errors.New("expected normalized object")
	}
	return structpb.NewStruct(obj)
}

func Summary(value any, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	normalized, err := Normalize(value)
	if err != nil {
		return truncate(`{"error":"unserializable"}`, maxBytes)
	}
	data, err := json.Marshal(canonical(normalized))
	if err != nil {
		return truncate(`{"error":"unserializable"}`, maxBytes)
	}
	return truncate(string(data), maxBytes)
}

func truncate(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	if maxBytes <= 0 {
		return ""
	}
	if maxBytes <= 3 {
		return validUTF8Prefix(value, maxBytes)
	}
	return validUTF8Prefix(value, maxBytes-3) + "..."
}

func validUTF8Prefix(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	for maxBytes > 0 && !utf8.RuneStart(value[maxBytes]) {
		maxBytes--
	}
	return value[:maxBytes]
}

func canonical(value any) any {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyBytes, _ := json.Marshal(key)
			valueBytes, _ := json.Marshal(canonical(v[key]))
			buf.Write(keyBytes)
			buf.WriteByte(':')
			buf.Write(valueBytes)
		}
		buf.WriteByte('}')
		var out any
		_ = json.Unmarshal(buf.Bytes(), &out)
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, canonical(item))
		}
		return out
	default:
		return v
	}
}
