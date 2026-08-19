import 'dart:async';

import '../models/turing_event.dart';

abstract class TuringEventSource {
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence});

  void close();
}

abstract class TuringSessionUpdateSource {
  Stream<TuringEvent> connectSessionUpdates();

  void close();
}
