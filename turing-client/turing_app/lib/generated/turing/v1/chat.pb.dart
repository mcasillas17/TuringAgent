// This is a generated file - do not edit.
//
// Generated from turing/v1/chat.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import '../../google/protobuf/struct.pb.dart' as $2;
import 'common.pb.dart' as $1;
import 'events.pb.dart' as $3;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

class SendMessageRequest extends $pb.GeneratedMessage {
  factory SendMessageRequest({
    $core.String? sessionId,
    $core.String? content,
    $core.String? contentType,
    $1.AgentId? agentId,
    $1.ModelProvider? modelProvider,
    $core.String? model,
    $core.String? idempotencyKey,
    $core.Iterable<$core.String>? requestedTools,
    $core.int? requiredContextTokens,
    $core.int? minimumWorkerMaxConcurrentRuns,
  }) {
    final result = create();
    if (sessionId != null) result.sessionId = sessionId;
    if (content != null) result.content = content;
    if (contentType != null) result.contentType = contentType;
    if (agentId != null) result.agentId = agentId;
    if (modelProvider != null) result.modelProvider = modelProvider;
    if (model != null) result.model = model;
    if (idempotencyKey != null) result.idempotencyKey = idempotencyKey;
    if (requestedTools != null) result.requestedTools.addAll(requestedTools);
    if (requiredContextTokens != null)
      result.requiredContextTokens = requiredContextTokens;
    if (minimumWorkerMaxConcurrentRuns != null)
      result.minimumWorkerMaxConcurrentRuns = minimumWorkerMaxConcurrentRuns;
    return result;
  }

  SendMessageRequest._();

