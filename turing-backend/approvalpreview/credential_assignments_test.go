package approvalpreview

import (
	"fmt"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestCompositeAssignmentsUseTheStructuredCredentialPolicy(t *testing.T) {
	for _, key := range []string{
		"aws_secret_access_key", "AWS_SECRET_ACCESS_KEY", "service_client_secret_value",
		"database_password_value", "google_application_credentials",
		"service_private_key_material", "AUTHORIZATION_HEADER", "api_key_value",
		"my_api-key-value", "refreshTokenValue", "session_cookie_data", "auth", "auths",
	} {
		t.Run(key, func(t *testing.T) {
			_, state := SanitizeArguments(map[string]any{key: "x"})
			if state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED {
				t.Fatalf("structured credential key not recognized: %s", state)
			}
			for _, text := range []string{key + "=x", "export " + key + "=0", fmt.Sprintf(`"%s":"x"`, key), fmt.Sprintf("'%s' = 'x'", key)} {
				if !UnsafeText(text) {
					t.Errorf("assignment form not recognized for %s", key)
				}
				_, state := SanitizeArguments(map[string]any{"content": text})
				if state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED {
					t.Errorf("assignment enabled content review for %s", key)
				}
			}
		})
	}
	if !UnsafeText(`{"private key material": "x"}`) {
		t.Fatal("quoted composite key diverged from structured policy")
	}
}

func TestCompositeCredentialNamesWithoutAssignmentsRemainProse(t *testing.T) {
	for _, text := range []string{
		"aws_secret_access_key is an identifier, not a configured value.",
		"Explain token and authorization handling without including a secret.",
		"The service_private_key_material setting is documented at https://example.com/help.",
	} {
		safe, state := SanitizeArguments(map[string]any{"content": text})
		if state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY || !strings.Contains(safe, text) {
			t.Fatalf("ordinary prose was hidden: %s", state)
		}
	}
}
