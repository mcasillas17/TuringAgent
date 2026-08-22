import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../models/mcp_server.dart';
import '../../models/tool_descriptor.dart';
import '../../networking/api_client.dart';

/// Shared page scaffolding so every destination gets the same measure,
/// heading rhythm and scroll behaviour.
class WorkspacePage extends StatelessWidget {
  const WorkspacePage({
    super.key,
    required this.title,
    required this.subtitle,
    required this.child,
  });

  final String title;
  final String subtitle;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return SingleChildScrollView(
      padding: const EdgeInsets.fromLTRB(28, 28, 28, 40),
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 760),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                title,
                style: TextStyle(
                  fontSize: 24,
                  fontWeight: FontWeight.w700,
                  letterSpacing: -0.3,
                  color: palette.text,
                ),
              ),
              const SizedBox(height: 8),
              Text(
                subtitle,
                style: TextStyle(
                  fontSize: 14,
                  height: 1.55,
                  color: palette.textMuted,
                ),
              ),
              const SizedBox(height: 26),
              child,
            ],
          ),
        ),
      ),
    );
  }
}

/// Every tool the backend discovered, grouped by the MCP server offering it.
class McpsPage extends StatefulWidget {
  const McpsPage({super.key, required this.apiClient});

  final TuringApi apiClient;

  @override
  State<McpsPage> createState() => _McpsPageState();
}

class _McpsPageState extends State<McpsPage> {
  late Future<McpRegistrySnapshot> _registry;
  final Set<String> _pendingToolPolicies = {};
  // Keyed by serverId so a pending enable/disable or delete for one server
  // disables only that server's Switch/popup, never a different server's.
  final Set<String> _pendingServerMutations = {};
  bool _reimporting = false;

  @override
  void initState() {
    super.initState();
    _registry = widget.apiClient.listMcpServers();
  }

  void _reload() {
    setState(() {
      _registry = widget.apiClient.listMcpServers();
    });
  }

