package tools

import "context"

type Policy string

const (
	PolicySafe             Policy = "safe"
	PolicyApprovalRequired Policy = "approval_required"
	PolicyDisabled         Policy = "disabled"
)

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
