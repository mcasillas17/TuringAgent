package app

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UX-002a: the Needs-you queue is the owner's. It sits behind the client key
// on the public server only; the runtime and every other internal identity
// are refused, and it is not registered on the internal server at all.
func TestAttentionServiceIsAuthenticatedAndPublicOnly(t *testing.T) {
	app := newTestApp(t)
	if _, ok := app.PublicServer.GetServiceInfo()["turing.v1.AttentionService"]; !ok {
		t.Fatal("public server missing turing.v1.AttentionService")
	}
	if _, ok := app.InternalServer.GetServiceInfo()["turing.v1.AttentionService"]; ok {
		t.Fatal("internal server exposes turing.v1.AttentionService")
	}
	bearer := func(token string) context.Context {
		return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	}
	public := turingv1.NewAttentionServiceClient(newBufconnClient(t, app.PublicServer))
	internal := turingv1.NewAttentionServiceClient(newBufconnClient(t, app.InternalServer))
	calls := map[string]func(turingv1.AttentionServiceClient, context.Context) error{
		"ListAttention": func(client turingv1.AttentionServiceClient, ctx context.Context) error {
			_, err := client.ListAttention(ctx, &turingv1.ListAttentionRequest{})
			return err
		},
		"DismissAttention": func(client turingv1.AttentionServiceClient, ctx context.Context) error {
			_, err := client.DismissAttention(ctx, &turingv1.DismissAttentionRequest{AutomationId: "auto_x", OccurrenceKey: "run_x"})
			return err
		},
	}
	for name, call := range calls {
		if err := call(public, context.Background()); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("%s without a key = %v, want Unauthenticated", name, err)
		}
		for _, token := range []string{"internal", "internal-approval-consumer"} {
			if err := call(public, bearer(token)); status.Code(err) != codes.Unauthenticated {
				t.Fatalf("%s with internal token %q on the public server = %v, want Unauthenticated", name, token, err)
			}
			if err := call(internal, bearer(token)); status.Code(err) != codes.Unimplemented {
				t.Fatalf("%s with internal token %q on the internal server = %v, want Unimplemented", name, token, err)
			}
		}
	}
	if err := calls["ListAttention"](public, bearer("client")); err != nil {
		t.Fatalf("ListAttention with the client key = %v", err)
	}
	if err := calls["DismissAttention"](public, bearer("client")); status.Code(err) != codes.NotFound {
		t.Fatalf("DismissAttention of an unknown automation = %v, want NotFound", err)
	}
}

// The hint is wired end to end: disabling an automation, which publishes no
// event of its own, reaches a client's updates stream as ATTENTION_CHANGED.
func TestAnAutomationChangeHintsTheUpdatesStream(t *testing.T) {
	app := newTestApp(t)
	conn := newBufconnClient(t, app.PublicServer)
	ctx, cancel := context.WithTimeout(
		metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer client")), 5*time.Second)
	defer cancel()
	automations := turingv1.NewAutomationServiceClient(conn)
	created, err := automations.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
		Name: "Digest", Prompt: "x", Enabled: true,
		Schedule: &turingv1.AutomationSchedule{
			Kind: turingv1.AutomationScheduleKind_AUTOMATION_SCHEDULE_KIND_INTERVAL, IntervalMinutes: 5,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := turingv1.NewEventServiceClient(conn).SubscribeSessionUpdates(ctx, &turingv1.SubscribeSessionUpdatesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// The stream subscribes on the server once the call arrives; keep
	// changing the automation until a hint comes back.
	go func() {
		enabled := false
		for ctx.Err() == nil {
			_, _ = automations.SetAutomationEnabled(ctx, &turingv1.SetAutomationEnabledRequest{
				AutomationId: created.GetAutomationId(), Enabled: enabled,
			})
			enabled = !enabled
			time.Sleep(20 * time.Millisecond)
		}
	}()
	for {
		event, err := stream.Recv()
		if err != nil {
			t.Fatalf("no hint before the stream ended: %v", err)
		}
		if event.GetType() == turingv1.TuringEventType_TURING_EVENT_TYPE_ATTENTION_CHANGED {
			if event.GetSessionId() != "" || len(event.GetPayload().GetFields()) != 0 {
				t.Fatalf("hint = %+v, want it content-free", event)
			}
			return
		}
	}
}
