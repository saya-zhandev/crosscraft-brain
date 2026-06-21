# TODO — crosscraft-brain

---

## Brand consistency fixes ✅

All three hardcoded dark colors that leaked through merge — fixed 2026-06-21.
Root cause: React Flow / shadcn props, not Tailwind classes, so the rebrand sweep missed them.

| # | File | Line | Offending | Fix |
|---|------|------|-----------|-----|
| 1 | `apps/web/src/components/Editor.tsx` | 458 | ~~`<Background color="#1c2230" />`~~ | `color="var(--border-2)"` (Cloudy `#b1ada1`) |
| 2 | `apps/web/src/components/Editor.tsx` | 463 | ~~`maskColor="rgba(8,11,17,0.7)"`~~ | `maskColor="rgba(244,243,238,0.85)"` (Pampas `--bg`) |
| 3 | `apps/web/src/components/ui/dialog.tsx` | 19 | ~~`bg-black/60`~~ | `bg-[color-mix(in_srgb,var(--text)_60%,transparent)]` (warm near-black) |

---

## Mobile / Client Track — the crosscraft engine as a mobile backend

Rationale: crosscraft's Go binary is a **highly efficient, single-binary workflow
executor**. iOS/Android apps integrate via the REST API and push notifications;
the server handles all heavy lifting (OAuth2, API orchestration, AI, file
processing, scheduling) — the mobile app is a thin UI + triggers.

### Mobile enablers — built 2026-06-21

- [x] **API key auth** — `internal/auth`: bearer-token middleware (`cc_<nanoid>`),
      SHA-256 hashed, optional enforcement (`AUTH_REQUIRED=true`). Keys created
      via `POST /api/keys`, listed/deleted via `GET/DELETE /api/keys`. Keys can
      be embedded in webhook URLs (`?api_key=...`), making mobile-triggered
      webhooks trivially authenticated.
- [x] **Push notification node** — `core.pushNotification`: FCM HTTP v1 sender,
      JWT-assertion token exchange from service-account JSON. Sends to a device
      token with title/body/data payload. Works on both Android and iOS.
- [x] **Form trigger** — `core.formTrigger`: like webhook trigger but with
      required-field validation; designed for mobile form POSTs.
- [x] **Webhook Respond node** — `core.webhookRespond`: workflow reaches this
      node, sends a custom HTTP response (JSON body + status) to the caller,
      then suspends. Enables "POST form → process → respond with result → wait
      for next action" mobile interaction loops.
- [x] **FCM credential type** — `fcmServiceAccount` (project_id + service-account
      JSON key) in `credtype.Default()`.
- [x] **`db/mobile_schema.sql`** — `api_keys` table with hash index.

### Mobile enablers — next up

- [x] **OAuth2 for mobile** — PKCE flow (`S256`, `code_challenge`) shipped.
      `credtype.OAuth2.PKCE`, code_verifier generation/challenge, enabled on
      Google OAuth2.
- [x] **Load-options** — dynamic dropdown endpoint shipped.
      `GET /api/nodes/{type}/options?param=...&query=...&credentialId=...`
- [ ] **Deep-link resume** — mobile apps need to open `crosscraft://resume/{id}`
      URLs that POST to `/api/resume/{id}`. A mobile-optimized resume endpoint
      that accepts simpler payloads and returns compact JSON.
- [ ] **Barcode / QR trigger** — a `core.qrTrigger` that accepts `?code=...`
      query param (from mobile camera scanner), validates code format, and
      triggers workflows. Zero-config path: `POST /api/webhook/scan?api_key=...`
      with `{"code": "..."}` works today; this node adds code-format validation
      and lookup (SKU, serial, GS1).
- [ ] **Mobile webhook templates** — pre-built workflow templates for common
      mobile patterns: "Scan → Lookup → Respond", "Form → Validate → Notify",
      "Location → Geofence → Alert".
- [ ] **React Native / Flutter SDK** — thin TypeScript client lib that wraps the
      REST API + SSE stream + push notification registration. Ships as an npm
      package (`@crosscraft/mobile-client`).
- [ ] **SSE push bridge** — when a workflow reaches a `core.pushNotification`
      node, optionally bridge the SSE stream to the mobile device via FCM data
      message so the app can update its UI live.
