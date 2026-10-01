# Building an agent — implementing `agent.afm.md`

Read this when you are IMPLEMENTING an agent component. The contract it must
satisfy — the `/chat` wire shape, server-held memory, the identity gate, and
the allow-list as the security boundary — is in the skill body; this file is
how to build something that satisfies it.

An agent component is a TypeScript service running a Vercel AI SDK loop. Its
behaviour is fixed by `agent.afm.md` — a design-time contract you **implement**,
exactly as a service implements its `openapi.yaml`. There is no agent framework
to configure and no prompt to invent.

Everything a component obeys whatever its language — port, config, error shape,
dependency wiring — is the `aep` skill's component contract, not repeated here.
This file covers only what is different about an agent.

## What you generate, and from what

Both inputs are design-time contracts under `specs/`. **Neither is copied into
the component, and neither is read at runtime** — you compile them into code.

| Input | Output | Rule |
|---|---|---|
| the markdown body of `specs/design/components/<agent>/agent.afm.md` | `src/prompt.ts` — the body as a string constant | **verbatim**; never edit, extend or "improve" it |
| `x-aep.tools.openapi[].allow` in that document, plus `specs/design/components/<dep>/openapi.yaml` | `src/tools.ts` — one tool per allowed operation | only allow-listed operations; the sibling's contract stays where it lives |
| `model`, `max_iterations` | `src/config.ts`, `src/prompt.ts` constants | env var names come from the design's dependencies |
| `x-aep.attachments` | the `ATTACHMENTS` constant in `src/config.ts`, or `null` when the block is absent | copy `types`, `maxFiles`, `maxFileSizeMB` exactly |

The document's front matter uses AFM's own keys — `model`, `interfaces`,
`skills`, `max_iterations` — and puts everything platform-specific under
`x-aep`. Read extensions from there and nowhere else.

## Development flow

1. **Read the contracts** — the agent's `agent.afm.md`, and the `openapi.yaml` of
   every `component` dependency it names. Never edit anything under `specs/`.
2. **Generate** `src/prompt.ts` and `src/tools.ts` from them, per the table above.
3. **Implement** the loop and the HTTP surface — `src/agent.ts`, `src/main.ts`.
4. **Verify** — from the app path: `npm install && npm run build`.
5. **Evaluate** — run the agent's scenarios and write the report, per
   "Evaluate before you open the PR" below.
6. **PR** — only once step 4 exits 0, carrying the report step 5 wrote.

## Writing a tool from an OpenAPI operation

A tool is three things to a model: a **name**, a **description**, and a
**parameter schema**. An operation already carries all three.

- `operationId` → the tool name
- `summary` (and `description`) → the tool description, as prose a model reads
- path parameters + query parameters + request-body properties → **one flat
  `zod` object**. The model calls a tool with a single argument object; making it
  reason about which value belongs in the path and which in the body leaks your
  plumbing into its prompt. Split the values back out when you build the request.
- header parameters are **not** the model's — they are yours

```ts
addItem: tool({
  description: "Add an item to an open round",
  inputSchema: z.object({
    roundId: z.string(),
    description: z.string().describe("what the teammate wants, in their words"),
    quantity: z.number().int().min(1).optional().describe("defaults to 1"),
    price: z.number().optional().describe("omit when the teammate did not give one"),
  }),
  execute: ({ roundId, ...body }) =>
    call("POST", `/rounds/${encodeURIComponent(roundId)}/items`, body),
}),
```

Share one `call()` helper for every operation: it joins the injected base
address with `new URL` (never string concatenation), attaches the caller's
credential, and shapes the result. **Parse the response body defensively** — a
provider that returns HTML or an empty body on an error must still produce a
tool result, not throw:

```ts
const text = await response.text();
let body: unknown = null;
try { body = text ? JSON.parse(text) : null; } catch { body = text; }
return { ok: response.ok, status: response.status, body };
```

## The HTTP surface

Two routes on `node:http`, and no more. **Do not add an HTTP framework** —
Express, Fastify and friends earn nothing at this size and are a dependency the
platform then carries.

**Listen on `PORT`, and on 9090 when it is unset.** 9090 is the component
contract's port and stays the default, so a deployed agent is unaffected. The
variable exists because evaluation boots the agent again and again on one
machine: it hands the child an ephemeral port, and an agent that ignores it
binds an address the previous boot still holds and never becomes ready.

```ts
port: Number(process.env.PORT ?? 9090),
```

| Route | Behaviour |
|---|---|
| `POST /chat` | `{ conversationId?, message, attachments? }` in; **`{ conversationId, text, toolCalls }` out**. History lives in the agent's conversation store, never on the wire |
| `GET /healthz` | `200 {ok:true}`, or `503 {ok:false, missing:[…], store:"initialising"}` — `missing` lists UNSET env-var names, `store` is `"ready"` or `"initialising"`. They are separate: a fully-configured agent whose database is still provisioning has an EMPTY `missing` and `store:"initialising"` |

The response shape is fixed, not yours to choose:

- **`conversationId`** — the caller's only piece of state. ABSENT means "new
  conversation": create one and return its id. An id that is present but does
  not resolve for this user is **404, never a new conversation** — adopting a
  caller-chosen id lets one caller pick another's id and write under it. A
  caller never sends history and never receives it.
- **`text`** — the reply, as a plain string. This is what a UI renders.
- **`toolCalls`** — what a test asserts on.

**History is yours, not the caller's.** The document declares
`memory.type: server`: you load the conversation from your store, append the
caller's message, run the turn, append the FULL trail
(`result.steps.flatMap(s => s.response.messages)` — tool calls and results
included), and save. The caller replays nothing; a page refresh with the same
id continues the same conversation.

**A conversation belongs to one user.** Key every row by the gateway-injected
`x-user-id` and scope EVERY read and write with it. A request for a
conversation that does not exist under this user answers **404** — the same
404 whether the id is foreign or simply wrong, so an id leaks nothing.

**Message shapes never cross the wire.** `message` arrives as a plain string,
and `attachments` — only when the document declares `x-aep.attachments` — as
`[{ name, mediaType, data }]` with base64 `data`; `ModelMessage[]` lives only
between your store and the model call. There is nothing for a caller to
normalise and nothing for it to mis-render.

**Read the body with a cap, then validate, and answer 400.** A turn can carry
files, so read at most 24 MiB — 15 MiB of files is 20 MiB of base64, plus
framing — and answer **413** past it without reading on. A `message` that is
missing or not a string, a turn with neither text nor files, or a file the
document does not allow is a bad REQUEST, not a server error — letting it
through to the model call throws, which becomes a 500 and reads as the agent
being broken:

