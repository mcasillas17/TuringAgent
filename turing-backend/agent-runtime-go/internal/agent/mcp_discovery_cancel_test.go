package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthyWaiterRetriesCancelledRegistryDiscoveryLeader(t *testing.T) {
	firstStarted := make(chan struct{})
	var calls atomic.Int32
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{
		RegisteredMCPServers: func(ctx context.Context) (map[string]ToolLister, error) {
			if calls.Add(1) == 1 {
				close(firstStarted)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return map[string]ToolLister{}, nil
		},
	})
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		_, err := assistant.DiscoveredTools(leaderCtx)
		leaderDone <- err
	}()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("leader discovery did not start")
	}
	waiterDone := make(chan error, 1)
	go func() {
		_, err := assistant.DiscoveredTools(context.Background())
		waiterDone <- err
	}()
	cancelLeader()
	select {
	case <-leaderDone:
	case <-time.After(time.Second):
		t.Fatal("cancelled leader did not finish")
	}
	select {
	case err := <-waiterDone:
		if err != nil {
			t.Fatalf("healthy waiter inherited cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("healthy waiter did not retry")
	}
	if calls.Load() < 2 {
		t.Fatalf("registry discovery calls = %d, want retry", calls.Load())
	}
}
