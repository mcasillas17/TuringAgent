package host

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type healthServer struct {
	turingv1.UnimplementedHealthServiceServer
	ok    bool
	block chan struct{}
}

func (s *healthServer) Check(ctx context.Context, _ *turingv1.HealthCheckRequest) (*turingv1.HealthCheckResponse, error) {
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &turingv1.HealthCheckResponse{Ok: s.ok}, nil
}

// serveHealth starts an orchestrator stand-in that, like the real one,
// answers Unauthenticated unless the authorization header is exactly
// "Bearer <key>". It records every header it was sent.
func serveHealth(t *testing.T, key string, handler *healthServer) (port int, headers func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	intercept := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		values := md.Get("authorization")
		mu.Lock()
		seen = append(seen, strings.Join(values, "|"))
		mu.Unlock()
		if len(values) != 1 || values[0] != "Bearer "+key {
			return nil, status.Error(codes.Unauthenticated, "invalid bearer token")
		}
		return next(ctx, req)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(intercept))
	turingv1.RegisterHealthServiceServer(server, handler)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().(*net.TCPAddr).Port, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func TestCheckHealthSucceedsWithTheRightKeyAndSendsItAsABearerToken(t *testing.T) {
	port, headers := serveHealth(t, "tk_right", &healthServer{ok: true})

	if err := CheckHealth(context.Background(), port, "tk_right"); err != nil {
		t.Fatalf("CheckHealth = %v", err)
	}
	if got := headers(); len(got) != 1 || got[0] != "Bearer tk_right" {
		t.Fatalf("authorization headers = %q, want exactly [Bearer tk_right]", got)
	}
}

func TestCheckHealthNamesAWrongKeyUnauthenticated(t *testing.T) {
	port, _ := serveHealth(t, "tk_right", &healthServer{ok: true})

	err := CheckHealth(context.Background(), port, "tk_wrong")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
	if strings.Contains(err.Error(), "tk_wrong") || strings.Contains(err.Error(), "tk_right") {
		t.Fatalf("the error shows a key: %q", err)
	}
}

func TestCheckHealthTreatsNotOKAsAnError(t *testing.T) {
	port, _ := serveHealth(t, "k", &healthServer{ok: false})

	err := CheckHealth(context.Background(), port, "k")
	if err == nil || errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want a plain failure", err)
	}
}

func TestCheckHealthFailsFastWhenNothingListens(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	started := time.Now()
	err = CheckHealth(context.Background(), port, "k")
	if err == nil || errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want a connection failure", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("a refused connection took %s", elapsed)
	}
}

func TestCheckHealthGivesUpOnAnOrchestratorThatNeverAnswers(t *testing.T) {
	previous := healthTimeout
	healthTimeout = 100 * time.Millisecond
	t.Cleanup(func() { healthTimeout = previous })
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	port, _ := serveHealth(t, "k", &healthServer{ok: true, block: block})

	started := time.Now()
	err := CheckHealth(context.Background(), port, "k")
	if err == nil {
		t.Fatal("CheckHealth did not fail")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("CheckHealth waited %s", elapsed)
	}
}
