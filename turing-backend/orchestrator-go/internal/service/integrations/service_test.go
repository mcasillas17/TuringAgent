package integrations

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	_ "github.com/mattn/go-sqlite3"
)

// A string improbable enough that finding it anywhere means it came from here.
const testCredential = "shibboleth-app-password-8f31c2"

// The status code is the whole contract with the client: it is what tells a
// UI to say "that name is taken" rather than "something went wrong".
func TestConnectionErrorMapsEachFailureToItsCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"missing connection", repository.ErrConnectionNotFound, codes.NotFound},
		{"already revoked", repository.ErrConnectionAlreadyRevoked, codes.FailedPrecondition},
		{"duplicate name", repository.ErrConnectionNameTaken, codes.AlreadyExists},
		{"empty name", repository.ErrConnectionNameEmpty, codes.InvalidArgument},
		{"name too long", repository.ErrConnectionNameTooLong, codes.InvalidArgument},
		{"account too long", repository.ErrConnectionAccountTooLong, codes.InvalidArgument},
		{"endpoint too long", repository.ErrConnectionEndpointTooLong, codes.InvalidArgument},
		{"endpoint malformed", repository.ErrConnectionEndpointInvalid, codes.InvalidArgument},
		{"provider missing", repository.ErrConnectionProviderRequired, codes.InvalidArgument},
		{"credential missing", repository.ErrConnectionSecretRequired, codes.InvalidArgument},
		{"consent missing", repository.ErrConnectionConsentRequired, codes.PermissionDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := status.Code(connectionError(tt.err, "fallback")); got != tt.want {
				t.Fatalf("code = %v, want %v", got, tt.want)
			}
		})
	}
}

// A storage error must not reach the caller with its text intact. For this
// table that matters more than most: the failing statement names the column
// the credential lives in.
func TestConnectionErrorHidesUnrecognisedFailures(t *testing.T) {
	err := connectionError(fmt.Errorf(
		"UPDATE integration_connections SET credential_ciphertext = 'x' failed: /app/data/turing.db is locked"),
		"revoke connection failed")

	if got := status.Code(err); got != codes.Internal {
		t.Fatalf("code = %v, want Internal", got)
	}
	message := status.Convert(err).Message()
	if message != "revoke connection failed" {
		t.Fatalf("message = %q, want the fixed fallback", message)
	}
	if strings.Contains(message, "credential_ciphertext") || strings.Contains(message, "turing.db") {
		t.Fatalf("storage detail leaked to the caller: %q", message)
	}
}

func TestIntegrationFacetsRefuseInBothDirections(t *testing.T) {
	service, _, ctx := newIntegrationServer(t)
	public := NewPublicServer(service)
	internal := NewInternalServer(service)

	management := []struct {
		name string
		call func() error
	}{
		{"list providers", func() error { _, err := internal.ListProviders(ctx, &turingv1.ListProvidersRequest{}); return err }},
		{"connect", func() error { _, err := internal.ConnectAccount(ctx, githubRequestFixture()); return err }},
		{"list", func() error { _, err := internal.ListConnections(ctx, &turingv1.ListConnectionsRequest{}); return err }},
		{"get", func() error { _, err := internal.GetConnection(ctx, &turingv1.GetConnectionRequest{}); return err }},
		{"revoke", func() error {
			_, err := internal.RevokeConnection(ctx, &turingv1.RevokeConnectionRequest{})
			return err
		}},
		{"delete", func() error {
			_, err := internal.DeleteConnection(ctx, &turingv1.DeleteConnectionRequest{})
			return err
		}},
	}
	for _, test := range management {
		t.Run("internal "+test.name, func(t *testing.T) {
			if got := status.Code(test.call()); got != codes.PermissionDenied {
				t.Fatalf("code = %v, want PermissionDenied", got)
			}
		})
	}
	if _, err := public.ListIntegrationTools(ctx, &turingv1.ListIntegrationToolsRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public list tools code = %v, want PermissionDenied", status.Code(err))
	}
	if _, err := public.CallIntegrationTool(ctx, &turingv1.CallIntegrationToolRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("public call code = %v, want PermissionDenied", status.Code(err))
	}
}

