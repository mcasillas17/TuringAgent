package testkit

import "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"

// ResolveLocalModel is the local model and context window the orchestrator
// reads from env, for tests outside the module that hold it to the runtime.
func ResolveLocalModel(env map[string]string) (string, int, error) {
	cfg, err := config.LoadFromMap(env)
	return cfg.LocalModel, cfg.LocalContextWindowTokens, err
}
