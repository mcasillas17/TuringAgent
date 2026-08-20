// This is a generated file - do not edit.
//
// Generated from turing/v1/common.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use agentIdDescriptor instead')
const AgentId$json = {
  '1': 'AgentId',
  '2': [
    {'1': 'AGENT_ID_UNSPECIFIED', '2': 0},
    {'1': 'AGENT_ID_GENERAL_ASSISTANT', '2': 1},
  ],
};

/// Descriptor for `AgentId`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List agentIdDescriptor = $convert.base64Decode(
    'CgdBZ2VudElkEhgKFEFHRU5UX0lEX1VOU1BFQ0lGSUVEEAASHgoaQUdFTlRfSURfR0VORVJBTF'
    '9BU1NJU1RBTlQQAQ==');

@$core.Deprecated('Use modelProviderDescriptor instead')
const ModelProvider$json = {
  '1': 'ModelProvider',
  '2': [
    {'1': 'MODEL_PROVIDER_UNSPECIFIED', '2': 0},
    {'1': 'MODEL_PROVIDER_OLLAMA', '2': 1},
    {'1': 'MODEL_PROVIDER_OPENAI_COMPATIBLE', '2': 2},
  ],
};

/// Descriptor for `ModelProvider`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List modelProviderDescriptor = $convert.base64Decode(
    'Cg1Nb2RlbFByb3ZpZGVyEh4KGk1PREVMX1BST1ZJREVSX1VOU1BFQ0lGSUVEEAASGQoVTU9ERU'
    'xfUFJPVklERVJfT0xMQU1BEAESJAogTU9ERUxfUFJPVklERVJfT1BFTkFJX0NPTVBBVElCTEUQ'
    'Ag==');

@$core.Deprecated('Use routingRequirementKindDescriptor instead')
const RoutingRequirementKind$json = {
  '1': 'RoutingRequirementKind',
  '2': [
    {'1': 'ROUTING_REQUIREMENT_KIND_UNSPECIFIED', '2': 0},
    {'1': 'ROUTING_REQUIREMENT_KIND_PROVIDER', '2': 1},
    {'1': 'ROUTING_REQUIREMENT_KIND_MODEL', '2': 2},
    {'1': 'ROUTING_REQUIREMENT_KIND_CONTEXT', '2': 3},
    {'1': 'ROUTING_REQUIREMENT_KIND_TOOL', '2': 4},
    {'1': 'ROUTING_REQUIREMENT_KIND_AGENT', '2': 5},
    {'1': 'ROUTING_REQUIREMENT_KIND_CAPACITY', '2': 6},
    {'1': 'ROUTING_REQUIREMENT_KIND_EXTERNAL_AGENT_CREDENTIAL', '2': 7},
  ],
};

/// Descriptor for `RoutingRequirementKind`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List routingRequirementKindDescriptor = $convert.base64Decode(
    'ChZSb3V0aW5nUmVxdWlyZW1lbnRLaW5kEigKJFJPVVRJTkdfUkVRVUlSRU1FTlRfS0lORF9VTl'
    'NQRUNJRklFRBAAEiUKIVJPVVRJTkdfUkVRVUlSRU1FTlRfS0lORF9QUk9WSURFUhABEiIKHlJP'
    'VVRJTkdfUkVRVUlSRU1FTlRfS0lORF9NT0RFTBACEiQKIFJPVVRJTkdfUkVRVUlSRU1FTlRfS0'
    'lORF9DT05URVhUEAMSIQodUk9VVElOR19SRVFVSVJFTUVOVF9LSU5EX1RPT0wQBBIiCh5ST1VU'
    'SU5HX1JFUVVJUkVNRU5UX0tJTkRfQUdFTlQQBRIlCiFST1VUSU5HX1JFUVVJUkVNRU5UX0tJTk'
    'RfQ0FQQUNJVFkQBhI2CjJST1VUSU5HX1JFUVVJUkVNRU5UX0tJTkRfRVhURVJOQUxfQUdFTlRf'
    'Q1JFREVOVElBTBAH');

@$core.Deprecated('Use messageRoleDescriptor instead')
const MessageRole$json = {
  '1': 'MessageRole',
  '2': [
    {'1': 'MESSAGE_ROLE_UNSPECIFIED', '2': 0},
    {'1': 'MESSAGE_ROLE_SYSTEM', '2': 1},
    {'1': 'MESSAGE_ROLE_USER', '2': 2},
    {'1': 'MESSAGE_ROLE_ASSISTANT', '2': 3},
    {'1': 'MESSAGE_ROLE_TOOL', '2': 4},
  ],
};