func TestConnectAccountRoundTripsWithoutTheCredential(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)

	created, err := server.ConnectAccount(ctx, githubRequestFixture())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	if created.GetConnectionId() == "" {
		t.Fatal("connection has no id")
	}
	if created.GetStatus() != turingv1.ConnectionStatus_CONNECTION_STATUS_CONNECTED {
		t.Fatalf("status = %v, want connected", created.GetStatus())
	}
	if created.GetProvider() != turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB {
		t.Fatalf("provider = %v, want GitHub", created.GetProvider())
	}
	if created.GetConsentGrantedAt() == nil || created.GetConnectedAt() == nil {
		t.Fatalf("consent %v / connected %v, want both recorded", created.GetConsentGrantedAt(), created.GetConnectedAt())
	}
	if created.GetRevokedAt() != nil {
		t.Fatalf("revoked_at = %v on a fresh connection", created.GetRevokedAt())
	}
	// The user has to be shown what they agreed to, so it travels back with
	// the connection rather than being re-derived from the provider list.
	if len(created.GetGrantedScopes()) == 0 {
		t.Fatal("connection carries no record of what it grants")
	}

	listed, err := server.ListConnections(ctx, &turingv1.ListConnectionsRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed.GetConnections()) != 1 {
		t.Fatalf("listed %d connections, want 1", len(listed.GetConnections()))
	}
	fetched, err := server.GetConnection(ctx, &turingv1.GetConnectionRequest{ConnectionId: created.GetConnectionId()})
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// The assertion that matters most: no response carries the secret.
	assertNoCredential(t, "connect response", created)
	assertNoCredential(t, "list response", listed)
	assertNoCredential(t, "get response", fetched)
}

