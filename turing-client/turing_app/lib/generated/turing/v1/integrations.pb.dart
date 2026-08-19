// This is a generated file - do not edit.
//
// Generated from turing/v1/integrations.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

import '../../google/protobuf/timestamp.pb.dart' as $1;
import 'integrations.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'integrations.pbenum.dart';

/// What a provider is, what credential it takes, and what that credential
/// grants. Served by the backend rather than hardcoded in a client, so every
/// client states the same thing about what connecting gives away.
class ProviderDescriptor extends $pb.GeneratedMessage {
  factory ProviderDescriptor({
    IntegrationProvider? provider,
    $core.String? displayName,
    $core.String? category,
    $core.bool? supported,
    $core.String? unsupportedReason,
    $core.String? secretLabel,
    $core.String? secretHelp,
    $core.String? accountLabel,
    $core.bool? requiresEndpoint,
    $core.String? endpointLabel,
    $core.Iterable<$core.String>? grants,
  }) {
    final result = create();
    if (provider != null) result.provider = provider;
    if (displayName != null) result.displayName = displayName;
    if (category != null) result.category = category;
    if (supported != null) result.supported = supported;
    if (unsupportedReason != null) result.unsupportedReason = unsupportedReason;
    if (secretLabel != null) result.secretLabel = secretLabel;
    if (secretHelp != null) result.secretHelp = secretHelp;
    if (accountLabel != null) result.accountLabel = accountLabel;
    if (requiresEndpoint != null) result.requiresEndpoint = requiresEndpoint;
    if (endpointLabel != null) result.endpointLabel = endpointLabel;
    if (grants != null) result.grants.addAll(grants);
    return result;
  }

  ProviderDescriptor._();

