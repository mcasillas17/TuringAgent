// This is a generated file - do not edit.
//
// Generated from turing/v1/chat.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

class CancelRunResult extends $pb.ProtobufEnum {
  static const CancelRunResult CANCEL_RUN_RESULT_UNSPECIFIED =
      CancelRunResult._(
          0, _omitEnumNames ? '' : 'CANCEL_RUN_RESULT_UNSPECIFIED');
  static const CancelRunResult CANCEL_RUN_RESULT_ACCEPTED =
      CancelRunResult._(1, _omitEnumNames ? '' : 'CANCEL_RUN_RESULT_ACCEPTED');
  static const CancelRunResult CANCEL_RUN_RESULT_ALREADY_TERMINAL =
      CancelRunResult._(
          2, _omitEnumNames ? '' : 'CANCEL_RUN_RESULT_ALREADY_TERMINAL');
  static const CancelRunResult CANCEL_RUN_RESULT_UNAVAILABLE =
      CancelRunResult._(
          3, _omitEnumNames ? '' : 'CANCEL_RUN_RESULT_UNAVAILABLE');

  static const $core.List<CancelRunResult> values = <CancelRunResult>[
    CANCEL_RUN_RESULT_UNSPECIFIED,
    CANCEL_RUN_RESULT_ACCEPTED,
    CANCEL_RUN_RESULT_ALREADY_TERMINAL,
    CANCEL_RUN_RESULT_UNAVAILABLE,
  ];

  static final $core.List<CancelRunResult?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static CancelRunResult? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CancelRunResult._(super.value, super.name);
}

class CancellationProgress extends $pb.ProtobufEnum {
  static const CancellationProgress CANCELLATION_PROGRESS_UNSPECIFIED =
      CancellationProgress._(
          0, _omitEnumNames ? '' : 'CANCELLATION_PROGRESS_UNSPECIFIED');
  static const CancellationProgress CANCELLATION_PROGRESS_NOT_CANCELLED =
      CancellationProgress._(
          1, _omitEnumNames ? '' : 'CANCELLATION_PROGRESS_NOT_CANCELLED');
  static const CancellationProgress CANCELLATION_PROGRESS_STOPPING =
      CancellationProgress._(
          2, _omitEnumNames ? '' : 'CANCELLATION_PROGRESS_STOPPING');
  static const CancellationProgress CANCELLATION_PROGRESS_RECONCILED =
      CancellationProgress._(
          3, _omitEnumNames ? '' : 'CANCELLATION_PROGRESS_RECONCILED');

  static const $core.List<CancellationProgress> values = <CancellationProgress>[
    CANCELLATION_PROGRESS_UNSPECIFIED,
    CANCELLATION_PROGRESS_NOT_CANCELLED,
    CANCELLATION_PROGRESS_STOPPING,
    CANCELLATION_PROGRESS_RECONCILED,
  ];

  static final $core.List<CancellationProgress?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static CancellationProgress? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CancellationProgress._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
