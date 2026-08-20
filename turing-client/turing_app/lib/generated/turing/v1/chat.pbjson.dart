// This is a generated file - do not edit.
//
// Generated from turing/v1/chat.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use sendMessageRequestDescriptor instead')
const SendMessageRequest$json = {
  '1': 'SendMessageRequest',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'content', '3': 2, '4': 1, '5': 9, '10': 'content'},
    {'1': 'content_type', '3': 3, '4': 1, '5': 9, '10': 'contentType'},
    {
      '1': 'agent_id',
      '3': 4,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.AgentId',
      '10': 'agentId'
    },
    {
      '1': 'model_provider',
      '3': 5,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ModelProvider',
      '10': 'modelProvider'
    },
    {'1': 'model', '3': 6, '4': 1, '5': 9, '10': 'model'},
    {'1': 'idempotency_key', '3': 7, '4': 1, '5': 9, '10': 'idempotencyKey'},
    {'1': 'requested_tools', '3': 8, '4': 3, '5': 9, '10': 'requestedTools'},
    {
      '1': 'required_context_tokens',
      '3': 9,
      '4': 1,
      '5': 5,
      '10': 'requiredContextTokens'
    },
    {
      '1': 'minimum_worker_max_concurrent_runs',
      '3': 10,
      '4': 1,
      '5': 5,
      '10': 'minimumWorkerMaxConcurrentRuns'
    },
  ],
};

/// Descriptor for `SendMessageRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List sendMessageRequestDescriptor = $convert.base64Decode(
    'ChJTZW5kTWVzc2FnZVJlcXVlc3QSHQoKc2Vzc2lvbl9pZBgBIAEoCVIJc2Vzc2lvbklkEhgKB2'
    'NvbnRlbnQYAiABKAlSB2NvbnRlbnQSIQoMY29udGVudF90eXBlGAMgASgJUgtjb250ZW50VHlw'
    'ZRItCghhZ2VudF9pZBgEIAEoDjISLnR1cmluZy52MS5BZ2VudElkUgdhZ2VudElkEj8KDm1vZG'
    'VsX3Byb3ZpZGVyGAUgASgOMhgudHVyaW5nLnYxLk1vZGVsUHJvdmlkZXJSDW1vZGVsUHJvdmlk'
    'ZXISFAoFbW9kZWwYBiABKAlSBW1vZGVsEicKD2lkZW1wb3RlbmN5X2tleRgHIAEoCVIOaWRlbX'
    'BvdGVuY3lLZXkSJwoPcmVxdWVzdGVkX3Rvb2xzGAggAygJUg5yZXF1ZXN0ZWRUb29scxI2Chdy'
    'ZXF1aXJlZF9jb250ZXh0X3Rva2VucxgJIAEoBVIVcmVxdWlyZWRDb250ZXh0VG9rZW5zEkoKIm'
    '1pbmltdW1fd29ya2VyX21heF9jb25jdXJyZW50X3J1bnMYCiABKAVSHm1pbmltdW1Xb3JrZXJN'
    'YXhDb25jdXJyZW50UnVucw==');

@$core.Deprecated('Use runQueuedDescriptor instead')
const RunQueued$json = {
  '1': 'RunQueued',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'job_id', '3': 2, '4': 1, '5': 9, '10': 'jobId'},
    {'1': 'trace_id', '3': 3, '4': 1, '5': 9, '10': 'traceId'},
    {
      '1': 'run_state',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunState',
      '10': 'runState'
    },
  ],
};

/// Descriptor for `RunQueued`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runQueuedDescriptor = $convert.base64Decode(
    'CglSdW5RdWV1ZWQSFQoGcnVuX2lkGAEgASgJUgVydW5JZBIVCgZqb2JfaWQYAiABKAlSBWpvYk'
    'lkEhkKCHRyYWNlX2lkGAMgASgJUgd0cmFjZUlkEjAKCXJ1bl9zdGF0ZRgEIAEoCzITLnR1cmlu'
    'Zy52MS5SdW5TdGF0ZVIIcnVuU3RhdGU=');

