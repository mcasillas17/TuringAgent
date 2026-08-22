package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryInvalidationDiscardsInFlightDiscovery(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{
		RegisteredMCPServers: func(context.Context) (map[string]ToolLister, error) {
			if calls.Add(1) == 1 {
				close(firstStarted)
				<-releaseFirst
				return map[string]ToolLister{
					"vendor": &registryTestClient{tools: []map[string]any{{
						"name": "vendor.stale", "inputSchema": map[string]any{"type": "object"},
					}}},
				}, nil
			}
			return map[string]ToolLister{
				"vendor": &registryTestClient{tools: []map[string]any{{
					"name": "vendor.current", "inputSchema": map[string]any{"type": "object"},
				}}},
			}, nil
		},
	})
	type result struct {
		tools []DiscoveredTool
		err   error
	}
	done := make(chan result, 1)
	go func() {
		tools, err := assistant.DiscoveredTools(context.Background())
		done <- result{tools: tools, err: err}
	}()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first discovery did not start")
	}
	assistant.InvalidateToolRegistry()
	close(releaseFirst)

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		for _, tool := range got.tools {
			if tool.ToolName == "vendor.stale" {
				t.Fatalf("stale discovery was published: %+v", got.tools)
			}
		}
		if calls.Load() < 2 {
			t.Fatalf("discovery calls = %d, want retry after invalidation", calls.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("discovery did not finish")
	}
}
