import 'tool_descriptor.dart';

enum McpServerTier { unspecified, bundled, localContainer, remoteUrl }

enum McpServerLiveness { unspecified, unknown, up, down }

class McpServer {
  McpServer({
    required this.serverId,
    required this.name,
    required this.transport,
    required this.url,
    required this.tier,
    required this.enabled,
    required this.liveness,
    required this.statusMessage,
    required this.sandboxConfined,
    required List<ToolDescriptor> tools,
  }) : tools = List.unmodifiable(tools);

  final String serverId;
  final String name;
  final String transport;
  final String url;
  final McpServerTier tier;
  final bool enabled;
  final McpServerLiveness liveness;
  final String statusMessage;
  final bool sandboxConfined;
  final List<ToolDescriptor> tools;
}

class UnsupportedMcpServer {
  const UnsupportedMcpServer({required this.name, required this.reason});

  final String name;
  final String reason;
}

/// The outcome of an on-demand mcp.json re-import. Servers listed as
/// unsupported were refused with a reason; imported servers still arrive
/// disabled — importing is not enabling.
class McpReimportReport {
  McpReimportReport({
    required List<String> imported,
    required List<UnsupportedMcpServer> unsupported,
  }) : imported = List.unmodifiable(imported),
       unsupported = List.unmodifiable(unsupported);

  final List<String> imported;
  final List<UnsupportedMcpServer> unsupported;
}

class McpRegistrySnapshot {
  McpRegistrySnapshot({
    required List<McpServer> servers,
    required List<UnsupportedMcpServer> unsupported,
  }) : servers = List.unmodifiable(servers),
       unsupported = List.unmodifiable(unsupported);

  final List<McpServer> servers;
  final List<UnsupportedMcpServer> unsupported;
}
