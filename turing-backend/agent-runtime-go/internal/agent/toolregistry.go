package agent

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

type toolLister interface {
	tools.MCPClient
	ListTools(ctx context.Context) ([]map[string]any, error)
}

type ToolEntry struct {
	ServerName string
	Client     tools.MCPClient
	Definition llm.ToolDefinition
}

type ToolRegistry struct {
	definitions []llm.ToolDefinition
	entries     map[string]ToolEntry
}

// BuildToolRegistry discovers servers in name order and preserves each server's tool order.
func BuildToolRegistry(ctx context.Context, servers map[string]toolLister) (*ToolRegistry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	serverNames := make([]string, 0, len(servers))
	for serverName := range servers {
		serverNames = append(serverNames, serverName)
	}
	sort.Strings(serverNames)

	registry := &ToolRegistry{entries: make(map[string]ToolEntry)}
	for _, serverName := range serverNames {
		client := servers[serverName]
		if isNilToolLister(client) {
			return nil, fmt.Errorf("MCP server %q has nil client", serverName)
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("discover tools from MCP server %q: %w", serverName, err)
		}

		discovered, err := client.ListTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("discover tools from MCP server %q: %w", serverName, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("discover tools from MCP server %q: %w", serverName, err)
		}

		for index, raw := range discovered {
			nameValue, present := raw["name"]
			name, valid := nameValue.(string)
			if !present || !valid || strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("MCP server %q tool %d has invalid name: must be a non-blank string", serverName, index)
			}

			description := ""
			if value, present := raw["description"]; present {
				var valid bool
				description, valid = value.(string)
				if !valid {
					return nil, fmt.Errorf("MCP server %q tool %q has invalid description: must be a string", serverName, name)
				}
			}

			parameters := map[string]any{"type": "object"}
			if value, present := raw["inputSchema"]; present && value != nil {
				var valid bool
				parameters, valid = value.(map[string]any)
				if !valid {
					return nil, fmt.Errorf("MCP server %q tool %q has invalid inputSchema: must be an object", serverName, name)
				}
				if parameters == nil {
					parameters = map[string]any{"type": "object"}
				}
			}

			if existing, duplicate := registry.entries[name]; duplicate {
				return nil, fmt.Errorf("tool %q from MCP server %q duplicates tool from MCP server %q", name, serverName, existing.ServerName)
			}

			definition := llm.ToolDefinition{
				Name:        name,
				Description: description,
				Parameters:  parameters,
			}
			registry.entries[name] = ToolEntry{
				ServerName: serverName,
				Client:     client,
				Definition: definition,
			}
			registry.definitions = append(registry.definitions, definition)
		}
	}

	return registry, nil
}

func (r *ToolRegistry) Definitions() []llm.ToolDefinition {
	definitions := make([]llm.ToolDefinition, len(r.definitions))
	copy(definitions, r.definitions)
	return definitions
}

func (r *ToolRegistry) Lookup(name string) (ToolEntry, bool) {
	entry, ok := r.entries[name]
	return entry, ok
}

func isNilToolLister(client toolLister) bool {
	if client == nil {
		return true
	}
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
