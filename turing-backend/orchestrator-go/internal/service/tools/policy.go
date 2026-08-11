package tools

import "context"

type Policy string

const (
	PolicySafe             Policy = "safe"
	PolicyApprovalRequired Policy = "approval_required"
	PolicyDisabled         Policy = "disabled"
)

type PolicyLookup interface {
	GetToolPolicy(ctx context.Context, serverName string, toolName string) (string, bool, error)
}

func GetPolicy(ctx context.Context, lookup PolicyLookup, serverName string, toolName string) (Policy, bool, error) {
	if permanentlyDisabled(toolName) {
		return PolicyDisabled, true, nil
	}
	if lookup != nil {
		policy, ok, err := lookup.GetToolPolicy(ctx, serverName, toolName)
		if err != nil {
			return "", false, err
		}
		if ok {
			return Policy(policy), true, nil
		}
	}
	policy, ok := seedPolicies[toolName]
	return policy, ok, nil
}