- [ ] **Offline queue** — mobile-optimized trigger that accepts batched/
      timestamped items and replays them in order when connectivity returns.
- [ ] **Biometric / device attestation** — credential type that validates mobile
      device integrity (iOS DeviceCheck, Android SafetyNet/Play Integrity).

### Reprioritised existing items (mobile-first ordering)

| Priority | Item | Why mobile-first |
|----------|------|------------------|
| 1 | OAuth2 PKCE (done) | Mobile apps can't keep client secrets |
| 2 | Webhook Respond (done) | Mobile needs request → response loops |
| 3 | Load-options (done) | Mobile pickers (spreadsheets, channels) |
| 4 | SSE stream optimisation | Live run monitoring on mobile |
| 5 | Webhook trigger templates | Common mobile interaction patterns |
| 6 | Error + Execute Workflow (done) | Compose workflows from mobile triggers |
| 7 | Form Trigger (done) | Mobile form submissions |
| 8 | Push notifications (done) | Re-engage mobile users |
| 9 | API key auth (done) | Authenticate mobile clients |

---

## Integration Nodes Roadmap (Go-native)

Build a first-party catalog of integration nodes, in Go, prioritising the stacks our
users live in: **Google → Microsoft → Adobe**, then the long tail. n8n's node catalog
is the reference for *which operations matter* (resource → operation shape); the
implementation is our own native-Go `NodeDefinition`s, which buys us connection
pooling, real concurrency, streaming uploads/downloads, typed official SDKs, and a
single static binary.

> Legend: `[ ]` not started · `[~]` in progress · `[x]` done.
> Each node's bullets are its **operations** (n8n-style). A node is "done" only with
> its trigger(s), credential type, golden-path test, and a palette icon/description.

---

## How nodes work here (so this list is actionable)

- A node is a `schema.NodeDefinition` in `server/internal/nodes/<pack>/…`, registered
  in [main.go](server/cmd/crosscraft/main.go) via `registry.New().Register(...)`.
- Built-ins (`set`, `if`, `http`, `code`, `wait`, triggers) live in
  [nodes/core](server/internal/nodes/core); AI in [nodes/ai](server/internal/nodes/ai).
- New packs go in `server/internal/nodes/{google,microsoft,adobe}` and register the
  same way. Group them with `group: 'integration'` (or `'trigger'`/`'transform'`).
- Credentials: the AES-256-GCM store (`store.CreateCredential` / `ctx.Credential`)
  already holds arbitrary JSON. Param type `credential` + `credentialType` wires the
  picker. **Missing piece:** an OAuth2 authorization-code flow (see Phase 0).
- **Definition of Done per node:** operations implemented · OAuth2/credential type ·
  pagination + rate-limit/retry · trigger (poll or webhook) where n8n has one ·
  one end-to-end test (httptest or sandbox) · icon + description + param schema.

---

## Phase 0 — Foundational infra (blocks every OAuth integration)

These are prerequisites, not optional. Build once, reuse everywhere.

- [x] **OAuth2 credential flow** — `internal/oauth`: authorization-code
      (`GET /api/oauth2/auth-url` + `/callback`) **and** client-credentials
      (server-to-server). Refresh + persist back to the encrypted credential blob.
      **PKCE shipped** (S256 code challenge, enabled for Google OAuth2).
- [x] **Credential *types* registry** — `internal/credtype` + `GET /api/credential-types`
      (Google / Microsoft / Adobe IMS / generic OAuth2 / header-auth / Adobe Sign).
- [x] **Per-service token source** — auto-refreshing `*http.Client` via
      `oauth.ClientForCredential`, wired to nodes through `ExecContext.AuthorizedClient`.
- [x] **Declarative REST node framework** — `internal/rest`: data-defined
      resources/operations → `NodeDefinition` (path interpolation, query/JSON body,
      header/OAuth2 auth, retry, response→items, shared-param dedupe, `BaseURLParam`).
- [x] **Pagination / rate-limit / retry** — 429/5xx retry with `Retry-After` done.
      **Cursor / page-token / offset pagination shipped** (`rest.Pagination`, auto
      page-walking with max-pages guard).
