# Quality Audit — crosscraft-brain

**Date:** 2026-06-21  
**Branch:** `feat/schedule-and-file-nodes`  
**Scope:** Full repo — Go backend, TypeScript frontend, database schema, Docker config, dependency graph  
**Method:** Structured review of all source files, test files, config, and architecture

---

## Executive Summary

**Overall grade: B+ (strong MVP, targeted hardening needed before production)**

The codebase is clean, well-structured, and thoughtfully designed. The Go engine is the clear strength — idiomatic, tested, and architecturally sound. The frontend is a thin SPA that delegates to the API properly. The audit found **no critical security vulnerabilities**, **one medium-severity bug**, and several areas where test coverage and hardening would be appropriate before going to production. The code is production-ready for internal/trusted environments; external-facing deployment needs the items in the Security and Hardening sections addressed.

---

## 1. Architecture & Design

### 1.1 Overall Architecture — ✅ Strong

The single-binary pattern (Go backend + embedded Vite SPA via `go:embed`) is well-executed:

- `server/cmd/crosscraft/main.go` wires everything in ~100 lines — clean composition root
- `server/web/embed.go` uses `embed.FS` + `fs.Sub` idiomatically
- The registry pattern (`internal/registry`) is the spine — UI descriptors and engine execution both read from the same `NodeDefinition` map
- The engine.Store interface is defined at the consumer (`internal/engine/store.go:26`), keeping the engine decoupled from Postgres — the right Go idiom

### 1.2 Separation of Concerns — ✅ Good

| Layer | Package | Role |
|-------|---------|------|
| HTTP | `internal/api` | REST + SSE + SPA serving |
| Engine | `internal/engine` | Topological executor + suspend/resume |
| Persistence | `internal/store` | Postgres (pgx); implements `engine.Store` |
| Nodes | `internal/nodes/*` | Node definitions (core, google, microsoft, etc.) |
| Auth | `internal/oauth` + `internal/credtype` | OAuth2 flow + credential types |
| Schema | `internal/schema` | Wire contract (mirrors TS `@crosscraft/schema`) |
| Expressions | `internal/expr` | `{{ }}` evaluator (goja) |
| AI | `internal/llm` | Anthropic Messages API client |

Each layer has a clear single responsibility. Dependencies flow inward: `api` → `engine` → `store` / `registry` / `expr`.

### 1.3 Declarative REST Framework — ✅ Excellent

`internal/rest` is the force-multiplier: a data-defined REST integration framework. One `rest.Node{}` struct with `Auth`, `Ops`, and param schemas compiles to a full `NodeDefinition`. This is how the entire Microsoft Graph catalog and many first-party integrations (Slack, Notion, Stripe, GitHub, etc.) ship. The design is clean and n8n-inspired.

**Notable design decision:** `rest.go:183` uses `context.Background()` for HTTP requests instead of the caller's `ctx`. This is intentional (a REST call should outlive an HTTP client disconnect), but the comment doesn't explain the trade-off. Worth documenting.

### 1.4 Durable Suspend/Resume — ✅ Well-designed

The engine supports durable pause (node returns `SuspendRequest`) → state checkpointed to Postgres → `/api/resume/{id}` rehydrates and continues. The `ClaimWaiting` atomic CAS pattern (`store.go:313-320`) guards double-resume correctly. Recovery on restart (`engine.go:519-531`) re-enqueues stranded "running" executions.

**Gap:** Resume continues from the checkpointed `visited` list, but the frontier recomputation (`engine.go:505-513`) only looks at successor edges — parallel branches that weren't visited but share an upstream with a visited node won't be rediscovered. This is correct for the current DAG semantics but worth a comment.

### 1.5 Async Worker Pool — ✅ Good pattern

`engine.StartWorkers` creates a bounded pool (default 8 workers, 256 queue depth) with a per-execution mutex guard (`engine.go:439-452`). This prevents double-driving and allows horizontal scaling (multiple instances behind a load balancer can share Postgres).

### 1.6 Frontend Architecture — ✅ Adequate for MVP

Single-page React app with react-router, React Flow canvas, shadcn/ui components. Clean separation of routes, components, and lib utilities. The `apps/web/src/lib/client.ts` API client mirrors the Go API surface.

---

## 2. Correctness & Bugs

### 2.1 🟡 Medium: `rest.go` uses `context.Background()` bypassing caller cancellation

**File:** `server/internal/rest/rest.go:181`  
**Severity:** Medium  

```go
req, err := http.NewRequestWithContext(context.Background(), method, u, body)
```

