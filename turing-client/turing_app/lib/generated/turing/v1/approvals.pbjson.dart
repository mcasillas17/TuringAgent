// This is a generated file - do not edit.
//
// Generated from turing/v1/approvals.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use approvalStatusDescriptor instead')
const ApprovalStatus$json = {
  '1': 'ApprovalStatus',
  '2': [
    {'1': 'APPROVAL_STATUS_UNSPECIFIED', '2': 0},
    {'1': 'APPROVAL_STATUS_PENDING', '2': 1},
    {'1': 'APPROVAL_STATUS_APPROVED', '2': 2},
    {'1': 'APPROVAL_STATUS_DENIED', '2': 3},
    {'1': 'APPROVAL_STATUS_EXPIRED', '2': 4},
    {'1': 'APPROVAL_STATUS_CONSUMED', '2': 5},
  ],
};

/// Descriptor for `ApprovalStatus`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List approvalStatusDescriptor = $convert.base64Decode(
    'Cg5BcHByb3ZhbFN0YXR1cxIfChtBUFBST1ZBTF9TVEFUVVNfVU5TUEVDSUZJRUQQABIbChdBUF'
    'BST1ZBTF9TVEFUVVNfUEVORElORxABEhwKGEFQUFJPVkFMX1NUQVRVU19BUFBST1ZFRBACEhoK'
    'FkFQUFJPVkFMX1NUQVRVU19ERU5JRUQQAxIbChdBUFBST1ZBTF9TVEFUVVNfRVhQSVJFRBAEEh'
    'wKGEFQUFJPVkFMX1NUQVRVU19DT05TVU1FRBAF');

@$core.Deprecated('Use approvalPreviewStateDescriptor instead')
const ApprovalPreviewState$json = {
  '1': 'ApprovalPreviewState',
  '2': [
    {'1': 'APPROVAL_PREVIEW_STATE_UNSPECIFIED', '2': 0},
    {'1': 'APPROVAL_PREVIEW_STATE_READY', '2': 1},
    {'1': 'APPROVAL_PREVIEW_STATE_UNAVAILABLE', '2': 2},
    {'1': 'APPROVAL_PREVIEW_STATE_UNSUPPORTED', '2': 3},
    {'1': 'APPROVAL_PREVIEW_STATE_REDACTED', '2': 4},
    {'1': 'APPROVAL_PREVIEW_STATE_OVERSIZED', '2': 5},
    {'1': 'APPROVAL_PREVIEW_STATE_BINARY', '2': 6},
    {'1': 'APPROVAL_PREVIEW_STATE_EXPIRED', '2': 7},
    {'1': 'APPROVAL_PREVIEW_STATE_STALE', '2': 8},
    {'1': 'APPROVAL_PREVIEW_STATE_TERMINAL', '2': 9},
  ],
};

/// Descriptor for `ApprovalPreviewState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List approvalPreviewStateDescriptor = $convert.base64Decode(
    'ChRBcHByb3ZhbFByZXZpZXdTdGF0ZRImCiJBUFBST1ZBTF9QUkVWSUVXX1NUQVRFX1VOU1BFQ0'
    'lGSUVEEAASIAocQVBQUk9WQUxfUFJFVklFV19TVEFURV9SRUFEWRABEiYKIkFQUFJPVkFMX1BS'
    'RVZJRVdfU1RBVEVfVU5BVkFJTEFCTEUQAhImCiJBUFBST1ZBTF9QUkVWSUVXX1NUQVRFX1VOU1'
    'VQUE9SVEVEEAMSIwofQVBQUk9WQUxfUFJFVklFV19TVEFURV9SRURBQ1RFRBAEEiQKIEFQUFJP'
    'VkFMX1BSRVZJRVdfU1RBVEVfT1ZFUlNJWkVEEAUSIQodQVBQUk9WQUxfUFJFVklFV19TVEFURV'
    '9CSU5BUlkQBhIiCh5BUFBST1ZBTF9QUkVWSUVXX1NUQVRFX0VYUElSRUQQBxIgChxBUFBST1ZB'
    'TF9QUkVWSUVXX1NUQVRFX1NUQUxFEAgSIwofQVBQUk9WQUxfUFJFVklFV19TVEFURV9URVJNSU'
    '5BTBAJ');

