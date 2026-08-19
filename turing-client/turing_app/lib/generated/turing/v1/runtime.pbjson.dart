// This is a generated file - do not edit.
//
// Generated from turing/v1/runtime.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use toolDiscoveryStatusDescriptor instead')
const ToolDiscoveryStatus$json = {
  '1': 'ToolDiscoveryStatus',
  '2': [
    {'1': 'TOOL_DISCOVERY_STATUS_UNSPECIFIED', '2': 0},
    {'1': 'TOOL_DISCOVERY_STATUS_COMPLETE', '2': 1},
    {'1': 'TOOL_DISCOVERY_STATUS_FAILED', '2': 2},
  ],
};

/// Descriptor for `ToolDiscoveryStatus`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List toolDiscoveryStatusDescriptor = $convert.base64Decode(
    'ChNUb29sRGlzY292ZXJ5U3RhdHVzEiUKIVRPT0xfRElTQ09WRVJZX1NUQVRVU19VTlNQRUNJRk'
    'lFRBAAEiIKHlRPT0xfRElTQ09WRVJZX1NUQVRVU19DT01QTEVURRABEiAKHFRPT0xfRElTQ09W'
    'RVJZX1NUQVRVU19GQUlMRUQQAg==');

@$core.Deprecated('Use agentJobDescriptor instead')
const AgentJob$json = {
  '1': 'AgentJob',
  '2': [
    {'1': 'job_id', '3': 1, '4': 1, '5': 9, '10': 'jobId'},
    {'1': 'run_id', '3': 2, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'session_id', '3': 3, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'user_message_id', '3': 4, '4': 1, '5': 9, '10': 'userMessageId'},
    {
      '1': 'assistant_message_id',
      '3': 5,
      '4': 1,
      '5': 9,
      '10': 'assistantMessageId'
    },
    {
      '1': 'agent_id',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.AgentId',
      '10': 'agentId'
    },
    {'1': 'trace_id', '3': 7, '4': 1, '5': 9, '10': 'traceId'},
    {
      '1': 'model_provider',
      '3': 8,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ModelProvider',
      '10': 'modelProvider'
    },
    {'1': 'model', '3': 9, '4': 1, '5': 9, '10': 'model'},
    {'1': 'user_text', '3': 10, '4': 1, '5': 9, '10': 'userText'},
    {'1': 'requested_tools', '3': 11, '4': 3, '5': 9, '10': 'requestedTools'},
    {'1': 'attempt', '3': 12, '4': 1, '5': 5, '10': 'attempt'},
    {
      '1': 'skills',
      '3': 13,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.AttachedSkill',
      '10': 'skills'
    },
    {
      '1': 'external_agent',
      '3': 14,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.ExternalAgentTarget',
      '10': 'externalAgent'
    },
  ],
};

/// Descriptor for `AgentJob`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List agentJobDescriptor = $convert.base64Decode(
    'CghBZ2VudEpvYhIVCgZqb2JfaWQYASABKAlSBWpvYklkEhUKBnJ1bl9pZBgCIAEoCVIFcnVuSW'
    'QSHQoKc2Vzc2lvbl9pZBgDIAEoCVIJc2Vzc2lvbklkEiYKD3VzZXJfbWVzc2FnZV9pZBgEIAEo'
    'CVINdXNlck1lc3NhZ2VJZBIwChRhc3Npc3RhbnRfbWVzc2FnZV9pZBgFIAEoCVISYXNzaXN0YW'
    '50TWVzc2FnZUlkEi0KCGFnZW50X2lkGAYgASgOMhIudHVyaW5nLnYxLkFnZW50SWRSB2FnZW50'
    'SWQSGQoIdHJhY2VfaWQYByABKAlSB3RyYWNlSWQSPwoObW9kZWxfcHJvdmlkZXIYCCABKA4yGC'
    '50dXJpbmcudjEuTW9kZWxQcm92aWRlclINbW9kZWxQcm92aWRlchIUCgVtb2RlbBgJIAEoCVIF'
    'bW9kZWwSGwoJdXNlcl90ZXh0GAogASgJUgh1c2VyVGV4dBInCg9yZXF1ZXN0ZWRfdG9vbHMYCy'
    'ADKAlSDnJlcXVlc3RlZFRvb2xzEhgKB2F0dGVtcHQYDCABKAVSB2F0dGVtcHQSMAoGc2tpbGxz'
    'GA0gAygLMhgudHVyaW5nLnYxLkF0dGFjaGVkU2tpbGxSBnNraWxscxJFCg5leHRlcm5hbF9hZ2'
    'VudBgOIAEoCzIeLnR1cmluZy52MS5FeHRlcm5hbEFnZW50VGFyZ2V0Ug1leHRlcm5hbEFnZW50');

