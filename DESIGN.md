# crosscraft-brain — UI/UX Design Reference

> Snapshot of the **current** studio UI/UX, written to brief a Claude Code revamp.
> It describes what exists today, the design tokens, the interaction flows, the hard
> constraints a revamp must preserve, and the known rough edges worth fixing.

## 1. Product context (what the UI is for)

The studio is the front end of a **forkable workflow-automation platform**. It clones the
*ergonomics* of n8n (palette ▸ canvas ▸ inspector, drag-to-build, run-and-watch) but is our
own React app with our own brand and our own engine. Verticals **fork**
the platform and add node packs + branding — so the UI must stay generic and theme-driven.

Audience: technical-ish operators building and monitoring automations. Density and clarity
matter more than marketing polish.

The four product pillars the UI must surface: **Visual Editor · Integrations · AI · Transparent Monitoring.**

## 2. Tech & where styling lives

- **Next.js 15 (App Router) · React 19 · TypeScript.**
- Canvas: **React Flow** (`@xyflow/react`) — [apps/studio/components/Editor.tsx](apps/studio/components/Editor.tsx).
- **Tailwind v4 and shadcn deps are installed but NOT actually used** — current components are
  styled with **inline `style={{}}` objects** plus a handful of utility classes
  (`.btn`, `.input`, `.textarea`, `label.fld`) and CSS variables in
  [apps/studio/app/globals.css](apps/studio/app/globals.css). This inconsistency is the #1
  thing to clean up (see §7).
