import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/workspace/integrations_page.dart';
import 'package:turing_flutter_app/models/agent_descriptor.dart';
import 'package:turing_flutter_app/models/integration.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/session_deletion.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';

import '../support/no_audit_api.dart';
import '../support/no_mcp_registry_api.dart';
import '../support/no_memory_api.dart';
import '../support/no_skills_api.dart';
import '../support/no_external_agents_api.dart';
import '../support/no_automations_api.dart';
import '../support/no_remote_egress_api.dart';
import '../support/no_session_lifecycle_api.dart';
import '../support/no_telemetry_api.dart';

/// What the user types into the credential field. Nothing on screen may ever
/// contain it after the form is submitted.
const _secret = 'shibboleth-app-password-8f31c2';

const _desktop = Size(1400, 900);
const _phone = Size(390, 780);

void main() {
  group('what the page says before anything is connected', () {
    testWidgets('an empty list says so and explains the new egress gate', (
      tester,
    ) async {
      await _pumpPage(tester, _IntegrationsApi());

      expect(find.text('No accounts connected'), findsOneWidget);
      expect(
        find.text('Connected-account tools ask before every call'),
        findsOneWidget,
      );
      expect(
        find.textContaining('per-run consent'),
        findsOneWidget,
        reason: 'the page names the egress consent consequence up front',
      );
    });

    testWidgets('a provider that cannot be connected says why, in place', (
      tester,
    ) async {
      await _pumpPage(tester, _IntegrationsApi());

      expect(find.text('Not supported'), findsOneWidget);
      expect(find.text('Google (Gmail, Calendar, Drive)'), findsOneWidget);
      expect(
        find.textContaining('only issue credentials through OAuth'),
        findsOneWidget,
      );
    });

    testWidgets('a backend failure offers a retry rather than an empty page', (
      tester,
    ) async {
      final api = _IntegrationsApi()..listError = StateError('backend down');
      await _pumpPage(tester, api);

      expect(find.text('Could not reach the backend'), findsOneWidget);
      // "No accounts connected" here would be a confident statement about
      // state we just failed to read.
      expect(find.text('No accounts connected'), findsNothing);

      api.listError = null;
      await tester.tap(find.text('Try again'));
      await tester.pumpAndSettle();

      expect(find.text('No accounts connected'), findsOneWidget);
    });
  });

  group('connecting an account', () {
    testWidgets(
      'GitHub connect explains tool approval and egress before consent',
      (tester) async {
        await _pumpPage(tester, _IntegrationsApi());
        await tester.tap(find.text('Connect an account'));
        await tester.pumpAndSettle();
        await tester.tap(find.widgetWithText(ChoiceChip, 'GitHub'));
        await tester.pumpAndSettle();

        expect(
          find.textContaining('GitHub tools become available'),
          findsOneWidget,
        );
        expect(find.textContaining('local chat sends'), findsOneWidget);
      },
    );

    testWidgets('the grants are shown before the button can be pressed', (
      tester,
    ) async {
      final api = _IntegrationsApi();
      await _pumpPage(tester, api);

      await tester.tap(find.text('Connect an account'));
      await tester.pumpAndSettle();

      expect(find.text('What this will allow'), findsOneWidget);
      expect(find.textContaining('Read every message'), findsOneWidget);
      // Unticked to begin with. A pre-ticked consent box is consent nobody
      // gave.
      final checkbox = tester.widget<Checkbox>(find.byType(Checkbox));
      expect(checkbox.value, isFalse);
      expect(_connectButton(tester).onPressed, isNull);
    });

    testWidgets('without consent the credential is never sent', (tester) async {
      final api = _IntegrationsApi();
      await _pumpPage(tester, api);
      await tester.tap(find.text('Connect an account'));
      await tester.pumpAndSettle();

      await _fillForm(tester);
      // Consent left untouched.
      await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
      await tester.pumpAndSettle();

      expect(api.connectCalls, isEmpty);
      expect(find.text('Connect an account'), findsWidgets);
    });

    testWidgets('consenting then connecting stores it and shows the account', (
      tester,
    ) async {
      final api = _IntegrationsApi();
      await _pumpPage(tester, api);
      await tester.tap(find.text('Connect an account'));
      await tester.pumpAndSettle();

      await _fillForm(tester);
      await tester.tap(find.byType(Checkbox));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
      await tester.pumpAndSettle();

      expect(api.connectCalls, hasLength(1));
      final call = api.connectCalls.single;
      expect(call.provider, IntegrationProviderKind.imap);
      expect(call.displayName, 'Personal mail');
      expect(call.endpoint, 'imap.example.com');
      expect(call.credential, _secret);
      expect(
        call.consentAcknowledged,
        isTrue,
        reason: 'the backend refuses without it, so the client must send it',
      );
      expect(find.text('Personal mail'), findsOneWidget);
    });

    testWidgets('the credential never appears on screen afterwards', (
      tester,
    ) async {
      final api = _IntegrationsApi();
      await _pumpPage(tester, api);
      await tester.tap(find.text('Connect an account'));
      await tester.pumpAndSettle();
      await _fillForm(tester);
      await tester.tap(find.byType(Checkbox));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
      await tester.pumpAndSettle();

      expect(find.textContaining(_secret), findsNothing);
      expect(
        find.textContaining('8f31c2'),
        findsNothing,
        reason: 'not even the tail the backend redacts to',
      );
      // What is shown is the backend's redaction.
      expect(find.textContaining('••••'), findsOneWidget);
    });

    testWidgets('switching provider drops a consent given for the other one', (
      tester,
    ) async {
      await _pumpPage(tester, _IntegrationsApi());
      await tester.tap(find.text('Connect an account'));
      await tester.pumpAndSettle();
      await tester.tap(find.byType(Checkbox));
      await tester.pumpAndSettle();
      expect(_connectButton(tester).onPressed, isNotNull);

      await tester.tap(find.widgetWithText(ChoiceChip, 'Notion'));
      await tester.pumpAndSettle();

      // The grants on screen are Notion's now; a tick carried over would
      // record agreement to something never shown.
      expect(_connectButton(tester).onPressed, isNull);
      expect(
        find.textContaining('shared with this integration'),
        findsOneWidget,
      );
    });

    testWidgets('switching provider does not carry the old fields over', (
      tester,
    ) async {
      final api = _IntegrationsApi();
      await _pumpPage(tester, api);
      await tester.tap(find.text('Connect an account'));
      await tester.pumpAndSettle();
      await _fillForm(tester);

      await tester.tap(find.widgetWithText(ChoiceChip, 'Notion'));
      await tester.pumpAndSettle();
      // The name is the user's label for the connection, not the provider's,
      // so it stays. Everything provider-specific goes.
      await tester.enterText(
        _fieldFor(tester, 'Internal integration token'),
        'notion-token-abcdef',
      );
      await tester.tap(find.byType(Checkbox));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
      await tester.pumpAndSettle();

      final call = api.connectCalls.single;
      expect(call.provider, IntegrationProviderKind.notion);
      expect(
        call.endpoint,
        isEmpty,
        reason: "a hidden field's value must not be submitted",
      );
      expect(
        call.credential,
        'notion-token-abcdef',
        reason: 'the token typed for the other provider must not be sent here',
      );
      expect(call.accountLabel, isEmpty);
    });

    testWidgets('a server field appears only where one is needed', (
      tester,
    ) async {
      await _pumpPage(tester, _IntegrationsApi());
      await tester.tap(find.text('Connect an account'));
      await tester.pumpAndSettle();

      expect(
        find.text('IMAP server (for example imap.example.com)'),
        findsOneWidget,
      );

      await tester.tap(find.widgetWithText(ChoiceChip, 'Notion'));
      await tester.pumpAndSettle();

      expect(
        find.text('IMAP server (for example imap.example.com)'),
        findsNothing,
      );
    });

    testWidgets('a refused connect keeps the form and shows the reason', (
      tester,
    ) async {
      final api = _IntegrationsApi()..connectError = StateError('key missing');
      await _pumpPage(tester, api);
      await tester.tap(find.text('Connect an account'));
      await tester.pumpAndSettle();
      await _fillForm(tester);
      await tester.tap(find.byType(Checkbox));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
      await tester.pumpAndSettle();

      // Still open, with what was typed intact and the failure beside it.
      expect(find.textContaining('key missing'), findsOneWidget);
      expect(find.widgetWithText(FilledButton, 'Connect'), findsOneWidget);
    });

    testWidgets('a missing name is caught before a round trip', (tester) async {
      final api = _IntegrationsApi();
      await _pumpPage(tester, api);
      await tester.tap(find.text('Connect an account'));
      await tester.pumpAndSettle();
      await tester.enterText(_fieldFor(tester, 'App password'), _secret);
      await tester.tap(find.byType(Checkbox));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
      await tester.pumpAndSettle();

      expect(api.connectCalls, isEmpty);
      expect(find.textContaining('name you will recognise'), findsOneWidget);
    });
  });

  group('a connected account', () {
    testWidgets('GitHub tools expose a live policy editor', (tester) async {
      final api = _IntegrationsApi()
        ..connections.add(_connectedGitHub())
        ..tools.add(
          const ToolDescriptor(
            serverName: 'integrations',
            toolName: 'github.list_issues',
            policy: ToolPolicy.approvalRequired,
          ),
        );
      await _pumpPage(tester, api);

      expect(find.text('Agent tools'), findsOneWidget);
      expect(find.text('github.list_issues'), findsOneWidget);
      expect(find.text('Asks first'), findsOneWidget);

      await tester.tap(find.byType(DropdownButton<ToolPolicy>));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Disabled').last);
      await tester.pumpAndSettle();

      expect(api.policyUpdates, ['integrations/github.list_issues:disabled']);
      expect(find.text('Disabled'), findsOneWidget);
    });

    testWidgets('shows what it allows and how it is identified', (
      tester,
    ) async {
      final api = _IntegrationsApi()..connections.add(_connected());
      await _pumpPage(tester, api);

      expect(find.text('Personal mail'), findsOneWidget);
      expect(find.text('Connected'), findsOneWidget);
      expect(find.text('me@example.com'), findsOneWidget);
      expect(find.text('••••••••c2f1'), findsOneWidget);
      expect(find.text('What this allows'), findsOneWidget);
      expect(find.textContaining('Read every message'), findsOneWidget);
      expect(find.text('2026-05-10'), findsOneWidget);
    });

    testWidgets('revoking says what it does and what it cannot do', (
      tester,
    ) async {
      final api = _IntegrationsApi()..connections.add(_connected());
      await _pumpPage(tester, api);

      await tester.tap(find.text('Revoke access'));
      await tester.pumpAndSettle();

      expect(find.textContaining('destroys the credential'), findsOneWidget);
      // The limit of what revoking here can achieve, stated rather than
      // implied.
      expect(
        find.textContaining('only the provider can do that'),
        findsOneWidget,
      );

      await tester.tap(find.widgetWithText(TextButton, 'Revoke'));
      await tester.pumpAndSettle();

      expect(api.revoked, ['conn_1']);
      expect(find.text('Revoked'), findsOneWidget);
      expect(find.text('Destroyed when you revoked it'), findsOneWidget);
      // Nothing left to revoke.
      expect(find.text('Revoke access'), findsNothing);
    });

    testWidgets('cancelling the revoke dialog changes nothing', (tester) async {
      final api = _IntegrationsApi()..connections.add(_connected());
      await _pumpPage(tester, api);

      await tester.tap(find.text('Revoke access'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(TextButton, 'Cancel'));
      await tester.pumpAndSettle();

      expect(api.revoked, isEmpty);
      expect(find.text('Connected'), findsOneWidget);
    });

    testWidgets('a failed revoke says so instead of appearing to work', (
      tester,
    ) async {
      final api = _IntegrationsApi()
        ..connections.add(_connected())
        ..revokeError = StateError('backend down');
      await _pumpPage(tester, api);

      await tester.tap(find.text('Revoke access'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(TextButton, 'Revoke'));
      await tester.pumpAndSettle();

      expect(find.textContaining('Could not revoke it'), findsOneWidget);
      // The card still says connected, because it still is.
      expect(find.text('Connected'), findsOneWidget);
    });

    testWidgets('removing deletes the record after confirming', (tester) async {
      final api = _IntegrationsApi()..connections.add(_connected());
      await _pumpPage(tester, api);

      await tester.tap(find.text('Remove'));
      await tester.pumpAndSettle();
      expect(
        find.textContaining('deletes the connection and its history'),
        findsOneWidget,
      );
      // Scoped to the dialog: the card's own "Remove" is still mounted
      // behind it, so an unscoped finder matches two buttons and which one
      // wins is a matter of timing. It passed locally and failed in CI.
      await tester.tap(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.widgetWithText(TextButton, 'Remove'),
        ),
      );
      await tester.pumpAndSettle();

      expect(api.deleted, ['conn_1']);
      expect(find.text('No accounts connected'), findsOneWidget);
    });

    testWidgets('a revoked account keeps its history, not its credential', (
      tester,
    ) async {
      final api = _IntegrationsApi()..connections.add(_revoked());
      await _pumpPage(tester, api);

      expect(find.text('Revoked'), findsOneWidget);
      expect(find.text('Destroyed when you revoked it'), findsOneWidget);
      expect(
        find.text('What this allowed, until you revoked it'),
        findsOneWidget,
      );
      expect(find.text('2026-05-11'), findsOneWidget);
    });

    testWidgets('a state this build does not know is not called connected', (
      tester,
    ) async {
      final api = _IntegrationsApi()
        ..connections.add(
          const IntegrationConnection(
            connectionId: 'conn_x',
            provider: IntegrationProviderKind.unknown,
            displayName: 'From the future',
            state: IntegrationConnectionState.unknown,
          ),
        );
      await _pumpPage(tester, api);

      expect(find.text('Unknown state'), findsOneWidget);
      expect(find.text('Connected'), findsNothing);
      // "Destroyed when you revoked it" would be a definite claim about a
      // state we have just admitted we cannot read.
      expect(find.text('Unknown'), findsOneWidget);
      expect(find.text('Destroyed when you revoked it'), findsNothing);
      expect(
        find.text('Revoke access'),
        findsNothing,
        reason: 'offering to revoke what we cannot describe would be a guess',
      );
    });
  });

  group('when the backend cannot store a credential', () {
    testWidgets('the page says so before offering the form', (tester) async {
      final api = _IntegrationsApi()..storageConfigured = false;
      await _pumpPage(tester, api);

      expect(find.text('Nothing can be connected yet'), findsOneWidget);
      expect(
        find.textContaining('TURING_INTEGRATION_KEY'),
        findsOneWidget,
        reason: 'the reason names what to fix',
      );
      // Disabled, not failing on submit: nobody should paste a live app
      // password into a form that cannot store it.
      expect(_connectAccountButton(tester).onPressed, isNull);
    });

    testWidgets('the catalogue is still shown, refusals included', (
      tester,
    ) async {
      final api = _IntegrationsApi()..storageConfigured = false;
      await _pumpPage(tester, api);

      // What could be connected in principle does not depend on whether this
      // machine is set up yet.
      expect(find.text('Not supported'), findsOneWidget);
      expect(find.text('Google (Gmail, Calendar, Drive)'), findsOneWidget);
    });
  });

  group('a credential whose key is gone', () {
    testWidgets('says it cannot be used instead of showing a hint', (
      tester,
    ) async {
      final api = _IntegrationsApi()
        ..connections.add(
          IntegrationConnection(
            connectionId: 'conn_9',
            provider: IntegrationProviderKind.imap,
            displayName: 'Personal mail',
            state: IntegrationConnectionState.connected,
            credentialHint: '••••••••c2f1',
            credentialUnreadable: true,
            grantedScopes: const ['Read every message.'],
            connectedAt: DateTime.utc(2026, 5, 10, 12),
          ),
        );
      await _pumpPage(tester, api);

      expect(
        find.text('Sealed with a key this machine no longer has'),
        findsOneWidget,
      );
      expect(
        find.text('••••••••c2f1'),
        findsNothing,
        reason: 'a hint beside a dead credential reads as a working one',
      );
      expect(
        find.textContaining('connect the account again'),
        findsOneWidget,
        reason: 'the user is told what to do about it',
      );
      // Still revocable: the record and the row are still there.
      expect(find.text('Revoke access'), findsOneWidget);
    });
  });

  group('the page fits a phone', () {
    for (final size in const [_phone, Size(320, 640), Size(300, 400)]) {
      testWidgets('no overflow at ${size.width}x${size.height}', (
        tester,
      ) async {
        final api = _IntegrationsApi()..connections.add(_connected());
        await _pumpPage(tester, api, size: size);

        expect(tester.takeException(), isNull);
      });
    }

    testWidgets('the GitHub policy editor wraps without overflow', (
      tester,
    ) async {
      final api = _IntegrationsApi()
        ..connections.add(_connectedGitHub())
        ..tools.add(
          const ToolDescriptor(
            serverName: 'integrations',
            toolName: 'github.create_comment',
            policy: ToolPolicy.approvalRequired,
          ),
        );
      await _pumpPage(tester, api, size: const Size(320, 640));

      expect(tester.takeException(), isNull);
      expect(find.text('github.create_comment'), findsOneWidget);
    });

    testWidgets('the connect dialog opens on a phone without overflowing', (
      tester,
    ) async {
      await _pumpPage(tester, _IntegrationsApi(), size: _phone);

      await tester.tap(find.text('Connect an account'));
      await tester.pumpAndSettle();

      expect(tester.takeException(), isNull);
      expect(find.text('What this will allow'), findsOneWidget);
      expect(_connectButton(tester).onPressed, isNull);
    });
  });
}

Future<void> _pumpPage(
  WidgetTester tester,
  _IntegrationsApi api, {
  Size size = _desktop,
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);

  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(body: IntegrationsPage(apiClient: api)),
    ),
  );
  await tester.pumpAndSettle();
}

