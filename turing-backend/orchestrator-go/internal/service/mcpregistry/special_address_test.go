package mcpregistry

import (
	"context"
	"net"
	"testing"
)

func TestRemoteTransportRejectsSharedAndSpecialUseAddresses(t *testing.T) {
	for _, address := range []string{
		"100.64.0.1",
		"198.18.0.1",
		"192.0.2.1",
		"2001:db8::1",
		"2001:2::1",
		"64:ff9b::7f00:1",
		"::7f00:1",
		"100:0:0:1::1",
	} {
		t.Run(address, func(t *testing.T) {
			_, err := resolvePublicMCPAddress(
				context.Background(),
				"vendor.example:443",
				func(context.Context, string) ([]net.IPAddr, error) {
					return []net.IPAddr{{IP: net.ParseIP(address)}}, nil
				},
			)
			if err == nil {
				t.Fatalf("special-use address %s was accepted", address)
			}
		})
	}
}
