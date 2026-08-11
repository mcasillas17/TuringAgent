package tools

import "testing"

func TestDefaultPolicyForDiscoveredToolIsDenyBiased(t *testing.T) {
	tests := []struct {
		serverName string
		toolName   string
		want       Policy
	}{
		{serverName: "system", toolName: "system.time", want: PolicySafe},
		{serverName: "system", toolName: "system.info", want: PolicySafe},
		{serverName: "files", toolName: "files.read", want: PolicySafe},
		{serverName: "files", toolName: "files.create", want: PolicyApprovalRequired},
		{serverName: "files", toolName: "files.delete", want: PolicyDisabled},
		{serverName: "files", toolName: "files.move", want: PolicyDisabled},
		{serverName: "custom", toolName: "brand.new.tool", want: PolicyApprovalRequired},
		{serverName: "untrusted", toolName: "system.time", want: PolicyApprovalRequired},
		{serverName: "untrusted", toolName: "files.delete", want: PolicyApprovalRequired},
	}
	for _, test := range tests {
		t.Run(test.serverName+"/"+test.toolName, func(t *testing.T) {
			if got := DefaultPolicyFor(test.serverName, test.toolName); got != test.want {
				t.Fatalf("DefaultPolicyFor(%q, %q) = %q, want %q", test.serverName, test.toolName, got, test.want)
			}
		})
	}
}
