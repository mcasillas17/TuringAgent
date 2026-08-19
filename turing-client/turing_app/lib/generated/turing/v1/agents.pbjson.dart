// This is a generated file - do not edit.
//
// Generated from turing/v1/agents.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use externalAgentProviderDescriptor instead')
const ExternalAgentProvider$json = {
  '1': 'ExternalAgentProvider',
  '2': [
    {'1': 'EXTERNAL_AGENT_PROVIDER_UNSPECIFIED', '2': 0},
    {'1': 'EXTERNAL_AGENT_PROVIDER_ANTHROPIC', '2': 1},
    {'1': 'EXTERNAL_AGENT_PROVIDER_OPENAI', '2': 2},
    {'1': 'EXTERNAL_AGENT_PROVIDER_GOOGLE', '2': 3},
    {'1': 'EXTERNAL_AGENT_PROVIDER_XAI', '2': 4},
    {'1': 'EXTERNAL_AGENT_PROVIDER_OTHER', '2': 5},
  ],
};

/// Descriptor for `ExternalAgentProvider`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List externalAgentProviderDescriptor = $convert.base64Decode(
    'ChVFeHRlcm5hbEFnZW50UHJvdmlkZXISJwojRVhURVJOQUxfQUdFTlRfUFJPVklERVJfVU5TUE'
    'VDSUZJRUQQABIlCiFFWFRFUk5BTF9BR0VOVF9QUk9WSURFUl9BTlRIUk9QSUMQARIiCh5FWFRF'
    'Uk5BTF9BR0VOVF9QUk9WSURFUl9PUEVOQUkQAhIiCh5FWFRFUk5BTF9BR0VOVF9QUk9WSURFUl'
    '9HT09HTEUQAxIfChtFWFRFUk5BTF9BR0VOVF9QUk9WSURFUl9YQUkQBBIhCh1FWFRFUk5BTF9B'
    'R0VOVF9QUk9WSURFUl9PVEhFUhAF');

@$core.Deprecated('Use externalAgentDescriptor instead')
const ExternalAgent$json = {
  '1': 'ExternalAgent',
  '2': [
    {'1': 'agent_id', '3': 1, '4': 1, '5': 9, '10': 'agentId'},
    {'1': 'display_name', '3': 2, '4': 1, '5': 9, '10': 'displayName'},
    {
      '1': 'provider',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ExternalAgentProvider',
      '10': 'provider'
    },
    {'1': 'base_url', '3': 4, '4': 1, '5': 9, '10': 'baseUrl'},
    {'1': 'model', '3': 5, '4': 1, '5': 9, '10': 'model'},
    {'1': 'credential_ref', '3': 6, '4': 1, '5': 9, '10': 'credentialRef'},
    {
      '1': 'credential_available',
      '3': 7,
      '4': 1,
      '5': 8,
      '10': 'credentialAvailable'
    },
    {
      '1': 'created_at',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'createdAt'
    },
    {
      '1': 'updated_at',
      '3': 9,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'updatedAt'
    },
  ],
};

/// Descriptor for `ExternalAgent`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List externalAgentDescriptor = $convert.base64Decode(
    'Cg1FeHRlcm5hbEFnZW50EhkKCGFnZW50X2lkGAEgASgJUgdhZ2VudElkEiEKDGRpc3BsYXlfbm'
    'FtZRgCIAEoCVILZGlzcGxheU5hbWUSPAoIcHJvdmlkZXIYAyABKA4yIC50dXJpbmcudjEuRXh0'
    'ZXJuYWxBZ2VudFByb3ZpZGVyUghwcm92aWRlchIZCghiYXNlX3VybBgEIAEoCVIHYmFzZVVybB'
    'IUCgVtb2RlbBgFIAEoCVIFbW9kZWwSJQoOY3JlZGVudGlhbF9yZWYYBiABKAlSDWNyZWRlbnRp'
    'YWxSZWYSMQoUY3JlZGVudGlhbF9hdmFpbGFibGUYByABKAhSE2NyZWRlbnRpYWxBdmFpbGFibG'
    'USOQoKY3JlYXRlZF9hdBgIIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSCWNyZWF0'
    'ZWRBdBI5Cgp1cGRhdGVkX2F0GAkgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIJdX'
    'BkYXRlZEF0');

