// This is a generated file - do not edit.
//
// Generated from turing/v1/tools.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use toolCallPhaseDescriptor instead')
const ToolCallPhase$json = {
  '1': 'ToolCallPhase',
  '2': [
    {'1': 'TOOL_CALL_PHASE_UNSPECIFIED', '2': 0},
    {'1': 'TOOL_CALL_PHASE_BEFORE', '2': 1},
    {'1': 'TOOL_CALL_PHASE_AFTER', '2': 2},
  ],
};

/// Descriptor for `ToolCallPhase`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List toolCallPhaseDescriptor = $convert.base64Decode(
    'Cg1Ub29sQ2FsbFBoYXNlEh8KG1RPT0xfQ0FMTF9QSEFTRV9VTlNQRUNJRklFRBAAEhoKFlRPT0'
    'xfQ0FMTF9QSEFTRV9CRUZPUkUQARIZChVUT09MX0NBTExfUEhBU0VfQUZURVIQAg==');

@$core.Deprecated('Use toolCallStatusDescriptor instead')
const ToolCallStatus$json = {
  '1': 'ToolCallStatus',
  '2': [
    {'1': 'TOOL_CALL_STATUS_UNSPECIFIED', '2': 0},
    {'1': 'TOOL_CALL_STATUS_COMPLETED', '2': 1},
    {'1': 'TOOL_CALL_STATUS_FAILED', '2': 2},
    {'1': 'TOOL_CALL_STATUS_DENIED', '2': 3},
  ],
};

/// Descriptor for `ToolCallStatus`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List toolCallStatusDescriptor = $convert.base64Decode(
    'Cg5Ub29sQ2FsbFN0YXR1cxIgChxUT09MX0NBTExfU1RBVFVTX1VOU1BFQ0lGSUVEEAASHgoaVE'
    '9PTF9DQUxMX1NUQVRVU19DT01QTEVURUQQARIbChdUT09MX0NBTExfU1RBVFVTX0ZBSUxFRBAC'
    'EhsKF1RPT0xfQ0FMTF9TVEFUVVNfREVOSUVEEAM=');

@$core.Deprecated('Use toolCallErrorDescriptor instead')
const ToolCallError$json = {
  '1': 'ToolCallError',
  '2': [
    {'1': 'code', '3': 1, '4': 1, '5': 9, '10': 'code'},
    {'1': 'message', '3': 2, '4': 1, '5': 9, '10': 'message'},
  ],
};

/// Descriptor for `ToolCallError`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List toolCallErrorDescriptor = $convert.base64Decode(
    'Cg1Ub29sQ2FsbEVycm9yEhIKBGNvZGUYASABKAlSBGNvZGUSGAoHbWVzc2FnZRgCIAEoCVIHbW'
    'Vzc2FnZQ==');

@$core.Deprecated('Use toolCallBeaconDescriptor instead')
const ToolCallBeacon$json = {
  '1': 'ToolCallBeacon',
  '2': [
    {
      '1': 'phase',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ToolCallPhase',
      '10': 'phase'
    },
    {'1': 'tool_call_id', '3': 2, '4': 1, '5': 9, '10': 'toolCallId'},
    {
      '1': 'agent_id',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.AgentId',
      '10': 'agentId'
    },
    {'1': 'server_name', '3': 4, '4': 1, '5': 9, '10': 'serverName'},
    {'1': 'tool_name', '3': 5, '4': 1, '5': 9, '10': 'toolName'},
    {
      '1': 'args',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'args'
    },
    {
      '1': 'status',
      '3': 7,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ToolCallStatus',
      '10': 'status'
    },
    {'1': 'result_summary', '3': 8, '4': 1, '5': 9, '10': 'resultSummary'},
    {'1': 'duration_ms', '3': 9, '4': 1, '5': 3, '10': 'durationMs'},
    {
      '1': 'error',
      '3': 10,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.ToolCallError',
      '10': 'error'
    },
    {'1': 'run_id', '3': 11, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'trace_id', '3': 12, '4': 1, '5': 9, '10': 'traceId'},
    {
      '1': 'model_tool_call_id',
      '3': 13,
      '4': 1,
      '5': 9,
      '10': 'modelToolCallId'
    },
  ],
};

