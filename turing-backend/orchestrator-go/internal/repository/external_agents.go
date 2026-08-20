package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

// Bounds are set here rather than in an editor, because the editor is one
// client of several and every one of these strings is copied into a job
// payload for each message sent to the agent.
const (
	maxExternalAgentNameRunes          = 120
	maxExternalAgentModelRunes         = 200
	maxExternalAgentBaseURLRunes       = 500
	maxExternalAgentCredentialRefRunes = 64
)

var (
	ErrExternalAgentNotFound            = errors.New("agent not found")
	ErrExternalAgentNameTaken           = errors.New("an agent with that name already exists")
	ErrExternalAgentNameEmpty           = errors.New("agent name is required")
	ErrExternalAgentNameTooLong         = errors.New("agent name is too long")
	ErrExternalAgentModelEmpty          = errors.New("agent model is required")
	ErrExternalAgentModelTooLong        = errors.New("agent model is too long")
	ErrExternalAgentBaseURLEmpty        = errors.New("agent base URL is required")
	ErrExternalAgentBaseURLInvalid      = errors.New("agent base URL must be an absolute http or https URL with no query or fragment")
	ErrExternalAgentBaseURLTooLong      = errors.New("agent base URL is too long")
	ErrExternalAgentBaseURLInsecure     = errors.New("a base URL that is not on this machine must use https")
	ErrExternalAgentBaseURLCredentials  = errors.New("agent base URL must not carry a username or password")
	ErrExternalAgentCredentialRefEmpty  = errors.New("agent credential name is required")
	ErrExternalAgentCredentialRefFormat = errors.New("agent credential name may only contain letters, digits, dot, dash and underscore")
	ErrExternalAgentCredentialRefLong   = errors.New("agent credential name is too long")
	ErrExternalAgentProviderInvalid     = errors.New("agent provider is unsupported")
)

// externalAgentProviders is the closed set the column accepts. Kept as strings
// rather than integers so a row read by a human, or by a build that predates a
// new vendor, still says what it means.
var externalAgentProviders = map[string]struct{}{
	"anthropic": {},
	"openai":    {},
	"google":    {},
	"xai":       {},
	"other":     {},
}

// ExternalAgent is an assistant that does not run on this machine.
//
// There is no API key field, and that is the point: CredentialRef names a key
// the backend resolves from its own environment. See 0007_agents.sql.
type ExternalAgent struct {
	AgentID       string
	DisplayName   string
	Provider      string
	BaseURL       string
	Model         string
	CredentialRef string
	CreatedAt     string
	UpdatedAt     string
}

// externalAgentColumns is the one place the column list lives. Every read below
// selects exactly this and scans it with scanExternalAgent, so adding a column
// is one edit rather than four that must agree.
const externalAgentColumns = `id, display_name, provider, base_url, model, credential_ref, created_at, updated_at`

// scanRow is what *sql.Row and *sql.Rows have in common, so one scan serves
// single-row reads inside and outside a transaction as well as list reads.
type scanRow interface {
	Scan(dest ...any) error
}

func scanExternalAgent(row scanRow) (ExternalAgent, error) {
	var agent ExternalAgent
	err := row.Scan(&agent.AgentID, &agent.DisplayName, &agent.Provider, &agent.BaseURL,
		&agent.Model, &agent.CredentialRef, &agent.CreatedAt, &agent.UpdatedAt)
	return agent, err
}

// ExternalAgentInput is what a caller supplies for a create or an update.
type ExternalAgentInput struct {
	DisplayName   string
	Provider      string
	BaseURL       string
	Model         string
	CredentialRef string
}

// ExternalAgentTarget is what a queued job carries: enough to reach the agent
// and to name it in the transcript, and nothing else.
//
// The json tags are load-bearing. This struct is written into
// jobs.payload_json and read back when the job is claimed, possibly by a
// different build; adding tags later would silently decode every already
// queued job's target to empty strings, which would route a run that was meant
// for a cloud agent at an empty base URL.
type ExternalAgentTarget struct {
	DisplayName   string `json:"displayName"`
	BaseURL       string `json:"baseUrl"`
	CredentialRef string `json:"credentialRef"`
}

