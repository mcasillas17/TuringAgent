// This is a generated file - do not edit.
//
// Generated from turing/v1/memory.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use memoryCandidateKindDescriptor instead')
const MemoryCandidateKind$json = {
  '1': 'MemoryCandidateKind',
  '2': [
    {'1': 'MEMORY_CANDIDATE_KIND_UNSPECIFIED', '2': 0},
    {'1': 'MEMORY_CANDIDATE_KIND_BELIEF', '2': 1},
    {'1': 'MEMORY_CANDIDATE_KIND_PROFILE_EDIT', '2': 2},
  ],
};

/// Descriptor for `MemoryCandidateKind`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List memoryCandidateKindDescriptor = $convert.base64Decode(
    'ChNNZW1vcnlDYW5kaWRhdGVLaW5kEiUKIU1FTU9SWV9DQU5ESURBVEVfS0lORF9VTlNQRUNJRk'
    'lFRBAAEiAKHE1FTU9SWV9DQU5ESURBVEVfS0lORF9CRUxJRUYQARImCiJNRU1PUllfQ0FORElE'
    'QVRFX0tJTkRfUFJPRklMRV9FRElUEAI=');

@$core.Deprecated('Use memoryCandidateStateDescriptor instead')
const MemoryCandidateState$json = {
  '1': 'MemoryCandidateState',
  '2': [
    {'1': 'MEMORY_CANDIDATE_STATE_UNSPECIFIED', '2': 0},
    {'1': 'MEMORY_CANDIDATE_STATE_PENDING', '2': 1},
    {'1': 'MEMORY_CANDIDATE_STATE_PROMOTED', '2': 2},
    {'1': 'MEMORY_CANDIDATE_STATE_REJECTED', '2': 3},
    {'1': 'MEMORY_CANDIDATE_STATE_WITHDRAWN', '2': 4},
  ],
};

/// Descriptor for `MemoryCandidateState`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List memoryCandidateStateDescriptor = $convert.base64Decode(
    'ChRNZW1vcnlDYW5kaWRhdGVTdGF0ZRImCiJNRU1PUllfQ0FORElEQVRFX1NUQVRFX1VOU1BFQ0'
    'lGSUVEEAASIgoeTUVNT1JZX0NBTkRJREFURV9TVEFURV9QRU5ESU5HEAESIwofTUVNT1JZX0NB'
    'TkRJREFURV9TVEFURV9QUk9NT1RFRBACEiMKH01FTU9SWV9DQU5ESURBVEVfU1RBVEVfUkVKRU'
    'NURUQQAxIkCiBNRU1PUllfQ0FORElEQVRFX1NUQVRFX1dJVEhEUkFXThAE');

@$core.Deprecated('Use memoryNoteStatusDescriptor instead')
const MemoryNoteStatus$json = {
  '1': 'MemoryNoteStatus',
  '2': [
    {'1': 'MEMORY_NOTE_STATUS_UNSPECIFIED', '2': 0},
    {'1': 'MEMORY_NOTE_STATUS_MANAGED', '2': 1},
    {'1': 'MEMORY_NOTE_STATUS_UNMANAGED', '2': 2},
    {'1': 'MEMORY_NOTE_STATUS_WITHDRAWN', '2': 3},
  ],
};

/// Descriptor for `MemoryNoteStatus`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List memoryNoteStatusDescriptor = $convert.base64Decode(
    'ChBNZW1vcnlOb3RlU3RhdHVzEiIKHk1FTU9SWV9OT1RFX1NUQVRVU19VTlNQRUNJRklFRBAAEh'
    '4KGk1FTU9SWV9OT1RFX1NUQVRVU19NQU5BR0VEEAESIAocTUVNT1JZX05PVEVfU1RBVFVTX1VO'
    'TUFOQUdFRBACEiAKHE1FTU9SWV9OT1RFX1NUQVRVU19XSVRIRFJBV04QAw==');

@$core.Deprecated('Use memoryProvenanceKindDescriptor instead')
const MemoryProvenanceKind$json = {
  '1': 'MemoryProvenanceKind',
  '2': [
    {'1': 'MEMORY_PROVENANCE_KIND_UNSPECIFIED', '2': 0},
    {'1': 'MEMORY_PROVENANCE_KIND_PROMOTED_FROM_CANDIDATE', '2': 1},
    {'1': 'MEMORY_PROVENANCE_KIND_USER_AUTHORED', '2': 2},
    {'1': 'MEMORY_PROVENANCE_KIND_IMPORTED', '2': 3},
  ],
};

/// Descriptor for `MemoryProvenanceKind`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List memoryProvenanceKindDescriptor = $convert.base64Decode(
    'ChRNZW1vcnlQcm92ZW5hbmNlS2luZBImCiJNRU1PUllfUFJPVkVOQU5DRV9LSU5EX1VOU1BFQ0'
    'lGSUVEEAASMgouTUVNT1JZX1BST1ZFTkFOQ0VfS0lORF9QUk9NT1RFRF9GUk9NX0NBTkRJREFU'
    'RRABEigKJE1FTU9SWV9QUk9WRU5BTkNFX0tJTkRfVVNFUl9BVVRIT1JFRBACEiMKH01FTU9SWV'
    '9QUk9WRU5BTkNFX0tJTkRfSU1QT1JURUQQAw==');

