package mcpregistry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

type mcpLookupIP func(context.Context, string) ([]net.IPAddr, error)

var localMCPNetwork = netip.MustParsePrefix("172.31.254.0/24")

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
	return backendegress.ResolvePublicAddress(ctx, address, backendegress.LookupIP(lookup))
}

func rejectMCPRedirect(_ *http.Request, _ []*http.Request) error {
	return errors.New("MCP redirects are not allowed")
}
