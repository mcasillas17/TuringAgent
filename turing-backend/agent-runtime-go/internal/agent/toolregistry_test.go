package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
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

	registry, err := BuildToolRegistry(context.Background(), map[string]toolLister{
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

func TestBuildRegistryAdvertisesDeterministically(t *testing.T) {
	want := []string{"a_second", "a_first", "z_only"}
	for iteration := 0; iteration < 50; iteration++ {
		servers := make(map[string]toolLister)
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
	tests := map[string]map[string]toolLister{
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

func TestBuildRegistryDefinitionsReturnsFreshSlice(t *testing.T) {
	registry, err := BuildToolRegistry(context.Background(), map[string]toolLister{
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

func TestBuildRegistryRejectsInvalidToolNames(t *testing.T) {
	tests := map[string]map[string]any{
		"missing":    {},
		"null":       {"name": nil},
		"blank":      {"name": " \t"},
		"non-string": {"name": 42},
	}
	for name, tool := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := BuildToolRegistry(context.Background(), map[string]toolLister{
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
	_, err := BuildToolRegistry(context.Background(), map[string]toolLister{
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
			_, err := BuildToolRegistry(context.Background(), map[string]toolLister{
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

func TestBuildRegistryRejectsDuplicateToolNames(t *testing.T) {
	t.Run("within server", func(t *testing.T) {
		_, err := BuildToolRegistry(context.Background(), map[string]toolLister{
			"same-server": &registryTestClient{tools: []map[string]any{
				{"name": "duplicate"},
				{"name": "duplicate"},
			}},
		})
		if err == nil {
			t.Fatal("BuildToolRegistry returned nil error")
		}
		assertErrorContains(t, err, "duplicate", "same-server")
	})

	t.Run("across servers", func(t *testing.T) {
		_, err := BuildToolRegistry(context.Background(), map[string]toolLister{
			"first-server":  &registryTestClient{tools: []map[string]any{{"name": "duplicate"}}},
			"second-server": &registryTestClient{tools: []map[string]any{{"name": "duplicate"}}},
		})
		if err == nil {
			t.Fatal("BuildToolRegistry returned nil error")
		}
		assertErrorContains(t, err, "duplicate", "first-server", "second-server")
	})
}

func TestBuildRegistryRejectsNilClient(t *testing.T) {
	t.Run("nil interface", func(t *testing.T) {
		_, err := BuildToolRegistry(context.Background(), map[string]toolLister{"nil-server": nil})
		if err == nil {
			t.Fatal("BuildToolRegistry returned nil error")
		}
		assertErrorContains(t, err, "nil-server", "nil")
	})

	t.Run("typed nil", func(t *testing.T) {
		var client *registryTestClient
		_, err := BuildToolRegistry(context.Background(), map[string]toolLister{"nil-server": client})
		if err == nil {
			t.Fatal("BuildToolRegistry returned nil error")
		}
		assertErrorContains(t, err, "nil-server", "nil")
	})
}

func TestBuildRegistryAddsServerContextToDiscoveryErrors(t *testing.T) {
	discoveryErr := errors.New("discovery failed")
	_, err := BuildToolRegistry(context.Background(), map[string]toolLister{
		"broken-server": &registryTestClient{listErr: discoveryErr},
	})
	if !errors.Is(err, discoveryErr) {
		t.Fatalf("error = %v, want wrapped discovery error", err)
	}
	assertErrorContains(t, err, "broken-server")
}

func TestBuildRegistryRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &registryTestClient{}

	_, err := BuildToolRegistry(ctx, map[string]toolLister{"server": client})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if client.listCalls != 0 {
		t.Fatalf("ListTools calls = %d, want zero for already-canceled context", client.listCalls)
	}
}

type registryTestClient struct {
	tools     []map[string]any
	listErr   error
	listCalls int
}

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