@$core.Deprecated('Use memoryUnavailableReasonDescriptor instead')
const MemoryUnavailableReason$json = {
  '1': 'MemoryUnavailableReason',
  '2': [
    {'1': 'MEMORY_UNAVAILABLE_REASON_UNSPECIFIED', '2': 0},
    {'1': 'MEMORY_UNAVAILABLE_REASON_NONE', '2': 1},
    {'1': 'MEMORY_UNAVAILABLE_REASON_DISABLED', '2': 2},
    {'1': 'MEMORY_UNAVAILABLE_REASON_VAULT_MISSING', '2': 3},
    {'1': 'MEMORY_UNAVAILABLE_REASON_VAULT_UNREADABLE', '2': 4},
    {'1': 'MEMORY_UNAVAILABLE_REASON_CONTENT_PARSE_FAILED', '2': 5},
    {'1': 'MEMORY_UNAVAILABLE_REASON_CONTENT_TOO_LARGE', '2': 6},
  ],
};

/// Descriptor for `MemoryUnavailableReason`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List memoryUnavailableReasonDescriptor = $convert.base64Decode(
    'ChdNZW1vcnlVbmF2YWlsYWJsZVJlYXNvbhIpCiVNRU1PUllfVU5BVkFJTEFCTEVfUkVBU09OX1'
    'VOU1BFQ0lGSUVEEAASIgoeTUVNT1JZX1VOQVZBSUxBQkxFX1JFQVNPTl9OT05FEAESJgoiTUVN'
    'T1JZX1VOQVZBSUxBQkxFX1JFQVNPTl9ESVNBQkxFRBACEisKJ01FTU9SWV9VTkFWQUlMQUJMRV'
    '9SRUFTT05fVkFVTFRfTUlTU0lORxADEi4KKk1FTU9SWV9VTkFWQUlMQUJMRV9SRUFTT05fVkFV'
    'TFRfVU5SRUFEQUJMRRAEEjIKLk1FTU9SWV9VTkFWQUlMQUJMRV9SRUFTT05fQ09OVEVOVF9QQV'
    'JTRV9GQUlMRUQQBRIvCitNRU1PUllfVU5BVkFJTEFCTEVfUkVBU09OX0NPTlRFTlRfVE9PX0xB'
    'UkdFEAY=');

@$core.Deprecated('Use memoryProvenanceDescriptor instead')
const MemoryProvenance$json = {
  '1': 'MemoryProvenance',
  '2': [
    {
      '1': 'kind',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryProvenanceKind',
      '10': 'kind'
    },
    {'1': 'source_session_id', '3': 2, '4': 1, '5': 9, '10': 'sourceSessionId'},
    {
      '1': 'source_session_title',
      '3': 3,
      '4': 1,
      '5': 9,
      '10': 'sourceSessionTitle'
    },
    {
      '1': 'observed_at',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'observedAt'
    },
    {'1': 'withdrawn', '3': 5, '4': 1, '5': 8, '10': 'withdrawn'},
    {
      '1': 'withdrawn_at',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'withdrawnAt'
    },
    {'1': 'evidence_count', '3': 7, '4': 1, '5': 5, '10': 'evidenceCount'},
  ],
};

/// Descriptor for `MemoryProvenance`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List memoryProvenanceDescriptor = $convert.base64Decode(
    'ChBNZW1vcnlQcm92ZW5hbmNlEjMKBGtpbmQYASABKA4yHy50dXJpbmcudjEuTWVtb3J5UHJvdm'
    'VuYW5jZUtpbmRSBGtpbmQSKgoRc291cmNlX3Nlc3Npb25faWQYAiABKAlSD3NvdXJjZVNlc3Np'
    'b25JZBIwChRzb3VyY2Vfc2Vzc2lvbl90aXRsZRgDIAEoCVISc291cmNlU2Vzc2lvblRpdGxlEj'
    'sKC29ic2VydmVkX2F0GAQgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIKb2JzZXJ2'
    'ZWRBdBIcCgl3aXRoZHJhd24YBSABKAhSCXdpdGhkcmF3bhI9Cgx3aXRoZHJhd25fYXQYBiABKA'
    'syGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wUgt3aXRoZHJhd25BdBIlCg5ldmlkZW5jZV9j'
    'b3VudBgHIAEoBVINZXZpZGVuY2VDb3VudA==');

@$core.Deprecated('Use memoryCandidateDescriptor instead')
const MemoryCandidate$json = {
  '1': 'MemoryCandidate',
  '2': [
    {'1': 'candidate_id', '3': 1, '4': 1, '5': 9, '10': 'candidateId'},
    {
      '1': 'kind',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryCandidateKind',
      '10': 'kind'
    },
    {'1': 'inbox_path', '3': 3, '4': 1, '5': 9, '10': 'inboxPath'},
    {'1': 'content', '3': 4, '4': 1, '5': 9, '10': 'content'},
    {'1': 'content_hash', '3': 5, '4': 1, '5': 9, '10': 'contentHash'},
    {
      '1': 'state',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryCandidateState',
      '10': 'state'
    },
    {
      '1': 'provenance',
      '3': 7,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.MemoryProvenance',
      '10': 'provenance'
    },
    {'1': 'promoted_note_id', '3': 8, '4': 1, '5': 9, '10': 'promotedNoteId'},
    {
      '1': 'created_at',
      '3': 9,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'createdAt'
    },
    {
      '1': 'updated_at',
      '3': 10,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'updatedAt'
    },
    {
      '1': 'decided_at',
      '3': 11,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'decidedAt'
    },
    {'1': 'parse_error', '3': 12, '4': 1, '5': 9, '10': 'parseError'},
    {
      '1': 'unavailable_reason',
      '3': 13,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryUnavailableReason',
      '10': 'unavailableReason'
    },
    {'1': 'managed', '3': 14, '4': 1, '5': 8, '10': 'managed'},
  ],
};