The REST node creates its own background context instead of using the execution context. This means:
- A workflow execution that times out or is cancelled won't cancel in-flight REST calls
- Long-running REST calls can leak goroutines

**Intentional reasoning (from context):** REST calls should complete even if the HTTP client disconnects. But this should use a detached context with its own timeout (e.g., `context.WithTimeout(context.Background(), 30*time.Second)`) rather than a raw `context.Background()`. Alternatively, pass `ctx` through `ExecContext` for callers that do want cancellation.

### 2.2 🟡 Low: SSE stream has hardcoded 600-iteration cap with no timeout escape

**File:** `server/internal/api/api.go:306`  
**Severity:** Low  

```go
for i := 0; i < 600; i++ {
```

600 iterations × 700ms = max ~7 minutes. An execution stuck in "running" forever (e.g., waiting on a never-fired resume) will poll for 7 minutes, then silently drop the connection with no error event. The comment says "until the run finishes" but the code doesn't check for "running" → the loop exits on `i >= 600` without sending a timeout event to the client.

**Fix:** Send a `{"status": "timeout"}` event before the loop's natural exit, or check `st.Status == "running"` and continue polling only for active runs.

### 2.3 🟡 Low: OAuth2 state map has no periodic cleanup

**File:** `server/internal/oauth/oauth.go:45`  
**Severity:** Low  

The `states` map is only cleaned during `putState` (when adding new states). If the service runs for a long time without new OAuth flows, expired states accumulate. The map is bounded by the number of OAuth flows initiated per 10-minute window, so this is low-impact, but a periodic cleanup goroutine (or a `sync.Map` with TTL) would be more robust.

### 2.4 ✅ No race conditions found in core engine

The engine's `active` map is properly guarded by `sync.Mutex` (`engine.go:439-452`). The scheduler's `last` map is similarly guarded (`scheduler.go:106-117`). OAuth2's `states` map is guarded (`oauth.go:183-205`). All shared-state access patterns are correct.

### 2.5 ✅ Expression evaluator correct

`server/internal/expr/expr.go` correctly handles whole-string expressions (typed return) vs. embedded expressions (string interpolation). The `stringify` function handles nil, string, map, slice, and primitives correctly. The Scope defaulting (`expr.go:53-54` — `$json` defaults to `{}`) matches the TS engine's behavior.

### 2.6 ✅ Crypto implementation correct

AES-256-GCM with random 12-byte nonce, 16-byte authentication tag, hex-encoded in `"iv:tag:ciphertext"` format. The decrypt reconstructs `ciphertext || tag` correctly (`crypto.go:81`). Key validation (64 hex chars → 32 bytes) is enforced at construction.

### 2.7 ✅ ID generation correct

`internal/id/id.go` uses `crypto/rand` (not `math/rand`), generates 21-char base64url-style IDs. The bitmask `&63` correctly maps to the 64-char alphabet. Collision probability is negligible for the use case.

### 2.8 ✅ `findTrigger` fallback correct

`engine.go:174-188` first looks for nodes with `IsTrigger: true`, then falls back to nodes with no incoming edges. This correctly handles both explicitly-marked triggers and the "first node in graph" convention.

### 2.9 🟢 Info: No input size limits on API handlers

The REST API handlers (`createWorkflow`, `saveWorkflow`, `run`, `resume`, `createCredential`) don't enforce request body size limits. A maliciously large JSON payload could exhaust memory. Go's `http.Server` doesn't enforce a default limit.

---

## 3. Security

### 3.1 🟡 Medium: CORS allows all origins with credentials-bearing headers

**File:** `server/internal/api/api.go:604-614`  
**Severity:** Medium  

```go
w.Header().Set("Access-Control-Allow-Origin", "*")
w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
```

`Access-Control-Allow-Origin: *` combined with endpoints that handle credentials (OAuth2 tokens, API keys) means any website can make authenticated requests to the API if the user is logged in. For a single-binary deployment on localhost this is fine; for a publicly-deployed instance, this should be configurable via `PUBLIC_BASE_URL` or similar.

**Note:** There's no `Access-Control-Allow-Credentials: true` set, so browsers won't send cookies — but the API uses header-based auth (API keys, Bearer tokens), not cookies, so the risk is about XSS on the same origin rather than cross-origin cookie theft.

### 3.2 🟡 Low: Default credential secret is all-zeros

**File:** `server/cmd/crosscraft/main.go:39`  
**Severity:** Low (documented, but dangerous if overlooked)  

```go
secret := env("CREDENTIALS_SECRET", strings.Repeat("0", 64))
```

