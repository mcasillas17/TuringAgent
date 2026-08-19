/// Third-party accounts the user has connected, and the catalogue of what can
/// be connected at all.
///
/// No type in this file has a field for a credential, and none should ever
/// grow one. The secret travels exactly once — from the connect form to the
/// backend — and is never read back.
library;

enum IntegrationProviderKind {
  imap,
  caldav,
  notion,
  github,
  googleWorkspace,
  microsoft365,
  slack,

  /// A provider this build does not know, sent by a newer backend. Rendered
  /// as unknown rather than guessed at.
  unknown,
}

enum IntegrationConnectionState {
  connected,
  revoked,

  /// A state this build does not know. Never treated as connected: claiming
  /// an account still has access when we cannot tell is the wrong answer.
  unknown,
}

/// The catalogue, plus whether this backend can store a credential at all.
class IntegrationCatalogue {
  const IntegrationCatalogue({
    required this.providers,
    required this.storageConfigured,
    this.storageUnconfiguredReason = '',
  });

  final List<IntegrationProviderInfo> providers;

  /// False when the backend has no TURING_INTEGRATION_KEY. Sent with the
  /// catalogue so the client can say so before asking anyone to paste a live
  /// app password into a form that cannot work.
  final bool storageConfigured;
  final String storageUnconfiguredReason;

  List<IntegrationProviderInfo> get connectable =>
      providers.where((provider) => provider.supported).toList();

  List<IntegrationProviderInfo> get refused =>
      providers.where((provider) => !provider.supported).toList();
}

/// What a provider is, what it takes, and what holding its credential allows.
class IntegrationProviderInfo {
  const IntegrationProviderInfo({
    required this.kind,
    required this.displayName,
    required this.category,
    required this.supported,
    this.unsupportedReason = '',
    this.secretLabel = '',
    this.secretHelp = '',
    this.accountLabel = '',
    this.requiresEndpoint = false,
    this.endpointLabel = '',
    this.grants = const [],
  });

  final IntegrationProviderKind kind;
  final String displayName;
  final String category;

  /// False for providers that only issue credentials through OAuth. They are
  /// listed anyway, with [unsupportedReason], so a missing provider reads as
  /// a stated limitation rather than an oversight.
  final bool supported;
  final String unsupportedReason;

  final String secretLabel;
  final String secretHelp;
  final String accountLabel;
  final bool requiresEndpoint;
  final String endpointLabel;

  /// Exactly what the credential allows, in the user's terms. Shown in full
  /// before connecting; none of it is optional, so none of it is a checkbox.
  final List<String> grants;
}

/// A connected account. A standing grant of access until it is revoked.
class IntegrationConnection {
  const IntegrationConnection({
    required this.connectionId,
    required this.provider,
    required this.displayName,
    required this.state,
    this.accountLabel = '',
    this.endpoint = '',
    this.credentialHint = '',
    this.grantedScopes = const [],
    this.connectedAt,
    this.revokedAt,
    this.credentialUnreadable = false,
  });

  final String connectionId;
  final IntegrationProviderKind provider;
  final String displayName;
  final IntegrationConnectionState state;
  final String accountLabel;
  final String endpoint;

  /// A redaction — bullets and at most four trailing characters. The backend
  /// never sends the credential itself.
  final String credentialHint;

  /// What the user agreed this connection allows, captured when they
  /// connected it.
  final List<String> grantedScopes;

  /// True when the key that sealed this credential is gone — rotated, lost,
  /// or restored from a different .env. The connection can never be used
  /// again and has to be reconnected. Shown rather than hidden: a card that
  /// kept saying "Connected" would be claiming access the app does not have.
  final bool credentialUnreadable;

  final DateTime? connectedAt;
  final DateTime? revokedAt;

  bool get isConnected => state == IntegrationConnectionState.connected;
}
