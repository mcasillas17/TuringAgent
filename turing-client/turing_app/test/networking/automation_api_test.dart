import 'package:fixnum/fixnum.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grpc/grpc.dart' as grpc;
import 'package:turing_flutter_app/generated/turing/v1/automations.pbgrpc.dart'
    as automationgrpc;
import 'package:turing_flutter_app/networking/grpc_client.dart';

void main() {
  Future<TuringGrpcApi> connect(grpc.Service service) async {
    final server = grpc.Server.create(services: [service]);
    await server.serve(address: '127.0.0.1', port: 0);
    final channel = grpc.ClientChannel(
      '127.0.0.1',
      port: server.port!,
      options: const grpc.ChannelOptions(
        credentials: grpc.ChannelCredentials.insecure(),
      ),
    );
    addTearDown(() async {
      await channel.shutdown();
      await server.shutdown();
    });
    return TuringGrpcApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      apiKey: 'client-key',
      channel: channel,
    );
  }

  test('getUnattendedQuiet maps the setting and its revision', () async {
    final service = _QuietService()
      ..current = automationgrpc.UnattendedQuiet(
        quiet: true,
        revision: Int64(4),
        unavailable: true,
      );
    final api = await connect(service);

    final setting = await api.getUnattendedQuiet();

    expect(setting.quiet, isTrue);
    expect(setting.revision, 4);
    expect(setting.unavailable, isTrue);
  });

  // The revision is the whole of the compare-and-set: a client that dropped
  // it would have every write after the first refused as stale.
  test('setUnattendedQuiet names the revision it read and maps a stale '
      'answer', () async {
    final service = _QuietService()
      ..current = automationgrpc.UnattendedQuiet(
        quiet: true,
        revision: Int64(7),
        unavailable: true,
      );
    final api = await connect(service);

    final write = await api.setUnattendedQuiet(quiet: true, revision: 4);

    final request = service.writes.single;
    expect(request.quiet, isTrue);
    expect(request.revision, Int64(4));
    expect(write.stale, isTrue);
    expect(write.setting.quiet, isTrue);
    expect(write.setting.revision, 7);
    expect(write.setting.unavailable, isTrue);
  });
}

/// Answers every write as stale with [current], which is all these tests need
/// of the backend's compare-and-set.
class _QuietService extends automationgrpc.AutomationServiceBase {
  automationgrpc.UnattendedQuiet current = automationgrpc.UnattendedQuiet();
  final writes = <automationgrpc.SetUnattendedQuietRequest>[];

  @override
  Future<automationgrpc.UnattendedQuiet> getUnattendedQuiet(
    grpc.ServiceCall call,
    automationgrpc.GetUnattendedQuietRequest request,
  ) async => current;

  @override
  Future<automationgrpc.SetUnattendedQuietResponse> setUnattendedQuiet(
    grpc.ServiceCall call,
    automationgrpc.SetUnattendedQuietRequest request,
  ) async {
    writes.add(request);
    return automationgrpc.SetUnattendedQuietResponse(
      setting: current,
      stale: true,
    );
  }

  @override
  Future<automationgrpc.Automation> createAutomation(
    grpc.ServiceCall call,
    automationgrpc.CreateAutomationRequest request,
  ) => throw UnimplementedError();

  @override
  Future<automationgrpc.Automation> updateAutomation(
    grpc.ServiceCall call,
    automationgrpc.UpdateAutomationRequest request,
  ) => throw UnimplementedError();

  @override
  Future<automationgrpc.Automation> setAutomationEnabled(
    grpc.ServiceCall call,
    automationgrpc.SetAutomationEnabledRequest request,
  ) => throw UnimplementedError();

  @override
  Future<automationgrpc.DeleteAutomationResponse> deleteAutomation(
    grpc.ServiceCall call,
    automationgrpc.DeleteAutomationRequest request,
  ) => throw UnimplementedError();

  @override
  Future<automationgrpc.ListAutomationsResponse> listAutomations(
    grpc.ServiceCall call,
    automationgrpc.ListAutomationsRequest request,
  ) => throw UnimplementedError();
}
