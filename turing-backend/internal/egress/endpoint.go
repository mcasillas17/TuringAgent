package egress

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

type EndpointClass int

const (
	EndpointHTTPS EndpointClass = iota + 1
	EndpointLoopbackHTTP
	EndpointHTTP
)

type Endpoint struct {
	Canonical string
	Host      string
	Class     EndpointClass
}

var ErrInvalidKeyedEndpoint = errors.New("credentialed endpoint must be an absolute HTTPS URL or an HTTP loopback URL without credentials, query, or fragment")
var ErrInvalidLocalEndpoint = errors.New("local provider endpoint must use localhost, host.docker.internal, or a loopback IP literal")

func ParseKeyedEndpoint(raw string) (Endpoint, error) {
	return parseEndpoint(raw, true)
}

func ParseUnkeyedEndpoint(raw string) (Endpoint, error) {
	return parseEndpoint(raw, false)
}

func ParseLocalEndpoint(raw string) (Endpoint, error) {
	endpoint, err := parseEndpoint(raw, false)
	if err != nil {
		return Endpoint{}, ErrInvalidLocalEndpoint
	}
	parsed, err := url.Parse(endpoint.Canonical)
	if err != nil {
		return Endpoint{}, ErrInvalidLocalEndpoint
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "localhost" || hostname == "host.docker.internal" {
		return endpoint, nil
	}
	address, err := netip.ParseAddr(hostname)
	if err != nil || !address.Unmap().IsLoopback() {
		return Endpoint{}, ErrInvalidLocalEndpoint
	}
	return endpoint, nil
}

func parseEndpoint(raw string, keyed bool) (Endpoint, error) {
	if raw == "" || raw != strings.TrimSpace(raw) {
		return Endpoint{}, ErrInvalidKeyedEndpoint
	}
	if strings.Contains(raw, "#") {
		return Endpoint{}, ErrInvalidKeyedEndpoint
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || !parsed.IsAbs() || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return Endpoint{}, ErrInvalidKeyedEndpoint
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return Endpoint{}, ErrInvalidKeyedEndpoint
	}

	hostname := strings.ToLower(parsed.Hostname())
	if strings.Contains(hostname, ":") {
		if _, parseErr := netip.ParseAddr(hostname); parseErr != nil {
			return Endpoint{}, ErrInvalidKeyedEndpoint
		}
	}
	port := parsed.Port()
	if port != "" {
		number, parseErr := strconv.Atoi(port)
		if parseErr != nil || number <= 0 || number > 65535 {
			return Endpoint{}, ErrInvalidKeyedEndpoint
		}
		port = strconv.Itoa(number)
	}
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}

	hostForURL := hostname
	loopback := hostname == "localhost"
	if address, parseErr := netip.ParseAddr(hostname); parseErr == nil {
		address = address.Unmap()
		hostname = address.String()
		hostForURL = hostname
		loopback = address.IsLoopback()
		if address.Is6() {
			hostForURL = "[" + hostname + "]"
		}
	}
	if port != "" {
		hostForURL += ":" + port
	}

	class := EndpointHTTPS
	if scheme == "http" {
		if keyed && !loopback {
			return Endpoint{}, ErrInvalidKeyedEndpoint
		}
		if loopback {
			class = EndpointLoopbackHTTP
		} else {
			class = EndpointHTTP
		}
	}

	canonicalURL := &url.URL{
		Scheme:  scheme,
		Host:    hostForURL,
		Path:    parsed.Path,
		RawPath: parsed.RawPath,
	}
	canonical := strings.TrimRight(canonicalURL.String(), "/")
	if canonical == scheme+":" {
		return Endpoint{}, ErrInvalidKeyedEndpoint
	}
	return Endpoint{Canonical: canonical, Host: hostForURL, Class: class}, nil
}

type RedirectBlockedError struct {
	From string
	To   string
}

func (e *RedirectBlockedError) Error() string {
	return fmt.Sprintf("remote provider redirect from %s to %s is disabled", e.From, e.To)
}

func (*RedirectBlockedError) Retryable() bool { return false }

func NoRedirectClient(base *http.Client) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	restricted := *base
	restricted.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		from := ""
		if len(via) > 0 {
			from = redirectHost(via[len(via)-1].URL)
		}
		return &RedirectBlockedError{From: from, To: redirectHost(request.URL)}
	}
	return &restricted
}

func RedactRedirectError(err error) error {
	var blocked *RedirectBlockedError
	if errors.As(err, &blocked) {
		return blocked
	}
	return err
}

func redirectHost(value *url.URL) string {
	if value == nil {
		return ""
	}
	return strings.ToLower(value.Host)
}
