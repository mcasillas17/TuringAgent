package audit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/safejson"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Request bounds. Every one of these caps a value a client controls, so it is
// enforced before any database work happens.
const (
	// defaultAuditPageLimit is the page size used when the client sends no
	// limit (0). It is a documented default, not a silent minimum.
	defaultAuditPageLimit = 50
	// maxAuditPageLimit matches the repository's own hard ceiling on Limit.
	maxAuditPageLimit = 100
	// maxAuditCursorBytes bounds the encoded (base64) cursor a client may send
	// back, so a hostile client cannot make us base64-decode an arbitrarily
	// large blob before we reject it. It also transitively bounds the decoded
	// size: raw-URL-base64 yields at most 6/8 of the encoded length, so 2048
	// encoded bytes can never decode to more than ~1536 bytes — no separate
	// decoded-size cap is needed.
	maxAuditCursorBytes = 2048
	// maxAuditCorrelationIDBytes / maxAuditActionBytes bound the exact-match
	// filter strings. They are matched verbatim against stored columns, so an
	// over-long value can only ever match nothing — reject it up front.
	maxAuditCorrelationIDBytes = 256
	maxAuditActionBytes        = 128
	// auditCursorVersion is the only cursor version this build emits or
	// accepts. A mismatch means the cursor came from a different contract and
	// is rejected rather than reinterpreted.
	auditCursorVersion = 1
	// auditCursorKeyDomain domain-separates the cursor MAC key derived from a
	// configured secret, so the same secret used elsewhere in the process can
	// never yield this key. It is a fixed label, never client input.
	auditCursorKeyDomain = "turing.audit.cursor.v1"
)

// Upper bounds (in bytes) on individual projected structural metadata fields.
// Stored metadata is untrusted — the repository already caps how many bytes it
// reads per column, and the service enforces these tighter, field-specific
// bounds on top. A required field (audit_id, actor_type, action) that is empty
// or fails any structural check projects the fixed redactedAuditMetadata
// literal; an optional field (correlation_id, actor_id, target) that fails is
// omitted. Two optional fields are additionally omitted by action regardless
// of how safe their bytes are — see auditDisclosureFor below.
const (
	maxAuditIDMetadataBytes          = 256
	maxAuditCorrelationMetadataBytes = 256
	maxAuditActorTypeMetadataBytes   = 32
	maxAuditActorIDMetadataBytes     = 256
	maxAuditActionMetadataBytes      = 128
	maxAuditTargetMetadataBytes      = 512
)

// redactedAuditMetadata is the fixed literal a required structural field is
// replaced with when its stored value is not structurally safe (invalid UTF-8,
// over its byte bound, or carrying control characters) or is empty. It is a
// constant, never the stored value, so redaction can never echo untrusted
// bytes, and an empty required column (including an over-read-bound one the
// repository collapsed to ”) never surfaces as a bare empty string.
const redactedAuditMetadata = "[redacted]"

// Action-scoped structural omissions. Bounds and character checks decide
// whether a stored value is safe to *put on the wire*; these decide whether a
// value is safe to *disclose at all*, which is a separate question a byte
// bound cannot answer. Both are stored in structurally clean columns, so
// nothing else in the projection would ever drop them:
//
//   - auditApprovalActionPrefix: every approval.* row is written with the
//     approval id as its target (service/approvals/service.go), and
//     signApprovalToken puts that same approval id in the `jti` claim of the
//     short-lived JWT that authorizes a mutating file tool. The public
//     contract is that a JTI is never returned, so target is omitted for the
//     whole family. A prefix rather than a five-way list is the safe default:
//     a future approval action inherits the omission instead of leaking a JTI
//     until someone notices.
//   - auditPeerActorAction: auth.failed is the one action recorded with the
//     caller's peer address as actor_id (app.go's persistAuthFailure), and the
//     contract is that a recorded peer address is never returned. Its method
//     (target) and its allowlisted method/requestId payload still are, which
//     is what keeps the row useful.
//
// Neither rule rewrites what is stored — the audit log keeps the full record —
// they only bound what this read API discloses.
const (
	auditApprovalActionPrefix = "approval."
	auditPeerActorAction      = "auth.failed"
)

// auditMetadataDisclosure says which optional structural fields an action may
// disclose. omitTarget is decided from both the repository's
// ActionHasApprovalPrefix bit — derived in-query from the original action's
// first 9 bytes, so it survives the bounded Action column collapsing an
// oversized approval.* action to "" — and, as defense for directly constructed
// records (tests, callers that bypass the repository), whether the raw mapped
// action itself begins with the approval. prefix. Either being true fails
// closed. omitActorID is still keyed on the raw action; auth.failed's action is
// far shorter than the read bound, so it never collapses.
type auditMetadataDisclosure struct {
	omitActorID bool
	omitTarget  bool
}

func auditDisclosureFor(record *repository.AuditRecord) auditMetadataDisclosure {
	return auditMetadataDisclosure{
		omitActorID: record.Action == auditPeerActorAction,
		omitTarget:  record.ActionHasApprovalPrefix || strings.HasPrefix(record.Action, auditApprovalActionPrefix),
	}
}

