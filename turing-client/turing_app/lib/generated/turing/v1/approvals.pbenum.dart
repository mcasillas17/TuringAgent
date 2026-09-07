// This is a generated file - do not edit.
//
// Generated from turing/v1/approvals.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

class ApprovalStatus extends $pb.ProtobufEnum {
  static const ApprovalStatus APPROVAL_STATUS_UNSPECIFIED =
      ApprovalStatus._(0, _omitEnumNames ? '' : 'APPROVAL_STATUS_UNSPECIFIED');
  static const ApprovalStatus APPROVAL_STATUS_PENDING =
      ApprovalStatus._(1, _omitEnumNames ? '' : 'APPROVAL_STATUS_PENDING');
  static const ApprovalStatus APPROVAL_STATUS_APPROVED =
      ApprovalStatus._(2, _omitEnumNames ? '' : 'APPROVAL_STATUS_APPROVED');
  static const ApprovalStatus APPROVAL_STATUS_DENIED =
      ApprovalStatus._(3, _omitEnumNames ? '' : 'APPROVAL_STATUS_DENIED');
  static const ApprovalStatus APPROVAL_STATUS_EXPIRED =
      ApprovalStatus._(4, _omitEnumNames ? '' : 'APPROVAL_STATUS_EXPIRED');
  static const ApprovalStatus APPROVAL_STATUS_CONSUMED =
      ApprovalStatus._(5, _omitEnumNames ? '' : 'APPROVAL_STATUS_CONSUMED');

  static const $core.List<ApprovalStatus> values = <ApprovalStatus>[
    APPROVAL_STATUS_UNSPECIFIED,
    APPROVAL_STATUS_PENDING,
    APPROVAL_STATUS_APPROVED,
    APPROVAL_STATUS_DENIED,
    APPROVAL_STATUS_EXPIRED,
    APPROVAL_STATUS_CONSUMED,
  ];

  static final $core.List<ApprovalStatus?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static ApprovalStatus? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ApprovalStatus._(super.value, super.name);
}

class ApprovalPreviewState extends $pb.ProtobufEnum {
  static const ApprovalPreviewState APPROVAL_PREVIEW_STATE_UNSPECIFIED =
      ApprovalPreviewState._(
          0, _omitEnumNames ? '' : 'APPROVAL_PREVIEW_STATE_UNSPECIFIED');
  static const ApprovalPreviewState APPROVAL_PREVIEW_STATE_READY =
      ApprovalPreviewState._(
          1, _omitEnumNames ? '' : 'APPROVAL_PREVIEW_STATE_READY');
  static const ApprovalPreviewState APPROVAL_PREVIEW_STATE_UNAVAILABLE =
      ApprovalPreviewState._(
          2, _omitEnumNames ? '' : 'APPROVAL_PREVIEW_STATE_UNAVAILABLE');
  static const ApprovalPreviewState APPROVAL_PREVIEW_STATE_UNSUPPORTED =
      ApprovalPreviewState._(
          3, _omitEnumNames ? '' : 'APPROVAL_PREVIEW_STATE_UNSUPPORTED');
  static const ApprovalPreviewState APPROVAL_PREVIEW_STATE_REDACTED =
      ApprovalPreviewState._(
          4, _omitEnumNames ? '' : 'APPROVAL_PREVIEW_STATE_REDACTED');
  static const ApprovalPreviewState APPROVAL_PREVIEW_STATE_OVERSIZED =
      ApprovalPreviewState._(
          5, _omitEnumNames ? '' : 'APPROVAL_PREVIEW_STATE_OVERSIZED');
  static const ApprovalPreviewState APPROVAL_PREVIEW_STATE_BINARY =
      ApprovalPreviewState._(
          6, _omitEnumNames ? '' : 'APPROVAL_PREVIEW_STATE_BINARY');
  static const ApprovalPreviewState APPROVAL_PREVIEW_STATE_EXPIRED =
      ApprovalPreviewState._(
          7, _omitEnumNames ? '' : 'APPROVAL_PREVIEW_STATE_EXPIRED');
  static const ApprovalPreviewState APPROVAL_PREVIEW_STATE_STALE =
      ApprovalPreviewState._(
          8, _omitEnumNames ? '' : 'APPROVAL_PREVIEW_STATE_STALE');
  static const ApprovalPreviewState APPROVAL_PREVIEW_STATE_TERMINAL =
      ApprovalPreviewState._(
          9, _omitEnumNames ? '' : 'APPROVAL_PREVIEW_STATE_TERMINAL');

  static const $core.List<ApprovalPreviewState> values = <ApprovalPreviewState>[
    APPROVAL_PREVIEW_STATE_UNSPECIFIED,
    APPROVAL_PREVIEW_STATE_READY,
    APPROVAL_PREVIEW_STATE_UNAVAILABLE,
    APPROVAL_PREVIEW_STATE_UNSUPPORTED,
    APPROVAL_PREVIEW_STATE_REDACTED,
    APPROVAL_PREVIEW_STATE_OVERSIZED,
    APPROVAL_PREVIEW_STATE_BINARY,
    APPROVAL_PREVIEW_STATE_EXPIRED,
    APPROVAL_PREVIEW_STATE_STALE,
    APPROVAL_PREVIEW_STATE_TERMINAL,
  ];

  static final $core.List<ApprovalPreviewState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 9);
  static ApprovalPreviewState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ApprovalPreviewState._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
