// This is a generated file - do not edit.
//
// Generated from turing/v1/attention.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

/// The Needs-you queue's closed stage table, read top to bottom, first match
/// wins. Every row is an obligation on the owner: healthy states take no space.
class AttentionStage extends $pb.ProtobufEnum {
  static const AttentionStage ATTENTION_STAGE_UNSPECIFIED =
      AttentionStage._(0, _omitEnumNames ? '' : 'ATTENTION_STAGE_UNSPECIFIED');

  /// A pending, unexpired approval on any run. A delegated task's appears under
  /// its parent conversation.
  static const AttentionStage ATTENTION_STAGE_PERSON_APPROVAL =
      AttentionStage._(
          1, _omitEnumNames ? '' : 'ATTENTION_STAGE_PERSON_APPROVAL');

  /// A delegated task waiting for the owner's consent to tools that leave the
  /// machine.
  static const AttentionStage ATTENTION_STAGE_PERSON_CONSENT = AttentionStage._(
      2, _omitEnumNames ? '' : 'ATTENTION_STAGE_PERSON_CONSENT');

  /// An enabled automation whose latest occurrence failed or was blocked,
  /// unless the owner dismissed that occurrence.
  static const AttentionStage ATTENTION_STAGE_PERSON_BLOCKED = AttentionStage._(
      3, _omitEnumNames ? '' : 'ATTENTION_STAGE_PERSON_BLOCKED');

  /// A deletion the owner can retry, held failed and retryable.
  static const AttentionStage ATTENTION_STAGE_PERSON_DELETION =
      AttentionStage._(
          4, _omitEnumNames ? '' : 'ATTENTION_STAGE_PERSON_DELETION');

  /// An open watch proposal. Nothing raises it until PRO-001.
  static const AttentionStage ATTENTION_STAGE_PERSON_PROPOSAL =
      AttentionStage._(
          5, _omitEnumNames ? '' : 'ATTENTION_STAGE_PERSON_PROPOSAL');

  /// A row the projection could not classify. It is listed in its source's
  /// place, never dropped, so an empty list is always a fact.
  static const AttentionStage ATTENTION_STAGE_UNAVAILABLE =
      AttentionStage._(6, _omitEnumNames ? '' : 'ATTENTION_STAGE_UNAVAILABLE');

  static const $core.List<AttentionStage> values = <AttentionStage>[
    ATTENTION_STAGE_UNSPECIFIED,
    ATTENTION_STAGE_PERSON_APPROVAL,
    ATTENTION_STAGE_PERSON_CONSENT,
    ATTENTION_STAGE_PERSON_BLOCKED,
    ATTENTION_STAGE_PERSON_DELETION,
    ATTENTION_STAGE_PERSON_PROPOSAL,
    ATTENTION_STAGE_UNAVAILABLE,
  ];

  static final $core.List<AttentionStage?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static AttentionStage? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AttentionStage._(super.value, super.name);
}

/// The source a row came from. Together with the stage it says which action
/// the row offers. The values are also the third key of the list's order.
class AttentionSourceKind extends $pb.ProtobufEnum {
  static const AttentionSourceKind ATTENTION_SOURCE_KIND_UNSPECIFIED =
      AttentionSourceKind._(
          0, _omitEnumNames ? '' : 'ATTENTION_SOURCE_KIND_UNSPECIFIED');
  static const AttentionSourceKind ATTENTION_SOURCE_KIND_APPROVAL =
      AttentionSourceKind._(
          1, _omitEnumNames ? '' : 'ATTENTION_SOURCE_KIND_APPROVAL');
  static const AttentionSourceKind ATTENTION_SOURCE_KIND_DELEGATION =
      AttentionSourceKind._(
          2, _omitEnumNames ? '' : 'ATTENTION_SOURCE_KIND_DELEGATION');
  static const AttentionSourceKind ATTENTION_SOURCE_KIND_AUTOMATION =
      AttentionSourceKind._(
          3, _omitEnumNames ? '' : 'ATTENTION_SOURCE_KIND_AUTOMATION');
  static const AttentionSourceKind ATTENTION_SOURCE_KIND_DELETION =
      AttentionSourceKind._(
          4, _omitEnumNames ? '' : 'ATTENTION_SOURCE_KIND_DELETION');

  /// A watch suspended by a source failure. Nothing lists one until PRO-001.
  static const AttentionSourceKind ATTENTION_SOURCE_KIND_WATCH =
      AttentionSourceKind._(
          5, _omitEnumNames ? '' : 'ATTENTION_SOURCE_KIND_WATCH');

  /// An open watch proposal. Nothing lists one until PRO-001.
  static const AttentionSourceKind ATTENTION_SOURCE_KIND_PROPOSAL =
      AttentionSourceKind._(
          6, _omitEnumNames ? '' : 'ATTENTION_SOURCE_KIND_PROPOSAL');

  static const $core.List<AttentionSourceKind> values = <AttentionSourceKind>[
    ATTENTION_SOURCE_KIND_UNSPECIFIED,
    ATTENTION_SOURCE_KIND_APPROVAL,
    ATTENTION_SOURCE_KIND_DELEGATION,
    ATTENTION_SOURCE_KIND_AUTOMATION,
    ATTENTION_SOURCE_KIND_DELETION,
    ATTENTION_SOURCE_KIND_WATCH,
    ATTENTION_SOURCE_KIND_PROPOSAL,
  ];

  static final $core.List<AttentionSourceKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static AttentionSourceKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AttentionSourceKind._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