/// Descriptor for `MemoryCandidate`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List memoryCandidateDescriptor = $convert.base64Decode(
    'Cg9NZW1vcnlDYW5kaWRhdGUSIQoMY2FuZGlkYXRlX2lkGAEgASgJUgtjYW5kaWRhdGVJZBIyCg'
    'RraW5kGAIgASgOMh4udHVyaW5nLnYxLk1lbW9yeUNhbmRpZGF0ZUtpbmRSBGtpbmQSHQoKaW5i'
    'b3hfcGF0aBgDIAEoCVIJaW5ib3hQYXRoEhgKB2NvbnRlbnQYBCABKAlSB2NvbnRlbnQSIQoMY2'
    '9udGVudF9oYXNoGAUgASgJUgtjb250ZW50SGFzaBI1CgVzdGF0ZRgGIAEoDjIfLnR1cmluZy52'
    'MS5NZW1vcnlDYW5kaWRhdGVTdGF0ZVIFc3RhdGUSOwoKcHJvdmVuYW5jZRgHIAMoCzIbLnR1cm'
    'luZy52MS5NZW1vcnlQcm92ZW5hbmNlUgpwcm92ZW5hbmNlEigKEHByb21vdGVkX25vdGVfaWQY'
    'CCABKAlSDnByb21vdGVkTm90ZUlkEjkKCmNyZWF0ZWRfYXQYCSABKAsyGi5nb29nbGUucHJvdG'
    '9idWYuVGltZXN0YW1wUgljcmVhdGVkQXQSOQoKdXBkYXRlZF9hdBgKIAEoCzIaLmdvb2dsZS5w'
    'cm90b2J1Zi5UaW1lc3RhbXBSCXVwZGF0ZWRBdBI5CgpkZWNpZGVkX2F0GAsgASgLMhouZ29vZ2'
    'xlLnByb3RvYnVmLlRpbWVzdGFtcFIJZGVjaWRlZEF0Eh8KC3BhcnNlX2Vycm9yGAwgASgJUgpw'
    'YXJzZUVycm9yElEKEnVuYXZhaWxhYmxlX3JlYXNvbhgNIAEoDjIiLnR1cmluZy52MS5NZW1vcn'
    'lVbmF2YWlsYWJsZVJlYXNvblIRdW5hdmFpbGFibGVSZWFzb24SGAoHbWFuYWdlZBgOIAEoCFIH'
    'bWFuYWdlZA==');

@$core.Deprecated('Use memoryNoteDescriptor instead')
const MemoryNote$json = {
  '1': 'MemoryNote',
  '2': [
    {'1': 'note_id', '3': 1, '4': 1, '5': 9, '10': 'noteId'},
    {'1': 'path', '3': 2, '4': 1, '5': 9, '10': 'path'},
    {'1': 'title', '3': 3, '4': 1, '5': 9, '10': 'title'},
    {'1': 'content', '3': 4, '4': 1, '5': 9, '10': 'content'},
    {'1': 'content_hash', '3': 5, '4': 1, '5': 9, '10': 'contentHash'},
    {
      '1': 'status',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryNoteStatus',
      '10': 'status'
    },
    {
      '1': 'tier',
      '3': 7,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryTier',
      '10': 'tier'
    },
    {
      '1': 'provenance',
      '3': 8,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.MemoryProvenance',
      '10': 'provenance'
    },
    {
      '1': 'created_at',
      '3': 9,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'createdAt'
    },
    {
      '1': 'updated_at',
      '3': 10,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'updatedAt'
    },
    {'1': 'parse_error', '3': 11, '4': 1, '5': 9, '10': 'parseError'},
    {
      '1': 'unavailable_reason',
      '3': 12,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryUnavailableReason',
      '10': 'unavailableReason'
    },
  ],
};

/// Descriptor for `MemoryNote`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List memoryNoteDescriptor = $convert.base64Decode(
    'CgpNZW1vcnlOb3RlEhcKB25vdGVfaWQYASABKAlSBm5vdGVJZBISCgRwYXRoGAIgASgJUgRwYX'
    'RoEhQKBXRpdGxlGAMgASgJUgV0aXRsZRIYCgdjb250ZW50GAQgASgJUgdjb250ZW50EiEKDGNv'
    'bnRlbnRfaGFzaBgFIAEoCVILY29udGVudEhhc2gSMwoGc3RhdHVzGAYgASgOMhsudHVyaW5nLn'
    'YxLk1lbW9yeU5vdGVTdGF0dXNSBnN0YXR1cxIpCgR0aWVyGAcgASgOMhUudHVyaW5nLnYxLk1l'
    'bW9yeVRpZXJSBHRpZXISOwoKcHJvdmVuYW5jZRgIIAMoCzIbLnR1cmluZy52MS5NZW1vcnlQcm'
    '92ZW5hbmNlUgpwcm92ZW5hbmNlEjkKCmNyZWF0ZWRfYXQYCSABKAsyGi5nb29nbGUucHJvdG9i'
    'dWYuVGltZXN0YW1wUgljcmVhdGVkQXQSOQoKdXBkYXRlZF9hdBgKIAEoCzIaLmdvb2dsZS5wcm'
    '90b2J1Zi5UaW1lc3RhbXBSCXVwZGF0ZWRBdBIfCgtwYXJzZV9lcnJvchgLIAEoCVIKcGFyc2VF'
    'cnJvchJRChJ1bmF2YWlsYWJsZV9yZWFzb24YDCABKA4yIi50dXJpbmcudjEuTWVtb3J5VW5hdm'
    'FpbGFibGVSZWFzb25SEXVuYXZhaWxhYmxlUmVhc29u');

