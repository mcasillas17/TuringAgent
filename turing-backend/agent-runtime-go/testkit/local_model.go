package testkit

import "github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/config"

// ResolveLocalModel is the local model and context window the runtime reads
// from env, for tests outside the module that hold it to the orchestrator.
func ResolveLocalModel(env map[string]string) (string, int, error) {
	cfg, err := config.LoadFromEnv(func(key string) string { return env[key] })
	return cfg.LocalModel, cfg.LocalContextWindowTokens, err
}
