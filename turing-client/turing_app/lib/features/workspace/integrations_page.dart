import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../models/integration.dart';
import '../../models/tool_descriptor.dart';
import '../../networking/api_client.dart';
import 'workspace_pages.dart';

/// Saved third-party accounts and functional integration tools.
/// Unsupported providers stay visible with an explanation and no connect form.
/// Saved status and historical consent never imply tool availability.
class IntegrationsPage extends StatefulWidget {
  const IntegrationsPage({super.key, required this.apiClient});

  final TuringApi apiClient;

  @override
  State<IntegrationsPage> createState() => _IntegrationsPageState();
}

class _IntegrationsData {
  const _IntegrationsData({
    required this.catalogue,
    required this.connections,
    required this.tools,
  });

  final IntegrationCatalogue? catalogue;
  final List<IntegrationConnection> connections;
  final List<ToolDescriptor>? tools;
}

class _IntegrationsPageState extends State<IntegrationsPage> {
  late Future<_IntegrationsData> _data;

  @override
  void initState() {
    super.initState();
    _data = _load();
  }

  Future<_IntegrationsData> _load() async {
    // Management uses each row's stored metadata. Catalog and policy failures
    // must not hide accounts the user needs to revoke or remove; null marks
    // unavailable data and is rendered with an explicit retry notice.
    final policyApi = widget.apiClient is PseudoServerPolicyApi
        ? widget.apiClient as PseudoServerPolicyApi
        : null;
    final results = await Future.wait([
      widget.apiClient.listIntegrationProviders().then<IntegrationCatalogue?>(
        (value) => value,
        onError: (Object error) => null,
      ),
      widget.apiClient.listConnections(),
      (policyApi?.listPseudoServerTools(serverName: 'integrations') ??
              Future.value(const <ToolDescriptor>[]))
          .then<List<ToolDescriptor>?>(
            (value) => value,
            onError: (Object error) => null,
          ),
    ]);
    return _IntegrationsData(
      catalogue: results[0] as IntegrationCatalogue?,
      connections: results[1] as List<IntegrationConnection>,
      tools: results[2] as List<ToolDescriptor>?,
    );
  }

  void _reload() {
    setState(() {
      _data = _load();
    });
  }

  Future<void> _connect(List<IntegrationProviderInfo> providers) async {
    final connected = await showDialog<bool>(
      context: context,
      builder: (_) =>
          _ConnectDialog(apiClient: widget.apiClient, providers: providers),
    );
    if (connected == true) _reload();
  }

