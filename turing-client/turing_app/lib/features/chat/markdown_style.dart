import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:material_ui/material_ui.dart';

/// The style sheet flutter_markdown's `MarkdownStyleSheet.fromTheme` builds,
/// built from material_ui's [ThemeData] instead.
///
/// flutter_markdown still reads the theme through the framework's deprecated
/// `package:flutter/material.dart`, which cannot see the material_ui theme
/// this app installs. On its own it therefore styles markdown from the
/// default light theme, which in dark mode is dark text on a dark surface.
/// Every [MarkdownBody] passes this sheet (or one merged onto it), so the app's
/// theme decides the text and block styles. What flutter_markdown draws with
/// its own legacy widgets still follows the light default: the scrollbar of a
/// code block and, on Linux and Windows, the selection context menu. Remove
/// this once the markdown package moves to material_ui.
MarkdownStyleSheet markdownStyleSheetFromTheme(ThemeData theme) {
  final body = theme.textTheme.bodyMedium!;
  return MarkdownStyleSheet(
    a: const TextStyle(color: Colors.blue),
    p: body,
    pPadding: EdgeInsets.zero,
    code: body.copyWith(
      backgroundColor: theme.cardTheme.color,
      fontFamily: 'monospace',
      fontSize: body.fontSize! * 0.85,
    ),
    h1: theme.textTheme.headlineSmall,
    h1Padding: EdgeInsets.zero,
    h2: theme.textTheme.titleLarge,
    h2Padding: EdgeInsets.zero,
    h3: theme.textTheme.titleMedium,
    h3Padding: EdgeInsets.zero,
    h4: theme.textTheme.bodyLarge,
    h4Padding: EdgeInsets.zero,
    h5: theme.textTheme.bodyLarge,
    h5Padding: EdgeInsets.zero,
    h6: theme.textTheme.bodyLarge,
    h6Padding: EdgeInsets.zero,
    em: const TextStyle(fontStyle: FontStyle.italic),
    strong: const TextStyle(fontWeight: FontWeight.bold),
    del: const TextStyle(decoration: TextDecoration.lineThrough),
    blockquote: body,
    img: body,
    checkbox: body.copyWith(color: theme.primaryColor),
    blockSpacing: 8.0,
    listIndent: 24.0,
    listBullet: body,
    listBulletPadding: const EdgeInsets.only(right: 4),
    tableHead: const TextStyle(fontWeight: FontWeight.w600),
    tableBody: body,
    tableHeadAlign: TextAlign.center,
    tablePadding: const EdgeInsets.only(bottom: 4.0),
    tableBorder: TableBorder.all(color: theme.dividerColor),
    tableColumnWidth: const FlexColumnWidth(),
    tableCellsPadding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
    tableCellsDecoration: const BoxDecoration(),
    blockquotePadding: const EdgeInsets.all(8.0),
    blockquoteDecoration: BoxDecoration(
      color: Colors.blue.shade100,
      borderRadius: BorderRadius.circular(2.0),
    ),
    codeblockPadding: const EdgeInsets.all(8.0),
    codeblockDecoration: BoxDecoration(
      color: theme.cardTheme.color ?? theme.cardColor,
      borderRadius: BorderRadius.circular(2.0),
    ),
    horizontalRuleDecoration: BoxDecoration(
      border: Border(top: BorderSide(width: 5.0, color: theme.dividerColor)),
    ),
  );
}
