// This is a generated file - do not edit.
//
// Generated from turing/v1/attention.proto.

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

@$core.Deprecated('Use attentionStageDescriptor instead')
const AttentionStage$json = {
  '1': 'AttentionStage',
  '2': [
    {'1': 'ATTENTION_STAGE_UNSPECIFIED', '2': 0},
    {'1': 'ATTENTION_STAGE_PERSON_APPROVAL', '2': 1},
    {'1': 'ATTENTION_STAGE_PERSON_CONSENT', '2': 2},
    {'1': 'ATTENTION_STAGE_PERSON_BLOCKED', '2': 3},
    {'1': 'ATTENTION_STAGE_PERSON_DELETION', '2': 4},
    {'1': 'ATTENTION_STAGE_PERSON_PROPOSAL', '2': 5},
    {'1': 'ATTENTION_STAGE_UNAVAILABLE', '2': 6},
  ],
};

/// Descriptor for `AttentionStage`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List attentionStageDescriptor = $convert.base64Decode(
    'Cg5BdHRlbnRpb25TdGFnZRIfChtBVFRFTlRJT05fU1RBR0VfVU5TUEVDSUZJRUQQABIjCh9BVF'
    'RFTlRJT05fU1RBR0VfUEVSU09OX0FQUFJPVkFMEAESIgoeQVRURU5USU9OX1NUQUdFX1BFUlNP'
    'Tl9DT05TRU5UEAISIgoeQVRURU5USU9OX1NUQUdFX1BFUlNPTl9CTE9DS0VEEAMSIwofQVRURU'
    '5USU9OX1NUQUdFX1BFUlNPTl9ERUxFVElPThAEEiMKH0FUVEVOVElPTl9TVEFHRV9QRVJTT05f'
    'UFJPUE9TQUwQBRIfChtBVFRFTlRJT05fU1RBR0VfVU5BVkFJTEFCTEUQBg==');

@$core.Deprecated('Use attentionSourceKindDescriptor instead')
const AttentionSourceKind$json = {
  '1': 'AttentionSourceKind',
  '2': [
    {'1': 'ATTENTION_SOURCE_KIND_UNSPECIFIED', '2': 0},
    {'1': 'ATTENTION_SOURCE_KIND_APPROVAL', '2': 1},
    {'1': 'ATTENTION_SOURCE_KIND_DELEGATION', '2': 2},
    {'1': 'ATTENTION_SOURCE_KIND_AUTOMATION', '2': 3},
    {'1': 'ATTENTION_SOURCE_KIND_DELETION', '2': 4},
    {'1': 'ATTENTION_SOURCE_KIND_WATCH', '2': 5},
    {'1': 'ATTENTION_SOURCE_KIND_PROPOSAL', '2': 6},
  ],
};

/// Descriptor for `AttentionSourceKind`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List attentionSourceKindDescriptor = $convert.base64Decode(
    'ChNBdHRlbnRpb25Tb3VyY2VLaW5kEiUKIUFUVEVOVElPTl9TT1VSQ0VfS0lORF9VTlNQRUNJRk'
    'lFRBAAEiIKHkFUVEVOVElPTl9TT1VSQ0VfS0lORF9BUFBST1ZBTBABEiQKIEFUVEVOVElPTl9T'
    'T1VSQ0VfS0lORF9ERUxFR0FUSU9OEAISJAogQVRURU5USU9OX1NPVVJDRV9LSU5EX0FVVE9NQV'
    'RJT04QAxIiCh5BVFRFTlRJT05fU09VUkNFX0tJTkRfREVMRVRJT04QBBIfChtBVFRFTlRJT05f'
    'U09VUkNFX0tJTkRfV0FUQ0gQBRIiCh5BVFRFTlRJT05fU09VUkNFX0tJTkRfUFJPUE9TQUwQBg'
    '==');

@$core.Deprecated('Use attentionRowDescriptor instead')
const AttentionRow$json = {
  '1': 'AttentionRow',
  '2': [
    {
      '1': 'stage',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.AttentionStage',
      '10': 'stage'
    },
    {
      '1': 'kind',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.AttentionSourceKind',
      '10': 'kind'
    },
    {
      '1': 'since',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'since'
    },
    {'1': 'session_id', '3': 4, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'session_title', '3': 5, '4': 1, '5': 9, '10': 'sessionTitle'},
    {'1': 'approval_id', '3': 6, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'run_id', '3': 7, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'delegation_id', '3': 8, '4': 1, '5': 9, '10': 'delegationId'},
    {'1': 'automation_id', '3': 9, '4': 1, '5': 9, '10': 'automationId'},
    {'1': 'automation_name', '3': 10, '4': 1, '5': 9, '10': 'automationName'},
    {'1': 'occurrence_key', '3': 11, '4': 1, '5': 9, '10': 'occurrenceKey'},
    {'1': 'cause', '3': 12, '4': 1, '5': 9, '10': 'cause'},
  ],
};

