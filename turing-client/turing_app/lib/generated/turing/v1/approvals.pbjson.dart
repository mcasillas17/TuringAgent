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

@$core.Deprecated('Use approveApprovalRequestDescriptor instead')
const ApproveApprovalRequest$json = {
  '1': 'ApproveApprovalRequest',
  '2': [
    {'1': 'approval_id', '3': 1, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'comment', '3': 2, '4': 1, '5': 9, '10': 'comment'},
  ],
};

/// Descriptor for `ApproveApprovalRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List approveApprovalRequestDescriptor =
    $convert.base64Decode(
        'ChZBcHByb3ZlQXBwcm92YWxSZXF1ZXN0Eh8KC2FwcHJvdmFsX2lkGAEgASgJUgphcHByb3ZhbE'
        'lkEhgKB2NvbW1lbnQYAiABKAlSB2NvbW1lbnQ=');

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
