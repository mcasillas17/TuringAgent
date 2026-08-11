// This is a generated file - do not edit.
//
// Generated from turing/v1/chat.proto.

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

import 'chat.pb.dart' as $0;

export 'chat.pb.dart';

@$pb.GrpcServiceName('turing.v1.ChatService')
class ChatServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  ChatServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseStream<$0.ChatStreamEvent> sendMessage(
    $0.SendMessageRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createStreamingCall(
        _$sendMessage, $async.Stream.fromIterable([request]),
        options: options);
  }

  // method descriptors

  static final _$sendMessage =
      $grpc.ClientMethod<$0.SendMessageRequest, $0.ChatStreamEvent>(
          '/turing.v1.ChatService/SendMessage',
          ($0.SendMessageRequest value) => value.writeToBuffer(),
          $0.ChatStreamEvent.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.ChatService')
abstract class ChatServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.ChatService';

  ChatServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.SendMessageRequest, $0.ChatStreamEvent>(
        'SendMessage',
        sendMessage_Pre,
        false,
        true,
        ($core.List<$core.int> value) =>
            $0.SendMessageRequest.fromBuffer(value),
        ($0.ChatStreamEvent value) => value.writeToBuffer()));
  }

  $async.Stream<$0.ChatStreamEvent> sendMessage_Pre($grpc.ServiceCall $call,
      $async.Future<$0.SendMessageRequest> $request) async* {
    yield* sendMessage($call, await $request);
  }

  $async.Stream<$0.ChatStreamEvent> sendMessage(
      $grpc.ServiceCall call, $0.SendMessageRequest request);
}
