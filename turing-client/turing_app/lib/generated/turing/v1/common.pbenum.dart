// This is a generated file - do not edit.
//
// Generated from turing/v1/common.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

class AgentId extends $pb.ProtobufEnum {
  static const AgentId AGENT_ID_UNSPECIFIED =
      AgentId._(0, _omitEnumNames ? '' : 'AGENT_ID_UNSPECIFIED');
  static const AgentId AGENT_ID_GENERAL_ASSISTANT =
      AgentId._(1, _omitEnumNames ? '' : 'AGENT_ID_GENERAL_ASSISTANT');

  static const $core.List<AgentId> values = <AgentId>[
    AGENT_ID_UNSPECIFIED,
    AGENT_ID_GENERAL_ASSISTANT,
  ];

  static final $core.List<AgentId?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 1);
  static AgentId? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AgentId._(super.value, super.name);
}

class ModelProvider extends $pb.ProtobufEnum {
  static const ModelProvider MODEL_PROVIDER_UNSPECIFIED =
      ModelProvider._(0, _omitEnumNames ? '' : 'MODEL_PROVIDER_UNSPECIFIED');
  static const ModelProvider MODEL_PROVIDER_OLLAMA =
      ModelProvider._(1, _omitEnumNames ? '' : 'MODEL_PROVIDER_OLLAMA');
  static const ModelProvider MODEL_PROVIDER_OPENAI_COMPATIBLE = ModelProvider._(
      2, _omitEnumNames ? '' : 'MODEL_PROVIDER_OPENAI_COMPATIBLE');

  static const $core.List<ModelProvider> values = <ModelProvider>[
    MODEL_PROVIDER_UNSPECIFIED,
    MODEL_PROVIDER_OLLAMA,
    MODEL_PROVIDER_OPENAI_COMPATIBLE,
  ];

  static final $core.List<ModelProvider?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static ModelProvider? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ModelProvider._(super.value, super.name);
}

class RoutingRequirementKind extends $pb.ProtobufEnum {
  static const RoutingRequirementKind ROUTING_REQUIREMENT_KIND_UNSPECIFIED =
      RoutingRequirementKind._(
          0, _omitEnumNames ? '' : 'ROUTING_REQUIREMENT_KIND_UNSPECIFIED');
  static const RoutingRequirementKind ROUTING_REQUIREMENT_KIND_PROVIDER =
      RoutingRequirementKind._(
          1, _omitEnumNames ? '' : 'ROUTING_REQUIREMENT_KIND_PROVIDER');
  static const RoutingRequirementKind ROUTING_REQUIREMENT_KIND_MODEL =
      RoutingRequirementKind._(
          2, _omitEnumNames ? '' : 'ROUTING_REQUIREMENT_KIND_MODEL');
  static const RoutingRequirementKind ROUTING_REQUIREMENT_KIND_CONTEXT =
      RoutingRequirementKind._(
          3, _omitEnumNames ? '' : 'ROUTING_REQUIREMENT_KIND_CONTEXT');
  static const RoutingRequirementKind ROUTING_REQUIREMENT_KIND_TOOL =
      RoutingRequirementKind._(
          4, _omitEnumNames ? '' : 'ROUTING_REQUIREMENT_KIND_TOOL');
  static const RoutingRequirementKind ROUTING_REQUIREMENT_KIND_AGENT =
      RoutingRequirementKind._(
          5, _omitEnumNames ? '' : 'ROUTING_REQUIREMENT_KIND_AGENT');
  static const RoutingRequirementKind ROUTING_REQUIREMENT_KIND_CAPACITY =
      RoutingRequirementKind._(
          6, _omitEnumNames ? '' : 'ROUTING_REQUIREMENT_KIND_CAPACITY');
  static const RoutingRequirementKind
      ROUTING_REQUIREMENT_KIND_EXTERNAL_AGENT_CREDENTIAL =
      RoutingRequirementKind._(
          7,
          _omitEnumNames
              ? ''
              : 'ROUTING_REQUIREMENT_KIND_EXTERNAL_AGENT_CREDENTIAL');

