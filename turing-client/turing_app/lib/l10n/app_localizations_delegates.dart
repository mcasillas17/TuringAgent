import 'package:material_ui/material_ui.dart';

import 'generated/app_localizations.dart';

/// Every localizations delegate the app installs, for [MaterialApp] and the
/// test hosts alike.
///
/// The app's widgets come from material_ui, whose widgets read material_ui's
/// [GlobalMaterialLocalizations], so those delegates are listed here.
///
/// [AppLocalizations.localizationsDelegates] is kept as well. gen-l10n
/// generates it with flutter_localizations' Material and Cupertino delegates,
/// which localize only widgets built from the framework's deprecated
/// `package:flutter/material.dart`. flutter_markdown still builds those: its
/// selectable text opens a context menu that throws without them.
/// Localizations loads one delegate per type, so the shared widgets delegate
/// is not loaded twice.
const List<LocalizationsDelegate<dynamic>> appLocalizationsDelegates =
    <LocalizationsDelegate<dynamic>>[
      ...AppLocalizations.localizationsDelegates,
      ...GlobalMaterialLocalizations.delegates,
    ];