@$core.Deprecated('Use externalAgentTargetDescriptor instead')
const ExternalAgentTarget$json = {
  '1': 'ExternalAgentTarget',
  '2': [
    {'1': 'display_name', '3': 1, '4': 1, '5': 9, '10': 'displayName'},
    {'1': 'base_url', '3': 2, '4': 1, '5': 9, '10': 'baseUrl'},
    {'1': 'credential_ref', '3': 3, '4': 1, '5': 9, '10': 'credentialRef'},
  ],
};

/// Descriptor for `ExternalAgentTarget`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List externalAgentTargetDescriptor = $convert.base64Decode(
    'ChNFeHRlcm5hbEFnZW50VGFyZ2V0EiEKDGRpc3BsYXlfbmFtZRgBIAEoCVILZGlzcGxheU5hbW'
    'USGQoIYmFzZV91cmwYAiABKAlSB2Jhc2VVcmwSJQoOY3JlZGVudGlhbF9yZWYYAyABKAlSDWNy'
    'ZWRlbnRpYWxSZWY=');

@$core.Deprecated('Use attachedSkillDescriptor instead')
const AttachedSkill$json = {
  '1': 'AttachedSkill',
  '2': [
    {'1': 'name', '3': 1, '4': 1, '5': 9, '10': 'name'},
    {'1': 'instructions', '3': 2, '4': 1, '5': 9, '10': 'instructions'},
  ],
};

/// Descriptor for `AttachedSkill`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List attachedSkillDescriptor = $convert.base64Decode(
    'Cg1BdHRhY2hlZFNraWxsEhIKBG5hbWUYASABKAlSBG5hbWUSIgoMaW5zdHJ1Y3Rpb25zGAIgAS'
    'gJUgxpbnN0cnVjdGlvbnM=');

@$core.Deprecated('Use discoveredToolDescriptor instead')
const DiscoveredTool$json = {
  '1': 'DiscoveredTool',
  '2': [
    {'1': 'server_name', '3': 1, '4': 1, '5': 9, '10': 'serverName'},
    {'1': 'tool_name', '3': 2, '4': 1, '5': 9, '10': 'toolName'},
    {
      '1': 'schema',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'schema'
    },
  ],
};

/// Descriptor for `DiscoveredTool`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List discoveredToolDescriptor = $convert.base64Decode(
    'Cg5EaXNjb3ZlcmVkVG9vbBIfCgtzZXJ2ZXJfbmFtZRgBIAEoCVIKc2VydmVyTmFtZRIbCgl0b2'
    '9sX25hbWUYAiABKAlSCHRvb2xOYW1lEi8KBnNjaGVtYRgDIAEoCzIXLmdvb2dsZS5wcm90b2J1'
    'Zi5TdHJ1Y3RSBnNjaGVtYQ==');

@$core.Deprecated('Use runtimeWorkerReadyDescriptor instead')
const RuntimeWorkerReady$json = {
  '1': 'RuntimeWorkerReady',
  '2': [
    {'1': 'worker_id', '3': 1, '4': 1, '5': 9, '10': 'workerId'},
    {
      '1': 'agent_id',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.AgentId',
      '10': 'agentId'
    },
    {
      '1': 'max_concurrent_runs',
      '3': 3,
      '4': 1,
      '5': 5,
      '10': 'maxConcurrentRuns'
    },
    {
      '1': 'tools',
      '3': 4,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.DiscoveredTool',
      '10': 'tools'
    },
    {
      '1': 'tool_discovery_status',
      '3': 5,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ToolDiscoveryStatus',
      '10': 'toolDiscoveryStatus'
    },
  ],
};

/// Descriptor for `RuntimeWorkerReady`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeWorkerReadyDescriptor = $convert.base64Decode(
    'ChJSdW50aW1lV29ya2VyUmVhZHkSGwoJd29ya2VyX2lkGAEgASgJUgh3b3JrZXJJZBItCghhZ2'
    'VudF9pZBgCIAEoDjISLnR1cmluZy52MS5BZ2VudElkUgdhZ2VudElkEi4KE21heF9jb25jdXJy'
    'ZW50X3J1bnMYAyABKAVSEW1heENvbmN1cnJlbnRSdW5zEi8KBXRvb2xzGAQgAygLMhkudHVyaW'
    '5nLnYxLkRpc2NvdmVyZWRUb29sUgV0b29scxJSChV0b29sX2Rpc2NvdmVyeV9zdGF0dXMYBSAB'
    'KA4yHi50dXJpbmcudjEuVG9vbERpc2NvdmVyeVN0YXR1c1ITdG9vbERpc2NvdmVyeVN0YXR1cw'
    '==');

