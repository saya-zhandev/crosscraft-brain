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

- [ ] **OAuth2 credential flow** — authorization-code + PKCE, refresh-token storage,
      automatic token refresh on 401. Routes: `GET /api/oauth2/authorize?credType=…`
      (redirect to provider), `GET /api/oauth2/callback` (exchange code → store
      tokens). Persist `access_token`/`refresh_token`/`expiry` in the encrypted
      credential blob.
- [ ] **Credential *types* registry** — declarative `CredentialType{ name, fields,
      oauth2: {authUrl, tokenUrl, scopes, …} }` so Google/MS/etc. self-describe (the
      canvas renders the right form / "Connect" button). Mirrors n8n's credential
      types.
- [ ] **Per-service token source** — `oauth2.TokenSource` adapter that reads/writes the
      credential store and auto-refreshes; hand the resulting `*http.Client` to each
      vendor SDK.
- [ ] **Declarative REST node framework** — a Go helper to define a node from data
      (`resource → operation → {method, path, qs, body, pagination}`) so REST-backed
      nodes (most of them) are ~50 lines, like n8n's *declarative* nodes. This is the
      biggest force-multiplier; build it before Phase 1 scales.
- [ ] **Pagination / rate-limit / retry helpers** — cursor + page-token + offset
      strategies; exponential backoff with `Retry-After`; concurrency-bounded fan-out.
- [ ] **Binary data handling** — stream large files to/from nodes (uploads/downloads)
      without buffering whole files in memory; a binary store (disk/S3) keyed by run.
- [ ] **Load-options ("resource locator")** — `GET /api/nodes/{type}/options?...` so
      params can offer dynamic dropdowns (pick a spreadsheet / mailbox / channel).
- [ ] **Trigger infra** — generalised **polling triggers** (interval + dedupe cursor)
      and **schedule/cron trigger**; webhook-trigger registration for providers that
      push (Graph subscriptions, Adobe Sign webhooks). Extends the existing
      `core.webhookTrigger` + durable `wait`/resume.
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
- [ ] **Google Sheets** + **Trigger** (`rowAdded`, `rowUpdated` — polling)
  - Spreadsheet: Create, Delete
  - Sheet: Append Row, Append-or-Update Row, Update Row, Get Row(s)/Lookup, Clear,
    Create, Delete, Delete Rows/Columns
- [ ] **Gmail** + **Trigger** (message received — polling)
  - Message: Send, Reply, Get, Get Many, Delete, Mark Read/Unread, Add/Remove Labels
  - Draft: Create, Get, Get Many, Delete
  - Label: Create, Get, Get Many, Delete
  - Thread: Get, Get Many, Reply, Trash/Untrash, Delete, Add/Remove Labels
- [ ] **Google Calendar** + **Trigger** (event created/updated/started/ended)
  - Calendar: Availability (free/busy)
  - Event: Create, Get, Get Many, Update, Delete
- [ ] **Google Drive** + **Trigger** (file/folder created/updated in a folder)
  - File: Upload, Download, Create-from-text, Copy, Move, Share, Update, Delete
  - Folder: Create, Share, Delete
  - File/Folder: Search
  - Shared Drive: Create, Get, Get Many, Update, Delete
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

### Microsoft 365 (Graph)
- [ ] **Outlook** + **Trigger** (message received)
  - Message: Send, Reply, Get, Get Many, Move, Update, Delete; Attachment: add/get
  - Draft: Create/Update/Send; Folder + Folder Message: manage
  - Event (calendar), Contact: CRUD
- [ ] **Microsoft Excel (Graph)**
  - Workbook: Get, Get Many; Worksheet: Get, Get Many, Clear, Delete
  - Table: Add Row, Get Rows, Lookup, Get Columns; Range: Get, Update
- [ ] **OneDrive** — File: Upload (stream), Download, Copy, Rename, Share, Search,
      Delete; Folder: Create, Get Children, Rename, Search, Delete
- [ ] **SharePoint** — Site: Get; List: Create, Get, Get Many; List Item: CRUD; File: manage
- [ ] **Microsoft Teams** + **Trigger** — Channel: CRUD; Channel Message: Create, Get Many;
      Chat Message: Create, Get, Get Many; Planner Task: CRUD
- [ ] **OneNote** — Notebook/Section: Get, Get Many; Page: Create, Get, Get Many, Delete
- [ ] **Microsoft To Do** — Task / Task List / Linked Resource: CRUD
- [ ] **Microsoft Graph (generic)** — raw authenticated Graph call (escape hatch)
- [ ] **Dynamics 365 (CRM)** — Account, Contact, Opportunity, Lead, etc.: CRUD + query

### Azure
- [ ] **Azure Blob Storage** — Container: manage; Blob: Upload (stream), Download
      (stream), List, Delete
- [ ] **Azure Cosmos DB** — Database/Container: manage; Item: Create, Get, Query, Delete
- [ ] **Microsoft SQL Server** — Execute Query, Insert, Update, Delete (DB node)
- [ ] **Power BI** — Dataset: push rows; Report/Dashboard: Get, Get Many
- [ ] **Azure DevOps** — Work Item, Pipeline, Repo: manage (+ trigger)
- [ ] **Azure OpenAI** — *AI variant of the LLM nodes via `AI_BASE_URL`*

---

## Phase 3 — Adobe  (`nodes/adobe`)

**Note:** Adobe ships **no official Go SDKs** → implement via REST on `net/http`
(perfect fit for the declarative framework). Auth is OAuth Server-to-Server / JWT
(Adobe Developer Console) per product.

- [ ] **Adobe Acrobat Sign** (e-signature) + **Trigger** (agreement signed/declined —
      webhook)
  - Agreement: Send for Signature, Get, Get Many, Cancel, Get Documents, Get Signing URL
  - Library Document, Reminder, Webhook: manage
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
- [ ] **Triggers:** Schedule/Cron, Error Trigger, Execute Workflow (+ trigger),
      Email (IMAP) Trigger, Form Trigger, Interval, Manual chat trigger
- [~] **Data:** shipped: Date & Time (now/parse/add/subtract), Crypto (hash / HMAC /
      Base64), Rename Keys. _Remaining:_ Edit Image, Compression (zip/gzip), Convert
      to/from File, Extract From File (CSV/JSON/XML/PDF/ODS), Spreadsheet File, HTML
      extract, XML, Markdown, JSON, Sort Keys.
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
