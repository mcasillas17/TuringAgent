package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
)

func TestBuildRegistryDiscoversToolsAndSupportsLookup(t *testing.T) {
	alpha := &registryTestClient{tools: []map[string]any{
		{
			"name":        "alpha_first",
			"description": "first alpha tool",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"value": map[string]any{"type": "string"}},
			},
		},
		{"name": "alpha_second"},
	}}
	beta := &registryTestClient{tools: []map[string]any{
		{"name": "beta_only", "description": "beta tool", "inputSchema": nil},
	}}

	registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"beta":  beta,
		"alpha": alpha,
	})
	if err != nil {
		t.Fatalf("BuildToolRegistry returned error: %v", err)
	}

	if alpha.listCalls != 1 || beta.listCalls != 1 {
		t.Fatalf("ListTools calls = alpha:%d beta:%d, want one each", alpha.listCalls, beta.listCalls)
	}

	definitions := registry.Definitions()
	if got, want := definitionNames(definitions), []string{"alpha_first", "alpha_second", "beta_only"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("definition names = %v, want %v", got, want)
	}
	if definitions[0].Description != "first alpha tool" {
		t.Fatalf("description = %q, want first alpha tool", definitions[0].Description)
	}
	if got := definitions[0].Parameters["type"]; got != "object" {
		t.Fatalf("custom schema type = %v, want object", got)
	}
	for _, index := range []int{1, 2} {
		if !reflect.DeepEqual(definitions[index].Parameters, map[string]any{"type": "object"}) {
			t.Fatalf("default schema at index %d = %#v", index, definitions[index].Parameters)
		}
	}

	entry, ok := registry.Lookup("alpha_first")
	if !ok {
		t.Fatal("Lookup(alpha_first) returned false")
	}
	if entry.ServerName != "alpha" || entry.Client != alpha || entry.Definition.Name != "alpha_first" {
		t.Fatalf("lookup entry = %#v, want alpha server/client/definition", entry)
	}
	if _, ok := registry.Lookup("unknown"); ok {
		t.Fatal("Lookup(unknown) returned true")
	}
}

func TestBuildRegistryRetainsProductionLikeCatalogMetadata(t *testing.T) {
	systemSchema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
	filesSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
		"required":             []any{"path", "content"},
		"additionalProperties": false,
	}
	registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"system": &registryTestClient{tools: []map[string]any{{
			"name": "system.info", "description": "Return runtime information.", "inputSchema": systemSchema,
		}}},
		"files": &registryTestClient{tools: []map[string]any{{
			"name": "files.create", "description": "Create a UTF-8 text file.", "inputSchema": filesSchema,
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	definitions := registry.Definitions()
	if len(definitions) != 2 {
		t.Fatalf("Definitions length = %d, want 2", len(definitions))
	}
	byName := make(map[string]llm.ToolDefinition, len(definitions))
	for _, definition := range definitions {
		byName[definition.Name] = definition
	}
	if got := byName["system.info"]; got.Description != "Return runtime information." || !reflect.DeepEqual(got.Parameters, systemSchema) {
		t.Fatalf("system.info definition = %#v", got)
	}
	if got := byName["files.create"]; got.Description != "Create a UTF-8 text file." || !reflect.DeepEqual(got.Parameters, filesSchema) {
		t.Fatalf("files.create definition = %#v", got)
	}
}

func TestBuildRegistryAdvertisesDeterministically(t *testing.T) {
	want := []string{"a_second", "a_first", "z_only"}
	for iteration := 0; iteration < 50; iteration++ {
		servers := make(map[string]ToolLister)
		if iteration%2 == 0 {
			servers["z-server"] = &registryTestClient{tools: []map[string]any{{"name": "z_only"}}}
			servers["a-server"] = &registryTestClient{tools: []map[string]any{{"name": "a_second"}, {"name": "a_first"}}}
		} else {
			servers["a-server"] = &registryTestClient{tools: []map[string]any{{"name": "a_second"}, {"name": "a_first"}}}
			servers["z-server"] = &registryTestClient{tools: []map[string]any{{"name": "z_only"}}}
		}

		registry, err := BuildToolRegistry(context.Background(), servers)
		if err != nil {
			t.Fatalf("iteration %d: BuildToolRegistry returned error: %v", iteration, err)
		}
		if got := definitionNames(registry.Definitions()); !reflect.DeepEqual(got, want) {
			t.Fatalf("iteration %d: definition names = %v, want %v", iteration, got, want)
		}
	}
}

func TestBuildRegistryReturnsUsableEmptyRegistry(t *testing.T) {
	tests := map[string]map[string]ToolLister{
		"empty server map": {},
		"server with no tools": {
			"empty": &registryTestClient{},
		},
	}
	for name, servers := range tests {
		t.Run(name, func(t *testing.T) {
			registry, err := BuildToolRegistry(context.Background(), servers)
			if err != nil {
				t.Fatalf("BuildToolRegistry returned error: %v", err)
			}
			if registry == nil {
				t.Fatal("BuildToolRegistry returned nil registry")
			}
			if definitions := registry.Definitions(); len(definitions) != 0 {
				t.Fatalf("Definitions = %v, want empty", definitions)
			}
			if _, ok := registry.Lookup("missing"); ok {
				t.Fatal("Lookup on empty registry returned true")
			}
		})
	}
}

func TestBuildRegistryDoesNotAdvertiseToolsMarkedDisabled(t *testing.T) {
	registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"server": &registryTestClient{tools: []map[string]any{
			{"name": "safe", "policy": "safe"},
			{"name": "disabled", "policy": "disabled"},
			{"name": "approval", "policy": "approval_required"},
		}},
	})
	if err != nil {
		t.Fatalf("BuildToolRegistry returned error: %v", err)
	}
	if got, want := definitionNames(registry.Definitions()), []string{"safe", "approval"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("definition names = %v, want %v", got, want)
	}
	if _, exists := registry.Lookup("disabled"); exists {
		t.Fatal("disabled tool was callable")
	}
}

