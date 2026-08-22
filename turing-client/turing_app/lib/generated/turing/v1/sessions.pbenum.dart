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

/// Selects one response representation so hit metadata does not duplicate
/// unbounded message bodies.
class SearchMessagesResponseFormat extends $pb.ProtobufEnum {
  /// Resolves to LEGACY_MESSAGES for old callers.
  static const SearchMessagesResponseFormat
      SEARCH_MESSAGES_RESPONSE_FORMAT_UNSPECIFIED =
      SearchMessagesResponseFormat._(0,
          _omitEnumNames ? '' : 'SEARCH_MESSAGES_RESPONSE_FORMAT_UNSPECIFIED');

  /// Returns only SearchMessagesResponse.messages.
  static const SearchMessagesResponseFormat
      SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES =
      SearchMessagesResponseFormat._(
          1,
          _omitEnumNames
              ? ''
              : 'SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES');

  /// Returns only SearchMessagesResponse.hits.
  static const SearchMessagesResponseFormat
      SEARCH_MESSAGES_RESPONSE_FORMAT_HITS = SearchMessagesResponseFormat._(
          2, _omitEnumNames ? '' : 'SEARCH_MESSAGES_RESPONSE_FORMAT_HITS');

  static const $core.List<SearchMessagesResponseFormat> values =
      <SearchMessagesResponseFormat>[
    SEARCH_MESSAGES_RESPONSE_FORMAT_UNSPECIFIED,
    SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES,
    SEARCH_MESSAGES_RESPONSE_FORMAT_HITS,
  ];

  static final $core.List<SearchMessagesResponseFormat?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static SearchMessagesResponseFormat? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SearchMessagesResponseFormat._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
