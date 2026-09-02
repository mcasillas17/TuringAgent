// This is a generated file - do not edit.
//
// Generated from turing/v1/telemetry.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import '../../google/protobuf/timestamp.pb.dart' as $1;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

/// A closed window of time the report covers. Sent back with the report so a
/// client renders the window the server actually used rather than the one it
/// asked for, and so a screenshot of the numbers still says what they cover.
class TelemetryWindow extends $pb.GeneratedMessage {
  factory TelemetryWindow({
    $core.int? days,
    $1.Timestamp? start,
    $1.Timestamp? end,
  }) {
    final result = create();
    if (days != null) result.days = days;
    if (start != null) result.start = start;
    if (end != null) result.end = end;
    return result;
  }

  TelemetryWindow._();

  factory TelemetryWindow.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory TelemetryWindow.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'TelemetryWindow',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aI(1, _omitFieldNames ? '' : 'days')
    ..aOM<$1.Timestamp>(2, _omitFieldNames ? '' : 'start',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(3, _omitFieldNames ? '' : 'end',
        subBuilder: $1.Timestamp.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TelemetryWindow clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TelemetryWindow copyWith(void Function(TelemetryWindow) updates) =>
      super.copyWith((message) => updates(message as TelemetryWindow))
          as TelemetryWindow;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static TelemetryWindow create() => TelemetryWindow._();
  @$core.override
  TelemetryWindow createEmptyInstance() => create();
  static $pb.PbList<TelemetryWindow> createRepeated() =>
      $pb.PbList<TelemetryWindow>();
  @$core.pragma('dart2js:noInline')
  static TelemetryWindow getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<TelemetryWindow>(create);
  static TelemetryWindow? _defaultInstance;

  @$pb.TagNumber(1)
  $core.int get days => $_getIZ(0);
  @$pb.TagNumber(1)
  set days($core.int value) => $_setSignedInt32(0, value);
  @$pb.TagNumber(1)
  $core.bool hasDays() => $_has(0);
  @$pb.TagNumber(1)
  void clearDays() => $_clearField(1);

  /// Inclusive lower bound, snapped to the start of a UTC day so that "N days"
  /// means today plus the N-1 before it, and the daily series has exactly N
  /// buckets.
  @$pb.TagNumber(2)
  $1.Timestamp get start => $_getN(1);
  @$pb.TagNumber(2)
  set start($1.Timestamp value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasStart() => $_has(1);
  @$pb.TagNumber(2)
  void clearStart() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.Timestamp ensureStart() => $_ensure(1);

  /// Exclusive upper bound: the instant the report was computed.
  @$pb.TagNumber(3)
  $1.Timestamp get end => $_getN(2);
  @$pb.TagNumber(3)
  set end($1.Timestamp value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasEnd() => $_has(2);
  @$pb.TagNumber(3)
  void clearEnd() => $_clearField(3);
  @$pb.TagNumber(3)
  $1.Timestamp ensureEnd() => $_ensure(2);
}

/// Runs started in the window, by outcome.
///
/// `in_flight` is every run that has not reached a terminal status yet —
/// queued, running or waiting on an approval. It is reported separately rather
/// than folded into a failure count, because a run that has not finished has
/// not failed.
class RunTotals extends $pb.GeneratedMessage {
  factory RunTotals({
    $fixnum.Int64? total,
    $fixnum.Int64? completed,
    $fixnum.Int64? failed,
    $fixnum.Int64? cancelled,
    $fixnum.Int64? inFlight,
    $fixnum.Int64? averageDurationMs,
  }) {
    final result = create();
    if (total != null) result.total = total;
    if (completed != null) result.completed = completed;
    if (failed != null) result.failed = failed;
    if (cancelled != null) result.cancelled = cancelled;
    if (inFlight != null) result.inFlight = inFlight;
    if (averageDurationMs != null) result.averageDurationMs = averageDurationMs;
    return result;
  }

  RunTotals._();

  factory RunTotals.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RunTotals.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RunTotals',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aInt64(1, _omitFieldNames ? '' : 'total')
    ..aInt64(2, _omitFieldNames ? '' : 'completed')
    ..aInt64(3, _omitFieldNames ? '' : 'failed')
    ..aInt64(4, _omitFieldNames ? '' : 'cancelled')
    ..aInt64(5, _omitFieldNames ? '' : 'inFlight')
    ..aInt64(6, _omitFieldNames ? '' : 'averageDurationMs')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunTotals clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunTotals copyWith(void Function(RunTotals) updates) =>
      super.copyWith((message) => updates(message as RunTotals)) as RunTotals;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RunTotals create() => RunTotals._();
  @$core.override
  RunTotals createEmptyInstance() => create();
  static $pb.PbList<RunTotals> createRepeated() => $pb.PbList<RunTotals>();
  @$core.pragma('dart2js:noInline')
  static RunTotals getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<RunTotals>(create);
  static RunTotals? _defaultInstance;

  @$pb.TagNumber(1)
  $fixnum.Int64 get total => $_getI64(0);
  @$pb.TagNumber(1)
  set total($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasTotal() => $_has(0);
  @$pb.TagNumber(1)
  void clearTotal() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get completed => $_getI64(1);
  @$pb.TagNumber(2)
  set completed($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCompleted() => $_has(1);
  @$pb.TagNumber(2)
  void clearCompleted() => $_clearField(2);

  @$pb.TagNumber(3)
  $fixnum.Int64 get failed => $_getI64(2);
  @$pb.TagNumber(3)
  set failed($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasFailed() => $_has(2);
  @$pb.TagNumber(3)
  void clearFailed() => $_clearField(3);

  @$pb.TagNumber(4)
  $fixnum.Int64 get cancelled => $_getI64(3);
  @$pb.TagNumber(4)
  set cancelled($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasCancelled() => $_has(3);
  @$pb.TagNumber(4)
  void clearCancelled() => $_clearField(4);

  @$pb.TagNumber(5)
  $fixnum.Int64 get inFlight => $_getI64(4);
  @$pb.TagNumber(5)
  set inFlight($fixnum.Int64 value) => $_setInt64(4, value);
  @$pb.TagNumber(5)
  $core.bool hasInFlight() => $_has(4);
  @$pb.TagNumber(5)
  void clearInFlight() => $_clearField(5);

  /// DERIVED: the mean of finished_at - started_at over runs in the window that
  /// recorded both. Absent when no run in the window did, which is not the same
  /// as a mean of zero.
  @$pb.TagNumber(6)
  $fixnum.Int64 get averageDurationMs => $_getI64(5);
  @$pb.TagNumber(6)
  set averageDurationMs($fixnum.Int64 value) => $_setInt64(5, value);
  @$pb.TagNumber(6)
  $core.bool hasAverageDurationMs() => $_has(5);
  @$pb.TagNumber(6)
  void clearAverageDurationMs() => $_clearField(6);
}

/// Token usage, and how much of the work it actually describes.
///
/// The two counters exist because the totals are honest only alongside them. A
/// provider that reports nothing contributes no tokens and no error — it simply
/// does not appear in the sum — so a total read without `runs_without_usage`
/// beside it silently understates by an unknown amount.
class TokenTotals extends $pb.GeneratedMessage {
  factory TokenTotals({
    $fixnum.Int64? inputTokens,
    $fixnum.Int64? outputTokens,
    $fixnum.Int64? runsWithUsage,
    $fixnum.Int64? runsWithoutUsage,
  }) {
    final result = create();
    if (inputTokens != null) result.inputTokens = inputTokens;
    if (outputTokens != null) result.outputTokens = outputTokens;
    if (runsWithUsage != null) result.runsWithUsage = runsWithUsage;
    if (runsWithoutUsage != null) result.runsWithoutUsage = runsWithoutUsage;
    return result;
  }

  TokenTotals._();

  factory TokenTotals.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory TokenTotals.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'TokenTotals',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aInt64(1, _omitFieldNames ? '' : 'inputTokens')
    ..aInt64(2, _omitFieldNames ? '' : 'outputTokens')
    ..aInt64(3, _omitFieldNames ? '' : 'runsWithUsage')
    ..aInt64(4, _omitFieldNames ? '' : 'runsWithoutUsage')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TokenTotals clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TokenTotals copyWith(void Function(TokenTotals) updates) =>
      super.copyWith((message) => updates(message as TokenTotals))
          as TokenTotals;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static TokenTotals create() => TokenTotals._();
  @$core.override
  TokenTotals createEmptyInstance() => create();
  static $pb.PbList<TokenTotals> createRepeated() => $pb.PbList<TokenTotals>();
  @$core.pragma('dart2js:noInline')
  static TokenTotals getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<TokenTotals>(create);
  static TokenTotals? _defaultInstance;

  /// MEASURED, summed over the runs whose provider reported usage. Absent when
  /// no run in the window reported any.
  @$pb.TagNumber(1)
  $fixnum.Int64 get inputTokens => $_getI64(0);
  @$pb.TagNumber(1)
  set inputTokens($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasInputTokens() => $_has(0);
  @$pb.TagNumber(1)
  void clearInputTokens() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get outputTokens => $_getI64(1);
  @$pb.TagNumber(2)
  set outputTokens($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasOutputTokens() => $_has(1);
  @$pb.TagNumber(2)
  void clearOutputTokens() => $_clearField(2);

  /// Finished runs in the window whose provider reported token usage.
  @$pb.TagNumber(3)
  $fixnum.Int64 get runsWithUsage => $_getI64(2);
  @$pb.TagNumber(3)
  set runsWithUsage($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasRunsWithUsage() => $_has(2);
  @$pb.TagNumber(3)
  void clearRunsWithUsage() => $_clearField(3);

  /// Finished runs in the window whose provider reported none. Their token
  /// count is unknown; it is never estimated.
  @$pb.TagNumber(4)
  $fixnum.Int64 get runsWithoutUsage => $_getI64(3);
  @$pb.TagNumber(4)
  set runsWithoutUsage($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasRunsWithoutUsage() => $_has(3);
  @$pb.TagNumber(4)
  void clearRunsWithoutUsage() => $_clearField(4);
}

/// One tool, and how it was used. Keyed by (server, tool) to match the
/// orchestrator's own policy lookup: a same-named tool on another server is a
/// different tool.
class ToolUsage extends $pb.GeneratedMessage {
  factory ToolUsage({
    $core.String? serverName,
    $core.String? toolName,
    $fixnum.Int64? calls,
    $fixnum.Int64? failed,
    $fixnum.Int64? denied,
    $fixnum.Int64? averageDurationMs,
  }) {
    final result = create();
    if (serverName != null) result.serverName = serverName;
    if (toolName != null) result.toolName = toolName;
    if (calls != null) result.calls = calls;
    if (failed != null) result.failed = failed;
    if (denied != null) result.denied = denied;
    if (averageDurationMs != null) result.averageDurationMs = averageDurationMs;
    return result;
  }

  ToolUsage._();

  factory ToolUsage.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ToolUsage.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ToolUsage',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'serverName')
    ..aOS(2, _omitFieldNames ? '' : 'toolName')
    ..aInt64(3, _omitFieldNames ? '' : 'calls')
    ..aInt64(4, _omitFieldNames ? '' : 'failed')
    ..aInt64(5, _omitFieldNames ? '' : 'denied')
    ..aInt64(6, _omitFieldNames ? '' : 'averageDurationMs')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ToolUsage clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ToolUsage copyWith(void Function(ToolUsage) updates) =>
      super.copyWith((message) => updates(message as ToolUsage)) as ToolUsage;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ToolUsage create() => ToolUsage._();
  @$core.override
  ToolUsage createEmptyInstance() => create();
  static $pb.PbList<ToolUsage> createRepeated() => $pb.PbList<ToolUsage>();
  @$core.pragma('dart2js:noInline')
  static ToolUsage getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ToolUsage>(create);
  static ToolUsage? _defaultInstance;

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

  @$pb.TagNumber(3)
  $fixnum.Int64 get calls => $_getI64(2);
  @$pb.TagNumber(3)
  set calls($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasCalls() => $_has(2);
  @$pb.TagNumber(3)
  void clearCalls() => $_clearField(3);

  @$pb.TagNumber(4)
  $fixnum.Int64 get failed => $_getI64(3);
  @$pb.TagNumber(4)
  set failed($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasFailed() => $_has(3);
  @$pb.TagNumber(4)
  void clearFailed() => $_clearField(4);

  @$pb.TagNumber(5)
  $fixnum.Int64 get denied => $_getI64(4);
  @$pb.TagNumber(5)
  set denied($fixnum.Int64 value) => $_setInt64(4, value);
  @$pb.TagNumber(5)
  $core.bool hasDenied() => $_has(4);
  @$pb.TagNumber(5)
  void clearDenied() => $_clearField(5);

  /// DERIVED: mean over the calls that recorded a duration. Absent when none
  /// did — a tool whose calls were all denied never ran, and reporting 0 ms for
  /// it would claim it did.
  @$pb.TagNumber(6)
  $fixnum.Int64 get averageDurationMs => $_getI64(5);
  @$pb.TagNumber(6)
  set averageDurationMs($fixnum.Int64 value) => $_setInt64(5, value);
  @$pb.TagNumber(6)
  $core.bool hasAverageDurationMs() => $_has(5);
  @$pb.TagNumber(6)
  void clearAverageDurationMs() => $_clearField(6);
}

/// One (provider, model) pair the runs in the window used. `provider` and
/// `model` are the strings the run was recorded with, not an enum, so a model
/// pulled after this build still reports under its own name.
class ModelUsage extends $pb.GeneratedMessage {
  factory ModelUsage({
    $core.String? provider,
    $core.String? model,
    $fixnum.Int64? runs,
    $fixnum.Int64? inputTokens,
    $fixnum.Int64? outputTokens,
    $fixnum.Int64? runsWithoutUsage,
  }) {
    final result = create();
    if (provider != null) result.provider = provider;
    if (model != null) result.model = model;
    if (runs != null) result.runs = runs;
    if (inputTokens != null) result.inputTokens = inputTokens;
    if (outputTokens != null) result.outputTokens = outputTokens;
    if (runsWithoutUsage != null) result.runsWithoutUsage = runsWithoutUsage;
    return result;
  }

  ModelUsage._();

  factory ModelUsage.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ModelUsage.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ModelUsage',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'provider')
    ..aOS(2, _omitFieldNames ? '' : 'model')
    ..aInt64(3, _omitFieldNames ? '' : 'runs')
    ..aInt64(4, _omitFieldNames ? '' : 'inputTokens')
    ..aInt64(5, _omitFieldNames ? '' : 'outputTokens')
    ..aInt64(6, _omitFieldNames ? '' : 'runsWithoutUsage')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ModelUsage clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ModelUsage copyWith(void Function(ModelUsage) updates) =>
      super.copyWith((message) => updates(message as ModelUsage)) as ModelUsage;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ModelUsage create() => ModelUsage._();
  @$core.override
  ModelUsage createEmptyInstance() => create();
  static $pb.PbList<ModelUsage> createRepeated() => $pb.PbList<ModelUsage>();
  @$core.pragma('dart2js:noInline')
  static ModelUsage getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ModelUsage>(create);
  static ModelUsage? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get provider => $_getSZ(0);
  @$pb.TagNumber(1)
  set provider($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasProvider() => $_has(0);
  @$pb.TagNumber(1)
  void clearProvider() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get model => $_getSZ(1);
  @$pb.TagNumber(2)
  set model($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasModel() => $_has(1);
  @$pb.TagNumber(2)
  void clearModel() => $_clearField(2);

  @$pb.TagNumber(3)
  $fixnum.Int64 get runs => $_getI64(2);
  @$pb.TagNumber(3)
  set runs($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasRuns() => $_has(2);
  @$pb.TagNumber(3)
  void clearRuns() => $_clearField(3);

  @$pb.TagNumber(4)
  $fixnum.Int64 get inputTokens => $_getI64(3);
  @$pb.TagNumber(4)
  set inputTokens($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasInputTokens() => $_has(3);
  @$pb.TagNumber(4)
  void clearInputTokens() => $_clearField(4);

  @$pb.TagNumber(5)
  $fixnum.Int64 get outputTokens => $_getI64(4);
  @$pb.TagNumber(5)
  set outputTokens($fixnum.Int64 value) => $_setInt64(4, value);
  @$pb.TagNumber(5)
  $core.bool hasOutputTokens() => $_has(4);
  @$pb.TagNumber(5)
  void clearOutputTokens() => $_clearField(5);

  /// Runs in this group with no recorded token usage — the same denominator as
  /// `runs`, whatever their status. Counting only completed ones would let a
  /// group of 40 runs where 12 failed report a token total covering 28 and
  /// nothing without usage, which reads as a complete measurement of all 40.
  @$pb.TagNumber(6)
  $fixnum.Int64 get runsWithoutUsage => $_getI64(5);
  @$pb.TagNumber(6)
  set runsWithoutUsage($fixnum.Int64 value) => $_setInt64(5, value);
  @$pb.TagNumber(6)
  $core.bool hasRunsWithoutUsage() => $_has(5);
  @$pb.TagNumber(6)
  void clearRunsWithoutUsage() => $_clearField(6);
}

/// What left the machine, and where it went.
///
/// Recorded per run at the moment the message was accepted, not looked up from
/// the conversation's current destination: re-pointing or deleting an agent
/// afterwards must not rewrite the record of where earlier messages were sent.
class ExternalAgentUsage extends $pb.GeneratedMessage {
  factory ExternalAgentUsage({
    $core.String? displayName,
    $core.String? endpointHost,
    $fixnum.Int64? runs,
    $fixnum.Int64? inputTokens,
    $fixnum.Int64? outputTokens,
    $fixnum.Int64? runsWithoutUsage,
  }) {
    final result = create();
    if (displayName != null) result.displayName = displayName;
    if (endpointHost != null) result.endpointHost = endpointHost;
    if (runs != null) result.runs = runs;
    if (inputTokens != null) result.inputTokens = inputTokens;
    if (outputTokens != null) result.outputTokens = outputTokens;
    if (runsWithoutUsage != null) result.runsWithoutUsage = runsWithoutUsage;
    return result;
  }

  ExternalAgentUsage._();

  factory ExternalAgentUsage.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ExternalAgentUsage.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ExternalAgentUsage',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'displayName')
    ..aOS(2, _omitFieldNames ? '' : 'endpointHost')
    ..aInt64(3, _omitFieldNames ? '' : 'runs')
    ..aInt64(4, _omitFieldNames ? '' : 'inputTokens')
    ..aInt64(5, _omitFieldNames ? '' : 'outputTokens')
    ..aInt64(6, _omitFieldNames ? '' : 'runsWithoutUsage')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ExternalAgentUsage clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ExternalAgentUsage copyWith(void Function(ExternalAgentUsage) updates) =>
      super.copyWith((message) => updates(message as ExternalAgentUsage))
          as ExternalAgentUsage;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ExternalAgentUsage create() => ExternalAgentUsage._();
  @$core.override
  ExternalAgentUsage createEmptyInstance() => create();
  static $pb.PbList<ExternalAgentUsage> createRepeated() =>
      $pb.PbList<ExternalAgentUsage>();
  @$core.pragma('dart2js:noInline')
  static ExternalAgentUsage getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ExternalAgentUsage>(create);
  static ExternalAgentUsage? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get displayName => $_getSZ(0);
  @$pb.TagNumber(1)
  set displayName($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasDisplayName() => $_has(0);
  @$pb.TagNumber(1)
  void clearDisplayName() => $_clearField(1);

  /// Host only — never the full URL, never a path, never a credential.
  @$pb.TagNumber(2)
  $core.String get endpointHost => $_getSZ(1);
  @$pb.TagNumber(2)
  set endpointHost($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasEndpointHost() => $_has(1);
  @$pb.TagNumber(2)
  void clearEndpointHost() => $_clearField(2);

  @$pb.TagNumber(3)
  $fixnum.Int64 get runs => $_getI64(2);
  @$pb.TagNumber(3)
  set runs($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasRuns() => $_has(2);
  @$pb.TagNumber(3)
  void clearRuns() => $_clearField(3);

  @$pb.TagNumber(4)
  $fixnum.Int64 get inputTokens => $_getI64(3);
  @$pb.TagNumber(4)
  set inputTokens($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasInputTokens() => $_has(3);
  @$pb.TagNumber(4)
  void clearInputTokens() => $_clearField(4);

  @$pb.TagNumber(5)
  $fixnum.Int64 get outputTokens => $_getI64(4);
  @$pb.TagNumber(5)
  set outputTokens($fixnum.Int64 value) => $_setInt64(4, value);
  @$pb.TagNumber(5)
  $core.bool hasOutputTokens() => $_has(4);
  @$pb.TagNumber(5)
  void clearOutputTokens() => $_clearField(5);

  /// Runs in this group with no recorded token usage — the same denominator as
  /// `runs`, whatever their status. Counting only completed ones would let a
  /// group of 40 runs where 12 failed report a token total covering 28 and
  /// nothing without usage, which reads as a complete measurement of all 40.
  @$pb.TagNumber(6)
  $fixnum.Int64 get runsWithoutUsage => $_getI64(5);
  @$pb.TagNumber(6)
  set runsWithoutUsage($fixnum.Int64 value) => $_setInt64(5, value);
  @$pb.TagNumber(6)
  $core.bool hasRunsWithoutUsage() => $_has(5);
  @$pb.TagNumber(6)
  void clearRunsWithoutUsage() => $_clearField(6);
}

/// Runs nobody was present for, and the approvals they granted themselves.
class AutomationTotals extends $pb.GeneratedMessage {
  factory AutomationTotals({
    $fixnum.Int64? runs,
    $fixnum.Int64? completed,
    $fixnum.Int64? failed,
    $fixnum.Int64? unattendedApprovals,
  }) {
    final result = create();
    if (runs != null) result.runs = runs;
    if (completed != null) result.completed = completed;
    if (failed != null) result.failed = failed;
    if (unattendedApprovals != null)
      result.unattendedApprovals = unattendedApprovals;
    return result;
  }

  AutomationTotals._();

  factory AutomationTotals.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory AutomationTotals.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'AutomationTotals',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aInt64(1, _omitFieldNames ? '' : 'runs')
    ..aInt64(2, _omitFieldNames ? '' : 'completed')
    ..aInt64(3, _omitFieldNames ? '' : 'failed')
    ..aInt64(4, _omitFieldNames ? '' : 'unattendedApprovals')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AutomationTotals clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AutomationTotals copyWith(void Function(AutomationTotals) updates) =>
      super.copyWith((message) => updates(message as AutomationTotals))
          as AutomationTotals;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static AutomationTotals create() => AutomationTotals._();
  @$core.override
  AutomationTotals createEmptyInstance() => create();
  static $pb.PbList<AutomationTotals> createRepeated() =>
      $pb.PbList<AutomationTotals>();
  @$core.pragma('dart2js:noInline')
  static AutomationTotals getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<AutomationTotals>(create);
  static AutomationTotals? _defaultInstance;

  @$pb.TagNumber(1)
  $fixnum.Int64 get runs => $_getI64(0);
  @$pb.TagNumber(1)
  set runs($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRuns() => $_has(0);
  @$pb.TagNumber(1)
  void clearRuns() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get completed => $_getI64(1);
  @$pb.TagNumber(2)
  set completed($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCompleted() => $_has(1);
  @$pb.TagNumber(2)
  void clearCompleted() => $_clearField(2);

  @$pb.TagNumber(3)
  $fixnum.Int64 get failed => $_getI64(2);
  @$pb.TagNumber(3)
  set failed($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasFailed() => $_has(2);
  @$pb.TagNumber(3)
  void clearFailed() => $_clearField(3);

  /// Approvals decided by an automation's allowlist rather than by a person.
  /// Counted because consent given in advance is weaker than consent given in
  /// the moment, and the weaker kind should be visible.
  @$pb.TagNumber(4)
  $fixnum.Int64 get unattendedApprovals => $_getI64(3);
  @$pb.TagNumber(4)
  set unattendedApprovals($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasUnattendedApprovals() => $_has(3);
  @$pb.TagNumber(4)
  void clearUnattendedApprovals() => $_clearField(4);
}

/// Connected third-party accounts.
///
/// NOT WINDOWED and deliberately not a usage count: nothing in a run reads a
/// connection yet, so there is no usage to measure. These are current state,
/// and the client labels them as such rather than letting them read as activity
/// during the window.
class IntegrationTotals extends $pb.GeneratedMessage {
  factory IntegrationTotals({
    $fixnum.Int64? connected,
    $fixnum.Int64? revoked,
  }) {
    final result = create();
    if (connected != null) result.connected = connected;
    if (revoked != null) result.revoked = revoked;
    return result;
  }

  IntegrationTotals._();

  factory IntegrationTotals.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory IntegrationTotals.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'IntegrationTotals',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aInt64(1, _omitFieldNames ? '' : 'connected')
    ..aInt64(2, _omitFieldNames ? '' : 'revoked')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  IntegrationTotals clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  IntegrationTotals copyWith(void Function(IntegrationTotals) updates) =>
      super.copyWith((message) => updates(message as IntegrationTotals))
          as IntegrationTotals;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static IntegrationTotals create() => IntegrationTotals._();
  @$core.override
  IntegrationTotals createEmptyInstance() => create();
  static $pb.PbList<IntegrationTotals> createRepeated() =>
      $pb.PbList<IntegrationTotals>();
  @$core.pragma('dart2js:noInline')
  static IntegrationTotals getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<IntegrationTotals>(create);
  static IntegrationTotals? _defaultInstance;

  @$pb.TagNumber(1)
  $fixnum.Int64 get connected => $_getI64(0);
  @$pb.TagNumber(1)
  set connected($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasConnected() => $_has(0);
  @$pb.TagNumber(1)
  void clearConnected() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get revoked => $_getI64(1);
  @$pb.TagNumber(2)
  set revoked($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRevoked() => $_has(1);
  @$pb.TagNumber(2)
  void clearRevoked() => $_clearField(2);
}

/// One UTC day inside the window. Days with no activity are present with zeroes
/// so a client can draw a continuous axis without inventing the gaps.
///
/// The window starts on a UTC day boundary, so a request for N days returns
/// exactly N entries and the label over a chart matches the bars under it. The
/// LAST entry is the day still in progress and is expected to read low.
class DailyActivity extends $pb.GeneratedMessage {
  factory DailyActivity({
    $core.String? date,
    $fixnum.Int64? runs,
    $fixnum.Int64? toolCalls,
    $fixnum.Int64? inputTokens,
    $fixnum.Int64? outputTokens,
  }) {
    final result = create();
    if (date != null) result.date = date;
    if (runs != null) result.runs = runs;
    if (toolCalls != null) result.toolCalls = toolCalls;
    if (inputTokens != null) result.inputTokens = inputTokens;
    if (outputTokens != null) result.outputTokens = outputTokens;
    return result;
  }

  DailyActivity._();

  factory DailyActivity.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DailyActivity.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DailyActivity',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'date')
    ..aInt64(2, _omitFieldNames ? '' : 'runs')
    ..aInt64(3, _omitFieldNames ? '' : 'toolCalls')
    ..aInt64(4, _omitFieldNames ? '' : 'inputTokens')
    ..aInt64(5, _omitFieldNames ? '' : 'outputTokens')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DailyActivity clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DailyActivity copyWith(void Function(DailyActivity) updates) =>
      super.copyWith((message) => updates(message as DailyActivity))
          as DailyActivity;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DailyActivity create() => DailyActivity._();
  @$core.override
  DailyActivity createEmptyInstance() => create();
  static $pb.PbList<DailyActivity> createRepeated() =>
      $pb.PbList<DailyActivity>();
  @$core.pragma('dart2js:noInline')
  static DailyActivity getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DailyActivity>(create);
  static DailyActivity? _defaultInstance;

  /// YYYY-MM-DD, UTC. UTC rather than local time because that is what the rows
  /// are stored in; converting would move activity across day boundaries in a
  /// way no stored value could justify.
  @$pb.TagNumber(1)
  $core.String get date => $_getSZ(0);
  @$pb.TagNumber(1)
  set date($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasDate() => $_has(0);
  @$pb.TagNumber(1)
  void clearDate() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get runs => $_getI64(1);
  @$pb.TagNumber(2)
  set runs($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRuns() => $_has(1);
  @$pb.TagNumber(2)
  void clearRuns() => $_clearField(2);

  @$pb.TagNumber(3)
  $fixnum.Int64 get toolCalls => $_getI64(2);
  @$pb.TagNumber(3)
  set toolCalls($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasToolCalls() => $_has(2);
  @$pb.TagNumber(3)
  void clearToolCalls() => $_clearField(3);

  @$pb.TagNumber(4)
  $fixnum.Int64 get inputTokens => $_getI64(3);
  @$pb.TagNumber(4)
  set inputTokens($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasInputTokens() => $_has(3);
  @$pb.TagNumber(4)
  void clearInputTokens() => $_clearField(4);

  @$pb.TagNumber(5)
  $fixnum.Int64 get outputTokens => $_getI64(4);
  @$pb.TagNumber(5)
  set outputTokens($fixnum.Int64 value) => $_setInt64(4, value);
  @$pb.TagNumber(5)
  $core.bool hasOutputTokens() => $_has(4);
  @$pb.TagNumber(5)
  void clearOutputTokens() => $_clearField(5);
}

class GetTelemetrySummaryRequest extends $pb.GeneratedMessage {
  factory GetTelemetrySummaryRequest({
    $core.int? windowDays,
  }) {
    final result = create();
    if (windowDays != null) result.windowDays = windowDays;
    return result;
  }

  GetTelemetrySummaryRequest._();

  factory GetTelemetrySummaryRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetTelemetrySummaryRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetTelemetrySummaryRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aI(1, _omitFieldNames ? '' : 'windowDays')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetTelemetrySummaryRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetTelemetrySummaryRequest copyWith(
          void Function(GetTelemetrySummaryRequest) updates) =>
      super.copyWith(
              (message) => updates(message as GetTelemetrySummaryRequest))
          as GetTelemetrySummaryRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetTelemetrySummaryRequest create() => GetTelemetrySummaryRequest._();
  @$core.override
  GetTelemetrySummaryRequest createEmptyInstance() => create();
  static $pb.PbList<GetTelemetrySummaryRequest> createRepeated() =>
      $pb.PbList<GetTelemetrySummaryRequest>();
  @$core.pragma('dart2js:noInline')
  static GetTelemetrySummaryRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetTelemetrySummaryRequest>(create);
  static GetTelemetrySummaryRequest? _defaultInstance;

  /// Whole days back from now. Must be between 1 and 365; the server clamps
  /// nothing and rejects out-of-range values, so a client never renders a
  /// window different from the one it asked for without being told.
  @$pb.TagNumber(1)
  $core.int get windowDays => $_getIZ(0);
  @$pb.TagNumber(1)
  set windowDays($core.int value) => $_setSignedInt32(0, value);
  @$pb.TagNumber(1)
  $core.bool hasWindowDays() => $_has(0);
  @$pb.TagNumber(1)
  void clearWindowDays() => $_clearField(1);
}

class GetTelemetrySummaryResponse extends $pb.GeneratedMessage {
  factory GetTelemetrySummaryResponse({
    TelemetryWindow? window,
    RunTotals? runs,
    TokenTotals? tokens,
    $core.Iterable<ToolUsage>? tools,
    $core.Iterable<ModelUsage>? models,
    $core.Iterable<ExternalAgentUsage>? externalAgents,
    AutomationTotals? automations,
    IntegrationTotals? integrations,
    $core.Iterable<DailyActivity>? daily,
  }) {
    final result = create();
    if (window != null) result.window = window;
    if (runs != null) result.runs = runs;
    if (tokens != null) result.tokens = tokens;
    if (tools != null) result.tools.addAll(tools);
    if (models != null) result.models.addAll(models);
    if (externalAgents != null) result.externalAgents.addAll(externalAgents);
    if (automations != null) result.automations = automations;
    if (integrations != null) result.integrations = integrations;
    if (daily != null) result.daily.addAll(daily);
    return result;
  }

  GetTelemetrySummaryResponse._();

  factory GetTelemetrySummaryResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetTelemetrySummaryResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetTelemetrySummaryResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOM<TelemetryWindow>(1, _omitFieldNames ? '' : 'window',
        subBuilder: TelemetryWindow.create)
    ..aOM<RunTotals>(2, _omitFieldNames ? '' : 'runs',
        subBuilder: RunTotals.create)
    ..aOM<TokenTotals>(3, _omitFieldNames ? '' : 'tokens',
        subBuilder: TokenTotals.create)
    ..pPM<ToolUsage>(4, _omitFieldNames ? '' : 'tools',
        subBuilder: ToolUsage.create)
    ..pPM<ModelUsage>(5, _omitFieldNames ? '' : 'models',
        subBuilder: ModelUsage.create)
    ..pPM<ExternalAgentUsage>(6, _omitFieldNames ? '' : 'externalAgents',
        subBuilder: ExternalAgentUsage.create)
    ..aOM<AutomationTotals>(7, _omitFieldNames ? '' : 'automations',
        subBuilder: AutomationTotals.create)
    ..aOM<IntegrationTotals>(8, _omitFieldNames ? '' : 'integrations',
        subBuilder: IntegrationTotals.create)
    ..pPM<DailyActivity>(9, _omitFieldNames ? '' : 'daily',
        subBuilder: DailyActivity.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetTelemetrySummaryResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetTelemetrySummaryResponse copyWith(
          void Function(GetTelemetrySummaryResponse) updates) =>
      super.copyWith(
              (message) => updates(message as GetTelemetrySummaryResponse))
          as GetTelemetrySummaryResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetTelemetrySummaryResponse create() =>
      GetTelemetrySummaryResponse._();
  @$core.override
  GetTelemetrySummaryResponse createEmptyInstance() => create();
  static $pb.PbList<GetTelemetrySummaryResponse> createRepeated() =>
      $pb.PbList<GetTelemetrySummaryResponse>();
  @$core.pragma('dart2js:noInline')
  static GetTelemetrySummaryResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetTelemetrySummaryResponse>(create);
  static GetTelemetrySummaryResponse? _defaultInstance;

  @$pb.TagNumber(1)
  TelemetryWindow get window => $_getN(0);
  @$pb.TagNumber(1)
  set window(TelemetryWindow value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasWindow() => $_has(0);
  @$pb.TagNumber(1)
  void clearWindow() => $_clearField(1);
  @$pb.TagNumber(1)
  TelemetryWindow ensureWindow() => $_ensure(0);

  @$pb.TagNumber(2)
  RunTotals get runs => $_getN(1);
  @$pb.TagNumber(2)
  set runs(RunTotals value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasRuns() => $_has(1);
  @$pb.TagNumber(2)
  void clearRuns() => $_clearField(2);
  @$pb.TagNumber(2)
  RunTotals ensureRuns() => $_ensure(1);

  @$pb.TagNumber(3)
  TokenTotals get tokens => $_getN(2);
  @$pb.TagNumber(3)
  set tokens(TokenTotals value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasTokens() => $_has(2);
  @$pb.TagNumber(3)
  void clearTokens() => $_clearField(3);
  @$pb.TagNumber(3)
  TokenTotals ensureTokens() => $_ensure(2);

  /// Ordered by calls descending, then by name, and capped server-side. "Most
  /// used" is the question being asked, so the order is part of the answer.
  @$pb.TagNumber(4)
  $pb.PbList<ToolUsage> get tools => $_getList(3);

  @$pb.TagNumber(5)
  $pb.PbList<ModelUsage> get models => $_getList(4);

  @$pb.TagNumber(6)
  $pb.PbList<ExternalAgentUsage> get externalAgents => $_getList(5);

  @$pb.TagNumber(7)
  AutomationTotals get automations => $_getN(6);
  @$pb.TagNumber(7)
  set automations(AutomationTotals value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasAutomations() => $_has(6);
  @$pb.TagNumber(7)
  void clearAutomations() => $_clearField(7);
  @$pb.TagNumber(7)
  AutomationTotals ensureAutomations() => $_ensure(6);

  @$pb.TagNumber(8)
  IntegrationTotals get integrations => $_getN(7);
  @$pb.TagNumber(8)
  set integrations(IntegrationTotals value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasIntegrations() => $_has(7);
  @$pb.TagNumber(8)
  void clearIntegrations() => $_clearField(8);
  @$pb.TagNumber(8)
  IntegrationTotals ensureIntegrations() => $_ensure(7);

  @$pb.TagNumber(9)
  $pb.PbList<DailyActivity> get daily => $_getList(8);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
