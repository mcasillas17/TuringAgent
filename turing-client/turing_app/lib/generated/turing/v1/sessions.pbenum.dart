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

class SessionListFilter extends $pb.ProtobufEnum {
  static const SessionListFilter SESSION_LIST_FILTER_UNSPECIFIED =
      SessionListFilter._(
          0, _omitEnumNames ? '' : 'SESSION_LIST_FILTER_UNSPECIFIED');
  static const SessionListFilter SESSION_LIST_FILTER_ACTIVE =
      SessionListFilter._(
          1, _omitEnumNames ? '' : 'SESSION_LIST_FILTER_ACTIVE');
  static const SessionListFilter SESSION_LIST_FILTER_ARCHIVED =
      SessionListFilter._(
          2, _omitEnumNames ? '' : 'SESSION_LIST_FILTER_ARCHIVED');
  static const SessionListFilter SESSION_LIST_FILTER_ALL =
      SessionListFilter._(3, _omitEnumNames ? '' : 'SESSION_LIST_FILTER_ALL');

  static const $core.List<SessionListFilter> values = <SessionListFilter>[
    SESSION_LIST_FILTER_UNSPECIFIED,
    SESSION_LIST_FILTER_ACTIVE,
    SESSION_LIST_FILTER_ARCHIVED,
    SESSION_LIST_FILTER_ALL,
  ];

  static final $core.List<SessionListFilter?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static SessionListFilter? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SessionListFilter._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
