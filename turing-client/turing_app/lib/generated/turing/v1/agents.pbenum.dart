// This is a generated file - do not edit.
//
// Generated from turing/v1/agents.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

/// Which vendor an external agent is. Purely descriptive: every provider below
/// is reached through the same OpenAI-compatible chat-completions endpoint, so
/// this changes the label and the suggested base URL, not the transport.
class ExternalAgentProvider extends $pb.ProtobufEnum {
  static const ExternalAgentProvider EXTERNAL_AGENT_PROVIDER_UNSPECIFIED =
      ExternalAgentProvider._(
          0, _omitEnumNames ? '' : 'EXTERNAL_AGENT_PROVIDER_UNSPECIFIED');
  static const ExternalAgentProvider EXTERNAL_AGENT_PROVIDER_ANTHROPIC =
      ExternalAgentProvider._(
          1, _omitEnumNames ? '' : 'EXTERNAL_AGENT_PROVIDER_ANTHROPIC');
  static const ExternalAgentProvider EXTERNAL_AGENT_PROVIDER_OPENAI =
      ExternalAgentProvider._(
          2, _omitEnumNames ? '' : 'EXTERNAL_AGENT_PROVIDER_OPENAI');
  static const ExternalAgentProvider EXTERNAL_AGENT_PROVIDER_GOOGLE =
      ExternalAgentProvider._(
          3, _omitEnumNames ? '' : 'EXTERNAL_AGENT_PROVIDER_GOOGLE');
  static const ExternalAgentProvider EXTERNAL_AGENT_PROVIDER_XAI =
      ExternalAgentProvider._(
          4, _omitEnumNames ? '' : 'EXTERNAL_AGENT_PROVIDER_XAI');

  /// An OpenAI-compatible endpoint that is none of the above.
  static const ExternalAgentProvider EXTERNAL_AGENT_PROVIDER_OTHER =
      ExternalAgentProvider._(
          5, _omitEnumNames ? '' : 'EXTERNAL_AGENT_PROVIDER_OTHER');

  static const $core.List<ExternalAgentProvider> values =
      <ExternalAgentProvider>[
    EXTERNAL_AGENT_PROVIDER_UNSPECIFIED,
    EXTERNAL_AGENT_PROVIDER_ANTHROPIC,
    EXTERNAL_AGENT_PROVIDER_OPENAI,
    EXTERNAL_AGENT_PROVIDER_GOOGLE,
    EXTERNAL_AGENT_PROVIDER_XAI,
    EXTERNAL_AGENT_PROVIDER_OTHER,
  ];

  static final $core.List<ExternalAgentProvider?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static ExternalAgentProvider? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ExternalAgentProvider._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