@$core.Deprecated('Use memoryProfileDescriptor instead')
const MemoryProfile$json = {
  '1': 'MemoryProfile',
  '2': [
    {'1': 'content', '3': 1, '4': 1, '5': 9, '10': 'content'},
    {'1': 'content_hash', '3': 2, '4': 1, '5': 9, '10': 'contentHash'},
    {
      '1': 'status',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryNoteStatus',
      '10': 'status'
    },
    {
      '1': 'updated_at',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'updatedAt'
    },
    {'1': 'parse_error', '3': 5, '4': 1, '5': 9, '10': 'parseError'},
    {
      '1': 'unavailable_reason',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryUnavailableReason',
      '10': 'unavailableReason'
    },
  ],
};

/// Descriptor for `MemoryProfile`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List memoryProfileDescriptor = $convert.base64Decode(
    'Cg1NZW1vcnlQcm9maWxlEhgKB2NvbnRlbnQYASABKAlSB2NvbnRlbnQSIQoMY29udGVudF9oYX'
    'NoGAIgASgJUgtjb250ZW50SGFzaBIzCgZzdGF0dXMYAyABKA4yGy50dXJpbmcudjEuTWVtb3J5'
    'Tm90ZVN0YXR1c1IGc3RhdHVzEjkKCnVwZGF0ZWRfYXQYBCABKAsyGi5nb29nbGUucHJvdG9idW'
    'YuVGltZXN0YW1wUgl1cGRhdGVkQXQSHwoLcGFyc2VfZXJyb3IYBSABKAlSCnBhcnNlRXJyb3IS'
    'UQoSdW5hdmFpbGFibGVfcmVhc29uGAYgASgOMiIudHVyaW5nLnYxLk1lbW9yeVVuYXZhaWxhYm'
    'xlUmVhc29uUhF1bmF2YWlsYWJsZVJlYXNvbg==');

@$core.Deprecated('Use memoryPersonaDescriptor instead')
const MemoryPersona$json = {
  '1': 'MemoryPersona',
  '2': [
    {'1': 'content', '3': 1, '4': 1, '5': 9, '10': 'content'},
    {'1': 'content_hash', '3': 2, '4': 1, '5': 9, '10': 'contentHash'},
    {
      '1': 'status',
      '3': 3,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryNoteStatus',
      '10': 'status'
    },
    {
      '1': 'updated_at',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'updatedAt'
    },
    {'1': 'parse_error', '3': 5, '4': 1, '5': 9, '10': 'parseError'},
    {
      '1': 'unavailable_reason',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryUnavailableReason',
      '10': 'unavailableReason'
    },
  ],
};

/// Descriptor for `MemoryPersona`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List memoryPersonaDescriptor = $convert.base64Decode(
    'Cg1NZW1vcnlQZXJzb25hEhgKB2NvbnRlbnQYASABKAlSB2NvbnRlbnQSIQoMY29udGVudF9oYX'
    'NoGAIgASgJUgtjb250ZW50SGFzaBIzCgZzdGF0dXMYAyABKA4yGy50dXJpbmcudjEuTWVtb3J5'
    'Tm90ZVN0YXR1c1IGc3RhdHVzEjkKCnVwZGF0ZWRfYXQYBCABKAsyGi5nb29nbGUucHJvdG9idW'
    'YuVGltZXN0YW1wUgl1cGRhdGVkQXQSHwoLcGFyc2VfZXJyb3IYBSABKAlSCnBhcnNlRXJyb3IS'
    'UQoSdW5hdmFpbGFibGVfcmVhc29uGAYgASgOMiIudHVyaW5nLnYxLk1lbW9yeVVuYXZhaWxhYm'
    'xlUmVhc29uUhF1bmF2YWlsYWJsZVJlYXNvbg==');

@$core.Deprecated('Use memoryTierStateDescriptor instead')
const MemoryTierState$json = {
  '1': 'MemoryTierState',
  '2': [
    {
      '1': 'tier',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryTier',
      '10': 'tier'
    },
    {'1': 'enabled', '3': 2, '4': 1, '5': 8, '10': 'enabled'},
    {'1': 'note_count', '3': 3, '4': 1, '5': 5, '10': 'noteCount'},
    {
      '1': 'pending_candidate_count',
      '3': 4,
      '4': 1,
      '5': 5,
      '10': 'pendingCandidateCount'
    },
    {
      '1': 'updated_at',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'updatedAt'
    },
    {
      '1': 'unavailable_reason',
      '3': 6,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryUnavailableReason',
      '10': 'unavailableReason'
    },
    {'1': 'parse_error', '3': 7, '4': 1, '5': 9, '10': 'parseError'},
  ],
};

/// Descriptor for `MemoryTierState`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List memoryTierStateDescriptor = $convert.base64Decode(
    'Cg9NZW1vcnlUaWVyU3RhdGUSKQoEdGllchgBIAEoDjIVLnR1cmluZy52MS5NZW1vcnlUaWVyUg'
    'R0aWVyEhgKB2VuYWJsZWQYAiABKAhSB2VuYWJsZWQSHQoKbm90ZV9jb3VudBgDIAEoBVIJbm90'
    'ZUNvdW50EjYKF3BlbmRpbmdfY2FuZGlkYXRlX2NvdW50GAQgASgFUhVwZW5kaW5nQ2FuZGlkYX'
    'RlQ291bnQSOQoKdXBkYXRlZF9hdBgFIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXBS'
    'CXVwZGF0ZWRBdBJRChJ1bmF2YWlsYWJsZV9yZWFzb24YBiABKA4yIi50dXJpbmcudjEuTWVtb3'
    'J5VW5hdmFpbGFibGVSZWFzb25SEXVuYXZhaWxhYmxlUmVhc29uEh8KC3BhcnNlX2Vycm9yGAcg'
    'ASgJUgpwYXJzZUVycm9y');