/// Fills the connect form. Fields are addressed by their label so the test
/// breaks if a field is renamed out from under the user.
Future<void> _fillForm(WidgetTester tester) async {
  await tester.enterText(_fieldFor(tester, 'Name'), 'Personal mail');
  await tester.enterText(_fieldFor(tester, 'Email address'), 'me@example.com');
  await tester.enterText(
    _fieldFor(tester, 'IMAP server (for example imap.example.com)'),
    'imap.example.com',
  );
  await tester.enterText(_fieldFor(tester, 'App password'), _secret);
  await tester.pumpAndSettle();
}

Finder _fieldFor(WidgetTester tester, String label) =>
    find.ancestor(of: find.text(label), matching: find.byType(TextField)).first;

FilledButton _connectButton(WidgetTester tester) =>
    tester.widget<FilledButton>(find.widgetWithText(FilledButton, 'Connect'));

/// The page's button is a `FilledButton.icon`, which is a private subclass —
/// `find.byType` matches the exact runtime type and would miss it.
FilledButton _connectAccountButton(WidgetTester tester) =>
    tester.widget<FilledButton>(
      find.ancestor(
        of: find.text('Connect an account'),
        matching: find.byWidgetPredicate((widget) => widget is FilledButton),
      ),
    );

IntegrationConnection _connected() => IntegrationConnection(
  connectionId: 'conn_1',
  provider: IntegrationProviderKind.imap,
  displayName: 'Personal mail',
  state: IntegrationConnectionState.connected,
  accountLabel: 'me@example.com',
  endpoint: 'imap.example.com',
  credentialHint: '••••••••c2f1',
  grantedScopes: const ['Read every message in every mailbox.'],
  connectedAt: DateTime.utc(2026, 5, 10, 12),
);

