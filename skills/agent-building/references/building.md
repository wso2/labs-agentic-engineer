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
| `POST /chat` | `{ conversationId?, message }` in; **`{ conversationId, text, toolCalls }` out**. History lives in the agent's conversation store, never on the wire |
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

**Message shapes never cross the wire.** `message` arrives as a plain string;
`ModelMessage[]` lives only between your store and `generateText`. There is
nothing for a caller to normalise and nothing for it to mis-render.

**Then validate, and answer 400.** A `message` that is missing, not a string,
or empty is a bad REQUEST, not a server error — letting it through to
`generateText` throws, which becomes a 500 and reads as the agent being
broken:

```ts
if (typeof body.message !== "string" || body.message.trim() === "") {
  return sendJson(res, 400, { error: "expected { message: string }" });
}
```

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
reporting it missing answers 503 forever. `MODEL_API_KEY` is what `missing`
is for.

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
// 2. parse { conversationId?, message }; message must be a non-empty string → else 400
// 3. await ensureStore() — 500 while the DB isn't ready yet; then:
//    conversationId present → loadConversation(id, userId); null → 404 { error: "conversation not found" }
//    absent → id = crypto.randomUUID(), history = [] (no row yet — the first save creates it)
// 4. const full = [...history, { role: "user", content: message }]
// 5. const result = await runTurn(full)   // generateText, unchanged
// 6. await saveConversation(id, userId, [...full, ...result.steps.flatMap(s => s.response.messages)])
//    — this INSERT..ON CONFLICT is the only place a row is created, so a turn
//    that throws in step 5 leaves nothing in the store to orphan
// 7. sendJson(res, 200, { conversationId: id, text: result.text, toolCalls: result.toolCalls })
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
turn carries the user's own words and their bearer.

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
console.error("chat turn failed:", err);
return sendJson(res, 500, { error: "internal error" });
```

**RELAY THIS ONE SHAPE AND NOTHING ELSE.** Do not "improve" this into forwarding
upstream errors generally. An upstream failure body can carry the gateway's
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

Three variables carry it, and all three are injected by the platform —
`MODEL_ENDPOINT` (the base URL), `MODEL_NAME`, `MODEL_API_KEY`. Read them in
config like everything else, and build the provider client from them:

```ts
import { createAnthropic } from "@ai-sdk/anthropic";

const model = createAnthropic({
  baseURL: process.env.MODEL_ENDPOINT,
  apiKey:  process.env.MODEL_API_KEY,
})(process.env.MODEL_NAME ?? "claude-sonnet-5");
```

**`MODEL_ENDPOINT` is a base the SDK appends to, and it already ends in the
API version segment** (`…/v1`). Never append a path of your own; the SDK asks
for `<base>/messages` itself.

### `MODEL_API_KEY_HEADER` — a temporary override

When the platform sets `MODEL_API_KEY_HEADER`, send the key under THAT header
name instead of the SDK's own:

```ts
const keyHeader = process.env.MODEL_API_KEY_HEADER;   // normally unset
const model = createAnthropic(
  keyHeader
    ? { baseURL, apiKey: "unused", headers: { [keyHeader]: apiKey } }
    : { baseURL, apiKey },                            // the SDK's own default
)(modelName);
```

This is a HACK with an expiry date. An agent whose model traffic is governed
reaches the model through Agent Manager's per-agent proxy, and that proxy
authenticates on `API-Key` — a name it does not yet let anyone configure. The
Anthropic SDK hardcodes `x-api-key` and offers no way to rename it, so a
governed agent's request arrives unauthenticated. Naming the header in the
environment is what bridges the two.

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

Write `src/tracing.ts` unconditionally. It is listed in the "Layout" tree, and
it is inert when the platform sets no endpoint — the decision to export or not
is made at runtime, below, never by omitting the file.

```ts
// tracing.ts — imported for side effects from the top of main.ts, before
// anything creates a model client.
import { trace } from "@opentelemetry/api";
import { NodeTracerProvider, BatchSpanProcessor } from "@opentelemetry/sdk-trace-node";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";
import { Resource } from "@opentelemetry/resources";

const endpoint = process.env.AMP_OTEL_ENDPOINT;
const apiKey = process.env.AMP_AGENT_API_KEY;

export const tracer = trace.getTracer("agent");