  static const $core.List<RoutingRequirementKind> values =
      <RoutingRequirementKind>[
    ROUTING_REQUIREMENT_KIND_UNSPECIFIED,
    ROUTING_REQUIREMENT_KIND_PROVIDER,
    ROUTING_REQUIREMENT_KIND_MODEL,
    ROUTING_REQUIREMENT_KIND_CONTEXT,
    ROUTING_REQUIREMENT_KIND_TOOL,
    ROUTING_REQUIREMENT_KIND_AGENT,
    ROUTING_REQUIREMENT_KIND_CAPACITY,
    ROUTING_REQUIREMENT_KIND_EXTERNAL_AGENT_CREDENTIAL,
  ];

  static final $core.List<RoutingRequirementKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static RoutingRequirementKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const RoutingRequirementKind._(super.value, super.name);
}

class MessageRole extends $pb.ProtobufEnum {
  static const MessageRole MESSAGE_ROLE_UNSPECIFIED =
      MessageRole._(0, _omitEnumNames ? '' : 'MESSAGE_ROLE_UNSPECIFIED');
  static const MessageRole MESSAGE_ROLE_SYSTEM =
      MessageRole._(1, _omitEnumNames ? '' : 'MESSAGE_ROLE_SYSTEM');
  static const MessageRole MESSAGE_ROLE_USER =
      MessageRole._(2, _omitEnumNames ? '' : 'MESSAGE_ROLE_USER');
  static const MessageRole MESSAGE_ROLE_ASSISTANT =
      MessageRole._(3, _omitEnumNames ? '' : 'MESSAGE_ROLE_ASSISTANT');
  static const MessageRole MESSAGE_ROLE_TOOL =
      MessageRole._(4, _omitEnumNames ? '' : 'MESSAGE_ROLE_TOOL');

  static const $core.List<MessageRole> values = <MessageRole>[
    MESSAGE_ROLE_UNSPECIFIED,
    MESSAGE_ROLE_SYSTEM,
    MESSAGE_ROLE_USER,
    MESSAGE_ROLE_ASSISTANT,
    MESSAGE_ROLE_TOOL,
  ];

  static final $core.List<MessageRole?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static MessageRole? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MessageRole._(super.value, super.name);
}

class ToolPolicy extends $pb.ProtobufEnum {
  static const ToolPolicy TOOL_POLICY_UNSPECIFIED =
      ToolPolicy._(0, _omitEnumNames ? '' : 'TOOL_POLICY_UNSPECIFIED');
  static const ToolPolicy TOOL_POLICY_SAFE =
      ToolPolicy._(1, _omitEnumNames ? '' : 'TOOL_POLICY_SAFE');
  static const ToolPolicy TOOL_POLICY_APPROVAL_REQUIRED =
      ToolPolicy._(2, _omitEnumNames ? '' : 'TOOL_POLICY_APPROVAL_REQUIRED');
  static const ToolPolicy TOOL_POLICY_DISABLED =
      ToolPolicy._(3, _omitEnumNames ? '' : 'TOOL_POLICY_DISABLED');

  static const $core.List<ToolPolicy> values = <ToolPolicy>[
    TOOL_POLICY_UNSPECIFIED,
    TOOL_POLICY_SAFE,
    TOOL_POLICY_APPROVAL_REQUIRED,
    TOOL_POLICY_DISABLED,
  ];

  static final $core.List<ToolPolicy?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ToolPolicy? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ToolPolicy._(super.value, super.name);
}

class RunStatus extends $pb.ProtobufEnum {
  static const RunStatus RUN_STATUS_UNSPECIFIED =
      RunStatus._(0, _omitEnumNames ? '' : 'RUN_STATUS_UNSPECIFIED');
  static const RunStatus RUN_STATUS_QUEUED =
      RunStatus._(1, _omitEnumNames ? '' : 'RUN_STATUS_QUEUED');
  static const RunStatus RUN_STATUS_RUNNING =
      RunStatus._(2, _omitEnumNames ? '' : 'RUN_STATUS_RUNNING');
  static const RunStatus RUN_STATUS_WAITING_APPROVAL =
      RunStatus._(3, _omitEnumNames ? '' : 'RUN_STATUS_WAITING_APPROVAL');
  static const RunStatus RUN_STATUS_COMPLETED =
      RunStatus._(4, _omitEnumNames ? '' : 'RUN_STATUS_COMPLETED');
  static const RunStatus RUN_STATUS_FAILED =
      RunStatus._(5, _omitEnumNames ? '' : 'RUN_STATUS_FAILED');
  static const RunStatus RUN_STATUS_CANCELLED =
      RunStatus._(6, _omitEnumNames ? '' : 'RUN_STATUS_CANCELLED');

