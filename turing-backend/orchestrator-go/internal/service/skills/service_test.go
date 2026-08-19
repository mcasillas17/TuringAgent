package skills

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	_ "github.com/mattn/go-sqlite3"
)

// The status code is the whole contract with the client: it is what tells a UI
// to say "that name is taken" rather than "something went wrong".
func TestSkillErrorMapsEachFailureToItsCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"missing skill", repository.ErrSkillNotFound, codes.NotFound},
		{"missing session", repository.ErrSessionNotFound, codes.NotFound},
		{"not attached", repository.ErrSkillNotAttached, codes.NotFound},
		{"duplicate name", repository.ErrSkillNameTaken, codes.AlreadyExists},
		{"empty name", repository.ErrSkillNameEmpty, codes.InvalidArgument},
		{"empty instructions", repository.ErrSkillNoContent, codes.InvalidArgument},
		{"name too long", repository.ErrSkillNameTooLong, codes.InvalidArgument},
		{"instructions too long", repository.ErrSkillInstructionsLong, codes.InvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := status.Code(skillError(tt.err, "fallback")); got != tt.want {
				t.Fatalf("code = %v, want %v", got, tt.want)
			}
		})
	}
}

// A storage error must not reach the caller with its text intact — it can name
// tables, paths, or the contents of a failing statement.
func TestSkillErrorHidesUnrecognisedFailures(t *testing.T) {
	err := skillError(fmt.Errorf("no such column: secret_column in /var/data/turing.db"), "create skill failed")

	if got := status.Code(err); got != codes.Internal {
		t.Fatalf("code = %v, want Internal", got)
	}
	message := status.Convert(err).Message()
	if message != "create skill failed" {
		t.Fatalf("message = %q, want the fixed fallback", message)
	}
	if strings.Contains(message, "secret_column") || strings.Contains(message, "turing.db") {
		t.Fatalf("storage detail leaked to the caller: %q", message)
	}
}

