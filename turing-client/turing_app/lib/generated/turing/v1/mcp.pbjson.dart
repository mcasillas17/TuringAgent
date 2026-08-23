// This is a generated file - do not edit.
//
// Generated from turing/v1/mcp.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use mcpServerTierDescriptor instead')
const McpServerTier$json = {
  '1': 'McpServerTier',
  '2': [
    {'1': 'MCP_SERVER_TIER_UNSPECIFIED', '2': 0},
    {'1': 'MCP_SERVER_TIER_BUNDLED', '2': 1},
    {'1': 'MCP_SERVER_TIER_LOCAL_CONTAINER', '2': 2},
    {'1': 'MCP_SERVER_TIER_REMOTE_URL', '2': 3},
  ],
};

/// Descriptor for `McpServerTier`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List mcpServerTierDescriptor = $convert.base64Decode(
    'Cg1NY3BTZXJ2ZXJUaWVyEh8KG01DUF9TRVJWRVJfVElFUl9VTlNQRUNJRklFRBAAEhsKF01DUF'
    '9TRVJWRVJfVElFUl9CVU5ETEVEEAESIwofTUNQX1NFUlZFUl9USUVSX0xPQ0FMX0NPTlRBSU5F'
    'UhACEh4KGk1DUF9TRVJWRVJfVElFUl9SRU1PVEVfVVJMEAM=');

@$core.Deprecated('Use mcpServerLivenessDescriptor instead')
const McpServerLiveness$json = {
  '1': 'McpServerLiveness',
  '2': [
    {'1': 'MCP_SERVER_LIVENESS_UNSPECIFIED', '2': 0},
    {'1': 'MCP_SERVER_LIVENESS_UNKNOWN', '2': 1},
    {'1': 'MCP_SERVER_LIVENESS_UP', '2': 2},
    {'1': 'MCP_SERVER_LIVENESS_DOWN', '2': 3},
  ],
};

/// Descriptor for `McpServerLiveness`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List mcpServerLivenessDescriptor = $convert.base64Decode(
    'ChFNY3BTZXJ2ZXJMaXZlbmVzcxIjCh9NQ1BfU0VSVkVSX0xJVkVORVNTX1VOU1BFQ0lGSUVEEA'
    'ASHwobTUNQX1NFUlZFUl9MSVZFTkVTU19VTktOT1dOEAESGgoWTUNQX1NFUlZFUl9MSVZFTkVT'
    'U19VUBACEhwKGE1DUF9TRVJWRVJfTElWRU5FU1NfRE9XThAD');

@$core.Deprecated('Use mcpToolDescriptorDescriptor instead')
const McpToolDescriptor$json = {
  '1': 'McpToolDescriptor',
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
    {'1': 'present', '3': 5, '4': 1, '5': 8, '10': 'present'},
  ],
};

/// Descriptor for `McpToolDescriptor`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List mcpToolDescriptorDescriptor = $convert.base64Decode(
    'ChFNY3BUb29sRGVzY3JpcHRvchIbCgl0b29sX25hbWUYASABKAlSCHRvb2xOYW1lEi0KBnBvbG'
    'ljeRgCIAEoDjIVLnR1cmluZy52MS5Ub29sUG9saWN5UgZwb2xpY3kSLwoGc2NoZW1hGAMgASgL'
    'MhcuZ29vZ2xlLnByb3RvYnVmLlN0cnVjdFIGc2NoZW1hEhgKB2VuYWJsZWQYBCABKAhSB2VuYW'
    'JsZWQSGAoHcHJlc2VudBgFIAEoCFIHcHJlc2VudA==');