@$core.Deprecated('Use memorySettingsDescriptor instead')
const MemorySettings$json = {
  '1': 'MemorySettings',
  '2': [
    {'1': 'enabled', '3': 1, '4': 1, '5': 8, '10': 'enabled'},
    {'1': 'vault_root', '3': 2, '4': 1, '5': 9, '10': 'vaultRoot'},
    {'1': 'vault_writable', '3': 3, '4': 1, '5': 8, '10': 'vaultWritable'},
    {
      '1': 'unavailable_reason',
      '3': 4,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryUnavailableReason',
      '10': 'unavailableReason'
    },
    {'1': 'parse_error', '3': 5, '4': 1, '5': 9, '10': 'parseError'},
  ],
};

/// Descriptor for `MemorySettings`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List memorySettingsDescriptor = $convert.base64Decode(
    'Cg5NZW1vcnlTZXR0aW5ncxIYCgdlbmFibGVkGAEgASgIUgdlbmFibGVkEh0KCnZhdWx0X3Jvb3'
    'QYAiABKAlSCXZhdWx0Um9vdBIlCg52YXVsdF93cml0YWJsZRgDIAEoCFINdmF1bHRXcml0YWJs'
    'ZRJRChJ1bmF2YWlsYWJsZV9yZWFzb24YBCABKA4yIi50dXJpbmcudjEuTWVtb3J5VW5hdmFpbG'
    'FibGVSZWFzb25SEXVuYXZhaWxhYmxlUmVhc29uEh8KC3BhcnNlX2Vycm9yGAUgASgJUgpwYXJz'
    'ZUVycm9y');

@$core.Deprecated('Use listMemoryStateRequestDescriptor instead')
const ListMemoryStateRequest$json = {
  '1': 'ListMemoryStateRequest',
};

/// Descriptor for `ListMemoryStateRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listMemoryStateRequestDescriptor =
    $convert.base64Decode('ChZMaXN0TWVtb3J5U3RhdGVSZXF1ZXN0');

@$core.Deprecated('Use listMemoryStateResponseDescriptor instead')
const ListMemoryStateResponse$json = {
  '1': 'ListMemoryStateResponse',
  '2': [
    {
      '1': 'settings',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.MemorySettings',
      '10': 'settings'
    },
    {
      '1': 'tiers',
      '3': 2,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.MemoryTierState',
      '10': 'tiers'
    },
    {
      '1': 'notes',
      '3': 3,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.MemoryNote',
      '10': 'notes'
    },
    {
      '1': 'candidates',
      '3': 4,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.MemoryCandidate',
      '10': 'candidates'
    },
    {
      '1': 'profile',
      '3': 5,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.MemoryProfile',
      '10': 'profile'
    },
    {
      '1': 'persona',
      '3': 6,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.MemoryPersona',
      '10': 'persona'
    },
  ],
};

/// Descriptor for `ListMemoryStateResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listMemoryStateResponseDescriptor = $convert.base64Decode(
    'ChdMaXN0TWVtb3J5U3RhdGVSZXNwb25zZRI1CghzZXR0aW5ncxgBIAEoCzIZLnR1cmluZy52MS'
    '5NZW1vcnlTZXR0aW5nc1IIc2V0dGluZ3MSMAoFdGllcnMYAiADKAsyGi50dXJpbmcudjEuTWVt'
    'b3J5VGllclN0YXRlUgV0aWVycxIrCgVub3RlcxgDIAMoCzIVLnR1cmluZy52MS5NZW1vcnlOb3'
    'RlUgVub3RlcxI6CgpjYW5kaWRhdGVzGAQgAygLMhoudHVyaW5nLnYxLk1lbW9yeUNhbmRpZGF0'
    'ZVIKY2FuZGlkYXRlcxIyCgdwcm9maWxlGAUgASgLMhgudHVyaW5nLnYxLk1lbW9yeVByb2ZpbG'
    'VSB3Byb2ZpbGUSMgoHcGVyc29uYRgGIAEoCzIYLnR1cmluZy52MS5NZW1vcnlQZXJzb25hUgdw'
    'ZXJzb25h');

@$core.Deprecated('Use getMemorySettingsRequestDescriptor instead')
const GetMemorySettingsRequest$json = {
  '1': 'GetMemorySettingsRequest',
};

/// Descriptor for `GetMemorySettingsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getMemorySettingsRequestDescriptor =
    $convert.base64Decode('ChhHZXRNZW1vcnlTZXR0aW5nc1JlcXVlc3Q=');

@$core.Deprecated('Use setMemoryEnabledRequestDescriptor instead')
const SetMemoryEnabledRequest$json = {
  '1': 'SetMemoryEnabledRequest',
  '2': [
    {'1': 'enabled', '3': 1, '4': 1, '5': 8, '10': 'enabled'},
    {
      '1': 'tier',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryTier',
      '10': 'tier'
    },
  ],
};

/// Descriptor for `SetMemoryEnabledRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List setMemoryEnabledRequestDescriptor =
    $convert.base64Decode(
        'ChdTZXRNZW1vcnlFbmFibGVkUmVxdWVzdBIYCgdlbmFibGVkGAEgASgIUgdlbmFibGVkEikKBH'
        'RpZXIYAiABKA4yFS50dXJpbmcudjEuTWVtb3J5VGllclIEdGllcg==');

@$core.Deprecated('Use listMemoryCandidatesRequestDescriptor instead')
const ListMemoryCandidatesRequest$json = {
  '1': 'ListMemoryCandidatesRequest',
  '2': [
    {
      '1': 'state',
      '3': 1,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryCandidateState',
      '10': 'state'
    },
    {
      '1': 'kind',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryCandidateKind',
      '10': 'kind'
    },
  ],
};