@$core.Deprecated('Use approveApprovalRequestDescriptor instead')
const ApproveApprovalRequest$json = {
  '1': 'ApproveApprovalRequest',
  '2': [
    {'1': 'approval_id', '3': 1, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'comment', '3': 2, '4': 1, '5': 9, '10': 'comment'},
    {'1': 'preview_hash', '3': 3, '4': 1, '5': 9, '10': 'previewHash'},
    {'1': 'args_hash', '3': 4, '4': 1, '5': 9, '10': 'argsHash'},
  ],
};

/// Descriptor for `ApproveApprovalRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List approveApprovalRequestDescriptor = $convert.base64Decode(
    'ChZBcHByb3ZlQXBwcm92YWxSZXF1ZXN0Eh8KC2FwcHJvdmFsX2lkGAEgASgJUgphcHByb3ZhbE'
    'lkEhgKB2NvbW1lbnQYAiABKAlSB2NvbW1lbnQSIQoMcHJldmlld19oYXNoGAMgASgJUgtwcmV2'
    'aWV3SGFzaBIbCglhcmdzX2hhc2gYBCABKAlSCGFyZ3NIYXNo');

@$core.Deprecated('Use getApprovalDetailsRequestDescriptor instead')
const GetApprovalDetailsRequest$json = {
  '1': 'GetApprovalDetailsRequest',
  '2': [
    {'1': 'approval_id', '3': 1, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'refresh_preview', '3': 2, '4': 1, '5': 8, '10': 'refreshPreview'},
  ],
};

/// Descriptor for `GetApprovalDetailsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getApprovalDetailsRequestDescriptor =
    $convert.base64Decode(
        'ChlHZXRBcHByb3ZhbERldGFpbHNSZXF1ZXN0Eh8KC2FwcHJvdmFsX2lkGAEgASgJUgphcHByb3'
        'ZhbElkEicKD3JlZnJlc2hfcHJldmlldxgCIAEoCFIOcmVmcmVzaFByZXZpZXc=');

@$core.Deprecated('Use approvalDetailsDescriptor instead')
const ApprovalDetails$json = {
  '1': 'ApprovalDetails',
  '2': [
    {'1': 'approval_id', '3': 1, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'session_id', '3': 2, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'run_id', '3': 3, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'tool_call_id', '3': 4, '4': 1, '5': 9, '10': 'toolCallId'},
    {'1': 'tool_name', '3': 5, '4': 1, '5': 9, '10': 'toolName'},
    {'1': 'server_name', '3': 6, '4': 1, '5': 9, '10': 'serverName'},
    {'1': 'args_hash', '3': 7, '4': 1, '5': 9, '10': 'argsHash'},
    {'1': 'preview_hash', '3': 8, '4': 1, '5': 9, '10': 'previewHash'},
    {'1': 'expires_at', '3': 9, '4': 1, '5': 9, '10': 'expiresAt'},
    {
      '1': 'status',
      '3': 10,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ApprovalStatus',
      '10': 'status'
    },
    {
      '1': 'preview_state',
      '3': 11,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ApprovalPreviewState',
      '10': 'previewState'
    },
    {'1': 'arguments_json', '3': 12, '4': 1, '5': 9, '10': 'argumentsJson'},
    {
      '1': 'file_preview',
      '3': 13,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.FileMutationPreview',
      '10': 'filePreview'
    },
    {'1': 'can_approve', '3': 14, '4': 1, '5': 8, '10': 'canApprove'},
    {'1': 'can_deny', '3': 15, '4': 1, '5': 8, '10': 'canDeny'},
  ],
};

