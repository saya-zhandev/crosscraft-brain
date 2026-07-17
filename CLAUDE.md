# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run Commands

```bash
# Development (frontend only — requires the Go backend running separately)
cd apps/web && pnpm dev                    # Vite dev server on :3000, proxies /api → :8080

# Full stack via Docker (recommended for quick start)
docker compose up --build                  # Postgres + Go binary on :8080

# Go backend
cd server && go build ./cmd/crosscraft     # Build the production binary
cd server && go build ./cmd/benchserver    # Build the profiling/benchmark binary
cd server && go test ./...                 # Run all Go tests
cd server && go test ./internal/... -run TestName  # Single test

# Frontend
cd apps/web && pnpm build                  # Production build → server/web/dist/
cd apps/web && pnpm typecheck              # TypeScript check (tsc --noEmit)

# Root-level helpers (Turborepo)
pnpm build                                 # Build all packages + apps
pnpm typecheck                             # Typecheck all packages + apps
pnpm db:migrate db/mobile_schema.sql       # Apply mobile schema on top of base schema
pnpm db:up                                 # Start Postgres container only
pnpm docker:up / docker:down / docker:reset  # Manage full stack
```

The Go binary embeds the built SPA from `server/web/dist/`. For production Docker builds, the `web` stage runs `pnpm --filter @crosscraft/web build`, then the `build` stage compiles the Go binary with `//go:embed all:dist`.

## Architecture

CrossCraft Brain is a **node-based workflow automation platform** — a visual canvas where users compose integrations by connecting nodes, and an engine executes those graphs with durable suspend/resume.

### Monorepo layout

```
crosscraft-brain/
├── apps/web/              # React 19 SPA (Vite + Tailwind v4 + xyflow canvas)
├── packages/schema/       # @crosscraft/schema — TypeScript types shared across the frontend
├── server/                # Go backend (single binary)
│   ├── cmd/crosscraft/    #   main.go — wires everything together
│   ├── internal/          #   all Go packages
│   └── web/               #   embed the built SPA (//go:embed all:dist)
├── proto/                 #   gRPC protobuf contract (crosscraft.proto)
├── db/                    #   PostgreSQL schema + migrations
└── docker-compose.yml     #   Postgres + server (one-command stack)
```

### Core concept: the workflow graph

A `Workflow` is a DAG of `WFNode` (typed node instances with params) connected by `WFEdge` (source → target with port handles). Items (`[]schema.Item` — arbitrary JSON objects) flow along edges. The Go `schema` package mirrors `@crosscraft/schema` byte-for-byte so the frontend and backend speak the same contract.

### Go backend subsystems (in initialization order)

1. **Registry** (`internal/registry/`) — In-memory map of `nodeType → NodeDefinition`. Each `NodeDefinition` carries metadata (label, icon, group, params, inputs/outputs) and an `Execute` function. Both the UI catalog (`GET /api/nodes`) and the engine read from the same registrations. Nodes register in `main.go` via `reg.Register(core.Nodes...).Register(google.Nodes()...)...`.

2. **Store** (`internal/store/`) — PostgreSQL persistence via pgxpool. Implements `engine.Store` (the execution-lifecycle contract) plus read queries for the HTTP API. Credential data is AES-encrypted at rest (`internal/crypto/`). Also provides `BinaryStore` — a swappable interface (disk-backed by default, S3-compatible) for large file uploads/downloads during node execution.

3. **Engine** (`internal/engine/`) — Topological DAG executor. A run walks the graph from its trigger node, executing each node when all upstream sources are done. After each step, the full `RunState` is checkpointed to Postgres (durable). A node may return a `SuspendRequest` — the run pauses and resumes when `/api/resume/{id}` is called (e.g., a webhook trigger waiting for an external caller). In production the engine runs an async worker pool (`StartWorkers` — 8 workers, 256-deep queue) and recovers any executions left `running` by a crashed process.

4. **API** (`internal/api/`) — Chi-based HTTP router at `/api`. Key endpoints: node catalog, workflow CRUD, run/resume/webhook, execution monitoring (including SSE stream), credentials, OAuth2 flow, copilot (LLM-powered graph builder), and API key management for mobile clients.

5. **Mobile gRPC** (`internal/mobile/`) — Implements `CrossCraftMobile` service from `proto/crosscraft.proto`. Thin adapter over the same engine + store. Multiplexed on the same port via `r.ProtoMajor == 2 && Content-Type: application/grpc` detection.

6. **Scheduler** (`internal/scheduler/`) — Polls active workflows every 15 seconds, fires those whose `core.scheduleTrigger` node is due (interval or cron mode). Tracks last-fire times in memory.

7. **LLM** (`internal/llm/`) — Anthropic Messages API client. Powers the Copilot (`POST /api/copilot` — natural language → graph ops) and any AI nodes. Supports any Anthropic-Messages-compatible endpoint via env vars.

