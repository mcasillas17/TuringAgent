// This is a generated file - do not edit.
//
// Generated from turing/v1/sessions.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use sessionDeletionStateDescriptor instead')
const SessionDeletionState$json = {
  '1': 'SessionDeletionState',
  '2': [
    {'1': 'SESSION_DELETION_STATE_UNSPECIFIED', '2': 0},
    {'1': 'SESSION_DELETION_STATE_IN_PROGRESS', '2': 1},
    {'1': 'SESSION_DELETION_STATE_FAILED_EXTERNAL', '2': 2},
    {'1': 'SESSION_DELETION_STATE_COMPLETED', '2': 3},
  ],
};

/// Descriptor for `SessionDeletionState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List sessionDeletionStateDescriptor = $convert.base64Decode(
    'ChRTZXNzaW9uRGVsZXRpb25TdGF0ZRImCiJTRVNTSU9OX0RFTEVUSU9OX1NUQVRFX1VOU1BFQ0'
    'lGSUVEEAASJgoiU0VTU0lPTl9ERUxFVElPTl9TVEFURV9JTl9QUk9HUkVTUxABEioKJlNFU1NJ'
    'T05fREVMRVRJT05fU1RBVEVfRkFJTEVEX0VYVEVSTkFMEAISJAogU0VTU0lPTl9ERUxFVElPTl'
    '9TVEFURV9DT01QTEVURUQQAw==');

@$core.Deprecated('Use sessionDescriptor instead')
const Session$json = {
  '1': 'Session',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'title', '3': 2, '4': 1, '5': 9, '10': 'title'},
    {'1': 'status', '3': 3, '4': 1, '5': 9, '10': 'status'},
    {
      '1': 'created_at',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'createdAt'
    },
    {
      '1': 'updated_at',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'updatedAt'
    },
  ],
};

/// Descriptor for `Session`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List sessionDescriptor = $convert.base64Decode(
    'CgdTZXNzaW9uEh0KCnNlc3Npb25faWQYASABKAlSCXNlc3Npb25JZBIUCgV0aXRsZRgCIAEoCV'
    'IFdGl0bGUSFgoGc3RhdHVzGAMgASgJUgZzdGF0dXMSOQoKY3JlYXRlZF9hdBgEIAEoCzIaLmdv'
    'b2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSCWNyZWF0ZWRBdBI5Cgp1cGRhdGVkX2F0GAUgASgLMh'
    'ouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIJdXBkYXRlZEF0');

@$core.Deprecated('Use createSessionRequestDescriptor instead')
const CreateSessionRequest$json = {
  '1': 'CreateSessionRequest',
  '2': [
    {'1': 'title', '3': 1, '4': 1, '5': 9, '10': 'title'},
  ],
};

/// Descriptor for `CreateSessionRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List createSessionRequestDescriptor =
    $convert.base64Decode(
        'ChRDcmVhdGVTZXNzaW9uUmVxdWVzdBIUCgV0aXRsZRgBIAEoCVIFdGl0bGU=');

@$core.Deprecated('Use createSessionResponseDescriptor instead')
const CreateSessionResponse$json = {
  '1': 'CreateSessionResponse',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
    {
      '1': 'created_at',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'createdAt'
    },
  ],
};

/// Descriptor for `CreateSessionResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List createSessionResponseDescriptor = $convert.base64Decode(
    'ChVDcmVhdGVTZXNzaW9uUmVzcG9uc2USHQoKc2Vzc2lvbl9pZBgBIAEoCVIJc2Vzc2lvbklkEj'
    'kKCmNyZWF0ZWRfYXQYAiABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wUgljcmVhdGVk'
    'QXQ=');

@$core.Deprecated('Use listSessionsRequestDescriptor instead')
const ListSessionsRequest$json = {
  '1': 'ListSessionsRequest',
  '2': [
    {
      '1': 'page',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.PageRequest',
      '10': 'page'
    },
  ],
};

/// Descriptor for `ListSessionsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listSessionsRequestDescriptor = $convert.base64Decode(
    'ChNMaXN0U2Vzc2lvbnNSZXF1ZXN0EioKBHBhZ2UYASABKAsyFi50dXJpbmcudjEuUGFnZVJlcX'
    'Vlc3RSBHBhZ2U=');

@$core.Deprecated('Use listSessionsResponseDescriptor instead')
const ListSessionsResponse$json = {
  '1': 'ListSessionsResponse',
  '2': [
    {
      '1': 'sessions',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.Session',
      '10': 'sessions'
    },
    {
      '1': 'page',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.PageResponse',
      '10': 'page'
    },
  ],
};