// The one test to keep if every other one were deleted: the credential the
// user pasted is not sitting in the database in the clear — not in the
// connections table, not in an event, not in an audit row, not anywhere.
func TestTheStoredCredentialIsNowherePlaintextInTheDatabase(t *testing.T) {
	server, database, ctx := newIntegrationServer(t)

	if _, err := server.ConnectAccount(ctx, githubRequestFixture()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	assertPlaintextAbsentFromDatabase(t, ctx, database, testCredential)
}

// Holding a credential with standing access to repositories is the most
// consequential thing this service does, so it leaves a record — the same way
// deleting a session does. What the record must not contain is the credential
// or its redaction.
func TestConnectingRevokingAndDeletingAreAudited(t *testing.T) {
	server, database, ctx := newIntegrationServer(t)

	created, err := server.ConnectAccount(ctx, githubRequestFixture())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := server.RevokeConnection(ctx, &turingv1.RevokeConnectionRequest{
		ConnectionId: created.GetConnectionId(),
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := server.DeleteConnection(ctx, &turingv1.DeleteConnectionRequest{
		ConnectionId: created.GetConnectionId(),
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	rows, err := database.QueryContext(ctx,
		`SELECT action, target, payload_json FROM audit_logs ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var actions []string
	for rows.Next() {
		var action, target, payload string
		if err := rows.Scan(&action, &target, &payload); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
		if target != created.GetConnectionId() {
			t.Fatalf("%s target = %q, want the connection id", action, target)
		}
		// The record says what happened without retaining what was given.
		if strings.Contains(payload, testCredential) || strings.Contains(payload, "••••") {
			t.Fatalf("%s payload carries credential material: %s", action, payload)
		}
		if !strings.Contains(payload, "github") {
			t.Fatalf("%s payload = %s, want it to name the provider", action, payload)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"integration.connected", "integration.revoked", "integration.deleted"}
	if len(actions) != len(want) {
		t.Fatalf("audit actions = %v, want %v", actions, want)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("audit actions = %v, want %v", actions, want)
		}
	}
	// Still no event: the event stream is per conversation, and a connection
	// belongs to no conversation.
	var events int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("events = %d after connecting an account, want none", events)
	}
}

// The audit sweep would be worthless if the credential could reach the record
// by a route the payload assertion above does not cover.
func TestAuditRowsAreCoveredByTheWholeDatabaseSweep(t *testing.T) {
	server, database, ctx := newIntegrationServer(t)

	if _, err := server.ConnectAccount(ctx, githubRequestFixture()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	var audited int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if audited == 0 {
		t.Fatal("no audit rows, so the sweep proves nothing about them")
	}
	assertPlaintextAbsentFromDatabase(t, ctx, database, testCredential)
}

// Consent is checked before anything else happens, so a client that forgot it
// cannot leave a half-connected account behind.
func TestConnectAccountRefusesWithoutConsentAndStoresNothing(t *testing.T) {
	server, database, ctx := newIntegrationServer(t)
	request := githubRequestFixture()
	request.ConsentAcknowledged = false

	_, err := server.ConnectAccount(ctx, request)

	if got := status.Code(err); got != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied", got)
	}
	assertNoConnectionsStored(t, ctx, database)
	assertPlaintextAbsentFromDatabase(t, ctx, database, testCredential)
}

// Naming a provider we cannot connect is the honest half; refusing to pretend
// otherwise is the other half.
func TestConnectAccountRefusesOtherUnimplementedProvidersWithTheReason(t *testing.T) {
	server, database, ctx := newIntegrationServer(t)

	for _, kind := range []turingv1.IntegrationProvider{
		turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GOOGLE_WORKSPACE,
		turingv1.IntegrationProvider_INTEGRATION_PROVIDER_MICROSOFT_365,
		turingv1.IntegrationProvider_INTEGRATION_PROVIDER_SLACK,
	} {
		request := githubRequestFixture()
		request.Provider = kind

		_, err := server.ConnectAccount(ctx, request)

		if got := status.Code(err); got != codes.FailedPrecondition {
			t.Fatalf("%v: code = %v, want FailedPrecondition", kind, got)
		}
		if message := status.Convert(err).Message(); !strings.Contains(message, "not implemented") {
			t.Fatalf("%v: message = %q, want it to say why", kind, message)
		}
	}
	assertNoConnectionsStored(t, ctx, database)
}

func TestConnectAccountRejectsAProviderItDoesNotKnow(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)
	request := githubRequestFixture()
	request.Provider = turingv1.IntegrationProvider_INTEGRATION_PROVIDER_UNSPECIFIED

	_, err := server.ConnectAccount(ctx, request)

	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", got)
	}
}

// Without a key there is nothing to seal with. Storing the credential anyway
// would silently downgrade the protection the schema comment promises.
func TestConnectAccountRefusesWhenNoIntegrationKeyIsConfigured(t *testing.T) {
	database := openIntegrationTestDB(t)
	server := New(repository.New(database), nil, audit.New(repository.New(database)))
	ctx := context.Background()

	_, err := server.ConnectAccount(ctx, githubRequestFixture())

	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition", got)
	}
	if message := status.Convert(err).Message(); !strings.Contains(message, "TURING_INTEGRATION_KEY") {
		t.Fatalf("message = %q, want it to name the setting to fix", message)
	}
	assertNoConnectionsStored(t, ctx, database)
	// Reading and listing still work: an unconfigured key blocks connecting,
	// not seeing what is already connected.
	if _, err := server.ListConnections(ctx, &turingv1.ListConnectionsRequest{}); err != nil {
		t.Fatalf("list without a key: %v", err)
	}
	if _, err := server.ListProviders(ctx, &turingv1.ListProvidersRequest{}); err != nil {
		t.Fatalf("list providers without a key: %v", err)
	}
}

func TestConnectAccountValidatesTheFieldsItWasGiven(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*turingv1.ConnectAccountRequest)
	}{
		{"no credential", func(r *turingv1.ConnectAccountRequest) { r.Credential = "   " }},
		{"credential too long", func(r *turingv1.ConnectAccountRequest) { r.Credential = strings.Repeat("a", 4097) }},
		{"name too long", func(r *turingv1.ConnectAccountRequest) { r.DisplayName = strings.Repeat("a", 121) }},
		{"account too long", func(r *turingv1.ConnectAccountRequest) { r.AccountLabel = strings.Repeat("a", 321) }},
		{"no name", func(r *turingv1.ConnectAccountRequest) { r.DisplayName = "  " }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, database, ctx := newIntegrationServer(t)
			request := githubRequestFixture()
			tt.mutate(request)

			_, err := server.ConnectAccount(ctx, request)

			if got := status.Code(err); got != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument", got)
			}
			assertNoConnectionsStored(t, ctx, database)
		})
	}
}

func TestConnectAccountDoesNotDemandAnEndpointFromAHostedProvider(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)

	_, err := server.ConnectAccount(ctx, &turingv1.ConnectAccountRequest{
		Provider:            turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB,
		DisplayName:         "Work GitHub",
		AccountLabel:        "Acme",
		Credential:          testCredential,
		ConsentAcknowledged: true,
	})

	if err != nil {
		t.Fatalf("connect GitHub without an endpoint: %v", err)
	}
}

func TestConnectAccountRejectsASecondConnectionWithTheSameName(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)
	if _, err := server.ConnectAccount(ctx, githubRequestFixture()); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	duplicate := githubRequestFixture()
	duplicate.DisplayName = strings.ToUpper(duplicate.DisplayName)

	_, err := server.ConnectAccount(ctx, duplicate)

	if got := status.Code(err); got != codes.AlreadyExists {
		t.Fatalf("code = %v, want AlreadyExists", got)
	}
}

// The hint exists so a user can tell two connections apart. Four characters
// of a long token does that; four characters of a six-character one does not.
func TestRedactRevealsAtMostFourCharactersAndOnlyFromALongSecret(t *testing.T) {
	long := redact("abcdefghijklmnop")
	if !strings.HasSuffix(long, "mnop") {
		t.Fatalf("redact(long) = %q, want it to end in the last four characters", long)
	}
	if strings.Contains(long, "abcdefghijkl") {
		t.Fatalf("redact(long) = %q, leaked more than the tail", long)
	}
	if got := redact("short"); strings.Contains(got, "ort") {
		t.Fatalf("redact(short) = %q, want nothing but bullets", got)
	}
	if got := redact(""); strings.ContainsAny(got, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("redact(empty) = %q", got)
	}
}

func TestConnectAccountReturnsARedactionRatherThanTheCredential(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)

	created, err := server.ConnectAccount(ctx, githubRequestFixture())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	hint := created.GetCredentialHint()
	if hint == "" {
		t.Fatal("no hint at all: the user cannot tell two connections apart")
	}
	if strings.Contains(testCredential, hint) {
		t.Fatalf("hint %q is a verbatim substring of the credential", hint)
	}
	if !strings.HasSuffix(hint, testCredential[len(testCredential)-4:]) {
		t.Fatalf("hint = %q, want it to end in the credential's last four characters", hint)
	}
}

func TestRevokeConnectionDestroysTheCredentialAndSaysSo(t *testing.T) {
	server, database, ctx := newIntegrationServer(t)
	created, err := server.ConnectAccount(ctx, githubRequestFixture())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	revoked, err := server.RevokeConnection(ctx, &turingv1.RevokeConnectionRequest{
		ConnectionId: created.GetConnectionId(),
	})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if revoked.GetStatus() != turingv1.ConnectionStatus_CONNECTION_STATUS_REVOKED {
		t.Fatalf("status = %v, want revoked", revoked.GetStatus())
	}
	if revoked.GetRevokedAt() == nil {
		t.Fatal("revoked connection has no revocation time")
	}
	if revoked.GetCredentialHint() != "" {
		t.Fatalf("hint = %q after revocation, want it gone", revoked.GetCredentialHint())
	}
	var ciphertext []byte
	if err := database.QueryRowContext(ctx,
		`SELECT credential_ciphertext FROM integration_connections WHERE id = ?`,
		created.GetConnectionId()).Scan(&ciphertext); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	if len(ciphertext) != 0 {
		t.Fatalf("credential survived revocation: %d bytes", len(ciphertext))
	}
	// The record survives so the user can still see the account once had
	// access, and what it was allowed to do.
	if len(revoked.GetGrantedScopes()) == 0 {
		t.Fatal("revoked connection forgot what it had been granted")
	}
}

func TestRevokeAndDeleteReportMissingAndAlreadyRevoked(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)
	created, err := server.ConnectAccount(ctx, githubRequestFixture())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	id := created.GetConnectionId()
	if _, err := server.RevokeConnection(ctx, &turingv1.RevokeConnectionRequest{ConnectionId: id}); err != nil {
		t.Fatalf("first revoke: %v", err)
	}

	_, err = server.RevokeConnection(ctx, &turingv1.RevokeConnectionRequest{ConnectionId: id})
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("second revoke code = %v, want FailedPrecondition", got)
	}
	_, err = server.RevokeConnection(ctx, &turingv1.RevokeConnectionRequest{ConnectionId: "conn_nope"})
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("revoke missing code = %v, want NotFound", got)
	}
	_, err = server.GetConnection(ctx, &turingv1.GetConnectionRequest{ConnectionId: "conn_nope"})
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("get missing code = %v, want NotFound", got)
	}
	_, err = server.DeleteConnection(ctx, &turingv1.DeleteConnectionRequest{ConnectionId: "conn_nope"})
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("delete missing code = %v, want NotFound", got)
	}
}

func TestDeleteConnectionRemovesTheRecordAndTheCredential(t *testing.T) {
	server, database, ctx := newIntegrationServer(t)
	created, err := server.ConnectAccount(ctx, githubRequestFixture())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	if _, err := server.DeleteConnection(ctx, &turingv1.DeleteConnectionRequest{
		ConnectionId: created.GetConnectionId(),
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	assertNoConnectionsStored(t, ctx, database)
	_, err = server.GetConnection(ctx, &turingv1.GetConnectionRequest{ConnectionId: created.GetConnectionId()})
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("get after delete = %v, want NotFound", got)
	}
}

// The catalogue is what the UI renders, so its shape is a contract: a
// supported provider that says nothing about what it grants would produce a
// consent step with nothing to consent to.
func TestListProvidersDescribesEveryEntryHonestly(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)

	response, err := server.ListProviders(ctx, &turingv1.ListProvidersRequest{})
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}

	supported := map[turingv1.IntegrationProvider]bool{}
	for _, descriptor := range response.GetProviders() {
		if descriptor.GetDisplayName() == "" || descriptor.GetCategory() == "" {
			t.Fatalf("%v has no name or category", descriptor.GetProvider())
		}
		if descriptor.GetSupported() {
			supported[descriptor.GetProvider()] = true
			if len(descriptor.GetGrants()) == 0 {
				t.Fatalf("%v is connectable but says nothing about what it grants", descriptor.GetProvider())
			}
			if descriptor.GetSecretLabel() == "" {
				t.Fatalf("%v does not say what the user should paste", descriptor.GetProvider())
			}
			if descriptor.GetUnsupportedReason() != "" {
				t.Fatalf("%v is supported but carries a refusal reason", descriptor.GetProvider())
			}
			if descriptor.GetRequiresEndpoint() && descriptor.GetEndpointLabel() == "" {
				t.Fatalf("%v needs an endpoint but does not say which", descriptor.GetProvider())
			}
			continue
		}
		if descriptor.GetUnsupportedReason() == "" {
			t.Fatalf("%v is refused without a reason", descriptor.GetProvider())
		}
		if len(descriptor.GetGrants()) != 0 {
			t.Fatalf("%v cannot be connected but lists grants, which implies it can", descriptor.GetProvider())
		}
	}

	if len(supported) != 1 || !supported[turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB] {
		t.Fatalf("connectable providers = %v, want only GitHub", supported)
	}
}

// A row written by a build that knows a provider this one does not must not
// be reported as one of the providers we do know.
func TestAConnectionWithAnUnknownProviderIsNotGuessedAt(t *testing.T) {
	server, database, ctx := newIntegrationServer(t)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO integration_connections (
			id, provider, display_name, account_label, endpoint, credential_ciphertext,
			credential_hint, status, granted_scopes_json, consent_granted_at,
			connected_at, created_at, updated_at)
		VALUES ('conn_future', 'telepathy', 'Future thing', '', '', X'00', '••••',
			'orbiting', '["Something"]', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z',
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	fetched, err := server.GetConnection(ctx, &turingv1.GetConnectionRequest{ConnectionId: "conn_future"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if fetched.GetProvider() != turingv1.IntegrationProvider_INTEGRATION_PROVIDER_UNSPECIFIED {
		t.Fatalf("provider = %v, want UNSPECIFIED for a provider this build does not know", fetched.GetProvider())
	}
	// And a status it does not understand is not reported as connected —
	// claiming an account is live when we cannot tell is the wrong answer.
	if fetched.GetStatus() != turingv1.ConnectionStatus_CONNECTION_STATUS_UNSPECIFIED {
		t.Fatalf("status = %v, want UNSPECIFIED for an unknown status", fetched.GetStatus())
	}
}

func TestNilRequestsAreRejectedRatherThanPanicking(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)

	if _, err := server.ConnectAccount(ctx, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("connect nil = %v", err)
	}
	if _, err := server.GetConnection(ctx, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("get nil = %v", err)
	}
	if _, err := server.RevokeConnection(ctx, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("revoke nil = %v", err)
	}
	if _, err := server.DeleteConnection(ctx, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("delete nil = %v", err)
	}
}

func TestParseTimestampReturnsNilForAnUnparseableValue(t *testing.T) {
	if got := parseTimestamp("not a timestamp"); got != nil {
		t.Fatalf("parseTimestamp = %v, want nil", got)
	}
	// A never-revoked connection has an empty string here, and an empty
	// timestamp must not become the epoch.
	if got := parseTimestamp(""); got != nil {
		t.Fatalf("parseTimestamp(empty) = %v, want nil", got)
	}
}

// The stored value must not be a portable blob. Moving it into another
// connection — the thing someone with write access to data/turing.db would
// try — must make it unopenable rather than redirecting a live token to a
// server of their choosing.
func TestAStoredCredentialCannotBeMovedToAnotherConnection(t *testing.T) {
	server, database, ctx := newIntegrationServer(t)
	first, err := server.ConnectAccount(ctx, githubRequestFixture())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	second := githubRequestFixture()
	second.DisplayName = "Second GitHub"
	second.Credential = "another-app-password-0000"
	other, err := server.ConnectAccount(ctx, second)
	if err != nil {
		t.Fatalf("connect second: %v", err)
	}

	var stolen []byte
	if err := database.QueryRowContext(ctx,
		`SELECT credential_ciphertext FROM integration_connections WHERE id = ?`,
		first.GetConnectionId()).Scan(&stolen); err != nil {
		t.Fatal(err)
	}
	sealer := server.sealer
	if _, err := sealer.Open(stolen, []byte(first.GetConnectionId())); err != nil {
		t.Fatalf("the credential does not open under its own connection: %v", err)
	}

	if _, err := sealer.Open(stolen, []byte(other.GetConnectionId())); err == nil {
		t.Fatal("a credential moved to another connection opened cleanly")
	}
}

// A newline in a credential is not a typo, it is an injected command waiting
// for the HTTP request that eventually uses the GitHub token.
func TestConnectAccountRejectsControlCharactersInACredential(t *testing.T) {
	for _, name := range []string{"newline", "carriage return", "null", "tab"} {
		t.Run(name, func(t *testing.T) {
			server, database, ctx := newIntegrationServer(t)
			request := githubRequestFixture()
			switch name {
			case "newline":
				request.Credential = "token\nA001 LOGOUT"
			case "carriage return":
				request.Credential = "token\r\nA001 LOGOUT"
			case "null":
				request.Credential = "token\x00rest"
			case "tab":
				request.Credential = "token\tmore"
			}

			_, err := server.ConnectAccount(ctx, request)

			if got := status.Code(err); got != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument", got)
			}
			assertNoConnectionsStored(t, ctx, database)
		})
	}
}

// A hosted provider has one address, and it is not the user's to set. A form
// that left a stale value behind must not make a GitHub connection claim to
// live on somebody's mail server.
func TestConnectAccountDropsAnEndpointAProviderDoesNotUse(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)

	created, err := server.ConnectAccount(ctx, &turingv1.ConnectAccountRequest{
		Provider:            turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB,
		DisplayName:         "Work GitHub",
		AccountLabel:        "Acme",
		Endpoint:            "imap.example.com",
		Credential:          testCredential,
		ConsentAcknowledged: true,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	if created.GetEndpoint() != "" {
		t.Fatalf("endpoint = %q, want it dropped for a hosted provider", created.GetEndpoint())
	}
}

// Rotate or lose TURING_INTEGRATION_KEY and the stored credentials can never
// be opened again. A connection that kept claiming to work would be the app
// asserting access it does not have — VISION's first failure.
func TestAConnectionSealedWithAKeyWeNoLongerHaveSaysSo(t *testing.T) {
	server, database, ctx := newIntegrationServer(t)
	created, err := server.ConnectAccount(ctx, githubRequestFixture())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if created.GetCredentialUnreadable() {
		t.Fatal("a freshly sealed credential is reported unreadable")
	}

	// Same repository, different key: what restoring .env from elsewhere, or
	// rotating the key, looks like.
	rotatedKey := make([]byte, secretbox.KeySize)
	rotatedKey[0] = 0xAA
	rotated, err := secretbox.New(rotatedKey)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	after := New(repo, rotated, audit.New(repo))

	fetched, err := after.GetConnection(ctx, &turingv1.GetConnectionRequest{
		ConnectionId: created.GetConnectionId(),
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !fetched.GetCredentialUnreadable() {
		t.Fatal("a connection sealed with a key we no longer have still claims to work")
	}
	listed, err := after.ListConnections(ctx, &turingv1.ListConnectionsRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !listed.GetConnections()[0].GetCredentialUnreadable() {
		t.Fatal("the list does not carry the same answer as the get")
	}

	// A revoked connection has no credential at all, so it is not "unreadable"
	// — that would invite somebody to reconnect a thing they deliberately
	// ended.
	revoked, err := server.RevokeConnection(ctx, &turingv1.RevokeConnectionRequest{
		ConnectionId: created.GetConnectionId(),
	})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if revoked.GetCredentialUnreadable() {
		t.Fatal("a revoked connection reports an unreadable credential")
	}
}

// With no key there is nothing to seal with, and the catalogue says so up
// front — before a client asks anyone to paste a live app password into a
// form that cannot work.
func TestListProvidersSaysWhenNothingCanBeStored(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)

	configured, err := server.ListProviders(ctx, &turingv1.ListProvidersRequest{})
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}
	if !configured.GetCredentialStorageConfigured() {
		t.Fatal("a configured backend reports that it cannot store credentials")
	}
	if configured.GetStorageUnconfiguredReason() != "" {
		t.Fatalf("reason = %q on a configured backend", configured.GetStorageUnconfiguredReason())
	}

	database := openIntegrationTestDB(t)
	repo := repository.New(database)
	unconfigured, err := New(repo, nil, audit.New(repo)).
		ListProviders(ctx, &turingv1.ListProvidersRequest{})
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}
	if unconfigured.GetCredentialStorageConfigured() {
		t.Fatal("a backend with no key claims it can store credentials")
	}
	if !strings.Contains(unconfigured.GetStorageUnconfiguredReason(), "TURING_INTEGRATION_KEY") {
		t.Fatalf("reason = %q, want it to name the setting", unconfigured.GetStorageUnconfiguredReason())
	}
	// The catalogue itself is still served: what can be connected in principle
	// does not depend on whether this machine is set up yet.
	if len(unconfigured.GetProviders()) != len(configured.GetProviders()) {
		t.Fatal("the catalogue shrank when the key was missing")
	}
}

// One of the four stated invariants is "never logged". Nothing logs today;
// this is what fails the day somebody adds log.Printf("%+v", req).
func TestNothingOnTheConnectPathIsLogged(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)
	var logged strings.Builder
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	created, err := server.ConnectAccount(ctx, githubRequestFixture())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := server.RevokeConnection(ctx, &turingv1.RevokeConnectionRequest{
		ConnectionId: created.GetConnectionId(),
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if strings.Contains(logged.String(), testCredential) {
		t.Fatalf("the credential reached the log: %s", logged.String())
	}
	if logged.Len() != 0 {
		t.Fatalf("connecting logged %q; a credential is one careless format verb away", logged.String())
	}
}

func githubRequestFixture() *turingv1.ConnectAccountRequest {
	return &turingv1.ConnectAccountRequest{
		Provider:            turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB,
		DisplayName:         "Personal GitHub",
		AccountLabel:        "octocat",
		Credential:          testCredential,
		ConsentAcknowledged: true,
	}
}

// assertNoCredential serialises the whole message, so it covers fields added
// after this test was written rather than only the ones it knows to check.
func assertNoCredential(t *testing.T, what string, message proto.Message) {
	t.Helper()
	encoded, err := protojson.Marshal(message)
	if err != nil {
		t.Fatalf("marshal %s: %v", what, err)
	}
	if strings.Contains(string(encoded), testCredential) {
		t.Fatalf("%s carries the credential: %s", what, encoded)
	}
	// The tail is in the hint by design; anything longer is not.
	if strings.Contains(string(encoded), testCredential[:len(testCredential)-4]) {
		t.Fatalf("%s carries most of the credential: %s", what, encoded)
	}
}

// assertPlaintextAbsentFromDatabase reads every column of every table,
// including the ones this feature does not write, because the point is to
// catch a credential that reached somewhere it was never meant to go.
func assertPlaintextAbsentFromDatabase(t *testing.T, ctx context.Context, database *db.DB, secret string) {
	t.Helper()
	tables, err := database.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type IN ('table', 'view')`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var names []string
	for tables.Next() {
		var name string
		if err := tables.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := tables.Err(); err != nil {
		t.Fatal(err)
	}
	_ = tables.Close()
	if len(names) == 0 {
		t.Fatal("no tables found: the sweep would pass vacuously")
	}

	scanned := 0
	for _, name := range names {
		rows, err := database.QueryContext(ctx, `SELECT * FROM "`+name+`"`)
		if err != nil {
			// Some FTS5 shadow tables are not directly selectable; skipping
			// one is fine as long as the sweep still covers real tables.
			continue
		}
		columns, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			t.Fatalf("columns of %s: %v", name, err)
		}
		for rows.Next() {
			cells := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range cells {
				pointers[i] = &cells[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				_ = rows.Close()
				t.Fatalf("scan %s: %v", name, err)
			}
			scanned++
			for i, cell := range cells {
				var text string
				switch value := cell.(type) {
				case nil:
					continue
				case []byte:
					text = string(value)
				case string:
					text = value
				default:
					text = fmt.Sprintf("%v", value)
				}
				if strings.Contains(text, secret) {
					_ = rows.Close()
					t.Fatalf("credential found in plaintext in %s.%s", name, columns[i])
				}
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("iterate %s: %v", name, err)
		}
		_ = rows.Close()
	}
	if scanned == 0 {
		t.Fatal("no rows were scanned: the sweep would pass vacuously")
	}
}

func assertNoConnectionsStored(t *testing.T, ctx context.Context, database *db.DB) {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM integration_connections`).Scan(&count); err != nil {
		t.Fatalf("count connections: %v", err)
	}
	if count != 0 {
		t.Fatalf("%d connections stored, want none", count)
	}
}

func newIntegrationServer(t *testing.T) (*Server, *db.DB, context.Context) {
	t.Helper()
	database := openIntegrationTestDB(t)
	sealer, err := secretbox.FromHexKey(hex.EncodeToString(make([]byte, secretbox.KeySize)))
	if err != nil {
		t.Fatalf("sealer: %v", err)
	}
	repo := repository.New(database)
	return New(repo, sealer, audit.New(repo)), database, context.Background()
}

func openIntegrationTestDB(t *testing.T) *db.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	sqlDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared&_foreign_keys=on", name))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	database := &db.DB{DB: sqlDB}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}