```ts
// src/config.ts: from x-aep.attachments, or null when the document declares none.
export const ATTACHMENTS: { types: string[]; maxFiles: number; maxFileSizeMB: number } | null = null;
const BODY_CAP = 24 * 1024 * 1024;

// Keep at most BODY_CAP. Past it, answer 413 ONCE and keep reading without
// keeping anything, so the client finishes sending and actually sees the 413 —
// destroying the request or closing the socket mid-upload resets the
// connection and the caller gets a network error instead. Past twice the cap,
// stop draining and drop it. Resolves null when the request was refused.
function readBody(req: IncomingMessage, res: ServerResponse): Promise<string | null> {
  return new Promise((resolve) => {
    const chunks: Buffer[] = [];
    let size = 0;
    let over = false;
    const refuse = () => { over = true; chunks.length = 0; sendJson(res, 413, { error: "request too large" }); };
    if (Number(req.headers["content-length"] ?? 0) > BODY_CAP) refuse();
    req.on("data", (chunk: Buffer) => {
      size += chunk.length;
      if (size > 2 * BODY_CAP) { req.destroy(); return; }
      if (over) return;
      if (size > BODY_CAP) { refuse(); return; }
      chunks.push(chunk);
    });
    req.on("end", () => resolve(over ? null : Buffer.concat(chunks).toString("utf8")));
    req.on("error", () => resolve(null));
    req.on("close", () => { if (!req.complete) resolve(null); });
  });
}

type Attachment = { name: string; mediaType: string; data: string };

// The whole request vocabulary. A field outside it is refused, so a caller
// speaking a newer contract than this agent was built for hears so, instead of
// having the field silently ignored.
const BODY_FIELDS = new Set(["conversationId", "message", "attachments"]);

function validate(body: any): { message: string; attachments: Attachment[] } | { error: string } {
  const unknown = Object.keys(body ?? {}).find((key) => !BODY_FIELDS.has(key));
  if (unknown) return { error: `unknown field: ${unknown}` };
  const message = typeof body?.message === "string" ? body.message.trim() : null;
  const attachments: Attachment[] = Array.isArray(body?.attachments) ? body.attachments : [];
  if (message === null) return { error: "expected { message: string }" };
  if (attachments.length > 0 && !ATTACHMENTS) return { error: "this agent does not accept attachments" };
  if (message === "" && attachments.length === 0) return { error: "expected a message or attachments" };
  if (ATTACHMENTS && attachments.length > ATTACHMENTS.maxFiles) return { error: `at most ${ATTACHMENTS.maxFiles} files per message` };
  let total = 0;
  for (const a of attachments) {
    if (typeof a?.name !== "string" || typeof a?.mediaType !== "string" || typeof a?.data !== "string") return { error: "each attachment needs name, mediaType and data" };
    if (!ATTACHMENTS!.types.includes(a.mediaType)) return { error: `${a.name}: this agent does not accept ${a.mediaType}` };
    const bytes = Buffer.byteLength(a.data, "base64");
    if (bytes > ATTACHMENTS!.maxFileSizeMB * 1024 * 1024) return { error: `${a.name}: larger than ${ATTACHMENTS!.maxFileSizeMB} MB` };
    total += bytes;
  }
  if (total > 15 * 1024 * 1024) return { error: "the files together are over 15 MB" };
  return { message, attachments };
}

// in the handler — `attachments` is declared before the try, so the catch can read it:
// const raw = await readBody(req, res); if (raw === null) return;
// const v = validate(JSON.parse(raw)); if ("error" in v) return sendJson(res, 400, { error: v.error });
// message = v.message; attachments = v.attachments;
```

A body that is not JSON is the same 400 (`expected { message: string }`).

## Conversation store

The design gives this component a `postgres-cnpg` platform-resource
dependency. Its connection details arrive as five env vars named
`<DEP_NAME>_<OUTPUT>`, uppercased — for a dependency named `memory-db`:
`MEMORY_DB_HOST`, `MEMORY_DB_PORT`, `MEMORY_DB_DBNAME`, `MEMORY_DB_USER`,
`MEMORY_DB_PASSWORD` (a shared `project-db` yields `PROJECT_DB_*`). Read them
in config like every other injected value. Dependency: `pg`.

**The database configuration is optional, and its absence is not a fault.**
Never list a `MEMORY_DB_*` variable in `/healthz`'s `missing`: an agent run
without one is correctly configured for the in-memory backing below, and
reporting it missing answers 503 forever. The required `MODEL_*` variables
(see "Model access") are what `missing` is for.

Map them exactly as below. The database name is the one to get right: the
resource's output is `dbname`, so the variable is `MEMORY_DB_DBNAME` — not
`MEMORY_DB_NAME`, which does not exist and which the field name below
otherwise invites.

```ts
memoryDbHost:     process.env.MEMORY_DB_HOST,
memoryDbPort:     process.env.MEMORY_DB_PORT,
memoryDbName:     process.env.MEMORY_DB_DBNAME,
memoryDbUser:     process.env.MEMORY_DB_USER,
memoryDbPassword: process.env.MEMORY_DB_PASSWORD,
```

**Copy this schema and these queries — do not redesign them.** One table, the
whole conversation as one JSONB value, loaded and saved as a unit. Two
backings implement one interface, and the configuration chooses between them:

```ts
// store.ts
import pg from "pg";
import type { ModelMessage } from "ai";
import { config } from "./config.js";

interface ConversationStore {
  init(): Promise<void>;
  load(id: string, userId: string): Promise<ModelMessage[] | null>;
  save(id: string, userId: string, messages: ModelMessage[]): Promise<void>;
}

const INIT = `CREATE TABLE IF NOT EXISTS conversations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id text NOT NULL,
  messages jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
)`;

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function postgresStore(): ConversationStore {
  // Built from the five injected parts — postgres-cnpg exposes no single URL
  // output. Never log this object: it carries the password. The field names
  // below match a dependency named `memory-db`; under the shared `project-db`
  // form, use that dependency's own prefix (`config.projectDbHost`, etc.) —
  // whatever your config.ts actually reads. Constructed HERE, not at module
  // load, so an agent running on the in-memory backing never opens a pool
  // against an address it was never given.
  const pool = new pg.Pool({
    host: config.memoryDbHost,
    port: Number(config.memoryDbPort),
    database: config.memoryDbName,
    user: config.memoryDbUser,
    password: config.memoryDbPassword,
  });

  return {
    init: () => pool.query(INIT).then(() => undefined),

    load: async (id, userId) => {
      if (!UUID_RE.test(id)) return null; // malformed = not found, no pg error
      const r = await pool.query(
        "SELECT messages FROM conversations WHERE id = $1 AND user_id = $2",
        [id, userId],
      );
      return r.rowCount ? (r.rows[0].messages as ModelMessage[]) : null;
    },

    // One idempotent statement covers both create and update: generate the id
    // in the app (crypto.randomUUID()), then upsert. A turn that fails after
    // the id is minted but before the save leaves nothing behind — there is no
    // separate "create the row" step to half-complete.
    save: async (id, userId, messages) => {
      await pool.query(
        `INSERT INTO conversations (id, user_id, messages)
         VALUES ($1, $2, $3::jsonb)
         ON CONFLICT (id) DO UPDATE
           SET messages = $3::jsonb, updated_at = now()
           WHERE conversations.user_id = $2`,
        [id, userId, JSON.stringify(messages)],
      );
    },
  };
}

// One process, one Map, nothing durable — see "Which backing, and when"
// below. It carries the SAME user fence as the SQL: a conversation belongs to
// one user here too, so a behaviour that holds in the cluster is the one an
// evaluation or a local run observes.
function memoryStore(): ConversationStore {
  const rows = new Map<string, { userId: string; messages: ModelMessage[] }>();
  return {
    init: async () => {}, // nothing to provision: ready the moment it exists
    load: async (id, userId) => {
      const row = rows.get(id);
      return row !== undefined && row.userId === userId ? row.messages : null;
    },
    save: async (id, userId, messages) => {
      const row = rows.get(id);
      if (row !== undefined && row.userId !== userId) return; // the upsert's WHERE
      rows.set(id, { userId, messages });
    },
  };
}

const store: ConversationStore = config.memoryDbHost ? postgresStore() : memoryStore();

// Never await initStore() before listen() — see "Never await initStore()"
// below for why. initStore() fires the schema init and returns immediately;
// ensureStore() is what every turn awaits, retrying until it succeeds.
let ready = false;
export function isStoreReady(): boolean {
  return ready;
}
export async function ensureStore(): Promise<void> {
  if (ready) return;
  await store.init();
  ready = true;
}
export function initStore(): void {
  ensureStore().catch((err) => {
    console.error("store not ready yet:", err);
  });
}

export function loadConversation(
  id: string, userId: string,
): Promise<ModelMessage[] | null> {
  return store.load(id, userId);
}

export function saveConversation(
  id: string, userId: string, messages: ModelMessage[],
): Promise<void> {
  return store.save(id, userId, messages);
}
```