- Icons: **lucide-react** (node icons are referenced by string name in each node's descriptor).
- Theme is **dark only**.

## 3. Design tokens (current)

Defined as CSS variables in [globals.css](apps/studio/app/globals.css):

| Token | Value | Use |
|------|-------|-----|
| `--bg` | `#0b0e14` | app background / canvas / input bg |
| `--panel` | `#11151d` | surfaces |
| `--panel-2` | `#161b26` | raised surfaces, buttons, node body |
| `--border` | `#232a37` | hairlines |
| `--text` | `#e6e9ef` | primary text |
| `--muted` | `#8b94a7` | secondary text/labels |
| `--accent` | `#6366f1` (indigo) | primary action, handles |
| `--accent-2` | `#818cf8` | hover, selected edge |
| `--ok` `--warn` `--err` `--wait` | `#22c55e` `#f59e0b` `#ef4444` `#38bdf8` | run status |

**Node *group* colors** (hardcoded in [CcNode.tsx](apps/studio/components/CcNode.tsx) and
[Palette.tsx](apps/studio/components/Palette.tsx)) — these map a node's `group` to an accent:

| group | color | examples |
|-------|-------|----------|
| trigger | `#22c55e` green | Manual, Webhook, Harvest |
| transform | `#6366f1` indigo | Set Fields |
| flow | `#f59e0b` amber | If, Code, Wait, Close Lot |
| integration | `#38bdf8` sky | HTTP, Record CTE |
| ai | `#a855f7` purple | Summarize, Classify, Extract |

**Run-status borders** on nodes: running=amber, success=green, error=red, waiting=sky.

Type: system UI sans for chrome; monospace for code/JSON fields. Radii ~8px (controls),
10–12px (cards/nodes). No spacing scale — paddings are ad-hoc inline values.

## 4. Information architecture / screens

```
/                      Home — workflow list + "create" (apps/studio/app/page.tsx)
/editor/[id]           Editor — the main workspace (components/Editor.tsx)
/executions/[wfId]     Runs — history + per-run step I/O (app/executions/[workflowId]/page.tsx)
```

### Home (`/`)
Centered ~720px column on dark bg. Title "crosscraft / workflow studio", a one-line "create
workflow" input + button, then a vertical list of workflow cards (name + active dot). Minimal,
unbranded, no empty-state art.

### Editor (`/editor/[id]`) — the core screen
Full-viewport, three-zone n8n-style layout with a top bar:

```
┌─────────────────────────────────────────────────────────────────────────┐
│ ← | <workflow name (inline edit)>      status •  □active  ✨Copilot  Runs  Save  ▶Run │  top bar
├──────────┬──────────────────────────────────────────────┬───────────────┤
│ Palette  │                                              │  Inspector     │
│ (220px)  │            React Flow canvas                 │   OR Copilot   │
│ grouped, │   dotted bg, nodes w/ handles, controls      │   (340px,      │
│ draggable│                                              │   right slot)  │
└──────────┴──────────────────────────────────────────────┴───────────────┘
```

- **Top bar** ([Editor.tsx](apps/studio/components/Editor.tsx)): back link, inline-editable
  name, small live **status text** (`saving… / running… / waiting · resume: POST …/success`),
  an **active** checkbox, **Copilot** toggle, **Runs** link, **Save**, **Run** (accent).
- **Palette** ([Palette.tsx](apps/studio/components/Palette.tsx)): fixed 220px left rail,
  nodes grouped under headers Triggers / Transform / Flow / Integrations / AI; each is a small
  draggable card (icon + label). **No search/filter.** Drag onto canvas to add.
- **Canvas**: React Flow with a dotted `Background`, bottom-left `Controls` (zoom/fit). Edges
  are grey, accent when selected. **No minimap, no undo/redo, no multi-select copy/paste.**
- **Nodes** ([CcNode.tsx](apps/studio/components/CcNode.tsx)): rounded card, left = a
  group-colored icon chip + node name + (smaller) node-type label; **input handle** on the
  left, **output handle(s)** on the right (stacked with tiny labels when >1, e.g. If→true/false).
  Border recolors to the live run status.
- **Right slot** is shared/mutually-exclusive: selecting a node shows the **Inspector**;
  toggling Copilot replaces it. You can't see both at once.
  - **Inspector** ([Inspector.tsx](apps/studio/components/Inspector.tsx)): node-type label,
    Delete, editable node **name**, then an **auto-generated form from the node's `ParamSchema`**
    (string→input, number→number, boolean→checkbox, select→dropdown, json/expression→textarea,
    credential→text id). Below: the **last run's status + output/input/error** for that node
    (raw JSON in `<pre>`).
  - **Copilot** ([Copilot.tsx](apps/studio/components/Copilot.tsx)): simple chat column
    (assistant/user bubbles, indigo for user), one text input + Send. Posts the prompt + current
    graph to `/api/copilot`; applies returned graph ops to the canvas.

### Runs (`/executions/[workflowId]`)
Header with back-to-editor + "Transparent monitoring" tagline. Left column = list of run
cards (short id + status dot + timestamp). Selecting one reveals a right column listing each
**step** with status and its **output/error JSON**. Two-column when a run is open, one when not.

## 5. Key interaction flows

1. **Author**: drag node from palette → drop → click to open Inspector → fill params →
   drag handle-to-handle to connect → **Save**.
2. **Run & monitor (live)**: **Run** saves then starts an execution and opens an SSE stream
   (`/api/executions/{id}/stream`); node **borders light up** by status in real time; if the
   run suspends, the status line shows the **resume URL**. Click a node to inspect its I/O.
3. **Resume (durable wait)**: a waiting run is advanced by an external `POST /api/resume/{id}`
   (today done outside the UI / via the vertical's field app) — there is **no in-UI "resume"
   affordance** yet.
4. **AI copilot**: open panel → describe a workflow in NL → nodes/edges appear on the canvas
   for review.
5. **History**: Runs page to review past executions and per-node data.

## 6. Hard constraints a revamp MUST preserve

- **Registry-driven UI**: the palette and the Inspector form are generated from serializable
  **node descriptors** fetched at `/api/nodes` (`type, label, group, icon, inputs, outputs,
  params: ParamSchema[]`). Do **not** hardcode node lists or per-node forms — keep rendering
  from `ParamSchema` so any fork's nodes appear automatically. Types:
  [packages/schema/src/index.ts](packages/schema/src/index.ts).
- **Group + status color semantics** (§3) are meaningful, not decorative — keep the mapping
  even if the palette changes.
- **Theme via tokens**: a fork rebrands by overriding tokens, not by editing components. The
  revamp should make this *more* true (a real token layer), not less.
- **Graph contract**: canvas reads/writes the `Workflow { nodes, edges }` shape and applies
  `GraphOp[]` from the copilot. Save/load via the `/api/workflows` SDK ([lib/client.ts](apps/studio/lib/client.ts)).
- **Live monitoring** stays SSE-driven from `execution_steps`; node status overlay is core to
  "transparent monitoring."
- Dark, dense, operator-focused canvas feel.

## 7. Known limitations / revamp opportunities

- **Styling is inline + ad-hoc.** Adopt the already-installed **Tailwind v4 + shadcn/ui**
  consistently; extract a proper **design-token layer** (CSS vars → Tailwind theme) that forks
  can override for branding. Remove inline-style sprawl.
- **No component library / spacing scale / typography scale** — define them.
- **Right panel is single-slot** (Inspector *or* Copilot). Consider docked, resizable panels
  so config + AI + run-output can coexist.
- **Palette**: no search, no descriptions on hover beyond `title`, no favorites/recents.
- **Canvas**: add minimap, undo/redo, multi-select, copy/paste, alignment, keyboard shortcuts,
  edge labels, and a friendly **empty state** ("drag a trigger to start").
- **No in-UI resume / manual-trigger payload / credentials management** (the credentials API
  exists at `/api/credentials` but has no screen).
- **Monitoring is a separate page**; consider a run drawer/tab within the editor and a true
  per-run **graph replay** overlay (not just a step list).
- **Status feedback is a tiny text label** — add toasts, a run status pill, error surfacing.
- **Home is bare**; needs workflow cards with status/last-run, search, and branding slots.
- **No light mode, no responsive/mobile, accessibility not addressed** (focus states, ARIA,
  keyboard nav, contrast).
- **Expression/JSON fields are plain textareas** — opportunity for validation, `{{ }}`
  autocomplete against upstream node outputs, and inline errors.
- **Branding hooks**: logo, product name ("crosscraft"), and accent are not centralized for
  forks — make them config.

## 8. File map (for the revamp)

```
apps/studio/app/globals.css            theme tokens + utility classes (start here)
apps/studio/app/page.tsx               Home
apps/studio/app/editor/[id]/page.tsx   loads workflow, renders <Editor>
apps/studio/app/executions/[workflowId]/page.tsx   Runs/monitoring
apps/studio/components/Editor.tsx      top bar + 3-zone layout + run/SSE + copilot wiring
apps/studio/components/Palette.tsx     left node palette (grouped, draggable)
apps/studio/components/CcNode.tsx      React Flow custom node (icon chip, handles, status)
apps/studio/components/Inspector.tsx   right config panel (auto-form from ParamSchema) + run I/O
apps/studio/components/Copilot.tsx     right AI chat panel
apps/studio/lib/client.ts              typed API client (workflows/runs/nodes)
packages/schema/src/index.ts           ParamSchema / NodeDefinition / Workflow / GraphOp (the UI contract)
```

> Revamp goal in one line: **same registry-driven, n8n-style ergonomics and live monitoring,
> rebuilt on a consistent Tailwind/shadcn token system that forks can rebrand — with a richer
> canvas, coexisting panels, and proper feedback/empty/loading/error states.**
