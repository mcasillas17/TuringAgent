package runcorrelation

import (
	"errors"
	"strings"
	"testing"
)

// A valid link is the shape atomic enqueue creates: the run points at the
// assistant message, the assistant message points back at the run, and both
// agree on the session.
func validLink() Link {
	return Link{
		RunID:                 "run-1",
		RunSessionID:          "session-1",
		RunAssistantMessageID: "message-assistant-1",
		MessageID:             "message-assistant-1",
		MessageSessionID:      "session-1",
		MessageRunID:          "run-1",
		MessageRole:           "assistant",
	}
}

func TestValidateAcceptsOneBidirectionalAssistantLink(t *testing.T) {
	if err := Validate(validLink()); err != nil {
		t.Fatalf("Validate(valid link) = %v, want nil", err)
	}
}

func TestValidateRejectsMismatchedRunMessageSessionRoleOrIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Link)
	}{
		{name: "message_points_at_another_run", mutate: func(link *Link) { link.MessageRunID = "run-2" }},
		{name: "run_points_at_another_message", mutate: func(link *Link) { link.RunAssistantMessageID = "message-assistant-2" }},
		{name: "message_belongs_to_another_session", mutate: func(link *Link) { link.MessageSessionID = "session-2" }},
		{name: "user_role", mutate: func(link *Link) { link.MessageRole = "user" }},
		{name: "system_role", mutate: func(link *Link) { link.MessageRole = "system" }},
		{name: "empty_role", mutate: func(link *Link) { link.MessageRole = "" }},
		{name: "uppercase_role", mutate: func(link *Link) { link.MessageRole = "Assistant" }},
		{name: "missing_run_id", mutate: func(link *Link) { link.RunID = "" }},
		{name: "missing_message_run_id", mutate: func(link *Link) { link.MessageRunID = "" }},
		{name: "missing_run_assistant_message_id", mutate: func(link *Link) { link.RunAssistantMessageID = "" }},
		{name: "missing_message_id", mutate: func(link *Link) { link.MessageID = "" }},
		{name: "missing_run_session_id", mutate: func(link *Link) { link.RunSessionID = "" }},
		{name: "missing_message_session_id", mutate: func(link *Link) { link.MessageSessionID = "" }},
		{name: "everything_absent", mutate: func(link *Link) { *link = Link{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			link := validLink()
			test.mutate(&link)
			err := Validate(link)
			if err == nil {
				t.Fatalf("Validate(%s) = nil, want a correlation conflict", test.name)
			}
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("Validate(%s) = %v, want ErrConflict", test.name, err)
			}
		})
	}
}

// Correlation failures are read by migration preflight and by the public read
// path, so the error may name the class of problem and nothing about the rows.
func TestValidateReturnsOnlyTheValueFreeCorrelationSentinel(t *testing.T) {
	const sentinel = "run/message correlation conflict"
	if ErrConflict.Error() != sentinel {
		t.Fatalf("sentinel = %q, want %q", ErrConflict, sentinel)
	}

	link := Link{
		RunID:                 "run-secret-1",
		RunSessionID:          "session-secret-1",
		RunAssistantMessageID: "message-secret-1",
		MessageID:             "message-secret-2",
		MessageSessionID:      "session-secret-2",
		MessageRunID:          "run-secret-2",
		MessageRole:           "assistant text: sk-live-SECRET",
	}
	err := Validate(link)
	if err == nil {
		t.Fatal("Validate(fully mismatched link) = nil, want a correlation conflict")
	}
	if err.Error() != sentinel {
		t.Fatalf("error = %q, want exactly %q", err, sentinel)
	}
	for _, value := range []string{
		link.RunID, link.RunSessionID, link.RunAssistantMessageID,
		link.MessageID, link.MessageSessionID, link.MessageRunID, link.MessageRole,
	} {
		if strings.Contains(err.Error(), value) {
			t.Fatalf("error %q leaked %q", err, value)
		}
	}
}