  Future<void> _reimport() async {
    if (_reimporting) return;
    setState(() => _reimporting = true);
    try {
      final report = await widget.apiClient.reimportMcpJson();
      if (!mounted) return;
      _reload();
      await showDialog<void>(
        context: context,
        builder: (_) => _ImportReportDialog(report: report),
      );
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('$error')));
    } finally {
      if (mounted) setState(() => _reimporting = false);
    }
  }

  Future<void> _rotateToken(McpServer server) async {
    final rotated = await showDialog<bool>(
      context: context,
      // The dialog manages its own dismissal via PopScope so it cannot be
      // dismissed by the barrier while a rotation is in flight.
      barrierDismissible: false,
      builder: (_) =>
          _RotateTokenDialog(apiClient: widget.apiClient, server: server),
    );
    if (rotated == true && mounted) _reload();
  }

  Future<void> _setServerEnabled(McpServer server, bool enabled) async {
    if (_pendingServerMutations.contains(server.serverId)) return;
    setState(() => _pendingServerMutations.add(server.serverId));
    try {
      await widget.apiClient.setMcpServerEnabled(
        serverId: server.serverId,
        enabled: enabled,
      );
      if (mounted) _reload();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('$error')));
    } finally {
      if (mounted) {
        setState(() => _pendingServerMutations.remove(server.serverId));
      }
    }
  }

  Future<void> _setToolPolicy(
    McpServer server,
    ToolDescriptor tool,
    ToolPolicy policy,
  ) async {
    final mutationKey = '${server.serverId}/${tool.toolName}';
    if (_pendingToolPolicies.contains(mutationKey)) return;
    setState(() => _pendingToolPolicies.add(mutationKey));
    try {
      await widget.apiClient.updateMcpToolPolicy(
        serverId: server.serverId,
        toolName: tool.toolName,
        policy: policy,
      );
      if (mounted) _reload();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('$error')));
    } finally {
      if (mounted) {
        setState(() => _pendingToolPolicies.remove(mutationKey));
      }
    }
  }

  Future<void> _deleteServer(McpServer server) async {
    if (_pendingServerMutations.contains(server.serverId)) return;
    setState(() => _pendingServerMutations.add(server.serverId));
    try {
      await widget.apiClient.deleteMcpServer(serverId: server.serverId);
      if (mounted) _reload();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('$error')));
    } finally {
      if (mounted) {
        setState(() => _pendingServerMutations.remove(server.serverId));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return WorkspacePage(
      title: 'MCPs',
      subtitle:
          'Registered tool servers and their policies. Registering a server '
          'here does not turn it on — every server, added or imported, stays '
          'disabled until you enable it. Enabling a remote server contacts '
          'its endpoint to discover its tools; every run still asks before '
          'sending a tool call\'s arguments/results, the same per-run '
          'egress confirmation as any other destination off this machine.',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _AddServerCard(apiClient: widget.apiClient, onRegistered: _reload),
          const SizedBox(height: 16),
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton.icon(
              key: const Key('mcpsReimportButton'),
              onPressed: _reimporting ? null : _reimport,
              icon: const Icon(Icons.sync, size: 16),
              label: Text(_reimporting ? 'Reimporting…' : 'Re-import mcp.json'),
            ),
          ),
          const SizedBox(height: 12),
          FutureBuilder<McpRegistrySnapshot>(
            future: _registry,
            builder: (context, snapshot) {
              if (snapshot.connectionState != ConnectionState.done) {
                return const WorkspaceLoading();
              }
              if (snapshot.hasError) {
                return _PageError(
                  message: '${snapshot.error}',
                  onRetry: _reload,
                );
              }
              final registry =
                  snapshot.data ??
                  McpRegistrySnapshot(servers: const [], unsupported: const []);
              if (registry.servers.isEmpty && registry.unsupported.isEmpty) {
                return WorkspaceNotice(
                  icon: Icons.hub_outlined,
                  title: 'No MCP servers registered',
                  body:
                      'Add a server here to register it immediately. For '
                      'bulk setup, edit the mounted mcp.json file and choose '
                      'Re-import mcp.json. Neither path needs a backend '
                      'restart.',
                  onRetry: _reload,
                );
              }
              final servers = registry.servers.toList()
                ..sort((a, b) => a.name.compareTo(b.name));
              return Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  for (final unsupported in registry.unsupported) ...[
                    WorkspaceNotice(
                      icon: Icons.block_outlined,
                      title: '${unsupported.name} was not imported',
                      body: unsupported.reason,
                      tone: AppColors.warning,
                    ),
                    const SizedBox(height: 12),
                  ],
                  for (final server in servers) ...[
                    _ServerCard(
                      server: server,
                      palette: palette,
                      onEnabledChanged: server.tier == McpServerTier.bundled
                          ? null
                          : (enabled) => _setServerEnabled(server, enabled),
                      onDelete: server.tier == McpServerTier.bundled
                          ? null
                          : () => _deleteServer(server),
                      onRotateToken: server.tier == McpServerTier.bundled
                          ? null
                          : () => _rotateToken(server),
                      onPolicyChanged: (tool, policy) =>
                          _setToolPolicy(server, tool, policy),
                      pendingToolPolicies: _pendingToolPolicies,
                      busy: _pendingServerMutations.contains(server.serverId),
                    ),
                    const SizedBox(height: 12),
                  ],
                ],
              );
            },
          ),
        ],
      ),
    );
  }
}

class _ServerCard extends StatelessWidget {
  const _ServerCard({
    required this.server,
    required this.palette,
    required this.onEnabledChanged,
    required this.onDelete,
    required this.onRotateToken,
    required this.onPolicyChanged,
    required this.pendingToolPolicies,
    required this.busy,
  });

