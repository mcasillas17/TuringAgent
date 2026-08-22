// This is a generated file - do not edit.
//
// Generated from turing/v1/integrations.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use integrationProviderDescriptor instead')
const IntegrationProvider$json = {
  '1': 'IntegrationProvider',
  '2': [
    {'1': 'INTEGRATION_PROVIDER_UNSPECIFIED', '2': 0},
    {'1': 'INTEGRATION_PROVIDER_IMAP', '2': 1},
    {'1': 'INTEGRATION_PROVIDER_CALDAV', '2': 2},
    {'1': 'INTEGRATION_PROVIDER_NOTION', '2': 3},
    {'1': 'INTEGRATION_PROVIDER_GITHUB', '2': 4},
    {'1': 'INTEGRATION_PROVIDER_GOOGLE_WORKSPACE', '2': 5},
    {'1': 'INTEGRATION_PROVIDER_MICROSOFT_365', '2': 6},
    {'1': 'INTEGRATION_PROVIDER_SLACK', '2': 7},
  ],
};

/// Descriptor for `IntegrationProvider`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List integrationProviderDescriptor = $convert.base64Decode(
    'ChNJbnRlZ3JhdGlvblByb3ZpZGVyEiQKIElOVEVHUkFUSU9OX1BST1ZJREVSX1VOU1BFQ0lGSU'
    'VEEAASHQoZSU5URUdSQVRJT05fUFJPVklERVJfSU1BUBABEh8KG0lOVEVHUkFUSU9OX1BST1ZJ'
    'REVSX0NBTERBVhACEh8KG0lOVEVHUkFUSU9OX1BST1ZJREVSX05PVElPThADEh8KG0lOVEVHUk'
    'FUSU9OX1BST1ZJREVSX0dJVEhVQhAEEikKJUlOVEVHUkFUSU9OX1BST1ZJREVSX0dPT0dMRV9X'
    'T1JLU1BBQ0UQBRImCiJJTlRFR1JBVElPTl9QUk9WSURFUl9NSUNST1NPRlRfMzY1EAYSHgoaSU'
    '5URUdSQVRJT05fUFJPVklERVJfU0xBQ0sQBw==');

@$core.Deprecated('Use connectionStatusDescriptor instead')
const ConnectionStatus$json = {
  '1': 'ConnectionStatus',
  '2': [
    {'1': 'CONNECTION_STATUS_UNSPECIFIED', '2': 0},
    {'1': 'CONNECTION_STATUS_CONNECTED', '2': 1},
    {'1': 'CONNECTION_STATUS_REVOKED', '2': 2},
  ],
};

/// Descriptor for `ConnectionStatus`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List connectionStatusDescriptor = $convert.base64Decode(
    'ChBDb25uZWN0aW9uU3RhdHVzEiEKHUNPTk5FQ1RJT05fU1RBVFVTX1VOU1BFQ0lGSUVEEAASHw'
    'obQ09OTkVDVElPTl9TVEFUVVNfQ09OTkVDVEVEEAESHQoZQ09OTkVDVElPTl9TVEFUVVNfUkVW'
    'T0tFRBAC');

@$core.Deprecated('Use providerDescriptorDescriptor instead')
const ProviderDescriptor$json = {
  '1': 'ProviderDescriptor',
  '2': [
    {
      '1': 'provider',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.IntegrationProvider',
      '10': 'provider'
    },
    {'1': 'display_name', '3': 2, '4': 1, '5': 9, '10': 'displayName'},
    {'1': 'category', '3': 3, '4': 1, '5': 9, '10': 'category'},
    {'1': 'supported', '3': 4, '4': 1, '5': 8, '10': 'supported'},
    {
      '1': 'unsupported_reason',
      '3': 5,
      '4': 1,
      '5': 9,
      '10': 'unsupportedReason'
    },
    {'1': 'secret_label', '3': 6, '4': 1, '5': 9, '10': 'secretLabel'},
    {'1': 'secret_help', '3': 7, '4': 1, '5': 9, '10': 'secretHelp'},
    {'1': 'account_label', '3': 8, '4': 1, '5': 9, '10': 'accountLabel'},
    {
      '1': 'requires_endpoint',
      '3': 9,
      '4': 1,
      '5': 8,
      '10': 'requiresEndpoint'
    },
    {'1': 'endpoint_label', '3': 10, '4': 1, '5': 9, '10': 'endpointLabel'},
    {'1': 'grants', '3': 11, '4': 3, '5': 9, '10': 'grants'},
  ],
};

