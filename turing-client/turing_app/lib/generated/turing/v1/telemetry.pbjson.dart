// This is a generated file - do not edit.
//
// Generated from turing/v1/telemetry.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use telemetryWindowDescriptor instead')
const TelemetryWindow$json = {
  '1': 'TelemetryWindow',
  '2': [
    {'1': 'days', '3': 1, '4': 1, '5': 5, '10': 'days'},
    {
      '1': 'start',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'start'
    },
    {
      '1': 'end',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'end'
    },
  ],
};

/// Descriptor for `TelemetryWindow`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List telemetryWindowDescriptor = $convert.base64Decode(
    'Cg9UZWxlbWV0cnlXaW5kb3cSEgoEZGF5cxgBIAEoBVIEZGF5cxIwCgVzdGFydBgCIAEoCzIaLm'
    'dvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSBXN0YXJ0EiwKA2VuZBgDIAEoCzIaLmdvb2dsZS5w'
    'cm90b2J1Zi5UaW1lc3RhbXBSA2VuZA==');

@$core.Deprecated('Use runTotalsDescriptor instead')
const RunTotals$json = {
  '1': 'RunTotals',
  '2': [
    {'1': 'total', '3': 1, '4': 1, '5': 3, '10': 'total'},
    {'1': 'completed', '3': 2, '4': 1, '5': 3, '10': 'completed'},
    {'1': 'failed', '3': 3, '4': 1, '5': 3, '10': 'failed'},
    {'1': 'cancelled', '3': 4, '4': 1, '5': 3, '10': 'cancelled'},
    {'1': 'in_flight', '3': 5, '4': 1, '5': 3, '10': 'inFlight'},
    {
      '1': 'average_duration_ms',
      '3': 6,
      '4': 1,
      '5': 3,
      '9': 0,
      '10': 'averageDurationMs',
      '17': true
    },
  ],
  '8': [
    {'1': '_average_duration_ms'},
  ],
};

/// Descriptor for `RunTotals`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List runTotalsDescriptor = $convert.base64Decode(
    'CglSdW5Ub3RhbHMSFAoFdG90YWwYASABKANSBXRvdGFsEhwKCWNvbXBsZXRlZBgCIAEoA1IJY2'
    '9tcGxldGVkEhYKBmZhaWxlZBgDIAEoA1IGZmFpbGVkEhwKCWNhbmNlbGxlZBgEIAEoA1IJY2Fu'
    'Y2VsbGVkEhsKCWluX2ZsaWdodBgFIAEoA1IIaW5GbGlnaHQSMwoTYXZlcmFnZV9kdXJhdGlvbl'
    '9tcxgGIAEoA0gAUhFhdmVyYWdlRHVyYXRpb25Nc4gBAUIWChRfYXZlcmFnZV9kdXJhdGlvbl9t'
    'cw==');

@$core.Deprecated('Use tokenTotalsDescriptor instead')
const TokenTotals$json = {
  '1': 'TokenTotals',
  '2': [
    {
      '1': 'input_tokens',
      '3': 1,
      '4': 1,
      '5': 3,
      '9': 0,
      '10': 'inputTokens',
      '17': true
    },
    {
      '1': 'output_tokens',
      '3': 2,
      '4': 1,
      '5': 3,
      '9': 1,
      '10': 'outputTokens',
      '17': true
    },
    {'1': 'runs_with_usage', '3': 3, '4': 1, '5': 3, '10': 'runsWithUsage'},
    {
      '1': 'runs_without_usage',
      '3': 4,
      '4': 1,
      '5': 3,
      '10': 'runsWithoutUsage'
    },
  ],
  '8': [
    {'1': '_input_tokens'},
    {'1': '_output_tokens'},
  ],
};

/// Descriptor for `TokenTotals`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List tokenTotalsDescriptor = $convert.base64Decode(
    'CgtUb2tlblRvdGFscxImCgxpbnB1dF90b2tlbnMYASABKANIAFILaW5wdXRUb2tlbnOIAQESKA'
    'oNb3V0cHV0X3Rva2VucxgCIAEoA0gBUgxvdXRwdXRUb2tlbnOIAQESJgoPcnVuc193aXRoX3Vz'
    'YWdlGAMgASgDUg1ydW5zV2l0aFVzYWdlEiwKEnJ1bnNfd2l0aG91dF91c2FnZRgEIAEoA1IQcn'
    'Vuc1dpdGhvdXRVc2FnZUIPCg1faW5wdXRfdG9rZW5zQhAKDl9vdXRwdXRfdG9rZW5z');