@$core.Deprecated('Use runStartedDescriptor instead')
const RunStarted$json = {
  '1': 'RunStarted',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'job_id', '3': 2, '4': 1, '5': 9, '10': 'jobId'},
    {'1': 'attempt', '3': 3, '4': 1, '5': 5, '10': 'attempt'},
    {
      '1': 'run_state',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunState',
      '10': 'runState'
    },
  ],
};

/// Descriptor for `RunStarted`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runStartedDescriptor = $convert.base64Decode(
    'CgpSdW5TdGFydGVkEhUKBnJ1bl9pZBgBIAEoCVIFcnVuSWQSFQoGam9iX2lkGAIgASgJUgVqb2'
    'JJZBIYCgdhdHRlbXB0GAMgASgFUgdhdHRlbXB0EjAKCXJ1bl9zdGF0ZRgEIAEoCzITLnR1cmlu'
    'Zy52MS5SdW5TdGF0ZVIIcnVuU3RhdGU=');

@$core.Deprecated('Use messageStartedDescriptor instead')
const MessageStarted$json = {
  '1': 'MessageStarted',
  '2': [
    {'1': 'message_id', '3': 1, '4': 1, '5': 9, '10': 'messageId'},
    {
      '1': 'role',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MessageRole',
      '10': 'role'
    },
  ],
};

/// Descriptor for `MessageStarted`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List messageStartedDescriptor = $convert.base64Decode(
    'Cg5NZXNzYWdlU3RhcnRlZBIdCgptZXNzYWdlX2lkGAEgASgJUgltZXNzYWdlSWQSKgoEcm9sZR'
    'gCIAEoDjIWLnR1cmluZy52MS5NZXNzYWdlUm9sZVIEcm9sZQ==');

@$core.Deprecated('Use tokenDeltaDescriptor instead')
const TokenDelta$json = {
  '1': 'TokenDelta',
  '2': [
    {'1': 'message_id', '3': 1, '4': 1, '5': 9, '10': 'messageId'},
    {'1': 'delta', '3': 2, '4': 1, '5': 9, '10': 'delta'},
  ],
};

/// Descriptor for `TokenDelta`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List tokenDeltaDescriptor = $convert.base64Decode(
    'CgpUb2tlbkRlbHRhEh0KCm1lc3NhZ2VfaWQYASABKAlSCW1lc3NhZ2VJZBIUCgVkZWx0YRgCIA'
    'EoCVIFZGVsdGE=');

@$core.Deprecated('Use toolEventDescriptor instead')
const ToolEvent$json = {
  '1': 'ToolEvent',
  '2': [
    {'1': 'tool_call_id', '3': 1, '4': 1, '5': 9, '10': 'toolCallId'},
    {'1': 'server_name', '3': 2, '4': 1, '5': 9, '10': 'serverName'},
    {'1': 'tool_name', '3': 3, '4': 1, '5': 9, '10': 'toolName'},
    {
      '1': 'payload',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'payload'
    },
  ],
};

/// Descriptor for `ToolEvent`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List toolEventDescriptor = $convert.base64Decode(
    'CglUb29sRXZlbnQSIAoMdG9vbF9jYWxsX2lkGAEgASgJUgp0b29sQ2FsbElkEh8KC3NlcnZlcl'
    '9uYW1lGAIgASgJUgpzZXJ2ZXJOYW1lEhsKCXRvb2xfbmFtZRgDIAEoCVIIdG9vbE5hbWUSMQoH'
    'cGF5bG9hZBgEIAEoCzIXLmdvb2dsZS5wcm90b2J1Zi5TdHJ1Y3RSB3BheWxvYWQ=');

@$core.Deprecated('Use approvalEventDescriptor instead')
const ApprovalEvent$json = {
  '1': 'ApprovalEvent',
  '2': [
    {'1': 'approval_id', '3': 1, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'tool_name', '3': 2, '4': 1, '5': 9, '10': 'toolName'},
    {'1': 'args_summary', '3': 3, '4': 1, '5': 9, '10': 'argsSummary'},
    {
      '1': 'run_state',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunState',
      '10': 'runState'
    },
  ],
};