@$core.Deprecated('Use runtimeHeartbeatDescriptor instead')
const RuntimeHeartbeat$json = {
  '1': 'RuntimeHeartbeat',
  '2': [
    {'1': 'worker_id', '3': 1, '4': 1, '5': 9, '10': 'workerId'},
  ],
};

/// Descriptor for `RuntimeHeartbeat`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeHeartbeatDescriptor = $convert.base64Decode(
    'ChBSdW50aW1lSGVhcnRiZWF0EhsKCXdvcmtlcl9pZBgBIAEoCVIId29ya2VySWQ=');

@$core.Deprecated('Use runTokenUsageDescriptor instead')
const RunTokenUsage$json = {
  '1': 'RunTokenUsage',
  '2': [
    {
      '1': 'input_tokens',
      '3': 1,
      '4': 1,
      '5': 3,
      '9': 0,
      '10': 'inputTokens',
      '17': true
    },
    {
      '1': 'output_tokens',
      '3': 2,
      '4': 1,
      '5': 3,
      '9': 1,
      '10': 'outputTokens',
      '17': true
    },
  ],
  '8': [
    {'1': '_input_tokens'},
    {'1': '_output_tokens'},
  ],
};

/// Descriptor for `RunTokenUsage`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runTokenUsageDescriptor = $convert.base64Decode(
    'Cg1SdW5Ub2tlblVzYWdlEiYKDGlucHV0X3Rva2VucxgBIAEoA0gAUgtpbnB1dFRva2Vuc4gBAR'
    'IoCg1vdXRwdXRfdG9rZW5zGAIgASgDSAFSDG91dHB1dFRva2Vuc4gBAUIPCg1faW5wdXRfdG9r'
    'ZW5zQhAKDl9vdXRwdXRfdG9rZW5z');

@$core.Deprecated('Use runtimeRunCompletedDescriptor instead')
const RuntimeRunCompleted$json = {
  '1': 'RuntimeRunCompleted',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
    {
      '1': 'assistant_message_id',
      '3': 2,
      '4': 1,
      '5': 9,
      '10': 'assistantMessageId'
    },
    {'1': 'content', '3': 3, '4': 1, '5': 9, '10': 'content'},
    {
      '1': 'usage',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'usage'
    },
    {
      '1': 'token_usage',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunTokenUsage',
      '10': 'tokenUsage'
    },
  ],
};

/// Descriptor for `RuntimeRunCompleted`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeRunCompletedDescriptor = $convert.base64Decode(
    'ChNSdW50aW1lUnVuQ29tcGxldGVkEhUKBnJ1bl9pZBgBIAEoCVIFcnVuSWQSMAoUYXNzaXN0YW'
    '50X21lc3NhZ2VfaWQYAiABKAlSEmFzc2lzdGFudE1lc3NhZ2VJZBIYCgdjb250ZW50GAMgASgJ'
    'Ugdjb250ZW50Ei0KBXVzYWdlGAQgASgLMhcuZ29vZ2xlLnByb3RvYnVmLlN0cnVjdFIFdXNhZ2'
    'USOQoLdG9rZW5fdXNhZ2UYBSABKAsyGC50dXJpbmcudjEuUnVuVG9rZW5Vc2FnZVIKdG9rZW5V'
    'c2FnZQ==');

@$core.Deprecated('Use runtimeRunFailedDescriptor instead')
const RuntimeRunFailed$json = {
  '1': 'RuntimeRunFailed',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'code', '3': 2, '4': 1, '5': 9, '10': 'code'},
    {'1': 'message', '3': 3, '4': 1, '5': 9, '10': 'message'},
    {'1': 'retryable', '3': 4, '4': 1, '5': 8, '10': 'retryable'},
  ],
};

/// Descriptor for `RuntimeRunFailed`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeRunFailedDescriptor = $convert.base64Decode(
    'ChBSdW50aW1lUnVuRmFpbGVkEhUKBnJ1bl9pZBgBIAEoCVIFcnVuSWQSEgoEY29kZRgCIAEoCV'
    'IEY29kZRIYCgdtZXNzYWdlGAMgASgJUgdtZXNzYWdlEhwKCXJldHJ5YWJsZRgEIAEoCFIJcmV0'
    'cnlhYmxl');