// Upper bounds (in bytes) on individual projected payload string fields. Each
// is <= 512. A stored value longer than its bound is omitted from the
// response, never truncated, so a reader can never coerce an over-long stored
// value into an accepted shorter one. payload_json is untrusted stored data;
// these bounds are what keep one row from returning an unbounded string.
const (
	maxAuditToolNameBytes       = 512
	maxAuditServerNameBytes     = 512
	maxAuditPhaseBytes          = 128
	maxAuditStatusBytes         = 128
	maxAuditReasonBytes         = 512
	maxAuditErrorCodeBytes      = 256
	maxAuditProviderBytes       = 256
	maxAuditDisplayNameBytes    = 512
	maxAuditAutomationIDBytes   = 256
	maxAuditAutomationNameBytes = 512
	maxAuditMethodBytes         = 128
	maxAuditRequestIDBytes      = 256
	// maxAuditDecisionRationaleBytes matches the approvals writer's own
	// maxAuditRationaleBytes: the writer already bounds a human comment or
	// denial reason to 512 bytes before storing it (and sets the matching
	// *Truncated flag when it had to). This reader re-checks the same bound
	// against untrusted stored bytes rather than trusting it.
	maxAuditDecisionRationaleBytes = 512
	// maxAuditMCPServerTierBytes bounds the stored tier label
	// ("bundled" / "local_container" / "remote_url" —
	// repository.MCPServerTier), far larger than any of those three but
	// still small enough to reject a garbled value outright rather than
	// forwarding it.
	maxAuditMCPServerTierBytes = 128
	// maxAuditMCPServerURLBytes matches mcpregistry's own
	// maxMCPServerURLBytes: that is the writer's own bound on the
	// canonicalized URL it stores, so this reader accepts nothing that
	// writer could not have produced.
	maxAuditMCPServerURLBytes = 2048
	// maxAuditToolPolicyBytes bounds the canonical policy string
	// ("safe" / "approval_required" / "disabled" — mcpregistry's own
	// policyFromProto), the same reasoning as maxAuditMCPServerTierBytes.
	maxAuditToolPolicyBytes = 128
)

// auditTimestampLayout is repository.FormatTimestamp's fixed-width layout. The
// service parses stored created_at values and cursor anchors with it. Because
// every field is pinned to an exact width — a literal 'Z' with no zone offset
// and exactly nine fractional digits — a string parses under this layout only
// if it is already the one canonical form FormatTimestamp emits. Parsing alone
// therefore proves canonical form; no separate re-format round-trip is needed.
const auditTimestampLayout = "2006-01-02T15:04:05.000000000Z"

// errInvalidAuditCursor is the single, value-free error every cursor failure
// maps to. A cursor can fail for many structural reasons; none of them are
// distinguished to the client and none echo the cursor or filter contents.
var errInvalidAuditCursor = status.Error(codes.InvalidArgument, "page.cursor is invalid")

type Server struct {
	turingv1.UnimplementedAuditServiceServer
	repo *repository.Repository
	// cursorKey is the HMAC-SHA256 key that authenticates every emitted cursor.
	// It is never logged or returned in an error. A cursor minted under one key
	// is rejected under any other, which is what makes pagination survive a
	// restart only when the same secret is configured.
	cursorKey [sha256.Size]byte
}

// New builds the audit server. cursorSecret is optional and at most one may be
// passed:
//
//   - one non-empty secret: the cursor MAC key is SHA-256 over a fixed
//     domain prefix plus the secret. The app wires the server-side approval
//     signing secret (TURING_APPROVAL_JWT_SECRET) here, so the same secret
//     yields the same key across restarts and reconstructions and cursors stay
//     valid. The SHA-256 domain prefix keeps this key separate from any other
//     use of that secret in the process (e.g. signing approval tokens); the
//     derivation is one-way, so nothing here reconstructs the approval secret.
//   - omitted (or empty): a fresh 32-byte key is drawn from crypto/rand, so
//     cursors are unforgeable but do not survive a reconstruction. This is for
//     isolated unit tests and Record-only callers that never mint a durable
//     cursor; the production app always supplies the required approval secret.
//
// The constructor takes a bare secret string and never imports the config
// package: choosing which secret is a server-only signing key is the app's
// job, not the service's.
//
// A crypto/rand failure panics rather than proceeding with a predictable key,
// because a forgeable cursor MAC undermines the whole point of the MAC and New
// has no error to return. Passing more than one secret is a programming error
// and also panics.
func New(repo *repository.Repository, cursorSecret ...string) *Server {
	if len(cursorSecret) > 1 {
		panic("audit.New accepts at most one cursor secret")
	}
	var key [sha256.Size]byte
	if len(cursorSecret) == 1 && cursorSecret[0] != "" {
		key = sha256.Sum256([]byte(auditCursorKeyDomain + cursorSecret[0]))
	} else if _, err := rand.Read(key[:]); err != nil {
		panic("audit.New: cursor MAC key generation failed: " + err.Error())
	}
	return &Server{repo: repo, cursorKey: key}
}