/// Descriptor for `ApprovalDetails`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List approvalDetailsDescriptor = $convert.base64Decode(
    'Cg9BcHByb3ZhbERldGFpbHMSHwoLYXBwcm92YWxfaWQYASABKAlSCmFwcHJvdmFsSWQSHQoKc2'
    'Vzc2lvbl9pZBgCIAEoCVIJc2Vzc2lvbklkEhUKBnJ1bl9pZBgDIAEoCVIFcnVuSWQSIAoMdG9v'
    'bF9jYWxsX2lkGAQgASgJUgp0b29sQ2FsbElkEhsKCXRvb2xfbmFtZRgFIAEoCVIIdG9vbE5hbW'
    'USHwoLc2VydmVyX25hbWUYBiABKAlSCnNlcnZlck5hbWUSGwoJYXJnc19oYXNoGAcgASgJUghh'
    'cmdzSGFzaBIhCgxwcmV2aWV3X2hhc2gYCCABKAlSC3ByZXZpZXdIYXNoEh0KCmV4cGlyZXNfYX'
    'QYCSABKAlSCWV4cGlyZXNBdBIxCgZzdGF0dXMYCiABKA4yGS50dXJpbmcudjEuQXBwcm92YWxT'
    'dGF0dXNSBnN0YXR1cxJECg1wcmV2aWV3X3N0YXRlGAsgASgOMh8udHVyaW5nLnYxLkFwcHJvdm'
    'FsUHJldmlld1N0YXRlUgxwcmV2aWV3U3RhdGUSJQoOYXJndW1lbnRzX2pzb24YDCABKAlSDWFy'
    'Z3VtZW50c0pzb24SQQoMZmlsZV9wcmV2aWV3GA0gASgLMh4udHVyaW5nLnYxLkZpbGVNdXRhdG'
    'lvblByZXZpZXdSC2ZpbGVQcmV2aWV3Eh8KC2Nhbl9hcHByb3ZlGA4gASgIUgpjYW5BcHByb3Zl'
    'EhkKCGNhbl9kZW55GA8gASgIUgdjYW5EZW55');

@$core.Deprecated('Use fileMutationPreviewDescriptor instead')
const FileMutationPreview$json = {
  '1': 'FileMutationPreview',
  '2': [
    {'1': 'logical_path', '3': 1, '4': 1, '5': 9, '10': 'logicalPath'},
    {'1': 'physical_path', '3': 2, '4': 1, '5': 9, '10': 'physicalPath'},
    {'1': 'operation', '3': 3, '4': 1, '5': 9, '10': 'operation'},
    {'1': 'before_exists', '3': 4, '4': 1, '5': 8, '10': 'beforeExists'},
    {'1': 'before_hash', '3': 5, '4': 1, '5': 9, '10': 'beforeHash'},
    {'1': 'after_hash', '3': 6, '4': 1, '5': 9, '10': 'afterHash'},
    {'1': 'before_text', '3': 7, '4': 1, '5': 9, '10': 'beforeText'},
    {'1': 'after_text', '3': 8, '4': 1, '5': 9, '10': 'afterText'},
    {'1': 'unified_diff', '3': 9, '4': 1, '5': 9, '10': 'unifiedDiff'},
  ],
};

/// Descriptor for `FileMutationPreview`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List fileMutationPreviewDescriptor = $convert.base64Decode(
    'ChNGaWxlTXV0YXRpb25QcmV2aWV3EiEKDGxvZ2ljYWxfcGF0aBgBIAEoCVILbG9naWNhbFBhdG'
    'gSIwoNcGh5c2ljYWxfcGF0aBgCIAEoCVIMcGh5c2ljYWxQYXRoEhwKCW9wZXJhdGlvbhgDIAEo'
    'CVIJb3BlcmF0aW9uEiMKDWJlZm9yZV9leGlzdHMYBCABKAhSDGJlZm9yZUV4aXN0cxIfCgtiZW'
    'ZvcmVfaGFzaBgFIAEoCVIKYmVmb3JlSGFzaBIdCgphZnRlcl9oYXNoGAYgASgJUglhZnRlckhh'
    'c2gSHwoLYmVmb3JlX3RleHQYByABKAlSCmJlZm9yZVRleHQSHQoKYWZ0ZXJfdGV4dBgIIAEoCV'
    'IJYWZ0ZXJUZXh0EiEKDHVuaWZpZWRfZGlmZhgJIAEoCVILdW5pZmllZERpZmY=');

@$core.Deprecated('Use denyApprovalRequestDescriptor instead')
const DenyApprovalRequest$json = {
  '1': 'DenyApprovalRequest',
  '2': [
    {'1': 'approval_id', '3': 1, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'reason', '3': 2, '4': 1, '5': 9, '10': 'reason'},
  ],
};

/// Descriptor for `DenyApprovalRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List denyApprovalRequestDescriptor = $convert.base64Decode(
    'ChNEZW55QXBwcm92YWxSZXF1ZXN0Eh8KC2FwcHJvdmFsX2lkGAEgASgJUgphcHByb3ZhbElkEh'
    'YKBnJlYXNvbhgCIAEoCVIGcmVhc29u');

@$core.Deprecated('Use approvalResponseDescriptor instead')
const ApprovalResponse$json = {
  '1': 'ApprovalResponse',
  '2': [
    {'1': 'approval_id', '3': 1, '4': 1, '5': 9, '10': 'approvalId'},
    {
      '1': 'status',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ApprovalStatus',
      '10': 'status'
    },
    {
      '1': 'reservation',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.SandboxArtifactReservation',
      '10': 'reservation'
    },
  ],
};