func validateExternalAgent(input ExternalAgentInput) (ExternalAgentInput, error) {
	cleaned := ExternalAgentInput{
		DisplayName:   strings.TrimSpace(input.DisplayName),
		Provider:      strings.TrimSpace(strings.ToLower(input.Provider)),
		BaseURL:       strings.TrimSpace(input.BaseURL),
		Model:         strings.TrimSpace(input.Model),
		CredentialRef: strings.TrimSpace(input.CredentialRef),
	}
	switch {
	case cleaned.DisplayName == "":
		return ExternalAgentInput{}, ErrExternalAgentNameEmpty
	case len([]rune(cleaned.DisplayName)) > maxExternalAgentNameRunes:
		return ExternalAgentInput{}, ErrExternalAgentNameTooLong
	case cleaned.Model == "":
		return ExternalAgentInput{}, ErrExternalAgentModelEmpty
	case len([]rune(cleaned.Model)) > maxExternalAgentModelRunes:
		return ExternalAgentInput{}, ErrExternalAgentModelTooLong
	}
	if _, ok := externalAgentProviders[cleaned.Provider]; !ok {
		return ExternalAgentInput{}, ErrExternalAgentProviderInvalid
	}
	if err := validateExternalAgentBaseURL(cleaned.BaseURL); err != nil {
		return ExternalAgentInput{}, err
	}
	if err := validateCredentialRef(cleaned.CredentialRef); err != nil {
		return ExternalAgentInput{}, err
	}
	return cleaned, nil
}

// validateExternalAgentBaseURL rejects anything that is not a plain absolute
// http(s) endpoint, and refuses plaintext http to anywhere but this machine.
//
// The https rule is not pedantry. Everything routed to one of these carries
// the whole conversation, and an http endpoint on someone else's network would
// carry it in the clear — a privacy hole no wording in the client could
// honestly warn about. Loopback and the Docker host alias stay allowed so a
// local OpenAI-compatible gateway is still usable, since traffic there has not
// left the machine.
func validateExternalAgentBaseURL(baseURL string) error {
	if baseURL == "" {
		return ErrExternalAgentBaseURLEmpty
	}
	if len([]rune(baseURL)) > maxExternalAgentBaseURLRunes {
		return ErrExternalAgentBaseURLTooLong
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return ErrExternalAgentBaseURLInvalid
	}
	// https://user:sk-secret@host/v1 passes every check above — it is absolute,
	// https, has a hostname, and carries no query or fragment — and Go's HTTP
	// client turns that userinfo into an Authorization header. It IS a
	// credential. Accepting it would put a third-party secret in the one place
	// this whole design exists to keep clean: the base_url column, from which it
	// would be copied into every job payload, sent over the runtime stream, and
	// handed back to any client that lists agents. Refused here, at the only
	// door into that column.
	if parsed.User != nil {
		return ErrExternalAgentBaseURLCredentials
	}
	if parsed.Scheme == "http" && !isLocalHostname(parsed.Hostname()) {
		return ErrExternalAgentBaseURLInsecure
	}
	return nil
}

func isLocalHostname(hostname string) bool {
	switch strings.ToLower(hostname) {
	case "localhost", "host.docker.internal":
		return true
	}
	if ip := net.ParseIP(hostname); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// validateCredentialRef bounds the one string that crosses into an
// environment lookup. Restricting it to an obvious identifier alphabet keeps a
// crafted name from becoming anything but a failed lookup.
func validateCredentialRef(ref string) error {
	if ref == "" {
		return ErrExternalAgentCredentialRefEmpty
	}
	if len([]rune(ref)) > maxExternalAgentCredentialRefRunes {
		return ErrExternalAgentCredentialRefLong
	}
	for _, character := range ref {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '_', character == '-', character == '.':
			continue
		default:
			return ErrExternalAgentCredentialRefFormat
		}
	}
	return nil
}

func (r *Repository) CreateExternalAgent(ctx context.Context, input ExternalAgentInput) (ExternalAgent, error) {
	cleaned, err := validateExternalAgent(input)
	if err != nil {
		return ExternalAgent{}, err
	}
	createdAt := now()
	agent := ExternalAgent{
		AgentID:       ids.New("agent"),
		DisplayName:   cleaned.DisplayName,
		Provider:      cleaned.Provider,
		BaseURL:       cleaned.BaseURL,
		Model:         cleaned.Model,
		CredentialRef: cleaned.CredentialRef,
		CreatedAt:     createdAt,
		UpdatedAt:     createdAt,
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO external_agents (id, display_name, provider, base_url, model, credential_ref, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		agent.AgentID, agent.DisplayName, agent.Provider, agent.BaseURL, agent.Model, agent.CredentialRef, createdAt, createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			return ExternalAgent{}, ErrExternalAgentNameTaken
		}
		return ExternalAgent{}, err
	}
	return agent, nil
}

