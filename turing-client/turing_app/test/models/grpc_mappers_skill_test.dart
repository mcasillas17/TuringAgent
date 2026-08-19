import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/generated/turing/v1/skills.pb.dart'
    as skillpb;
import 'package:turing_flutter_app/models/grpc_mappers.dart';

void main() {
  test('skill mapper preserves file metadata and decision state', () {
    final model = GrpcMappers.skillToModel(
      skillpb.Skill(
        skillId: 'writing/tone',
        name: 'Tone',
        description: 'Keeps prose direct',
        body: 'Be brief.',
        category: 'writing',
        version: '2.1',
        author: 'Ada',
        license: 'MIT',
        requires: ['files.update'],
        grantedCapabilities: ['files.read'],
        missingCapabilities: ['files.update'],
        enabled: true,
        parseError: 'example error',
        folderPath: '/skills/writing/tone',
      ),
    );

    expect(model.skillId, 'writing/tone');
    expect(model.name, 'Tone');
    expect(model.description, 'Keeps prose direct');
    expect(model.body, 'Be brief.');
    expect(model.category, 'writing');
    expect(model.version, '2.1');
    expect(model.author, 'Ada');
    expect(model.license, 'MIT');
    expect(model.requires, ['files.update']);
    expect(model.grantedCapabilities, ['files.read']);
    expect(model.missingCapabilities, ['files.update']);
    expect(model.enabled, isTrue);
    expect(model.parseError, 'example error');
    expect(model.folderPath, '/skills/writing/tone');
  });
}