@$core.Deprecated('Use createExternalAgentRequestDescriptor instead')
const CreateExternalAgentRequest$json = {
  '1': 'CreateExternalAgentRequest',
  '2': [
    {'1': 'display_name', '3': 1, '4': 1, '5': 9, '10': 'displayName'},
    {
      '1': 'provider',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ExternalAgentProvider',
      '10': 'provider'
    },
    {'1': 'base_url', '3': 3, '4': 1, '5': 9, '10': 'baseUrl'},
    {'1': 'model', '3': 4, '4': 1, '5': 9, '10': 'model'},
    {'1': 'credential_ref', '3': 5, '4': 1, '5': 9, '10': 'credentialRef'},
  ],
};

/// Descriptor for `CreateExternalAgentRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List createExternalAgentRequestDescriptor = $convert.base64Decode(
    'ChpDcmVhdGVFeHRlcm5hbEFnZW50UmVxdWVzdBIhCgxkaXNwbGF5X25hbWUYASABKAlSC2Rpc3'
    'BsYXlOYW1lEjwKCHByb3ZpZGVyGAIgASgOMiAudHVyaW5nLnYxLkV4dGVybmFsQWdlbnRQcm92'
    'aWRlclIIcHJvdmlkZXISGQoIYmFzZV91cmwYAyABKAlSB2Jhc2VVcmwSFAoFbW9kZWwYBCABKA'
    'lSBW1vZGVsEiUKDmNyZWRlbnRpYWxfcmVmGAUgASgJUg1jcmVkZW50aWFsUmVm');

@$core.Deprecated('Use updateExternalAgentRequestDescriptor instead')
const UpdateExternalAgentRequest$json = {
  '1': 'UpdateExternalAgentRequest',
  '2': [
    {'1': 'agent_id', '3': 1, '4': 1, '5': 9, '10': 'agentId'},
    {'1': 'display_name', '3': 2, '4': 1, '5': 9, '10': 'displayName'},
    {
      '1': 'provider',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ExternalAgentProvider',
      '10': 'provider'
    },
    {'1': 'base_url', '3': 4, '4': 1, '5': 9, '10': 'baseUrl'},
    {'1': 'model', '3': 5, '4': 1, '5': 9, '10': 'model'},
    {'1': 'credential_ref', '3': 6, '4': 1, '5': 9, '10': 'credentialRef'},
  ],
};

/// Descriptor for `UpdateExternalAgentRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List updateExternalAgentRequestDescriptor = $convert.base64Decode(
    'ChpVcGRhdGVFeHRlcm5hbEFnZW50UmVxdWVzdBIZCghhZ2VudF9pZBgBIAEoCVIHYWdlbnRJZB'
    'IhCgxkaXNwbGF5X25hbWUYAiABKAlSC2Rpc3BsYXlOYW1lEjwKCHByb3ZpZGVyGAMgASgOMiAu'
    'dHVyaW5nLnYxLkV4dGVybmFsQWdlbnRQcm92aWRlclIIcHJvdmlkZXISGQoIYmFzZV91cmwYBC'
    'ABKAlSB2Jhc2VVcmwSFAoFbW9kZWwYBSABKAlSBW1vZGVsEiUKDmNyZWRlbnRpYWxfcmVmGAYg'
    'ASgJUg1jcmVkZW50aWFsUmVm');

@$core.Deprecated('Use deleteExternalAgentRequestDescriptor instead')
const DeleteExternalAgentRequest$json = {
  '1': 'DeleteExternalAgentRequest',
  '2': [
    {'1': 'agent_id', '3': 1, '4': 1, '5': 9, '10': 'agentId'},
  ],
};

/// Descriptor for `DeleteExternalAgentRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List deleteExternalAgentRequestDescriptor =
    $convert.base64Decode(
        'ChpEZWxldGVFeHRlcm5hbEFnZW50UmVxdWVzdBIZCghhZ2VudF9pZBgBIAEoCVIHYWdlbnRJZA'
        '==');