if (endpoint && apiKey) {
  const provider = new NodeTracerProvider({
    // SET THIS OR THE TRACES ARE ANONYMOUS. A provider built without a
    // resource reports `service.name: unknown_service:node`, and every agent
    // in the org looks identical in the trace view — spans all correct, view
    // useless. The platform supplies the name in OTEL_SERVICE_NAME; a bare
    // NodeTracerProvider does not run resource detection, so read it.
    resource: new Resource({
      "service.name": process.env.OTEL_SERVICE_NAME ?? "agent",
    }),
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
```

Wrap every model call:

```ts
import { tracer } from "./tracing.js";

const model = process.env.MODEL_NAME ?? "claude-sonnet-5";

const result = await tracer.startActiveSpan(`chat ${model}`, async (span) => {
  try {
    const r = await generateText({ model: modelClient, prompt });
    span.setAttributes({
      "gen_ai.system": "anthropic",
      "gen_ai.request.model": model,
      "gen_ai.usage.input_tokens": r.usage?.inputTokens ?? 0,
      "gen_ai.usage.output_tokens": r.usage?.outputTokens ?? 0,
    });
    return r;
  } catch (err) {
    span.recordException(err as Error);
    span.setStatus({ code: 2 }); // ERROR
    throw err;
  } finally {
    span.end(); // a span never ended is a span never exported
  }
});
```

**Reading the variables into config is not instrumenting.** An agent that
loads `AMP_OTEL_ENDPOINT` and never constructs an exporter emits nothing, and
nothing about it looks broken — the pod is healthy, the turns succeed, and the
trace store is simply empty. If you add the config entries, add the provider and
the spans in the same change.

**Instrument the model call by hand.** OpenLLMetry (`@traceloop/node-server-sdk`)
does not auto-instrument the Vercel AI SDK: installing it produces a tracer that
emits nothing for `generateText`, which reads as a broken collector rather than
as a missing instrumentation. Wrap each model call in a span yourself, following
the OpenTelemetry `gen_ai.*` semantic conventions:

| attribute | value |
|---|---|
| `gen_ai.system` | `anthropic` |
| `gen_ai.request.model` | `MODEL_NAME` |
| `gen_ai.usage.input_tokens` | from the SDK result's `usage` |
| `gen_ai.usage.output_tokens` | from the SDK result's `usage` |

Name the span `chat <model>`, and record tool calls as child spans so a turn
reads as one tree.

**Respect `TRACELOOP_TRACE_CONTENT`.** When it is `false` — which is the
platform default — never put prompts, completions, or tool arguments on a span.
Model, token counts, latency and outcome are what the platform observes; message
content is the agent's most sensitive traffic and exporting it is a decision with
a privacy review behind it, not a default. Treat an unset value as `false`.

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
where this lives — `steps.flatMap`, NOT `result.response.messages`, which is
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

**Bound the loop** with `stopWhen: stepCountIs(max_iterations)`. An unbounded
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
    ├── tracing.ts        # OpenTelemetry setup — see "Tracing"
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
reason. These majors are what the platform runs (`services/agents`).

**The model provider comes from the document's `model.provider`.** Map it to its
package: `anthropic` → `@ai-sdk/anthropic` (`createAnthropic`), `openai` →
`@ai-sdk/openai` (`createOpenAI`). **When the document does not name one, use
Anthropic** — the platform default. Never infer a provider from `model.url` or
`model.name`; a wrong guess builds against a different SDK entirely and only
fails when a real key is supplied.

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
>
> Why it is off: the harness boots the agent with only `MODEL_API_KEY`
> forwarded, so an agent whose model access also needs `MODEL_ENDPOINT` and
> `MODEL_NAME` — which is every agent once its model access is governed through
> the AI gateway — fails at boot and scores 0/6 for a reason that has nothing to
> do with its behaviour. Re-enable once the harness forwards the whole
> `MODEL_*` set.

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

The organisation's Anthropic key is the credential, for the agent under test
and for the judge alike. The harness finds it itself, under either of the two
names it can arrive as: `AEP_EVAL_ANTHROPIC_API_KEY` in a build pod, where the
platform mounts the org's default key, and `ANTHROPIC_API_KEY` outside one.
Never `CLAUDE_CODE_OAUTH_TOKEN`, which is the platform's own coding budget and
authenticates none of the API calls the judge makes. **Pass no key on the
command line and set none yourself** — a key you export is a key that ends up
in a build log. In particular, if the harness reports no key, do NOT copy the
`ANTHROPIC_API_KEY` you can see into it: in a pod that one is the
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
