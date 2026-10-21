// This is a generated file - do not edit.
//
// Generated from turing/v1/team.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports
// ignore_for_file: unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use agentProfileStateDescriptor instead')
const AgentProfileState$json = {
  '1': 'AgentProfileState',
  '2': [
    {'1': 'AGENT_PROFILE_STATE_UNSPECIFIED', '2': 0},
    {'1': 'AGENT_PROFILE_STATE_ACTIVE', '2': 1},
    {'1': 'AGENT_PROFILE_STATE_DISABLED', '2': 2},
    {'1': 'AGENT_PROFILE_STATE_NEEDS_GRANT', '2': 3},
    {'1': 'AGENT_PROFILE_STATE_UNAVAILABLE', '2': 4},
    {'1': 'AGENT_PROFILE_STATE_PARSE_ERROR', '2': 5},
  ],
};

/// Descriptor for `AgentProfileState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List agentProfileStateDescriptor = $convert.base64Decode(
    'ChFBZ2VudFByb2ZpbGVTdGF0ZRIjCh9BR0VOVF9QUk9GSUxFX1NUQVRFX1VOU1BFQ0lGSUVEEA'
    'ASHgoaQUdFTlRfUFJPRklMRV9TVEFURV9BQ1RJVkUQARIgChxBR0VOVF9QUk9GSUxFX1NUQVRF'
    'X0RJU0FCTEVEEAISIwofQUdFTlRfUFJPRklMRV9TVEFURV9ORUVEU19HUkFOVBADEiMKH0FHRU'
    '5UX1BST0ZJTEVfU1RBVEVfVU5BVkFJTEFCTEUQBBIjCh9BR0VOVF9QUk9GSUxFX1NUQVRFX1BB'
    'UlNFX0VSUk9SEAU=');

@$core.Deprecated('Use agentProfileMemoryAccessDescriptor instead')
const AgentProfileMemoryAccess$json = {
  '1': 'AgentProfileMemoryAccess',
  '2': [
    {'1': 'AGENT_PROFILE_MEMORY_ACCESS_UNSPECIFIED', '2': 0},
    {'1': 'AGENT_PROFILE_MEMORY_ACCESS_NONE', '2': 1},
    {'1': 'AGENT_PROFILE_MEMORY_ACCESS_READ', '2': 2},
    {'1': 'AGENT_PROFILE_MEMORY_ACCESS_PROPOSE', '2': 3},
  ],
};

/// Descriptor for `AgentProfileMemoryAccess`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List agentProfileMemoryAccessDescriptor = $convert.base64Decode(
    'ChhBZ2VudFByb2ZpbGVNZW1vcnlBY2Nlc3MSKwonQUdFTlRfUFJPRklMRV9NRU1PUllfQUNDRV'
    'NTX1VOU1BFQ0lGSUVEEAASJAogQUdFTlRfUFJPRklMRV9NRU1PUllfQUNDRVNTX05PTkUQARIk'
    'CiBBR0VOVF9QUk9GSUxFX01FTU9SWV9BQ0NFU1NfUkVBRBACEicKI0FHRU5UX1BST0ZJTEVfTU'
    'VNT1JZX0FDQ0VTU19QUk9QT1NFEAM=');

@$core.Deprecated('Use agentProfileToolExclusionDescriptor instead')
const AgentProfileToolExclusion$json = {
  '1': 'AgentProfileToolExclusion',
  '2': [
    {'1': 'tool', '3': 1, '4': 1, '5': 9, '10': 'tool'},
    {'1': 'reason', '3': 2, '4': 1, '5': 9, '10': 'reason'},
  ],
};

/// Descriptor for `AgentProfileToolExclusion`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List agentProfileToolExclusionDescriptor =
    $convert.base64Decode(
        'ChlBZ2VudFByb2ZpbGVUb29sRXhjbHVzaW9uEhIKBHRvb2wYASABKAlSBHRvb2wSFgoGcmVhc2'
        '9uGAIgASgJUgZyZWFzb24=');

