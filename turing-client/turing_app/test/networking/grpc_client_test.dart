import 'package:flutter_test/flutter_test.dart';
import 'package:fixnum/fixnum.dart';
import 'package:grpc/grpc.dart' as grpc;
import 'package:turing_flutter_app/generated/google/protobuf/timestamp.pb.dart'
    as timestamppb;
import 'package:turing_flutter_app/generated/turing/v1/audit.pb.dart'
    as auditpb;
import 'package:turing_flutter_app/generated/turing/v1/audit.pbgrpc.dart'
    as auditgrpc;
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/generated/turing/v1/events.pb.dart'
    as eventpb;
import 'package:turing_flutter_app/generated/turing/v1/events.pbgrpc.dart'
    as eventgrpc;
import 'package:turing_flutter_app/generated/turing/v1/sessions.pb.dart'
    as sessionpb;
import 'package:turing_flutter_app/generated/turing/v1/sessions.pbgrpc.dart'
    as sessiongrpc;
import 'package:turing_flutter_app/models/audit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/session_page.dart';
import 'package:turing_flutter_app/networking/grpc_client.dart';

void main() {
  test('adds bearer token metadata', () {
    final metadata = GrpcAuthMetadata(apiKey: 'client-key').headers();

    expect(metadata['authorization'], 'Bearer client-key');
  });

  test(
    'listMessages forwards the before anchor with a bounded deadline',
    () async {
      final service = _CapturingSessionService();
      final server = grpc.Server.create(services: [service]);
      await server.serve(address: '127.0.0.1', port: 0);
      final channel = grpc.ClientChannel(
        '127.0.0.1',
        port: server.port!,
        options: const grpc.ChannelOptions(
          credentials: grpc.ChannelCredentials.insecure(),
        ),
      );
      addTearDown(() async {
        await channel.shutdown();
        await server.shutdown();
      });
      final api = TuringGrpcApi(
        baseUrl: 'http://127.0.0.1:${server.port}',
        apiKey: 'client-key',
        channel: channel,
      );

      final startedAt = DateTime.now();
      await api.listMessages(
        sessionId: 'session-1',
        limit: 25,
        before: 'message-anchor',
      );

      expect(service.listMessagesRequest?.sessionId, 'session-1');
      expect(service.listMessagesRequest?.limit, 25);
      expect(service.listMessagesRequest?.beforeMessageId, 'message-anchor');
      expect(service.listMessagesDeadline, isNotNull);
      expect(service.listMessagesDeadline!.isAfter(startedAt), isTrue);
      expect(
        service.listMessagesDeadline!.difference(startedAt),
        lessThanOrEqualTo(const Duration(seconds: 11)),
      );
    },
  );

  test('getSession forwards the session id with a bounded deadline', () async {
    final service = _CapturingSessionService();
    final server = grpc.Server.create(services: [service]);
    await server.serve(address: '127.0.0.1', port: 0);
    final channel = grpc.ClientChannel(
      '127.0.0.1',
      port: server.port!,
      options: const grpc.ChannelOptions(
        credentials: grpc.ChannelCredentials.insecure(),
      ),
    );
    addTearDown(() async {
      await channel.shutdown();
      await server.shutdown();
    });
    final api = TuringGrpcApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      apiKey: 'client-key',
      channel: channel,
    );

    final startedAt = DateTime.now();
    final session = await api.getSession(sessionId: 'session-42');

    expect(service.getSessionRequest?.sessionId, 'session-42');
    expect(service.getSessionDeadline, isNotNull);
    expect(service.getSessionDeadline!.isAfter(startedAt), isTrue);
    expect(
      service.getSessionDeadline!.difference(startedAt),
      lessThanOrEqualTo(const Duration(seconds: 11)),
    );
    expect(session.sessionId, 'session-42');
    expect(session.title, 'Search target');
    expect(session.status, SessionStatus.active);
    expect(session.updatedAt, DateTime.utc(2026, 8, 13, 12, 34, 56));
  });

  test(
    'listSessionPage preserves filter, cursor, page, and nanoseconds',
    () async {
      final service = _CapturingSessionService();
      final server = grpc.Server.create(services: [service]);
      await server.serve(address: '127.0.0.1', port: 0);
      final channel = grpc.ClientChannel(
        '127.0.0.1',
        port: server.port!,
        options: const grpc.ChannelOptions(
          credentials: grpc.ChannelCredentials.insecure(),
        ),
      );
      addTearDown(() async {
        await channel.shutdown();
        await server.shutdown();
      });
      final api = TuringGrpcApi(
        baseUrl: 'http://127.0.0.1:${server.port}',
        apiKey: 'client-key',
        channel: channel,
      );

      final page = await api.listSessionPage(
        filter: SessionListFilter.archived,
        limit: 25,
        cursor: 'cursor-before',
      );

      expect(service.listSessionsRequest?.page.limit, 25);
      expect(service.listSessionsRequest?.page.cursor, 'cursor-before');
      expect(
        service.listSessionsRequest?.filter,
        sessionpb.SessionListFilter.SESSION_LIST_FILTER_ARCHIVED,
      );
      expect(page.sessions.single.status, SessionStatus.archived);
      expect(page.sessions.single.updatedAtNanoseconds, 1000000900);
      expect(page.nextCursor, 'cursor-next');
    },
  );

  test('session lifecycle methods return authoritative snapshots', () async {
    final service = _CapturingSessionService();
    final server = grpc.Server.create(services: [service]);
    await server.serve(address: '127.0.0.1', port: 0);
    final channel = grpc.ClientChannel(
      '127.0.0.1',
      port: server.port!,
      options: const grpc.ChannelOptions(
        credentials: grpc.ChannelCredentials.insecure(),
      ),
    );
    addTearDown(() async {
      await channel.shutdown();
      await server.shutdown();
    });
    final api = TuringGrpcApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      apiKey: 'client-key',
      channel: channel,
    );

    final renamed = await api.renameSession(
      sessionId: 'session-42',
      title: 'Renamed',
    );
    final archived = await api.archiveSession(sessionId: 'session-42');
    final restored = await api.restoreSession(sessionId: 'session-42');

    expect(service.renameSessionRequest?.title, 'Renamed');
    expect(service.archiveSessionRequest?.sessionId, 'session-42');
    expect(service.restoreSessionRequest?.sessionId, 'session-42');
    expect(renamed.title, 'Renamed');
    expect(archived.status, SessionStatus.archived);
    expect(restored.status, SessionStatus.active);
  });

  test('createSession preserves timestamp nanoseconds for ordering', () async {
    final service = _CapturingSessionService();
    final server = grpc.Server.create(services: [service]);
    await server.serve(address: '127.0.0.1', port: 0);
    final channel = grpc.ClientChannel(
      '127.0.0.1',
      port: server.port!,
      options: const grpc.ChannelOptions(
        credentials: grpc.ChannelCredentials.insecure(),
      ),
    );
    addTearDown(() async {
      await channel.shutdown();
      await server.shutdown();
    });
    final api = TuringGrpcApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      apiKey: 'client-key',
      channel: channel,
    );

    final created = await api.createSession();

    expect(created['sessionId'], 'session-created');
    expect(created['createdAtNanoseconds'], 1000000900);
  });

  test(
    'searchMessages requests hit format and preserves the raw query, empty session filter, limit, and bounded deadline',
    () async {
      final service = _CapturingSessionService();
      service.searchMessagesResponse = sessionpb.SearchMessagesResponse(
        hits: [
          sessionpb.SearchHit(
            message: commonpb.Message(
              messageId: 'message-42',
              sessionId: 'session-42',
              runId: 'run-42',
              role: commonpb.MessageRole.MESSAGE_ROLE_ASSISTANT,
              content: 'hello  there',
              sequence: Int64(99),
              createdAt: timestamppb.Timestamp.fromDateTime(
                DateTime.utc(2026, 8, 13, 12, 35, 56),
              ),
            ),
            score: 0.5,
            snippet: 'hello  there',
          ),
        ],
      );
      final api = await _startSessionApi(service);

      final startedAt = DateTime.now();
      final hits = await api.searchMessages(query: 'hello  there', limit: 25);

      expect(service.searchMessagesCallCount, 1);
      expect(service.searchMessagesRequest?.query, 'hello  there');
      expect(service.searchMessagesRequest?.sessionId, '');
      expect(service.searchMessagesRequest?.limit, 25);
      expect(
        service.searchMessagesRequest?.responseFormat,
        sessionpb
            .SearchMessagesResponseFormat
            .SEARCH_MESSAGES_RESPONSE_FORMAT_HITS,
      );
      expect(service.searchMessagesDeadline, isNotNull);
      expect(service.searchMessagesDeadline!.isAfter(startedAt), isTrue);
      expect(
        service.searchMessagesDeadline!.difference(startedAt),
        lessThanOrEqualTo(const Duration(seconds: 11)),
      );
      expect(hits, hasLength(1));
      expect(hits.single.sessionId, 'session-42');
      expect(hits.single.score, 0.5);
      expect(hits.single.snippet, 'hello  there');
      expect(hits.single.message.messageId, 'message-42');
      expect(hits.single.message.runId, 'run-42');
      expect(hits.single.message.role, 'assistant');
      expect(hits.single.message.content, 'hello  there');
      expect(hits.single.message.sequence, 99);
      expect(
        hits.single.message.createdAt,
        DateTime.utc(2026, 8, 13, 12, 35, 56),
      );
    },
  );

  // The server ranks hits and the client must render that exact order: it may
  // not drop, reorder, or reverse rows on the way to the search list.
  test(
    'searchMessages preserves server hit order across every canonical hit',
    () async {
      final service = _CapturingSessionService();
      service.searchMessagesResponse = sessionpb.SearchMessagesResponse(
        hits: [
          sessionpb.SearchHit(
            message: commonpb.Message(
              messageId: 'message-top',
              sessionId: 'session-a',
              role: commonpb.MessageRole.MESSAGE_ROLE_USER,
              content: 'top  needle',
              sequence: Int64(1),
              createdAt: timestamppb.Timestamp.fromDateTime(
                DateTime.utc(2026, 8, 13, 12, 35, 56),
              ),
            ),
            score: 0.91,
            snippet: 'top  needle snippet',
          ),
          sessionpb.SearchHit(
            message: commonpb.Message(
              messageId: 'message-middle',
              sessionId: 'session-b',
              role: commonpb.MessageRole.MESSAGE_ROLE_ASSISTANT,
              content: 'middle needle',
              sequence: Int64(2),
              createdAt: timestamppb.Timestamp.fromDateTime(
                DateTime.utc(2026, 8, 13, 12, 36, 56),
              ),
            ),
            score: 0.52,
            snippet: 'middle needle snippet',
          ),
          sessionpb.SearchHit(
            message: commonpb.Message(
              messageId: 'message-bottom',
              sessionId: 'session-c',
              role: commonpb.MessageRole.MESSAGE_ROLE_USER,
              content: 'bottom needle',
              sequence: Int64(3),
              createdAt: timestamppb.Timestamp.fromDateTime(
                DateTime.utc(2026, 8, 13, 12, 37, 56),
              ),
            ),
            score: 0.13,
            snippet: 'bottom needle snippet',
          ),
        ],
      );
      final api = await _startSessionApi(service);

      final hits = await api.searchMessages(query: 'needle');

      expect(service.searchMessagesCallCount, 1);
      expect(hits, hasLength(3));
      expect(hits.map((hit) => hit.message.messageId).toList(), <String>[
        'message-top',
        'message-middle',
        'message-bottom',
      ]);
      expect(hits.map((hit) => hit.sessionId).toList(), <String>[
        'session-a',
        'session-b',
        'session-c',
      ]);
      expect(hits.map((hit) => hit.score).toList(), <double>[0.91, 0.52, 0.13]);
      expect(hits.map((hit) => hit.snippet).toList(), <String>[
        'top  needle snippet',
        'middle needle snippet',
        'bottom needle snippet',
      ]);
      expect(hits.map((hit) => hit.message.content).toList(), <String>[
        'top  needle',
        'middle needle',
        'bottom needle',
      ]);
    },
  );

  // A nonconforming server may echo the same result in both arrays. Hits win
  // outright: concatenating would double every row in the search list.
  test('searchMessages prefers hits and ignores duplicate messages', () async {
    final duplicated = commonpb.Message(
      messageId: 'message-42',
      sessionId: 'session-42',
      role: commonpb.MessageRole.MESSAGE_ROLE_ASSISTANT,
      content: 'hello  there',
      sequence: Int64(99),
      createdAt: timestamppb.Timestamp.fromDateTime(
        DateTime.utc(2026, 8, 13, 12, 35, 56),
      ),
    );
    final service = _CapturingSessionService();
    service.searchMessagesResponse = sessionpb.SearchMessagesResponse(
      hits: [
        sessionpb.SearchHit(
          message: duplicated,
          score: 0.25,
          snippet: 'hello  there',
        ),
      ],
      messages: [
        duplicated,
        commonpb.Message(
          messageId: 'message-43',
          sessionId: 'session-43',
          role: commonpb.MessageRole.MESSAGE_ROLE_USER,
          content: 'stale legacy row',
          sequence: Int64(100),
          createdAt: timestamppb.Timestamp.fromDateTime(
            DateTime.utc(2026, 8, 13, 12, 36, 56),
          ),
        ),
      ],
    );
    final api = await _startSessionApi(service);

    final hits = await api.searchMessages(query: 'hello  there');

    expect(service.searchMessagesCallCount, 1);
    expect(hits, hasLength(1));
    expect(hits.single.message.messageId, 'message-42');
    expect(hits.single.score, 0.25);
    expect(hits.single.snippet, 'hello  there');
  });

  test(
    'searchMessages maps an old-server messages-only response through legacy fallback',
    () async {
      final service = _CapturingSessionService();
      service.searchMessagesResponse = sessionpb.SearchMessagesResponse(
        messages: [
          commonpb.Message(
            messageId: 'message-1',
            sessionId: 'session-1',
            runId: 'run-1',
            role: commonpb.MessageRole.MESSAGE_ROLE_USER,
            content: 'first  legacy',
            sequence: Int64(1),
            createdAt: timestamppb.Timestamp.fromDateTime(
              DateTime.utc(2026, 8, 13, 12, 30),
            ),
          ),
          commonpb.Message(
            messageId: 'message-2',
            sessionId: 'session-2',
            role: commonpb.MessageRole.MESSAGE_ROLE_ASSISTANT,
            content: 'second legacy',
            sequence: Int64(2),
            createdAt: timestamppb.Timestamp.fromDateTime(
              DateTime.utc(2026, 8, 13, 12, 31),
            ),
          ),
        ],
      );
      final api = await _startSessionApi(service);

      final hits = await api.searchMessages(query: 'legacy');

      expect(service.searchMessagesCallCount, 1);
      expect(hits.map((hit) => hit.message.messageId).toList(), <String>[
        'message-1',
        'message-2',
      ]);
      expect(hits.map((hit) => hit.sessionId).toList(), <String>[
        'session-1',
        'session-2',
      ]);
      expect(hits.every((hit) => hit.score == null), isTrue);
      expect(hits.every((hit) => hit.snippet == null), isTrue);
      expect(hits.first.message.runId, 'run-1');
      expect(hits.first.message.role, 'user');
      expect(hits.first.message.content, 'first  legacy');
      expect(hits.first.message.sequence, 1);
      expect(hits.first.message.createdAt, DateTime.utc(2026, 8, 13, 12, 30));
      expect(hits.last.message.runId, isNull);
      expect(hits.last.message.role, 'assistant');
      expect(hits.last.message.content, 'second legacy');
      expect(hits.last.message.sequence, 2);
      expect(hits.last.message.createdAt, DateTime.utc(2026, 8, 13, 12, 31));
    },
  );

  test('searchMessages returns empty when both arrays are empty', () async {
    final service = _CapturingSessionService();
    service.searchMessagesResponse = sessionpb.SearchMessagesResponse();
    final api = await _startSessionApi(service);

    final hits = await api.searchMessages(query: 'no  results');

    expect(service.searchMessagesCallCount, 1);
    expect(hits, isEmpty);
  });

  // Callers group and sort hits without owning the list, so both branches must
  // hand back a fixed-length result rather than a mutable buffer.
  test(
    'searchMessages returns fixed-length lists from both branches',
    () async {
      final service = _CapturingSessionService();
      final message = commonpb.Message(
        messageId: 'message-42',
        sessionId: 'session-42',
        role: commonpb.MessageRole.MESSAGE_ROLE_USER,
        content: 'needle',
        sequence: Int64(1),
        createdAt: timestamppb.Timestamp.fromDateTime(
          DateTime.utc(2026, 8, 13, 12, 35, 56),
        ),
      );
      service.searchMessagesResponse = sessionpb.SearchMessagesResponse(
        hits: [
          sessionpb.SearchHit(message: message, score: 0.5, snippet: 'needle'),
        ],
      );
      final api = await _startSessionApi(service);

      final canonical = await api.searchMessages(query: 'needle');
      expect(
        () => canonical.add(canonical.single),
        throwsA(isA<UnsupportedError>()),
      );

      service.searchMessagesResponse = sessionpb.SearchMessagesResponse(
        messages: [message],
      );
      final legacy = await api.searchMessages(query: 'needle');
      expect(() => legacy.add(legacy.single), throwsA(isA<UnsupportedError>()));
    },
  );

  // The strict mapper's rejection has to reach the caller through the network
  // path too, instead of being softened into a partial or legacy result.
  test('searchMessages propagates a malformed canonical hit', () async {
    final service = _CapturingSessionService();
    service.searchMessagesResponse = sessionpb.SearchMessagesResponse(
      hits: [
        sessionpb.SearchHit(
          message: commonpb.Message(
            messageId: 'message-42',
            sessionId: 'session-42',
            role: commonpb.MessageRole.MESSAGE_ROLE_USER,
            content: 'needle',
            sequence: Int64(1),
            createdAt: timestamppb.Timestamp.fromDateTime(
              DateTime.utc(2026, 8, 13, 12, 35, 56),
            ),
          ),
          score: 0.5,
          snippet: '',
        ),
      ],
      messages: [
        commonpb.Message(
          messageId: 'message-43',
          sessionId: 'session-43',
          role: commonpb.MessageRole.MESSAGE_ROLE_USER,
          content: 'legacy fallback that must not be used',
          sequence: Int64(2),
          createdAt: timestamppb.Timestamp.fromDateTime(
            DateTime.utc(2026, 8, 13, 12, 36, 56),
          ),
        ),
      ],
    );
    final api = await _startSessionApi(service);

    await expectLater(
      api.searchMessages(query: 'needle'),
      throwsA(isA<FormatException>()),
    );
    expect(service.searchMessagesCallCount, 1);
  });

  // A failed hit-format call must surface as a failure. Retrying in legacy
  // format would double server load on overload and hide the real status.
  group('searchMessages propagates rpc failures without a legacy retry', () {
    final failures = <String, grpc.GrpcError>{
      'internal': const grpc.GrpcError.internal('boom'),
      'resource exhausted': const grpc.GrpcError.resourceExhausted('slow down'),
      'deadline exceeded': const grpc.GrpcError.deadlineExceeded('too late'),
    };

    failures.forEach((name, failure) {
      test(name, () async {
        final service = _CapturingSessionService();
        service.searchMessagesError = failure;
        final api = await _startSessionApi(service);

        await expectLater(
          api.searchMessages(query: 'boom'),
          throwsA(
            isA<grpc.GrpcError>().having(
              (error) => error.code,
              'code',
              failure.code,
            ),
          ),
        );

        expect(service.searchMessagesCallCount, 1);
        expect(
          service.searchMessagesRequest?.responseFormat,
          sessionpb
              .SearchMessagesResponseFormat
              .SEARCH_MESSAGES_RESPONSE_FORMAT_HITS,
        );
      });
    });
  });

  test('searchMessages sends the default limit when omitted', () async {
    final service = _CapturingSessionService();
    service.searchMessagesResponse = sessionpb.SearchMessagesResponse();
    final api = await _startSessionApi(service);

    await api.searchMessages(query: 'default limit');

    expect(service.searchMessagesRequest?.limit, 50);
    expect(service.searchMessagesRequest?.query, 'default limit');
    expect(service.searchMessagesRequest?.sessionId, '');
  });

  test(
    'listEvents preserves latest sequence with a bounded deadline',
    () async {
      final service = _LatestSequenceEventService();
      final server = grpc.Server.create(services: [service]);
      await server.serve(address: '127.0.0.1', port: 0);
      final channel = grpc.ClientChannel(
        '127.0.0.1',
        port: server.port!,
        options: const grpc.ChannelOptions(
          credentials: grpc.ChannelCredentials.insecure(),
        ),
      );
      addTearDown(() async {
        await channel.shutdown();
        await server.shutdown();
      });
      final api = TuringGrpcApi(
        baseUrl: 'http://127.0.0.1:${server.port}',
        apiKey: 'client-key',
        channel: channel,
      );

      final startedAt = DateTime.now();
      final page = await api.listEvents(sessionId: 'session-1');

      expect(page.latestSequence, 700);
      expect(service.deadline, isNotNull);
      expect(service.deadline!.isAfter(startedAt), isTrue);
      expect(
        service.deadline!.difference(startedAt),
        lessThanOrEqualTo(const Duration(seconds: 11)),
      );
    },
  );

  test(
    'listAuditEntries preserves every filter, ascending order, limit, cursor, '
    'and a bounded deadline',
    () async {
      final service = _CapturingAuditService();
      final server = grpc.Server.create(services: [service]);
      await server.serve(address: '127.0.0.1', port: 0);
      final channel = grpc.ClientChannel(
        '127.0.0.1',
        port: server.port!,
        options: const grpc.ChannelOptions(
          credentials: grpc.ChannelCredentials.insecure(),
        ),
      );
      addTearDown(() async {
        await channel.shutdown();
        await server.shutdown();
      });
      final api = TuringGrpcApi(
        baseUrl: 'http://127.0.0.1:${server.port}',
        apiKey: 'client-key',
        channel: channel,
      );

      final startedAt = DateTime.now();
      await api.listAuditEntries(
        correlationId: 'run_1',
        action: 'tool.call.before',
        createdAtStart: DateTime.utc(2026, 8, 18),
        createdAtEnd: DateTime.utc(2026, 8, 19),
        order: AuditOrder.ascending,
        limit: 25,
        cursor: 'cursor-1',
      );

      final request = service.request;
      expect(request, isNotNull);
      expect(request!.hasCorrelationId(), isTrue);
      expect(request.correlationId, 'run_1');
      expect(request.hasAction(), isTrue);
      expect(request.action, 'tool.call.before');
      expect(request.hasCreatedAtStart(), isTrue);
      expect(
        request.createdAtStart.toDateTime().toUtc(),
        DateTime.utc(2026, 8, 18),
      );
      expect(request.hasCreatedAtEnd(), isTrue);
      expect(
        request.createdAtEnd.toDateTime().toUtc(),
        DateTime.utc(2026, 8, 19),
      );
      expect(request.order, auditpb.AuditOrder.AUDIT_ORDER_ASCENDING);
      expect(request.page.limit, 25);
      expect(request.page.cursor, 'cursor-1');
      expect(service.deadline, isNotNull);
      expect(service.deadline!.isAfter(startedAt), isTrue);
      expect(
        service.deadline!.difference(startedAt),
        lessThanOrEqualTo(const Duration(seconds: 11)),
      );
    },
  );

  test('listAuditEntries omits optional filters and defaults order/cursor when '
      'the caller passes none', () async {
    final service = _CapturingAuditService();
    final server = grpc.Server.create(services: [service]);
    await server.serve(address: '127.0.0.1', port: 0);
    final channel = grpc.ClientChannel(
      '127.0.0.1',
      port: server.port!,
      options: const grpc.ChannelOptions(
        credentials: grpc.ChannelCredentials.insecure(),
      ),
    );
    addTearDown(() async {
      await channel.shutdown();
      await server.shutdown();
    });
    final api = TuringGrpcApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      apiKey: 'client-key',
      channel: channel,
    );

    await api.listAuditEntries();

    final request = service.request;
    expect(request, isNotNull);
    expect(request!.hasCorrelationId(), isFalse);
    expect(request.hasAction(), isFalse);
    expect(request.hasCreatedAtStart(), isFalse);
    expect(request.hasCreatedAtEnd(), isFalse);
    expect(request.order, auditpb.AuditOrder.AUDIT_ORDER_DESCENDING);
    expect(request.page.limit, 50);
    expect(request.page.cursor, '');
  });

  test('listAuditEntries maps present and absent payload fields without '
      'inventing values, and preserves a non-empty next cursor', () async {
    final service = _CapturingAuditService()
      ..response = auditpb.ListAuditEntriesResponse(
        entries: [
          auditpb.AuditEntry(
            auditId: 'audit-1',
            correlationId: 'run_1',
            actorType: 'user',
            actorId: 'user-1',
            action: 'tool.call.before',
            target: 'sandbox/file.txt',
            payload: auditpb.AuditPayload(
              state: auditpb.AuditPayloadState.AUDIT_PAYLOAD_STATE_PRESENT,
              toolName: '',
              serverName: '',
              phase: '',
              status: '',
              reason: '',
              durationMs: Int64(0),
              errorCode: '',
              provider: '',
              displayName: '',
              unattended: false,
              automationId: '',
              automationName: '',
              method: '',
              requestId: '',
              deletedRuns: Int64(0),
              deletedMessages: Int64(0),
              decisionComment: '',
              decisionCommentTruncated: false,
              denialReason: '',
              denialReasonTruncated: false,
            ),
            createdAt: timestamppb.Timestamp.fromDateTime(
              DateTime.utc(2026, 8, 18, 10),
            ),
          ),
          auditpb.AuditEntry(
            auditId: 'audit-2',
            actorType: 'system',
            action: 'automation.run.completed',
            payload: auditpb.AuditPayload(
              state: auditpb.AuditPayloadState.AUDIT_PAYLOAD_STATE_ABSENT,
            ),
            createdAt: timestamppb.Timestamp.fromDateTime(
              DateTime.utc(2026, 8, 19, 11),
            ),
          ),
        ],
        page: commonpb.PageResponse(nextCursor: 'cursor-2'),
      );
    final server = grpc.Server.create(services: [service]);
    await server.serve(address: '127.0.0.1', port: 0);
    final channel = grpc.ClientChannel(
      '127.0.0.1',
      port: server.port!,
      options: const grpc.ChannelOptions(
        credentials: grpc.ChannelCredentials.insecure(),
      ),
    );
    addTearDown(() async {
      await channel.shutdown();
      await server.shutdown();
    });
    final api = TuringGrpcApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      apiKey: 'client-key',
      channel: channel,
    );

    final page = await api.listAuditEntries();

    expect(page.entries, hasLength(2));
    expect(page.nextCursor, 'cursor-2');

    final full = page.entries[0];
    expect(full.auditId, 'audit-1');
    expect(full.correlationId, 'run_1');
    expect(full.actorType, 'user');
    expect(full.actorId, 'user-1');
    expect(full.action, 'tool.call.before');
    expect(full.target, 'sandbox/file.txt');
    expect(full.createdAt, DateTime.utc(2026, 8, 18, 10));
    expect(full.createdAt.isUtc, isTrue);
    expect(full.payload.state, AuditPayloadState.present);
    // Explicitly-set falsy/empty optionals must survive as themselves, not
    // collapse to null just because they compare equal to a default.
    expect(full.payload.toolName, '');
    expect(full.payload.serverName, '');
    expect(full.payload.phase, '');
    expect(full.payload.status, '');
    expect(full.payload.reason, '');
    expect(full.payload.durationMs, 0);
    expect(full.payload.errorCode, '');
    expect(full.payload.provider, '');
    expect(full.payload.displayName, '');
    expect(full.payload.unattended, false);
    expect(full.payload.automationId, '');
    expect(full.payload.automationName, '');
    expect(full.payload.method, '');
    expect(full.payload.requestId, '');
    expect(full.payload.deletedRuns, 0);
    expect(full.payload.deletedMessages, 0);
    expect(full.payload.decisionComment, '');
    expect(full.payload.decisionCommentTruncated, false);
    expect(full.payload.denialReason, '');
    expect(full.payload.denialReasonTruncated, false);

    final minimal = page.entries[1];
    expect(minimal.auditId, 'audit-2');
    expect(minimal.correlationId, isNull);
    expect(minimal.actorType, 'system');
    expect(minimal.actorId, isNull);
    expect(minimal.action, 'automation.run.completed');
    expect(minimal.target, isNull);
    expect(minimal.createdAt, DateTime.utc(2026, 8, 19, 11));
    expect(minimal.payload.state, AuditPayloadState.absent);
    expect(minimal.payload.toolName, isNull);
    expect(minimal.payload.serverName, isNull);
    expect(minimal.payload.phase, isNull);
    expect(minimal.payload.status, isNull);
    expect(minimal.payload.reason, isNull);
    expect(minimal.payload.durationMs, isNull);
    expect(minimal.payload.errorCode, isNull);
    expect(minimal.payload.provider, isNull);
    expect(minimal.payload.displayName, isNull);
    expect(minimal.payload.unattended, isNull);
    expect(minimal.payload.automationId, isNull);
    expect(minimal.payload.automationName, isNull);
    expect(minimal.payload.method, isNull);
    expect(minimal.payload.requestId, isNull);
    expect(minimal.payload.deletedRuns, isNull);
    expect(minimal.payload.deletedMessages, isNull);
    expect(minimal.payload.decisionComment, isNull);
    expect(minimal.payload.decisionCommentTruncated, isNull);
    expect(minimal.payload.denialReason, isNull);
    expect(minimal.payload.denialReasonTruncated, isNull);
  });

  // The approval rationale is the one payload field a person authored, so the
  // client has to carry their answer back unchanged: the words they typed, the
  // fact that they typed nothing, and the fact that no human field exists at
  // all are three different results, not one nullable string.
  test('listAuditEntries preserves an approval rationale, its explicit empty '
      'value, and its truncation flag without inventing any of them', () async {
    final service = _CapturingAuditService()
      ..response = auditpb.ListAuditEntriesResponse(
        entries: [
          auditpb.AuditEntry(
            auditId: 'audit-approved',
            actorType: 'user',
            action: 'approval.approved',
            payload: auditpb.AuditPayload(
              state: auditpb.AuditPayloadState.AUDIT_PAYLOAD_STATE_PRESENT,
              toolName: 'files.update',
              decisionComment: 'looked at the diff, fine',
              decisionCommentTruncated: true,
            ),
            createdAt: timestamppb.Timestamp.fromDateTime(
              DateTime.utc(2026, 8, 18, 10),
            ),
          ),
          auditpb.AuditEntry(
            auditId: 'audit-approved-silent',
            actorType: 'user',
            action: 'approval.approved',
            payload: auditpb.AuditPayload(
              state: auditpb.AuditPayloadState.AUDIT_PAYLOAD_STATE_PRESENT,
              toolName: 'files.update',
              decisionComment: '',
            ),
            createdAt: timestamppb.Timestamp.fromDateTime(
              DateTime.utc(2026, 8, 18, 11),
            ),
          ),
          auditpb.AuditEntry(
            auditId: 'audit-denied',
            actorType: 'user',
            action: 'approval.denied',
            payload: auditpb.AuditPayload(
              state: auditpb.AuditPayloadState.AUDIT_PAYLOAD_STATE_PRESENT,
              toolName: 'files.update',
              denialReason: 'path is outside the sandbox',
              denialReasonTruncated: false,
            ),
            createdAt: timestamppb.Timestamp.fromDateTime(
              DateTime.utc(2026, 8, 18, 12),
            ),
          ),
          auditpb.AuditEntry(
            auditId: 'audit-unattended',
            actorType: 'system',
            action: 'approval.approved',
            payload: auditpb.AuditPayload(
              state: auditpb.AuditPayloadState.AUDIT_PAYLOAD_STATE_PRESENT,
              toolName: 'files.update',
              unattended: true,
            ),
            createdAt: timestamppb.Timestamp.fromDateTime(
              DateTime.utc(2026, 8, 18, 13),
            ),
          ),
        ],
      );
    final server = grpc.Server.create(services: [service]);
    await server.serve(address: '127.0.0.1', port: 0);
    final channel = grpc.ClientChannel(
      '127.0.0.1',
      port: server.port!,
      options: const grpc.ChannelOptions(
        credentials: grpc.ChannelCredentials.insecure(),
      ),
    );
    addTearDown(() async {
      await channel.shutdown();
      await server.shutdown();
    });
    final api = TuringGrpcApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      apiKey: 'client-key',
      channel: channel,
    );

    final page = await api.listAuditEntries();

    final approved = page.entries[0].payload;
    expect(approved.decisionComment, 'looked at the diff, fine');
    expect(approved.decisionCommentTruncated, isTrue);
    expect(approved.denialReason, isNull);
    expect(approved.denialReasonTruncated, isNull);

    final silent = page.entries[1].payload;
    expect(silent.decisionComment, '');
    expect(silent.decisionCommentTruncated, isNull);

    final denied = page.entries[2].payload;
    expect(denied.denialReason, 'path is outside the sandbox');
    expect(denied.denialReasonTruncated, isFalse);
    expect(denied.decisionComment, isNull);
    expect(denied.decisionCommentTruncated, isNull);

    final unattended = page.entries[3].payload;
    expect(unattended.unattended, isTrue);
    expect(unattended.decisionComment, isNull);
    expect(unattended.decisionCommentTruncated, isNull);
    expect(unattended.denialReason, isNull);
    expect(unattended.denialReasonTruncated, isNull);
  });

  test('listAuditEntries maps an empty next cursor to null', () async {
    final service = _CapturingAuditService()
      ..response = auditpb.ListAuditEntriesResponse(
        entries: [
          auditpb.AuditEntry(
            auditId: 'audit-1',
            actorType: 'user',
            action: 'tool.call.before',
            payload: auditpb.AuditPayload(
              state: auditpb.AuditPayloadState.AUDIT_PAYLOAD_STATE_ABSENT,
            ),
            createdAt: timestamppb.Timestamp.fromDateTime(
              DateTime.utc(2026, 8, 18, 10),
            ),
          ),
        ],
        page: commonpb.PageResponse(),
      );
    final server = grpc.Server.create(services: [service]);
    await server.serve(address: '127.0.0.1', port: 0);
    final channel = grpc.ClientChannel(
      '127.0.0.1',
      port: server.port!,
      options: const grpc.ChannelOptions(
        credentials: grpc.ChannelCredentials.insecure(),
      ),
    );
    addTearDown(() async {
      await channel.shutdown();
      await server.shutdown();
    });
    final api = TuringGrpcApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      apiKey: 'client-key',
      channel: channel,
    );

    final page = await api.listAuditEntries();

    expect(page.nextCursor, isNull);
  });

  test(
    'listAuditEntries fails loudly when the server reports an unspecified '
    'payload state rather than guessing present, absent, or scrubbed',
    () async {
      final service = _CapturingAuditService()
        ..response = auditpb.ListAuditEntriesResponse(
          entries: [
            auditpb.AuditEntry(
              auditId: 'audit-1',
              actorType: 'user',
              action: 'tool.call.before',
              payload: auditpb.AuditPayload(
                state:
                    auditpb.AuditPayloadState.AUDIT_PAYLOAD_STATE_UNSPECIFIED,
              ),
              createdAt: timestamppb.Timestamp.fromDateTime(
                DateTime.utc(2026, 8, 18, 10),
              ),
            ),
          ],
        );
      final server = grpc.Server.create(services: [service]);
      await server.serve(address: '127.0.0.1', port: 0);
      final channel = grpc.ClientChannel(
        '127.0.0.1',
        port: server.port!,
        options: const grpc.ChannelOptions(
          credentials: grpc.ChannelCredentials.insecure(),
        ),
      );
      addTearDown(() async {
        await channel.shutdown();
        await server.shutdown();
      });
      final api = TuringGrpcApi(
        baseUrl: 'http://127.0.0.1:${server.port}',
        apiKey: 'client-key',
        channel: channel,
      );

      await expectLater(
        () => api.listAuditEntries(),
        throwsA(isA<FormatException>()),
      );
    },
  );
}

