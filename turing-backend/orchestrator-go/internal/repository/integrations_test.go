package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
)

func newConnectionTestRepo(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	return New(openTestDB(t)), context.Background()
}

func validConnection() NewConnection {
	return NewConnection{
		ConnectionID:         ids.New("conn"),
		Provider:             "imap",
		DisplayName:          "Personal mail",
		AccountLabel:         "me@example.com",
		Endpoint:             "imap.example.com",
		CredentialCiphertext: []byte{0x01, 0x02, 0x03},
		CredentialHint:       "••••••••cd12",
		GrantedScopes:        []string{"Read every message."},
	}
}

func mustCreateConnection(t *testing.T, ctx context.Context, repo *Repository, input NewConnection) Connection {
	t.Helper()
	connection, err := repo.CreateConnection(ctx, input)
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	return connection
}

func TestCreateConnectionStoresWhatItWasGivenAndTrimsIt(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	input := validConnection()
	input.DisplayName = "  Personal mail  "
	input.AccountLabel = "  me@example.com  "

	created := mustCreateConnection(t, ctx, repo, input)

	if created.ConnectionID == "" || !strings.HasPrefix(created.ConnectionID, "conn_") {
		t.Fatalf("connection id = %q, want a conn_ id", created.ConnectionID)
	}
	if created.DisplayName != "Personal mail" || created.AccountLabel != "me@example.com" {
		t.Fatalf("stored %q / %q, want both trimmed", created.DisplayName, created.AccountLabel)
	}
	if created.Status != ConnectionStatusConnected {
		t.Fatalf("status = %q, want connected", created.Status)
	}
	if created.ConsentGrantedAt == "" || created.ConnectedAt == "" {
		t.Fatalf("consent %q / connected %q, want both recorded", created.ConsentGrantedAt, created.ConnectedAt)
	}
	if created.RevokedAt != "" {
		t.Fatalf("revoked_at = %q on a fresh connection", created.RevokedAt)
	}

	fetched, err := repo.GetConnection(ctx, created.ConnectionID)
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if len(fetched.GrantedScopes) != 1 || fetched.GrantedScopes[0] != "Read every message." {
		t.Fatalf("granted scopes = %v, want the consented set round-tripped", fetched.GrantedScopes)
	}
	if fetched.CredentialHint != "••••••••cd12" {
		t.Fatalf("hint = %q, want the redaction stored at connect time", fetched.CredentialHint)
	}
}