@$core.Deprecated('Use runtimeCancelledAckDescriptor instead')
const RuntimeCancelledAck$json = {
  '1': 'RuntimeCancelledAck',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
  ],
};

/// Descriptor for `RuntimeCancelledAck`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeCancelledAckDescriptor =
    $convert.base64Decode(
        'ChNSdW50aW1lQ2FuY2VsbGVkQWNrEhUKBnJ1bl9pZBgBIAEoCVIFcnVuSWQ=');

@$core.Deprecated('Use runtimeUpdateDescriptor instead')
const RuntimeUpdate$json = {
  '1': 'RuntimeUpdate',
  '2': [
    {
      '1': 'worker_ready',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RuntimeWorkerReady',
      '9': 0,
      '10': 'workerReady'
    },
    {
      '1': 'heartbeat',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RuntimeHeartbeat',
      '9': 0,
      '10': 'heartbeat'
    },
    {
      '1': 'event',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.TuringEvent',
      '9': 0,
      '10': 'event'
    },
    {
      '1': 'tool_beacon',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.ToolCallBeacon',
      '9': 0,
      '10': 'toolBeacon'
    },
    {
      '1': 'run_completed',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RuntimeRunCompleted',
      '9': 0,
      '10': 'runCompleted'
    },
    {
      '1': 'run_failed',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RuntimeRunFailed',
      '9': 0,
      '10': 'runFailed'
    },
    {
      '1': 'run_cancelled_ack',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RuntimeCancelledAck',
      '9': 0,
      '10': 'runCancelledAck'
    },
  ],
  '8': [
    {'1': 'update'},
  ],
};

/// Descriptor for `RuntimeUpdate`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeUpdateDescriptor = $convert.base64Decode(
    'Cg1SdW50aW1lVXBkYXRlEkIKDHdvcmtlcl9yZWFkeRgBIAEoCzIdLnR1cmluZy52MS5SdW50aW'
    '1lV29ya2VyUmVhZHlIAFILd29ya2VyUmVhZHkSOwoJaGVhcnRiZWF0GAIgASgLMhsudHVyaW5n'
    'LnYxLlJ1bnRpbWVIZWFydGJlYXRIAFIJaGVhcnRiZWF0Ei4KBWV2ZW50GAMgASgLMhYudHVyaW'
    '5nLnYxLlR1cmluZ0V2ZW50SABSBWV2ZW50EjwKC3Rvb2xfYmVhY29uGAQgASgLMhkudHVyaW5n'
    'LnYxLlRvb2xDYWxsQmVhY29uSABSCnRvb2xCZWFjb24SRQoNcnVuX2NvbXBsZXRlZBgFIAEoCz'
    'IeLnR1cmluZy52MS5SdW50aW1lUnVuQ29tcGxldGVkSABSDHJ1bkNvbXBsZXRlZBI8CgpydW5f'
    'ZmFpbGVkGAYgASgLMhsudHVyaW5nLnYxLlJ1bnRpbWVSdW5GYWlsZWRIAFIJcnVuRmFpbGVkEk'
    'wKEXJ1bl9jYW5jZWxsZWRfYWNrGAcgASgLMh4udHVyaW5nLnYxLlJ1bnRpbWVDYW5jZWxsZWRB'
    'Y2tIAFIPcnVuQ2FuY2VsbGVkQWNrQggKBnVwZGF0ZQ==');

@$core.Deprecated('Use runtimeWorkerAcceptedDescriptor instead')
const RuntimeWorkerAccepted$json = {
  '1': 'RuntimeWorkerAccepted',
  '2': [
    {'1': 'worker_id', '3': 1, '4': 1, '5': 9, '10': 'workerId'},
  ],
};

/// Descriptor for `RuntimeWorkerAccepted`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeWorkerAcceptedDescriptor = $convert.base64Decode(
    'ChVSdW50aW1lV29ya2VyQWNjZXB0ZWQSGwoJd29ya2VyX2lkGAEgASgJUgh3b3JrZXJJZA==');

@$core.Deprecated('Use runtimeRunCancelledDescriptor instead')
const RuntimeRunCancelled$json = {
  '1': 'RuntimeRunCancelled',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'reason', '3': 2, '4': 1, '5': 9, '10': 'reason'},
  ],
};

/// Descriptor for `RuntimeRunCancelled`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeRunCancelledDescriptor = $convert.base64Decode(
    'ChNSdW50aW1lUnVuQ2FuY2VsbGVkEhUKBnJ1bl9pZBgBIAEoCVIFcnVuSWQSFgoGcmVhc29uGA'
    'IgASgJUgZyZWFzb24=');

