import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';
import 'package:turing_flutter_app/l10n/memory_localizations.dart';
import 'package:turing_flutter_app/models/grpc_mappers.dart';
import 'package:turing_flutter_app/models/memory.dart';
import 'package:turing_flutter_app/models/remote_egress.dart';

void main() {
  late AppLocalizations l10n;

  Future<void> loadCopy(WidgetTester tester) async {
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: AppLocalizations.localizationsDelegates,
        supportedLocales: AppLocalizations.supportedLocales,
        home: Builder(
          builder: (context) {
            l10n = AppLocalizations.of(context);
            return const SizedBox.shrink();
          },
        ),
      ),
    );
  }

  testWidgets('every memory status this client can decode has copy', (
    tester,
  ) async {
    await loadCopy(tester);

    for (final reason in MemoryUnavailableReason.values) {
      expect(
        localizedMemoryUnavailableCopy(l10n, reason),
        isNotEmpty,
        reason: '$reason would render as a blank explanation',
      );
    }
    for (final status in MemoryNoteStatus.values) {
      expect(localizedMemoryNoteStatusCopy(l10n, status), isNotEmpty);
    }
    for (final state in MemoryCandidateState.values) {
      expect(localizedMemoryCandidateStateCopy(l10n, state), isNotEmpty);
    }
    for (final kind in MemoryCandidateKind.values) {
      expect(localizedMemoryCandidateKindCopy(l10n, kind), isNotEmpty);
    }
    for (final tier in MemoryTier.values) {
      expect(localizedMemoryTierCopy(l10n, tier), isNotEmpty);
    }
  });

  testWidgets('an unreadable vault never reads as a healthy or off one', (
    tester,
  ) async {
    await loadCopy(tester);

    final distinct = {
      for (final reason in MemoryUnavailableReason.values)
        reason: localizedMemoryUnavailableCopy(l10n, reason),
    };
    expect(
      distinct[MemoryUnavailableReason.none],
      isNot(distinct[MemoryUnavailableReason.disabled]),
    );
    expect(
      distinct[MemoryUnavailableReason.vaultMissing],
      isNot(distinct[MemoryUnavailableReason.vaultUnreadable]),
    );
    expect(
      distinct[MemoryUnavailableReason.contentParseFailed],
      isNot(distinct[MemoryUnavailableReason.contentTooLarge]),
    );
    // The server not saying anything is its own answer, and it is not "fine".
    expect(
      distinct[MemoryUnavailableReason.unspecified],
      isNot(distinct[MemoryUnavailableReason.none]),
    );
  });

  test('every disclosed egress category has a label the dialog can show', () {
    for (final category in commonpb.EgressDataCategory.values) {
      if (category ==
          commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_UNSPECIFIED) {
        continue;
      }
      // Pins the wire enum to this client's own list: a category added to the
      // proto without a Dart member would leave the consent dialog silently
      // shorter than the run it describes.
      final wireName = GrpcMappers.egressDataCategoryToString(category);
      final match = EgressDataCategory.values.where(
        (value) => value.wireName == wireName,
      );
      expect(
        match,
        hasLength(1),
        reason: '$wireName has no category in this client',
      );
      expect(match, hasLength(1));
    }
  });

  testWidgets('every egress category the dialog can name has copy', (
    tester,
  ) async {
    await loadCopy(tester);

    for (final category in EgressDataCategory.values) {
      expect(
        localizedEgressCategoryCopy(l10n, category),
        isNotEmpty,
        reason: '$category would render as a blank line of consent',
      );
    }
  });

  testWidgets('every memory tier the egress dialog can be handed has copy', (
    tester,
  ) async {
    await loadCopy(tester);

    for (final tier in MemoryEgressTier.values) {
      expect(localizedEgressMemoryTierCopy(l10n, tier), isNotEmpty);
    }
  });

  testWidgets('memory is one of the categories the dialog can name', (
    tester,
  ) async {
    await loadCopy(tester);

    expect(EgressDataCategory.memoryProfile.wireName, 'memory_profile');
    expect(
      localizedEgressCategoryCopy(l10n, EgressDataCategory.memoryProfile),
      'Memory and profile',
    );
  });

  // The Memory page and the consent dialog are the two surfaces where the
  // product says what it does with words the user did not write. Neither may
  // carry English that a translator cannot reach.
  test('the memory page and the egress dialog hold no hardcoded English', () {
    for (final path in const [
      'lib/features/workspace/memory_page.dart',
      'lib/features/chat/remote_egress_dialog.dart',
    ]) {
      final source = File(path).readAsStringSync();
      final offenders = <String>[];
      for (final match in RegExp(r"'([^'\\\n]{4,})'").allMatches(source)) {
        final literal = match.group(1)!;
        if (!RegExp(r'[A-Za-z] [A-Za-z]').hasMatch(literal)) continue;
        if (literal.startsWith('package:')) continue;
        offenders.add(literal);
      }
      expect(
        offenders,
        isEmpty,
        reason: '$path still spells prose in Dart instead of in the ARB',
      );
    }
  });
}