func (r *Repository) UpdateExternalAgent(ctx context.Context, agentID string, input ExternalAgentInput) (ExternalAgent, error) {
	cleaned, err := validateExternalAgent(input)
	if err != nil {
		return ExternalAgent{}, err
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE external_agents
		SET display_name = ?, provider = ?, base_url = ?, model = ?, credential_ref = ?, updated_at = ?
		WHERE id = ?`,
		cleaned.DisplayName, cleaned.Provider, cleaned.BaseURL, cleaned.Model, cleaned.CredentialRef, now(), agentID)
	if err != nil {
		if isUniqueViolation(err) {
			return ExternalAgent{}, ErrExternalAgentNameTaken
		}
		return ExternalAgent{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ExternalAgent{}, err
	}
	if affected == 0 {
		return ExternalAgent{}, ErrExternalAgentNotFound
	}
	return r.GetExternalAgent(ctx, agentID)
}

func (r *Repository) GetExternalAgent(ctx context.Context, agentID string) (ExternalAgent, error) {
	agent, err := scanExternalAgent(r.db.QueryRowContext(ctx,
		`SELECT `+externalAgentColumns+` FROM external_agents WHERE id = ?`, agentID))
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalAgent{}, ErrExternalAgentNotFound
	}
	return agent, err
}

// DeleteExternalAgent removes an agent, which also returns every conversation
// routed to it to the local assistant (ON DELETE CASCADE). Failing towards
// "stays on this machine" is the only safe direction.
func (r *Repository) DeleteExternalAgent(ctx context.Context, agentID string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM external_agents WHERE id = ?`, agentID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrExternalAgentNotFound
	}
	return nil
}

// ListExternalAgents orders by name so the list reads the same way every
// visit. A list that reorders itself between visits is one nobody can learn.
func (r *Repository) ListExternalAgents(ctx context.Context) ([]ExternalAgent, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+externalAgentColumns+` FROM external_agents ORDER BY display_name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var agents []ExternalAgent
	for rows.Next() {
		agent, err := scanExternalAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

// SetSessionAgent routes a conversation to an agent, replacing wherever it was
// going before. Idempotent: choosing the same destination twice is what a
// double tap looks like and it means the same as choosing it once.
func (r *Repository) SetSessionAgent(ctx context.Context, sessionID string, agentID string) (ExternalAgent, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ExternalAgent{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := requireActiveSessionTx(ctx, tx, sessionID); err != nil {
		return ExternalAgent{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_external_agent (session_id, agent_id, routed_at) VALUES (?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET agent_id = excluded.agent_id, routed_at = excluded.routed_at`,
		sessionID, agentID, now()); err != nil {
		if isForeignKeyViolation(err) {
			// The constraint does not say which side is missing, and telling
			// someone their agent is gone when it is the conversation that
			// went stale sends them to look in the wrong place.
			//
			// Probed on tx, NOT on the pool. The connection pool is capped at
			// one connection (db.Open), so a read issued through r.db while
			// this transaction still holds that connection waits for a
			// connection the transaction will not release until the read
			// returns — a deadlock that only shows up on the error path.
			return ExternalAgent{}, missingRouteSideTx(ctx, tx, sessionID)
		}
		return ExternalAgent{}, err
	}
	agent, err := scanExternalAgent(tx.QueryRowContext(ctx,
		`SELECT `+externalAgentColumns+` FROM external_agents WHERE id = ?`, agentID))
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalAgent{}, ErrExternalAgentNotFound
	}
	if err != nil {
		return ExternalAgent{}, err
	}
	// Pointing a conversation at a third party is the most consequential thing
	// this app lets someone do, so it leaves a record — in the same
	// transaction as the routing itself, or the record could disagree with
	// reality. The endpoint host, not the full URL: enough to say who the
	// recipient is without copying a string a user might have pasted anything
	// into.
	if err := recordAuditTx(ctx, tx, "", "client", "", "session.routed", sessionID,
		auditPayload(map[string]any{
			"agentId":  agent.AgentID,
			"agent":    agent.DisplayName,
			"endpoint": ExternalAgentEndpointHost(agent.BaseURL),
			"model":    agent.Model,
		})); err != nil {
		return ExternalAgent{}, err
	}
	if err := tx.Commit(); err != nil {
		return ExternalAgent{}, err
	}
	return agent, nil
}

