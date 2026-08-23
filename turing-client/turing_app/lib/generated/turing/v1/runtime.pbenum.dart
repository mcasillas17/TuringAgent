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

import 'package:protobuf/protobuf.dart' as $pb;

/// Where a run failure actually came from, supplied by the reporting call site
/// so the orchestrator can normalize it into a public RunOutcomeReason without
/// classifying on provider-controlled message text. UNSPECIFIED means the field
/// was absent and UNKNOWN covers an origin a newer reporter introduced; both
/// fail closed to an internal-failure outcome. Internal to the runtime protocol,
/// never surfaced to clients.
class FailureOrigin extends $pb.ProtobufEnum {
  static const FailureOrigin FAILURE_ORIGIN_UNSPECIFIED =
      FailureOrigin._(0, _omitEnumNames ? '' : 'FAILURE_ORIGIN_UNSPECIFIED');
  static const FailureOrigin FAILURE_ORIGIN_UNKNOWN =
      FailureOrigin._(1, _omitEnumNames ? '' : 'FAILURE_ORIGIN_UNKNOWN');
  static const FailureOrigin FAILURE_ORIGIN_CONTEXT_ASSEMBLY = FailureOrigin._(
      2, _omitEnumNames ? '' : 'FAILURE_ORIGIN_CONTEXT_ASSEMBLY');
  static const FailureOrigin FAILURE_ORIGIN_EXTERNAL_PROVIDER = FailureOrigin._(
      3, _omitEnumNames ? '' : 'FAILURE_ORIGIN_EXTERNAL_PROVIDER');
  static const FailureOrigin FAILURE_ORIGIN_PROVIDER_CONFIGURATION =
      FailureOrigin._(
          4, _omitEnumNames ? '' : 'FAILURE_ORIGIN_PROVIDER_CONFIGURATION');
  static const FailureOrigin FAILURE_ORIGIN_PROVIDER_PROTOCOL = FailureOrigin._(
      5, _omitEnumNames ? '' : 'FAILURE_ORIGIN_PROVIDER_PROTOCOL');
  static const FailureOrigin FAILURE_ORIGIN_PROVIDER_TRANSPORT =
      FailureOrigin._(
          6, _omitEnumNames ? '' : 'FAILURE_ORIGIN_PROVIDER_TRANSPORT');
  static const FailureOrigin FAILURE_ORIGIN_PROVIDER_OUTPUT_GUARD =
      FailureOrigin._(
          7, _omitEnumNames ? '' : 'FAILURE_ORIGIN_PROVIDER_OUTPUT_GUARD');
  static const FailureOrigin FAILURE_ORIGIN_TOOL_INFRASTRUCTURE =
      FailureOrigin._(
          8, _omitEnumNames ? '' : 'FAILURE_ORIGIN_TOOL_INFRASTRUCTURE');
  static const FailureOrigin FAILURE_ORIGIN_TOOL_EXECUTION =
      FailureOrigin._(9, _omitEnumNames ? '' : 'FAILURE_ORIGIN_TOOL_EXECUTION');
  static const FailureOrigin FAILURE_ORIGIN_TOOL_GUARD =
      FailureOrigin._(10, _omitEnumNames ? '' : 'FAILURE_ORIGIN_TOOL_GUARD');
  static const FailureOrigin FAILURE_ORIGIN_TOOL_POLICY =
      FailureOrigin._(11, _omitEnumNames ? '' : 'FAILURE_ORIGIN_TOOL_POLICY');
  static const FailureOrigin FAILURE_ORIGIN_APPROVAL_TRANSPORT =
      FailureOrigin._(
          12, _omitEnumNames ? '' : 'FAILURE_ORIGIN_APPROVAL_TRANSPORT');
  static const FailureOrigin FAILURE_ORIGIN_APPROVAL_EXPIRY = FailureOrigin._(
      13, _omitEnumNames ? '' : 'FAILURE_ORIGIN_APPROVAL_EXPIRY');
  static const FailureOrigin FAILURE_ORIGIN_AUTOMATION_POLICY = FailureOrigin._(
      14, _omitEnumNames ? '' : 'FAILURE_ORIGIN_AUTOMATION_POLICY');
  static const FailureOrigin FAILURE_ORIGIN_WORKER_RUNTIME = FailureOrigin._(
      15, _omitEnumNames ? '' : 'FAILURE_ORIGIN_WORKER_RUNTIME');
  static const FailureOrigin FAILURE_ORIGIN_DISPATCH =
      FailureOrigin._(16, _omitEnumNames ? '' : 'FAILURE_ORIGIN_DISPATCH');
  static const FailureOrigin FAILURE_ORIGIN_RECOVERY =
      FailureOrigin._(17, _omitEnumNames ? '' : 'FAILURE_ORIGIN_RECOVERY');
  static const FailureOrigin FAILURE_ORIGIN_ORCHESTRATOR_INTERNAL =
      FailureOrigin._(
          18, _omitEnumNames ? '' : 'FAILURE_ORIGIN_ORCHESTRATOR_INTERNAL');
  static const FailureOrigin FAILURE_ORIGIN_CLIENT_LIFECYCLE = FailureOrigin._(
      19, _omitEnumNames ? '' : 'FAILURE_ORIGIN_CLIENT_LIFECYCLE');