/// Descriptor for `ToolCallBeacon`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List toolCallBeaconDescriptor = $convert.base64Decode(
    'Cg5Ub29sQ2FsbEJlYWNvbhIuCgVwaGFzZRgBIAEoDjIYLnR1cmluZy52MS5Ub29sQ2FsbFBoYX'
    'NlUgVwaGFzZRIgCgx0b29sX2NhbGxfaWQYAiABKAlSCnRvb2xDYWxsSWQSLQoIYWdlbnRfaWQY'
    'AyABKA4yEi50dXJpbmcudjEuQWdlbnRJZFIHYWdlbnRJZBIfCgtzZXJ2ZXJfbmFtZRgEIAEoCV'
    'IKc2VydmVyTmFtZRIbCgl0b29sX25hbWUYBSABKAlSCHRvb2xOYW1lEisKBGFyZ3MYBiABKAsy'
    'Fy5nb29nbGUucHJvdG9idWYuU3RydWN0UgRhcmdzEjEKBnN0YXR1cxgHIAEoDjIZLnR1cmluZy'
    '52MS5Ub29sQ2FsbFN0YXR1c1IGc3RhdHVzEiUKDnJlc3VsdF9zdW1tYXJ5GAggASgJUg1yZXN1'
    'bHRTdW1tYXJ5Eh8KC2R1cmF0aW9uX21zGAkgASgDUgpkdXJhdGlvbk1zEi4KBWVycm9yGAogAS'
    'gLMhgudHVyaW5nLnYxLlRvb2xDYWxsRXJyb3JSBWVycm9yEhUKBnJ1bl9pZBgLIAEoCVIFcnVu'
    'SWQSGQoIdHJhY2VfaWQYDCABKAlSB3RyYWNlSWQSKwoSbW9kZWxfdG9vbF9jYWxsX2lkGA0gAS'
    'gJUg9tb2RlbFRvb2xDYWxsSWQ=');

@$core.Deprecated('Use toolPolicyDecisionDescriptor instead')
const ToolPolicyDecision$json = {
  '1': 'ToolPolicyDecision',
  '2': [
    {
      '1': 'decision',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ToolPolicyDecision.Decision',
      '10': 'decision'
    },
    {'1': 'tool_call_id', '3': 2, '4': 1, '5': 9, '10': 'toolCallId'},
    {'1': 'approval_id', '3': 3, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'reason', '3': 4, '4': 1, '5': 9, '10': 'reason'},
    {'1': 'terminal_run', '3': 5, '4': 1, '5': 8, '10': 'terminalRun'},
    {
      '1': 'phase',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ToolCallPhase',
      '10': 'phase'
    },
    {'1': 'provenance_token', '3': 7, '4': 1, '5': 9, '10': 'provenanceToken'},
    {'1': 'run_state_version', '3': 8, '4': 1, '5': 3, '10': 'runStateVersion'},
    {'1': 'read_only', '3': 9, '4': 1, '5': 8, '10': 'readOnly'},
  ],
  '4': [ToolPolicyDecision_Decision$json],
};

@$core.Deprecated('Use toolPolicyDecisionDescriptor instead')
const ToolPolicyDecision_Decision$json = {
  '1': 'Decision',
  '2': [
    {'1': 'DECISION_UNSPECIFIED', '2': 0},
    {'1': 'DECISION_ALLOW', '2': 1},
    {'1': 'DECISION_DENY', '2': 2},
    {'1': 'DECISION_APPROVAL_REQUIRED', '2': 3},
  ],
};

/// Descriptor for `ToolPolicyDecision`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List toolPolicyDecisionDescriptor = $convert.base64Decode(
    'ChJUb29sUG9saWN5RGVjaXNpb24SQgoIZGVjaXNpb24YASABKA4yJi50dXJpbmcudjEuVG9vbF'
    'BvbGljeURlY2lzaW9uLkRlY2lzaW9uUghkZWNpc2lvbhIgCgx0b29sX2NhbGxfaWQYAiABKAlS'
    'CnRvb2xDYWxsSWQSHwoLYXBwcm92YWxfaWQYAyABKAlSCmFwcHJvdmFsSWQSFgoGcmVhc29uGA'
    'QgASgJUgZyZWFzb24SIQoMdGVybWluYWxfcnVuGAUgASgIUgt0ZXJtaW5hbFJ1bhIuCgVwaGFz'
    'ZRgGIAEoDjIYLnR1cmluZy52MS5Ub29sQ2FsbFBoYXNlUgVwaGFzZRIpChBwcm92ZW5hbmNlX3'
    'Rva2VuGAcgASgJUg9wcm92ZW5hbmNlVG9rZW4SKgoRcnVuX3N0YXRlX3ZlcnNpb24YCCABKANS'
    'D3J1blN0YXRlVmVyc2lvbhIbCglyZWFkX29ubHkYCSABKAhSCHJlYWRPbmx5ImsKCERlY2lzaW'
    '9uEhgKFERFQ0lTSU9OX1VOU1BFQ0lGSUVEEAASEgoOREVDSVNJT05fQUxMT1cQARIRCg1ERUNJ'
    'U0lPTl9ERU5ZEAISHgoaREVDSVNJT05fQVBQUk9WQUxfUkVRVUlSRUQQAw==');