/// Descriptor for `MessageRole`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List messageRoleDescriptor = $convert.base64Decode(
    'CgtNZXNzYWdlUm9sZRIcChhNRVNTQUdFX1JPTEVfVU5TUEVDSUZJRUQQABIXChNNRVNTQUdFX1'
    'JPTEVfU1lTVEVNEAESFQoRTUVTU0FHRV9ST0xFX1VTRVIQAhIaChZNRVNTQUdFX1JPTEVfQVNT'
    'SVNUQU5UEAMSFQoRTUVTU0FHRV9ST0xFX1RPT0wQBA==');

@$core.Deprecated('Use toolPolicyDescriptor instead')
const ToolPolicy$json = {
  '1': 'ToolPolicy',
  '2': [
    {'1': 'TOOL_POLICY_UNSPECIFIED', '2': 0},
    {'1': 'TOOL_POLICY_SAFE', '2': 1},
    {'1': 'TOOL_POLICY_APPROVAL_REQUIRED', '2': 2},
    {'1': 'TOOL_POLICY_DISABLED', '2': 3},
  ],
};

/// Descriptor for `ToolPolicy`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List toolPolicyDescriptor = $convert.base64Decode(
    'CgpUb29sUG9saWN5EhsKF1RPT0xfUE9MSUNZX1VOU1BFQ0lGSUVEEAASFAoQVE9PTF9QT0xJQ1'
    'lfU0FGRRABEiEKHVRPT0xfUE9MSUNZX0FQUFJPVkFMX1JFUVVJUkVEEAISGAoUVE9PTF9QT0xJ'
    'Q1lfRElTQUJMRUQQAw==');

@$core.Deprecated('Use runStatusDescriptor instead')
const RunStatus$json = {
  '1': 'RunStatus',
  '2': [
    {'1': 'RUN_STATUS_UNSPECIFIED', '2': 0},
    {'1': 'RUN_STATUS_QUEUED', '2': 1},
    {'1': 'RUN_STATUS_RUNNING', '2': 2},
    {'1': 'RUN_STATUS_WAITING_APPROVAL', '2': 3},
    {'1': 'RUN_STATUS_COMPLETED', '2': 4},
    {'1': 'RUN_STATUS_FAILED', '2': 5},
    {'1': 'RUN_STATUS_CANCELLED', '2': 6},
  ],
};

/// Descriptor for `RunStatus`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List runStatusDescriptor = $convert.base64Decode(
    'CglSdW5TdGF0dXMSGgoWUlVOX1NUQVRVU19VTlNQRUNJRklFRBAAEhUKEVJVTl9TVEFUVVNfUV'
    'VFVUVEEAESFgoSUlVOX1NUQVRVU19SVU5OSU5HEAISHwobUlVOX1NUQVRVU19XQUlUSU5HX0FQ'
    'UFJPVkFMEAMSGAoUUlVOX1NUQVRVU19DT01QTEVURUQQBBIVChFSVU5fU1RBVFVTX0ZBSUxFRB'
    'AFEhgKFFJVTl9TVEFUVVNfQ0FOQ0VMTEVEEAY=');

@$core.Deprecated('Use runLifecycleDescriptor instead')
const RunLifecycle$json = {
  '1': 'RunLifecycle',
  '2': [
    {'1': 'RUN_LIFECYCLE_UNSPECIFIED', '2': 0},
    {'1': 'RUN_LIFECYCLE_UNKNOWN', '2': 1},
    {'1': 'RUN_LIFECYCLE_QUEUED', '2': 2},
    {'1': 'RUN_LIFECYCLE_RUNNING', '2': 3},
    {'1': 'RUN_LIFECYCLE_WAITING_APPROVAL', '2': 4},
    {'1': 'RUN_LIFECYCLE_RECOVERING', '2': 5},
    {'1': 'RUN_LIFECYCLE_COMPLETED', '2': 6},
    {'1': 'RUN_LIFECYCLE_FAILED', '2': 7},
    {'1': 'RUN_LIFECYCLE_CANCELLED', '2': 8},
  ],
};

