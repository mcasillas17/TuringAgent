// This is a generated file - do not edit.
//
// Generated from turing/v1/runtime.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import '../../google/protobuf/struct.pb.dart' as $2;
import 'common.pb.dart' as $1;
import 'events.pb.dart' as $3;
import 'runtime.pbenum.dart';
import 'tools.pb.dart' as $4;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'runtime.pbenum.dart';

class AgentJob extends $pb.GeneratedMessage {
  factory AgentJob({
    $core.String? jobId,
    $core.String? runId,
    $core.String? sessionId,
    $core.String? userMessageId,
    $core.String? assistantMessageId,
    $1.AgentId? agentId,
    $core.String? traceId,
    $1.ModelProvider? modelProvider,
    $core.String? model,
    $core.String? userText,
    $core.Iterable<$core.String>? requestedTools,
    $core.int? attempt,
    $core.Iterable<SkillSnapshot>? skills,
    ExternalAgentTarget? externalAgent,
    $core.int? requiredContextTokens,
    $core.int? minimumWorkerMaxConcurrentRuns,
    $1.RunEgressDecision? egressDecision,
    $core.Iterable<$core.String>? selectedTools,
    $fixnum.Int64? expectedStateVersion,
    $core.String? assignmentAttemptId,
    PinnedPersonaSnapshot? pinnedPersona,
    PinnedProfileSnapshot? pinnedProfile,
    $core.String? memorySnapshotFingerprint,
  }) {
    final result = create();
    if (jobId != null) result.jobId = jobId;
    if (runId != null) result.runId = runId;
    if (sessionId != null) result.sessionId = sessionId;
    if (userMessageId != null) result.userMessageId = userMessageId;
    if (assistantMessageId != null)
      result.assistantMessageId = assistantMessageId;
    if (agentId != null) result.agentId = agentId;
    if (traceId != null) result.traceId = traceId;
    if (modelProvider != null) result.modelProvider = modelProvider;
    if (model != null) result.model = model;
    if (userText != null) result.userText = userText;
    if (requestedTools != null) result.requestedTools.addAll(requestedTools);
    if (attempt != null) result.attempt = attempt;
    if (skills != null) result.skills.addAll(skills);
    if (externalAgent != null) result.externalAgent = externalAgent;
    if (requiredContextTokens != null)
      result.requiredContextTokens = requiredContextTokens;
    if (minimumWorkerMaxConcurrentRuns != null)
      result.minimumWorkerMaxConcurrentRuns = minimumWorkerMaxConcurrentRuns;
    if (egressDecision != null) result.egressDecision = egressDecision;
    if (selectedTools != null) result.selectedTools.addAll(selectedTools);
    if (expectedStateVersion != null)
      result.expectedStateVersion = expectedStateVersion;
    if (assignmentAttemptId != null)
      result.assignmentAttemptId = assignmentAttemptId;
    if (pinnedPersona != null) result.pinnedPersona = pinnedPersona;
    if (pinnedProfile != null) result.pinnedProfile = pinnedProfile;
    if (memorySnapshotFingerprint != null)
      result.memorySnapshotFingerprint = memorySnapshotFingerprint;
    return result;
  }

  AgentJob._();