**Which backing, and when.** A DEPLOYED agent has the `postgres-cnpg`
dependency injected, so `MEMORY_DB_HOST` is set and every turn goes to
Postgres — durable, shared by every replica, and what the platform promises.
The in-memory backing is for the two situations where no database exists:
**build-time evaluation**, which boots the agent with no `MEMORY_DB_*` so a
scenario can prove the agent remembers a second turn without provisioning a
database for it, and **running the component locally**. It keeps one process's
conversations, loses them on restart, and is not shared between replicas.
Durability is not optional in production; it is supplied by the dependency the
design already declares.

**The `AND user_id = $2` on every statement IS the security boundary.** The
store, not the prompt and not the caller, is what keeps one traveler out of
another's conversation. Never write a query on `conversations` without it.

The handler flow, exactly:

```ts
// 1. gate: 401 without x-user-id (unchanged)
// 2. readBody (413 past 24 MiB) and validate (above) → else 400
// 3. await ensureStore() — 500 while the DB isn't ready yet; then:
//    conversationId present → loadConversation(id, userId); null → 404 { error: "conversation not found" }
//    absent → id = crypto.randomUUID(), history = [] (no row yet — the first save creates it)
// 4. the user message: text plus one file part per attachment (the AI SDK's own parts)
//    const user = { role: "user", content: [
//      ...(message ? [{ type: "text", text: message }] : []),
//      ...attachments.map((a) => ({ type: "file", data: a.data, mediaType: a.mediaType, filename: a.name })),
//    ] };
//    const full = [...history, user]
// 5. const turn = await traceTurn(
//      { conversationId: id, model: config.modelName, system: genAiSystem, message },
//      (hooks) => runTurn(full, hooks))   // see "Model access", "The model call" and "Tracing"
// 6. store text, not files — each file part becomes a note naming it:
//    const stored = { ...user, content: user.content.map((p) => p.type === "file"
//      ? { type: "text", text: `[attached: ${p.filename} (${p.mediaType})]` } : p) };
//    await saveConversation(id, userId, [...history, stored, ...turn.steps.flatMap(s => s.response.messages)])
//    — this INSERT..ON CONFLICT is the only place a row is created, so a turn
//    that throws in step 5 leaves nothing in the store to orphan
// 7. sendJson(res, 200, { conversationId: id, text: turn.text, toolCalls: turn.toolCalls })
```

**Wrap the whole of that in `try`/`catch`, and never let a rejection escape.**
Steps 3, 5 and 6 all reach the network — the database, then the model provider
— so every one of them throws in normal operation: a database still
provisioning, an unset or rejected `MODEL_API_KEY`, a provider timeout. An
async handler that throws inside `node:http` produces an UNHANDLED REJECTION,
and Node's default is to terminate the process. The pod then crash-loops on
the first user message, which is the exact failure the rest of this section
promises it will not have. A caught error is one 500 and a live agent; an
uncaught one takes the agent down for everybody.

```ts
server.on("request", (req, res) => {
  void handle(req, res).catch((err) => {          // the last line of defence:
    console.error("chat turn failed:", err);      // `void handle(...)` alone
    if (!res.headersSent) sendJson(res, 500, { error: "internal error" });
    else res.destroy();                           // already streaming: cut it
  });
});
```

Log the error, never the request body or the `Authorization` header — a chat
turn carries the user's own words and their bearer. Log attachment names and
sizes, never their data.

**A guardrail block is the ONE upstream error you relay.** When an agent's model
access is governed, the gateway can refuse a turn on policy grounds and answers
**422** with a body naming what intervened:

```json
{"message":{"action":"GUARDRAIL_INTERVENED",
            "actionReason":"Violation of applied word count constraints detected",
            "direction":"REQUEST",
            "interveningGuardrail":"word-count-guardrail"},
 "type":"WORD_COUNT_GUARDRAIL"}
```

That is a fact about the CALLER'S MESSAGE, not a fault in this agent. Reporting
it as a 500 tells the user "the agent broke" when the truth is "your message was
refused, and here is why" — and the two are indistinguishable in the UI, which
is exactly how a working guardrail first looked like an outage.

```ts
// Reads the AI SDK's APICallError body; returns null for anything else.
function guardrailBlock(err: unknown): { name: string; reason: string } | null {
  const body = (err as { responseBody?: string; data?: unknown })?.responseBody;
  if (!body) return null;
  try {
    const m = JSON.parse(body)?.message;
    if (m?.action !== "GUARDRAIL_INTERVENED") return null;
    return { name: m.interveningGuardrail ?? "guardrail", reason: m.actionReason ?? "refused by policy" };
  } catch { return null; }
}

// in the handler's catch:
const g = guardrailBlock(err);
if (g) return sendJson(res, 422, { error: g.reason, guardrail: g.name });
// A provider rejecting a turn that carried files: say which files, in a FIXED
// message. The provider body stays in the log — never forward it (see below).
const status = (err as { statusCode?: number })?.statusCode;
if (attachments.length > 0 && (status === 400 || status === 413 || status === 415)) {
  console.error("model rejected attached files:", attachments.map((a) => `${a.name} (${a.mediaType})`), err);
  return sendJson(res, 422, { error: "the model could not read the attached file(s)", files: attachments.map((a) => a.name) });
}
console.error("chat turn failed:", err);
return sendJson(res, 500, { error: "internal error" });
```