/// Descriptor for `ProviderDescriptor`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List providerDescriptorDescriptor = $convert.base64Decode(
    'ChJQcm92aWRlckRlc2NyaXB0b3ISOgoIcHJvdmlkZXIYASABKA4yHi50dXJpbmcudjEuSW50ZW'
    'dyYXRpb25Qcm92aWRlclIIcHJvdmlkZXISIQoMZGlzcGxheV9uYW1lGAIgASgJUgtkaXNwbGF5'
    'TmFtZRIaCghjYXRlZ29yeRgDIAEoCVIIY2F0ZWdvcnkSHAoJc3VwcG9ydGVkGAQgASgIUglzdX'
    'Bwb3J0ZWQSLQoSdW5zdXBwb3J0ZWRfcmVhc29uGAUgASgJUhF1bnN1cHBvcnRlZFJlYXNvbhIh'
    'CgxzZWNyZXRfbGFiZWwYBiABKAlSC3NlY3JldExhYmVsEh8KC3NlY3JldF9oZWxwGAcgASgJUg'
    'pzZWNyZXRIZWxwEiMKDWFjY291bnRfbGFiZWwYCCABKAlSDGFjY291bnRMYWJlbBIrChFyZXF1'
    'aXJlc19lbmRwb2ludBgJIAEoCFIQcmVxdWlyZXNFbmRwb2ludBIlCg5lbmRwb2ludF9sYWJlbB'
    'gKIAEoCVINZW5kcG9pbnRMYWJlbBIWCgZncmFudHMYCyADKAlSBmdyYW50cw==');

@$core.Deprecated('Use connectionDescriptor instead')
const Connection$json = {
  '1': 'Connection',
  '2': [
    {'1': 'connection_id', '3': 1, '4': 1, '5': 9, '10': 'connectionId'},
    {
      '1': 'provider',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.IntegrationProvider',
      '10': 'provider'
    },
    {'1': 'display_name', '3': 3, '4': 1, '5': 9, '10': 'displayName'},
    {'1': 'account_label', '3': 4, '4': 1, '5': 9, '10': 'accountLabel'},
    {'1': 'endpoint', '3': 5, '4': 1, '5': 9, '10': 'endpoint'},
    {'1': 'credential_hint', '3': 6, '4': 1, '5': 9, '10': 'credentialHint'},
    {
      '1': 'status',
      '3': 7,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ConnectionStatus',
      '10': 'status'
    },
    {'1': 'granted_scopes', '3': 8, '4': 3, '5': 9, '10': 'grantedScopes'},
    {
      '1': 'consent_granted_at',
      '3': 9,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'consentGrantedAt'
    },
    {
      '1': 'connected_at',
      '3': 10,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'connectedAt'
    },
    {
      '1': 'revoked_at',
      '3': 11,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'revokedAt'
    },
    {
      '1': 'updated_at',
      '3': 12,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'updatedAt'
    },
    {
      '1': 'credential_unreadable',
      '3': 13,
      '4': 1,
      '5': 8,
      '10': 'credentialUnreadable'
    },
  ],
};

