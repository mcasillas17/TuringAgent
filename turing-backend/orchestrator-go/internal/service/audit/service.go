package audit

import (
	"context"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/safejson"
)

type Server struct {
	repo *repository.Repository
}

func New(repo *repository.Repository) *Server {
	return &Server{repo: repo}
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