The README acknowledges this and the Docker Compose file loads it from `.env`. However, if someone deploys without setting `CREDENTIALS_SECRET`, all credentials are encrypted with a predictable key. Consider:
1. Printing a loud warning on startup when the default is used
2. Generating a random key on first boot and storing it

### 3.3 ✅ OAuth2 state parameter correctly randomized

`oauth.go:258-261` uses `crypto/rand` for 16-byte state, hex-encoded → 32 chars. States are single-use (deleted on `takeState`) and expire after 10 minutes. CSRF protection is correct.

### 3.4 ✅ SQL injection protection — parameterized queries throughout

Every SQL query in `store.go` uses `$1, $2, ...` placeholders. No string concatenation for query building. The `pgx` driver handles this correctly. ✅ Clean.

### 3.5 ✅ OAuth2 redirect URI validated

The OAuth2 callback (`api.go:533-543`) validates the `state` parameter before exchanging the code. The redirect URI is server-constructed (`oauth.go:71`), not user-supplied. ✅ Correct.

### 3.6 🟢 Info: No rate limiting on any endpoint

There's no rate limiting on workflow runs, credential creation, or the copilot endpoint. An attacker could:
- Flood `/api/workflows/{id}/run` to exhaust DB connections
- Brute-force credential creation (though data is encrypted)
- Flood the copilot endpoint to consume AI credits

For production, add chi rate-limit middleware (e.g., `go-chi/httprate`).

### 3.7 🟢 Info: Webhook endpoint reveals workflow existence

`api.go:236-238` returns a 404 with the path in the error message when no workflow matches. An attacker could probe paths to discover active webhooks. For a publicly-deployed instance, consider a generic 404 message.

### 3.8 ✅ No secrets in source code or config files

`.env.example` contains only placeholder values. The `.env` is `.gitignore`d. The `docker-compose.yml` uses `${VAR:-default}` interpolation with safe defaults. ✅ Clean.

### 3.9 🟢 Info: Docker runs as nonroot

`Dockerfile:34` uses `gcr.io/distroless/static-debian12:nonroot` and `USER nonroot:nonroot`. ✅ Good. The Postgres container uses the default `postgres` user, which is standard.

---

## 4. Code Quality & Maintainability

### 4.1 Go Code — ✅ High Quality

The Go code is idiomatic, well-commented, and consistent:
- Package-level documentation comments on every package
- Descriptive function names (`enqueueReadySuccessors`, `recoverRunning`, `mapResponse`)
- Consistent error handling (errors returned, not panicked — except `id.New()` where `crypto/rand` failure is genuinely fatal)
- `engine.Store` interface at consumer side (Go best practice)
- `var _ engine.Store = (*Store)(nil)` compile-time interface check (`store.go:34`)

### 4.2 🟡 Minor: Several `str()`, `asInt()`, `asFloat()` helpers are duplicated

The `str()` helper appears in:
- `server/internal/oauth/oauth.go:244-248`
- `server/internal/scheduler/scheduler.go:119-123`
- `server/internal/api/api.go:597-602` (as `strOr` with slightly different semantics)

Consider a shared `internal/util` package or consolidate these. Low priority — each is a few lines and they serve slightly different defaults.

### 4.3 🟡 Minor: `_ =` error suppression in several places

- `api.go:102` — `_ = f.Close()` (harmless, file handle)
- `api.go:90` — `_, _ = w.Write(b)` (writing to response writer after headers sent)
- `api.go:301` — `b, _ := json.Marshal(v)` (in tight SSE loop)
- `api.go:564` — `_ = json.NewEncoder(w).Encode(v)` (response already written)

These are mostly in I/O paths where errors are non-recoverable (client disconnected). Acceptable for an MVP, but for production, at least log these at DEBUG level.

### 4.4 ✅ Frontend code structure is clean

React components are well-organized by concern. The `lib/client.ts` pattern centralizes API calls. Routes are separated from components. The shadcn/ui component wrappers in `components/ui/` are minimal and idiomatic.

### 4.5 ✅ No dead code or unused exports

All exported functions in `internal/` packages are consumed. The `internal/schema` package has full mirror coverage of the TypeScript types. No orphaned files.

### 4.6 🟢 Info: Some larger files could benefit from splitting

- `server/internal/api/api.go` (616 lines) — the copilot handler (lines 369-511, ~140 lines) could move to its own file
- `server/internal/store/store.go` (482 lines) — credential CRUD (lines 349-457) could be a separate file

These aren't urgent — the files are still navigable — but worth considering as the codebase grows.