@$core.Deprecated('Use toolUsageDescriptor instead')
const ToolUsage$json = {
  '1': 'ToolUsage',
  '2': [
    {'1': 'server_name', '3': 1, '4': 1, '5': 9, '10': 'serverName'},
    {'1': 'tool_name', '3': 2, '4': 1, '5': 9, '10': 'toolName'},
    {'1': 'calls', '3': 3, '4': 1, '5': 3, '10': 'calls'},
    {'1': 'failed', '3': 4, '4': 1, '5': 3, '10': 'failed'},
    {'1': 'denied', '3': 5, '4': 1, '5': 3, '10': 'denied'},
    {
      '1': 'average_duration_ms',
      '3': 6,
      '4': 1,
      '5': 3,
      '9': 0,
      '10': 'averageDurationMs',
      '17': true
    },
  ],
  '8': [
    {'1': '_average_duration_ms'},
  ],
};

/// Descriptor for `ToolUsage`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List toolUsageDescriptor = $convert.base64Decode(
    'CglUb29sVXNhZ2USHwoLc2VydmVyX25hbWUYASABKAlSCnNlcnZlck5hbWUSGwoJdG9vbF9uYW'
    '1lGAIgASgJUgh0b29sTmFtZRIUCgVjYWxscxgDIAEoA1IFY2FsbHMSFgoGZmFpbGVkGAQgASgD'
    'UgZmYWlsZWQSFgoGZGVuaWVkGAUgASgDUgZkZW5pZWQSMwoTYXZlcmFnZV9kdXJhdGlvbl9tcx'
    'gGIAEoA0gAUhFhdmVyYWdlRHVyYXRpb25Nc4gBAUIWChRfYXZlcmFnZV9kdXJhdGlvbl9tcw==');

@$core.Deprecated('Use modelUsageDescriptor instead')
const ModelUsage$json = {
  '1': 'ModelUsage',
  '2': [
    {'1': 'provider', '3': 1, '4': 1, '5': 9, '10': 'provider'},
    {'1': 'model', '3': 2, '4': 1, '5': 9, '10': 'model'},
    {'1': 'runs', '3': 3, '4': 1, '5': 3, '10': 'runs'},
    {
      '1': 'input_tokens',
      '3': 4,
      '4': 1,
      '5': 3,
      '9': 0,
      '10': 'inputTokens',
      '17': true
    },
    {
      '1': 'output_tokens',
      '3': 5,
      '4': 1,
      '5': 3,
      '9': 1,
      '10': 'outputTokens',
      '17': true
    },
    {
      '1': 'runs_without_usage',
      '3': 6,
      '4': 1,
      '5': 3,
      '10': 'runsWithoutUsage'
    },
  ],
  '8': [
    {'1': '_input_tokens'},
    {'1': '_output_tokens'},
  ],
};

/// Descriptor for `ModelUsage`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List modelUsageDescriptor = $convert.base64Decode(
    'CgpNb2RlbFVzYWdlEhoKCHByb3ZpZGVyGAEgASgJUghwcm92aWRlchIUCgVtb2RlbBgCIAEoCV'
    'IFbW9kZWwSEgoEcnVucxgDIAEoA1IEcnVucxImCgxpbnB1dF90b2tlbnMYBCABKANIAFILaW5w'
    'dXRUb2tlbnOIAQESKAoNb3V0cHV0X3Rva2VucxgFIAEoA0gBUgxvdXRwdXRUb2tlbnOIAQESLA'
    'oScnVuc193aXRob3V0X3VzYWdlGAYgASgDUhBydW5zV2l0aG91dFVzYWdlQg8KDV9pbnB1dF90'
    'b2tlbnNCEAoOX291dHB1dF90b2tlbnM=');

@$core.Deprecated('Use externalAgentUsageDescriptor instead')
const ExternalAgentUsage$json = {
  '1': 'ExternalAgentUsage',
  '2': [
    {'1': 'display_name', '3': 1, '4': 1, '5': 9, '10': 'displayName'},
    {'1': 'endpoint_host', '3': 2, '4': 1, '5': 9, '10': 'endpointHost'},
    {'1': 'runs', '3': 3, '4': 1, '5': 3, '10': 'runs'},
    {
      '1': 'input_tokens',
      '3': 4,
      '4': 1,
      '5': 3,
      '9': 0,
      '10': 'inputTokens',
      '17': true
    },
    {
      '1': 'output_tokens',
      '3': 5,
      '4': 1,
      '5': 3,
      '9': 1,
      '10': 'outputTokens',
      '17': true
    },
    {
      '1': 'runs_without_usage',
      '3': 6,
      '4': 1,
      '5': 3,
      '10': 'runsWithoutUsage'
    },
  ],
  '8': [
    {'1': '_input_tokens'},
    {'1': '_output_tokens'},
  ],
};

