import 'package:flutter/material.dart';

/// The top-level places in the app.
///
/// Three of these are backed by something the backend actually does today
/// ([chats], [mcps], [agents]); the rest are named here because they are the
/// intended shape of the product, and their views say plainly that they are
/// not built yet. A destination that looks functional but silently does
/// nothing is worse than one that admits what it is.
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
    implemented: false,
    summary:
        'Reusable instructions you can hand the agent — a checklist it should '
        'follow, a format it should always write in, a procedure it should '
        'run the same way every time.',
    blockedOn:
        'The agent takes a single system prompt today. Skills need somewhere '
        'to store them, a way to pick which apply to a conversation, and a '
        'way to compose several without them contradicting each other.',
  ),
  integrations(
    label: 'Integrations',
    icon: Icons.extension_outlined,
    selectedIcon: Icons.extension,
    implemented: false,
    summary:
        'Accounts and services the agent can reach on your behalf — mail, '
        'calendars, repositories, issue trackers.',
    blockedOn:
        'Nothing here yet holds credentials for a third party. That needs a '
        'place to keep tokens, a consent step before first use, and a way to '
        'revoke access — none of which should be improvised.',
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
    implemented: false,
    summary:
        'Work that runs without you starting it — on a schedule, or when '
        'something changes.',
    blockedOn:
        'Runs are only ever created by you sending a message. A scheduler '
        'would need to create them on its own, and unattended runs raise a '
        'question the approval flow currently answers by asking you: what '
        'happens when a tool needs approval and nobody is watching.',
  ),
  agents(
    label: 'Agents',
    icon: Icons.groups_outlined,
    selectedIcon: Icons.groups,
    implemented: true,
  );

  const ShellDestination({
    required this.label,
    required this.icon,
    required this.selectedIcon,
    required this.implemented,
    this.summary,
    this.blockedOn,
  });

  final String label;
  final IconData icon;
  final IconData selectedIcon;

  /// Whether this view shows real backend state. False means the view
  /// describes an intention.
  final bool implemented;

  /// What this destination is meant to do, in the user's terms.
  final String? summary;

  /// Why it does not exist yet. Stated so the gap is legible instead of
  /// looking like an oversight.
  final String? blockedOn;

  /// The destinations listed above the conversation list, in declaration
  /// order. Derived rather than hand-listed: a destination added to the enum
  /// but forgotten here would be unreachable, and nothing would fail.
  /// [chats] is excluded because the conversation list is its own navigation.
  static List<ShellDestination> get navigation =>
      values.where((d) => d != chats).toList(growable: false);
}
