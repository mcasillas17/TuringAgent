import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/generated/turing/v1/mcp.pb.dart';
import 'package:turing_flutter_app/generated/turing/v1/sessions.pb.dart';
import 'package:turing_flutter_app/generated/turing/v1/tools.pb.dart';

void main() {
  test('ToolCallBeacon preserves model tool call ID', () {
    final beacon = ToolCallBeacon(modelToolCallId: 'model-call-1');

    final decoded = ToolCallBeacon.fromBuffer(beacon.writeToBuffer());

    expect(decoded.modelToolCallId, 'model-call-1');
    expect(decoded.hasModelToolCallId(), isTrue);
  });

  test('ListMessagesRequest preserves causal history anchor', () {
    final request = ListMessagesRequest(beforeMessageId: 'msg-current');

    final decoded = ListMessagesRequest.fromBuffer(request.writeToBuffer());

    expect(decoded.beforeMessageId, 'msg-current');
    expect(decoded.hasBeforeMessageId(), isTrue);
  });

  test('RegisterMcpServerRequest preserves registration fields', () {
    final request = RegisterMcpServerRequest(
      name: 'vendor',
      url: 'https://vendor.example.com/mcp',
      tier: McpServerTier.MCP_SERVER_TIER_REMOTE_URL,
      bearerToken: 'secret-token',
    );

    final decoded = RegisterMcpServerRequest.fromBuffer(request.writeToBuffer());

    expect(decoded.name, 'vendor');
    expect(decoded.url, 'https://vendor.example.com/mcp');
    expect(decoded.tier, McpServerTier.MCP_SERVER_TIER_REMOTE_URL);
    expect(decoded.bearerToken, 'secret-token');
  });

  test('RegisterMcpServer response is an McpServerDescriptor without a token', () {
    final descriptor = McpServerDescriptor(serverId: 'server-1', name: 'vendor');

    final decoded = McpServerDescriptor.fromBuffer(descriptor.writeToBuffer());

    expect(decoded.serverId, 'server-1');
    expect(decoded.name, 'vendor');
  });

  test('ReimportMcpJsonResponse preserves imported, skipped, and refused lists', () {
    final response = ReimportMcpJsonResponse(
      imported: ['vendor-a'],
      skipped: ['vendor-b'],
      unsupported: [UnsupportedMcpServer(name: 'vendor-c', reason: 'unsupported transport')],
    );

    final decoded = ReimportMcpJsonResponse.fromBuffer(response.writeToBuffer());

    expect(decoded.imported, ['vendor-a']);
    expect(decoded.skipped, ['vendor-b']);
    expect(decoded.unsupported, hasLength(1));
    expect(decoded.unsupported.single.name, 'vendor-c');
    expect(decoded.unsupported.single.reason, 'unsupported transport');
  });

  test('ReimportMcpJsonRequest is empty', () {
    final request = ReimportMcpJsonRequest();

    final decoded = ReimportMcpJsonRequest.fromBuffer(request.writeToBuffer());

    expect(decoded, isNotNull);
  });

  test('RotateMcpServerTokenRequest preserves server id and bearer token', () {
    final request = RotateMcpServerTokenRequest(
      serverId: 'server-1',
      bearerToken: 'new-secret-token',
    );

    final decoded = RotateMcpServerTokenRequest.fromBuffer(request.writeToBuffer());

    expect(decoded.serverId, 'server-1');
    expect(decoded.bearerToken, 'new-secret-token');
  });
}