/// Descriptor for `RunLifecycle`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List runLifecycleDescriptor = $convert.base64Decode(
    'CgxSdW5MaWZlY3ljbGUSHQoZUlVOX0xJRkVDWUNMRV9VTlNQRUNJRklFRBAAEhkKFVJVTl9MSU'
    'ZFQ1lDTEVfVU5LTk9XThABEhgKFFJVTl9MSUZFQ1lDTEVfUVVFVUVEEAISGQoVUlVOX0xJRkVD'
    'WUNMRV9SVU5OSU5HEAMSIgoeUlVOX0xJRkVDWUNMRV9XQUlUSU5HX0FQUFJPVkFMEAQSHAoYUl'
    'VOX0xJRkVDWUNMRV9SRUNPVkVSSU5HEAUSGwoXUlVOX0xJRkVDWUNMRV9DT01QTEVURUQQBhIY'
    'ChRSVU5fTElGRUNZQ0xFX0ZBSUxFRBAHEhsKF1JVTl9MSUZFQ1lDTEVfQ0FOQ0VMTEVEEAg=');

@$core.Deprecated('Use runOutcomeReasonDescriptor instead')
const RunOutcomeReason$json = {
  '1': 'RunOutcomeReason',
  '2': [
    {'1': 'RUN_OUTCOME_REASON_UNSPECIFIED', '2': 0},
    {'1': 'RUN_OUTCOME_REASON_UNKNOWN', '2': 1},
    {'1': 'RUN_OUTCOME_REASON_NONE', '2': 2},
    {'1': 'RUN_OUTCOME_REASON_COMPLETED_NO_CONTENT', '2': 3},
    {'1': 'RUN_OUTCOME_REASON_USER_CANCELLED', '2': 4},
    {'1': 'RUN_OUTCOME_REASON_ABANDONED', '2': 5},
    {'1': 'RUN_OUTCOME_REASON_EXPIRED', '2': 6},
    {'1': 'RUN_OUTCOME_REASON_CONTEXT_LIMIT', '2': 7},
    {'1': 'RUN_OUTCOME_REASON_PROVIDER_FAILURE', '2': 8},
    {'1': 'RUN_OUTCOME_REASON_TOOL_FAILURE', '2': 9},
    {'1': 'RUN_OUTCOME_REASON_POLICY_DENIED', '2': 10},
    {'1': 'RUN_OUTCOME_REASON_RETRIES_EXHAUSTED', '2': 11},
    {'1': 'RUN_OUTCOME_REASON_RECOVERY_INTERRUPTED', '2': 12},
    {'1': 'RUN_OUTCOME_REASON_SIDE_EFFECT_UNCERTAIN', '2': 13},
    {'1': 'RUN_OUTCOME_REASON_APPROVAL_DELIVERY_FAILED', '2': 14},
    {'1': 'RUN_OUTCOME_REASON_INTERNAL_FAILURE', '2': 15},
    {'1': 'RUN_OUTCOME_REASON_LEGACY_UNKNOWN', '2': 16},
  ],
};

/// Descriptor for `RunOutcomeReason`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List runOutcomeReasonDescriptor = $convert.base64Decode(
    'ChBSdW5PdXRjb21lUmVhc29uEiIKHlJVTl9PVVRDT01FX1JFQVNPTl9VTlNQRUNJRklFRBAAEh'
    '4KGlJVTl9PVVRDT01FX1JFQVNPTl9VTktOT1dOEAESGwoXUlVOX09VVENPTUVfUkVBU09OX05P'
    'TkUQAhIrCidSVU5fT1VUQ09NRV9SRUFTT05fQ09NUExFVEVEX05PX0NPTlRFTlQQAxIlCiFSVU'
    '5fT1VUQ09NRV9SRUFTT05fVVNFUl9DQU5DRUxMRUQQBBIgChxSVU5fT1VUQ09NRV9SRUFTT05f'
    'QUJBTkRPTkVEEAUSHgoaUlVOX09VVENPTUVfUkVBU09OX0VYUElSRUQQBhIkCiBSVU5fT1VUQ0'
    '9NRV9SRUFTT05fQ09OVEVYVF9MSU1JVBAHEicKI1JVTl9PVVRDT01FX1JFQVNPTl9QUk9WSURF'
    'Ul9GQUlMVVJFEAgSIwofUlVOX09VVENPTUVfUkVBU09OX1RPT0xfRkFJTFVSRRAJEiQKIFJVTl'
    '9PVVRDT01FX1JFQVNPTl9QT0xJQ1lfREVOSUVEEAoSKAokUlVOX09VVENPTUVfUkVBU09OX1JF'
    'VFJJRVNfRVhIQVVTVEVEEAsSKwonUlVOX09VVENPTUVfUkVBU09OX1JFQ09WRVJZX0lOVEVSUl'
    'VQVEVEEAwSLAooUlVOX09VVENPTUVfUkVBU09OX1NJREVfRUZGRUNUX1VOQ0VSVEFJThANEi8K'
    'K1JVTl9PVVRDT01FX1JFQVNPTl9BUFBST1ZBTF9ERUxJVkVSWV9GQUlMRUQQDhInCiNSVU5fT1'
    'VUQ09NRV9SRUFTT05fSU5URVJOQUxfRkFJTFVSRRAPEiUKIVJVTl9PVVRDT01FX1JFQVNPTl9M'
    'RUdBQ1lfVU5LTk9XThAQ');