IntegrationConnection _revoked() => IntegrationConnection(
  connectionId: 'conn_2',
  provider: IntegrationProviderKind.notion,
  displayName: 'Work notes',
  state: IntegrationConnectionState.revoked,
  accountLabel: 'Acme',
  grantedScopes: const ['Read every page shared with the integration.'],
  connectedAt: DateTime.utc(2026, 5, 10, 12),
  revokedAt: DateTime.utc(2026, 5, 11, 12),
);

IntegrationConnection _connectedGitHub() => IntegrationConnection(
  connectionId: 'conn_github',
  provider: IntegrationProviderKind.github,
  displayName: 'Personal GitHub',
  state: IntegrationConnectionState.connected,
  accountLabel: 'octocat',
  credentialHint: '••••••••c2f1',
  grantedScopes: const ['Read repository data.', 'Create issue comments.'],
  connectedAt: DateTime.utc(2026, 5, 10, 12),
);

class _ConnectCall {
  const _ConnectCall({
    required this.provider,
    required this.displayName,
    required this.accountLabel,
    required this.endpoint,
    required this.credential,
    required this.consentAcknowledged,
  });

  final IntegrationProviderKind provider;
  final String displayName;
  final String accountLabel;
  final String endpoint;
  final String credential;
  final bool consentAcknowledged;
}

