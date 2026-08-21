import 'package:turing_flutter_app/models/remote_egress.dart';
import 'package:turing_flutter_app/networking/api_client.dart';

mixin NoRemoteEgressApi {
  Future<RemoteEgressDisclosure?> prepareRemoteEgress({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    required String idempotencyKey,
  }) async {
    if (modelProvider == 'ollama') return null;
    throw const TuringApiException(
      code: 'remote_egress_unsupported',
      message: 'This client cannot prepare remote egress consent',
    );
  }

  Future<Map<String, dynamic>> sendMessageWithRemoteEgressConsent({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    required String idempotencyKey,
    required RemoteEgressConsent consent,
  }) {
    throw const TuringApiException(
      code: 'remote_egress_unsupported',
      message: 'This client cannot send remote egress consent',
    );
  }
}
