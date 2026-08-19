import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/workspace/session_skills_bar.dart';
import 'package:turing_flutter_app/features/workspace/skills_page.dart';
import 'package:turing_flutter_app/models/agent_descriptor.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/skill.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';

void main() {
  group('the skill library', () {
    testWidgets('says what a skill is for when there are none', (tester) async {
      await _pumpSkills(tester, _SkillApi());

      expect(find.text('No skills yet'), findsOneWidget);
      // A bare empty list would leave the user with no idea what to type.
      expect(find.textContaining('standing instruction'), findsOneWidget);
    });

    testWidgets('writing a skill saves name and instructions', (tester) async {
      final api = _SkillApi();
      await _pumpSkills(tester, api);

      await tester.tap(find.text('New skill'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).at(0), '  Tone  ');
      await tester.enterText(find.byType(TextField).at(1), 'Be brief.');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      expect(api.created, [('Tone', 'Be brief.')]);
      expect(find.text('Tone'), findsOneWidget);
    });

    testWidgets(
      'a skill with no instructions is refused before the round trip',
      (tester) async {
        final api = _SkillApi();
        await _pumpSkills(tester, api);

        await tester.tap(find.text('New skill'));
        await tester.pumpAndSettle();
        await tester.enterText(find.byType(TextField).at(0), 'Named');
        await tester.tap(find.text('Save'));
        await tester.pumpAndSettle();

        expect(
          find.textContaining('needs both a name and instructions'),
          findsOneWidget,
        );
        expect(api.created, isEmpty, reason: 'nothing was sent to the backend');
      },
    );

    testWidgets('a rejected name is shown in the dialog, not swallowed', (
      tester,
    ) async {
      final api = _SkillApi()..createError = StateError('name already exists');
      await _pumpSkills(tester, api);

      await tester.tap(find.text('New skill'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).at(0), 'Tone');
      await tester.enterText(find.byType(TextField).at(1), 'Be brief.');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      expect(find.textContaining('name already exists'), findsOneWidget);
      // The dialog stays open so the typed instructions are not lost.
      expect(find.text('New skill'), findsWidgets);
    });

    testWidgets('editing an existing skill sends the update', (tester) async {
      final api = _SkillApi()
        ..skills.add(
          const Skill(
            skillId: 'skill_1',
            name: 'Tone',
            instructions: 'Be brief.',
          ),
        );
      await _pumpSkills(tester, api);

      await tester.tap(find.byTooltip('Edit skill'));
      await tester.pumpAndSettle();
      // Pre-filled, or editing means retyping from memory.
      expect(find.text('Tone'), findsWidgets);
      expect(find.text('Be brief.'), findsWidgets);

      await tester.enterText(find.byType(TextField).at(1), 'Write at length.');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      expect(api.updated, [('skill_1', 'Tone', 'Write at length.')]);
      expect(find.text('Write at length.'), findsOneWidget);
    });

    testWidgets('confirming the delete removes it from the list', (
      tester,
    ) async {
      final api = _SkillApi()
        ..skills.add(
          const Skill(
            skillId: 'skill_1',
            name: 'Tone',
            instructions: 'Be brief.',
          ),
        );
      await _pumpSkills(tester, api);

      await tester.tap(find.byTooltip('Delete skill'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(TextButton, 'Delete'));
      await tester.pumpAndSettle();

      expect(api.deleted, ['skill_1']);
      expect(find.text('No skills yet'), findsOneWidget);
    });

    testWidgets('deleting warns that it detaches everywhere', (tester) async {
      final api = _SkillApi()
        ..skills.add(
          const Skill(
            skillId: 'skill_1',
            name: 'Tone',
            instructions: 'Be brief.',
          ),
        );
      await _pumpSkills(tester, api);

      await tester.tap(find.byTooltip('Delete skill'));
      await tester.pumpAndSettle();

      expect(
        find.textContaining('every conversation using it'),
        findsOneWidget,
      );
      // The snapshot guarantee is the non-obvious part, so it is stated.
      expect(find.textContaining('already being generated'), findsOneWidget);

      await tester.tap(find.widgetWithText(TextButton, 'Cancel'));
      await tester.pumpAndSettle();
      expect(api.deleted, isEmpty);
    });
  });

  group('attaching skills to a conversation', () {
    testWidgets('the bar names the attached skills', (tester) async {
      final api = _SkillApi()
        ..skills.addAll(const [
          Skill(skillId: 'skill_1', name: 'Tone', instructions: 'Be brief.'),
          Skill(skillId: 'skill_2', name: 'Format', instructions: 'Bullets.'),
        ])
        ..attached['sess_1'] = ['skill_1', 'skill_2'];
      await _pumpBar(tester, api);

      expect(find.text('Tone, Format'), findsOneWidget);
    });

    testWidgets('with nothing attached it stays present but quiet', (
      tester,
    ) async {
      await _pumpBar(tester, _SkillApi());

      // Present, or attaching a skill would be undiscoverable.
      expect(find.text('No skills attached'), findsOneWidget);
      expect(find.text('Change'), findsOneWidget);
    });

    testWidgets('ticking a skill attaches it and the bar follows', (
      tester,
    ) async {
      final api = _SkillApi()
        ..skills.add(
          const Skill(
            skillId: 'skill_1',
            name: 'Tone',
            instructions: 'Be brief.',
          ),
        );
      await _pumpBar(tester, api);
      expect(find.text('No skills attached'), findsOneWidget);

      await tester.tap(find.text('Change'));
      await tester.pumpAndSettle();
      await tester.tap(find.byType(CheckboxListTile));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Done'));
      await tester.pumpAndSettle();

      expect(api.attached['sess_1'], ['skill_1']);
      expect(find.text('Tone'), findsOneWidget);
    });

    testWidgets('unticking detaches it', (tester) async {
      final api = _SkillApi()
        ..skills.add(
          const Skill(
            skillId: 'skill_1',
            name: 'Tone',
            instructions: 'Be brief.',
          ),
        )
        ..attached['sess_1'] = ['skill_1'];
      await _pumpBar(tester, api);

      await tester.tap(find.text('Change'));
      await tester.pumpAndSettle();
      await tester.tap(find.byType(CheckboxListTile));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Done'));
      await tester.pumpAndSettle();

      expect(api.attached['sess_1'], isEmpty);
      expect(find.text('No skills attached'), findsOneWidget);
    });

    testWidgets('an empty library points at where skills are written', (
      tester,
    ) async {
      await _pumpBar(tester, _SkillApi());

      await tester.tap(find.text('Change'));
      await tester.pumpAndSettle();

      expect(
        find.textContaining('have not written any skills'),
        findsOneWidget,
      );
    });

    testWidgets('a failed toggle says so inside the sheet', (tester) async {
      final api = _SkillApi()
        ..skills.add(
          const Skill(
            skillId: 'skill_1',
            name: 'Tone',
            instructions: 'Be brief.',
          ),
        )
        ..attachError = const _Offline();
      await _pumpBar(tester, api);

      await tester.tap(find.text('Change'));
      await tester.pumpAndSettle();
      await tester.tap(find.byType(CheckboxListTile));
      await tester.pumpAndSettle();

      // A snackbar would render in the Scaffold underneath this modal sheet,
      // so the user would see the tick fail to move and be told nothing.
      expect(find.textContaining('Could not change that'), findsOneWidget);
    });

    testWidgets('a failed library read is not reported as an empty library', (
      tester,
    ) async {
      final api = _SkillApi()..listSkillsError = const _Offline();
      await _pumpBar(tester, api);

      await tester.tap(find.text('Change'));
      await tester.pumpAndSettle();

      expect(find.textContaining('Could not load your skills'), findsOneWidget);
      expect(
        find.textContaining('have not written any skills'),
        findsNothing,
        reason: 'telling them to write one they may already have',
      );
    });

    testWidgets('a failed read offers a retry rather than vanishing', (
      tester,
    ) async {
      // An Exception, not an Error: a real transport failure is a GrpcError,
      // and the bar deliberately catches only Exception so a programming bug
      // still surfaces instead of being swallowed as "offline".
      final api = _SkillApi()..listSessionSkillsError = const _Offline();
      await _pumpBar(tester, api);

      // Hiding the bar outright would leave a conversation applying standing
      // instructions with no way to see or change them.
      expect(find.textContaining('Could not load'), findsOneWidget);
      expect(find.text('Retry'), findsOneWidget);

      api.listSessionSkillsError = null;
      api
        ..skills.add(
          const Skill(
            skillId: 'skill_1',
            name: 'Tone',
            instructions: 'Be brief.',
          ),
        )
        ..attached['sess_1'] = ['skill_1'];
      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();

      expect(find.text('Tone'), findsOneWidget);
    });

    testWidgets('the sheet does not overflow a short landscape phone', (
      tester,
    ) async {
      final api = _SkillApi()
        ..skills.addAll([
          for (var i = 0; i < 8; i++)
            Skill(
              skillId: 'skill_$i',
              name: 'Skill $i',
              instructions: 'Instructions $i',
            ),
        ]);
      await _pumpBar(tester, api, size: const Size(740, 360));

      await tester.tap(find.text('Change'));
      await tester.pumpAndSettle();

      expect(tester.takeException(), isNull);
    });
  });
}

Future<void> _pumpSkills(WidgetTester tester, _SkillApi api) async {
  tester.view.physicalSize = const Size(1200, 900);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(body: SkillsPage(apiClient: api)),
    ),
  );
  await tester.pumpAndSettle();
}

