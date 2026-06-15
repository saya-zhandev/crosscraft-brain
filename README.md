# crosscraft-brain

A **forkable workflow-automation platform** — its own visual canvas and its own execution
engine, shipped as a **single Go binary** that serves both the API and an embedded React SPA.
No n8n dependency. Fork it to build vertical automation products: add a node pack and rebuild.

Four pillars: **Visual Workflow Editor · Integrations · AI · Transparent Monitoring.**

## Architecture

- **`server/`** — Go backend: a topological execution engine with durable suspend/resume, a
  node registry, Postgres persistence (pgx), credentials crypto (AES-256-GCM), the REST + SSE
  API (chi), and the embedded SPA (`go:embed`). User `code` nodes and `{{ }}` expressions run
  in an embedded JS interpreter (goja); every other node is native Go.
- **`apps/web/`** — Vite + React SPA: the canvas (React Flow / `@xyflow/react`), palette,
  inspector, AI copilot, live run monitoring. Tailwind v4 + shadcn/ui; dark, dense, and
  theme-driven so a fork rebrands via CSS tokens rather than component edits.
- **`packages/schema/`** — the shared TypeScript contract (`Workflow`, `NodeDescriptor`,
  `ParamSchema`, `GraphOp`, …) the SPA speaks; the Go structs mirror it on the wire.
- **`db/`** — `schema.sql` (core tables) + `migrate.ts` (applies it).

The production build is one binary: `vite build` emits the SPA into `server/web/dist`, which
the Go binary embeds and serves alongside `/api`. Instances are stateless (Postgres is the only
shared state), so they scale horizontally behind a load balancer.

## The registry is the spine

Every node self-describes via a `NodeDefinition`. The **canvas** reads serializable descriptors
(`GET /api/nodes`) to build the palette and auto-generate config forms; the **engine** reads the
registry to execute. A fork adds nodes in one place — `server/internal/nodes/…` — and rebuilds.

## Durable suspend/resume (the load-bearing primitive)

A node may suspend the run; the engine persists the full run state to Postgres
(`executions.state`, `status='waiting'`) and returns. `POST /api/resume/{id}` rehydrates and
continues, injecting the payload as the resumed node's output — how long-running, multi-step
automations that pause at each stage (an approval, an external event) work.

## Quick start — one command (Docker)

```bash
cp .env.example .env        # optional: CREDENTIALS_SECRET + AI keys (sensible defaults otherwise)
docker compose up --build   # → http://localhost:8080
```

Two services: **postgres** → **server** (the single Go binary, started once Postgres is healthy).
Postgres applies `db/schema.sql` on first init of an empty data volume. Tear down with
`docker compose down` (keep data) or `docker compose down -v` (drop the DB volume).

## Quick start — local dev (hot reload)

```bash
corepack enable && pnpm install
cp .env.example .env
pnpm db:up                                       # Postgres on :5433 (docker)
node --env-file=.env --import tsx db/migrate.ts  # apply core schema
go -C server run ./cmd/crosscraft                # API + embedded UI on :8080
pnpm --filter @crosscraft/web dev                # Vite dev on :3000 (proxies /api → :8080)
```

Open the app, create a workflow, drag nodes from the palette, connect, configure in the
inspector, and hit **Run** — nodes light up live (Transparent Monitoring). The **Runs** page
shows every node's input/output for any execution.

> **AI provider:** set `ANTHROPIC_API_KEY` for Claude, or point at any Anthropic-Messages-
> compatible endpoint via `AI_BASE_URL` / `AI_API_KEY` / `AI_MODEL_FAST` / `AI_MODEL_SMART`
> (e.g. DeepSeek's `https://api.deepseek.com/anthropic` with `deepseek-chat`).

## Verify

```bash
go -C server test ./...   # engine + goja: run → suspend → resume, {{ }} expressions, etc.
```

HTTP surface: `GET /api/nodes`, `GET|POST /api/workflows`, `POST /api/workflows/{id}/run`,
`GET /api/executions/{id}/stream` (SSE), `POST /api/resume/{id}`, `POST /api/webhook/{path}`,
`GET|POST /api/credentials`, `POST /api/copilot`.

## Stack

Go (engine + API + `go:embed`) · goja · pgx/Postgres · chi · React 19 · Vite · react-router ·
Tailwind v4 · React Flow (`@xyflow/react`) · shadcn/ui · pnpm. AI via the Anthropic-compatible
Messages API (Claude, or any compatible endpoint).

## Scope (MVP)

Triggers = manual + webhook + resume. Expression engine is intentionally minimal. Auth/RBAC,
version history, a node marketplace, and cross-restart run recovery are future work.