/// Descriptor for `ListMemoryCandidatesRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listMemoryCandidatesRequestDescriptor =
    $convert.base64Decode(
        'ChtMaXN0TWVtb3J5Q2FuZGlkYXRlc1JlcXVlc3QSNQoFc3RhdGUYASABKA4yHy50dXJpbmcudj'
        'EuTWVtb3J5Q2FuZGlkYXRlU3RhdGVSBXN0YXRlEjIKBGtpbmQYAiABKA4yHi50dXJpbmcudjEu'
        'TWVtb3J5Q2FuZGlkYXRlS2luZFIEa2luZA==');

@$core.Deprecated('Use listMemoryCandidatesResponseDescriptor instead')
const ListMemoryCandidatesResponse$json = {
  '1': 'ListMemoryCandidatesResponse',
  '2': [
    {
      '1': 'candidates',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.MemoryCandidate',
      '10': 'candidates'
    },
    {
      '1': 'unavailable_reason',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryUnavailableReason',
      '10': 'unavailableReason'
    },
  ],
};

/// Descriptor for `ListMemoryCandidatesResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listMemoryCandidatesResponseDescriptor = $convert.base64Decode(
    'ChxMaXN0TWVtb3J5Q2FuZGlkYXRlc1Jlc3BvbnNlEjoKCmNhbmRpZGF0ZXMYASADKAsyGi50dX'
    'JpbmcudjEuTWVtb3J5Q2FuZGlkYXRlUgpjYW5kaWRhdGVzElEKEnVuYXZhaWxhYmxlX3JlYXNv'
    'bhgCIAEoDjIiLnR1cmluZy52MS5NZW1vcnlVbmF2YWlsYWJsZVJlYXNvblIRdW5hdmFpbGFibG'
    'VSZWFzb24=');

@$core.Deprecated('Use getMemoryCandidateRequestDescriptor instead')
const GetMemoryCandidateRequest$json = {
  '1': 'GetMemoryCandidateRequest',
  '2': [
    {'1': 'candidate_id', '3': 1, '4': 1, '5': 9, '10': 'candidateId'},
  ],
};

/// Descriptor for `GetMemoryCandidateRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getMemoryCandidateRequestDescriptor =
    $convert.base64Decode(
        'ChlHZXRNZW1vcnlDYW5kaWRhdGVSZXF1ZXN0EiEKDGNhbmRpZGF0ZV9pZBgBIAEoCVILY2FuZG'
        'lkYXRlSWQ=');

@$core.Deprecated('Use promoteMemoryCandidateRequestDescriptor instead')
const PromoteMemoryCandidateRequest$json = {
  '1': 'PromoteMemoryCandidateRequest',
  '2': [
    {'1': 'candidate_id', '3': 1, '4': 1, '5': 9, '10': 'candidateId'},
    {
      '1': 'expected_content_hash',
      '3': 2,
      '4': 1,
      '5': 9,
      '10': 'expectedContentHash'
    },
    {'1': 'edited_content', '3': 3, '4': 1, '5': 9, '10': 'editedContent'},
    {
      '1': 'target_tier',
      '3': 4,
      '4': 1,
      '5': 14,
      '6': '.turing.v1.MemoryTier',
      '10': 'targetTier'
    },
  ],
};

/// Descriptor for `PromoteMemoryCandidateRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List promoteMemoryCandidateRequestDescriptor = $convert.base64Decode(
    'Ch1Qcm9tb3RlTWVtb3J5Q2FuZGlkYXRlUmVxdWVzdBIhCgxjYW5kaWRhdGVfaWQYASABKAlSC2'
    'NhbmRpZGF0ZUlkEjIKFWV4cGVjdGVkX2NvbnRlbnRfaGFzaBgCIAEoCVITZXhwZWN0ZWRDb250'
    'ZW50SGFzaBIlCg5lZGl0ZWRfY29udGVudBgDIAEoCVINZWRpdGVkQ29udGVudBI2Cgt0YXJnZX'
    'RfdGllchgEIAEoDjIVLnR1cmluZy52MS5NZW1vcnlUaWVyUgp0YXJnZXRUaWVy');

@$core.Deprecated('Use promoteMemoryCandidateResponseDescriptor instead')
const PromoteMemoryCandidateResponse$json = {
  '1': 'PromoteMemoryCandidateResponse',
  '2': [
    {
      '1': 'candidate',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.MemoryCandidate',
      '10': 'candidate'
    },
    {
      '1': 'note',
      '3': 2,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.MemoryNote',
      '10': 'note'
    },
  ],
};

/// Descriptor for `PromoteMemoryCandidateResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List promoteMemoryCandidateResponseDescriptor =
    $convert.base64Decode(
        'Ch5Qcm9tb3RlTWVtb3J5Q2FuZGlkYXRlUmVzcG9uc2USOAoJY2FuZGlkYXRlGAEgASgLMhoudH'
        'VyaW5nLnYxLk1lbW9yeUNhbmRpZGF0ZVIJY2FuZGlkYXRlEikKBG5vdGUYAiABKAsyFS50dXJp'
        'bmcudjEuTWVtb3J5Tm90ZVIEbm90ZQ==');

@$core.Deprecated('Use rejectMemoryCandidateRequestDescriptor instead')
const RejectMemoryCandidateRequest$json = {
  '1': 'RejectMemoryCandidateRequest',
  '2': [
    {'1': 'candidate_id', '3': 1, '4': 1, '5': 9, '10': 'candidateId'},
    {
      '1': 'expected_content_hash',
      '3': 2,
      '4': 1,
      '5': 9,
      '10': 'expectedContentHash'
    },
    {'1': 'reason', '3': 3, '4': 1, '5': 9, '10': 'reason'},
  ],
};

