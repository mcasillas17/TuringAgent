package tools

type policyKey struct {
	serverName string
	toolName   string
}

var seedPolicies = map[policyKey]Policy{
	{serverName: "system", toolName: "system.health"}: PolicySafe,
	{serverName: "system", toolName: "system.time"}:   PolicySafe,
	{serverName: "system", toolName: "system.echo"}:   PolicySafe,
	{serverName: "system", toolName: "system.info"}:   PolicySafe,
	{serverName: "files", toolName: "files.list"}:     PolicySafe,
	{serverName: "files", toolName: "files.search"}:   PolicySafe,
	{serverName: "files", toolName: "files.read"}:     PolicySafe,
	{serverName: "files", toolName: "files.create"}:   PolicyApprovalRequired,
	{serverName: "files", toolName: "files.update"}:   PolicyApprovalRequired,
}

// DefaultPolicyFor assigns an orchestrator-owned policy to a tool when it is
// first discovered. Unknown tools require approval and are never assumed safe.
func DefaultPolicyFor(serverName string, toolName string) Policy {
	if permanentlyDisabled(serverName, toolName) {
		return PolicyDisabled
	}
	if policy, ok := seedPolicies[policyKey{serverName: serverName, toolName: toolName}]; ok {
		return policy
	}
	return PolicyApprovalRequired
}

func permanentlyDisabled(serverName string, toolName string) bool {
	return serverName == "files" && (toolName == "files.delete" || toolName == "files.move")
}
