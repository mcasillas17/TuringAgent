// This is a generated file - do not edit.
//
// Generated from turing/v1/audit.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

class AuditOrder extends $pb.ProtobufEnum {
  static const AuditOrder AUDIT_ORDER_UNSPECIFIED =
      AuditOrder._(0, _omitEnumNames ? '' : 'AUDIT_ORDER_UNSPECIFIED');
  static const AuditOrder AUDIT_ORDER_DESCENDING =
      AuditOrder._(1, _omitEnumNames ? '' : 'AUDIT_ORDER_DESCENDING');
  static const AuditOrder AUDIT_ORDER_ASCENDING =
      AuditOrder._(2, _omitEnumNames ? '' : 'AUDIT_ORDER_ASCENDING');

  static const $core.List<AuditOrder> values = <AuditOrder>[
    AUDIT_ORDER_UNSPECIFIED,
    AUDIT_ORDER_DESCENDING,
    AUDIT_ORDER_ASCENDING,
  ];

  static final $core.List<AuditOrder?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static AuditOrder? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AuditOrder._(super.value, super.name);
}

/// Payload state says whether the structured audit payload was absent, present,
/// or scrubbed before storage.
class AuditPayloadState extends $pb.ProtobufEnum {
  static const AuditPayloadState AUDIT_PAYLOAD_STATE_UNSPECIFIED =
      AuditPayloadState._(
          0, _omitEnumNames ? '' : 'AUDIT_PAYLOAD_STATE_UNSPECIFIED');

  /// No payload was ever stored for the row.
  static const AuditPayloadState AUDIT_PAYLOAD_STATE_ABSENT =
      AuditPayloadState._(
          1, _omitEnumNames ? '' : 'AUDIT_PAYLOAD_STATE_ABSENT');

  /// A payload existed, but only action-allowlisted typed fields may be returned.
  static const AuditPayloadState AUDIT_PAYLOAD_STATE_PRESENT =
      AuditPayloadState._(
          2, _omitEnumNames ? '' : 'AUDIT_PAYLOAD_STATE_PRESENT');

  /// Stored content was deliberately replaced after withdrawal or deletion.
  static const AuditPayloadState AUDIT_PAYLOAD_STATE_SCRUBBED =
      AuditPayloadState._(
          3, _omitEnumNames ? '' : 'AUDIT_PAYLOAD_STATE_SCRUBBED');

  static const $core.List<AuditPayloadState> values = <AuditPayloadState>[
    AUDIT_PAYLOAD_STATE_UNSPECIFIED,
    AUDIT_PAYLOAD_STATE_ABSENT,
    AUDIT_PAYLOAD_STATE_PRESENT,
    AUDIT_PAYLOAD_STATE_SCRUBBED,
  ];

  static final $core.List<AuditPayloadState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static AuditPayloadState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AuditPayloadState._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