/// Starts an in-process session server bound to a client for one test and
/// tears both down afterwards.
Future<TuringGrpcApi> _startSessionApi(_CapturingSessionService service) async {
  final server = grpc.Server.create(services: [service]);
  await server.serve(address: '127.0.0.1', port: 0);
  final channel = grpc.ClientChannel(
    '127.0.0.1',
    port: server.port!,
    options: const grpc.ChannelOptions(
      credentials: grpc.ChannelCredentials.insecure(),
    ),
  );
  addTearDown(() async {
    await channel.shutdown();
    await server.shutdown();
  });
  return TuringGrpcApi(
    baseUrl: 'http://127.0.0.1:${server.port}',
    apiKey: 'client-key',
    channel: channel,
  );
}

class _CapturingSessionService extends sessiongrpc.SessionServiceBase {
  sessionpb.GetSessionRequest? getSessionRequest;
  DateTime? getSessionDeadline;
  sessionpb.ListSessionsRequest? listSessionsRequest;
  sessionpb.RenameSessionRequest? renameSessionRequest;
  sessionpb.ArchiveSessionRequest? archiveSessionRequest;
  sessionpb.RestoreSessionRequest? restoreSessionRequest;

  @override
  Future<sessionpb.CreateSessionResponse> createSession(
    grpc.ServiceCall call,
    sessionpb.CreateSessionRequest request,
  ) async {
    return sessionpb.CreateSessionResponse(
      sessionId: 'session-created',
      createdAt: timestamppb.Timestamp(seconds: Int64(1), nanos: 900),
    );
  }