@$core.Deprecated('Use mcpServerDescriptorDescriptor instead')
const McpServerDescriptor$json = {
  '1': 'McpServerDescriptor',
  '2': [
    {'1': 'server_id', '3': 1, '4': 1, '5': 9, '10': 'serverId'},
    {'1': 'name', '3': 2, '4': 1, '5': 9, '10': 'name'},
    {'1': 'transport', '3': 3, '4': 1, '5': 9, '10': 'transport'},
    {'1': 'url', '3': 4, '4': 1, '5': 9, '10': 'url'},
    {
      '1': 'tier',
      '3': 5,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.McpServerTier',
      '10': 'tier'
    },
    {'1': 'enabled', '3': 6, '4': 1, '5': 8, '10': 'enabled'},
    {
      '1': 'liveness',
      '3': 7,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.McpServerLiveness',
      '10': 'liveness'
    },
    {'1': 'status_message', '3': 8, '4': 1, '5': 9, '10': 'statusMessage'},
    {'1': 'sandbox_confined', '3': 9, '4': 1, '5': 8, '10': 'sandboxConfined'},
    {
      '1': 'tools',
      '3': 10,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.McpToolDescriptor',
      '10': 'tools'
    },
  ],
};

/// Descriptor for `McpServerDescriptor`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List mcpServerDescriptorDescriptor = $convert.base64Decode(
    'ChNNY3BTZXJ2ZXJEZXNjcmlwdG9yEhsKCXNlcnZlcl9pZBgBIAEoCVIIc2VydmVySWQSEgoEbm'
    'FtZRgCIAEoCVIEbmFtZRIcCgl0cmFuc3BvcnQYAyABKAlSCXRyYW5zcG9ydBIQCgN1cmwYBCAB'
    'KAlSA3VybBIsCgR0aWVyGAUgASgOMhgudHVyaW5nLnYxLk1jcFNlcnZlclRpZXJSBHRpZXISGA'
    'oHZW5hYmxlZBgGIAEoCFIHZW5hYmxlZBI4CghsaXZlbmVzcxgHIAEoDjIcLnR1cmluZy52MS5N'
    'Y3BTZXJ2ZXJMaXZlbmVzc1IIbGl2ZW5lc3MSJQoOc3RhdHVzX21lc3NhZ2UYCCABKAlSDXN0YX'
    'R1c01lc3NhZ2USKQoQc2FuZGJveF9jb25maW5lZBgJIAEoCFIPc2FuZGJveENvbmZpbmVkEjIK'
    'BXRvb2xzGAogAygLMhwudHVyaW5nLnYxLk1jcFRvb2xEZXNjcmlwdG9yUgV0b29scw==');

@$core.Deprecated('Use unsupportedMcpServerDescriptor instead')
const UnsupportedMcpServer$json = {
  '1': 'UnsupportedMcpServer',
  '2': [
    {'1': 'name', '3': 1, '4': 1, '5': 9, '10': 'name'},
    {'1': 'reason', '3': 2, '4': 1, '5': 9, '10': 'reason'},
  ],
};

/// Descriptor for `UnsupportedMcpServer`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List unsupportedMcpServerDescriptor = $convert.base64Decode(
    'ChRVbnN1cHBvcnRlZE1jcFNlcnZlchISCgRuYW1lGAEgASgJUgRuYW1lEhYKBnJlYXNvbhgCIA'
    'EoCVIGcmVhc29u');

@$core.Deprecated('Use listMcpServersRequestDescriptor instead')
const ListMcpServersRequest$json = {
  '1': 'ListMcpServersRequest',
};

/// Descriptor for `ListMcpServersRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listMcpServersRequestDescriptor =
    $convert.base64Decode('ChVMaXN0TWNwU2VydmVyc1JlcXVlc3Q=');

@$core.Deprecated('Use listMcpServersResponseDescriptor instead')
const ListMcpServersResponse$json = {
  '1': 'ListMcpServersResponse',
  '2': [
    {
      '1': 'servers',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.McpServerDescriptor',
      '10': 'servers'
    },
    {
      '1': 'unsupported',
      '3': 2,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.UnsupportedMcpServer',
      '10': 'unsupported'
    },
  ],
};

/// Descriptor for `ListMcpServersResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listMcpServersResponseDescriptor = $convert.base64Decode(
    'ChZMaXN0TWNwU2VydmVyc1Jlc3BvbnNlEjgKB3NlcnZlcnMYASADKAsyHi50dXJpbmcudjEuTW'
    'NwU2VydmVyRGVzY3JpcHRvclIHc2VydmVycxJBCgt1bnN1cHBvcnRlZBgCIAMoCzIfLnR1cmlu'
    'Zy52MS5VbnN1cHBvcnRlZE1jcFNlcnZlclILdW5zdXBwb3J0ZWQ=');