func TestBuildRegistryRejectsUnknownAdvertisedPolicy(t *testing.T) {
	_, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"server": &registryTestClient{tools: []map[string]any{{
			"name": "unknown-policy", "policy": "maybe",
		}}},
	})
	if err == nil || ToolDiscoveryRetryable(err) ||
		!strings.Contains(err.Error(), "invalid policy") {
		t.Fatalf("BuildToolRegistry error = %T %v, want permanent invalid policy error", err, err)
	}
}

func TestBuildRegistryDefinitionsReturnsFreshSlice(t *testing.T) {
	registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"server": &registryTestClient{tools: []map[string]any{{"name": "original"}}},
	})
	if err != nil {
		t.Fatalf("BuildToolRegistry returned error: %v", err)
	}

	first := registry.Definitions()
	first[0].Name = "mutated"
	second := registry.Definitions()
	if second[0].Name != "original" {
		t.Fatalf("second Definitions name = %q, want original", second[0].Name)
	}
}

func TestBuildRegistryDeepCopiesSourceSchema(t *testing.T) {
	schema := nestedRegistrySchema()
	client := &registryTestClient{tools: []map[string]any{{
		"name":        "isolated",
		"inputSchema": schema,
	}}}
	registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{"server": client})
	if err != nil {
		t.Fatalf("BuildToolRegistry returned error: %v", err)
	}

	mutateNestedRegistrySchema(schema)

	assertNestedRegistrySchemaUnchanged(t, registry.Definitions()[0].Parameters)
	entry, ok := registry.Lookup("isolated")
	if !ok {
		t.Fatal("Lookup(isolated) returned false")
	}
	assertNestedRegistrySchemaUnchanged(t, entry.Definition.Parameters)
	if entry.Client != client {
		t.Fatal("Lookup did not preserve client identity")
	}
}

func TestBuildRegistryDeepCopiesTypedNestedSourceSchema(t *testing.T) {
	properties := map[string]map[string]any{
		"choice": {
			"type": "string",
			"enum": []string{"alpha", "beta"},
		},
	}
	required := []string{"choice"}
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
	registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"server": &registryTestClient{tools: []map[string]any{{
			"name":        "typed",
			"inputSchema": schema,
		}}},
	})
	if err != nil {
		t.Fatalf("BuildToolRegistry returned error: %v", err)
	}

	properties["choice"]["type"] = "number"
	properties["choice"]["enum"].([]string)[0] = "mutated"
	required[0] = "mutated"

	assertNestedRegistrySchemaUnchanged(t, registry.Definitions()[0].Parameters)
}

