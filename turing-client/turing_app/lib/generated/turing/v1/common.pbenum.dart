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

class EgressDataCategory extends $pb.ProtobufEnum {
  static const EgressDataCategory EGRESS_DATA_CATEGORY_UNSPECIFIED =
      EgressDataCategory._(
          0, _omitEnumNames ? '' : 'EGRESS_DATA_CATEGORY_UNSPECIFIED');
  static const EgressDataCategory EGRESS_DATA_CATEGORY_CURRENT_MESSAGE =
      EgressDataCategory._(
          1, _omitEnumNames ? '' : 'EGRESS_DATA_CATEGORY_CURRENT_MESSAGE');
  static const EgressDataCategory EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY =
      EgressDataCategory._(
          2, _omitEnumNames ? '' : 'EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY');
  static const EgressDataCategory EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL =
      EgressDataCategory._(
          3, _omitEnumNames ? '' : 'EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL');
  static const EgressDataCategory EGRESS_DATA_CATEGORY_MEMORY_PROFILE =
      EgressDataCategory._(
          4, _omitEnumNames ? '' : 'EGRESS_DATA_CATEGORY_MEMORY_PROFILE');
  static const EgressDataCategory EGRESS_DATA_CATEGORY_SKILL_CONTENT =
      EgressDataCategory._(
          5, _omitEnumNames ? '' : 'EGRESS_DATA_CATEGORY_SKILL_CONTENT');
  static const EgressDataCategory EGRESS_DATA_CATEGORY_TOOL_SCHEMAS =
      EgressDataCategory._(
          6, _omitEnumNames ? '' : 'EGRESS_DATA_CATEGORY_TOOL_SCHEMAS');
  static const EgressDataCategory EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS =
      EgressDataCategory._(
          7, _omitEnumNames ? '' : 'EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS');
  static const EgressDataCategory EGRESS_DATA_CATEGORY_TOOL_RESULTS =
      EgressDataCategory._(
          8, _omitEnumNames ? '' : 'EGRESS_DATA_CATEGORY_TOOL_RESULTS');
  static const EgressDataCategory EGRESS_DATA_CATEGORY_ATTACHMENTS =
      EgressDataCategory._(
          9, _omitEnumNames ? '' : 'EGRESS_DATA_CATEGORY_ATTACHMENTS');

  static const $core.List<EgressDataCategory> values = <EgressDataCategory>[
    EGRESS_DATA_CATEGORY_UNSPECIFIED,
    EGRESS_DATA_CATEGORY_CURRENT_MESSAGE,
    EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY,
    EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL,
    EGRESS_DATA_CATEGORY_MEMORY_PROFILE,
    EGRESS_DATA_CATEGORY_SKILL_CONTENT,
    EGRESS_DATA_CATEGORY_TOOL_SCHEMAS,
    EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
    EGRESS_DATA_CATEGORY_TOOL_RESULTS,
    EGRESS_DATA_CATEGORY_ATTACHMENTS,
  ];

  static final $core.List<EgressDataCategory?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 9);
  static EgressDataCategory? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const EgressDataCategory._(super.value, super.name);
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

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
