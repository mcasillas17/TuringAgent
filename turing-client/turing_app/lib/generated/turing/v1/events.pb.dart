// This is a generated file - do not edit.
//
// Generated from turing/v1/events.proto.

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
import '../../google/protobuf/timestamp.pb.dart' as $1;
import 'common.pb.dart' as $3;
import 'events.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'events.pbenum.dart';

class TuringEvent extends $pb.GeneratedMessage {
  factory TuringEvent({
    $core.String? eventId,
    $core.String? sessionId,
    $core.String? runId,
    $core.String? traceId,
    $fixnum.Int64? sequence,
    TuringEventType? type,
    $1.Timestamp? createdAt,
    $2.Struct? payload,
    $3.RunState? runState,
  }) {
    final result = create();
    if (eventId != null) result.eventId = eventId;
    if (sessionId != null) result.sessionId = sessionId;
    if (runId != null) result.runId = runId;
    if (traceId != null) result.traceId = traceId;
    if (sequence != null) result.sequence = sequence;
    if (type != null) result.type = type;
    if (createdAt != null) result.createdAt = createdAt;
    if (payload != null) result.payload = payload;
    if (runState != null) result.runState = runState;
    return result;
  }

  TuringEvent._();

  factory TuringEvent.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory TuringEvent.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'TuringEvent',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'eventId')
    ..aOS(2, _omitFieldNames ? '' : 'sessionId')
    ..aOS(3, _omitFieldNames ? '' : 'runId')
    ..aOS(4, _omitFieldNames ? '' : 'traceId')
    ..aInt64(5, _omitFieldNames ? '' : 'sequence')
    ..aE<TuringEventType>(6, _omitFieldNames ? '' : 'type',
        enumValues: TuringEventType.values)
    ..aOM<$1.Timestamp>(7, _omitFieldNames ? '' : 'createdAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$2.Struct>(8, _omitFieldNames ? '' : 'payload',
        subBuilder: $2.Struct.create)
    ..aOM<$3.RunState>(9, _omitFieldNames ? '' : 'runState',
        subBuilder: $3.RunState.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TuringEvent clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  TuringEvent copyWith(void Function(TuringEvent) updates) =>
      super.copyWith((message) => updates(message as TuringEvent))
          as TuringEvent;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static TuringEvent create() => TuringEvent._();
  @$core.override
  TuringEvent createEmptyInstance() => create();
  static $pb.PbList<TuringEvent> createRepeated() => $pb.PbList<TuringEvent>();
  @$core.pragma('dart2js:noInline')
  static TuringEvent getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<TuringEvent>(create);
  static TuringEvent? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get eventId => $_getSZ(0);
  @$pb.TagNumber(1)
  set eventId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasEventId() => $_has(0);
  @$pb.TagNumber(1)
  void clearEventId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get sessionId => $_getSZ(1);
  @$pb.TagNumber(2)
  set sessionId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasSessionId() => $_has(1);
  @$pb.TagNumber(2)
  void clearSessionId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get runId => $_getSZ(2);
  @$pb.TagNumber(3)
  set runId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasRunId() => $_has(2);
  @$pb.TagNumber(3)
  void clearRunId() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get traceId => $_getSZ(3);
  @$pb.TagNumber(4)
  set traceId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasTraceId() => $_has(3);
  @$pb.TagNumber(4)
  void clearTraceId() => $_clearField(4);

  @$pb.TagNumber(5)
  $fixnum.Int64 get sequence => $_getI64(4);
  @$pb.TagNumber(5)
  set sequence($fixnum.Int64 value) => $_setInt64(4, value);
  @$pb.TagNumber(5)
  $core.bool hasSequence() => $_has(4);
  @$pb.TagNumber(5)
  void clearSequence() => $_clearField(5);

  @$pb.TagNumber(6)
  TuringEventType get type => $_getN(5);
  @$pb.TagNumber(6)
  set type(TuringEventType value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasType() => $_has(5);
  @$pb.TagNumber(6)
  void clearType() => $_clearField(6);

  @$pb.TagNumber(7)
  $1.Timestamp get createdAt => $_getN(6);
  @$pb.TagNumber(7)
  set createdAt($1.Timestamp value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasCreatedAt() => $_has(6);
  @$pb.TagNumber(7)
  void clearCreatedAt() => $_clearField(7);
  @$pb.TagNumber(7)
  $1.Timestamp ensureCreatedAt() => $_ensure(6);

  @$pb.TagNumber(8)
  $2.Struct get payload => $_getN(7);
  @$pb.TagNumber(8)
  set payload($2.Struct value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasPayload() => $_has(7);
  @$pb.TagNumber(8)
  void clearPayload() => $_clearField(8);
  @$pb.TagNumber(8)
  $2.Struct ensurePayload() => $_ensure(7);

  /// The resulting run state, so replayed history carries the same
  /// authoritative outcome the live stream did. Set only for the repository's
  /// own closed carrier set — agent.run.queued/started/state_changed/
  /// completed/failed/cancelled and the approval.requested/approval.approved
  /// carriers (approval.approved never itself moves a run's lifecycle; it
  /// still carries the run state as it stands). Absent for every other event
  /// type, including every other approval event (approval.denied/expired/
  /// consumed), even when that row's own payload contains a well-formed value
  /// under a runState key.
  @$pb.TagNumber(9)
  $3.RunState get runState => $_getN(8);
  @$pb.TagNumber(9)
  set runState($3.RunState value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasRunState() => $_has(8);
  @$pb.TagNumber(9)
  void clearRunState() => $_clearField(9);
  @$pb.TagNumber(9)
  $3.RunState ensureRunState() => $_ensure(8);
}

class ListEventsRequest extends $pb.GeneratedMessage {
  factory ListEventsRequest({
    $core.String? sessionId,
    $fixnum.Int64? afterSequence,
    $core.int? limit,
  }) {
    final result = create();
    if (sessionId != null) result.sessionId = sessionId;
    if (afterSequence != null) result.afterSequence = afterSequence;
    if (limit != null) result.limit = limit;
    return result;
  }

  ListEventsRequest._();

  factory ListEventsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListEventsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListEventsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..aInt64(2, _omitFieldNames ? '' : 'afterSequence')
    ..aI(3, _omitFieldNames ? '' : 'limit')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListEventsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListEventsRequest copyWith(void Function(ListEventsRequest) updates) =>
      super.copyWith((message) => updates(message as ListEventsRequest))
          as ListEventsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListEventsRequest create() => ListEventsRequest._();
  @$core.override
  ListEventsRequest createEmptyInstance() => create();
  static $pb.PbList<ListEventsRequest> createRepeated() =>
      $pb.PbList<ListEventsRequest>();
  @$core.pragma('dart2js:noInline')
  static ListEventsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListEventsRequest>(create);
  static ListEventsRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get afterSequence => $_getI64(1);
  @$pb.TagNumber(2)
  set afterSequence($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasAfterSequence() => $_has(1);
  @$pb.TagNumber(2)
  void clearAfterSequence() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.int get limit => $_getIZ(2);
  @$pb.TagNumber(3)
  set limit($core.int value) => $_setSignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasLimit() => $_has(2);
  @$pb.TagNumber(3)
  void clearLimit() => $_clearField(3);
}

class ListEventsResponse extends $pb.GeneratedMessage {
  factory ListEventsResponse({
    $core.Iterable<TuringEvent>? events,
    $fixnum.Int64? latestSequence,
    $core.bool? resyncRequired,
  }) {
    final result = create();
    if (events != null) result.events.addAll(events);
    if (latestSequence != null) result.latestSequence = latestSequence;
    if (resyncRequired != null) result.resyncRequired = resyncRequired;
    return result;
  }

  ListEventsResponse._();

  factory ListEventsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListEventsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListEventsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..pPM<TuringEvent>(1, _omitFieldNames ? '' : 'events',
        subBuilder: TuringEvent.create)
    ..aInt64(2, _omitFieldNames ? '' : 'latestSequence')
    ..aOB(3, _omitFieldNames ? '' : 'resyncRequired')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListEventsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListEventsResponse copyWith(void Function(ListEventsResponse) updates) =>
      super.copyWith((message) => updates(message as ListEventsResponse))
          as ListEventsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListEventsResponse create() => ListEventsResponse._();
  @$core.override
  ListEventsResponse createEmptyInstance() => create();
  static $pb.PbList<ListEventsResponse> createRepeated() =>
      $pb.PbList<ListEventsResponse>();
  @$core.pragma('dart2js:noInline')
  static ListEventsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListEventsResponse>(create);
  static ListEventsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<TuringEvent> get events => $_getList(0);

  @$pb.TagNumber(2)
  $fixnum.Int64 get latestSequence => $_getI64(1);
  @$pb.TagNumber(2)
  set latestSequence($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasLatestSequence() => $_has(1);
  @$pb.TagNumber(2)
  void clearLatestSequence() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.bool get resyncRequired => $_getBF(2);
  @$pb.TagNumber(3)
  set resyncRequired($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasResyncRequired() => $_has(2);
  @$pb.TagNumber(3)
  void clearResyncRequired() => $_clearField(3);
}

class SubscribeSessionEventsRequest extends $pb.GeneratedMessage {
  factory SubscribeSessionEventsRequest({
    $core.String? sessionId,
    $fixnum.Int64? afterSequence,
  }) {
    final result = create();
    if (sessionId != null) result.sessionId = sessionId;
    if (afterSequence != null) result.afterSequence = afterSequence;
    return result;
  }

  SubscribeSessionEventsRequest._();

  factory SubscribeSessionEventsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SubscribeSessionEventsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SubscribeSessionEventsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..aInt64(2, _omitFieldNames ? '' : 'afterSequence')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SubscribeSessionEventsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SubscribeSessionEventsRequest copyWith(
          void Function(SubscribeSessionEventsRequest) updates) =>
      super.copyWith(
              (message) => updates(message as SubscribeSessionEventsRequest))
          as SubscribeSessionEventsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SubscribeSessionEventsRequest create() =>
      SubscribeSessionEventsRequest._();
  @$core.override
  SubscribeSessionEventsRequest createEmptyInstance() => create();
  static $pb.PbList<SubscribeSessionEventsRequest> createRepeated() =>
      $pb.PbList<SubscribeSessionEventsRequest>();
  @$core.pragma('dart2js:noInline')
  static SubscribeSessionEventsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SubscribeSessionEventsRequest>(create);
  static SubscribeSessionEventsRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get afterSequence => $_getI64(1);
  @$pb.TagNumber(2)
  set afterSequence($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasAfterSequence() => $_has(1);
  @$pb.TagNumber(2)
  void clearAfterSequence() => $_clearField(2);
}

class SubscribeSessionUpdatesRequest extends $pb.GeneratedMessage {
  factory SubscribeSessionUpdatesRequest() => create();

  SubscribeSessionUpdatesRequest._();

  factory SubscribeSessionUpdatesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SubscribeSessionUpdatesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SubscribeSessionUpdatesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SubscribeSessionUpdatesRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SubscribeSessionUpdatesRequest copyWith(
          void Function(SubscribeSessionUpdatesRequest) updates) =>
      super.copyWith(
              (message) => updates(message as SubscribeSessionUpdatesRequest))
          as SubscribeSessionUpdatesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SubscribeSessionUpdatesRequest create() =>
      SubscribeSessionUpdatesRequest._();
  @$core.override
  SubscribeSessionUpdatesRequest createEmptyInstance() => create();
  static $pb.PbList<SubscribeSessionUpdatesRequest> createRepeated() =>
      $pb.PbList<SubscribeSessionUpdatesRequest>();
  @$core.pragma('dart2js:noInline')
  static SubscribeSessionUpdatesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SubscribeSessionUpdatesRequest>(create);
  static SubscribeSessionUpdatesRequest? _defaultInstance;
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