  Future<void> _revoke(IntegrationConnection connection) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text('Revoke "${connection.displayName}"?'),
        content: const Text(
          'This destroys the credential stored for this connection. '
          'The saved record and its consent history remain.\n\n'
          'It does not delete the app password or token at the provider — '
          'only the provider can do that. If the credential could have been '
          'copied, delete it there too.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('Revoke'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await _run(
      () => widget.apiClient.revokeConnection(
        connectionId: connection.connectionId,
      ),
      'Could not revoke it',
    );
  }

  Future<void> _remove(IntegrationConnection connection) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text('Remove "${connection.displayName}"?'),
        content: const Text(
          'This deletes the saved connection record and any credential still '
          'stored for it. Audit records remain.\n\n'
          'Local deletion does not revoke the token or app password at the '
          'provider, or any copies elsewhere. Delete it at the provider too '
          'if you want to end that access.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('Remove'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await _run(
      () => widget.apiClient.deleteConnection(
        connectionId: connection.connectionId,
      ),
      'Could not remove it',
    );
  }

  Future<void> _run(Future<void> Function() action, String failure) async {
    try {
      await action();
      if (!mounted) return;
      _reload();
    } catch (error) {
      if (!mounted) return;
      // Reloaded either way: after a failure the list on screen is a claim
      // about state we have just failed to change.
      _reload();
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('$failure: $error')));
    }
  }

  Future<void> _setPolicy(ToolDescriptor tool, ToolPolicy policy) async {
    final policyApi = widget.apiClient is PseudoServerPolicyApi
        ? widget.apiClient as PseudoServerPolicyApi
        : null;
    if (policyApi == null) return;
    await _run(() async {
      await policyApi.updateToolPolicyByName(
        serverName: 'integrations',
        toolName: tool.toolName,
        policy: policy,
      );
    }, 'Could not update the tool policy');
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return WorkspacePage(
      title: 'Integrations',
      subtitle:
          'GitHub has functional tools. Other providers remain listed with their '
          'limitations. A saved connection records a credential and consent; '
          'its stored status does not mean integration tools are available.',
      child: FutureBuilder<_IntegrationsData>(
        future: _data,
        builder: (context, snapshot) {
          if (snapshot.connectionState != ConnectionState.done) {
            return const WorkspaceLoading();
          }
          if (snapshot.hasError) {
            return WorkspaceNotice(
              icon: Icons.error_outline,
              title: 'Could not reach the backend',
              body: '${snapshot.error}',
              onRetry: _reload,
              tone: AppColors.danger,
            );
          }
          final data = snapshot.requireData;
          final catalogue = data.catalogue;
          final canConnect =
              catalogue != null &&
              catalogue.storageConfigured &&
              catalogue.connectable.isNotEmpty;
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              WorkspaceNotice(
                icon: Icons.shield_outlined,
                title: 'GitHub tools use approval and egress policies',
                body:
                    'GitHub tools can use a connected GitHub account. Reads '
                    'and writes default to “Asks first,” and a local-model '
                    'send asks for per-run consent while any tool is enabled.',
                tone: AppColors.warning,
              ),
              const SizedBox(height: 18),
              // Said before the button, not after the form: asking someone to
              // paste a live app password into something that cannot store it
              // is the worst moment to find out.
              if (catalogue == null) ...[
                WorkspaceNotice(
                  icon: Icons.error_outline,
                  title: 'Provider catalog unavailable',
                  body:
                      'New connections are unavailable until the catalog can be loaded. '
                      'Saved accounts remain visible for revoke or removal.',
                  onRetry: _reload,
                  tone: AppColors.warning,
                ),
                const SizedBox(height: 18),
              ] else if (!catalogue.storageConfigured) ...[
                WorkspaceNotice(
                  icon: Icons.key_off_outlined,
                  title: 'Nothing can be connected yet',
                  body: catalogue.storageUnconfiguredReason.isEmpty
                      ? 'The backend has no key to seal a credential with, so '
                            'connecting an account is refused rather than '
                            'storing one in the clear.'
                      : catalogue.storageUnconfiguredReason,
                  tone: AppColors.danger,
                ),
                const SizedBox(height: 18),
              ],
              if (data.tools == null) ...[
                WorkspaceNotice(
                  icon: Icons.error_outline,
                  title: 'Tool policies unavailable',
                  body:
                      'Tool policies could not be loaded. Saved accounts can still be managed.',
                  onRetry: _reload,
                  tone: AppColors.warning,
                ),
                const SizedBox(height: 18),
              ],
              Align(
                alignment: Alignment.centerLeft,
                child: FilledButton.icon(
                  onPressed: canConnect
                      ? () => _connect(catalogue.connectable)
                      : null,
                  icon: const Icon(Icons.add, size: 18),
                  label: const Text('Connect an account'),
                ),
              ),
              const SizedBox(height: 18),
              if (data.connections.isEmpty)
                WorkspaceNotice(
                  icon: Icons.link_off_outlined,
                  title: 'No accounts connected',
                  body:
                      'Nothing here holds a credential for anyone else. '
                      'Connecting an account stores a token you created at '
                      'that service, sealed with a key kept outside the '
                      'database.',
                )
              else
                for (final connection in data.connections)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 10),
                    child: _ConnectionCard(
                      connection: connection,
                      provider: catalogue?.providers
                          .where(
                            (provider) => provider.kind == connection.provider,
                          )
                          .firstOrNull,
                      storageConfigured: catalogue?.storageConfigured ?? false,
                      palette: palette,
                      onRevoke: () => _revoke(connection),
                      onRemove: () => _remove(connection),
                      tools:
                          connection.provider == IntegrationProviderKind.github
                          ? data.tools ?? const []
                          : const [],
                      onPolicyChanged: _setPolicy,
                    ),
                  ),
              if (catalogue != null && catalogue.refused.isNotEmpty) ...[
                const SizedBox(height: 22),
                Text(
                  'Not supported',
                  style: TextStyle(
                    fontSize: 15,
                    fontWeight: FontWeight.w700,
                    color: palette.text,
                  ),
                ),
                const SizedBox(height: 10),
                for (final provider in catalogue.refused)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 10),
                    child: WorkspaceNotice(
                      icon: Icons.block_outlined,
                      title: provider.displayName,
                      body: provider.unsupportedReason,
                    ),
                  ),
              ],
            ],
          );
        },
      ),
    );
  }
}

