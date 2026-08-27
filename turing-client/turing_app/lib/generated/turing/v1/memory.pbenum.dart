// This is a generated file - do not edit.
//
// Generated from turing/v1/memory.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

class MemoryCandidateKind extends $pb.ProtobufEnum {
  static const MemoryCandidateKind MEMORY_CANDIDATE_KIND_UNSPECIFIED =
      MemoryCandidateKind._(
          0, _omitEnumNames ? '' : 'MEMORY_CANDIDATE_KIND_UNSPECIFIED');
  static const MemoryCandidateKind MEMORY_CANDIDATE_KIND_BELIEF =
      MemoryCandidateKind._(
          1, _omitEnumNames ? '' : 'MEMORY_CANDIDATE_KIND_BELIEF');
  static const MemoryCandidateKind MEMORY_CANDIDATE_KIND_PROFILE_EDIT =
      MemoryCandidateKind._(
          2, _omitEnumNames ? '' : 'MEMORY_CANDIDATE_KIND_PROFILE_EDIT');

  static const $core.List<MemoryCandidateKind> values = <MemoryCandidateKind>[
    MEMORY_CANDIDATE_KIND_UNSPECIFIED,
    MEMORY_CANDIDATE_KIND_BELIEF,
    MEMORY_CANDIDATE_KIND_PROFILE_EDIT,
  ];

  static final $core.List<MemoryCandidateKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static MemoryCandidateKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MemoryCandidateKind._(super.value, super.name);
}

class MemoryCandidateState extends $pb.ProtobufEnum {
  static const MemoryCandidateState MEMORY_CANDIDATE_STATE_UNSPECIFIED =
      MemoryCandidateState._(
          0, _omitEnumNames ? '' : 'MEMORY_CANDIDATE_STATE_UNSPECIFIED');
  static const MemoryCandidateState MEMORY_CANDIDATE_STATE_PENDING =
      MemoryCandidateState._(
          1, _omitEnumNames ? '' : 'MEMORY_CANDIDATE_STATE_PENDING');
  static const MemoryCandidateState MEMORY_CANDIDATE_STATE_PROMOTED =
      MemoryCandidateState._(
          2, _omitEnumNames ? '' : 'MEMORY_CANDIDATE_STATE_PROMOTED');
  static const MemoryCandidateState MEMORY_CANDIDATE_STATE_REJECTED =
      MemoryCandidateState._(
          3, _omitEnumNames ? '' : 'MEMORY_CANDIDATE_STATE_REJECTED');

  /// The session that produced the candidate was deleted. The candidate is kept
  /// so the record does not silently vanish, but it can no longer be promoted.
  static const MemoryCandidateState MEMORY_CANDIDATE_STATE_WITHDRAWN =
      MemoryCandidateState._(
          4, _omitEnumNames ? '' : 'MEMORY_CANDIDATE_STATE_WITHDRAWN');

  /// The user accepted this profile edit and the server claimed the apply
  /// before touching profile.md. It is not a decision waiting to be taken: the
  /// decision was taken, and what is unfinished is the write or the bookkeeping
  /// after it. No decision RPC accepts a candidate in this state — in
  /// particular a rejection cannot win once the profile may already carry these
  /// words — and a client renders it as an apply being finished rather than as
  /// a proposal with buttons.
  static const MemoryCandidateState MEMORY_CANDIDATE_STATE_PROFILE_APPLYING =
      MemoryCandidateState._(
          5, _omitEnumNames ? '' : 'MEMORY_CANDIDATE_STATE_PROFILE_APPLYING');

  static const $core.List<MemoryCandidateState> values = <MemoryCandidateState>[
    MEMORY_CANDIDATE_STATE_UNSPECIFIED,
    MEMORY_CANDIDATE_STATE_PENDING,
    MEMORY_CANDIDATE_STATE_PROMOTED,
    MEMORY_CANDIDATE_STATE_REJECTED,
    MEMORY_CANDIDATE_STATE_WITHDRAWN,
    MEMORY_CANDIDATE_STATE_PROFILE_APPLYING,
  ];

  static final $core.List<MemoryCandidateState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static MemoryCandidateState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MemoryCandidateState._(super.value, super.name);
}

/// Whether Turing may rewrite the file. An unmanaged note is one the user has
/// taken over by hand; Turing reads it and never writes it back.
class MemoryNoteStatus extends $pb.ProtobufEnum {
  static const MemoryNoteStatus MEMORY_NOTE_STATUS_UNSPECIFIED =
      MemoryNoteStatus._(
          0, _omitEnumNames ? '' : 'MEMORY_NOTE_STATUS_UNSPECIFIED');
  static const MemoryNoteStatus MEMORY_NOTE_STATUS_MANAGED =
      MemoryNoteStatus._(1, _omitEnumNames ? '' : 'MEMORY_NOTE_STATUS_MANAGED');
  static const MemoryNoteStatus MEMORY_NOTE_STATUS_UNMANAGED =
      MemoryNoteStatus._(
          2, _omitEnumNames ? '' : 'MEMORY_NOTE_STATUS_UNMANAGED');
  static const MemoryNoteStatus MEMORY_NOTE_STATUS_WITHDRAWN =
      MemoryNoteStatus._(
          3, _omitEnumNames ? '' : 'MEMORY_NOTE_STATUS_WITHDRAWN');

