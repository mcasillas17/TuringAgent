import 'package:fixnum/fixnum.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart';
import 'package:turing_flutter_app/generated/turing/v1/attention.pb.dart'
    as attentionpb;
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/generated/turing/v1/events.pb.dart'
    as eventpb;
import 'package:turing_flutter_app/models/attention.dart';
import 'package:turing_flutter_app/models/grpc_mappers.dart';

void main() {
  attentionpb.AttentionRow approvalRow() => attentionpb.AttentionRow(
    stage: attentionpb.AttentionStage.ATTENTION_STAGE_PERSON_APPROVAL,
    kind: attentionpb.AttentionSourceKind.ATTENTION_SOURCE_KIND_APPROVAL,
    since: Timestamp(seconds: Int64(1795000000), nanos: 5000),
    sessionId: 'sess_1',
    sessionTitle: 'Chat',
    approvalId: 'appr_1',
    runId: 'run_1',
  );

  test('a page carries its rows in order and its next cursor', () {
    final page = GrpcMappers.attentionPageToModel(
      attentionpb.ListAttentionResponse(
        rows: [
          approvalRow(),
          attentionpb.AttentionRow(
            stage: attentionpb.AttentionStage.ATTENTION_STAGE_PERSON_BLOCKED,
            kind: attentionpb
                .AttentionSourceKind
                .ATTENTION_SOURCE_KIND_AUTOMATION,
            since: Timestamp(seconds: Int64(1794000000)),
            sessionId: 'sess_auto',
            runId: 'run_auto',
            automationId: 'auto_1',
            automationName: 'Digest',
            occurrenceKey: 'run_auto',
            cause: 'automation_grant_revoked',
          ),
        ],
        page: commonpb.PageResponse(nextCursor: 'cursor-2'),
      ),
    );

    expect(page.nextCursor, 'cursor-2');
    expect(page.rows.map((row) => row.stage), [
      AttentionStage.approval,
      AttentionStage.blocked,
    ]);
    final approval = page.rows.first;
    expect(approval.kind, AttentionSourceKind.approval);
    expect(approval.approvalId, 'appr_1');
    expect(approval.runId, 'run_1');
    expect(approval.sessionTitle, 'Chat');
    expect(
      approval.since,
      DateTime.fromMicrosecondsSinceEpoch(1795000000000005, isUtc: true),
    );
    final automation = page.rows.last;
    expect(automation.automationName, 'Digest');
    expect(automation.occurrenceKey, 'run_auto');
    expect(automation.cause, 'automation_grant_revoked');
  });

  test('an empty next cursor means the last page', () {
    final page = GrpcMappers.attentionPageToModel(
      attentionpb.ListAttentionResponse(rows: [approvalRow()]),
    );
    expect(page.nextCursor, isNull);
  });

  test('a stage this build does not know is unavailable, not dropped', () {
    final row = approvalRow()..unknownFields.mergeVarintField(1, Int64(42));
    row.clearStage();
    final model = GrpcMappers.attentionRowToModel(row);
    expect(model.stage, AttentionStage.unavailable);
    expect(model.approvalId, 'appr_1');
  });

  test('a kind this build does not know is unavailable', () {
    final row = approvalRow()..clearKind();
    row.unknownFields.mergeVarintField(2, Int64(42));
    expect(
      GrpcMappers.attentionRowToModel(row).stage,
      AttentionStage.unavailable,
    );
  });

  test('a row missing the ids its action needs is unavailable', () {
    final cases = <attentionpb.AttentionRow>[
      approvalRow()..approvalId = '',
      approvalRow()..runId = '',
      approvalRow()
        ..kind = attentionpb.AttentionSourceKind.ATTENTION_SOURCE_KIND_DELETION,
      attentionpb.AttentionRow(
        stage: attentionpb.AttentionStage.ATTENTION_STAGE_PERSON_CONSENT,
        kind: attentionpb.AttentionSourceKind.ATTENTION_SOURCE_KIND_DELEGATION,
        sessionId: 'sess_parent',
      ),
      attentionpb.AttentionRow(
        stage: attentionpb.AttentionStage.ATTENTION_STAGE_PERSON_BLOCKED,
        kind: attentionpb.AttentionSourceKind.ATTENTION_SOURCE_KIND_AUTOMATION,
        automationId: 'auto_1',
      ),
      attentionpb.AttentionRow(
        stage: attentionpb.AttentionStage.ATTENTION_STAGE_PERSON_DELETION,
        kind: attentionpb.AttentionSourceKind.ATTENTION_SOURCE_KIND_DELETION,
      ),
      attentionpb.AttentionRow(
        stage: attentionpb.AttentionStage.ATTENTION_STAGE_UNSPECIFIED,
        kind: attentionpb.AttentionSourceKind.ATTENTION_SOURCE_KIND_APPROVAL,
        approvalId: 'appr_1',
        runId: 'run_1',
      ),
    ];
    for (final row in cases) {
      expect(
        GrpcMappers.attentionRowToModel(row).stage,
        AttentionStage.unavailable,
        reason: row.toString(),
      );
    }
  });

  test('a row without since keeps its stage', () {
    final model = GrpcMappers.attentionRowToModel(approvalRow()..clearSince());
    expect(model.stage, AttentionStage.approval);
    expect(model.since, isNull);
  });

  test('the content-free hint has its own type name', () {
    expect(
      GrpcMappers.eventTypeToString(
        eventpb.TuringEventType.TURING_EVENT_TYPE_ATTENTION_CHANGED,
      ),
      'attention.changed',
    );
  });
}