IconData providerIcon(IntegrationProviderKind kind) {
  switch (kind) {
    case IntegrationProviderKind.imap:
    case IntegrationProviderKind.googleWorkspace:
    case IntegrationProviderKind.microsoft365:
      return Icons.mail_outline;
    case IntegrationProviderKind.caldav:
      return Icons.event_outlined;
    case IntegrationProviderKind.notion:
      return Icons.description_outlined;
    case IntegrationProviderKind.github:
      return Icons.code;
    case IntegrationProviderKind.slack:
      return Icons.forum_outlined;
    case IntegrationProviderKind.unknown:
      return Icons.help_outline;
  }
}

String formatDay(DateTime when) {
  final local = when.toLocal();
  final month = local.month.toString().padLeft(2, '0');
  final day = local.day.toString().padLeft(2, '0');
  return '${local.year}-$month-$day';
}

class _ConnectionCard extends StatelessWidget {
  const _ConnectionCard({
    required this.connection,
    required this.provider,
    required this.storageConfigured,
    required this.palette,
    required this.onRevoke,
    required this.onRemove,
    required this.tools,
    required this.onPolicyChanged,
  });

  final IntegrationConnection connection;
  final IntegrationProviderInfo? provider;
  final bool storageConfigured;
  final AppPalette palette;
  final VoidCallback onRevoke;
  final VoidCallback onRemove;
  final List<ToolDescriptor> tools;
  final Future<void> Function(ToolDescriptor, ToolPolicy) onPolicyChanged;