/// Descriptor for `ListSessionsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listSessionsResponseDescriptor = $convert.base64Decode(
    'ChRMaXN0U2Vzc2lvbnNSZXNwb25zZRIuCghzZXNzaW9ucxgBIAMoCzISLnR1cmluZy52MS5TZX'
    'NzaW9uUghzZXNzaW9ucxIrCgRwYWdlGAIgASgLMhcudHVyaW5nLnYxLlBhZ2VSZXNwb25zZVIE'
    'cGFnZQ==');

@$core.Deprecated('Use getSessionRequestDescriptor instead')
const GetSessionRequest$json = {
  '1': 'GetSessionRequest',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
  ],
};

/// Descriptor for `GetSessionRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getSessionRequestDescriptor = $convert.base64Decode(
    'ChFHZXRTZXNzaW9uUmVxdWVzdBIdCgpzZXNzaW9uX2lkGAEgASgJUglzZXNzaW9uSWQ=');

@$core.Deprecated('Use deleteSessionRequestDescriptor instead')
const DeleteSessionRequest$json = {
  '1': 'DeleteSessionRequest',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
  ],
};

/// Descriptor for `DeleteSessionRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List deleteSessionRequestDescriptor = $convert.base64Decode(
    'ChREZWxldGVTZXNzaW9uUmVxdWVzdBIdCgpzZXNzaW9uX2lkGAEgASgJUglzZXNzaW9uSWQ=');

@$core.Deprecated('Use sessionDeletionReceiptDescriptor instead')
const SessionDeletionReceipt$json = {
  '1': 'SessionDeletionReceipt',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
    {
      '1': 'state',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.SessionDeletionState',
      '10': 'state'
    },
    {
      '1': 'lifecycle_version',
      '3': 3,
      '4': 1,
      '5': 3,
      '10': 'lifecycleVersion'
    },
    {'1': 'retryable', '3': 4, '4': 1, '5': 8, '10': 'retryable'},
    {'1': 'error_code', '3': 5, '4': 1, '5': 9, '10': 'errorCode'},
    {
      '1': 'terminal_sequence',
      '3': 6,
      '4': 1,
      '5': 3,
      '10': 'terminalSequence'
    },
    {'1': 'run_count', '3': 7, '4': 1, '5': 5, '10': 'runCount'},
    {'1': 'message_count', '3': 8, '4': 1, '5': 5, '10': 'messageCount'},
    {
      '1': 'retained_legacy_artifact_count',
      '3': 9,
      '4': 1,
      '5': 5,
      '10': 'retainedLegacyArtifactCount'
    },
  ],
};

/// Descriptor for `SessionDeletionReceipt`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List sessionDeletionReceiptDescriptor = $convert.base64Decode(
    'ChZTZXNzaW9uRGVsZXRpb25SZWNlaXB0Eh0KCnNlc3Npb25faWQYASABKAlSCXNlc3Npb25JZB'
    'I1CgVzdGF0ZRgCIAEoDjIfLnR1cmluZy52MS5TZXNzaW9uRGVsZXRpb25TdGF0ZVIFc3RhdGUS'
    'KwoRbGlmZWN5Y2xlX3ZlcnNpb24YAyABKANSEGxpZmVjeWNsZVZlcnNpb24SHAoJcmV0cnlhYm'
    'xlGAQgASgIUglyZXRyeWFibGUSHQoKZXJyb3JfY29kZRgFIAEoCVIJZXJyb3JDb2RlEisKEXRl'
    'cm1pbmFsX3NlcXVlbmNlGAYgASgDUhB0ZXJtaW5hbFNlcXVlbmNlEhsKCXJ1bl9jb3VudBgHIA'
    'EoBVIIcnVuQ291bnQSIwoNbWVzc2FnZV9jb3VudBgIIAEoBVIMbWVzc2FnZUNvdW50EkMKHnJl'
    'dGFpbmVkX2xlZ2FjeV9hcnRpZmFjdF9jb3VudBgJIAEoBVIbcmV0YWluZWRMZWdhY3lBcnRpZm'
    'FjdENvdW50');

@$core.Deprecated('Use listSessionDeletionReceiptsRequestDescriptor instead')
const ListSessionDeletionReceiptsRequest$json = {
  '1': 'ListSessionDeletionReceiptsRequest',
};

/// Descriptor for `ListSessionDeletionReceiptsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listSessionDeletionReceiptsRequestDescriptor =
    $convert.base64Decode('CiJMaXN0U2Vzc2lvbkRlbGV0aW9uUmVjZWlwdHNSZXF1ZXN0');