func TestToolRegistryAccessorsIsolateNormalizedTypedNestedSchemas(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]map[string]any{
			"choice": {
				"type": "string",
				"enum": []string{"alpha", "beta"},
			},
		},
		"required": []string{"choice"},
	}
	registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"server": &registryTestClient{tools: []map[string]any{{
			"name":        "typed",
			"inputSchema": schema,
		}}},
	})
	if err != nil {
		t.Fatalf("BuildToolRegistry returned error: %v", err)
	}

	first := registry.Definitions()[0].Parameters
	mutateNestedRegistrySchema(first)
	entry, ok := registry.Lookup("typed")
	if !ok {
		t.Fatal("Lookup(typed) returned false")
	}
	assertNestedRegistrySchemaUnchanged(t, entry.Definition.Parameters)
	assertNestedRegistrySchemaUnchanged(t, registry.Definitions()[0].Parameters)
}

func TestBuildRegistryRejectsUnsupportedJSONValuesInInputSchema(t *testing.T) {
	tests := map[string]any{
		"channel":  make(chan int),
		"function": func() {},
		"complex":  complex(1, 2),
	}
	for name, unsupported := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
				"invalid-server": &registryTestClient{tools: []map[string]any{{
					"name": "unsupported_schema",
					"inputSchema": map[string]any{
						"type":       "object",
						"properties": map[string]any{"bad": unsupported},
					},
				}}},
			})
			if err == nil {
				t.Fatal("BuildToolRegistry returned nil error")
			}
			assertErrorContains(t, err, "invalid-server", "unsupported_schema", "inputSchema", "unsupported")
		})
	}
}

func TestToolRegistryDefinitionsDeepCopiesSchema(t *testing.T) {
	registry := buildNestedSchemaRegistry(t)

	first := registry.Definitions()
	mutateNestedRegistrySchema(first[0].Parameters)

	assertNestedRegistrySchemaUnchanged(t, registry.Definitions()[0].Parameters)
	entry, ok := registry.Lookup("isolated")
	if !ok {
		t.Fatal("Lookup(isolated) returned false")
	}
	assertNestedRegistrySchemaUnchanged(t, entry.Definition.Parameters)
}

func TestToolRegistryLookupDeepCopiesSchema(t *testing.T) {
	registry := buildNestedSchemaRegistry(t)

	first, ok := registry.Lookup("isolated")
	if !ok {
		t.Fatal("Lookup(isolated) returned false")
	}
	mutateNestedRegistrySchema(first.Definition.Parameters)

	second, ok := registry.Lookup("isolated")
	if !ok {
		t.Fatal("second Lookup(isolated) returned false")
	}
	assertNestedRegistrySchemaUnchanged(t, second.Definition.Parameters)
	assertNestedRegistrySchemaUnchanged(t, registry.Definitions()[0].Parameters)
}

func TestToolRegistryConcurrentAccessorsReturnIsolatedSchemas(t *testing.T) {
	registry := buildNestedSchemaRegistry(t)

	const goroutines = 32
	var wait sync.WaitGroup
	wait.Add(goroutines)
	for index := 0; index < goroutines; index++ {
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 100; iteration++ {
				definitions := registry.Definitions()
				assertNestedRegistrySchemaUnchanged(t, definitions[0].Parameters)
				mutateNestedRegistrySchema(definitions[0].Parameters)

				entry, ok := registry.Lookup("isolated")
				if !ok {
					t.Error("Lookup(isolated) returned false")
					return
				}
				assertNestedRegistrySchemaUnchanged(t, entry.Definition.Parameters)
				mutateNestedRegistrySchema(entry.Definition.Parameters)
			}
		}()
	}
	wait.Wait()

	assertNestedRegistrySchemaUnchanged(t, registry.Definitions()[0].Parameters)
}