  static const $core.List<MemoryNoteStatus> values = <MemoryNoteStatus>[
    MEMORY_NOTE_STATUS_UNSPECIFIED,
    MEMORY_NOTE_STATUS_MANAGED,
    MEMORY_NOTE_STATUS_UNMANAGED,
    MEMORY_NOTE_STATUS_WITHDRAWN,
  ];

  static final $core.List<MemoryNoteStatus?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static MemoryNoteStatus? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MemoryNoteStatus._(super.value, super.name);
}

class MemoryProvenanceKind extends $pb.ProtobufEnum {
  static const MemoryProvenanceKind MEMORY_PROVENANCE_KIND_UNSPECIFIED =
      MemoryProvenanceKind._(
          0, _omitEnumNames ? '' : 'MEMORY_PROVENANCE_KIND_UNSPECIFIED');
  static const MemoryProvenanceKind
      MEMORY_PROVENANCE_KIND_PROMOTED_FROM_CANDIDATE = MemoryProvenanceKind._(
          1,
          _omitEnumNames
              ? ''
              : 'MEMORY_PROVENANCE_KIND_PROMOTED_FROM_CANDIDATE');
  static const MemoryProvenanceKind MEMORY_PROVENANCE_KIND_USER_AUTHORED =
      MemoryProvenanceKind._(
          2, _omitEnumNames ? '' : 'MEMORY_PROVENANCE_KIND_USER_AUTHORED');
  static const MemoryProvenanceKind MEMORY_PROVENANCE_KIND_IMPORTED =
      MemoryProvenanceKind._(
          3, _omitEnumNames ? '' : 'MEMORY_PROVENANCE_KIND_IMPORTED');

  static const $core.List<MemoryProvenanceKind> values = <MemoryProvenanceKind>[
    MEMORY_PROVENANCE_KIND_UNSPECIFIED,
    MEMORY_PROVENANCE_KIND_PROMOTED_FROM_CANDIDATE,
    MEMORY_PROVENANCE_KIND_USER_AUTHORED,
    MEMORY_PROVENANCE_KIND_IMPORTED,
  ];

  static final $core.List<MemoryProvenanceKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static MemoryProvenanceKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MemoryProvenanceKind._(super.value, super.name);
}

/// Why a read produced nothing. NONE is distinct from UNSPECIFIED so a client
/// can tell "the server answered: nothing is wrong" from "the server did not
/// say", and never renders an empty vault as a healthy one.
class MemoryUnavailableReason extends $pb.ProtobufEnum {
  static const MemoryUnavailableReason MEMORY_UNAVAILABLE_REASON_UNSPECIFIED =
      MemoryUnavailableReason._(
          0, _omitEnumNames ? '' : 'MEMORY_UNAVAILABLE_REASON_UNSPECIFIED');
  static const MemoryUnavailableReason MEMORY_UNAVAILABLE_REASON_NONE =
      MemoryUnavailableReason._(
          1, _omitEnumNames ? '' : 'MEMORY_UNAVAILABLE_REASON_NONE');
  static const MemoryUnavailableReason MEMORY_UNAVAILABLE_REASON_DISABLED =
      MemoryUnavailableReason._(
          2, _omitEnumNames ? '' : 'MEMORY_UNAVAILABLE_REASON_DISABLED');
  static const MemoryUnavailableReason MEMORY_UNAVAILABLE_REASON_VAULT_MISSING =
      MemoryUnavailableReason._(
          3, _omitEnumNames ? '' : 'MEMORY_UNAVAILABLE_REASON_VAULT_MISSING');
  static const MemoryUnavailableReason
      MEMORY_UNAVAILABLE_REASON_VAULT_UNREADABLE = MemoryUnavailableReason._(4,
          _omitEnumNames ? '' : 'MEMORY_UNAVAILABLE_REASON_VAULT_UNREADABLE');
  static const MemoryUnavailableReason
      MEMORY_UNAVAILABLE_REASON_CONTENT_PARSE_FAILED =
      MemoryUnavailableReason._(
          5,
          _omitEnumNames
              ? ''
              : 'MEMORY_UNAVAILABLE_REASON_CONTENT_PARSE_FAILED');
  static const MemoryUnavailableReason
      MEMORY_UNAVAILABLE_REASON_CONTENT_TOO_LARGE = MemoryUnavailableReason._(6,
          _omitEnumNames ? '' : 'MEMORY_UNAVAILABLE_REASON_CONTENT_TOO_LARGE');

  static const $core.List<MemoryUnavailableReason> values =
      <MemoryUnavailableReason>[
    MEMORY_UNAVAILABLE_REASON_UNSPECIFIED,
    MEMORY_UNAVAILABLE_REASON_NONE,
    MEMORY_UNAVAILABLE_REASON_DISABLED,
    MEMORY_UNAVAILABLE_REASON_VAULT_MISSING,
    MEMORY_UNAVAILABLE_REASON_VAULT_UNREADABLE,
    MEMORY_UNAVAILABLE_REASON_CONTENT_PARSE_FAILED,
    MEMORY_UNAVAILABLE_REASON_CONTENT_TOO_LARGE,
  ];

  static final $core.List<MemoryUnavailableReason?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static MemoryUnavailableReason? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MemoryUnavailableReason._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
