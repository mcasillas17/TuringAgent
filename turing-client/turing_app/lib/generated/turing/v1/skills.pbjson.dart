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
    {'1': 'description', '3': 3, '4': 1, '5': 9, '10': 'description'},
    {'1': 'body', '3': 4, '4': 1, '5': 9, '10': 'body'},
    {'1': 'category', '3': 5, '4': 1, '5': 9, '10': 'category'},
    {'1': 'version', '3': 6, '4': 1, '5': 9, '10': 'version'},
    {'1': 'author', '3': 7, '4': 1, '5': 9, '10': 'author'},
    {'1': 'license', '3': 8, '4': 1, '5': 9, '10': 'license'},
    {'1': 'requires', '3': 9, '4': 3, '5': 9, '10': 'requires'},
    {
      '1': 'granted_capabilities',
      '3': 10,
      '4': 3,
      '5': 9,
      '10': 'grantedCapabilities'
    },
    {
      '1': 'missing_capabilities',
      '3': 11,
      '4': 3,
      '5': 9,
      '10': 'missingCapabilities'
    },
    {'1': 'enabled', '3': 12, '4': 1, '5': 8, '10': 'enabled'},
    {'1': 'parse_error', '3': 13, '4': 1, '5': 9, '10': 'parseError'},
    {'1': 'folder_path', '3': 14, '4': 1, '5': 9, '10': 'folderPath'},
  ],
};

/// Descriptor for `Skill`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List skillDescriptor = $convert.base64Decode(
    'CgVTa2lsbBIZCghza2lsbF9pZBgBIAEoCVIHc2tpbGxJZBISCgRuYW1lGAIgASgJUgRuYW1lEi'
    'AKC2Rlc2NyaXB0aW9uGAMgASgJUgtkZXNjcmlwdGlvbhISCgRib2R5GAQgASgJUgRib2R5EhoK'
    'CGNhdGVnb3J5GAUgASgJUghjYXRlZ29yeRIYCgd2ZXJzaW9uGAYgASgJUgd2ZXJzaW9uEhYKBm'
    'F1dGhvchgHIAEoCVIGYXV0aG9yEhgKB2xpY2Vuc2UYCCABKAlSB2xpY2Vuc2USGgoIcmVxdWly'
    'ZXMYCSADKAlSCHJlcXVpcmVzEjEKFGdyYW50ZWRfY2FwYWJpbGl0aWVzGAogAygJUhNncmFudG'
    'VkQ2FwYWJpbGl0aWVzEjEKFG1pc3NpbmdfY2FwYWJpbGl0aWVzGAsgAygJUhNtaXNzaW5nQ2Fw'
    'YWJpbGl0aWVzEhgKB2VuYWJsZWQYDCABKAhSB2VuYWJsZWQSHwoLcGFyc2VfZXJyb3IYDSABKA'
    'lSCnBhcnNlRXJyb3ISHwoLZm9sZGVyX3BhdGgYDiABKAlSCmZvbGRlclBhdGg=');

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

@$core.Deprecated('Use getSkillRequestDescriptor instead')
const GetSkillRequest$json = {
  '1': 'GetSkillRequest',
  '2': [
    {'1': 'skill_id', '3': 1, '4': 1, '5': 9, '10': 'skillId'},
  ],
};

/// Descriptor for `GetSkillRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getSkillRequestDescriptor = $convert.base64Decode(
    'Cg9HZXRTa2lsbFJlcXVlc3QSGQoIc2tpbGxfaWQYASABKAlSB3NraWxsSWQ=');

@$core.Deprecated('Use setSkillEnabledRequestDescriptor instead')
const SetSkillEnabledRequest$json = {
  '1': 'SetSkillEnabledRequest',
  '2': [
    {'1': 'skill_id', '3': 1, '4': 1, '5': 9, '10': 'skillId'},
    {'1': 'enabled', '3': 2, '4': 1, '5': 8, '10': 'enabled'},
  ],
};

/// Descriptor for `SetSkillEnabledRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setSkillEnabledRequestDescriptor =
    $convert.base64Decode(
        'ChZTZXRTa2lsbEVuYWJsZWRSZXF1ZXN0EhkKCHNraWxsX2lkGAEgASgJUgdza2lsbElkEhgKB2'
        'VuYWJsZWQYAiABKAhSB2VuYWJsZWQ=');

@$core.Deprecated('Use setSkillCapabilityGrantRequestDescriptor instead')
const SetSkillCapabilityGrantRequest$json = {
  '1': 'SetSkillCapabilityGrantRequest',
  '2': [
    {'1': 'skill_id', '3': 1, '4': 1, '5': 9, '10': 'skillId'},
    {'1': 'capability', '3': 2, '4': 1, '5': 9, '10': 'capability'},
    {'1': 'granted', '3': 3, '4': 1, '5': 8, '10': 'granted'},
  ],
};

/// Descriptor for `SetSkillCapabilityGrantRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setSkillCapabilityGrantRequestDescriptor =
    $convert.base64Decode(
        'Ch5TZXRTa2lsbENhcGFiaWxpdHlHcmFudFJlcXVlc3QSGQoIc2tpbGxfaWQYASABKAlSB3NraW'
        'xsSWQSHgoKY2FwYWJpbGl0eRgCIAEoCVIKY2FwYWJpbGl0eRIYCgdncmFudGVkGAMgASgIUgdn'
        'cmFudGVk');
