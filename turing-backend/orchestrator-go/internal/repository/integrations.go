package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Bounded here rather than in an editor, which is one client of several.
const (
	maxConnectionNameRunes         = 120
	maxConnectionAccountLabelRunes = 320
	maxConnectionEndpointRunes     = 255
)

const (
	ConnectionStatusConnected = "connected"
	ConnectionStatusRevoked   = "revoked"
)

// credentialHeaderBytes is how much of the sealed value the read path is
// allowed to see: its format version and key fingerprint, and not one byte of
// ciphertext. Kept in step with secretbox.HeaderSize by the test that seals a
// real value and compares the two.
const credentialHeaderBytes = 5

func credentialHeader(sealed []byte) []byte {
	if len(sealed) < credentialHeaderBytes {
		return nil
	}
	return append([]byte(nil), sealed[:credentialHeaderBytes]...)
}

var (
	ErrConnectionNotFound         = errors.New("connection not found")
	ErrConnectionNameTaken        = errors.New("a connection with that name already exists")
	ErrConnectionNameEmpty        = errors.New("connection name is required")
	ErrConnectionNameTooLong      = errors.New("connection name is too long")
	ErrConnectionAccountTooLong   = errors.New("account label is too long")
	ErrConnectionEndpointTooLong  = errors.New("endpoint is too long")
	ErrConnectionEndpointInvalid  = errors.New("endpoint must be a bare host name, optionally with a port")
	ErrConnectionProviderRequired = errors.New("provider is required")
	ErrConnectionIDRequired       = errors.New("connection id is required")
	ErrConnectionSecretRequired   = errors.New("a sealed credential is required")
	ErrConnectionAlreadyRevoked   = errors.New("connection is already revoked")
	ErrConnectionConsentRequired  = errors.New("what the connection grants must be recorded")
)

// Connection is what a read of the table yields. There is deliberately no
// credential field on it: the sealed secret is written by CreateConnection and
// read by nothing in this file, so no list, get or mapping can leak it by
// accident. The read path never even needs the key.
type Connection struct {
	ConnectionID   string
	Provider       string
	DisplayName    string
	AccountLabel   string
	Endpoint       string
	CredentialHint string
	// CredentialHeader is the first few bytes of the sealed value — its
	// format version and key fingerprint, never any part of the secret. It is
	// read so a caller can say "the key that sealed this is gone" without
	// opening anything, and is empty once revoked.
	CredentialHeader []byte
	Status           string
	GrantedScopes    []string
	ConsentGrantedAt string
	ConnectedAt      string
	RevokedAt        string
	CreatedAt        string
	UpdatedAt        string
}

// NewConnection is the write side. The credential arrives already sealed —
// the plaintext the user pasted never reaches this package, so it cannot
// reach a SQL statement, a query log, or a scan destination.
type NewConnection struct {
	// ConnectionID is chosen by the caller, because the credential is sealed
	// against it before it gets here: the sealed value is bound to this exact
	// row and will not open under another.
	ConnectionID         string
	Provider             string
	DisplayName          string
	AccountLabel         string
	Endpoint             string
	CredentialCiphertext []byte
	CredentialHint       string
	GrantedScopes        []string
}

func validateConnection(input NewConnection) (NewConnection, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.AccountLabel = strings.TrimSpace(input.AccountLabel)
	input.Endpoint = strings.TrimSpace(input.Endpoint)
	input.ConnectionID = strings.TrimSpace(input.ConnectionID)
	switch {
	case input.ConnectionID == "":
		return NewConnection{}, ErrConnectionIDRequired
	case input.Provider == "":
		return NewConnection{}, ErrConnectionProviderRequired
	case input.DisplayName == "":
		return NewConnection{}, ErrConnectionNameEmpty
	case len([]rune(input.DisplayName)) > maxConnectionNameRunes:
		return NewConnection{}, ErrConnectionNameTooLong
	case len([]rune(input.AccountLabel)) > maxConnectionAccountLabelRunes:
		return NewConnection{}, ErrConnectionAccountTooLong
	case len([]rune(input.Endpoint)) > maxConnectionEndpointRunes:
		return NewConnection{}, ErrConnectionEndpointTooLong
	case !isPlausibleHost(input.Endpoint):
		return NewConnection{}, ErrConnectionEndpointInvalid
	case len(input.CredentialCiphertext) == 0:
		return NewConnection{}, ErrConnectionSecretRequired
	case len(input.GrantedScopes) == 0:
		return NewConnection{}, ErrConnectionConsentRequired
	}
	return input, nil
}

