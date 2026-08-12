package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestTokenFromMetadata(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer secret"))
	got, ok := TokenFromMetadata(ctx)
	if !ok || got != "secret" {
		t.Fatalf("TokenFromMetadata = %q/%v", got, ok)
	}
}

func TestConstantTimeTokenMatch(t *testing.T) {
	if !TokenMatches("secret", "secret") {
		t.Fatal("same token did not match")
	}
	if TokenMatches("secret", "different") {
		t.Fatal("different tokens matched")
	}
}

func TestAsyncFailureRecorderKeepsAuthenticationFailureNonblocking(t *testing.T) {
	started := make(chan struct{})
	recorder := NewAsyncFailureRecorder(func(ctx context.Context, _ Failure) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := recorder.Close(closeCtx); err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("close recorder: %v", err)
		}
	})
	interceptor := UnaryInterceptor("expected", InterceptorOptions{
		ActorType:       "client",
		FailureRecorder: recorder.Record,
	})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer invalid"))

	start := time.Now()
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/turing.v1.HealthService/Check"}, func(context.Context, any) (any, error) {
		t.Fatal("unauthenticated request reached handler")
		return nil, nil
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("interceptor error = %v, want Unauthenticated", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("authentication failure blocked on audit storage for %v", elapsed)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("queued audit was not delivered")
	}
}

func TestAsyncFailureRecorderBoundsPendingAudits(t *testing.T) {
	started := make(chan struct{})
	recorder := NewAsyncFailureRecorder(func(ctx context.Context, _ Failure) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	if err := recorder.Record(context.Background(), Failure{Method: "first"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first audit did not start")
	}
	for index := 0; index < authFailureQueueCapacity*2; index++ {
		if err := recorder.Record(context.Background(), Failure{Method: "flood"}); err != nil {
			t.Fatal(err)
		}
	}
	if pending := len(recorder.queue); pending != authFailureQueueCapacity {
		t.Fatalf("pending audits = %d, want bounded capacity %d", pending, authFailureQueueCapacity)
	}
	if dropped := recorder.dropped.Load(); dropped == 0 {
		t.Fatal("audit flood did not drop excess records")
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := recorder.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
}
