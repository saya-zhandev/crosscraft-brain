# crosscraft-brain

A **forkable workflow-automation platform skeleton** — its own visual canvas and its own
execution engine. No n8n dependency. Fork it to build vertical automation products: add a
node pack, a few tables, and branding — the core stays untouched.

Four pillars: **Visual Workflow Editor · Integrations · AI · Transparent Monitoring.**

> **Re-platform in progress:** the backend is moving to a single **Go** binary with an
> embedded **Vite** SPA (the Go server lives in `server/`). See [BUILD.md](BUILD.md) for the
> plan and status. The sections below describe the current (TypeScript) stack.

## Why it exists

We want to ship many vertical automation apps that each look like a focused product, not
like generic workflow software. crosscraft-brain owns the engine + canvas so every vertical
is a thin fork: add a node pack, a few tables, branding — the core is untouched.

## Monorepo layout

```
packages/
  schema/      the contract: Workflow, NodeDefinition, ParamSchema, GraphOp (shared by all)
  engine/      topological executor, durable suspend/resume, expression eval, Postgres persistence
  nodes-core/  trigger(manual/webhook), set, if, http, code, wait
  nodes-ai/    ai.summarize / ai.classify / ai.extract + LLM provider abstraction (Anthropic)
apps/
  studio/      Next.js app = Editor + Integrations + AI Copilot + Monitoring; hosts the engine API
server/        Go backend (engine + API), re-platform target — see BUILD.md
db/            schema.sql (core) + migrate.ts
```

## Key idea: the registry is the spine

Every node self-describes via `NodeDefinition`. The **canvas** reads serializable
descriptors (`/api/nodes`) to build the palette and auto-generate config forms; the
**engine** reads the registry to execute. A fork registers its pack in one place:
[apps/studio/lib/registry.ts](apps/studio/lib/registry.ts)
(`new Registry().register(...coreNodes, ...aiNodes)`).

## Durable suspend/resume (the load-bearing primitive)

A node may suspend the run (`kind: 'webhook'`); the engine persists the full run state to
Postgres (`executions.state`, `status='waiting'`) and returns. `POST /api/resume/{id}`
rehydrates and continues, injecting the payload as the resumed node's output. This is how
long-running processes that pause at each stage (an approval, an external event) work — the
load-bearing primitive for durable, multi-step automations.

## Quick start — one command (Docker)

Boots Postgres and serves the app from a single Go binary. Nothing else to install.

```bash
cp .env.example .env     # optional: set CREDENTIALS_SECRET + AI keys (sensible defaults otherwise)
pnpm docker:up           # = docker compose up --build  → http://localhost:8080
```

The stack has two services: **postgres** → **server** (the single Go binary serving the API +
embedded SPA, started once Postgres is healthy). Postgres applies the core schema itself on first init of an empty data volume
(`db/*.sql` mounted into its init dir). Tear down with `pnpm docker:down` (keep data) or
`pnpm docker:reset` (drop the DB volume, so the schema re-applies on the next `up`).

## Quick start — local dev (hot reload)

```bash
corepack enable && pnpm install
cp .env.example .env                 # set CREDENTIALS_SECRET; AI keys for Copilot/AI nodes
pnpm db:up                           # just Postgres on :5433 (docker)
node --env-file=.env --import tsx db/migrate.ts   # apply core schema
go -C server run ./cmd/crosscraft    # Go API + embedded UI on :8080
pnpm --filter @crosscraft/web dev    # Vite dev on :3000 (proxies /api -> :8080)
```

> **AI provider:** set `ANTHROPIC_API_KEY` for Claude, or point at any Anthropic-Messages-
> compatible endpoint via `AI_BASE_URL` / `AI_API_KEY` / `AI_MODEL_FAST` / `AI_MODEL_SMART`
> (e.g. DeepSeek's `https://api.deepseek.com/anthropic` with `deepseek-chat`).

Open the app (http://localhost:3000 in dev, :8080 from the binary), create a workflow, drag
nodes from the palette, connect, configure in the inspector, and hit **Run** — nodes light up
live (Transparent Monitoring). The **Runs** page shows every node's input/output for any run.

## Verify (no UI needed)

```bash
# Engine unit tests: run -> suspend -> resume -> success, goja {{ }} expressions, etc.
go -C server test ./...
```

The HTTP surface: `POST /api/webhook/{path}` to start, `POST /api/resume/{id}` to advance,
`GET /api/executions/{id}` to inspect.

## Stack

Next.js 15 · React 19 · TypeScript · Tailwind v4 · React Flow (`@xyflow/react`) · Postgres ·
pnpm + Turborepo · Anthropic Claude (`claude-haiku-4-5` in-node, `claude-sonnet-4-6` copilot).
Backend re-platform target: **Go** (`server/`) + **Vite** SPA — see [BUILD.md](BUILD.md).

## Scope (MVP)

Triggers = manual + webhook + resume (schedule/queue worker are post-MVP, behind the stable
`engine.run/resume` interface). Expression engine is intentionally minimal. Auth/RBAC,
versioning history and a node marketplace are future work.
