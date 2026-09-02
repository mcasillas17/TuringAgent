// This is a generated file - do not edit.
//
// Generated from turing/v1/automations.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

import '../../google/protobuf/timestamp.pb.dart' as $1;
import 'automations.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'automations.pbenum.dart';

/// A saved prompt the orchestrator sends on a schedule, without the user
/// starting it.
///
/// Every other run in the system exists because someone typed something. An
/// automation is the exception, which is why it carries an explicit allowlist:
/// a run nobody is watching cannot stop and ask.
class Automation extends $pb.GeneratedMessage {
  factory Automation({
    $core.String? automationId,
    $core.String? name,
    $core.String? prompt,
    AutomationSchedule? schedule,
    $core.bool? enabled,
    $core.Iterable<AutomationTool>? allowedTools,
    $1.Timestamp? lastRunAt,
    $1.Timestamp? nextRunAt,
    $core.String? sessionId,
    $core.String? lastRunId,
    $core.String? lastRunStatus,
    $core.String? lastRunError,
    $1.Timestamp? createdAt,
    $1.Timestamp? updatedAt,
    $core.String? lastOccurrenceFailureCode,
    $1.Timestamp? lastOccurrenceFailedAt,
  }) {
    final result = create();
    if (automationId != null) result.automationId = automationId;
    if (name != null) result.name = name;
    if (prompt != null) result.prompt = prompt;
    if (schedule != null) result.schedule = schedule;
    if (enabled != null) result.enabled = enabled;
    if (allowedTools != null) result.allowedTools.addAll(allowedTools);
    if (lastRunAt != null) result.lastRunAt = lastRunAt;
    if (nextRunAt != null) result.nextRunAt = nextRunAt;
    if (sessionId != null) result.sessionId = sessionId;
    if (lastRunId != null) result.lastRunId = lastRunId;
    if (lastRunStatus != null) result.lastRunStatus = lastRunStatus;
    if (lastRunError != null) result.lastRunError = lastRunError;
    if (createdAt != null) result.createdAt = createdAt;
    if (updatedAt != null) result.updatedAt = updatedAt;
    if (lastOccurrenceFailureCode != null)
      result.lastOccurrenceFailureCode = lastOccurrenceFailureCode;
    if (lastOccurrenceFailedAt != null)
      result.lastOccurrenceFailedAt = lastOccurrenceFailedAt;
    return result;
  }

  Automation._();

