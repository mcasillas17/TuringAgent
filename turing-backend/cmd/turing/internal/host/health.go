package host

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ErrUnauthenticated means the orchestrator is up and refused the key: the one
// in .env is not the one it started with.
var ErrUnauthenticated = errors.New("the orchestrator did not accept the app key")

var healthTimeout = 3 * time.Second

// CheckHealth asks the orchestrator on 127.0.0.1:port whether it is healthy,
// the way the app does: plain gRPC, with the key as a bearer token.
func CheckHealth(ctx context.Context, port int, key string) error {
	conn, err := grpc.NewClient(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("could not reach the orchestrator on port %d: %w", port, err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+key)
	response, err := turingv1.NewHealthServiceClient(conn).Check(ctx, &turingv1.HealthCheckRequest{})
	if status.Code(err) == codes.Unauthenticated {
		return ErrUnauthenticated
	}
	if err != nil {
		return fmt.Errorf("the orchestrator did not answer on port %d: %w", port, err)
	}
	if !response.GetOk() {
		return fmt.Errorf("the orchestrator on port %d reports it is not healthy", port)
	}
	return nil
}