### 4.7 ✅ Consistent JSON field naming

The Go struct tags mirror the TypeScript interfaces exactly — camelCase JSON keys, consistent use of `omitempty`. The `NodeDescriptor`/`NodeDefinition` split is clean.

---

## 5. Testing & Coverage

### 5.1 Test Summary

| Package | Test File | Tests | Quality |
|---------|-----------|-------|---------|
| `engine` | `engine_test.go` | 4 tests | ✅ Good — exercises goja expressions, suspend/resume, double-resume guard, crash recovery |
| `oauth` | `oauth_test.go` | 2 tests | ✅ Good — full OAuth2 flow with fake server, client credentials grant |
| `rest` | `rest_test.go` | 1 test | ✅ Good — GET/POST with auth, body, response mapping |
| `scheduler` | `scheduler_test.go` | 3 tests | ✅ Good — interval, cron, invalid cron |
| `nodes/core` | `files_test.go`, `fn_test.go` | ? tests | ✅ Decent |
| `nodes/google` | `google_test.go`, `media_test.go` | ? tests | ✅ Decent |
| `nodes/microsoft` | `microsoft_test.go` | ? tests | ✅ Decent |
| `nodes/adobe` | `adobe_test.go` | ? tests | ✅ Decent |

### 5.2 Testing Gaps

| Gap | Severity | Notes |
|-----|----------|-------|
| No integration tests for the API layer | Medium | The HTTP handlers are untested — no `httptest` against `api.NewRouter` |
| No tests for the `crypto` package | Low | Encrypt/decrypt round-trip test would catch regressions |
| No tests for the `id` package | Low | Collision test or format validation |
| No tests for the `expr` package | Medium | Expression evaluation is critical — needs unit tests for `ResolveValue` edge cases |
| No tests for `credtype` | Low | Registration and lookup are trivial but worth a smoke test |
| No tests for the scheduler's `fireDue` loop | Medium | The scheduling logic is only tested at the `due()` function level; the tick loop is untested |
| No frontend tests | Low | No Jest/Vitest tests in `apps/web` |
| No stress/concurrency tests | Low | The worker pool and `ClaimWaiting` CAS deserve a concurrent test |

### 5.3 Test Quality Assessment

The existing tests are well-written:
- `engine_test.go` uses an in-memory `memStore` that implements the full `Store` interface — good test isolation
- `oauth_test.go` spins up `httptest.Server` for the token endpoint — realistic integration
- `rest_test.go` tests both GET and POST with header auth and JSON body
- The tests verify exact JSON output, not just status codes

### 5.4 Missing Test Scenarios

1. **Expression edge cases:** nested `{{ }}`, expressions in select params, `$now` usage, `$node()` references
2. **Engine:** workflows with multiple parallel branches, workflows with the `if`/`switch` branch pruning
3. **Store:** concurrent `ClaimWaiting` calls (two goroutines racing on the same execution)
4. **API:** malformed JSON bodies, missing required fields, large payloads
5. **SSE:** client disconnection mid-stream, execution finishing during poll

---

## 6. Dependency & Infrastructure Health

### 6.1 Go Dependencies — ✅ Lean

```
github.com/dop251/goja        — JS interpreter (expressions + Code node)
github.com/go-chi/chi/v5     — HTTP router
github.com/jackc/pgx/v5      — Postgres driver
github.com/robfig/cron/v3    — Cron parser
golang.org/x/oauth2           — OAuth2 client
```

Only 5 direct dependencies. No framework bloat. The Go 1.26 toolchain is current. ✅ Excellent.

### 6.2 Frontend Dependencies — ✅ Modern

React 19, Vite 6, Tailwind v4, React Flow (`@xyflow/react`), shadcn/ui, react-router v7. All current-generation. Turborepo for monorepo orchestration. pnpm as package manager. ✅ Good choices.

### 6.3 Docker Setup — ✅ Clean

Multi-stage Dockerfile: `web` (pnpm build) → `build` (Go compile) → `runtime` (distroless). The distroless `nonroot` base image is secure by default. Docker Compose correctly uses health checks for Postgres → server dependency ordering. ✅ Good.

### 6.4 Database Schema — ✅ Clean

`db/schema.sql` is minimal: `workflows`, `executions`, `execution_steps`, `credentials`. Proper foreign keys with `ON DELETE CASCADE`. JSONB for graph/state. Indexes on the right columns (`executions_wf_idx`, `steps_exec_idx`). No ORM — raw SQL via pgx. ✅ Good for this scale.