- [~] **Binary data handling** — in-memory base64 via `Item.Binary` works (Drive media
      upload/download). _Remaining:_ a streaming binary store (disk/S3) keyed by run so
      large files don't buffer in memory.
- [x] **Load-options ("resource locator")** — `GET /api/nodes/{type}/options?param=...`
      shipped. `NodeDefinition.LoadOptions` + `ParamSchema.HasDynamicOptions` +
      `Registry.LoadOptions`; UI gets `hasLoadOptions` in descriptors.
- [~] **Trigger infra** — **schedule/cron trigger shipped** (`internal/scheduler` +
      `core.scheduleTrigger`, interval + 5-field cron via robfig/cron). _Remaining:_
      generalised **polling triggers** (interval + dedupe cursor) and webhook-trigger
      registration for providers that push (Graph subscriptions, Adobe Sign webhooks);
      durable schedule state across restarts.
- [~] **Generic escape hatches** — `core.http` already works with header-auth
      credentials. _Remaining:_ `core.graphql`, per-vendor "raw API call" nodes.

---

## Phase 1 — Google Workspace & Cloud  (`nodes/google`)

**Go SDKs:** `google.golang.org/api/<svc>/<ver>` (sheets/v4, gmail/v1, calendar/v3,
drive/v3, docs/v1, slides/v1, people/v1, tasks/v1, forms/v1, chat/v1, youtube/v3,
analyticsdata/v1beta, …), `cloud.google.com/go/*` (storage, bigquery, firestore,
pubsub, translate, language, vision), auth via `golang.org/x/oauth2/google`.
**Auth:** OAuth2 (per-user) + Service Account / domain-wide delegation option.

### Workspace
- [x] **Google Sheets** — shipped: spreadsheet get/create; values get/append/update/clear.
      _Remaining:_ delete spreadsheet, delete rows/cols, header→object row mapping,
      **Trigger** (rowAdded/rowUpdated polling).
- [x] **Gmail** (read) — shipped: message list/get, label list.
      _Remaining:_ send/reply (MIME build), drafts, threads, mark read / labels,
      **Trigger** (polling).
- [x] **Google Calendar** — shipped: event list/get/create/delete, calendar list.
      _Remaining:_ event update, free/busy availability, **Trigger**.
- [x] **Google Drive** — shipped: file list/get/delete, folder create, **media
      upload/download** (`google.driveUpload` / `google.driveDownload`; multipart +
      `alt=media`). _Remaining:_ copy/move/share, create-from-text, shared drives,
      **Trigger**; true streaming via the binary store.
- [ ] **Google Docs** — Document: Create, Get, Update (insert/replace text, styling)
- [ ] **Google Slides** — Presentation: Create, Get, Replace Text, Get Page Thumbnail
- [ ] **Google Contacts (People API)** — Contact: Create, Get, Get Many, Update, Delete
- [ ] **Google Tasks** — Task: Create, Get, Get Many, Update, Delete; Task List: CRUD
- [ ] **Google Forms** + **Trigger** (new response) — Form: Get; Response: Get, Get Many
- [ ] **Google Chat** — Message: Create/Send; Space: Get, Get Many; Member: Get, Get Many
- [ ] **Gemini** — *already covered by AI nodes; add a Google-auth variant if needed*

### Google Cloud
- [ ] **Google Cloud Storage** — Bucket: CRUD; Object: Upload (stream), Download
      (stream), Get, Get Many, Update, Delete
- [ ] **BigQuery** — Execute Query (SQL); Record: Insert, Get Many; Dataset/Table: manage
- [ ] **Cloud Firestore** — Document: Create, Get, Get Many, Update, Delete, Query;
      Collection: list
- [ ] **Cloud Pub/Sub** — Publish Message; Subscription: Pull (+ trigger)
- [ ] **Cloud Translation** — Translate Text, Detect Language
- [ ] **Cloud Natural Language** — Analyze Sentiment / Entities / Syntax, Classify
- [ ] **Cloud Vision** — Label/Text/Face/Safe-search Detection (OCR)
- [ ] **Cloud Speech-to-Text / Text-to-Speech** — Transcribe / Synthesize (stream)

