# crosscraft-brain

A **forkable workflow-automation platform skeleton** — its own visual canvas and its own
execution engine. No n8n dependency. Fork it to build vertical automation products; the
first fork is **FarmersFront** (produce supply-chain traceability), included here as the
`nodes-farm` example pack.

Four pillars: **Visual Workflow Editor · Integrations · AI · Transparent Monitoring.**

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
  nodes-farm/  EXAMPLE FORK pack: farm.startLot, recordCooling/Packing/Shipping, closeLot
apps/
  studio/      Next.js app = Editor + Integrations + AI Copilot + Monitoring; hosts the engine API
db/            schema.sql (core) + farm.sql (vertical) + migrate.ts
```

## Key idea: the registry is the spine

Every node self-describes via `NodeDefinition`. The **canvas** reads serializable
descriptors (`/api/nodes`) to build the palette and auto-generate config forms; the
**engine** reads the registry to execute. A fork registers its pack in one place:
[apps/studio/lib/registry.ts](apps/studio/lib/registry.ts)
(`new Registry().register(...coreNodes, ...aiNodes, ...farmNodes)`).

## Durable suspend/resume (the load-bearing primitive)

A node may `ctx.suspend({ kind: 'webhook' })`; the engine persists the full run state to
Postgres (`executions.state`, `status='waiting'`) and returns. `POST /api/resume/{id}`
rehydrates and continues, injecting the payload as the resumed node's output. This is how
"one lot = one long-running execution that pauses at each stage" works — generalized from
the validated `farmersback` webhook-wait pattern, now fully owned.

## Quick start

```bash
corepack enable && pnpm install
cp .env.example .env                 # set CREDENTIALS_SECRET; ANTHROPIC_API_KEY for AI
pnpm db:up                           # Postgres on :5433 (docker)
node --env-file=.env --import tsx db/migrate.ts            # core tables
node --env-file=.env --import tsx db/migrate.ts db/farm.sql # + farm vertical tables
pnpm --filter @crosscraft/studio dev # studio on http://localhost:3000
```

Open the studio, create a workflow, drag nodes from the palette, connect, configure in the
inspector, and hit **Run** — nodes light up live (Transparent Monitoring). The **Runs** page
shows every node's input/output for any past execution.

## Verify (no UI needed)

```bash
# Engine: run -> suspend -> resume -> success, with per-step I/O persisted
node --env-file=.env --import tsx scripts/engine-smoke.ts
```

The studio HTTP surface mirrors this: `POST /api/webhook/{path}` to start, `POST
/api/resume/{id}` to advance, `GET /api/executions/{id}` to inspect.

## The FarmersFront fork (proof of forkability)

`nodes-farm` + `db/farm.sql` + one line in `lib/registry.ts` + `/api/farm/report` turn the
skeleton into a real vertical. Run the lot lifecycle entirely on this engine:

```bash
# start a lot (Harvest), then advance Cool/Pack/Ship, then get the trace report
curl -X POST localhost:3000/api/webhook/start-lot -H 'content-type: application/json' \
  -d '{"commodity":"Romaine","harvest_date":"2026-06-09","location":"Field B7"}'
# -> { executionId }   ... then 3x:
curl -X POST localhost:3000/api/resume/<executionId> -H 'content-type: application/json' \
  -d '{"location":"Cooler #2","kde":{"temp_f":34}}'
curl -X POST localhost:3000/api/farm/report -H 'content-type: application/json' \
  -d '{"executionId":"<executionId>"}'        # -> HTML trace (CTEs + KDEs + TLC)
```

One lot = one execution; each stage is a `core.wait` resumed by a field event; CTE/KDE rows
land in `events`. PDF rendering is the same Gotenberg step proven in `../farmersback`.

## Stack

Next.js 15 · React 19 · TypeScript · Tailwind v4 · React Flow (`@xyflow/react`) · Postgres ·
pnpm + Turborepo · Anthropic Claude (`claude-haiku-4-5` in-node, `claude-sonnet-4-6` copilot).

## Scope (MVP)

Triggers = manual + webhook + resume (schedule/queue worker are post-MVP, behind the stable
`engine.run/resume` interface). Expression engine is intentionally minimal. Auth/RBAC,
versioning history and a node marketplace are future work.