/// Descriptor for `ExternalAgentUsage`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List externalAgentUsageDescriptor = $convert.base64Decode(
    'ChJFeHRlcm5hbEFnZW50VXNhZ2USIQoMZGlzcGxheV9uYW1lGAEgASgJUgtkaXNwbGF5TmFtZR'
    'IjCg1lbmRwb2ludF9ob3N0GAIgASgJUgxlbmRwb2ludEhvc3QSEgoEcnVucxgDIAEoA1IEcnVu'
    'cxImCgxpbnB1dF90b2tlbnMYBCABKANIAFILaW5wdXRUb2tlbnOIAQESKAoNb3V0cHV0X3Rva2'
    'VucxgFIAEoA0gBUgxvdXRwdXRUb2tlbnOIAQESLAoScnVuc193aXRob3V0X3VzYWdlGAYgASgD'
    'UhBydW5zV2l0aG91dFVzYWdlQg8KDV9pbnB1dF90b2tlbnNCEAoOX291dHB1dF90b2tlbnM=');

@$core.Deprecated('Use automationTotalsDescriptor instead')
const AutomationTotals$json = {
  '1': 'AutomationTotals',
  '2': [
    {'1': 'runs', '3': 1, '4': 1, '5': 3, '10': 'runs'},
    {'1': 'completed', '3': 2, '4': 1, '5': 3, '10': 'completed'},
    {'1': 'failed', '3': 3, '4': 1, '5': 3, '10': 'failed'},
    {
      '1': 'unattended_approvals',
      '3': 4,
      '4': 1,
      '5': 3,
      '10': 'unattendedApprovals'
    },
  ],
};

/// Descriptor for `AutomationTotals`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List automationTotalsDescriptor = $convert.base64Decode(
    'ChBBdXRvbWF0aW9uVG90YWxzEhIKBHJ1bnMYASABKANSBHJ1bnMSHAoJY29tcGxldGVkGAIgAS'
    'gDUgljb21wbGV0ZWQSFgoGZmFpbGVkGAMgASgDUgZmYWlsZWQSMQoUdW5hdHRlbmRlZF9hcHBy'
    'b3ZhbHMYBCABKANSE3VuYXR0ZW5kZWRBcHByb3ZhbHM=');

@$core.Deprecated('Use integrationTotalsDescriptor instead')
const IntegrationTotals$json = {
  '1': 'IntegrationTotals',
  '2': [
    {'1': 'connected', '3': 1, '4': 1, '5': 3, '10': 'connected'},
    {'1': 'revoked', '3': 2, '4': 1, '5': 3, '10': 'revoked'},
  ],
};

/// Descriptor for `IntegrationTotals`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List integrationTotalsDescriptor = $convert.base64Decode(
    'ChFJbnRlZ3JhdGlvblRvdGFscxIcCgljb25uZWN0ZWQYASABKANSCWNvbm5lY3RlZBIYCgdyZX'
    'Zva2VkGAIgASgDUgdyZXZva2Vk');

@$core.Deprecated('Use dailyActivityDescriptor instead')
const DailyActivity$json = {
  '1': 'DailyActivity',
  '2': [
    {'1': 'date', '3': 1, '4': 1, '5': 9, '10': 'date'},
    {'1': 'runs', '3': 2, '4': 1, '5': 3, '10': 'runs'},
    {'1': 'tool_calls', '3': 3, '4': 1, '5': 3, '10': 'toolCalls'},
    {
      '1': 'input_tokens',
      '3': 4,
      '4': 1,
      '5': 3,
      '9': 0,
      '10': 'inputTokens',
      '17': true
    },
    {
      '1': 'output_tokens',
      '3': 5,
      '4': 1,
      '5': 3,
      '9': 1,
      '10': 'outputTokens',
      '17': true
    },
  ],
  '8': [
    {'1': '_input_tokens'},
    {'1': '_output_tokens'},
  ],
};