/// Descriptor for `AttentionRow`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List attentionRowDescriptor = $convert.base64Decode(
    'CgxBdHRlbnRpb25Sb3cSLwoFc3RhZ2UYASABKA4yGS50dXJpbmcudjEuQXR0ZW50aW9uU3RhZ2'
    'VSBXN0YWdlEjIKBGtpbmQYAiABKA4yHi50dXJpbmcudjEuQXR0ZW50aW9uU291cmNlS2luZFIE'
    'a2luZBIwCgVzaW5jZRgDIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSBXNpbmNlEh'
    '0KCnNlc3Npb25faWQYBCABKAlSCXNlc3Npb25JZBIjCg1zZXNzaW9uX3RpdGxlGAUgASgJUgxz'
    'ZXNzaW9uVGl0bGUSHwoLYXBwcm92YWxfaWQYBiABKAlSCmFwcHJvdmFsSWQSFQoGcnVuX2lkGA'
    'cgASgJUgVydW5JZBIjCg1kZWxlZ2F0aW9uX2lkGAggASgJUgxkZWxlZ2F0aW9uSWQSIwoNYXV0'
    'b21hdGlvbl9pZBgJIAEoCVIMYXV0b21hdGlvbklkEicKD2F1dG9tYXRpb25fbmFtZRgKIAEoCV'
    'IOYXV0b21hdGlvbk5hbWUSJQoOb2NjdXJyZW5jZV9rZXkYCyABKAlSDW9jY3VycmVuY2VLZXkS'
    'FAoFY2F1c2UYDCABKAlSBWNhdXNl');

@$core.Deprecated('Use listAttentionRequestDescriptor instead')
const ListAttentionRequest$json = {
  '1': 'ListAttentionRequest',
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

/// Descriptor for `ListAttentionRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listAttentionRequestDescriptor = $convert.base64Decode(
    'ChRMaXN0QXR0ZW50aW9uUmVxdWVzdBIqCgRwYWdlGAEgASgLMhYudHVyaW5nLnYxLlBhZ2VSZX'
    'F1ZXN0UgRwYWdl');

@$core.Deprecated('Use listAttentionResponseDescriptor instead')
const ListAttentionResponse$json = {
  '1': 'ListAttentionResponse',
  '2': [
    {
      '1': 'rows',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.AttentionRow',
      '10': 'rows'
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

/// Descriptor for `ListAttentionResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listAttentionResponseDescriptor = $convert.base64Decode(
    'ChVMaXN0QXR0ZW50aW9uUmVzcG9uc2USKwoEcm93cxgBIAMoCzIXLnR1cmluZy52MS5BdHRlbn'
    'Rpb25Sb3dSBHJvd3MSKwoEcGFnZRgCIAEoCzIXLnR1cmluZy52MS5QYWdlUmVzcG9uc2VSBHBh'
    'Z2U=');

@$core.Deprecated('Use dismissAttentionRequestDescriptor instead')
const DismissAttentionRequest$json = {
  '1': 'DismissAttentionRequest',
  '2': [
    {'1': 'automation_id', '3': 1, '4': 1, '5': 9, '10': 'automationId'},
    {'1': 'occurrence_key', '3': 2, '4': 1, '5': 9, '10': 'occurrenceKey'},
  ],
};

/// Descriptor for `DismissAttentionRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List dismissAttentionRequestDescriptor =
    $convert.base64Decode(
        'ChdEaXNtaXNzQXR0ZW50aW9uUmVxdWVzdBIjCg1hdXRvbWF0aW9uX2lkGAEgASgJUgxhdXRvbW'
        'F0aW9uSWQSJQoOb2NjdXJyZW5jZV9rZXkYAiABKAlSDW9jY3VycmVuY2VLZXk=');

@$core.Deprecated('Use dismissAttentionResponseDescriptor instead')
const DismissAttentionResponse$json = {
  '1': 'DismissAttentionResponse',
};

/// Descriptor for `DismissAttentionResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List dismissAttentionResponseDescriptor =
    $convert.base64Decode('ChhEaXNtaXNzQXR0ZW50aW9uUmVzcG9uc2U=');
