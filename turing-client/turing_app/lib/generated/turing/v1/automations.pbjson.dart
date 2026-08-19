// This is a generated file - do not edit.
//
// Generated from turing/v1/automations.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use automationScheduleKindDescriptor instead')
const AutomationScheduleKind$json = {
  '1': 'AutomationScheduleKind',
  '2': [
    {'1': 'AUTOMATION_SCHEDULE_KIND_UNSPECIFIED', '2': 0},
    {'1': 'AUTOMATION_SCHEDULE_KIND_INTERVAL', '2': 1},
    {'1': 'AUTOMATION_SCHEDULE_KIND_DAILY', '2': 2},
  ],
};

/// Descriptor for `AutomationScheduleKind`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List automationScheduleKindDescriptor = $convert.base64Decode(
    'ChZBdXRvbWF0aW9uU2NoZWR1bGVLaW5kEigKJEFVVE9NQVRJT05fU0NIRURVTEVfS0lORF9VTl'
    'NQRUNJRklFRBAAEiUKIUFVVE9NQVRJT05fU0NIRURVTEVfS0lORF9JTlRFUlZBTBABEiIKHkFV'
    'VE9NQVRJT05fU0NIRURVTEVfS0lORF9EQUlMWRAC');

@$core.Deprecated('Use automationDescriptor instead')
const Automation$json = {
  '1': 'Automation',
  '2': [
    {'1': 'automation_id', '3': 1, '4': 1, '5': 9, '10': 'automationId'},
    {'1': 'name', '3': 2, '4': 1, '5': 9, '10': 'name'},
    {'1': 'prompt', '3': 3, '4': 1, '5': 9, '10': 'prompt'},
    {
      '1': 'schedule',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.AutomationSchedule',
      '10': 'schedule'
    },
    {'1': 'enabled', '3': 5, '4': 1, '5': 8, '10': 'enabled'},
    {
      '1': 'allowed_tools',
      '3': 6,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.AutomationTool',
      '10': 'allowedTools'
    },
    {
      '1': 'last_run_at',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'lastRunAt'
    },
    {
      '1': 'next_run_at',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'nextRunAt'
    },
    {'1': 'session_id', '3': 9, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'last_run_id', '3': 10, '4': 1, '5': 9, '10': 'lastRunId'},
    {'1': 'last_run_status', '3': 11, '4': 1, '5': 9, '10': 'lastRunStatus'},
    {'1': 'last_run_error', '3': 12, '4': 1, '5': 9, '10': 'lastRunError'},
    {
      '1': 'created_at',
      '3': 13,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'createdAt'
    },
    {
      '1': 'updated_at',
      '3': 14,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'updatedAt'
    },
  ],
};

/// Descriptor for `Automation`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List automationDescriptor = $convert.base64Decode(
    'CgpBdXRvbWF0aW9uEiMKDWF1dG9tYXRpb25faWQYASABKAlSDGF1dG9tYXRpb25JZBISCgRuYW'
    '1lGAIgASgJUgRuYW1lEhYKBnByb21wdBgDIAEoCVIGcHJvbXB0EjkKCHNjaGVkdWxlGAQgASgL'
    'Mh0udHVyaW5nLnYxLkF1dG9tYXRpb25TY2hlZHVsZVIIc2NoZWR1bGUSGAoHZW5hYmxlZBgFIA'
    'EoCFIHZW5hYmxlZBI+Cg1hbGxvd2VkX3Rvb2xzGAYgAygLMhkudHVyaW5nLnYxLkF1dG9tYXRp'
    'b25Ub29sUgxhbGxvd2VkVG9vbHMSOgoLbGFzdF9ydW5fYXQYByABKAsyGi5nb29nbGUucHJvdG'
    '9idWYuVGltZXN0YW1wUglsYXN0UnVuQXQSOgoLbmV4dF9ydW5fYXQYCCABKAsyGi5nb29nbGUu'
    'cHJvdG9idWYuVGltZXN0YW1wUgluZXh0UnVuQXQSHQoKc2Vzc2lvbl9pZBgJIAEoCVIJc2Vzc2'
    'lvbklkEh4KC2xhc3RfcnVuX2lkGAogASgJUglsYXN0UnVuSWQSJgoPbGFzdF9ydW5fc3RhdHVz'
    'GAsgASgJUg1sYXN0UnVuU3RhdHVzEiQKDmxhc3RfcnVuX2Vycm9yGAwgASgJUgxsYXN0UnVuRX'
    'Jyb3ISOQoKY3JlYXRlZF9hdBgNIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSCWNy'
    'ZWF0ZWRBdBI5Cgp1cGRhdGVkX2F0GA4gASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcF'
    'IJdXBkYXRlZEF0');

