import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/utils/content_presence.dart';

void main() {
  test('content presence uses the approved Unicode scalar table', () {
    final vectors = <String, bool>{
      '': false,
      '\u0009': false,
      '\u000A': false,
      '\u000B': false,
      '\u000C': false,
      '\u000D': false,
      '\u0020': false,
      '\u0085': false,
      '\u00A0': false,
      '\u1680': false,
      '\u2000': false,
      '\u2001': false,
      '\u2002': false,
      '\u2003': false,
      '\u2004': false,
      '\u2005': false,
      '\u2006': false,
      '\u2007': false,
      '\u2008': false,
      '\u2009': false,
      '\u200A': false,
      '\u2028': false,
      '\u2029': false,
      '\u202F': false,
      '\u205F': false,
      '\u3000': false,
      _allApprovedWhitespace: false,
      'a': true,
      '\u200B': true,
      '\uFFFD': true,
      '\u0020\u3000\n a \t\u00A0': true,
      '\u0020\u200B\u0020': true,
      String.fromCharCode(0xD800): true,
    };

    for (final entry in vectors.entries) {
      expect(
        hasDisplayableContent(entry.key),
        entry.value,
        reason: 'unexpected classification for ${entry.key.runes.toList()}',
      );
    }
  });
}

const _allApprovedWhitespace =
    '\u0009\u000A\u000B\u000C\u000D\u0020\u0085\u00A0\u1680'
    '\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A'
    '\u2028\u2029\u202F\u205F\u3000';
