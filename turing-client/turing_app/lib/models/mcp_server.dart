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

class McpRegistrySnapshot {
  McpRegistrySnapshot({
    required List<McpServer> servers,
    required List<UnsupportedMcpServer> unsupported,
  }) : servers = List.unmodifiable(servers),
       unsupported = List.unmodifiable(unsupported);

  final List<McpServer> servers;
  final List<UnsupportedMcpServer> unsupported;
}

/// The outcome of reimporting the backend's mcp.json configuration: which
/// servers were newly registered, which were already present and left alone,
/// and which were refused, with the reason each was refused.
class McpImportReport {
  McpImportReport({
    required List<String> imported,
    required List<String> skipped,
    required List<UnsupportedMcpServer> refused,
  }) : imported = List.unmodifiable(imported),
       skipped = List.unmodifiable(skipped),
       refused = List.unmodifiable(refused);

  final List<String> imported;
  final List<String> skipped;
  final List<UnsupportedMcpServer> refused;
}