/// Descriptor for `ApprovalResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List approvalResponseDescriptor = $convert.base64Decode(
    'ChBBcHByb3ZhbFJlc3BvbnNlEh8KC2FwcHJvdmFsX2lkGAEgASgJUgphcHByb3ZhbElkEjEKBn'
    'N0YXR1cxgCIAEoDjIZLnR1cmluZy52MS5BcHByb3ZhbFN0YXR1c1IGc3RhdHVzEkcKC3Jlc2Vy'
    'dmF0aW9uGAMgASgLMiUudHVyaW5nLnYxLlNhbmRib3hBcnRpZmFjdFJlc2VydmF0aW9uUgtyZX'
    'NlcnZhdGlvbg==');

@$core.Deprecated('Use sandboxArtifactReservationDescriptor instead')
const SandboxArtifactReservation$json = {
  '1': 'SandboxArtifactReservation',
  '2': [
    {'1': 'artifact_id', '3': 1, '4': 1, '5': 9, '10': 'artifactId'},
    {'1': 'physical_path', '3': 2, '4': 1, '5': 9, '10': 'physicalPath'},
    {'1': 'policy', '3': 3, '4': 1, '5': 9, '10': 'policy'},
    {
      '1': 'deletion_generation',
      '3': 4,
      '4': 1,
      '5': 3,
      '10': 'deletionGeneration'
    },
  ],
};

/// Descriptor for `SandboxArtifactReservation`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List sandboxArtifactReservationDescriptor = $convert.base64Decode(
    'ChpTYW5kYm94QXJ0aWZhY3RSZXNlcnZhdGlvbhIfCgthcnRpZmFjdF9pZBgBIAEoCVIKYXJ0aW'
    'ZhY3RJZBIjCg1waHlzaWNhbF9wYXRoGAIgASgJUgxwaHlzaWNhbFBhdGgSFgoGcG9saWN5GAMg'
    'ASgJUgZwb2xpY3kSLwoTZGVsZXRpb25fZ2VuZXJhdGlvbhgEIAEoA1ISZGVsZXRpb25HZW5lcm'
    'F0aW9u');

@$core.Deprecated('Use getApprovalForRuntimeRequestDescriptor instead')
const GetApprovalForRuntimeRequest$json = {
  '1': 'GetApprovalForRuntimeRequest',
  '2': [
    {'1': 'approval_id', '3': 1, '4': 1, '5': 9, '10': 'approvalId'},
  ],
};

/// Descriptor for `GetApprovalForRuntimeRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getApprovalForRuntimeRequestDescriptor =
    $convert.base64Decode(
        'ChxHZXRBcHByb3ZhbEZvclJ1bnRpbWVSZXF1ZXN0Eh8KC2FwcHJvdmFsX2lkGAEgASgJUgphcH'
        'Byb3ZhbElk');

@$core.Deprecated('Use runtimeApprovalStateDescriptor instead')
const RuntimeApprovalState$json = {
  '1': 'RuntimeApprovalState',
  '2': [
    {'1': 'approval_id', '3': 1, '4': 1, '5': 9, '10': 'approvalId'},
    {
      '1': 'status',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.ApprovalStatus',
      '10': 'status'
    },
    {'1': 'approval_token', '3': 3, '4': 1, '5': 9, '10': 'approvalToken'},
  ],
};

/// Descriptor for `RuntimeApprovalState`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runtimeApprovalStateDescriptor = $convert.base64Decode(
    'ChRSdW50aW1lQXBwcm92YWxTdGF0ZRIfCgthcHByb3ZhbF9pZBgBIAEoCVIKYXBwcm92YWxJZB'
    'IxCgZzdGF0dXMYAiABKA4yGS50dXJpbmcudjEuQXBwcm92YWxTdGF0dXNSBnN0YXR1cxIlCg5h'
    'cHByb3ZhbF90b2tlbhgDIAEoCVINYXBwcm92YWxUb2tlbg==');

@$core.Deprecated('Use consumeApprovalRequestDescriptor instead')
const ConsumeApprovalRequest$json = {
  '1': 'ConsumeApprovalRequest',
  '2': [
    {'1': 'approval_id', '3': 1, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'provenance_token', '3': 2, '4': 1, '5': 9, '10': 'provenanceToken'},
    {'1': 'physical_path', '3': 3, '4': 1, '5': 9, '10': 'physicalPath'},
  ],
};