@$core.Deprecated('Use agentProfileDescriptor instead')
const AgentProfile$json = {
  '1': 'AgentProfile',
  '2': [
    {'1': 'profile_id', '3': 1, '4': 1, '5': 9, '10': 'profileId'},
    {'1': 'name', '3': 2, '4': 1, '5': 9, '10': 'name'},
    {'1': 'emoji', '3': 3, '4': 1, '5': 9, '10': 'emoji'},
    {'1': 'description', '3': 4, '4': 1, '5': 9, '10': 'description'},
    {'1': 'version', '3': 5, '4': 1, '5': 9, '10': 'version'},
    {'1': 'model', '3': 6, '4': 1, '5': 9, '10': 'model'},
    {'1': 'resolved_model', '3': 7, '4': 1, '5': 9, '10': 'resolvedModel'},
    {'1': 'tools', '3': 8, '4': 3, '5': 9, '10': 'tools'},
    {'1': 'skills', '3': 9, '4': 3, '5': 9, '10': 'skills'},
    {
      '1': 'memory',
      '3': 10,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.AgentProfileMemoryAccess',
      '10': 'memory'
    },
    {'1': 'requires', '3': 11, '4': 3, '5': 9, '10': 'requires'},
    {'1': 'max_tool_calls', '3': 12, '4': 1, '5': 5, '10': 'maxToolCalls'},
    {'1': 'revision', '3': 13, '4': 1, '5': 9, '10': 'revision'},
    {'1': 'enabled', '3': 14, '4': 1, '5': 8, '10': 'enabled'},
    {'1': 'granted_revision', '3': 15, '4': 1, '5': 9, '10': 'grantedRevision'},
    {
      '1': 'state',
      '3': 16,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.AgentProfileState',
      '10': 'state'
    },
    {'1': 'resolved_tools', '3': 17, '4': 3, '5': 9, '10': 'resolvedTools'},
    {
      '1': 'unavailable_reasons',
      '3': 18,
      '4': 3,
      '5': 9,
      '10': 'unavailableReasons'
    },
    {
      '1': 'excluded_tools',
      '3': 19,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.AgentProfileToolExclusion',
      '10': 'excludedTools'
    },
    {'1': 'parse_error', '3': 20, '4': 1, '5': 9, '10': 'parseError'},
    {'1': 'consent_tools', '3': 21, '4': 3, '5': 9, '10': 'consentTools'},
  ],
};

/// Descriptor for `AgentProfile`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List agentProfileDescriptor = $convert.base64Decode(
    'CgxBZ2VudFByb2ZpbGUSHQoKcHJvZmlsZV9pZBgBIAEoCVIJcHJvZmlsZUlkEhIKBG5hbWUYAi'
    'ABKAlSBG5hbWUSFAoFZW1vamkYAyABKAlSBWVtb2ppEiAKC2Rlc2NyaXB0aW9uGAQgASgJUgtk'
    'ZXNjcmlwdGlvbhIYCgd2ZXJzaW9uGAUgASgJUgd2ZXJzaW9uEhQKBW1vZGVsGAYgASgJUgVtb2'
    'RlbBIlCg5yZXNvbHZlZF9tb2RlbBgHIAEoCVINcmVzb2x2ZWRNb2RlbBIUCgV0b29scxgIIAMo'
    'CVIFdG9vbHMSFgoGc2tpbGxzGAkgAygJUgZza2lsbHMSOwoGbWVtb3J5GAogASgOMiMudHVyaW'
    '5nLnYxLkFnZW50UHJvZmlsZU1lbW9yeUFjY2Vzc1IGbWVtb3J5EhoKCHJlcXVpcmVzGAsgAygJ'
    'UghyZXF1aXJlcxIkCg5tYXhfdG9vbF9jYWxscxgMIAEoBVIMbWF4VG9vbENhbGxzEhoKCHJldm'
    'lzaW9uGA0gASgJUghyZXZpc2lvbhIYCgdlbmFibGVkGA4gASgIUgdlbmFibGVkEikKEGdyYW50'
    'ZWRfcmV2aXNpb24YDyABKAlSD2dyYW50ZWRSZXZpc2lvbhIyCgVzdGF0ZRgQIAEoDjIcLnR1cm'
    'luZy52MS5BZ2VudFByb2ZpbGVTdGF0ZVIFc3RhdGUSJQoOcmVzb2x2ZWRfdG9vbHMYESADKAlS'
    'DXJlc29sdmVkVG9vbHMSLwoTdW5hdmFpbGFibGVfcmVhc29ucxgSIAMoCVISdW5hdmFpbGFibG'
    'VSZWFzb25zEksKDmV4Y2x1ZGVkX3Rvb2xzGBMgAygLMiQudHVyaW5nLnYxLkFnZW50UHJvZmls'
    'ZVRvb2xFeGNsdXNpb25SDWV4Y2x1ZGVkVG9vbHMSHwoLcGFyc2VfZXJyb3IYFCABKAlSCnBhcn'
    'NlRXJyb3ISIwoNY29uc2VudF90b29scxgVIAMoCVIMY29uc2VudFRvb2xz');

