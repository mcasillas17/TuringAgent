package mcpregistry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

type mcpLookupIP func(context.Context, string) ([]net.IPAddr, error)

var localMCPNetwork = netip.MustParsePrefix("172.31.254.0/24")

var specialUseMCPNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
}

func (s *Server) clientFor(server repository.MCPServerRecord) *http.Client {
	if s.httpClient != nil {
		client := *s.httpClient
		client.CheckRedirect = rejectMCPRedirect
		if client.Timeout <= 0 {
			client.Timeout = 30 * time.Second
		}
		return &client
	}
	if server.Tier == repository.MCPServerTierBundled {
		client := *http.DefaultClient
		client.CheckRedirect = rejectMCPRedirect
		client.Timeout = 30 * time.Second
		return &client
	}
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	if server.Tier == repository.MCPServerTierLocalContainer && s.localClient != nil {
		return s.localClient
	}
	if server.Tier == repository.MCPServerTierRemoteURL && s.remoteClient != nil {
		return s.remoteClient
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	resolver := net.DefaultResolver
	resolve := resolvePublicMCPAddress
	if server.Tier == repository.MCPServerTierLocalContainer {
		resolve = resolveLocalMCPAddress
	}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			resolved, err := resolve(ctx, address, resolver.LookupIPAddr)
			if err != nil {
				return nil, err
			}

			return dialer.DialContext(ctx, network, resolved)
		},
	}
	client := &http.Client{
		Transport: transport, CheckRedirect: rejectMCPRedirect,
		Timeout: 30 * time.Second,
	}
	if server.Tier == repository.MCPServerTierLocalContainer {
		s.localClient = client
	} else {
		s.remoteClient = client
	}
	return client
}

func resolveLocalMCPAddress(ctx context.Context, address string, lookup mcpLookupIP) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return "", errors.New("local MCP dial address is invalid")
	}
	addresses, err := lookup(ctx, host)
	if err != nil {
		return "", fmt.Errorf("resolve local MCP host: %w", err)
	}
	if len(addresses) == 0 {
		return "", errors.New("local MCP host resolved to no addresses")
	}
	for _, resolved := range addresses {
		ip, ok := netip.AddrFromSlice(resolved.IP)
		if !ok || !localMCPNetwork.Contains(ip.Unmap()) {
			return "", errors.New("local MCP host must resolve only inside net-mcp-registry")
		}
	}
	return net.JoinHostPort(addresses[0].IP.String(), port), nil
}

func resolvePublicMCPAddress(ctx context.Context, address string, lookup mcpLookupIP) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return "", errors.New("remote MCP dial address is invalid")
	}
	addresses, err := lookup(ctx, host)
	if err != nil {
		return "", fmt.Errorf("resolve remote MCP host: %w", err)
	}
	if len(addresses) == 0 {
		return "", errors.New("remote MCP host resolved to no addresses")
	}
	for _, resolved := range addresses {
		ip, ok := netip.AddrFromSlice(resolved.IP)
		if !ok {
			return "", errors.New("remote MCP host resolved to an invalid address")
		}
		ip = ip.Unmap()
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
			ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsMulticast() || ip.IsUnspecified() || isSpecialUseMCPAddress(ip) {
			return "", errors.New("remote MCP host must resolve only to public addresses")
		}
	}

	return net.JoinHostPort(addresses[0].IP.String(), port), nil
}

func isSpecialUseMCPAddress(address netip.Addr) bool {
	for _, network := range specialUseMCPNetworks {
		if network.Contains(address) {
			return true
		}
	}
	return false
}

func rejectMCPRedirect(_ *http.Request, _ []*http.Request) error {
	return errors.New("MCP redirects are not allowed")
}