func TestGetSealedConnectionCredentialReturnsTheNamedLiveCredentialAndFailsClosedAfterRevoke(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	input := validConnection()
	input.CredentialCiphertext = []byte("sealed-for-exact-connection")
	created := mustCreateConnection(t, ctx, repo, input)

	credential, err := repo.GetSealedConnectionCredential(ctx, created.ConnectionID)
	if err != nil {
		t.Fatal(err)
	}
	if string(credential.Ciphertext) != string(input.CredentialCiphertext) || credential.Status != ConnectionStatusConnected {
		t.Fatalf("credential = %+v", credential)
	}
	if _, err := repo.RevokeConnection(ctx, created.ConnectionID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetSealedConnectionCredential(ctx, created.ConnectionID); !errors.Is(err, ErrConnectionNotUsable) {
		t.Fatalf("revoked credential error = %v, want ErrConnectionNotUsable", err)
	}
}

func TestIntegrationApprovalRenderBoundaryUsesOneCompleteBuilder(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	input := validConnection()
	input.Provider = "github"
	input.DisplayName = "Work GitHub"
	input.Endpoint = ""
	connection := mustCreateConnection(t, ctx, repo, input)
	args := map[string]any{"connection_id": connection.ConnectionID, "owner": "octo", "repo": "project", "issue_number": float64(7), "body": ""}
	base, err := repo.IntegrationApprovalRender(ctx, "github.create_comment", args)
	if err != nil {
		t.Fatal(err)
	}
	bodyBytes := MaxIntegrationApprovalRenderBytes - len([]byte(base))
	args["body"] = strings.Repeat("x", bodyBytes)
	exact, err := repo.IntegrationApprovalRender(ctx, "github.create_comment", args)
	if err != nil || len([]byte(exact)) != MaxIntegrationApprovalRenderBytes {
		t.Fatalf("exact bytes=%d err=%v", len([]byte(exact)), err)
	}
	args["body"] = strings.Repeat("x", bodyBytes+1)
	over, err := repo.IntegrationApprovalRender(ctx, "github.create_comment", args)
	if err != nil || len([]byte(over)) != MaxIntegrationApprovalRenderBytes+1 {
		t.Fatalf("over bytes=%d err=%v", len([]byte(over)), err)
	}
	if !strings.HasSuffix(exact, strings.Repeat("x", bodyBytes)) {
		t.Fatal("approval render truncated the body")
	}
}

func TestIntegrationApprovalRenderRejectsDestinationWhitespace(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	input := validConnection()
	input.Provider = "github"
	input.Endpoint = ""
	connection := mustCreateConnection(t, ctx, repo, input)
	args := map[string]any{
		"connection_id": connection.ConnectionID, "owner": " octo", "repo": "project",
		"issue_number": float64(7), "body": "complete body",
	}
	if _, err := repo.IntegrationApprovalRender(ctx, "github.create_comment", args); err == nil {
		t.Fatal("approval render normalized a destination that provider dispatch rejects")
	}
}

func TestIntegrationEndpointsResolveDuringRegistrationWindowAndReEnable(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	input := validConnection()
	input.Provider = "github"
	input.Endpoint = ""
	input.DisplayName = "GitHub"
	mustCreateConnection(t, ctx, repo, input)
	selected := []string{"integrations/github.list_issues"}
	endpoints, err := repo.IntegrationEndpointsForTools(ctx, selected)
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("bootstrap endpoints=%+v err=%v", endpoints, err)
	}
	if err := repo.UpsertTools(ctx, []DiscoveredTool{{ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{}`, Policy: "approval_required"}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetToolPolicyByName(ctx, "integrations", "github.list_issues", "disabled"); err != nil {
		t.Fatal(err)
	}
	endpoints, err = repo.IntegrationEndpointsForTools(ctx, selected)
	if err != nil || len(endpoints) != 0 {
		t.Fatalf("disabled endpoints=%+v err=%v", endpoints, err)
	}
	if err := repo.SetToolPolicyByName(ctx, "integrations", "github.list_issues", "approval_required"); err != nil {
		t.Fatal(err)
	}
	endpoints, err = repo.IntegrationEndpointsForTools(ctx, selected)
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("re-enabled endpoints=%+v err=%v", endpoints, err)
	}
}

func TestIntegrationDisclosureNamesRemainDistinctAfterRuneCap(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	prefix := strings.Repeat("同", 64)
	for index, id := range []string{"conn_distinct_aaaaaaaa", "conn_distinct_bbbbbbbb"} {
		input := validConnection()
		input.ConnectionID = id
		input.Provider = "github"
		input.Endpoint = ""
		input.DisplayName = prefix + string(rune('A'+index))
		mustCreateConnection(t, ctx, repo, input)
	}
	endpoints, err := repo.IntegrationEndpointsForTools(ctx, []string{"integrations/github.list_issues"})
	if err != nil || len(endpoints) != 2 {
		t.Fatalf("endpoints=%+v err=%v", endpoints, err)
	}
	if endpoints[0].DisplayName == endpoints[1].DisplayName {
		t.Fatalf("capped names collided: %q", endpoints[0].DisplayName)
	}
	for _, endpoint := range endpoints {
		if got := len([]rune(endpoint.DisplayName)); got > maxIntegrationDisplayNameRunes {
			t.Fatalf("display name runes=%d, want <=%d", got, maxIntegrationDisplayNameRunes)
		}
	}
}

// The consent snapshot is what makes "you agreed to this" checkable later. A
// connection stored with no recorded grants would be a consent record that
// says nothing.
func TestCreateConnectionRequiresARecordedConsentSnapshot(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	input := validConnection()
	input.GrantedScopes = nil

	_, err := repo.CreateConnection(ctx, input)

	if !errors.Is(err, ErrConnectionConsentRequired) {
		t.Fatalf("error = %v, want ErrConnectionConsentRequired", err)
	}
}

func TestCreateConnectionRejectsMalformedInput(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	tests := []struct {
		name   string
		mutate func(*NewConnection)
		want   error
	}{
		{"no provider", func(c *NewConnection) { c.Provider = "  " }, ErrConnectionProviderRequired},
		{"no name", func(c *NewConnection) { c.DisplayName = "   " }, ErrConnectionNameEmpty},
		{"name too long", func(c *NewConnection) { c.DisplayName = strings.Repeat("a", 121) }, ErrConnectionNameTooLong},
		{"account too long", func(c *NewConnection) { c.AccountLabel = strings.Repeat("a", 321) }, ErrConnectionAccountTooLong},
		{"endpoint too long", func(c *NewConnection) { c.Endpoint = strings.Repeat("a", 256) }, ErrConnectionEndpointTooLong},
		// The endpoint is eventually dialled by a mail or calendar client, so
		// anything with structure in it changes what "connect here" means.
		{"endpoint with spaces", func(c *NewConnection) { c.Endpoint = "imap example com" }, ErrConnectionEndpointInvalid},
		{"endpoint with a scheme", func(c *NewConnection) { c.Endpoint = "https://evil.example" }, ErrConnectionEndpointInvalid},
		{"endpoint with a path", func(c *NewConnection) { c.Endpoint = "imap.example.com/../x" }, ErrConnectionEndpointInvalid},
		{"endpoint with userinfo", func(c *NewConnection) { c.Endpoint = "user@evil.example" }, ErrConnectionEndpointInvalid},
		{"endpoint with a query", func(c *NewConnection) { c.Endpoint = "imap.example.com?x=1" }, ErrConnectionEndpointInvalid},
		{"endpoint with a control character", func(c *NewConnection) { c.Endpoint = "imap.example.com\x00" }, ErrConnectionEndpointInvalid},
		// A row with no ciphertext would look connected and be unusable.
		{"no sealed credential", func(c *NewConnection) { c.CredentialCiphertext = nil }, ErrConnectionSecretRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validConnection()
			tt.mutate(&input)
			if _, err := repo.CreateConnection(ctx, input); !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCreateConnectionAcceptsAHostWithAPort(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	input := validConnection()
	input.Endpoint = "imap.example.com:993"

	created, err := repo.CreateConnection(ctx, input)
	if err != nil {
		t.Fatalf("a host with a port was rejected: %v", err)
	}
	if created.Endpoint != "imap.example.com:993" {
		t.Fatalf("endpoint = %q, want it stored as given", created.Endpoint)
	}
}

func TestCreateConnectionRejectsADuplicateNameRegardlessOfCase(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	mustCreateConnection(t, ctx, repo, validConnection())

	duplicate := validConnection()
	duplicate.DisplayName = "personal MAIL"
	if _, err := repo.CreateConnection(ctx, duplicate); !errors.Is(err, ErrConnectionNameTaken) {
		t.Fatalf("error = %v, want ErrConnectionNameTaken", err)
	}
}

// Reconnecting an account under the name it used to have is the normal thing
// to do after revoking it, so its own history must not block it.
func TestARevokedConnectionDoesNotHoldItsNameHostage(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	created := mustCreateConnection(t, ctx, repo, validConnection())
	if _, err := repo.RevokeConnection(ctx, created.ConnectionID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if _, err := repo.CreateConnection(ctx, validConnection()); err != nil {
		t.Fatalf("reconnect under the same name: %v", err)
	}
}

// The assertion that matters most in this file: revoking destroys the secret
// rather than hiding it behind a status.
func TestRevokeConnectionDestroysTheStoredCredential(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	created := mustCreateConnection(t, ctx, repo, validConnection())

	revoked, err := repo.RevokeConnection(ctx, created.ConnectionID)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if revoked.Status != ConnectionStatusRevoked || revoked.RevokedAt == "" {
		t.Fatalf("revoked = %+v, want revoked status and a timestamp", revoked)
	}
	if revoked.CredentialHint != "" {
		t.Fatalf("hint = %q after revocation, want it gone too", revoked.CredentialHint)
	}
	var ciphertext []byte
	if err := repo.db.QueryRowContext(ctx,
		`SELECT credential_ciphertext FROM integration_connections WHERE id = ?`,
		created.ConnectionID).Scan(&ciphertext); err != nil {
		t.Fatalf("read ciphertext column: %v", err)
	}
	if len(ciphertext) != 0 {
		t.Fatalf("credential survived revocation: %d bytes still stored", len(ciphertext))
	}
	// The record of the connection survives; only the secret goes.
	if _, err := repo.GetConnection(ctx, created.ConnectionID); err != nil {
		t.Fatalf("get after revoke: %v", err)
	}
}

// A row written by a build that knows a status this one does not must still
// be revocable. Refusing to destroy a credential because the status string is
// unfamiliar is the one outcome that must not happen.
func TestRevokeConnectionDestroysTheCredentialOfAnUnrecognisedStatus(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	created := mustCreateConnection(t, ctx, repo, validConnection())
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE integration_connections SET status = 'orbiting' WHERE id = ?`,
		created.ConnectionID); err != nil {
		t.Fatal(err)
	}

	revoked, err := repo.RevokeConnection(ctx, created.ConnectionID)
	if err != nil {
		t.Fatalf("revoke a row with an unknown status: %v", err)
	}

	if revoked.Status != ConnectionStatusRevoked {
		t.Fatalf("status = %q, want revoked", revoked.Status)
	}
	var ciphertext []byte
	if err := repo.db.QueryRowContext(ctx,
		`SELECT credential_ciphertext FROM integration_connections WHERE id = ?`,
		created.ConnectionID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if len(ciphertext) != 0 {
		t.Fatalf("credential survived revocation: %d bytes", len(ciphertext))
	}
}

func TestRevokeConnectionSeparatesMissingFromAlreadyRevoked(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	created := mustCreateConnection(t, ctx, repo, validConnection())
	if _, err := repo.RevokeConnection(ctx, created.ConnectionID); err != nil {
		t.Fatalf("first revoke: %v", err)
	}

	if _, err := repo.RevokeConnection(ctx, created.ConnectionID); !errors.Is(err, ErrConnectionAlreadyRevoked) {
		t.Fatalf("second revoke = %v, want ErrConnectionAlreadyRevoked", err)
	}
	if _, err := repo.RevokeConnection(ctx, "conn_nope"); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("revoke missing = %v, want ErrConnectionNotFound", err)
	}
	if _, err := repo.GetConnection(ctx, "conn_nope"); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("get missing = %v, want ErrConnectionNotFound", err)
	}
}

func TestDeleteConnectionRemovesTheRowIncludingALiveCredential(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	created := mustCreateConnection(t, ctx, repo, validConnection())

	if err := repo.DeleteConnection(ctx, created.ConnectionID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var remaining int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM integration_connections WHERE id = ?`, created.ConnectionID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("%d rows left after delete", remaining)
	}
	if err := repo.DeleteConnection(ctx, created.ConnectionID); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("second delete = %v, want ErrConnectionNotFound", err)
	}
}

// Live connections first, then by name: the list has to read the same way on
// every visit, and the ones that still have access belong at the top.
func TestListConnectionsPutsLiveOnesFirstThenOrdersByName(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	for _, name := range []string{"Zed notes", "Alpha mail", "Revoked one"} {
		input := validConnection()
		input.DisplayName = name
		created := mustCreateConnection(t, ctx, repo, input)
		if name == "Revoked one" {
			if _, err := repo.RevokeConnection(ctx, created.ConnectionID); err != nil {
				t.Fatalf("revoke: %v", err)
			}
		}
	}

	connections, err := repo.ListConnections(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	var got []string
	for _, connection := range connections {
		got = append(got, connection.DisplayName)
	}
	want := []string{"Alpha mail", "Zed notes", "Revoked one"}
	if len(got) != len(want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("listed %v, want %v", got, want)
		}
	}
}

func TestListConnectionsIsEmptyBeforeAnythingIsConnected(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)

	connections, err := repo.ListConnections(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(connections) != 0 {
		t.Fatalf("listed %d connections on a fresh database", len(connections))
	}
}

// A row written by a build that stored something this one cannot parse must
// surface as an error, not as a connection with no grants that reads as
// "you agreed to nothing".
func TestGetConnectionFailsRatherThanInventingAnEmptyConsent(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	created := mustCreateConnection(t, ctx, repo, validConnection())
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE integration_connections SET granted_scopes_json = 'not json' WHERE id = ?`,
		created.ConnectionID); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.GetConnection(ctx, created.ConnectionID); err == nil {
		t.Fatal("get succeeded with unreadable consent, want an error")
	}
	if _, err := repo.ListConnections(ctx); err == nil {
		t.Fatal("list succeeded with unreadable consent, want an error")
	}
}

// Guards the schema's partial index: without the WHERE clause, revoked rows
// would collide with live ones.
func TestConnectionNameIndexOnlyConstrainsLiveConnections(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	var indexSQL sql.NullString
	if err := repo.db.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'integration_connections_name_unique'`).
		Scan(&indexSQL); err != nil {
		t.Fatalf("read index definition: %v", err)
	}
	if !strings.Contains(indexSQL.String, "WHERE status = 'connected'") {
		t.Fatalf("index = %q, want it limited to connected rows", indexSQL.String)
	}
}

// The read path reads a fixed number of bytes off the front of the sealed
// value. If secretbox's header ever grows, reading the old width would slice
// a key fingerprint in half and report every connection as unreadable, so the
// two constants are pinned together here rather than by eye.
func TestTheCredentialHeaderMatchesTheSealFormat(t *testing.T) {
	if credentialHeaderBytes != secretbox.HeaderSize {
		t.Fatalf("read %d header bytes, but a sealed value's header is %d",
			credentialHeaderBytes, secretbox.HeaderSize)
	}
}

// The read path must see the header and nothing else: enough to say which key
// sealed the row, not one byte of what was sealed.
func TestReadingAConnectionExposesTheHeaderButNotTheCiphertext(t *testing.T) {
	repo, ctx := newConnectionTestRepo(t)
	key := make([]byte, secretbox.KeySize)
	key[0] = 0x5a
	sealer, err := secretbox.New(key)
	if err != nil {
		t.Fatal(err)
	}
	input := validConnection()
	sealed, err := sealer.Seal([]byte("app-password-shibboleth"), []byte(input.ConnectionID))
	if err != nil {
		t.Fatal(err)
	}
	input.CredentialCiphertext = sealed
	created := mustCreateConnection(t, ctx, repo, input)

	fetched, err := repo.GetConnection(ctx, created.ConnectionID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if len(fetched.CredentialHeader) != secretbox.HeaderSize {
		t.Fatalf("header = %d bytes, want %d", len(fetched.CredentialHeader), secretbox.HeaderSize)
	}
	if !sealer.SealedWithThisKey(fetched.CredentialHeader) {
		t.Fatal("the header read back does not identify the key that sealed it")
	}
	if len(fetched.CredentialHeader) >= len(sealed) {
		t.Fatal("the read path pulled back the whole sealed value")
	}
	// And a revoked connection has no header, because it has no credential.
	revoked, err := repo.RevokeConnection(ctx, created.ConnectionID)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if len(revoked.CredentialHeader) != 0 {
		t.Fatalf("header = %v after revocation, want none", revoked.CredentialHeader)
	}
}
