package tools

import (
	"errors"
	"fmt"
	"runtime"
	"time"
)

func List() []map[string]any {
	emptySchema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
	return []map[string]any{
		{"name": "system.health", "description": "Check whether the system MCP service is healthy.", "inputSchema": emptySchema, "policy": "safe"},
		{"name": "system.time", "description": "Return the current UTC time.", "inputSchema": emptySchema, "policy": "safe"},
		{
			"name":        "system.echo",
			"description": "Echo the supplied text, or an empty string when text is omitted.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"text": map[string]any{"type": "string"}},
				"additionalProperties": false,
			},
			"policy": "safe",
		},
		{"name": "system.info", "description": "Return operating system, architecture, and Go runtime information.", "inputSchema": emptySchema, "policy": "safe"},
	}
}

func Call(name string, args map[string]any) (map[string]any, error) {
	switch name {
	case "system.health":
		if err := rejectArguments(args); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "service": "turing-mcp-system"}, nil
	case "system.time":
		if err := rejectArguments(args); err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		return map[string]any{"iso": now.Format(time.RFC3339Nano), "unixMs": now.UnixMilli(), "timezone": "UTC"}, nil
	case "system.echo":
		for argument := range args {
			if argument != "text" {
				return nil, fmt.Errorf("unknown argument %q", argument)
			}
		}
		text := ""
		if value, present := args["text"]; present {
			var valid bool
			text, valid = value.(string)
			if !valid {
				return nil, errors.New("text must be a string")
			}
		}
		return map[string]any{"text": text}, nil
	case "system.info":
		if err := rejectArguments(args); err != nil {
			return nil, err
		}
		return map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH, "runtime": runtime.Version()}, nil
	default:
		return nil, errors.New("unknown tool")
	}
}

func rejectArguments(args map[string]any) error {
	for argument := range args {
		return fmt.Errorf("unknown argument %q", argument)
	}
	return nil
}
