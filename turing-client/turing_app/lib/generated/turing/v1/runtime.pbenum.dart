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