/// A working in-memory backend, so the UI is tested against something that
/// behaves like the real one rather than a stub that always says yes.
class _IntegrationsApi
    with
        NoAuditApi,
        NoMcpRegistryApi,
        NoMemoryApi,
        NoSkillsApi,
        NoExternalAgentsApi,
        NoAutomationsApi,
        NoRemoteEgressApi,
        NoSessionLifecycleApi,
        NoTelemetryApi
    implements TuringApi, PseudoServerPolicyApi {
  final List<IntegrationConnection> connections = [];
  final List<ToolDescriptor> tools = [];
  final List<_ConnectCall> connectCalls = [];
  final List<String> revoked = [];
  final List<String> deleted = [];
  final List<String> policyUpdates = [];
  Object? listError;
  Object? connectError;
  Object? revokeError;
  int nextId = 10;

  /// Mirrors the backend: with no TURING_INTEGRATION_KEY there is nothing to
  /// seal a credential with, and the catalogue says so.
  bool storageConfigured = true;

  @override
  Future<IntegrationCatalogue> listIntegrationProviders() async {
    final error = listError;
    if (error != null) throw error;
    return IntegrationCatalogue(
      storageConfigured: storageConfigured,
      storageUnconfiguredReason: storageConfigured
          ? ''
          : 'integrations are not configured: set TURING_INTEGRATION_KEY in '
                'turing-backend/.env',
      providers: const [
        IntegrationProviderInfo(
          kind: IntegrationProviderKind.imap,
          displayName: 'Mail (IMAP)',
          category: 'Mail',
          supported: true,
          secretLabel: 'App password',
          secretHelp: 'Create an app password in your provider settings.',
          accountLabel: 'Email address',
          requiresEndpoint: true,
          endpointLabel: 'IMAP server (for example imap.example.com)',
          grants: [
            'Read every message in every mailbox on this account.',
            'Move, flag and delete messages.',
          ],
        ),
        IntegrationProviderInfo(
          kind: IntegrationProviderKind.notion,
          displayName: 'Notion',
          category: 'Notes',
          supported: true,
          secretLabel: 'Internal integration token',
          accountLabel: 'Workspace name',
          grants: ['Read every page shared with this integration.'],
        ),
        IntegrationProviderInfo(
          kind: IntegrationProviderKind.github,
          displayName: 'GitHub',
          category: 'Code',
          supported: true,
          secretLabel: 'Personal access token',
          accountLabel: 'GitHub account',
          grants: ['Read repository data.', 'Create issue comments.'],
        ),
        IntegrationProviderInfo(
          kind: IntegrationProviderKind.googleWorkspace,
          displayName: 'Google (Gmail, Calendar, Drive)',
          category: 'Mail',
          supported: false,
          unsupportedReason:
              "Google's APIs only issue credentials through OAuth, which "
              'needs a registered client and a browser redirect.',
        ),
      ],
    );
  }

  @override
  Future<List<IntegrationConnection>> listConnections() async {
    final error = listError;
    if (error != null) throw error;
    return List.unmodifiable(connections);
  }

  @override
  Future<IntegrationConnection> connectAccount({
    required IntegrationProviderKind provider,
    required String displayName,
    required String credential,
    required bool consentAcknowledged,
    String accountLabel = '',
    String endpoint = '',
  }) async {
    final error = connectError;
    if (error != null) throw error;
    // The real backend refuses this outright; the fake refuses it too so a
    // client that stopped sending consent could not pass these tests.
    if (!consentAcknowledged) {
      throw StateError(
        'connecting an account requires agreeing to what it '
        'grants',
      );
    }
    connectCalls.add(
      _ConnectCall(
        provider: provider,
        displayName: displayName,
        accountLabel: accountLabel,
        endpoint: endpoint,
        credential: credential,
        consentAcknowledged: consentAcknowledged,
      ),
    );
    final connection = IntegrationConnection(
      connectionId: 'conn_${nextId++}',
      provider: provider,
      displayName: displayName,
      state: IntegrationConnectionState.connected,
      accountLabel: accountLabel,
      endpoint: endpoint,
      // Redacted by the backend, exactly as the real one does.
      credentialHint: '••••••••wxyz',
      grantedScopes: const ['Read every message in every mailbox.'],
      connectedAt: DateTime.utc(2026, 5, 12, 9),
    );
    connections.add(connection);
    return connection;
  }

  @override
  Future<IntegrationConnection> revokeConnection({
    required String connectionId,
  }) async {
    final error = revokeError;
    if (error != null) throw error;
    revoked.add(connectionId);
    final index = connections.indexWhere(
      (connection) => connection.connectionId == connectionId,
    );
    final existing = connections[index];
    final replacement = IntegrationConnection(
      connectionId: existing.connectionId,
      provider: existing.provider,
      displayName: existing.displayName,
      state: IntegrationConnectionState.revoked,
      accountLabel: existing.accountLabel,
      endpoint: existing.endpoint,
      grantedScopes: existing.grantedScopes,
      connectedAt: existing.connectedAt,
      revokedAt: DateTime.utc(2026, 5, 13, 9),
    );
    connections[index] = replacement;
    return replacement;
  }

  @override
  Future<void> deleteConnection({required String connectionId}) async {
    deleted.add(connectionId);
    connections.removeWhere(
      (connection) => connection.connectionId == connectionId,
    );
  }

  @override
  Future<List<ToolDescriptor>> listPseudoServerTools({
    required String serverName,
  }) async {
    expect(serverName, 'integrations');
    return List.unmodifiable(tools);
  }

  @override
  Future<ToolDescriptor> updateToolPolicyByName({
    required String serverName,
    required String toolName,
    required ToolPolicy policy,
  }) async {
    final index = tools.indexWhere((tool) => tool.toolName == toolName);
    final updated = ToolDescriptor(
      serverName: serverName,
      toolName: toolName,
      policy: policy,
      enabled: policy != ToolPolicy.disabled,
    );
    tools[index] = updated;
    policyUpdates.add('$serverName/$toolName:${policy.name}');
    return updated;
  }

  // Nothing below is exercised by these tests.
  @override
  Future<List<ToolDescriptor>> listTools() async => const [];

  @override
  Future<List<AgentDescriptor>> listAgents() async => const [];

  @override
  Future<Map<String, dynamic>> getConfig() async => const {};

  @override
  Future<Map<String, dynamic>> createSession({String? title}) async => const {};

  @override
  Future<List<Session>> listSessions({int limit = 50, String? after}) async =>
      const [];

  @override
  Future<Session> getSession({required String sessionId}) async =>
      throw UnimplementedError();

  @override
  Future<SessionDeletionReceipt> deleteSession({
    required String sessionId,
  }) async => const SessionDeletionReceipt.completed();

  @override
  Future<List<SessionDeletionReceipt>> listSessionDeletionReceipts() async =>
      const [];

  @override
  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  }) async => const [];

  @override
  Future<List<SearchHit>> searchMessages({
    required String query,
    int limit = 50,
  }) async => const [];

  @override
  Future<TuringEventPage> listEvents({
    required String sessionId,
    int? after,
    int limit = 500,
  }) async => const TuringEventPage(events: [], latestSequence: 0);

  @override
  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    String? idempotencyKey,
  }) async => const {};

  @override
  Future<Map<String, dynamic>> approveApproval(
    String approvalId, {
    String? comment,
  }) async => const {};

  @override
  Future<Map<String, dynamic>> denyApproval(
    String approvalId, {
    String? reason,
  }) async => const {};
}
