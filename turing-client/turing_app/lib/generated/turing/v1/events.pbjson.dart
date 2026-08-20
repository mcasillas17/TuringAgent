// This is a generated file - do not edit.
//
// Generated from turing/v1/events.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use turingEventTypeDescriptor instead')
const TuringEventType$json = {
  '1': 'TuringEventType',
  '2': [
    {'1': 'TURING_EVENT_TYPE_UNSPECIFIED', '2': 0},
    {'1': 'TURING_EVENT_TYPE_MESSAGE_STARTED', '2': 1},
    {'1': 'TURING_EVENT_TYPE_MESSAGE_DELTA', '2': 2},
    {'1': 'TURING_EVENT_TYPE_MESSAGE_COMPLETED', '2': 3},
    {'1': 'TURING_EVENT_TYPE_AGENT_RUN_QUEUED', '2': 4},
    {'1': 'TURING_EVENT_TYPE_AGENT_RUN_STARTED', '2': 5},
    {'1': 'TURING_EVENT_TYPE_AGENT_RUN_STEP', '2': 6},
    {'1': 'TURING_EVENT_TYPE_AGENT_RUN_COMPLETED', '2': 7},
    {'1': 'TURING_EVENT_TYPE_AGENT_RUN_FAILED', '2': 8},
    {'1': 'TURING_EVENT_TYPE_AGENT_RUN_CANCELLED', '2': 9},
    {'1': 'TURING_EVENT_TYPE_TOOL_CALL_STARTED', '2': 10},
    {'1': 'TURING_EVENT_TYPE_TOOL_CALL_COMPLETED', '2': 11},
    {'1': 'TURING_EVENT_TYPE_TOOL_CALL_FAILED', '2': 12},
    {'1': 'TURING_EVENT_TYPE_TOOL_CALL_DENIED', '2': 13},
    {'1': 'TURING_EVENT_TYPE_APPROVAL_REQUESTED', '2': 14},
    {'1': 'TURING_EVENT_TYPE_APPROVAL_APPROVED', '2': 15},
    {'1': 'TURING_EVENT_TYPE_APPROVAL_DENIED', '2': 16},
    {'1': 'TURING_EVENT_TYPE_APPROVAL_EXPIRED', '2': 17},
    {'1': 'TURING_EVENT_TYPE_APPROVAL_CONSUMED', '2': 18},
    {'1': 'TURING_EVENT_TYPE_ERROR', '2': 19},
    {'1': 'TURING_EVENT_TYPE_SYSTEM', '2': 20},
    {'1': 'TURING_EVENT_TYPE_SESSION_UPDATED', '2': 21},
    {'1': 'TURING_EVENT_TYPE_SESSION_DELETED', '2': 22},
  ],
};

/// Descriptor for `TuringEventType`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List turingEventTypeDescriptor = $convert.base64Decode(
    'Cg9UdXJpbmdFdmVudFR5cGUSIQodVFVSSU5HX0VWRU5UX1RZUEVfVU5TUEVDSUZJRUQQABIlCi'
    'FUVVJJTkdfRVZFTlRfVFlQRV9NRVNTQUdFX1NUQVJURUQQARIjCh9UVVJJTkdfRVZFTlRfVFlQ'
    'RV9NRVNTQUdFX0RFTFRBEAISJwojVFVSSU5HX0VWRU5UX1RZUEVfTUVTU0FHRV9DT01QTEVURU'
    'QQAxImCiJUVVJJTkdfRVZFTlRfVFlQRV9BR0VOVF9SVU5fUVVFVUVEEAQSJwojVFVSSU5HX0VW'
    'RU5UX1RZUEVfQUdFTlRfUlVOX1NUQVJURUQQBRIkCiBUVVJJTkdfRVZFTlRfVFlQRV9BR0VOVF'
    '9SVU5fU1RFUBAGEikKJVRVUklOR19FVkVOVF9UWVBFX0FHRU5UX1JVTl9DT01QTEVURUQQBxIm'
    'CiJUVVJJTkdfRVZFTlRfVFlQRV9BR0VOVF9SVU5fRkFJTEVEEAgSKQolVFVSSU5HX0VWRU5UX1'
    'RZUEVfQUdFTlRfUlVOX0NBTkNFTExFRBAJEicKI1RVUklOR19FVkVOVF9UWVBFX1RPT0xfQ0FM'
    'TF9TVEFSVEVEEAoSKQolVFVSSU5HX0VWRU5UX1RZUEVfVE9PTF9DQUxMX0NPTVBMRVRFRBALEi'
    'YKIlRVUklOR19FVkVOVF9UWVBFX1RPT0xfQ0FMTF9GQUlMRUQQDBImCiJUVVJJTkdfRVZFTlRf'
    'VFlQRV9UT09MX0NBTExfREVOSUVEEA0SKAokVFVSSU5HX0VWRU5UX1RZUEVfQVBQUk9WQUxfUk'
    'VRVUVTVEVEEA4SJwojVFVSSU5HX0VWRU5UX1RZUEVfQVBQUk9WQUxfQVBQUk9WRUQQDxIlCiFU'
    'VVJJTkdfRVZFTlRfVFlQRV9BUFBST1ZBTF9ERU5JRUQQEBImCiJUVVJJTkdfRVZFTlRfVFlQRV'
    '9BUFBST1ZBTF9FWFBJUkVEEBESJwojVFVSSU5HX0VWRU5UX1RZUEVfQVBQUk9WQUxfQ09OU1VN'
    'RUQQEhIbChdUVVJJTkdfRVZFTlRfVFlQRV9FUlJPUhATEhwKGFRVUklOR19FVkVOVF9UWVBFX1'
    'NZU1RFTRAUEiUKIVRVUklOR19FVkVOVF9UWVBFX1NFU1NJT05fVVBEQVRFRBAVEiUKIVRVUklO'
    'R19FVkVOVF9UWVBFX1NFU1NJT05fREVMRVRFRBAW');

