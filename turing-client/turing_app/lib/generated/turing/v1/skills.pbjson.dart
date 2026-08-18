// This is a generated file - do not edit.
//
// Generated from turing/v1/skills.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use skillDescriptor instead')
const Skill$json = {
  '1': 'Skill',
  '2': [
    {'1': 'skill_id', '3': 1, '4': 1, '5': 9, '10': 'skillId'},
    {'1': 'name', '3': 2, '4': 1, '5': 9, '10': 'name'},
    {'1': 'instructions', '3': 3, '4': 1, '5': 9, '10': 'instructions'},
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

/// Descriptor for `Skill`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List skillDescriptor = $convert.base64Decode(
    'CgVTa2lsbBIZCghza2lsbF9pZBgBIAEoCVIHc2tpbGxJZBISCgRuYW1lGAIgASgJUgRuYW1lEi'
    'IKDGluc3RydWN0aW9ucxgDIAEoCVIMaW5zdHJ1Y3Rpb25zEjkKCmNyZWF0ZWRfYXQYBCABKAsy'
    'Gi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wUgljcmVhdGVkQXQSOQoKdXBkYXRlZF9hdBgFIA'
    'EoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSCXVwZGF0ZWRBdA==');

@$core.Deprecated('Use createSkillRequestDescriptor instead')
const CreateSkillRequest$json = {
  '1': 'CreateSkillRequest',
  '2': [
    {'1': 'name', '3': 1, '4': 1, '5': 9, '10': 'name'},
    {'1': 'instructions', '3': 2, '4': 1, '5': 9, '10': 'instructions'},
  ],
};

/// Descriptor for `CreateSkillRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List createSkillRequestDescriptor = $convert.base64Decode(
    'ChJDcmVhdGVTa2lsbFJlcXVlc3QSEgoEbmFtZRgBIAEoCVIEbmFtZRIiCgxpbnN0cnVjdGlvbn'
    'MYAiABKAlSDGluc3RydWN0aW9ucw==');

@$core.Deprecated('Use updateSkillRequestDescriptor instead')
const UpdateSkillRequest$json = {
  '1': 'UpdateSkillRequest',
  '2': [
    {'1': 'skill_id', '3': 1, '4': 1, '5': 9, '10': 'skillId'},
    {'1': 'name', '3': 2, '4': 1, '5': 9, '10': 'name'},
    {'1': 'instructions', '3': 3, '4': 1, '5': 9, '10': 'instructions'},
  ],
};

/// Descriptor for `UpdateSkillRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List updateSkillRequestDescriptor = $convert.base64Decode(
    'ChJVcGRhdGVTa2lsbFJlcXVlc3QSGQoIc2tpbGxfaWQYASABKAlSB3NraWxsSWQSEgoEbmFtZR'
    'gCIAEoCVIEbmFtZRIiCgxpbnN0cnVjdGlvbnMYAyABKAlSDGluc3RydWN0aW9ucw==');

@$core.Deprecated('Use deleteSkillRequestDescriptor instead')
const DeleteSkillRequest$json = {
  '1': 'DeleteSkillRequest',
  '2': [
    {'1': 'skill_id', '3': 1, '4': 1, '5': 9, '10': 'skillId'},
  ],
};

/// Descriptor for `DeleteSkillRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List deleteSkillRequestDescriptor =
    $convert.base64Decode(
        'ChJEZWxldGVTa2lsbFJlcXVlc3QSGQoIc2tpbGxfaWQYASABKAlSB3NraWxsSWQ=');

@$core.Deprecated('Use deleteSkillResponseDescriptor instead')
const DeleteSkillResponse$json = {
  '1': 'DeleteSkillResponse',
};

/// Descriptor for `DeleteSkillResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List deleteSkillResponseDescriptor =
    $convert.base64Decode('ChNEZWxldGVTa2lsbFJlc3BvbnNl');

@$core.Deprecated('Use listSkillsRequestDescriptor instead')
const ListSkillsRequest$json = {
  '1': 'ListSkillsRequest',
};

/// Descriptor for `ListSkillsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listSkillsRequestDescriptor =
    $convert.base64Decode('ChFMaXN0U2tpbGxzUmVxdWVzdA==');

@$core.Deprecated('Use listSkillsResponseDescriptor instead')
const ListSkillsResponse$json = {
  '1': 'ListSkillsResponse',
  '2': [
    {
      '1': 'skills',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.Skill',
      '10': 'skills'
    },
  ],
};

/// Descriptor for `ListSkillsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listSkillsResponseDescriptor = $convert.base64Decode(
    'ChJMaXN0U2tpbGxzUmVzcG9uc2USKAoGc2tpbGxzGAEgAygLMhAudHVyaW5nLnYxLlNraWxsUg'
    'Zza2lsbHM=');

@$core.Deprecated('Use attachSkillRequestDescriptor instead')
const AttachSkillRequest$json = {
  '1': 'AttachSkillRequest',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'skill_id', '3': 2, '4': 1, '5': 9, '10': 'skillId'},
  ],
};

/// Descriptor for `AttachSkillRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List attachSkillRequestDescriptor = $convert.base64Decode(
    'ChJBdHRhY2hTa2lsbFJlcXVlc3QSHQoKc2Vzc2lvbl9pZBgBIAEoCVIJc2Vzc2lvbklkEhkKCH'
    'NraWxsX2lkGAIgASgJUgdza2lsbElk');

@$core.Deprecated('Use detachSkillRequestDescriptor instead')
const DetachSkillRequest$json = {
  '1': 'DetachSkillRequest',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'skill_id', '3': 2, '4': 1, '5': 9, '10': 'skillId'},
  ],
};

/// Descriptor for `DetachSkillRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List detachSkillRequestDescriptor = $convert.base64Decode(
    'ChJEZXRhY2hTa2lsbFJlcXVlc3QSHQoKc2Vzc2lvbl9pZBgBIAEoCVIJc2Vzc2lvbklkEhkKCH'
    'NraWxsX2lkGAIgASgJUgdza2lsbElk');

@$core.Deprecated('Use sessionSkillsResponseDescriptor instead')
const SessionSkillsResponse$json = {
  '1': 'SessionSkillsResponse',
  '2': [
    {
      '1': 'skills',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.Skill',
      '10': 'skills'
    },
  ],
};

/// Descriptor for `SessionSkillsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List sessionSkillsResponseDescriptor = $convert.base64Decode(
    'ChVTZXNzaW9uU2tpbGxzUmVzcG9uc2USKAoGc2tpbGxzGAEgAygLMhAudHVyaW5nLnYxLlNraW'
    'xsUgZza2lsbHM=');

@$core.Deprecated('Use listSessionSkillsRequestDescriptor instead')
const ListSessionSkillsRequest$json = {
  '1': 'ListSessionSkillsRequest',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
  ],
};

/// Descriptor for `ListSessionSkillsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listSessionSkillsRequestDescriptor =
    $convert.base64Decode(
        'ChhMaXN0U2Vzc2lvblNraWxsc1JlcXVlc3QSHQoKc2Vzc2lvbl9pZBgBIAEoCVIJc2Vzc2lvbk'
        'lk');
