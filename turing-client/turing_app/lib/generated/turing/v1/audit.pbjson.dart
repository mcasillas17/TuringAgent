// This is a generated file - do not edit.
//
// Generated from turing/v1/audit.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use auditOrderDescriptor instead')
const AuditOrder$json = {
  '1': 'AuditOrder',
  '2': [
    {'1': 'AUDIT_ORDER_UNSPECIFIED', '2': 0},
    {'1': 'AUDIT_ORDER_DESCENDING', '2': 1},
    {'1': 'AUDIT_ORDER_ASCENDING', '2': 2},
  ],
};

/// Descriptor for `AuditOrder`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List auditOrderDescriptor = $convert.base64Decode(
    'CgpBdWRpdE9yZGVyEhsKF0FVRElUX09SREVSX1VOU1BFQ0lGSUVEEAASGgoWQVVESVRfT1JERV'
    'JfREVTQ0VORElORxABEhkKFUFVRElUX09SREVSX0FTQ0VORElORxAC');

@$core.Deprecated('Use auditPayloadStateDescriptor instead')
const AuditPayloadState$json = {
  '1': 'AuditPayloadState',
  '2': [
    {'1': 'AUDIT_PAYLOAD_STATE_UNSPECIFIED', '2': 0},
    {'1': 'AUDIT_PAYLOAD_STATE_ABSENT', '2': 1},
    {'1': 'AUDIT_PAYLOAD_STATE_PRESENT', '2': 2},
    {'1': 'AUDIT_PAYLOAD_STATE_SCRUBBED', '2': 3},
  ],
};

/// Descriptor for `AuditPayloadState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List auditPayloadStateDescriptor = $convert.base64Decode(
    'ChFBdWRpdFBheWxvYWRTdGF0ZRIjCh9BVURJVF9QQVlMT0FEX1NUQVRFX1VOU1BFQ0lGSUVEEA'
    'ASHgoaQVVESVRfUEFZTE9BRF9TVEFURV9BQlNFTlQQARIfChtBVURJVF9QQVlMT0FEX1NUQVRF'
    'X1BSRVNFTlQQAhIgChxBVURJVF9QQVlMT0FEX1NUQVRFX1NDUlVCQkVEEAM=');