// auditPayload marshals an audit payload, degrading to an empty object rather
// than failing the change it describes. An unrecordable detail is worth less
// than the routing decision itself succeeding.
func auditPayload(fields map[string]any) string {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// missingRouteSideTx probes which end of a failed route does not exist. Only
// reached on the error path, so the extra read costs nothing in the normal
// case. Takes the transaction rather than the repository because the pool
// holds a single connection — see the call site.
func missingRouteSideTx(ctx context.Context, tx *sql.Tx, sessionID string) error {
	var exists int
	err := tx.QueryRowContext(ctx, sessionExistsQuery, sessionID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSessionNotFound
	}
	if err != nil {
		return err
	}
	// The conversation exists, so the other end of the constraint is what does
	// not.
	return ErrExternalAgentNotFound
}

// ClearSessionAgent returns a conversation to the local assistant. Clearing a
// conversation that was already local succeeds: the caller asked for a state,
// and that state is what it gets.
func (r *Repository) ClearSessionAgent(ctx context.Context, sessionID string) error {
	if err := r.requireSession(ctx, sessionID); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := requireActiveSessionTx(ctx, tx, sessionID); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM session_external_agent WHERE session_id = ?`, sessionID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	// Only when something actually changed. Recording every no-op clear would
	// bury the routing decisions the log exists to show.
	if affected > 0 {
		if err := recordAuditTx(ctx, tx, "", "client", "", "session.unrouted", sessionID, "{}"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetSessionAgent reports where a conversation's messages go. The boolean is
// false for the local assistant, which is not an absence of configuration but
// the default and the common case.
func (r *Repository) GetSessionAgent(ctx context.Context, sessionID string) (ExternalAgent, bool, error) {
	if err := r.requireSession(ctx, sessionID); err != nil {
		return ExternalAgent{}, false, err
	}
	agent, err := scanExternalAgent(r.db.QueryRowContext(ctx, sessionAgentQuery, sessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalAgent{}, false, nil
	}
	if err != nil {
		return ExternalAgent{}, false, err
	}
	return agent, true, nil
}

// sessionAgentQuery is shared by the read-committed path and the one inside
// the enqueue transaction, so the two can never disagree about what a
// conversation's destination is.
const sessionAgentQuery = `
	SELECT a.id, a.display_name, a.provider, a.base_url, a.model, a.credential_ref, a.created_at, a.updated_at
	FROM session_external_agent r
	JOIN external_agents a ON a.id = r.agent_id
	WHERE r.session_id = ?`

const sessionExistsQuery = `SELECT 1 FROM sessions WHERE id = ?`

func (r *Repository) requireSession(ctx context.Context, sessionID string) error {
	var deletionState string
	err := r.db.QueryRowContext(ctx, `SELECT deletion_state FROM sessions WHERE id = ?`, sessionID).Scan(&deletionState)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSessionNotFound
	}
	if err != nil {
		return err
	}
	if deletionState != "active" {
		return ErrSessionDeleting
	}
	return nil
}

// sessionExternalAgentTx reads the destination to freeze into a job payload.
// It runs inside the enqueue transaction so the snapshot matches the message
// the user actually sent: re-routing, editing or deleting the agent while the
// job waits in the queue must not change where an accepted message goes, and
// must not silently redirect a transcript to a different company.
func sessionExternalAgentTx(ctx context.Context, tx *sql.Tx, sessionID string) (ExternalAgent, bool, error) {
	agent, err := scanExternalAgent(tx.QueryRowContext(ctx, sessionAgentQuery, sessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalAgent{}, false, nil
	}
	if err != nil {
		return ExternalAgent{}, false, err
	}
	return agent, true, nil
}

// ExternalAgentEndpointHost is what the "leaves your machine" notice names.
// The host alone, not the full URL: it is the part that identifies who
// received the conversation, and a path adds noise to a sentence that has to
// be read in a hurry.
func ExternalAgentEndpointHost(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return baseURL
	}
	return parsed.Host
}