  static const $core.List<FailureOrigin> values = <FailureOrigin>[
    FAILURE_ORIGIN_UNSPECIFIED,
    FAILURE_ORIGIN_UNKNOWN,
    FAILURE_ORIGIN_CONTEXT_ASSEMBLY,
    FAILURE_ORIGIN_EXTERNAL_PROVIDER,
    FAILURE_ORIGIN_PROVIDER_CONFIGURATION,
    FAILURE_ORIGIN_PROVIDER_PROTOCOL,
    FAILURE_ORIGIN_PROVIDER_TRANSPORT,
    FAILURE_ORIGIN_PROVIDER_OUTPUT_GUARD,
    FAILURE_ORIGIN_TOOL_INFRASTRUCTURE,
    FAILURE_ORIGIN_TOOL_EXECUTION,
    FAILURE_ORIGIN_TOOL_GUARD,
    FAILURE_ORIGIN_TOOL_POLICY,
    FAILURE_ORIGIN_APPROVAL_TRANSPORT,
    FAILURE_ORIGIN_APPROVAL_EXPIRY,
    FAILURE_ORIGIN_AUTOMATION_POLICY,
    FAILURE_ORIGIN_WORKER_RUNTIME,
    FAILURE_ORIGIN_DISPATCH,
    FAILURE_ORIGIN_RECOVERY,
    FAILURE_ORIGIN_ORCHESTRATOR_INTERNAL,
    FAILURE_ORIGIN_CLIENT_LIFECYCLE,
  ];

  static final $core.List<FailureOrigin?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 19);
  static FailureOrigin? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const FailureOrigin._(super.value, super.name);
}

/// Whether the orchestrator may retry this failure inside the same run. Internal
/// dispatch policy only: it is not a user-facing retry promise and never becomes
/// public outcome text. Unspecified and unknown are both treated as never, so an
/// unrecognized class can never widen automatic retrying.
class AutomaticRetryClass extends $pb.ProtobufEnum {
  static const AutomaticRetryClass AUTOMATIC_RETRY_CLASS_UNSPECIFIED =
      AutomaticRetryClass._(
          0, _omitEnumNames ? '' : 'AUTOMATIC_RETRY_CLASS_UNSPECIFIED');
  static const AutomaticRetryClass AUTOMATIC_RETRY_CLASS_UNKNOWN =
      AutomaticRetryClass._(
          1, _omitEnumNames ? '' : 'AUTOMATIC_RETRY_CLASS_UNKNOWN');
  static const AutomaticRetryClass AUTOMATIC_RETRY_CLASS_NEVER =
      AutomaticRetryClass._(
          2, _omitEnumNames ? '' : 'AUTOMATIC_RETRY_CLASS_NEVER');
  static const AutomaticRetryClass AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT =
      AutomaticRetryClass._(
          3, _omitEnumNames ? '' : 'AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT');

  static const $core.List<AutomaticRetryClass> values = <AutomaticRetryClass>[
    AUTOMATIC_RETRY_CLASS_UNSPECIFIED,
    AUTOMATIC_RETRY_CLASS_UNKNOWN,
    AUTOMATIC_RETRY_CLASS_NEVER,
    AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
  ];

  static final $core.List<AutomaticRetryClass?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static AutomaticRetryClass? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AutomaticRetryClass._(super.value, super.name);
}

class ToolDiscoveryStatus extends $pb.ProtobufEnum {
  /// Legacy runtime that cannot report an authoritative capability snapshot.
  static const ToolDiscoveryStatus TOOL_DISCOVERY_STATUS_UNSPECIFIED =
      ToolDiscoveryStatus._(
          0, _omitEnumNames ? '' : 'TOOL_DISCOVERY_STATUS_UNSPECIFIED');

  /// Discovery succeeded; tools is authoritative, including when it is empty.
  static const ToolDiscoveryStatus TOOL_DISCOVERY_STATUS_COMPLETE =
      ToolDiscoveryStatus._(
          1, _omitEnumNames ? '' : 'TOOL_DISCOVERY_STATUS_COMPLETE');

  /// Discovery was attempted but failed. The orchestrator rejects the worker.
  static const ToolDiscoveryStatus TOOL_DISCOVERY_STATUS_FAILED =
      ToolDiscoveryStatus._(
          2, _omitEnumNames ? '' : 'TOOL_DISCOVERY_STATUS_FAILED');

  static const $core.List<ToolDiscoveryStatus> values = <ToolDiscoveryStatus>[
    TOOL_DISCOVERY_STATUS_UNSPECIFIED,
    TOOL_DISCOVERY_STATUS_COMPLETE,
    TOOL_DISCOVERY_STATUS_FAILED,
  ];

  static final $core.List<ToolDiscoveryStatus?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static ToolDiscoveryStatus? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ToolDiscoveryStatus._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
