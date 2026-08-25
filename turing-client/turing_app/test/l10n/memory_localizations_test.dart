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
      expect(match.single.label, isNotEmpty);
    }
  });

  test('memory is one of the categories the dialog can name', () {
    expect(EgressDataCategory.memoryProfile.wireName, 'memory_profile');
    expect(EgressDataCategory.memoryProfile.label, 'Memory and profile');
  });
}
