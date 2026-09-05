package integrations

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mattn/go-sqlite3"
	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/secretbox"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

var powerlessProviders = []turingv1.IntegrationProvider{
	turingv1.IntegrationProvider_INTEGRATION_PROVIDER_IMAP,
	turingv1.IntegrationProvider_INTEGRATION_PROVIDER_CALDAV,
	turingv1.IntegrationProvider_INTEGRATION_PROVIDER_NOTION,
}

// Only header inspection is allowed. Even failed attempts to seal or decrypt
// are counted; embedding the synthetic secret in errors also tests redaction.
type forbiddenCredentialSealer struct {
	inner CredentialSealer
	seals atomic.Int32
	opens atomic.Int32
}

func (s *forbiddenCredentialSealer) Seal(_, _ []byte) ([]byte, error) {
	s.seals.Add(1)
	return nil, errors.New(testCredential)
}
func (s *forbiddenCredentialSealer) Open(_, _ []byte) ([]byte, error) {
	s.opens.Add(1)
	return nil, errors.New(testCredential)
}
func (s *forbiddenCredentialSealer) SealedWithThisKey(header []byte) bool {
	return s.inner != nil && s.inner.SealedWithThisKey(header)
}

func TestPowerlessProvidersRemainNamedWithoutCredentialSolicitation(t *testing.T) {
	server, _, ctx := newIntegrationServer(t)
	response, err := server.ListProviders(ctx, &turingv1.ListProvidersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	byKind := map[turingv1.IntegrationProvider]*turingv1.ProviderDescriptor{}
	for _, descriptor := range response.GetProviders() {
		byKind[descriptor.GetProvider()] = descriptor
	}
	for _, kind := range powerlessProviders {
		descriptor := byKind[kind]
		if descriptor == nil || descriptor.GetDisplayName() == "" || descriptor.GetSupported() {
			t.Errorf("%v must remain named but unsupported: %v", kind, descriptor)
		}
		if !strings.Contains(descriptor.GetUnsupportedReason(), "tools are not implemented") {
			t.Errorf("%v must explain the missing tools: %v", kind, descriptor)
		}
		if descriptor.GetSecretLabel() != "" || descriptor.GetSecretHelp() != "" || len(descriptor.GetGrants()) != 0 {
			t.Errorf("%v still solicits a credential or advertises grants", kind)
		}
	}
	if !byKind[turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB].GetSupported() {
		t.Fatal("GitHub must remain connectable")
	}
}

func TestPowerlessConnectionsRefusedBeforeSideEffectsIncludingDirectRPC(t *testing.T) {
	for _, kind := range powerlessProviders {
		t.Run(kind.String(), func(t *testing.T) {
			server, database, ctx := newIntegrationServer(t)
			spy := &forbiddenCredentialSealer{}
			server.sealer = spy
			notifier := &integrationNotifier{}
			server.SetRegistryChangeNotifier(notifier)
			var network atomic.Int32
			server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				network.Add(1)
				return nil, errors.New("unexpected network call")
			})})
			listener := bufconn.Listen(1024 * 1024)
			rpcServer := grpc.NewServer()
			turingv1.RegisterIntegrationServiceServer(rpcServer, NewPublicServer(server))
			go func() { _ = rpcServer.Serve(listener) }()
			t.Cleanup(rpcServer.Stop)
			clientConn, err := grpc.NewClient("passthrough:///integrations", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = clientConn.Close() })
			client := turingv1.NewIntegrationServiceClient(clientConn)
			request := &turingv1.ConnectAccountRequest{Provider: kind, DisplayName: "Legacy client request", AccountLabel: "synthetic", Endpoint: "example.com", Credential: testCredential, ConsentAcknowledged: true}
			for _, credential := range []string{testCredential, testCredential + "\nInjected", strings.Repeat(testCredential, 200)} {
				request.Credential = credential
				// Refusal also precedes validation of provider-specific metadata.
				if credential != testCredential {
					request.Endpoint = "invalid endpoint/with/path"
				}
				for _, directRPC := range []bool{false, true} {
					var response *turingv1.Connection
					if directRPC {
						// No catalog fetch: an older client submits the existing wire request.
						response, err = client.ConnectAccount(ctx, request)
					} else {
						response, err = server.ConnectAccount(ctx, request)
					}
					if status.Code(err) != codes.FailedPrecondition || response != nil {
						t.Errorf("direct RPC=%v: response=%v error=%v", directRPC, response, err)
					}
					if !strings.Contains(status.Convert(err).Message(), "tools are not implemented") || strings.Contains(err.Error(), testCredential) {
						t.Errorf("refusal must give a fixed tools explanation: %v", err)
					}
				}
			}
			// Provider support precedes both consent and key configuration.
			server.sealer = nil
			request.ConsentAcknowledged = false
			_, err = server.ConnectAccount(ctx, request)
			if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "tools are not implemented") {
				t.Errorf("keyless refusal: %v", err)
			}
			if spy.seals.Load() != 0 || spy.opens.Load() != 0 || notifier.calls.Load() != 0 || network.Load() != 0 {
				t.Errorf("side effects: seal=%d open=%d registry=%d network=%d", spy.seals.Load(), spy.opens.Load(), notifier.calls.Load(), network.Load())
			}
			assertNoConnectionsStored(t, ctx, database)
			for _, table := range []string{"audit_logs", "events", "tools"} {
				var count int
				if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
					t.Errorf("%s rows=%d error=%v", table, count, err)
				}
			}
			assertPlaintextAbsentFromDatabase(t, ctx, database, testCredential)
		})
	}
}

