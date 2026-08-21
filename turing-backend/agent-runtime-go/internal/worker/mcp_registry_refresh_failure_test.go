package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestRegistryRefreshFailureTriggersSafeReconnect(t *testing.T) {
	worker := New(Options{
		DiscoverTools: func(context.Context) ([]*turingv1.DiscoveredTool, error) {
			return nil, errors.New("temporary registry read failure")
		},
	}, nil, &registryRefreshExecutor{})
	fatal := make(chan error, 1)
	worker.setFatalChannel(fatal)
	err := worker.handleCommand(
		context.Background(),
		nil,
		&turingv1.RuntimeCommand{
			Command: &turingv1.RuntimeCommand_McpRegistryChanged{
				McpRegistryChanged: &turingv1.RuntimeMcpRegistryChanged{
					RegistrationId: "registration-current",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("refresh blocked command dispatcher: %v", err)
	}
	select {
	case refreshErr := <-fatal:
		if refreshErr == nil {
			t.Fatal("refresh failure reported nil fatal error")
		}
	case <-time.After(time.Second):
		t.Fatal("refresh failure did not trigger reconnect")
	}
}