@$core.Deprecated('Use runtimeApprovalUpdatedDescriptor instead')
const RuntimeApprovalUpdated$json = {
  '1': 'RuntimeApprovalUpdated',
  '2': [
    {'1': 'approval_id', '3': 1, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'approval_token', '3': 2, '4': 1, '5': 9, '10': 'approvalToken'},
    {'1': 'status', '3': 3, '4': 1, '5': 9, '10': 'status'},
  ],
};

/// Descriptor for `RuntimeApprovalUpdated`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeApprovalUpdatedDescriptor = $convert.base64Decode(
    'ChZSdW50aW1lQXBwcm92YWxVcGRhdGVkEh8KC2FwcHJvdmFsX2lkGAEgASgJUgphcHByb3ZhbE'
    'lkEiUKDmFwcHJvdmFsX3Rva2VuGAIgASgJUg1hcHByb3ZhbFRva2VuEhYKBnN0YXR1cxgDIAEo'
    'CVIGc3RhdHVz');

@$core.Deprecated('Use runtimeShutdownRequestedDescriptor instead')
const RuntimeShutdownRequested$json = {
  '1': 'RuntimeShutdownRequested',
  '2': [
    {'1': 'reason', '3': 1, '4': 1, '5': 9, '10': 'reason'},
  ],
};

/// Descriptor for `RuntimeShutdownRequested`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeShutdownRequestedDescriptor =
    $convert.base64Decode(
        'ChhSdW50aW1lU2h1dGRvd25SZXF1ZXN0ZWQSFgoGcmVhc29uGAEgASgJUgZyZWFzb24=');

@$core.Deprecated('Use runtimeCommandDescriptor instead')
const RuntimeCommand$json = {
  '1': 'RuntimeCommand',
  '2': [
    {
      '1': 'worker_accepted',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RuntimeWorkerAccepted',
      '9': 0,
      '10': 'workerAccepted'
    },
    {
      '1': 'run_assigned',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.AgentJob',
      '9': 0,
      '10': 'runAssigned'
    },
    {
      '1': 'run_cancelled',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RuntimeRunCancelled',
      '9': 0,
      '10': 'runCancelled'
    },
    {
      '1': 'approval_updated',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RuntimeApprovalUpdated',
      '9': 0,
      '10': 'approvalUpdated'
    },
    {
      '1': 'shutdown_requested',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RuntimeShutdownRequested',
      '9': 0,
      '10': 'shutdownRequested'
    },
    {
      '1': 'tool_policy_decision',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.ToolPolicyDecision',
      '9': 0,
      '10': 'toolPolicyDecision'
    },
  ],
  '8': [
    {'1': 'command'},
  ],
};

/// Descriptor for `RuntimeCommand`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeCommandDescriptor = $convert.base64Decode(
    'Cg5SdW50aW1lQ29tbWFuZBJLCg93b3JrZXJfYWNjZXB0ZWQYASABKAsyIC50dXJpbmcudjEuUn'
    'VudGltZVdvcmtlckFjY2VwdGVkSABSDndvcmtlckFjY2VwdGVkEjgKDHJ1bl9hc3NpZ25lZBgC'
    'IAEoCzITLnR1cmluZy52MS5BZ2VudEpvYkgAUgtydW5Bc3NpZ25lZBJFCg1ydW5fY2FuY2VsbG'
    'VkGAMgASgLMh4udHVyaW5nLnYxLlJ1bnRpbWVSdW5DYW5jZWxsZWRIAFIMcnVuQ2FuY2VsbGVk'
    'Ek4KEGFwcHJvdmFsX3VwZGF0ZWQYBCABKAsyIS50dXJpbmcudjEuUnVudGltZUFwcHJvdmFsVX'
    'BkYXRlZEgAUg9hcHByb3ZhbFVwZGF0ZWQSVAoSc2h1dGRvd25fcmVxdWVzdGVkGAUgASgLMiMu'
    'dHVyaW5nLnYxLlJ1bnRpbWVTaHV0ZG93blJlcXVlc3RlZEgAUhFzaHV0ZG93blJlcXVlc3RlZB'
    'JRChR0b29sX3BvbGljeV9kZWNpc2lvbhgGIAEoCzIdLnR1cmluZy52MS5Ub29sUG9saWN5RGVj'
    'aXNpb25IAFISdG9vbFBvbGljeURlY2lzaW9uQgkKB2NvbW1hbmQ=');