  factory AgentJob.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory AgentJob.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'AgentJob',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'jobId')
    ..aOS(2, _omitFieldNames ? '' : 'runId')
    ..aOS(3, _omitFieldNames ? '' : 'sessionId')
    ..aOS(4, _omitFieldNames ? '' : 'userMessageId')
    ..aOS(5, _omitFieldNames ? '' : 'assistantMessageId')
    ..aE<$1.AgentId>(6, _omitFieldNames ? '' : 'agentId',
        enumValues: $1.AgentId.values)
    ..aOS(7, _omitFieldNames ? '' : 'traceId')
    ..aE<$1.ModelProvider>(8, _omitFieldNames ? '' : 'modelProvider',
        enumValues: $1.ModelProvider.values)
    ..aOS(9, _omitFieldNames ? '' : 'model')
    ..aOS(10, _omitFieldNames ? '' : 'userText')
    ..pPS(11, _omitFieldNames ? '' : 'requestedTools')
    ..aI(12, _omitFieldNames ? '' : 'attempt')
    ..pPM<SkillSnapshot>(13, _omitFieldNames ? '' : 'skills',
        subBuilder: SkillSnapshot.create)
    ..aOM<ExternalAgentTarget>(14, _omitFieldNames ? '' : 'externalAgent',
        subBuilder: ExternalAgentTarget.create)
    ..aI(15, _omitFieldNames ? '' : 'requiredContextTokens')
    ..aI(16, _omitFieldNames ? '' : 'minimumWorkerMaxConcurrentRuns')
    ..aOM<$1.RunEgressDecision>(17, _omitFieldNames ? '' : 'egressDecision',
        subBuilder: $1.RunEgressDecision.create)
    ..pPS(18, _omitFieldNames ? '' : 'selectedTools')
    ..aInt64(19, _omitFieldNames ? '' : 'expectedStateVersion')
    ..aOS(20, _omitFieldNames ? '' : 'assignmentAttemptId')
    ..aOM<PinnedPersonaSnapshot>(21, _omitFieldNames ? '' : 'pinnedPersona',
        subBuilder: PinnedPersonaSnapshot.create)
    ..aOM<PinnedProfileSnapshot>(22, _omitFieldNames ? '' : 'pinnedProfile',
        subBuilder: PinnedProfileSnapshot.create)
    ..aOS(23, _omitFieldNames ? '' : 'memorySnapshotFingerprint')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AgentJob clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AgentJob copyWith(void Function(AgentJob) updates) =>
      super.copyWith((message) => updates(message as AgentJob)) as AgentJob;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static AgentJob create() => AgentJob._();
  @$core.override
  AgentJob createEmptyInstance() => create();
  static $pb.PbList<AgentJob> createRepeated() => $pb.PbList<AgentJob>();
  @$core.pragma('dart2js:noInline')
  static AgentJob getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<AgentJob>(create);
  static AgentJob? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get jobId => $_getSZ(0);
  @$pb.TagNumber(1)
  set jobId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasJobId() => $_has(0);
  @$pb.TagNumber(1)
  void clearJobId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get runId => $_getSZ(1);
  @$pb.TagNumber(2)
  set runId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRunId() => $_has(1);
  @$pb.TagNumber(2)
  void clearRunId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get sessionId => $_getSZ(2);
  @$pb.TagNumber(3)
  set sessionId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasSessionId() => $_has(2);
  @$pb.TagNumber(3)
  void clearSessionId() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get userMessageId => $_getSZ(3);
  @$pb.TagNumber(4)
  set userMessageId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasUserMessageId() => $_has(3);
  @$pb.TagNumber(4)
  void clearUserMessageId() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get assistantMessageId => $_getSZ(4);
  @$pb.TagNumber(5)
  set assistantMessageId($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasAssistantMessageId() => $_has(4);
  @$pb.TagNumber(5)
  void clearAssistantMessageId() => $_clearField(5);

  @$pb.TagNumber(6)
  $1.AgentId get agentId => $_getN(5);
  @$pb.TagNumber(6)
  set agentId($1.AgentId value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasAgentId() => $_has(5);
  @$pb.TagNumber(6)
  void clearAgentId() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.String get traceId => $_getSZ(6);
  @$pb.TagNumber(7)
  set traceId($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasTraceId() => $_has(6);
  @$pb.TagNumber(7)
  void clearTraceId() => $_clearField(7);

  @$pb.TagNumber(8)
  $1.ModelProvider get modelProvider => $_getN(7);
  @$pb.TagNumber(8)
  set modelProvider($1.ModelProvider value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasModelProvider() => $_has(7);
  @$pb.TagNumber(8)
  void clearModelProvider() => $_clearField(8);

  @$pb.TagNumber(9)
  $core.String get model => $_getSZ(8);
  @$pb.TagNumber(9)
  set model($core.String value) => $_setString(8, value);
  @$pb.TagNumber(9)
  $core.bool hasModel() => $_has(8);
  @$pb.TagNumber(9)
  void clearModel() => $_clearField(9);

  @$pb.TagNumber(10)
  $core.String get userText => $_getSZ(9);
  @$pb.TagNumber(10)
  set userText($core.String value) => $_setString(9, value);
  @$pb.TagNumber(10)
  $core.bool hasUserText() => $_has(9);
  @$pb.TagNumber(10)
  void clearUserText() => $_clearField(10);

  @$pb.TagNumber(11)
  $pb.PbList<$core.String> get requestedTools => $_getList(10);

  @$pb.TagNumber(12)
  $core.int get attempt => $_getIZ(11);
  @$pb.TagNumber(12)
  set attempt($core.int value) => $_setSignedInt32(11, value);
  @$pb.TagNumber(12)
  $core.bool hasAttempt() => $_has(11);
  @$pb.TagNumber(12)
  void clearAttempt() => $_clearField(12);

  /// The eligible filesystem skills when the message was accepted, captured
  /// then rather than read at execution time so edits cannot rewrite a queued
  /// run. Older queued jobs may still carry the legacy name/instructions pair.
  @$pb.TagNumber(13)
  $pb.PbList<SkillSnapshot> get skills => $_getList(12);

  /// Set only when the conversation was deliberately routed away from the
  /// local assistant. Absent is the default and means "run this here".
  @$pb.TagNumber(14)
  ExternalAgentTarget get externalAgent => $_getN(13);
  @$pb.TagNumber(14)
  set externalAgent(ExternalAgentTarget value) => $_setField(14, value);
  @$pb.TagNumber(14)
  $core.bool hasExternalAgent() => $_has(13);
  @$pb.TagNumber(14)
  void clearExternalAgent() => $_clearField(14);
  @$pb.TagNumber(14)
  ExternalAgentTarget ensureExternalAgent() => $_ensure(13);

  @$pb.TagNumber(15)
  $core.int get requiredContextTokens => $_getIZ(14);
  @$pb.TagNumber(15)
  set requiredContextTokens($core.int value) => $_setSignedInt32(14, value);
  @$pb.TagNumber(15)
  $core.bool hasRequiredContextTokens() => $_has(14);
  @$pb.TagNumber(15)
  void clearRequiredContextTokens() => $_clearField(15);

  @$pb.TagNumber(16)
  $core.int get minimumWorkerMaxConcurrentRuns => $_getIZ(15);
  @$pb.TagNumber(16)
  set minimumWorkerMaxConcurrentRuns($core.int value) =>
      $_setSignedInt32(15, value);
  @$pb.TagNumber(16)
  $core.bool hasMinimumWorkerMaxConcurrentRuns() => $_has(15);
  @$pb.TagNumber(16)
  void clearMinimumWorkerMaxConcurrentRuns() => $_clearField(16);

  @$pb.TagNumber(17)
  $1.RunEgressDecision get egressDecision => $_getN(16);
  @$pb.TagNumber(17)
  set egressDecision($1.RunEgressDecision value) => $_setField(17, value);
  @$pb.TagNumber(17)
  $core.bool hasEgressDecision() => $_has(16);
  @$pb.TagNumber(17)
  void clearEgressDecision() => $_clearField(17);
  @$pb.TagNumber(17)
  $1.RunEgressDecision ensureEgressDecision() => $_ensure(16);

  /// Exact tool names frozen by the orchestrator for this run. The runtime may
  /// expose a subset after context budgeting, never tools outside this set.
  @$pb.TagNumber(18)
  $pb.PbList<$core.String> get selectedTools => $_getList(17);

  /// The run's state version at assignment. The worker echoes it on later
  /// reports so the orchestrator can reject anything computed against a state it
  /// has already moved past.
  @$pb.TagNumber(19)
  $fixnum.Int64 get expectedStateVersion => $_getI64(18);
  @$pb.TagNumber(19)
  set expectedStateVersion($fixnum.Int64 value) => $_setInt64(18, value);
  @$pb.TagNumber(19)
  $core.bool hasExpectedStateVersion() => $_has(18);
  @$pb.TagNumber(19)
  void clearExpectedStateVersion() => $_clearField(19);

  /// Durable identity of this assignment attempt. It is what proves a later
  /// report or resume came from the attempt that still owns the run, rather than
  /// from a fenced predecessor; the worker must echo it unchanged.
  @$pb.TagNumber(20)
  $core.String get assignmentAttemptId => $_getSZ(19);
  @$pb.TagNumber(20)
  set assignmentAttemptId($core.String value) => $_setString(19, value);
  @$pb.TagNumber(20)
  $core.bool hasAssignmentAttemptId() => $_has(19);
  @$pb.TagNumber(20)
  void clearAssignmentAttemptId() => $_clearField(20);

  /// The persona and profile as they read when the message was accepted, pinned
  /// exactly like skills so a later vault edit cannot rewrite a queued run.
  /// withheld says the tier was off or unreadable, which is a different fact
  /// from an empty body and must not be inferred from one.
  @$pb.TagNumber(21)
  PinnedPersonaSnapshot get pinnedPersona => $_getN(20);
  @$pb.TagNumber(21)
  set pinnedPersona(PinnedPersonaSnapshot value) => $_setField(21, value);
  @$pb.TagNumber(21)
  $core.bool hasPinnedPersona() => $_has(20);
  @$pb.TagNumber(21)
  void clearPinnedPersona() => $_clearField(21);
  @$pb.TagNumber(21)
  PinnedPersonaSnapshot ensurePinnedPersona() => $_ensure(20);

  @$pb.TagNumber(22)
  PinnedProfileSnapshot get pinnedProfile => $_getN(21);
  @$pb.TagNumber(22)
  set pinnedProfile(PinnedProfileSnapshot value) => $_setField(22, value);
  @$pb.TagNumber(22)
  $core.bool hasPinnedProfile() => $_has(21);
  @$pb.TagNumber(22)
  void clearPinnedProfile() => $_clearField(22);
  @$pb.TagNumber(22)
  PinnedProfileSnapshot ensurePinnedProfile() => $_ensure(21);

  /// Binds this job to the memory snapshot the egress decision was granted
  /// against. Internal to the run protocol; never surfaced to a client.
  @$pb.TagNumber(23)
  $core.String get memorySnapshotFingerprint => $_getSZ(22);
  @$pb.TagNumber(23)
  set memorySnapshotFingerprint($core.String value) => $_setString(22, value);
  @$pb.TagNumber(23)
  $core.bool hasMemorySnapshotFingerprint() => $_has(22);
  @$pb.TagNumber(23)
  void clearMemorySnapshotFingerprint() => $_clearField(23);
}

class PinnedPersonaSnapshot extends $pb.GeneratedMessage {
  factory PinnedPersonaSnapshot({
    $core.String? personaId,
    $core.String? displayName,
    $core.String? body,
    $core.String? contentHash,
    $core.bool? withheld,
  }) {
    final result = create();
    if (personaId != null) result.personaId = personaId;
    if (displayName != null) result.displayName = displayName;
    if (body != null) result.body = body;
    if (contentHash != null) result.contentHash = contentHash;
    if (withheld != null) result.withheld = withheld;
    return result;
  }

  PinnedPersonaSnapshot._();

  factory PinnedPersonaSnapshot.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PinnedPersonaSnapshot.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PinnedPersonaSnapshot',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'personaId')
    ..aOS(2, _omitFieldNames ? '' : 'displayName')
    ..aOS(3, _omitFieldNames ? '' : 'body')
    ..aOS(4, _omitFieldNames ? '' : 'contentHash')
    ..aOB(5, _omitFieldNames ? '' : 'withheld')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PinnedPersonaSnapshot clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PinnedPersonaSnapshot copyWith(
          void Function(PinnedPersonaSnapshot) updates) =>
      super.copyWith((message) => updates(message as PinnedPersonaSnapshot))
          as PinnedPersonaSnapshot;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PinnedPersonaSnapshot create() => PinnedPersonaSnapshot._();
  @$core.override
  PinnedPersonaSnapshot createEmptyInstance() => create();
  static $pb.PbList<PinnedPersonaSnapshot> createRepeated() =>
      $pb.PbList<PinnedPersonaSnapshot>();
  @$core.pragma('dart2js:noInline')
  static PinnedPersonaSnapshot getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PinnedPersonaSnapshot>(create);
  static PinnedPersonaSnapshot? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get personaId => $_getSZ(0);
  @$pb.TagNumber(1)
  set personaId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasPersonaId() => $_has(0);
  @$pb.TagNumber(1)
  void clearPersonaId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get displayName => $_getSZ(1);
  @$pb.TagNumber(2)
  set displayName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDisplayName() => $_has(1);
  @$pb.TagNumber(2)
  void clearDisplayName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get body => $_getSZ(2);
  @$pb.TagNumber(3)
  set body($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasBody() => $_has(2);
  @$pb.TagNumber(3)
  void clearBody() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get contentHash => $_getSZ(3);
  @$pb.TagNumber(4)
  set contentHash($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasContentHash() => $_has(3);
  @$pb.TagNumber(4)
  void clearContentHash() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.bool get withheld => $_getBF(4);
  @$pb.TagNumber(5)
  set withheld($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasWithheld() => $_has(4);
  @$pb.TagNumber(5)
  void clearWithheld() => $_clearField(5);
}

class PinnedProfileSnapshot extends $pb.GeneratedMessage {
  factory PinnedProfileSnapshot({
    $core.String? profileId,
    $core.String? body,
    $core.String? contentHash,
    $core.bool? withheld,
  }) {
    final result = create();
    if (profileId != null) result.profileId = profileId;
    if (body != null) result.body = body;
    if (contentHash != null) result.contentHash = contentHash;
    if (withheld != null) result.withheld = withheld;
    return result;
  }

  PinnedProfileSnapshot._();

  factory PinnedProfileSnapshot.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PinnedProfileSnapshot.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PinnedProfileSnapshot',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'profileId')
    ..aOS(2, _omitFieldNames ? '' : 'body')
    ..aOS(3, _omitFieldNames ? '' : 'contentHash')
    ..aOB(4, _omitFieldNames ? '' : 'withheld')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PinnedProfileSnapshot clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PinnedProfileSnapshot copyWith(
          void Function(PinnedProfileSnapshot) updates) =>
      super.copyWith((message) => updates(message as PinnedProfileSnapshot))
          as PinnedProfileSnapshot;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PinnedProfileSnapshot create() => PinnedProfileSnapshot._();
  @$core.override
  PinnedProfileSnapshot createEmptyInstance() => create();
  static $pb.PbList<PinnedProfileSnapshot> createRepeated() =>
      $pb.PbList<PinnedProfileSnapshot>();
  @$core.pragma('dart2js:noInline')
  static PinnedProfileSnapshot getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PinnedProfileSnapshot>(create);
  static PinnedProfileSnapshot? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get profileId => $_getSZ(0);
  @$pb.TagNumber(1)
  set profileId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasProfileId() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfileId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get body => $_getSZ(1);
  @$pb.TagNumber(2)
  set body($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasBody() => $_has(1);
  @$pb.TagNumber(2)
  void clearBody() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get contentHash => $_getSZ(2);
  @$pb.TagNumber(3)
  set contentHash($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasContentHash() => $_has(2);
  @$pb.TagNumber(3)
  void clearContentHash() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.bool get withheld => $_getBF(3);
  @$pb.TagNumber(4)
  set withheld($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasWithheld() => $_has(3);
  @$pb.TagNumber(4)
  void clearWithheld() => $_clearField(4);
}

/// Where to send a run that the user routed off this machine.
///
/// Deliberately not the full ExternalAgent message: the runtime has no use for
/// identifiers, timestamps or the provider label, and this is replayed from a
/// stored job payload where a narrower shape is a narrower thing to keep in
/// sync. The model is not repeated here either — AgentJob.model already carries
/// it, frozen at enqueue from the agent's configuration.
///
/// credential_ref is a NAME, not a secret. The key it names is resolved from
/// the runtime's own environment at execution time, so no third-party API key
/// is ever written to the database, sent over this stream, or handed to a
/// client.
class ExternalAgentTarget extends $pb.GeneratedMessage {
  factory ExternalAgentTarget({
    $core.String? displayName,
    $core.String? baseUrl,
    $core.String? credentialRef,
    $core.String? agentId,
  }) {
    final result = create();
    if (displayName != null) result.displayName = displayName;
    if (baseUrl != null) result.baseUrl = baseUrl;
    if (credentialRef != null) result.credentialRef = credentialRef;
    if (agentId != null) result.agentId = agentId;
    return result;
  }

  ExternalAgentTarget._();

  factory ExternalAgentTarget.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ExternalAgentTarget.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ExternalAgentTarget',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'displayName')
    ..aOS(2, _omitFieldNames ? '' : 'baseUrl')
    ..aOS(3, _omitFieldNames ? '' : 'credentialRef')
    ..aOS(4, _omitFieldNames ? '' : 'agentId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ExternalAgentTarget clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ExternalAgentTarget copyWith(void Function(ExternalAgentTarget) updates) =>
      super.copyWith((message) => updates(message as ExternalAgentTarget))
          as ExternalAgentTarget;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ExternalAgentTarget create() => ExternalAgentTarget._();
  @$core.override
  ExternalAgentTarget createEmptyInstance() => create();
  static $pb.PbList<ExternalAgentTarget> createRepeated() =>
      $pb.PbList<ExternalAgentTarget>();
  @$core.pragma('dart2js:noInline')
  static ExternalAgentTarget getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ExternalAgentTarget>(create);
  static ExternalAgentTarget? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get displayName => $_getSZ(0);
  @$pb.TagNumber(1)
  set displayName($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasDisplayName() => $_has(0);
  @$pb.TagNumber(1)
  void clearDisplayName() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get baseUrl => $_getSZ(1);
  @$pb.TagNumber(2)
  set baseUrl($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasBaseUrl() => $_has(1);
  @$pb.TagNumber(2)
  void clearBaseUrl() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get credentialRef => $_getSZ(2);
  @$pb.TagNumber(3)
  set credentialRef($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasCredentialRef() => $_has(2);
  @$pb.TagNumber(3)
  void clearCredentialRef() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get agentId => $_getSZ(3);
  @$pb.TagNumber(4)
  set agentId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasAgentId() => $_has(3);
  @$pb.TagNumber(4)
  void clearAgentId() => $_clearField(4);
}

/// A frozen skill snapshot as the runtime sees it. instructions retains field 2
/// for wire compatibility with jobs queued by the database-backed model; for
/// filesystem skills it carries the SKILL.md body.
class SkillSnapshot extends $pb.GeneratedMessage {
  factory SkillSnapshot({
    $core.String? name,
    $core.String? instructions,
    $core.String? skillId,
    $core.String? description,
    $core.String? category,
    $core.Iterable<$core.MapEntry<$core.String, $core.String>>? references,
    $core.bool? withheld,
    $core.Iterable<$core.String>? missingCapabilities,
  }) {
    final result = create();
    if (name != null) result.name = name;
    if (instructions != null) result.instructions = instructions;
    if (skillId != null) result.skillId = skillId;
    if (description != null) result.description = description;
    if (category != null) result.category = category;
    if (references != null) result.references.addEntries(references);
    if (withheld != null) result.withheld = withheld;
    if (missingCapabilities != null)
      result.missingCapabilities.addAll(missingCapabilities);
    return result;
  }

  SkillSnapshot._();

  factory SkillSnapshot.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SkillSnapshot.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SkillSnapshot',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'name')
    ..aOS(2, _omitFieldNames ? '' : 'instructions')
    ..aOS(3, _omitFieldNames ? '' : 'skillId')
    ..aOS(4, _omitFieldNames ? '' : 'description')
    ..aOS(5, _omitFieldNames ? '' : 'category')
    ..m<$core.String, $core.String>(6, _omitFieldNames ? '' : 'references',
        entryClassName: 'SkillSnapshot.ReferencesEntry',
        keyFieldType: $pb.PbFieldType.OS,
        valueFieldType: $pb.PbFieldType.OS,
        packageName: const $pb.PackageName('turing.v1'))
    ..aOB(7, _omitFieldNames ? '' : 'withheld')
    ..pPS(8, _omitFieldNames ? '' : 'missingCapabilities')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SkillSnapshot clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SkillSnapshot copyWith(void Function(SkillSnapshot) updates) =>
      super.copyWith((message) => updates(message as SkillSnapshot))
          as SkillSnapshot;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SkillSnapshot create() => SkillSnapshot._();
  @$core.override
  SkillSnapshot createEmptyInstance() => create();
  static $pb.PbList<SkillSnapshot> createRepeated() =>
      $pb.PbList<SkillSnapshot>();
  @$core.pragma('dart2js:noInline')
  static SkillSnapshot getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SkillSnapshot>(create);
  static SkillSnapshot? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get name => $_getSZ(0);
  @$pb.TagNumber(1)
  set name($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasName() => $_has(0);
  @$pb.TagNumber(1)
  void clearName() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get instructions => $_getSZ(1);
  @$pb.TagNumber(2)
  set instructions($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasInstructions() => $_has(1);
  @$pb.TagNumber(2)
  void clearInstructions() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get skillId => $_getSZ(2);
  @$pb.TagNumber(3)
  set skillId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasSkillId() => $_has(2);
  @$pb.TagNumber(3)
  void clearSkillId() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get description => $_getSZ(3);
  @$pb.TagNumber(4)
  set description($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasDescription() => $_has(3);
  @$pb.TagNumber(4)
  void clearDescription() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get category => $_getSZ(4);
  @$pb.TagNumber(5)
  set category($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasCategory() => $_has(4);
  @$pb.TagNumber(5)
  void clearCategory() => $_clearField(5);

  @$pb.TagNumber(6)
  $pb.PbMap<$core.String, $core.String> get references => $_getMap(5);

  /// True when this enabled skill was missing one or more declared grants when
  /// the run was queued. Metadata remains visible in the enabled index, but
  /// neither its body nor its references are present in the snapshot.
  @$pb.TagNumber(7)
  $core.bool get withheld => $_getBF(6);
  @$pb.TagNumber(7)
  set withheld($core.bool value) => $_setBool(6, value);
  @$pb.TagNumber(7)
  $core.bool hasWithheld() => $_has(6);
  @$pb.TagNumber(7)
  void clearWithheld() => $_clearField(7);

  @$pb.TagNumber(8)
  $pb.PbList<$core.String> get missingCapabilities => $_getList(7);
}

class DiscoveredTool extends $pb.GeneratedMessage {
  factory DiscoveredTool({
    $core.String? serverName,
    $core.String? toolName,
    $2.Struct? schema,
  }) {
    final result = create();
    if (serverName != null) result.serverName = serverName;
    if (toolName != null) result.toolName = toolName;
    if (schema != null) result.schema = schema;
    return result;
  }

  DiscoveredTool._();

  factory DiscoveredTool.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DiscoveredTool.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DiscoveredTool',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'serverName')
    ..aOS(2, _omitFieldNames ? '' : 'toolName')
    ..aOM<$2.Struct>(3, _omitFieldNames ? '' : 'schema',
        subBuilder: $2.Struct.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DiscoveredTool clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DiscoveredTool copyWith(void Function(DiscoveredTool) updates) =>
      super.copyWith((message) => updates(message as DiscoveredTool))
          as DiscoveredTool;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DiscoveredTool create() => DiscoveredTool._();
  @$core.override
  DiscoveredTool createEmptyInstance() => create();
  static $pb.PbList<DiscoveredTool> createRepeated() =>
      $pb.PbList<DiscoveredTool>();
  @$core.pragma('dart2js:noInline')
  static DiscoveredTool getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DiscoveredTool>(create);
  static DiscoveredTool? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get serverName => $_getSZ(0);
  @$pb.TagNumber(1)
  set serverName($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasServerName() => $_has(0);
  @$pb.TagNumber(1)
  void clearServerName() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get toolName => $_getSZ(1);
  @$pb.TagNumber(2)
  set toolName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasToolName() => $_has(1);
  @$pb.TagNumber(2)
  void clearToolName() => $_clearField(2);

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
}

/// A complete, authoritative snapshot for one runtime registration.
class WorkerCapabilities extends $pb.GeneratedMessage {
  factory WorkerCapabilities({
    $core.Iterable<$1.ModelCapability>? models,
    $core.Iterable<$1.AgentId>? agentIds,
    $core.Iterable<DiscoveredTool>? tools,
    $core.int? maxConcurrentRuns,
    $core.bool? supportsExternalAgents,
    $core.Iterable<$core.String>? externalAgentCredentialRefs,
    $core.int? remoteEgressDecisionVersion,
  }) {
    final result = create();
    if (models != null) result.models.addAll(models);
    if (agentIds != null) result.agentIds.addAll(agentIds);
    if (tools != null) result.tools.addAll(tools);
    if (maxConcurrentRuns != null) result.maxConcurrentRuns = maxConcurrentRuns;
    if (supportsExternalAgents != null)
      result.supportsExternalAgents = supportsExternalAgents;
    if (externalAgentCredentialRefs != null)
      result.externalAgentCredentialRefs.addAll(externalAgentCredentialRefs);
    if (remoteEgressDecisionVersion != null)
      result.remoteEgressDecisionVersion = remoteEgressDecisionVersion;
    return result;
  }

  WorkerCapabilities._();

  factory WorkerCapabilities.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory WorkerCapabilities.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'WorkerCapabilities',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..pPM<$1.ModelCapability>(1, _omitFieldNames ? '' : 'models',
        subBuilder: $1.ModelCapability.create)
    ..pc<$1.AgentId>(2, _omitFieldNames ? '' : 'agentIds', $pb.PbFieldType.KE,
        valueOf: $1.AgentId.valueOf,
        enumValues: $1.AgentId.values,
        defaultEnumValue: $1.AgentId.AGENT_ID_UNSPECIFIED)
    ..pPM<DiscoveredTool>(3, _omitFieldNames ? '' : 'tools',
        subBuilder: DiscoveredTool.create)
    ..aI(4, _omitFieldNames ? '' : 'maxConcurrentRuns')
    ..aOB(5, _omitFieldNames ? '' : 'supportsExternalAgents')
    ..pPS(6, _omitFieldNames ? '' : 'externalAgentCredentialRefs')
    ..aI(7, _omitFieldNames ? '' : 'remoteEgressDecisionVersion')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  WorkerCapabilities clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  WorkerCapabilities copyWith(void Function(WorkerCapabilities) updates) =>
      super.copyWith((message) => updates(message as WorkerCapabilities))
          as WorkerCapabilities;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static WorkerCapabilities create() => WorkerCapabilities._();
  @$core.override
  WorkerCapabilities createEmptyInstance() => create();
  static $pb.PbList<WorkerCapabilities> createRepeated() =>
      $pb.PbList<WorkerCapabilities>();
  @$core.pragma('dart2js:noInline')
  static WorkerCapabilities getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<WorkerCapabilities>(create);
  static WorkerCapabilities? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$1.ModelCapability> get models => $_getList(0);

  @$pb.TagNumber(2)
  $pb.PbList<$1.AgentId> get agentIds => $_getList(1);

  @$pb.TagNumber(3)
  $pb.PbList<DiscoveredTool> get tools => $_getList(2);

  @$pb.TagNumber(4)
  $core.int get maxConcurrentRuns => $_getIZ(3);
  @$pb.TagNumber(4)
  set maxConcurrentRuns($core.int value) => $_setSignedInt32(3, value);
  @$pb.TagNumber(4)
  $core.bool hasMaxConcurrentRuns() => $_has(3);
  @$pb.TagNumber(4)
  void clearMaxConcurrentRuns() => $_clearField(4);

  /// Kept for older orchestrators. New routing decisions use the exact refs
  /// below and never treat this coarse bit as authorization.
  @$pb.TagNumber(5)
  $core.bool get supportsExternalAgents => $_getBF(4);
  @$pb.TagNumber(5)
  set supportsExternalAgents($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasSupportsExternalAgents() => $_has(4);
  @$pb.TagNumber(5)
  void clearSupportsExternalAgents() => $_clearField(5);

  /// Credential names only, never API keys. This complete set authorizes which
  /// frozen external-agent destinations this worker can execute.
  @$pb.TagNumber(6)
  $pb.PbList<$core.String> get externalAgentCredentialRefs => $_getList(5);

  /// Highest run-egress decision version this worker enforces before provider
  /// I/O. Zero means it predates explicit remote-egress enforcement.
  @$pb.TagNumber(7)
  $core.int get remoteEgressDecisionVersion => $_getIZ(6);
  @$pb.TagNumber(7)
  set remoteEgressDecisionVersion($core.int value) =>
      $_setSignedInt32(6, value);
  @$pb.TagNumber(7)
  $core.bool hasRemoteEgressDecisionVersion() => $_has(6);
  @$pb.TagNumber(7)
  void clearRemoteEgressDecisionVersion() => $_clearField(7);
}

class RuntimeWorkerReady extends $pb.GeneratedMessage {
  factory RuntimeWorkerReady({
    $core.String? workerId,
    $1.AgentId? agentId,
    $core.int? maxConcurrentRuns,
    $core.Iterable<DiscoveredTool>? tools,
    ToolDiscoveryStatus? toolDiscoveryStatus,
    $core.String? registrationId,
    WorkerCapabilities? capabilities,
  }) {
    final result = create();
    if (workerId != null) result.workerId = workerId;
    if (agentId != null) result.agentId = agentId;
    if (maxConcurrentRuns != null) result.maxConcurrentRuns = maxConcurrentRuns;
    if (tools != null) result.tools.addAll(tools);
    if (toolDiscoveryStatus != null)
      result.toolDiscoveryStatus = toolDiscoveryStatus;
    if (registrationId != null) result.registrationId = registrationId;
    if (capabilities != null) result.capabilities = capabilities;
    return result;
  }

  RuntimeWorkerReady._();

  factory RuntimeWorkerReady.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeWorkerReady.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeWorkerReady',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'workerId')
    ..aE<$1.AgentId>(2, _omitFieldNames ? '' : 'agentId',
        enumValues: $1.AgentId.values)
    ..aI(3, _omitFieldNames ? '' : 'maxConcurrentRuns')
    ..pPM<DiscoveredTool>(4, _omitFieldNames ? '' : 'tools',
        subBuilder: DiscoveredTool.create)
    ..aE<ToolDiscoveryStatus>(5, _omitFieldNames ? '' : 'toolDiscoveryStatus',
        enumValues: ToolDiscoveryStatus.values)
    ..aOS(6, _omitFieldNames ? '' : 'registrationId')
    ..aOM<WorkerCapabilities>(7, _omitFieldNames ? '' : 'capabilities',
        subBuilder: WorkerCapabilities.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeWorkerReady clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeWorkerReady copyWith(void Function(RuntimeWorkerReady) updates) =>
      super.copyWith((message) => updates(message as RuntimeWorkerReady))
          as RuntimeWorkerReady;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeWorkerReady create() => RuntimeWorkerReady._();
  @$core.override
  RuntimeWorkerReady createEmptyInstance() => create();
  static $pb.PbList<RuntimeWorkerReady> createRepeated() =>
      $pb.PbList<RuntimeWorkerReady>();
  @$core.pragma('dart2js:noInline')
  static RuntimeWorkerReady getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeWorkerReady>(create);
  static RuntimeWorkerReady? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get workerId => $_getSZ(0);
  @$pb.TagNumber(1)
  set workerId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasWorkerId() => $_has(0);
  @$pb.TagNumber(1)
  void clearWorkerId() => $_clearField(1);

  @$pb.TagNumber(2)
  $1.AgentId get agentId => $_getN(1);
  @$pb.TagNumber(2)
  set agentId($1.AgentId value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasAgentId() => $_has(1);
  @$pb.TagNumber(2)
  void clearAgentId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.int get maxConcurrentRuns => $_getIZ(2);
  @$pb.TagNumber(3)
  set maxConcurrentRuns($core.int value) => $_setSignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasMaxConcurrentRuns() => $_has(2);
  @$pb.TagNumber(3)
  void clearMaxConcurrentRuns() => $_clearField(3);

  /// Complete snapshot of tools discovered by this worker.
  @$pb.TagNumber(4)
  $pb.PbList<DiscoveredTool> get tools => $_getList(3);

  @$pb.TagNumber(5)
  ToolDiscoveryStatus get toolDiscoveryStatus => $_getN(4);
  @$pb.TagNumber(5)
  set toolDiscoveryStatus(ToolDiscoveryStatus value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasToolDiscoveryStatus() => $_has(4);
  @$pb.TagNumber(5)
  void clearToolDiscoveryStatus() => $_clearField(5);

  /// Unique to this stream. A reconnect for the same worker_id uses a fresh ID.
  @$pb.TagNumber(6)
  $core.String get registrationId => $_getSZ(5);
  @$pb.TagNumber(6)
  set registrationId($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasRegistrationId() => $_has(5);
  @$pb.TagNumber(6)
  void clearRegistrationId() => $_clearField(6);

  @$pb.TagNumber(7)
  WorkerCapabilities get capabilities => $_getN(6);
  @$pb.TagNumber(7)
  set capabilities(WorkerCapabilities value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasCapabilities() => $_has(6);
  @$pb.TagNumber(7)
  void clearCapabilities() => $_clearField(7);
  @$pb.TagNumber(7)
  WorkerCapabilities ensureCapabilities() => $_ensure(6);
}

class RuntimeWorkerCapabilitiesUpdated extends $pb.GeneratedMessage {
  factory RuntimeWorkerCapabilitiesUpdated({
    $core.String? workerId,
    $core.String? registrationId,
    WorkerCapabilities? capabilities,
  }) {
    final result = create();
    if (workerId != null) result.workerId = workerId;
    if (registrationId != null) result.registrationId = registrationId;
    if (capabilities != null) result.capabilities = capabilities;
    return result;
  }

  RuntimeWorkerCapabilitiesUpdated._();

  factory RuntimeWorkerCapabilitiesUpdated.fromBuffer(
          $core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeWorkerCapabilitiesUpdated.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeWorkerCapabilitiesUpdated',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'workerId')
    ..aOS(2, _omitFieldNames ? '' : 'registrationId')
    ..aOM<WorkerCapabilities>(3, _omitFieldNames ? '' : 'capabilities',
        subBuilder: WorkerCapabilities.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeWorkerCapabilitiesUpdated clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeWorkerCapabilitiesUpdated copyWith(
          void Function(RuntimeWorkerCapabilitiesUpdated) updates) =>
      super.copyWith(
              (message) => updates(message as RuntimeWorkerCapabilitiesUpdated))
          as RuntimeWorkerCapabilitiesUpdated;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeWorkerCapabilitiesUpdated create() =>
      RuntimeWorkerCapabilitiesUpdated._();
  @$core.override
  RuntimeWorkerCapabilitiesUpdated createEmptyInstance() => create();
  static $pb.PbList<RuntimeWorkerCapabilitiesUpdated> createRepeated() =>
      $pb.PbList<RuntimeWorkerCapabilitiesUpdated>();
  @$core.pragma('dart2js:noInline')
  static RuntimeWorkerCapabilitiesUpdated getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeWorkerCapabilitiesUpdated>(
          create);
  static RuntimeWorkerCapabilitiesUpdated? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get workerId => $_getSZ(0);
  @$pb.TagNumber(1)
  set workerId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasWorkerId() => $_has(0);
  @$pb.TagNumber(1)
  void clearWorkerId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get registrationId => $_getSZ(1);
  @$pb.TagNumber(2)
  set registrationId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRegistrationId() => $_has(1);
  @$pb.TagNumber(2)
  void clearRegistrationId() => $_clearField(2);

  /// Replaces the previous snapshot rather than patching it.
  @$pb.TagNumber(3)
  WorkerCapabilities get capabilities => $_getN(2);
  @$pb.TagNumber(3)
  set capabilities(WorkerCapabilities value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasCapabilities() => $_has(2);
  @$pb.TagNumber(3)
  void clearCapabilities() => $_clearField(3);
  @$pb.TagNumber(3)
  WorkerCapabilities ensureCapabilities() => $_ensure(2);
}

class RuntimeHeartbeat extends $pb.GeneratedMessage {
  factory RuntimeHeartbeat({
    $core.String? workerId,
  }) {
    final result = create();
    if (workerId != null) result.workerId = workerId;
    return result;
  }

  RuntimeHeartbeat._();

  factory RuntimeHeartbeat.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeHeartbeat.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeHeartbeat',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'workerId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeHeartbeat clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeHeartbeat copyWith(void Function(RuntimeHeartbeat) updates) =>
      super.copyWith((message) => updates(message as RuntimeHeartbeat))
          as RuntimeHeartbeat;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeHeartbeat create() => RuntimeHeartbeat._();
  @$core.override
  RuntimeHeartbeat createEmptyInstance() => create();
  static $pb.PbList<RuntimeHeartbeat> createRepeated() =>
      $pb.PbList<RuntimeHeartbeat>();
  @$core.pragma('dart2js:noInline')
  static RuntimeHeartbeat getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeHeartbeat>(create);
  static RuntimeHeartbeat? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get workerId => $_getSZ(0);
  @$pb.TagNumber(1)
  set workerId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasWorkerId() => $_has(0);
  @$pb.TagNumber(1)
  void clearWorkerId() => $_clearField(1);
}

/// Token counts a provider reported, summed over every model turn a run made.
///
/// Both fields are optional and unset means UNKNOWN, never zero. Ollama reports
/// prompt_eval_count/eval_count on its terminal chunk and an OpenAI-compatible
/// provider reports a usage object; a provider that reports neither leaves this
/// message absent entirely. Nothing anywhere estimates these — a token count
/// nobody measured is worse than no token count, because someone will spend a
/// decision on it.
class RunTokenUsage extends $pb.GeneratedMessage {
  factory RunTokenUsage({
    $fixnum.Int64? inputTokens,
    $fixnum.Int64? outputTokens,
  }) {
    final result = create();
    if (inputTokens != null) result.inputTokens = inputTokens;
    if (outputTokens != null) result.outputTokens = outputTokens;
    return result;
  }

  RunTokenUsage._();

  factory RunTokenUsage.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RunTokenUsage.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RunTokenUsage',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aInt64(1, _omitFieldNames ? '' : 'inputTokens')
    ..aInt64(2, _omitFieldNames ? '' : 'outputTokens')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunTokenUsage clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunTokenUsage copyWith(void Function(RunTokenUsage) updates) =>
      super.copyWith((message) => updates(message as RunTokenUsage))
          as RunTokenUsage;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RunTokenUsage create() => RunTokenUsage._();
  @$core.override
  RunTokenUsage createEmptyInstance() => create();
  static $pb.PbList<RunTokenUsage> createRepeated() =>
      $pb.PbList<RunTokenUsage>();
  @$core.pragma('dart2js:noInline')
  static RunTokenUsage getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RunTokenUsage>(create);
  static RunTokenUsage? _defaultInstance;

  @$pb.TagNumber(1)
  $fixnum.Int64 get inputTokens => $_getI64(0);
  @$pb.TagNumber(1)
  set inputTokens($fixnum.Int64 value) => $_setInt64(0, value);
  @$pb.TagNumber(1)
  $core.bool hasInputTokens() => $_has(0);
  @$pb.TagNumber(1)
  void clearInputTokens() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get outputTokens => $_getI64(1);
  @$pb.TagNumber(2)
  set outputTokens($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasOutputTokens() => $_has(1);
  @$pb.TagNumber(2)
  void clearOutputTokens() => $_clearField(2);
}

class RuntimeRunCompleted extends $pb.GeneratedMessage {
  factory RuntimeRunCompleted({
    $core.String? runId,
    $core.String? assistantMessageId,
    $core.String? content,
    $2.Struct? usage,
    RunTokenUsage? tokenUsage,
    $fixnum.Int64? expectedStateVersion,
  }) {
    final result = create();
    if (runId != null) result.runId = runId;
    if (assistantMessageId != null)
      result.assistantMessageId = assistantMessageId;
    if (content != null) result.content = content;
    if (usage != null) result.usage = usage;
    if (tokenUsage != null) result.tokenUsage = tokenUsage;
    if (expectedStateVersion != null)
      result.expectedStateVersion = expectedStateVersion;
    return result;
  }

  RuntimeRunCompleted._();

  factory RuntimeRunCompleted.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeRunCompleted.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeRunCompleted',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'runId')
    ..aOS(2, _omitFieldNames ? '' : 'assistantMessageId')
    ..aOS(3, _omitFieldNames ? '' : 'content')
    ..aOM<$2.Struct>(4, _omitFieldNames ? '' : 'usage',
        subBuilder: $2.Struct.create)
    ..aOM<RunTokenUsage>(5, _omitFieldNames ? '' : 'tokenUsage',
        subBuilder: RunTokenUsage.create)
    ..aInt64(6, _omitFieldNames ? '' : 'expectedStateVersion')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeRunCompleted clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeRunCompleted copyWith(void Function(RuntimeRunCompleted) updates) =>
      super.copyWith((message) => updates(message as RuntimeRunCompleted))
          as RuntimeRunCompleted;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeRunCompleted create() => RuntimeRunCompleted._();
  @$core.override
  RuntimeRunCompleted createEmptyInstance() => create();
  static $pb.PbList<RuntimeRunCompleted> createRepeated() =>
      $pb.PbList<RuntimeRunCompleted>();
  @$core.pragma('dart2js:noInline')
  static RuntimeRunCompleted getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeRunCompleted>(create);
  static RuntimeRunCompleted? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get runId => $_getSZ(0);
  @$pb.TagNumber(1)
  set runId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRunId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRunId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get assistantMessageId => $_getSZ(1);
  @$pb.TagNumber(2)
  set assistantMessageId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasAssistantMessageId() => $_has(1);
  @$pb.TagNumber(2)
  void clearAssistantMessageId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get content => $_getSZ(2);
  @$pb.TagNumber(3)
  set content($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasContent() => $_has(2);
  @$pb.TagNumber(3)
  void clearContent() => $_clearField(3);

  /// Free-form provider payload, echoed into the run-completed event. Kept as
  /// it was rather than repurposed: token counts need presence semantics a
  /// Struct cannot express without the reader guessing whether a missing key
  /// means zero, so they travel in their own typed field below.
  @$pb.TagNumber(4)
  $2.Struct get usage => $_getN(3);
  @$pb.TagNumber(4)
  set usage($2.Struct value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasUsage() => $_has(3);
  @$pb.TagNumber(4)
  void clearUsage() => $_clearField(4);
  @$pb.TagNumber(4)
  $2.Struct ensureUsage() => $_ensure(3);

  @$pb.TagNumber(5)
  RunTokenUsage get tokenUsage => $_getN(4);
  @$pb.TagNumber(5)
  set tokenUsage(RunTokenUsage value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasTokenUsage() => $_has(4);
  @$pb.TagNumber(5)
  void clearTokenUsage() => $_clearField(5);
  @$pb.TagNumber(5)
  RunTokenUsage ensureTokenUsage() => $_ensure(4);

  /// The state version this report was computed against; the terminal
  /// transition commits only from that exact version.
  @$pb.TagNumber(6)
  $fixnum.Int64 get expectedStateVersion => $_getI64(5);
  @$pb.TagNumber(6)
  set expectedStateVersion($fixnum.Int64 value) => $_setInt64(5, value);
  @$pb.TagNumber(6)
  $core.bool hasExpectedStateVersion() => $_has(5);
  @$pb.TagNumber(6)
  void clearExpectedStateVersion() => $_clearField(6);
}

class RuntimeRunFailed extends $pb.GeneratedMessage {
  factory RuntimeRunFailed({
    $core.String? runId,
    $core.String? code,
    $core.String? message,
    $core.bool? retryable,
    FailureOrigin? failureOrigin,
    AutomaticRetryClass? automaticRetryClass,
    $fixnum.Int64? expectedStateVersion,
  }) {
    final result = create();
    if (runId != null) result.runId = runId;
    if (code != null) result.code = code;
    if (message != null) result.message = message;
    if (retryable != null) result.retryable = retryable;
    if (failureOrigin != null) result.failureOrigin = failureOrigin;
    if (automaticRetryClass != null)
      result.automaticRetryClass = automaticRetryClass;
    if (expectedStateVersion != null)
      result.expectedStateVersion = expectedStateVersion;
    return result;
  }

  RuntimeRunFailed._();

  factory RuntimeRunFailed.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeRunFailed.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeRunFailed',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'runId')
    ..aOS(2, _omitFieldNames ? '' : 'code')
    ..aOS(3, _omitFieldNames ? '' : 'message')
    ..aOB(4, _omitFieldNames ? '' : 'retryable')
    ..aE<FailureOrigin>(5, _omitFieldNames ? '' : 'failureOrigin',
        enumValues: FailureOrigin.values)
    ..aE<AutomaticRetryClass>(6, _omitFieldNames ? '' : 'automaticRetryClass',
        enumValues: AutomaticRetryClass.values)
    ..aInt64(7, _omitFieldNames ? '' : 'expectedStateVersion')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeRunFailed clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeRunFailed copyWith(void Function(RuntimeRunFailed) updates) =>
      super.copyWith((message) => updates(message as RuntimeRunFailed))
          as RuntimeRunFailed;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeRunFailed create() => RuntimeRunFailed._();
  @$core.override
  RuntimeRunFailed createEmptyInstance() => create();
  static $pb.PbList<RuntimeRunFailed> createRepeated() =>
      $pb.PbList<RuntimeRunFailed>();
  @$core.pragma('dart2js:noInline')
  static RuntimeRunFailed getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeRunFailed>(create);
  static RuntimeRunFailed? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get runId => $_getSZ(0);
  @$pb.TagNumber(1)
  set runId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRunId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRunId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get code => $_getSZ(1);
  @$pb.TagNumber(2)
  set code($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCode() => $_has(1);
  @$pb.TagNumber(2)
  void clearCode() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get message => $_getSZ(2);
  @$pb.TagNumber(3)
  set message($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasMessage() => $_has(2);
  @$pb.TagNumber(3)
  void clearMessage() => $_clearField(3);

  /// Superseded by automatic_retry_class and ignored by the failure normalizer.
  /// Retained at field 4 for wire compatibility with older workers.
  @$pb.TagNumber(4)
  $core.bool get retryable => $_getBF(3);
  @$pb.TagNumber(4)
  set retryable($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasRetryable() => $_has(3);
  @$pb.TagNumber(4)
  void clearRetryable() => $_clearField(4);

  /// Where the failure came from, used to normalize a public outcome reason
  /// instead of leaking worker-authored text to clients.
  @$pb.TagNumber(5)
  FailureOrigin get failureOrigin => $_getN(4);
  @$pb.TagNumber(5)
  set failureOrigin(FailureOrigin value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasFailureOrigin() => $_has(4);
  @$pb.TagNumber(5)
  void clearFailureOrigin() => $_clearField(5);

  /// Whether the orchestrator may retry inside this run. Internal policy only.
  @$pb.TagNumber(6)
  AutomaticRetryClass get automaticRetryClass => $_getN(5);
  @$pb.TagNumber(6)
  set automaticRetryClass(AutomaticRetryClass value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasAutomaticRetryClass() => $_has(5);
  @$pb.TagNumber(6)
  void clearAutomaticRetryClass() => $_clearField(6);

  /// The state version this report was computed against; the terminal
  /// transition commits only from that exact version.
  @$pb.TagNumber(7)
  $fixnum.Int64 get expectedStateVersion => $_getI64(6);
  @$pb.TagNumber(7)
  set expectedStateVersion($fixnum.Int64 value) => $_setInt64(6, value);
  @$pb.TagNumber(7)
  $core.bool hasExpectedStateVersion() => $_has(6);
  @$pb.TagNumber(7)
  void clearExpectedStateVersion() => $_clearField(7);
}

class RuntimeCancelledAck extends $pb.GeneratedMessage {
  factory RuntimeCancelledAck({
    $core.String? runId,
    $fixnum.Int64? observedStateVersion,
  }) {
    final result = create();
    if (runId != null) result.runId = runId;
    if (observedStateVersion != null)
      result.observedStateVersion = observedStateVersion;
    return result;
  }

  RuntimeCancelledAck._();

  factory RuntimeCancelledAck.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeCancelledAck.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeCancelledAck',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'runId')
    ..aInt64(2, _omitFieldNames ? '' : 'observedStateVersion')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeCancelledAck clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeCancelledAck copyWith(void Function(RuntimeCancelledAck) updates) =>
      super.copyWith((message) => updates(message as RuntimeCancelledAck))
          as RuntimeCancelledAck;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeCancelledAck create() => RuntimeCancelledAck._();
  @$core.override
  RuntimeCancelledAck createEmptyInstance() => create();
  static $pb.PbList<RuntimeCancelledAck> createRepeated() =>
      $pb.PbList<RuntimeCancelledAck>();
  @$core.pragma('dart2js:noInline')
  static RuntimeCancelledAck getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeCancelledAck>(create);
  static RuntimeCancelledAck? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get runId => $_getSZ(0);
  @$pb.TagNumber(1)
  set runId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRunId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRunId() => $_clearField(1);

  /// The version the worker had actually observed when it acknowledged, which
  /// lets the orchestrator tell a current acknowledgement from a stale one.
  @$pb.TagNumber(2)
  $fixnum.Int64 get observedStateVersion => $_getI64(1);
  @$pb.TagNumber(2)
  set observedStateVersion($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasObservedStateVersion() => $_has(1);
  @$pb.TagNumber(2)
  void clearObservedStateVersion() => $_clearField(2);
}

/// Sent only after the worker accepted an approval decision and restored the
/// matching owned attempt to a ready-but-paused boundary. Run, approval, worker,
/// assignment attempt, and expected version together are the fencing identity:
/// a repeat of the exact same identity on the same live stream replays the same
/// acceptance, and anything else is fenced.
class RuntimeApprovalResumeReady extends $pb.GeneratedMessage {
  factory RuntimeApprovalResumeReady({
    $core.String? runId,
    $core.String? approvalId,
    $fixnum.Int64? expectedStateVersion,
    $core.String? assignmentAttemptId,
  }) {
    final result = create();
    if (runId != null) result.runId = runId;
    if (approvalId != null) result.approvalId = approvalId;
    if (expectedStateVersion != null)
      result.expectedStateVersion = expectedStateVersion;
    if (assignmentAttemptId != null)
      result.assignmentAttemptId = assignmentAttemptId;
    return result;
  }

  RuntimeApprovalResumeReady._();

  factory RuntimeApprovalResumeReady.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeApprovalResumeReady.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeApprovalResumeReady',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'runId')
    ..aOS(2, _omitFieldNames ? '' : 'approvalId')
    ..aInt64(3, _omitFieldNames ? '' : 'expectedStateVersion')
    ..aOS(4, _omitFieldNames ? '' : 'assignmentAttemptId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeApprovalResumeReady clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeApprovalResumeReady copyWith(
          void Function(RuntimeApprovalResumeReady) updates) =>
      super.copyWith(
              (message) => updates(message as RuntimeApprovalResumeReady))
          as RuntimeApprovalResumeReady;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeApprovalResumeReady create() => RuntimeApprovalResumeReady._();
  @$core.override
  RuntimeApprovalResumeReady createEmptyInstance() => create();
  static $pb.PbList<RuntimeApprovalResumeReady> createRepeated() =>
      $pb.PbList<RuntimeApprovalResumeReady>();
  @$core.pragma('dart2js:noInline')
  static RuntimeApprovalResumeReady getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeApprovalResumeReady>(create);
  static RuntimeApprovalResumeReady? _defaultInstance;

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

  /// The pre-transition version the worker expects; waiting-approval commits to
  /// running at expected_state_version + 1.
  @$pb.TagNumber(3)
  $fixnum.Int64 get expectedStateVersion => $_getI64(2);
  @$pb.TagNumber(3)
  set expectedStateVersion($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasExpectedStateVersion() => $_has(2);
  @$pb.TagNumber(3)
  void clearExpectedStateVersion() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get assignmentAttemptId => $_getSZ(3);
  @$pb.TagNumber(4)
  set assignmentAttemptId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasAssignmentAttemptId() => $_has(3);
  @$pb.TagNumber(4)
  void clearAssignmentAttemptId() => $_clearField(4);
}

enum RuntimeUpdate_Update {
  workerReady,
  heartbeat,
  event,
  toolBeacon,
  runCompleted,
  runFailed,
  runCancelledAck,
  workerCapabilitiesUpdated,
  approvalResumeReady,
  notSet
}

class RuntimeUpdate extends $pb.GeneratedMessage {
  factory RuntimeUpdate({
    RuntimeWorkerReady? workerReady,
    RuntimeHeartbeat? heartbeat,
    $3.TuringEvent? event,
    $4.ToolCallBeacon? toolBeacon,
    RuntimeRunCompleted? runCompleted,
    RuntimeRunFailed? runFailed,
    RuntimeCancelledAck? runCancelledAck,
    RuntimeWorkerCapabilitiesUpdated? workerCapabilitiesUpdated,
    RuntimeApprovalResumeReady? approvalResumeReady,
  }) {
    final result = create();
    if (workerReady != null) result.workerReady = workerReady;
    if (heartbeat != null) result.heartbeat = heartbeat;
    if (event != null) result.event = event;
    if (toolBeacon != null) result.toolBeacon = toolBeacon;
    if (runCompleted != null) result.runCompleted = runCompleted;
    if (runFailed != null) result.runFailed = runFailed;
    if (runCancelledAck != null) result.runCancelledAck = runCancelledAck;
    if (workerCapabilitiesUpdated != null)
      result.workerCapabilitiesUpdated = workerCapabilitiesUpdated;
    if (approvalResumeReady != null)
      result.approvalResumeReady = approvalResumeReady;
    return result;
  }

  RuntimeUpdate._();

  factory RuntimeUpdate.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeUpdate.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static const $core.Map<$core.int, RuntimeUpdate_Update>
      _RuntimeUpdate_UpdateByTag = {
    1: RuntimeUpdate_Update.workerReady,
    2: RuntimeUpdate_Update.heartbeat,
    3: RuntimeUpdate_Update.event,
    4: RuntimeUpdate_Update.toolBeacon,
    5: RuntimeUpdate_Update.runCompleted,
    6: RuntimeUpdate_Update.runFailed,
    7: RuntimeUpdate_Update.runCancelledAck,
    8: RuntimeUpdate_Update.workerCapabilitiesUpdated,
    9: RuntimeUpdate_Update.approvalResumeReady,
    0: RuntimeUpdate_Update.notSet
  };
  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeUpdate',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..oo(0, [1, 2, 3, 4, 5, 6, 7, 8, 9])
    ..aOM<RuntimeWorkerReady>(1, _omitFieldNames ? '' : 'workerReady',
        subBuilder: RuntimeWorkerReady.create)
    ..aOM<RuntimeHeartbeat>(2, _omitFieldNames ? '' : 'heartbeat',
        subBuilder: RuntimeHeartbeat.create)
    ..aOM<$3.TuringEvent>(3, _omitFieldNames ? '' : 'event',
        subBuilder: $3.TuringEvent.create)
    ..aOM<$4.ToolCallBeacon>(4, _omitFieldNames ? '' : 'toolBeacon',
        subBuilder: $4.ToolCallBeacon.create)
    ..aOM<RuntimeRunCompleted>(5, _omitFieldNames ? '' : 'runCompleted',
        subBuilder: RuntimeRunCompleted.create)
    ..aOM<RuntimeRunFailed>(6, _omitFieldNames ? '' : 'runFailed',
        subBuilder: RuntimeRunFailed.create)
    ..aOM<RuntimeCancelledAck>(7, _omitFieldNames ? '' : 'runCancelledAck',
        subBuilder: RuntimeCancelledAck.create)
    ..aOM<RuntimeWorkerCapabilitiesUpdated>(
        8, _omitFieldNames ? '' : 'workerCapabilitiesUpdated',
        subBuilder: RuntimeWorkerCapabilitiesUpdated.create)
    ..aOM<RuntimeApprovalResumeReady>(
        9, _omitFieldNames ? '' : 'approvalResumeReady',
        subBuilder: RuntimeApprovalResumeReady.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeUpdate clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeUpdate copyWith(void Function(RuntimeUpdate) updates) =>
      super.copyWith((message) => updates(message as RuntimeUpdate))
          as RuntimeUpdate;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeUpdate create() => RuntimeUpdate._();
  @$core.override
  RuntimeUpdate createEmptyInstance() => create();
  static $pb.PbList<RuntimeUpdate> createRepeated() =>
      $pb.PbList<RuntimeUpdate>();
  @$core.pragma('dart2js:noInline')
  static RuntimeUpdate getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeUpdate>(create);
  static RuntimeUpdate? _defaultInstance;

  @$pb.TagNumber(1)
  @$pb.TagNumber(2)
  @$pb.TagNumber(3)
  @$pb.TagNumber(4)
  @$pb.TagNumber(5)
  @$pb.TagNumber(6)
  @$pb.TagNumber(7)
  @$pb.TagNumber(8)
  @$pb.TagNumber(9)
  RuntimeUpdate_Update whichUpdate() =>
      _RuntimeUpdate_UpdateByTag[$_whichOneof(0)]!;
  @$pb.TagNumber(1)
  @$pb.TagNumber(2)
  @$pb.TagNumber(3)
  @$pb.TagNumber(4)
  @$pb.TagNumber(5)
  @$pb.TagNumber(6)
  @$pb.TagNumber(7)
  @$pb.TagNumber(8)
  @$pb.TagNumber(9)
  void clearUpdate() => $_clearField($_whichOneof(0));

  @$pb.TagNumber(1)
  RuntimeWorkerReady get workerReady => $_getN(0);
  @$pb.TagNumber(1)
  set workerReady(RuntimeWorkerReady value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasWorkerReady() => $_has(0);
  @$pb.TagNumber(1)
  void clearWorkerReady() => $_clearField(1);
  @$pb.TagNumber(1)
  RuntimeWorkerReady ensureWorkerReady() => $_ensure(0);

  @$pb.TagNumber(2)
  RuntimeHeartbeat get heartbeat => $_getN(1);
  @$pb.TagNumber(2)
  set heartbeat(RuntimeHeartbeat value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasHeartbeat() => $_has(1);
  @$pb.TagNumber(2)
  void clearHeartbeat() => $_clearField(2);
  @$pb.TagNumber(2)
  RuntimeHeartbeat ensureHeartbeat() => $_ensure(1);

  @$pb.TagNumber(3)
  $3.TuringEvent get event => $_getN(2);
  @$pb.TagNumber(3)
  set event($3.TuringEvent value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasEvent() => $_has(2);
  @$pb.TagNumber(3)
  void clearEvent() => $_clearField(3);
  @$pb.TagNumber(3)
  $3.TuringEvent ensureEvent() => $_ensure(2);

  @$pb.TagNumber(4)
  $4.ToolCallBeacon get toolBeacon => $_getN(3);
  @$pb.TagNumber(4)
  set toolBeacon($4.ToolCallBeacon value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasToolBeacon() => $_has(3);
  @$pb.TagNumber(4)
  void clearToolBeacon() => $_clearField(4);
  @$pb.TagNumber(4)
  $4.ToolCallBeacon ensureToolBeacon() => $_ensure(3);

  @$pb.TagNumber(5)
  RuntimeRunCompleted get runCompleted => $_getN(4);
  @$pb.TagNumber(5)
  set runCompleted(RuntimeRunCompleted value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasRunCompleted() => $_has(4);
  @$pb.TagNumber(5)
  void clearRunCompleted() => $_clearField(5);
  @$pb.TagNumber(5)
  RuntimeRunCompleted ensureRunCompleted() => $_ensure(4);

  @$pb.TagNumber(6)
  RuntimeRunFailed get runFailed => $_getN(5);
  @$pb.TagNumber(6)
  set runFailed(RuntimeRunFailed value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasRunFailed() => $_has(5);
  @$pb.TagNumber(6)
  void clearRunFailed() => $_clearField(6);
  @$pb.TagNumber(6)
  RuntimeRunFailed ensureRunFailed() => $_ensure(5);

  @$pb.TagNumber(7)
  RuntimeCancelledAck get runCancelledAck => $_getN(6);
  @$pb.TagNumber(7)
  set runCancelledAck(RuntimeCancelledAck value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasRunCancelledAck() => $_has(6);
  @$pb.TagNumber(7)
  void clearRunCancelledAck() => $_clearField(7);
  @$pb.TagNumber(7)
  RuntimeCancelledAck ensureRunCancelledAck() => $_ensure(6);

  @$pb.TagNumber(8)
  RuntimeWorkerCapabilitiesUpdated get workerCapabilitiesUpdated => $_getN(7);
  @$pb.TagNumber(8)
  set workerCapabilitiesUpdated(RuntimeWorkerCapabilitiesUpdated value) =>
      $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasWorkerCapabilitiesUpdated() => $_has(7);
  @$pb.TagNumber(8)
  void clearWorkerCapabilitiesUpdated() => $_clearField(8);
  @$pb.TagNumber(8)
  RuntimeWorkerCapabilitiesUpdated ensureWorkerCapabilitiesUpdated() =>
      $_ensure(7);

  @$pb.TagNumber(9)
  RuntimeApprovalResumeReady get approvalResumeReady => $_getN(8);
  @$pb.TagNumber(9)
  set approvalResumeReady(RuntimeApprovalResumeReady value) =>
      $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasApprovalResumeReady() => $_has(8);
  @$pb.TagNumber(9)
  void clearApprovalResumeReady() => $_clearField(9);
  @$pb.TagNumber(9)
  RuntimeApprovalResumeReady ensureApprovalResumeReady() => $_ensure(8);
}

class RuntimeWorkerAccepted extends $pb.GeneratedMessage {
  factory RuntimeWorkerAccepted({
    $core.String? workerId,
    $core.String? registrationId,
  }) {
    final result = create();
    if (workerId != null) result.workerId = workerId;
    if (registrationId != null) result.registrationId = registrationId;
    return result;
  }

  RuntimeWorkerAccepted._();

  factory RuntimeWorkerAccepted.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeWorkerAccepted.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeWorkerAccepted',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'workerId')
    ..aOS(2, _omitFieldNames ? '' : 'registrationId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeWorkerAccepted clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeWorkerAccepted copyWith(
          void Function(RuntimeWorkerAccepted) updates) =>
      super.copyWith((message) => updates(message as RuntimeWorkerAccepted))
          as RuntimeWorkerAccepted;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeWorkerAccepted create() => RuntimeWorkerAccepted._();
  @$core.override
  RuntimeWorkerAccepted createEmptyInstance() => create();
  static $pb.PbList<RuntimeWorkerAccepted> createRepeated() =>
      $pb.PbList<RuntimeWorkerAccepted>();
  @$core.pragma('dart2js:noInline')
  static RuntimeWorkerAccepted getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeWorkerAccepted>(create);
  static RuntimeWorkerAccepted? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get workerId => $_getSZ(0);
  @$pb.TagNumber(1)
  set workerId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasWorkerId() => $_has(0);
  @$pb.TagNumber(1)
  void clearWorkerId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get registrationId => $_getSZ(1);
  @$pb.TagNumber(2)
  set registrationId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRegistrationId() => $_has(1);
  @$pb.TagNumber(2)
  void clearRegistrationId() => $_clearField(2);
}

class RuntimeRunCancelled extends $pb.GeneratedMessage {
  factory RuntimeRunCancelled({
    $core.String? runId,
    $core.String? reason,
    $fixnum.Int64? stateVersion,
  }) {
    final result = create();
    if (runId != null) result.runId = runId;
    if (reason != null) result.reason = reason;
    if (stateVersion != null) result.stateVersion = stateVersion;
    return result;
  }

  RuntimeRunCancelled._();

  factory RuntimeRunCancelled.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeRunCancelled.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeRunCancelled',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'runId')
    ..aOS(2, _omitFieldNames ? '' : 'reason')
    ..aInt64(3, _omitFieldNames ? '' : 'stateVersion')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeRunCancelled clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeRunCancelled copyWith(void Function(RuntimeRunCancelled) updates) =>
      super.copyWith((message) => updates(message as RuntimeRunCancelled))
          as RuntimeRunCancelled;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeRunCancelled create() => RuntimeRunCancelled._();
  @$core.override
  RuntimeRunCancelled createEmptyInstance() => create();
  static $pb.PbList<RuntimeRunCancelled> createRepeated() =>
      $pb.PbList<RuntimeRunCancelled>();
  @$core.pragma('dart2js:noInline')
  static RuntimeRunCancelled getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeRunCancelled>(create);
  static RuntimeRunCancelled? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get runId => $_getSZ(0);
  @$pb.TagNumber(1)
  set runId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRunId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRunId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get reason => $_getSZ(1);
  @$pb.TagNumber(2)
  set reason($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasReason() => $_has(1);
  @$pb.TagNumber(2)
  void clearReason() => $_clearField(2);

  /// The version this cancellation committed at, so the worker never rolls its
  /// view back to an older state.
  @$pb.TagNumber(3)
  $fixnum.Int64 get stateVersion => $_getI64(2);
  @$pb.TagNumber(3)
  set stateVersion($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasStateVersion() => $_has(2);
  @$pb.TagNumber(3)
  void clearStateVersion() => $_clearField(3);
}

class RuntimeApprovalUpdated extends $pb.GeneratedMessage {
  factory RuntimeApprovalUpdated({
    $core.String? approvalId,
    $core.String? approvalToken,
    $core.String? status,
    $fixnum.Int64? stateVersion,
  }) {
    final result = create();
    if (approvalId != null) result.approvalId = approvalId;
    if (approvalToken != null) result.approvalToken = approvalToken;
    if (status != null) result.status = status;
    if (stateVersion != null) result.stateVersion = stateVersion;
    return result;
  }

  RuntimeApprovalUpdated._();

  factory RuntimeApprovalUpdated.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeApprovalUpdated.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeApprovalUpdated',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'approvalId')
    ..aOS(2, _omitFieldNames ? '' : 'approvalToken')
    ..aOS(3, _omitFieldNames ? '' : 'status')
    ..aInt64(4, _omitFieldNames ? '' : 'stateVersion')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeApprovalUpdated clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeApprovalUpdated copyWith(
          void Function(RuntimeApprovalUpdated) updates) =>
      super.copyWith((message) => updates(message as RuntimeApprovalUpdated))
          as RuntimeApprovalUpdated;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeApprovalUpdated create() => RuntimeApprovalUpdated._();
  @$core.override
  RuntimeApprovalUpdated createEmptyInstance() => create();
  static $pb.PbList<RuntimeApprovalUpdated> createRepeated() =>
      $pb.PbList<RuntimeApprovalUpdated>();
  @$core.pragma('dart2js:noInline')
  static RuntimeApprovalUpdated getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeApprovalUpdated>(create);
  static RuntimeApprovalUpdated? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get approvalId => $_getSZ(0);
  @$pb.TagNumber(1)
  set approvalId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasApprovalId() => $_has(0);
  @$pb.TagNumber(1)
  void clearApprovalId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get approvalToken => $_getSZ(1);
  @$pb.TagNumber(2)
  set approvalToken($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasApprovalToken() => $_has(1);
  @$pb.TagNumber(2)
  void clearApprovalToken() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get status => $_getSZ(2);
  @$pb.TagNumber(3)
  set status($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasStatus() => $_has(2);
  @$pb.TagNumber(3)
  void clearStatus() => $_clearField(3);

  /// The run version at the time of this decision. Delivering a decision does
  /// not by itself resume the run; only a matching Ready/Accepted exchange does.
  @$pb.TagNumber(4)
  $fixnum.Int64 get stateVersion => $_getI64(3);
  @$pb.TagNumber(4)
  set stateVersion($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasStateVersion() => $_has(3);
  @$pb.TagNumber(4)
  void clearStateVersion() => $_clearField(4);
}

/// The orchestrator's durable acceptance of a resume: waiting-approval has
/// committed to running at state_version. It names the commit, not proof that
/// the worker received it, and the worker must not execute the approved tool or
/// continue model work until it arrives. If delivery fails after the commit, the
/// run is fenced to recovering rather than reverted.
class RuntimeApprovalResumeAccepted extends $pb.GeneratedMessage {
  factory RuntimeApprovalResumeAccepted({
    $core.String? runId,
    $core.String? approvalId,
    $fixnum.Int64? stateVersion,
    $core.String? assignmentAttemptId,
  }) {
    final result = create();
    if (runId != null) result.runId = runId;
    if (approvalId != null) result.approvalId = approvalId;
    if (stateVersion != null) result.stateVersion = stateVersion;
    if (assignmentAttemptId != null)
      result.assignmentAttemptId = assignmentAttemptId;
    return result;
  }

  RuntimeApprovalResumeAccepted._();

  factory RuntimeApprovalResumeAccepted.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeApprovalResumeAccepted.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeApprovalResumeAccepted',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'runId')
    ..aOS(2, _omitFieldNames ? '' : 'approvalId')
    ..aInt64(3, _omitFieldNames ? '' : 'stateVersion')
    ..aOS(4, _omitFieldNames ? '' : 'assignmentAttemptId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeApprovalResumeAccepted clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeApprovalResumeAccepted copyWith(
          void Function(RuntimeApprovalResumeAccepted) updates) =>
      super.copyWith(
              (message) => updates(message as RuntimeApprovalResumeAccepted))
          as RuntimeApprovalResumeAccepted;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeApprovalResumeAccepted create() =>
      RuntimeApprovalResumeAccepted._();
  @$core.override
  RuntimeApprovalResumeAccepted createEmptyInstance() => create();
  static $pb.PbList<RuntimeApprovalResumeAccepted> createRepeated() =>
      $pb.PbList<RuntimeApprovalResumeAccepted>();
  @$core.pragma('dart2js:noInline')
  static RuntimeApprovalResumeAccepted getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeApprovalResumeAccepted>(create);
  static RuntimeApprovalResumeAccepted? _defaultInstance;

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
  $fixnum.Int64 get stateVersion => $_getI64(2);
  @$pb.TagNumber(3)
  set stateVersion($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasStateVersion() => $_has(2);
  @$pb.TagNumber(3)
  void clearStateVersion() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get assignmentAttemptId => $_getSZ(3);
  @$pb.TagNumber(4)
  set assignmentAttemptId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasAssignmentAttemptId() => $_has(3);
  @$pb.TagNumber(4)
  void clearAssignmentAttemptId() => $_clearField(4);
}

class RuntimeShutdownRequested extends $pb.GeneratedMessage {
  factory RuntimeShutdownRequested({
    $core.String? reason,
  }) {
    final result = create();
    if (reason != null) result.reason = reason;
    return result;
  }

  RuntimeShutdownRequested._();

  factory RuntimeShutdownRequested.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeShutdownRequested.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeShutdownRequested',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'reason')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeShutdownRequested clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeShutdownRequested copyWith(
          void Function(RuntimeShutdownRequested) updates) =>
      super.copyWith((message) => updates(message as RuntimeShutdownRequested))
          as RuntimeShutdownRequested;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeShutdownRequested create() => RuntimeShutdownRequested._();
  @$core.override
  RuntimeShutdownRequested createEmptyInstance() => create();
  static $pb.PbList<RuntimeShutdownRequested> createRepeated() =>
      $pb.PbList<RuntimeShutdownRequested>();
  @$core.pragma('dart2js:noInline')
  static RuntimeShutdownRequested getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeShutdownRequested>(create);
  static RuntimeShutdownRequested? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get reason => $_getSZ(0);
  @$pb.TagNumber(1)
  set reason($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasReason() => $_has(0);
  @$pb.TagNumber(1)
  void clearReason() => $_clearField(1);
}

class RuntimeMcpRegistryChanged extends $pb.GeneratedMessage {
  factory RuntimeMcpRegistryChanged({
    $core.String? registrationId,
  }) {
    final result = create();
    if (registrationId != null) result.registrationId = registrationId;
    return result;
  }

  RuntimeMcpRegistryChanged._();

  factory RuntimeMcpRegistryChanged.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeMcpRegistryChanged.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeMcpRegistryChanged',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'registrationId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeMcpRegistryChanged clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeMcpRegistryChanged copyWith(
          void Function(RuntimeMcpRegistryChanged) updates) =>
      super.copyWith((message) => updates(message as RuntimeMcpRegistryChanged))
          as RuntimeMcpRegistryChanged;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeMcpRegistryChanged create() => RuntimeMcpRegistryChanged._();
  @$core.override
  RuntimeMcpRegistryChanged createEmptyInstance() => create();
  static $pb.PbList<RuntimeMcpRegistryChanged> createRepeated() =>
      $pb.PbList<RuntimeMcpRegistryChanged>();
  @$core.pragma('dart2js:noInline')
  static RuntimeMcpRegistryChanged getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeMcpRegistryChanged>(create);
  static RuntimeMcpRegistryChanged? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get registrationId => $_getSZ(0);
  @$pb.TagNumber(1)
  set registrationId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRegistrationId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRegistrationId() => $_clearField(1);
}

enum RuntimeCommand_Command {
  workerAccepted,
  runAssigned,
  runCancelled,
  approvalUpdated,
  shutdownRequested,
  toolPolicyDecision,
  mcpRegistryChanged,
  approvalResumeAccepted,
  notSet
}

class RuntimeCommand extends $pb.GeneratedMessage {
  factory RuntimeCommand({
    RuntimeWorkerAccepted? workerAccepted,
    AgentJob? runAssigned,
    RuntimeRunCancelled? runCancelled,
    RuntimeApprovalUpdated? approvalUpdated,
    RuntimeShutdownRequested? shutdownRequested,
    $4.ToolPolicyDecision? toolPolicyDecision,
    RuntimeMcpRegistryChanged? mcpRegistryChanged,
    RuntimeApprovalResumeAccepted? approvalResumeAccepted,
  }) {
    final result = create();
    if (workerAccepted != null) result.workerAccepted = workerAccepted;
    if (runAssigned != null) result.runAssigned = runAssigned;
    if (runCancelled != null) result.runCancelled = runCancelled;
    if (approvalUpdated != null) result.approvalUpdated = approvalUpdated;
    if (shutdownRequested != null) result.shutdownRequested = shutdownRequested;
    if (toolPolicyDecision != null)
      result.toolPolicyDecision = toolPolicyDecision;
    if (mcpRegistryChanged != null)
      result.mcpRegistryChanged = mcpRegistryChanged;
    if (approvalResumeAccepted != null)
      result.approvalResumeAccepted = approvalResumeAccepted;
    return result;
  }

  RuntimeCommand._();

  factory RuntimeCommand.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeCommand.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static const $core.Map<$core.int, RuntimeCommand_Command>
      _RuntimeCommand_CommandByTag = {
    1: RuntimeCommand_Command.workerAccepted,
    2: RuntimeCommand_Command.runAssigned,
    3: RuntimeCommand_Command.runCancelled,
    4: RuntimeCommand_Command.approvalUpdated,
    5: RuntimeCommand_Command.shutdownRequested,
    6: RuntimeCommand_Command.toolPolicyDecision,
    7: RuntimeCommand_Command.mcpRegistryChanged,
    8: RuntimeCommand_Command.approvalResumeAccepted,
    0: RuntimeCommand_Command.notSet
  };
  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeCommand',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..oo(0, [1, 2, 3, 4, 5, 6, 7, 8])
    ..aOM<RuntimeWorkerAccepted>(1, _omitFieldNames ? '' : 'workerAccepted',
        subBuilder: RuntimeWorkerAccepted.create)
    ..aOM<AgentJob>(2, _omitFieldNames ? '' : 'runAssigned',
        subBuilder: AgentJob.create)
    ..aOM<RuntimeRunCancelled>(3, _omitFieldNames ? '' : 'runCancelled',
        subBuilder: RuntimeRunCancelled.create)
    ..aOM<RuntimeApprovalUpdated>(4, _omitFieldNames ? '' : 'approvalUpdated',
        subBuilder: RuntimeApprovalUpdated.create)
    ..aOM<RuntimeShutdownRequested>(
        5, _omitFieldNames ? '' : 'shutdownRequested',
        subBuilder: RuntimeShutdownRequested.create)
    ..aOM<$4.ToolPolicyDecision>(6, _omitFieldNames ? '' : 'toolPolicyDecision',
        subBuilder: $4.ToolPolicyDecision.create)
    ..aOM<RuntimeMcpRegistryChanged>(
        7, _omitFieldNames ? '' : 'mcpRegistryChanged',
        subBuilder: RuntimeMcpRegistryChanged.create)
    ..aOM<RuntimeApprovalResumeAccepted>(
        8, _omitFieldNames ? '' : 'approvalResumeAccepted',
        subBuilder: RuntimeApprovalResumeAccepted.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeCommand clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeCommand copyWith(void Function(RuntimeCommand) updates) =>
      super.copyWith((message) => updates(message as RuntimeCommand))
          as RuntimeCommand;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeCommand create() => RuntimeCommand._();
  @$core.override
  RuntimeCommand createEmptyInstance() => create();
  static $pb.PbList<RuntimeCommand> createRepeated() =>
      $pb.PbList<RuntimeCommand>();
  @$core.pragma('dart2js:noInline')
  static RuntimeCommand getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeCommand>(create);
  static RuntimeCommand? _defaultInstance;

  @$pb.TagNumber(1)
  @$pb.TagNumber(2)
  @$pb.TagNumber(3)
  @$pb.TagNumber(4)
  @$pb.TagNumber(5)
  @$pb.TagNumber(6)
  @$pb.TagNumber(7)
  @$pb.TagNumber(8)
  RuntimeCommand_Command whichCommand() =>
      _RuntimeCommand_CommandByTag[$_whichOneof(0)]!;
  @$pb.TagNumber(1)
  @$pb.TagNumber(2)
  @$pb.TagNumber(3)
  @$pb.TagNumber(4)
  @$pb.TagNumber(5)
  @$pb.TagNumber(6)
  @$pb.TagNumber(7)
  @$pb.TagNumber(8)
  void clearCommand() => $_clearField($_whichOneof(0));

  @$pb.TagNumber(1)
  RuntimeWorkerAccepted get workerAccepted => $_getN(0);
  @$pb.TagNumber(1)
  set workerAccepted(RuntimeWorkerAccepted value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasWorkerAccepted() => $_has(0);
  @$pb.TagNumber(1)
  void clearWorkerAccepted() => $_clearField(1);
  @$pb.TagNumber(1)
  RuntimeWorkerAccepted ensureWorkerAccepted() => $_ensure(0);

  @$pb.TagNumber(2)
  AgentJob get runAssigned => $_getN(1);
  @$pb.TagNumber(2)
  set runAssigned(AgentJob value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasRunAssigned() => $_has(1);
  @$pb.TagNumber(2)
  void clearRunAssigned() => $_clearField(2);
  @$pb.TagNumber(2)
  AgentJob ensureRunAssigned() => $_ensure(1);

  @$pb.TagNumber(3)
  RuntimeRunCancelled get runCancelled => $_getN(2);
  @$pb.TagNumber(3)
  set runCancelled(RuntimeRunCancelled value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasRunCancelled() => $_has(2);
  @$pb.TagNumber(3)
  void clearRunCancelled() => $_clearField(3);
  @$pb.TagNumber(3)
  RuntimeRunCancelled ensureRunCancelled() => $_ensure(2);

  @$pb.TagNumber(4)
  RuntimeApprovalUpdated get approvalUpdated => $_getN(3);
  @$pb.TagNumber(4)
  set approvalUpdated(RuntimeApprovalUpdated value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasApprovalUpdated() => $_has(3);
  @$pb.TagNumber(4)
  void clearApprovalUpdated() => $_clearField(4);
  @$pb.TagNumber(4)
  RuntimeApprovalUpdated ensureApprovalUpdated() => $_ensure(3);

  @$pb.TagNumber(5)
  RuntimeShutdownRequested get shutdownRequested => $_getN(4);
  @$pb.TagNumber(5)
  set shutdownRequested(RuntimeShutdownRequested value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasShutdownRequested() => $_has(4);
  @$pb.TagNumber(5)
  void clearShutdownRequested() => $_clearField(5);
  @$pb.TagNumber(5)
  RuntimeShutdownRequested ensureShutdownRequested() => $_ensure(4);

  @$pb.TagNumber(6)
  $4.ToolPolicyDecision get toolPolicyDecision => $_getN(5);
  @$pb.TagNumber(6)
  set toolPolicyDecision($4.ToolPolicyDecision value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasToolPolicyDecision() => $_has(5);
  @$pb.TagNumber(6)
  void clearToolPolicyDecision() => $_clearField(6);
  @$pb.TagNumber(6)
  $4.ToolPolicyDecision ensureToolPolicyDecision() => $_ensure(5);

  @$pb.TagNumber(7)
  RuntimeMcpRegistryChanged get mcpRegistryChanged => $_getN(6);
  @$pb.TagNumber(7)
  set mcpRegistryChanged(RuntimeMcpRegistryChanged value) =>
      $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasMcpRegistryChanged() => $_has(6);
  @$pb.TagNumber(7)
  void clearMcpRegistryChanged() => $_clearField(7);
  @$pb.TagNumber(7)
  RuntimeMcpRegistryChanged ensureMcpRegistryChanged() => $_ensure(6);

  @$pb.TagNumber(8)
  RuntimeApprovalResumeAccepted get approvalResumeAccepted => $_getN(7);
  @$pb.TagNumber(8)
  set approvalResumeAccepted(RuntimeApprovalResumeAccepted value) =>
      $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasApprovalResumeAccepted() => $_has(7);
  @$pb.TagNumber(8)
  void clearApprovalResumeAccepted() => $_clearField(8);
  @$pb.TagNumber(8)
  RuntimeApprovalResumeAccepted ensureApprovalResumeAccepted() => $_ensure(7);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
