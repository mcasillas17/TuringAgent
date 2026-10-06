package tools

import (
	"context"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

type Policy string

const (
	PolicySafe             Policy = "safe"
	PolicyApprovalRequired Policy = "approval_required"
	PolicyDisabled         Policy = "disabled"
)

// ProtoFor is a stored policy as a tool descriptor reports it. Anything that
// is not safe or disabled is reported as needing approval, the fail-closed
// reading.
func ProtoFor(policy string) turingv1.ToolPolicy {
	switch Policy(policy) {
	case PolicySafe:
		return turingv1.ToolPolicy_TOOL_POLICY_SAFE
	case PolicyDisabled:
		return turingv1.ToolPolicy_TOOL_POLICY_DISABLED
	default:
		return turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED
	}
}

type PolicyLookup interface {
	GetToolPolicy(ctx context.Context, serverName string, toolName string) (policy string, enabled bool, found bool, err error)
	ToolRegistryInitialized(ctx context.Context) (bool, error)
}

func GetPolicy(ctx context.Context, lookup PolicyLookup, serverName string, toolName string) (Policy, bool, error) {
	if permanentlyDisabled(serverName, toolName) {
		return PolicyDisabled, true, nil
	}
	if lookup != nil {
		policy, enabled, found, err := lookup.GetToolPolicy(ctx, serverName, toolName)
		if err != nil {
			return "", false, err
		}
		if found && enabled {
			return Policy(policy), true, nil
		}
		if found {
			return "", false, nil
		}
		initialized, err := lookup.ToolRegistryInitialized(ctx)
		if err != nil {
			return "", false, err
		}
		if initialized {
			return "", false, nil
		}
	}
	policy, ok := seedPolicies[policyKey{serverName: serverName, toolName: toolName}]
	return policy, ok, nil
}
