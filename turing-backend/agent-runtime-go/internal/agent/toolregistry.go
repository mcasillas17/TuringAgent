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
			if value, present := raw["policy"]; present {
				policy, valid := value.(string)
				if !valid {
					return nil, permanentToolDiscoveryError(fmt.Errorf("MCP server %q tool %q has invalid policy: must be a string", serverName, name))
				}
				switch policy {
				case "safe", "approval_required":
				case "disabled":
					continue
				default:
					return nil, permanentToolDiscoveryError(fmt.Errorf("MCP server %q tool %q has invalid policy %q", serverName, name, policy))
				}
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
					if ctxErr := ctx.Err(); ctxErr != nil {
						return nil, ctxErr
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
					if err := validateInputSchemaShape(parameters); err != nil {
						return nil, permanentToolDiscoveryError(fmt.Errorf(
							"MCP server %q tool %q has invalid inputSchema: %w",
							serverName,
							name,
							err,
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

func validateInputSchemaShape(schema map[string]any) error {
	var properties map[string]any
	if rawProperties, present := schema["properties"]; present {
		var ok bool
		properties, ok = rawProperties.(map[string]any)
		if !ok {
			return errors.New("properties must be an object")
		}
		for name, rawDefinition := range properties {
			definition, ok := rawDefinition.(map[string]any)
			if !ok {
				return fmt.Errorf("property %q definition must be an object", name)
			}
			if err := validateInputSchemaShape(definition); err != nil {
				return fmt.Errorf("property %q: %w", name, err)
			}
		}
	}

	if rawRequired, present := schema["required"]; present {
		required, ok := rawRequired.([]any)
		if !ok {
			return errors.New("required must be an array")
		}
		for index, rawName := range required {
			name, ok := rawName.(string)
			if !ok {
				return fmt.Errorf("required entry %d must be a string", index)
			}
			if _, present := properties[name]; !present {
				return fmt.Errorf("required name %q is absent from properties", name)
			}
		}
	}

	if additionalProperties, present := schema["additionalProperties"]; present {
		switch value := additionalProperties.(type) {
		case bool:
		case map[string]any:
			if err := validateInputSchemaShape(value); err != nil {
				return fmt.Errorf("additionalProperties: %w", err)
			}
		default:
			return errors.New("additionalProperties must be a boolean or schema object")
		}
	}

	for _, keyword := range []string{"items", "not", "if", "then", "else"} {
		rawNested, present := schema[keyword]
		if !present {
			continue
		}
		switch nested := rawNested.(type) {
		case bool:
		case map[string]any:
			if err := validateInputSchemaShape(nested); err != nil {
				return fmt.Errorf("%s: %w", keyword, err)
			}
		default:
			return fmt.Errorf("%s must be a boolean or schema object", keyword)
		}
	}

	for _, keyword := range []string{"prefixItems", "oneOf", "anyOf", "allOf"} {
		rawNested, present := schema[keyword]
		if !present {
			continue
		}
		nested, ok := rawNested.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array of boolean or schema objects", keyword)
		}
		for index, rawEntry := range nested {
			switch entry := rawEntry.(type) {
			case bool:
			case map[string]any:
				if err := validateInputSchemaShape(entry); err != nil {
					return fmt.Errorf("%s entry %d: %w", keyword, index, err)
				}
			default:
				return fmt.Errorf("%s entry %d must be a boolean or schema object", keyword, index)
			}
		}
	}

	if rawDefinitions, present := schema["$defs"]; present {
		definitions, ok := rawDefinitions.(map[string]any)
		if !ok {
			return errors.New("$defs must be an object of boolean or schema values")
		}
		for name, rawDefinition := range definitions {
			switch definition := rawDefinition.(type) {
			case bool:
			case map[string]any:
				if err := validateInputSchemaShape(definition); err != nil {
					return fmt.Errorf("$defs entry %q: %w", name, err)
				}
			default:
				return fmt.Errorf("$defs entry %q must be a boolean or schema object", name)
			}
		}
	}
	return nil
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
