package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/mcp"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/safejson"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

// ToolLister discovers and invokes tools for an MCP server.
type ToolLister interface {
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

type toolOrigin struct {
	serverName  string
	serverIndex int
	listIndex   int
}

type ToolDiscoveryError struct {
	err       error
	retryable bool
}

func (e ToolDiscoveryError) Error() string   { return e.err.Error() }
func (e ToolDiscoveryError) Unwrap() error   { return e.err }
func (e ToolDiscoveryError) Retryable() bool { return e.retryable }

func ToolDiscoveryRetryable(err error) bool {
	var discoveryErr interface{ Retryable() bool }
	if errors.As(err, &discoveryErr) {
		return discoveryErr.Retryable()
	}
	return true
}

func permanentToolDiscoveryError(err error) error {
	return ToolDiscoveryError{err: err}
}

func retryableToolDiscoveryError(err error) error {
	return ToolDiscoveryError{err: err, retryable: true}
}

// BuildToolRegistry discovers servers in name order and preserves each server's tool order.
func BuildToolRegistry(ctx context.Context, servers map[string]ToolLister) (*ToolRegistry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	serverNames := make([]string, 0, len(servers))
	for serverName := range servers {
		serverNames = append(serverNames, serverName)
	}
	sort.Strings(serverNames)

	registry := &ToolRegistry{
		entries: make(map[string]ToolEntry),
	}
	origins := make(map[string]toolOrigin)
	for serverIndex, serverName := range serverNames {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("discover tools from MCP server %q: %w", serverName, err)
		}
		client := servers[serverName]
		if isNilToolLister(client) {
			return nil, permanentToolDiscoveryError(fmt.Errorf("MCP server %q has nil client", serverName))
		}

		discovered, err := client.ListTools(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			wrapped := fmt.Errorf("discover tools from MCP server %q: %w", serverName, err)
			var classified mcp.RetryableError
			if errors.As(err, &classified) && !mcp.Retryable(err) {
				return nil, permanentToolDiscoveryError(wrapped)
			}
			return nil, retryableToolDiscoveryError(wrapped)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		for index, raw := range discovered {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			nameValue, present := raw["name"]
			name, valid := nameValue.(string)
			if !present || !valid || strings.TrimSpace(name) == "" {
				return nil, permanentToolDiscoveryError(fmt.Errorf("MCP server %q tool %d has invalid name: must be a non-blank string", serverName, index))
			}

			description := ""
			if value, present := raw["description"]; present {
				var valid bool
				description, valid = value.(string)
				if !valid {
					return nil, permanentToolDiscoveryError(fmt.Errorf("MCP server %q tool %q has invalid description: must be a string", serverName, name))
				}
			}

			parameters := map[string]any{"type": "object"}
			if value, present := raw["inputSchema"]; present && value != nil {
				var valid bool
				parameters, valid = value.(map[string]any)
				if !valid {
					return nil, permanentToolDiscoveryError(fmt.Errorf("MCP server %q tool %q has invalid inputSchema: must be an object", serverName, name))
				}
				if parameters == nil {
					parameters = map[string]any{"type": "object"}
				} else {
					parameters, err = normalizeJSONMap(parameters)
					if err != nil {
						if ctxErr := ctx.Err(); ctxErr != nil {
							return nil, ctxErr
						}
						return nil, permanentToolDiscoveryError(fmt.Errorf("MCP server %q tool %q has invalid inputSchema: %w", serverName, name, err))
					}
					rootType, present := parameters["type"]
					if !present {
						parameters["type"] = "object"
					} else if rootTypeString, valid := rootType.(string); !valid || rootTypeString != "object" {
						return nil, permanentToolDiscoveryError(fmt.Errorf(
							"MCP server %q tool %d %q has invalid inputSchema root type: must be string %q",
							serverName,
							index,
							name,
							"object",
						))
					}
				}
			}

			if original, duplicate := origins[name]; duplicate {
				return nil, permanentToolDiscoveryError(fmt.Errorf(
					"tool %q at MCP server %q (server index %d, list index %d) duplicates original at MCP server %q (server index %d, list index %d)",
					name,
					serverName,
					serverIndex,
					index,
					original.serverName,
					original.serverIndex,
					original.listIndex,
				))
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
			origins[name] = toolOrigin{
				serverName:  serverName,
				serverIndex: serverIndex,
				listIndex:   index,
			}
			registry.definitions = append(registry.definitions, definition)
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return registry, nil
}

func (r *ToolRegistry) Definitions() []llm.ToolDefinition {
	definitions := make([]llm.ToolDefinition, len(r.definitions))
	for index, definition := range r.definitions {
		definition.Parameters = cloneNormalizedJSONMap(definition.Parameters)
		definitions[index] = definition
	}
	return definitions
}

func (r *ToolRegistry) Lookup(name string) (ToolEntry, bool) {
	entry, ok := r.entries[name]
	if ok {
		entry.Definition.Parameters = cloneNormalizedJSONMap(entry.Definition.Parameters)
	}
	return entry, ok
}

func normalizeJSONMap(source map[string]any) (map[string]any, error) {
	data, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("normalize JSON object: %w", err)
	}
	normalized, err := safejson.DecodeObject(json.NewDecoder(bytes.NewReader(data)))
	if err != nil {
		return nil, fmt.Errorf("normalize JSON object: %w", err)
	}
	return normalized, nil
}

func cloneNormalizedJSONMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneNormalizedJSONValue(value)
	}
	return cloned
}

func cloneNormalizedJSONValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneNormalizedJSONMap(value)
	case []any:
		cloned := make([]any, len(value))
		for index, item := range value {
			cloned[index] = cloneNormalizedJSONValue(item)
		}
		return cloned
	default:
		return value
	}
}

func isNilToolLister(client ToolLister) bool {
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
