# BUILD.md — Re-platform crosscraft to a Go single binary + Vite SPA

## Context

`crosscraft-brain` is a forkable workflow-automation platform. Today the **entire
backend lives inside the Next.js app**: the execution engine, node packs, Postgres
access, credentials crypto, webhooks/resume, SSE monitoring and the AI copilot are all
TypeScript ([packages/engine](packages/engine), [packages/nodes-core](packages/nodes-core),
[apps/studio/app/api/*](apps/studio/app/api)). The React canvas
([apps/studio/components/Editor.tsx](apps/studio/components/Editor.tsx) on XYFlow +
Tailwind/shadcn) is the product's "touch" and must be preserved.

We are moving the **whole backend to a single Go binary** (durable concurrency at scale,
single-binary ops, performance, maintainability) and replacing the Next.js shell with a
**Vite + React SPA** that the Go binary serves via `go:embed`. The canvas components are
reused as-is. We also **remove the farm vertical** so the skeleton ships vertical-agnostic.

**Locked decisions:**
- Go owns the **whole backend**; one stateless binary serves API **and** the embedded UI.
- **Native Go nodes**; `goja` (pure-Go JS) used **only** for the `core.code` node and the
  `{{ }}` expression evaluator.
- Frontend = **Vite + React SPA** (drop Next.js). Chosen over Next static-export / Next-BFF
  for scalable adoption: simplest build, lowest fork tax, no static-export gymnastics, and
  **stateless instances scale horizontally** behind a load balancer (Postgres is the only
  shared state).
- **Remove the farm vertical** (`nodes-farm`, `db/farm.sql`, `/api/farm/report`, registry +
  compose wiring, docs references).
- **Spike first** to de-risk goja + engine + SSE + contract parity.

## Why this is tractable

The backend is small and clean: engine ~240 lines, expression evaluator ~70, store ~170,
core nodes ~225. Only **two** things truly need JavaScript and both are contained:
`core.code` (`new Function('items','$trigger',…)`) and `{{ }}` evaluation
([expression.ts](packages/engine/src/expression.ts), scope `$json,$input,$trigger,$node,
$now,JSON,Math`). Everything else — `set`, `if`, `http`, triggers, `wait`, suspend/resume
— is plain logic. SSE is already just DB polling every 700 ms
([stream/route.ts](apps/studio/app/api/executions/[id]/stream/route.ts)). The canvas is
already 100% client components, so dropping Next.js loses nothing of value.

## Target architecture

One Go binary `crosscraft` that serves the embedded SPA + REST/SSE API on one origin,
runs the topological engine with durable suspend/resume, evaluates `{{ }}` + Code via
`goja`, and persists to the **existing core Postgres schema** ([db/schema.sql](db/schema.sql)).

### Go module layout (new `server/` at repo root, outside the pnpm workspace)

```
server/
  go.mod
  cmd/crosscraft/main.go     # config, pgx pool, registry wiring, http server
  internal/
    schema/    # Go structs mirroring @crosscraft/schema (json tags = identical wire shapes)
    expr/      # goja {{ }} evaluator + Code-node runner (the crux)
    engine/    # run(), resume(), drive(), gatherInput, RunState — port of engine.ts
    registry/  # map[string]NodeDefinition + Descriptors() (strips Execute)
    nodes/core # manualTrigger, webhookTrigger, set, if, http, code, wait
    nodes/ai   # summarize, classify, extract (via internal/llm)
    llm/       # Anthropic client: Complete() + Structured(); honors AI_BASE_URL override
    store/     # pgx queries: workflows, executions, steps, credentials
    crypto/    # AES-256-GCM matching packages/engine/src/crypto.ts byte-for-byte
    api/       # chi router + handlers + SPA static fileserver (index.html fallback)
  web/embed.go # go:embed of the built Vite dist
```

**Libraries:** `dop251/goja` (pure Go, no cgo → static binary), `jackc/pgx/v5` (+ pgxpool),
`go-chi/chi/v5`, `anthropics/anthropic-sdk-go`, `matoous/go-nanoid`. Same env vars as today
(`DATABASE_URL`, `CREDENTIALS_SECRET`, `ANTHROPIC_API_KEY`, `AI_BASE_URL`, `AI_API_KEY`,
`AI_MODEL_FAST`, `AI_MODEL_SMART`, `PUBLIC_BASE_URL`, `PORT`).

### The goja crux (`internal/expr`)
- **Expressions:** per eval, bind `$json` (map), `$input`, `$trigger` (item arrays), and
  `$node` (Go `func(id string) []Item`); `JSON`/`Math` are goja built-ins; `$now` =
  `time.Now()`. Run `( <expr> )`, `Export()` the result. Preserve both modes from
  `resolveValue`: whole-string expr keeps the raw typed value; embedded `{{ }}` are
  stringified/interpolated.
- **Code node:** wrap source as `(function(items,$trigger){ "use strict"; <code> })`, call
  with bound args, normalize the return to `Item[]` (same as the current code node).
- **Concurrency:** `goja.Runtime` is **not** goroutine-safe → `sync.Pool` of runtimes + a
  compiled-`*goja.Program` cache keyed by source. Add an execution timeout via
  `runtime.Interrupt` from a watchdog goroutine (an improvement over today's unbounded `Function`).

### Engine port (`internal/engine`)
Direct translation of [engine.ts](packages/engine/src/engine.ts): `gatherInput`, `isReady`,
`enqueueReadySuccessors`, `buildContext`, `drive`, `run`, `resume`, `findTrigger`.
`RunState{ triggerItems, nodeOutputs map[string]map[string][]Item, visited []string }`.
`NodeResult` = struct with `Outputs map[string][]Item` **or** `Suspend *SuspendRequest`.
Preserve untaken-branch pruning and the suspend→`setWaiting`→`resume` flow verbatim. Drive
runs in a **bounded worker pool**; for `/run` and webhook ingress, drive to the first
suspend/terminal so the suspending node's `respond` is still returned to the caller.

### Persistence (`internal/store`) — core schema unchanged
Port [store.ts](packages/engine/src/store.ts) 1:1 with pgx (`workflows`, `executions`,
`execution_steps`, `credentials`); reuse `db/schema.sql` as-is. Credentials: read
[crypto.ts](packages/engine/src/crypto.ts) and replicate the exact AES-256-GCM
serialization (IV/tag/ciphertext, hex, 32-byte key from `CREDENTIALS_SECRET`).

### API surface (matches the contract `client.ts` already calls)
`GET /api/nodes` · `GET|POST /api/workflows` · `GET|PUT /api/workflows/{id}` ·
`POST /api/workflows/{id}/run` · `GET /api/executions` · `GET /api/executions/{id}` ·
`GET /api/executions/{id}/stream` (SSE) · `POST /api/resume/{id}` ·
`POST /api/webhook/{path}` · `GET|POST /api/credentials` · `DELETE /api/credentials/{id}` ·
`POST /api/copilot`. SSE = port the 700 ms poll loop with `http.Flusher` + request-context
cancellation. Copilot = port the prompt/tool-schema from
[copilot/route.ts](apps/studio/app/api/copilot/route.ts) onto `anthropic-sdk-go` structured
output emitting `GraphOp[]`. **No `/api/farm/report`.**

## Remove the farm vertical (explicit)
Delete [packages/nodes-farm](packages/nodes-farm), `db/farm.sql`, the
`apps/studio/app/api/farm/` route, the farm import/registration in
[apps/studio/lib/registry.ts](apps/studio/lib/registry.ts), the `db/farm.sql` mount in
[docker-compose.yml](docker-compose.yml), and farm references in
[README.md](README.md)/[DESIGN.md](DESIGN.md). The Go backend never implements farm nodes.

## Frontend: Next.js → Vite + React SPA (preserve UI/UX)
The canvas components move over essentially unchanged; only the shell changes.
1. New Vite React + TS app (replaces `apps/studio` Next scaffolding). Keep Tailwind v4 +
   shadcn, [globals.css](apps/studio/app/globals.css), [brand.ts](apps/studio/lib/brand.ts),
   [ui.ts](apps/studio/lib/ui.ts), and all of `components/*` (XYFlow Editor, CcNode, Palette,
   Inspector, Copilot, NodeRunOutput, ResumeDialog) verbatim.
2. **Routing via react-router:** `/` (workflows), `/editor/:id`, `/executions/:workflowId`,
   `/credentials`. Replace `next/link` → react-router `Link`; `next/navigation`
   (`useRouter`/`useParams`/`usePathname`) → react-router hooks; drop `'use client'` and the
   server component [editor/[id]/page.tsx](apps/studio/app/editor/[id]/page.tsx) (data now
   fetched client-side via `api.getWorkflow(id)`); replace `next/font`/`next/image` if used.
3. **Move `NodeDescriptor`** from `@crosscraft/engine` into
   [@crosscraft/schema](packages/schema/src/index.ts) so the SPA depends only on the schema
   package. Keep `client.ts` as the API client (same-origin in prod; Vite `server.proxy`
   sends `/api` → the Go server in dev).
4. **Serve from Go:** `go:embed` the Vite `dist`; the API router serves static assets and
   falls back to `index.html` for non-`/api` routes (clean SPA routing — no static-export hacks).
5. Remove the TS backend after parity: `packages/engine`, `packages/nodes-core`,
   `packages/nodes-ai`, and all `apps/studio/app/api/*`. `@crosscraft/schema` stays as the
   SPA's type source.

## Dev & Docker
- New multi-stage [Dockerfile](Dockerfile): (1) node builds the Vite SPA → `dist`, copied to
  `server/web/`; (2) golang builds the binary with embedded assets; (3) distroless/scratch
  final image with just the binary → tiny single-binary image.
- [docker-compose.yml](docker-compose.yml): replace the `studio` (Node) service with a
  `server` (Go) service; drop the `db/farm.sql` mount; Postgres otherwise unchanged.
- Local dev: run the Go server + `vite dev` with the `/api` proxy (optionally `air` for Go reload).

## Execution plan

**Phase 0 — Backend spike (de-risk; keep the *current* UI to validate).** Stand up `server/`
with pgx to the existing DB. Port `internal/schema`, `internal/expr` (goja),
`internal/engine`, and nodes `manualTrigger, set, if, http, code, wait`. Implement
`GET /api/nodes`, workflows CRUD, `POST /run`, `GET /executions`, SSE, `POST /resume`. Point
the **still-Next** frontend at the Go server via a dev rewrite — this validates the backend
without touching the frontend yet.
*Success criteria:* the existing canvas, unchanged, runs `manual → set → if → http` **and** a
durable `wait`→`resume`, with a goja `code` node, showing live SSE. Proves goja + engine +
suspend/resume + SSE + contract parity in one shot.

**Phase 1 — Backend completion.** AI nodes + `internal/llm`, credentials + `internal/crypto`,
webhook ingress, copilot. Remove the farm vertical. Add a Go parity test mirroring
[scripts/engine-smoke.ts](scripts/engine-smoke.ts) (run→suspend→resume→success with per-step I/O).

**Phase 2 — Vite SPA + single-binary cutover.** Build the Vite app, migrate routing/links,
move `NodeDescriptor`, `go:embed` + index.html fallback, new Dockerfile + compose. Delete the
Next.js app, the TS engine/node packages, and all TS API routes.

## Risks & mitigations
- **goja ≠ V8** for exotic JS → keep expressions small (already the intent); add timeout +
  clear errors; cover with parity tests.
- **Next→Vite migration** (routing/links/env) → mechanical and bounded; canvas components are
  already client-only; do it in Phase 2 after the backend is proven.
- **Cross-restart run recovery** (a run stuck `running` if its instance dies mid-flight) —
  current TS has the same gap; an optional startup recovery sweep is **future work, out of scope**.

## Verification
- Phase 0: drive a workflow from the real UI against Go; confirm `executions`/`execution_steps`
  rows; exercise `wait`→`POST /api/resume/{id}`; watch SSE update node status live; `curl` each
  endpoint and diff JSON shapes vs. the current Node responses.
- Phase 1: `go test ./...` incl. the smoke-equivalent; manual credential create→use; copilot
  returns valid `GraphOp[]` the canvas applies.
- Phase 2: `vite build` + `docker compose up --build` → a single Go container + Postgres serves
  the full app at `PUBLIC_BASE_URL`; full canvas workflow (core + AI) end-to-end; run two Go
  instances behind a proxy to confirm stateless horizontal scaling.

## Out of scope
Auth/RBAC, workflow version history, node marketplace, cross-restart run recovery,
multi-tenant isolation, the farm vertical (removed).