/// Descriptor for `ApprovalEvent`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List approvalEventDescriptor = $convert.base64Decode(
    'Cg1BcHByb3ZhbEV2ZW50Eh8KC2FwcHJvdmFsX2lkGAEgASgJUgphcHByb3ZhbElkEhsKCXRvb2'
    'xfbmFtZRgCIAEoCVIIdG9vbE5hbWUSIQoMYXJnc19zdW1tYXJ5GAMgASgJUgthcmdzU3VtbWFy'
    'eRIwCglydW5fc3RhdGUYBCABKAsyEy50dXJpbmcudjEuUnVuU3RhdGVSCHJ1blN0YXRl');

@$core.Deprecated('Use messageCompletedDescriptor instead')
const MessageCompleted$json = {
  '1': 'MessageCompleted',
  '2': [
    {'1': 'message_id', '3': 1, '4': 1, '5': 9, '10': 'messageId'},
    {'1': 'content', '3': 2, '4': 1, '5': 9, '10': 'content'},
  ],
};

/// Descriptor for `MessageCompleted`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List messageCompletedDescriptor = $convert.base64Decode(
    'ChBNZXNzYWdlQ29tcGxldGVkEh0KCm1lc3NhZ2VfaWQYASABKAlSCW1lc3NhZ2VJZBIYCgdjb2'
    '50ZW50GAIgASgJUgdjb250ZW50');

@$core.Deprecated('Use runCompletedDescriptor instead')
const RunCompleted$json = {
  '1': 'RunCompleted',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
    {
      '1': 'assistant_message_id',
      '3': 2,
      '4': 1,
      '5': 9,
      '10': 'assistantMessageId'
    },
    {
      '1': 'run_state',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunState',
      '10': 'runState'
    },
  ],
};

/// Descriptor for `RunCompleted`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runCompletedDescriptor = $convert.base64Decode(
    'CgxSdW5Db21wbGV0ZWQSFQoGcnVuX2lkGAEgASgJUgVydW5JZBIwChRhc3Npc3RhbnRfbWVzc2'
    'FnZV9pZBgCIAEoCVISYXNzaXN0YW50TWVzc2FnZUlkEjAKCXJ1bl9zdGF0ZRgDIAEoCzITLnR1'
    'cmluZy52MS5SdW5TdGF0ZVIIcnVuU3RhdGU=');

@$core.Deprecated('Use runFailedDescriptor instead')
const RunFailed$json = {
  '1': 'RunFailed',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'code', '3': 2, '4': 1, '5': 9, '10': 'code'},
    {'1': 'message', '3': 3, '4': 1, '5': 9, '10': 'message'},
    {
      '1': 'retryable',
      '3': 4,
      '4': 1,
      '5': 8,
      '8': {'3': true},
      '10': 'retryable',
    },
    {
      '1': 'run_state',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunState',
      '10': 'runState'
    },
  ],
};

/// Descriptor for `RunFailed`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runFailedDescriptor = $convert.base64Decode(
    'CglSdW5GYWlsZWQSFQoGcnVuX2lkGAEgASgJUgVydW5JZBISCgRjb2RlGAIgASgJUgRjb2RlEh'
    'gKB21lc3NhZ2UYAyABKAlSB21lc3NhZ2USIAoJcmV0cnlhYmxlGAQgASgIQgIYAVIJcmV0cnlh'
    'YmxlEjAKCXJ1bl9zdGF0ZRgFIAEoCzITLnR1cmluZy52MS5SdW5TdGF0ZVIIcnVuU3RhdGU=');

@$core.Deprecated('Use runCancelledDescriptor instead')
const RunCancelled$json = {
  '1': 'RunCancelled',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'reason', '3': 2, '4': 1, '5': 9, '10': 'reason'},
    {
      '1': 'run_state',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunState',
      '10': 'runState'
    },
  ],
};

/// Descriptor for `RunCancelled`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runCancelledDescriptor = $convert.base64Decode(
    'CgxSdW5DYW5jZWxsZWQSFQoGcnVuX2lkGAEgASgJUgVydW5JZBIWCgZyZWFzb24YAiABKAlSBn'
    'JlYXNvbhIwCglydW5fc3RhdGUYAyABKAsyEy50dXJpbmcudjEuUnVuU3RhdGVSCHJ1blN0YXRl');

