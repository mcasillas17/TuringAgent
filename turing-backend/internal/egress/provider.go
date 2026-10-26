package egress

// LocalProvider is the stored and wire identity of the local route: a model
// running on this machine, so a send needs no remote-egress consent. Any
// local engine runs under it, Ollama or an OpenAI-compatible server. It is
// spelled "ollama" because it was named before other engines existed, and the
// stored rows, job payloads and egress decisions that carry it cannot change.
const LocalProvider = "ollama"