@$core.Deprecated('Use runStateDescriptor instead')
const RunState$json = {
  '1': 'RunState',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'user_message_id', '3': 2, '4': 1, '5': 9, '10': 'userMessageId'},
    {
      '1': 'assistant_message_id',
      '3': 3,
      '4': 1,
      '5': 9,
      '10': 'assistantMessageId'
    },
    {
      '1': 'lifecycle',
      '3': 4,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.RunLifecycle',
      '10': 'lifecycle'
    },
    {
      '1': 'outcome_reason',
      '3': 5,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.RunOutcomeReason',
      '10': 'outcomeReason'
    },
    {'1': 'state_version', '3': 6, '4': 1, '5': 3, '10': 'stateVersion'},
    {
      '1': 'state_updated_at',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'stateUpdatedAt'
    },
    {
      '1': 'finished_at',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'finishedAt'
    },
    {
      '1': 'has_displayable_content',
      '3': 9,
      '4': 1,
      '5': 8,
      '10': 'hasDisplayableContent'
    },
  ],
};

/// Descriptor for `RunState`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runStateDescriptor = $convert.base64Decode(
    'CghSdW5TdGF0ZRIVCgZydW5faWQYASABKAlSBXJ1bklkEiYKD3VzZXJfbWVzc2FnZV9pZBgCIA'
    'EoCVINdXNlck1lc3NhZ2VJZBIwChRhc3Npc3RhbnRfbWVzc2FnZV9pZBgDIAEoCVISYXNzaXN0'
    'YW50TWVzc2FnZUlkEjUKCWxpZmVjeWNsZRgEIAEoDjIXLnR1cmluZy52MS5SdW5MaWZlY3ljbG'
    'VSCWxpZmVjeWNsZRJCCg5vdXRjb21lX3JlYXNvbhgFIAEoDjIbLnR1cmluZy52MS5SdW5PdXRj'
    'b21lUmVhc29uUg1vdXRjb21lUmVhc29uEiMKDXN0YXRlX3ZlcnNpb24YBiABKANSDHN0YXRlVm'
    'Vyc2lvbhJEChBzdGF0ZV91cGRhdGVkX2F0GAcgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVz'
    'dGFtcFIOc3RhdGVVcGRhdGVkQXQSOwoLZmluaXNoZWRfYXQYCCABKAsyGi5nb29nbGUucHJvdG'
    '9idWYuVGltZXN0YW1wUgpmaW5pc2hlZEF0EjYKF2hhc19kaXNwbGF5YWJsZV9jb250ZW50GAkg'
    'ASgIUhVoYXNEaXNwbGF5YWJsZUNvbnRlbnQ=');

@$core.Deprecated('Use requestMetadataDescriptor instead')
const RequestMetadata$json = {
  '1': 'RequestMetadata',
  '2': [
    {'1': 'request_id', '3': 1, '4': 1, '5': 9, '10': 'requestId'},
  ],
};

/// Descriptor for `RequestMetadata`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List requestMetadataDescriptor = $convert.base64Decode(
    'Cg9SZXF1ZXN0TWV0YWRhdGESHQoKcmVxdWVzdF9pZBgBIAEoCVIJcmVxdWVzdElk');

