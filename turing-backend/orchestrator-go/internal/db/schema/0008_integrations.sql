-- Integrations: third-party accounts the user connects with a credential they
-- minted themselves (an app password, an internal integration token, a PAT).
--
-- SECRET AT REST. This table is the first place in TuringAgent where a secret
-- lives in the database rather than in turing-backend/.env. That is a
-- deliberate change to the project's secret story and the reason it is
-- acceptable is that the secret is not stored in the clear:
-- credential_ciphertext holds an AES-256-GCM sealed box whose key is
-- TURING_INTEGRATION_KEY, read from .env (chmod 600, gitignored) and never
-- written to this file. A copy of data/turing.db — a backup, a synced folder,
-- an accidental commit — therefore yields the provider, the account label and
-- four characters of hint, but no usable credential.
--
-- What it does NOT protect against, stated so nobody mistakes it for more
-- than it is: anyone who can read the whole turing-backend directory gets
-- .env as well and can decrypt everything here; so can anyone who can read
-- the orchestrator's memory or environment. This is protection against a
-- stray copy of the database, not against an attacker on the machine.
CREATE TABLE IF NOT EXISTS integration_connections (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  display_name TEXT NOT NULL,
  -- The non-secret account identifier: an address, a workspace, a username.
  account_label TEXT NOT NULL DEFAULT '',
  -- Host for providers that are not a single hosted service (IMAP, CalDAV).
  endpoint TEXT NOT NULL DEFAULT '',
  -- Sealed, never plaintext. The first five bytes are a format version and a
  -- fingerprint of the key, which is what lets a reader say "the key that
  -- sealed this is gone" without opening anything. The rest is bound to this
  -- row's id, so a sealed value moved into another row will not open. NULL
  -- once revoked: revocation destroys the credential rather than flipping a
  -- flag beside it.
  credential_ciphertext BLOB,
  -- A redaction produced when the credential was stored: bullets and at most
  -- four trailing characters. Deliberately NOT sealed — listing connections
  -- would otherwise have to decrypt every row to draw a label, and four
  -- characters beside an account label that is already in the clear is not
  -- the part worth protecting. It is the one thing here derived from the
  -- secret, and this is the decision to keep it that way.
  credential_hint TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  -- What the user agreed the credential grants, captured at consent time. A
  -- later build that widens a provider's grants does not get to claim this
  -- connection was consented to the wider set.
  granted_scopes_json TEXT NOT NULL,
  consent_granted_at TEXT NOT NULL,
  connected_at TEXT NOT NULL,
  revoked_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

-- Two live connections sharing a name would make both ambiguous everywhere
-- they are referred to. Revoked ones are excluded so that reconnecting an
-- account under the name it already had is not blocked by its own history.
CREATE UNIQUE INDEX IF NOT EXISTS integration_connections_name_unique
  ON integration_connections (display_name COLLATE NOCASE)
  WHERE status = 'connected';

CREATE INDEX IF NOT EXISTS integration_connections_by_status
  ON integration_connections (status, created_at);