Future<void> _pumpBar(
  WidgetTester tester,
  _SkillApi api, {
  Size size = const Size(1200, 900),
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: SessionSkillsBar(apiClient: api, sessionId: 'sess_1'),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

class _Offline implements Exception {
  const _Offline();

  @override
  String toString() => 'offline';
}

class _SkillApi implements TuringApi {
  final List<Skill> skills = [];
  final Map<String, List<String>> attached = {};
  final List<(String, String)> created = [];
  final List<(String, String, String)> updated = [];
  final List<String> deleted = [];
  Object? createError;
  Object? attachError;
  Object? listSkillsError;
  Object? listSessionSkillsError;
  int nextId = 100;

  @override
  Future<List<Skill>> listSkills() async {
    final error = listSkillsError;
    if (error != null) throw error;
    return List.unmodifiable(skills);
  }

  @override
  Future<List<Skill>> listSessionSkills({required String sessionId}) async {
    final error = listSessionSkillsError;
    if (error != null) throw error;
    final ids = attached[sessionId] ?? const <String>[];
    return skills.where((skill) => ids.contains(skill.skillId)).toList();
  }

  @override
  Future<Skill> createSkill({
    required String name,
    required String instructions,
  }) async {
    final error = createError;
    if (error != null) throw error;
    created.add((name, instructions));
    final skill = Skill(
      skillId: 'skill_${nextId++}',
      name: name,
      instructions: instructions,
    );
    skills.add(skill);
    return skill;
  }

  @override
  Future<Skill> updateSkill({
    required String skillId,
    required String name,
    required String instructions,
  }) async {
    updated.add((skillId, name, instructions));
    final index = skills.indexWhere((skill) => skill.skillId == skillId);
    final replacement = Skill(
      skillId: skillId,
      name: name,
      instructions: instructions,
    );
    if (index >= 0) skills[index] = replacement;
    return replacement;
  }

  @override
  Future<void> deleteSkill({required String skillId}) async {
    deleted.add(skillId);
    skills.removeWhere((skill) => skill.skillId == skillId);
  }

  @override
  Future<List<Skill>> attachSkill({
    required String sessionId,
    required String skillId,
  }) async {
    final error = attachError;
    if (error != null) throw error;
    final ids = attached.putIfAbsent(sessionId, () => []);
    if (!ids.contains(skillId)) ids.add(skillId);
    return listSessionSkills(sessionId: sessionId);
  }

  @override
  Future<List<Skill>> detachSkill({
    required String sessionId,
    required String skillId,
  }) async {
    attached[sessionId]?.remove(skillId);
    return listSessionSkills(sessionId: sessionId);
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
  Future<void> deleteSession({required String sessionId}) async {}

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