@$core.Deprecated('Use listSessionDeletionReceiptsResponseDescriptor instead')
const ListSessionDeletionReceiptsResponse$json = {
  '1': 'ListSessionDeletionReceiptsResponse',
  '2': [
    {
      '1': 'deletions',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.SessionDeletionReceipt',
      '10': 'deletions'
    },
  ],
};

/// Descriptor for `ListSessionDeletionReceiptsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listSessionDeletionReceiptsResponseDescriptor =
    $convert.base64Decode(
        'CiNMaXN0U2Vzc2lvbkRlbGV0aW9uUmVjZWlwdHNSZXNwb25zZRI/CglkZWxldGlvbnMYASADKA'
        'syIS50dXJpbmcudjEuU2Vzc2lvbkRlbGV0aW9uUmVjZWlwdFIJZGVsZXRpb25z');

@$core.Deprecated('Use deleteSessionResponseDescriptor instead')
const DeleteSessionResponse$json = {
  '1': 'DeleteSessionResponse',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
    {
      '1': 'deletion',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.SessionDeletionReceipt',
      '10': 'deletion'
    },
  ],
};

/// Descriptor for `DeleteSessionResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List deleteSessionResponseDescriptor = $convert.base64Decode(
    'ChVEZWxldGVTZXNzaW9uUmVzcG9uc2USHQoKc2Vzc2lvbl9pZBgBIAEoCVIJc2Vzc2lvbklkEj'
    '0KCGRlbGV0aW9uGAIgASgLMiEudHVyaW5nLnYxLlNlc3Npb25EZWxldGlvblJlY2VpcHRSCGRl'
    'bGV0aW9u');

@$core.Deprecated('Use listMessagesRequestDescriptor instead')
const ListMessagesRequest$json = {
  '1': 'ListMessagesRequest',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'limit', '3': 2, '4': 1, '5': 5, '10': 'limit'},
    {'1': 'before_message_id', '3': 3, '4': 1, '5': 9, '10': 'beforeMessageId'},
  ],
};

/// Descriptor for `ListMessagesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listMessagesRequestDescriptor = $convert.base64Decode(
    'ChNMaXN0TWVzc2FnZXNSZXF1ZXN0Eh0KCnNlc3Npb25faWQYASABKAlSCXNlc3Npb25JZBIUCg'
    'VsaW1pdBgCIAEoBVIFbGltaXQSKgoRYmVmb3JlX21lc3NhZ2VfaWQYAyABKAlSD2JlZm9yZU1l'
    'c3NhZ2VJZA==');

@$core.Deprecated('Use listMessagesResponseDescriptor instead')
const ListMessagesResponse$json = {
  '1': 'ListMessagesResponse',
  '2': [
    {
      '1': 'messages',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.Message',
      '10': 'messages'
    },
  ],
};

/// Descriptor for `ListMessagesResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listMessagesResponseDescriptor = $convert.base64Decode(
    'ChRMaXN0TWVzc2FnZXNSZXNwb25zZRIuCghtZXNzYWdlcxgBIAMoCzISLnR1cmluZy52MS5NZX'
    'NzYWdlUghtZXNzYWdlcw==');

@$core.Deprecated('Use searchMessagesRequestDescriptor instead')
const SearchMessagesRequest$json = {
  '1': 'SearchMessagesRequest',
  '2': [
    {'1': 'query', '3': 1, '4': 1, '5': 9, '10': 'query'},
    {'1': 'session_id', '3': 2, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'limit', '3': 3, '4': 1, '5': 5, '10': 'limit'},
    {
      '1': 'exclude_session_id',
      '3': 4,
      '4': 1,
      '5': 9,
      '10': 'excludeSessionId'
    },
  ],
};

/// Descriptor for `SearchMessagesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List searchMessagesRequestDescriptor = $convert.base64Decode(
    'ChVTZWFyY2hNZXNzYWdlc1JlcXVlc3QSFAoFcXVlcnkYASABKAlSBXF1ZXJ5Eh0KCnNlc3Npb2'
    '5faWQYAiABKAlSCXNlc3Npb25JZBIUCgVsaW1pdBgDIAEoBVIFbGltaXQSLAoSZXhjbHVkZV9z'
    'ZXNzaW9uX2lkGAQgASgJUhBleGNsdWRlU2Vzc2lvbklk');

@$core.Deprecated('Use searchMessagesResponseDescriptor instead')
const SearchMessagesResponse$json = {
  '1': 'SearchMessagesResponse',
  '2': [
    {
      '1': 'messages',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.Message',
      '10': 'messages'
    },
  ],
};