@$core.Deprecated('Use listAgentProfilesRequestDescriptor instead')
const ListAgentProfilesRequest$json = {
  '1': 'ListAgentProfilesRequest',
};

/// Descriptor for `ListAgentProfilesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listAgentProfilesRequestDescriptor =
    $convert.base64Decode('ChhMaXN0QWdlbnRQcm9maWxlc1JlcXVlc3Q=');

@$core.Deprecated('Use listAgentProfilesResponseDescriptor instead')
const ListAgentProfilesResponse$json = {
  '1': 'ListAgentProfilesResponse',
  '2': [
    {
      '1': 'profiles',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.AgentProfile',
      '10': 'profiles'
    },
  ],
};

/// Descriptor for `ListAgentProfilesResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listAgentProfilesResponseDescriptor =
    $convert.base64Decode(
        'ChlMaXN0QWdlbnRQcm9maWxlc1Jlc3BvbnNlEjMKCHByb2ZpbGVzGAEgAygLMhcudHVyaW5nLn'
        'YxLkFnZW50UHJvZmlsZVIIcHJvZmlsZXM=');

@$core.Deprecated('Use setAgentProfileEnabledRequestDescriptor instead')
const SetAgentProfileEnabledRequest$json = {
  '1': 'SetAgentProfileEnabledRequest',
  '2': [
    {'1': 'profile_id', '3': 1, '4': 1, '5': 9, '10': 'profileId'},
    {'1': 'enabled', '3': 2, '4': 1, '5': 8, '10': 'enabled'},
  ],
};

/// Descriptor for `SetAgentProfileEnabledRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setAgentProfileEnabledRequestDescriptor =
    $convert.base64Decode(
        'Ch1TZXRBZ2VudFByb2ZpbGVFbmFibGVkUmVxdWVzdBIdCgpwcm9maWxlX2lkGAEgASgJUglwcm'
        '9maWxlSWQSGAoHZW5hYmxlZBgCIAEoCFIHZW5hYmxlZA==');

@$core.Deprecated('Use grantAgentProfileRequestDescriptor instead')
const GrantAgentProfileRequest$json = {
  '1': 'GrantAgentProfileRequest',
  '2': [
    {'1': 'profile_id', '3': 1, '4': 1, '5': 9, '10': 'profileId'},
    {'1': 'revision', '3': 2, '4': 1, '5': 9, '10': 'revision'},
  ],
};

/// Descriptor for `GrantAgentProfileRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List grantAgentProfileRequestDescriptor =
    $convert.base64Decode(
        'ChhHcmFudEFnZW50UHJvZmlsZVJlcXVlc3QSHQoKcHJvZmlsZV9pZBgBIAEoCVIJcHJvZmlsZU'
        'lkEhoKCHJldmlzaW9uGAIgASgJUghyZXZpc2lvbg==');

@$core.Deprecated('Use teamToolDescriptorDescriptor instead')
const TeamToolDescriptor$json = {
  '1': 'TeamToolDescriptor',
  '2': [
    {'1': 'tool_name', '3': 1, '4': 1, '5': 9, '10': 'toolName'},
    {
      '1': 'policy',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ToolPolicy',
      '10': 'policy'
    },
    {
      '1': 'schema',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'schema'
    },
    {'1': 'enabled', '3': 4, '4': 1, '5': 8, '10': 'enabled'},
    {'1': 'description', '3': 5, '4': 1, '5': 9, '10': 'description'},
  ],
};

