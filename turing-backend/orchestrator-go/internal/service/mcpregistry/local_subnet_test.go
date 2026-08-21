package mcpregistry

import (
	"context"
	"net"
	"testing"
)

func TestLocalContainerTransportAcceptsOnlyRegistrySubnet(t *testing.T) {
	lookup := func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("172.31.253.10")}}, nil
	}
	if _, err := resolveLocalMCPAddress(context.Background(), "vendor:9000", lookup); err == nil {
		t.Fatal("local MCP host outside net-mcp-registry subnet was accepted")
	}
	lookup = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("172.31.254.10")}}, nil
	}
	if _, err := resolveLocalMCPAddress(context.Background(), "vendor:9000", lookup); err != nil {
		t.Fatalf("registry-network address was rejected: %v", err)
	}
}
