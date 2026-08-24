import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/models/mcp_server.dart';

void main() {
  test(
    'McpImportReport copies imported/skipped/refused into unmodifiable lists',
    () {
      final sourceImported = ['server-a'];
      final sourceSkipped = ['server-b'];
      final sourceRefused = [
        const UnsupportedMcpServer(
          name: 'server-c',
          reason: 'unsupported transport: stdio',
        ),
      ];

      final report = McpImportReport(
        imported: sourceImported,
        skipped: sourceSkipped,
        refused: sourceRefused,
      );

      // Mutating the caller's lists after construction must not leak into the
      // report: the report owns its own defensive copy.
      sourceImported.add('server-a2');
      sourceSkipped.add('server-b2');
      sourceRefused.add(
        const UnsupportedMcpServer(name: 'server-d', reason: 'later'),
      );

      expect(report.imported, ['server-a']);
      expect(report.skipped, ['server-b']);
      expect(report.refused, hasLength(1));
      expect(report.refused.single.name, 'server-c');
      expect(report.refused.single.reason, 'unsupported transport: stdio');

      expect(() => report.imported.add('server-x'), throwsUnsupportedError);
      expect(() => report.skipped.add('server-x'), throwsUnsupportedError);
      expect(
        () => report.refused.add(
          const UnsupportedMcpServer(name: 'x', reason: 'y'),
        ),
        throwsUnsupportedError,
      );
    },
  );
}
