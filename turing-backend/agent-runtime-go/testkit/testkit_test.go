package testkit

import (
	"testing"
	"time"
)

func TestWorkerConfigTimeoutsDefaultAndAllowOverrides(t *testing.T) {
	defaults := WorkerConfig{}
	if defaults.modelTimeout() != 120*time.Second {
		t.Fatalf("default model timeout = %v, want 120s", defaults.modelTimeout())
	}
	if defaults.toolTimeout() != 30*time.Second {
		t.Fatalf("default tool timeout = %v, want 30s", defaults.toolTimeout())
	}

	configured := WorkerConfig{ModelTimeout: time.Second, ToolTimeout: 2 * time.Second}
	if configured.modelTimeout() != time.Second || configured.toolTimeout() != 2*time.Second {
		t.Fatalf("configured timeouts = %v/%v, want 1s/2s", configured.modelTimeout(), configured.toolTimeout())
	}
}