/// Descriptor for `TeamToolDescriptor`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List teamToolDescriptorDescriptor = $convert.base64Decode(
    'ChJUZWFtVG9vbERlc2NyaXB0b3ISGwoJdG9vbF9uYW1lGAEgASgJUgh0b29sTmFtZRItCgZwb2'
    'xpY3kYAiABKA4yFS50dXJpbmcudjEuVG9vbFBvbGljeVIGcG9saWN5Ei8KBnNjaGVtYRgDIAEo'
    'CzIXLmdvb2dsZS5wcm90b2J1Zi5TdHJ1Y3RSBnNjaGVtYRIYCgdlbmFibGVkGAQgASgIUgdlbm'
    'FibGVkEiAKC2Rlc2NyaXB0aW9uGAUgASgJUgtkZXNjcmlwdGlvbg==');

@$core.Deprecated('Use listTeamToolsRequestDescriptor instead')
const ListTeamToolsRequest$json = {
  '1': 'ListTeamToolsRequest',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
  ],
};

/// Descriptor for `ListTeamToolsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listTeamToolsRequestDescriptor =
    $convert.base64Decode(
        'ChRMaXN0VGVhbVRvb2xzUmVxdWVzdBIVCgZydW5faWQYASABKAlSBXJ1bklk');

@$core.Deprecated('Use listTeamToolsResponseDescriptor instead')
const ListTeamToolsResponse$json = {
  '1': 'ListTeamToolsResponse',
  '2': [
    {
      '1': 'tools',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.TeamToolDescriptor',
      '10': 'tools'
    },
  ],
};

/// Descriptor for `ListTeamToolsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listTeamToolsResponseDescriptor = $convert.base64Decode(
    'ChVMaXN0VGVhbVRvb2xzUmVzcG9uc2USMwoFdG9vbHMYASADKAsyHS50dXJpbmcudjEuVGVhbV'
    'Rvb2xEZXNjcmlwdG9yUgV0b29scw==');

@$core.Deprecated('Use callTeamToolRequestDescriptor instead')
const CallTeamToolRequest$json = {
  '1': 'CallTeamToolRequest',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
    {
      '1': 'assignment_attempt_id',
      '3': 2,
      '4': 1,
      '5': 9,
      '10': 'assignmentAttemptId'
    },
    {'1': 'approval_id', '3': 3, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'tool_call_id', '3': 4, '4': 1, '5': 9, '10': 'toolCallId'},
    {'1': 'tool_name', '3': 5, '4': 1, '5': 9, '10': 'toolName'},
    {
      '1': 'args',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'args'
    },
  ],
};

/// Descriptor for `CallTeamToolRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List callTeamToolRequestDescriptor = $convert.base64Decode(
    'ChNDYWxsVGVhbVRvb2xSZXF1ZXN0EhUKBnJ1bl9pZBgBIAEoCVIFcnVuSWQSMgoVYXNzaWdubW'
    'VudF9hdHRlbXB0X2lkGAIgASgJUhNhc3NpZ25tZW50QXR0ZW1wdElkEh8KC2FwcHJvdmFsX2lk'
    'GAMgASgJUgphcHByb3ZhbElkEiAKDHRvb2xfY2FsbF9pZBgEIAEoCVIKdG9vbENhbGxJZBIbCg'
    'l0b29sX25hbWUYBSABKAlSCHRvb2xOYW1lEisKBGFyZ3MYBiABKAsyFy5nb29nbGUucHJvdG9i'
    'dWYuU3RydWN0UgRhcmdz');

@$core.Deprecated('Use callTeamToolResponseDescriptor instead')
const CallTeamToolResponse$json = {
  '1': 'CallTeamToolResponse',
  '2': [
    {
      '1': 'result',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'result'
    },
  ],
};

/// Descriptor for `CallTeamToolResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List callTeamToolResponseDescriptor = $convert.base64Decode(
    'ChRDYWxsVGVhbVRvb2xSZXNwb25zZRIvCgZyZXN1bHQYASABKAsyFy5nb29nbGUucHJvdG9idW'
    'YuU3RydWN0UgZyZXN1bHQ=');

