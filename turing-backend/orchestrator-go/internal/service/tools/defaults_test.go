package tools

import "testing"

func TestDefaultPolicyForDiscoveredToolIsDenyBiased(t *testing.T) {
	tests := []struct {
		name string
		want Policy
	}{
		{name: "system.time", want: PolicySafe},
		{name: "system.info", want: PolicySafe},
		{name: "files.read", want: PolicySafe},
		{name: "files.create", want: PolicyApprovalRequired},
		{name: "files.delete", want: PolicyDisabled},
		{name: "files.move", want: PolicyDisabled},
		{name: "brand.new.tool", want: PolicyApprovalRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DefaultPolicyFor(test.name); got != test.want {
				t.Fatalf("DefaultPolicyFor(%q) = %q, want %q", test.name, got, test.want)
			}
		})
	}
}
