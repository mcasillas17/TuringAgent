package runtime

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// fakeAuthority stands in for the team service's profile resolution.
type fakeAuthority struct {
	mu     sync.Mutex
	active bool
	err    error
	asked  []string
	// during, when set, runs inside the resolution, as whatever else happens
	// while the profile file is read.
	during func(ctx context.Context)
}

func (f *fakeAuthority) DelegatedProfileActive(ctx context.Context, profileID, revision string) (bool, error) {
	f.mu.Lock()
	f.asked = append(f.asked, profileID+"@"+revision)
	during, active, err := f.during, f.active, f.err
	f.mu.Unlock()
	if during != nil {
		during(ctx)
	}
	return active, err
}

func (f *fakeAuthority) questions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.asked)
}

// A claimed task whose specialist's profile no longer resolves active at the
// task's revision — its file deleted or its authority edited, or unreadable —
// is withdrawn before any worker is sent it. Its claim's execution is released
// with it, so the one slot the configuration allows goes to the continuation
// the withdrawal joined, and that is the first run the worker is given.
func TestATaskWhoseSpecialistIsNoLongerActiveNeverReachesAWorker(t *testing.T) {
	for _, tc := range []struct {
		name      string
		authority *fakeAuthority
	}{
		{name: "not active", authority: &fakeAuthority{}},
		{name: "unresolvable", authority: &fakeAuthority{err: errors.New("team root must be a real directory")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := newHarnessWithDispatch(t, DispatchConfig{MaxConcurrentRuns: 1})
			h.service.SetDelegationAuthority(tc.authority)
			sessionID, parent, delegation := seedDelegatedParent(t, h, []string{"system/system.time"})
			published, unsubscribe := h.bus.Subscribe(sessionID)
			defer unsubscribe()

			stream := connectWorkerCapabilities(t, h, "team-worker", "registration-team", teamCapabilities(1, "system.time"))
			if err := h.service.DispatchPending(ctx); err != nil {
				t.Fatal(err)
			}

			assigned := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
				return command.GetRunAssigned() != nil
			}).GetRunAssigned()
			if assigned.GetRunId() == delegation.ChildRunID || assigned.GetSessionId() != sessionID || assigned.GetRunId() == parent.RunID {
				t.Fatalf("assigned = %s in %s, want the parent's continuation, never the task", assigned.GetRunId(), assigned.GetSessionId())
			}
			child, err := h.repo.GetRun(ctx, delegation.ChildRunID)
			if err != nil {
				t.Fatal(err)
			}
			if child.Status != "cancelled" || child.OutcomeReason != string(runoutcome.ReasonAbandoned) ||
				child.CancellationReason != runoutcome.CodeProfileGrantRevoked || child.ExecutionActive {
				t.Fatalf("task = %+v; want it withdrawn profile_grant_revoked with its execution released", child)
			}
			if asked := tc.authority.questions(); !slices.Equal(asked, []string{"research@rev-1"}) {
				t.Fatalf("authority asked %v, want the task's specialist at its revision once", asked)
			}
			for {
				select {
				case event := <-published:
					if event.Type == "delegation.finished" {
						return
					}
				case <-time.After(time.Second):
					t.Fatal("the withdrawal's join was not published on the turn's stream")
				}
			}
		})
	}
}

// A dispatch that runs out of time while the profile is read proves nothing
// about the profile: the task goes back to the queue, unharmed, and is sent
// on a later dispatch.
func TestATaskWhoseDispatchEndsDuringTheProfileCheckIsRequeued(t *testing.T) {
	h := newHarness(t)
	authority := &fakeAuthority{}
	h.service.SetDelegationAuthority(authority)
	stream := connectWorkerCapabilities(t, h, "team-worker", "registration-team", teamCapabilities(1, "system.time"))
	_, _, delegation := seedDelegatedParent(t, h, []string{"system/system.time"})
	dispatchCtx, endDispatch := context.WithCancel(context.Background())
	// The resolver's own routing reads see the ended context and report no
	// model served, so the profile reads as not active.
	authority.during = func(context.Context) { endDispatch() }

	if err := h.service.DispatchPending(dispatchCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("dispatch = %v, want it cut short", err)
	}
	child, err := h.repo.GetRun(context.Background(), delegation.ChildRunID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "queued" || child.CancellationReason != "" || child.ExecutionActive {
		t.Fatalf("task = %+v; want it back in the queue, not withdrawn", child)
	}

	authority.mu.Lock()
	authority.during, authority.active = nil, true
	authority.mu.Unlock()
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetRunId() != delegation.ChildRunID {
		t.Fatalf("assigned %s, want the requeued task %s", assigned.GetRunId(), delegation.ChildRunID)
	}
}

// A task that something else ends while its profile is read — the user
// cancelling it from its card — is left as that ended it, and the claim's
// execution is released: nothing was ever sent.
func TestATaskEndedDuringItsProfileCheckIsLeftAsItEnded(t *testing.T) {
	h := newHarness(t)
	authority := &fakeAuthority{}
	h.service.SetDelegationAuthority(authority)
	stream := connectWorkerCapabilities(t, h, "team-worker", "registration-team", teamCapabilities(1, "system.time"))
	sessionID, parent, delegation := seedDelegatedParent(t, h, []string{"system/system.time"})
	authority.during = func(ctx context.Context) {
		if _, err := h.repo.CancelUserRun(ctx, delegation.ChildSessionID, delegation.ChildRunID, "cancel-task"); err != nil {
			t.Error(err)
		}
	}

	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	child, err := h.repo.GetRun(context.Background(), delegation.ChildRunID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "cancelled" || child.OutcomeReason != string(runoutcome.ReasonUserCancelled) ||
		child.CancellationReason == runoutcome.CodeProfileGrantRevoked || child.ExecutionActive {
		t.Fatalf("task = %+v; want it cancelled by the user, its execution released", child)
	}
	assigned := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetRunId() == delegation.ChildRunID || assigned.GetSessionId() != sessionID || assigned.GetRunId() == parent.RunID {
		t.Fatalf("assigned %s, want the turn's continuation, never the task", assigned.GetRunId())
	}
}

// A task whose specialist still resolves active at its revision is sent as
// before.
func TestATaskWhoseSpecialistIsStillActiveIsDispatched(t *testing.T) {
	h := newHarness(t)
	authority := &fakeAuthority{active: true}
	h.service.SetDelegationAuthority(authority)
	_, _, delegation := seedDelegatedParent(t, h, []string{"system/system.time"})
	stream := connectWorkerCapabilities(t, h, "team-worker", "registration-team", teamCapabilities(1, "system.time"))
	if err := h.service.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	assigned := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetRunId() != delegation.ChildRunID {
		t.Fatalf("assigned %s, want the task %s", assigned.GetRunId(), delegation.ChildRunID)
	}
	if asked := authority.questions(); !slices.Equal(asked, []string{"research@rev-1"}) {
		t.Fatalf("authority asked %v, want the task's specialist once", asked)
	}
}