  factory SendMessageRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SendMessageRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SendMessageRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..aOS(2, _omitFieldNames ? '' : 'content')
    ..aOS(3, _omitFieldNames ? '' : 'contentType')
    ..e<$1.AgentId>(4, _omitFieldNames ? '' : 'agentId', $pb.PbFieldType.OE,
        defaultOrMaker: $1.AgentId.AGENT_ID_UNSPECIFIED,
        valueOf: $1.AgentId.valueOf,
        enumValues: $1.AgentId.values)
    ..e<$1.ModelProvider>(
        5, _omitFieldNames ? '' : 'modelProvider', $pb.PbFieldType.OE,
        defaultOrMaker: $1.ModelProvider.MODEL_PROVIDER_UNSPECIFIED,
        valueOf: $1.ModelProvider.valueOf,
        enumValues: $1.ModelProvider.values)
    ..aOS(6, _omitFieldNames ? '' : 'model')
    ..aOS(7, _omitFieldNames ? '' : 'idempotencyKey')
    ..pPS(8, _omitFieldNames ? '' : 'requestedTools')
    ..a<$core.int>(
        9, _omitFieldNames ? '' : 'requiredContextTokens', $pb.PbFieldType.O3)
    ..a<$core.int>(10, _omitFieldNames ? '' : 'minimumWorkerMaxConcurrentRuns',
        $pb.PbFieldType.O3)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SendMessageRequest clone() => SendMessageRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SendMessageRequest copyWith(void Function(SendMessageRequest) updates) =>
      super.copyWith((message) => updates(message as SendMessageRequest))
          as SendMessageRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SendMessageRequest create() => SendMessageRequest._();
  @$core.override
  SendMessageRequest createEmptyInstance() => create();
  static $pb.PbList<SendMessageRequest> createRepeated() =>
      $pb.PbList<SendMessageRequest>();
  @$core.pragma('dart2js:noInline')
  static SendMessageRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SendMessageRequest>(create);
  static SendMessageRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get content => $_getSZ(1);
  @$pb.TagNumber(2)
  set content($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasContent() => $_has(1);
  @$pb.TagNumber(2)
  void clearContent() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get contentType => $_getSZ(2);
  @$pb.TagNumber(3)
  set contentType($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasContentType() => $_has(2);
  @$pb.TagNumber(3)
  void clearContentType() => $_clearField(3);

  @$pb.TagNumber(4)
  $1.AgentId get agentId => $_getN(3);
  @$pb.TagNumber(4)
  set agentId($1.AgentId value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasAgentId() => $_has(3);
  @$pb.TagNumber(4)
  void clearAgentId() => $_clearField(4);

  @$pb.TagNumber(5)
  $1.ModelProvider get modelProvider => $_getN(4);
  @$pb.TagNumber(5)
  set modelProvider($1.ModelProvider value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasModelProvider() => $_has(4);
  @$pb.TagNumber(5)
  void clearModelProvider() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get model => $_getSZ(5);
  @$pb.TagNumber(6)
  set model($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasModel() => $_has(5);
  @$pb.TagNumber(6)
  void clearModel() => $_clearField(6);

  /// Opaque key retained for retries of this exact operation. Reusing a key
  /// with a different normalized request returns ALREADY_EXISTS.
  @$pb.TagNumber(7)
  $core.String get idempotencyKey => $_getSZ(6);
  @$pb.TagNumber(7)
  set idempotencyKey($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasIdempotencyKey() => $_has(6);
  @$pb.TagNumber(7)
  void clearIdempotencyKey() => $_clearField(7);

  /// Exact server/tool names that a compatible worker must advertise.
  @$pb.TagNumber(8)
  $pb.PbList<$core.String> get requestedTools => $_getList(7);

  @$pb.TagNumber(9)
  $core.int get requiredContextTokens => $_getIZ(8);
  @$pb.TagNumber(9)
  set requiredContextTokens($core.int value) => $_setSignedInt32(8, value);
  @$pb.TagNumber(9)
  $core.bool hasRequiredContextTokens() => $_has(8);
  @$pb.TagNumber(9)
  void clearRequiredContextTokens() => $_clearField(9);

  @$pb.TagNumber(10)
  $core.int get minimumWorkerMaxConcurrentRuns => $_getIZ(9);
  @$pb.TagNumber(10)
  set minimumWorkerMaxConcurrentRuns($core.int value) =>
      $_setSignedInt32(9, value);
  @$pb.TagNumber(10)
  $core.bool hasMinimumWorkerMaxConcurrentRuns() => $_has(9);
  @$pb.TagNumber(10)
  void clearMinimumWorkerMaxConcurrentRuns() => $_clearField(10);
}

class RunQueued extends $pb.GeneratedMessage {
  factory RunQueued({
    $core.String? runId,
    $core.String? jobId,
    $core.String? traceId,
    $1.RunState? runState,
  }) {
    final result = create();
    if (runId != null) result.runId = runId;
    if (jobId != null) result.jobId = jobId;
    if (traceId != null) result.traceId = traceId;
    if (runState != null) result.runState = runState;
    return result;
  }

  RunQueued._();

  factory RunQueued.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RunQueued.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RunQueued',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'runId')
    ..aOS(2, _omitFieldNames ? '' : 'jobId')
    ..aOS(3, _omitFieldNames ? '' : 'traceId')
    ..aOM<$1.RunState>(4, _omitFieldNames ? '' : 'runState',
        subBuilder: $1.RunState.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunQueued clone() => RunQueued()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunQueued copyWith(void Function(RunQueued) updates) =>
      super.copyWith((message) => updates(message as RunQueued)) as RunQueued;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RunQueued create() => RunQueued._();
  @$core.override
  RunQueued createEmptyInstance() => create();
  static $pb.PbList<RunQueued> createRepeated() => $pb.PbList<RunQueued>();
  @$core.pragma('dart2js:noInline')
  static RunQueued getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<RunQueued>(create);
  static RunQueued? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get runId => $_getSZ(0);
  @$pb.TagNumber(1)
  set runId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRunId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRunId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get jobId => $_getSZ(1);
  @$pb.TagNumber(2)
  set jobId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasJobId() => $_has(1);
  @$pb.TagNumber(2)
  void clearJobId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get traceId => $_getSZ(2);
  @$pb.TagNumber(3)
  set traceId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasTraceId() => $_has(2);
  @$pb.TagNumber(3)
  void clearTraceId() => $_clearField(3);

  @$pb.TagNumber(4)
  $1.RunState get runState => $_getN(3);
  @$pb.TagNumber(4)
  set runState($1.RunState value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasRunState() => $_has(3);
  @$pb.TagNumber(4)
  void clearRunState() => $_clearField(4);
  @$pb.TagNumber(4)
  $1.RunState ensureRunState() => $_ensure(3);
}

class RunStarted extends $pb.GeneratedMessage {
  factory RunStarted({
    $core.String? runId,
    $core.String? jobId,
    $core.int? attempt,
    $1.RunState? runState,
  }) {
    final result = create();
    if (runId != null) result.runId = runId;
    if (jobId != null) result.jobId = jobId;
    if (attempt != null) result.attempt = attempt;
    if (runState != null) result.runState = runState;
    return result;
  }

  RunStarted._();

  factory RunStarted.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RunStarted.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RunStarted',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'runId')
    ..aOS(2, _omitFieldNames ? '' : 'jobId')
    ..a<$core.int>(3, _omitFieldNames ? '' : 'attempt', $pb.PbFieldType.O3)
    ..aOM<$1.RunState>(4, _omitFieldNames ? '' : 'runState',
        subBuilder: $1.RunState.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunStarted clone() => RunStarted()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunStarted copyWith(void Function(RunStarted) updates) =>
      super.copyWith((message) => updates(message as RunStarted)) as RunStarted;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RunStarted create() => RunStarted._();
  @$core.override
  RunStarted createEmptyInstance() => create();
  static $pb.PbList<RunStarted> createRepeated() => $pb.PbList<RunStarted>();
  @$core.pragma('dart2js:noInline')
  static RunStarted getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RunStarted>(create);
  static RunStarted? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get runId => $_getSZ(0);
  @$pb.TagNumber(1)
  set runId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRunId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRunId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get jobId => $_getSZ(1);
  @$pb.TagNumber(2)
  set jobId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasJobId() => $_has(1);
  @$pb.TagNumber(2)
  void clearJobId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.int get attempt => $_getIZ(2);
  @$pb.TagNumber(3)
  set attempt($core.int value) => $_setSignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasAttempt() => $_has(2);
  @$pb.TagNumber(3)
  void clearAttempt() => $_clearField(3);

  @$pb.TagNumber(4)
  $1.RunState get runState => $_getN(3);
  @$pb.TagNumber(4)
  set runState($1.RunState value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasRunState() => $_has(3);
  @$pb.TagNumber(4)
  void clearRunState() => $_clearField(4);
  @$pb.TagNumber(4)
  $1.RunState ensureRunState() => $_ensure(3);
}

class MessageStarted extends $pb.GeneratedMessage {
  factory MessageStarted({
    $core.String? messageId,
    $1.MessageRole? role,
  }) {
    final result = create();
    if (messageId != null) result.messageId = messageId;
    if (role != null) result.role = role;
    return result;
  }

  MessageStarted._();

  factory MessageStarted.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory MessageStarted.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'MessageStarted',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'messageId')
    ..e<$1.MessageRole>(2, _omitFieldNames ? '' : 'role', $pb.PbFieldType.OE,
        defaultOrMaker: $1.MessageRole.MESSAGE_ROLE_UNSPECIFIED,
        valueOf: $1.MessageRole.valueOf,
        enumValues: $1.MessageRole.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MessageStarted clone() => MessageStarted()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MessageStarted copyWith(void Function(MessageStarted) updates) =>
      super.copyWith((message) => updates(message as MessageStarted))
          as MessageStarted;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static MessageStarted create() => MessageStarted._();
  @$core.override
  MessageStarted createEmptyInstance() => create();
  static $pb.PbList<MessageStarted> createRepeated() =>
      $pb.PbList<MessageStarted>();
  @$core.pragma('dart2js:noInline')
  static MessageStarted getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<MessageStarted>(create);
  static MessageStarted? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get messageId => $_getSZ(0);
  @$pb.TagNumber(1)
  set messageId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasMessageId() => $_has(0);
  @$pb.TagNumber(1)
  void clearMessageId() => $_clearField(1);

  @$pb.TagNumber(2)
  $1.MessageRole get role => $_getN(1);
  @$pb.TagNumber(2)
  set role($1.MessageRole value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasRole() => $_has(1);
  @$pb.TagNumber(2)
  void clearRole() => $_clearField(2);
}

class TokenDelta extends $pb.GeneratedMessage {
  factory TokenDelta({
    $core.String? messageId,
    $core.String? delta,
  }) {
    final result = create();
    if (messageId != null) result.messageId = messageId;
    if (delta != null) result.delta = delta;
    return result;
  }

  TokenDelta._();

  factory TokenDelta.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory TokenDelta.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'TokenDelta',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'messageId')
    ..aOS(2, _omitFieldNames ? '' : 'delta')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TokenDelta clone() => TokenDelta()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TokenDelta copyWith(void Function(TokenDelta) updates) =>
      super.copyWith((message) => updates(message as TokenDelta)) as TokenDelta;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static TokenDelta create() => TokenDelta._();
  @$core.override
  TokenDelta createEmptyInstance() => create();
  static $pb.PbList<TokenDelta> createRepeated() => $pb.PbList<TokenDelta>();
  @$core.pragma('dart2js:noInline')
  static TokenDelta getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<TokenDelta>(create);
  static TokenDelta? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get messageId => $_getSZ(0);
  @$pb.TagNumber(1)
  set messageId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasMessageId() => $_has(0);
  @$pb.TagNumber(1)
  void clearMessageId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get delta => $_getSZ(1);
  @$pb.TagNumber(2)
  set delta($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDelta() => $_has(1);
  @$pb.TagNumber(2)
  void clearDelta() => $_clearField(2);
}

class ToolEvent extends $pb.GeneratedMessage {
  factory ToolEvent({
    $core.String? toolCallId,
    $core.String? serverName,
    $core.String? toolName,
    $2.Struct? payload,
  }) {
    final result = create();
    if (toolCallId != null) result.toolCallId = toolCallId;
    if (serverName != null) result.serverName = serverName;
    if (toolName != null) result.toolName = toolName;
    if (payload != null) result.payload = payload;
    return result;
  }

  ToolEvent._();

  factory ToolEvent.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ToolEvent.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ToolEvent',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'toolCallId')
    ..aOS(2, _omitFieldNames ? '' : 'serverName')
    ..aOS(3, _omitFieldNames ? '' : 'toolName')
    ..aOM<$2.Struct>(4, _omitFieldNames ? '' : 'payload',
        subBuilder: $2.Struct.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ToolEvent clone() => ToolEvent()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ToolEvent copyWith(void Function(ToolEvent) updates) =>
      super.copyWith((message) => updates(message as ToolEvent)) as ToolEvent;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ToolEvent create() => ToolEvent._();
  @$core.override
  ToolEvent createEmptyInstance() => create();
  static $pb.PbList<ToolEvent> createRepeated() => $pb.PbList<ToolEvent>();
  @$core.pragma('dart2js:noInline')
  static ToolEvent getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ToolEvent>(create);
  static ToolEvent? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get toolCallId => $_getSZ(0);
  @$pb.TagNumber(1)
  set toolCallId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasToolCallId() => $_has(0);
  @$pb.TagNumber(1)
  void clearToolCallId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get serverName => $_getSZ(1);
  @$pb.TagNumber(2)
  set serverName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasServerName() => $_has(1);
  @$pb.TagNumber(2)
  void clearServerName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get toolName => $_getSZ(2);
  @$pb.TagNumber(3)
  set toolName($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasToolName() => $_has(2);
  @$pb.TagNumber(3)
  void clearToolName() => $_clearField(3);

  @$pb.TagNumber(4)
  $2.Struct get payload => $_getN(3);
  @$pb.TagNumber(4)
  set payload($2.Struct value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasPayload() => $_has(3);
  @$pb.TagNumber(4)
  void clearPayload() => $_clearField(4);
  @$pb.TagNumber(4)
  $2.Struct ensurePayload() => $_ensure(3);
}

class ApprovalEvent extends $pb.GeneratedMessage {
  factory ApprovalEvent({
    $core.String? approvalId,
    $core.String? toolName,
    $core.String? argsSummary,
    $1.RunState? runState,
  }) {
    final result = create();
    if (approvalId != null) result.approvalId = approvalId;
    if (toolName != null) result.toolName = toolName;
    if (argsSummary != null) result.argsSummary = argsSummary;
    if (runState != null) result.runState = runState;
    return result;
  }

  ApprovalEvent._();

  factory ApprovalEvent.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ApprovalEvent.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ApprovalEvent',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'approvalId')
    ..aOS(2, _omitFieldNames ? '' : 'toolName')
    ..aOS(3, _omitFieldNames ? '' : 'argsSummary')
    ..aOM<$1.RunState>(4, _omitFieldNames ? '' : 'runState',
        subBuilder: $1.RunState.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApprovalEvent clone() => ApprovalEvent()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApprovalEvent copyWith(void Function(ApprovalEvent) updates) =>
      super.copyWith((message) => updates(message as ApprovalEvent))
          as ApprovalEvent;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ApprovalEvent create() => ApprovalEvent._();
  @$core.override
  ApprovalEvent createEmptyInstance() => create();
  static $pb.PbList<ApprovalEvent> createRepeated() =>
      $pb.PbList<ApprovalEvent>();
  @$core.pragma('dart2js:noInline')
  static ApprovalEvent getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ApprovalEvent>(create);
  static ApprovalEvent? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get approvalId => $_getSZ(0);
  @$pb.TagNumber(1)
  set approvalId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasApprovalId() => $_has(0);
  @$pb.TagNumber(1)
  void clearApprovalId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get toolName => $_getSZ(1);
  @$pb.TagNumber(2)
  set toolName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasToolName() => $_has(1);
  @$pb.TagNumber(2)
  void clearToolName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get argsSummary => $_getSZ(2);
  @$pb.TagNumber(3)
  set argsSummary($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasArgsSummary() => $_has(2);
  @$pb.TagNumber(3)
  void clearArgsSummary() => $_clearField(3);

  @$pb.TagNumber(4)
  $1.RunState get runState => $_getN(3);
  @$pb.TagNumber(4)
  set runState($1.RunState value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasRunState() => $_has(3);
  @$pb.TagNumber(4)
  void clearRunState() => $_clearField(4);
  @$pb.TagNumber(4)
  $1.RunState ensureRunState() => $_ensure(3);
}

class MessageCompleted extends $pb.GeneratedMessage {
  factory MessageCompleted({
    $core.String? messageId,
    $core.String? content,
  }) {
    final result = create();
    if (messageId != null) result.messageId = messageId;
    if (content != null) result.content = content;
    return result;
  }

  MessageCompleted._();

  factory MessageCompleted.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory MessageCompleted.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'MessageCompleted',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'messageId')
    ..aOS(2, _omitFieldNames ? '' : 'content')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MessageCompleted clone() => MessageCompleted()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MessageCompleted copyWith(void Function(MessageCompleted) updates) =>
      super.copyWith((message) => updates(message as MessageCompleted))
          as MessageCompleted;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static MessageCompleted create() => MessageCompleted._();
  @$core.override
  MessageCompleted createEmptyInstance() => create();
  static $pb.PbList<MessageCompleted> createRepeated() =>
      $pb.PbList<MessageCompleted>();
  @$core.pragma('dart2js:noInline')
  static MessageCompleted getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<MessageCompleted>(create);
  static MessageCompleted? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get messageId => $_getSZ(0);
  @$pb.TagNumber(1)
  set messageId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasMessageId() => $_has(0);
  @$pb.TagNumber(1)
  void clearMessageId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get content => $_getSZ(1);
  @$pb.TagNumber(2)
  set content($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasContent() => $_has(1);
  @$pb.TagNumber(2)
  void clearContent() => $_clearField(2);
}

class RunCompleted extends $pb.GeneratedMessage {
  factory RunCompleted({
    $core.String? runId,
    $core.String? assistantMessageId,
    $1.RunState? runState,
  }) {
    final result = create();
    if (runId != null) result.runId = runId;
    if (assistantMessageId != null)
      result.assistantMessageId = assistantMessageId;
    if (runState != null) result.runState = runState;
    return result;
  }

  RunCompleted._();

  factory RunCompleted.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RunCompleted.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RunCompleted',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'runId')
    ..aOS(2, _omitFieldNames ? '' : 'assistantMessageId')
    ..aOM<$1.RunState>(3, _omitFieldNames ? '' : 'runState',
        subBuilder: $1.RunState.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunCompleted clone() => RunCompleted()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunCompleted copyWith(void Function(RunCompleted) updates) =>
      super.copyWith((message) => updates(message as RunCompleted))
          as RunCompleted;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RunCompleted create() => RunCompleted._();
  @$core.override
  RunCompleted createEmptyInstance() => create();
  static $pb.PbList<RunCompleted> createRepeated() =>
      $pb.PbList<RunCompleted>();
  @$core.pragma('dart2js:noInline')
  static RunCompleted getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RunCompleted>(create);
  static RunCompleted? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get runId => $_getSZ(0);
  @$pb.TagNumber(1)
  set runId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRunId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRunId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get assistantMessageId => $_getSZ(1);
  @$pb.TagNumber(2)
  set assistantMessageId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasAssistantMessageId() => $_has(1);
  @$pb.TagNumber(2)
  void clearAssistantMessageId() => $_clearField(2);

  @$pb.TagNumber(3)
  $1.RunState get runState => $_getN(2);
  @$pb.TagNumber(3)
  set runState($1.RunState value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasRunState() => $_has(2);
  @$pb.TagNumber(3)
  void clearRunState() => $_clearField(3);
  @$pb.TagNumber(3)
  $1.RunState ensureRunState() => $_ensure(2);
}

class RunFailed extends $pb.GeneratedMessage {
  factory RunFailed({
    $core.String? runId,
    $core.String? code,
    $core.String? message,
    @$core.Deprecated('This field is deprecated.') $core.bool? retryable,
    $1.RunState? runState,
  }) {
    final result = create();
    if (runId != null) result.runId = runId;
    if (code != null) result.code = code;
    if (message != null) result.message = message;
    if (retryable != null) result.retryable = retryable;
    if (runState != null) result.runState = runState;
    return result;
  }

  RunFailed._();

  factory RunFailed.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RunFailed.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RunFailed',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'runId')
    ..aOS(2, _omitFieldNames ? '' : 'code')
    ..aOS(3, _omitFieldNames ? '' : 'message')
    ..aOB(4, _omitFieldNames ? '' : 'retryable')
    ..aOM<$1.RunState>(5, _omitFieldNames ? '' : 'runState',
        subBuilder: $1.RunState.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunFailed clone() => RunFailed()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunFailed copyWith(void Function(RunFailed) updates) =>
      super.copyWith((message) => updates(message as RunFailed)) as RunFailed;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RunFailed create() => RunFailed._();
  @$core.override
  RunFailed createEmptyInstance() => create();
  static $pb.PbList<RunFailed> createRepeated() => $pb.PbList<RunFailed>();
  @$core.pragma('dart2js:noInline')
  static RunFailed getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<RunFailed>(create);
  static RunFailed? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get runId => $_getSZ(0);
  @$pb.TagNumber(1)
  set runId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRunId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRunId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get code => $_getSZ(1);
  @$pb.TagNumber(2)
  set code($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCode() => $_has(1);
  @$pb.TagNumber(2)
  void clearCode() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get message => $_getSZ(2);
  @$pb.TagNumber(3)
  set message($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasMessage() => $_has(2);
  @$pb.TagNumber(3)
  void clearMessage() => $_clearField(3);

  @$core.Deprecated('This field is deprecated.')
  @$pb.TagNumber(4)
  $core.bool get retryable => $_getBF(3);
  @$core.Deprecated('This field is deprecated.')
  @$pb.TagNumber(4)
  set retryable($core.bool value) => $_setBool(3, value);
  @$core.Deprecated('This field is deprecated.')
  @$pb.TagNumber(4)
  $core.bool hasRetryable() => $_has(3);
  @$core.Deprecated('This field is deprecated.')
  @$pb.TagNumber(4)
  void clearRetryable() => $_clearField(4);

  @$pb.TagNumber(5)
  $1.RunState get runState => $_getN(4);
  @$pb.TagNumber(5)
  set runState($1.RunState value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasRunState() => $_has(4);
  @$pb.TagNumber(5)
  void clearRunState() => $_clearField(5);
  @$pb.TagNumber(5)
  $1.RunState ensureRunState() => $_ensure(4);
}

class RunCancelled extends $pb.GeneratedMessage {
  factory RunCancelled({
    $core.String? runId,
    $core.String? reason,
    $1.RunState? runState,
  }) {
    final result = create();
    if (runId != null) result.runId = runId;
    if (reason != null) result.reason = reason;
    if (runState != null) result.runState = runState;
    return result;
  }

  RunCancelled._();

  factory RunCancelled.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RunCancelled.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RunCancelled',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'runId')
    ..aOS(2, _omitFieldNames ? '' : 'reason')
    ..aOM<$1.RunState>(3, _omitFieldNames ? '' : 'runState',
        subBuilder: $1.RunState.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunCancelled clone() => RunCancelled()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunCancelled copyWith(void Function(RunCancelled) updates) =>
      super.copyWith((message) => updates(message as RunCancelled))
          as RunCancelled;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RunCancelled create() => RunCancelled._();
  @$core.override
  RunCancelled createEmptyInstance() => create();
  static $pb.PbList<RunCancelled> createRepeated() =>
      $pb.PbList<RunCancelled>();
  @$core.pragma('dart2js:noInline')
  static RunCancelled getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RunCancelled>(create);
  static RunCancelled? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get runId => $_getSZ(0);
  @$pb.TagNumber(1)
  set runId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRunId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRunId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get reason => $_getSZ(1);
  @$pb.TagNumber(2)
  set reason($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasReason() => $_has(1);
  @$pb.TagNumber(2)
  void clearReason() => $_clearField(2);

  @$pb.TagNumber(3)
  $1.RunState get runState => $_getN(2);
  @$pb.TagNumber(3)
  set runState($1.RunState value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasRunState() => $_has(2);
  @$pb.TagNumber(3)
  void clearRunState() => $_clearField(3);
  @$pb.TagNumber(3)
  $1.RunState ensureRunState() => $_ensure(2);
}

class RunStateChanged extends $pb.GeneratedMessage {
  factory RunStateChanged({
    $1.RunState? runState,
  }) {
    final result = create();
    if (runState != null) result.runState = runState;
    return result;
  }

  RunStateChanged._();

  factory RunStateChanged.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RunStateChanged.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RunStateChanged',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOM<$1.RunState>(1, _omitFieldNames ? '' : 'runState',
        subBuilder: $1.RunState.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunStateChanged clone() => RunStateChanged()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunStateChanged copyWith(void Function(RunStateChanged) updates) =>
      super.copyWith((message) => updates(message as RunStateChanged))
          as RunStateChanged;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RunStateChanged create() => RunStateChanged._();
  @$core.override
  RunStateChanged createEmptyInstance() => create();
  static $pb.PbList<RunStateChanged> createRepeated() =>
      $pb.PbList<RunStateChanged>();
  @$core.pragma('dart2js:noInline')
  static RunStateChanged getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RunStateChanged>(create);
  static RunStateChanged? _defaultInstance;

  @$pb.TagNumber(1)
  $1.RunState get runState => $_getN(0);
  @$pb.TagNumber(1)
  set runState($1.RunState value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasRunState() => $_has(0);
  @$pb.TagNumber(1)
  void clearRunState() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.RunState ensureRunState() => $_ensure(0);
}

enum ChatStreamEvent_Event {
  runQueued,
  runStarted,
  messageStarted,
  tokenDelta,
  toolCallStarted,
  toolCallCompleted,
  toolCallFailed,
  approvalRequested,
  approvalApproved,
  approvalDenied,
  approvalExpired,
  approvalConsumed,
  messageCompleted,
  runCompleted,
  runFailed,
  runCancelled,
  persistedEvent,
  runStateChanged,
  notSet
}

class ChatStreamEvent extends $pb.GeneratedMessage {
  factory ChatStreamEvent({
    $core.String? sessionId,
    $core.String? runId,
    $core.String? traceId,
    $fixnum.Int64? sequence,
    RunQueued? runQueued,
    RunStarted? runStarted,
    MessageStarted? messageStarted,
    TokenDelta? tokenDelta,
    ToolEvent? toolCallStarted,
    ToolEvent? toolCallCompleted,
    ToolEvent? toolCallFailed,
    ApprovalEvent? approvalRequested,
    ApprovalEvent? approvalApproved,
    ApprovalEvent? approvalDenied,
    ApprovalEvent? approvalExpired,
    ApprovalEvent? approvalConsumed,
    MessageCompleted? messageCompleted,
    RunCompleted? runCompleted,
    RunFailed? runFailed,
    RunCancelled? runCancelled,
    $3.TuringEvent? persistedEvent,
    RunStateChanged? runStateChanged,
  }) {
    final result = create();
    if (sessionId != null) result.sessionId = sessionId;
    if (runId != null) result.runId = runId;
    if (traceId != null) result.traceId = traceId;
    if (sequence != null) result.sequence = sequence;
    if (runQueued != null) result.runQueued = runQueued;
    if (runStarted != null) result.runStarted = runStarted;
    if (messageStarted != null) result.messageStarted = messageStarted;
    if (tokenDelta != null) result.tokenDelta = tokenDelta;
    if (toolCallStarted != null) result.toolCallStarted = toolCallStarted;
    if (toolCallCompleted != null) result.toolCallCompleted = toolCallCompleted;
    if (toolCallFailed != null) result.toolCallFailed = toolCallFailed;
    if (approvalRequested != null) result.approvalRequested = approvalRequested;
    if (approvalApproved != null) result.approvalApproved = approvalApproved;
    if (approvalDenied != null) result.approvalDenied = approvalDenied;
    if (approvalExpired != null) result.approvalExpired = approvalExpired;
    if (approvalConsumed != null) result.approvalConsumed = approvalConsumed;
    if (messageCompleted != null) result.messageCompleted = messageCompleted;
    if (runCompleted != null) result.runCompleted = runCompleted;
    if (runFailed != null) result.runFailed = runFailed;
    if (runCancelled != null) result.runCancelled = runCancelled;
    if (persistedEvent != null) result.persistedEvent = persistedEvent;
    if (runStateChanged != null) result.runStateChanged = runStateChanged;
    return result;
  }

  ChatStreamEvent._();

  factory ChatStreamEvent.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ChatStreamEvent.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static const $core.Map<$core.int, ChatStreamEvent_Event>
      _ChatStreamEvent_EventByTag = {
    10: ChatStreamEvent_Event.runQueued,
    11: ChatStreamEvent_Event.runStarted,
    12: ChatStreamEvent_Event.messageStarted,
    13: ChatStreamEvent_Event.tokenDelta,
    14: ChatStreamEvent_Event.toolCallStarted,
    15: ChatStreamEvent_Event.toolCallCompleted,
    16: ChatStreamEvent_Event.toolCallFailed,
    17: ChatStreamEvent_Event.approvalRequested,
    18: ChatStreamEvent_Event.approvalApproved,
    19: ChatStreamEvent_Event.approvalDenied,
    20: ChatStreamEvent_Event.approvalExpired,
    21: ChatStreamEvent_Event.approvalConsumed,
    22: ChatStreamEvent_Event.messageCompleted,
    23: ChatStreamEvent_Event.runCompleted,
    24: ChatStreamEvent_Event.runFailed,
    25: ChatStreamEvent_Event.runCancelled,
    26: ChatStreamEvent_Event.persistedEvent,
    27: ChatStreamEvent_Event.runStateChanged,
    0: ChatStreamEvent_Event.notSet
  };
  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ChatStreamEvent',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..oo(0, [
      10,
      11,
      12,
      13,
      14,
      15,
      16,
      17,
      18,
      19,
      20,
      21,
      22,
      23,
      24,
      25,
      26,
      27
    ])
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..aOS(2, _omitFieldNames ? '' : 'runId')
    ..aOS(3, _omitFieldNames ? '' : 'traceId')
    ..aInt64(4, _omitFieldNames ? '' : 'sequence')
    ..aOM<RunQueued>(10, _omitFieldNames ? '' : 'runQueued',
        subBuilder: RunQueued.create)
    ..aOM<RunStarted>(11, _omitFieldNames ? '' : 'runStarted',
        subBuilder: RunStarted.create)
    ..aOM<MessageStarted>(12, _omitFieldNames ? '' : 'messageStarted',
        subBuilder: MessageStarted.create)
    ..aOM<TokenDelta>(13, _omitFieldNames ? '' : 'tokenDelta',
        subBuilder: TokenDelta.create)
    ..aOM<ToolEvent>(14, _omitFieldNames ? '' : 'toolCallStarted',
        subBuilder: ToolEvent.create)
    ..aOM<ToolEvent>(15, _omitFieldNames ? '' : 'toolCallCompleted',
        subBuilder: ToolEvent.create)
    ..aOM<ToolEvent>(16, _omitFieldNames ? '' : 'toolCallFailed',
        subBuilder: ToolEvent.create)
    ..aOM<ApprovalEvent>(17, _omitFieldNames ? '' : 'approvalRequested',
        subBuilder: ApprovalEvent.create)
    ..aOM<ApprovalEvent>(18, _omitFieldNames ? '' : 'approvalApproved',
        subBuilder: ApprovalEvent.create)
    ..aOM<ApprovalEvent>(19, _omitFieldNames ? '' : 'approvalDenied',
        subBuilder: ApprovalEvent.create)
    ..aOM<ApprovalEvent>(20, _omitFieldNames ? '' : 'approvalExpired',
        subBuilder: ApprovalEvent.create)
    ..aOM<ApprovalEvent>(21, _omitFieldNames ? '' : 'approvalConsumed',
        subBuilder: ApprovalEvent.create)
    ..aOM<MessageCompleted>(22, _omitFieldNames ? '' : 'messageCompleted',
        subBuilder: MessageCompleted.create)
    ..aOM<RunCompleted>(23, _omitFieldNames ? '' : 'runCompleted',
        subBuilder: RunCompleted.create)
    ..aOM<RunFailed>(24, _omitFieldNames ? '' : 'runFailed',
        subBuilder: RunFailed.create)
    ..aOM<RunCancelled>(25, _omitFieldNames ? '' : 'runCancelled',
        subBuilder: RunCancelled.create)
    ..aOM<$3.TuringEvent>(26, _omitFieldNames ? '' : 'persistedEvent',
        subBuilder: $3.TuringEvent.create)
    ..aOM<RunStateChanged>(27, _omitFieldNames ? '' : 'runStateChanged',
        subBuilder: RunStateChanged.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ChatStreamEvent clone() => ChatStreamEvent()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ChatStreamEvent copyWith(void Function(ChatStreamEvent) updates) =>
      super.copyWith((message) => updates(message as ChatStreamEvent))
          as ChatStreamEvent;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ChatStreamEvent create() => ChatStreamEvent._();
  @$core.override
  ChatStreamEvent createEmptyInstance() => create();
  static $pb.PbList<ChatStreamEvent> createRepeated() =>
      $pb.PbList<ChatStreamEvent>();
  @$core.pragma('dart2js:noInline')
  static ChatStreamEvent getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ChatStreamEvent>(create);
  static ChatStreamEvent? _defaultInstance;

  ChatStreamEvent_Event whichEvent() =>
      _ChatStreamEvent_EventByTag[$_whichOneof(0)]!;
  void clearEvent() => $_clearField($_whichOneof(0));

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get runId => $_getSZ(1);
  @$pb.TagNumber(2)
  set runId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRunId() => $_has(1);
  @$pb.TagNumber(2)
  void clearRunId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get traceId => $_getSZ(2);
  @$pb.TagNumber(3)
  set traceId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasTraceId() => $_has(2);
  @$pb.TagNumber(3)
  void clearTraceId() => $_clearField(3);

  @$pb.TagNumber(4)
  $fixnum.Int64 get sequence => $_getI64(3);
  @$pb.TagNumber(4)
  set sequence($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasSequence() => $_has(3);
  @$pb.TagNumber(4)
  void clearSequence() => $_clearField(4);

  @$pb.TagNumber(10)
  RunQueued get runQueued => $_getN(4);
  @$pb.TagNumber(10)
  set runQueued(RunQueued value) => $_setField(10, value);
  @$pb.TagNumber(10)
  $core.bool hasRunQueued() => $_has(4);
  @$pb.TagNumber(10)
  void clearRunQueued() => $_clearField(10);
  @$pb.TagNumber(10)
  RunQueued ensureRunQueued() => $_ensure(4);

  @$pb.TagNumber(11)
  RunStarted get runStarted => $_getN(5);
  @$pb.TagNumber(11)
  set runStarted(RunStarted value) => $_setField(11, value);
  @$pb.TagNumber(11)
  $core.bool hasRunStarted() => $_has(5);
  @$pb.TagNumber(11)
  void clearRunStarted() => $_clearField(11);
  @$pb.TagNumber(11)
  RunStarted ensureRunStarted() => $_ensure(5);

  @$pb.TagNumber(12)
  MessageStarted get messageStarted => $_getN(6);
  @$pb.TagNumber(12)
  set messageStarted(MessageStarted value) => $_setField(12, value);
  @$pb.TagNumber(12)
  $core.bool hasMessageStarted() => $_has(6);
  @$pb.TagNumber(12)
  void clearMessageStarted() => $_clearField(12);
  @$pb.TagNumber(12)
  MessageStarted ensureMessageStarted() => $_ensure(6);

  @$pb.TagNumber(13)
  TokenDelta get tokenDelta => $_getN(7);
  @$pb.TagNumber(13)
  set tokenDelta(TokenDelta value) => $_setField(13, value);
  @$pb.TagNumber(13)
  $core.bool hasTokenDelta() => $_has(7);
  @$pb.TagNumber(13)
  void clearTokenDelta() => $_clearField(13);
  @$pb.TagNumber(13)
  TokenDelta ensureTokenDelta() => $_ensure(7);

  @$pb.TagNumber(14)
  ToolEvent get toolCallStarted => $_getN(8);
  @$pb.TagNumber(14)
  set toolCallStarted(ToolEvent value) => $_setField(14, value);
  @$pb.TagNumber(14)
  $core.bool hasToolCallStarted() => $_has(8);
  @$pb.TagNumber(14)
  void clearToolCallStarted() => $_clearField(14);
  @$pb.TagNumber(14)
  ToolEvent ensureToolCallStarted() => $_ensure(8);

  @$pb.TagNumber(15)
  ToolEvent get toolCallCompleted => $_getN(9);
  @$pb.TagNumber(15)
  set toolCallCompleted(ToolEvent value) => $_setField(15, value);
  @$pb.TagNumber(15)
  $core.bool hasToolCallCompleted() => $_has(9);
  @$pb.TagNumber(15)
  void clearToolCallCompleted() => $_clearField(15);
  @$pb.TagNumber(15)
  ToolEvent ensureToolCallCompleted() => $_ensure(9);

  @$pb.TagNumber(16)
  ToolEvent get toolCallFailed => $_getN(10);
  @$pb.TagNumber(16)
  set toolCallFailed(ToolEvent value) => $_setField(16, value);
  @$pb.TagNumber(16)
  $core.bool hasToolCallFailed() => $_has(10);
  @$pb.TagNumber(16)
  void clearToolCallFailed() => $_clearField(16);
  @$pb.TagNumber(16)
  ToolEvent ensureToolCallFailed() => $_ensure(10);

  @$pb.TagNumber(17)
  ApprovalEvent get approvalRequested => $_getN(11);
  @$pb.TagNumber(17)
  set approvalRequested(ApprovalEvent value) => $_setField(17, value);
  @$pb.TagNumber(17)
  $core.bool hasApprovalRequested() => $_has(11);
  @$pb.TagNumber(17)
  void clearApprovalRequested() => $_clearField(17);
  @$pb.TagNumber(17)
  ApprovalEvent ensureApprovalRequested() => $_ensure(11);

  @$pb.TagNumber(18)
  ApprovalEvent get approvalApproved => $_getN(12);
  @$pb.TagNumber(18)
  set approvalApproved(ApprovalEvent value) => $_setField(18, value);
  @$pb.TagNumber(18)
  $core.bool hasApprovalApproved() => $_has(12);
  @$pb.TagNumber(18)
  void clearApprovalApproved() => $_clearField(18);
  @$pb.TagNumber(18)
  ApprovalEvent ensureApprovalApproved() => $_ensure(12);

  @$pb.TagNumber(19)
  ApprovalEvent get approvalDenied => $_getN(13);
  @$pb.TagNumber(19)
  set approvalDenied(ApprovalEvent value) => $_setField(19, value);
  @$pb.TagNumber(19)
  $core.bool hasApprovalDenied() => $_has(13);
  @$pb.TagNumber(19)
  void clearApprovalDenied() => $_clearField(19);
  @$pb.TagNumber(19)
  ApprovalEvent ensureApprovalDenied() => $_ensure(13);

  @$pb.TagNumber(20)
  ApprovalEvent get approvalExpired => $_getN(14);
  @$pb.TagNumber(20)
  set approvalExpired(ApprovalEvent value) => $_setField(20, value);
  @$pb.TagNumber(20)
  $core.bool hasApprovalExpired() => $_has(14);
  @$pb.TagNumber(20)
  void clearApprovalExpired() => $_clearField(20);
  @$pb.TagNumber(20)
  ApprovalEvent ensureApprovalExpired() => $_ensure(14);

  @$pb.TagNumber(21)
  ApprovalEvent get approvalConsumed => $_getN(15);
  @$pb.TagNumber(21)
  set approvalConsumed(ApprovalEvent value) => $_setField(21, value);
  @$pb.TagNumber(21)
  $core.bool hasApprovalConsumed() => $_has(15);
  @$pb.TagNumber(21)
  void clearApprovalConsumed() => $_clearField(21);
  @$pb.TagNumber(21)
  ApprovalEvent ensureApprovalConsumed() => $_ensure(15);

  @$pb.TagNumber(22)
  MessageCompleted get messageCompleted => $_getN(16);
  @$pb.TagNumber(22)
  set messageCompleted(MessageCompleted value) => $_setField(22, value);
  @$pb.TagNumber(22)
  $core.bool hasMessageCompleted() => $_has(16);
  @$pb.TagNumber(22)
  void clearMessageCompleted() => $_clearField(22);
  @$pb.TagNumber(22)
  MessageCompleted ensureMessageCompleted() => $_ensure(16);

  @$pb.TagNumber(23)
  RunCompleted get runCompleted => $_getN(17);
  @$pb.TagNumber(23)
  set runCompleted(RunCompleted value) => $_setField(23, value);
  @$pb.TagNumber(23)
  $core.bool hasRunCompleted() => $_has(17);
  @$pb.TagNumber(23)
  void clearRunCompleted() => $_clearField(23);
  @$pb.TagNumber(23)
  RunCompleted ensureRunCompleted() => $_ensure(17);

  @$pb.TagNumber(24)
  RunFailed get runFailed => $_getN(18);
  @$pb.TagNumber(24)
  set runFailed(RunFailed value) => $_setField(24, value);
  @$pb.TagNumber(24)
  $core.bool hasRunFailed() => $_has(18);
  @$pb.TagNumber(24)
  void clearRunFailed() => $_clearField(24);
  @$pb.TagNumber(24)
  RunFailed ensureRunFailed() => $_ensure(18);

  @$pb.TagNumber(25)
  RunCancelled get runCancelled => $_getN(19);
  @$pb.TagNumber(25)
  set runCancelled(RunCancelled value) => $_setField(25, value);
  @$pb.TagNumber(25)
  $core.bool hasRunCancelled() => $_has(19);
  @$pb.TagNumber(25)
  void clearRunCancelled() => $_clearField(25);
  @$pb.TagNumber(25)
  RunCancelled ensureRunCancelled() => $_ensure(19);

  @$pb.TagNumber(26)
  $3.TuringEvent get persistedEvent => $_getN(20);
  @$pb.TagNumber(26)
  set persistedEvent($3.TuringEvent value) => $_setField(26, value);
  @$pb.TagNumber(26)
  $core.bool hasPersistedEvent() => $_has(20);
  @$pb.TagNumber(26)
  void clearPersistedEvent() => $_clearField(26);
  @$pb.TagNumber(26)
  $3.TuringEvent ensurePersistedEvent() => $_ensure(20);

  @$pb.TagNumber(27)
  RunStateChanged get runStateChanged => $_getN(21);
  @$pb.TagNumber(27)
  set runStateChanged(RunStateChanged value) => $_setField(27, value);
  @$pb.TagNumber(27)
  $core.bool hasRunStateChanged() => $_has(21);
  @$pb.TagNumber(27)
  void clearRunStateChanged() => $_clearField(27);
  @$pb.TagNumber(27)
  RunStateChanged ensureRunStateChanged() => $_ensure(21);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