@$core.Deprecated('Use auditPayloadDescriptor instead')
const AuditPayload$json = {
  '1': 'AuditPayload',
  '2': [
    {
      '1': 'state',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.AuditPayloadState',
      '10': 'state'
    },
    {
      '1': 'tool_name',
      '3': 2,
      '4': 1,
      '5': 9,
      '9': 0,
      '10': 'toolName',
      '17': true
    },
    {
      '1': 'server_name',
      '3': 3,
      '4': 1,
      '5': 9,
      '9': 1,
      '10': 'serverName',
      '17': true
    },
    {'1': 'phase', '3': 4, '4': 1, '5': 9, '9': 2, '10': 'phase', '17': true},
    {'1': 'status', '3': 5, '4': 1, '5': 9, '9': 3, '10': 'status', '17': true},
    {'1': 'reason', '3': 6, '4': 1, '5': 9, '9': 4, '10': 'reason', '17': true},
    {
      '1': 'duration_ms',
      '3': 7,
      '4': 1,
      '5': 3,
      '9': 5,
      '10': 'durationMs',
      '17': true
    },
    {
      '1': 'error_code',
      '3': 8,
      '4': 1,
      '5': 9,
      '9': 6,
      '10': 'errorCode',
      '17': true
    },
    {
      '1': 'provider',
      '3': 9,
      '4': 1,
      '5': 9,
      '9': 7,
      '10': 'provider',
      '17': true
    },
    {
      '1': 'display_name',
      '3': 10,
      '4': 1,
      '5': 9,
      '9': 8,
      '10': 'displayName',
      '17': true
    },
    {
      '1': 'unattended',
      '3': 11,
      '4': 1,
      '5': 8,
      '9': 9,
      '10': 'unattended',
      '17': true
    },
    {
      '1': 'automation_id',
      '3': 12,
      '4': 1,
      '5': 9,
      '9': 10,
      '10': 'automationId',
      '17': true
    },
    {
      '1': 'automation_name',
      '3': 13,
      '4': 1,
      '5': 9,
      '9': 11,
      '10': 'automationName',
      '17': true
    },
    {
      '1': 'method',
      '3': 14,
      '4': 1,
      '5': 9,
      '9': 12,
      '10': 'method',
      '17': true
    },
    {
      '1': 'request_id',
      '3': 15,
      '4': 1,
      '5': 9,
      '9': 13,
      '10': 'requestId',
      '17': true
    },
    {
      '1': 'deleted_runs',
      '3': 16,
      '4': 1,
      '5': 3,
      '9': 14,
      '10': 'deletedRuns',
      '17': true
    },
    {
      '1': 'deleted_messages',
      '3': 17,
      '4': 1,
      '5': 3,
      '9': 15,
      '10': 'deletedMessages',
      '17': true
    },
    {
      '1': 'decision_comment',
      '3': 18,
      '4': 1,
      '5': 9,
      '9': 16,
      '10': 'decisionComment',
      '17': true
    },
    {
      '1': 'decision_comment_truncated',
      '3': 19,
      '4': 1,
      '5': 8,
      '9': 17,
      '10': 'decisionCommentTruncated',
      '17': true
    },
    {
      '1': 'denial_reason',
      '3': 20,
      '4': 1,
      '5': 9,
      '9': 18,
      '10': 'denialReason',
      '17': true
    },
    {
      '1': 'denial_reason_truncated',
      '3': 21,
      '4': 1,
      '5': 8,
      '9': 19,
      '10': 'denialReasonTruncated',
      '17': true
    },
    {
      '1': 'endpoint_host',
      '3': 22,
      '4': 1,
      '5': 9,
      '9': 20,
      '10': 'endpointHost',
      '17': true
    },
    {
      '1': 'egress_data_categories',
      '3': 23,
      '4': 3,
      '5': 14,
      '6': '.turing.v1.EgressDataCategory',
      '10': 'egressDataCategories'
    },
    {
      '1': 'egress_decision_version',
      '3': 24,
      '4': 1,
      '5': 5,
      '9': 21,
      '10': 'egressDecisionVersion',
      '17': true
    },
    {
      '1': 'egress_consent_granted_at',
      '3': 25,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '9': 22,
      '10': 'egressConsentGrantedAt',
      '17': true
    },
  ],
  '8': [
    {'1': '_tool_name'},
    {'1': '_server_name'},
    {'1': '_phase'},
    {'1': '_status'},
    {'1': '_reason'},
    {'1': '_duration_ms'},
    {'1': '_error_code'},
    {'1': '_provider'},
    {'1': '_display_name'},
    {'1': '_unattended'},
    {'1': '_automation_id'},
    {'1': '_automation_name'},
    {'1': '_method'},
    {'1': '_request_id'},
    {'1': '_deleted_runs'},
    {'1': '_deleted_messages'},
    {'1': '_decision_comment'},
    {'1': '_decision_comment_truncated'},
    {'1': '_denial_reason'},
    {'1': '_denial_reason_truncated'},
    {'1': '_endpoint_host'},
    {'1': '_egress_decision_version'},
    {'1': '_egress_consent_granted_at'},
  ],
};

