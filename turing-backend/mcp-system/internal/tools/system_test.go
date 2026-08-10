package tools

import (
	"reflect"
	"strings"
	"testing"
)

func TestCallSystemTime(t *testing.T) {
	result, err := Call("system.time", map[string]any{})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if result["iso"] == "" {
		t.Fatalf("expected iso timestamp")
	}
}

func TestCallRejectsArgumentsForZeroArgumentTools(t *testing.T) {
	for _, name := range []string{"system.health", "system.time", "system.info"} {
		t.Run(name, func(t *testing.T) {
			if _, err := Call(name, map[string]any{"unexpected": true}); err == nil {
				t.Fatalf("Call(%q) accepted an argument outside its schema", name)
			}
		})
	}
}

func TestCallEchoEnforcesAdvertisedSchema(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"unknown argument": {"text": "hello", "unexpected": true},
		"non-string text":  {"text": 123},
		"null text":        {"text": nil},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Call("system.echo", args); err == nil {
				t.Fatalf("Call(system.echo, %#v) accepted invalid arguments", args)
			}
		})
	}

	result, err := Call("system.echo", map[string]any{})
	if err != nil {
		t.Fatalf("Call(system.echo) returned error for omitted text: %v", err)
	}
	if result["text"] != "" {
		t.Fatalf("omitted echo text = %#v, want empty string", result["text"])
	}
}

func TestSystemInfoDoesNotExposeSecrets(t *testing.T) {
	result, err := Call("system.info", map[string]any{})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if _, ok := result["env"]; ok {
		t.Fatalf("system.info must not expose env")
	}
}

func TestListAdvertisesDocumentedObjectSchemas(t *testing.T) {
	advertised := List()
	if len(advertised) != 4 {
		t.Fatalf("List returned %d tools, want 4", len(advertised))
	}
	for _, tool := range advertised {
		name, _ := tool["name"].(string)
		description, _ := tool["description"].(string)
		if strings.TrimSpace(description) == "" {
			t.Errorf("%s description is empty", name)
		}
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok || schema["type"] != "object" {
			t.Errorf("%s inputSchema = %#v, want object root", name, tool["inputSchema"])
			continue
		}
		if schema["additionalProperties"] != false {
			t.Errorf("%s additionalProperties = %#v, want false", name, schema["additionalProperties"])
		}
		if tool["policy"] != "safe" {
			t.Errorf("%s policy = %#v, want safe", name, tool["policy"])
		}
		if name == "system.echo" {
			want := map[string]any{"text": map[string]any{"type": "string"}}
			if !reflect.DeepEqual(schema["properties"], want) {
				t.Errorf("system.echo properties = %#v, want %#v", schema["properties"], want)
			}
		} else if properties, ok := schema["properties"].(map[string]any); !ok || len(properties) != 0 {
			t.Errorf("%s properties = %#v, want empty object", name, schema["properties"])
		}
	}
}