/// Descriptor for `RejectMemoryCandidateRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List rejectMemoryCandidateRequestDescriptor =
    $convert.base64Decode(
        'ChxSZWplY3RNZW1vcnlDYW5kaWRhdGVSZXF1ZXN0EiEKDGNhbmRpZGF0ZV9pZBgBIAEoCVILY2'
        'FuZGlkYXRlSWQSMgoVZXhwZWN0ZWRfY29udGVudF9oYXNoGAIgASgJUhNleHBlY3RlZENvbnRl'
        'bnRIYXNoEhYKBnJlYXNvbhgDIAEoCVIGcmVhc29u');

@$core.Deprecated('Use rejectMemoryCandidateResponseDescriptor instead')
const RejectMemoryCandidateResponse$json = {
  '1': 'RejectMemoryCandidateResponse',
  '2': [
    {
      '1': 'candidate',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.MemoryCandidate',
      '10': 'candidate'
    },
  ],
};

/// Descriptor for `RejectMemoryCandidateResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List rejectMemoryCandidateResponseDescriptor =
    $convert.base64Decode(
        'Ch1SZWplY3RNZW1vcnlDYW5kaWRhdGVSZXNwb25zZRI4CgljYW5kaWRhdGUYASABKAsyGi50dX'
        'JpbmcudjEuTWVtb3J5Q2FuZGlkYXRlUgljYW5kaWRhdGU=');

@$core.Deprecated('Use getMemoryProfileRequestDescriptor instead')
const GetMemoryProfileRequest$json = {
  '1': 'GetMemoryProfileRequest',
};

/// Descriptor for `GetMemoryProfileRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getMemoryProfileRequestDescriptor =
    $convert.base64Decode('ChdHZXRNZW1vcnlQcm9maWxlUmVxdWVzdA==');

@$core.Deprecated('Use applyMemoryProfileRequestDescriptor instead')
const ApplyMemoryProfileRequest$json = {
  '1': 'ApplyMemoryProfileRequest',
  '2': [
    {'1': 'content', '3': 1, '4': 1, '5': 9, '10': 'content'},
    {
      '1': 'expected_content_hash',
      '3': 2,
      '4': 1,
      '5': 9,
      '10': 'expectedContentHash'
    },
    {'1': 'candidate_id', '3': 3, '4': 1, '5': 9, '10': 'candidateId'},
  ],
};

/// Descriptor for `ApplyMemoryProfileRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List applyMemoryProfileRequestDescriptor = $convert.base64Decode(
    'ChlBcHBseU1lbW9yeVByb2ZpbGVSZXF1ZXN0EhgKB2NvbnRlbnQYASABKAlSB2NvbnRlbnQSMg'
    'oVZXhwZWN0ZWRfY29udGVudF9oYXNoGAIgASgJUhNleHBlY3RlZENvbnRlbnRIYXNoEiEKDGNh'
    'bmRpZGF0ZV9pZBgDIAEoCVILY2FuZGlkYXRlSWQ=');

@$core.Deprecated('Use applyMemoryProfileResponseDescriptor instead')
const ApplyMemoryProfileResponse$json = {
  '1': 'ApplyMemoryProfileResponse',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.MemoryProfile',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `ApplyMemoryProfileResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List applyMemoryProfileResponseDescriptor =
    $convert.base64Decode(
        'ChpBcHBseU1lbW9yeVByb2ZpbGVSZXNwb25zZRIyCgdwcm9maWxlGAEgASgLMhgudHVyaW5nLn'
        'YxLk1lbW9yeVByb2ZpbGVSB3Byb2ZpbGU=');

@$core.Deprecated('Use getMemoryPersonaRequestDescriptor instead')
const GetMemoryPersonaRequest$json = {
  '1': 'GetMemoryPersonaRequest',
};

/// Descriptor for `GetMemoryPersonaRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List getMemoryPersonaRequestDescriptor =
    $convert.base64Decode('ChdHZXRNZW1vcnlQZXJzb25hUmVxdWVzdA==');

@$core.Deprecated('Use saveMemoryPersonaRequestDescriptor instead')
const SaveMemoryPersonaRequest$json = {
  '1': 'SaveMemoryPersonaRequest',
  '2': [
    {'1': 'content', '3': 1, '4': 1, '5': 9, '10': 'content'},
    {
      '1': 'expected_content_hash',
      '3': 2,
      '4': 1,
      '5': 9,
      '10': 'expectedContentHash'
    },
  ],
};

/// Descriptor for `SaveMemoryPersonaRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List saveMemoryPersonaRequestDescriptor =
    $convert.base64Decode(
        'ChhTYXZlTWVtb3J5UGVyc29uYVJlcXVlc3QSGAoHY29udGVudBgBIAEoCVIHY29udGVudBIyCh'
        'VleHBlY3RlZF9jb250ZW50X2hhc2gYAiABKAlSE2V4cGVjdGVkQ29udGVudEhhc2g=');

@$core.Deprecated('Use saveMemoryPersonaResponseDescriptor instead')
const SaveMemoryPersonaResponse$json = {
  '1': 'SaveMemoryPersonaResponse',
  '2': [
    {
      '1': 'persona',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.MemoryPersona',
      '10': 'persona'
    },
  ],
};

/// Descriptor for `SaveMemoryPersonaResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List saveMemoryPersonaResponseDescriptor =
    $convert.base64Decode(
        'ChlTYXZlTWVtb3J5UGVyc29uYVJlc3BvbnNlEjIKB3BlcnNvbmEYASABKAsyGC50dXJpbmcudj'
        'EuTWVtb3J5UGVyc29uYVIHcGVyc29uYQ==');

@$core.Deprecated('Use saveMemoryProfileRequestDescriptor instead')
const SaveMemoryProfileRequest$json = {
  '1': 'SaveMemoryProfileRequest',
  '2': [
    {'1': 'content', '3': 1, '4': 1, '5': 9, '10': 'content'},
    {
      '1': 'expected_content_hash',
      '3': 2,
      '4': 1,
      '5': 9,
      '10': 'expectedContentHash'
    },
  ],
};

/// Descriptor for `SaveMemoryProfileRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List saveMemoryProfileRequestDescriptor =
    $convert.base64Decode(
        'ChhTYXZlTWVtb3J5UHJvZmlsZVJlcXVlc3QSGAoHY29udGVudBgBIAEoCVIHY29udGVudBIyCh'
        'VleHBlY3RlZF9jb250ZW50X2hhc2gYAiABKAlSE2V4cGVjdGVkQ29udGVudEhhc2g=');

@$core.Deprecated('Use saveMemoryProfileResponseDescriptor instead')
const SaveMemoryProfileResponse$json = {
  '1': 'SaveMemoryProfileResponse',
  '2': [
    {
      '1': 'profile',
      '3': 1,
      '4': 1,
      '5': 11,
      '6': '.turing.v1.MemoryProfile',
      '10': 'profile'
    },
  ],
};

/// Descriptor for `SaveMemoryProfileResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List saveMemoryProfileResponseDescriptor =
    $convert.base64Decode(
        'ChlTYXZlTWVtb3J5UHJvZmlsZVJlc3BvbnNlEjIKB3Byb2ZpbGUYASABKAsyGC50dXJpbmcudj'
        'EuTWVtb3J5UHJvZmlsZVIHcHJvZmlsZQ==');

@$core.Deprecated('Use memoryToolDescriptorDescriptor instead')
const MemoryToolDescriptor$json = {
  '1': 'MemoryToolDescriptor',
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
    {'1': 'description', '3': 5, '4': 1, '5': 9, '10': 'description'},
  ],
};