  @override
  Widget build(BuildContext context) {
    final connectedAt = connection.connectedAt;
    final revokedAt = connection.revokedAt;
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: palette.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: palette.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(
                providerIcon(connection.provider),
                size: 18,
                color: AppColors.brand,
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  connection.displayName,
                  style: TextStyle(
                    fontSize: 14.5,
                    fontWeight: FontWeight.w600,
                    color: palette.text,
                  ),
                ),
              ),
              const SizedBox(width: 8),
              Flexible(child: _StateChip(state: connection.state)),
            ],
          ),
          const SizedBox(height: 8),
          if (provider == null || !provider!.supported) ...[
            Text(
              provider == null
                  ? 'Tool availability is unknown. A stored connection does not mean tools are available.'
                  : 'Tools unavailable: ${provider!.unsupportedReason}',
              style: TextStyle(
                fontSize: 12.5,
                height: 1.5,
                color: palette.textMuted,
              ),
            ),
            if (connection.isConnected)
              Text(
                provider == null ||
                        !const [
                          IntegrationProviderKind.imap,
                          IntegrationProviderKind.caldav,
                          IntegrationProviderKind.notion,
                        ].contains(connection.provider)
                    ? 'The saved credential is retained until you explicitly revoke or remove it.'
                    : 'Saved by an earlier release. The credential is retained without being used. '
                          'Choose Revoke access to delete the local credential, or Remove to delete '
                          'the saved connection record too.',
                style: TextStyle(
                  fontSize: 12.5,
                  height: 1.5,
                  color: palette.textMuted,
                ),
              ),
            const SizedBox(height: 8),
          ],
          if (connection.accountLabel.isNotEmpty)
            _Detail(label: 'Account', value: connection.accountLabel),
          if (connection.endpoint.isNotEmpty)
            _Detail(label: 'Server', value: connection.endpoint),
          _Detail(
            label: 'Credential',
            // Said in words rather than shown as an empty field: a blank here
            // would read as a rendering bug rather than as the point. Each
            // branch is a different claim, and the unknown one must not
            // borrow either of the others.
            value: switch (connection.state) {
              IntegrationConnectionState.connected =>
                connection.credentialUnreadable
                    ? 'Sealed with a key this machine no longer has'
                    : connection.credentialHint,
              IntegrationConnectionState.revoked =>
                'Destroyed when you revoked it',
              IntegrationConnectionState.unknown => 'Unknown',
            },
          ),
          if (connectedAt != null)
            _Detail(label: 'Saved on', value: formatDay(connectedAt)),
          if (revokedAt != null)
            _Detail(label: 'Revoked on', value: formatDay(revokedAt)),
          if (connection.credentialUnreadable && connection.isConnected) ...[
            const SizedBox(height: 8),
            Text(
              'The credential was sealed with a key this backend does not have. '
              'You can still revoke or remove it without that key.'
              '${provider?.supported == true && storageConfigured ? ' To use this provider, remove it and connect the account again.' : ''}',
              style: TextStyle(
                fontSize: 12.5,
                height: 1.5,
                color: AppColors.danger,
              ),
            ),
          ],
          if (connection.grantedScopes.isNotEmpty) ...[
            const SizedBox(height: 10),
            Text(
              'Recorded consent',
              style: TextStyle(
                fontSize: 12.5,
                fontWeight: FontWeight.w600,
                color: palette.text,
              ),
            ),
            const SizedBox(height: 5),
            for (final grant in connection.grantedScopes)
              Padding(
                padding: const EdgeInsets.only(bottom: 3),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '• ',
                      style: TextStyle(
                        fontSize: 12.5,
                        color: palette.textMuted,
                      ),
                    ),
                    Expanded(
                      child: Text(
                        grant,
                        style: TextStyle(
                          fontSize: 12.5,
                          height: 1.5,
                          color: palette.textMuted,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
          ],
          if (connection.isConnected &&
              !connection.credentialUnreadable &&
              provider?.supported == true &&
              tools.isNotEmpty) ...[
            const SizedBox(height: 10),
            Text(
              'Agent tools',
              style: TextStyle(
                fontSize: 12.5,
                fontWeight: FontWeight.w600,
                color: palette.text,
              ),
            ),
            const SizedBox(height: 5),
            for (final tool in tools)
              Padding(
                padding: const EdgeInsets.only(bottom: 6),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      tool.toolName,
                      style: const TextStyle(fontFamily: 'monospace'),
                    ),
                    const SizedBox(height: 2),
                    DropdownButton<ToolPolicy>(
                      value: tool.policy == ToolPolicy.unspecified
                          ? null
                          : tool.policy,
                      hint: const Text('Unknown policy'),
                      isDense: true,
                      isExpanded: true,
                      onChanged: (policy) {
                        if (policy != null) onPolicyChanged(tool, policy);
                      },
                      items: const [
                        DropdownMenuItem(
                          value: ToolPolicy.safe,
                          child: Text('Runs freely'),
                        ),
                        DropdownMenuItem(
                          value: ToolPolicy.approvalRequired,
                          child: Text('Asks first'),
                        ),
                        DropdownMenuItem(
                          value: ToolPolicy.disabled,
                          child: Text('Disabled'),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
          ],
          const SizedBox(height: 6),
          // Wrapped so the two actions stack instead of overflowing on a
          // phone-width card.
          Wrap(
            spacing: 4,
            children: [
              if (connection.isConnected)
                TextButton.icon(
                  onPressed: onRevoke,
                  icon: const Icon(Icons.link_off, size: 16),
                  label: const Text('Revoke access'),
                ),
              TextButton.icon(
                onPressed: onRemove,
                icon: const Icon(Icons.delete_outline, size: 16),
                label: const Text('Remove'),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _Detail extends StatelessWidget {
  const _Detail({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 3),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 92,
            child: Text(
              label,
              style: TextStyle(fontSize: 12.5, color: palette.textMuted),
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: TextStyle(fontSize: 12.5, color: palette.text),
            ),
          ),
        ],
      ),
    );
  }
}

class _StateChip extends StatelessWidget {
  const _StateChip({required this.state});

  final IntegrationConnectionState state;

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    // This is stored lifecycle state, independently of provider support.
    final (label, color) = switch (state) {
      IntegrationConnectionState.connected => (
        'Stored: connected',
        AppColors.brand,
      ),
      IntegrationConnectionState.revoked => ('Revoked', palette.textMuted),
      IntegrationConnectionState.unknown => (
        'Unknown state',
        AppColors.warning,
      ),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.13),
        borderRadius: BorderRadius.circular(20),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 11.5,
          fontWeight: FontWeight.w600,
          color: color,
        ),
      ),
    );
  }
}

/// The consent step.
///
/// The grants are shown in full before the credential field is even useful:
/// the Connect button stays disabled until the box is ticked, and the box
/// starts unticked. Nothing here is a scope the user picks, because the scope
/// of a pasted credential is decided where it was created — pretending
/// otherwise with checkboxes would be a lie with a nicer interface.
class _ConnectDialog extends StatefulWidget {
  const _ConnectDialog({required this.apiClient, required this.providers});

  final TuringApi apiClient;
  final List<IntegrationProviderInfo> providers;

  @override
  State<_ConnectDialog> createState() => _ConnectDialogState();
}

class _ConnectDialogState extends State<_ConnectDialog> {
  late IntegrationProviderInfo _provider = widget.providers.first;
  final TextEditingController _name = TextEditingController();
  final TextEditingController _account = TextEditingController();
  final TextEditingController _endpoint = TextEditingController();
  final TextEditingController _credential = TextEditingController();
  bool _consented = false;
  bool _saving = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _account.dispose();
    _endpoint.dispose();
    _credential.dispose();
    super.dispose();
  }

  void _selectProvider(IntegrationProviderInfo provider) {
    setState(() {
      _provider = provider;
      // Consent is to a specific provider's grants. Carrying a tick across a
      // change of provider would record agreement to something the user was
      // never shown.
      _consented = false;
      _error = null;
      // The rest belongs to the provider that was on screen when it was
      // typed. A server address left behind by a hidden field, or a token
      // meant for another service, would otherwise be stored against this
      // one — the endpoint visibly, the credential silently.
      _account.clear();
      _endpoint.clear();
      _credential.clear();
    });
  }

  Future<void> _save() async {
    if (_saving) return;
    final name = _name.text.trim();
    final credential = _credential.text.trim();
    // Checked here as well as on the server so the reason appears beside the
    // field instead of as a failed round trip.
    if (name.isEmpty) {
      setState(() => _error = 'Give the connection a name you will recognise.');
      return;
    }
    if (credential.isEmpty) {
      setState(
        () => _error = 'Paste the ${_provider.secretLabel.toLowerCase()}.',
      );
      return;
    }
    if (_provider.requiresEndpoint && _endpoint.text.trim().isEmpty) {
      setState(() => _error = 'This provider needs a server address.');
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await widget.apiClient.connectAccount(
        provider: _provider.kind,
        displayName: name,
        accountLabel: _account.text.trim(),
        endpoint: _endpoint.text.trim(),
        credential: credential,
        consentAcknowledged: _consented,
      );
      if (!mounted) return;
      Navigator.of(context).pop(true);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = '$error';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return AlertDialog(
      title: const Text('Connect an account'),
      content: ConstrainedBox(
        // Not a fixed width: this dialog has to open on a phone too.
        constraints: const BoxConstraints(maxWidth: 460),
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Wrap(
                spacing: 8,
                runSpacing: 4,
                children: [
                  for (final provider in widget.providers)
                    ChoiceChip(
                      label: Text(provider.displayName),
                      selected: provider.kind == _provider.kind,
                      onSelected: (_) => _selectProvider(provider),
                    ),
                ],
              ),
              const SizedBox(height: 14),
              TextField(
                controller: _name,
                autofocus: true,
                decoration: const InputDecoration(
                  labelText: 'Name',
                  helperText: 'How you will recognise it in a list',
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _account,
                decoration: InputDecoration(
                  labelText: _provider.accountLabel.isEmpty
                      ? 'Account'
                      : _provider.accountLabel,
                ),
              ),
              if (_provider.requiresEndpoint) ...[
                const SizedBox(height: 12),
                TextField(
                  controller: _endpoint,
                  decoration: InputDecoration(
                    labelText: _provider.endpointLabel.isEmpty
                        ? 'Server'
                        : _provider.endpointLabel,
                  ),
                ),
              ],
              const SizedBox(height: 12),
              TextField(
                controller: _credential,
                obscureText: true,
                decoration: InputDecoration(
                  labelText: _provider.secretLabel.isEmpty
                      ? 'Credential'
                      : _provider.secretLabel,
                  helperText: _provider.secretHelp,
                  helperMaxLines: 4,
                ),
              ),
              const SizedBox(height: 18),
              Text(
                'What this will allow',
                style: TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w700,
                  color: palette.text,
                ),
              ),
              const SizedBox(height: 6),
              for (final grant in _provider.grants)
                Padding(
                  padding: const EdgeInsets.only(bottom: 4),
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '• ',
                        style: TextStyle(
                          fontSize: 12.5,
                          color: palette.textMuted,
                        ),
                      ),
                      Expanded(
                        child: Text(
                          grant,
                          style: TextStyle(
                            fontSize: 12.5,
                            height: 1.5,
                            color: palette.textMuted,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              if (_provider.kind == IntegrationProviderKind.github) ...[
                const SizedBox(height: 8),
                Text(
                  'After connecting, GitHub tools become available. Their '
                  'default “Asks first” policy covers reads too, and local '
                  'chat sends will also ask for per-run egress consent until '
                  'you disable those tools.',
                  style: TextStyle(
                    fontSize: 12.5,
                    height: 1.5,
                    color: AppColors.warning,
                  ),
                ),
              ],
              const SizedBox(height: 6),
              CheckboxListTile(
                value: _consented,
                onChanged: _saving
                    ? null
                    : (value) => setState(() => _consented = value ?? false),
                controlAffinity: ListTileControlAffinity.leading,
                contentPadding: EdgeInsets.zero,
                title: Text(
                  'I understand this is standing access to the account, '
                  'until I revoke it.',
                  style: TextStyle(fontSize: 12.5, color: palette.text),
                ),
              ),
              Text(
                'The credential is stored on this machine, sealed with a key '
                'that lives in your .env file rather than in the database. '
                'Revoking destroys it here; deleting it at the provider is '
                'still yours to do.',
                style: TextStyle(fontSize: 12, color: palette.textMuted),
              ),
              if (_error != null) ...[
                const SizedBox(height: 12),
                Text(
                  _error!,
                  style: TextStyle(fontSize: 13, color: AppColors.danger),
                ),
              ],
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: _saving ? null : () => Navigator.of(context).pop(false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          // Disabled rather than failing on tap: the reason it cannot be
          // pressed is the sentence directly above it.
          onPressed: (_saving || !_consented) ? null : _save,
          child: Text(_saving ? 'Connecting...' : 'Connect'),
        ),
      ],
    );
  }
}
