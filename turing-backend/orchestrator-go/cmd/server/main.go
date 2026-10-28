package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/mcasillas17/TuringAgent/turing-backend/internal/servicecmd"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/app"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"google.golang.org/grpc"
)

// checks are the subcommands that validate without serving. The Dockerfile's
// org.turing.checks label must list exactly these; a test enforces it.
var checks = []servicecmd.Check{{Name: "config", Run: checkConfig}}

func main() {
	os.Exit(servicecmd.Main(os.Args[1:], os.Environ(), checks, run, os.Stdout, os.Stderr))
}

// checkConfig is the environment-only part of startup: the same loader run()
// starts with, and nothing after it. app.New's other refusals need the
// database, the files under the mounted roots, or concern generated secrets,
// so they surface when the change is applied, not here.
func checkConfig(environ []string, _ io.Writer) ([]string, error) {
	cfg, err := config.LoadFromMap(servicecmd.EnvMap(environ))
	if err != nil {
		return nil, err
	}
	return cfg.DeprecatedKeys, nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	orchestrator, err := app.New(cfg)
	if err != nil {
		return err
	}
	defer orchestrator.Stop()

	publicLis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.PublicPort))
	if err != nil {
		return fmt.Errorf("listen public: %w", err)
	}
	internalLis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.InternalPort))
	if err != nil {
		_ = publicLis.Close()
		return fmt.Errorf("listen internal: %w", err)
	}

	errCh := make(chan error, 2)
	go serve(errCh, "public", orchestrator.PublicServer, publicLis)
	go serve(errCh, "internal", orchestrator.InternalServer, internalLis)

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	select {
	case sig := <-signalCh:
		log.Printf("received %s, shutting down", sig)
		orchestrator.Stop()
		return nil
	case err := <-errCh:
		orchestrator.Stop()
		return err
	}
}

func serve(errCh chan<- error, name string, server *grpc.Server, lis net.Listener) {
	if err := server.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		errCh <- fmt.Errorf("serve %s: %w", name, err)
	}
}