/// Descriptor for `MemoryToolDescriptor`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List memoryToolDescriptorDescriptor = $convert.base64Decode(
    'ChRNZW1vcnlUb29sRGVzY3JpcHRvchIbCgl0b29sX25hbWUYASABKAlSCHRvb2xOYW1lEi0KBn'
    'BvbGljeRgCIAEoDjIVLnR1cmluZy52MS5Ub29sUG9saWN5UgZwb2xpY3kSLwoGc2NoZW1hGAMg'
    'ASgLMhcuZ29vZ2xlLnByb3RvYnVmLlN0cnVjdFIGc2NoZW1hEhgKB2VuYWJsZWQYBCABKAhSB2'
    'VuYWJsZWQSIAoLZGVzY3JpcHRpb24YBSABKAlSC2Rlc2NyaXB0aW9u');

@$core.Deprecated('Use listMemoryToolsRequestDescriptor instead')
const ListMemoryToolsRequest$json = {
  '1': 'ListMemoryToolsRequest',
};

/// Descriptor for `ListMemoryToolsRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listMemoryToolsRequestDescriptor =
    $convert.base64Decode('ChZMaXN0TWVtb3J5VG9vbHNSZXF1ZXN0');

@$core.Deprecated('Use listMemoryToolsResponseDescriptor instead')
const ListMemoryToolsResponse$json = {
  '1': 'ListMemoryToolsResponse',
  '2': [
    {
      '1': 'tools',
      '3': 1,
      '4': 3,
      '5': 11,
      '6': '.turing.v1.MemoryToolDescriptor',
      '10': 'tools'
    },
  ],
};

/// Descriptor for `ListMemoryToolsResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List listMemoryToolsResponseDescriptor =
    $convert.base64Decode(
        'ChdMaXN0TWVtb3J5VG9vbHNSZXNwb25zZRI1CgV0b29scxgBIAMoCzIfLnR1cmluZy52MS5NZW'
        '1vcnlUb29sRGVzY3JpcHRvclIFdG9vbHM=');

@$core.Deprecated('Use callMemoryToolRequestDescriptor instead')
const CallMemoryToolRequest$json = {
  '1': 'CallMemoryToolRequest',
  '2': [
    {'1': 'run_id', '3': 1, '4': 1, '5': 9, '10': 'runId'},
    {'1': 'approval_id', '3': 2, '4': 1, '5': 9, '10': 'approvalId'},
    {'1': 'tool_name', '3': 3, '4': 1, '5': 9, '10': 'toolName'},
    {
      '1': 'args',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Struct',
      '10': 'args'
    },
  ],
};

/// Descriptor for `CallMemoryToolRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List callMemoryToolRequestDescriptor = $convert.base64Decode(
    'ChVDYWxsTWVtb3J5VG9vbFJlcXVlc3QSFQoGcnVuX2lkGAEgASgJUgVydW5JZBIfCgthcHByb3'
    'ZhbF9pZBgCIAEoCVIKYXBwcm92YWxJZBIbCgl0b29sX25hbWUYAyABKAlSCHRvb2xOYW1lEisK'
    'BGFyZ3MYBCABKAsyFy5nb29nbGUucHJvdG9idWYuU3RydWN0UgRhcmdz');

@$core.Deprecated('Use callMemoryToolResponseDescriptor instead')
const CallMemoryToolResponse$json = {
  '1': 'CallMemoryToolResponse',
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

/// Descriptor for `CallMemoryToolResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List callMemoryToolResponseDescriptor =
    $convert.base64Decode(
        'ChZDYWxsTWVtb3J5VG9vbFJlc3BvbnNlEi8KBnJlc3VsdBgBIAEoCzIXLmdvb2dsZS5wcm90b2'
        'J1Zi5TdHJ1Y3RSBnJlc3VsdA==');