@$core.Deprecated('Use automationToolDescriptor instead')
const AutomationTool$json = {
  '1': 'AutomationTool',
  '2': [
    {'1': 'server_name', '3': 1, '4': 1, '5': 9, '10': 'serverName'},
    {'1': 'tool_name', '3': 2, '4': 1, '5': 9, '10': 'toolName'},
  ],
};

/// Descriptor for `AutomationTool`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List automationToolDescriptor = $convert.base64Decode(
    'Cg5BdXRvbWF0aW9uVG9vbBIfCgtzZXJ2ZXJfbmFtZRgBIAEoCVIKc2VydmVyTmFtZRIbCgl0b2'
    '9sX25hbWUYAiABKAlSCHRvb2xOYW1l');

@$core.Deprecated('Use automationScheduleDescriptor instead')
const AutomationSchedule$json = {
  '1': 'AutomationSchedule',
  '2': [
    {
      '1': 'kind',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.AutomationScheduleKind',
      '10': 'kind'
    },
    {'1': 'interval_minutes', '3': 2, '4': 1, '5': 5, '10': 'intervalMinutes'},
    {'1': 'daily_minute_utc', '3': 3, '4': 1, '5': 5, '10': 'dailyMinuteUtc'},
  ],
};

/// Descriptor for `AutomationSchedule`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List automationScheduleDescriptor = $convert.base64Decode(
    'ChJBdXRvbWF0aW9uU2NoZWR1bGUSNQoEa2luZBgBIAEoDjIhLnR1cmluZy52MS5BdXRvbWF0aW'
    '9uU2NoZWR1bGVLaW5kUgRraW5kEikKEGludGVydmFsX21pbnV0ZXMYAiABKAVSD2ludGVydmFs'
    'TWludXRlcxIoChBkYWlseV9taW51dGVfdXRjGAMgASgFUg5kYWlseU1pbnV0ZVV0Yw==');

@$core.Deprecated('Use createAutomationRequestDescriptor instead')
const CreateAutomationRequest$json = {
  '1': 'CreateAutomationRequest',
  '2': [
    {'1': 'name', '3': 1, '4': 1, '5': 9, '10': 'name'},
    {'1': 'prompt', '3': 2, '4': 1, '5': 9, '10': 'prompt'},
    {
      '1': 'schedule',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.AutomationSchedule',
      '10': 'schedule'
    },
    {'1': 'enabled', '3': 4, '4': 1, '5': 8, '10': 'enabled'},
    {
      '1': 'allowed_tools',
      '3': 5,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.AutomationTool',
      '10': 'allowedTools'
    },
  ],
};

/// Descriptor for `CreateAutomationRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List createAutomationRequestDescriptor = $convert.base64Decode(
    'ChdDcmVhdGVBdXRvbWF0aW9uUmVxdWVzdBISCgRuYW1lGAEgASgJUgRuYW1lEhYKBnByb21wdB'
    'gCIAEoCVIGcHJvbXB0EjkKCHNjaGVkdWxlGAMgASgLMh0udHVyaW5nLnYxLkF1dG9tYXRpb25T'
    'Y2hlZHVsZVIIc2NoZWR1bGUSGAoHZW5hYmxlZBgEIAEoCFIHZW5hYmxlZBI+Cg1hbGxvd2VkX3'
    'Rvb2xzGAUgAygLMhkudHVyaW5nLnYxLkF1dG9tYXRpb25Ub29sUgxhbGxvd2VkVG9vbHM=');

