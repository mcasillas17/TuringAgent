package runtime

import (
	"context"
	"testing"
	"time"
)

func TestNotifyMCPRegistryChangedTargetsEachCurrentRegistration(t *testing.T) {
	h := newHarness(t)
	commands := make(chan workerCommand, 1)
	h.service.mu.Lock()
	h.service.workers["worker-registry-refresh"] = &worker{
		commands:       commands,
		registrationID: "registration-current",
		maxConcurrent:  1,
		assignments:    map[string]assignment{},
	}
	h.service.mu.Unlock()

	if err := h.service.NotifyMCPRegistryChanged(context.Background()); err != nil {
		t.Fatal(err)
	}

	select {
	case queued := <-commands:
		changed := queued.command.GetMcpRegistryChanged()
		if changed.GetRegistrationId() != "registration-current" {
			t.Fatalf("registry change = %+v", changed)
		}
	case <-time.After(time.Second):
		t.Fatal("registry change command was not delivered")
	}
}
