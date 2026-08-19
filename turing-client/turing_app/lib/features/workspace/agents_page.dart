import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../models/agent_descriptor.dart';
import '../../models/external_agent.dart';
import '../../networking/api_client.dart';
import 'workspace_pages.dart';

/// Where a conversation can be sent, and what that costs in privacy.
///
/// Turing's own assistant is listed first and cannot be configured away: it is
/// the default, and the only destination that keeps a conversation on this
/// machine. Everything else here is a company that will receive the whole
/// transcript, so the page says that plainly rather than presenting the two
/// kinds as interchangeable rows in a list.
class AgentsPage extends StatefulWidget {
  const AgentsPage({super.key, required this.apiClient});

  final TuringApi apiClient;

  @override
  State<AgentsPage> createState() => _AgentsPageState();
}

class _AgentsPageState extends State<AgentsPage> {
  late Future<_AgentsView> _view;

  @override
  void initState() {
    super.initState();
    _view = _load();
  }

  Future<_AgentsView> _load() async {
    // Both at once: the page cannot render without each, so serialising them
    // only doubles how long the user waits on an empty panel.
    final results = await Future.wait([
      widget.apiClient.listAgents(),
      widget.apiClient.listExternalAgents(),
    ]);
    return _AgentsView(
      local: results[0] as List<AgentDescriptor>,
      external: results[1] as List<ExternalAgent>,
    );
  }

  void _reload() {
    setState(() {
      _view = _load();
    });
  }

  Future<void> _edit({ExternalAgent? existing}) async {
    final saved = await showDialog<bool>(
      context: context,
      builder: (_) =>
          ExternalAgentEditor(apiClient: widget.apiClient, existing: existing),
    );
    if (saved == true) _reload();
  }

  Future<void> _delete(ExternalAgent agent) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text('Remove "${agent.displayName}"?'),
        content: const Text(
          'Any conversation currently routed here goes back to Turing on this '
          'machine. Replies already being generated are unaffected — they were '
          'given this agent\'s details when you sent the message.',
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
    try {
      await widget.apiClient.deleteExternalAgent(agentId: agent.agentId);
      if (!mounted) return;
      _reload();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('Could not remove it: $error')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return WorkspacePage(
      title: 'Agents',
      subtitle:
          'Who answers your messages. Turing runs on this machine and is the '
          'default. You can also add an assistant that does not — Claude, '
          'ChatGPT, Gemini, Grok — and send individual conversations to it.',
      child: FutureBuilder<_AgentsView>(
        future: _view,
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
          final view =
              snapshot.data ?? const _AgentsView(local: [], external: []);
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (final agent in view.local)
                Padding(
                  padding: const EdgeInsets.only(bottom: 10),
                  child: _LocalAgentCard(
                    displayName: agent.displayName,
                    palette: palette,
                  ),
                ),
              const SizedBox(height: 12),
              Align(
                alignment: Alignment.centerLeft,
                child: FilledButton.icon(
                  onPressed: () => _edit(),
                  icon: const Icon(Icons.add, size: 18),
                  label: const Text('New agent'),
                ),
              ),
              const SizedBox(height: 18),
              if (view.external.isEmpty)
                WorkspaceNotice(
                  icon: Icons.cloud_off_outlined,
                  title: 'No conversation leaves this machine',
                  body:
                      'You have not added an assistant that runs somewhere '
                      'else. Add one and you can send a chosen conversation to '
                      'it — every message in that conversation, and everything '
                      'the agent reads with your tools, goes to that company. '
                      'Conversations you do not route stay here.',
                )
              else ...[
                WorkspaceNotice(
                  icon: Icons.cloud_upload_outlined,
                  title: 'These receive whatever you send them',
                  body:
                      'A conversation routed to one of these sends its whole '
                      'transcript there, along with the results of any tool it '
                      'runs on your files. Nothing is routed automatically — '
                      'you pick a destination per conversation, and the bar '
                      'above the messages says which one it is.',
                  tone: AppColors.warning,
                ),
                const SizedBox(height: 12),
                for (final agent in view.external)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 10),
                    child: _ExternalAgentCard(
                      agent: agent,
                      palette: palette,
                      onEdit: () => _edit(existing: agent),
                      onDelete: () => _delete(agent),
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

class _AgentsView {
  const _AgentsView({required this.local, required this.external});

  final List<AgentDescriptor> local;
  final List<ExternalAgent> external;
}

class _LocalAgentCard extends StatelessWidget {
  const _LocalAgentCard({required this.displayName, required this.palette});

  final String displayName;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: palette.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: palette.border),
      ),
      child: Row(
        children: [
          Icon(Icons.smart_toy_outlined, size: 18, color: AppColors.brand),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  displayName,
                  style: TextStyle(
                    fontSize: 14.5,
                    fontWeight: FontWeight.w600,
                    color: palette.text,
                  ),
                ),
                const SizedBox(height: 3),
                Text(
                  'Runs on this machine, through your configured model '
                  'provider. The default for every conversation, and it '
                  'cannot be removed.',
                  style: TextStyle(fontSize: 12.5, color: palette.textMuted),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _ExternalAgentCard extends StatelessWidget {
  const _ExternalAgentCard({
    required this.agent,
    required this.palette,
    required this.onEdit,
    required this.onDelete,
  });

  final ExternalAgent agent;
  final AppPalette palette;
  final VoidCallback onEdit;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
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
            children: [
              Icon(Icons.cloud_outlined, size: 18, color: AppColors.warning),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  agent.displayName,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    fontSize: 14.5,
                    fontWeight: FontWeight.w600,
                    color: palette.text,
                  ),
                ),
              ),
              IconButton(
                icon: const Icon(Icons.edit_outlined, size: 17),
                tooltip: 'Edit agent',
                color: palette.textMuted,
                visualDensity: VisualDensity.compact,
                onPressed: onEdit,
              ),
              IconButton(
                icon: const Icon(Icons.delete_outline, size: 17),
                tooltip: 'Remove agent',
                color: palette.textMuted,
                visualDensity: VisualDensity.compact,
                onPressed: onDelete,
              ),
            ],
          ),
          const SizedBox(height: 6),
          Text(
            '${agent.provider.label} · ${agent.model} · ${agent.endpointHost}',
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(
              fontSize: 12.5,
              height: 1.5,
              color: palette.textMuted,
            ),
          ),
          const SizedBox(height: 8),
          // Said here rather than discovered on the first failed message: an
          // agent whose key the backend cannot find is configuration that
          // looks complete and is not.
          if (agent.credentialAvailable)
            _CredentialChip(
              label: 'API key "${agent.credentialRef}" found',
              color: AppColors.success,
              icon: Icons.key_outlined,
            )
          else
            _CredentialChip(
              label:
                  'No API key named "${agent.credentialRef}" — add it and '
                  'restart the backend',
              color: AppColors.danger,
              icon: Icons.key_off_outlined,
            ),
        ],
      ),
    );
  }
}

