package tools

import (
	"errors"
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
		return map[string]any{"ok": true, "service": "turing-mcp-system"}, nil
	case "system.time":
		now := time.Now().UTC()
		return map[string]any{"iso": now.Format(time.RFC3339Nano), "unixMs": now.UnixMilli(), "timezone": "UTC"}, nil
	case "system.echo":
		text, _ := args["text"].(string)
		return map[string]any{"text": text}, nil
	case "system.info":
		return map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH, "runtime": runtime.Version()}, nil
	default:
		return nil, errors.New("unknown tool")
	}
}
