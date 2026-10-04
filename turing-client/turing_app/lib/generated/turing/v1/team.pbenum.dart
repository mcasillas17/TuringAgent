// This is a generated file - do not edit.
//
// Generated from turing/v1/team.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

/// Where a specialist profile stands. Exactly one applies, checked in this
/// order: a file that does not parse, a profile the user has not enabled, one
/// whose current declaration the user has not granted, one that cannot run as
/// declared now (its model is not served by a live worker, or a required tool
/// does not resolve), and finally ACTIVE: enabled, granted at the current
/// revision and resolvable. ACTIVE claims nothing about delegation.
class AgentProfileState extends $pb.ProtobufEnum {
  static const AgentProfileState AGENT_PROFILE_STATE_UNSPECIFIED =
      AgentProfileState._(
          0, _omitEnumNames ? '' : 'AGENT_PROFILE_STATE_UNSPECIFIED');
  static const AgentProfileState AGENT_PROFILE_STATE_ACTIVE =
      AgentProfileState._(
          1, _omitEnumNames ? '' : 'AGENT_PROFILE_STATE_ACTIVE');
  static const AgentProfileState AGENT_PROFILE_STATE_DISABLED =
      AgentProfileState._(
          2, _omitEnumNames ? '' : 'AGENT_PROFILE_STATE_DISABLED');
  static const AgentProfileState AGENT_PROFILE_STATE_NEEDS_GRANT =
      AgentProfileState._(
          3, _omitEnumNames ? '' : 'AGENT_PROFILE_STATE_NEEDS_GRANT');
  static const AgentProfileState AGENT_PROFILE_STATE_UNAVAILABLE =
      AgentProfileState._(
          4, _omitEnumNames ? '' : 'AGENT_PROFILE_STATE_UNAVAILABLE');
  static const AgentProfileState AGENT_PROFILE_STATE_PARSE_ERROR =
      AgentProfileState._(
          5, _omitEnumNames ? '' : 'AGENT_PROFILE_STATE_PARSE_ERROR');

  static const $core.List<AgentProfileState> values = <AgentProfileState>[
    AGENT_PROFILE_STATE_UNSPECIFIED,
    AGENT_PROFILE_STATE_ACTIVE,
    AGENT_PROFILE_STATE_DISABLED,
    AGENT_PROFILE_STATE_NEEDS_GRANT,
    AGENT_PROFILE_STATE_UNAVAILABLE,
    AGENT_PROFILE_STATE_PARSE_ERROR,
  ];

  static final $core.List<AgentProfileState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static AgentProfileState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AgentProfileState._(super.value, super.name);
}

/// How much of the memory vault a specialist's runs may touch.
class AgentProfileMemoryAccess extends $pb.ProtobufEnum {
  static const AgentProfileMemoryAccess
      AGENT_PROFILE_MEMORY_ACCESS_UNSPECIFIED = AgentProfileMemoryAccess._(
          0, _omitEnumNames ? '' : 'AGENT_PROFILE_MEMORY_ACCESS_UNSPECIFIED');
  static const AgentProfileMemoryAccess AGENT_PROFILE_MEMORY_ACCESS_NONE =
      AgentProfileMemoryAccess._(
          1, _omitEnumNames ? '' : 'AGENT_PROFILE_MEMORY_ACCESS_NONE');

  /// memory.search and memory.read.
  static const AgentProfileMemoryAccess AGENT_PROFILE_MEMORY_ACCESS_READ =
      AgentProfileMemoryAccess._(
          2, _omitEnumNames ? '' : 'AGENT_PROFILE_MEMORY_ACCESS_READ');

  /// Also memory.remember, which still needs an approval per call.
  static const AgentProfileMemoryAccess AGENT_PROFILE_MEMORY_ACCESS_PROPOSE =
      AgentProfileMemoryAccess._(
          3, _omitEnumNames ? '' : 'AGENT_PROFILE_MEMORY_ACCESS_PROPOSE');

  static const $core.List<AgentProfileMemoryAccess> values =
      <AgentProfileMemoryAccess>[
    AGENT_PROFILE_MEMORY_ACCESS_UNSPECIFIED,
    AGENT_PROFILE_MEMORY_ACCESS_NONE,
    AGENT_PROFILE_MEMORY_ACCESS_READ,
    AGENT_PROFILE_MEMORY_ACCESS_PROPOSE,
  ];

  static final $core.List<AgentProfileMemoryAccess?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static AgentProfileMemoryAccess? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AgentProfileMemoryAccess._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
