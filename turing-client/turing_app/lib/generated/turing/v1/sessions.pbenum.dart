// This is a generated file - do not edit.
//
// Generated from turing/v1/sessions.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

class SessionDeletionState extends $pb.ProtobufEnum {
  static const SessionDeletionState SESSION_DELETION_STATE_UNSPECIFIED =
      SessionDeletionState._(
          0, _omitEnumNames ? '' : 'SESSION_DELETION_STATE_UNSPECIFIED');
  static const SessionDeletionState SESSION_DELETION_STATE_IN_PROGRESS =
      SessionDeletionState._(
          1, _omitEnumNames ? '' : 'SESSION_DELETION_STATE_IN_PROGRESS');
  static const SessionDeletionState SESSION_DELETION_STATE_FAILED_EXTERNAL =
      SessionDeletionState._(
          2, _omitEnumNames ? '' : 'SESSION_DELETION_STATE_FAILED_EXTERNAL');
  static const SessionDeletionState SESSION_DELETION_STATE_COMPLETED =
      SessionDeletionState._(
          3, _omitEnumNames ? '' : 'SESSION_DELETION_STATE_COMPLETED');

  static const $core.List<SessionDeletionState> values = <SessionDeletionState>[
    SESSION_DELETION_STATE_UNSPECIFIED,
    SESSION_DELETION_STATE_IN_PROGRESS,
    SESSION_DELETION_STATE_FAILED_EXTERNAL,
    SESSION_DELETION_STATE_COMPLETED,
  ];

  static final $core.List<SessionDeletionState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static SessionDeletionState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SessionDeletionState._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
