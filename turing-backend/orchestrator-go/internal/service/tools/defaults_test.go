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

// TestMemoryToolPolicySeedsArrive pins what a memory tool is allowed to do
// before anyone has touched a setting: reading the user's own memory is safe,
// and writing a proposal into their vault stops and asks.
func TestMemoryToolPolicySeedsArrive(t *testing.T) {
	for tool, want := range map[string]Policy{
		"memory.search":   PolicySafe,
		"memory.read":     PolicySafe,
		"memory.remember": PolicyApprovalRequired,
	} {
		if got := DefaultPolicyFor("memory", tool); got != want {
			t.Fatalf("DefaultPolicyFor(memory, %s) = %q, want %q", tool, got, want)
		}
		if owner, bundled := BundledServerForTool(tool); !bundled || owner != "memory" {
			t.Fatalf("BundledServerForTool(%s) = %q,%v, want memory,true", tool, owner, bundled)
		}
	}
	// read_only is what lets the runtime call a failed read a recoverable tool
	// error rather than a side effect it has to assume happened. It is not the
	// same question as the policy, and stays true even if the user raises
	// search or read to approval_required.
	for tool, want := range map[string]bool{
		"memory.search":   true,
		"memory.read":     true,
		"memory.remember": false,
	} {
		if got := ToolReadOnly("memory", tool); got != want {
			t.Fatalf("ToolReadOnly(memory, %s) = %v, want %v", tool, got, want)
		}
	}
	// memory.remember is not in the bundled seed table, so raising or lowering
	// it is the user's call rather than something the registry refuses.
	if BundledToolRequiresApproval("memory", "memory.remember") {
		t.Fatal("memory.remember is pinned as a bundled approval tool; its policy is the user's to set")
	}
}