8. **OAuth** (`internal/oauth/`) — OAuth2 flow handler. Also implements `engine.ClientProvider` so integration nodes can get authenticated `*http.Client` instances for REST calls.

9. **REST framework** (`internal/rest/`) — Declarative integration-node builder. Define a REST integration as data (resources, operations, auth config), call `Build()`, and get a complete `schema.NodeDefinition`. Handles pagination, retry with backoff, path interpolation, and dynamic params. Most Google, Microsoft, Adobe, and other integration nodes are built this way rather than hand-writing Execute functions.

10. **Expression engine** (`internal/expr/`) — Goja (Go JavaScript VM) based evaluator for `{{ expression }}` syntax in node params and the user Code node. Whole-string expressions return typed values; embedded expressions are interpolated as strings.

11. **Polling triggers** (`internal/triggers/`) — Generalized framework for interval-based change detection (e.g., new Gmail, new Sheets rows). Handles rate-limiting, deduplication via stable IDs, cursor management, and idle backoff with exponential interval doubling.

### Frontend (apps/web)

- **Canvas**: React Flow (`@xyflow/react`) renders the node graph. Nodes are positioned on an infinite canvas; edges connect output ports to input ports.
- **UI**: shadcn/ui-style components built on Radix UI primitives + Tailwind CSS v4 (`components/ui/`). CVA for variant management.
- **Routing**: React Router v7 — `Home`, `EditorRoute` (the workflow canvas), `Credentials`, `Executions`.
- **Key components**: `Editor.tsx` (canvas shell), `CcNode.tsx` (custom node renderer), `Inspector.tsx` (node params panel), `Palette.tsx` (node catalog sidebar), `Copilot.tsx` (AI chat for graph building).
- **API client**: `lib/client.ts` — typed fetch wrapper for the Go REST API.
- **State**: No external state library — components use React `useState`/`useRef`/`useEffect`. The canvas uses `@xyflow/react`'s `useNodesState`/`useEdgesState`. Undo/redo is managed via ref-based stacks (not a library).
- **Design tokens**: Two-layer system in `globals.css` — CSS custom properties define the brand palette, then `@theme inline {}` maps them to Tailwind v4 utility classes (e.g., `bg-panel`, `text-muted`).

### Node packs (17 integration categories)

All under `server/internal/nodes/`: `core` (flow, HTTP, files, code, JWT, etc.), `google`, `microsoft`, `aws`, `azure`, `adobe`, `ai` (OpenAI, Anthropic, Cohere, Mistral, etc.), `comm` (Slack, Discord, Telegram, Twilio), `commerce` (Shopify, WooCommerce), `accounting` (QuickBooks, Xero), `crm` (Salesforce, Zoho), `database` (Postgres, MySQL, MongoDB, Redis), `dev` (GitHub, GitLab, Sentry), `marketing` (Mailchimp, HubSpot, SendGrid), `payments` (Stripe, PayPal, Square), `productivity` (Notion, Airtable, Jira, Linear, Todoist), `protocols` (GraphQL, NATS, WebSocket), `storage` (Dropbox, Box).

### Adding a new integration node

1. Create a file in `server/internal/nodes/<vertical>/` with a `schema.NodeDefinition` (metadata + `Execute` func).
2. Export a `var Nodes = []schema.NodeDefinition{...}`.
3. Add `reg.Register(<vertical>.Nodes()...)` in `server/cmd/crosscraft/main.go`.
4. The node appears automatically in the canvas palette via `GET /api/nodes`. No frontend changes needed.

### Database

Core tables in `db/schema.sql`: `workflows` (graph as JSONB), `executions` (with `state` JSONB for resume), `execution_steps` (per-node I/O capture), `credentials` (encrypted). Mobile/client tables in `db/mobile_schema.sql`: `api_keys` (SHA-256 of bearer tokens), `device_tokens` (push notification targeting).

### Docker

Single `docker-compose.yml`: Postgres 16 Alpine + the Go server. Set secrets in `.env` at the repo root (Compose auto-loads it). `ANTHROPIC_API_KEY` / `AI_API_KEY` are optional — without them, AI features degrade gracefully.

### Key env vars

| Variable | Purpose |
|---|---|
| `DATABASE_URL` | Postgres connection string |
| `CREDENTIALS_SECRET` | 32-byte hex key for credential encryption at rest |
| `PUBLIC_BASE_URL` | Base URL for OAuth callbacks and webhook URLs |
| `ANTHROPIC_API_KEY` / `AI_API_KEY` | AI provider key (Copilot + AI nodes) |
| `AI_MODEL_FAST` / `AI_MODEL_SMART` | Model IDs for cheap/heavy AI calls |
| `PORT` | HTTP listen port (default 8080) |

### Testing

Go tests use standard `go test`. The `engine.Store` and `api.WorkflowStore`/`api.WorkflowRunner` are defined as interfaces at the consumer side — tests inject in-memory fakes. The binary store also provides `MemoryBinaryStore` for tests. Frontend has no test suite yet.
