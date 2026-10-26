/// The local route's wire identity: a model running on this machine, whichever
/// engine serves it. Spelled 'ollama' because it was named before other engines
/// existed; the backend stores and routes on it that way.
const localModelProvider = 'ollama';

/// A model provider's name as the app shows it: the local route is "Local",
/// whichever engine serves it; any other provider keeps its identity.
String modelProviderLabel(String provider) =>
    provider == localModelProvider ? 'Local' : provider;

/// The engine and model serving the local route, as the backend reports them.
/// [label] is display text the backend's runtime advertises, such as "Ollama"
/// or "ds4"; it may be empty.
class LocalEngine {
  const LocalEngine({required this.label, required this.model});

  final String label;
  final String model;
}
