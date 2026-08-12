import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('macOS builds can connect to the backend', () {
    for (final path in [
      'macos/Runner/DebugProfile.entitlements',
      'macos/Runner/Release.entitlements',
    ]) {
      final entitlements = File(path).readAsStringSync();
      expect(
        entitlements,
        matches(
          RegExp(
            r'<key>com\.apple\.security\.network\.client</key>\s*<true\s*/>',
          ),
        ),
        reason: '$path must allow outbound backend connections',
      );
    }
  });
}