/// Descriptor for `DailyActivity`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List dailyActivityDescriptor = $convert.base64Decode(
    'Cg1EYWlseUFjdGl2aXR5EhIKBGRhdGUYASABKAlSBGRhdGUSEgoEcnVucxgCIAEoA1IEcnVucx'
    'IdCgp0b29sX2NhbGxzGAMgASgDUgl0b29sQ2FsbHMSJgoMaW5wdXRfdG9rZW5zGAQgASgDSABS'
    'C2lucHV0VG9rZW5ziAEBEigKDW91dHB1dF90b2tlbnMYBSABKANIAVIMb3V0cHV0VG9rZW5ziA'
    'EBQg8KDV9pbnB1dF90b2tlbnNCEAoOX291dHB1dF90b2tlbnM=');

@$core.Deprecated('Use getTelemetrySummaryRequestDescriptor instead')
const GetTelemetrySummaryRequest$json = {
  '1': 'GetTelemetrySummaryRequest',
  '2': [
    {'1': 'window_days', '3': 1, '4': 1, '5': 5, '10': 'windowDays'},
  ],
};

/// Descriptor for `GetTelemetrySummaryRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getTelemetrySummaryRequestDescriptor =
    $convert.base64Decode(
        'ChpHZXRUZWxlbWV0cnlTdW1tYXJ5UmVxdWVzdBIfCgt3aW5kb3dfZGF5cxgBIAEoBVIKd2luZG'
        '93RGF5cw==');

@$core.Deprecated('Use getTelemetrySummaryResponseDescriptor instead')
const GetTelemetrySummaryResponse$json = {
  '1': 'GetTelemetrySummaryResponse',
  '2': [
    {
      '1': 'window',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.TelemetryWindow',
      '10': 'window'
    },
    {
      '1': 'runs',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.RunTotals',
      '10': 'runs'
    },
    {
      '1': 'tokens',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.TokenTotals',
      '10': 'tokens'
    },
    {
      '1': 'tools',
      '3': 4,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.ToolUsage',
      '10': 'tools'
    },
    {
      '1': 'models',
      '3': 5,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.ModelUsage',
      '10': 'models'
    },
    {
      '1': 'external_agents',
      '3': 6,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.ExternalAgentUsage',
      '10': 'externalAgents'
    },
    {
      '1': 'automations',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.AutomationTotals',
      '10': 'automations'
    },
    {
      '1': 'integrations',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.IntegrationTotals',
      '10': 'integrations'
    },
    {
      '1': 'daily',
      '3': 9,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.DailyActivity',
      '10': 'daily'
    },
  ],
};

/// Descriptor for `GetTelemetrySummaryResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getTelemetrySummaryResponseDescriptor = $convert.base64Decode(
    'ChtHZXRUZWxlbWV0cnlTdW1tYXJ5UmVzcG9uc2USMgoGd2luZG93GAEgASgLMhoudHVyaW5nLn'
    'YxLlRlbGVtZXRyeVdpbmRvd1IGd2luZG93EigKBHJ1bnMYAiABKAsyFC50dXJpbmcudjEuUnVu'
    'VG90YWxzUgRydW5zEi4KBnRva2VucxgDIAEoCzIWLnR1cmluZy52MS5Ub2tlblRvdGFsc1IGdG'
    '9rZW5zEioKBXRvb2xzGAQgAygLMhQudHVyaW5nLnYxLlRvb2xVc2FnZVIFdG9vbHMSLQoGbW9k'
    'ZWxzGAUgAygLMhUudHVyaW5nLnYxLk1vZGVsVXNhZ2VSBm1vZGVscxJGCg9leHRlcm5hbF9hZ2'
    'VudHMYBiADKAsyHS50dXJpbmcudjEuRXh0ZXJuYWxBZ2VudFVzYWdlUg5leHRlcm5hbEFnZW50'
    'cxI9CgthdXRvbWF0aW9ucxgHIAEoCzIbLnR1cmluZy52MS5BdXRvbWF0aW9uVG90YWxzUgthdX'
    'RvbWF0aW9ucxJACgxpbnRlZ3JhdGlvbnMYCCABKAsyHC50dXJpbmcudjEuSW50ZWdyYXRpb25U'
    'b3RhbHNSDGludGVncmF0aW9ucxIuCgVkYWlseRgJIAMoCzIYLnR1cmluZy52MS5EYWlseUFjdG'
    'l2aXR5UgVkYWlseQ==');