func TestLegacyPowerlessConnectionsSurviveReopenAndExplicitCleanup(t *testing.T) {
	for _, keyMode := range []string{"original", "missing", "wrong"} {
		for _, kind := range powerlessProviders {
			t.Run(keyMode+"/"+kind.String(), func(t *testing.T) {
				ctx := context.Background()
				path := filepath.Join(t.TempDir(), "integrations.db")
				open := func() *db.DB {
					sqlDB, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
					if err != nil {
						t.Fatal(err)
					}
					sqlDB.SetMaxOpenConns(1)
					database := &db.DB{DB: sqlDB}
					t.Cleanup(func() { _ = database.Close() })
					if err := db.ApplyMigrations(ctx, database); err != nil {
						t.Fatal(err)
					}
					return database
				}
				database := open()
				repo := repository.New(database)
				sealer, err := secretbox.New(make([]byte, secretbox.KeySize))
				if err != nil {
					t.Fatal(err)
				}
				entry, _ := lookupProvider(kind)
				before := map[string]repository.Connection{}
				sealedBefore := map[string][]byte{}
				// Seed earlier-release rows directly, never through ConnectAccount.
				for _, id := range []string{"legacy_revoke", "legacy_delete", "legacy_revoked"} {
					sealed, err := sealer.Seal([]byte(testCredential), []byte(id))
					if err != nil {
						t.Fatal(err)
					}
					row, err := repo.CreateConnection(ctx, repository.NewConnection{
						ConnectionID: id, Provider: entry.storageKey, DisplayName: id,
						AccountLabel: "synthetic@example.com", Endpoint: "legacy.example.com",
						CredentialCiphertext: sealed, CredentialHint: redact(testCredential),
						GrantedScopes: []string{"Historical grant from an earlier release."},
					})
					if err != nil {
						t.Fatal(err)
					}
					if id == "legacy_revoked" {
						row, err = repo.RevokeConnection(ctx, id)
						if err != nil {
							t.Fatal(err)
						}
						sealed = nil
					}
					before[id], sealedBefore[id] = row, sealed
				}
				if err := database.Close(); err != nil {
					t.Fatal(err)
				}
				database = open()
				repo = repository.New(database)
				if keyMode == "wrong" {
					key := make([]byte, secretbox.KeySize)
					key[0] = 1
					sealer, err = secretbox.New(key)
					if err != nil {
						t.Fatal(err)
					}
				}
				spy := &forbiddenCredentialSealer{inner: sealer}
				server := New(repo, spy, audit.New(repo))
				if keyMode == "missing" {
					server.sealer = nil
				}
				server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Error("legacy management contacted a provider")
					return nil, errors.New("unexpected network call")
				})})
				listed, err := server.ListConnections(ctx, &turingv1.ListConnectionsRequest{})
				if err != nil || len(listed.GetConnections()) != 3 {
					t.Fatalf("list=%v error=%v", listed, err)
				}
				assertNoCredential(t, "legacy list", listed)
				for _, row := range listed.GetConnections() {
					id := row.GetConnectionId()
					fetched, err := server.GetConnection(ctx, &turingv1.GetConnectionRequest{ConnectionId: id})
					if err != nil || !proto.Equal(row, fetched) {
						t.Fatalf("get/list disagree: %v", err)
					}
					if row.GetProvider() != kind || row.GetStatus() != statusToProto(before[id].Status) || row.GetCredentialUnreadable() != (keyMode != "original" && id != "legacy_revoked") {
						t.Fatalf("legacy status changed: %v", row)
					}
					original := before[id]
					if row.GetDisplayName() != original.DisplayName || row.GetAccountLabel() != original.AccountLabel ||
						row.GetEndpoint() != original.Endpoint || row.GetCredentialHint() != original.CredentialHint ||
						!reflect.DeepEqual(row.GetGrantedScopes(), original.GrantedScopes) ||
						!proto.Equal(row.GetConsentGrantedAt(), parseTimestamp(original.ConsentGrantedAt)) ||
						!proto.Equal(row.GetConnectedAt(), parseTimestamp(original.ConnectedAt)) ||
						!proto.Equal(row.GetRevokedAt(), parseTimestamp(original.RevokedAt)) ||
						!proto.Equal(row.GetUpdatedAt(), parseTimestamp(original.UpdatedAt)) {
						t.Fatalf("public legacy metadata changed: %v", row)
					}
					stored, err := repo.GetConnection(ctx, id)
					if err != nil || !reflect.DeepEqual(stored, before[id]) {
						t.Fatalf("legacy metadata changed: before=%+v after=%+v error=%v", before[id], stored, err)
					}
					var sealed []byte
					if err := database.QueryRowContext(ctx, "SELECT credential_ciphertext FROM integration_connections WHERE id = ?", id).Scan(&sealed); err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(sealed, sealedBefore[id]) {
						t.Fatal("legacy credential was rewritten")
					}
				}
				tools, err := server.ListIntegrationTools(ctx, &turingv1.ListIntegrationToolsRequest{})
				if err != nil || len(tools.GetTools()) != 0 {
					t.Fatalf("legacy rows activated tools: %v %v", tools, err)
				}
				var auditCount int
				if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_logs").Scan(&auditCount); err != nil || auditCount != 0 {
					t.Fatalf("passive reads wrote audit rows: %d %v", auditCount, err)
				}
				revoked, err := server.RevokeConnection(ctx, &turingv1.RevokeConnectionRequest{ConnectionId: "legacy_revoke"})
				if err != nil || revoked.GetStatus() != turingv1.ConnectionStatus_CONNECTION_STATUS_REVOKED || revoked.GetCredentialHint() != "" || revoked.GetCredentialUnreadable() || revoked.GetRevokedAt() == nil {
					t.Fatalf("revoke=%v error=%v", revoked, err)
				}
				if !reflect.DeepEqual(revoked.GetGrantedScopes(), before["legacy_revoke"].GrantedScopes) {
					t.Fatal("revocation erased historical grants")
				}
				var remaining int
				if err := database.QueryRowContext(ctx, "SELECT coalesce(length(credential_ciphertext), 0) FROM integration_connections WHERE id = 'legacy_revoke'").Scan(&remaining); err != nil || remaining != 0 {
					t.Fatalf("credential survived revoke: %d %v", remaining, err)
				}
				for id := range before {
					if _, err := server.DeleteConnection(ctx, &turingv1.DeleteConnectionRequest{ConnectionId: id}); err != nil {
						t.Fatal(err)
					}
				}
				assertNoConnectionsStored(t, ctx, database)
				var auditPayloads string
				if err := database.QueryRowContext(ctx, "SELECT group_concat(payload_json) FROM audit_logs").Scan(&auditPayloads); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(auditPayloads, testCredential) || strings.Contains(auditPayloads, "••••") {
					t.Fatal("legacy cleanup audit retained credential material")
				}
				if spy.opens.Load() != 0 || spy.seals.Load() != 0 {
					t.Fatalf("legacy management touched secrets: opens=%d seals=%d", spy.opens.Load(), spy.seals.Load())
				}
				assertPlaintextAbsentFromDatabase(t, ctx, database, testCredential)
			})
		}
	}
}