func TestCreateSkillRoundTrips(t *testing.T) {
	server, ctx := newSkillServer(t)

	created, err := server.CreateSkill(ctx, &turingv1.CreateSkillRequest{
		Name:         "Tone",
		Instructions: "Be brief.",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.GetSkillId() == "" || created.GetName() != "Tone" || created.GetInstructions() != "Be brief." {
		t.Fatalf("created = %+v, want the submitted fields and an id", created)
	}
	// Timestamps are what let a client sort or show "last edited"; a nil here
	// means the string form and the parser have drifted apart.
	if created.GetCreatedAt() == nil || created.GetUpdatedAt() == nil {
		t.Fatalf("created = %+v, want both timestamps populated", created)
	}

	listed, err := server.ListSkills(ctx, &turingv1.ListSkillsRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed.GetSkills()) != 1 || listed.GetSkills()[0].GetSkillId() != created.GetSkillId() {
		t.Fatalf("listed = %+v, want the created skill", listed.GetSkills())
	}
}

func TestCreateSkillRejectsADuplicateName(t *testing.T) {
	server, ctx := newSkillServer(t)
	if _, err := server.CreateSkill(ctx, &turingv1.CreateSkillRequest{
		Name: "Tone", Instructions: "Be brief.",
	}); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err := server.CreateSkill(ctx, &turingv1.CreateSkillRequest{
		Name: "tone", Instructions: "Something else.",
	})

	if got := status.Code(err); got != codes.AlreadyExists {
		t.Fatalf("code = %v, want AlreadyExists", got)
	}
}

func TestCreateSkillRejectsOversizedInstructions(t *testing.T) {
	server, ctx := newSkillServer(t)

	_, err := server.CreateSkill(ctx, &turingv1.CreateSkillRequest{
		Name:         "Tone",
		Instructions: strings.Repeat("a", 20000),
	})

	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", got)
	}
}

// Attaching to a conversation that no longer exists must not report a missing
// skill — that sends the user looking in the wrong place.
func TestAttachSkillDistinguishesAMissingSessionFromAMissingSkill(t *testing.T) {
	server, ctx := newSkillServer(t)
	skill, err := server.CreateSkill(ctx, &turingv1.CreateSkillRequest{
		Name: "Tone", Instructions: "Be brief.",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = server.AttachSkill(ctx, &turingv1.AttachSkillRequest{
		SessionId: "sess_gone",
		SkillId:   skill.GetSkillId(),
	})
	if status.Convert(err).Message() != "conversation not found" {
		t.Fatalf("message = %q, want it to name the conversation", status.Convert(err).Message())
	}
}

func TestAttachAndDetachReturnTheConversationsWholeSet(t *testing.T) {
	server, ctx := newSkillServer(t)
	session := newSession(t, ctx, server)
	tone := mustCreate(t, ctx, server, "Tone", "Be brief.")
	format := mustCreate(t, ctx, server, "Format", "Use bullets.")

	attached, err := server.AttachSkill(ctx, &turingv1.AttachSkillRequest{
		SessionId: session, SkillId: tone.GetSkillId(),
	})
	if err != nil {
		t.Fatalf("attach tone: %v", err)
	}
	if len(attached.GetSkills()) != 1 {
		t.Fatalf("attached = %+v, want one", attached.GetSkills())
	}

	attached, err = server.AttachSkill(ctx, &turingv1.AttachSkillRequest{
		SessionId: session, SkillId: format.GetSkillId(),
	})
	if err != nil {
		t.Fatalf("attach format: %v", err)
	}
	// Sorted by name, so the set reads identically wherever it is shown.
	if names := skillNames(attached.GetSkills()); names != "Format,Tone" {
		t.Fatalf("attached = %s, want Format,Tone", names)
	}

	remaining, err := server.DetachSkill(ctx, &turingv1.DetachSkillRequest{
		SessionId: session, SkillId: format.GetSkillId(),
	})
	if err != nil {
		t.Fatalf("detach: %v", err)
	}
	if names := skillNames(remaining.GetSkills()); names != "Tone" {
		t.Fatalf("remaining = %s, want Tone", names)
	}
}

func TestDetachReportsASkillThatWasNotAttached(t *testing.T) {
	server, ctx := newSkillServer(t)
	session := newSession(t, ctx, server)
	skill := mustCreate(t, ctx, server, "Tone", "Be brief.")

	_, err := server.DetachSkill(ctx, &turingv1.DetachSkillRequest{
		SessionId: session, SkillId: skill.GetSkillId(),
	})

	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", got)
	}
}

func TestUpdateSkillReportsAMissingSkill(t *testing.T) {
	server, ctx := newSkillServer(t)

	_, err := server.UpdateSkill(ctx, &turingv1.UpdateSkillRequest{
		SkillId: "skill_gone", Name: "Tone", Instructions: "Be brief.",
	})

	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", got)
	}
}

// An empty conversation must answer with an empty list rather than an error,
// or every fresh chat would show a failure.
func TestListSessionSkillsIsEmptyForAFreshConversation(t *testing.T) {
	server, ctx := newSkillServer(t)
	session := newSession(t, ctx, server)

	response, err := server.ListSessionSkills(ctx, &turingv1.ListSessionSkillsRequest{
		SessionId: session,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(response.GetSkills()) != 0 {
		t.Fatalf("skills = %+v, want none", response.GetSkills())
	}
}

func TestParseTimestampReturnsNilForAnUnparseableValue(t *testing.T) {
	if got := parseTimestamp("not a timestamp"); got != nil {
		t.Fatalf("parseTimestamp = %v, want nil", got)
	}
}

type skillServer struct {
	*Server
	repo *repository.Repository
}

func newSkillServer(t *testing.T) (*skillServer, context.Context) {
	t.Helper()
	database := openSkillTestDB(t)
	repo := repository.New(database)
	return &skillServer{Server: New(repo), repo: repo}, context.Background()
}

func newSession(t *testing.T, ctx context.Context, server *skillServer) string {
	t.Helper()
	session, err := server.repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return session.SessionID
}

func mustCreate(t *testing.T, ctx context.Context, server *skillServer, name, instructions string) *turingv1.Skill {
	t.Helper()
	skill, err := server.CreateSkill(ctx, &turingv1.CreateSkillRequest{
		Name: name, Instructions: instructions,
	})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return skill
}

func skillNames(skills []*turingv1.Skill) string {
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.GetName())
	}
	return strings.Join(names, ",")
}

func openSkillTestDB(t *testing.T) *db.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	sqlDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared&_foreign_keys=on", name))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	database := &db.DB{DB: sqlDB}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}
