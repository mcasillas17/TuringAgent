import 'package:flutter/material.dart';

import '../../networking/auth_storage.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    required this.authStorage,
    this.onSaved,
    this.initialBackendUrl = 'http://localhost:3000',
    this.initialApiKey = '',
    this.embedded = false,
  });

  final ClientAuthStorage authStorage;
  final VoidCallback? onSaved;
  final String initialBackendUrl;
  final String initialApiKey;
  final bool embedded;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  late final TextEditingController _backendUrl;
  late final TextEditingController _apiKey;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _backendUrl = TextEditingController(text: widget.initialBackendUrl);
    _apiKey = TextEditingController(text: widget.initialApiKey);
  }

  @override
  Widget build(BuildContext context) {
    final body = ListView(
      padding: const EdgeInsets.all(16),
      children: [
        TextField(
          controller: _backendUrl,
          decoration: const InputDecoration(labelText: 'Backend URL'),
          keyboardType: TextInputType.url,
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _apiKey,
          decoration: const InputDecoration(labelText: 'API key'),
          obscureText: true,
        ),
        const SizedBox(height: 16),
        Align(
          alignment: Alignment.centerLeft,
          child: FilledButton.icon(
            onPressed: _saving ? null : _save,
            icon: const Icon(Icons.save),
            label: Text(_saving ? 'Saving...' : 'Save'),
          ),
        ),
      ],
    );

    if (widget.embedded) {
      return body;
    }

    return Scaffold(
      appBar: AppBar(title: const Text('Project Turing Settings')),
      body: body,
    );
  }

  Future<void> _save() async {
    setState(() => _saving = true);
    // Without this the button just flashes and nothing else happens: any
    // failure below escapes as an unhandled async error, leaving the user with
    // no result and no reason.
    try {
      await widget.authStorage.save(
        backendUrl: _backendUrl.text,
        apiKey: _apiKey.text,
      );
      if (!mounted) return;
      widget.onSaved?.call();
      if (Navigator.of(context).canPop()) {
        Navigator.of(context).pop();
      }
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Could not save settings: $error')),
      );
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  void dispose() {
    _backendUrl.dispose();
    _apiKey.dispose();
    super.dispose();
  }
}