**RELAY THIS ONE SHAPE AND NOTHING ELSE.** Do not "improve" this into forwarding
upstream errors generally. (The file-rejection 422 above relays nothing: its
message is fixed and names only the caller's own files.) An upstream failure body can carry the gateway's
address, the provider handle, the model account, an SDK stack trace — and in some
error shapes the request headers, which is where `MODEL_API_KEY` lives. A
default-relay catch is a credential-disclosure bug, not a better error message.
Everything that is not a recognised `GUARDRAIL_INTERVENED` body stays a generic
500 with the detail in the log.

A blocked turn must also leave NOTHING in the conversation store. That already
holds if the handler flow above is followed — the save is step 6 and the model
call is step 5 — so relay the refusal and return; never save the user's message
on the way out.

**Never await `initStore()` before `listen`.** The DB may not be reachable yet
— `postgres-cnpg` provisions asynchronously, so the first schema init of a
freshly deployed agent routinely fails. An awaited rejection there is an unhandled
promise before the server ever binds its port: the process exits and the pod
crash-loops, and `/healthz` never gets the chance to report it. That is why
`store.ts` above splits init in two: call `initStore()` once at startup,
fire-and-forget, before `listen` — and have step 3 of the handler flow
`await ensureStore()` first, on every request. Until the schema init
succeeds, `ensureStore()` keeps retrying and every turn 500s, which is what
the rest of this section already assumes; `/healthz` reports the not-ready
condition via `isStoreReady()` instead of the pod crash-looping. History is
APPEND-ONLY: no turn rewrites a prior message (a stable prefix is what keeps
the model provider's prompt cache effective).

**Two things this store deliberately does NOT solve.** Both are the platform's
to fix, not this component's, so do not invent a local answer:

- **Two turns in flight on one conversation overwrite each other.** There is no
  version check and no locking: both load, both save, and the second write wins
  silently. The caller is what prevents it — it disables send while a turn is
  pending, so one conversation never has two turns at once.
- **`messages` grows without bound.** Nothing trims or summarises it, so a long
  enough conversation eventually fails every turn or truncates the model's
  context. Retention and summarisation land with the platform store; do not add
  ad-hoc trimming here.

## Model access

The platform injects the org's model connection as environment variables,
governed or not — which one is not the agent's business:

| Variable | What it is |
|---|---|
| `MODEL_ENDPOINT` | the base URL, ending in its version segment (`…/v1`) |
| `MODEL_NAME` | the model id, as the host names it |
| `MODEL_API_KEY` | the credential |
| `MODEL_API_FORMAT` | `anthropic` or `openai-compatible` — which API the host speaks |
| `MODEL_API_AUTH_SCHEME` | `x-api-key` or `bearer`; unset means the SDK's default |
| `MODEL_API_KEY_HEADER` | normally unset — see below |

Read them in config like everything else. **The first four are required:**
`/healthz` lists each one that is unset in `missing`. None has a fallback —
not even a model name: a default id is right for one host and a 404 on every
other, so an agent missing `MODEL_NAME` is misconfigured and says so, rather
than asking some host for a model it may not serve.

The connection's format decides the SDK at runtime, so both provider packages
are always installed (see "Layout") and the client is built by one switch:

```ts
// agent.ts
import { createAnthropic } from "@ai-sdk/anthropic";
import { createOpenAICompatible } from "@ai-sdk/openai-compatible";
import type { LanguageModel } from "ai";

// Filled from config once /healthz's required variables are all set.
export interface ModelSettings {
  format: string;      // MODEL_API_FORMAT
  baseURL: string;     // MODEL_ENDPOINT
  apiKey: string;      // MODEL_API_KEY
  modelName: string;   // MODEL_NAME
  keyHeader?: string;  // MODEL_API_KEY_HEADER — normally unset
  authScheme?: string; // MODEL_API_AUTH_SCHEME
}

export function modelClient(
  { format, baseURL, apiKey, modelName, keyHeader, authScheme }: ModelSettings,
): LanguageModel {
  switch (format) {
    case "anthropic":
      return createAnthropic({
        baseURL,
        ...(keyHeader
          ? { apiKey: "unused", headers: { [keyHeader]: apiKey } } // SDK will not start without an apiKey
          : authScheme === "bearer"
            ? { authToken: apiKey }                                // Authorization: Bearer
            : { apiKey }),                                         // x-api-key
      })(modelName);
    case "openai-compatible":
      return createOpenAICompatible({
        name: "model",
        baseURL,
        includeUsage: true, // a streamed turn reports usage only when asked
        // No apiKey under the override: this SDK sends Authorization only when
        // given one, so the key goes out once, under the named header.
        ...(keyHeader ? { headers: { [keyHeader]: apiKey } } : { apiKey }),
      })(modelName);
    default:
      throw new Error(`unsupported MODEL_API_FORMAT: ${format}`);
  }
}
```

**`openai-compatible` is `@ai-sdk/openai-compatible`, never `@ai-sdk/openai`.**
The OpenAI package defaults to OpenAI's Responses API, which not every host
serves; the compatible package speaks Chat Completions, the API an
OpenAI-compatible host is compatible with. Keep
`includeUsage: true`: without it a streamed reply carries no token counts, and
the trace and the platform's cost view read zero.

**`MODEL_ENDPOINT` is a base the SDK appends to, and it already ends in the
API version segment** (`…/v1`). Never append a path of your own; the SDK asks
for `<base>/messages` or `<base>/chat/completions` itself.

### The model call — `streamText`, consumed to completion

A turn is ONE function, and it streams. The reply is still one JSON body;
streaming is how the turn reaches the model, not how it reaches the caller:

```ts
// agent.ts
import { streamText, stepCountIs, type ModelMessage } from "ai";
import type { TurnHooks } from "./tracing.js";
// SYSTEM_PROMPT and MAX_ITERATIONS from prompt.ts, tools from tools.ts,
// modelSettings() — the ModelSettings above — from config.ts.

export async function runTurn(messages: ModelMessage[], hooks: TurnHooks) {
  let failure: unknown;
  const result = streamText({
    model: modelClient(modelSettings()),
    system: SYSTEM_PROMPT,
    messages,
    tools,
    stopWhen: stepCountIs(MAX_ITERATIONS),
    // A provider error arrives HERE, not as the rejection below.
    onError: ({ error }) => { failure ??= error; },
    // Opens and closes a span per model call and per tool call — "Tracing".
    ...hooks,
  });
  // Awaiting these drives the stream, every tool step included, to its end.
  const [text, steps, toolCalls, usage] = await Promise.all([
    result.text, result.steps, result.toolCalls, result.totalUsage,
  ]).catch((err: unknown) => { throw failure ?? err; });
  if (failure !== undefined) throw failure;
  return { text, steps, toolCalls, usage };
}
```

**`streamText`, never `generateText`.** The two produce the same turn, but
they validate different response shapes, and only the streamed one holds on
every host: an Anthropic-format host other than Anthropic's own (Ollama's
`/v1/messages`, measured) answers a reasoning model with thinking blocks the
non-streaming parser rejects, so `generateText` fails every turn there with a
200 in hand. One code path is what lets the org switch its connection without a
rebuild.

**Keep the `onError` capture and both rethrows.** `streamText` reports a
provider failure to `onError`; its promises reject with a generic "no output"
error instead, and once a step has finished they resolve with the steps so far.
Without the capture, a refused step 2 of a tool loop becomes a half-finished
turn saved as if it succeeded, and the handler's guardrail check never sees the
upstream body it reads. With it, every failure is the original error, thrown.

`usage` is the whole turn's, every step summed — the figure tracing records.

### `MODEL_API_KEY_HEADER` — a temporary override

When the platform sets `MODEL_API_KEY_HEADER`, send the key under THAT header
name and no other — the branch above, on either format.

This is a HACK with an expiry date. An agent whose model traffic is governed
reaches the model through Agent Manager's per-agent proxy, and that proxy
authenticates on `API-Key` — a name it does not yet let anyone configure. Each
SDK has its own fixed header (`x-api-key` for Anthropic's, `Authorization:
Bearer` for the OpenAI-compatible one) and no way to rename it, so a governed
agent's request would arrive unauthenticated. Naming the header in the
environment is what bridges the two. The Anthropic SDK refuses to start without
an `apiKey`, so it is given a placeholder the proxy ignores; the
OpenAI-compatible SDK is given none, so no stray `Authorization` goes out.

**Write the branch, not the workaround alone.** Agent Manager's team has
confirmed the proxy's header will become configurable; when it does the
platform stops setting the variable, and an agent written this way reverts to
the SDK default with no change. An agent that hardcodes `API-Key` breaks on
that day, and one that ignores the variable cannot be governed today.

## Tracing

When the platform sets `AMP_OTEL_ENDPOINT` and `AMP_AGENT_API_KEY`, export
OpenTelemetry spans to it. When it does not — every ungoverned environment —
export nothing and start normally. **Never fail startup over tracing.** An agent
that will not boot because a trace collector is unreachable has traded the whole
service for a graph.

**The dependencies are already in the pinned list under "Layout"** — the four
`@opentelemetry/*` packages below are part of every agent's `package.json`, not
an addition you make here:

```
@opentelemetry/api
@opentelemetry/sdk-trace-node
@opentelemetry/exporter-trace-otlp-http
@opentelemetry/resources
```

### What a turn looks like in Agent Manager

One trace per `/chat` turn, shaped as a tree:

```
invoke_agent <agent>          the turn: user message in, reply out, total tokens
├── chat <model>              model call 1: messages sent, tool call requested
├── execute_tool searchBooks  the tool: arguments in, result out
└── chat <model>              model call 2: the tool result in, the reply out
```

Agent Manager decides what each span IS from `gen_ai.operation.name` —
`invoke_agent`, `chat`, `execute_tool`. A span without it shows as `unknown`,
and the trace view renders it as an opaque bar: that is what a single span
wrapped around the whole turn produced before this section existed. Its viewer
also reads every message and tool payload as a **JSON string**; an object or
array attribute is dropped without a word.

### `src/tracing.ts` — copy it whole

Write it unconditionally. It is listed in the "Layout" tree, and it is inert
when the platform sets no endpoint — the decision to export or not is made at
runtime, below, never by omitting the file.

```ts
// tracing.ts — imported for side effects from the top of main.ts, before
// anything creates a model client. Also exports traceTurn, which main.ts
// wraps every /chat turn in.
import { context, trace, SpanKind, SpanStatusCode, type Span } from "@opentelemetry/api";
import { NodeTracerProvider, BatchSpanProcessor } from "@opentelemetry/sdk-trace-node";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";
import { Resource } from "@opentelemetry/resources";
import type {
  LanguageModelCallEndEvent,
  LanguageModelCallStartEvent,
  LanguageModelUsage,
  ModelMessage,
  ToolExecutionEndEvent,
  ToolExecutionStartEvent,
} from "ai";

const endpoint = process.env.AMP_OTEL_ENDPOINT;
const apiKey = process.env.AMP_AGENT_API_KEY;
const agentName = process.env.OTEL_SERVICE_NAME ?? "agent";

// Prompts, completions, tool arguments and results go on spans unless the
// platform sets this to "false". ON when unset, as Agent Manager's own
// instrumentation defaults.
const recordContent = process.env.TRACELOOP_TRACE_CONTENT !== "false";

const tracer = trace.getTracer("agent");

if (endpoint && apiKey) {
  const provider = new NodeTracerProvider({
    // SET THIS OR THE TRACES ARE ANONYMOUS. A provider built without a
    // resource reports `service.name: unknown_service:node`, and every agent
    // in the org looks identical in the trace view — spans all correct, view
    // useless. The platform supplies the name in OTEL_SERVICE_NAME; a bare
    // NodeTracerProvider does not run resource detection, so read it.
    resource: new Resource({ "service.name": agentName }),
    spanProcessors: [
      new BatchSpanProcessor(
        new OTLPTraceExporter({
          // The exporter appends nothing — AMP_OTEL_ENDPOINT is a base.
          url: `${endpoint}/v1/traces`,
          headers: { "x-amp-api-key": apiKey },
        }),
      ),
    ],
  });
  provider.register();
  // Without this the last spans of a turn die with the pod.
  process.on("SIGTERM", () => {
    void provider.shutdown().finally(() => process.exit(0));
  });
}

// The streamText callbacks that open and close the per-step spans. Declared
// as METHODS, not function-typed properties: streamText types its callbacks by
// the agent's own tool set, and only a method's parameter is checked loosely
// enough to accept that narrower event. As properties, every agent with typed
// tools fails to compile.
export interface TurnHooks {
  onLanguageModelCallStart(e: LanguageModelCallStartEvent): void;
  onLanguageModelCallEnd(e: LanguageModelCallEndEvent): void;
  onToolExecutionStart(e: ToolExecutionStartEvent): void;
  onToolExecutionEnd(e: ToolExecutionEndEvent): void;
}

export interface TurnTrace {
  conversationId: string;
  model: string; // MODEL_NAME
  system: string; // "anthropic" | "openai" — see "Tracing"
  message: string; // this turn's user message
}

// One agent turn: an `invoke_agent` span, with a `chat` child per model call
// and an `execute_tool` child per tool call. Agent Manager classifies each
// span by gen_ai.operation.name; a span without one shows as "unknown".
export async function traceTurn<T extends { text: string; usage: LanguageModelUsage }>(
  turn: TurnTrace,
  run: (hooks: TurnHooks) => Promise<T>,
): Promise<T> {
  const agent = tracer.startSpan(`invoke_agent ${agentName}`, {
    attributes: {
      "gen_ai.operation.name": "invoke_agent",
      "gen_ai.agent.name": agentName,
      "gen_ai.conversation.id": turn.conversationId,
      "gen_ai.system": turn.system,
      "gen_ai.request.model": turn.model,
    },
  });
  const parent = trace.setSpan(context.active(), agent);
  if (recordContent) {
    agent.setAttribute("gen_ai.input.messages", toMessages([{ role: "user", content: turn.message }]));
  }

  // Model calls in a turn run one after another, so one open span is enough;
  // tool calls in a step may run in parallel, so they are keyed by call id.
  let chat: Span | undefined;
  const toolSpans = new Map<string, Span>();

  const hooks: TurnHooks = {
    onLanguageModelCallStart: (e) => {
      // A call that is retried starts again without having ended.
      if (chat) {
        chat.setStatus({ code: SpanStatusCode.ERROR, message: "model call retried" });
        chat.end();
      }
      chat = tracer.startSpan(
        `chat ${e.modelId}`,
        {
          kind: SpanKind.CLIENT,
          attributes: {
            "gen_ai.operation.name": "chat",
            "gen_ai.system": turn.system,
            "gen_ai.request.model": e.modelId,
          },
        },
        parent,
      );
      if (recordContent) {
        chat.setAttribute("gen_ai.input.messages", toMessages(e.messages));
        if (e.instructions !== undefined) {
          chat.setAttribute(
            "gen_ai.system_instructions",
            typeof e.instructions === "string" ? e.instructions : JSON.stringify(e.instructions),
          );
        }
      }
    },
    onLanguageModelCallEnd: (e) => {
      if (!chat) return;
      chat.setAttributes({
        "gen_ai.response.model": e.modelId,
        "gen_ai.response.finish_reasons": [e.finishReason],
        "gen_ai.usage.input_tokens": e.usage.inputTokens ?? 0,
        "gen_ai.usage.output_tokens": e.usage.outputTokens ?? 0,
      });
      if (recordContent) {
        chat.setAttribute(
          "gen_ai.output.messages",
          JSON.stringify([{ role: "assistant", parts: e.content.flatMap(toPart) }]),
        );
      }
      chat.setStatus({ code: SpanStatusCode.OK });
      chat.end();
      chat = undefined;
    },
    onToolExecutionStart: (e) => {
      const span = tracer.startSpan(
        `execute_tool ${e.toolCall.toolName}`,
        {
          attributes: {
            "gen_ai.operation.name": "execute_tool",
            "gen_ai.tool.name": e.toolCall.toolName,
            "gen_ai.tool.call.id": e.toolCall.toolCallId,
          },
        },
        parent,
      );
      if (recordContent) {
        span.setAttribute("gen_ai.tool.call.arguments", JSON.stringify(e.toolCall.input ?? {}));
      }
      toolSpans.set(e.toolCall.toolCallId, span);
    },
    onToolExecutionEnd: (e) => {
      const span = toolSpans.get(e.toolCall.toolCallId);
      if (!span) return;
      toolSpans.delete(e.toolCall.toolCallId);
      if (e.toolOutput.type === "tool-error") {
        span.setAttribute("error.type", "tool_error");
        // An error message can carry the provider's response body: content.
        span.setStatus({
          code: SpanStatusCode.ERROR,
          ...(recordContent ? { message: String(e.toolOutput.error) } : {}),
        });
      } else {
        if (recordContent) {
          span.setAttribute("gen_ai.tool.call.result", JSON.stringify(e.toolOutput.output ?? null));
        }
        span.setStatus({ code: SpanStatusCode.OK });
      }
      span.end();
    },
  };

  try {
    const result = await run(hooks);
    agent.setAttributes({
      "gen_ai.usage.input_tokens": result.usage.inputTokens ?? 0,
      "gen_ai.usage.output_tokens": result.usage.outputTokens ?? 0,
    });
    if (recordContent) {
      agent.setAttribute("gen_ai.output.messages", toMessages([{ role: "assistant", content: result.text }]));
    }
    agent.setStatus({ code: SpanStatusCode.OK });
    return result;
  } catch (err) {
    // The class name is metadata; the message and stack are content.
    agent.setAttribute("error.type", (err as Error)?.name ?? "Error");
    if (recordContent) {
      agent.recordException(err as Error);
      agent.setStatus({ code: SpanStatusCode.ERROR, message: String((err as Error)?.message ?? err) });
    } else {
      agent.setStatus({ code: SpanStatusCode.ERROR });
    }
    throw err;
  } finally {
    // A failed model call never reaches its end callback: close what is open
    // so no span is lost. A span never ended is a span never exported.
    for (const span of [chat, ...toolSpans.values()]) {
      if (!span) continue;
      span.setStatus({ code: SpanStatusCode.ERROR, message: "turn ended before this step completed" });
      span.end();
    }
    agent.end();
  }
}

// OpenTelemetry GenAI message shape, as a JSON string — Agent Manager reads
// these attributes as strings and drops anything else.
type Part =
  | { type: "text"; content: string }
  | { type: "tool_call"; id: string; name: string; arguments: unknown }
  | { type: "tool_call_response"; id: string; response: unknown };

function toMessages(messages: ReadonlyArray<ModelMessage>): string {
  return JSON.stringify(
    messages.map((m) => ({
      role: m.role,
      parts: typeof m.content === "string"
        ? [{ type: "text", content: m.content }]
        : m.content.flatMap(toPart),
    })),
  );
}

// One message part. Reasoning, files and sources are not message content.
function toPart(part: { type: string }): Part[] {
  const p = part as { type: string } & Record<string, unknown>;
  switch (p.type) {
    case "text":
      return [{ type: "text", content: String(p.text) }];
    case "tool-call":
      return [{ type: "tool_call", id: String(p.toolCallId), name: String(p.toolName), arguments: p.input }];
    case "tool-result":
      // The SDK wraps a result as { type: "json" | "text", value }; the value
      // is the result.
      return [{ type: "tool_call_response", id: String(p.toolCallId), response: (p.output as { value?: unknown })?.value ?? p.output }];
    default:
      return [];
  }
}
```

The handler wraps each turn in it — step 5 of the handler flow — and `runTurn`
spreads the hooks into `streamText` (see "The model call"). `genAiSystem` is the
OpenTelemetry name of the API the client speaks, not of the host behind it:

```ts
const genAiSystem = config.modelApiFormat === "openai-compatible" ? "openai" : "anthropic";
```

**The hooks are the AI SDK's own lifecycle callbacks, not a wrapper around
`execute`.** `onLanguageModelCallStart/End` fire once per model call and
`onToolExecutionStart/End` once per tool call, with the messages, the model's
output content, the tool input and the tool result in hand. Wrapping each
generated tool's `execute` instead would see the tool but never the model call,
and a turn would still show one opaque span per step.

**Every span is closed on every path.** A model call that fails never reaches
its end callback; the `finally` closes whatever is still open as `ERROR`, so a
turn that dies in step 2 still shows step 1, the tool it ran, and where it
stopped. A span never ended is a span never exported.

### Content: on unless the platform says otherwise

`TRACELOOP_TRACE_CONTENT` decides whether spans carry message content — the
user's message, the system prompt, every model input and output, and tool
arguments and results. The platform sets it (`true` by default, as Agent
Manager's own instrumentation defaults); unset is treated as `true`, and only
the exact value `false` turns content off. With it off, the tree, the timings,
the token counts and the outcome are all still there — only the text is gone.

Content is the agent's most sensitive traffic. **Never add a content attribute
outside a `recordContent` check**, and never log it instead: the platform's
switch is only a switch if every path honours it. Error messages count: a
provider's error can echo the request or the data behind it, so with content
off a failed span carries its `error.type` and an `ERROR` status, and no
message or recorded exception.

### Mistakes that look like a broken collector

**Reading the variables into config is not instrumenting.** An agent that
loads `AMP_OTEL_ENDPOINT` and never constructs an exporter emits nothing, and
nothing about it looks broken — the pod is healthy, the turns succeed, and the
trace store is simply empty. If you add the config entries, add the provider and
the spans in the same change.

**Instrument by hand; no auto-instrumentation covers this stack.** OpenLLMetry
(`@traceloop/node-server-sdk`) does not instrument the Vercel AI SDK: installing
it produces a tracer that emits nothing for `streamText`. Do not add it, and do
not add any `@opentelemetry/*` package beyond the four above.

## Constraints

**The allow-list is the security boundary.** An operation the design did not
allow is simply not generated, so it cannot be called however the user phrases
the request. Never generate a tool the document does not list, and never add one
because it "would be useful".

**Authorization belongs to the provider, never to this component.** Forward the
caller's credential on every tool call, held out-of-band (`AsyncLocalStorage`) so
the model never sees or selects it. Instructions like *"only edit items they
added"* are UX — they shape a good answer. The provider returning `403` is what
makes it true. Never implement an ownership or permission check here.

**Reject callers the gateway did not vouch for.** Two different things pass
through this component and collapsing them is the usual mistake:

- **Inbound — who is calling me.** When the component's `design.json` carries
  the project's `thunder-app` dependency, the API Platform Gateway has already
  validated the caller's token and hands you the result as headers. Read
  `X-User-Id`; answer **401** when it is missing, exactly as `api-management`
  requires of a service. Never parse the JWT yourself — the gateway is the only
  route to this pod, and re-verifying a token the gateway already verified is
  duplicated trust with a second thing to get wrong.
- **Outbound — whose authority I act under.** Forward the original
  `Authorization` header downstream unchanged, per the paragraph above. The
  headers are for your own door; the bearer is what the provider needs.

Both, together, in the request handler — the header check is a gate, not a
value the tools consume:

```ts
// `node:http`, not Express: set the code, then end. And a header Node saw
// TWICE arrives as string[] — accepting it would key rows by a joined
// "victim, attacker" value, so a non-string is refused rather than coerced.
const userId = req.headers["x-user-id"];
if (typeof userId !== "string" || userId === "") {
  res.statusCode = 401;
  res.end();
  return;
}
await callContext.run({ authorization: req.headers.authorization }, () => reply(body));
```

The gate matters even when every API behind the agent authorises its own
callers. They protect the *data*; nothing else protects the *spend*. An
unauthenticated agent endpoint is a bill anyone who finds it can run up on the
organisation's model key.

**A conversation's stored history is not a source of authorization.** The
`message` a caller sends is untrusted input, even once it is persisted into
the store — a caller cannot manufacture a fake assistant turn or tool result
by typing one, only the model produces those, but nothing in the transcript
should ever be read as granting authority. Authority comes from the
credential on the request, never from the transcript.

**History must carry tool calls and their results.** Save the whole turn, not
just the reply, and load it back next request. An agent given only its own
prose has lost everything its tools told it, and will re-look-up or invent
identifiers it already had. The save in step 6 of the handler flow above is
where this lives — `steps.flatMap`, NOT the result's `response.messages`, which is
the LAST step only and silently drops every tool call and result.

**Return tool errors to the model; do not throw.** A `409 cutoff has passed` is
something the agent should explain, not a failed turn.

**Stateless process, stateful store.** A deployed agent holds no conversation
of its own — every turn loads from and saves to Postgres, so replicas and
restarts of the process itself are safe. `postgres-cnpg` is PVC-backed, so a
database pod restart does not lose conversations either. The in-memory backing
is the deliberate exception, and only where there is no database to reach:
never reach for process memory as a cache, a fallback, or a place to keep
anything the store does not already hold.

**Bound the loop** with `stopWhen: stepCountIs(max_iterations)`, as `runTurn` does. An unbounded
agent spends money until something else stops it.

**Start even when unconfigured.** The component contract requires a component to
start with no required environment variables, and an agent cannot serve a turn
without a model credential. Reconcile the two by starting anyway, logging what is
missing, and reporting it from `/healthz` — never by crash-looping, and never by
discovering it inside a user's first message.

## Layout

Nothing from `specs/` ships in the component. The image contains compiled code.

```
<app-path>/
├── package.json          # the pinned set below — nothing else
├── tsconfig.json         # nodenext, strict, rootDir "src", outDir "dist"
├── Dockerfile
└── src/
    ├── prompt.ts         # GENERATED from the AFM body
    ├── tools.ts          # GENERATED from the dependency's openapi.yaml
    ├── config.ts         # env, read once
    ├── tracing.ts        # OpenTelemetry setup + traceTurn — see "Tracing"
    ├── agent.ts          # the AI SDK loop
    └── main.ts           # HTTP surface + per-request credential context
```

Mark both generated files as generated, and name the contract they came from —
a reader must know to change the design rather than the code.

**Dependencies are pinned. Use these majors exactly, and add nothing beyond
this list:**

```json
"dependencies": {
  "ai": "^7.0.2",
  "@ai-sdk/anthropic": "^4.0.0",
  "@ai-sdk/openai-compatible": "^3.0.55",
  "zod": "^4.3.6",
  "pg": "^8.13.0",
  "@opentelemetry/api": "^1.9.0",
  "@opentelemetry/sdk-trace-node": "^1.28.0",
  "@opentelemetry/exporter-trace-otlp-http": "^0.55.0",
  "@opentelemetry/resources": "^1.28.0"
},
"devDependencies": {
  "@types/node": "^25.5.0",
  "typescript": "^6.0.2",
  "@types/pg": "^8.11.0"
}
```

The four `@opentelemetry/*` entries are required, not optional — `tracing.ts`
imports them unconditionally and the build fails without them. They are inert
at runtime when the platform sets no `AMP_OTEL_ENDPOINT`; see "Tracing".

**Do not "check the latest" and choose for yourself.** The AI SDK's major
versions are not compatible, and a run that resolves its own version lands one
behind and writes code against the wrong API. `pg` is pinned for the same
reason. These majors are what the platform runs (`components/dataplane/ae-system-project/ae-studio/ae-design-agent`).

Both provider packages are required, not a choice: **the model provider is
chosen at runtime from `MODEL_API_FORMAT`**, never at build time — not from the
document's `model.provider`, and never inferred from `model.url` or
`model.name`. The org can switch its connection without a rebuild, and an agent
compiled against one SDK would stop answering the day it did. See "Model
access".

```dockerfile
FROM node:22-slim AS builder
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:22-slim
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev
COPY --from=builder /src/dist ./dist
EXPOSE 9090
CMD ["node", "dist/main.js"]
```

Per-request credential, reachable from a tool without the model seeing it:

```ts
export const callContext = new AsyncLocalStorage<{ authorization?: string }>();

// request handler:
await callContext.run({ authorization: req.headers.authorization }, () => reply(body));

// inside call():
const { authorization } = callContext.getStore() ?? {};
```

## Evaluate before you open the PR

> **PAUSED on this deployment.** Agent evaluation is OFF unless the environment
> sets `AEP_AGENT_EVAL=on`. When it is unset or anything else: SKIP this whole
> section — do not build the harness, do not run the scenarios — and say in the
> PR description that evaluation was skipped because it is disabled here. The
> scenario file is still authored at design time, so nothing else changes and
> turning this back on is a one-word change to this line.

Once `npm run build` exits 0, and before the PR, run the agent's scenarios
against the agent you just built. `specs/validation/agent-scenarios.json` was
written at design time from the requirements — it is the behavioural oracle
for this component, and `references/designing.md` describes its shape.

The harness is `@aep/agent-eval`. It is private and never published, so it is
never invoked with `npx` — that would fetch some other package of that name
from the registry, at an unpinned version. It reaches a build two ways, and
`command -v agent-eval` tells you which one you are in:

- **A build pod.** The runner image ships the harness at
  `$AEP_AGENT_EVAL_HOME` (`/opt/aep/agent-eval`) with `agent-eval` on `PATH`.
  Nothing has to be built.
- **The platform monorepo** — a playground or local run. The harness is the
  workspace package `packages/agent-eval`, and it has to be compiled once.

```bash
# from the project root — the folder holding specs/.
if command -v agent-eval > /dev/null 2>&1; then
  run_eval() { agent-eval "$@"; }
else
  # `git rev-parse --show-toplevel` finds the PLATFORM monorepo here and only
  # here. In a build pod the generated project is its own git repository, so
  # the same expression would answer with the project — which is exactly why
  # the pod is served by the branch above and never by this one.
  corepack pnpm --filter @aep/agent-eval build
  run_eval() { node "$(git rev-parse --show-toplevel)/packages/agent-eval/dist/bin/agent-eval.js" "$@"; }
fi
run_eval \
  --scenarios specs/validation/agent-scenarios.json \
  --app <app-path> \
  --afm specs/design/components/<agent>/agent.afm.md \
  --out tests/agent-eval
```

All four flags are required:

| Flag | What it points at |
|---|---|
| `--scenarios` | the scenario file the design wrote |
| `--app` | the component's App Path. The harness runs `dist/main.js` there, which is why it comes after the build |
| `--afm` | the agent document. Its front matter is the ONLY source of the provider contracts to stub and of the allow-list they are served under; its body is never read |
| `--out` | where `report.md` lands — `tests/agent-eval`, beside the validation phase's artifacts |

It boots the agent as a child process on an ephemeral port (`PORT`), with no
`MEMORY_DB_*`, so it runs on the in-memory store and a second turn still has
to remember. It serves each provider contract from its own examples, and
answers **403** for an operation the allow-list withholds — a call that lands
there is reported as tool over-reach, a security finding rather than a rubric
miss. A simulated user drives the conversation and WITHHOLDS the facts the
scenario says to withhold — that is what makes "asks for what it needs"
observable rather than asserted.

The organisation's model connection is the credential, for the agent under
test and for the judge alike: the harness boots the agent with the same
`MODEL_*` variables a deployment gives it, and grades on the same connection.
The harness finds it itself. In a build pod the platform mounts it as
`AEP_EVAL_MODEL_API_KEY` beside the connection's format, URL, model and auth
scheme; outside one the harness falls back to `ANTHROPIC_API_KEY`, on
Anthropic's API. Never `CLAUDE_CODE_OAUTH_TOKEN`, which is the platform's own
coding budget and authenticates none of the API calls the judge makes. **Pass
no key on the command line and set none yourself** — a key you export is a key
that ends up in a build log. In particular, if the harness reports no key, do
NOT copy the `ANTHROPIC_API_KEY` you can see into it: in a pod that one is the
organisation's CODING credential, which it may bill separately on purpose, and
the platform withheld it from evaluation deliberately. No evaluation is the
correct outcome there, and it is report content like any other.

Without a key the agent is booted with no `MODEL_API_KEY`, so its own
`/healthz` answers 503 and the report says the agent never became ready,
quoting the `missing` list that names it. Nothing about that fails the build:
the run exits 0 and the PR carries the report either way.

### The fix loop

A scenario is satisfied when it earns at least **0.8 of its achievable
`mustCover` weight** and violates **no `mustNot`** — zero tolerance there,
because those lines encode harm (inventing a price, claiming a failed write
succeeded) and a rubric that tolerates one 20% of the time is not a rubric.

**When a scenario falls short, revise the PROMPT and run it again — at most 3
rounds.** Each round: edit `src/prompt.ts`, `npm run build`, re-run the block
above — the whole block, since `run_eval` is defined inside it and a shell
function does not survive to your next command. Cite the rubric line that drove
each change; `report.md` names them, with the judge's own reason. Lines under
"Ungraded" are a grading gap, not an agent failure — never revise against one.

**Stop early if a round scores worse than the one before it**, and keep the
earlier prompt. **The best-scoring prompt ships, not the last one tried** — a
revision can make an agent worse.

**Revise ONLY the markdown body** — `# Role`, `# Instructions`, `# Style`.
NEVER the front matter. `x-aep.tools.openapi[].allow` is the security
boundary: a build that widened it to pass a scenario would be granting the
agent permissions nobody approved. A scenario failing because the agent lacks
an operation is a finding to report, not a thing to fix here.

### What the PR carries

**A low score never fails the build.** Evaluation reports; it does not gate.
Open the PR either way, with `tests/agent-eval/report.md` in it.

If the prompt changed, run the final round with `AGENT_EVAL_PROMPT_CHANGED=1`
so the report says so, say it in the PR body, and open a SECOND PR carrying
the same body to `agent.afm.md`, labelled `agent-spec-updated`. The build's PR
ships the revised prompt — the agent a user first meets is the good one — and
the document catches up under a human's review. Never edit anything under
`specs/` from the build's own PR.
