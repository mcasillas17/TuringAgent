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
	if defaults.approvalTimeout() != 5*time.Second {
		t.Fatalf("default approval timeout = %v, want 5s", defaults.approvalTimeout())
	}
	if defaults.totalToolTimeout() != 30*time.Second {
		t.Fatalf("default total tool timeout = %v, want 30s", defaults.totalToolTimeout())
	}

	configured := WorkerConfig{
		ModelTimeout:     time.Second,
		ToolTimeout:      2 * time.Second,
		ApprovalTimeout:  3 * time.Second,
		TotalToolTimeout: 4 * time.Second,
	}
	if configured.modelTimeout() != time.Second ||
		configured.toolTimeout() != 2*time.Second ||
		configured.approvalTimeout() != 3*time.Second ||
		configured.totalToolTimeout() != 4*time.Second {
		t.Fatalf("configured timeouts = %v/%v/%v/%v, want 1s/2s/3s/4s",
			configured.modelTimeout(),
			configured.toolTimeout(),
			configured.approvalTimeout(),
			configured.totalToolTimeout(),
		)
	}
}
