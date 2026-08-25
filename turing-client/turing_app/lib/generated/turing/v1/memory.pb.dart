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

import '../../google/protobuf/struct.pb.dart' as $2;
import '../../google/protobuf/timestamp.pb.dart' as $1;
import 'common.pbenum.dart' as $3;
import 'memory.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'memory.pbenum.dart';

/// Where a claim came from, and whether it still stands.
class MemoryProvenance extends $pb.GeneratedMessage {
  factory MemoryProvenance({
    MemoryProvenanceKind? kind,
    $core.String? sourceSessionId,
    $core.String? sourceSessionTitle,
    $1.Timestamp? observedAt,
    $core.bool? withdrawn,
    $1.Timestamp? withdrawnAt,
    $core.int? evidenceCount,
  }) {
    final result = create();
    if (kind != null) result.kind = kind;
    if (sourceSessionId != null) result.sourceSessionId = sourceSessionId;
    if (sourceSessionTitle != null)
      result.sourceSessionTitle = sourceSessionTitle;
    if (observedAt != null) result.observedAt = observedAt;
    if (withdrawn != null) result.withdrawn = withdrawn;
    if (withdrawnAt != null) result.withdrawnAt = withdrawnAt;
    if (evidenceCount != null) result.evidenceCount = evidenceCount;
    return result;
  }

  MemoryProvenance._();

  factory MemoryProvenance.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory MemoryProvenance.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'MemoryProvenance',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..e<MemoryProvenanceKind>(
        1, _omitFieldNames ? '' : 'kind', $pb.PbFieldType.OE,
        defaultOrMaker: MemoryProvenanceKind.MEMORY_PROVENANCE_KIND_UNSPECIFIED,
        valueOf: MemoryProvenanceKind.valueOf,
        enumValues: MemoryProvenanceKind.values)
    ..aOS(2, _omitFieldNames ? '' : 'sourceSessionId')
    ..aOS(3, _omitFieldNames ? '' : 'sourceSessionTitle')
    ..aOM<$1.Timestamp>(4, _omitFieldNames ? '' : 'observedAt',
        subBuilder: $1.Timestamp.create)
    ..aOB(5, _omitFieldNames ? '' : 'withdrawn')
    ..aOM<$1.Timestamp>(6, _omitFieldNames ? '' : 'withdrawnAt',
        subBuilder: $1.Timestamp.create)
    ..a<$core.int>(
        7, _omitFieldNames ? '' : 'evidenceCount', $pb.PbFieldType.O3)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryProvenance clone() => MemoryProvenance()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryProvenance copyWith(void Function(MemoryProvenance) updates) =>
      super.copyWith((message) => updates(message as MemoryProvenance))
          as MemoryProvenance;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static MemoryProvenance create() => MemoryProvenance._();
  @$core.override
  MemoryProvenance createEmptyInstance() => create();
  static $pb.PbList<MemoryProvenance> createRepeated() =>
      $pb.PbList<MemoryProvenance>();
  @$core.pragma('dart2js:noInline')
  static MemoryProvenance getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<MemoryProvenance>(create);
  static MemoryProvenance? _defaultInstance;

