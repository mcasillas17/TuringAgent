import 'dart:async';

import 'package:fixnum/fixnum.dart';
import 'package:grpc/grpc.dart' as grpc;

import '../generated/turing/v1/events.pb.dart' as eventpb;
import '../generated/turing/v1/events.pbgrpc.dart' as eventgrpc;
import '../models/grpc_mappers.dart';
import '../models/turing_event.dart';
import 'grpc_client.dart';
import 'event_source.dart';

class TuringGrpcEventSource
    implements TuringEventSource, TuringSessionUpdateSource {
  TuringGrpcEventSource({
    required this.baseUrl,
    required this.apiKey,
    grpc.ClientChannel? channel,
  }) : _channel = channel ?? createTuringGrpcChannel(baseUrl),
       _ownsChannel = channel == null {
    _events = eventgrpc.EventServiceClient(
      _channel,
      options: grpc.CallOptions(
        metadata: GrpcAuthMetadata(apiKey: apiKey).headers(),
      ),
    );
  }

  final String baseUrl;
  final String apiKey;
  final grpc.ClientChannel _channel;
  final bool _ownsChannel;
  late final eventgrpc.EventServiceClient _events;

  @override
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) {
    return _events
        .subscribeSessionEvents(
          eventpb.SubscribeSessionEventsRequest(
            sessionId: sessionId,
            afterSequence: Int64(lastSequence ?? 0),
          ),
        )
        .map(GrpcMappers.turingEventToTuringEvent);
  }

  @override
  Stream<TuringEvent> connectSessionUpdates() {
    return _events
        .subscribeSessionUpdates(eventpb.SubscribeSessionUpdatesRequest())
        .map(GrpcMappers.turingEventToTuringEvent);
  }

  @override
  void close() {
    if (_ownsChannel) {
      unawaited(_channel.shutdown());
    }
  }
}