// CreateConnection stores a connected account.
//
// SECRET AT REST — this is the point of storage. credential_ciphertext is an
// AES-256-GCM sealed box; its key lives in turing-backend/.env, never in this
// database. See internal/secretbox and schema/0008_integrations.sql for what
// that does and does not protect against. Nothing else about a connection is
// secret, and nothing here is written to an event or an audit row.
// isPlausibleHost keeps a URL, a path, or anything with structure in it out of
// a column that a mail or calendar client will eventually dial. It is not a
// full host parser and does not pretend to be one — it rejects the shapes that
// would change what "connect to this endpoint" means.
func isPlausibleHost(endpoint string) bool {
	if endpoint == "" {
		return true // absent is fine; whether one is required is the provider's business
	}
	if strings.ContainsAny(endpoint, " \t\r\n/\\?#@") {
		return false
	}
	if strings.Contains(endpoint, "://") {
		return false
	}
	if strings.HasPrefix(endpoint, ".") || strings.Contains(endpoint, "..") {
		return false
	}
	for _, r := range endpoint {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (r *Repository) CreateConnection(ctx context.Context, input NewConnection) (Connection, error) {
	input, err := validateConnection(input)
	if err != nil {
		return Connection{}, err
	}
	scopesJSON, err := json.Marshal(input.GrantedScopes)
	if err != nil {
		return Connection{}, err
	}
	createdAt := now()
	connection := Connection{
		ConnectionID:     input.ConnectionID,
		Provider:         input.Provider,
		DisplayName:      input.DisplayName,
		AccountLabel:     input.AccountLabel,
		Endpoint:         input.Endpoint,
		CredentialHint:   input.CredentialHint,
		CredentialHeader: credentialHeader(input.CredentialCiphertext),
		Status:           ConnectionStatusConnected,
		GrantedScopes:    input.GrantedScopes,
		ConsentGrantedAt: createdAt,
		ConnectedAt:      createdAt,
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO integration_connections (
			id, provider, display_name, account_label, endpoint,
			credential_ciphertext, credential_hint, status, granted_scopes_json,
			consent_granted_at, connected_at, revoked_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
		connection.ConnectionID, connection.Provider, connection.DisplayName,
		connection.AccountLabel, connection.Endpoint,
		input.CredentialCiphertext, connection.CredentialHint,
		connection.Status, string(scopesJSON),
		createdAt, createdAt, createdAt, createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Connection{}, ErrConnectionNameTaken
		}
		return Connection{}, err
	}
	return connection, nil
}

// ListConnections puts live connections above revoked ones, then orders by
// name so the list reads the same way every visit. Revoked ones stay visible
// on purpose: an account that once had standing access to your mail is a
// thing you should still be able to see a record of.
func (r *Repository) ListConnections(ctx context.Context) ([]Connection, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, provider, display_name, account_label, endpoint, credential_hint,
		       substr(credential_ciphertext, 1, ?), status, granted_scopes_json,
		       consent_granted_at, connected_at, revoked_at, created_at, updated_at
		FROM integration_connections
		ORDER BY CASE status WHEN 'connected' THEN 0 ELSE 1 END,
		         display_name COLLATE NOCASE, id`, credentialHeaderBytes)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var connections []Connection
	for rows.Next() {
		connection, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		connections = append(connections, connection)
	}
	return connections, rows.Err()
}

func (r *Repository) GetConnection(ctx context.Context, connectionID string) (Connection, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, provider, display_name, account_label, endpoint, credential_hint,
		       substr(credential_ciphertext, 1, ?), status, granted_scopes_json,
		       consent_granted_at, connected_at, revoked_at, created_at, updated_at
		FROM integration_connections WHERE id = ?`, credentialHeaderBytes, connectionID)
	connection, err := scanConnection(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrConnectionNotFound
	}
	return connection, err
}

// RevokeConnection destroys the stored credential.
//
// It overwrites the ciphertext column with NULL rather than setting a flag
// beside it, because a revocation that leaves the secret readable is not a
// revocation. The match is on "not already revoked" rather than on
// "connected", so a row whose status this build does not recognise still gets
// its credential destroyed — refusing to revoke something we cannot classify
// is the one outcome that must not happen here.
//
// Two honest limits: SQLite runs here with secure_delete off and WAL on, so
// the freed bytes can survive in the file until they are reused or the
// database is VACUUMed (they are ciphertext, and the key is not in this
// file); and nothing Turing does can invalidate the credential at the
// provider — only the provider can, by deleting the app password or token.
func (r *Repository) RevokeConnection(ctx context.Context, connectionID string) (Connection, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Connection{}, err
	}
	defer func() { _ = tx.Rollback() }()

	revokedAt := now()
	result, err := tx.ExecContext(ctx, `
		UPDATE integration_connections
		SET credential_ciphertext = NULL, credential_hint = '', status = ?,
		    revoked_at = ?, updated_at = ?
		WHERE id = ? AND status != ?`,
		ConnectionStatusRevoked, revokedAt, revokedAt, connectionID, ConnectionStatusRevoked)
	if err != nil {
		return Connection{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Connection{}, err
	}
	// The read happens inside the same transaction as the write. Outside it, a
	// concurrent delete between the two would turn a revocation that really
	// happened into a "not found", which is the one report that would leave
	// someone believing their credential is still live.
	row := tx.QueryRowContext(ctx, `
		SELECT id, provider, display_name, account_label, endpoint, credential_hint,
		       substr(credential_ciphertext, 1, ?), status, granted_scopes_json,
		       consent_granted_at, connected_at, revoked_at, created_at, updated_at
		FROM integration_connections WHERE id = ?`, credentialHeaderBytes, connectionID)
	connection, err := scanConnection(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrConnectionNotFound
	}
	if err != nil {
		return Connection{}, err
	}
	if affected == 0 {
		// The row exists and nothing changed, so it was already revoked. The
		// two cases mean different things to whoever pressed the button.
		return Connection{}, ErrConnectionAlreadyRevoked
	}
	if err := tx.Commit(); err != nil {
		return Connection{}, err
	}
	return connection, nil
}

// DeleteConnection removes the row, credential included. Revoking first is not
// required — deleting a live connection destroys the secret in the same
// statement.
func (r *Repository) DeleteConnection(ctx context.Context, connectionID string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM integration_connections WHERE id = ?`, connectionID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrConnectionNotFound
	}
	return nil
}

type connectionScanner interface {
	Scan(dest ...any) error
}

func scanConnection(row connectionScanner) (Connection, error) {
	var connection Connection
	var scopesJSON string
	var revokedAt sql.NullString
	if err := row.Scan(
		&connection.ConnectionID, &connection.Provider, &connection.DisplayName,
		&connection.AccountLabel, &connection.Endpoint, &connection.CredentialHint,
		&connection.CredentialHeader, &connection.Status, &scopesJSON,
		&connection.ConsentGrantedAt, &connection.ConnectedAt, &revokedAt,
		&connection.CreatedAt, &connection.UpdatedAt,
	); err != nil {
		return Connection{}, err
	}
	connection.RevokedAt = revokedAt.String
	if err := json.Unmarshal([]byte(scopesJSON), &connection.GrantedScopes); err != nil {
		return Connection{}, err
	}
	return connection, nil
}
