// This is a generated file - do not edit.
//
// Generated from turing/v1/events.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:async' as $async;
import 'dart:core' as $core;

import 'package:grpc/service_api.dart' as $grpc;
import 'package:protobuf/protobuf.dart' as $pb;

import 'events.pb.dart' as $0;

export 'events.pb.dart';

@$pb.GrpcServiceName('turing.v1.EventService')
class EventServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  EventServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.ListEventsResponse> listEvents(
    $0.ListEventsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listEvents, request, options: options);
  }

  $grpc.ResponseStream<$0.TuringEvent> subscribeSessionEvents(
    $0.SubscribeSessionEventsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createStreamingCall(
        _$subscribeSessionEvents, $async.Stream.fromIterable([request]),
        options: options);
  }

  $grpc.ResponseStream<$0.TuringEvent> subscribeSessionUpdates(
    $0.SubscribeSessionUpdatesRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createStreamingCall(
        _$subscribeSessionUpdates, $async.Stream.fromIterable([request]),
        options: options);
  }

  // method descriptors

  static final _$listEvents =
      $grpc.ClientMethod<$0.ListEventsRequest, $0.ListEventsResponse>(
          '/turing.v1.EventService/ListEvents',
          ($0.ListEventsRequest value) => value.writeToBuffer(),
          $0.ListEventsResponse.fromBuffer);
  static final _$subscribeSessionEvents =
      $grpc.ClientMethod<$0.SubscribeSessionEventsRequest, $0.TuringEvent>(
          '/turing.v1.EventService/SubscribeSessionEvents',
          ($0.SubscribeSessionEventsRequest value) => value.writeToBuffer(),
          $0.TuringEvent.fromBuffer);
  static final _$subscribeSessionUpdates =
      $grpc.ClientMethod<$0.SubscribeSessionUpdatesRequest, $0.TuringEvent>(
          '/turing.v1.EventService/SubscribeSessionUpdates',
          ($0.SubscribeSessionUpdatesRequest value) => value.writeToBuffer(),
          $0.TuringEvent.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.EventService')
abstract class EventServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.EventService';

  EventServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.ListEventsRequest, $0.ListEventsResponse>(
        'ListEvents',
        listEvents_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.ListEventsRequest.fromBuffer(value),
        ($0.ListEventsResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.SubscribeSessionEventsRequest, $0.TuringEvent>(
            'SubscribeSessionEvents',
            subscribeSessionEvents_Pre,
            false,
            true,
            ($core.List<$core.int> value) =>
                $0.SubscribeSessionEventsRequest.fromBuffer(value),
            ($0.TuringEvent value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.SubscribeSessionUpdatesRequest, $0.TuringEvent>(
            'SubscribeSessionUpdates',
            subscribeSessionUpdates_Pre,
            false,
            true,
            ($core.List<$core.int> value) =>
                $0.SubscribeSessionUpdatesRequest.fromBuffer(value),
            ($0.TuringEvent value) => value.writeToBuffer()));
  }

  $async.Future<$0.ListEventsResponse> listEvents_Pre($grpc.ServiceCall $call,
      $async.Future<$0.ListEventsRequest> $request) async {
    return listEvents($call, await $request);
  }

  $async.Future<$0.ListEventsResponse> listEvents(
      $grpc.ServiceCall call, $0.ListEventsRequest request);

  $async.Stream<$0.TuringEvent> subscribeSessionEvents_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.SubscribeSessionEventsRequest> $request) async* {
    yield* subscribeSessionEvents($call, await $request);
  }

  $async.Stream<$0.TuringEvent> subscribeSessionEvents(
      $grpc.ServiceCall call, $0.SubscribeSessionEventsRequest request);

  $async.Stream<$0.TuringEvent> subscribeSessionUpdates_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.SubscribeSessionUpdatesRequest> $request) async* {
    yield* subscribeSessionUpdates($call, await $request);
  }

  $async.Stream<$0.TuringEvent> subscribeSessionUpdates(
      $grpc.ServiceCall call, $0.SubscribeSessionUpdatesRequest request);
}