@$core.Deprecated('Use delegationDescriptor instead')
const Delegation$json = {
  '1': 'Delegation',
  '2': [
    {'1': 'delegation_id', '3': 1, '4': 1, '5': 9, '10': 'delegationId'},
    {'1': 'parent_session_id', '3': 2, '4': 1, '5': 9, '10': 'parentSessionId'},
    {'1': 'parent_run_id', '3': 3, '4': 1, '5': 9, '10': 'parentRunId'},
    {'1': 'child_session_id', '3': 4, '4': 1, '5': 9, '10': 'childSessionId'},
    {'1': 'child_run_id', '3': 5, '4': 1, '5': 9, '10': 'childRunId'},
    {'1': 'profile_id', '3': 6, '4': 1, '5': 9, '10': 'profileId'},
    {'1': 'display_name', '3': 7, '4': 1, '5': 9, '10': 'displayName'},
    {'1': 'emoji', '3': 8, '4': 1, '5': 9, '10': 'emoji'},
    {'1': 'task', '3': 9, '4': 1, '5': 9, '10': 'task'},
    {'1': 'state', '3': 10, '4': 1, '5': 9, '10': 'state'},
    {'1': 'approval_id', '3': 11, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'error_code', '3': 12, '4': 1, '5': 9, '10': 'errorCode'},
    {'1': 'joined', '3': 13, '4': 1, '5': 8, '10': 'joined'},
    {
      '1': 'continuation_run_id',
      '3': 14,
      '4': 1,
      '5': 9,
      '10': 'continuationRunId'
    },
    {
      '1': 'created_at',
      '3': 15,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'createdAt'
    },
    {
      '1': 'finished_at',
      '3': 16,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'finishedAt'
    },
    {'1': 'summary', '3': 17, '4': 1, '5': 9, '10': 'summary'},
    {'1': 'egress_tools', '3': 18, '4': 3, '5': 9, '10': 'egressTools'},
    {'1': 'egress_consent', '3': 19, '4': 1, '5': 9, '10': 'egressConsent'},
    {'1': 'egress_required', '3': 20, '4': 1, '5': 8, '10': 'egressRequired'},
  ],
};

/// Descriptor for `Delegation`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List delegationDescriptor = $convert.base64Decode(
    'CgpEZWxlZ2F0aW9uEiMKDWRlbGVnYXRpb25faWQYASABKAlSDGRlbGVnYXRpb25JZBIqChFwYX'
    'JlbnRfc2Vzc2lvbl9pZBgCIAEoCVIPcGFyZW50U2Vzc2lvbklkEiIKDXBhcmVudF9ydW5faWQY'
    'AyABKAlSC3BhcmVudFJ1bklkEigKEGNoaWxkX3Nlc3Npb25faWQYBCABKAlSDmNoaWxkU2Vzc2'
    'lvbklkEiAKDGNoaWxkX3J1bl9pZBgFIAEoCVIKY2hpbGRSdW5JZBIdCgpwcm9maWxlX2lkGAYg'
    'ASgJUglwcm9maWxlSWQSIQoMZGlzcGxheV9uYW1lGAcgASgJUgtkaXNwbGF5TmFtZRIUCgVlbW'
    '9qaRgIIAEoCVIFZW1vamkSEgoEdGFzaxgJIAEoCVIEdGFzaxIUCgVzdGF0ZRgKIAEoCVIFc3Rh'
    'dGUSHwoLYXBwcm92YWxfaWQYCyABKAlSCmFwcHJvdmFsSWQSHQoKZXJyb3JfY29kZRgMIAEoCV'
    'IJZXJyb3JDb2RlEhYKBmpvaW5lZBgNIAEoCFIGam9pbmVkEi4KE2NvbnRpbnVhdGlvbl9ydW5f'
    'aWQYDiABKAlSEWNvbnRpbnVhdGlvblJ1bklkEjkKCmNyZWF0ZWRfYXQYDyABKAsyGi5nb29nbG'
    'UucHJvdG9idWYuVGltZXN0YW1wUgljcmVhdGVkQXQSOwoLZmluaXNoZWRfYXQYECABKAsyGi5n'
    'b29nbGUucHJvdG9idWYuVGltZXN0YW1wUgpmaW5pc2hlZEF0EhgKB3N1bW1hcnkYESABKAlSB3'
    'N1bW1hcnkSIQoMZWdyZXNzX3Rvb2xzGBIgAygJUgtlZ3Jlc3NUb29scxIlCg5lZ3Jlc3NfY29u'
    'c2VudBgTIAEoCVINZWdyZXNzQ29uc2VudBInCg9lZ3Jlc3NfcmVxdWlyZWQYFCABKAhSDmVncm'
    'Vzc1JlcXVpcmVk');

@$core.Deprecated('Use listDelegationsRequestDescriptor instead')
const ListDelegationsRequest$json = {
  '1': 'ListDelegationsRequest',
  '2': [
    {'1': 'parent_session_id', '3': 1, '4': 1, '5': 9, '10': 'parentSessionId'},
  ],
};