/// Descriptor for `ConsumeApprovalRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List consumeApprovalRequestDescriptor = $convert.base64Decode(
    'ChZDb25zdW1lQXBwcm92YWxSZXF1ZXN0Eh8KC2FwcHJvdmFsX2lkGAEgASgJUgphcHByb3ZhbE'
    'lkEikKEHByb3ZlbmFuY2VfdG9rZW4YAiABKAlSD3Byb3ZlbmFuY2VUb2tlbhIjCg1waHlzaWNh'
    'bF9wYXRoGAMgASgJUgxwaHlzaWNhbFBhdGg=');

@$core.Deprecated('Use finalizeSandboxArtifactRequestDescriptor instead')
const FinalizeSandboxArtifactRequest$json = {
  '1': 'FinalizeSandboxArtifactRequest',
  '2': [
    {'1': 'artifact_id', '3': 1, '4': 1, '5': 9, '10': 'artifactId'},
    {'1': 'provenance_token', '3': 2, '4': 1, '5': 9, '10': 'provenanceToken'},
    {'1': 'committed', '3': 3, '4': 1, '5': 8, '10': 'committed'},
  ],
};

/// Descriptor for `FinalizeSandboxArtifactRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List finalizeSandboxArtifactRequestDescriptor =
    $convert.base64Decode(
        'Ch5GaW5hbGl6ZVNhbmRib3hBcnRpZmFjdFJlcXVlc3QSHwoLYXJ0aWZhY3RfaWQYASABKAlSCm'
        'FydGlmYWN0SWQSKQoQcHJvdmVuYW5jZV90b2tlbhgCIAEoCVIPcHJvdmVuYW5jZVRva2VuEhwK'
        'CWNvbW1pdHRlZBgDIAEoCFIJY29tbWl0dGVk');

@$core.Deprecated('Use finalizeSandboxArtifactResponseDescriptor instead')
const FinalizeSandboxArtifactResponse$json = {
  '1': 'FinalizeSandboxArtifactResponse',
  '2': [
    {'1': 'artifact_id', '3': 1, '4': 1, '5': 9, '10': 'artifactId'},
    {'1': 'state', '3': 2, '4': 1, '5': 9, '10': 'state'},
  ],
};

/// Descriptor for `FinalizeSandboxArtifactResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List finalizeSandboxArtifactResponseDescriptor =
    $convert.base64Decode(
        'Ch9GaW5hbGl6ZVNhbmRib3hBcnRpZmFjdFJlc3BvbnNlEh8KC2FydGlmYWN0X2lkGAEgASgJUg'
        'phcnRpZmFjdElkEhQKBXN0YXRlGAIgASgJUgVzdGF0ZQ==');

@$core.Deprecated('Use checkSessionCapabilityRequestDescriptor instead')
const CheckSessionCapabilityRequest$json = {
  '1': 'CheckSessionCapabilityRequest',
  '2': [
    {'1': 'provenance_token', '3': 1, '4': 1, '5': 9, '10': 'provenanceToken'},
  ],
};

/// Descriptor for `CheckSessionCapabilityRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List checkSessionCapabilityRequestDescriptor =
    $convert.base64Decode(
        'Ch1DaGVja1Nlc3Npb25DYXBhYmlsaXR5UmVxdWVzdBIpChBwcm92ZW5hbmNlX3Rva2VuGAEgAS'
        'gJUg9wcm92ZW5hbmNlVG9rZW4=');

@$core.Deprecated('Use sessionCapabilityStateDescriptor instead')
const SessionCapabilityState$json = {
  '1': 'SessionCapabilityState',
  '2': [
    {'1': 'active', '3': 1, '4': 1, '5': 8, '10': 'active'},
    {
      '1': 'deletion_generation',
      '3': 2,
      '4': 1,
      '5': 3,
      '10': 'deletionGeneration'
    },
  ],
};

/// Descriptor for `SessionCapabilityState`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List sessionCapabilityStateDescriptor =
    $convert.base64Decode(
        'ChZTZXNzaW9uQ2FwYWJpbGl0eVN0YXRlEhYKBmFjdGl2ZRgBIAEoCFIGYWN0aXZlEi8KE2RlbG'
        'V0aW9uX2dlbmVyYXRpb24YAiABKANSEmRlbGV0aW9uR2VuZXJhdGlvbg==');