  sessionpb.ListMessagesRequest? listMessagesRequest;
  DateTime? listMessagesDeadline;
  sessionpb.SearchMessagesRequest? searchMessagesRequest;
  DateTime? searchMessagesDeadline;
  int searchMessagesCallCount = 0;
  grpc.GrpcError? searchMessagesError;
  sessionpb.SearchMessagesResponse searchMessagesResponse =
      sessionpb.SearchMessagesResponse();

  @override
  Future<sessionpb.Session> getSession(
    grpc.ServiceCall call,
    sessionpb.GetSessionRequest request,
  ) async {
    getSessionRequest = request;
    getSessionDeadline = call.deadline;
    return sessionpb.Session(
      sessionId: 'session-42',
      title: 'Search target',
      status: 'active',
      updatedAt: timestamppb.Timestamp.fromDateTime(
        DateTime.utc(2026, 8, 13, 12, 34, 56),
      ),
    );
  }

  @override
  Future<sessionpb.ListSessionsResponse> listSessions(
    grpc.ServiceCall call,
    sessionpb.ListSessionsRequest request,
  ) async {
    listSessionsRequest = request;
    return sessionpb.ListSessionsResponse(
      sessions: [
        sessionpb.Session(
          sessionId: 'session-archived',
          title: 'Archived',
          status: 'archived',
          updatedAt: timestamppb.Timestamp(seconds: Int64(1), nanos: 900),
        ),
      ],
      page: commonpb.PageResponse(nextCursor: 'cursor-next'),
    );
  }

