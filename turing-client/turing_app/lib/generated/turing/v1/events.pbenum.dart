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

import 'package:protobuf/protobuf.dart' as $pb;

class TuringEventType extends $pb.ProtobufEnum {
  static const TuringEventType TURING_EVENT_TYPE_UNSPECIFIED =
      TuringEventType._(
          0, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_UNSPECIFIED');
  static const TuringEventType TURING_EVENT_TYPE_MESSAGE_STARTED =
      TuringEventType._(
          1, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_MESSAGE_STARTED');
  static const TuringEventType TURING_EVENT_TYPE_MESSAGE_DELTA =
      TuringEventType._(
          2, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_MESSAGE_DELTA');
  static const TuringEventType TURING_EVENT_TYPE_MESSAGE_COMPLETED =
      TuringEventType._(
          3, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_MESSAGE_COMPLETED');
  static const TuringEventType TURING_EVENT_TYPE_AGENT_RUN_QUEUED =
      TuringEventType._(
          4, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_AGENT_RUN_QUEUED');
  static const TuringEventType TURING_EVENT_TYPE_AGENT_RUN_STARTED =
      TuringEventType._(
          5, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_AGENT_RUN_STARTED');
  static const TuringEventType TURING_EVENT_TYPE_AGENT_RUN_STEP =
      TuringEventType._(
          6, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_AGENT_RUN_STEP');
  static const TuringEventType TURING_EVENT_TYPE_AGENT_RUN_COMPLETED =
      TuringEventType._(
          7, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_AGENT_RUN_COMPLETED');
  static const TuringEventType TURING_EVENT_TYPE_AGENT_RUN_FAILED =
      TuringEventType._(
          8, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_AGENT_RUN_FAILED');
  static const TuringEventType TURING_EVENT_TYPE_AGENT_RUN_CANCELLED =
      TuringEventType._(
          9, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_AGENT_RUN_CANCELLED');
  static const TuringEventType TURING_EVENT_TYPE_TOOL_CALL_STARTED =
      TuringEventType._(
          10, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_TOOL_CALL_STARTED');
  static const TuringEventType TURING_EVENT_TYPE_TOOL_CALL_COMPLETED =
      TuringEventType._(
          11, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_TOOL_CALL_COMPLETED');
  static const TuringEventType TURING_EVENT_TYPE_TOOL_CALL_FAILED =
      TuringEventType._(
          12, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_TOOL_CALL_FAILED');
  static const TuringEventType TURING_EVENT_TYPE_TOOL_CALL_DENIED =
      TuringEventType._(
          13, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_TOOL_CALL_DENIED');
  static const TuringEventType TURING_EVENT_TYPE_APPROVAL_REQUESTED =
      TuringEventType._(
          14, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_APPROVAL_REQUESTED');
  static const TuringEventType TURING_EVENT_TYPE_APPROVAL_APPROVED =
      TuringEventType._(
          15, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_APPROVAL_APPROVED');
  static const TuringEventType TURING_EVENT_TYPE_APPROVAL_DENIED =
      TuringEventType._(
          16, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_APPROVAL_DENIED');
  static const TuringEventType TURING_EVENT_TYPE_APPROVAL_EXPIRED =
      TuringEventType._(
          17, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_APPROVAL_EXPIRED');
  static const TuringEventType TURING_EVENT_TYPE_APPROVAL_CONSUMED =
      TuringEventType._(
          18, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_APPROVAL_CONSUMED');
  static const TuringEventType TURING_EVENT_TYPE_ERROR =
      TuringEventType._(19, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_ERROR');
  static const TuringEventType TURING_EVENT_TYPE_SYSTEM =
      TuringEventType._(20, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_SYSTEM');
  static const TuringEventType TURING_EVENT_TYPE_SESSION_UPDATED =
      TuringEventType._(
          21, _omitEnumNames ? '' : 'TURING_EVENT_TYPE_SESSION_UPDATED');
  static const TuringEventType TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED =
      TuringEventType._(23,
          _omitEnumNames ? '' : 'TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED');

  static const $core.List<TuringEventType> values = <TuringEventType>[
    TURING_EVENT_TYPE_UNSPECIFIED,
    TURING_EVENT_TYPE_MESSAGE_STARTED,
    TURING_EVENT_TYPE_MESSAGE_DELTA,
    TURING_EVENT_TYPE_MESSAGE_COMPLETED,
    TURING_EVENT_TYPE_AGENT_RUN_QUEUED,
    TURING_EVENT_TYPE_AGENT_RUN_STARTED,
    TURING_EVENT_TYPE_AGENT_RUN_STEP,
    TURING_EVENT_TYPE_AGENT_RUN_COMPLETED,
    TURING_EVENT_TYPE_AGENT_RUN_FAILED,
    TURING_EVENT_TYPE_AGENT_RUN_CANCELLED,
    TURING_EVENT_TYPE_TOOL_CALL_STARTED,
    TURING_EVENT_TYPE_TOOL_CALL_COMPLETED,
    TURING_EVENT_TYPE_TOOL_CALL_FAILED,
    TURING_EVENT_TYPE_TOOL_CALL_DENIED,
    TURING_EVENT_TYPE_APPROVAL_REQUESTED,
    TURING_EVENT_TYPE_APPROVAL_APPROVED,
    TURING_EVENT_TYPE_APPROVAL_DENIED,
    TURING_EVENT_TYPE_APPROVAL_EXPIRED,
    TURING_EVENT_TYPE_APPROVAL_CONSUMED,
    TURING_EVENT_TYPE_ERROR,
    TURING_EVENT_TYPE_SYSTEM,
    TURING_EVENT_TYPE_SESSION_UPDATED,
    TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED,
  ];

  static final $core.List<TuringEventType?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 23);
  static TuringEventType? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const TuringEventType._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
