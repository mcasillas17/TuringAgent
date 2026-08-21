package tools

import (
	"sort"
	"strings"
)

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

func BundledServerForTool(toolName string) (string, bool) {
	switch {
	case strings.HasPrefix(toolName, "system."):
		return "system", true
	case strings.HasPrefix(toolName, "files."):
		return "files", true
	case toolName == "skills_list" || toolName == "skill_view":
		return "skills", true
	}

	for key := range seedPolicies {
		if key.toolName == toolName {
			return key.serverName, true
		}
	}
	return "", false
}

func BundledToolRequiresApproval(serverName string, toolName string) bool {
	return seedPolicies[policyKey{serverName: serverName, toolName: toolName}] == PolicyApprovalRequired
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
