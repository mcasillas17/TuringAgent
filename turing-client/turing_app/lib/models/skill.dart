/// A named block of instructions the user attaches to the conversations where
/// it applies.
///
/// Attachment is explicit — the agent never picks skills for you — so what is
/// attached to a conversation is exactly what someone attached by hand.
class Skill {
  const Skill({
    required this.skillId,
    required this.name,
    required this.instructions,
  });

  final String skillId;
  final String name;
  final String instructions;
}
