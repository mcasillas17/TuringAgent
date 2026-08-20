package events

import "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"

func FromRepositoryEvent(event repository.Event) Event {
	runID := ""
	if event.RunID.Valid {
		runID = event.RunID.String
	}
	return Event{
		EventID:     event.EventID,
		SessionID:   event.SessionID,
		RunID:       runID,
		TraceID:     event.TraceID,
		Sequence:    event.Sequence,
		Type:        event.Type,
		CreatedAt:   event.CreatedAt,
		PayloadJSON: event.PayloadJSON,
	}
}
