import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/workspace/skills_page.dart';
import 'package:turing_flutter_app/models/agent_descriptor.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/skill.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';

import '../support/no_audit_api.dart';
import '../support/no_automations_api.dart';
import '../support/no_external_agents_api.dart';
import '../support/no_integrations_api.dart';
import '../support/no_telemetry_api.dart';

void main() {
  testWidgets('empty library points to the real editor folder', (tester) async {
    await _pumpSkills(tester, _SkillApi());

    expect(find.text('No skill files found'), findsOneWidget);
    expect(find.textContaining('turing-backend/skills'), findsWidgets);
    expect(find.text('New skill'), findsNothing);
  });

  testWidgets('browser shows path metadata body and parse errors', (
    tester,
  ) async {
    final api = _SkillApi()
      ..skills.addAll([
        _skill(body: 'Be brief.', version: '2.1', author: 'Ada'),
        _skill(
          id: 'writing/broken',
          name: 'Broken',
          description: '',
          parseError: 'frontmatter description is required',
        ),
      ]);
    await _pumpSkills(tester, api);

    expect(find.text('writing/tone'), findsOneWidget);
    expect(find.text('Be brief.'), findsOneWidget);
    expect(find.textContaining('Version 2.1'), findsOneWidget);
    expect(find.textContaining('Ada'), findsOneWidget);
    expect(find.textContaining('description is required'), findsOneWidget);
  });

  testWidgets('enabling does not grant a declared capability', (tester) async {
    final api = _SkillApi()
      ..skills.add(
        _skill(
          requires: const ['files.update'],
          missing: const ['files.update'],
        ),
      );
    await _pumpSkills(tester, api);

    await tester.tap(find.byType(Switch));
    await tester.pumpAndSettle();

    expect(api.enableCalls, [('writing/tone', true)]);
    expect(api.grantCalls, isEmpty);
    expect(
      find.textContaining('Withheld until every capability'),
      findsOneWidget,
    );
  });

  testWidgets('capability consent uses its own mutation', (tester) async {
    final api = _SkillApi()
      ..skills.add(
        _skill(
          enabled: true,
          requires: const ['files.update'],
          missing: const ['files.update'],
        ),
      );
    await _pumpSkills(tester, api);

    await tester.tap(find.byType(Checkbox));
    await tester.pumpAndSettle();

    expect(api.grantCalls, [('writing/tone', 'files.update', true)]);
    expect(find.text('Ready to load'), findsOneWidget);
  });

  testWidgets('backend failure is not reported as an empty folder', (
    tester,
  ) async {
    final api = _SkillApi()..listError = StateError('offline');
    await _pumpSkills(tester, api);

    expect(find.text('Could not reach the backend'), findsOneWidget);
    expect(find.text('No skill files found'), findsNothing);
    expect(find.text('Try again'), findsOneWidget);
  });

  testWidgets('browser does not overflow compact landscape layouts', (
    tester,
  ) async {
    final api = _SkillApi()
      ..skills.addAll([
        for (var index = 0; index < 8; index++)
          _skill(
            id: 'category/skill-$index',
            name: 'Skill $index',
            body: 'A body that remains readable in compact layouts.',
            requires: const ['files.read', 'files.update'],
            missing: const ['files.read', 'files.update'],
          ),
      ]);
    await _pumpSkills(tester, api, size: const Size(740, 360));

    expect(tester.takeException(), isNull);
  });
}

Skill _skill({
  String id = 'writing/tone',
  String name = 'Tone',
  String description = 'Keeps prose direct',
  String body = '',
  String version = '',
  String author = '',
  bool enabled = false,
  List<String> requires = const [],
  List<String> granted = const [],
  List<String> missing = const [],
  String parseError = '',
}) => Skill(
  skillId: id,
  name: name,
  description: description,
  body: body,
  category: id.split('/').first,
  version: version,
  author: author,
  license: '',
  requires: requires,
  grantedCapabilities: granted,
  missingCapabilities: missing,
  enabled: enabled,
  parseError: parseError,
  folderPath: '/skills/$id',
);

Future<void> _pumpSkills(
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
      home: Scaffold(body: SkillsPage(apiClient: api)),
    ),
  );
  await tester.pumpAndSettle();
}

class _SkillApi
    with
        NoAuditApi,
        NoExternalAgentsApi,
        NoIntegrationsApi,
        NoAutomationsApi,
        NoTelemetryApi
    implements TuringApi {
  final List<Skill> skills = [];
  final List<(String, bool)> enableCalls = [];
  final List<(String, String, bool)> grantCalls = [];
  Object? listError;

  @override
  Future<List<Skill>> listSkills() async {
    if (listError case final error?) throw error;
    return List.unmodifiable(skills);
  }

  @override
  Future<Skill> getSkill({required String skillId}) async =>
      skills.singleWhere((skill) => skill.skillId == skillId);

  @override
  Future<Skill> setSkillEnabled({
    required String skillId,
    required bool enabled,
  }) async {
    enableCalls.add((skillId, enabled));
    return _replace(skillId, (skill) => skill.copyWith(enabled: enabled));
  }

  @override
  Future<Skill> setSkillCapabilityGrant({
    required String skillId,
    required String capability,
    required bool granted,
  }) async {
    grantCalls.add((skillId, capability, granted));
    return _replace(skillId, (skill) {
      final grants = {...skill.grantedCapabilities};
      final missing = {...skill.missingCapabilities};
      if (granted) {
        grants.add(capability);
        missing.remove(capability);
      } else {
        grants.remove(capability);
        missing.add(capability);
      }
      return skill.copyWith(
        grantedCapabilities: grants.toList()..sort(),
        missingCapabilities: missing.toList()..sort(),
      );
    });
  }

  Skill _replace(String id, Skill Function(Skill) update) {
    final index = skills.indexWhere((skill) => skill.skillId == id);
    skills[index] = update(skills[index]);
    return skills[index];
  }

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

  @override
  Future<List<ToolDescriptor>> listTools() async => const [];

  @override
  Future<List<AgentDescriptor>> listAgents() async => const [];
}
