import 'package:material_ui/material_ui.dart';

class ThemeLogic {
  // Singleton pattern: Ensure we only have ONE theme manager in the app
  static final ThemeLogic _instance = ThemeLogic._internal();
  factory ThemeLogic() => _instance;
  ThemeLogic._internal();

  // The state holder
  // We default to System, but you can change to ThemeMode.dark
  final ValueNotifier<ThemeMode> mode = ValueNotifier(ThemeMode.dark);

  // The Action
  void toggleTheme(bool isDark) {
    mode.value = isDark ? ThemeMode.dark : ThemeMode.light;
    // Todo: Add logic here later to save preference to SharedPreferences
  }

  // Helper to check current status
  bool get isDark => mode.value == ThemeMode.dark;
}
