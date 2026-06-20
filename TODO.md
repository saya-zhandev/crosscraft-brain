# TODO — Integration Nodes Roadmap (Go-native)

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
      _Remaining:_ PKCE.
- [x] **Credential *types* registry** — `internal/credtype` + `GET /api/credential-types`
      (Google / Microsoft / Adobe IMS / generic OAuth2 / header-auth / Adobe Sign).
- [x] **Per-service token source** — auto-refreshing `*http.Client` via
      `oauth.ClientForCredential`, wired to nodes through `ExecContext.AuthorizedClient`.
- [x] **Declarative REST node framework** — `internal/rest`: data-defined
      resources/operations → `NodeDefinition` (path interpolation, query/JSON body,
      header/OAuth2 auth, retry, response→items, shared-param dedupe, `BaseURLParam`).
- [~] **Pagination / rate-limit / retry** — 429/5xx retry with `Retry-After` done;
      cursor / page-token / offset pagination strategies still TODO.
- [~] **Binary data handling** — in-memory base64 via `Item.Binary` works (Drive media
      upload/download). _Remaining:_ a streaming binary store (disk/S3) keyed by run so
      large files don't buffer in memory.
- [ ] **Load-options ("resource locator")** — `GET /api/nodes/{type}/options?...` so
      params can offer dynamic dropdowns (pick a spreadsheet / mailbox / channel).
- [~] **Trigger infra** — **schedule/cron trigger shipped** (`internal/scheduler` +
      `core.scheduleTrigger`, interval + 5-field cron via robfig/cron). _Remaining:_
      generalised **polling triggers** (interval + dedupe cursor) and webhook-trigger
      registration for providers that push (Graph subscriptions, Adobe Sign webhooks);
      durable schedule state across restarts.
- [ ] **Generic escape hatches** — `core.graphql`, generic **OAuth2 HTTP Request**
      (auth-aware `http`), and per-vendor "raw API call" nodes (Google/Graph/Adobe)
      so anything not yet wrapped is still reachable.

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
      Sort, Remove Duplicates, No Operation, Stop & Error.
      _Remaining:_ Loop Over Items / Split in Batches (needs loop-back semantics in the
      engine), Compare Datasets.
- [~] **Triggers:** Schedule/Cron **shipped** (`core.scheduleTrigger`).
      _Remaining:_ Error Trigger, Execute Workflow (+ trigger), Email (IMAP) Trigger,
      Form Trigger, Manual chat trigger.
- [~] **Data:** shipped: Date & Time (now/parse/add/subtract), Crypto (hash / HMAC /
      Base64), Rename Keys, **Extract From File** (CSV/JSON/text), **Convert to File**
      (CSV/JSON). _Remaining:_ Edit Image, Compression (zip/gzip), Extract From File
      (XML/PDF/ODS), Spreadsheet File (xlsx), HTML extract, XML, Markdown, JSON, Sort Keys.
- [ ] **Comms primitives:** Send Email (SMTP), Read Email (IMAP), FTP/SFTP, SSH,
      Execute Command, RSS Read, Webhook Respond
- [ ] **AI cluster (LangChain-style):** AI Agent, Basic LLM Chain, Q&A/Retrieval Chain,
      Vector Store (Pinecone/PGVector), Embeddings, Memory, Tool nodes, Output Parser,
      Text Splitter, Document Loader  *(builds on existing `nodes/ai` + goja tools)*

---

## Phase 5 — Common integrations backlog (broader n8n catalog, prioritised)

Ordered roughly by demand. Most are REST → declarative framework; webhooks where the
provider supports them.

- [ ] **Communication:** Slack (+trigger), Discord, Telegram (+trigger), Twilio
      (SMS/WhatsApp), WhatsApp Business, Mattermost, Zoom, Webex
- [ ] **Productivity / PM:** Notion (+trigger), Airtable (+trigger), Asana, Trello,
      ClickUp, Jira, Linear, monday.com, Todoist, Coda
- [ ] **CRM / Marketing:** HubSpot, Salesforce, Pipedrive, Zoho CRM, Mailchimp,
      SendGrid, Customer.io, Intercom, ActiveCampaign, Brevo
- [ ] **Dev / DevOps:** GitHub (+trigger), GitLab, Bitbucket, Jenkins, Docker, Sentry,
      PagerDuty, Grafana
- [ ] **Cloud / Storage / DB:** AWS (S3, SES, SQS, Lambda, DynamoDB, Textract,
      Rekognition), Postgres, MySQL, MongoDB, Redis, Snowflake, Supabase, Dropbox, Box
- [ ] **Payments / Commerce:** Stripe (+trigger), PayPal, Shopify (+trigger),
      WooCommerce, QuickBooks, Xero, Square
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
