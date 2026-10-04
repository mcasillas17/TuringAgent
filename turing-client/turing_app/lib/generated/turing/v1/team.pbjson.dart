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
    'NlRXJyb3I=');

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