### Google Marketing / Media
- [ ] **Google Analytics (GA4)** — Report: Run; User Activity
- [ ] **Google Ads** — Campaign/AdGroup: Get, Get Many; report queries
- [ ] **Google Search Console** — Search Analytics query; Sitemaps
- [ ] **YouTube** — Video: Upload (stream), Get, Get Many, Update, Delete, Rate;
      Channel/Playlist/PlaylistItem: manage; Comment, Subscription, Search
- [ ] **Google Business Profile** + **Trigger** — Post, Review (reply), Location
- [ ] **Google Perspective** — Analyze Comment (toxicity)

---

## Phase 2 — Microsoft 365 & Azure  (`nodes/microsoft`)

**Go SDKs:** `github.com/microsoftgraph/msgraph-sdk-go` (Kiota) for 365; auth
`github.com/Azure/azure-sdk-for-go/sdk/azidentity`; Azure data via
`github.com/Azure/azure-sdk-for-go/sdk/...` (azblob, azcosmos); MSSQL via
`github.com/microsoft/go-mssqldb`. **Auth:** OAuth2 (Microsoft identity platform).

### Microsoft 365 (Graph) — **shipped** (declarative, `microsoftOAuth2Api`, Graph v1.0)
- [x] **Outlook** — core mail (list/get/send, …)
- [x] **Microsoft Calendar (Graph)** — events: list/get/create/delete
- [x] **Excel (Graph)** — worksheets + tables (rows)
- [x] **OneDrive** — files/folders (metadata)
- [x] **Microsoft Teams** — channels + messages
- [x] **Microsoft To Do** — task lists + tasks

### Microsoft tail — **next** (drafted)
- [ ] **Flesh out shipped services** — Outlook: reply, move, drafts, folders,
      attachments; Excel: range get/update + workbook sessions; Teams: channel CRUD +
      Planner tasks; Calendar: update + list calendars.
- [ ] **SharePoint** (Graph `…/sites/{siteId}`) — Site: Get/Search; List:
      Get/Get Many/Create; List Item: Get/Get Many/Create/Update/Delete; Drive/File:
      list/get + upload/download (binary). Declarative + a `siteId` load-options picker.
- [ ] **OneNote** (Graph) — Notebook: Get/Get Many; Section: Get/Get Many; Page:
      Get/Get Many/Create (HTML body)/Delete. Page-create is multipart (HTML + binary).
- [ ] **Microsoft Graph (generic)** — raw authenticated Graph call (method + path +
      query + JSON body) reusing the OAuth2 client; one declarative node, free-form path
      param — the escape hatch for anything unwrapped.
- [ ] **Triggers** (Outlook / Teams / OneDrive / SharePoint) — Graph **change-notification
      subscriptions** (webhooks) with subscription create/renew/validate, into the durable
      `wait`/resume plumbing; **delta-query polling** fallback. Needs Phase-0 trigger infra.
- [ ] **OneDrive / SharePoint media** — upload (`PUT /content`; resumable upload session
      for >4 MB) + download (`GET /content`) into `Item.Binary`, mirroring
      `google.driveUpload/Download`; true streaming via the binary store.
- [ ] **Dynamics 365 (CRM)** — Web API (`/api/data/v9.2`): Account, Contact, Lead,
      Opportunity + arbitrary entity: Create/Get/Get Many (OData `$filter`/`$select`)/
      Update/Delete. Declarative + `BaseURLParam` for the org URL; a `dynamicsOAuth2Api`
      cred (resource-scoped token).

### Azure — **next** (drafted; not pure Graph → native, not declarative)
- [ ] **Azure Blob Storage** — Container: list/create/delete; Blob: upload (stream),
      download (stream), list, delete, copy. Native via
      `azure-sdk-for-go/sdk/storage/azblob` + `azidentity` (a new credential kind:
      connection string or service-principal — not the OAuth2 REST client).
- [ ] **Azure Cosmos DB** — Database/Container: manage; Item: Create/Get/Query(SQL)/
      Upsert/Delete. Native via `sdk/data/azcosmos`.
- [ ] **Microsoft SQL Server** — Execute Query/Insert/Update/Delete: a **DB node** via
      `github.com/microsoft/go-mssqldb` (connection-string credential), parameterized
      queries; sibling to a future generic SQL node.
