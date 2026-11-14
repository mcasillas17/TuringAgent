package cli

import (
	"strings"
	"testing"
)

func TestContainerEngineURLRewritesWhatNamesThisMachineForTheContainers(t *testing.T) {
	tests := []struct{ raw, want string }{
		{"http://localhost:11434", "http://host.docker.internal:11434"},
		{"http://127.0.0.1:8000/v1", "http://host.docker.internal:8000/v1"},
		{"http://[::1]:8000/v1", "http://host.docker.internal:8000/v1"},
		{"http://LOCALHOST:8000/v1/", "http://host.docker.internal:8000/v1"},
		{"http://localhost/v1", "http://host.docker.internal/v1"},
		{"https://localhost:8443/v1", "https://host.docker.internal:8443/v1"},
		{"http://localhost:80", "http://host.docker.internal"},
		{"http://host.docker.internal:8000/v1", "http://host.docker.internal:8000/v1"},
		{"http://host.docker.internal:11434/", "http://host.docker.internal:11434"},
		{"http://127.0.0.1:8000/api/v1/", "http://host.docker.internal:8000/api/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := containerEngineURL(tt.raw)

			if err != nil || got != tt.want {
				t.Fatalf("containerEngineURL(%q) = %q, %v; want %q", tt.raw, got, err, tt.want)
			}
			if again, err := containerEngineURL(got); err != nil || again != got {
				t.Fatalf("containerEngineURL(%q) = %q, %v; a canonical URL must come back unchanged", got, again, err)
			}
		})
	}
}

func TestContainerEngineURLRefusesWhatTheRuntimeRefusesAndNeverRepeatsIt(t *testing.T) {
	tests := []struct {
		name, raw string
	}{
		{"a LAN address", "http://192.168.1.50:8000/v1"},
		{"another machine by name", "http://engine.example.com:8000/v1"},
		{"a user and password", "http://user:CANARY-PASSWORD@localhost:8000/v1"},
		{"a user alone", "http://CANARY-USER@host.docker.internal:8000/v1"},
		{"a query", "http://localhost:8000/v1?key=CANARY-QUERY"},
		{"an empty query", "http://localhost:8000/v1?"},
		{"a fragment", "http://localhost:8000/v1#CANARY-FRAGMENT"},
		{"an empty fragment", "http://localhost:8000/v1#"},
		{"a scheme that is not http", "ftp://localhost:8000/CANARY"},
		{"no scheme", "localhost:8000/CANARY"},
		{"nothing", ""},
		{"surrounding space", " http://localhost:8000/v1 "},
		{"a port out of range", "http://localhost:99999/CANARY"},
		{"a loopback address the rewrite does not cover", "http://127.0.0.2:8000/CANARY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := containerEngineURL(tt.raw)

			if err == nil {
				t.Fatalf("containerEngineURL(%q) = %q, want a refusal", tt.raw, got)
			}
			if got != "" {
				t.Fatalf("a refusal returned %q", got)
			}
			if strings.Contains(err.Error(), "CANARY") || (tt.raw != "" && strings.Contains(err.Error(), strings.TrimSpace(tt.raw))) {
				t.Fatalf("the error repeats the URL: %v", err)
			}
		})
	}
}

func TestContainerEngineURLSaysWhatTheRuntimeTakes(t *testing.T) {
	_, err := containerEngineURL("http://192.168.1.50:8000/v1")

	for _, want := range []string{"http or https", "this machine", "no user, password, query or fragment", "host.docker.internal"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to mention %q", err, want)
		}
	}
}

func TestContainerEngineURLSaysWhyAnotherLoopbackAddressIsRefused(t *testing.T) {
	_, err := containerEngineURL("http://127.0.0.2:8000/v1")

	if err == nil || !strings.Contains(err.Error(), "loopback") || !strings.Contains(err.Error(), "container itself") {
		t.Fatalf("error = %v, want it to say that a loopback address is the container itself", err)
	}
}