@$core.Deprecated('Use turingEventDescriptor instead')
const TuringEvent$json = {
  '1': 'TuringEvent',
  '2': [
    {'1': 'event_id', '3': 1, '4': 1, '5': 9, '10': 'eventId'},
    {'1': 'session_id', '3': 2, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'run_id', '3': 3, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'trace_id', '3': 4, '4': 1, '5': 9, '10': 'traceId'},
    {'1': 'sequence', '3': 5, '4': 1, '5': 3, '10': 'sequence'},
    {
      '1': 'type',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.TuringEventType',
      '10': 'type'
    },
    {
      '1': 'created_at',
      '3': 7,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'createdAt'
    },
    {
      '1': 'payload',
      '3': 8,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'payload'
    },
  ],
};

/// Descriptor for `TuringEvent`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List turingEventDescriptor = $convert.base64Decode(
    'CgtUdXJpbmdFdmVudBIZCghldmVudF9pZBgBIAEoCVIHZXZlbnRJZBIdCgpzZXNzaW9uX2lkGA'
    'IgASgJUglzZXNzaW9uSWQSFQoGcnVuX2lkGAMgASgJUgVydW5JZBIZCgh0cmFjZV9pZBgEIAEo'
    'CVIHdHJhY2VJZBIaCghzZXF1ZW5jZRgFIAEoA1IIc2VxdWVuY2USLgoEdHlwZRgGIAEoDjIaLn'
    'R1cmluZy52MS5UdXJpbmdFdmVudFR5cGVSBHR5cGUSOQoKY3JlYXRlZF9hdBgHIAEoCzIaLmdv'
    'b2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBSCWNyZWF0ZWRBdBIxCgdwYXlsb2FkGAggASgLMhcuZ2'
    '9vZ2xlLnByb3RvYnVmLlN0cnVjdFIHcGF5bG9hZA==');

@$core.Deprecated('Use listEventsRequestDescriptor instead')
const ListEventsRequest$json = {
  '1': 'ListEventsRequest',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'after_sequence', '3': 2, '4': 1, '5': 3, '10': 'afterSequence'},
    {'1': 'limit', '3': 3, '4': 1, '5': 5, '10': 'limit'},
  ],
};

/// Descriptor for `ListEventsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listEventsRequestDescriptor = $convert.base64Decode(
    'ChFMaXN0RXZlbnRzUmVxdWVzdBIdCgpzZXNzaW9uX2lkGAEgASgJUglzZXNzaW9uSWQSJQoOYW'
    'Z0ZXJfc2VxdWVuY2UYAiABKANSDWFmdGVyU2VxdWVuY2USFAoFbGltaXQYAyABKAVSBWxpbWl0');

@$core.Deprecated('Use listEventsResponseDescriptor instead')
const ListEventsResponse$json = {
  '1': 'ListEventsResponse',
  '2': [
    {
      '1': 'events',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.TuringEvent',
      '10': 'events'
    },
    {'1': 'latest_sequence', '3': 2, '4': 1, '5': 3, '10': 'latestSequence'},
    {'1': 'resync_required', '3': 3, '4': 1, '5': 8, '10': 'resyncRequired'},
  ],
};

/// Descriptor for `ListEventsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listEventsResponseDescriptor = $convert.base64Decode(
    'ChJMaXN0RXZlbnRzUmVzcG9uc2USLgoGZXZlbnRzGAEgAygLMhYudHVyaW5nLnYxLlR1cmluZ0'
    'V2ZW50UgZldmVudHMSJwoPbGF0ZXN0X3NlcXVlbmNlGAIgASgDUg5sYXRlc3RTZXF1ZW5jZRIn'
    'Cg9yZXN5bmNfcmVxdWlyZWQYAyABKAhSDnJlc3luY1JlcXVpcmVk');

@$core.Deprecated('Use subscribeSessionEventsRequestDescriptor instead')
const SubscribeSessionEventsRequest$json = {
  '1': 'SubscribeSessionEventsRequest',
  '2': [
    {'1': 'session_id', '3': 1, '4': 1, '5': 9, '10': 'sessionId'},
    {'1': 'after_sequence', '3': 2, '4': 1, '5': 3, '10': 'afterSequence'},
  ],
};

/// Descriptor for `SubscribeSessionEventsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List subscribeSessionEventsRequestDescriptor =
    $convert.base64Decode(
        'Ch1TdWJzY3JpYmVTZXNzaW9uRXZlbnRzUmVxdWVzdBIdCgpzZXNzaW9uX2lkGAEgASgJUglzZX'
        'NzaW9uSWQSJQoOYWZ0ZXJfc2VxdWVuY2UYAiABKANSDWFmdGVyU2VxdWVuY2U=');

@$core.Deprecated('Use subscribeSessionUpdatesRequestDescriptor instead')
const SubscribeSessionUpdatesRequest$json = {
  '1': 'SubscribeSessionUpdatesRequest',
};

/// Descriptor for `SubscribeSessionUpdatesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List subscribeSessionUpdatesRequestDescriptor =
    $convert.base64Decode('Ch5TdWJzY3JpYmVTZXNzaW9uVXBkYXRlc1JlcXVlc3Q=');