  static const $core.List<RunStatus> values = <RunStatus>[
    RUN_STATUS_UNSPECIFIED,
    RUN_STATUS_QUEUED,
    RUN_STATUS_RUNNING,
    RUN_STATUS_WAITING_APPROVAL,
    RUN_STATUS_COMPLETED,
    RUN_STATUS_FAILED,
    RUN_STATUS_CANCELLED,
  ];

  static final $core.List<RunStatus?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static RunStatus? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const RunStatus._(super.value, super.name);
}

class RunLifecycle extends $pb.ProtobufEnum {
  static const RunLifecycle RUN_LIFECYCLE_UNSPECIFIED =
      RunLifecycle._(0, _omitEnumNames ? '' : 'RUN_LIFECYCLE_UNSPECIFIED');
  static const RunLifecycle RUN_LIFECYCLE_UNKNOWN =
      RunLifecycle._(1, _omitEnumNames ? '' : 'RUN_LIFECYCLE_UNKNOWN');
  static const RunLifecycle RUN_LIFECYCLE_QUEUED =
      RunLifecycle._(2, _omitEnumNames ? '' : 'RUN_LIFECYCLE_QUEUED');
  static const RunLifecycle RUN_LIFECYCLE_RUNNING =
      RunLifecycle._(3, _omitEnumNames ? '' : 'RUN_LIFECYCLE_RUNNING');
  static const RunLifecycle RUN_LIFECYCLE_WAITING_APPROVAL =
      RunLifecycle._(4, _omitEnumNames ? '' : 'RUN_LIFECYCLE_WAITING_APPROVAL');
  static const RunLifecycle RUN_LIFECYCLE_RECOVERING =
      RunLifecycle._(5, _omitEnumNames ? '' : 'RUN_LIFECYCLE_RECOVERING');
  static const RunLifecycle RUN_LIFECYCLE_COMPLETED =
      RunLifecycle._(6, _omitEnumNames ? '' : 'RUN_LIFECYCLE_COMPLETED');
  static const RunLifecycle RUN_LIFECYCLE_FAILED =
      RunLifecycle._(7, _omitEnumNames ? '' : 'RUN_LIFECYCLE_FAILED');
  static const RunLifecycle RUN_LIFECYCLE_CANCELLED =
      RunLifecycle._(8, _omitEnumNames ? '' : 'RUN_LIFECYCLE_CANCELLED');

  static const $core.List<RunLifecycle> values = <RunLifecycle>[
    RUN_LIFECYCLE_UNSPECIFIED,
    RUN_LIFECYCLE_UNKNOWN,
    RUN_LIFECYCLE_QUEUED,
    RUN_LIFECYCLE_RUNNING,
    RUN_LIFECYCLE_WAITING_APPROVAL,
    RUN_LIFECYCLE_RECOVERING,
    RUN_LIFECYCLE_COMPLETED,
    RUN_LIFECYCLE_FAILED,
    RUN_LIFECYCLE_CANCELLED,
  ];

  static final $core.List<RunLifecycle?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 8);
  static RunLifecycle? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const RunLifecycle._(super.value, super.name);
}

