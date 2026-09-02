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
    ..aE<MemoryProvenanceKind>(1, _omitFieldNames ? '' : 'kind',
        enumValues: MemoryProvenanceKind.values)
    ..aOS(2, _omitFieldNames ? '' : 'sourceSessionId')
    ..aOS(3, _omitFieldNames ? '' : 'sourceSessionTitle')
    ..aOM<$1.Timestamp>(4, _omitFieldNames ? '' : 'observedAt',
        subBuilder: $1.Timestamp.create)
    ..aOB(5, _omitFieldNames ? '' : 'withdrawn')
    ..aOM<$1.Timestamp>(6, _omitFieldNames ? '' : 'withdrawnAt',
        subBuilder: $1.Timestamp.create)
    ..aI(7, _omitFieldNames ? '' : 'evidenceCount')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryProvenance clone() => deepCopy();
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
    $core.bool? untracked,
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
    if (untracked != null) result.untracked = untracked;
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
    ..aE<MemoryCandidateKind>(2, _omitFieldNames ? '' : 'kind',
        enumValues: MemoryCandidateKind.values)
    ..aOS(3, _omitFieldNames ? '' : 'inboxPath')
    ..aOS(4, _omitFieldNames ? '' : 'content')
    ..aOS(5, _omitFieldNames ? '' : 'contentHash')
    ..aE<MemoryCandidateState>(6, _omitFieldNames ? '' : 'state',
        enumValues: MemoryCandidateState.values)
    ..pPM<MemoryProvenance>(7, _omitFieldNames ? '' : 'provenance',
        subBuilder: MemoryProvenance.create)
    ..aOS(8, _omitFieldNames ? '' : 'promotedNoteId')
    ..aOM<$1.Timestamp>(9, _omitFieldNames ? '' : 'createdAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(10, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(11, _omitFieldNames ? '' : 'decidedAt',
        subBuilder: $1.Timestamp.create)
    ..aOS(12, _omitFieldNames ? '' : 'parseError')
    ..aE<MemoryUnavailableReason>(
        13, _omitFieldNames ? '' : 'unavailableReason',
        enumValues: MemoryUnavailableReason.values)
    ..aOB(14, _omitFieldNames ? '' : 'managed')
    ..aOB(15, _omitFieldNames ? '' : 'untracked')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryCandidate clone() => deepCopy();
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

  /// True for an inbox file Turing wrote and then lost the record of: a
  /// creation that crashed between the write and its transaction. It is
  /// unmanaged for exactly the same reason a hand-dropped draft is — there is
  /// no row, so no decision RPC applies — but it is not the user's own draft,
  /// and telling them it is would be a lie about who wrote a claim about them.
  /// It is listed rather than deleted, because a file they may already have
  /// read is not something to remove on a guess.
  @$pb.TagNumber(15)
  $core.bool get untracked => $_getBF(14);
  @$pb.TagNumber(15)
  set untracked($core.bool value) => $_setBool(14, value);
  @$pb.TagNumber(15)
  $core.bool hasUntracked() => $_has(14);
  @$pb.TagNumber(15)
  void clearUntracked() => $_clearField(15);
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
    ..aE<MemoryNoteStatus>(6, _omitFieldNames ? '' : 'status',
        enumValues: MemoryNoteStatus.values)
    ..aE<$3.MemoryTier>(7, _omitFieldNames ? '' : 'tier',
        enumValues: $3.MemoryTier.values)
    ..pPM<MemoryProvenance>(8, _omitFieldNames ? '' : 'provenance',
        subBuilder: MemoryProvenance.create)
    ..aOM<$1.Timestamp>(9, _omitFieldNames ? '' : 'createdAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(10, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $1.Timestamp.create)
    ..aOS(11, _omitFieldNames ? '' : 'parseError')
    ..aE<MemoryUnavailableReason>(
        12, _omitFieldNames ? '' : 'unavailableReason',
        enumValues: MemoryUnavailableReason.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryNote clone() => deepCopy();
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
    $core.bool? pinnedTruncated,
    $core.int? pinnedBytes,
  }) {
    final result = create();
    if (content != null) result.content = content;
    if (contentHash != null) result.contentHash = contentHash;
    if (status != null) result.status = status;
    if (updatedAt != null) result.updatedAt = updatedAt;
    if (parseError != null) result.parseError = parseError;
    if (unavailableReason != null) result.unavailableReason = unavailableReason;
    if (pinnedTruncated != null) result.pinnedTruncated = pinnedTruncated;
    if (pinnedBytes != null) result.pinnedBytes = pinnedBytes;
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
    ..aE<MemoryNoteStatus>(3, _omitFieldNames ? '' : 'status',
        enumValues: MemoryNoteStatus.values)
    ..aOM<$1.Timestamp>(4, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $1.Timestamp.create)
    ..aOS(5, _omitFieldNames ? '' : 'parseError')
    ..aE<MemoryUnavailableReason>(6, _omitFieldNames ? '' : 'unavailableReason',
        enumValues: MemoryUnavailableReason.values)
    ..aOB(7, _omitFieldNames ? '' : 'pinnedTruncated')
    ..aI(8, _omitFieldNames ? '' : 'pinnedBytes')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryProfile clone() => deepCopy();
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

  /// The document as it stands on disk, whole. This is an editor's view, not a
  /// run's: the runtime's pin is bounded and carries a notice saying so, and
  /// handing that here would put words in the editor the user never typed and
  /// save them back into their own file.
  @$pb.TagNumber(1)
  $core.String get content => $_getSZ(0);
  @$pb.TagNumber(1)
  set content($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasContent() => $_has(0);
  @$pb.TagNumber(1)
  void clearContent() => $_clearField(1);

  /// A hash of exactly the bytes in `content`. Doubles as the compare-and-set
  /// token for ApplyMemoryProfile and for the user's own SaveMemoryProfile,
  /// which are verified against the file — so this can never be the pin's
  /// post-truncation hash, or a long document could be read and never saved.
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

  /// True when this document is longer than the runtime's pin budget, so a run
  /// carries a fragment of what is above. Stated rather than left for a client
  /// to infer from a byte count it would have to know the budget to interpret.
  @$pb.TagNumber(7)
  $core.bool get pinnedTruncated => $_getBF(6);
  @$pb.TagNumber(7)
  set pinnedTruncated($core.bool value) => $_setBool(6, value);
  @$pb.TagNumber(7)
  $core.bool hasPinnedTruncated() => $_has(6);
  @$pb.TagNumber(7)
  void clearPinnedTruncated() => $_clearField(7);

  /// How many bytes of this document reach a prompt: the rune-safe cut at or
  /// below the budget when it is truncated, its whole length when it is not,
  /// and zero when nothing survives trimming. It counts the document's own
  /// bytes, never the truncation notice the runtime appends to the pin.
  @$pb.TagNumber(8)
  $core.int get pinnedBytes => $_getIZ(7);
  @$pb.TagNumber(8)
  set pinnedBytes($core.int value) => $_setSignedInt32(7, value);
  @$pb.TagNumber(8)
  $core.bool hasPinnedBytes() => $_has(7);
  @$pb.TagNumber(8)
  void clearPinnedBytes() => $_clearField(8);
}

/// The single persona document: who Turing is.
///
/// It is not a second profile. The profile is a description of the user that
/// the agent may propose edits to; the persona is the user's instruction about
/// Turing, pinned unframed into every run, and no agent-facing path in the
/// system writes it. Its own message keeps that asymmetry visible on the wire
/// instead of leaving it to a comment on a shared one.
class MemoryPersona extends $pb.GeneratedMessage {
  factory MemoryPersona({
    $core.String? content,
    $core.String? contentHash,
    MemoryNoteStatus? status,
    $1.Timestamp? updatedAt,
    $core.String? parseError,
    MemoryUnavailableReason? unavailableReason,
    $core.bool? pinnedTruncated,
    $core.int? pinnedBytes,
  }) {
    final result = create();
    if (content != null) result.content = content;
    if (contentHash != null) result.contentHash = contentHash;
    if (status != null) result.status = status;
    if (updatedAt != null) result.updatedAt = updatedAt;
    if (parseError != null) result.parseError = parseError;
    if (unavailableReason != null) result.unavailableReason = unavailableReason;
    if (pinnedTruncated != null) result.pinnedTruncated = pinnedTruncated;
    if (pinnedBytes != null) result.pinnedBytes = pinnedBytes;
    return result;
  }

  MemoryPersona._();

  factory MemoryPersona.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory MemoryPersona.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'MemoryPersona',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'content')
    ..aOS(2, _omitFieldNames ? '' : 'contentHash')
    ..aE<MemoryNoteStatus>(3, _omitFieldNames ? '' : 'status',
        enumValues: MemoryNoteStatus.values)
    ..aOM<$1.Timestamp>(4, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $1.Timestamp.create)
    ..aOS(5, _omitFieldNames ? '' : 'parseError')
    ..aE<MemoryUnavailableReason>(6, _omitFieldNames ? '' : 'unavailableReason',
        enumValues: MemoryUnavailableReason.values)
    ..aOB(7, _omitFieldNames ? '' : 'pinnedTruncated')
    ..aI(8, _omitFieldNames ? '' : 'pinnedBytes')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryPersona clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryPersona copyWith(void Function(MemoryPersona) updates) =>
      super.copyWith((message) => updates(message as MemoryPersona))
          as MemoryPersona;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static MemoryPersona create() => MemoryPersona._();
  @$core.override
  MemoryPersona createEmptyInstance() => create();
  static $pb.PbList<MemoryPersona> createRepeated() =>
      $pb.PbList<MemoryPersona>();
  @$core.pragma('dart2js:noInline')
  static MemoryPersona getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<MemoryPersona>(create);
  static MemoryPersona? _defaultInstance;

  /// The document as it stands on disk, whole. See MemoryProfile.content.
  @$pb.TagNumber(1)
  $core.String get content => $_getSZ(0);
  @$pb.TagNumber(1)
  set content($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasContent() => $_has(0);
  @$pb.TagNumber(1)
  void clearContent() => $_clearField(1);

  /// A hash of exactly the bytes in `content`, and the compare-and-set token
  /// for SaveMemoryPersona.
  @$pb.TagNumber(2)
  $core.String get contentHash => $_getSZ(1);
  @$pb.TagNumber(2)
  set contentHash($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasContentHash() => $_has(1);
  @$pb.TagNumber(2)
  void clearContentHash() => $_clearField(2);

  /// Always UNMANAGED: Turing reads this document and never rewrites it.
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

  /// True when a run carries only part of this document.
  @$pb.TagNumber(7)
  $core.bool get pinnedTruncated => $_getBF(6);
  @$pb.TagNumber(7)
  set pinnedTruncated($core.bool value) => $_setBool(6, value);
  @$pb.TagNumber(7)
  $core.bool hasPinnedTruncated() => $_has(6);
  @$pb.TagNumber(7)
  void clearPinnedTruncated() => $_clearField(7);

  /// How many bytes of this document reach a prompt.
  @$pb.TagNumber(8)
  $core.int get pinnedBytes => $_getIZ(7);
  @$pb.TagNumber(8)
  set pinnedBytes($core.int value) => $_setSignedInt32(7, value);
  @$pb.TagNumber(8)
  $core.bool hasPinnedBytes() => $_has(7);
  @$pb.TagNumber(8)
  void clearPinnedBytes() => $_clearField(8);
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
    ..aE<$3.MemoryTier>(1, _omitFieldNames ? '' : 'tier',
        enumValues: $3.MemoryTier.values)
    ..aOB(2, _omitFieldNames ? '' : 'enabled')
    ..aI(3, _omitFieldNames ? '' : 'noteCount')
    ..aI(4, _omitFieldNames ? '' : 'pendingCandidateCount')
    ..aOM<$1.Timestamp>(5, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $1.Timestamp.create)
    ..aE<MemoryUnavailableReason>(6, _omitFieldNames ? '' : 'unavailableReason',
        enumValues: MemoryUnavailableReason.values)
    ..aOS(7, _omitFieldNames ? '' : 'parseError')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryTierState clone() => deepCopy();
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
    ..aE<MemoryUnavailableReason>(4, _omitFieldNames ? '' : 'unavailableReason',
        enumValues: MemoryUnavailableReason.values)
    ..aOS(5, _omitFieldNames ? '' : 'parseError')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemorySettings clone() => deepCopy();
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
  ListMemoryStateRequest clone() => deepCopy();
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
    MemoryPersona? persona,
  }) {
    final result = create();
    if (settings != null) result.settings = settings;
    if (tiers != null) result.tiers.addAll(tiers);
    if (notes != null) result.notes.addAll(notes);
    if (candidates != null) result.candidates.addAll(candidates);
    if (profile != null) result.profile = profile;
    if (persona != null) result.persona = persona;
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
    ..pPM<MemoryTierState>(2, _omitFieldNames ? '' : 'tiers',
        subBuilder: MemoryTierState.create)
    ..pPM<MemoryNote>(3, _omitFieldNames ? '' : 'notes',
        subBuilder: MemoryNote.create)
    ..pPM<MemoryCandidate>(4, _omitFieldNames ? '' : 'candidates',
        subBuilder: MemoryCandidate.create)
    ..aOM<MemoryProfile>(5, _omitFieldNames ? '' : 'profile',
        subBuilder: MemoryProfile.create)
    ..aOM<MemoryPersona>(6, _omitFieldNames ? '' : 'persona',
        subBuilder: MemoryPersona.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryStateResponse clone() => deepCopy();
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

  @$pb.TagNumber(6)
  MemoryPersona get persona => $_getN(5);
  @$pb.TagNumber(6)
  set persona(MemoryPersona value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasPersona() => $_has(5);
  @$pb.TagNumber(6)
  void clearPersona() => $_clearField(6);
  @$pb.TagNumber(6)
  MemoryPersona ensurePersona() => $_ensure(5);
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
  GetMemorySettingsRequest clone() => deepCopy();
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
    ..aE<$3.MemoryTier>(2, _omitFieldNames ? '' : 'tier',
        enumValues: $3.MemoryTier.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetMemoryEnabledRequest clone() => deepCopy();
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
    ..aE<MemoryCandidateState>(1, _omitFieldNames ? '' : 'state',
        enumValues: MemoryCandidateState.values)
    ..aE<MemoryCandidateKind>(2, _omitFieldNames ? '' : 'kind',
        enumValues: MemoryCandidateKind.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryCandidatesRequest clone() => deepCopy();
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
    ..pPM<MemoryCandidate>(1, _omitFieldNames ? '' : 'candidates',
        subBuilder: MemoryCandidate.create)
    ..aE<MemoryUnavailableReason>(2, _omitFieldNames ? '' : 'unavailableReason',
        enumValues: MemoryUnavailableReason.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryCandidatesResponse clone() => deepCopy();
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
  GetMemoryCandidateRequest clone() => deepCopy();
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
    @$core.Deprecated('This field is deprecated.')
    $core.String? expectedContentHash,
    $core.String? editedContent,
    $3.MemoryTier? targetTier,
    $core.String? expectedCandidateHash,
  }) {
    final result = create();
    if (candidateId != null) result.candidateId = candidateId;
    if (expectedContentHash != null)
      result.expectedContentHash = expectedContentHash;
    if (editedContent != null) result.editedContent = editedContent;
    if (targetTier != null) result.targetTier = targetTier;
    if (expectedCandidateHash != null)
      result.expectedCandidateHash = expectedCandidateHash;
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
    ..aE<$3.MemoryTier>(4, _omitFieldNames ? '' : 'targetTier',
        enumValues: $3.MemoryTier.values)
    ..aOS(5, _omitFieldNames ? '' : 'expectedCandidateHash')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PromoteMemoryCandidateRequest clone() => deepCopy();
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

  /// Deprecated: the older spelling of expected_candidate_hash, and checked
  /// against exactly the same thing. It once named the database row's copy of
  /// the proposal, which made every proposal the user edited in their vault
  /// permanently undecidable — the listing can only ever serve the file's hash,
  /// and the row's was the only one that would be accepted. It is still honoured
  /// when expected_candidate_hash is empty, so a client built before the split
  /// is not left with no compare-and-set at all. New clients send
  /// expected_candidate_hash.
  @$core.Deprecated('This field is deprecated.')
  @$pb.TagNumber(2)
  $core.String get expectedContentHash => $_getSZ(1);
  @$core.Deprecated('This field is deprecated.')
  @$pb.TagNumber(2)
  set expectedContentHash($core.String value) => $_setString(1, value);
  @$core.Deprecated('This field is deprecated.')
  @$pb.TagNumber(2)
  $core.bool hasExpectedContentHash() => $_has(1);
  @$core.Deprecated('This field is deprecated.')
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

  /// Compare-and-set against the candidate file's own bytes as they read now,
  /// re-read at decision time inside the same serialisation as the mutation.
  ///
  /// This is the only candidate compare-and-set there is. It names the file
  /// rather than the row because the file is what every listing serves and what
  /// the user was shown — the vault is a vault so they can open a proposal and
  /// rewrite it — so a proposal edited between the listing and the decision is
  /// refused instead of promoted as text they never read. Empty means the caller
  /// is making no claim about the file.
  ///
  /// Which decision applies is read from the same bytes: a proposal the file now
  /// declares a profile_edit is not promoted into beliefs/, whatever the row
  /// remembers Turing having proposed.
  @$pb.TagNumber(5)
  $core.String get expectedCandidateHash => $_getSZ(4);
  @$pb.TagNumber(5)
  set expectedCandidateHash($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasExpectedCandidateHash() => $_has(4);
  @$pb.TagNumber(5)
  void clearExpectedCandidateHash() => $_clearField(5);
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
  PromoteMemoryCandidateResponse clone() => deepCopy();
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
    @$core.Deprecated('This field is deprecated.')
    $core.String? expectedContentHash,
    $core.String? reason,
    $core.String? expectedCandidateHash,
  }) {
    final result = create();
    if (candidateId != null) result.candidateId = candidateId;
    if (expectedContentHash != null)
      result.expectedContentHash = expectedContentHash;
    if (reason != null) result.reason = reason;
    if (expectedCandidateHash != null)
      result.expectedCandidateHash = expectedCandidateHash;
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
    ..aOS(4, _omitFieldNames ? '' : 'expectedCandidateHash')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RejectMemoryCandidateRequest clone() => deepCopy();
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

  /// Deprecated: the older spelling of expected_candidate_hash, honoured only
  /// when that field is empty. See
  /// PromoteMemoryCandidateRequest.expected_content_hash.
  @$core.Deprecated('This field is deprecated.')
  @$pb.TagNumber(2)
  $core.String get expectedContentHash => $_getSZ(1);
  @$core.Deprecated('This field is deprecated.')
  @$pb.TagNumber(2)
  set expectedContentHash($core.String value) => $_setString(1, value);
  @$core.Deprecated('This field is deprecated.')
  @$pb.TagNumber(2)
  $core.bool hasExpectedContentHash() => $_has(1);
  @$core.Deprecated('This field is deprecated.')
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

  /// Compare-and-set against the candidate file's own bytes. See
  /// PromoteMemoryCandidateRequest.expected_candidate_hash: a rejection is a
  /// decision about a claim, and a claim the user did not read is not one they
  /// refused. A rejection asks nothing about the kind — it is the user saying no
  /// to whatever is there — so a proposal whose frontmatter no longer parses can
  /// still be thrown away.
  ///
  /// Optional, and deliberately so. A client that could not read the proposal
  /// has no hash to make a claim with, and sending one it invented — or the
  /// hash of the bytes it failed to parse — would be answering a question it
  /// cannot answer. Empty is that client saying it is making no claim about the
  /// file, which is the one case where a rejection may proceed over a file the
  /// server cannot read either. A non-empty hash against an unreadable file is
  /// still refused: the claim cannot be checked, so it cannot be honoured.
  @$pb.TagNumber(4)
  $core.String get expectedCandidateHash => $_getSZ(3);
  @$pb.TagNumber(4)
  set expectedCandidateHash($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasExpectedCandidateHash() => $_has(3);
  @$pb.TagNumber(4)
  void clearExpectedCandidateHash() => $_clearField(4);
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
  RejectMemoryCandidateResponse clone() => deepCopy();
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
  GetMemoryProfileRequest clone() => deepCopy();
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
    $core.String? expectedCandidateHash,
  }) {
    final result = create();
    if (content != null) result.content = content;
    if (expectedContentHash != null)
      result.expectedContentHash = expectedContentHash;
    if (candidateId != null) result.candidateId = candidateId;
    if (expectedCandidateHash != null)
      result.expectedCandidateHash = expectedCandidateHash;
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
    ..aOS(4, _omitFieldNames ? '' : 'expectedCandidateHash')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApplyMemoryProfileRequest clone() => deepCopy();
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

  /// The whole resulting profile document, as the user reviewed it — never the
  /// proposal on its own. A client that sent the candidate's fragment here
  /// would be asking the server to replace the user's profile with a paragraph.
  @$pb.TagNumber(1)
  $core.String get content => $_getSZ(0);
  @$pb.TagNumber(1)
  set content($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasContent() => $_has(0);
  @$pb.TagNumber(1)
  void clearContent() => $_clearField(1);

  /// Compare-and-set against MemoryProfile.content_hash — the profile document
  /// this apply is replacing, and nothing else. This is the one request where
  /// expected_content_hash still names a document rather than a candidate.
  ///
  /// It must be the hash the resulting document was *composed against*, which is
  /// not always the most recent one read: a client holding a result the user has
  /// edited, over a profile that has since moved, has to send the older token and
  /// be refused, rather than pair those words with a document they never saw.
  /// Empty means the caller expects no profile to exist yet.
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
  /// there is no path here and no way to write the profile without one. Whether
  /// it is a profile_edit is read from the candidate file, not from the row.
  @$pb.TagNumber(3)
  $core.String get candidateId => $_getSZ(2);
  @$pb.TagNumber(3)
  set candidateId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasCandidateId() => $_has(2);
  @$pb.TagNumber(3)
  void clearCandidateId() => $_clearField(3);

  /// Compare-and-set against the candidate file's own bytes. See
  /// PromoteMemoryCandidateRequest.expected_candidate_hash: this binds the
  /// resulting document to the exact proposal it was composed from, so a
  /// proposal edited in the vault after the user read it cannot be applied as
  /// though they had accepted the new words.
  @$pb.TagNumber(4)
  $core.String get expectedCandidateHash => $_getSZ(3);
  @$pb.TagNumber(4)
  set expectedCandidateHash($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasExpectedCandidateHash() => $_has(3);
  @$pb.TagNumber(4)
  void clearExpectedCandidateHash() => $_clearField(4);
}

class ApplyMemoryProfileResponse extends $pb.GeneratedMessage {
  factory ApplyMemoryProfileResponse({
    MemoryProfile? profile,
    $core.bool? cleanupPending,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    if (cleanupPending != null) result.cleanupPending = cleanupPending;
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
    ..aOB(2, _omitFieldNames ? '' : 'cleanupPending')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApplyMemoryProfileResponse clone() => deepCopy();
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

  /// True when profile.md now holds `content` and the proposal it came from is
  /// still sitting in the inbox because it could not be removed. The write is
  /// the part that matters and it landed, so this is not a failure — but the
  /// user would otherwise be shown a proposal they have already accepted, with
  /// no explanation, and no decision RPC will take it. Turing's own cleanup
  /// finishes it; saying so is what keeps the page honest in the meantime.
  @$pb.TagNumber(2)
  $core.bool get cleanupPending => $_getBF(1);
  @$pb.TagNumber(2)
  set cleanupPending($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCleanupPending() => $_has(1);
  @$pb.TagNumber(2)
  void clearCleanupPending() => $_clearField(2);
}

class GetMemoryPersonaRequest extends $pb.GeneratedMessage {
  factory GetMemoryPersonaRequest() => create();

  GetMemoryPersonaRequest._();

  factory GetMemoryPersonaRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetMemoryPersonaRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetMemoryPersonaRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetMemoryPersonaRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetMemoryPersonaRequest copyWith(
          void Function(GetMemoryPersonaRequest) updates) =>
      super.copyWith((message) => updates(message as GetMemoryPersonaRequest))
          as GetMemoryPersonaRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetMemoryPersonaRequest create() => GetMemoryPersonaRequest._();
  @$core.override
  GetMemoryPersonaRequest createEmptyInstance() => create();
  static $pb.PbList<GetMemoryPersonaRequest> createRepeated() =>
      $pb.PbList<GetMemoryPersonaRequest>();
  @$core.pragma('dart2js:noInline')
  static GetMemoryPersonaRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetMemoryPersonaRequest>(create);
  static GetMemoryPersonaRequest? _defaultInstance;
}

/// The user saving persona.md by hand. There is no candidate here and no path:
/// this is the one document the user alone authors, and the only file this
/// request can write.
class SaveMemoryPersonaRequest extends $pb.GeneratedMessage {
  factory SaveMemoryPersonaRequest({
    $core.String? content,
    $core.String? expectedContentHash,
  }) {
    final result = create();
    if (content != null) result.content = content;
    if (expectedContentHash != null)
      result.expectedContentHash = expectedContentHash;
    return result;
  }

  SaveMemoryPersonaRequest._();

  factory SaveMemoryPersonaRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SaveMemoryPersonaRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SaveMemoryPersonaRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'content')
    ..aOS(2, _omitFieldNames ? '' : 'expectedContentHash')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SaveMemoryPersonaRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SaveMemoryPersonaRequest copyWith(
          void Function(SaveMemoryPersonaRequest) updates) =>
      super.copyWith((message) => updates(message as SaveMemoryPersonaRequest))
          as SaveMemoryPersonaRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SaveMemoryPersonaRequest create() => SaveMemoryPersonaRequest._();
  @$core.override
  SaveMemoryPersonaRequest createEmptyInstance() => create();
  static $pb.PbList<SaveMemoryPersonaRequest> createRepeated() =>
      $pb.PbList<SaveMemoryPersonaRequest>();
  @$core.pragma('dart2js:noInline')
  static SaveMemoryPersonaRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SaveMemoryPersonaRequest>(create);
  static SaveMemoryPersonaRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get content => $_getSZ(0);
  @$pb.TagNumber(1)
  set content($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasContent() => $_has(0);
  @$pb.TagNumber(1)
  void clearContent() => $_clearField(1);

  /// Compare-and-set against MemoryPersona.content_hash. Empty means the caller
  /// expects no persona to exist yet. A save composed against text the vault has
  /// since moved on from is refused rather than applied over the newer words —
  /// the same posture as ApplyMemoryProfile, for the same reason: the file is
  /// open in the user's own editor.
  @$pb.TagNumber(2)
  $core.String get expectedContentHash => $_getSZ(1);
  @$pb.TagNumber(2)
  set expectedContentHash($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasExpectedContentHash() => $_has(1);
  @$pb.TagNumber(2)
  void clearExpectedContentHash() => $_clearField(2);
}

class SaveMemoryPersonaResponse extends $pb.GeneratedMessage {
  factory SaveMemoryPersonaResponse({
    MemoryPersona? persona,
  }) {
    final result = create();
    if (persona != null) result.persona = persona;
    return result;
  }

  SaveMemoryPersonaResponse._();

  factory SaveMemoryPersonaResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SaveMemoryPersonaResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SaveMemoryPersonaResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOM<MemoryPersona>(1, _omitFieldNames ? '' : 'persona',
        subBuilder: MemoryPersona.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SaveMemoryPersonaResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SaveMemoryPersonaResponse copyWith(
          void Function(SaveMemoryPersonaResponse) updates) =>
      super.copyWith((message) => updates(message as SaveMemoryPersonaResponse))
          as SaveMemoryPersonaResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SaveMemoryPersonaResponse create() => SaveMemoryPersonaResponse._();
  @$core.override
  SaveMemoryPersonaResponse createEmptyInstance() => create();
  static $pb.PbList<SaveMemoryPersonaResponse> createRepeated() =>
      $pb.PbList<SaveMemoryPersonaResponse>();
  @$core.pragma('dart2js:noInline')
  static SaveMemoryPersonaResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SaveMemoryPersonaResponse>(create);
  static SaveMemoryPersonaResponse? _defaultInstance;

  @$pb.TagNumber(1)
  MemoryPersona get persona => $_getN(0);
  @$pb.TagNumber(1)
  set persona(MemoryPersona value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasPersona() => $_has(0);
  @$pb.TagNumber(1)
  void clearPersona() => $_clearField(1);
  @$pb.TagNumber(1)
  MemoryPersona ensurePersona() => $_ensure(0);
}

/// The user saving profile.md by hand, which is a different authority from
/// ApplyMemoryProfile: that one applies a proposal Turing wrote and the user
/// accepted, this one is the user writing about themselves directly. Keeping
/// them separate is what lets the proposal path keep requiring a candidate.
class SaveMemoryProfileRequest extends $pb.GeneratedMessage {
  factory SaveMemoryProfileRequest({
    $core.String? content,
    $core.String? expectedContentHash,
  }) {
    final result = create();
    if (content != null) result.content = content;
    if (expectedContentHash != null)
      result.expectedContentHash = expectedContentHash;
    return result;
  }

  SaveMemoryProfileRequest._();

  factory SaveMemoryProfileRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SaveMemoryProfileRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SaveMemoryProfileRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'content')
    ..aOS(2, _omitFieldNames ? '' : 'expectedContentHash')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SaveMemoryProfileRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SaveMemoryProfileRequest copyWith(
          void Function(SaveMemoryProfileRequest) updates) =>
      super.copyWith((message) => updates(message as SaveMemoryProfileRequest))
          as SaveMemoryProfileRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SaveMemoryProfileRequest create() => SaveMemoryProfileRequest._();
  @$core.override
  SaveMemoryProfileRequest createEmptyInstance() => create();
  static $pb.PbList<SaveMemoryProfileRequest> createRepeated() =>
      $pb.PbList<SaveMemoryProfileRequest>();
  @$core.pragma('dart2js:noInline')
  static SaveMemoryProfileRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SaveMemoryProfileRequest>(create);
  static SaveMemoryProfileRequest? _defaultInstance;

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
}

class SaveMemoryProfileResponse extends $pb.GeneratedMessage {
  factory SaveMemoryProfileResponse({
    MemoryProfile? profile,
  }) {
    final result = create();
    if (profile != null) result.profile = profile;
    return result;
  }

  SaveMemoryProfileResponse._();

  factory SaveMemoryProfileResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SaveMemoryProfileResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SaveMemoryProfileResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOM<MemoryProfile>(1, _omitFieldNames ? '' : 'profile',
        subBuilder: MemoryProfile.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SaveMemoryProfileResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SaveMemoryProfileResponse copyWith(
          void Function(SaveMemoryProfileResponse) updates) =>
      super.copyWith((message) => updates(message as SaveMemoryProfileResponse))
          as SaveMemoryProfileResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SaveMemoryProfileResponse create() => SaveMemoryProfileResponse._();
  @$core.override
  SaveMemoryProfileResponse createEmptyInstance() => create();
  static $pb.PbList<SaveMemoryProfileResponse> createRepeated() =>
      $pb.PbList<SaveMemoryProfileResponse>();
  @$core.pragma('dart2js:noInline')
  static SaveMemoryProfileResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SaveMemoryProfileResponse>(create);
  static SaveMemoryProfileResponse? _defaultInstance;

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
    ..aE<$3.ToolPolicy>(2, _omitFieldNames ? '' : 'policy',
        enumValues: $3.ToolPolicy.values)
    ..aOM<$2.Struct>(3, _omitFieldNames ? '' : 'schema',
        subBuilder: $2.Struct.create)
    ..aOB(4, _omitFieldNames ? '' : 'enabled')
    ..aOS(5, _omitFieldNames ? '' : 'description')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MemoryToolDescriptor clone() => deepCopy();
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
  ListMemoryToolsRequest clone() => deepCopy();
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
    ..pPM<MemoryToolDescriptor>(1, _omitFieldNames ? '' : 'tools',
        subBuilder: MemoryToolDescriptor.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMemoryToolsResponse clone() => deepCopy();
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
  CallMemoryToolRequest clone() => deepCopy();
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
  CallMemoryToolResponse clone() => deepCopy();
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
