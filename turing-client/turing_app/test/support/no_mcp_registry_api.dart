import 'package:turing_flutter_app/models/mcp_server.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';

mixin NoMcpRegistryApi {
  Future<McpRegistrySnapshot> listMcpServers() async =>
      McpRegistrySnapshot(servers: const [], unsupported: const []);

  Future<McpServer> setMcpServerEnabled({
    required String serverId,
    required bool enabled,
  }) async => throw UnimplementedError(
    'this test does not exercise MCP server management',
  );

  Future<ToolDescriptor> updateMcpToolPolicy({
    required String serverId,
    required String toolName,
    required ToolPolicy policy,
  }) async => throw UnimplementedError(
    'this test does not exercise MCP tool policy management',
  );

  Future<void> deleteMcpServer({required String serverId}) async =>
      throw UnimplementedError(
        'this test does not exercise MCP server deletion',
      );

  Future<McpServer> registerMcpServer({
    required String name,
    required String url,
    required McpServerTier tier,
    String bearerToken = '',
  }) async => throw UnimplementedError(
    'this test does not exercise MCP server registration',
  );

  Future<McpImportReport> reimportMcpJson() async =>
      throw UnimplementedError('this test does not exercise MCP JSON reimport');

  Future<McpServer> rotateMcpServerToken({
    required String serverId,
    required String bearerToken,
  }) async => throw UnimplementedError(
    'this test does not exercise MCP server token rotation',
  );
}