### 6.5 `.gitignore` and Secrets — ✅ Clean

`.env` is gitignored (confirmed via `.env.example` pattern). The `crosscraft.exe` binary in `/server` is committed — intentional for the embedded SPA build? Worth adding to `.gitignore` if it's a build artifact.

### 6.6 License — ✅ Present

GNU GPLv3 (`LICENSE` file, 35KB — confirmed complete). Package.json and README reference it.

---

## 7. Summary of Findings

### By Severity

| Severity | Count | Items |
|----------|-------|-------|
| 🔴 Critical | 0 | — |
| 🟡 Medium | 3 | `context.Background()` in REST calls, CORS wildcard, missing integration tests |
| 🟢 Low / Info | 10 | SSE timeout silent, OAuth2 state cleanup, default credential key, no rate limiting, helper duplication, error suppression, large files, webhook info leak, committed binary, missing unit tests |

### Top 5 Actions for Production Readiness

1. **Add API-layer integration tests** — `httptest` against `api.NewRouter` with a real test database (or mock store)
2. **Add rate limiting** — at minimum on `/api/copilot` and `/api/workflows/{id}/run`
3. **Replace `context.Background()` in REST nodes** — use a detached context with explicit timeout
4. **Make CORS origin configurable** — read from env, default to `*` for dev
5. **Warn on default `CREDENTIALS_SECRET`** — log loudly or refuse to start in production

### What's Already Excellent

- Engine design (topological executor + durable suspend/resume + async worker pool)
- Declarative REST framework (`internal/rest`) — elegant force multiplier
- Registry pattern as the single source of truth for nodes
- SQL discipline (parameterized queries everywhere)
- OAuth2 flow implementation (state management, token refresh, persistence)
- Go code quality (interfaces at consumer, compile-time checks, idiomatic patterns)
- Docker multi-stage build with distroless runtime
- Database schema (minimal, correct indexes, proper constraints)

---

## 8. Appendix: File-by-File Notes

| File | Lines | Quality | Notes |
|------|-------|---------|-------|
| `server/cmd/crosscraft/main.go` | 101 | ✅ | Clean composition root; 8 node packs registered |
| `server/internal/api/api.go` | 616 | ✅ | Could split copilot handler; SSE timeout note |
| `server/internal/engine/engine.go` | 554 | ✅ | Core loop correct; async/sync dual mode clean |
| `server/internal/engine/store.go` | 47 | ✅ | Interface at consumer — Go best practice |
| `server/internal/store/store.go` | 482 | ✅ | Parameterized SQL; compile-time interface check |
| `server/internal/registry/registry.go` | 55 | ✅ | Simple, correct, chainable |
| `server/internal/schema/schema.go` | 221 | ✅ | Complete TS mirror; `NodeDescriptor` split clean |
| `server/internal/crypto/crypto.go` | 87 | ✅ | AES-256-GCM correct; hex encoding correct |
| `server/internal/expr/expr.go` | 161 | ✅ | Expression eval correct; fresh VM per eval |
| `server/internal/expr/code.go` | 72 | ✅ | Code node runner correct; JS→Go bridge clean |
| `server/internal/llm/llm.go` | 208 | ✅ | Anthropic API client; supports alt endpoints |
| `server/internal/oauth/oauth.go` | 263 | ✅ | OAuth2 flow correct; token refresh correct |
| `server/internal/credtype/credtype.go` | 189 | ✅ | 24 credential types registered; clean data model |
| `server/internal/rest/rest.go` | 378 | ✅ | Force multiplier; retry logic; `context.Background()` note |
| `server/internal/scheduler/scheduler.go` | 150 | ✅ | Simple polling scheduler; cron support |
| `server/internal/id/id.go` | 20 | ✅ | crypto/rand IDs; correct alphabet |
| `server/web/embed.go` | 22 | ✅ | Correct `go:embed` + `fs.Sub` usage |
| `db/schema.sql` | 46 | ✅ | Minimal; correct constraints and indexes |
| `db/migrate.ts` | 31 | ✅ | Simple migration runner |
| `apps/web/vite.config.ts` | 30 | ✅ | Correct proxy config; `emptyOutDir: false` for `.gitkeep` |
| `apps/web/src/main.tsx` | 27 | ✅ | Clean React 19 entry point |
| `packages/schema/src/index.ts` | 171 | ✅ | Complete TS contract; matches Go types |
| `Dockerfile` | 40 | ✅ | Multi-stage; distroless nonroot |
| `docker-compose.yml` | 52 | ✅ | Health checks; volume mounts; env interpolation |
