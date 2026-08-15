import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/message_send_unconfirmed_card.dart';

void main() {
  testWidgets('renders the unconfirmed-send message text', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageSendUnconfirmedCard(
            message:
                "We couldn't confirm whether this message was sent. Check "
                'the conversation before sending it again.',
          ),
        ),
      ),
    );

    expect(
      find.textContaining("We couldn't confirm whether this message was sent"),
      findsOneWidget,
    );
  });

  testWidgets(
    'renders the "Message send unconfirmed" outcome label visibly, not just '
    'in the semantics label, and never renders "Run failed" or "Run '
    'cancelled"',
    (tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: MessageSendUnconfirmedCard(message: 'connection lost'),
          ),
        ),
      );

      // Sighted users read the widget tree, not the accessibility tree: the
      // outcome must be visible on screen, not only announced to assistive
      // technology via the `Semantics` label.
      expect(find.text('Message send unconfirmed'), findsOneWidget);
      // The true outcome is unknown, not a known terminal state — a run may
      // or may not have been queued server-side (see this card's own doc
      // comment) — so neither of these definite claims may ever appear.
      expect(find.text('Run failed'), findsNothing);
      expect(find.text('Run cancelled'), findsNothing);
      // The prior, now-corrected name for this exact outcome asserted a
      // certainty ("not sent") this client does not have. It must never
      // resurface once the fix regresses.
      expect(find.text('Message not sent'), findsNothing);
    },
  );

  testWidgets(
    'exposes the exact "Message send unconfirmed: ..." semantics label as a '
    'live region',
    (tester) async {
      final handle = tester.ensureSemantics();
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: MessageSendUnconfirmedCard(message: 'connection lost'),
          ),
        ),
      );

      // Assert against the actual rendered semantics tree, not just the
      // `Semantics` widget's constructor arguments: a widget can be built
      // with a `liveRegion: true` argument and still fail to reach the
      // rendered `SemanticsNode` if it is merged away, excluded by an
      // ancestor, or the render object never attaches it. `bySemanticsLabel`
      // only matches a node that assistive technology would actually see.
      expect(
        find.bySemanticsLabel('Message send unconfirmed: connection lost'),
        findsOneWidget,
      );
      expect(
        tester.getSemantics(find.byType(MessageSendUnconfirmedCard)),
        matchesSemantics(
          label: 'Message send unconfirmed: connection lost',
          isLiveRegion: true,
        ),
      );
      handle.dispose();
    },
  );

  testWidgets(
    'uses an error icon and theme-derived error colors, distinct from '
    'ordinary content',
    (tester) async {
      late ColorScheme colorScheme;
      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) {
              colorScheme = Theme.of(context).colorScheme;
              return const Scaffold(
                body: MessageSendUnconfirmedCard(message: 'connection lost'),
              );
            },
          ),
        ),
      );

      // Visual distinction is pinned against the *theme's* error colors, not
      // a hardcoded palette: this fails if the card stops using the error
      // container styling (e.g. reverts to a plain/neutral card) while
      // staying correct whichever concrete color scheme the running app
      // supplies.
      final icon = tester.widget<Icon>(
        find.descendant(
          of: find.byType(MessageSendUnconfirmedCard),
          matching: find.byIcon(Icons.error_outline),
        ),
      );
      expect(icon.color, colorScheme.onErrorContainer);

      final card = tester.widget<Card>(
        find.descendant(
          of: find.byType(MessageSendUnconfirmedCard),
          matching: find.byType(Card),
        ),
      );
      expect(card.color, colorScheme.errorContainer);

      // The error color must actually differ from a plain surface color, or
      // pinning "errorContainer" would be a distinction without a
      // difference.
      expect(colorScheme.errorContainer, isNot(colorScheme.surface));
    },
  );
}
