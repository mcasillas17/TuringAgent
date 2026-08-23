// This is a generated file - do not edit.
//
// Generated from turing/v1/tools.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import '../../google/protobuf/struct.pb.dart' as $0;
import 'common.pbenum.dart' as $1;
import 'tools.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'tools.pbenum.dart';

class ToolCallError extends $pb.GeneratedMessage {
  factory ToolCallError({
    $core.String? code,
    $core.String? message,
  }) {
    final result = create();
    if (code != null) result.code = code;
    if (message != null) result.message = message;
    return result;
  }

  ToolCallError._();

  factory ToolCallError.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ToolCallError.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ToolCallError',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'code')
    ..aOS(2, _omitFieldNames ? '' : 'message')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ToolCallError clone() => ToolCallError()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ToolCallError copyWith(void Function(ToolCallError) updates) =>
      super.copyWith((message) => updates(message as ToolCallError))
          as ToolCallError;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ToolCallError create() => ToolCallError._();
  @$core.override
  ToolCallError createEmptyInstance() => create();
  static $pb.PbList<ToolCallError> createRepeated() =>
      $pb.PbList<ToolCallError>();
  @$core.pragma('dart2js:noInline')
  static ToolCallError getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ToolCallError>(create);
  static ToolCallError? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get code => $_getSZ(0);
  @$pb.TagNumber(1)
  set code($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasCode() => $_has(0);
  @$pb.TagNumber(1)
  void clearCode() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get message => $_getSZ(1);
  @$pb.TagNumber(2)
  set message($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasMessage() => $_has(1);
  @$pb.TagNumber(2)
  void clearMessage() => $_clearField(2);
}

class ToolCallBeacon extends $pb.GeneratedMessage {
  factory ToolCallBeacon({
    ToolCallPhase? phase,
    $core.String? toolCallId,
    $1.AgentId? agentId,
    $core.String? serverName,
    $core.String? toolName,
    $0.Struct? args,
    ToolCallStatus? status,
    $core.String? resultSummary,
    $fixnum.Int64? durationMs,
    ToolCallError? error,
    $core.String? runId,
    $core.String? traceId,
    $core.String? modelToolCallId,
  }) {
    final result = create();
    if (phase != null) result.phase = phase;
    if (toolCallId != null) result.toolCallId = toolCallId;
    if (agentId != null) result.agentId = agentId;
    if (serverName != null) result.serverName = serverName;
    if (toolName != null) result.toolName = toolName;
    if (args != null) result.args = args;
    if (status != null) result.status = status;
    if (resultSummary != null) result.resultSummary = resultSummary;
    if (durationMs != null) result.durationMs = durationMs;
    if (error != null) result.error = error;
    if (runId != null) result.runId = runId;
    if (traceId != null) result.traceId = traceId;
    if (modelToolCallId != null) result.modelToolCallId = modelToolCallId;
    return result;
  }

  ToolCallBeacon._();

  factory ToolCallBeacon.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ToolCallBeacon.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ToolCallBeacon',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..e<ToolCallPhase>(1, _omitFieldNames ? '' : 'phase', $pb.PbFieldType.OE,
        defaultOrMaker: ToolCallPhase.TOOL_CALL_PHASE_UNSPECIFIED,
        valueOf: ToolCallPhase.valueOf,
        enumValues: ToolCallPhase.values)
    ..aOS(2, _omitFieldNames ? '' : 'toolCallId')
    ..e<$1.AgentId>(3, _omitFieldNames ? '' : 'agentId', $pb.PbFieldType.OE,
        defaultOrMaker: $1.AgentId.AGENT_ID_UNSPECIFIED,
        valueOf: $1.AgentId.valueOf,
        enumValues: $1.AgentId.values)
    ..aOS(4, _omitFieldNames ? '' : 'serverName')
    ..aOS(5, _omitFieldNames ? '' : 'toolName')
    ..aOM<$0.Struct>(6, _omitFieldNames ? '' : 'args',
        subBuilder: $0.Struct.create)
    ..e<ToolCallStatus>(7, _omitFieldNames ? '' : 'status', $pb.PbFieldType.OE,
        defaultOrMaker: ToolCallStatus.TOOL_CALL_STATUS_UNSPECIFIED,
        valueOf: ToolCallStatus.valueOf,
        enumValues: ToolCallStatus.values)
    ..aOS(8, _omitFieldNames ? '' : 'resultSummary')
    ..aInt64(9, _omitFieldNames ? '' : 'durationMs')
    ..aOM<ToolCallError>(10, _omitFieldNames ? '' : 'error',
        subBuilder: ToolCallError.create)
    ..aOS(11, _omitFieldNames ? '' : 'runId')
    ..aOS(12, _omitFieldNames ? '' : 'traceId')
    ..aOS(13, _omitFieldNames ? '' : 'modelToolCallId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ToolCallBeacon clone() => ToolCallBeacon()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ToolCallBeacon copyWith(void Function(ToolCallBeacon) updates) =>
      super.copyWith((message) => updates(message as ToolCallBeacon))
          as ToolCallBeacon;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ToolCallBeacon create() => ToolCallBeacon._();
  @$core.override
  ToolCallBeacon createEmptyInstance() => create();
  static $pb.PbList<ToolCallBeacon> createRepeated() =>
      $pb.PbList<ToolCallBeacon>();
  @$core.pragma('dart2js:noInline')
  static ToolCallBeacon getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ToolCallBeacon>(create);
  static ToolCallBeacon? _defaultInstance;

  @$pb.TagNumber(1)
  ToolCallPhase get phase => $_getN(0);
  @$pb.TagNumber(1)
  set phase(ToolCallPhase value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasPhase() => $_has(0);
  @$pb.TagNumber(1)
  void clearPhase() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get toolCallId => $_getSZ(1);
  @$pb.TagNumber(2)
  set toolCallId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasToolCallId() => $_has(1);
  @$pb.TagNumber(2)
  void clearToolCallId() => $_clearField(2);

  @$pb.TagNumber(3)
  $1.AgentId get agentId => $_getN(2);
  @$pb.TagNumber(3)
  set agentId($1.AgentId value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasAgentId() => $_has(2);
  @$pb.TagNumber(3)
  void clearAgentId() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get serverName => $_getSZ(3);
  @$pb.TagNumber(4)
  set serverName($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasServerName() => $_has(3);
  @$pb.TagNumber(4)
  void clearServerName() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get toolName => $_getSZ(4);
  @$pb.TagNumber(5)
  set toolName($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasToolName() => $_has(4);
  @$pb.TagNumber(5)
  void clearToolName() => $_clearField(5);

  @$pb.TagNumber(6)
  $0.Struct get args => $_getN(5);
  @$pb.TagNumber(6)
  set args($0.Struct value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasArgs() => $_has(5);
  @$pb.TagNumber(6)
  void clearArgs() => $_clearField(6);
  @$pb.TagNumber(6)
  $0.Struct ensureArgs() => $_ensure(5);

  @$pb.TagNumber(7)
  ToolCallStatus get status => $_getN(6);
  @$pb.TagNumber(7)
  set status(ToolCallStatus value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasStatus() => $_has(6);
  @$pb.TagNumber(7)
  void clearStatus() => $_clearField(7);

  @$pb.TagNumber(8)
  $core.String get resultSummary => $_getSZ(7);
  @$pb.TagNumber(8)
  set resultSummary($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasResultSummary() => $_has(7);
  @$pb.TagNumber(8)
  void clearResultSummary() => $_clearField(8);

  @$pb.TagNumber(9)
  $fixnum.Int64 get durationMs => $_getI64(8);
  @$pb.TagNumber(9)
  set durationMs($fixnum.Int64 value) => $_setInt64(8, value);
  @$pb.TagNumber(9)
  $core.bool hasDurationMs() => $_has(8);
  @$pb.TagNumber(9)
  void clearDurationMs() => $_clearField(9);

  @$pb.TagNumber(10)
  ToolCallError get error => $_getN(9);
  @$pb.TagNumber(10)
  set error(ToolCallError value) => $_setField(10, value);
  @$pb.TagNumber(10)
  $core.bool hasError() => $_has(9);
  @$pb.TagNumber(10)
  void clearError() => $_clearField(10);
  @$pb.TagNumber(10)
  ToolCallError ensureError() => $_ensure(9);

  @$pb.TagNumber(11)
  $core.String get runId => $_getSZ(10);
  @$pb.TagNumber(11)
  set runId($core.String value) => $_setString(10, value);
  @$pb.TagNumber(11)
  $core.bool hasRunId() => $_has(10);
  @$pb.TagNumber(11)
  void clearRunId() => $_clearField(11);

  @$pb.TagNumber(12)
  $core.String get traceId => $_getSZ(11);
  @$pb.TagNumber(12)
  set traceId($core.String value) => $_setString(11, value);
  @$pb.TagNumber(12)
  $core.bool hasTraceId() => $_has(11);
  @$pb.TagNumber(12)
  void clearTraceId() => $_clearField(12);

  @$pb.TagNumber(13)
  $core.String get modelToolCallId => $_getSZ(12);
  @$pb.TagNumber(13)
  set modelToolCallId($core.String value) => $_setString(12, value);
  @$pb.TagNumber(13)
  $core.bool hasModelToolCallId() => $_has(12);
  @$pb.TagNumber(13)
  void clearModelToolCallId() => $_clearField(13);
}

class ToolPolicyDecision extends $pb.GeneratedMessage {
  factory ToolPolicyDecision({
    ToolPolicyDecision_Decision? decision,
    $core.String? toolCallId,
    $core.String? approvalId,
    $core.String? reason,
    $core.bool? terminalRun,
    ToolCallPhase? phase,
    $core.String? provenanceToken,
    $fixnum.Int64? runStateVersion,
    $core.bool? readOnly,
  }) {
    final result = create();
    if (decision != null) result.decision = decision;
    if (toolCallId != null) result.toolCallId = toolCallId;
    if (approvalId != null) result.approvalId = approvalId;
    if (reason != null) result.reason = reason;
    if (terminalRun != null) result.terminalRun = terminalRun;
    if (phase != null) result.phase = phase;
    if (provenanceToken != null) result.provenanceToken = provenanceToken;
    if (runStateVersion != null) result.runStateVersion = runStateVersion;
    if (readOnly != null) result.readOnly = readOnly;
    return result;
  }

  ToolPolicyDecision._();

  factory ToolPolicyDecision.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ToolPolicyDecision.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ToolPolicyDecision',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..e<ToolPolicyDecision_Decision>(
        1, _omitFieldNames ? '' : 'decision', $pb.PbFieldType.OE,
        defaultOrMaker: ToolPolicyDecision_Decision.DECISION_UNSPECIFIED,
        valueOf: ToolPolicyDecision_Decision.valueOf,
        enumValues: ToolPolicyDecision_Decision.values)
    ..aOS(2, _omitFieldNames ? '' : 'toolCallId')
    ..aOS(3, _omitFieldNames ? '' : 'approvalId')
    ..aOS(4, _omitFieldNames ? '' : 'reason')
    ..aOB(5, _omitFieldNames ? '' : 'terminalRun')
    ..e<ToolCallPhase>(6, _omitFieldNames ? '' : 'phase', $pb.PbFieldType.OE,
        defaultOrMaker: ToolCallPhase.TOOL_CALL_PHASE_UNSPECIFIED,
        valueOf: ToolCallPhase.valueOf,
        enumValues: ToolCallPhase.values)
    ..aOS(7, _omitFieldNames ? '' : 'provenanceToken')
    ..aInt64(8, _omitFieldNames ? '' : 'runStateVersion')
    ..aOB(9, _omitFieldNames ? '' : 'readOnly')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ToolPolicyDecision clone() => ToolPolicyDecision()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ToolPolicyDecision copyWith(void Function(ToolPolicyDecision) updates) =>
      super.copyWith((message) => updates(message as ToolPolicyDecision))
          as ToolPolicyDecision;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ToolPolicyDecision create() => ToolPolicyDecision._();
  @$core.override
  ToolPolicyDecision createEmptyInstance() => create();
  static $pb.PbList<ToolPolicyDecision> createRepeated() =>
      $pb.PbList<ToolPolicyDecision>();
  @$core.pragma('dart2js:noInline')
  static ToolPolicyDecision getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ToolPolicyDecision>(create);
  static ToolPolicyDecision? _defaultInstance;

  @$pb.TagNumber(1)
  ToolPolicyDecision_Decision get decision => $_getN(0);
  @$pb.TagNumber(1)
  set decision(ToolPolicyDecision_Decision value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasDecision() => $_has(0);
  @$pb.TagNumber(1)
  void clearDecision() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get toolCallId => $_getSZ(1);
  @$pb.TagNumber(2)
  set toolCallId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasToolCallId() => $_has(1);
  @$pb.TagNumber(2)
  void clearToolCallId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get approvalId => $_getSZ(2);
  @$pb.TagNumber(3)
  set approvalId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasApprovalId() => $_has(2);
  @$pb.TagNumber(3)
  void clearApprovalId() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get reason => $_getSZ(3);
  @$pb.TagNumber(4)
  set reason($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasReason() => $_has(3);
  @$pb.TagNumber(4)
  void clearReason() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.bool get terminalRun => $_getBF(4);
  @$pb.TagNumber(5)
  set terminalRun($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasTerminalRun() => $_has(4);
  @$pb.TagNumber(5)
  void clearTerminalRun() => $_clearField(5);

  @$pb.TagNumber(6)
  ToolCallPhase get phase => $_getN(5);
  @$pb.TagNumber(6)
  set phase(ToolCallPhase value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasPhase() => $_has(5);
  @$pb.TagNumber(6)
  void clearPhase() => $_clearField(6);

  /// Server-issued, server-signed capability binding this tool call to its
  /// session, run, deletion generation, tool, arguments and path scope. The
  /// runtime forwards it verbatim; it never mints or edits one.
  @$pb.TagNumber(7)
  $core.String get provenanceToken => $_getSZ(6);
  @$pb.TagNumber(7)
  set provenanceToken($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasProvenanceToken() => $_has(6);
  @$pb.TagNumber(7)
  void clearProvenanceToken() => $_clearField(7);

  /// The run's committed state version at the moment of this decision. A
  /// matching tool beacon can therefore prove ownership: the response carries
  /// the version forward before tool or model work continues, so a worker never
  /// advances past the state the orchestrator has committed.
  @$pb.TagNumber(8)
  $fixnum.Int64 get runStateVersion => $_getI64(7);
  @$pb.TagNumber(8)
  set runStateVersion($fixnum.Int64 value) => $_setInt64(7, value);
  @$pb.TagNumber(8)
  $core.bool hasRunStateVersion() => $_has(7);
  @$pb.TagNumber(8)
  void clearRunStateVersion() => $_clearField(8);

  /// Authored by the orchestrator. Approval waiting is still policy-driven;
  /// this bit only controls failure/side-effect classification.
  @$pb.TagNumber(9)
  $core.bool get readOnly => $_getBF(8);
  @$pb.TagNumber(9)
  set readOnly($core.bool value) => $_setBool(8, value);
  @$pb.TagNumber(9)
  $core.bool hasReadOnly() => $_has(8);
  @$pb.TagNumber(9)
  void clearReadOnly() => $_clearField(9);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
