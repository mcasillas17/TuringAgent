package testkit

import (
	"context"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/sessions"
	"google.golang.org/grpc"
)

// RecallStore exposes the real migrated store and SessionService to
// cross-internal-package tests. It starts no app workers or external services.
type RecallStore struct {
	database *db.DB
	repo     *repository.Repository
	service  *sessions.Server
}

type RecallSession struct {
	ID string
	At time.Time
}

type RecallMessage struct {
	ID, SessionID, Role, Content string
	At                           time.Time
}

func OpenRecallStore(ctx context.Context, path string) (*RecallStore, error) {
	database, err := db.Open(path)
	if err != nil {
		return nil, err
	}
	if err := db.ApplyMigrations(ctx, database); err != nil {
		_ = database.Close()
		return nil, err
	}
	repo := repository.New(database)
	return &RecallStore{database: database, repo: repo, service: sessions.New(repo, config.Config{}, nil)}, nil
}

func (s *RecallStore) Close() error { return s.database.Close() }

func (s *RecallStore) Server() *grpc.Server {
	server := grpc.NewServer()
	turingv1.RegisterSessionServiceServer(server, s.service)
	return server
}

// Seed is deliberately only corpus insertion, not retrieval. Binding explicit
// IDs/times here preserves production migrations, constraints and FTS triggers.
// Sequence follows fixture order within each session, just as persisted history.
func (s *RecallStore) Seed(ctx context.Context, sessions []RecallSession, messages []RecallMessage) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, session := range sessions {
		at := repository.FormatTimestamp(session.At)
		if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(id, title, created_at, updated_at) VALUES (?, '', ?, ?)`, session.ID, at, at); err != nil {
			return err
		}
	}
	sequences := map[string]int{}
	for _, message := range messages {
		sequences[message.SessionID]++
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages(id, session_id, role, content, sequence, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			message.ID, message.SessionID, message.Role, message.Content, sequences[message.SessionID], repository.FormatTimestamp(message.At)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *RecallStore) Archive(ctx context.Context, id string) error {
	_, err := s.service.ArchiveSession(ctx, &turingv1.ArchiveSessionRequest{SessionId: id})
	return err
}

// BeginWithdrawal parks the real lifecycle at its committed visibility boundary;
// it does not fake deletion_state or delete rows with test-only SQL.
func (s *RecallStore) BeginWithdrawal(ctx context.Context, id string) error {
	_, err := s.repo.BeginSessionDeletion(ctx, id)
	return err
}

func (s *RecallStore) Delete(ctx context.Context, id string) (string, error) {
	result, err := s.service.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: id})
	if err != nil {
		return "", err
	}
	return result.GetDeletion().GetState().String(), nil
}

func (s *RecallStore) DeletionState(ctx context.Context, id string) (string, error) {
	receipt, err := s.repo.SessionDeletionReceipt(ctx, id)
	return receipt.State, err
}

// SearchIDs never exports scores: only order is a valid cross-query metric.
func (s *RecallStore) SearchIDs(ctx context.Context, query, sessionID, excludedSessionID string, hits bool) ([]string, error) {
	ids := []string{}
	if hits {
		rows, err := s.repo.SearchMessageHits(ctx, sessionID, excludedSessionID, query, 5)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			ids = append(ids, row.Message.MessageID)
		}
		return ids, nil
	}
	rows, err := s.repo.SearchMessages(ctx, sessionID, excludedSessionID, query, 5)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		ids = append(ids, row.MessageID)
	}
	return ids, nil
}