/// Descriptor for `ListDelegationsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listDelegationsRequestDescriptor =
    $convert.base64Decode(
        'ChZMaXN0RGVsZWdhdGlvbnNSZXF1ZXN0EioKEXBhcmVudF9zZXNzaW9uX2lkGAEgASgJUg9wYX'
        'JlbnRTZXNzaW9uSWQ=');

@$core.Deprecated('Use listDelegationsResponseDescriptor instead')
const ListDelegationsResponse$json = {
  '1': 'ListDelegationsResponse',
  '2': [
    {
      '1': 'delegations',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.Delegation',
      '10': 'delegations'
    },
  ],
};

/// Descriptor for `ListDelegationsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listDelegationsResponseDescriptor =
    $convert.base64Decode(
        'ChdMaXN0RGVsZWdhdGlvbnNSZXNwb25zZRI3CgtkZWxlZ2F0aW9ucxgBIAMoCzIVLnR1cmluZy'
        '52MS5EZWxlZ2F0aW9uUgtkZWxlZ2F0aW9ucw==');

@$core.Deprecated('Use getDelegationRequestDescriptor instead')
const GetDelegationRequest$json = {
  '1': 'GetDelegationRequest',
  '2': [
    {'1': 'delegation_id', '3': 1, '4': 1, '5': 9, '10': 'delegationId'},
  ],
};

/// Descriptor for `GetDelegationRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getDelegationRequestDescriptor = $convert.base64Decode(
    'ChRHZXREZWxlZ2F0aW9uUmVxdWVzdBIjCg1kZWxlZ2F0aW9uX2lkGAEgASgJUgxkZWxlZ2F0aW'
    '9uSWQ=');

@$core.Deprecated('Use prepareDelegationEgressRequestDescriptor instead')
const PrepareDelegationEgressRequest$json = {
  '1': 'PrepareDelegationEgressRequest',
  '2': [
    {'1': 'delegation_id', '3': 1, '4': 1, '5': 9, '10': 'delegationId'},
  ],
};

/// Descriptor for `PrepareDelegationEgressRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List prepareDelegationEgressRequestDescriptor =
    $convert.base64Decode(
        'Ch5QcmVwYXJlRGVsZWdhdGlvbkVncmVzc1JlcXVlc3QSIwoNZGVsZWdhdGlvbl9pZBgBIAEoCV'
        'IMZGVsZWdhdGlvbklk');

@$core.Deprecated('Use prepareDelegationEgressResponseDescriptor instead')
const PrepareDelegationEgressResponse$json = {
  '1': 'PrepareDelegationEgressResponse',
  '2': [
    {
      '1': 'disclosure',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RemoteEgressDisclosure',
      '10': 'disclosure'
    },
  ],
};

/// Descriptor for `PrepareDelegationEgressResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List prepareDelegationEgressResponseDescriptor =
    $convert.base64Decode(
        'Ch9QcmVwYXJlRGVsZWdhdGlvbkVncmVzc1Jlc3BvbnNlEkEKCmRpc2Nsb3N1cmUYASABKAsyIS'
        '50dXJpbmcudjEuUmVtb3RlRWdyZXNzRGlzY2xvc3VyZVIKZGlzY2xvc3VyZQ==');

@$core.Deprecated('Use decideDelegationEgressRequestDescriptor instead')
const DecideDelegationEgressRequest$json = {
  '1': 'DecideDelegationEgressRequest',
  '2': [
    {'1': 'delegation_id', '3': 1, '4': 1, '5': 9, '10': 'delegationId'},
    {
      '1': 'consent',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RemoteEgressConsent',
      '10': 'consent'
    },
  ],
};

/// Descriptor for `DecideDelegationEgressRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List decideDelegationEgressRequestDescriptor =
    $convert.base64Decode(
        'Ch1EZWNpZGVEZWxlZ2F0aW9uRWdyZXNzUmVxdWVzdBIjCg1kZWxlZ2F0aW9uX2lkGAEgASgJUg'
        'xkZWxlZ2F0aW9uSWQSOAoHY29uc2VudBgCIAEoCzIeLnR1cmluZy52MS5SZW1vdGVFZ3Jlc3ND'
        'b25zZW50Ugdjb25zZW50');
