// Package attention serves the owner's Needs-you queue: one cross-session
// list of what waits on them, classified by the closed stage table, paged in
// one total order, and an occurrence-bound dismissal of one automation
// failure. It returns ids, titles and closed codes, never tool arguments,
// message content or transcripts.
package attention

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	projection "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/attention"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// pageTokenVersion is the only token shape this build emits or accepts.
	pageTokenVersion = 1
	// maxPageTokenBytes bounds the encoded token before it is decoded.
	maxPageTokenBytes = 512
	// maxIDBytes bounds every id a request or a token names.
	maxIDBytes = 256
	// maxCauseBytes bounds a closed code; a longer stored value is left out.
	maxCauseBytes = 64
)

var errInvalidPageToken = status.Error(codes.InvalidArgument, "page.cursor is invalid")

// HintPublisher tells subscribers the queue may have changed.
type HintPublisher interface {
	PublishAttentionChanged()
}

type Server struct {
	turingv1.UnimplementedAttentionServiceServer
	repo  *repository.Repository
	hints HintPublisher
}

func New(repo *repository.Repository, hints HintPublisher) *Server {
	return &Server{repo: repo, hints: hints}
}

// pageToken is the opaque, base64url-encoded cursor: the first page's
// watermark and the last row's sort key. It carries no authority: every page
// re-reads every row's current state.
type pageToken struct {
	Version   int    `json:"v"`
	Watermark int64  `json:"w"`
	Rank      int    `json:"r"`
	Since     int64  `json:"s"`
	Kind      int    `json:"k"`
	ID        string `json:"i"`
}

func (s *Server) ListAttention(ctx context.Context, req *turingv1.ListAttentionRequest) (*turingv1.ListAttentionResponse, error) {
	limit := int(req.GetPage().GetLimit())
	if limit < 0 || limit > repository.MaxAttentionPageLimit {
		return nil, status.Error(codes.InvalidArgument, "page.limit must be between 1 and 100")
	}
	if limit == 0 {
		limit = repository.MaxAttentionPageLimit
	}
	query := repository.AttentionQuery{Limit: limit}
	if cursor := req.GetPage().GetCursor(); cursor != "" {
		token, err := decodePageToken(cursor)
		if err != nil {
			return nil, errInvalidPageToken
		}
		query.Watermark = token.Watermark
		query.After = &repository.AttentionCursor{
			Rank: token.Rank, SinceNanos: token.Since, Kind: projection.Kind(token.Kind), ID: token.ID,
		}
	}
	page, err := s.repo.ListAttention(ctx, query)
	if err != nil {
		return nil, status.Error(codes.Internal, "list attention failed")
	}
	response := &turingv1.ListAttentionResponse{
		Rows: make([]*turingv1.AttentionRow, 0, len(page.Rows)),
		Page: &turingv1.PageResponse{},
	}
	for _, row := range page.Rows {
		response.Rows = append(response.Rows, mapRow(row))
	}
	if page.More {
		last := page.Rows[len(page.Rows)-1].Cursor()
		response.Page.NextCursor, err = encodePageToken(pageToken{
			Version: pageTokenVersion, Watermark: page.Watermark,
			Rank: last.Rank, Since: last.SinceNanos, Kind: int(last.Kind), ID: last.ID,
		})
		if err != nil {
			return nil, status.Error(codes.Internal, "list attention failed")
		}
	}
	return response, nil
}

// DismissAttention hides the one automation failure the owner saw, named by
// the occurrence_key its row carried, until a newer occurrence.
func (s *Server) DismissAttention(ctx context.Context, req *turingv1.DismissAttentionRequest) (*turingv1.DismissAttentionResponse, error) {
	automationID, key := req.GetAutomationId(), req.GetOccurrenceKey()
	if !validID(automationID) || !validID(key) {
		return nil, status.Error(codes.InvalidArgument, "automation_id and occurrence_key are required")
	}
	switch err := s.repo.DismissAttention(ctx, automationID, key); {
	case errors.Is(err, repository.ErrAutomationNotFound):
		return nil, status.Error(codes.NotFound, "automation not found")
	case errors.Is(err, repository.ErrAttentionOccurrenceStale):
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	case err != nil:
		return nil, status.Error(codes.Internal, "dismiss attention failed")
	}
	if s.hints != nil {
		s.hints.PublishAttentionChanged()
	}
	return &turingv1.DismissAttentionResponse{}, nil
}

// mapRow classifies a row by the pure table. A row its source's stage does
// not describe is listed as unavailable, in its source's place.
func mapRow(row repository.AttentionRow) *turingv1.AttentionRow {
	stage := projection.Stage(row.Conditions)
	if stage != projection.SourceStage(row.Kind) {
		stage = projection.Unavailable
	}
	return &turingv1.AttentionRow{
		Stage:          wireStage(stage),
		Kind:           turingv1.AttentionSourceKind(row.Kind),
		Since:          timestamppb.New(time.Unix(0, row.SinceNanos).UTC()),
		SessionId:      row.SessionID,
		SessionTitle:   row.SessionTitle,
		ApprovalId:     row.ApprovalID,
		RunId:          row.RunID,
		DelegationId:   row.DelegationID,
		AutomationId:   row.AutomationID,
		AutomationName: row.AutomationName,
		OccurrenceKey:  row.OccurrenceKey,
		Cause:          closedCode(row.Cause),
	}
}

func wireStage(stage projection.StageID) turingv1.AttentionStage {
	switch stage {
	case projection.PersonApproval:
		return turingv1.AttentionStage_ATTENTION_STAGE_PERSON_APPROVAL
	case projection.PersonConsent:
		return turingv1.AttentionStage_ATTENTION_STAGE_PERSON_CONSENT
	case projection.PersonBlocked:
		return turingv1.AttentionStage_ATTENTION_STAGE_PERSON_BLOCKED
	case projection.PersonDeletion:
		return turingv1.AttentionStage_ATTENTION_STAGE_PERSON_DELETION
	case projection.PersonProposal:
		return turingv1.AttentionStage_ATTENTION_STAGE_PERSON_PROPOSAL
	default:
		return turingv1.AttentionStage_ATTENTION_STAGE_UNAVAILABLE
	}
}

// closedCode passes on a stored code only in the shape every closed code
// this product writes has: lower-case letters, digits and underscores.
func closedCode(value string) string {
	if value == "" || len(value) > maxCauseBytes {
		return ""
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return ""
		}
	}
	return value
}

func validID(value string) bool {
	if value == "" || len(value) > maxIDBytes {
		return false
	}
	for _, r := range value {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	return true
}

func encodePageToken(token pageToken) (string, error) {
	encoded, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

// decodePageToken refuses anything this build did not emit: a token that does
// not decode, names an unknown stage or kind, or pairs a kind with another
// stage than its own.
func decodePageToken(cursor string) (pageToken, error) {
	if len(cursor) > maxPageTokenBytes {
		return pageToken{}, errInvalidPageToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return pageToken{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var token pageToken
	if err := decoder.Decode(&token); err != nil {
		return pageToken{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return pageToken{}, errInvalidPageToken
	}
	kind := projection.Kind(token.Kind)
	stage := projection.SourceStage(kind)
	if token.Version != pageTokenVersion || token.Watermark <= 0 || stage == projection.Unavailable ||
		token.Rank != projection.Rank(stage) || !validID(token.ID) {
		return pageToken{}, errInvalidPageToken
	}
	return token, nil
}