func TestGitHubSealingFailureIsReachedAndRedacted(t *testing.T) {
	server, database, ctx := newIntegrationServer(t)
	spy := &forbiddenCredentialSealer{}
	server.sealer = spy
	_, err := server.ConnectAccount(ctx, &turingv1.ConnectAccountRequest{Provider: turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB, DisplayName: "GitHub", Credential: testCredential, ConsentAcknowledged: true})
	if spy.seals.Load() != 1 || status.Code(err) != codes.Internal || status.Convert(err).Message() != "could not store the credential" {
		t.Fatalf("seals=%d error=%v", spy.seals.Load(), err)
	}
	assertNoConnectionsStored(t, ctx, database)
}

func TestLegacyPowerlessRowsCannotDispatchTools(t *testing.T) {
	for _, kind := range powerlessProviders {
		t.Run(kind.String(), func(t *testing.T) {
			server, repo, database, runID, _ := integrationCallHarness(t, "synthetic-github-token")
			ctx := context.Background()
			entry, _ := lookupProvider(kind)
			const id = "legacy_tool_target"
			sealed, err := server.sealer.Seal([]byte(testCredential), []byte(id))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.CreateConnection(ctx, repository.NewConnection{
				ConnectionID: id, Provider: entry.storageKey, DisplayName: "Legacy account",
				CredentialCiphertext: sealed, CredentialHint: redact(testCredential),
				GrantedScopes: []string{"Historical grant."},
			}); err != nil {
				t.Fatal(err)
			}
			// Even a forged decision fixture naming the legacy row cannot turn
			// it into a GitHub connection or cause its credential to be read or opened.
			configureHarnessTool(t, repo, database, runID, "github.list_issues", []repository.IntegrationEndpointEgress{{
				Endpoint: repository.GitHubIntegrationEndpoint, EndpointHost: repository.GitHubIntegrationEndpointHost,
				ConnectionID: id, DisplayName: "Legacy account", Tools: []string{"github.list_issues"},
			}})
			if err := repo.SetToolPolicyByName(ctx, "integrations", "github.list_issues", "safe"); err != nil {
				t.Fatal(err)
			}
			// The temporary view observes actual reads of legacy ciphertext,
			// including reads that the service might discard without decrypting.
			var credentialReads atomic.Int32
			connection, err := database.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = connection.Raw(func(driverConnection any) error {
				return driverConnection.(*sqlite3.SQLiteConn).RegisterFunc("observe_legacy_credential", func(value []byte) []byte {
					credentialReads.Add(1)
					return value
				}, false)
			})
			_ = connection.Close()
			if err != nil {
				t.Fatal(err)
			}
			// db.Open keeps one pooled connection, so both dispatch reads use
			// this view. No production table or saved credential is changed.
			if _, err := database.ExecContext(ctx, `CREATE TEMP VIEW integration_connections AS
				SELECT id, provider, display_name, status,
					CASE WHEN provider = 'github' THEN credential_ciphertext
					ELSE observe_legacy_credential(credential_ciphertext) END AS credential_ciphertext
				FROM main.integration_connections`); err != nil {
				t.Fatal(err)
			}
			spy := &forbiddenCredentialSealer{inner: server.sealer}
			server.sealer = spy
			server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Error("legacy tool dispatch contacted a provider")
				return nil, errors.New("unexpected network call")
			})})
			args, err := structpb.NewStruct(map[string]any{"connection_id": id, "owner": "synthetic", "repo": "synthetic"})
			if err != nil {
				t.Fatal(err)
			}
			for tool, wantCode := range map[string]codes.Code{entry.storageKey + ".list": codes.NotFound, "github.list_issues": codes.FailedPrecondition} {
				response, err := server.CallIntegrationTool(ctx, &turingv1.CallIntegrationToolRequest{RunId: runID, ToolName: tool, Args: args})
				if status.Code(err) != wantCode || response != nil {
					t.Fatalf("legacy tool %s: response=%v, error=%v, want %v", tool, response, err, wantCode)
				}
				if strings.Contains(err.Error(), testCredential) {
					t.Fatal("legacy credential leaked in dispatch error")
				}
			}
			if err := server.validateImmediatelyBeforeIntegrationDispatch(ctx, runID, id, "github.list_issues", "safe", sealed); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("immediate dispatch recheck error=%v, want FailedPrecondition", err)
			}
			if credentialReads.Load() != 0 || spy.opens.Load() != 0 || spy.seals.Load() != 0 {
				t.Fatalf("legacy dispatch read ciphertext %d times, opened %d times, sealed %d times", credentialReads.Load(), spy.opens.Load(), spy.seals.Load())
			}
		})
	}
}

func TestIntegrationProviderWireValuesRemainStable(t *testing.T) {
	for value, name := range []string{"UNSPECIFIED", "IMAP", "CALDAV", "NOTION", "GITHUB", "GOOGLE_WORKSPACE", "MICROSOFT_365", "SLACK"} {
		var request turingv1.ConnectAccountRequest
		// Field 1, varint: a request encoded by an earlier release.
		if err := proto.Unmarshal([]byte{0x08, byte(value)}, &request); err != nil {
			t.Fatal(err)
		}
		if request.GetProvider().String() != "INTEGRATION_PROVIDER_"+name {
			t.Errorf("wire value %d decoded as %v", value, request.GetProvider())
		}
	}
}
