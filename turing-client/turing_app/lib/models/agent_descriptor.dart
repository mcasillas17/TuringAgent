/// An agent the backend can route a run to.
///
/// Today the backend reports exactly one. Routing a conversation to a
/// different agent — a hosted model, or a second local one — is a planned
/// capability, not a shipped one, so the Agents view says so rather than
/// implying a picker that does nothing.
class AgentDescriptor {
  const AgentDescriptor({required this.id, required this.displayName});

  final String id;
  final String displayName;
}
