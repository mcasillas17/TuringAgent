package main

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"testing"
)

func TestInflightRegistryCancelsOnlyTheNamedRequest(t *testing.T) {
	registry := newInflightRegistry(8)
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelFirst()
	defer cancelSecond()

	releaseFirst, ok := registry.add("agent", "a", cancelFirst)
	if !ok {
		t.Fatal("add refused the first request")
	}
	defer releaseFirst()
	releaseSecond, ok := registry.add("agent", "b", cancelSecond)
	if !ok {
		t.Fatal("add refused the second request")
	}
	defer releaseSecond()

	if !registry.cancel("agent", "a") {
		t.Fatal("cancel did not find request a")
	}
	<-firstCtx.Done()
	select {
	case <-secondCtx.Done():
		t.Fatal("cancelling a also cancelled b")
	default:
	}
}

func TestInflightRegistryDistinguishesStringAndNumericIDs(t *testing.T) {
	// JSON-RPC ids may be strings or integers, and "1" is not 1. Collapsing
	// them would let one caller cancel a request it does not own.
	registry := newInflightRegistry(8)
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	release, ok := registry.add("agent", json.Number("1"), cancel)
	if !ok {
		t.Fatal("add refused a numeric id")
	}
	defer release()

	if registry.cancel("agent", "1") {
		t.Fatal("a string id cancelled a numerically identified request")
	}
	if !registry.cancel("agent", json.Number("1")) {
		t.Fatal("the numeric id could not cancel its own request")
	}
}

func TestInflightRegistryMatchesTheTwoDecodedFormsOfOneNumericID(t *testing.T) {
	// A request id decodes through UseNumber (json.Number); the requestId in a
	// cancellation's params does not (float64). The same numeric id must reach
	// the same entry either way, or cancellation is inert for every id the
	// runtime actually sends.
	registry := newInflightRegistry(8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release, ok := registry.add("agent", json.Number("4242"), cancel)
	if !ok {
		t.Fatal("add refused a numeric id")
	}
	defer release()

	if !registry.cancel("agent", float64(4242)) {
		t.Fatal("a float64 requestId did not reach the request registered under json.Number")
	}
	<-ctx.Done()
}

func TestInflightRegistryRefusesANumericIDItCannotRepresentExactly(t *testing.T) {
	// A fractional, infinite or out-of-range float names no request the id
	// grammar can produce, so it must be refused rather than rounded onto a
	// neighbouring request.
	registry := newInflightRegistry(8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release, _ := registry.add("agent", json.Number("9007199254740993"), cancel)
	defer release()

	for name, id := range map[string]any{
		"fractional":   float64(1.5),
		"infinite":     math.Inf(1),
		"not a number": math.NaN(),
		"past exact":   float64(1 << 60),
		"unsupported":  true,
	} {
		if _, keyed := inflightKey("agent", id); keyed {
			t.Errorf("inflightKey accepted a %s id", name)
		}
	}
	select {
	case <-ctx.Done():
		t.Fatal("an unrepresentable id cancelled a registered request")
	default:
	}
}

func TestInflightRegistryForgetsAReleasedRequest(t *testing.T) {
	registry := newInflightRegistry(8)
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	release, _ := registry.add("agent", "a", cancel)
	release()

	if registry.cancel("agent", "a") {
		t.Fatal("a completed request is still cancellable; the entry leaked")
	}
	// Read the map directly: this test is in the same package, and an accessor
	// with no production caller is surface the server does not need.
	registry.mu.Lock()
	remaining := len(registry.cancels)
	registry.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("registry holds %d entries after release, want 0", remaining)
	}
}

func TestInflightRegistryReleaseIsIdempotentAndDoesNotEvictAReusedID(t *testing.T) {
	// A late release must never remove an entry that a *later* request with
	// the same id has since taken, or that second request becomes
	// uncancellable.
	registry := newInflightRegistry(8)
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelFirst()
	defer cancelSecond()

	releaseFirst, _ := registry.add("agent", "a", cancelFirst)
	releaseFirst()
	releaseSecond, ok := registry.add("agent", "a", cancelSecond)
	if !ok {
		t.Fatal("the id could not be reused after release")
	}
	defer releaseSecond()
	releaseFirst() // a duplicate, late release of the first request

	if !registry.cancel("agent", "a") {
		t.Fatal("the second request was evicted by the first one's late release")
	}
	<-secondCtx.Done()
	_ = firstCtx
}

func TestInflightRegistryRefusesADuplicateInFlightID(t *testing.T) {
	registry := newInflightRegistry(8)
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	release, ok := registry.add("agent", "a", cancel)
	if !ok {
		t.Fatal("add refused the first request")
	}
	defer release()
	if _, ok := registry.add("agent", "a", func() {}); ok {
		t.Fatal("two concurrent requests may not share one id: a cancellation could not name either")
	}
}

func TestInflightRegistryIsBounded(t *testing.T) {
	registry := newInflightRegistry(2)
	for _, id := range []string{"a", "b"} {
		if _, ok := registry.add("agent", id, func() {}); !ok {
			t.Fatalf("add refused %q below the limit", id)
		}
	}
	if _, ok := registry.add("agent", "c", func() {}); ok {
		t.Fatal("the registry grew past its limit")
	}
}

func TestInflightRegistryIsSafeUnderConcurrentUse(t *testing.T) {
	registry := newInflightRegistry(64)
	var wait sync.WaitGroup
	for index := range 32 {
		wait.Add(2)
		id := json.Number(string(rune('0' + index%10)))
		go func() {
			defer wait.Done()
			_, cancel := context.WithCancel(context.Background())
			if release, ok := registry.add("agent", id, cancel); ok {
				release()
			} else {
				cancel()
			}
		}()
		go func() {
			defer wait.Done()
			registry.cancel("agent", id)
		}()
	}
	wait.Wait()
}

func TestInflightRegistryTreatsNegativeZeroAsTheSameNumericID(t *testing.T) {
	// jsonrpc's id grammar accepts "-0", and JSON's -0 and 0 are one number.
	// The float form a cancellation carries always canonicalizes to "0", so
	// without folding the sign a request registered as "-0" could never be
	// cancelled, and worse, a cancellation naming -0 would land on whichever
	// sibling request holds the id 0.
	registry := newInflightRegistry(8)

	negative, cancelNegative := context.WithCancel(context.Background())
	defer cancelNegative()
	releaseNegative, ok := registry.add("agent", json.Number("-0"), cancelNegative)
	if !ok {
		t.Fatal("add refused the id -0")
	}
	defer releaseNegative()

	if !registry.cancel("agent", math.Copysign(0, -1)) {
		t.Fatal("a negative-zero requestId did not reach the request registered under json.Number(\"-0\")")
	}
	<-negative.Done()
	releaseNegative()

	// And the inverse: -0 must not be a second, distinct id that a cancellation
	// naming 0 can be diverted onto.
	_, cancelPositive := context.WithCancel(context.Background())
	defer cancelPositive()
	releasePositive, ok := registry.add("agent", json.Number("0"), cancelPositive)
	if !ok {
		t.Fatal("add refused the id 0")
	}
	defer releasePositive()

	negativeKey, keyed := inflightKey("agent", json.Number("-0"))
	if !keyed {
		t.Fatal("inflightKey refused json.Number(\"-0\")")
	}
	positiveKey, keyed := inflightKey("agent", json.Number("0"))
	if !keyed {
		t.Fatal("inflightKey refused json.Number(\"0\")")
	}
	if negativeKey != positiveKey {
		t.Fatalf("keys for -0 and 0 = %q and %q, want one key for one JSON number", negativeKey, positiveKey)
	}
}
