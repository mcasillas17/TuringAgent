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

class AutomationScheduleKind extends $pb.ProtobufEnum {
  static const AutomationScheduleKind AUTOMATION_SCHEDULE_KIND_UNSPECIFIED =
      AutomationScheduleKind._(
          0, _omitEnumNames ? '' : 'AUTOMATION_SCHEDULE_KIND_UNSPECIFIED');
  static const AutomationScheduleKind AUTOMATION_SCHEDULE_KIND_INTERVAL =
      AutomationScheduleKind._(
          1, _omitEnumNames ? '' : 'AUTOMATION_SCHEDULE_KIND_INTERVAL');
  static const AutomationScheduleKind AUTOMATION_SCHEDULE_KIND_DAILY =
      AutomationScheduleKind._(
          2, _omitEnumNames ? '' : 'AUTOMATION_SCHEDULE_KIND_DAILY');

  static const $core.List<AutomationScheduleKind> values =
      <AutomationScheduleKind>[
    AUTOMATION_SCHEDULE_KIND_UNSPECIFIED,
    AUTOMATION_SCHEDULE_KIND_INTERVAL,
    AUTOMATION_SCHEDULE_KIND_DAILY,
  ];

  static final $core.List<AutomationScheduleKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static AutomationScheduleKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AutomationScheduleKind._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