func TestBuildRegistryRejectsInvalidToolNames(t *testing.T) {
	tests := map[string]map[string]any{
		"missing":    {},
		"null":       {"name": nil},
		"blank":      {"name": " \t"},
		"non-string": {"name": 42},
	}
	for name, tool := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
				"invalid-server": &registryTestClient{tools: []map[string]any{tool}},
			})
			if err == nil {
				t.Fatal("BuildToolRegistry returned nil error")
			}
			assertErrorContains(t, err, "invalid-server", "tool 0", "name")
		})
	}
}

func TestBuildRegistryRejectsInvalidDescription(t *testing.T) {
	_, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"invalid-server": &registryTestClient{tools: []map[string]any{{
			"name":        "bad_description",
			"description": 7,
		}}},
	})
	if err == nil {
		t.Fatal("BuildToolRegistry returned nil error")
	}
	assertErrorContains(t, err, "invalid-server", "bad_description", "description")
}

func TestBuildRegistryRejectsInvalidInputSchema(t *testing.T) {
	tests := map[string]any{
		"string": "not an object",
		"array":  []any{},
		"number": 12,
	}
	for name, schema := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
				"invalid-server": &registryTestClient{tools: []map[string]any{{
					"name":        "bad_schema",
					"inputSchema": schema,
				}}},
			})
			if err == nil {
				t.Fatal("BuildToolRegistry returned nil error")
			}
			assertErrorContains(t, err, "invalid-server", "bad_schema", "inputSchema")
		})
	}
}

func TestBuildRegistryNormalizesMissingInputSchemaRootType(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{"value": map[string]any{"type": "string"}},
	}
	registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"server": &registryTestClient{tools: []map[string]any{{
			"name":        "normalized",
			"inputSchema": schema,
		}}},
	})
	if err != nil {
		t.Fatalf("BuildToolRegistry returned error: %v", err)
	}
	if got := registry.Definitions()[0].Parameters["type"]; got != "object" {
		t.Fatalf("normalized root type = %#v, want object", got)
	}
	if _, mutated := schema["type"]; mutated {
		t.Fatal("normalization mutated source schema")
	}
}

func TestBuildRegistryDefaultsNilInputSchemas(t *testing.T) {
	var typedNil map[string]any
	tests := map[string]map[string]any{
		"missing":   {"name": "defaulted"},
		"nil":       {"name": "defaulted", "inputSchema": nil},
		"typed nil": {"name": "defaulted", "inputSchema": typedNil},
	}
	for name, tool := range tests {
		t.Run(name, func(t *testing.T) {
			registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
				"server": &registryTestClient{tools: []map[string]any{tool}},
			})
			if err != nil {
				t.Fatalf("BuildToolRegistry returned error: %v", err)
			}
			if got, want := registry.Definitions()[0].Parameters, map[string]any{"type": "object"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("Parameters = %#v, want %#v", got, want)
			}
		})
	}
}

func TestBuildRegistryAcceptsExplicitObjectInputSchemaRootType(t *testing.T) {
	registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"server": &registryTestClient{tools: []map[string]any{{
			"name":        "object_schema",
			"inputSchema": map[string]any{"type": "object"},
		}}},
	})
	if err != nil {
		t.Fatalf("BuildToolRegistry returned error: %v", err)
	}
	if got := registry.Definitions()[0].Parameters["type"]; got != "object" {
		t.Fatalf("root type = %#v, want object", got)
	}
}

func TestBuildRegistryRejectsInvalidInputSchemaRootType(t *testing.T) {
	tests := map[string]any{
		"null":        nil,
		"boolean":     true,
		"number":      12,
		"array":       []any{"object"},
		"map":         map[string]any{},
		"array type":  "array",
		"string type": "string",
	}
	for name, rootType := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
				"schema-server": &registryTestClient{tools: []map[string]any{
					{"name": "first"},
					{
						"name":        "bad_schema",
						"inputSchema": map[string]any{"type": rootType},
					},
				}},
			})
			if err == nil {
				t.Fatal("BuildToolRegistry returned nil error")
			}
			assertErrorContains(t, err, "schema-server", "bad_schema", "tool 1", "type", "object")
		})
	}
}