  factory ProviderDescriptor.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ProviderDescriptor.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ProviderDescriptor',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..e<IntegrationProvider>(
        1, _omitFieldNames ? '' : 'provider', $pb.PbFieldType.OE,
        defaultOrMaker: IntegrationProvider.INTEGRATION_PROVIDER_UNSPECIFIED,
        valueOf: IntegrationProvider.valueOf,
        enumValues: IntegrationProvider.values)
    ..aOS(2, _omitFieldNames ? '' : 'displayName')
    ..aOS(3, _omitFieldNames ? '' : 'category')
    ..aOB(4, _omitFieldNames ? '' : 'supported')
    ..aOS(5, _omitFieldNames ? '' : 'unsupportedReason')
    ..aOS(6, _omitFieldNames ? '' : 'secretLabel')
    ..aOS(7, _omitFieldNames ? '' : 'secretHelp')
    ..aOS(8, _omitFieldNames ? '' : 'accountLabel')
    ..aOB(9, _omitFieldNames ? '' : 'requiresEndpoint')
    ..aOS(10, _omitFieldNames ? '' : 'endpointLabel')
    ..pPS(11, _omitFieldNames ? '' : 'grants')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ProviderDescriptor clone() => ProviderDescriptor()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ProviderDescriptor copyWith(void Function(ProviderDescriptor) updates) =>
      super.copyWith((message) => updates(message as ProviderDescriptor))
          as ProviderDescriptor;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ProviderDescriptor create() => ProviderDescriptor._();
  @$core.override
  ProviderDescriptor createEmptyInstance() => create();
  static $pb.PbList<ProviderDescriptor> createRepeated() =>
      $pb.PbList<ProviderDescriptor>();
  @$core.pragma('dart2js:noInline')
  static ProviderDescriptor getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ProviderDescriptor>(create);
  static ProviderDescriptor? _defaultInstance;

  @$pb.TagNumber(1)
  IntegrationProvider get provider => $_getN(0);
  @$pb.TagNumber(1)
  set provider(IntegrationProvider value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProvider() => $_has(0);
  @$pb.TagNumber(1)
  void clearProvider() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get displayName => $_getSZ(1);
  @$pb.TagNumber(2)
  set displayName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDisplayName() => $_has(1);
  @$pb.TagNumber(2)
  void clearDisplayName() => $_clearField(2);

  /// "Mail", "Calendar", "Notes", "Code" — how the user thinks about it.
  @$pb.TagNumber(3)
  $core.String get category => $_getSZ(2);
  @$pb.TagNumber(3)
  set category($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasCategory() => $_has(2);
  @$pb.TagNumber(3)
  void clearCategory() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.bool get supported => $_getBF(3);
  @$pb.TagNumber(4)
  set supported($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasSupported() => $_has(3);
  @$pb.TagNumber(4)
  void clearSupported() => $_clearField(4);

  /// Why it cannot be connected. Set only when supported is false.
  @$pb.TagNumber(5)
  $core.String get unsupportedReason => $_getSZ(4);
  @$pb.TagNumber(5)
  set unsupportedReason($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasUnsupportedReason() => $_has(4);
  @$pb.TagNumber(5)
  void clearUnsupportedReason() => $_clearField(5);

  /// What the user is being asked to paste, and where they get it.
  @$pb.TagNumber(6)
  $core.String get secretLabel => $_getSZ(5);
  @$pb.TagNumber(6)
  set secretLabel($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasSecretLabel() => $_has(5);
  @$pb.TagNumber(6)
  void clearSecretLabel() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.String get secretHelp => $_getSZ(6);
  @$pb.TagNumber(7)
  set secretHelp($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasSecretHelp() => $_has(6);
  @$pb.TagNumber(7)
  void clearSecretHelp() => $_clearField(7);

  /// Label for the non-secret account identifier (an address, a workspace).
  @$pb.TagNumber(8)
  $core.String get accountLabel => $_getSZ(7);
  @$pb.TagNumber(8)
  set accountLabel($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasAccountLabel() => $_has(7);
  @$pb.TagNumber(8)
  void clearAccountLabel() => $_clearField(8);

  @$pb.TagNumber(9)
  $core.bool get requiresEndpoint => $_getBF(8);
  @$pb.TagNumber(9)
  set requiresEndpoint($core.bool value) => $_setBool(8, value);
  @$pb.TagNumber(9)
  $core.bool hasRequiresEndpoint() => $_has(8);
  @$pb.TagNumber(9)
  void clearRequiresEndpoint() => $_clearField(9);

  @$pb.TagNumber(10)
  $core.String get endpointLabel => $_getSZ(9);
  @$pb.TagNumber(10)
  set endpointLabel($core.String value) => $_setString(9, value);
  @$pb.TagNumber(10)
  $core.bool hasEndpointLabel() => $_has(9);
  @$pb.TagNumber(10)
  void clearEndpointLabel() => $_clearField(10);

  /// Exactly what a credential of this kind lets the holder do, in the user's
  /// terms. These are statements, not options: none of them can be narrowed
  /// from here, because the scope of a pasted credential is decided where it
  /// was minted. A client must show all of them before connecting.
  @$pb.TagNumber(11)
  $pb.PbList<$core.String> get grants => $_getList(10);
}

/// A connected account.
///
/// No field here carries the credential, and none ever will. The stored secret
/// is returned by no RPC in this file; a connection is described by its
/// provider, the account it points at, and a redacted hint — enough to tell two
/// connections apart, not enough to use one.
class Connection extends $pb.GeneratedMessage {
  factory Connection({
    $core.String? connectionId,
    IntegrationProvider? provider,
    $core.String? displayName,
    $core.String? accountLabel,
    $core.String? endpoint,
    $core.String? credentialHint,
    ConnectionStatus? status,
    $core.Iterable<$core.String>? grantedScopes,
    $1.Timestamp? consentGrantedAt,
    $1.Timestamp? connectedAt,
    $1.Timestamp? revokedAt,
    $1.Timestamp? updatedAt,
    $core.bool? credentialUnreadable,
  }) {
    final result = create();
    if (connectionId != null) result.connectionId = connectionId;
    if (provider != null) result.provider = provider;
    if (displayName != null) result.displayName = displayName;
    if (accountLabel != null) result.accountLabel = accountLabel;
    if (endpoint != null) result.endpoint = endpoint;
    if (credentialHint != null) result.credentialHint = credentialHint;
    if (status != null) result.status = status;
    if (grantedScopes != null) result.grantedScopes.addAll(grantedScopes);
    if (consentGrantedAt != null) result.consentGrantedAt = consentGrantedAt;
    if (connectedAt != null) result.connectedAt = connectedAt;
    if (revokedAt != null) result.revokedAt = revokedAt;
    if (updatedAt != null) result.updatedAt = updatedAt;
    if (credentialUnreadable != null)
      result.credentialUnreadable = credentialUnreadable;
    return result;
  }

  Connection._();

  factory Connection.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Connection.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Connection',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'connectionId')
    ..e<IntegrationProvider>(
        2, _omitFieldNames ? '' : 'provider', $pb.PbFieldType.OE,
        defaultOrMaker: IntegrationProvider.INTEGRATION_PROVIDER_UNSPECIFIED,
        valueOf: IntegrationProvider.valueOf,
        enumValues: IntegrationProvider.values)
    ..aOS(3, _omitFieldNames ? '' : 'displayName')
    ..aOS(4, _omitFieldNames ? '' : 'accountLabel')
    ..aOS(5, _omitFieldNames ? '' : 'endpoint')
    ..aOS(6, _omitFieldNames ? '' : 'credentialHint')
    ..e<ConnectionStatus>(
        7, _omitFieldNames ? '' : 'status', $pb.PbFieldType.OE,
        defaultOrMaker: ConnectionStatus.CONNECTION_STATUS_UNSPECIFIED,
        valueOf: ConnectionStatus.valueOf,
        enumValues: ConnectionStatus.values)
    ..pPS(8, _omitFieldNames ? '' : 'grantedScopes')
    ..aOM<$1.Timestamp>(9, _omitFieldNames ? '' : 'consentGrantedAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(10, _omitFieldNames ? '' : 'connectedAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(11, _omitFieldNames ? '' : 'revokedAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(12, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $1.Timestamp.create)
    ..aOB(13, _omitFieldNames ? '' : 'credentialUnreadable')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Connection clone() => Connection()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Connection copyWith(void Function(Connection) updates) =>
      super.copyWith((message) => updates(message as Connection)) as Connection;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Connection create() => Connection._();
  @$core.override
  Connection createEmptyInstance() => create();
  static $pb.PbList<Connection> createRepeated() => $pb.PbList<Connection>();
  @$core.pragma('dart2js:noInline')
  static Connection getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Connection>(create);
  static Connection? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get connectionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set connectionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasConnectionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearConnectionId() => $_clearField(1);

  @$pb.TagNumber(2)
  IntegrationProvider get provider => $_getN(1);
  @$pb.TagNumber(2)
  set provider(IntegrationProvider value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProvider() => $_has(1);
  @$pb.TagNumber(2)
  void clearProvider() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get displayName => $_getSZ(2);
  @$pb.TagNumber(3)
  set displayName($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasDisplayName() => $_has(2);
  @$pb.TagNumber(3)
  void clearDisplayName() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get accountLabel => $_getSZ(3);
  @$pb.TagNumber(4)
  set accountLabel($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasAccountLabel() => $_has(3);
  @$pb.TagNumber(4)
  void clearAccountLabel() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get endpoint => $_getSZ(4);
  @$pb.TagNumber(5)
  set endpoint($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasEndpoint() => $_has(4);
  @$pb.TagNumber(5)
  void clearEndpoint() => $_clearField(5);

  /// A redaction of the stored credential: bullets, plus at most the last four
  /// characters of a secret long enough that four characters do not give it
  /// away. Empty once revoked.
  @$pb.TagNumber(6)
  $core.String get credentialHint => $_getSZ(5);
  @$pb.TagNumber(6)
  set credentialHint($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasCredentialHint() => $_has(5);
  @$pb.TagNumber(6)
  void clearCredentialHint() => $_clearField(6);

  @$pb.TagNumber(7)
  ConnectionStatus get status => $_getN(6);
  @$pb.TagNumber(7)
  set status(ConnectionStatus value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasStatus() => $_has(6);
  @$pb.TagNumber(7)
  void clearStatus() => $_clearField(7);

  /// What the user agreed to when they connected, captured at that moment. If
  /// a later build widens what a provider grants, this still says what was
  /// actually consented to.
  @$pb.TagNumber(8)
  $pb.PbList<$core.String> get grantedScopes => $_getList(7);

  @$pb.TagNumber(9)
  $1.Timestamp get consentGrantedAt => $_getN(8);
  @$pb.TagNumber(9)
  set consentGrantedAt($1.Timestamp value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasConsentGrantedAt() => $_has(8);
  @$pb.TagNumber(9)
  void clearConsentGrantedAt() => $_clearField(9);
  @$pb.TagNumber(9)
  $1.Timestamp ensureConsentGrantedAt() => $_ensure(8);

  @$pb.TagNumber(10)
  $1.Timestamp get connectedAt => $_getN(9);
  @$pb.TagNumber(10)
  set connectedAt($1.Timestamp value) => $_setField(10, value);
  @$pb.TagNumber(10)
  $core.bool hasConnectedAt() => $_has(9);
  @$pb.TagNumber(10)
  void clearConnectedAt() => $_clearField(10);
  @$pb.TagNumber(10)
  $1.Timestamp ensureConnectedAt() => $_ensure(9);

  @$pb.TagNumber(11)
  $1.Timestamp get revokedAt => $_getN(10);
  @$pb.TagNumber(11)
  set revokedAt($1.Timestamp value) => $_setField(11, value);
  @$pb.TagNumber(11)
  $core.bool hasRevokedAt() => $_has(10);
  @$pb.TagNumber(11)
  void clearRevokedAt() => $_clearField(11);
  @$pb.TagNumber(11)
  $1.Timestamp ensureRevokedAt() => $_ensure(10);

  @$pb.TagNumber(12)
  $1.Timestamp get updatedAt => $_getN(11);
  @$pb.TagNumber(12)
  set updatedAt($1.Timestamp value) => $_setField(12, value);
  @$pb.TagNumber(12)
  $core.bool hasUpdatedAt() => $_has(11);
  @$pb.TagNumber(12)
  void clearUpdatedAt() => $_clearField(12);
  @$pb.TagNumber(12)
  $1.Timestamp ensureUpdatedAt() => $_ensure(11);

  /// True when the stored credential was sealed with a key the backend no
  /// longer has — TURING_INTEGRATION_KEY was rotated, lost, or restored from a
  /// different .env. The connection can never be used again and must be
  /// reconnected. Answered from the sealed value's key fingerprint, without
  /// decrypting anything; a connection that quietly claimed to work would be
  /// the app asserting access it does not have.
  @$pb.TagNumber(13)
  $core.bool get credentialUnreadable => $_getBF(12);
  @$pb.TagNumber(13)
  set credentialUnreadable($core.bool value) => $_setBool(12, value);
  @$pb.TagNumber(13)
  $core.bool hasCredentialUnreadable() => $_has(12);
  @$pb.TagNumber(13)
  void clearCredentialUnreadable() => $_clearField(13);
}

class ListProvidersRequest extends $pb.GeneratedMessage {
  factory ListProvidersRequest() => create();

  ListProvidersRequest._();

  factory ListProvidersRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListProvidersRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListProvidersRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListProvidersRequest clone() =>
      ListProvidersRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListProvidersRequest copyWith(void Function(ListProvidersRequest) updates) =>
      super.copyWith((message) => updates(message as ListProvidersRequest))
          as ListProvidersRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListProvidersRequest create() => ListProvidersRequest._();
  @$core.override
  ListProvidersRequest createEmptyInstance() => create();
  static $pb.PbList<ListProvidersRequest> createRepeated() =>
      $pb.PbList<ListProvidersRequest>();
  @$core.pragma('dart2js:noInline')
  static ListProvidersRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListProvidersRequest>(create);
  static ListProvidersRequest? _defaultInstance;
}

class ListProvidersResponse extends $pb.GeneratedMessage {
  factory ListProvidersResponse({
    $core.Iterable<ProviderDescriptor>? providers,
    $core.bool? credentialStorageConfigured,
    $core.String? storageUnconfiguredReason,
  }) {
    final result = create();
    if (providers != null) result.providers.addAll(providers);
    if (credentialStorageConfigured != null)
      result.credentialStorageConfigured = credentialStorageConfigured;
    if (storageUnconfiguredReason != null)
      result.storageUnconfiguredReason = storageUnconfiguredReason;
    return result;
  }

  ListProvidersResponse._();

  factory ListProvidersResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListProvidersResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListProvidersResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..pc<ProviderDescriptor>(
        1, _omitFieldNames ? '' : 'providers', $pb.PbFieldType.PM,
        subBuilder: ProviderDescriptor.create)
    ..aOB(2, _omitFieldNames ? '' : 'credentialStorageConfigured')
    ..aOS(3, _omitFieldNames ? '' : 'storageUnconfiguredReason')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListProvidersResponse clone() =>
      ListProvidersResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListProvidersResponse copyWith(
          void Function(ListProvidersResponse) updates) =>
      super.copyWith((message) => updates(message as ListProvidersResponse))
          as ListProvidersResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListProvidersResponse create() => ListProvidersResponse._();
  @$core.override
  ListProvidersResponse createEmptyInstance() => create();
  static $pb.PbList<ListProvidersResponse> createRepeated() =>
      $pb.PbList<ListProvidersResponse>();
  @$core.pragma('dart2js:noInline')
  static ListProvidersResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListProvidersResponse>(create);
  static ListProvidersResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<ProviderDescriptor> get providers => $_getList(0);

  /// False when no TURING_INTEGRATION_KEY is configured, in which case nothing
  /// can be connected because there is nothing to seal a credential with. Sent
  /// with the catalogue so a client can say so before asking anyone to paste a
  /// live app password into a form that cannot work.
  @$pb.TagNumber(2)
  $core.bool get credentialStorageConfigured => $_getBF(1);
  @$pb.TagNumber(2)
  set credentialStorageConfigured($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCredentialStorageConfigured() => $_has(1);
  @$pb.TagNumber(2)
  void clearCredentialStorageConfigured() => $_clearField(2);

  /// Why it is not configured, and what to do about it. Empty when it is.
  @$pb.TagNumber(3)
  $core.String get storageUnconfiguredReason => $_getSZ(2);
  @$pb.TagNumber(3)
  set storageUnconfiguredReason($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasStorageUnconfiguredReason() => $_has(2);
  @$pb.TagNumber(3)
  void clearStorageUnconfiguredReason() => $_clearField(3);
}

class ConnectAccountRequest extends $pb.GeneratedMessage {
  factory ConnectAccountRequest({
    IntegrationProvider? provider,
    $core.String? displayName,
    $core.String? accountLabel,
    $core.String? endpoint,
    $core.String? credential,
    $core.bool? consentAcknowledged,
  }) {
    final result = create();
    if (provider != null) result.provider = provider;
    if (displayName != null) result.displayName = displayName;
    if (accountLabel != null) result.accountLabel = accountLabel;
    if (endpoint != null) result.endpoint = endpoint;
    if (credential != null) result.credential = credential;
    if (consentAcknowledged != null)
      result.consentAcknowledged = consentAcknowledged;
    return result;
  }

  ConnectAccountRequest._();

  factory ConnectAccountRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ConnectAccountRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ConnectAccountRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..e<IntegrationProvider>(
        1, _omitFieldNames ? '' : 'provider', $pb.PbFieldType.OE,
        defaultOrMaker: IntegrationProvider.INTEGRATION_PROVIDER_UNSPECIFIED,
        valueOf: IntegrationProvider.valueOf,
        enumValues: IntegrationProvider.values)
    ..aOS(2, _omitFieldNames ? '' : 'displayName')
    ..aOS(3, _omitFieldNames ? '' : 'accountLabel')
    ..aOS(4, _omitFieldNames ? '' : 'endpoint')
    ..aOS(5, _omitFieldNames ? '' : 'credential')
    ..aOB(6, _omitFieldNames ? '' : 'consentAcknowledged')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ConnectAccountRequest clone() =>
      ConnectAccountRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ConnectAccountRequest copyWith(
          void Function(ConnectAccountRequest) updates) =>
      super.copyWith((message) => updates(message as ConnectAccountRequest))
          as ConnectAccountRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ConnectAccountRequest create() => ConnectAccountRequest._();
  @$core.override
  ConnectAccountRequest createEmptyInstance() => create();
  static $pb.PbList<ConnectAccountRequest> createRepeated() =>
      $pb.PbList<ConnectAccountRequest>();
  @$core.pragma('dart2js:noInline')
  static ConnectAccountRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ConnectAccountRequest>(create);
  static ConnectAccountRequest? _defaultInstance;

  @$pb.TagNumber(1)
  IntegrationProvider get provider => $_getN(0);
  @$pb.TagNumber(1)
  set provider(IntegrationProvider value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProvider() => $_has(0);
  @$pb.TagNumber(1)
  void clearProvider() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get displayName => $_getSZ(1);
  @$pb.TagNumber(2)
  set displayName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDisplayName() => $_has(1);
  @$pb.TagNumber(2)
  void clearDisplayName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get accountLabel => $_getSZ(2);
  @$pb.TagNumber(3)
  set accountLabel($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasAccountLabel() => $_has(2);
  @$pb.TagNumber(3)
  void clearAccountLabel() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get endpoint => $_getSZ(3);
  @$pb.TagNumber(4)
  set endpoint($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasEndpoint() => $_has(3);
  @$pb.TagNumber(4)
  void clearEndpoint() => $_clearField(4);

  /// Write-only. This is the only message in the API that carries a secret,
  /// and it only travels client to server. It is sealed before it reaches
  /// storage, is never written to an event or an audit record, and is never
  /// read back out over the wire.
  @$pb.TagNumber(5)
  $core.String get credential => $_getSZ(4);
  @$pb.TagNumber(5)
  set credential($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasCredential() => $_has(4);
  @$pb.TagNumber(5)
  void clearCredential() => $_clearField(5);

  /// The user's consent to the provider's grants, taken at connect time. The
  /// server refuses the request when this is false rather than treating a
  /// missing field as agreement, so a client cannot connect an account by
  /// omission.
  @$pb.TagNumber(6)
  $core.bool get consentAcknowledged => $_getBF(5);
  @$pb.TagNumber(6)
  set consentAcknowledged($core.bool value) => $_setBool(5, value);
  @$pb.TagNumber(6)
  $core.bool hasConsentAcknowledged() => $_has(5);
  @$pb.TagNumber(6)
  void clearConsentAcknowledged() => $_clearField(6);
}

class ListConnectionsRequest extends $pb.GeneratedMessage {
  factory ListConnectionsRequest() => create();

  ListConnectionsRequest._();

  factory ListConnectionsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListConnectionsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListConnectionsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListConnectionsRequest clone() =>
      ListConnectionsRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListConnectionsRequest copyWith(
          void Function(ListConnectionsRequest) updates) =>
      super.copyWith((message) => updates(message as ListConnectionsRequest))
          as ListConnectionsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListConnectionsRequest create() => ListConnectionsRequest._();
  @$core.override
  ListConnectionsRequest createEmptyInstance() => create();
  static $pb.PbList<ListConnectionsRequest> createRepeated() =>
      $pb.PbList<ListConnectionsRequest>();
  @$core.pragma('dart2js:noInline')
  static ListConnectionsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListConnectionsRequest>(create);
  static ListConnectionsRequest? _defaultInstance;
}

class ListConnectionsResponse extends $pb.GeneratedMessage {
  factory ListConnectionsResponse({
    $core.Iterable<Connection>? connections,
  }) {
    final result = create();
    if (connections != null) result.connections.addAll(connections);
    return result;
  }

  ListConnectionsResponse._();

  factory ListConnectionsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListConnectionsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListConnectionsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..pc<Connection>(
        1, _omitFieldNames ? '' : 'connections', $pb.PbFieldType.PM,
        subBuilder: Connection.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListConnectionsResponse clone() =>
      ListConnectionsResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListConnectionsResponse copyWith(
          void Function(ListConnectionsResponse) updates) =>
      super.copyWith((message) => updates(message as ListConnectionsResponse))
          as ListConnectionsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListConnectionsResponse create() => ListConnectionsResponse._();
  @$core.override
  ListConnectionsResponse createEmptyInstance() => create();
  static $pb.PbList<ListConnectionsResponse> createRepeated() =>
      $pb.PbList<ListConnectionsResponse>();
  @$core.pragma('dart2js:noInline')
  static ListConnectionsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListConnectionsResponse>(create);
  static ListConnectionsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<Connection> get connections => $_getList(0);
}

class GetConnectionRequest extends $pb.GeneratedMessage {
  factory GetConnectionRequest({
    $core.String? connectionId,
  }) {
    final result = create();
    if (connectionId != null) result.connectionId = connectionId;
    return result;
  }

  GetConnectionRequest._();

  factory GetConnectionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetConnectionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetConnectionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'connectionId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetConnectionRequest clone() =>
      GetConnectionRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetConnectionRequest copyWith(void Function(GetConnectionRequest) updates) =>
      super.copyWith((message) => updates(message as GetConnectionRequest))
          as GetConnectionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetConnectionRequest create() => GetConnectionRequest._();
  @$core.override
  GetConnectionRequest createEmptyInstance() => create();
  static $pb.PbList<GetConnectionRequest> createRepeated() =>
      $pb.PbList<GetConnectionRequest>();
  @$core.pragma('dart2js:noInline')
  static GetConnectionRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetConnectionRequest>(create);
  static GetConnectionRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get connectionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set connectionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasConnectionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearConnectionId() => $_clearField(1);
}

class RevokeConnectionRequest extends $pb.GeneratedMessage {
  factory RevokeConnectionRequest({
    $core.String? connectionId,
  }) {
    final result = create();
    if (connectionId != null) result.connectionId = connectionId;
    return result;
  }

  RevokeConnectionRequest._();

  factory RevokeConnectionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RevokeConnectionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RevokeConnectionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'connectionId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RevokeConnectionRequest clone() =>
      RevokeConnectionRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RevokeConnectionRequest copyWith(
          void Function(RevokeConnectionRequest) updates) =>
      super.copyWith((message) => updates(message as RevokeConnectionRequest))
          as RevokeConnectionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RevokeConnectionRequest create() => RevokeConnectionRequest._();
  @$core.override
  RevokeConnectionRequest createEmptyInstance() => create();
  static $pb.PbList<RevokeConnectionRequest> createRepeated() =>
      $pb.PbList<RevokeConnectionRequest>();
  @$core.pragma('dart2js:noInline')
  static RevokeConnectionRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RevokeConnectionRequest>(create);
  static RevokeConnectionRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get connectionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set connectionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasConnectionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearConnectionId() => $_clearField(1);
}

class DeleteConnectionRequest extends $pb.GeneratedMessage {
  factory DeleteConnectionRequest({
    $core.String? connectionId,
  }) {
    final result = create();
    if (connectionId != null) result.connectionId = connectionId;
    return result;
  }

  DeleteConnectionRequest._();

  factory DeleteConnectionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DeleteConnectionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DeleteConnectionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'connectionId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteConnectionRequest clone() =>
      DeleteConnectionRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteConnectionRequest copyWith(
          void Function(DeleteConnectionRequest) updates) =>
      super.copyWith((message) => updates(message as DeleteConnectionRequest))
          as DeleteConnectionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DeleteConnectionRequest create() => DeleteConnectionRequest._();
  @$core.override
  DeleteConnectionRequest createEmptyInstance() => create();
  static $pb.PbList<DeleteConnectionRequest> createRepeated() =>
      $pb.PbList<DeleteConnectionRequest>();
  @$core.pragma('dart2js:noInline')
  static DeleteConnectionRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DeleteConnectionRequest>(create);
  static DeleteConnectionRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get connectionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set connectionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasConnectionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearConnectionId() => $_clearField(1);
}

class DeleteConnectionResponse extends $pb.GeneratedMessage {
  factory DeleteConnectionResponse() => create();

  DeleteConnectionResponse._();

  factory DeleteConnectionResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DeleteConnectionResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DeleteConnectionResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteConnectionResponse clone() =>
      DeleteConnectionResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteConnectionResponse copyWith(
          void Function(DeleteConnectionResponse) updates) =>
      super.copyWith((message) => updates(message as DeleteConnectionResponse))
          as DeleteConnectionResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DeleteConnectionResponse create() => DeleteConnectionResponse._();
  @$core.override
  DeleteConnectionResponse createEmptyInstance() => create();
  static $pb.PbList<DeleteConnectionResponse> createRepeated() =>
      $pb.PbList<DeleteConnectionResponse>();
  @$core.pragma('dart2js:noInline')
  static DeleteConnectionResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DeleteConnectionResponse>(create);
  static DeleteConnectionResponse? _defaultInstance;
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
