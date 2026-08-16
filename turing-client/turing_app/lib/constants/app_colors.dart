import 'package:flutter/material.dart';

/// The palette is built for long sessions in a dim room, which is how a
/// local-first assistant actually gets used. Dark is the primary design target
/// and light is derived from it, not the other way round.
///
/// Surfaces step in small, deliberate increments (background -> surface ->
/// raised) so depth reads without borders everywhere. Accents are reserved:
/// one brand colour for interactive elements, and semantic colours used *only*
/// for meaning (a tool ran, an approval is waiting, a run failed) so that
/// colour in the transcript always carries information.
class AppColors {
  AppColors._();

  // --- BRAND -----------------------------------------------------------
  static const Color brand = Color(0xFF5B8CFF);
  static const Color brandMuted = Color(0xFF3D63B8);

  // --- DARK (primary target) -------------------------------------------
  static const Color darkBackground = Color(0xFF0E1116);
  static const Color darkSurface = Color(0xFF151A21);
  static const Color darkRaised = Color(0xFF1C232C);
  static const Color darkBorder = Color(0xFF262E39);
  static const Color darkText = Color(0xFFE6EAF0);
  static const Color darkTextMuted = Color(0xFF9AA5B4);

  // --- LIGHT ------------------------------------------------------------
  static const Color lightBackground = Color(0xFFF7F8FA);
  static const Color lightSurface = Color(0xFFFFFFFF);
  static const Color lightRaised = Color(0xFFF0F2F6);
  static const Color lightBorder = Color(0xFFDFE3EA);
  static const Color lightText = Color(0xFF11151B);
  static const Color lightTextMuted = Color(0xFF5C6673);

  // --- SEMANTIC ---------------------------------------------------------
  // Used only where the colour means something; never decoration.
  static const Color running = Color(0xFF5B8CFF);
  static const Color success = Color(0xFF3FB27F);
  static const Color warning = Color(0xFFD9A441);
  static const Color danger = Color(0xFFE56A6A);

  /// Resolves a palette for the current brightness so widgets never branch on
  /// `isDark` themselves.
  static AppPalette of(BuildContext context) =>
      Theme.of(context).brightness == Brightness.dark
      ? const AppPalette.dark()
      : const AppPalette.light();
}

/// The colours a widget actually needs, already resolved for the active theme.
class AppPalette {
  const AppPalette({
    required this.background,
    required this.surface,
    required this.raised,
    required this.border,
    required this.text,
    required this.textMuted,
  });

  const AppPalette.dark()
    : background = AppColors.darkBackground,
      surface = AppColors.darkSurface,
      raised = AppColors.darkRaised,
      border = AppColors.darkBorder,
      text = AppColors.darkText,
      textMuted = AppColors.darkTextMuted;

  const AppPalette.light()
    : background = AppColors.lightBackground,
      surface = AppColors.lightSurface,
      raised = AppColors.lightRaised,
      border = AppColors.lightBorder,
      text = AppColors.lightText,
      textMuted = AppColors.lightTextMuted;

  final Color background;
  final Color surface;
  final Color raised;
  final Color border;
  final Color text;
  final Color textMuted;
}