  @override
  Future<sessionpb.RenameSessionResponse> renameSession(
    grpc.ServiceCall call,
    sessionpb.RenameSessionRequest request,
  ) async {
    renameSessionRequest = request;
    return sessionpb.RenameSessionResponse(
      session: sessionpb.Session(
        sessionId: request.sessionId,
        title: request.title,
        status: 'active',
        updatedAt: timestamppb.Timestamp(seconds: Int64(2)),
      ),
    );
  }

  @override
  Future<sessionpb.ArchiveSessionResponse> archiveSession(
    grpc.ServiceCall call,
    sessionpb.ArchiveSessionRequest request,
  ) async {
    archiveSessionRequest = request;
    return sessionpb.ArchiveSessionResponse(
      session: sessionpb.Session(
        sessionId: request.sessionId,
        title: 'Renamed',
        status: 'archived',
        updatedAt: timestamppb.Timestamp(seconds: Int64(3)),
      ),
    );
  }

  @override
  Future<sessionpb.RestoreSessionResponse> restoreSession(
    grpc.ServiceCall call,
    sessionpb.RestoreSessionRequest request,
  ) async {
    restoreSessionRequest = request;
    return sessionpb.RestoreSessionResponse(
      session: sessionpb.Session(
        sessionId: request.sessionId,
        title: 'Renamed',
        status: 'active',
        updatedAt: timestamppb.Timestamp(seconds: Int64(4)),
      ),
    );
  }