/// Descriptor for `Connection`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List connectionDescriptor = $convert.base64Decode(
    'CgpDb25uZWN0aW9uEiMKDWNvbm5lY3Rpb25faWQYASABKAlSDGNvbm5lY3Rpb25JZBI6Cghwcm'
    '92aWRlchgCIAEoDjIeLnR1cmluZy52MS5JbnRlZ3JhdGlvblByb3ZpZGVyUghwcm92aWRlchIh'
    'CgxkaXNwbGF5X25hbWUYAyABKAlSC2Rpc3BsYXlOYW1lEiMKDWFjY291bnRfbGFiZWwYBCABKA'
    'lSDGFjY291bnRMYWJlbBIaCghlbmRwb2ludBgFIAEoCVIIZW5kcG9pbnQSJwoPY3JlZGVudGlh'
    'bF9oaW50GAYgASgJUg5jcmVkZW50aWFsSGludBIzCgZzdGF0dXMYByABKA4yGy50dXJpbmcudj'
    'EuQ29ubmVjdGlvblN0YXR1c1IGc3RhdHVzEiUKDmdyYW50ZWRfc2NvcGVzGAggAygJUg1ncmFu'
    'dGVkU2NvcGVzEkgKEmNvbnNlbnRfZ3JhbnRlZF9hdBgJIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi'
    '5UaW1lc3RhbXBSEGNvbnNlbnRHcmFudGVkQXQSPQoMY29ubmVjdGVkX2F0GAogASgLMhouZ29v'
    'Z2xlLnByb3RvYnVmLlRpbWVzdGFtcFILY29ubmVjdGVkQXQSOQoKcmV2b2tlZF9hdBgLIAEoCz'
    'IaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSCXJldm9rZWRBdBI5Cgp1cGRhdGVkX2F0GAwg'
    'ASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIJdXBkYXRlZEF0EjMKFWNyZWRlbnRpYW'
    'xfdW5yZWFkYWJsZRgNIAEoCFIUY3JlZGVudGlhbFVucmVhZGFibGU=');

@$core.Deprecated('Use listProvidersRequestDescriptor instead')
const ListProvidersRequest$json = {
  '1': 'ListProvidersRequest',
};

/// Descriptor for `ListProvidersRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listProvidersRequestDescriptor =
    $convert.base64Decode('ChRMaXN0UHJvdmlkZXJzUmVxdWVzdA==');

@$core.Deprecated('Use listProvidersResponseDescriptor instead')
const ListProvidersResponse$json = {
  '1': 'ListProvidersResponse',
  '2': [
    {
      '1': 'providers',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.ProviderDescriptor',
      '10': 'providers'
    },
    {
      '1': 'credential_storage_configured',
      '3': 2,
      '4': 1,
      '5': 8,
      '10': 'credentialStorageConfigured'
    },
    {
      '1': 'storage_unconfigured_reason',
      '3': 3,
      '4': 1,
      '5': 9,
      '10': 'storageUnconfiguredReason'
    },
  ],
};

/// Descriptor for `ListProvidersResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listProvidersResponseDescriptor = $convert.base64Decode(
    'ChVMaXN0UHJvdmlkZXJzUmVzcG9uc2USOwoJcHJvdmlkZXJzGAEgAygLMh0udHVyaW5nLnYxLl'
    'Byb3ZpZGVyRGVzY3JpcHRvclIJcHJvdmlkZXJzEkIKHWNyZWRlbnRpYWxfc3RvcmFnZV9jb25m'
    'aWd1cmVkGAIgASgIUhtjcmVkZW50aWFsU3RvcmFnZUNvbmZpZ3VyZWQSPgobc3RvcmFnZV91bm'
    'NvbmZpZ3VyZWRfcmVhc29uGAMgASgJUhlzdG9yYWdlVW5jb25maWd1cmVkUmVhc29u');

@$core.Deprecated('Use connectAccountRequestDescriptor instead')
const ConnectAccountRequest$json = {
  '1': 'ConnectAccountRequest',
  '2': [
    {
      '1': 'provider',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.IntegrationProvider',
      '10': 'provider'
    },
    {'1': 'display_name', '3': 2, '4': 1, '5': 9, '10': 'displayName'},
    {'1': 'account_label', '3': 3, '4': 1, '5': 9, '10': 'accountLabel'},
    {'1': 'endpoint', '3': 4, '4': 1, '5': 9, '10': 'endpoint'},
    {'1': 'credential', '3': 5, '4': 1, '5': 9, '10': 'credential'},
    {
      '1': 'consent_acknowledged',
      '3': 6,
      '4': 1,
      '5': 8,
      '10': 'consentAcknowledged'
    },
  ],
};