- [ ] **Power BI** (REST `api.powerbi.com/v1.0/myorg`) — Dataset: push rows + refresh;
      Report/Dashboard: Get/Get Many. Declarative.
- [ ] **Azure DevOps** (REST) — Work Item, Pipeline run, Repo/PR: get/create
      (+ trigger via service hooks). Declarative + `BaseURLParam` for the org.
- [ ] **Azure OpenAI** — AI variant of the LLM nodes via `AI_BASE_URL` (no new node).

---

## Phase 3 — Adobe  (`nodes/adobe`)

**Note:** Adobe ships **no official Go SDKs** → REST on the declarative framework.
Auth is ready: **`adobeOAuth2Api`** (IMS server-to-server / client-credentials) and
**`adobeSignApi`** (integration key). The remaining Adobe APIs below are mostly
**async job** flows (submit → poll → download) over **binary**, so they need the
Phase-0 streaming binary store + a small job-poll helper before they're built.

- [x] **Adobe Acrobat Sign** (e-signature) — shipped: agreement list/get/create, get
      signing URLs; library documents list. Auth: integration key (Bearer) via
      `adobeSignApi`; account shard overridable per node (`baseUrl`).
      _Remaining:_ send-for-signature, cancel, get documents, reminders, **Trigger**
      (signed/declined webhook).
- [ ] **Adobe PDF Services API** — the high-value Go workload (streamed file I/O):
  - Create PDF (from Office/HTML), Export PDF (→ Office/JPEG)
  - OCR, Compress, Linearize, Protect / Remove Protection
  - Combine/Merge, Split, Reorder/Rotate/Delete Pages, Insert/Replace Pages
  - **Extract** (text / tables / figures → JSON), **Document Generation** (template merge)
  - Get PDF Properties, Accessibility check/autotag
- [ ] **Adobe Firefly Services** — Generate Image (text-to-image), Generative Fill /
      Expand, Generate Object Composite (async jobs → poll)
- [ ] **Adobe Photoshop API** — Apply Edits, Smart Object replace, Run Action, Create
      Rendition (async jobs)
- [ ] **Adobe Lightroom API** — Auto-Tone, Apply Preset, Edit (async jobs)
- [ ] **Adobe Experience Manager (AEM) Assets** — Upload Asset (stream), Get, Update
      Metadata, Get Rendition
- [ ] **Adobe Analytics** — Run Report; Segments, Metrics, Dimensions: list
- [ ] **Adobe Stock** — Search, License, Get
- [ ] **Adobe Commerce (Magento)** — Customer, Product, Order, Invoice: CRUD (+ trigger)
- [ ] **Adobe Target** — Activity/Offer/Audience: manage (lower priority)

---

## Phase 4 — Core "function" nodes (n8n built-ins we still owe)

Beyond integrations, n8n ships logic/utility nodes. Several already exist; the rest
round out the editor so workflows don't need the Code node for everything.

**Have:** `manualTrigger`, `webhookTrigger`, `set` (Edit Fields), `if`, `http`,
`code`, `wait` — plus the Phase-4 batch below (all in `nodes/core`, unit-tested).

- [x] **Flow** (shipped): Switch, Filter, Merge (append), Split Out, Aggregate, Limit,
      Sort, Remove Duplicates, No Operation, Stop & Error, **Compare Datasets**
      (`core.compareDatasets`: dual-input, 4 output ports).
      _Remaining:_ Loop Over Items / Split in Batches (needs loop-back semantics in the
      engine).
- [~] **Triggers:** Schedule/Cron **shipped** (`core.scheduleTrigger`).
      **Form Trigger shipped** (`core.formTrigger`), **Error Trigger shipped**
      (`core.errorTrigger`), **Execute Workflow shipped** (`core.executeWorkflow`
      + engine `RunSubWorkflow`).
      _Remaining:_ Email (IMAP) Trigger, Manual chat trigger.
- [~] **Data:** shipped: Date & Time (now/parse/add/subtract), Crypto (hash / HMAC /
      Base64), Rename Keys, **Extract From File** (CSV/JSON/text), **Convert to File**
      (CSV/JSON), **Compression** (gzip/zip compress+decompress), **HTML Extract**
      (tag-strip), **JSON** (parse/stringify), **Sort Keys**.
      _Remaining:_ Edit Image, Extract From File (XML/PDF/ODS), Spreadsheet File (xlsx), Markdown.
