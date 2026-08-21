package runtime

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestNotifyMCPRegistryChangedTargetsEachCurrentRegistration(t *testing.T) {
	h := newHarness(t)
	commands := make(chan *turingv1.RuntimeCommand, 1)
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
	case command := <-commands:
		changed := command.GetMcpRegistryChanged()
		if changed.GetRegistrationId() != "registration-current" {
			t.Fatalf("registry change = %+v", changed)
		}
	case <-time.After(time.Second):
		t.Fatal("registry change command was not delivered")
	}
}
