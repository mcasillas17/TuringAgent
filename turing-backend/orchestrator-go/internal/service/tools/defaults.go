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

// pseudoSeedPolicies is the same idea for the orchestrator's own pseudo-servers,
// kept in a separate table because those tools are not served by a bundled MCP
// server and must not appear in LegacyPolicyDefaults — that list is the rollout
// fallback for a worker that reports no capabilities, and a worker cannot serve
// a tool the orchestrator dispatches for it.
//
// Reading the user's own memory is safe: it leaves the vault, and every
// conversation, exactly as it found it. memory.remember is deliberately absent,
// so it falls to the unknown default and stops to ask — writing a proposal into
// the user's vault is a change to what Turing believes about them.
var pseudoSeedPolicies = map[policyKey]Policy{
	{serverName: "memory", toolName: "memory.search"}: PolicySafe,
	{serverName: "memory", toolName: "memory.read"}:   PolicySafe,
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
	if policy, ok := pseudoSeedPolicies[policyKey{serverName: serverName, toolName: toolName}]; ok {
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
	case strings.HasPrefix(toolName, "github."):
		return "integrations", true
	case strings.HasPrefix(toolName, "memory."):
		return "memory", true
	}

	for key := range seedPolicies {
		if key.toolName == toolName {
			return key.serverName, true
		}
	}
	return "", false
}

// readOnlyTools names the tools whose failure the runtime may treat as
// recoverable, because nothing was changed by attempting them. It is a separate
// question from the policy: a user who raises memory.search to
// approval_required has changed when Turing may look, not what looking does.
var readOnlyTools = map[policyKey]bool{
	{serverName: "integrations", toolName: "github.list_issues"}: true,
	{serverName: "integrations", toolName: "github.get_issue"}:   true,
	{serverName: "integrations", toolName: "github.get_file"}:    true,
	{serverName: "memory", toolName: "memory.search"}:            true,
	{serverName: "memory", toolName: "memory.read"}:              true,
}

func ToolReadOnly(serverName, toolName string) bool {
	return readOnlyTools[policyKey{serverName: serverName, toolName: toolName}]
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
