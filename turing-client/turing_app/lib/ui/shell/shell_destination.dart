import 'package:flutter/material.dart';

/// The top-level places in the app.
///
/// Every one of these is backed by something the backend actually does. That
/// is the rule, not an accident of timing: a destination that looks functional
/// but silently does nothing is worse than one that admits what it is. While
/// some were unbuilt, they carried the copy explaining what was missing and
/// rendered it through a PlannedDestinationPage; both are in the history if a
/// future destination needs to ship before its backend does.
enum ShellDestination {
  chats(
    label: 'Chats',
    icon: Icons.forum_outlined,
    selectedIcon: Icons.forum,
    implemented: true,
  ),
  skills(
    label: 'Skills',
    icon: Icons.auto_awesome_outlined,
    selectedIcon: Icons.auto_awesome,
    implemented: true,
  ),
  memory(
    label: 'Memory',
    icon: Icons.psychology_outlined,
    selectedIcon: Icons.psychology,
    implemented: true,
  ),
  integrations(
    label: 'Integrations',
    icon: Icons.extension_outlined,
    selectedIcon: Icons.extension,
    implemented: true,
  ),
  mcps(
    label: 'MCPs',
    icon: Icons.hub_outlined,
    selectedIcon: Icons.hub,
    implemented: true,
  ),
  automations(
    label: 'Automations',
    icon: Icons.schedule_outlined,
    selectedIcon: Icons.schedule,
    implemented: true,
  ),
  agents(
    label: 'Agents',
    icon: Icons.groups_outlined,
    selectedIcon: Icons.groups,
    implemented: true,
  ),
  telemetry(
    label: 'Telemetry',
    icon: Icons.insights_outlined,
    selectedIcon: Icons.insights,
    implemented: true,
  );

  const ShellDestination({
    required this.label,
    required this.icon,
    required this.selectedIcon,
    required this.implemented,
  });

  final String label;
  final IconData icon;
  final IconData selectedIcon;

  /// Whether this view shows real backend state. Every destination sets this
  /// true today; a false one would need somewhere honest to say why, which is
  /// what the removed PlannedDestinationPage did.
  final bool implemented;

  /// The destinations listed above the conversation list, in declaration
  /// order. Derived rather than hand-listed: a destination added to the enum
  /// but forgotten here would be unreachable, and nothing would fail.
  /// [chats] is excluded because the conversation list is its own navigation.
  static List<ShellDestination> get navigation =>
      values.where((d) => d != chats).toList(growable: false);
}