func (s *Server) Record(ctx context.Context, correlationID string, actorType string, actorID string, action string, target string, payload map[string]any) error {
	payloadJSON, err := marshalPayload(payload)
	if err != nil {
		return err
	}
	return s.repo.RecordAudit(ctx, correlationID, actorType, actorID, action, target, payloadJSON)
}

func (s *Server) RecordForExistingRun(ctx context.Context, runID string, actorType string, actorID string, action string, target string, payload map[string]any) (bool, error) {
	payloadJSON, err := marshalPayload(payload)
	if err != nil {
		return false, err
	}
	return s.repo.RecordAuditForExistingRun(ctx, runID, actorType, actorID, action, target, payloadJSON)
}

func marshalPayload(payload map[string]any) (string, error) {
	payloadJSON := ""
	if payload != nil {
		data, err := safejson.MarshalCanonical(payload)
		if err != nil {
			return "", err
		}
		payloadJSON = string(data)
	}
	return payloadJSON, nil
}

// ListAuditEntries returns a bounded, redacted page of audit rows. It performs
// exactly one repository query. Every client-controlled value is validated
// before that query, the cursor is bound to the filter set and order, and each
// stored payload is projected through an action-specific allowlist so unknown
// fields and future actions are redacted by default.
func (s *Server) ListAuditEntries(ctx context.Context, req *turingv1.ListAuditEntriesRequest) (*turingv1.ListAuditEntriesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	filter, err := resolveAuditFilter(req)
	if err != nil {
		return nil, err
	}

	limit, err := resolveAuditLimit(req.Page)
	if err != nil {
		return nil, err
	}

	fingerprint := filter.fingerprint()

	var after *repository.AuditCursor
	if req.Page != nil && req.Page.Cursor != "" {
		after, err = s.decodeAuditCursor(req.Page.Cursor, fingerprint)
		if err != nil {
			return nil, err
		}
	}

	records, err := s.repo.ListAuditRecords(ctx, repository.AuditQuery{
		CorrelationID:  filter.correlationID,
		Action:         filter.action,
		CreatedAtStart: filter.start,
		CreatedAtEnd:   filter.end,
		Order:          filter.order,
		After:          after,
		Limit:          limit,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, "list audit entries failed")
	}

	// The repository over-fetches by one row purely to signal another page.
	// The extra row is never returned; it only decides next_cursor.
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}

	entries := make([]*turingv1.AuditEntry, 0, len(records))
	for i := range records {
		entry, err := mapAuditEntry(&records[i])
		if err != nil {
			return nil, status.Error(codes.Internal, "list audit entries failed")
		}
		entries = append(entries, entry)
	}

	page := &turingv1.PageResponse{}
	if hasMore && len(records) > 0 {
		last := records[len(records)-1]
		page.NextCursor = s.encodeAuditCursor(last.CreatedAt, last.RowID, fingerprint)
	}

	return &turingv1.ListAuditEntriesResponse{Entries: entries, Page: page}, nil
}

// auditFilter is the resolved, validated filter set for one read. It is the
// single source of truth both for the query and for the cursor fingerprint, so
// a cursor is always bound to the exact filters and order it was minted under.
type auditFilter struct {
	correlationID sql.NullString
	action        sql.NullString
	start         sql.NullString // inclusive, canonical
	end           sql.NullString // exclusive, canonical
	order         repository.AuditOrder
}

func resolveAuditFilter(req *turingv1.ListAuditEntriesRequest) (auditFilter, error) {
	var filter auditFilter

	if req.CorrelationId != nil {
		if err := validateExactAuditFilter(*req.CorrelationId, "correlation_id", maxAuditCorrelationIDBytes); err != nil {
			return auditFilter{}, err
		}
		filter.correlationID = sql.NullString{String: *req.CorrelationId, Valid: true}
	}
	if req.Action != nil {
		if err := validateExactAuditFilter(*req.Action, "action", maxAuditActionBytes); err != nil {
			return auditFilter{}, err
		}
		filter.action = sql.NullString{String: *req.Action, Valid: true}
	}

	var startTime, endTime time.Time
	hasStart, hasEnd := false, false
	if req.CreatedAtStart != nil {
		if err := req.CreatedAtStart.CheckValid(); err != nil {
			return auditFilter{}, status.Error(codes.InvalidArgument, "created_at_start is invalid")
		}
		startTime = req.CreatedAtStart.AsTime()
		filter.start = sql.NullString{String: repository.FormatTimestamp(startTime), Valid: true}
		hasStart = true
	}
	if req.CreatedAtEnd != nil {
		if err := req.CreatedAtEnd.CheckValid(); err != nil {
			return auditFilter{}, status.Error(codes.InvalidArgument, "created_at_end is invalid")
		}
		endTime = req.CreatedAtEnd.AsTime()
		filter.end = sql.NullString{String: repository.FormatTimestamp(endTime), Valid: true}
		hasEnd = true
	}
	// created_at_start is inclusive and created_at_end is exclusive, so an
	// empty-or-inverted window (start >= end) can never match anything and is
	// a client mistake, not a valid request.
	if hasStart && hasEnd && !startTime.Before(endTime) {
		return auditFilter{}, status.Error(codes.InvalidArgument, "created_at_start must be before created_at_end")
	}

	switch req.Order {
	case turingv1.AuditOrder_AUDIT_ORDER_UNSPECIFIED, turingv1.AuditOrder_AUDIT_ORDER_DESCENDING:
		filter.order = repository.AuditOrderDescending
	case turingv1.AuditOrder_AUDIT_ORDER_ASCENDING:
		filter.order = repository.AuditOrderAscending
	default:
		return auditFilter{}, status.Error(codes.InvalidArgument, "order is invalid")
	}

	return filter, nil
}