/// Descriptor for `AuditPayload`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List auditPayloadDescriptor = $convert.base64Decode(
    'CgxBdWRpdFBheWxvYWQSMgoFc3RhdGUYASABKA4yHC50dXJpbmcudjEuQXVkaXRQYXlsb2FkU3'
    'RhdGVSBXN0YXRlEiAKCXRvb2xfbmFtZRgCIAEoCUgAUgh0b29sTmFtZYgBARIkCgtzZXJ2ZXJf'
    'bmFtZRgDIAEoCUgBUgpzZXJ2ZXJOYW1liAEBEhkKBXBoYXNlGAQgASgJSAJSBXBoYXNliAEBEh'
    'sKBnN0YXR1cxgFIAEoCUgDUgZzdGF0dXOIAQESGwoGcmVhc29uGAYgASgJSARSBnJlYXNvbogB'
    'ARIkCgtkdXJhdGlvbl9tcxgHIAEoA0gFUgpkdXJhdGlvbk1ziAEBEiIKCmVycm9yX2NvZGUYCC'
    'ABKAlIBlIJZXJyb3JDb2RliAEBEh8KCHByb3ZpZGVyGAkgASgJSAdSCHByb3ZpZGVyiAEBEiYK'
    'DGRpc3BsYXlfbmFtZRgKIAEoCUgIUgtkaXNwbGF5TmFtZYgBARIjCgp1bmF0dGVuZGVkGAsgAS'
    'gISAlSCnVuYXR0ZW5kZWSIAQESKAoNYXV0b21hdGlvbl9pZBgMIAEoCUgKUgxhdXRvbWF0aW9u'
    'SWSIAQESLAoPYXV0b21hdGlvbl9uYW1lGA0gASgJSAtSDmF1dG9tYXRpb25OYW1liAEBEhsKBm'
    '1ldGhvZBgOIAEoCUgMUgZtZXRob2SIAQESIgoKcmVxdWVzdF9pZBgPIAEoCUgNUglyZXF1ZXN0'
    'SWSIAQESJgoMZGVsZXRlZF9ydW5zGBAgASgDSA5SC2RlbGV0ZWRSdW5ziAEBEi4KEGRlbGV0ZW'
    'RfbWVzc2FnZXMYESABKANID1IPZGVsZXRlZE1lc3NhZ2VziAEBEi4KEGRlY2lzaW9uX2NvbW1l'
    'bnQYEiABKAlIEFIPZGVjaXNpb25Db21tZW50iAEBEkEKGmRlY2lzaW9uX2NvbW1lbnRfdHJ1bm'
    'NhdGVkGBMgASgISBFSGGRlY2lzaW9uQ29tbWVudFRydW5jYXRlZIgBARIoCg1kZW5pYWxfcmVh'
    'c29uGBQgASgJSBJSDGRlbmlhbFJlYXNvbogBARI7ChdkZW5pYWxfcmVhc29uX3RydW5jYXRlZB'
    'gVIAEoCEgTUhVkZW5pYWxSZWFzb25UcnVuY2F0ZWSIAQESKAoNZW5kcG9pbnRfaG9zdBgWIAEo'
    'CUgUUgxlbmRwb2ludEhvc3SIAQESUwoWZWdyZXNzX2RhdGFfY2F0ZWdvcmllcxgXIAMoDjIdLn'
    'R1cmluZy52MS5FZ3Jlc3NEYXRhQ2F0ZWdvcnlSFGVncmVzc0RhdGFDYXRlZ29yaWVzEjsKF2Vn'
    'cmVzc19kZWNpc2lvbl92ZXJzaW9uGBggASgFSBVSFWVncmVzc0RlY2lzaW9uVmVyc2lvbogBAR'
    'JaChllZ3Jlc3NfY29uc2VudF9ncmFudGVkX2F0GBkgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRp'
    'bWVzdGFtcEgWUhZlZ3Jlc3NDb25zZW50R3JhbnRlZEF0iAEBQgwKCl90b29sX25hbWVCDgoMX3'
    'NlcnZlcl9uYW1lQggKBl9waGFzZUIJCgdfc3RhdHVzQgkKB19yZWFzb25CDgoMX2R1cmF0aW9u'
    'X21zQg0KC19lcnJvcl9jb2RlQgsKCV9wcm92aWRlckIPCg1fZGlzcGxheV9uYW1lQg0KC191bm'
    'F0dGVuZGVkQhAKDl9hdXRvbWF0aW9uX2lkQhIKEF9hdXRvbWF0aW9uX25hbWVCCQoHX21ldGhv'
    'ZEINCgtfcmVxdWVzdF9pZEIPCg1fZGVsZXRlZF9ydW5zQhMKEV9kZWxldGVkX21lc3NhZ2VzQh'
    'MKEV9kZWNpc2lvbl9jb21tZW50Qh0KG19kZWNpc2lvbl9jb21tZW50X3RydW5jYXRlZEIQCg5f'
    'ZGVuaWFsX3JlYXNvbkIaChhfZGVuaWFsX3JlYXNvbl90cnVuY2F0ZWRCEAoOX2VuZHBvaW50X2'
    'hvc3RCGgoYX2VncmVzc19kZWNpc2lvbl92ZXJzaW9uQhwKGl9lZ3Jlc3NfY29uc2VudF9ncmFu'
    'dGVkX2F0');

@$core.Deprecated('Use auditEntryDescriptor instead')
const AuditEntry$json = {
  '1': 'AuditEntry',
  '2': [
    {'1': 'audit_id', '3': 1, '4': 1, '5': 9, '10': 'auditId'},
    {
      '1': 'correlation_id',
      '3': 2,
      '4': 1,
      '5': 9,
      '9': 0,
      '10': 'correlationId',
      '17': true
    },
    {'1': 'actor_type', '3': 3, '4': 1, '5': 9, '10': 'actorType'},
    {
      '1': 'actor_id',
      '3': 4,
      '4': 1,
      '5': 9,
      '9': 1,
      '10': 'actorId',
      '17': true
    },
    {'1': 'action', '3': 5, '4': 1, '5': 9, '10': 'action'},
    {'1': 'target', '3': 6, '4': 1, '5': 9, '9': 2, '10': 'target', '17': true},
    {
      '1': 'payload',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.AuditPayload',
      '10': 'payload'
    },
    {
      '1': 'created_at',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'createdAt'
    },
  ],
  '8': [
    {'1': '_correlation_id'},
    {'1': '_actor_id'},
    {'1': '_target'},
  ],
};