@$core.Deprecated('Use setMcpServerEnabledRequestDescriptor instead')
const SetMcpServerEnabledRequest$json = {
  '1': 'SetMcpServerEnabledRequest',
  '2': [
    {'1': 'server_id', '3': 1, '4': 1, '5': 9, '10': 'serverId'},
    {'1': 'enabled', '3': 2, '4': 1, '5': 8, '10': 'enabled'},
  ],
};

/// Descriptor for `SetMcpServerEnabledRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setMcpServerEnabledRequestDescriptor =
    $convert.base64Decode(
        'ChpTZXRNY3BTZXJ2ZXJFbmFibGVkUmVxdWVzdBIbCglzZXJ2ZXJfaWQYASABKAlSCHNlcnZlck'
        'lkEhgKB2VuYWJsZWQYAiABKAhSB2VuYWJsZWQ=');

@$core.Deprecated('Use updateMcpToolPolicyRequestDescriptor instead')
const UpdateMcpToolPolicyRequest$json = {
  '1': 'UpdateMcpToolPolicyRequest',
  '2': [
    {'1': 'server_id', '3': 1, '4': 1, '5': 9, '10': 'serverId'},
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

/// Descriptor for `UpdateMcpToolPolicyRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List updateMcpToolPolicyRequestDescriptor =
    $convert.base64Decode(
        'ChpVcGRhdGVNY3BUb29sUG9saWN5UmVxdWVzdBIbCglzZXJ2ZXJfaWQYASABKAlSCHNlcnZlck'
        'lkEhsKCXRvb2xfbmFtZRgCIAEoCVIIdG9vbE5hbWUSLQoGcG9saWN5GAMgASgOMhUudHVyaW5n'
        'LnYxLlRvb2xQb2xpY3lSBnBvbGljeQ==');

@$core.Deprecated('Use updateToolPolicyByNameRequestDescriptor instead')
const UpdateToolPolicyByNameRequest$json = {
  '1': 'UpdateToolPolicyByNameRequest',
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

/// Descriptor for `UpdateToolPolicyByNameRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List updateToolPolicyByNameRequestDescriptor =
    $convert.base64Decode(
        'Ch1VcGRhdGVUb29sUG9saWN5QnlOYW1lUmVxdWVzdBIfCgtzZXJ2ZXJfbmFtZRgBIAEoCVIKc2'
        'VydmVyTmFtZRIbCgl0b29sX25hbWUYAiABKAlSCHRvb2xOYW1lEi0KBnBvbGljeRgDIAEoDjIV'
        'LnR1cmluZy52MS5Ub29sUG9saWN5UgZwb2xpY3k=');

@$core.Deprecated('Use listPseudoServerToolsRequestDescriptor instead')
const ListPseudoServerToolsRequest$json = {
  '1': 'ListPseudoServerToolsRequest',
  '2': [
    {'1': 'server_name', '3': 1, '4': 1, '5': 9, '10': 'serverName'},
  ],
};

/// Descriptor for `ListPseudoServerToolsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listPseudoServerToolsRequestDescriptor =
    $convert.base64Decode(
        'ChxMaXN0UHNldWRvU2VydmVyVG9vbHNSZXF1ZXN0Eh8KC3NlcnZlcl9uYW1lGAEgASgJUgpzZX'
        'J2ZXJOYW1l');

@$core.Deprecated('Use listPseudoServerToolsResponseDescriptor instead')
const ListPseudoServerToolsResponse$json = {
  '1': 'ListPseudoServerToolsResponse',
  '2': [
    {
      '1': 'tools',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.McpToolDescriptor',
      '10': 'tools'
    },
  ],
};

/// Descriptor for `ListPseudoServerToolsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listPseudoServerToolsResponseDescriptor =
    $convert.base64Decode(
        'Ch1MaXN0UHNldWRvU2VydmVyVG9vbHNSZXNwb25zZRIyCgV0b29scxgBIAMoCzIcLnR1cmluZy'
        '52MS5NY3BUb29sRGVzY3JpcHRvclIFdG9vbHM=');

@$core.Deprecated('Use deleteMcpServerRequestDescriptor instead')
const DeleteMcpServerRequest$json = {
  '1': 'DeleteMcpServerRequest',
  '2': [
    {'1': 'server_id', '3': 1, '4': 1, '5': 9, '10': 'serverId'},
  ],
};

/// Descriptor for `DeleteMcpServerRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List deleteMcpServerRequestDescriptor =
    $convert.base64Decode(
        'ChZEZWxldGVNY3BTZXJ2ZXJSZXF1ZXN0EhsKCXNlcnZlcl9pZBgBIAEoCVIIc2VydmVySWQ=');

@$core.Deprecated('Use deleteMcpServerResponseDescriptor instead')
const DeleteMcpServerResponse$json = {
  '1': 'DeleteMcpServerResponse',
};

/// Descriptor for `DeleteMcpServerResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List deleteMcpServerResponseDescriptor =
    $convert.base64Decode('ChdEZWxldGVNY3BTZXJ2ZXJSZXNwb25zZQ==');

@$core.Deprecated('Use callRegisteredMcpToolRequestDescriptor instead')
const CallRegisteredMcpToolRequest$json = {
  '1': 'CallRegisteredMcpToolRequest',
  '2': [
    {'1': 'server_id', '3': 1, '4': 1, '5': 9, '10': 'serverId'},
    {'1': 'run_id', '3': 2, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'approval_id', '3': 3, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'tool_name', '3': 4, '4': 1, '5': 9, '10': 'toolName'},
    {
      '1': 'args',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'args'
    },
  ],
};

/// Descriptor for `CallRegisteredMcpToolRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List callRegisteredMcpToolRequestDescriptor = $convert.base64Decode(
    'ChxDYWxsUmVnaXN0ZXJlZE1jcFRvb2xSZXF1ZXN0EhsKCXNlcnZlcl9pZBgBIAEoCVIIc2Vydm'
    'VySWQSFQoGcnVuX2lkGAIgASgJUgVydW5JZBIfCgthcHByb3ZhbF9pZBgDIAEoCVIKYXBwcm92'
    'YWxJZBIbCgl0b29sX25hbWUYBCABKAlSCHRvb2xOYW1lEisKBGFyZ3MYBSABKAsyFy5nb29nbG'
    'UucHJvdG9idWYuU3RydWN0UgRhcmdz');

@$core.Deprecated('Use callRegisteredMcpToolResponseDescriptor instead')
const CallRegisteredMcpToolResponse$json = {
  '1': 'CallRegisteredMcpToolResponse',
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

/// Descriptor for `CallRegisteredMcpToolResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List callRegisteredMcpToolResponseDescriptor =
    $convert.base64Decode(
        'Ch1DYWxsUmVnaXN0ZXJlZE1jcFRvb2xSZXNwb25zZRIvCgZyZXN1bHQYASABKAsyFy5nb29nbG'
        'UucHJvdG9idWYuU3RydWN0UgZyZXN1bHQ=');

@$core.Deprecated('Use mcpRequestDescriptor instead')
const McpRequest$json = {
  '1': 'McpRequest',
  '2': [
    {'1': 'server_name', '3': 1, '4': 1, '5': 9, '10': 'serverName'},
    {'1': 'method', '3': 2, '4': 1, '5': 9, '10': 'method'},
    {
      '1': 'params',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'params'
    },
  ],
};

/// Descriptor for `McpRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List mcpRequestDescriptor = $convert.base64Decode(
    'CgpNY3BSZXF1ZXN0Eh8KC3NlcnZlcl9uYW1lGAEgASgJUgpzZXJ2ZXJOYW1lEhYKBm1ldGhvZB'
    'gCIAEoCVIGbWV0aG9kEi8KBnBhcmFtcxgDIAEoCzIXLmdvb2dsZS5wcm90b2J1Zi5TdHJ1Y3RS'
    'BnBhcmFtcw==');

@$core.Deprecated('Use mcpResultDescriptor instead')
const McpResult$json = {
  '1': 'McpResult',
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

/// Descriptor for `McpResult`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List mcpResultDescriptor = $convert.base64Decode(
    'CglNY3BSZXN1bHQSLwoGcmVzdWx0GAEgASgLMhcuZ29vZ2xlLnByb3RvYnVmLlN0cnVjdFIGcm'
    'VzdWx0');