@$core.Deprecated('Use deleteExternalAgentResponseDescriptor instead')
const DeleteExternalAgentResponse$json = {
  '1': 'DeleteExternalAgentResponse',
};

/// Descriptor for `DeleteExternalAgentResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List deleteExternalAgentResponseDescriptor =
    $convert.base64Decode('ChtEZWxldGVFeHRlcm5hbEFnZW50UmVzcG9uc2U=');

@$core.Deprecated('Use listExternalAgentsRequestDescriptor instead')
const ListExternalAgentsRequest$json = {
  '1': 'ListExternalAgentsRequest',
};

/// Descriptor for `ListExternalAgentsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listExternalAgentsRequestDescriptor =
    $convert.base64Decode('ChlMaXN0RXh0ZXJuYWxBZ2VudHNSZXF1ZXN0');

@$core.Deprecated('Use listExternalAgentsResponseDescriptor instead')
const ListExternalAgentsResponse$json = {
  '1': 'ListExternalAgentsResponse',
  '2': [
    {
      '1': 'agents',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.ExternalAgent',
      '10': 'agents'
    },
  ],
};

/// Descriptor for `ListExternalAgentsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listExternalAgentsResponseDescriptor =
    $convert.base64Decode(
        'ChpMaXN0RXh0ZXJuYWxBZ2VudHNSZXNwb25zZRIwCgZhZ2VudHMYASADKAsyGC50dXJpbmcudj'
        'EuRXh0ZXJuYWxBZ2VudFIGYWdlbnRz');

@$core.Deprecated('Use getSessionAgentRequestDescriptor instead')
const GetSessionAgentRequest$json = {
  '1': 'GetSessionAgentRequest',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
  ],
};

/// Descriptor for `GetSessionAgentRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getSessionAgentRequestDescriptor =
    $convert.base64Decode(
        'ChZHZXRTZXNzaW9uQWdlbnRSZXF1ZXN0Eh0KCnNlc3Npb25faWQYASABKAlSCXNlc3Npb25JZA'
        '==');

@$core.Deprecated('Use setSessionAgentRequestDescriptor instead')
const SetSessionAgentRequest$json = {
  '1': 'SetSessionAgentRequest',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'agent_id', '3': 2, '4': 1, '5': 9, '10': 'agentId'},
  ],
};

/// Descriptor for `SetSessionAgentRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setSessionAgentRequestDescriptor =
    $convert.base64Decode(
        'ChZTZXRTZXNzaW9uQWdlbnRSZXF1ZXN0Eh0KCnNlc3Npb25faWQYASABKAlSCXNlc3Npb25JZB'
        'IZCghhZ2VudF9pZBgCIAEoCVIHYWdlbnRJZA==');

@$core.Deprecated('Use clearSessionAgentRequestDescriptor instead')
const ClearSessionAgentRequest$json = {
  '1': 'ClearSessionAgentRequest',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
  ],
};

/// Descriptor for `ClearSessionAgentRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List clearSessionAgentRequestDescriptor =
    $convert.base64Decode(
        'ChhDbGVhclNlc3Npb25BZ2VudFJlcXVlc3QSHQoKc2Vzc2lvbl9pZBgBIAEoCVIJc2Vzc2lvbk'
        'lk');

@$core.Deprecated('Use sessionAgentResponseDescriptor instead')
const SessionAgentResponse$json = {
  '1': 'SessionAgentResponse',
  '2': [
    {
      '1': 'agent',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.ExternalAgent',
      '10': 'agent'
    },
  ],
};

/// Descriptor for `SessionAgentResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List sessionAgentResponseDescriptor = $convert.base64Decode(
    'ChRTZXNzaW9uQWdlbnRSZXNwb25zZRIuCgVhZ2VudBgBIAEoCzIYLnR1cmluZy52MS5FeHRlcm'
    '5hbEFnZW50UgVhZ2VudA==');