func TestBuildRegistryRejectsDuplicateToolNames(t *testing.T) {
	t.Run("within server", func(t *testing.T) {
		_, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
			"same-server": &registryTestClient{tools: []map[string]any{
				{"name": "duplicate"},
				{"name": "duplicate"},
			}},
		})
		if err == nil {
			t.Fatal("BuildToolRegistry returned nil error")
		}
		want := `tool "duplicate" at MCP server "same-server" (server index 0, list index 1) duplicates original at MCP server "same-server" (server index 0, list index 0)`
		if err.Error() != want {
			t.Fatalf("error = %q, want %q", err, want)
		}
	})

	t.Run("across servers", func(t *testing.T) {
		_, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
			"first-server": &registryTestClient{tools: []map[string]any{
				{"name": "first"},
				{"name": "duplicate"},
			}},
			"second-server": &registryTestClient{tools: []map[string]any{
				{"name": "second"},
				{"name": "another"},
				{"name": "duplicate"},
			}},
		})
		if err == nil {
			t.Fatal("BuildToolRegistry returned nil error")
		}
		want := `tool "duplicate" at MCP server "second-server" (server index 1, list index 2) duplicates original at MCP server "first-server" (server index 0, list index 1)`
		if err.Error() != want {
			t.Fatalf("error = %q, want %q", err, want)
		}
	})
}

func TestBuildRegistryRejectsNilClient(t *testing.T) {
	t.Run("nil interface", func(t *testing.T) {
		_, err := BuildToolRegistry(context.Background(), map[string]ToolLister{"nil-server": nil})
		if err == nil {
			t.Fatal("BuildToolRegistry returned nil error")
		}
		assertErrorContains(t, err, "nil-server", "nil")
	})

	t.Run("typed nil", func(t *testing.T) {
		var client *registryTestClient
		_, err := BuildToolRegistry(context.Background(), map[string]ToolLister{"nil-server": client})
		if err == nil {
			t.Fatal("BuildToolRegistry returned nil error")
		}
		assertErrorContains(t, err, "nil-server", "nil")
	})
}

func TestBuildRegistryAddsServerContextToDiscoveryErrors(t *testing.T) {
	discoveryErr := errors.New("discovery failed")
	_, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"broken-server": &registryTestClient{listErr: discoveryErr},
	})
	if !errors.Is(err, discoveryErr) {
		t.Fatalf("error = %v, want wrapped discovery error", err)
	}
	assertErrorContains(t, err, "broken-server")
}

func TestBuildToolRegistryClassifiesValidationAndTransportErrors(t *testing.T) {
	tests := []struct {
		name      string
		servers   map[string]ToolLister
		retryable bool
	}{
		{
			name: "malformed schema is permanent",
			servers: map[string]ToolLister{"server": &registryTestClient{tools: []map[string]any{{
				"name": "bad", "inputSchema": "not-an-object",
			}}}},
			retryable: false,
		},
		{
			name: "duplicate name is permanent",
			servers: map[string]ToolLister{"server": &registryTestClient{tools: []map[string]any{
				{"name": "duplicate"}, {"name": "duplicate"},
			}}},
			retryable: false,
		},
		{
			name:      "nil client is permanent",
			servers:   map[string]ToolLister{"server": nil},
			retryable: false,
		},
		{
			name:      "list transport failure is retryable",
			servers:   map[string]ToolLister{"server": &registryTestClient{listErr: errors.New("transport unavailable")}},
			retryable: true,
		},
		{
			name:      "classified HTTP 401 is permanent",
			servers:   map[string]ToolLister{"server": &registryTestClient{listErr: classifiedListError{message: "MCP HTTP 401"}}},
			retryable: false,
		},
		{
			name: "classified HTTP 500 is retryable",
			servers: map[string]ToolLister{"server": &registryTestClient{listErr: classifiedListError{
				message: "MCP HTTP 500", retryable: true,
			}}},
			retryable: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildToolRegistry(context.Background(), test.servers)
			if err == nil {
				t.Fatal("BuildToolRegistry returned nil error")
			}
			var discoveryErr ToolDiscoveryError
			if !errors.As(err, &discoveryErr) {
				t.Fatalf("error = %T %v, want ToolDiscoveryError", err, err)
			}
			if got := ToolDiscoveryRetryable(err); got != test.retryable {
				t.Fatalf("ToolDiscoveryRetryable(error) = %t, want %t", got, test.retryable)
			}
		})
	}
}

func TestBuildRegistryRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &registryTestClient{}

	_, err := BuildToolRegistry(ctx, map[string]ToolLister{"server": client})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if client.listCalls != 0 {
		t.Fatalf("ListTools calls = %d, want zero for already-canceled context", client.listCalls)
	}
}

func TestBuildRegistryStopsToolNormalizationWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &registryTestClient{tools: []map[string]any{
		{
			"name": "canceling",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"value": cancelingJSONValue{cancel: cancel}},
			},
		},
		{
			"name":        "must_not_normalize",
			"inputSchema": map[string]any{"unsupported": func() {}},
		},
	}}

	_, err := BuildToolRegistry(ctx, map[string]ToolLister{"server": client})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestBuildRegistryStopsServerNormalizationWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	first := &registryTestClient{tools: []map[string]any{{
		"name": "canceling",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"value": cancelingJSONValue{cancel: cancel}},
		},
	}}}
	second := &registryTestClient{}

	_, err := BuildToolRegistry(ctx, map[string]ToolLister{
		"first":  first,
		"second": second,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if second.listCalls != 0 {
		t.Fatalf("second server ListTools calls = %d, want zero", second.listCalls)
	}
}

func TestBuildRegistryChecksCancellationBeforeSuccessfulReturn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &registryTestClient{tools: []map[string]any{{
		"name": "canceling",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"value": cancelingJSONValue{cancel: cancel}},
		},
	}}}

	_, err := BuildToolRegistry(ctx, map[string]ToolLister{"server": client})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

type cancelingJSONValue struct {
	cancel context.CancelFunc
}

func (value cancelingJSONValue) MarshalJSON() ([]byte, error) {
	value.cancel()
	return []byte(`"normalized"`), nil
}

type registryTestClient struct {
	tools     []map[string]any
	listErr   error
	listCalls int
}

type classifiedListError struct {
	message   string
	retryable bool
}

func (e classifiedListError) Error() string   { return e.message }
func (e classifiedListError) Retryable() bool { return e.retryable }

func (c *registryTestClient) ListTools(context.Context) ([]map[string]any, error) {
	c.listCalls++
	return c.tools, c.listErr
}

func (c *registryTestClient) CallTool(context.Context, string, map[string]any, ...string) (map[string]any, error) {
	return nil, nil
}

func definitionNames(definitions []llm.ToolDefinition) []string {
	names := make([]string, len(definitions))
	for index, definition := range definitions {
		names[index] = definition.Name
	}
	return names
}

func assertErrorContains(t *testing.T, err error, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not contain %q", err, part)
		}
	}
}

func buildNestedSchemaRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	registry, err := BuildToolRegistry(context.Background(), map[string]ToolLister{
		"server": &registryTestClient{tools: []map[string]any{{
			"name":        "isolated",
			"inputSchema": nestedRegistrySchema(),
		}}},
	})
	if err != nil {
		t.Fatalf("BuildToolRegistry returned error: %v", err)
	}
	return registry
}

func nestedRegistrySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"choice": map[string]any{
				"type": "string",
				"enum": []any{"alpha", "beta"},
			},
		},
		"required": []any{"choice"},
	}
}

func mutateNestedRegistrySchema(schema map[string]any) {
	properties := schema["properties"].(map[string]any)
	choice := properties["choice"].(map[string]any)
	choice["type"] = "number"
	enum := choice["enum"].([]any)
	enum[0] = "mutated"
	required := schema["required"].([]any)
	required[0] = "mutated"
}

func assertNestedRegistrySchemaUnchanged(t *testing.T, schema map[string]any) {
	t.Helper()
	if !reflect.DeepEqual(schema, nestedRegistrySchema()) {
		t.Errorf("schema = %#v, want %#v", schema, nestedRegistrySchema())
	}
}