/// Descriptor for `ConnectAccountRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List connectAccountRequestDescriptor = $convert.base64Decode(
    'ChVDb25uZWN0QWNjb3VudFJlcXVlc3QSOgoIcHJvdmlkZXIYASABKA4yHi50dXJpbmcudjEuSW'
    '50ZWdyYXRpb25Qcm92aWRlclIIcHJvdmlkZXISIQoMZGlzcGxheV9uYW1lGAIgASgJUgtkaXNw'
    'bGF5TmFtZRIjCg1hY2NvdW50X2xhYmVsGAMgASgJUgxhY2NvdW50TGFiZWwSGgoIZW5kcG9pbn'
    'QYBCABKAlSCGVuZHBvaW50Eh4KCmNyZWRlbnRpYWwYBSABKAlSCmNyZWRlbnRpYWwSMQoUY29u'
    'c2VudF9hY2tub3dsZWRnZWQYBiABKAhSE2NvbnNlbnRBY2tub3dsZWRnZWQ=');

@$core.Deprecated('Use listConnectionsRequestDescriptor instead')
const ListConnectionsRequest$json = {
  '1': 'ListConnectionsRequest',
};

/// Descriptor for `ListConnectionsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listConnectionsRequestDescriptor =
    $convert.base64Decode('ChZMaXN0Q29ubmVjdGlvbnNSZXF1ZXN0');

@$core.Deprecated('Use listConnectionsResponseDescriptor instead')
const ListConnectionsResponse$json = {
  '1': 'ListConnectionsResponse',
  '2': [
    {
      '1': 'connections',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.Connection',
      '10': 'connections'
    },
  ],
};

/// Descriptor for `ListConnectionsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listConnectionsResponseDescriptor =
    $convert.base64Decode(
        'ChdMaXN0Q29ubmVjdGlvbnNSZXNwb25zZRI3Cgtjb25uZWN0aW9ucxgBIAMoCzIVLnR1cmluZy'
        '52MS5Db25uZWN0aW9uUgtjb25uZWN0aW9ucw==');

@$core.Deprecated('Use getConnectionRequestDescriptor instead')
const GetConnectionRequest$json = {
  '1': 'GetConnectionRequest',
  '2': [
    {'1': 'connection_id', '3': 1, '4': 1, '5': 9, '10': 'connectionId'},
  ],
};

/// Descriptor for `GetConnectionRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getConnectionRequestDescriptor = $convert.base64Decode(
    'ChRHZXRDb25uZWN0aW9uUmVxdWVzdBIjCg1jb25uZWN0aW9uX2lkGAEgASgJUgxjb25uZWN0aW'
    '9uSWQ=');

@$core.Deprecated('Use revokeConnectionRequestDescriptor instead')
const RevokeConnectionRequest$json = {
  '1': 'RevokeConnectionRequest',
  '2': [
    {'1': 'connection_id', '3': 1, '4': 1, '5': 9, '10': 'connectionId'},
  ],
};

/// Descriptor for `RevokeConnectionRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List revokeConnectionRequestDescriptor =
    $convert.base64Decode(
        'ChdSZXZva2VDb25uZWN0aW9uUmVxdWVzdBIjCg1jb25uZWN0aW9uX2lkGAEgASgJUgxjb25uZW'
        'N0aW9uSWQ=');

@$core.Deprecated('Use deleteConnectionRequestDescriptor instead')
const DeleteConnectionRequest$json = {
  '1': 'DeleteConnectionRequest',
  '2': [
    {'1': 'connection_id', '3': 1, '4': 1, '5': 9, '10': 'connectionId'},
  ],
};

/// Descriptor for `DeleteConnectionRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List deleteConnectionRequestDescriptor =
    $convert.base64Decode(
        'ChdEZWxldGVDb25uZWN0aW9uUmVxdWVzdBIjCg1jb25uZWN0aW9uX2lkGAEgASgJUgxjb25uZW'
        'N0aW9uSWQ=');

@$core.Deprecated('Use deleteConnectionResponseDescriptor instead')
const DeleteConnectionResponse$json = {
  '1': 'DeleteConnectionResponse',
};

/// Descriptor for `DeleteConnectionResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List deleteConnectionResponseDescriptor =
    $convert.base64Decode('ChhEZWxldGVDb25uZWN0aW9uUmVzcG9uc2U=');

