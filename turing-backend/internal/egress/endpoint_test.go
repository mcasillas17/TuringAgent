package egress

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestParseKeyedEndpointCanonicalizesSecureAndLoopbackURLs(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		canonical string
		host      string
		class     EndpointClass
	}{
		{
			name:      "remote https",
			raw:       "HTTPS://Example.COM:443/v1/",
			canonical: "https://example.com/v1",
			host:      "example.com",
			class:     EndpointHTTPS,
		},
		{
			name:      "remote https leading-zero default port",
			raw:       "https://example.com:000443/v1",
			canonical: "https://example.com/v1",
			host:      "example.com",
			class:     EndpointHTTPS,
		},
		{
			name:      "localhost development",
			raw:       "http://LOCALHOST:8080/v1/",
			canonical: "http://localhost:8080/v1",
			host:      "localhost:8080",
			class:     EndpointLoopbackHTTP,
		},
		{
			name:      "localhost leading-zero default port",
			raw:       "http://localhost:00080/v1",
			canonical: "http://localhost/v1",
			host:      "localhost",
			class:     EndpointLoopbackHTTP,
		},
		{
			name:      "ipv4 loopback range",
			raw:       "http://127.20.30.40:8080/v1",
			canonical: "http://127.20.30.40:8080/v1",
			host:      "127.20.30.40:8080",
			class:     EndpointLoopbackHTTP,
		},
		{
			name:      "ipv6 loopback",
			raw:       "http://[0:0:0:0:0:0:0:1]:8080/v1/",
			canonical: "http://[::1]:8080/v1",
			host:      "[::1]:8080",
			class:     EndpointLoopbackHTTP,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			endpoint, err := ParseKeyedEndpoint(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			if endpoint.Canonical != test.canonical || endpoint.Host != test.host || endpoint.Class != test.class {
				t.Fatalf("endpoint = %+v, want canonical=%q host=%q class=%d", endpoint, test.canonical, test.host, test.class)
			}
		})
	}
}

func TestParseKeyedEndpointRejectsAmbiguousOrPlaintextRemoteURLs(t *testing.T) {
	for _, raw := range []string{
		"http://host.docker.internal:8080/v1",
		"http://example.com/v1",
		"http://192.168.1.10/v1",
		"http://[fd00::1]/v1",
		"https://user:password@example.com/v1",
		"https://example.com/v1?tenant=secret",
		"https://example.com/v1?",
		"https://example.com/v1#fragment",
		"ftp://example.com/v1",
		"/relative/v1",
		"https://",
		"https://::",
		"https://0::",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseKeyedEndpoint(raw); err == nil {
				t.Fatalf("ParseKeyedEndpoint(%q) succeeded", raw)
			}

		})
	}
}

func TestParseLocalEndpointRejectsRemoteHosts(t *testing.T) {
	for _, raw := range []string{
		"http://example.com:11434",
		"https://192.168.1.10:11434",
	} {
		if _, err := ParseLocalEndpoint(raw); err == nil {
			t.Fatalf("ParseLocalEndpoint(%q) succeeded", raw)
		}
	}
	for _, raw := range []string{
		"http://host.docker.internal:11434",
		"http://localhost:11434",
		"http://127.0.0.1:11434",
		"http://[::1]:11434",
	} {
		if _, err := ParseLocalEndpoint(raw); err != nil {
			t.Fatalf("ParseLocalEndpoint(%q): %v", raw, err)
		}
	}
}

func TestNoRedirectClientRejectsBeforeRedirectedRequest(t *testing.T) {
	var redirectedRequests atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedRequests.Add(1)
	}))
	defer target.Close()

	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	client := NoRedirectClient(redirect.Client())
	_, err := client.Get(redirect.URL)
	if err == nil {
		t.Fatal("redirect request succeeded")
	}
	var blocked *RedirectBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("redirect error = %T %v, want RedirectBlockedError", err, err)
	}
	if blocked.Retryable() {
		t.Fatal("blocked redirect is retryable")
	}
	if redirectedRequests.Load() != 0 {
		t.Fatalf("redirect target requests = %d, want 0", redirectedRequests.Load())
	}
}

func TestNoRedirectClientDoesNotMutateSuppliedClient(t *testing.T) {
	base := &http.Client{}
	restricted := NoRedirectClient(base)
	if restricted == base {
		t.Fatal("NoRedirectClient returned the supplied pointer")
	}
	if base.CheckRedirect != nil {
		t.Fatal("NoRedirectClient mutated the supplied client")
	}
	if restricted.CheckRedirect == nil {
		t.Fatal("restricted client has no redirect policy")
	}
}
