package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
)

type LookupIP func(context.Context, string) ([]net.IPAddr, error)

var specialUseNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("100:0:0:1::/64"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
}

// ResolvePublicAddress resolves every answer before selecting one. A hostile
// mixed DNS response therefore cannot smuggle a private destination into an
// otherwise public lookup.
func ResolvePublicAddress(ctx context.Context, address string, lookup LookupIP) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return "", errors.New("remote dial address is invalid")
	}
	addresses, err := lookup(ctx, host)
	if err != nil {
		return "", fmt.Errorf("resolve remote host: %w", err)
	}
	if len(addresses) == 0 {
		return "", errors.New("remote host resolved to no addresses")
	}
	for _, resolved := range addresses {
		ip, ok := netip.AddrFromSlice(resolved.IP)
		if !ok {
			return "", errors.New("remote host resolved to an invalid address")
		}
		ip = ip.Unmap()
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
			ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() ||
			ip.IsUnspecified() || isSpecialUseAddress(ip) {
			return "", errors.New("remote host must resolve only to public addresses")
		}
	}
	return net.JoinHostPort(addresses[0].IP.String(), port), nil
}

func isSpecialUseAddress(address netip.Addr) bool {
	for _, network := range specialUseNetworks {
		if network.Contains(address) {
			return true
		}
	}
	return false
}

func IsPublicIP(value net.IP) bool {
	ip, ok := netip.AddrFromSlice(value)
	if !ok {
		return false
	}
	ip = ip.Unmap()
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() &&
		!ip.IsUnspecified() && !isSpecialUseAddress(ip)
}