- [~] **Comms primitives:** **Send Email (SMTP)**, **Execute Command**, **RSS Read**
      (RSS 2.0 + Atom 1.0) shipped. **Push Notification (FCM) shipped**
      (`core.pushNotification`).
      _Remaining:_ Read Email (IMAP), FTP/SFTP, SSH.
      **Webhook Respond shipped** (`core.webhookRespond`).
- [ ] **AI cluster (LangChain-style):** AI Agent, Basic LLM Chain, Q&A/Retrieval Chain,
      Vector Store (Pinecone/PGVector), Embeddings, Memory, Tool nodes, Output Parser,
      Text Splitter, Document Loader  *(builds on existing `nodes/ai` + goja tools)*

---

## Phase 5 — Common integrations backlog (broader n8n catalog, prioritised)

Ordered roughly by demand. Most are REST → declarative framework; webhooks where the
provider supports them.

- [~] **Communication:** **Slack**, **Discord**, **Telegram**, **Twilio** (SMS/WhatsApp)
      shipped in `nodes/comm`.
      _Remaining:_ triggers (polling), WhatsApp Business, Mattermost, Zoom, Webex.
- [~] **Productivity / PM:** **Notion**, **Airtable**, **Asana**, **Trello**, **ClickUp**,
      **Jira** (Cloud), **Linear**, **Todoist** shipped in `nodes/productivity`.
      _Remaining:_ triggers (polling), monday.com, Coda.
- [~] **CRM / Marketing:** **HubSpot**, **Pipedrive**, **Mailchimp**, **SendGrid** shipped
      in `nodes/crm`.
      _Remaining:_ Salesforce, Zoho CRM, Customer.io, Intercom, ActiveCampaign, Brevo.
- [~] **Dev / DevOps:** **GitHub**, **GitLab**, **Sentry** shipped in `nodes/dev`.
      _Remaining:_ triggers, Bitbucket, Jenkins, Docker, PagerDuty, Grafana.
- [ ] **Cloud / Storage / DB:** AWS (S3, SES, SQS, Lambda, DynamoDB, Textract,
      Rekognition), Postgres, MySQL, MongoDB, Redis, Snowflake, Supabase, Dropbox, Box
- [~] **Payments / Commerce:** **Stripe** shipped in `nodes/payments`.
      _Remaining:_ triggers, PayPal, Shopify (+trigger), WooCommerce, QuickBooks, Xero, Square.
- [ ] **AI / ML:** OpenAI, Hugging Face, Cohere, Mistral, Pinecone, Qdrant, ElevenLabs,
      Stability AI, Perplexity
- [ ] **Generic protocols:** GraphQL, gRPC, SOAP, MQTT, AMQP/RabbitMQ, Kafka, NATS,
      WebSocket

---

## Why Go makes these "highly efficient" (design notes)

- **Official typed SDKs** for Google & Microsoft (Kiota-generated Graph SDK) → less
  hand-rolled REST, fewer bugs, native streaming.
- **Connection pooling & keep-alive** shared across runs (one `*http.Client` per
  credential) instead of per-request clients.
- **Streaming binary I/O** for Drive/OneDrive/GCS/Blob/PDF — never buffer whole files;
  pipe through the run with bounded memory.
- **Real concurrency** — fan-out over items/pages with a bounded pool (reuse the
  engine's worker-pool pattern); rate-limit centrally.
- **Single static binary** — every integration ships in the one `crosscraft` binary;
  no per-node runtime, no plugin installs.

## Cross-cutting checklist (apply to every node)

- [ ] Credential type registered (OAuth2 scopes / API key fields)
- [ ] Pagination + rate-limit + retry (`Retry-After`, backoff)
- [ ] Streaming for any file upload/download
- [ ] Trigger (polling cursor or webhook) where n8n has one
- [ ] `continueOnFail` + structured error items (don't kill the run on one bad item)
- [ ] Load-options for pickers (spreadsheets, mailboxes, channels…)
- [ ] Golden-path test (httptest mock or sandbox account) + palette icon/description
