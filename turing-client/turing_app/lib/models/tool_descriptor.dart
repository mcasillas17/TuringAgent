/// What the agent is allowed to do with a tool without asking you first.
enum ToolPolicy {
  /// The backend sent a policy this build does not know about. Shown as
  /// unknown rather than quietly assumed safe.
  unspecified,

  /// Runs without interrupting you.
  safe,

  /// Pauses the run and waits for you to approve the specific call.
  approvalRequired,

  /// Discovered but switched off; the agent cannot call it.
  disabled,
}

/// One tool exposed by one MCP server, as the backend currently sees it.
class ToolDescriptor {
  const ToolDescriptor({
    required this.serverName,
    required this.toolName,
    required this.policy,
  });

  final String serverName;
  final String toolName;
  final ToolPolicy policy;
}
