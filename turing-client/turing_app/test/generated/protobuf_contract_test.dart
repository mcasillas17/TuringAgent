import 'package:flutter_test/flutter_test.dart';
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
}