@$core.Deprecated('Use updateAutomationRequestDescriptor instead')
const UpdateAutomationRequest$json = {
  '1': 'UpdateAutomationRequest',
  '2': [
    {'1': 'automation_id', '3': 1, '4': 1, '5': 9, '10': 'automationId'},
    {'1': 'name', '3': 2, '4': 1, '5': 9, '10': 'name'},
    {'1': 'prompt', '3': 3, '4': 1, '5': 9, '10': 'prompt'},
    {
      '1': 'schedule',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.AutomationSchedule',
      '10': 'schedule'
    },
    {
      '1': 'allowed_tools',
      '3': 5,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.AutomationTool',
      '10': 'allowedTools'
    },
  ],
};

/// Descriptor for `UpdateAutomationRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List updateAutomationRequestDescriptor = $convert.base64Decode(
    'ChdVcGRhdGVBdXRvbWF0aW9uUmVxdWVzdBIjCg1hdXRvbWF0aW9uX2lkGAEgASgJUgxhdXRvbW'
    'F0aW9uSWQSEgoEbmFtZRgCIAEoCVIEbmFtZRIWCgZwcm9tcHQYAyABKAlSBnByb21wdBI5Cghz'
    'Y2hlZHVsZRgEIAEoCzIdLnR1cmluZy52MS5BdXRvbWF0aW9uU2NoZWR1bGVSCHNjaGVkdWxlEj'
    '4KDWFsbG93ZWRfdG9vbHMYBSADKAsyGS50dXJpbmcudjEuQXV0b21hdGlvblRvb2xSDGFsbG93'
    'ZWRUb29scw==');

@$core.Deprecated('Use setAutomationEnabledRequestDescriptor instead')
const SetAutomationEnabledRequest$json = {
  '1': 'SetAutomationEnabledRequest',
  '2': [
    {'1': 'automation_id', '3': 1, '4': 1, '5': 9, '10': 'automationId'},
    {'1': 'enabled', '3': 2, '4': 1, '5': 8, '10': 'enabled'},
  ],
};

/// Descriptor for `SetAutomationEnabledRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setAutomationEnabledRequestDescriptor =
    $convert.base64Decode(
        'ChtTZXRBdXRvbWF0aW9uRW5hYmxlZFJlcXVlc3QSIwoNYXV0b21hdGlvbl9pZBgBIAEoCVIMYX'
        'V0b21hdGlvbklkEhgKB2VuYWJsZWQYAiABKAhSB2VuYWJsZWQ=');

@$core.Deprecated('Use deleteAutomationRequestDescriptor instead')
const DeleteAutomationRequest$json = {
  '1': 'DeleteAutomationRequest',
  '2': [
    {'1': 'automation_id', '3': 1, '4': 1, '5': 9, '10': 'automationId'},
  ],
};

/// Descriptor for `DeleteAutomationRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List deleteAutomationRequestDescriptor =
    $convert.base64Decode(
        'ChdEZWxldGVBdXRvbWF0aW9uUmVxdWVzdBIjCg1hdXRvbWF0aW9uX2lkGAEgASgJUgxhdXRvbW'
        'F0aW9uSWQ=');

@$core.Deprecated('Use deleteAutomationResponseDescriptor instead')
const DeleteAutomationResponse$json = {
  '1': 'DeleteAutomationResponse',
};

/// Descriptor for `DeleteAutomationResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List deleteAutomationResponseDescriptor =
    $convert.base64Decode('ChhEZWxldGVBdXRvbWF0aW9uUmVzcG9uc2U=');

@$core.Deprecated('Use listAutomationsRequestDescriptor instead')
const ListAutomationsRequest$json = {
  '1': 'ListAutomationsRequest',
};

/// Descriptor for `ListAutomationsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listAutomationsRequestDescriptor =
    $convert.base64Decode('ChZMaXN0QXV0b21hdGlvbnNSZXF1ZXN0');

@$core.Deprecated('Use listAutomationsResponseDescriptor instead')
const ListAutomationsResponse$json = {
  '1': 'ListAutomationsResponse',
  '2': [
    {
      '1': 'automations',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.Automation',
      '10': 'automations'
    },
  ],
};

/// Descriptor for `ListAutomationsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listAutomationsResponseDescriptor =
    $convert.base64Decode(
        'ChdMaXN0QXV0b21hdGlvbnNSZXNwb25zZRI3CgthdXRvbWF0aW9ucxgBIAMoCzIVLnR1cmluZy'
        '52MS5BdXRvbWF0aW9uUgthdXRvbWF0aW9ucw==');