  @override
  Future<sessionpb.ListMessagesResponse> listMessages(
    grpc.ServiceCall call,
    sessionpb.ListMessagesRequest request,
  ) async {
    listMessagesRequest = request;
    listMessagesDeadline = call.deadline;
    return sessionpb.ListMessagesResponse();
  }

  @override
  Future<sessionpb.SearchMessagesResponse> searchMessages(
    grpc.ServiceCall call,
    sessionpb.SearchMessagesRequest request,
  ) async {
    searchMessagesRequest = request;
    searchMessagesDeadline = call.deadline;
    searchMessagesCallCount++;
    final failure = searchMessagesError;
    if (failure != null) {
      throw failure;
    }
    return searchMessagesResponse;
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _LatestSequenceEventService extends eventgrpc.EventServiceBase {
  DateTime? deadline;

  @override
  Future<eventpb.ListEventsResponse> listEvents(
    grpc.ServiceCall call,
    eventpb.ListEventsRequest request,
  ) async {
    deadline = call.deadline;
    return eventpb.ListEventsResponse(latestSequence: Int64(700));
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _CapturingAuditService extends auditgrpc.AuditServiceBase {
  auditpb.ListAuditEntriesRequest? request;
  DateTime? deadline;
  auditpb.ListAuditEntriesResponse response =
      auditpb.ListAuditEntriesResponse();

  @override
  Future<auditpb.ListAuditEntriesResponse> listAuditEntries(
    grpc.ServiceCall call,
    auditpb.ListAuditEntriesRequest request,
  ) async {
    this.request = request;
    deadline = call.deadline;
    return response;
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}