@$core.Deprecated('Use pageRequestDescriptor instead')
const PageRequest$json = {
  '1': 'PageRequest',
  '2': [
    {'1': 'limit', '3': 1, '4': 1, '5': 5, '10': 'limit'},
    {'1': 'cursor', '3': 2, '4': 1, '5': 9, '10': 'cursor'},
  ],
};

/// Descriptor for `PageRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List pageRequestDescriptor = $convert.base64Decode(
    'CgtQYWdlUmVxdWVzdBIUCgVsaW1pdBgBIAEoBVIFbGltaXQSFgoGY3Vyc29yGAIgASgJUgZjdX'
    'Jzb3I=');

@$core.Deprecated('Use pageResponseDescriptor instead')
const PageResponse$json = {
  '1': 'PageResponse',
  '2': [
    {'1': 'next_cursor', '3': 1, '4': 1, '5': 9, '10': 'nextCursor'},
  ],
};

/// Descriptor for `PageResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List pageResponseDescriptor = $convert.base64Decode(
    'CgxQYWdlUmVzcG9uc2USHwoLbmV4dF9jdXJzb3IYASABKAlSCm5leHRDdXJzb3I=');

@$core.Deprecated('Use errorDetailDescriptor instead')
const ErrorDetail$json = {
  '1': 'ErrorDetail',
  '2': [
    {'1': 'code', '3': 1, '4': 1, '5': 9, '10': 'code'},
    {'1': 'message', '3': 2, '4': 1, '5': 9, '10': 'message'},
    {'1': 'request_id', '3': 3, '4': 1, '5': 9, '10': 'requestId'},
    {
      '1': 'details',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'details'
    },
  ],
};

/// Descriptor for `ErrorDetail`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List errorDetailDescriptor = $convert.base64Decode(
    'CgtFcnJvckRldGFpbBISCgRjb2RlGAEgASgJUgRjb2RlEhgKB21lc3NhZ2UYAiABKAlSB21lc3'
    'NhZ2USHQoKcmVxdWVzdF9pZBgDIAEoCVIJcmVxdWVzdElkEjEKB2RldGFpbHMYBCABKAsyFy5n'
    'b29nbGUucHJvdG9idWYuU3RydWN0UgdkZXRhaWxz');

@$core.Deprecated('Use routingUnavailableDetailDescriptor instead')
const RoutingUnavailableDetail$json = {
  '1': 'RoutingUnavailableDetail',
  '2': [
    {
      '1': 'kind',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.RoutingRequirementKind',
      '10': 'kind'
    },
    {'1': 'requested', '3': 2, '4': 1, '5': 9, '10': 'requested'},
    {'1': 'available', '3': 3, '4': 3, '5': 9, '10': 'available'},
  ],
};

/// Descriptor for `RoutingUnavailableDetail`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List routingUnavailableDetailDescriptor = $convert.base64Decode(
    'ChhSb3V0aW5nVW5hdmFpbGFibGVEZXRhaWwSNQoEa2luZBgBIAEoDjIhLnR1cmluZy52MS5Sb3'
    'V0aW5nUmVxdWlyZW1lbnRLaW5kUgRraW5kEhwKCXJlcXVlc3RlZBgCIAEoCVIJcmVxdWVzdGVk'
    'EhwKCWF2YWlsYWJsZRgDIAMoCVIJYXZhaWxhYmxl');

@$core.Deprecated('Use modelCapabilityDescriptor instead')
const ModelCapability$json = {
  '1': 'ModelCapability',
  '2': [
    {
      '1': 'provider',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ModelProvider',
      '10': 'provider'
    },
    {'1': 'model', '3': 2, '4': 1, '5': 9, '10': 'model'},
    {
      '1': 'max_context_tokens',
      '3': 3,
      '4': 1,
      '5': 5,
      '10': 'maxContextTokens'
    },
  ],
};

/// Descriptor for `ModelCapability`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List modelCapabilityDescriptor = $convert.base64Decode(
    'Cg9Nb2RlbENhcGFiaWxpdHkSNAoIcHJvdmlkZXIYASABKA4yGC50dXJpbmcudjEuTW9kZWxQcm'
    '92aWRlclIIcHJvdmlkZXISFAoFbW9kZWwYAiABKAlSBW1vZGVsEiwKEm1heF9jb250ZXh0X3Rv'
    'a2VucxgDIAEoBVIQbWF4Q29udGV4dFRva2Vucw==');