/// Descriptor for `AuditEntry`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List auditEntryDescriptor = $convert.base64Decode(
    'CgpBdWRpdEVudHJ5EhkKCGF1ZGl0X2lkGAEgASgJUgdhdWRpdElkEioKDmNvcnJlbGF0aW9uX2'
    'lkGAIgASgJSABSDWNvcnJlbGF0aW9uSWSIAQESHQoKYWN0b3JfdHlwZRgDIAEoCVIJYWN0b3JU'
    'eXBlEh4KCGFjdG9yX2lkGAQgASgJSAFSB2FjdG9ySWSIAQESFgoGYWN0aW9uGAUgASgJUgZhY3'
    'Rpb24SGwoGdGFyZ2V0GAYgASgJSAJSBnRhcmdldIgBARIxCgdwYXlsb2FkGAcgASgLMhcudHVy'
    'aW5nLnYxLkF1ZGl0UGF5bG9hZFIHcGF5bG9hZBI5CgpjcmVhdGVkX2F0GAggASgLMhouZ29vZ2'
    'xlLnByb3RvYnVmLlRpbWVzdGFtcFIJY3JlYXRlZEF0QhEKD19jb3JyZWxhdGlvbl9pZEILCglf'
    'YWN0b3JfaWRCCQoHX3RhcmdldA==');

@$core.Deprecated('Use listAuditEntriesRequestDescriptor instead')
const ListAuditEntriesRequest$json = {
  '1': 'ListAuditEntriesRequest',
  '2': [
    {
      '1': 'correlation_id',
      '3': 1,
      '4': 1,
      '5': 9,
      '9': 0,
      '10': 'correlationId',
      '17': true
    },
    {'1': 'action', '3': 2, '4': 1, '5': 9, '9': 1, '10': 'action', '17': true},
    {
      '1': 'created_at_start',
      '3': 3,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '9': 2,
      '10': 'createdAtStart',
      '17': true
    },
    {
      '1': 'created_at_end',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '9': 3,
      '10': 'createdAtEnd',
      '17': true
    },
    {
      '1': 'order',
      '3': 5,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.AuditOrder',
      '10': 'order'
    },
    {
      '1': 'page',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.PageRequest',
      '10': 'page'
    },
  ],
  '8': [
    {'1': '_correlation_id'},
    {'1': '_action'},
    {'1': '_created_at_start'},
    {'1': '_created_at_end'},
  ],
};

/// Descriptor for `ListAuditEntriesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listAuditEntriesRequestDescriptor = $convert.base64Decode(
    'ChdMaXN0QXVkaXRFbnRyaWVzUmVxdWVzdBIqCg5jb3JyZWxhdGlvbl9pZBgBIAEoCUgAUg1jb3'
    'JyZWxhdGlvbklkiAEBEhsKBmFjdGlvbhgCIAEoCUgBUgZhY3Rpb26IAQESSQoQY3JlYXRlZF9h'
    'dF9zdGFydBgDIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBIAlIOY3JlYXRlZEF0U3'
    'RhcnSIAQESRQoOY3JlYXRlZF9hdF9lbmQYBCABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0'
    'YW1wSANSDGNyZWF0ZWRBdEVuZIgBARIrCgVvcmRlchgFIAEoDjIVLnR1cmluZy52MS5BdWRpdE'
    '9yZGVyUgVvcmRlchIqCgRwYWdlGAYgASgLMhYudHVyaW5nLnYxLlBhZ2VSZXF1ZXN0UgRwYWdl'
    'QhEKD19jb3JyZWxhdGlvbl9pZEIJCgdfYWN0aW9uQhMKEV9jcmVhdGVkX2F0X3N0YXJ0QhEKD1'
    '9jcmVhdGVkX2F0X2VuZA==');

@$core.Deprecated('Use listAuditEntriesResponseDescriptor instead')
const ListAuditEntriesResponse$json = {
  '1': 'ListAuditEntriesResponse',
  '2': [
    {
      '1': 'entries',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.AuditEntry',
      '10': 'entries'
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

/// Descriptor for `ListAuditEntriesResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listAuditEntriesResponseDescriptor = $convert.base64Decode(
    'ChhMaXN0QXVkaXRFbnRyaWVzUmVzcG9uc2USLwoHZW50cmllcxgBIAMoCzIVLnR1cmluZy52MS'
    '5BdWRpdEVudHJ5UgdlbnRyaWVzEisKBHBhZ2UYAiABKAsyFy50dXJpbmcudjEuUGFnZVJlc3Bv'
    'bnNlUgRwYWdl');