// validateExactAuditFilter rejects an exact-match filter value that could not
// be a legitimate stored identifier: empty, all-whitespace, over-long, invalid
// UTF-8, or carrying control characters / NUL. Accepted values are never
// trimmed or otherwise mutated — the match is verbatim. Error messages name
// the field but never echo the value.
func validateExactAuditFilter(value, field string, maxBytes int) error {
	if value == "" {
		return status.Error(codes.InvalidArgument, field+" must not be empty when set")
	}
	if strings.TrimSpace(value) == "" {
		return status.Error(codes.InvalidArgument, field+" must not be blank")
	}
	if len(value) > maxBytes {
		return status.Error(codes.InvalidArgument, field+" is too long")
	}
	if !utf8.ValidString(value) {
		return status.Error(codes.InvalidArgument, field+" must be valid UTF-8")
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return status.Error(codes.InvalidArgument, field+" must not contain control characters")
		}
	}
	return nil
}

func resolveAuditLimit(page *turingv1.PageRequest) (int, error) {
	if page == nil {
		return defaultAuditPageLimit, nil
	}
	if page.Limit < 0 || page.Limit > maxAuditPageLimit {
		return 0, status.Error(codes.InvalidArgument, "page.limit is out of range")
	}
	if page.Limit == 0 {
		return defaultAuditPageLimit, nil
	}
	return int(page.Limit), nil
}

// auditCursorPayload is the exact JSON shape of an opaque cursor. base64 is
// only transport here; the anchor contents are not secret, which is why every
// field is re-validated on decode rather than trusted. The MAC is what makes
// the anchor unforgeable: the filter fingerprint binds a cursor to its filters
// and order, but only the HMAC over the whole body authenticates the anchor
// (createdAt, rowID) itself.
type auditCursorPayload struct {
	Version     int    `json:"v"`
	CreatedAt   string `json:"createdAt"`
	RowID       int64  `json:"rowID"`
	Fingerprint string `json:"fingerprint"`
	MAC         string `json:"mac"`
}

// auditCursorBody is the authenticated portion of a cursor. The MAC is computed
// over its deterministic JSON, so any change to the version, the anchor, or the
// filter fingerprint invalidates the MAC. It is a separate type from the
// transport payload precisely so the MAC never covers itself.
type auditCursorBody struct {
	Version     int    `json:"v"`
	CreatedAt   string `json:"createdAt"`
	RowID       int64  `json:"rowID"`
	Fingerprint string `json:"fingerprint"`
}