@$core.Deprecated('Use providerConfigDescriptor instead')
const ProviderConfig$json = {
  '1': 'ProviderConfig',
  '2': [
    {
      '1': 'provider',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ModelProvider',
      '10': 'provider'
    },
    {'1': 'enabled', '3': 2, '4': 1, '5': 8, '10': 'enabled'},
    {'1': 'default_model', '3': 3, '4': 1, '5': 9, '10': 'defaultModel'},
    {
      '1': 'models',
      '3': 4,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.ModelCapability',
      '10': 'models'
    },
  ],
};

/// Descriptor for `ProviderConfig`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List providerConfigDescriptor = $convert.base64Decode(
    'Cg5Qcm92aWRlckNvbmZpZxI0Cghwcm92aWRlchgBIAEoDjIYLnR1cmluZy52MS5Nb2RlbFByb3'
    'ZpZGVyUghwcm92aWRlchIYCgdlbmFibGVkGAIgASgIUgdlbmFibGVkEiMKDWRlZmF1bHRfbW9k'
    'ZWwYAyABKAlSDGRlZmF1bHRNb2RlbBIyCgZtb2RlbHMYBCADKAsyGi50dXJpbmcudjEuTW9kZW'
    'xDYXBhYmlsaXR5UgZtb2RlbHM=');

@$core.Deprecated('Use agentDescriptorDescriptor instead')
const AgentDescriptor$json = {
  '1': 'AgentDescriptor',
  '2': [
    {'1': 'id', '3': 1, '4': 1, '5': 14, '6': '.turing.v1.AgentId', '10': 'id'},
    {'1': 'display_name', '3': 2, '4': 1, '5': 9, '10': 'displayName'},
    {'1': 'available', '3': 3, '4': 1, '5': 8, '10': 'available'},
  ],
};

/// Descriptor for `AgentDescriptor`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List agentDescriptorDescriptor = $convert.base64Decode(
    'Cg9BZ2VudERlc2NyaXB0b3ISIgoCaWQYASABKA4yEi50dXJpbmcudjEuQWdlbnRJZFICaWQSIQ'
    'oMZGlzcGxheV9uYW1lGAIgASgJUgtkaXNwbGF5TmFtZRIcCglhdmFpbGFibGUYAyABKAhSCWF2'
    'YWlsYWJsZQ==');

@$core.Deprecated('Use messageDescriptor instead')
const Message$json = {
  '1': 'Message',
  '2': [
    {'1': 'message_id', '3': 1, '4': 1, '5': 9, '10': 'messageId'},
    {'1': 'session_id', '3': 2, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'run_id', '3': 3, '4': 1, '5': 9, '10': 'runId'},
    {
      '1': 'role',
      '3': 4,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MessageRole',
      '10': 'role'
    },
    {'1': 'content', '3': 5, '4': 1, '5': 9, '10': 'content'},
    {'1': 'content_type', '3': 6, '4': 1, '5': 9, '10': 'contentType'},
    {'1': 'sequence', '3': 7, '4': 1, '5': 3, '10': 'sequence'},
    {
      '1': 'created_at',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'createdAt'
    },
    {
      '1': 'run_state',
      '3': 9,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunState',
      '10': 'runState'
    },
  ],
};

/// Descriptor for `Message`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List messageDescriptor = $convert.base64Decode(
    'CgdNZXNzYWdlEh0KCm1lc3NhZ2VfaWQYASABKAlSCW1lc3NhZ2VJZBIdCgpzZXNzaW9uX2lkGA'
    'IgASgJUglzZXNzaW9uSWQSFQoGcnVuX2lkGAMgASgJUgVydW5JZBIqCgRyb2xlGAQgASgOMhYu'
    'dHVyaW5nLnYxLk1lc3NhZ2VSb2xlUgRyb2xlEhgKB2NvbnRlbnQYBSABKAlSB2NvbnRlbnQSIQ'
    'oMY29udGVudF90eXBlGAYgASgJUgtjb250ZW50VHlwZRIaCghzZXF1ZW5jZRgHIAEoA1IIc2Vx'
    'dWVuY2USOQoKY3JlYXRlZF9hdBgIIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSCW'
    'NyZWF0ZWRBdBIwCglydW5fc3RhdGUYCSABKAsyEy50dXJpbmcudjEuUnVuU3RhdGVSCHJ1blN0'
    'YXRl');
