package tools

var seedPolicies = map[string]Policy{
	"system.health": PolicySafe,
	"system.time":   PolicySafe,
	"system.echo":   PolicySafe,
	"system.info":   PolicySafe,
	"files.list":    PolicySafe,
	"files.search":  PolicySafe,
	"files.read":    PolicySafe,
	"files.create":  PolicyApprovalRequired,
	"files.update":  PolicyApprovalRequired,
}

// DefaultPolicyFor assigns an orchestrator-owned policy to a tool when it is
// first discovered. Unknown tools require approval and are never assumed safe.
func DefaultPolicyFor(toolName string) Policy {
	switch toolName {
	case "files.delete", "files.move":
		return PolicyDisabled
	}
	if policy, ok := seedPolicies[toolName]; ok {
		return policy
	}
	return PolicyApprovalRequired
}
