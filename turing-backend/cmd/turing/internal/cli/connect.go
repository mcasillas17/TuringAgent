package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
)

const (
	// sec001Notice is all `connect` says to someone on the server over SSH:
	// the app has no other way in until device pairing exists.
	sec001Notice = "Run the app on this machine: until device pairing (SEC-001) ships, Turing accepts the app only from the machine it runs on."

	rotateQuestion = "Apply now? This restarts the orchestrator and stops a reply in progress."
	rotateDeclined = "The old key keeps working until the next start; run ./turing restart to switch."
)

// connect prints how the app connects to this server, or with --rotate-key
// makes a new app key first.
func (e Env) connect(ctx context.Context, args []string) int {
	set := e.flagSet("connect")
	rotate := set.Bool("rotate-key", false, "")
	yes := set.Bool("yes", false, "")
	if code, ok := e.parseFlags(set, args); !ok {
		return code
	}
	if *rotate {
		return e.rotateKey(ctx, *yes)
	}
	port, key, err := e.endpoint()
	if err != nil {
		return e.fail("%v", err)
	}
	e.printConnection(port, key)
	return 0
}

// printConnection is the one place the app's key is shown.
func (e Env) printConnection(port int, key string) {
	_, _ = fmt.Fprintf(e.Stdout, "Backend URL: http://localhost:%d\n", port)
	_, _ = fmt.Fprintf(e.Stdout, "API key: %s\n", key)
	_, _ = fmt.Fprintln(e.Stdout, "Anyone holding this key can use Turing.")
	if e.Getenv("SSH_CONNECTION") != "" {
		_, _ = fmt.Fprintln(e.Stdout, sec001Notice)
	}
}

// rotateKey saves a new app key in .env, which needs neither Docker nor the
// images, then on confirmation restarts the orchestrator with it.
func (e Env) rotateKey(ctx context.Context, yes bool) int {
	key, err := newClientKey()
	if err != nil {
		return e.fail("could not make a new key: %v; run ./turing connect --rotate-key again", err)
	}
	port, err := e.saveClientKey(key)
	if err != nil {
		return e.fail("%v", err)
	}
	_, _ = fmt.Fprintln(e.Stdout, "A new app key is saved in .env.")

	if !e.confirm(rotateQuestion, true, yes) {
		_, _ = fmt.Fprintln(e.Stdout, rotateDeclined)
		return 0
	}
	// `restart` would keep the orchestrator's old environment; `up -d`
	// recreates it with the new one.
	if code, err := e.compose().Run(ctx, nil, nil, e.Stdout, e.Stderr, "up", "-d"); err != nil || code != 0 {
		exit := e.composeFailed(ctx, code, err, "./turing restart")
		_, _ = fmt.Fprintln(e.Stderr, "The new app key is saved in .env; once Compose works, run ./turing restart to switch to it.")
		return exit
	}
	_, _ = fmt.Fprintln(e.Stdout, "Waiting for Turing to become healthy...")
	if err := e.waitHealthy(ctx, port, key); err != nil {
		return e.fail("the new app key is saved in .env, but %v", err)
	}
	_, _ = fmt.Fprintln(e.Stdout, "Turing is running with the new key. Give it to the app: the old key no longer works.")
	e.printConnection(port, key)
	return 0
}

// saveClientKey replaces the app's key in .env, under the .env lock, and
// returns the port the orchestrator is published on. It touches nothing when
// .env is unusable.
func (e Env) saveClientKey(key string) (port int, err error) {
	// A missing .env is said before the lock file is made.
	if _, statErr := os.Lstat(filepath.Join(e.BackendDir, ".env")); errors.Is(statErr, fs.ErrNotExist) {
		return 0, errNoDotEnv
	}
	unlock, err := envfile.Lock(filepath.Join(e.BackendDir, ".env.lock"))
	if errors.Is(err, envfile.ErrLocked) {
		return 0, fmt.Errorf("%w; wait for it to finish, then run ./turing connect --rotate-key again", err)
	}
	if err != nil {
		return 0, fmt.Errorf("cannot lock .env: %w; check that %s is writable by you, then run ./turing connect --rotate-key again", err, e.BackendDir)
	}
	defer unlock()

	file, err := e.readDotEnv()
	if err != nil {
		return 0, err
	}
	if port, err = publicPort(file); err != nil {
		return 0, err
	}
	if err := file.Set(clientKey, key); err != nil {
		return 0, fmt.Errorf("cannot set %s in .env: %w; fix .env and run ./turing connect --rotate-key again", clientKey, err)
	}
	if err := envfile.WriteAtomic(filepath.Join(e.BackendDir, ".env"), file.Bytes()); err != nil {
		return 0, fmt.Errorf("cannot save .env: %w; check .env, then run ./turing connect --rotate-key again", err)
	}
	return port, nil
}

// newClientKey makes a key in the format init.sh does: tk_ and 64 lowercase
// hex characters, from 32 random bytes.
func newClientKey() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "tk_" + hex.EncodeToString(raw[:]), nil
}
