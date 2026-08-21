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

  Future<void> _setServerEnabled(McpServer server, bool enabled) async {
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
    try {
      await widget.apiClient.deleteMcpServer(serverId: server.serverId);
      if (mounted) _reload();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('$error')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return WorkspacePage(
      title: 'MCPs',
      subtitle:
          'Registered tool servers and their policies. Imported servers stay '
          'off until you enable them. Remote tools require the same per-run '
          'egress confirmation as any other destination off this machine; '
          'while a remote tool is enabled and offered to the model, every run '
          'asks before sending.',
      child: FutureBuilder<McpRegistrySnapshot>(
        future: _registry,
        builder: (context, snapshot) {
          if (snapshot.connectionState != ConnectionState.done) {
            return const WorkspaceLoading();
          }
          if (snapshot.hasError) {
            return _PageError(message: '${snapshot.error}', onRetry: _reload);
          }
          final registry =
              snapshot.data ??
              McpRegistrySnapshot(servers: const [], unsupported: const []);
          if (registry.servers.isEmpty && registry.unsupported.isEmpty) {
            return WorkspaceNotice(
              icon: Icons.hub_outlined,
              title: 'No tools discovered',
              body:
                  'Add entries to the mounted mcp.json file, then restart the '
                  'backend to import them.',
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
                  onPolicyChanged: (tool, policy) =>
                      _setToolPolicy(server, tool, policy),
                  pendingToolPolicies: _pendingToolPolicies,
                ),
                const SizedBox(height: 12),
              ],
            ],
          );
        },
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
    required this.onPolicyChanged,
    required this.pendingToolPolicies,
  });

  final McpServer server;
  final AppPalette palette;
  final ValueChanged<bool>? onEnabledChanged;
  final VoidCallback? onDelete;
  final void Function(ToolDescriptor tool, ToolPolicy policy) onPolicyChanged;
  final Set<String> pendingToolPolicies;

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
                Row(
                  children: [
                    Icon(Icons.dns_outlined, size: 16, color: AppColors.brand),
                    const SizedBox(width: 9),
                    Expanded(
                      child: Text(
                        server.name,
                        style: TextStyle(
                          fontSize: 14.5,
                          fontWeight: FontWeight.w600,
                          color: palette.text,
                        ),
                      ),
                    ),
                    Text(
                      tools.length == 1 ? '1 tool' : '${tools.length} tools',
                      style: TextStyle(
                        fontSize: 12.5,
                        color: palette.textMuted,
                      ),
                    ),
                    const SizedBox(width: 8),
                    Switch(value: server.enabled, onChanged: onEnabledChanged),
                    if (onDelete != null)
                      IconButton(
                        tooltip: 'Remove ${server.name}',
                        onPressed: onDelete,
                        icon: const Icon(Icons.delete_outline, size: 18),
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
                  Text(
                    tool.toolName,
                    style: TextStyle(
                      fontSize: 13.5,
                      fontFamily: 'monospace',
                      color: palette.text,
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

  @override
  Widget build(BuildContext context) {
    return Container(
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
            style: TextStyle(color: AppColors.warning, fontSize: 11.5),
          ),
          isDense: true,
          onChanged: present && !busy ? onChanged : null,
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