@$core.Deprecated('Use integrationToolDescriptorDescriptor instead')
const IntegrationToolDescriptor$json = {
  '1': 'IntegrationToolDescriptor',
  '2': [
    {'1': 'tool_name', '3': 1, '4': 1, '5': 9, '10': 'toolName'},
    {'1': 'description', '3': 2, '4': 1, '5': 9, '10': 'description'},
    {
      '1': 'schema',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'schema'
    },
    {'1': 'read_only', '3': 4, '4': 1, '5': 8, '10': 'readOnly'},
    {
      '1': 'policy',
      '3': 5,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ToolPolicy',
      '10': 'policy'
    },
  ],
};

/// Descriptor for `IntegrationToolDescriptor`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List integrationToolDescriptorDescriptor = $convert.base64Decode(
    'ChlJbnRlZ3JhdGlvblRvb2xEZXNjcmlwdG9yEhsKCXRvb2xfbmFtZRgBIAEoCVIIdG9vbE5hbW'
    'USIAoLZGVzY3JpcHRpb24YAiABKAlSC2Rlc2NyaXB0aW9uEi8KBnNjaGVtYRgDIAEoCzIXLmdv'
    'b2dsZS5wcm90b2J1Zi5TdHJ1Y3RSBnNjaGVtYRIbCglyZWFkX29ubHkYBCABKAhSCHJlYWRPbm'
    'x5Ei0KBnBvbGljeRgFIAEoDjIVLnR1cmluZy52MS5Ub29sUG9saWN5UgZwb2xpY3k=');

@$core.Deprecated('Use listIntegrationToolsRequestDescriptor instead')
const ListIntegrationToolsRequest$json = {
  '1': 'ListIntegrationToolsRequest',
};

/// Descriptor for `ListIntegrationToolsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listIntegrationToolsRequestDescriptor =
    $convert.base64Decode('ChtMaXN0SW50ZWdyYXRpb25Ub29sc1JlcXVlc3Q=');

@$core.Deprecated('Use listIntegrationToolsResponseDescriptor instead')
const ListIntegrationToolsResponse$json = {
  '1': 'ListIntegrationToolsResponse',
  '2': [
    {
      '1': 'tools',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.IntegrationToolDescriptor',
      '10': 'tools'
    },
  ],
};

/// Descriptor for `ListIntegrationToolsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listIntegrationToolsResponseDescriptor =
    $convert.base64Decode(
        'ChxMaXN0SW50ZWdyYXRpb25Ub29sc1Jlc3BvbnNlEjoKBXRvb2xzGAEgAygLMiQudHVyaW5nLn'
        'YxLkludGVncmF0aW9uVG9vbERlc2NyaXB0b3JSBXRvb2xz');

@$core.Deprecated('Use callIntegrationToolRequestDescriptor instead')
const CallIntegrationToolRequest$json = {
  '1': 'CallIntegrationToolRequest',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'approval_id', '3': 2, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'tool_name', '3': 3, '4': 1, '5': 9, '10': 'toolName'},
    {
      '1': 'args',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'args'
    },
  ],
};

/// Descriptor for `CallIntegrationToolRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List callIntegrationToolRequestDescriptor =
    $convert.base64Decode(
        'ChpDYWxsSW50ZWdyYXRpb25Ub29sUmVxdWVzdBIVCgZydW5faWQYASABKAlSBXJ1bklkEh8KC2'
        'FwcHJvdmFsX2lkGAIgASgJUgphcHByb3ZhbElkEhsKCXRvb2xfbmFtZRgDIAEoCVIIdG9vbE5h'
        'bWUSKwoEYXJncxgEIAEoCzIXLmdvb2dsZS5wcm90b2J1Zi5TdHJ1Y3RSBGFyZ3M=');

@$core.Deprecated('Use callIntegrationToolResponseDescriptor instead')
const CallIntegrationToolResponse$json = {
  '1': 'CallIntegrationToolResponse',
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

/// Descriptor for `CallIntegrationToolResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List callIntegrationToolResponseDescriptor =
    $convert.base64Decode(
        'ChtDYWxsSW50ZWdyYXRpb25Ub29sUmVzcG9uc2USLwoGcmVzdWx0GAEgASgLMhcuZ29vZ2xlLn'
        'Byb3RvYnVmLlN0cnVjdFIGcmVzdWx0');
