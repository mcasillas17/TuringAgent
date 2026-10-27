package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// ServiceState is what `docker compose ps` reports for one service. Health is
// empty for a service that defines no health check.
type ServiceState struct {
	Service, State, Health string
}

// Ps lists every container Compose knows, stopped ones included. Compose
// prints JSON Lines, or on older versions one JSON array; both are read.
func Ps(ctx context.Context, c Compose) ([]ServiceState, error) {
	stdout, stderr, code, err := c.Output(ctx, nil, "ps", "--all", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("could not run compose ps: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("compose ps exited %d: %s", code, lastLines(stderr, 5))
	}
	trimmed := bytes.TrimSpace([]byte(stdout))
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var states []ServiceState
		if err := json.Unmarshal(trimmed, &states); err != nil {
			return nil, fmt.Errorf("could not read compose ps output: %w", err)
		}
		return states, nil
	}
	var states []ServiceState
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	for {
		var state ServiceState
		err := decoder.Decode(&state)
		if err == io.EOF {
			return states, nil
		}
		if err != nil {
			return nil, fmt.Errorf("could not read compose ps output: %w", err)
		}
		states = append(states, state)
	}
}