// cursorMAC returns the lowercase-hex HMAC-SHA256 of the cursor body under the
// server's key. json.Marshal cannot fail on auditCursorBody's fixed primitive
// members, so the error is discarded for the same reason the fingerprint and
// cursor encoders discard theirs. The key is never exposed.
func (s *Server) cursorMAC(version int, createdAt string, rowID int64, fingerprintHex string) string {
	body, _ := json.Marshal(auditCursorBody{
		Version:     version,
		CreatedAt:   createdAt,
		RowID:       rowID,
		Fingerprint: fingerprintHex,
	})
	mac := hmac.New(sha256.New, s.cursorKey[:])
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// encodeAuditCursor renders the opaque next-page cursor. It is infallible:
// auditCursorPayload has only fixed primitive members (an int version, the
// canonical timestamp string, an int64 row id, and two fixed 64-char hex
// digests), so json.Marshal cannot fail on it — the same reason
// auditFilter.fingerprint marshals with a discarded error. The encoded output
// is structurally bounded too (~310 base64 chars, including the MAC), always
// well under maxAuditCursorBytes, so no size guard is needed on this emit path;
// the decode path still enforces maxAuditCursorBytes on untrusted client input.
func (s *Server) encodeAuditCursor(createdAt string, rowID int64, fingerprint [sha256.Size]byte) string {
	fingerprintHex := hex.EncodeToString(fingerprint[:])
	data, _ := json.Marshal(auditCursorPayload{
		Version:     auditCursorVersion,
		CreatedAt:   createdAt,
		RowID:       rowID,
		Fingerprint: fingerprintHex,
		MAC:         s.cursorMAC(auditCursorVersion, createdAt, rowID, fingerprintHex),
	})
	return base64.RawURLEncoding.EncodeToString(data)
}

// decodeAuditCursor validates a client-supplied cursor and returns its anchor.
// Every structural check that can fail maps to errInvalidAuditCursor without
// echoing any value. The MAC over the complete body (version, anchor,
// fingerprint) is verified with the server's key before the anchor is used, so
// a client cannot swap in a different — but individually valid — rowID or
// canonical createdAt. The fingerprint must additionally be constant-time equal
// to the current request's filter fingerprint, which is what binds a cursor to
// the filters and order it was issued under.
//
// The caller only invokes this for a non-empty cursor, so an empty string is
// not a case handled here; the single length guard rejects anything longer
// than maxAuditCursorBytes before any decoding work.
func (s *Server) decodeAuditCursor(encoded string, fingerprint [sha256.Size]byte) (*repository.AuditCursor, error) {
	if len(encoded) > maxAuditCursorBytes {
		return nil, errInvalidAuditCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errInvalidAuditCursor
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var payload auditCursorPayload
	if err := dec.Decode(&payload); err != nil {
		return nil, errInvalidAuditCursor
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errInvalidAuditCursor
	}

	if payload.Version != auditCursorVersion {
		return nil, errInvalidAuditCursor
	}
	if payload.RowID <= 0 {
		return nil, errInvalidAuditCursor
	}
	// The anchor must parse under the fixed-width canonical layout. That layout
	// pins every field to an exact width — a literal 'Z' with no zone offset and
	// exactly nine fractional digits — so a string parses only if it is already
	// the one canonical form repository.FormatTimestamp emits. Parsing is thus
	// itself the canonical-form check, and the raw payload.CreatedAt handed to
	// the repository below is guaranteed to compare identically to stored
	// created_at. (A re-format round-trip check here would be unreachable: no
	// string parses under this layout without round-tripping to itself.)
	if _, err := time.Parse(auditTimestampLayout, payload.CreatedAt); err != nil {
		return nil, errInvalidAuditCursor
	}
	if !validAuditDigestHex(payload.Fingerprint) {
		return nil, errInvalidAuditCursor
	}
	if !validAuditDigestHex(payload.MAC) {
		return nil, errInvalidAuditCursor
	}
	// Authenticate the whole body before trusting the anchor. Both the MAC and
	// the fingerprint-binding comparison below are constant time.
	wantMAC := s.cursorMAC(payload.Version, payload.CreatedAt, payload.RowID, payload.Fingerprint)
	if subtle.ConstantTimeCompare([]byte(payload.MAC), []byte(wantMAC)) != 1 {
		return nil, errInvalidAuditCursor
	}
	want := hex.EncodeToString(fingerprint[:])
	if subtle.ConstantTimeCompare([]byte(payload.Fingerprint), []byte(want)) != 1 {
		return nil, errInvalidAuditCursor
	}

	return &repository.AuditCursor{CreatedAt: payload.CreatedAt, RowID: payload.RowID}, nil
}

// validAuditDigestHex reports whether value is exactly 64 lowercase hex
// characters — the shape of both a SHA-256 filter fingerprint and an
// HMAC-SHA256 cursor MAC (each a 32-byte digest).
func validAuditDigestHex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		isDigit := c >= '0' && c <= '9'
		isHexLower := c >= 'a' && c <= 'f'
		if !isDigit && !isHexLower {
			return false
		}
	}
	return true
}

// fingerprint hashes a canonical, typed view of the filters and resolved
// order. Resolving order first means AUDIT_ORDER_UNSPECIFIED and
// AUDIT_ORDER_DESCENDING produce the same fingerprint, matching the fact that
// they produce the same query. json.Marshal of a struct is deterministic
// (fixed field order), so the hash input is stable.
func (f auditFilter) fingerprint() [sha256.Size]byte {
	type canonical struct {
		CorrelationIDPresent bool   `json:"correlationIdPresent"`
		CorrelationID        string `json:"correlationId"`
		ActionPresent        bool   `json:"actionPresent"`
		Action               string `json:"action"`
		CreatedAtStart       string `json:"createdAtStart"`
		CreatedAtEnd         string `json:"createdAtEnd"`
		Order                int    `json:"order"`
	}
	data, _ := json.Marshal(canonical{
		CorrelationIDPresent: f.correlationID.Valid,
		CorrelationID:        f.correlationID.String,
		ActionPresent:        f.action.Valid,
		Action:               f.action.String,
		CreatedAtStart:       f.start.String,
		CreatedAtEnd:         f.end.String,
		Order:                int(f.order),
	})
	return sha256.Sum256(data)
}

func mapAuditEntry(record *repository.AuditRecord) (*turingv1.AuditEntry, error) {
	createdAt, err := time.Parse(auditTimestampLayout, record.CreatedAt)
	if err != nil {
		return nil, err
	}
	entry := &turingv1.AuditEntry{
		AuditId:   requiredAuditMetadata(record.AuditID, maxAuditIDMetadataBytes),
		ActorType: requiredAuditMetadata(record.ActorType, maxAuditActorTypeMetadataBytes),
		Action:    requiredAuditMetadata(record.Action, maxAuditActionMetadataBytes),
		Payload:   projectAuditPayload(record),
		CreatedAt: timestamppb.New(createdAt.UTC()),
	}
	if record.CorrelationID.Valid {
		if value, ok := optionalAuditMetadata(record.CorrelationID.String, maxAuditCorrelationMetadataBytes); ok {
			entry.CorrelationId = &value
		}
	}
	// Structural safety is necessary but not sufficient: some actions store a
	// value in these columns that is safe to print and still must not be
	// disclosed. auditDisclosureFor is that second gate. It takes the whole
	// record so the approval.* target omission can consult the repository's
	// bounded ActionHasApprovalPrefix bit, which stays true even when an
	// oversized action collapsed the Action column to "".
	disclosure := auditDisclosureFor(record)
	if record.ActorID.Valid && !disclosure.omitActorID {
		if value, ok := optionalAuditMetadata(record.ActorID.String, maxAuditActorIDMetadataBytes); ok {
			entry.ActorId = &value
		}
	}
	if record.Target.Valid && !disclosure.omitTarget {
		if value, ok := optionalAuditMetadata(record.Target.String, maxAuditTargetMetadataBytes); ok {
			entry.Target = &value
		}
	}
	return entry, nil
}