  factory Automation.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Automation.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Automation',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'automationId')
    ..aOS(2, _omitFieldNames ? '' : 'name')
    ..aOS(3, _omitFieldNames ? '' : 'prompt')
    ..aOM<AutomationSchedule>(4, _omitFieldNames ? '' : 'schedule',
        subBuilder: AutomationSchedule.create)
    ..aOB(5, _omitFieldNames ? '' : 'enabled')
    ..pPM<AutomationTool>(6, _omitFieldNames ? '' : 'allowedTools',
        subBuilder: AutomationTool.create)
    ..aOM<$1.Timestamp>(7, _omitFieldNames ? '' : 'lastRunAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(8, _omitFieldNames ? '' : 'nextRunAt',
        subBuilder: $1.Timestamp.create)
    ..aOS(9, _omitFieldNames ? '' : 'sessionId')
    ..aOS(10, _omitFieldNames ? '' : 'lastRunId')
    ..aOS(11, _omitFieldNames ? '' : 'lastRunStatus')
    ..aOS(12, _omitFieldNames ? '' : 'lastRunError')
    ..aOM<$1.Timestamp>(13, _omitFieldNames ? '' : 'createdAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(14, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $1.Timestamp.create)
    ..aOS(15, _omitFieldNames ? '' : 'lastOccurrenceFailureCode')
    ..aOM<$1.Timestamp>(16, _omitFieldNames ? '' : 'lastOccurrenceFailedAt',
        subBuilder: $1.Timestamp.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Automation clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Automation copyWith(void Function(Automation) updates) =>
      super.copyWith((message) => updates(message as Automation)) as Automation;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Automation create() => Automation._();
  @$core.override
  Automation createEmptyInstance() => create();
  static $pb.PbList<Automation> createRepeated() => $pb.PbList<Automation>();
  @$core.pragma('dart2js:noInline')
  static Automation getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Automation>(create);
  static Automation? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get automationId => $_getSZ(0);
  @$pb.TagNumber(1)
  set automationId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAutomationId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAutomationId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get name => $_getSZ(1);
  @$pb.TagNumber(2)
  set name($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasName() => $_has(1);
  @$pb.TagNumber(2)
  void clearName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get prompt => $_getSZ(2);
  @$pb.TagNumber(3)
  set prompt($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasPrompt() => $_has(2);
  @$pb.TagNumber(3)
  void clearPrompt() => $_clearField(3);

  @$pb.TagNumber(4)
  AutomationSchedule get schedule => $_getN(3);
  @$pb.TagNumber(4)
  set schedule(AutomationSchedule value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasSchedule() => $_has(3);
  @$pb.TagNumber(4)
  void clearSchedule() => $_clearField(4);
  @$pb.TagNumber(4)
  AutomationSchedule ensureSchedule() => $_ensure(3);

  @$pb.TagNumber(5)
  $core.bool get enabled => $_getBF(4);
  @$pb.TagNumber(5)
  set enabled($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasEnabled() => $_has(4);
  @$pb.TagNumber(5)
  void clearEnabled() => $_clearField(5);

  /// The tools this automation may run WITHOUT asking, named one by one. There
  /// is deliberately no wildcard: "everything" is not a consent anyone can
  /// give in advance. A tool that needs approval and is not in this list stops
  /// the run rather than waiting for a person who is not there.
  ///
  /// Scoped to this automation only. It is never consulted for a conversation
  /// the user drives by hand.
  @$pb.TagNumber(6)
  $pb.PbList<AutomationTool> get allowedTools => $_getList(5);

  @$pb.TagNumber(7)
  $1.Timestamp get lastRunAt => $_getN(6);
  @$pb.TagNumber(7)
  set lastRunAt($1.Timestamp value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasLastRunAt() => $_has(6);
  @$pb.TagNumber(7)
  void clearLastRunAt() => $_clearField(7);
  @$pb.TagNumber(7)
  $1.Timestamp ensureLastRunAt() => $_ensure(6);

  /// Absent while the automation is disabled: a disabled automation has no
  /// next run, and showing one would be a claim about the future that is false.
  @$pb.TagNumber(8)
  $1.Timestamp get nextRunAt => $_getN(7);
  @$pb.TagNumber(8)
  set nextRunAt($1.Timestamp value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasNextRunAt() => $_has(7);
  @$pb.TagNumber(8)
  void clearNextRunAt() => $_clearField(8);
  @$pb.TagNumber(8)
  $1.Timestamp ensureNextRunAt() => $_ensure(7);

  /// Where the automation's runs land. Empty until it has fired once.
  @$pb.TagNumber(9)
  $core.String get sessionId => $_getSZ(8);
  @$pb.TagNumber(9)
  set sessionId($core.String value) => $_setString(8, value);
  @$pb.TagNumber(9)
  $core.bool hasSessionId() => $_has(8);
  @$pb.TagNumber(9)
  void clearSessionId() => $_clearField(9);

  @$pb.TagNumber(10)
  $core.String get lastRunId => $_getSZ(9);
  @$pb.TagNumber(10)
  set lastRunId($core.String value) => $_setString(9, value);
  @$pb.TagNumber(10)
  $core.bool hasLastRunId() => $_has(9);
  @$pb.TagNumber(10)
  void clearLastRunId() => $_clearField(10);

  /// The outcome of the most recent run, so "what did this thing do while I
  /// was asleep" has an answer without opening the conversation.
  @$pb.TagNumber(11)
  $core.String get lastRunStatus => $_getSZ(10);
  @$pb.TagNumber(11)
  set lastRunStatus($core.String value) => $_setString(10, value);
  @$pb.TagNumber(11)
  $core.bool hasLastRunStatus() => $_has(10);
  @$pb.TagNumber(11)
  void clearLastRunStatus() => $_clearField(11);

  @$pb.TagNumber(12)
  $core.String get lastRunError => $_getSZ(11);
  @$pb.TagNumber(12)
  set lastRunError($core.String value) => $_setString(11, value);
  @$pb.TagNumber(12)
  $core.bool hasLastRunError() => $_has(11);
  @$pb.TagNumber(12)
  void clearLastRunError() => $_clearField(12);

  @$pb.TagNumber(13)
  $1.Timestamp get createdAt => $_getN(12);
  @$pb.TagNumber(13)
  set createdAt($1.Timestamp value) => $_setField(13, value);
  @$pb.TagNumber(13)
  $core.bool hasCreatedAt() => $_has(12);
  @$pb.TagNumber(13)
  void clearCreatedAt() => $_clearField(13);
  @$pb.TagNumber(13)
  $1.Timestamp ensureCreatedAt() => $_ensure(12);

  @$pb.TagNumber(14)
  $1.Timestamp get updatedAt => $_getN(13);
  @$pb.TagNumber(14)
  set updatedAt($1.Timestamp value) => $_setField(14, value);
  @$pb.TagNumber(14)
  $core.bool hasUpdatedAt() => $_has(13);
  @$pb.TagNumber(14)
  void clearUpdatedAt() => $_clearField(14);
  @$pb.TagNumber(14)
  $1.Timestamp ensureUpdatedAt() => $_ensure(13);

  /// Most recent scheduled occurrence that failed before a run could be
  /// created, kept separate so it never rewrites the outcome of last_run_id.
  @$pb.TagNumber(15)
  $core.String get lastOccurrenceFailureCode => $_getSZ(14);
  @$pb.TagNumber(15)
  set lastOccurrenceFailureCode($core.String value) => $_setString(14, value);
  @$pb.TagNumber(15)
  $core.bool hasLastOccurrenceFailureCode() => $_has(14);
  @$pb.TagNumber(15)
  void clearLastOccurrenceFailureCode() => $_clearField(15);

  @$pb.TagNumber(16)
  $1.Timestamp get lastOccurrenceFailedAt => $_getN(15);
  @$pb.TagNumber(16)
  set lastOccurrenceFailedAt($1.Timestamp value) => $_setField(16, value);
  @$pb.TagNumber(16)
  $core.bool hasLastOccurrenceFailedAt() => $_has(15);
  @$pb.TagNumber(16)
  void clearLastOccurrenceFailedAt() => $_clearField(16);
  @$pb.TagNumber(16)
  $1.Timestamp ensureLastOccurrenceFailedAt() => $_ensure(15);
}

/// A tool named by the pair the orchestrator's policy lookup uses, so an
/// allowlist entry cannot accidentally match a same-named tool on a different
/// server.
class AutomationTool extends $pb.GeneratedMessage {
  factory AutomationTool({
    $core.String? serverName,
    $core.String? toolName,
  }) {
    final result = create();
    if (serverName != null) result.serverName = serverName;
    if (toolName != null) result.toolName = toolName;
    return result;
  }

  AutomationTool._();

  factory AutomationTool.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory AutomationTool.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'AutomationTool',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'serverName')
    ..aOS(2, _omitFieldNames ? '' : 'toolName')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AutomationTool clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AutomationTool copyWith(void Function(AutomationTool) updates) =>
      super.copyWith((message) => updates(message as AutomationTool))
          as AutomationTool;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static AutomationTool create() => AutomationTool._();
  @$core.override
  AutomationTool createEmptyInstance() => create();
  static $pb.PbList<AutomationTool> createRepeated() =>
      $pb.PbList<AutomationTool>();
  @$core.pragma('dart2js:noInline')
  static AutomationTool getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<AutomationTool>(create);
  static AutomationTool? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get serverName => $_getSZ(0);
  @$pb.TagNumber(1)
  set serverName($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasServerName() => $_has(0);
  @$pb.TagNumber(1)
  void clearServerName() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get toolName => $_getSZ(1);
  @$pb.TagNumber(2)
  set toolName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasToolName() => $_has(1);
  @$pb.TagNumber(2)
  void clearToolName() => $_clearField(2);
}

/// Two shapes, both trivially checkable. A cron expression would be more
/// expressive and much harder to be sure about; nothing here needs it yet.
class AutomationSchedule extends $pb.GeneratedMessage {
  factory AutomationSchedule({
    AutomationScheduleKind? kind,
    $core.int? intervalMinutes,
    $core.int? dailyMinuteUtc,
  }) {
    final result = create();
    if (kind != null) result.kind = kind;
    if (intervalMinutes != null) result.intervalMinutes = intervalMinutes;
    if (dailyMinuteUtc != null) result.dailyMinuteUtc = dailyMinuteUtc;
    return result;
  }

  AutomationSchedule._();

  factory AutomationSchedule.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory AutomationSchedule.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'AutomationSchedule',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aE<AutomationScheduleKind>(1, _omitFieldNames ? '' : 'kind',
        enumValues: AutomationScheduleKind.values)
    ..aI(2, _omitFieldNames ? '' : 'intervalMinutes')
    ..aI(3, _omitFieldNames ? '' : 'dailyMinuteUtc')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AutomationSchedule clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AutomationSchedule copyWith(void Function(AutomationSchedule) updates) =>
      super.copyWith((message) => updates(message as AutomationSchedule))
          as AutomationSchedule;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static AutomationSchedule create() => AutomationSchedule._();
  @$core.override
  AutomationSchedule createEmptyInstance() => create();
  static $pb.PbList<AutomationSchedule> createRepeated() =>
      $pb.PbList<AutomationSchedule>();
  @$core.pragma('dart2js:noInline')
  static AutomationSchedule getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<AutomationSchedule>(create);
  static AutomationSchedule? _defaultInstance;

  @$pb.TagNumber(1)
  AutomationScheduleKind get kind => $_getN(0);
  @$pb.TagNumber(1)
  set kind(AutomationScheduleKind value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasKind() => $_has(0);
  @$pb.TagNumber(1)
  void clearKind() => $_clearField(1);

  /// INTERVAL: how often, in minutes.
  @$pb.TagNumber(2)
  $core.int get intervalMinutes => $_getIZ(1);
  @$pb.TagNumber(2)
  set intervalMinutes($core.int value) => $_setSignedInt32(1, value);
  @$pb.TagNumber(2)
  $core.bool hasIntervalMinutes() => $_has(1);
  @$pb.TagNumber(2)
  void clearIntervalMinutes() => $_clearField(2);

  /// DAILY: minutes past midnight, UTC. Stored in UTC rather than a local
  /// wall-clock time because the orchestrator has no reliable notion of the
  /// user's zone; clients convert for display.
  @$pb.TagNumber(3)
  $core.int get dailyMinuteUtc => $_getIZ(2);
  @$pb.TagNumber(3)
  set dailyMinuteUtc($core.int value) => $_setSignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasDailyMinuteUtc() => $_has(2);
  @$pb.TagNumber(3)
  void clearDailyMinuteUtc() => $_clearField(3);
}

class CreateAutomationRequest extends $pb.GeneratedMessage {
  factory CreateAutomationRequest({
    $core.String? name,
    $core.String? prompt,
    AutomationSchedule? schedule,
    $core.bool? enabled,
    $core.Iterable<AutomationTool>? allowedTools,
  }) {
    final result = create();
    if (name != null) result.name = name;
    if (prompt != null) result.prompt = prompt;
    if (schedule != null) result.schedule = schedule;
    if (enabled != null) result.enabled = enabled;
    if (allowedTools != null) result.allowedTools.addAll(allowedTools);
    return result;
  }

  CreateAutomationRequest._();

  factory CreateAutomationRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory CreateAutomationRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CreateAutomationRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'name')
    ..aOS(2, _omitFieldNames ? '' : 'prompt')
    ..aOM<AutomationSchedule>(3, _omitFieldNames ? '' : 'schedule',
        subBuilder: AutomationSchedule.create)
    ..aOB(4, _omitFieldNames ? '' : 'enabled')
    ..pPM<AutomationTool>(5, _omitFieldNames ? '' : 'allowedTools',
        subBuilder: AutomationTool.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateAutomationRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateAutomationRequest copyWith(
          void Function(CreateAutomationRequest) updates) =>
      super.copyWith((message) => updates(message as CreateAutomationRequest))
          as CreateAutomationRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static CreateAutomationRequest create() => CreateAutomationRequest._();
  @$core.override
  CreateAutomationRequest createEmptyInstance() => create();
  static $pb.PbList<CreateAutomationRequest> createRepeated() =>
      $pb.PbList<CreateAutomationRequest>();
  @$core.pragma('dart2js:noInline')
  static CreateAutomationRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CreateAutomationRequest>(create);
  static CreateAutomationRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get name => $_getSZ(0);
  @$pb.TagNumber(1)
  set name($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasName() => $_has(0);
  @$pb.TagNumber(1)
  void clearName() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get prompt => $_getSZ(1);
  @$pb.TagNumber(2)
  set prompt($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasPrompt() => $_has(1);
  @$pb.TagNumber(2)
  void clearPrompt() => $_clearField(2);

  @$pb.TagNumber(3)
  AutomationSchedule get schedule => $_getN(2);
  @$pb.TagNumber(3)
  set schedule(AutomationSchedule value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasSchedule() => $_has(2);
  @$pb.TagNumber(3)
  void clearSchedule() => $_clearField(3);
  @$pb.TagNumber(3)
  AutomationSchedule ensureSchedule() => $_ensure(2);

  @$pb.TagNumber(4)
  $core.bool get enabled => $_getBF(3);
  @$pb.TagNumber(4)
  set enabled($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasEnabled() => $_has(3);
  @$pb.TagNumber(4)
  void clearEnabled() => $_clearField(4);

  @$pb.TagNumber(5)
  $pb.PbList<AutomationTool> get allowedTools => $_getList(4);
}

class UpdateAutomationRequest extends $pb.GeneratedMessage {
  factory UpdateAutomationRequest({
    $core.String? automationId,
    $core.String? name,
    $core.String? prompt,
    AutomationSchedule? schedule,
    $core.Iterable<AutomationTool>? allowedTools,
  }) {
    final result = create();
    if (automationId != null) result.automationId = automationId;
    if (name != null) result.name = name;
    if (prompt != null) result.prompt = prompt;
    if (schedule != null) result.schedule = schedule;
    if (allowedTools != null) result.allowedTools.addAll(allowedTools);
    return result;
  }

  UpdateAutomationRequest._();

  factory UpdateAutomationRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory UpdateAutomationRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'UpdateAutomationRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'automationId')
    ..aOS(2, _omitFieldNames ? '' : 'name')
    ..aOS(3, _omitFieldNames ? '' : 'prompt')
    ..aOM<AutomationSchedule>(4, _omitFieldNames ? '' : 'schedule',
        subBuilder: AutomationSchedule.create)
    ..pPM<AutomationTool>(5, _omitFieldNames ? '' : 'allowedTools',
        subBuilder: AutomationTool.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateAutomationRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateAutomationRequest copyWith(
          void Function(UpdateAutomationRequest) updates) =>
      super.copyWith((message) => updates(message as UpdateAutomationRequest))
          as UpdateAutomationRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static UpdateAutomationRequest create() => UpdateAutomationRequest._();
  @$core.override
  UpdateAutomationRequest createEmptyInstance() => create();
  static $pb.PbList<UpdateAutomationRequest> createRepeated() =>
      $pb.PbList<UpdateAutomationRequest>();
  @$core.pragma('dart2js:noInline')
  static UpdateAutomationRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<UpdateAutomationRequest>(create);
  static UpdateAutomationRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get automationId => $_getSZ(0);
  @$pb.TagNumber(1)
  set automationId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAutomationId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAutomationId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get name => $_getSZ(1);
  @$pb.TagNumber(2)
  set name($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasName() => $_has(1);
  @$pb.TagNumber(2)
  void clearName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get prompt => $_getSZ(2);
  @$pb.TagNumber(3)
  set prompt($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasPrompt() => $_has(2);
  @$pb.TagNumber(3)
  void clearPrompt() => $_clearField(3);

  @$pb.TagNumber(4)
  AutomationSchedule get schedule => $_getN(3);
  @$pb.TagNumber(4)
  set schedule(AutomationSchedule value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasSchedule() => $_has(3);
  @$pb.TagNumber(4)
  void clearSchedule() => $_clearField(4);
  @$pb.TagNumber(4)
  AutomationSchedule ensureSchedule() => $_ensure(3);

  @$pb.TagNumber(5)
  $pb.PbList<AutomationTool> get allowedTools => $_getList(4);
}

class SetAutomationEnabledRequest extends $pb.GeneratedMessage {
  factory SetAutomationEnabledRequest({
    $core.String? automationId,
    $core.bool? enabled,
  }) {
    final result = create();
    if (automationId != null) result.automationId = automationId;
    if (enabled != null) result.enabled = enabled;
    return result;
  }

  SetAutomationEnabledRequest._();

  factory SetAutomationEnabledRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetAutomationEnabledRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetAutomationEnabledRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'automationId')
    ..aOB(2, _omitFieldNames ? '' : 'enabled')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetAutomationEnabledRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetAutomationEnabledRequest copyWith(
          void Function(SetAutomationEnabledRequest) updates) =>
      super.copyWith(
              (message) => updates(message as SetAutomationEnabledRequest))
          as SetAutomationEnabledRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetAutomationEnabledRequest create() =>
      SetAutomationEnabledRequest._();
  @$core.override
  SetAutomationEnabledRequest createEmptyInstance() => create();
  static $pb.PbList<SetAutomationEnabledRequest> createRepeated() =>
      $pb.PbList<SetAutomationEnabledRequest>();
  @$core.pragma('dart2js:noInline')
  static SetAutomationEnabledRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetAutomationEnabledRequest>(create);
  static SetAutomationEnabledRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get automationId => $_getSZ(0);
  @$pb.TagNumber(1)
  set automationId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAutomationId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAutomationId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get enabled => $_getBF(1);
  @$pb.TagNumber(2)
  set enabled($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasEnabled() => $_has(1);
  @$pb.TagNumber(2)
  void clearEnabled() => $_clearField(2);
}

class DeleteAutomationRequest extends $pb.GeneratedMessage {
  factory DeleteAutomationRequest({
    $core.String? automationId,
  }) {
    final result = create();
    if (automationId != null) result.automationId = automationId;
    return result;
  }

  DeleteAutomationRequest._();

  factory DeleteAutomationRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DeleteAutomationRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DeleteAutomationRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'automationId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteAutomationRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteAutomationRequest copyWith(
          void Function(DeleteAutomationRequest) updates) =>
      super.copyWith((message) => updates(message as DeleteAutomationRequest))
          as DeleteAutomationRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DeleteAutomationRequest create() => DeleteAutomationRequest._();
  @$core.override
  DeleteAutomationRequest createEmptyInstance() => create();
  static $pb.PbList<DeleteAutomationRequest> createRepeated() =>
      $pb.PbList<DeleteAutomationRequest>();
  @$core.pragma('dart2js:noInline')
  static DeleteAutomationRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DeleteAutomationRequest>(create);
  static DeleteAutomationRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get automationId => $_getSZ(0);
  @$pb.TagNumber(1)
  set automationId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAutomationId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAutomationId() => $_clearField(1);
}

class DeleteAutomationResponse extends $pb.GeneratedMessage {
  factory DeleteAutomationResponse() => create();

  DeleteAutomationResponse._();

  factory DeleteAutomationResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DeleteAutomationResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DeleteAutomationResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteAutomationResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteAutomationResponse copyWith(
          void Function(DeleteAutomationResponse) updates) =>
      super.copyWith((message) => updates(message as DeleteAutomationResponse))
          as DeleteAutomationResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DeleteAutomationResponse create() => DeleteAutomationResponse._();
  @$core.override
  DeleteAutomationResponse createEmptyInstance() => create();
  static $pb.PbList<DeleteAutomationResponse> createRepeated() =>
      $pb.PbList<DeleteAutomationResponse>();
  @$core.pragma('dart2js:noInline')
  static DeleteAutomationResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DeleteAutomationResponse>(create);
  static DeleteAutomationResponse? _defaultInstance;
}

class ListAutomationsRequest extends $pb.GeneratedMessage {
  factory ListAutomationsRequest() => create();

  ListAutomationsRequest._();

  factory ListAutomationsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListAutomationsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListAutomationsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAutomationsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAutomationsRequest copyWith(
          void Function(ListAutomationsRequest) updates) =>
      super.copyWith((message) => updates(message as ListAutomationsRequest))
          as ListAutomationsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListAutomationsRequest create() => ListAutomationsRequest._();
  @$core.override
  ListAutomationsRequest createEmptyInstance() => create();
  static $pb.PbList<ListAutomationsRequest> createRepeated() =>
      $pb.PbList<ListAutomationsRequest>();
  @$core.pragma('dart2js:noInline')
  static ListAutomationsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListAutomationsRequest>(create);
  static ListAutomationsRequest? _defaultInstance;
}

class ListAutomationsResponse extends $pb.GeneratedMessage {
  factory ListAutomationsResponse({
    $core.Iterable<Automation>? automations,
  }) {
    final result = create();
    if (automations != null) result.automations.addAll(automations);
    return result;
  }

  ListAutomationsResponse._();

  factory ListAutomationsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListAutomationsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListAutomationsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..pPM<Automation>(1, _omitFieldNames ? '' : 'automations',
        subBuilder: Automation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAutomationsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAutomationsResponse copyWith(
          void Function(ListAutomationsResponse) updates) =>
      super.copyWith((message) => updates(message as ListAutomationsResponse))
          as ListAutomationsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListAutomationsResponse create() => ListAutomationsResponse._();
  @$core.override
  ListAutomationsResponse createEmptyInstance() => create();
  static $pb.PbList<ListAutomationsResponse> createRepeated() =>
      $pb.PbList<ListAutomationsResponse>();
  @$core.pragma('dart2js:noInline')
  static ListAutomationsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListAutomationsResponse>(create);
  static ListAutomationsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<Automation> get automations => $_getList(0);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
