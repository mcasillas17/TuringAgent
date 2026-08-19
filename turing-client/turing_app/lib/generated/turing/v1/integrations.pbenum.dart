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

/// A third-party account the user has connected: mail, calendar, notes, code.
///
/// The connection model here is deliberately narrow. Turing has no registered
/// OAuth client with any provider, no redirect URI and no browser round trip,
/// so an authorization-code flow is not something it can honestly offer. What
/// it can do is hold a credential the user created themselves — an app
/// password, an internal integration token, a personal access token — which is
/// how these services are reached by scripts and self-hosted clients every
/// day. Providers that only issue credentials through OAuth are listed as
/// unsupported, with the reason, rather than shown as a button that cannot
/// work.
class IntegrationProvider extends $pb.ProtobufEnum {
  static const IntegrationProvider INTEGRATION_PROVIDER_UNSPECIFIED =
      IntegrationProvider._(
          0, _omitEnumNames ? '' : 'INTEGRATION_PROVIDER_UNSPECIFIED');

  /// Supported: the user pastes a credential they minted themselves.
  static const IntegrationProvider INTEGRATION_PROVIDER_IMAP =
      IntegrationProvider._(
          1, _omitEnumNames ? '' : 'INTEGRATION_PROVIDER_IMAP');
  static const IntegrationProvider INTEGRATION_PROVIDER_CALDAV =
      IntegrationProvider._(
          2, _omitEnumNames ? '' : 'INTEGRATION_PROVIDER_CALDAV');
  static const IntegrationProvider INTEGRATION_PROVIDER_NOTION =
      IntegrationProvider._(
          3, _omitEnumNames ? '' : 'INTEGRATION_PROVIDER_NOTION');
  static const IntegrationProvider INTEGRATION_PROVIDER_GITHUB =
      IntegrationProvider._(
          4, _omitEnumNames ? '' : 'INTEGRATION_PROVIDER_GITHUB');

  /// Named but not supported. They exist in this enum so the client can list
  /// them with a reason instead of leaving the user wondering whether Turing
  /// has heard of their mail provider.
  static const IntegrationProvider INTEGRATION_PROVIDER_GOOGLE_WORKSPACE =
      IntegrationProvider._(
          5, _omitEnumNames ? '' : 'INTEGRATION_PROVIDER_GOOGLE_WORKSPACE');
  static const IntegrationProvider INTEGRATION_PROVIDER_MICROSOFT_365 =
      IntegrationProvider._(
          6, _omitEnumNames ? '' : 'INTEGRATION_PROVIDER_MICROSOFT_365');
  static const IntegrationProvider INTEGRATION_PROVIDER_SLACK =
      IntegrationProvider._(
          7, _omitEnumNames ? '' : 'INTEGRATION_PROVIDER_SLACK');

  static const $core.List<IntegrationProvider> values = <IntegrationProvider>[
    INTEGRATION_PROVIDER_UNSPECIFIED,
    INTEGRATION_PROVIDER_IMAP,
    INTEGRATION_PROVIDER_CALDAV,
    INTEGRATION_PROVIDER_NOTION,
    INTEGRATION_PROVIDER_GITHUB,
    INTEGRATION_PROVIDER_GOOGLE_WORKSPACE,
    INTEGRATION_PROVIDER_MICROSOFT_365,
    INTEGRATION_PROVIDER_SLACK,
  ];

  static final $core.List<IntegrationProvider?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static IntegrationProvider? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const IntegrationProvider._(super.value, super.name);
}

class ConnectionStatus extends $pb.ProtobufEnum {
  static const ConnectionStatus CONNECTION_STATUS_UNSPECIFIED =
      ConnectionStatus._(
          0, _omitEnumNames ? '' : 'CONNECTION_STATUS_UNSPECIFIED');
  static const ConnectionStatus CONNECTION_STATUS_CONNECTED =
      ConnectionStatus._(
          1, _omitEnumNames ? '' : 'CONNECTION_STATUS_CONNECTED');

  /// The credential has been destroyed. The row survives so the user can still
  /// see that the account was once connected and when access ended.
  static const ConnectionStatus CONNECTION_STATUS_REVOKED =
      ConnectionStatus._(2, _omitEnumNames ? '' : 'CONNECTION_STATUS_REVOKED');

  static const $core.List<ConnectionStatus> values = <ConnectionStatus>[
    CONNECTION_STATUS_UNSPECIFIED,
    CONNECTION_STATUS_CONNECTED,
    CONNECTION_STATUS_REVOKED,
  ];

  static final $core.List<ConnectionStatus?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static ConnectionStatus? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ConnectionStatus._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
