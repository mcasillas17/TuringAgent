package llm

import (
	"net/http"
	"sort"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// pinnedProviderFailureOrigins is the (code, origin) pair every provider error
// this package produces is required to carry.
//
// The orchestrator selects a run's public outcome from exactly this pair, and
// it holds its own copy of the table because orchestrator-go's internal
// packages cannot be imported from this module directory (and this package
// cannot be imported from there either). Its copy is checked against
// providerFailureOrigins by reading this file's table out of source, and it
// additionally proves every pair survives normalization without falling back to
// the unknown code. What is pinned HERE is the other half: that the mapping
// boundary itself still answers this way, for every code the package can
// actually emit.
func pinnedProviderFailureOrigins() map[string]turingv1.FailureOrigin {
	return map[string]turingv1.FailureOrigin{
		"model_unavailable":    turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
		"model_auth_failed":    turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
		"model_request_failed": turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
		"model_quota_exceeded": turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
		"model_bad_chunk":      turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_PROTOCOL,
		"model_stream_error":   turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT,
		"model_timeout":        turingv1.FailureOrigin_FAILURE_ORIGIN_PROVIDER_TRANSPORT,
		"model_error":          turingv1.FailureOrigin_FAILURE_ORIGIN_EXTERNAL_PROVIDER,
	}
}

// unproducedProviderCodes are table entries no producer in this package can
// currently yield. model_timeout is the agent's own model-deadline code; it
// shares this vocabulary because the orchestrator maps one namespace, and it is
// listed so the coverage check below can be exhaustive rather than approximate.
func unproducedProviderCodes() map[string]string {
	return map[string]string{
		"model_timeout": "the agent reports its own model deadline under this code",
	}
}

// producedProviderErrorCodes drives the real classifiers rather than restating
// what they return, so a new branch that invents a code shows up here as an
// unpinned entry instead of being silently unclassified.
func producedProviderErrorCodes(t *testing.T) map[string]struct{} {
	t.Helper()
	produced := map[string]struct{}{
		// Spelled out at their call sites: a chunk that did not parse, and a
		// stream that stopped without a terminal event.
		"model_bad_chunk":    {},
		"model_stream_error": {},
	}
	for status := 100; status < 600; status++ {
		produced[providerHTTPErrorCode(status)] = struct{}{}
	}
	for _, envelope := range []struct {
		errorType string
		errorCode string
	}{
		{errorCode: "insufficient_quota"},
		{errorType: "invalid_request_error"},
		{errorType: "server_error"},
		{errorType: "something_new"},
	} {
		produced[classifyOpenAIError(envelope.errorType, envelope.errorCode)] = struct{}{}
	}
	return produced
}

// TestEveryProviderErrorCodeCarriesItsPinnedOrigin is the mapping-boundary pin.
// It asserts on providerError, the constructor every producer goes through, so
// what is proven is the origin an event actually leaves this package carrying.
func TestEveryProviderErrorCodeCarriesItsPinnedOrigin(t *testing.T) {
	pinned := pinnedProviderFailureOrigins()
	codes := make([]string, 0, len(pinned))
	for code := range pinned {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			event := providerError(code, "provider prose that must not decide anything")
			if event.Type != "error" {
				t.Fatalf("event type = %q, want error", event.Type)
			}
			if event.Code != code {
				t.Fatalf("event code = %q, want %q", event.Code, code)
			}
			if event.Origin != pinned[code] {
				t.Fatalf("origin for %q = %v, want %v", code, event.Origin, pinned[code])
			}
			if event.Origin == turingv1.FailureOrigin_FAILURE_ORIGIN_UNKNOWN {
				t.Fatalf("code %q left this package unclassified", code)
			}
		})
	}
}

// TestProviderFailureOriginTableMatchesThePin keeps the readable table and the
// pin in step in both directions: a row added without a pin, and a pin left
// behind by a deleted row, both fail.
func TestProviderFailureOriginTableMatchesThePin(t *testing.T) {
	pinned := pinnedProviderFailureOrigins()
	for code, origin := range providerFailureOrigins {
		want, ok := pinned[code]
		if !ok {
			t.Errorf("providerFailureOrigins maps the unpinned code %q to %v", code, origin)
			continue
		}
		if want != origin {
			t.Errorf("providerFailureOrigins maps %q to %v, the pin says %v", code, origin, want)
		}
	}
	for code := range pinned {
		if _, ok := providerFailureOrigins[code]; !ok {
			t.Errorf("the pin lists %q but providerFailureOrigins has no such row", code)
		}
	}
}

// TestEveryProducibleProviderCodeIsClassified closes the gap a value table
// cannot see on its own: a producer that emits a code nobody pinned. Every code
// the package's own classifiers can return must be in the table, and every
// table row must either be producible here or be listed as deliberately not.
func TestEveryProducibleProviderCodeIsClassified(t *testing.T) {
	produced := producedProviderErrorCodes(t)
	unproduced := unproducedProviderCodes()
	for code := range produced {
		if providerFailureOrigin(code) == turingv1.FailureOrigin_FAILURE_ORIGIN_UNKNOWN {
			t.Errorf("this package can emit %q, which providerFailureOrigin leaves unknown", code)
		}
		if _, deliberate := unproduced[code]; deliberate {
			t.Errorf("%q is listed as unproducible but a classifier returned it", code)
		}
	}
	for code := range providerFailureOrigins {
		if _, ok := produced[code]; ok {
			continue
		}
		if _, deliberate := unproduced[code]; !deliberate {
			t.Errorf("providerFailureOrigins holds %q, which no producer in this package yields", code)
		}
	}
}

// TestUnclassifiedProviderCodeStaysUnknown keeps the fail-closed default. A code
// this package has not classified must reach the orchestrator as unknown rather
// than borrowing a neighbouring origin, because the orchestrator's outcome
// choice is exactly as good as the origin it is handed.
func TestUnclassifiedProviderCodeStaysUnknown(t *testing.T) {
	for _, code := range []string{"", "model_", "tool_call_failed", "MODEL_UNAVAILABLE"} {
		if got := providerFailureOrigin(code); got != turingv1.FailureOrigin_FAILURE_ORIGIN_UNKNOWN {
			t.Fatalf("providerFailureOrigin(%q) = %v, want unknown", code, got)
		}
	}
}

// The HTTP classifier is the one producer whose codes are chosen from a number
// rather than written down, so its whole range is pinned here instead of being
// sampled by the coverage check above.
func TestProviderHTTPErrorCodeIsPinnedAcrossTheStatusRange(t *testing.T) {
	for _, test := range []struct {
		status int
		want   string
	}{
		{status: http.StatusRequestTimeout, want: "model_unavailable"},
		{status: http.StatusTooManyRequests, want: "model_unavailable"},
		{status: http.StatusInternalServerError, want: "model_unavailable"},
		{status: http.StatusServiceUnavailable, want: "model_unavailable"},
		{status: http.StatusUnauthorized, want: "model_auth_failed"},
		{status: http.StatusForbidden, want: "model_auth_failed"},
		{status: http.StatusBadRequest, want: "model_request_failed"},
		{status: http.StatusNotFound, want: "model_request_failed"},
		{status: http.StatusTeapot, want: "model_request_failed"},
	} {
		if got := providerHTTPErrorCode(test.status); got != test.want {
			t.Errorf("providerHTTPErrorCode(%d) = %q, want %q", test.status, got, test.want)
		}
	}
}
