package mcpregistry

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestRemoteTransportRejectsPrivateResolutionBeforeDial(t *testing.T) {
	_, err := resolvePublicMCPAddress(
		context.Background(),
		"vendor.example:443",
		func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("172.20.0.10")}}, nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "public") {
		t.Fatalf("resolve private address error = %v, want public-address refusal", err)
	}
}
