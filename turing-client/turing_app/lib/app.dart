import 'dart:async';

import 'package:flutter/material.dart';

import 'constants/app_colors.dart';
import 'features/settings/settings_screen.dart';
import 'logic/theme_logic.dart';
import 'l10n/generated/app_localizations.dart';
import 'networking/api_client.dart';
import 'networking/auth_storage.dart';
import 'networking/grpc_client.dart';
import 'networking/grpc_event_source.dart';
import 'networking/event_source.dart';
import 'ui/shell/responsive_shell.dart';

typedef TuringApiFactory =
    TuringApi Function({required String baseUrl, required String apiKey});
typedef TuringEventSourceFactory =
    TuringEventSource Function({
      required String baseUrl,
      required String apiKey,
    });

class TuringApp extends StatefulWidget {
  const TuringApp({
    super.key,
    this.authStorage = const AuthStorage(),
    this.apiFactory = _createGrpcApi,
    this.eventSourceFactory = _createGrpcEventSource,
  });

  final ClientAuthStorage authStorage;
  final TuringApiFactory apiFactory;
  final TuringEventSourceFactory eventSourceFactory;

  @override
  State<TuringApp> createState() => _TuringAppState();
}

class _TuringAppState extends State<TuringApp> {
  late Future<_ClientConfig?> _configFuture;

  @override
  void initState() {
    super.initState();
    _configFuture = _loadConfig();
  }

  void _reloadConfig() {
    // Braces, not an arrow: `() => _configFuture = _loadConfig()` RETURNS the
    // assigned Future, and setState asserts its callback returns nothing. With
    // the arrow form saving credentials threw here, so the app never left the
    // Settings screen even though the write had succeeded.
    setState(() {
      _configFuture = _loadConfig();
    });
  }

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<ThemeMode>(
      valueListenable: ThemeLogic().mode,
      builder: (context, currentMode, _) {
        return MaterialApp(
          title: 'Project Turing',
          debugShowCheckedModeBanner: false,
          localizationsDelegates: AppLocalizations.localizationsDelegates,
          supportedLocales: AppLocalizations.supportedLocales,
          theme: _buildTheme(Brightness.light),
          darkTheme: _buildTheme(Brightness.dark),
          themeMode: currentMode,
          home: FutureBuilder<_ClientConfig?>(
            future: _configFuture,
            builder: (context, snapshot) {
              if (snapshot.connectionState != ConnectionState.done) {
                return const Scaffold(
                  body: Center(child: CircularProgressIndicator()),
                );
              }

              final config = snapshot.data;
              if (config == null) {
                return SettingsScreen(
                  authStorage: widget.authStorage,
                  onSaved: _reloadConfig,
                );
              }

              return _ConfiguredTuringShell(
                config: config,
                authStorage: widget.authStorage,
                onSettingsChanged: _reloadConfig,
                apiFactory: widget.apiFactory,
                eventSourceFactory: widget.eventSourceFactory,
              );
            },
          ),
        );
      },
    );
  }

  Future<_ClientConfig?> _loadConfig() async {
    final backendUrl = await widget.authStorage.readBackendUrl();
    final apiKey = await widget.authStorage.readApiKey();
    if (backendUrl == null ||
        apiKey == null ||
        backendUrl.isEmpty ||
        apiKey.isEmpty) {
      return null;
    }
    return _ClientConfig(backendUrl: backendUrl, apiKey: apiKey);
  }

  ThemeData _buildTheme(Brightness brightness) {
    final isDark = brightness == Brightness.dark;
    final palette = isDark ? const AppPalette.dark() : const AppPalette.light();
    final scheme =
        ColorScheme.fromSeed(
          seedColor: AppColors.brand,
          brightness: brightness,
        ).copyWith(
          surface: palette.surface,
          onSurface: palette.text,
          outlineVariant: palette.border,
        );

    // A conversation is long-form reading, so the body text is sized and
    // spaced for that rather than for dense UI chrome.
    final base = isDark ? ThemeData.dark() : ThemeData.light();
    final text = base.textTheme.copyWith(
      bodyLarge: TextStyle(fontSize: 15, height: 1.55, color: palette.text),
      bodyMedium: TextStyle(fontSize: 14, height: 1.5, color: palette.text),
      bodySmall: TextStyle(
        fontSize: 12.5,
        height: 1.4,
        color: palette.textMuted,
      ),
      titleMedium: TextStyle(
        fontSize: 14.5,
        fontWeight: FontWeight.w600,
        color: palette.text,
      ),
      labelLarge: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600),
    );

    return ThemeData(
      brightness: brightness,
      useMaterial3: true,
      colorScheme: scheme,
      scaffoldBackgroundColor: palette.background,
      textTheme: text,
      dividerTheme: DividerThemeData(
        color: palette.border,
        thickness: 1,
        space: 1,
      ),
      appBarTheme: AppBarTheme(
        backgroundColor: palette.background,
        foregroundColor: palette.text,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        centerTitle: false,
        titleTextStyle: TextStyle(
          fontSize: 15,
          fontWeight: FontWeight.w600,
          color: palette.text,
        ),
      ),
      cardTheme: CardThemeData(
        color: palette.raised,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        margin: EdgeInsets.zero,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(10),
          side: BorderSide(color: palette.border),
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: palette.surface,
        hintStyle: TextStyle(color: palette.textMuted),
        contentPadding: const EdgeInsets.symmetric(
          horizontal: 14,
          vertical: 12,
        ),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: BorderSide(color: palette.border),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: BorderSide(color: palette.border),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: const BorderSide(color: AppColors.brand, width: 1.5),
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          backgroundColor: AppColors.brand,
          foregroundColor: Colors.white,
          padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 14),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(10),
          ),
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(foregroundColor: AppColors.brand),
      ),
      listTileTheme: ListTileThemeData(
        iconColor: palette.textMuted,
        textColor: palette.text,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
      ),
      snackBarTheme: SnackBarThemeData(
        backgroundColor: palette.raised,
        contentTextStyle: TextStyle(color: palette.text),
        behavior: SnackBarBehavior.floating,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
      ),
    );
  }
}

