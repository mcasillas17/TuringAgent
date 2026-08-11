# TuringAgent v1.0 Integration Checklist

Use this checklist to validate the integrated v1.0 stack on `pturing-v1-base`.

## 1. Foundation (Task 1-2)
- [ ] `turing-backend/scripts/init.sh` exists and generates a valid `.env` with random secrets.
- [ ] `turing-backend/scripts/dev.sh` exists and starts the local v1 backend stack.
- [ ] `turing-backend/infra/docker-compose.yml` is valid (`docker compose -f infra/docker-compose.yml config --quiet`) and starts the orchestrator, runtime, and MCP services by default.
- [ ] `turing-backend/shared-types` exists and `npm run build` generates `dist/` with valid type declarations.
- [ ] `turing-client/turing_app` contains the preserved Flutter shell and backend-connected client surfaces.

## 2. Orchestrator (Task 3-6)
- [ ] `turing-orchestrator` container builds and starts without immediate crash.
- [ ] Orchestrator logs show "WAL mode" and migration application for `0001_initial.sql`.
- [ ] `GET /health` returns `{ ok: true }`.
- [ ] `POST /api/sessions` requires valid `Bearer` auth and persists to SQLite.
- [ ] Internal port 3001 is reachable from other containers but NOT published to host.

## 3. Agent Runtime & Streaming (Task 7-10)
- [ ] `turing-agent-runtime-general` container logs show successful job polling from orchestrator.
- [ ] Enqueuing a message results in an `agent_runs` entry with status `running`.
- [ ] WebSocket connection remains open during agent execution.
- [ ] `message.delta` events contain cumulative text chunks that match the model output.
- [ ] Replay handshake (`hello` with `lastSequence`) correctly serves missed events from the database.

## 4. MCP & Tools (Task 11-14)
- [ ] MCP servers (`system` and `files`) validate the per-agent bearer token.
- [ ] A discovery-capable runtime sends `RuntimeWorkerReady` with
  `TOOL_DISCOVERY_STATUS_COMPLETE` and its authoritative `tools/list`
  snapshot; a failed discovery sends `TOOL_DISCOVERY_STATUS_FAILED` and the
  orchestrator rejects the worker.
- [ ] `ListTools` reflects the union of connected workers' discovered tools;
  disconnecting the last owner disables an omitted tool, and a fresh registry
  with no workers is empty.
- [ ] Tool authorization is scoped to the reporting worker: a worker cannot
  call a tool discovered only by another worker, and disabled or missing tools
  do not fall back to legacy policy defaults.
- [ ] Files MCP correctly rejects path traversal (e.g., `../../etc/passwd`).
- [ ] Safe tools complete successfully and emit `tool.call.completed`.
- [ ] Approval-required tools (e.g., `files.update`) block execution and emit `approval.requested`.
- [ ] Orchestrator signs a valid HS256 JWT for approved tools.
- [ ] `audit_logs` table contains entries for `auth.failed`, `tool.call.before`, and `tool.call.after`.

## 5. Flutter Client (Task 15)
- [ ] Flutter-specific setup and behavior are documented in `turing-client/turing_app/README.md`.
- [ ] The existing polished `ResponsiveShell` remains the main authenticated app surface.
- [ ] The Chat and Settings tabs use backend-connected client surfaces once credentials are configured.
- [ ] Devices, Stats, and Integrations remain placeholder tabs until their backend contracts are defined.

## 6. End-to-End (Task 16)
- [ ] `turing-backend/scripts/smoke.sh` passes 100% in a clean Docker environment.
- [ ] All `README.md` setup steps result in a working system.
- [ ] Verification scripts (`smoke-ws.mjs`) correctly handle network timeouts and retries.

---

## Verification Best Practices

- **Log Monitoring**: During full backend smoke tests, keep a terminal open with `cd turing-backend && docker compose -f infra/docker-compose.yml logs -f`.
- **Database Inspection**: Use `sqlite3 turing-backend/data/turing.db` to verify that tables are being populated as expected.
- **Network Isolation**: Verify that `turing-agent-runtime-general` cannot reach MCP servers it is not authorized for by checking Docker network configurations.
- **Ollama Mocking**: If Ollama is unavailable, verify that the runtime fails gracefully with a `model_unavailable` error code rather than an unhandled exception.
- **Clean Starts**: Use `docker compose -f turing-backend/infra/docker-compose.yml down -v` carefully when you need to reset local containers and volumes.
