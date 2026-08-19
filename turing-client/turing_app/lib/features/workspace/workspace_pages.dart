import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../models/agent_descriptor.dart';
import '../../models/tool_descriptor.dart';
import '../../networking/api_client.dart';
import '../../ui/shell/shell_destination.dart';

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

/// The view for a destination that is named but not built.
///
/// It states what the feature is for and what stands in the way, rather than
/// showing an empty list that looks broken or a mock that implies it works.
class PlannedDestinationPage extends StatelessWidget {
  const PlannedDestinationPage({super.key, required this.destination});

  final ShellDestination destination;

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return WorkspacePage(
      title: destination.label,
      subtitle: destination.summary ?? '',
      child: Container(
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
                Icon(
                  Icons.construction_outlined,
                  size: 17,
                  color: AppColors.warning,
                ),
                const SizedBox(width: 9),
                Text(
                  'Not built yet',
                  style: TextStyle(
                    fontSize: 13,
                    fontWeight: FontWeight.w600,
                    color: palette.text,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 11),
            Text(
              destination.blockedOn ?? '',
              style: TextStyle(
                fontSize: 13.5,
                height: 1.6,
                color: palette.textMuted,
              ),
            ),
          ],
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
  late Future<List<ToolDescriptor>> _tools;

  @override
  void initState() {
    super.initState();
    _tools = widget.apiClient.listTools();
  }

  void _reload() {
    setState(() {
      _tools = widget.apiClient.listTools();
    });
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return WorkspacePage(
      title: 'MCPs',
      subtitle:
          'The tools the backend discovered, grouped by the server offering '
          'them. These servers run alongside the backend and are not '
          'reachable from outside your machine. A server that is not running '
          'contributes no tools, so it does not appear here at all.',
      child: FutureBuilder<List<ToolDescriptor>>(
        future: _tools,
        builder: (context, snapshot) {
          if (snapshot.connectionState != ConnectionState.done) {
            return const WorkspaceLoading();
          }
          if (snapshot.hasError) {
            return _PageError(message: '${snapshot.error}', onRetry: _reload);
          }
          final tools = snapshot.data ?? const <ToolDescriptor>[];
          if (tools.isEmpty) {
            return WorkspaceNotice(
              icon: Icons.hub_outlined,
              title: 'No tools discovered',
              body:
                  'The backend has not registered any MCP tools. If the stack '
                  'is running, the tool servers may still be starting up.',
              onRetry: _reload,
            );
          }
          final servers = <String, List<ToolDescriptor>>{};
          for (final tool in tools) {
            servers.putIfAbsent(tool.serverName, () => []).add(tool);
          }
          final names = servers.keys.toList()..sort();
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (final name in names) ...[
                _ServerCard(
                  serverName: name,
                  tools: servers[name]!
                    ..sort((a, b) => a.toolName.compareTo(b.toolName)),
                  palette: palette,
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
    required this.serverName,
    required this.tools,
    required this.palette,
  });

  final String serverName;
  final List<ToolDescriptor> tools;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
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
            child: Row(
              children: [
                Icon(Icons.dns_outlined, size: 16, color: AppColors.brand),
                const SizedBox(width: 9),
                Expanded(
                  child: Text(
                    serverName,
                    style: TextStyle(
                      fontSize: 14.5,
                      fontWeight: FontWeight.w600,
                      color: palette.text,
                    ),
                  ),
                ),
                Text(
                  tools.length == 1 ? '1 tool' : '${tools.length} tools',
                  style: TextStyle(fontSize: 12.5, color: palette.textMuted),
                ),
              ],
            ),
          ),
          Divider(height: 1, color: palette.border),
          for (final tool in tools)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 11, 16, 11),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      tool.toolName,
                      style: TextStyle(
                        fontSize: 13.5,
                        fontFamily: 'monospace',
                        color: palette.text,
                      ),
                    ),
                  ),
                  const SizedBox(width: 12),
                  _PolicyChip(policy: tool.policy),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

/// The policy is the part that matters operationally: it is the difference
/// between a tool that runs silently and one that stops to ask you.
class _PolicyChip extends StatelessWidget {
  const _PolicyChip({required this.policy});

  final ToolPolicy policy;

  @override
  Widget build(BuildContext context) {
    final (label, color) = switch (policy) {
      ToolPolicy.safe => ('Runs freely', AppColors.success),
      ToolPolicy.approvalRequired => ('Asks first', AppColors.warning),
      ToolPolicy.disabled => ('Disabled', AppColors.danger),
      ToolPolicy.unspecified => ('Unknown policy', AppColors.warning),
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

/// The agents the backend will route a run to.
class AgentsPage extends StatefulWidget {
  const AgentsPage({super.key, required this.apiClient});

  final TuringApi apiClient;

  @override
  State<AgentsPage> createState() => _AgentsPageState();
}

class _AgentsPageState extends State<AgentsPage> {
  late Future<List<AgentDescriptor>> _agents;

  @override
  void initState() {
    super.initState();
    _agents = widget.apiClient.listAgents();
  }

  void _reload() {
    setState(() {
      _agents = widget.apiClient.listAgents();
    });
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return WorkspacePage(
      title: 'Agents',
      subtitle: 'Who your messages are routed to.',
      child: FutureBuilder<List<AgentDescriptor>>(
        future: _agents,
        builder: (context, snapshot) {
          if (snapshot.connectionState != ConnectionState.done) {
            return const WorkspaceLoading();
          }
          if (snapshot.hasError) {
            return _PageError(message: '${snapshot.error}', onRetry: _reload);
          }
          final agents = snapshot.data ?? const <AgentDescriptor>[];
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (final agent in agents)
                Padding(
                  padding: const EdgeInsets.only(bottom: 10),
                  child: Container(
                    padding: const EdgeInsets.all(16),
                    decoration: BoxDecoration(
                      color: palette.surface,
                      borderRadius: BorderRadius.circular(12),
                      border: Border.all(color: palette.border),
                    ),
                    child: Row(
                      children: [
                        Icon(
                          Icons.smart_toy_outlined,
                          size: 18,
                          color: AppColors.brand,
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                agent.displayName,
                                style: TextStyle(
                                  fontSize: 14.5,
                                  fontWeight: FontWeight.w600,
                                  color: palette.text,
                                ),
                              ),
                              const SizedBox(height: 3),
                              Text(
                                'Runs locally through your configured model '
                                'provider.',
                                style: TextStyle(
                                  fontSize: 12.5,
                                  color: palette.textMuted,
                                ),
                              ),
                            ],
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              const SizedBox(height: 8),
              // Said explicitly because the single row above otherwise reads
              // as a list that failed to load the rest.
              WorkspaceNotice(
                icon: Icons.alt_route_outlined,
                title: 'Routing to other agents is not built yet',
                body:
                    'Handing a conversation to a different assistant — a '
                    'hosted model, or a second local one — needs the backend '
                    'to address more than one agent: per-agent capacity, '
                    'routing on the run, and a tool policy per agent. Today '
                    'every run goes to the one above.',
              ),
            ],
          );
        },
      ),
    );
  }
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