@$core.Deprecated('Use runStateChangedDescriptor instead')
const RunStateChanged$json = {
  '1': 'RunStateChanged',
  '2': [
    {
      '1': 'run_state',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunState',
      '10': 'runState'
    },
  ],
};

/// Descriptor for `RunStateChanged`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runStateChangedDescriptor = $convert.base64Decode(
    'Cg9SdW5TdGF0ZUNoYW5nZWQSMAoJcnVuX3N0YXRlGAEgASgLMhMudHVyaW5nLnYxLlJ1blN0YX'
    'RlUghydW5TdGF0ZQ==');

@$core.Deprecated('Use chatStreamEventDescriptor instead')
const ChatStreamEvent$json = {
  '1': 'ChatStreamEvent',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'run_id', '3': 2, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'trace_id', '3': 3, '4': 1, '5': 9, '10': 'traceId'},
    {'1': 'sequence', '3': 4, '4': 1, '5': 3, '10': 'sequence'},
    {
      '1': 'run_queued',
      '3': 10,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunQueued',
      '9': 0,
      '10': 'runQueued'
    },
    {
      '1': 'run_started',
      '3': 11,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunStarted',
      '9': 0,
      '10': 'runStarted'
    },
    {
      '1': 'message_started',
      '3': 12,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.MessageStarted',
      '9': 0,
      '10': 'messageStarted'
    },
    {
      '1': 'token_delta',
      '3': 13,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.TokenDelta',
      '9': 0,
      '10': 'tokenDelta'
    },
    {
      '1': 'tool_call_started',
      '3': 14,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.ToolEvent',
      '9': 0,
      '10': 'toolCallStarted'
    },
    {
      '1': 'tool_call_completed',
      '3': 15,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.ToolEvent',
      '9': 0,
      '10': 'toolCallCompleted'
    },
    {
      '1': 'tool_call_failed',
      '3': 16,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.ToolEvent',
      '9': 0,
      '10': 'toolCallFailed'
    },
    {
      '1': 'approval_requested',
      '3': 17,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.ApprovalEvent',
      '9': 0,
      '10': 'approvalRequested'
    },
    {
      '1': 'approval_approved',
      '3': 18,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.ApprovalEvent',
      '9': 0,
      '10': 'approvalApproved'
    },
    {
      '1': 'approval_denied',
      '3': 19,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.ApprovalEvent',
      '9': 0,
      '10': 'approvalDenied'
    },
    {
      '1': 'approval_expired',
      '3': 20,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.ApprovalEvent',
      '9': 0,
      '10': 'approvalExpired'
    },
    {
      '1': 'approval_consumed',
      '3': 21,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.ApprovalEvent',
      '9': 0,
      '10': 'approvalConsumed'
    },
    {
      '1': 'message_completed',
      '3': 22,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.MessageCompleted',
      '9': 0,
      '10': 'messageCompleted'
    },
    {
      '1': 'run_completed',
      '3': 23,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunCompleted',
      '9': 0,
      '10': 'runCompleted'
    },
    {
      '1': 'run_failed',
      '3': 24,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunFailed',
      '9': 0,
      '10': 'runFailed'
    },
    {
      '1': 'run_cancelled',
      '3': 25,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunCancelled',
      '9': 0,
      '10': 'runCancelled'
    },
    {
      '1': 'persisted_event',
      '3': 26,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.TuringEvent',
      '9': 0,
      '10': 'persistedEvent'
    },
    {
      '1': 'run_state_changed',
      '3': 27,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunStateChanged',
      '9': 0,
      '10': 'runStateChanged'
    },
  ],
  '8': [
    {'1': 'event'},
  ],
};

