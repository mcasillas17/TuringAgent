package tools

import (
	"context"
	"errors"
	"testing"
)

func TestGetPolicyUsesStoredPolicyBeforeSeedFallback(t *testing.T) {
	lookup := stubPolicyLookup{policy: "approval_required", enabled: true, found: true}
	got, ok, err := GetPolicy(context.Background(), lookup, "system", "system.time")
	if err != nil || !ok || got != PolicyApprovalRequired {
		t.Fatalf("GetPolicy = %q/%v/%v, want approval_required/true/nil", got, ok, err)
	}
}

func TestGetPolicyFallsBackOnlyForKnownLegacyTools(t *testing.T) {
	lookup := stubPolicyLookup{}
	got, ok, err := GetPolicy(context.Background(), lookup, "system", "system.time")
	if err != nil || !ok || got != PolicySafe {
		t.Fatalf("known fallback = %q/%v/%v, want safe/true/nil", got, ok, err)
	}
	if got, ok, err := GetPolicy(context.Background(), lookup, "custom", "custom.unknown"); err != nil || ok || got != "" {
		t.Fatalf("unknown fallback = %q/%v/%v, want empty/false/nil", got, ok, err)
	}
	if got, ok, err := GetPolicy(context.Background(), lookup, "untrusted", "system.time"); err != nil || ok || got != "" {
		t.Fatalf("cross-server fallback = %q/%v/%v, want empty/false/nil", got, ok, err)
	}
}

func TestGetPolicyDeniesDisabledOrUnavailableDiscoveredTools(t *testing.T) {
	for name, lookup := range map[string]stubPolicyLookup{
		"disabled row":       {policy: "safe", found: true},
		"initialized absent": {initialized: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok, err := GetPolicy(context.Background(), lookup, "system", "system.time")
			if err != nil || ok || got != "" {
				t.Fatalf("GetPolicy = %q/%v/%v, want empty/false/nil", got, ok, err)
			}
		})
	}
}

func TestGetPolicyKeepsPermanentlyDisabledToolsDisabled(t *testing.T) {
	lookup := stubPolicyLookup{policy: "safe", enabled: true, found: true}
	got, ok, err := GetPolicy(context.Background(), lookup, "files", "files.delete")
	if err != nil || !ok || got != PolicyDisabled {
		t.Fatalf("files.delete policy = %q/%v/%v, want disabled/true/nil", got, ok, err)
	}
	got, ok, err = GetPolicy(context.Background(), lookup, "untrusted", "files.delete")
	if err != nil || !ok || got != PolicySafe {
		t.Fatalf("cross-server files.delete policy = %q/%v/%v, want stored safe/true/nil", got, ok, err)
	}
}

func TestGetPolicyPropagatesRepositoryFailure(t *testing.T) {
	wantErr := errors.New("database unavailable")
	got, ok, err := GetPolicy(context.Background(), stubPolicyLookup{err: wantErr}, "system", "system.time")
	if !errors.Is(err, wantErr) || ok || got != "" {
		t.Fatalf("GetPolicy = %q/%v/%v, want empty/false/repository error", got, ok, err)
	}
}

type stubPolicyLookup struct {
	policy      string
	enabled     bool
	found       bool
	initialized bool
	err         error
}

func (s stubPolicyLookup) GetToolPolicy(context.Context, string, string) (string, bool, bool, error) {
	return s.policy, s.enabled, s.found, s.err
}

func (s stubPolicyLookup) ToolRegistryInitialized(context.Context) (bool, error) {
	return s.initialized, s.err
}