/// Descriptor for `SearchMessagesResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List searchMessagesResponseDescriptor =
    $convert.base64Decode(
        'ChZTZWFyY2hNZXNzYWdlc1Jlc3BvbnNlEi4KCG1lc3NhZ2VzGAEgAygLMhIudHVyaW5nLnYxLk'
        '1lc3NhZ2VSCG1lc3NhZ2Vz');

@$core.Deprecated('Use getConfigRequestDescriptor instead')
const GetConfigRequest$json = {
  '1': 'GetConfigRequest',
};

/// Descriptor for `GetConfigRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getConfigRequestDescriptor =
    $convert.base64Decode('ChBHZXRDb25maWdSZXF1ZXN0');

@$core.Deprecated('Use getConfigResponseDescriptor instead')
const GetConfigResponse$json = {
  '1': 'GetConfigResponse',
  '2': [
    {
      '1': 'providers',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.ProviderConfig',
      '10': 'providers'
    },
    {
      '1': 'approvals_enabled',
      '3': 2,
      '4': 1,
      '5': 8,
      '10': 'approvalsEnabled'
    },
    {'1': 'files_mcp_enabled', '3': 3, '4': 1, '5': 8, '10': 'filesMcpEnabled'},
  ],
};

/// Descriptor for `GetConfigResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getConfigResponseDescriptor = $convert.base64Decode(
    'ChFHZXRDb25maWdSZXNwb25zZRI3Cglwcm92aWRlcnMYASADKAsyGS50dXJpbmcudjEuUHJvdm'
    'lkZXJDb25maWdSCXByb3ZpZGVycxIrChFhcHByb3ZhbHNfZW5hYmxlZBgCIAEoCFIQYXBwcm92'
    'YWxzRW5hYmxlZBIqChFmaWxlc19tY3BfZW5hYmxlZBgDIAEoCFIPZmlsZXNNY3BFbmFibGVk');

@$core.Deprecated('Use listAgentsRequestDescriptor instead')
const ListAgentsRequest$json = {
  '1': 'ListAgentsRequest',
};

/// Descriptor for `ListAgentsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listAgentsRequestDescriptor =
    $convert.base64Decode('ChFMaXN0QWdlbnRzUmVxdWVzdA==');

@$core.Deprecated('Use listAgentsResponseDescriptor instead')
const ListAgentsResponse$json = {
  '1': 'ListAgentsResponse',
  '2': [
    {
      '1': 'agents',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.AgentDescriptor',
      '10': 'agents'
    },
  ],
};

/// Descriptor for `ListAgentsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listAgentsResponseDescriptor = $convert.base64Decode(
    'ChJMaXN0QWdlbnRzUmVzcG9uc2USMgoGYWdlbnRzGAEgAygLMhoudHVyaW5nLnYxLkFnZW50RG'
    'VzY3JpcHRvclIGYWdlbnRz');

@$core.Deprecated('Use toolDescriptorDescriptor instead')
const ToolDescriptor$json = {
  '1': 'ToolDescriptor',
  '2': [
    {'1': 'server_name', '3': 1, '4': 1, '5': 9, '10': 'serverName'},
    {'1': 'tool_name', '3': 2, '4': 1, '5': 9, '10': 'toolName'},
    {
      '1': 'policy',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ToolPolicy',
      '10': 'policy'
    },
  ],
};

/// Descriptor for `ToolDescriptor`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List toolDescriptorDescriptor = $convert.base64Decode(
    'Cg5Ub29sRGVzY3JpcHRvchIfCgtzZXJ2ZXJfbmFtZRgBIAEoCVIKc2VydmVyTmFtZRIbCgl0b2'
    '9sX25hbWUYAiABKAlSCHRvb2xOYW1lEi0KBnBvbGljeRgDIAEoDjIVLnR1cmluZy52MS5Ub29s'
    'UG9saWN5UgZwb2xpY3k=');

@$core.Deprecated('Use listToolsRequestDescriptor instead')
const ListToolsRequest$json = {
  '1': 'ListToolsRequest',
};

/// Descriptor for `ListToolsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listToolsRequestDescriptor =
    $convert.base64Decode('ChBMaXN0VG9vbHNSZXF1ZXN0');

@$core.Deprecated('Use listToolsResponseDescriptor instead')
const ListToolsResponse$json = {
  '1': 'ListToolsResponse',
  '2': [
    {
      '1': 'tools',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.ToolDescriptor',
      '10': 'tools'
    },
  ],
};

/// Descriptor for `ListToolsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listToolsResponseDescriptor = $convert.base64Decode(
    'ChFMaXN0VG9vbHNSZXNwb25zZRIvCgV0b29scxgBIAMoCzIZLnR1cmluZy52MS5Ub29sRGVzY3'
    'JpcHRvclIFdG9vbHM=');