  @$pb.TagNumber(1)
  MemoryProvenanceKind get kind => $_getN(0);
  @$pb.TagNumber(1)
  set kind(MemoryProvenanceKind value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasKind() => $_has(0);
  @$pb.TagNumber(1)
  void clearKind() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get sourceSessionId => $_getSZ(1);
  @$pb.TagNumber(2)
  set sourceSessionId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasSourceSessionId() => $_has(1);
  @$pb.TagNumber(2)
  void clearSourceSessionId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get sourceSessionTitle => $_getSZ(2);
  @$pb.TagNumber(3)
  set sourceSessionTitle($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasSourceSessionTitle() => $_has(2);
  @$pb.TagNumber(3)
  void clearSourceSessionTitle() => $_clearField(3);

  @$pb.TagNumber(4)
  $1.Timestamp get observedAt => $_getN(3);
  @$pb.TagNumber(4)
  set observedAt($1.Timestamp value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasObservedAt() => $_has(3);
  @$pb.TagNumber(4)
  void clearObservedAt() => $_clearField(4);
  @$pb.TagNumber(4)
  $1.Timestamp ensureObservedAt() => $_ensure(3);

  /// Set once the originating session is gone. The note survives deletion, so
  /// the client has to be able to say the evidence behind it no longer exists
  /// rather than presenting an unsupported claim as supported.
  @$pb.TagNumber(5)
  $core.bool get withdrawn => $_getBF(4);
  @$pb.TagNumber(5)
  set withdrawn($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasWithdrawn() => $_has(4);
  @$pb.TagNumber(5)
  void clearWithdrawn() => $_clearField(5);

  @$pb.TagNumber(6)
  $1.Timestamp get withdrawnAt => $_getN(5);
  @$pb.TagNumber(6)
  set withdrawnAt($1.Timestamp value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasWithdrawnAt() => $_has(5);
  @$pb.TagNumber(6)
  void clearWithdrawnAt() => $_clearField(6);
  @$pb.TagNumber(6)
  $1.Timestamp ensureWithdrawnAt() => $_ensure(5);

  /// Count only. Excerpts are never returned; the vault holds hashes of them.
  @$pb.TagNumber(7)
  $core.int get evidenceCount => $_getIZ(6);
  @$pb.TagNumber(7)
  set evidenceCount($core.int value) => $_setSignedInt32(6, value);
  @$pb.TagNumber(7)
  $core.bool hasEvidenceCount() => $_has(6);
  @$pb.TagNumber(7)
  void clearEvidenceCount() => $_clearField(7);
}

/// A proposed memory awaiting the user's decision.
class MemoryCandidate extends $pb.GeneratedMessage {
  factory MemoryCandidate({
    $core.String? candidateId,
    MemoryCandidateKind? kind,
    $core.String? inboxPath,
    $core.String? content,
    $core.String? contentHash,
    MemoryCandidateState? state,
    $core.Iterable<MemoryProvenance>? provenance,
    $core.String? promotedNoteId,
    $1.Timestamp? createdAt,
    $1.Timestamp? updatedAt,
    $1.Timestamp? decidedAt,
    $core.String? parseError,
    MemoryUnavailableReason? unavailableReason,
    $core.bool? managed,
  }) {
    final result = create();
    if (candidateId != null) result.candidateId = candidateId;
    if (kind != null) result.kind = kind;
    if (inboxPath != null) result.inboxPath = inboxPath;
    if (content != null) result.content = content;
    if (contentHash != null) result.contentHash = contentHash;
    if (state != null) result.state = state;
    if (provenance != null) result.provenance.addAll(provenance);
    if (promotedNoteId != null) result.promotedNoteId = promotedNoteId;
    if (createdAt != null) result.createdAt = createdAt;
    if (updatedAt != null) result.updatedAt = updatedAt;
    if (decidedAt != null) result.decidedAt = decidedAt;
    if (parseError != null) result.parseError = parseError;
    if (unavailableReason != null) result.unavailableReason = unavailableReason;
    if (managed != null) result.managed = managed;
    return result;
  }

  MemoryCandidate._();

  factory MemoryCandidate.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory MemoryCandidate.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'MemoryCandidate',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'candidateId')
    ..e<MemoryCandidateKind>(
        2, _omitFieldNames ? '' : 'kind', $pb.PbFieldType.OE,
        defaultOrMaker: MemoryCandidateKind.MEMORY_CANDIDATE_KIND_UNSPECIFIED,
        valueOf: MemoryCandidateKind.valueOf,
        enumValues: MemoryCandidateKind.values)
    ..aOS(3, _omitFieldNames ? '' : 'inboxPath')
    ..aOS(4, _omitFieldNames ? '' : 'content')
    ..aOS(5, _omitFieldNames ? '' : 'contentHash')
    ..e<MemoryCandidateState>(
        6, _omitFieldNames ? '' : 'state', $pb.PbFieldType.OE,
        defaultOrMaker: MemoryCandidateState.MEMORY_CANDIDATE_STATE_UNSPECIFIED,
        valueOf: MemoryCandidateState.valueOf,
        enumValues: MemoryCandidateState.values)
    ..pc<MemoryProvenance>(
        7, _omitFieldNames ? '' : 'provenance', $pb.PbFieldType.PM,
        subBuilder: MemoryProvenance.create)
    ..aOS(8, _omitFieldNames ? '' : 'promotedNoteId')
    ..aOM<$1.Timestamp>(9, _omitFieldNames ? '' : 'createdAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(10, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(11, _omitFieldNames ? '' : 'decidedAt',
        subBuilder: $1.Timestamp.create)
    ..aOS(12, _omitFieldNames ? '' : 'parseError')
    ..e<MemoryUnavailableReason>(
        13, _omitFieldNames ? '' : 'unavailableReason', $pb.PbFieldType.OE,
        defaultOrMaker:
            MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_UNSPECIFIED,
        valueOf: MemoryUnavailableReason.valueOf,
        enumValues: MemoryUnavailableReason.values)
    ..aOB(14, _omitFieldNames ? '' : 'managed')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryCandidate clone() => MemoryCandidate()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryCandidate copyWith(void Function(MemoryCandidate) updates) =>
      super.copyWith((message) => updates(message as MemoryCandidate))
          as MemoryCandidate;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static MemoryCandidate create() => MemoryCandidate._();
  @$core.override
  MemoryCandidate createEmptyInstance() => create();
  static $pb.PbList<MemoryCandidate> createRepeated() =>
      $pb.PbList<MemoryCandidate>();
  @$core.pragma('dart2js:noInline')
  static MemoryCandidate getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<MemoryCandidate>(create);
  static MemoryCandidate? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get candidateId => $_getSZ(0);
  @$pb.TagNumber(1)
  set candidateId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasCandidateId() => $_has(0);
  @$pb.TagNumber(1)
  void clearCandidateId() => $_clearField(1);

  @$pb.TagNumber(2)
  MemoryCandidateKind get kind => $_getN(1);
  @$pb.TagNumber(2)
  set kind(MemoryCandidateKind value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasKind() => $_has(1);
  @$pb.TagNumber(2)
  void clearKind() => $_clearField(2);

  /// Path of the inbox file, relative to the vault root, so the user can open
  /// the same thing Turing is describing.
  @$pb.TagNumber(3)
  $core.String get inboxPath => $_getSZ(2);
  @$pb.TagNumber(3)
  set inboxPath($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasInboxPath() => $_has(2);
  @$pb.TagNumber(3)
  void clearInboxPath() => $_clearField(3);

  /// The complete proposed text, never a preview or truncation: the user is
  /// being asked to accept exactly this.
  @$pb.TagNumber(4)
  $core.String get content => $_getSZ(3);
  @$pb.TagNumber(4)
  set content($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasContent() => $_has(3);
  @$pb.TagNumber(4)
  void clearContent() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get contentHash => $_getSZ(4);
  @$pb.TagNumber(5)
  set contentHash($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasContentHash() => $_has(4);
  @$pb.TagNumber(5)
  void clearContentHash() => $_clearField(5);

  @$pb.TagNumber(6)
  MemoryCandidateState get state => $_getN(5);
  @$pb.TagNumber(6)
  set state(MemoryCandidateState value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasState() => $_has(5);
  @$pb.TagNumber(6)
  void clearState() => $_clearField(6);

  @$pb.TagNumber(7)
  $pb.PbList<MemoryProvenance> get provenance => $_getList(6);

  @$pb.TagNumber(8)
  $core.String get promotedNoteId => $_getSZ(7);
  @$pb.TagNumber(8)
  set promotedNoteId($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasPromotedNoteId() => $_has(7);
  @$pb.TagNumber(8)
  void clearPromotedNoteId() => $_clearField(8);

  @$pb.TagNumber(9)
  $1.Timestamp get createdAt => $_getN(8);
  @$pb.TagNumber(9)
  set createdAt($1.Timestamp value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasCreatedAt() => $_has(8);
  @$pb.TagNumber(9)
  void clearCreatedAt() => $_clearField(9);
  @$pb.TagNumber(9)
  $1.Timestamp ensureCreatedAt() => $_ensure(8);

  @$pb.TagNumber(10)
  $1.Timestamp get updatedAt => $_getN(9);
  @$pb.TagNumber(10)
  set updatedAt($1.Timestamp value) => $_setField(10, value);
  @$pb.TagNumber(10)
  $core.bool hasUpdatedAt() => $_has(9);
  @$pb.TagNumber(10)
  void clearUpdatedAt() => $_clearField(10);
  @$pb.TagNumber(10)
  $1.Timestamp ensureUpdatedAt() => $_ensure(9);

  @$pb.TagNumber(11)
  $1.Timestamp get decidedAt => $_getN(10);
  @$pb.TagNumber(11)
  set decidedAt($1.Timestamp value) => $_setField(11, value);
  @$pb.TagNumber(11)
  $core.bool hasDecidedAt() => $_has(10);
  @$pb.TagNumber(11)
  void clearDecidedAt() => $_clearField(11);
  @$pb.TagNumber(11)
  $1.Timestamp ensureDecidedAt() => $_ensure(10);

  /// Human-readable detail for unavailable_reason. Empty when content is whole.
  @$pb.TagNumber(12)
  $core.String get parseError => $_getSZ(11);
  @$pb.TagNumber(12)
  set parseError($core.String value) => $_setString(11, value);
  @$pb.TagNumber(12)
  $core.bool hasParseError() => $_has(11);
  @$pb.TagNumber(12)
  void clearParseError() => $_clearField(12);

  @$pb.TagNumber(13)
  MemoryUnavailableReason get unavailableReason => $_getN(12);
  @$pb.TagNumber(13)
  set unavailableReason(MemoryUnavailableReason value) => $_setField(13, value);
  @$pb.TagNumber(13)
  $core.bool hasUnavailableReason() => $_has(12);
  @$pb.TagNumber(13)
  void clearUnavailableReason() => $_clearField(13);

  /// False for a draft the user dropped into the inbox themselves. Turing has
  /// no row for it and will not rewrite or move it, so it is listed for reading
  /// and there is no RPC that promotes it — the user moves the file. A client
  /// that rendered an unmanaged draft with a Promote button would be offering
  /// an action the server refuses.
  @$pb.TagNumber(14)
  $core.bool get managed => $_getBF(13);
  @$pb.TagNumber(14)
  set managed($core.bool value) => $_setBool(13, value);
  @$pb.TagNumber(14)
  $core.bool hasManaged() => $_has(13);
  @$pb.TagNumber(14)
  void clearManaged() => $_clearField(14);
}

/// An accepted memory: a Markdown file in the user's vault.
class MemoryNote extends $pb.GeneratedMessage {
  factory MemoryNote({
    $core.String? noteId,
    $core.String? path,
    $core.String? title,
    $core.String? content,
    $core.String? contentHash,
    MemoryNoteStatus? status,
    $3.MemoryTier? tier,
    $core.Iterable<MemoryProvenance>? provenance,
    $1.Timestamp? createdAt,
    $1.Timestamp? updatedAt,
    $core.String? parseError,
    MemoryUnavailableReason? unavailableReason,
  }) {
    final result = create();
    if (noteId != null) result.noteId = noteId;
    if (path != null) result.path = path;
    if (title != null) result.title = title;
    if (content != null) result.content = content;
    if (contentHash != null) result.contentHash = contentHash;
    if (status != null) result.status = status;
    if (tier != null) result.tier = tier;
    if (provenance != null) result.provenance.addAll(provenance);
    if (createdAt != null) result.createdAt = createdAt;
    if (updatedAt != null) result.updatedAt = updatedAt;
    if (parseError != null) result.parseError = parseError;
    if (unavailableReason != null) result.unavailableReason = unavailableReason;
    return result;
  }

  MemoryNote._();

  factory MemoryNote.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory MemoryNote.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'MemoryNote',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'noteId')
    ..aOS(2, _omitFieldNames ? '' : 'path')
    ..aOS(3, _omitFieldNames ? '' : 'title')
    ..aOS(4, _omitFieldNames ? '' : 'content')
    ..aOS(5, _omitFieldNames ? '' : 'contentHash')
    ..e<MemoryNoteStatus>(
        6, _omitFieldNames ? '' : 'status', $pb.PbFieldType.OE,
        defaultOrMaker: MemoryNoteStatus.MEMORY_NOTE_STATUS_UNSPECIFIED,
        valueOf: MemoryNoteStatus.valueOf,
        enumValues: MemoryNoteStatus.values)
    ..e<$3.MemoryTier>(7, _omitFieldNames ? '' : 'tier', $pb.PbFieldType.OE,
        defaultOrMaker: $3.MemoryTier.MEMORY_TIER_UNSPECIFIED,
        valueOf: $3.MemoryTier.valueOf,
        enumValues: $3.MemoryTier.values)
    ..pc<MemoryProvenance>(
        8, _omitFieldNames ? '' : 'provenance', $pb.PbFieldType.PM,
        subBuilder: MemoryProvenance.create)
    ..aOM<$1.Timestamp>(9, _omitFieldNames ? '' : 'createdAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(10, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $1.Timestamp.create)
    ..aOS(11, _omitFieldNames ? '' : 'parseError')
    ..e<MemoryUnavailableReason>(
        12, _omitFieldNames ? '' : 'unavailableReason', $pb.PbFieldType.OE,
        defaultOrMaker:
            MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_UNSPECIFIED,
        valueOf: MemoryUnavailableReason.valueOf,
        enumValues: MemoryUnavailableReason.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryNote clone() => MemoryNote()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryNote copyWith(void Function(MemoryNote) updates) =>
      super.copyWith((message) => updates(message as MemoryNote)) as MemoryNote;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static MemoryNote create() => MemoryNote._();
  @$core.override
  MemoryNote createEmptyInstance() => create();
  static $pb.PbList<MemoryNote> createRepeated() => $pb.PbList<MemoryNote>();
  @$core.pragma('dart2js:noInline')
  static MemoryNote getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<MemoryNote>(create);
  static MemoryNote? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get noteId => $_getSZ(0);
  @$pb.TagNumber(1)
  set noteId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasNoteId() => $_has(0);
  @$pb.TagNumber(1)
  void clearNoteId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get path => $_getSZ(1);
  @$pb.TagNumber(2)
  set path($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasPath() => $_has(1);
  @$pb.TagNumber(2)
  void clearPath() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get title => $_getSZ(2);
  @$pb.TagNumber(3)
  set title($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasTitle() => $_has(2);
  @$pb.TagNumber(3)
  void clearTitle() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get content => $_getSZ(3);
  @$pb.TagNumber(4)
  set content($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasContent() => $_has(3);
  @$pb.TagNumber(4)
  void clearContent() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get contentHash => $_getSZ(4);
  @$pb.TagNumber(5)
  set contentHash($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasContentHash() => $_has(4);
  @$pb.TagNumber(5)
  void clearContentHash() => $_clearField(5);

  @$pb.TagNumber(6)
  MemoryNoteStatus get status => $_getN(5);
  @$pb.TagNumber(6)
  set status(MemoryNoteStatus value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasStatus() => $_has(5);
  @$pb.TagNumber(6)
  void clearStatus() => $_clearField(6);

  @$pb.TagNumber(7)
  $3.MemoryTier get tier => $_getN(6);
  @$pb.TagNumber(7)
  set tier($3.MemoryTier value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasTier() => $_has(6);
  @$pb.TagNumber(7)
  void clearTier() => $_clearField(7);

  @$pb.TagNumber(8)
  $pb.PbList<MemoryProvenance> get provenance => $_getList(7);

  @$pb.TagNumber(9)
  $1.Timestamp get createdAt => $_getN(8);
  @$pb.TagNumber(9)
  set createdAt($1.Timestamp value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasCreatedAt() => $_has(8);
  @$pb.TagNumber(9)
  void clearCreatedAt() => $_clearField(9);
  @$pb.TagNumber(9)
  $1.Timestamp ensureCreatedAt() => $_ensure(8);

  @$pb.TagNumber(10)
  $1.Timestamp get updatedAt => $_getN(9);
  @$pb.TagNumber(10)
  set updatedAt($1.Timestamp value) => $_setField(10, value);
  @$pb.TagNumber(10)
  $core.bool hasUpdatedAt() => $_has(9);
  @$pb.TagNumber(10)
  void clearUpdatedAt() => $_clearField(10);
  @$pb.TagNumber(10)
  $1.Timestamp ensureUpdatedAt() => $_ensure(9);

  @$pb.TagNumber(11)
  $core.String get parseError => $_getSZ(10);
  @$pb.TagNumber(11)
  set parseError($core.String value) => $_setString(10, value);
  @$pb.TagNumber(11)
  $core.bool hasParseError() => $_has(10);
  @$pb.TagNumber(11)
  void clearParseError() => $_clearField(11);

  @$pb.TagNumber(12)
  MemoryUnavailableReason get unavailableReason => $_getN(11);
  @$pb.TagNumber(12)
  set unavailableReason(MemoryUnavailableReason value) => $_setField(12, value);
  @$pb.TagNumber(12)
  $core.bool hasUnavailableReason() => $_has(11);
  @$pb.TagNumber(12)
  void clearUnavailableReason() => $_clearField(12);
}

/// The single profile document.
class MemoryProfile extends $pb.GeneratedMessage {
  factory MemoryProfile({
    $core.String? content,
    $core.String? contentHash,
    MemoryNoteStatus? status,
    $1.Timestamp? updatedAt,
    $core.String? parseError,
    MemoryUnavailableReason? unavailableReason,
  }) {
    final result = create();
    if (content != null) result.content = content;
    if (contentHash != null) result.contentHash = contentHash;
    if (status != null) result.status = status;
    if (updatedAt != null) result.updatedAt = updatedAt;
    if (parseError != null) result.parseError = parseError;
    if (unavailableReason != null) result.unavailableReason = unavailableReason;
    return result;
  }

  MemoryProfile._();

  factory MemoryProfile.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory MemoryProfile.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'MemoryProfile',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'content')
    ..aOS(2, _omitFieldNames ? '' : 'contentHash')
    ..e<MemoryNoteStatus>(
        3, _omitFieldNames ? '' : 'status', $pb.PbFieldType.OE,
        defaultOrMaker: MemoryNoteStatus.MEMORY_NOTE_STATUS_UNSPECIFIED,
        valueOf: MemoryNoteStatus.valueOf,
        enumValues: MemoryNoteStatus.values)
    ..aOM<$1.Timestamp>(4, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $1.Timestamp.create)
    ..aOS(5, _omitFieldNames ? '' : 'parseError')
    ..e<MemoryUnavailableReason>(
        6, _omitFieldNames ? '' : 'unavailableReason', $pb.PbFieldType.OE,
        defaultOrMaker:
            MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_UNSPECIFIED,
        valueOf: MemoryUnavailableReason.valueOf,
        enumValues: MemoryUnavailableReason.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryProfile clone() => MemoryProfile()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryProfile copyWith(void Function(MemoryProfile) updates) =>
      super.copyWith((message) => updates(message as MemoryProfile))
          as MemoryProfile;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static MemoryProfile create() => MemoryProfile._();
  @$core.override
  MemoryProfile createEmptyInstance() => create();
  static $pb.PbList<MemoryProfile> createRepeated() =>
      $pb.PbList<MemoryProfile>();
  @$core.pragma('dart2js:noInline')
  static MemoryProfile getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<MemoryProfile>(create);
  static MemoryProfile? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get content => $_getSZ(0);
  @$pb.TagNumber(1)
  set content($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasContent() => $_has(0);
  @$pb.TagNumber(1)
  void clearContent() => $_clearField(1);

  /// Doubles as the compare-and-set token for ApplyMemoryProfile.
  @$pb.TagNumber(2)
  $core.String get contentHash => $_getSZ(1);
  @$pb.TagNumber(2)
  set contentHash($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasContentHash() => $_has(1);
  @$pb.TagNumber(2)
  void clearContentHash() => $_clearField(2);

  @$pb.TagNumber(3)
  MemoryNoteStatus get status => $_getN(2);
  @$pb.TagNumber(3)
  set status(MemoryNoteStatus value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasStatus() => $_has(2);
  @$pb.TagNumber(3)
  void clearStatus() => $_clearField(3);

  @$pb.TagNumber(4)
  $1.Timestamp get updatedAt => $_getN(3);
  @$pb.TagNumber(4)
  set updatedAt($1.Timestamp value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasUpdatedAt() => $_has(3);
  @$pb.TagNumber(4)
  void clearUpdatedAt() => $_clearField(4);
  @$pb.TagNumber(4)
  $1.Timestamp ensureUpdatedAt() => $_ensure(3);

  @$pb.TagNumber(5)
  $core.String get parseError => $_getSZ(4);
  @$pb.TagNumber(5)
  set parseError($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasParseError() => $_has(4);
  @$pb.TagNumber(5)
  void clearParseError() => $_clearField(5);

  @$pb.TagNumber(6)
  MemoryUnavailableReason get unavailableReason => $_getN(5);
  @$pb.TagNumber(6)
  set unavailableReason(MemoryUnavailableReason value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasUnavailableReason() => $_has(5);
  @$pb.TagNumber(6)
  void clearUnavailableReason() => $_clearField(6);
}

class MemoryTierState extends $pb.GeneratedMessage {
  factory MemoryTierState({
    $3.MemoryTier? tier,
    $core.bool? enabled,
    $core.int? noteCount,
    $core.int? pendingCandidateCount,
    $1.Timestamp? updatedAt,
    MemoryUnavailableReason? unavailableReason,
    $core.String? parseError,
  }) {
    final result = create();
    if (tier != null) result.tier = tier;
    if (enabled != null) result.enabled = enabled;
    if (noteCount != null) result.noteCount = noteCount;
    if (pendingCandidateCount != null)
      result.pendingCandidateCount = pendingCandidateCount;
    if (updatedAt != null) result.updatedAt = updatedAt;
    if (unavailableReason != null) result.unavailableReason = unavailableReason;
    if (parseError != null) result.parseError = parseError;
    return result;
  }

  MemoryTierState._();

  factory MemoryTierState.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory MemoryTierState.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'MemoryTierState',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..e<$3.MemoryTier>(1, _omitFieldNames ? '' : 'tier', $pb.PbFieldType.OE,
        defaultOrMaker: $3.MemoryTier.MEMORY_TIER_UNSPECIFIED,
        valueOf: $3.MemoryTier.valueOf,
        enumValues: $3.MemoryTier.values)
    ..aOB(2, _omitFieldNames ? '' : 'enabled')
    ..a<$core.int>(3, _omitFieldNames ? '' : 'noteCount', $pb.PbFieldType.O3)
    ..a<$core.int>(
        4, _omitFieldNames ? '' : 'pendingCandidateCount', $pb.PbFieldType.O3)
    ..aOM<$1.Timestamp>(5, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $1.Timestamp.create)
    ..e<MemoryUnavailableReason>(
        6, _omitFieldNames ? '' : 'unavailableReason', $pb.PbFieldType.OE,
        defaultOrMaker:
            MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_UNSPECIFIED,
        valueOf: MemoryUnavailableReason.valueOf,
        enumValues: MemoryUnavailableReason.values)
    ..aOS(7, _omitFieldNames ? '' : 'parseError')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryTierState clone() => MemoryTierState()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryTierState copyWith(void Function(MemoryTierState) updates) =>
      super.copyWith((message) => updates(message as MemoryTierState))
          as MemoryTierState;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static MemoryTierState create() => MemoryTierState._();
  @$core.override
  MemoryTierState createEmptyInstance() => create();
  static $pb.PbList<MemoryTierState> createRepeated() =>
      $pb.PbList<MemoryTierState>();
  @$core.pragma('dart2js:noInline')
  static MemoryTierState getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<MemoryTierState>(create);
  static MemoryTierState? _defaultInstance;

  @$pb.TagNumber(1)
  $3.MemoryTier get tier => $_getN(0);
  @$pb.TagNumber(1)
  set tier($3.MemoryTier value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasTier() => $_has(0);
  @$pb.TagNumber(1)
  void clearTier() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get enabled => $_getBF(1);
  @$pb.TagNumber(2)
  set enabled($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasEnabled() => $_has(1);
  @$pb.TagNumber(2)
  void clearEnabled() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.int get noteCount => $_getIZ(2);
  @$pb.TagNumber(3)
  set noteCount($core.int value) => $_setSignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasNoteCount() => $_has(2);
  @$pb.TagNumber(3)
  void clearNoteCount() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.int get pendingCandidateCount => $_getIZ(3);
  @$pb.TagNumber(4)
  set pendingCandidateCount($core.int value) => $_setSignedInt32(3, value);
  @$pb.TagNumber(4)
  $core.bool hasPendingCandidateCount() => $_has(3);
  @$pb.TagNumber(4)
  void clearPendingCandidateCount() => $_clearField(4);

  @$pb.TagNumber(5)
  $1.Timestamp get updatedAt => $_getN(4);
  @$pb.TagNumber(5)
  set updatedAt($1.Timestamp value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasUpdatedAt() => $_has(4);
  @$pb.TagNumber(5)
  void clearUpdatedAt() => $_clearField(5);
  @$pb.TagNumber(5)
  $1.Timestamp ensureUpdatedAt() => $_ensure(4);

  @$pb.TagNumber(6)
  MemoryUnavailableReason get unavailableReason => $_getN(5);
  @$pb.TagNumber(6)
  set unavailableReason(MemoryUnavailableReason value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasUnavailableReason() => $_has(5);
  @$pb.TagNumber(6)
  void clearUnavailableReason() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.String get parseError => $_getSZ(6);
  @$pb.TagNumber(7)
  set parseError($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasParseError() => $_has(6);
  @$pb.TagNumber(7)
  void clearParseError() => $_clearField(7);
}

class MemorySettings extends $pb.GeneratedMessage {
  factory MemorySettings({
    $core.bool? enabled,
    $core.String? vaultRoot,
    $core.bool? vaultWritable,
    MemoryUnavailableReason? unavailableReason,
    $core.String? parseError,
  }) {
    final result = create();
    if (enabled != null) result.enabled = enabled;
    if (vaultRoot != null) result.vaultRoot = vaultRoot;
    if (vaultWritable != null) result.vaultWritable = vaultWritable;
    if (unavailableReason != null) result.unavailableReason = unavailableReason;
    if (parseError != null) result.parseError = parseError;
    return result;
  }

  MemorySettings._();

  factory MemorySettings.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory MemorySettings.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'MemorySettings',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOB(1, _omitFieldNames ? '' : 'enabled')
    ..aOS(2, _omitFieldNames ? '' : 'vaultRoot')
    ..aOB(3, _omitFieldNames ? '' : 'vaultWritable')
    ..e<MemoryUnavailableReason>(
        4, _omitFieldNames ? '' : 'unavailableReason', $pb.PbFieldType.OE,
        defaultOrMaker:
            MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_UNSPECIFIED,
        valueOf: MemoryUnavailableReason.valueOf,
        enumValues: MemoryUnavailableReason.values)
    ..aOS(5, _omitFieldNames ? '' : 'parseError')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemorySettings clone() => MemorySettings()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemorySettings copyWith(void Function(MemorySettings) updates) =>
      super.copyWith((message) => updates(message as MemorySettings))
          as MemorySettings;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static MemorySettings create() => MemorySettings._();
  @$core.override
  MemorySettings createEmptyInstance() => create();
  static $pb.PbList<MemorySettings> createRepeated() =>
      $pb.PbList<MemorySettings>();
  @$core.pragma('dart2js:noInline')
  static MemorySettings getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<MemorySettings>(create);
  static MemorySettings? _defaultInstance;

  @$pb.TagNumber(1)
  $core.bool get enabled => $_getBF(0);
  @$pb.TagNumber(1)
  set enabled($core.bool value) => $_setBool(0, value);
  @$pb.TagNumber(1)
  $core.bool hasEnabled() => $_has(0);
  @$pb.TagNumber(1)
  void clearEnabled() => $_clearField(1);

  /// Absolute path so the user can open the vault in their own editor.
  @$pb.TagNumber(2)
  $core.String get vaultRoot => $_getSZ(1);
  @$pb.TagNumber(2)
  set vaultRoot($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasVaultRoot() => $_has(1);
  @$pb.TagNumber(2)
  void clearVaultRoot() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.bool get vaultWritable => $_getBF(2);
  @$pb.TagNumber(3)
  set vaultWritable($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasVaultWritable() => $_has(2);
  @$pb.TagNumber(3)
  void clearVaultWritable() => $_clearField(3);

  @$pb.TagNumber(4)
  MemoryUnavailableReason get unavailableReason => $_getN(3);
  @$pb.TagNumber(4)
  set unavailableReason(MemoryUnavailableReason value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasUnavailableReason() => $_has(3);
  @$pb.TagNumber(4)
  void clearUnavailableReason() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get parseError => $_getSZ(4);
  @$pb.TagNumber(5)
  set parseError($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasParseError() => $_has(4);
  @$pb.TagNumber(5)
  void clearParseError() => $_clearField(5);
}

class ListMemoryStateRequest extends $pb.GeneratedMessage {
  factory ListMemoryStateRequest() => create();

  ListMemoryStateRequest._();

  factory ListMemoryStateRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListMemoryStateRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListMemoryStateRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryStateRequest clone() =>
      ListMemoryStateRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryStateRequest copyWith(
          void Function(ListMemoryStateRequest) updates) =>
      super.copyWith((message) => updates(message as ListMemoryStateRequest))
          as ListMemoryStateRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListMemoryStateRequest create() => ListMemoryStateRequest._();
  @$core.override
  ListMemoryStateRequest createEmptyInstance() => create();
  static $pb.PbList<ListMemoryStateRequest> createRepeated() =>
      $pb.PbList<ListMemoryStateRequest>();
  @$core.pragma('dart2js:noInline')
  static ListMemoryStateRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListMemoryStateRequest>(create);
  static ListMemoryStateRequest? _defaultInstance;
}

class ListMemoryStateResponse extends $pb.GeneratedMessage {
  factory ListMemoryStateResponse({
    MemorySettings? settings,
    $core.Iterable<MemoryTierState>? tiers,
    $core.Iterable<MemoryNote>? notes,
    $core.Iterable<MemoryCandidate>? candidates,
    MemoryProfile? profile,
  }) {
    final result = create();
    if (settings != null) result.settings = settings;
    if (tiers != null) result.tiers.addAll(tiers);
    if (notes != null) result.notes.addAll(notes);
    if (candidates != null) result.candidates.addAll(candidates);
    if (profile != null) result.profile = profile;
    return result;
  }

  ListMemoryStateResponse._();

  factory ListMemoryStateResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListMemoryStateResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListMemoryStateResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOM<MemorySettings>(1, _omitFieldNames ? '' : 'settings',
        subBuilder: MemorySettings.create)
    ..pc<MemoryTierState>(2, _omitFieldNames ? '' : 'tiers', $pb.PbFieldType.PM,
        subBuilder: MemoryTierState.create)
    ..pc<MemoryNote>(3, _omitFieldNames ? '' : 'notes', $pb.PbFieldType.PM,
        subBuilder: MemoryNote.create)
    ..pc<MemoryCandidate>(
        4, _omitFieldNames ? '' : 'candidates', $pb.PbFieldType.PM,
        subBuilder: MemoryCandidate.create)
    ..aOM<MemoryProfile>(5, _omitFieldNames ? '' : 'profile',
        subBuilder: MemoryProfile.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryStateResponse clone() =>
      ListMemoryStateResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryStateResponse copyWith(
          void Function(ListMemoryStateResponse) updates) =>
      super.copyWith((message) => updates(message as ListMemoryStateResponse))
          as ListMemoryStateResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListMemoryStateResponse create() => ListMemoryStateResponse._();
  @$core.override
  ListMemoryStateResponse createEmptyInstance() => create();
  static $pb.PbList<ListMemoryStateResponse> createRepeated() =>
      $pb.PbList<ListMemoryStateResponse>();
  @$core.pragma('dart2js:noInline')
  static ListMemoryStateResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListMemoryStateResponse>(create);
  static ListMemoryStateResponse? _defaultInstance;

  @$pb.TagNumber(1)
  MemorySettings get settings => $_getN(0);
  @$pb.TagNumber(1)
  set settings(MemorySettings value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasSettings() => $_has(0);
  @$pb.TagNumber(1)
  void clearSettings() => $_clearField(1);
  @$pb.TagNumber(1)
  MemorySettings ensureSettings() => $_ensure(0);

  @$pb.TagNumber(2)
  $pb.PbList<MemoryTierState> get tiers => $_getList(1);

  @$pb.TagNumber(3)
  $pb.PbList<MemoryNote> get notes => $_getList(2);

  @$pb.TagNumber(4)
  $pb.PbList<MemoryCandidate> get candidates => $_getList(3);

  @$pb.TagNumber(5)
  MemoryProfile get profile => $_getN(4);
  @$pb.TagNumber(5)
  set profile(MemoryProfile value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasProfile() => $_has(4);
  @$pb.TagNumber(5)
  void clearProfile() => $_clearField(5);
  @$pb.TagNumber(5)
  MemoryProfile ensureProfile() => $_ensure(4);
}

class GetMemorySettingsRequest extends $pb.GeneratedMessage {
  factory GetMemorySettingsRequest() => create();

  GetMemorySettingsRequest._();

  factory GetMemorySettingsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetMemorySettingsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetMemorySettingsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetMemorySettingsRequest clone() =>
      GetMemorySettingsRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetMemorySettingsRequest copyWith(
          void Function(GetMemorySettingsRequest) updates) =>
      super.copyWith((message) => updates(message as GetMemorySettingsRequest))
          as GetMemorySettingsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetMemorySettingsRequest create() => GetMemorySettingsRequest._();
  @$core.override
  GetMemorySettingsRequest createEmptyInstance() => create();
  static $pb.PbList<GetMemorySettingsRequest> createRepeated() =>
      $pb.PbList<GetMemorySettingsRequest>();
  @$core.pragma('dart2js:noInline')
  static GetMemorySettingsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetMemorySettingsRequest>(create);
  static GetMemorySettingsRequest? _defaultInstance;
}

class SetMemoryEnabledRequest extends $pb.GeneratedMessage {
  factory SetMemoryEnabledRequest({
    $core.bool? enabled,
    $3.MemoryTier? tier,
  }) {
    final result = create();
    if (enabled != null) result.enabled = enabled;
    if (tier != null) result.tier = tier;
    return result;
  }

  SetMemoryEnabledRequest._();

  factory SetMemoryEnabledRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetMemoryEnabledRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetMemoryEnabledRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOB(1, _omitFieldNames ? '' : 'enabled')
    ..e<$3.MemoryTier>(2, _omitFieldNames ? '' : 'tier', $pb.PbFieldType.OE,
        defaultOrMaker: $3.MemoryTier.MEMORY_TIER_UNSPECIFIED,
        valueOf: $3.MemoryTier.valueOf,
        enumValues: $3.MemoryTier.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetMemoryEnabledRequest clone() =>
      SetMemoryEnabledRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetMemoryEnabledRequest copyWith(
          void Function(SetMemoryEnabledRequest) updates) =>
      super.copyWith((message) => updates(message as SetMemoryEnabledRequest))
          as SetMemoryEnabledRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetMemoryEnabledRequest create() => SetMemoryEnabledRequest._();
  @$core.override
  SetMemoryEnabledRequest createEmptyInstance() => create();
  static $pb.PbList<SetMemoryEnabledRequest> createRepeated() =>
      $pb.PbList<SetMemoryEnabledRequest>();
  @$core.pragma('dart2js:noInline')
  static SetMemoryEnabledRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetMemoryEnabledRequest>(create);
  static SetMemoryEnabledRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.bool get enabled => $_getBF(0);
  @$pb.TagNumber(1)
  set enabled($core.bool value) => $_setBool(0, value);
  @$pb.TagNumber(1)
  $core.bool hasEnabled() => $_has(0);
  @$pb.TagNumber(1)
  void clearEnabled() => $_clearField(1);

  /// Unset (MEMORY_TIER_UNSPECIFIED) toggles memory as a whole.
  @$pb.TagNumber(2)
  $3.MemoryTier get tier => $_getN(1);
  @$pb.TagNumber(2)
  set tier($3.MemoryTier value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasTier() => $_has(1);
  @$pb.TagNumber(2)
  void clearTier() => $_clearField(2);
}

class ListMemoryCandidatesRequest extends $pb.GeneratedMessage {
  factory ListMemoryCandidatesRequest({
    MemoryCandidateState? state,
    MemoryCandidateKind? kind,
  }) {
    final result = create();
    if (state != null) result.state = state;
    if (kind != null) result.kind = kind;
    return result;
  }

  ListMemoryCandidatesRequest._();

  factory ListMemoryCandidatesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListMemoryCandidatesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListMemoryCandidatesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..e<MemoryCandidateState>(
        1, _omitFieldNames ? '' : 'state', $pb.PbFieldType.OE,
        defaultOrMaker: MemoryCandidateState.MEMORY_CANDIDATE_STATE_UNSPECIFIED,
        valueOf: MemoryCandidateState.valueOf,
        enumValues: MemoryCandidateState.values)
    ..e<MemoryCandidateKind>(
        2, _omitFieldNames ? '' : 'kind', $pb.PbFieldType.OE,
        defaultOrMaker: MemoryCandidateKind.MEMORY_CANDIDATE_KIND_UNSPECIFIED,
        valueOf: MemoryCandidateKind.valueOf,
        enumValues: MemoryCandidateKind.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryCandidatesRequest clone() =>
      ListMemoryCandidatesRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryCandidatesRequest copyWith(
          void Function(ListMemoryCandidatesRequest) updates) =>
      super.copyWith(
              (message) => updates(message as ListMemoryCandidatesRequest))
          as ListMemoryCandidatesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListMemoryCandidatesRequest create() =>
      ListMemoryCandidatesRequest._();
  @$core.override
  ListMemoryCandidatesRequest createEmptyInstance() => create();
  static $pb.PbList<ListMemoryCandidatesRequest> createRepeated() =>
      $pb.PbList<ListMemoryCandidatesRequest>();
  @$core.pragma('dart2js:noInline')
  static ListMemoryCandidatesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListMemoryCandidatesRequest>(create);
  static ListMemoryCandidatesRequest? _defaultInstance;

  @$pb.TagNumber(1)
  MemoryCandidateState get state => $_getN(0);
  @$pb.TagNumber(1)
  set state(MemoryCandidateState value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasState() => $_has(0);
  @$pb.TagNumber(1)
  void clearState() => $_clearField(1);

  @$pb.TagNumber(2)
  MemoryCandidateKind get kind => $_getN(1);
  @$pb.TagNumber(2)
  set kind(MemoryCandidateKind value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasKind() => $_has(1);
  @$pb.TagNumber(2)
  void clearKind() => $_clearField(2);
}

class ListMemoryCandidatesResponse extends $pb.GeneratedMessage {
  factory ListMemoryCandidatesResponse({
    $core.Iterable<MemoryCandidate>? candidates,
    MemoryUnavailableReason? unavailableReason,
  }) {
    final result = create();
    if (candidates != null) result.candidates.addAll(candidates);
    if (unavailableReason != null) result.unavailableReason = unavailableReason;
    return result;
  }

  ListMemoryCandidatesResponse._();

  factory ListMemoryCandidatesResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListMemoryCandidatesResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListMemoryCandidatesResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..pc<MemoryCandidate>(
        1, _omitFieldNames ? '' : 'candidates', $pb.PbFieldType.PM,
        subBuilder: MemoryCandidate.create)
    ..e<MemoryUnavailableReason>(
        2, _omitFieldNames ? '' : 'unavailableReason', $pb.PbFieldType.OE,
        defaultOrMaker:
            MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_UNSPECIFIED,
        valueOf: MemoryUnavailableReason.valueOf,
        enumValues: MemoryUnavailableReason.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryCandidatesResponse clone() =>
      ListMemoryCandidatesResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryCandidatesResponse copyWith(
          void Function(ListMemoryCandidatesResponse) updates) =>
      super.copyWith(
              (message) => updates(message as ListMemoryCandidatesResponse))
          as ListMemoryCandidatesResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListMemoryCandidatesResponse create() =>
      ListMemoryCandidatesResponse._();
  @$core.override
  ListMemoryCandidatesResponse createEmptyInstance() => create();
  static $pb.PbList<ListMemoryCandidatesResponse> createRepeated() =>
      $pb.PbList<ListMemoryCandidatesResponse>();
  @$core.pragma('dart2js:noInline')
  static ListMemoryCandidatesResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListMemoryCandidatesResponse>(create);
  static ListMemoryCandidatesResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<MemoryCandidate> get candidates => $_getList(0);

  @$pb.TagNumber(2)
  MemoryUnavailableReason get unavailableReason => $_getN(1);
  @$pb.TagNumber(2)
  set unavailableReason(MemoryUnavailableReason value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasUnavailableReason() => $_has(1);
  @$pb.TagNumber(2)
  void clearUnavailableReason() => $_clearField(2);
}

class GetMemoryCandidateRequest extends $pb.GeneratedMessage {
  factory GetMemoryCandidateRequest({
    $core.String? candidateId,
  }) {
    final result = create();
    if (candidateId != null) result.candidateId = candidateId;
    return result;
  }

  GetMemoryCandidateRequest._();

  factory GetMemoryCandidateRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetMemoryCandidateRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetMemoryCandidateRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'candidateId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetMemoryCandidateRequest clone() =>
      GetMemoryCandidateRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetMemoryCandidateRequest copyWith(
          void Function(GetMemoryCandidateRequest) updates) =>
      super.copyWith((message) => updates(message as GetMemoryCandidateRequest))
          as GetMemoryCandidateRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetMemoryCandidateRequest create() => GetMemoryCandidateRequest._();
  @$core.override
  GetMemoryCandidateRequest createEmptyInstance() => create();
  static $pb.PbList<GetMemoryCandidateRequest> createRepeated() =>
      $pb.PbList<GetMemoryCandidateRequest>();
  @$core.pragma('dart2js:noInline')
  static GetMemoryCandidateRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetMemoryCandidateRequest>(create);
  static GetMemoryCandidateRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get candidateId => $_getSZ(0);
  @$pb.TagNumber(1)
  set candidateId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasCandidateId() => $_has(0);
  @$pb.TagNumber(1)
  void clearCandidateId() => $_clearField(1);
}

class PromoteMemoryCandidateRequest extends $pb.GeneratedMessage {
  factory PromoteMemoryCandidateRequest({
    $core.String? candidateId,
    $core.String? expectedContentHash,
    $core.String? editedContent,
    $3.MemoryTier? targetTier,
  }) {
    final result = create();
    if (candidateId != null) result.candidateId = candidateId;
    if (expectedContentHash != null)
      result.expectedContentHash = expectedContentHash;
    if (editedContent != null) result.editedContent = editedContent;
    if (targetTier != null) result.targetTier = targetTier;
    return result;
  }

  PromoteMemoryCandidateRequest._();

  factory PromoteMemoryCandidateRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PromoteMemoryCandidateRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PromoteMemoryCandidateRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'candidateId')
    ..aOS(2, _omitFieldNames ? '' : 'expectedContentHash')
    ..aOS(3, _omitFieldNames ? '' : 'editedContent')
    ..e<$3.MemoryTier>(
        4, _omitFieldNames ? '' : 'targetTier', $pb.PbFieldType.OE,
        defaultOrMaker: $3.MemoryTier.MEMORY_TIER_UNSPECIFIED,
        valueOf: $3.MemoryTier.valueOf,
        enumValues: $3.MemoryTier.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PromoteMemoryCandidateRequest clone() =>
      PromoteMemoryCandidateRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PromoteMemoryCandidateRequest copyWith(
          void Function(PromoteMemoryCandidateRequest) updates) =>
      super.copyWith(
              (message) => updates(message as PromoteMemoryCandidateRequest))
          as PromoteMemoryCandidateRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PromoteMemoryCandidateRequest create() =>
      PromoteMemoryCandidateRequest._();
  @$core.override
  PromoteMemoryCandidateRequest createEmptyInstance() => create();
  static $pb.PbList<PromoteMemoryCandidateRequest> createRepeated() =>
      $pb.PbList<PromoteMemoryCandidateRequest>();
  @$core.pragma('dart2js:noInline')
  static PromoteMemoryCandidateRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PromoteMemoryCandidateRequest>(create);
  static PromoteMemoryCandidateRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get candidateId => $_getSZ(0);
  @$pb.TagNumber(1)
  set candidateId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasCandidateId() => $_has(0);
  @$pb.TagNumber(1)
  void clearCandidateId() => $_clearField(1);

  /// Compare-and-set against MemoryCandidate.content_hash: a decision composed
  /// against text that has since changed is refused, not applied to the new
  /// text the user never read.
  @$pb.TagNumber(2)
  $core.String get expectedContentHash => $_getSZ(1);
  @$pb.TagNumber(2)
  set expectedContentHash($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasExpectedContentHash() => $_has(1);
  @$pb.TagNumber(2)
  void clearExpectedContentHash() => $_clearField(2);

  /// Optional user edit accepted in place of the proposed content.
  @$pb.TagNumber(3)
  $core.String get editedContent => $_getSZ(2);
  @$pb.TagNumber(3)
  set editedContent($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasEditedContent() => $_has(2);
  @$pb.TagNumber(3)
  void clearEditedContent() => $_clearField(3);

  @$pb.TagNumber(4)
  $3.MemoryTier get targetTier => $_getN(3);
  @$pb.TagNumber(4)
  set targetTier($3.MemoryTier value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasTargetTier() => $_has(3);
  @$pb.TagNumber(4)
  void clearTargetTier() => $_clearField(4);
}

class PromoteMemoryCandidateResponse extends $pb.GeneratedMessage {
  factory PromoteMemoryCandidateResponse({
    MemoryCandidate? candidate,
    MemoryNote? note,
  }) {
    final result = create();
    if (candidate != null) result.candidate = candidate;
    if (note != null) result.note = note;
    return result;
  }

  PromoteMemoryCandidateResponse._();

  factory PromoteMemoryCandidateResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PromoteMemoryCandidateResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PromoteMemoryCandidateResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOM<MemoryCandidate>(1, _omitFieldNames ? '' : 'candidate',
        subBuilder: MemoryCandidate.create)
    ..aOM<MemoryNote>(2, _omitFieldNames ? '' : 'note',
        subBuilder: MemoryNote.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PromoteMemoryCandidateResponse clone() =>
      PromoteMemoryCandidateResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PromoteMemoryCandidateResponse copyWith(
          void Function(PromoteMemoryCandidateResponse) updates) =>
      super.copyWith(
              (message) => updates(message as PromoteMemoryCandidateResponse))
          as PromoteMemoryCandidateResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PromoteMemoryCandidateResponse create() =>
      PromoteMemoryCandidateResponse._();
  @$core.override
  PromoteMemoryCandidateResponse createEmptyInstance() => create();
  static $pb.PbList<PromoteMemoryCandidateResponse> createRepeated() =>
      $pb.PbList<PromoteMemoryCandidateResponse>();
  @$core.pragma('dart2js:noInline')
  static PromoteMemoryCandidateResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PromoteMemoryCandidateResponse>(create);
  static PromoteMemoryCandidateResponse? _defaultInstance;

  @$pb.TagNumber(1)
  MemoryCandidate get candidate => $_getN(0);
  @$pb.TagNumber(1)
  set candidate(MemoryCandidate value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasCandidate() => $_has(0);
  @$pb.TagNumber(1)
  void clearCandidate() => $_clearField(1);
  @$pb.TagNumber(1)
  MemoryCandidate ensureCandidate() => $_ensure(0);

  @$pb.TagNumber(2)
  MemoryNote get note => $_getN(1);
  @$pb.TagNumber(2)
  set note(MemoryNote value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasNote() => $_has(1);
  @$pb.TagNumber(2)
  void clearNote() => $_clearField(2);
  @$pb.TagNumber(2)
  MemoryNote ensureNote() => $_ensure(1);
}

class RejectMemoryCandidateRequest extends $pb.GeneratedMessage {
  factory RejectMemoryCandidateRequest({
    $core.String? candidateId,
    $core.String? expectedContentHash,
    $core.String? reason,
  }) {
    final result = create();
    if (candidateId != null) result.candidateId = candidateId;
    if (expectedContentHash != null)
      result.expectedContentHash = expectedContentHash;
    if (reason != null) result.reason = reason;
    return result;
  }

  RejectMemoryCandidateRequest._();

  factory RejectMemoryCandidateRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RejectMemoryCandidateRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RejectMemoryCandidateRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'candidateId')
    ..aOS(2, _omitFieldNames ? '' : 'expectedContentHash')
    ..aOS(3, _omitFieldNames ? '' : 'reason')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RejectMemoryCandidateRequest clone() =>
      RejectMemoryCandidateRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RejectMemoryCandidateRequest copyWith(
          void Function(RejectMemoryCandidateRequest) updates) =>
      super.copyWith(
              (message) => updates(message as RejectMemoryCandidateRequest))
          as RejectMemoryCandidateRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RejectMemoryCandidateRequest create() =>
      RejectMemoryCandidateRequest._();
  @$core.override
  RejectMemoryCandidateRequest createEmptyInstance() => create();
  static $pb.PbList<RejectMemoryCandidateRequest> createRepeated() =>
      $pb.PbList<RejectMemoryCandidateRequest>();
  @$core.pragma('dart2js:noInline')
  static RejectMemoryCandidateRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RejectMemoryCandidateRequest>(create);
  static RejectMemoryCandidateRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get candidateId => $_getSZ(0);
  @$pb.TagNumber(1)
  set candidateId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasCandidateId() => $_has(0);
  @$pb.TagNumber(1)
  void clearCandidateId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get expectedContentHash => $_getSZ(1);
  @$pb.TagNumber(2)
  set expectedContentHash($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasExpectedContentHash() => $_has(1);
  @$pb.TagNumber(2)
  void clearExpectedContentHash() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get reason => $_getSZ(2);
  @$pb.TagNumber(3)
  set reason($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasReason() => $_has(2);
  @$pb.TagNumber(3)
  void clearReason() => $_clearField(3);
}

class RejectMemoryCandidateResponse extends $pb.GeneratedMessage {
  factory RejectMemoryCandidateResponse({
    MemoryCandidate? candidate,
  }) {
    final result = create();
    if (candidate != null) result.candidate = candidate;
    return result;
  }

  RejectMemoryCandidateResponse._();

  factory RejectMemoryCandidateResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RejectMemoryCandidateResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RejectMemoryCandidateResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOM<MemoryCandidate>(1, _omitFieldNames ? '' : 'candidate',
        subBuilder: MemoryCandidate.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RejectMemoryCandidateResponse clone() =>
      RejectMemoryCandidateResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RejectMemoryCandidateResponse copyWith(
          void Function(RejectMemoryCandidateResponse) updates) =>
      super.copyWith(
              (message) => updates(message as RejectMemoryCandidateResponse))
          as RejectMemoryCandidateResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RejectMemoryCandidateResponse create() =>
      RejectMemoryCandidateResponse._();
  @$core.override
  RejectMemoryCandidateResponse createEmptyInstance() => create();
  static $pb.PbList<RejectMemoryCandidateResponse> createRepeated() =>
      $pb.PbList<RejectMemoryCandidateResponse>();
  @$core.pragma('dart2js:noInline')
  static RejectMemoryCandidateResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RejectMemoryCandidateResponse>(create);
  static RejectMemoryCandidateResponse? _defaultInstance;

  @$pb.TagNumber(1)
  MemoryCandidate get candidate => $_getN(0);
  @$pb.TagNumber(1)
  set candidate(MemoryCandidate value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasCandidate() => $_has(0);
  @$pb.TagNumber(1)
  void clearCandidate() => $_clearField(1);
  @$pb.TagNumber(1)
  MemoryCandidate ensureCandidate() => $_ensure(0);
}

class GetMemoryProfileRequest extends $pb.GeneratedMessage {
  factory GetMemoryProfileRequest() => create();

  GetMemoryProfileRequest._();

  factory GetMemoryProfileRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetMemoryProfileRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetMemoryProfileRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetMemoryProfileRequest clone() =>
      GetMemoryProfileRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetMemoryProfileRequest copyWith(
          void Function(GetMemoryProfileRequest) updates) =>
      super.copyWith((message) => updates(message as GetMemoryProfileRequest))
          as GetMemoryProfileRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetMemoryProfileRequest create() => GetMemoryProfileRequest._();
  @$core.override
  GetMemoryProfileRequest createEmptyInstance() => create();
  static $pb.PbList<GetMemoryProfileRequest> createRepeated() =>
      $pb.PbList<GetMemoryProfileRequest>();
  @$core.pragma('dart2js:noInline')
  static GetMemoryProfileRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetMemoryProfileRequest>(create);
  static GetMemoryProfileRequest? _defaultInstance;
}

class ApplyMemoryProfileRequest extends $pb.GeneratedMessage {
  factory ApplyMemoryProfileRequest({
    $core.String? content,
    $core.String? expectedContentHash,
    $core.String? candidateId,
  }) {
    final result = create();
    if (content != null) result.content = content;
    if (expectedContentHash != null)
      result.expectedContentHash = expectedContentHash;
    if (candidateId != null) result.candidateId = candidateId;
    return result;
  }

  ApplyMemoryProfileRequest._();

  factory ApplyMemoryProfileRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ApplyMemoryProfileRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ApplyMemoryProfileRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'content')
    ..aOS(2, _omitFieldNames ? '' : 'expectedContentHash')
    ..aOS(3, _omitFieldNames ? '' : 'candidateId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApplyMemoryProfileRequest clone() =>
      ApplyMemoryProfileRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApplyMemoryProfileRequest copyWith(
          void Function(ApplyMemoryProfileRequest) updates) =>
      super.copyWith((message) => updates(message as ApplyMemoryProfileRequest))
          as ApplyMemoryProfileRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ApplyMemoryProfileRequest create() => ApplyMemoryProfileRequest._();
  @$core.override
  ApplyMemoryProfileRequest createEmptyInstance() => create();
  static $pb.PbList<ApplyMemoryProfileRequest> createRepeated() =>
      $pb.PbList<ApplyMemoryProfileRequest>();
  @$core.pragma('dart2js:noInline')
  static ApplyMemoryProfileRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ApplyMemoryProfileRequest>(create);
  static ApplyMemoryProfileRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get content => $_getSZ(0);
  @$pb.TagNumber(1)
  set content($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasContent() => $_has(0);
  @$pb.TagNumber(1)
  void clearContent() => $_clearField(1);

  /// Compare-and-set against MemoryProfile.content_hash. Empty means the caller
  /// expects no profile to exist yet.
  @$pb.TagNumber(2)
  $core.String get expectedContentHash => $_getSZ(1);
  @$pb.TagNumber(2)
  set expectedContentHash($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasExpectedContentHash() => $_has(1);
  @$pb.TagNumber(2)
  void clearExpectedContentHash() => $_clearField(2);

  /// The pending profile_edit candidate this apply acts on. Turing writes
  /// profile.md only on the authority of a proposal the user is looking at, so
  /// there is no path here and no way to write the profile without one.
  @$pb.TagNumber(3)
  $core.String get candidateId => $_getSZ(2);
  @$pb.TagNumber(3)
  set candidateId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasCandidateId() => $_has(2);
  @$pb.TagNumber(3)
  void clearCandidateId() => $_clearField(3);
}

class ApplyMemoryProfileResponse extends $pb.GeneratedMessage {
  factory ApplyMemoryProfileResponse({
    MemoryProfile? profile,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    return result;
  }

  ApplyMemoryProfileResponse._();

  factory ApplyMemoryProfileResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ApplyMemoryProfileResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ApplyMemoryProfileResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOM<MemoryProfile>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: MemoryProfile.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApplyMemoryProfileResponse clone() =>
      ApplyMemoryProfileResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApplyMemoryProfileResponse copyWith(
          void Function(ApplyMemoryProfileResponse) updates) =>
      super.copyWith(
              (message) => updates(message as ApplyMemoryProfileResponse))
          as ApplyMemoryProfileResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ApplyMemoryProfileResponse create() => ApplyMemoryProfileResponse._();
  @$core.override
  ApplyMemoryProfileResponse createEmptyInstance() => create();
  static $pb.PbList<ApplyMemoryProfileResponse> createRepeated() =>
      $pb.PbList<ApplyMemoryProfileResponse>();
  @$core.pragma('dart2js:noInline')
  static ApplyMemoryProfileResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ApplyMemoryProfileResponse>(create);
  static ApplyMemoryProfileResponse? _defaultInstance;

  @$pb.TagNumber(1)
  MemoryProfile get profile => $_getN(0);
  @$pb.TagNumber(1)
  set profile(MemoryProfile value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProfile() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfile() => $_clearField(1);
  @$pb.TagNumber(1)
  MemoryProfile ensureProfile() => $_ensure(0);
}

class MemoryToolDescriptor extends $pb.GeneratedMessage {
  factory MemoryToolDescriptor({
    $core.String? toolName,
    $3.ToolPolicy? policy,
    $2.Struct? schema,
    $core.bool? enabled,
    $core.String? description,
  }) {
    final result = create();
    if (toolName != null) result.toolName = toolName;
    if (policy != null) result.policy = policy;
    if (schema != null) result.schema = schema;
    if (enabled != null) result.enabled = enabled;
    if (description != null) result.description = description;
    return result;
  }

  MemoryToolDescriptor._();

  factory MemoryToolDescriptor.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory MemoryToolDescriptor.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'MemoryToolDescriptor',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'toolName')
    ..e<$3.ToolPolicy>(2, _omitFieldNames ? '' : 'policy', $pb.PbFieldType.OE,
        defaultOrMaker: $3.ToolPolicy.TOOL_POLICY_UNSPECIFIED,
        valueOf: $3.ToolPolicy.valueOf,
        enumValues: $3.ToolPolicy.values)
    ..aOM<$2.Struct>(3, _omitFieldNames ? '' : 'schema',
        subBuilder: $2.Struct.create)
    ..aOB(4, _omitFieldNames ? '' : 'enabled')
    ..aOS(5, _omitFieldNames ? '' : 'description')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryToolDescriptor clone() =>
      MemoryToolDescriptor()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryToolDescriptor copyWith(void Function(MemoryToolDescriptor) updates) =>
      super.copyWith((message) => updates(message as MemoryToolDescriptor))
          as MemoryToolDescriptor;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static MemoryToolDescriptor create() => MemoryToolDescriptor._();
  @$core.override
  MemoryToolDescriptor createEmptyInstance() => create();
  static $pb.PbList<MemoryToolDescriptor> createRepeated() =>
      $pb.PbList<MemoryToolDescriptor>();
  @$core.pragma('dart2js:noInline')
  static MemoryToolDescriptor getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<MemoryToolDescriptor>(create);
  static MemoryToolDescriptor? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get toolName => $_getSZ(0);
  @$pb.TagNumber(1)
  set toolName($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasToolName() => $_has(0);
  @$pb.TagNumber(1)
  void clearToolName() => $_clearField(1);

  @$pb.TagNumber(2)
  $3.ToolPolicy get policy => $_getN(1);
  @$pb.TagNumber(2)
  set policy($3.ToolPolicy value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasPolicy() => $_has(1);
  @$pb.TagNumber(2)
  void clearPolicy() => $_clearField(2);

  @$pb.TagNumber(3)
  $2.Struct get schema => $_getN(2);
  @$pb.TagNumber(3)
  set schema($2.Struct value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasSchema() => $_has(2);
  @$pb.TagNumber(3)
  void clearSchema() => $_clearField(3);
  @$pb.TagNumber(3)
  $2.Struct ensureSchema() => $_ensure(2);

  @$pb.TagNumber(4)
  $core.bool get enabled => $_getBF(3);
  @$pb.TagNumber(4)
  set enabled($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasEnabled() => $_has(3);
  @$pb.TagNumber(4)
  void clearEnabled() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get description => $_getSZ(4);
  @$pb.TagNumber(5)
  set description($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasDescription() => $_has(4);
  @$pb.TagNumber(5)
  void clearDescription() => $_clearField(5);
}

class ListMemoryToolsRequest extends $pb.GeneratedMessage {
  factory ListMemoryToolsRequest() => create();

  ListMemoryToolsRequest._();

  factory ListMemoryToolsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListMemoryToolsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListMemoryToolsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryToolsRequest clone() =>
      ListMemoryToolsRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryToolsRequest copyWith(
          void Function(ListMemoryToolsRequest) updates) =>
      super.copyWith((message) => updates(message as ListMemoryToolsRequest))
          as ListMemoryToolsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListMemoryToolsRequest create() => ListMemoryToolsRequest._();
  @$core.override
  ListMemoryToolsRequest createEmptyInstance() => create();
  static $pb.PbList<ListMemoryToolsRequest> createRepeated() =>
      $pb.PbList<ListMemoryToolsRequest>();
  @$core.pragma('dart2js:noInline')
  static ListMemoryToolsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListMemoryToolsRequest>(create);
  static ListMemoryToolsRequest? _defaultInstance;
}

class ListMemoryToolsResponse extends $pb.GeneratedMessage {
  factory ListMemoryToolsResponse({
    $core.Iterable<MemoryToolDescriptor>? tools,
  }) {
    final result = create();
    if (tools != null) result.tools.addAll(tools);
    return result;
  }

  ListMemoryToolsResponse._();

  factory ListMemoryToolsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListMemoryToolsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListMemoryToolsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..pc<MemoryToolDescriptor>(
        1, _omitFieldNames ? '' : 'tools', $pb.PbFieldType.PM,
        subBuilder: MemoryToolDescriptor.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryToolsResponse clone() =>
      ListMemoryToolsResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryToolsResponse copyWith(
          void Function(ListMemoryToolsResponse) updates) =>
      super.copyWith((message) => updates(message as ListMemoryToolsResponse))
          as ListMemoryToolsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListMemoryToolsResponse create() => ListMemoryToolsResponse._();
  @$core.override
  ListMemoryToolsResponse createEmptyInstance() => create();
  static $pb.PbList<ListMemoryToolsResponse> createRepeated() =>
      $pb.PbList<ListMemoryToolsResponse>();
  @$core.pragma('dart2js:noInline')
  static ListMemoryToolsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListMemoryToolsResponse>(create);
  static ListMemoryToolsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<MemoryToolDescriptor> get tools => $_getList(0);
}

/// A memory tool call, dispatched by the runtime over the internal channel.
///
/// There is no session_id and no vault path. The run names itself, and the
/// server resolves the conversation the run belongs to from its own tables: a
/// caller that could name the session could file a memory against a
/// conversation it has nothing to do with.
class CallMemoryToolRequest extends $pb.GeneratedMessage {
  factory CallMemoryToolRequest({
    $core.String? runId,
    $core.String? approvalId,
    $core.String? toolName,
    $2.Struct? args,
  }) {
    final result = create();
    if (runId != null) result.runId = runId;
    if (approvalId != null) result.approvalId = approvalId;
    if (toolName != null) result.toolName = toolName;
    if (args != null) result.args = args;
    return result;
  }

  CallMemoryToolRequest._();

  factory CallMemoryToolRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory CallMemoryToolRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CallMemoryToolRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'runId')
    ..aOS(2, _omitFieldNames ? '' : 'approvalId')
    ..aOS(3, _omitFieldNames ? '' : 'toolName')
    ..aOM<$2.Struct>(4, _omitFieldNames ? '' : 'args',
        subBuilder: $2.Struct.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CallMemoryToolRequest clone() =>
      CallMemoryToolRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CallMemoryToolRequest copyWith(
          void Function(CallMemoryToolRequest) updates) =>
      super.copyWith((message) => updates(message as CallMemoryToolRequest))
          as CallMemoryToolRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static CallMemoryToolRequest create() => CallMemoryToolRequest._();
  @$core.override
  CallMemoryToolRequest createEmptyInstance() => create();
  static $pb.PbList<CallMemoryToolRequest> createRepeated() =>
      $pb.PbList<CallMemoryToolRequest>();
  @$core.pragma('dart2js:noInline')
  static CallMemoryToolRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CallMemoryToolRequest>(create);
  static CallMemoryToolRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get runId => $_getSZ(0);
  @$pb.TagNumber(1)
  set runId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRunId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRunId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get approvalId => $_getSZ(1);
  @$pb.TagNumber(2)
  set approvalId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasApprovalId() => $_has(1);
  @$pb.TagNumber(2)
  void clearApprovalId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get toolName => $_getSZ(2);
  @$pb.TagNumber(3)
  set toolName($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasToolName() => $_has(2);
  @$pb.TagNumber(3)
  void clearToolName() => $_clearField(3);

  @$pb.TagNumber(4)
  $2.Struct get args => $_getN(3);
  @$pb.TagNumber(4)
  set args($2.Struct value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasArgs() => $_has(3);
  @$pb.TagNumber(4)
  void clearArgs() => $_clearField(4);
  @$pb.TagNumber(4)
  $2.Struct ensureArgs() => $_ensure(3);
}

class CallMemoryToolResponse extends $pb.GeneratedMessage {
  factory CallMemoryToolResponse({
    $2.Struct? result,
  }) {
    final result$ = create();
    if (result != null) result$.result = result;
    return result$;
  }

  CallMemoryToolResponse._();

  factory CallMemoryToolResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory CallMemoryToolResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CallMemoryToolResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOM<$2.Struct>(1, _omitFieldNames ? '' : 'result',
        subBuilder: $2.Struct.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CallMemoryToolResponse clone() =>
      CallMemoryToolResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CallMemoryToolResponse copyWith(
          void Function(CallMemoryToolResponse) updates) =>
      super.copyWith((message) => updates(message as CallMemoryToolResponse))
          as CallMemoryToolResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static CallMemoryToolResponse create() => CallMemoryToolResponse._();
  @$core.override
  CallMemoryToolResponse createEmptyInstance() => create();
  static $pb.PbList<CallMemoryToolResponse> createRepeated() =>
      $pb.PbList<CallMemoryToolResponse>();
  @$core.pragma('dart2js:noInline')
  static CallMemoryToolResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CallMemoryToolResponse>(create);
  static CallMemoryToolResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $2.Struct get result => $_getN(0);
  @$pb.TagNumber(1)
  set result($2.Struct value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasResult() => $_has(0);
  @$pb.TagNumber(1)
  void clearResult() => $_clearField(1);
  @$pb.TagNumber(1)
  $2.Struct ensureResult() => $_ensure(0);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