class RunOutcomeReason extends $pb.ProtobufEnum {
  static const RunOutcomeReason RUN_OUTCOME_REASON_UNSPECIFIED =
      RunOutcomeReason._(
          0, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_UNSPECIFIED');
  static const RunOutcomeReason RUN_OUTCOME_REASON_UNKNOWN =
      RunOutcomeReason._(1, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_UNKNOWN');
  static const RunOutcomeReason RUN_OUTCOME_REASON_NONE =
      RunOutcomeReason._(2, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_NONE');
  static const RunOutcomeReason RUN_OUTCOME_REASON_COMPLETED_NO_CONTENT =
      RunOutcomeReason._(
          3, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_COMPLETED_NO_CONTENT');
  static const RunOutcomeReason RUN_OUTCOME_REASON_USER_CANCELLED =
      RunOutcomeReason._(
          4, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_USER_CANCELLED');
  static const RunOutcomeReason RUN_OUTCOME_REASON_ABANDONED =
      RunOutcomeReason._(
          5, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_ABANDONED');
  static const RunOutcomeReason RUN_OUTCOME_REASON_EXPIRED =
      RunOutcomeReason._(6, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_EXPIRED');
  static const RunOutcomeReason RUN_OUTCOME_REASON_CONTEXT_LIMIT =
      RunOutcomeReason._(
          7, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_CONTEXT_LIMIT');
  static const RunOutcomeReason RUN_OUTCOME_REASON_PROVIDER_FAILURE =
      RunOutcomeReason._(
          8, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_PROVIDER_FAILURE');
  static const RunOutcomeReason RUN_OUTCOME_REASON_TOOL_FAILURE =
      RunOutcomeReason._(
          9, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_TOOL_FAILURE');
  static const RunOutcomeReason RUN_OUTCOME_REASON_POLICY_DENIED =
      RunOutcomeReason._(
          10, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_POLICY_DENIED');
  static const RunOutcomeReason RUN_OUTCOME_REASON_RETRIES_EXHAUSTED =
      RunOutcomeReason._(
          11, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_RETRIES_EXHAUSTED');
  static const RunOutcomeReason RUN_OUTCOME_REASON_RECOVERY_INTERRUPTED =
      RunOutcomeReason._(
          12, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_RECOVERY_INTERRUPTED');
  static const RunOutcomeReason RUN_OUTCOME_REASON_SIDE_EFFECT_UNCERTAIN =
      RunOutcomeReason._(
          13, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_SIDE_EFFECT_UNCERTAIN');
  static const RunOutcomeReason RUN_OUTCOME_REASON_APPROVAL_DELIVERY_FAILED =
      RunOutcomeReason._(14,
          _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_APPROVAL_DELIVERY_FAILED');
  static const RunOutcomeReason RUN_OUTCOME_REASON_INTERNAL_FAILURE =
      RunOutcomeReason._(
          15, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_INTERNAL_FAILURE');
  static const RunOutcomeReason RUN_OUTCOME_REASON_LEGACY_UNKNOWN =
      RunOutcomeReason._(
          16, _omitEnumNames ? '' : 'RUN_OUTCOME_REASON_LEGACY_UNKNOWN');

  static const $core.List<RunOutcomeReason> values = <RunOutcomeReason>[
    RUN_OUTCOME_REASON_UNSPECIFIED,
    RUN_OUTCOME_REASON_UNKNOWN,
    RUN_OUTCOME_REASON_NONE,
    RUN_OUTCOME_REASON_COMPLETED_NO_CONTENT,
    RUN_OUTCOME_REASON_USER_CANCELLED,
    RUN_OUTCOME_REASON_ABANDONED,
    RUN_OUTCOME_REASON_EXPIRED,
    RUN_OUTCOME_REASON_CONTEXT_LIMIT,
    RUN_OUTCOME_REASON_PROVIDER_FAILURE,
    RUN_OUTCOME_REASON_TOOL_FAILURE,
    RUN_OUTCOME_REASON_POLICY_DENIED,
    RUN_OUTCOME_REASON_RETRIES_EXHAUSTED,
    RUN_OUTCOME_REASON_RECOVERY_INTERRUPTED,
    RUN_OUTCOME_REASON_SIDE_EFFECT_UNCERTAIN,
    RUN_OUTCOME_REASON_APPROVAL_DELIVERY_FAILED,
    RUN_OUTCOME_REASON_INTERNAL_FAILURE,
    RUN_OUTCOME_REASON_LEGACY_UNKNOWN,
  ];

  static final $core.List<RunOutcomeReason?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 16);
  static RunOutcomeReason? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const RunOutcomeReason._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
