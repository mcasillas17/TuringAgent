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

import 'package:protobuf/protobuf.dart' as $pb;

class ToolCallPhase extends $pb.ProtobufEnum {
  static const ToolCallPhase TOOL_CALL_PHASE_UNSPECIFIED =
      ToolCallPhase._(0, _omitEnumNames ? '' : 'TOOL_CALL_PHASE_UNSPECIFIED');
  static const ToolCallPhase TOOL_CALL_PHASE_BEFORE =
      ToolCallPhase._(1, _omitEnumNames ? '' : 'TOOL_CALL_PHASE_BEFORE');
  static const ToolCallPhase TOOL_CALL_PHASE_AFTER =
      ToolCallPhase._(2, _omitEnumNames ? '' : 'TOOL_CALL_PHASE_AFTER');

  static const $core.List<ToolCallPhase> values = <ToolCallPhase>[
    TOOL_CALL_PHASE_UNSPECIFIED,
    TOOL_CALL_PHASE_BEFORE,
    TOOL_CALL_PHASE_AFTER,
  ];

  static final $core.List<ToolCallPhase?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static ToolCallPhase? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ToolCallPhase._(super.value, super.name);
}

class ToolCallStatus extends $pb.ProtobufEnum {
  static const ToolCallStatus TOOL_CALL_STATUS_UNSPECIFIED =
      ToolCallStatus._(0, _omitEnumNames ? '' : 'TOOL_CALL_STATUS_UNSPECIFIED');
  static const ToolCallStatus TOOL_CALL_STATUS_COMPLETED =
      ToolCallStatus._(1, _omitEnumNames ? '' : 'TOOL_CALL_STATUS_COMPLETED');
  static const ToolCallStatus TOOL_CALL_STATUS_FAILED =
      ToolCallStatus._(2, _omitEnumNames ? '' : 'TOOL_CALL_STATUS_FAILED');
  static const ToolCallStatus TOOL_CALL_STATUS_DENIED =
      ToolCallStatus._(3, _omitEnumNames ? '' : 'TOOL_CALL_STATUS_DENIED');

  static const $core.List<ToolCallStatus> values = <ToolCallStatus>[
    TOOL_CALL_STATUS_UNSPECIFIED,
    TOOL_CALL_STATUS_COMPLETED,
    TOOL_CALL_STATUS_FAILED,
    TOOL_CALL_STATUS_DENIED,
  ];

  static final $core.List<ToolCallStatus?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ToolCallStatus? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ToolCallStatus._(super.value, super.name);
}

class ToolPolicyDecision_Decision extends $pb.ProtobufEnum {
  static const ToolPolicyDecision_Decision DECISION_UNSPECIFIED =
      ToolPolicyDecision_Decision._(
          0, _omitEnumNames ? '' : 'DECISION_UNSPECIFIED');
  static const ToolPolicyDecision_Decision DECISION_ALLOW =
      ToolPolicyDecision_Decision._(1, _omitEnumNames ? '' : 'DECISION_ALLOW');
  static const ToolPolicyDecision_Decision DECISION_DENY =
      ToolPolicyDecision_Decision._(2, _omitEnumNames ? '' : 'DECISION_DENY');
  static const ToolPolicyDecision_Decision DECISION_APPROVAL_REQUIRED =
      ToolPolicyDecision_Decision._(
          3, _omitEnumNames ? '' : 'DECISION_APPROVAL_REQUIRED');

  static const $core.List<ToolPolicyDecision_Decision> values =
      <ToolPolicyDecision_Decision>[
    DECISION_UNSPECIFIED,
    DECISION_ALLOW,
    DECISION_DENY,
    DECISION_APPROVAL_REQUIRED,
  ];

  static final $core.List<ToolPolicyDecision_Decision?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ToolPolicyDecision_Decision? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ToolPolicyDecision_Decision._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
