package mcpregistry

import (
	"context"
	"strings"
	"testing"
)

// TestMalformedAuthorizationHeaderReasonIsOperatorMeaningful pins the
// exact wording bearerFromHeaders uses when an entry's single
// Authorization header is present but unusable — missing the "Bearer "
// prefix, or empty after it — so a future edit cannot silently reintroduce
// a placeholder-looking string (a run of literal asterisks, for instance)
// in place of an operator-actionable explanation. The message must say
// plainly what is wrong (no non-empty bearer credential) without echoing
// anything from the header's own value.
func TestMalformedAuthorizationHeaderReasonIsOperatorMeaningful(t *testing.T) {
	const wantReason = "authorization header must contain a non-empty bearer credential"

	for _, test := range []struct {
		name   string
		header string
	}{
		{name: "missing Bearer prefix", header: "Token abc123"},
		{name: "Bearer with nothing after it", header: "Bearer "},
		{name: "Bearer with only whitespace after it", header: "Bearer    "},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, _ := newRegistryTestService(t)
			ctx := context.Background()

			report, err := service.ImportJSON(ctx, []byte(`{
				"mcpServers": {
					"vendor": {
						"url": "https://vendor.example/mcp",
						"headers": {"Authorization": "`+test.header+`"}
					}
				}
			}`))
			if err != nil {
				t.Fatal(err)
			}
			reason, refused := report.Unsupported["vendor"]
			if !refused {
				t.Fatalf("Unsupported = %+v, want vendor refused for a malformed Authorization header", report.Unsupported)
			}
			if reason != wantReason {
				t.Fatalf("reason = %q, want the exact fixed reason %q", reason, wantReason)
			}
			if strings.Contains(reason, "*") {
				t.Fatalf("reason = %q, must not contain literal asterisks (looks like a redaction/content-filter artifact, not an operator-meaningful message)", reason)
			}
		})
	}
}

// No refusal reason this package ever returns should contain a run of
// literal asterisks: that shape reads as an accidental content-filter or
// redaction artifact, not a deliberate, operator-meaningful explanation.
// This is a narrow, targeted regression guard for the specific historical
// bug (an unclosed "authorization header must use a non-empty ******"
// message), not a blanket policy scan.
func TestBearerFromHeadersErrorsContainNoLiteralAsterisks(t *testing.T) {
	if _, err := bearerFromHeaders([]mcpHeaderEntry{{Name: "Authorization", Value: "NotBearer x"}}); err == nil || strings.Contains(err.Error(), "*") {
		t.Fatalf("bearerFromHeaders error = %v, must not contain literal asterisks", err)
	}
	if _, err := bearerFromHeaders([]mcpHeaderEntry{{Name: "Authorization", Value: "Bearer "}}); err == nil || strings.Contains(err.Error(), "*") {
		t.Fatalf("bearerFromHeaders error = %v, must not contain literal asterisks", err)
	}
}