// structurallySafeAuditMetadata reports whether a stored metadata value is safe
// to place on the wire verbatim: within its byte bound, valid UTF-8, and free
// of NUL and Unicode control characters. The empty string is safe (it has no
// bytes to violate any of these), which is what lets a non-NULL empty optional
// value stay present as an empty string.
func structurallySafeAuditMetadata(value string, maxBytes int) bool {
	if len(value) > maxBytes {
		return false
	}
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// requiredAuditMetadata projects a required structural field: the stored value
// when it is structurally safe and non-empty, otherwise the fixed redaction
// literal. Empty is treated as invalid here (unlike optional fields) because a
// required semantic label — audit_id, actor_type, action — is never written
// empty on purpose, and the repository collapses an over-read-bound required
// column to ” before this sees it; redacting empty turns that overflow into a
// safe marker rather than a bare empty string on the wire. It never returns the
// unsafe stored value and never fails the whole page.
func requiredAuditMetadata(value string, maxBytes int) string {
	if value != "" && structurallySafeAuditMetadata(value, maxBytes) {
		return value
	}
	return redactedAuditMetadata
}

// optionalAuditMetadata projects an optional structural field: the stored value
// and true when it is structurally safe, otherwise ("", false) so the caller
// omits the field entirely rather than emitting an unsafe or placeholder value.
func optionalAuditMetadata(value string, maxBytes int) (string, bool) {
	if structurallySafeAuditMetadata(value, maxBytes) {
		return value, true
	}
	return "", false
}

// projectAuditPayload turns one stored row into a redacted AuditPayload. The
// three states are strictly separated: ABSENT for SQL NULL, SCRUBBED for the
// exact deletion tombstone, PRESENT for everything else — including malformed,
// non-object, over-sized, and unknown-action payloads, which yield PRESENT
// with no fields rather than an RPC failure or a raw fallback.
func projectAuditPayload(record *repository.AuditRecord) *turingv1.AuditPayload {
	switch {
	case !record.PayloadPresent:
		return &turingv1.AuditPayload{State: turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_ABSENT}
	case record.PayloadScrubbed:
		return &turingv1.AuditPayload{State: turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_SCRUBBED}
	}

	payload := &turingv1.AuditPayload{State: turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_PRESENT}
	if !record.PayloadJSON.Valid {
		// Present but over the repository's read bound: metadata only.
		return payload
	}
	object := parseAuditObject(record.PayloadJSON.String)
	if object == nil {
		// Malformed or non-object body: metadata only, never a raw fallback.
		return payload
	}
	applyAuditActionPolicy(payload, record.Action, object)
	return payload
}

// parseAuditObject decodes exactly one JSON object from a bounded payload. It
// uses UseNumber so integers are read exactly (no float coercion) and requires
// EOF after the object so trailing tokens are treated as malformed. Anything
// that is not a single JSON object returns nil.
func parseAuditObject(payloadJSON string) map[string]any {
	dec := json.NewDecoder(strings.NewReader(payloadJSON))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return object
}

// applyAuditActionPolicy copies only the exact, typed fields the action's
// reviewed rule allows. Every other action — including future ones — is
// default-deny: it keeps PRESENT state and carries no payload fields.
func applyAuditActionPolicy(payload *turingv1.AuditPayload, action string, object map[string]any) {
	switch action {
	case "tool.call.before", "tool.call.after":
		payload.ToolName = auditString(object, "toolName", maxAuditToolNameBytes)
		payload.ServerName = auditString(object, "serverName", maxAuditServerNameBytes)
		payload.Phase = auditString(object, "phase", maxAuditPhaseBytes)
		payload.Status = auditString(object, "status", maxAuditStatusBytes)
		payload.Reason = auditString(object, "reason", maxAuditReasonBytes)
		payload.DurationMs = auditInt64(object, "durationMs")
		payload.ErrorCode = auditNestedErrorCode(object)
	case "approval.requested", "approval.expired", "approval.consumed":
		applyApprovalCommonPayload(payload, object)
	case "approval.approved":
		// Only the human approve path records `comment` (TUR-002); an
		// unattended grant records none, and its absence is the answer.
		applyApprovalCommonPayload(payload, object)
		payload.DecisionComment = auditHumanRationale(object, "comment")
		payload.DecisionCommentTruncated = auditBool(object, "commentTruncated")
	case "approval.denied":
		// The stored key is `reason`, but it is a person's sentence, not the
		// tool-policy `reason` the tool.call.* rule projects — it is disclosed
		// under its own field so the two can never be conflated.
		applyApprovalCommonPayload(payload, object)
		payload.DenialReason = auditHumanRationale(object, "reason")
		payload.DenialReasonTruncated = auditBool(object, "reasonTruncated")
	case "automation.tool.blocked":
		payload.ToolName = auditString(object, "toolName", maxAuditToolNameBytes)
		payload.ServerName = auditString(object, "serverName", maxAuditServerNameBytes)
		payload.AutomationId = auditString(object, "automationId", maxAuditAutomationIDBytes)
		payload.AutomationName = auditString(object, "automationName", maxAuditAutomationNameBytes)
	case "integration.connected", "integration.revoked", "integration.deleted":
		payload.Provider = auditString(object, "provider", maxAuditProviderBytes)
		payload.DisplayName = auditString(object, "displayName", maxAuditDisplayNameBytes)
	case "auth.failed":
		payload.Method = auditString(object, "method", maxAuditMethodBytes)
		payload.RequestId = auditString(object, "requestId", maxAuditRequestIDBytes)
	case "session.deleted":
		payload.DeletedRuns = auditInt64(object, "runs")
		payload.DeletedMessages = auditInt64(object, "messages")
	case "egress.consent.recorded":
		payload.Provider = auditString(object, "provider", maxAuditProviderBytes)
		payload.EndpointHost = auditString(object, "endpointHost", maxAuditTargetMetadataBytes)
		payload.EgressDataCategories = auditEgressCategories(object)
		if version := auditInt64(object, "decisionVersion"); version != nil &&
			*version > 0 && *version <= math.MaxInt32 {
			converted := int32(*version)
			payload.EgressDecisionVersion = &converted
		}
		if raw := auditString(object, "consentGrantedAt", maxAuditTargetMetadataBytes); raw != nil {
			if parsed, err := time.Parse(time.RFC3339Nano, *raw); err == nil {
				payload.EgressConsentGrantedAt = timestamppb.New(parsed.UTC())
			}
		}
	case "automation.remote_egress_blocked":
		payload.ErrorCode = auditString(object, "code", maxAuditErrorCodeBytes)
		payload.Provider = auditString(object, "provider", maxAuditProviderBytes)
	case "session.artifact.cleanup.failed":
		payload.Status = auditString(object, "state", maxAuditStatusBytes)
		payload.Reason = auditString(object, "policy", maxAuditReasonBytes)
		payload.ErrorCode = auditString(object, "errorCode", maxAuditErrorCodeBytes)
	case "mcp.server.registered":
		payload.ServerName = auditString(object, "name", maxAuditServerNameBytes)
		payload.McpServerTier = auditString(object, "tier", maxAuditMCPServerTierBytes)
		payload.McpServerUrl = auditString(object, "url", maxAuditMCPServerURLBytes)
		payload.Adopted = auditBool(object, "adopted")
	case "mcp.server.enabled", "mcp.server.disabled":
		payload.ServerName = auditString(object, "name", maxAuditServerNameBytes)
		payload.McpServerTier = auditString(object, "tier", maxAuditMCPServerTierBytes)
		payload.RemoteDiscoveryAttempted = auditBool(object, "remoteDiscoveryAttempted")
		payload.DiscoverySucceeded = auditBool(object, "discoverySucceeded")
	case "mcp.server.token_rotated", "mcp.server.token_cleared":
		payload.ServerName = auditString(object, "name", maxAuditServerNameBytes)
		payload.TokenConfigured = auditBool(object, "tokenConfigured")
	case "mcp.server.reimported":
		payload.ImportedServers = auditInt64(object, "imported")
		payload.SkippedServers = auditInt64(object, "skipped")
		payload.RefusedServers = auditInt64(object, "refused")
	case "mcp.server.deleted":
		payload.ServerName = auditString(object, "name", maxAuditServerNameBytes)
		payload.McpServerTier = auditString(object, "tier", maxAuditMCPServerTierBytes)
	case "mcp.server.tool_policy_changed":
		payload.ServerName = auditString(object, "name", maxAuditServerNameBytes)
		payload.ToolName = auditString(object, "toolName", maxAuditToolNameBytes)
		payload.ToolPolicy = auditString(object, "toolPolicy", maxAuditToolPolicyBytes)
	default:
		// Unknown / future action: metadata only, no payload fields.
	}

}

func auditEgressCategories(object map[string]any) []turingv1.EgressDataCategory {
	raw, ok := object["dataCategories"].([]any)
	if !ok || len(raw) > 16 {
		return nil
	}
	categories := make([]turingv1.EgressDataCategory, 0, len(raw))
	seen := make(map[turingv1.EgressDataCategory]struct{}, len(raw))
	for _, item := range raw {
		name, ok := item.(string)
		if !ok {
			return nil
		}
		value, ok := turingv1.EgressDataCategory_value[name]
		if !ok {
			return nil
		}
		category := turingv1.EgressDataCategory(value)
		if category == turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_UNSPECIFIED {
			return nil
		}
		if _, duplicate := seen[category]; duplicate {
			return nil
		}
		seen[category] = struct{}{}
		categories = append(categories, category)
	}
	return categories
}

// applyApprovalCommonPayload copies the fields every approval.* action may
// disclose. The two human decision actions add their own rationale field on
// top; nothing here is action-specific, so a new approval action that reaches
// this switch inherits exactly this much and no rationale.
func applyApprovalCommonPayload(payload *turingv1.AuditPayload, object map[string]any) {
	payload.ToolName = auditString(object, "toolName", maxAuditToolNameBytes)
	payload.Unattended = auditBool(object, "unattended")
	payload.AutomationId = auditString(object, "automationId", maxAuditAutomationIDBytes)
	payload.AutomationName = auditString(object, "automationName", maxAuditAutomationNameBytes)
}

// auditString returns a pointer to the value at key only if it is a JSON
// string, non-empty, within maxBytes, and structurally safe for display —
// meaning it carries no NUL or Unicode control characters. A normal display
// value may contain Unicode and spaces, but a control character (newline, tab,
// and the like) makes the whole value unsafe, so it is omitted (nil) rather
// than truncated or coerced. UTF-8 validity is guaranteed upstream because
// object always originates from encoding/json, which replaces any invalid input
// bytes with U+FFFD; structurallySafeAuditMetadata re-checks it anyway so the
// reader is safe against any future non-json caller.
func auditString(object map[string]any, key string, maxBytes int) *string {
	raw, ok := object[key]
	if !ok {
		return nil
	}
	value, ok := raw.(string)
	if !ok {
		return nil
	}
	if value == "" {
		return nil
	}
	if !structurallySafeAuditMetadata(value, maxBytes) {
		return nil
	}
	return &value
}

// auditHumanRationale reads an approval decision's human-authored rationale —
// the comment a person typed when approving, or the reason they gave when
// denying (TUR-002). It is deliberately NOT auditString:
//
//   - auditString omits an empty value, because for a machine-written label
//     (toolName, status, phase) empty carries no information. Here empty is
//     information: it is the difference between "the person decided and typed
//     nothing" and "no human field was ever recorded for this row". So an
//     empty JSON string maps to a present, empty field.
//   - auditString rejects every control character, because a machine label has
//     no business containing one. This is free text a person wrote, so a
//     newline (U+000A), carriage return (U+000D), and tab (U+0009) are
//     preserved verbatim — they are how a typed sentence is formatted, not an
//     anomaly. Every other Unicode control character (NUL, BEL, ESC, DEL, and
//     the C1 range including U+0085 NEL) still makes the whole value unsafe:
//     they carry no authored meaning and are the ones that can rewrite a
//     terminal or a log line.
//
// Everything else stays as strict as the rest of the projection: it must be an
// actual JSON string, valid UTF-8, and within maxAuditDecisionRationaleBytes.
// A violation omits the field entirely — never a truncated, escaped, or
// repaired value. The writer already bounded and flagged what it stored, so a
// stored value that is over the bound or carries a disallowed control did not
// come from that writer, and this read path has no business guessing what it
// meant. The row itself still projects as PRESENT with its other fields.
//
// UTF-8 validity is guaranteed on the current path because object always
// originates from encoding/json, which replaces invalid input bytes with
// U+FFFD. The check is kept anyway so this reader stays safe if a non-json
// caller ever hands it a map, and it is covered by a direct unit test rather
// than assumed.
func auditHumanRationale(object map[string]any, key string) *string {
	raw, ok := object[key]
	if !ok {
		return nil
	}
	value, ok := raw.(string)
	if !ok {
		return nil
	}
	if len(value) > maxAuditDecisionRationaleBytes {
		return nil
	}
	if !utf8.ValidString(value) {
		return nil
	}
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r == 0 || unicode.IsControl(r) {
			return nil
		}
	}
	return &value
}

// auditBool returns a pointer only when the value is an exact JSON bool.
func auditBool(object map[string]any, key string) *bool {
	raw, ok := object[key]
	if !ok {
		return nil
	}
	value, ok := raw.(bool)
	if !ok {
		return nil
	}
	return &value
}

// auditInt64 returns a pointer only when the value is an exact, non-negative
// integer within int64 range. json.Number parsed via ParseInt rejects floats
// and exponents, so "1.5", "1e3", and " 1" are all omitted rather than coerced.
func auditInt64(object map[string]any, key string) *int64 {
	raw, ok := object[key]
	if !ok {
		return nil
	}
	number, ok := raw.(json.Number)
	if !ok {
		return nil
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil {
		return nil
	}
	if value < 0 {
		return nil
	}
	return &value
}

// auditNestedErrorCode reads only error.code, and only when error is itself a
// JSON object. error.message is never read. A malformed nested error omits the
// code entirely.
func auditNestedErrorCode(object map[string]any) *string {
	raw, ok := object["error"]
	if !ok {
		return nil
	}
	nested, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return auditString(nested, "code", maxAuditErrorCodeBytes)
}
