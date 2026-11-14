import 'package:flutter/gestures.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:turing_flutter_app/l10n/app_localizations_delegates.dart';
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';

Widget _host(Widget home) => MaterialApp(
  localizationsDelegates: appLocalizationsDelegates,
  supportedLocales: AppLocalizations.supportedLocales,
  home: Scaffold(body: home),
);

void main() {
  testWidgets('material_ui widgets get material_ui global localizations', (
    tester,
  ) async {
    late MaterialLocalizations localizations;
    AppLocalizations? appLocalizations;
    await tester.pumpWidget(
      _host(
        Builder(
          builder: (context) {
            localizations = MaterialLocalizations.of(context);
            appLocalizations = AppLocalizations.of(context);
            return const SizedBox.shrink();
          },
        ),
      ),
    );

    // Without material_ui's delegates, MaterialApp falls back to its
    // English-only defaults.
    expect(localizations, isA<GlobalMaterialLocalizations>());
    expect(appLocalizations, isNotNull);
  });

  testWidgets('selectable markdown opens its context menu', (tester) async {
    // flutter_markdown builds its selectable text from the deprecated
    // framework library, whose context menu needs that library's
    // MaterialLocalizations; material_ui's do not count.
    await tester.pumpWidget(
      _host(const MarkdownBody(data: 'Found three notes.', selectable: true)),
    );

    final gesture = await tester.startGesture(
      tester.getCenter(find.byType(EditableText)),
      kind: PointerDeviceKind.mouse,
      buttons: kSecondaryMouseButton,
    );
    await gesture.up();
    await tester.pumpAndSettle();

    expect(tester.takeException(), isNull);
    expect(find.text('Select all'), findsOneWidget);
  });
}