class _ClientConfig {
  const _ClientConfig({required this.backendUrl, required this.apiKey});

  final String backendUrl;
  final String apiKey;
}

class _ConfiguredTuringShell extends StatefulWidget {
  const _ConfiguredTuringShell({
    required this.config,
    required this.authStorage,
    required this.onSettingsChanged,
    required this.apiFactory,
    required this.eventSourceFactory,
  });

  final _ClientConfig config;
  final ClientAuthStorage authStorage;
  final VoidCallback onSettingsChanged;
  final TuringApiFactory apiFactory;
  final TuringEventSourceFactory eventSourceFactory;

  @override
  State<_ConfiguredTuringShell> createState() => _ConfiguredTuringShellState();
}

class _ConfiguredTuringShellState extends State<_ConfiguredTuringShell> {
  late TuringApi _apiClient;

  @override
  void initState() {
    super.initState();
    _apiClient = _createApiClient();
  }

  @override
  void didUpdateWidget(_ConfiguredTuringShell oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.config.backendUrl != widget.config.backendUrl ||
        oldWidget.config.apiKey != widget.config.apiKey ||
        oldWidget.apiFactory != widget.apiFactory) {
      _closeApiClient(_apiClient);
      _apiClient = _createApiClient();
    }
  }

  @override
  Widget build(BuildContext context) {
    return ResponsiveShell(
      apiClient: _apiClient,
      authStorage: widget.authStorage,
      initialBackendUrl: widget.config.backendUrl,
      initialApiKey: widget.config.apiKey,
      onSettingsChanged: widget.onSettingsChanged,
      eventSourceFactory: () => widget.eventSourceFactory(
        baseUrl: widget.config.backendUrl,
        apiKey: widget.config.apiKey,
      ),
      sessionUpdateSourceFactory: () {
        final source = widget.eventSourceFactory(
          baseUrl: widget.config.backendUrl,
          apiKey: widget.config.apiKey,
        );
        if (source is TuringSessionUpdateSource) {
          return source as TuringSessionUpdateSource;
        }
        source.close();
        return null;
      },
    );
  }

  @override
  void dispose() {
    _closeApiClient(_apiClient);
    super.dispose();
  }

  TuringApi _createApiClient() {
    return widget.apiFactory(
      baseUrl: widget.config.backendUrl,
      apiKey: widget.config.apiKey,
    );
  }

  void _closeApiClient(TuringApi apiClient) {
    if (apiClient is ClosableTuringApi) {
      unawaited(apiClient.close());
    }
  }
}

TuringApi _createGrpcApi({required String baseUrl, required String apiKey}) {
  return TuringGrpcApi(baseUrl: baseUrl, apiKey: apiKey);
}

TuringEventSource _createGrpcEventSource({
  required String baseUrl,
  required String apiKey,
}) {
  return TuringGrpcEventSource(baseUrl: baseUrl, apiKey: apiKey);
}