class _CredentialChip extends StatelessWidget {
  const _CredentialChip({
    required this.label,
    required this.color,
    required this.icon,
  });

  final String label;
  final Color color;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    return Align(
      alignment: Alignment.centerLeft,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
        decoration: BoxDecoration(
          color: color.withValues(alpha: 0.13),
          borderRadius: BorderRadius.circular(20),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 13, color: color),
            const SizedBox(width: 6),
            Flexible(
              child: Text(
                label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  fontSize: 11.5,
                  fontWeight: FontWeight.w600,
                  color: color,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// The form for adding or changing an external agent.
///
/// Public so the widget test can pump it directly rather than driving three
/// taps to reach it.
class ExternalAgentEditor extends StatefulWidget {
  const ExternalAgentEditor({
    super.key,
    required this.apiClient,
    this.existing,
  });

  final TuringApi apiClient;
  final ExternalAgent? existing;

  @override
  State<ExternalAgentEditor> createState() => _ExternalAgentEditorState();
}

class _ExternalAgentEditorState extends State<ExternalAgentEditor> {
  late final TextEditingController _name = TextEditingController(
    text: widget.existing?.displayName ?? '',
  );
  late final TextEditingController _baseUrl = TextEditingController(
    text:
        widget.existing?.baseUrl ??
        ExternalAgentProvider.anthropic.suggestedBaseUrl,
  );
  late final TextEditingController _model = TextEditingController(
    text: widget.existing?.model ?? '',
  );
  late final TextEditingController _credentialRef = TextEditingController(
    text: widget.existing?.credentialRef ?? '',
  );
  late ExternalAgentProvider _provider =
      widget.existing?.provider ?? ExternalAgentProvider.anthropic;
  bool _saving = false;
  String? _error;

  /// The dropdown's options.
  ///
  /// An agent stored by a newer backend can carry a provider this build does
  /// not know. Its row still has to be editable, and a dropdown whose value is
  /// absent from its items throws — so the unrecognised value is listed while
  /// it is the current one. Saving it is refused below: relabelling it as a
  /// vendor the user did not pick would change who receives the conversation.
  List<ExternalAgentProvider> get _providerChoices => [
    if (_provider == ExternalAgentProvider.unknown) _provider,
    ...ExternalAgentProvider.selectable,
  ];

  @override
  void dispose() {
    _name.dispose();
    _baseUrl.dispose();
    _model.dispose();
    _credentialRef.dispose();
    super.dispose();
  }

  /// Changing the vendor refills the endpoint only when the current one is
  /// still a suggestion. Overwriting a URL someone typed would silently
  /// re-point their agent at a different company.
  void _selectProvider(ExternalAgentProvider provider) {
    final wasSuggested = ExternalAgentProvider.values.any(
      (candidate) =>
          candidate.suggestedBaseUrl.isNotEmpty &&
          candidate.suggestedBaseUrl == _baseUrl.text.trim(),
    );
    setState(() {
      _provider = provider;
      if ((wasSuggested || _baseUrl.text.trim().isEmpty) &&
          provider.suggestedBaseUrl.isNotEmpty) {
        _baseUrl.text = provider.suggestedBaseUrl;
      }
    });
  }

  Future<void> _save() async {
    if (_saving) return;
    final name = _name.text.trim();
    final baseUrl = _baseUrl.text.trim();
    final model = _model.text.trim();
    final credentialRef = _credentialRef.text.trim();
    // Checked here as well as on the server so the reason appears next to the
    // fields rather than as a failed round trip.
    if (name.isEmpty ||
        baseUrl.isEmpty ||
        model.isEmpty ||
        credentialRef.isEmpty) {
      setState(() => _error = 'Every field is required.');
      return;
    }
    if (_provider == ExternalAgentProvider.unknown) {
      setState(
        () => _error =
            'This agent was set up by a newer version and its provider is not '
            'one this app recognises. Pick one before saving.',
      );
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final existing = widget.existing;
      if (existing == null) {
        await widget.apiClient.createExternalAgent(
          displayName: name,
          provider: _provider,
          baseUrl: baseUrl,
          model: model,
          credentialRef: credentialRef,
        );
      } else {
        await widget.apiClient.updateExternalAgent(
          agentId: existing.agentId,
          displayName: name,
          provider: _provider,
          baseUrl: baseUrl,
          model: model,
          credentialRef: credentialRef,
        );
      }
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
    // Bounded by the window as well as by taste: a fixed 460 overflows a phone
    // in portrait, which is exactly where the compact layout puts this dialog.
    // 160 is the dialog's own inset padding plus its content padding, doubled.
    final available = MediaQuery.of(context).size.width - 160;
    final width = available < 0 ? 0.0 : available;
    return AlertDialog(
      title: Text(widget.existing == null ? 'Add an agent' : 'Edit agent'),
      content: SizedBox(
        width: width < 460 ? width : 460,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              TextField(
                controller: _name,
                autofocus: true,
                decoration: const InputDecoration(
                  labelText: 'Name',
                  helperText: 'How you will recognise it in a list',
                ),
              ),
              const SizedBox(height: 16),
              DropdownButtonFormField<ExternalAgentProvider>(
                initialValue: _provider,
                // Without this the field sizes itself to its widest item and
                // does not ellipsize, so the longest provider label pushes the
                // dialog wider than a phone.
                isExpanded: true,
                decoration: const InputDecoration(labelText: 'Provider'),
                items: [
                  for (final provider in _providerChoices)
                    DropdownMenuItem(
                      value: provider,
                      child: Text(provider.label),
                    ),
                ],
                onChanged: (value) {
                  if (value != null) _selectProvider(value);
                },
              ),
              const SizedBox(height: 16),
              TextField(
                controller: _baseUrl,
                decoration: const InputDecoration(
                  labelText: 'Endpoint',
                  helperText:
                      'The OpenAI-compatible base URL. Prefilled as a '
                      'starting point — check it against the vendor. A '
                      'gateway on this machine is reached at '
                      'host.docker.internal, not localhost, because the '
                      'backend runs in a container.',
                ),
              ),
              const SizedBox(height: 16),
              TextField(
                controller: _model,
                decoration: const InputDecoration(
                  labelText: 'Model',
                  helperText: 'Exactly as the vendor names it',
                ),
              ),
              const SizedBox(height: 16),
              TextField(
                controller: _credentialRef,
                decoration: const InputDecoration(
                  labelText: 'API key name',
                  helperText:
                      'A name, not the key. Put the key itself in '
                      'TURING_AGENT_API_KEYS in turing-backend/.env, then '
                      'restart the backend.',
                ),
              ),
              if (_error != null) ...[
                const SizedBox(height: 14),
                Text(
                  _error!,
                  style: TextStyle(fontSize: 13, color: AppColors.danger),
                ),
              ],
              const SizedBox(height: 14),
              Text(
                'The key never passes through this app and is never stored in '
                'the database — only its name is. A conversation you send here '
                'leaves your machine in full.',
                style: TextStyle(fontSize: 12.5, color: palette.textMuted),
              ),
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
          onPressed: _saving ? null : _save,
          child: Text(_saving ? 'Saving...' : 'Save'),
        ),
      ],
    );
  }
}