/// Descriptor for `ChatStreamEvent`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List chatStreamEventDescriptor = $convert.base64Decode(
    'Cg9DaGF0U3RyZWFtRXZlbnQSHQoKc2Vzc2lvbl9pZBgBIAEoCVIJc2Vzc2lvbklkEhUKBnJ1bl'
    '9pZBgCIAEoCVIFcnVuSWQSGQoIdHJhY2VfaWQYAyABKAlSB3RyYWNlSWQSGgoIc2VxdWVuY2UY'
    'BCABKANSCHNlcXVlbmNlEjUKCnJ1bl9xdWV1ZWQYCiABKAsyFC50dXJpbmcudjEuUnVuUXVldW'
    'VkSABSCXJ1blF1ZXVlZBI4CgtydW5fc3RhcnRlZBgLIAEoCzIVLnR1cmluZy52MS5SdW5TdGFy'
    'dGVkSABSCnJ1blN0YXJ0ZWQSRAoPbWVzc2FnZV9zdGFydGVkGAwgASgLMhkudHVyaW5nLnYxLk'
    '1lc3NhZ2VTdGFydGVkSABSDm1lc3NhZ2VTdGFydGVkEjgKC3Rva2VuX2RlbHRhGA0gASgLMhUu'
    'dHVyaW5nLnYxLlRva2VuRGVsdGFIAFIKdG9rZW5EZWx0YRJCChF0b29sX2NhbGxfc3RhcnRlZB'
    'gOIAEoCzIULnR1cmluZy52MS5Ub29sRXZlbnRIAFIPdG9vbENhbGxTdGFydGVkEkYKE3Rvb2xf'
    'Y2FsbF9jb21wbGV0ZWQYDyABKAsyFC50dXJpbmcudjEuVG9vbEV2ZW50SABSEXRvb2xDYWxsQ2'
    '9tcGxldGVkEkAKEHRvb2xfY2FsbF9mYWlsZWQYECABKAsyFC50dXJpbmcudjEuVG9vbEV2ZW50'
    'SABSDnRvb2xDYWxsRmFpbGVkEkkKEmFwcHJvdmFsX3JlcXVlc3RlZBgRIAEoCzIYLnR1cmluZy'
    '52MS5BcHByb3ZhbEV2ZW50SABSEWFwcHJvdmFsUmVxdWVzdGVkEkcKEWFwcHJvdmFsX2FwcHJv'
    'dmVkGBIgASgLMhgudHVyaW5nLnYxLkFwcHJvdmFsRXZlbnRIAFIQYXBwcm92YWxBcHByb3ZlZB'
    'JDCg9hcHByb3ZhbF9kZW5pZWQYEyABKAsyGC50dXJpbmcudjEuQXBwcm92YWxFdmVudEgAUg5h'
    'cHByb3ZhbERlbmllZBJFChBhcHByb3ZhbF9leHBpcmVkGBQgASgLMhgudHVyaW5nLnYxLkFwcH'
    'JvdmFsRXZlbnRIAFIPYXBwcm92YWxFeHBpcmVkEkcKEWFwcHJvdmFsX2NvbnN1bWVkGBUgASgL'
    'MhgudHVyaW5nLnYxLkFwcHJvdmFsRXZlbnRIAFIQYXBwcm92YWxDb25zdW1lZBJKChFtZXNzYW'
    'dlX2NvbXBsZXRlZBgWIAEoCzIbLnR1cmluZy52MS5NZXNzYWdlQ29tcGxldGVkSABSEG1lc3Nh'
    'Z2VDb21wbGV0ZWQSPgoNcnVuX2NvbXBsZXRlZBgXIAEoCzIXLnR1cmluZy52MS5SdW5Db21wbG'
    'V0ZWRIAFIMcnVuQ29tcGxldGVkEjUKCnJ1bl9mYWlsZWQYGCABKAsyFC50dXJpbmcudjEuUnVu'
    'RmFpbGVkSABSCXJ1bkZhaWxlZBI+Cg1ydW5fY2FuY2VsbGVkGBkgASgLMhcudHVyaW5nLnYxLl'
    'J1bkNhbmNlbGxlZEgAUgxydW5DYW5jZWxsZWQSQQoPcGVyc2lzdGVkX2V2ZW50GBogASgLMhYu'
    'dHVyaW5nLnYxLlR1cmluZ0V2ZW50SABSDnBlcnNpc3RlZEV2ZW50EkgKEXJ1bl9zdGF0ZV9jaG'
    'FuZ2VkGBsgASgLMhoudHVyaW5nLnYxLlJ1blN0YXRlQ2hhbmdlZEgAUg9ydW5TdGF0ZUNoYW5n'
    'ZWRCBwoFZXZlbnQ=');
