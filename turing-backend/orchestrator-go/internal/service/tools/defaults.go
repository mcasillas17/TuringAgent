package tools

import "sort"

type policyKey struct {
	serverName string
	toolName   string
}

type PolicyDefault struct {
	ServerName string
	ToolName   string
	Policy     Policy
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

func LegacyPolicyDefaults() []PolicyDefault {
	defaults := make([]PolicyDefault, 0, len(seedPolicies))
	for key, policy := range seedPolicies {
		defaults = append(defaults, PolicyDefault{ServerName: key.serverName, ToolName: key.toolName, Policy: policy})
	}
	sort.Slice(defaults, func(i int, j int) bool {
		if defaults[i].ServerName == defaults[j].ServerName {
			return defaults[i].ToolName < defaults[j].ToolName
		}
		return defaults[i].ServerName < defaults[j].ServerName
	})
	return defaults
}
