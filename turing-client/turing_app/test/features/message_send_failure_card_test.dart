import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/message_send_failure_card.dart';

void main() {
  testWidgets('renders the not-sent message text', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageSendFailureCard(
            message:
                "Your message wasn't sent. Please check your session and "
                'try again.',
          ),
        ),
      ),
    );

    expect(find.textContaining("Your message wasn't sent"), findsOneWidget);
  });

  testWidgets(
    'renders the "Message not sent" outcome label visibly, not just in '
    'the semantics label, and never renders "Message send unconfirmed", '
    '"Run failed", or "Run cancelled"',
    (tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: MessageSendFailureCard(message: 'invalid session'),
          ),
        ),
      );

      // Sighted users read the widget tree, not the accessibility tree: the
      // outcome must be visible on screen, not only announced to assistive
      // technology via the `Semantics` label.
      expect(find.text('Message not sent'), findsOneWidget);
      // This card is reserved for outcomes CONCLUSIVELY proven to precede
      // `EnqueueUserMessage` server-side — a genuinely different, stronger
      // claim than the ambiguous unconfirmed outcome, and neither of these
      // other terminal outcomes may ever be conflated with it.
      expect(find.text('Message send unconfirmed'), findsNothing);
      expect(find.text('Run failed'), findsNothing);
      expect(find.text('Run cancelled'), findsNothing);
    },
  );

  testWidgets(
    'exposes the exact "Message not sent: ..." semantics label as a live '
    'region',
    (tester) async {
      final handle = tester.ensureSemantics();
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: MessageSendFailureCard(message: 'invalid session'),
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
        find.bySemanticsLabel('Message not sent: invalid session'),
        findsOneWidget,
      );
      expect(
        tester.getSemantics(find.byType(MessageSendFailureCard)),
        matchesSemantics(
          label: 'Message not sent: invalid session',
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
                body: MessageSendFailureCard(message: 'invalid session'),
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
          of: find.byType(MessageSendFailureCard),
          matching: find.byIcon(Icons.error_outline),
        ),
      );
      expect(icon.color, colorScheme.onErrorContainer);

      final card = tester.widget<Card>(
        find.descendant(
          of: find.byType(MessageSendFailureCard),
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