  final McpServer server;
  final AppPalette palette;
  final ValueChanged<bool>? onEnabledChanged;
  final VoidCallback? onDelete;
  final VoidCallback? onRotateToken;
  final void Function(ToolDescriptor tool, ToolPolicy policy) onPolicyChanged;
  final Set<String> pendingToolPolicies;
  // True while an enable/disable or delete for this server is in flight, so
  // the Switch and popup can be disabled and a rapid second tap cannot
  // submit a duplicate write.
  final bool busy;

  @override
  Widget build(BuildContext context) {
    final tools = server.tools.toList()
      ..sort((a, b) => a.toolName.compareTo(b.toolName));
    return Container(
      decoration: BoxDecoration(
        color: palette.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: palette.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 14, 16, 12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                // Split across two lines rather than one crowded Row: the
                // name needs to be able to take all available width and
                // ellipsize on its own line, while the trailing controls
                // (count/switch/menu) wrap onto a new line instead of
                // overflowing at compact widths.
                Row(
                  children: [
                    Icon(Icons.dns_outlined, size: 16, color: AppColors.brand),
                    const SizedBox(width: 9),
                    Expanded(
                      child: Text(
                        server.name,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 14.5,
                          fontWeight: FontWeight.w600,
                          color: palette.text,
                        ),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 6),
                Wrap(
                  alignment: WrapAlignment.spaceBetween,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  spacing: 8,
                  runSpacing: 6,
                  children: [
                    Text(
                      tools.length == 1 ? '1 tool' : '${tools.length} tools',
                      style: TextStyle(
                        fontSize: 12.5,
                        color: palette.textMuted,
                      ),
                    ),
                    Wrap(
                      spacing: 4,
                      runSpacing: 4,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: [
                        Switch(
                          value: server.enabled,
                          onChanged: busy ? null : onEnabledChanged,
                        ),
                        if (onRotateToken != null || onDelete != null)
                          PopupMenuButton<String>(
                            enabled: !busy,
                            tooltip: 'Actions for ${server.name}',
                            icon: const Icon(Icons.more_vert, size: 18),
                            onSelected: (value) {
                              if (value == 'rotate') onRotateToken?.call();
                              if (value == 'delete') onDelete?.call();
                            },
                            itemBuilder: (context) => [
                              if (onRotateToken != null)
                                const PopupMenuItem(
                                  value: 'rotate',
                                  child: Text('Rotate token'),
                                ),
                              if (onDelete != null)
                                const PopupMenuItem(
                                  value: 'delete',
                                  child: Text('Remove'),
                                ),
                            ],
                          ),
                      ],
                    ),
                  ],
                ),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 6,
                  runSpacing: 6,
                  children: [
                    _ServerBadge(label: _tierLabel(server.tier)),
                    _ServerBadge(
                      label: _livenessLabel(server),
                      tone: server.liveness == McpServerLiveness.down
                          ? AppColors.danger
                          : null,
                    ),
                    _ServerBadge(
                      label: server.sandboxConfined
                          ? 'Sandbox-confined'
                          : 'Not sandbox-confined',
                      tone: server.sandboxConfined
                          ? AppColors.success
                          : AppColors.warning,
                    ),
                  ],
                ),
                if (server.statusMessage.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text(
                    server.statusMessage,
                    style: TextStyle(fontSize: 12, color: AppColors.danger),
                  ),
                ],
              ],
            ),
          ),
          Divider(height: 1, color: palette.border),
          for (final tool in tools)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 11, 16, 11),
              child: Wrap(
                spacing: 12,
                runSpacing: 8,
                alignment: WrapAlignment.spaceBetween,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  ConstrainedBox(
                    // Bounded so a long tool name ellipsizes instead of
                    // pushing the policy picker off the card at compact
                    // widths, matching the name treatment in the header.
                    constraints: const BoxConstraints(maxWidth: 150),
                    child: Text(
                      tool.toolName,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 13.5,
                        fontFamily: 'monospace',
                        color: palette.text,
                      ),
                    ),
                  ),
                  _PolicyPicker(
                    policy: tool.policy,
                    present:
                        tool.present || server.tier == McpServerTier.bundled,
                    busy: pendingToolPolicies.contains(
                      '${server.serverId}/${tool.toolName}',
                    ),
                    onChanged: (policy) {
                      if (policy != null) onPolicyChanged(tool, policy);
                    },
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

/// Lets a server be registered directly from the page — no file edit or
/// restart needed. Kept as a single vertically stacked card (rather than a
/// row of fields) so it cannot overflow even at very narrow widths.
class _AddServerCard extends StatefulWidget {
  const _AddServerCard({required this.apiClient, required this.onRegistered});

  final TuringApi apiClient;
  final VoidCallback onRegistered;

  @override
  State<_AddServerCard> createState() => _AddServerCardState();
}

class _AddServerCardState extends State<_AddServerCard> {
  final _name = TextEditingController();
  final _url = TextEditingController();
  final _token = TextEditingController();
  McpServerTier _tier = McpServerTier.localContainer;
  bool _submitting = false;
  String? _error;
  String? _status;

  @override
  void dispose() {
    _name.dispose();
    _url.dispose();
    _token.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_submitting) return;
    final name = _name.text.trim();
    final url = _url.text.trim();
    // Checked here so the reason appears next to the fields instead of a
    // failed round trip, and so the token is never sent for an otherwise
    // invalid submission.
    if (name.isEmpty || url.isEmpty) {
      setState(() {
        _error = 'Name and URL are required.';
        _status = null;
      });
      return;
    }
    setState(() {
      _submitting = true;
      _error = null;
      _status = null;
    });
    try {
      await widget.apiClient.registerMcpServer(
        name: name,
        url: url,
        tier: _tier,
        bearerToken: _token.text,
      );
      if (!mounted) return;
      // Cleared on success — especially the token, which this app never
      // shows again once it has been sent.
      _name.clear();
      _url.clear();
      _token.clear();
      setState(() {
        _submitting = false;
        _status = '"$name" added. It stays disabled until you turn it on.';
      });
      widget.onRegistered();
    } catch (error) {
      if (!mounted) return;
      // The token is cleared even on failure: once it has been sent for an
      // attempt, this app does not hold onto it or offer it back — the user
      // retypes it if they retry, rather than it sitting in memory or being
      // resubmitted silently.
      _token.clear();
      setState(() {
        _submitting = false;
        _error = '$error';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return Container(
      decoration: BoxDecoration(
        color: palette.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: palette.border),
      ),
      padding: const EdgeInsets.fromLTRB(16, 14, 16, 16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Icon(Icons.add_circle_outline, size: 18, color: AppColors.brand),
              const SizedBox(width: 8),
              Text(
                'Add server',
                style: TextStyle(
                  fontSize: 14.5,
                  fontWeight: FontWeight.w600,
                  color: palette.text,
                ),
              ),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            'Registers immediately — no file edit or restart needed. It '
            'stays disabled until you turn it on.',
            style: TextStyle(fontSize: 12.5, color: palette.textMuted),
          ),
          const SizedBox(height: 12),
          TextField(
            key: const Key('mcpsAddName'),
            controller: _name,
            decoration: const InputDecoration(
              labelText: 'Name',
              isDense: true,
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 10),
          TextField(
            key: const Key('mcpsAddUrl'),
            controller: _url,
            decoration: const InputDecoration(
              labelText: 'URL',
              isDense: true,
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 10),
          DropdownButtonFormField<McpServerTier>(
            key: const Key('mcpsAddTier'),
            initialValue: _tier,
            isExpanded: true,
            decoration: const InputDecoration(
              labelText: 'Server type',
              isDense: true,
              border: OutlineInputBorder(),
            ),
            // Bundled servers ship with the backend and are never offered
            // here — only tiers a user can actually register are listed.
            items: const [
              DropdownMenuItem(
                value: McpServerTier.localContainer,
                child: Text('Local container'),
              ),
              DropdownMenuItem(
                value: McpServerTier.remoteUrl,
                child: Text('Remote URL'),
              ),
            ],
            onChanged: _submitting
                ? null
                : (value) {
                    if (value != null) setState(() => _tier = value);
                  },
          ),
          const SizedBox(height: 10),
          _ObscuredTokenField(
            fieldKey: const Key('mcpsAddToken'),
            controller: _token,
            labelText: 'Bearer token (optional)',
          ),
          const SizedBox(height: 12),
          Align(
            alignment: Alignment.centerLeft,
            child: FilledButton(
              key: const Key('mcpsAddSubmit'),
              onPressed: _submitting ? null : _submit,
              child: Text(_submitting ? 'Adding…' : 'Add server'),
            ),
          ),
          if (_error != null) ...[
            const SizedBox(height: 10),
            Semantics(
              liveRegion: true,
              child: Text(
                _error!,
                style: TextStyle(fontSize: 12.5, color: AppColors.danger),
              ),
            ),
          ],
          if (_status != null) ...[
            const SizedBox(height: 10),
            Semantics(
              liveRegion: true,
              child: Text(
                _status!,
                style: TextStyle(fontSize: 12.5, color: AppColors.success),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

/// Bounds dialog content to a sane width on desktop while staying scrollable
/// and shrinking down safely on compact/mobile viewports.
///
/// The subtracted amount is derived from what [AlertDialog] itself actually
/// reserves outside the content area — its `insetPadding` (dialog margin
/// from the screen edges) plus the default `contentPadding` it applies
/// around content (see `AlertDialog`'s defaults in the Flutter SDK) — rather
/// than a single opaque constant, so it tracks the dialog's real chrome
/// instead of an estimate that can drift out of sync with it.
double _dialogWidth(BuildContext context, double preferred) {
  final theme = Theme.of(context);
  final insetPadding =
      theme.dialogTheme.insetPadding ??
      const EdgeInsets.symmetric(horizontal: 40, vertical: 24);
  final contentPadding = EdgeInsets.only(
    left: 24,
    top: theme.useMaterial3 ? 16 : 20,
    right: 24,
    bottom: 24,
  );
  final horizontalChrome = insetPadding.horizontal + contentPadding.horizontal;
  final available = MediaQuery.of(context).size.width - horizontalChrome;
  final width = available < 0 ? 0.0 : available;
  return width < preferred ? width : preferred;
}

/// A token entry field with the security-sensitive configuration — obscured,
/// no autocorrect, no suggestions — kept in one place so the add-server form
/// and the rotate-token dialog can't drift out of sync with each other.
class _ObscuredTokenField extends StatelessWidget {
  const _ObscuredTokenField({
    required this.fieldKey,
    required this.controller,
    required this.labelText,
  });

  final Key fieldKey;
  final TextEditingController controller;
  final String labelText;

  @override
  Widget build(BuildContext context) {
    return TextField(
      key: fieldKey,
      controller: controller,
      obscureText: true,
      autocorrect: false,
      enableSuggestions: false,
      decoration: InputDecoration(
        labelText: labelText,
        isDense: true,
        border: const OutlineInputBorder(),
      ),
    );
  }
}

class _ImportReportDialog extends StatelessWidget {
  const _ImportReportDialog({required this.report});

  final McpImportReport report;

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('mcp.json re-imported'),
      content: SizedBox(
        width: _dialogWidth(context, 420),
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              _ReportSection(title: 'Imported', entries: report.imported),
              const SizedBox(height: 12),
              _ReportSection(title: 'Skipped', entries: report.skipped),
              const SizedBox(height: 12),
              _ReportSection(
                title: 'Refused',
                entries: report.refused.map((r) => '${r.name} — ${r.reason}'),
              ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Close'),
        ),
      ],
    );
  }
}

class _ReportSection extends StatelessWidget {
  const _ReportSection({required this.title, required this.entries});

  final String title;
  final Iterable<String> entries;

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    final list = entries.toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          title,
          style: TextStyle(fontWeight: FontWeight.w600, color: palette.text),
        ),
        const SizedBox(height: 4),
        if (list.isEmpty)
          Text('None', style: TextStyle(color: palette.textMuted))
        else
          for (final entry in list)
            Padding(
              padding: const EdgeInsets.only(bottom: 2),
              child: Text(entry, style: TextStyle(color: palette.text)),
            ),
      ],
    );
  }
}

/// Always opens with a fresh, empty, obscured token field — never
/// pre-filled with the current token, which this app never reads back.
class _RotateTokenDialog extends StatefulWidget {
  const _RotateTokenDialog({required this.apiClient, required this.server});

  final TuringApi apiClient;
  final McpServer server;

  @override
  State<_RotateTokenDialog> createState() => _RotateTokenDialogState();
}

class _RotateTokenDialogState extends State<_RotateTokenDialog> {
  final _token = TextEditingController();
  bool _submitting = false;
  String? _error;

  @override
  void dispose() {
    _token.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_submitting) return;
    setState(() {
      _submitting = true;
      _error = null;
    });
    try {
      await widget.apiClient.rotateMcpServerToken(
        serverId: widget.server.serverId,
        bearerToken: _token.text,
      );
      if (!mounted) return;
      _token.clear();
      Navigator.of(context).pop(true);
    } catch (error) {
      if (!mounted) return;
      // Cleared even on failure, matching the add-server form: once a
      // token has been sent for an attempt, this app does not hold onto it
      // or offer it back — the user retypes it if they retry.
      _token.clear();
      setState(() {
        _submitting = false;
        _error = '$error';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return PopScope(
      // Blocks the platform back gesture/button while a rotation is in
      // flight, matching the barrier (`barrierDismissible: false`, set by
      // the caller) and the disabled Cancel button below — the only way
      // out while pending is for the request to finish.
      canPop: !_submitting,
      child: AlertDialog(
        title: Text('Rotate token for ${widget.server.name}'),
        content: SizedBox(
          width: _dialogWidth(context, 420),
          child: SingleChildScrollView(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  'Enter a new bearer token, or leave it empty to clear the '
                  'token for this server.',
                ),
                const SizedBox(height: 12),
                _ObscuredTokenField(
                  fieldKey: const Key('mcpsRotateToken'),
                  controller: _token,
                  labelText: 'New bearer token',
                ),
                if (_error != null) ...[
                  const SizedBox(height: 10),
                  Semantics(
                    liveRegion: true,
                    child: Text(
                      _error!,
                      style: TextStyle(fontSize: 12.5, color: AppColors.danger),
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
        actions: [
          TextButton(
            onPressed: _submitting
                ? null
                : () => Navigator.of(context).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            key: const Key('mcpsRotateSubmit'),
            onPressed: _submitting ? null : _submit,
            child: Text(_submitting ? 'Rotating…' : 'Rotate token'),
          ),
        ],
      ),
    );
  }
}

class _PolicyPicker extends StatelessWidget {
  const _PolicyPicker({
    required this.policy,
    required this.present,
    required this.busy,
    required this.onChanged,
  });

  final ToolPolicy policy;
  final bool present;
  final bool busy;
  final ValueChanged<ToolPolicy?> onChanged;

  // A bounded max width so the dropdown row (item text + arrow icon) never
  // has to size itself to more than this, even at very narrow card widths.
  // `isExpanded` then lets the selected/hint text shrink to whatever's left
  // and ellipsize instead of overflowing the row.
  static const double _maxWidth = 168;

  @override
  Widget build(BuildContext context) {
    return ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: _maxWidth),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 9),
        decoration: BoxDecoration(
          color: _policyColor(policy).withValues(alpha: 0.13),
          borderRadius: BorderRadius.circular(20),
        ),
        child: DropdownButtonHideUnderline(
          child: DropdownButton<ToolPolicy>(
            value: !present || policy == ToolPolicy.unspecified ? null : policy,
            hint: Text(
              present ? 'Unknown policy' : 'Unavailable',
              overflow: TextOverflow.ellipsis,
              style: TextStyle(color: AppColors.warning, fontSize: 11.5),
            ),
            isDense: true,
            isExpanded: true,
            onChanged: present && !busy ? onChanged : null,
            items: const [
              DropdownMenuItem(
                value: ToolPolicy.safe,
                child: Text('Runs freely', overflow: TextOverflow.ellipsis),
              ),
              DropdownMenuItem(
                value: ToolPolicy.approvalRequired,
                child: Text('Asks first', overflow: TextOverflow.ellipsis),
              ),
              DropdownMenuItem(
                value: ToolPolicy.disabled,
                child: Text('Disabled', overflow: TextOverflow.ellipsis),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _ServerBadge extends StatelessWidget {
  const _ServerBadge({required this.label, this.tone});

  final String label;
  final Color? tone;

  @override
  Widget build(BuildContext context) {
    final color = tone ?? AppColors.of(context).textMuted;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(20),
      ),
      child: Text(label, style: TextStyle(fontSize: 11, color: color)),
    );
  }
}

Color _policyColor(ToolPolicy policy) => switch (policy) {
  ToolPolicy.safe => AppColors.success,
  ToolPolicy.approvalRequired => AppColors.warning,
  ToolPolicy.disabled => AppColors.danger,
  ToolPolicy.unspecified => AppColors.warning,
};

String _tierLabel(McpServerTier tier) => switch (tier) {
  McpServerTier.bundled => 'Bundled',
  McpServerTier.localContainer => 'Local third-party',
  McpServerTier.remoteUrl => 'Remote · per-run consent',
  McpServerTier.unspecified => 'Unknown tier',
};

String _livenessLabel(McpServer server) {
  if (!server.enabled) return 'Disabled';
  return switch (server.liveness) {
    McpServerLiveness.up => 'Up',
    McpServerLiveness.down => 'Down',
    McpServerLiveness.unknown => 'Not checked',
    McpServerLiveness.unspecified => 'Unknown status',
  };
}

/// Shared by every destination page, including ones in sibling files.
class WorkspaceLoading extends StatelessWidget {
  const WorkspaceLoading({super.key});

  @override
  Widget build(BuildContext context) => const Padding(
    padding: EdgeInsets.symmetric(vertical: 40),
    child: Center(
      child: SizedBox.square(
        dimension: 22,
        child: CircularProgressIndicator(strokeWidth: 2),
      ),
    ),
  );
}

class _PageError extends StatelessWidget {
  const _PageError({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) => WorkspaceNotice(
    icon: Icons.error_outline,
    title: 'Could not reach the backend',
    body: message,
    onRetry: onRetry,
    tone: AppColors.danger,
  );
}

class WorkspaceNotice extends StatelessWidget {
  const WorkspaceNotice({
    super.key,
    required this.icon,
    required this.title,
    required this.body,
    this.onRetry,
    this.tone,
  });

  final IconData icon;
  final String title;
  final String body;
  final VoidCallback? onRetry;
  final Color? tone;

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    final color = tone ?? palette.textMuted;
    return Container(
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: palette.raised,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: palette.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, size: 17, color: color),
              const SizedBox(width: 9),
              Expanded(
                child: Text(
                  title,
                  style: TextStyle(
                    fontSize: 13,
                    fontWeight: FontWeight.w600,
                    color: palette.text,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 11),
          Text(
            body,
            style: TextStyle(
              fontSize: 13.5,
              height: 1.6,
              color: palette.textMuted,
            ),
          ),
          if (onRetry != null) ...[
            const SizedBox(height: 14),
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                onPressed: onRetry,
                icon: const Icon(Icons.refresh, size: 16),
                label: const Text('Try again'),
              ),
            ),
          ],
        ],
      ),
    );
  }
}
