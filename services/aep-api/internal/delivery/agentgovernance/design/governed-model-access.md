# Governed model access

An `ai-agent` deployed into an environment that has an **AI gateway binding**
reaches its model through WSO2 Agent Manager instead of calling the org's model
connection directly, on any format the connection speaks. This note is the
shipped shape of that path.

## Two entry points, and why

| Entry point | When | Does |
|---|---|---|
| `EnsureRegistration` | the version's **`provision` gate**, at planning | provider lookup · agent record · model binding |
| `GovernAgent` | **every deploy**, and the converge sweep | the same, plus the key |

Neither writes the provider. They look it up (`FindProvider`) and fail closed
with `ErrProviderMissing` ("save the Default key again in Settings → Models")
when the org has none: the governor holds no key, so it cannot make one.

The gate is the FIRST assertion, never the only one. It exists so that an Agent
Manager which cannot serve this build fails it at planning — before a coding
cycle is spent — and so the milestone carries a ticket naming each agent's proxy,
which is where a human attaches a guardrail. `provisioning/agent_gate.go` holds
the gate; it is the same shape as the roles gate beside it.

**The key is deliberately not in the gate.** The reconcile has a rotate branch,
and rotation is safe only because a rollout is in flight to carry the new value.
At planning there is none: a whole coding cycle stands between the gate and the
next deploy, so a rotation there would cut off the agent currently RUNNING for
all of it, and permanently if the run then failed.

Because the deploy and the converge sweep keep asserting the registration, drift
a human causes in the AMP console — deleting a binding on the same screen they
attach guardrails from — still self-heals within a converge tick. The gate moves
the first attempt earlier; it removes nothing.

## Why the deploy stage, and not a background reconciler

Agent Manager returns a key's value **once**, at creation, and never again. A
reconciler that found a key it could not read would have to rotate it, which
cuts off whatever agent is currently using it. Doing the work inside the deploy
means there is always a deploy in flight to carry a new value, so rotation is
safe exactly when it is needed and at no other time.

`DeploymentService.Deploy` calls `GovernAgent` first and fails the deploy if it
errors. The environment's binding is a PROMISE that this agent's traffic is
governed; producing an ungoverned agent instead would be a silent policy
bypass. No binding — the pre-Agent-Manager case — skips the stage entirely and
the agent deploys on the org's connection directly, as it always did. So does an
org with no model connection: there is no provider to build, and the agent comes
up unconfigured.

## What it makes true, in order

The order is forced by the API: a provider must exist before a binding can name
it, and a binding before a key can be issued against it.

| Step | Object | Identity |
|---|---|---|
| 1 | LLM provider | `aep-<org>-anthropic`, holding the ORG's connection and key |
| 2 | External agent | the AEP component name, `provisioning: external` |
| 3 | Model binding | `aep-<component>`, naming that provider for this environment |
| 4 | Binding key | `aep-<component>-<environment>` |

**The project is not in that list, and that is not an omission.** Agent Manager
projects OpenChoreo's own Projects into its catalogue: an AEP project appears
there the moment the CR exists — same name, same creation timestamp — whether or
not it holds an agent. An earlier version of this stage called a get-or-create
on `/orgs/{org}/projects` and never created anything, because the projection
always won the race. Its one visible effect was that the `displayName` it would
have set never took, so the console still falls back to the deployment-pipeline
name. That fallback is Agent Manager's to fix; a second writer on a record it
owns is not the answer.

Step 1 is described below. It sends no `policies` field, so guardrails an
operator attached survive.

**The binding (step 4) is the whole point.** It is what gives Agent Manager a
per-agent view: the console shows the provider under the agent, a guardrail
attaches to THIS agent's traffic, and usage is attributed to it. AMP answers
with a proxy of its own for each binding — a GENERATED URL, read back and never
constructed.

## The provider, from the connection

One provider per org, built by `ProviderInputFor` from the org's model
connection (`modelconn.Connection`) and its key. The deploy path and the
organization domain's publisher both build it there, so the two can never
describe it differently.

| Field | From the connection |
|---|---|
| Template | the format: `anthropic` → `anthropic`, `openai-compatible` → `openai` |
| Upstream | the base URL's scheme and host, no path |
| Auth | the auth scheme: `x-api-key` → header `x-api-key`, value the key; `bearer` → header `Authorization`, value `Bearer <key>` |

Agent Manager has no generic OpenAI-compatible template, but both `openai` and
`anthropic` take any upstream, so the connection supplies everything a
template's own metadata would default. The Bearer prefix is written by AEP:
Agent Manager's API does not apply a template's `valuePrefix` (its console adds
it in the browser). For Anthropic's own API the provider is `anthropic`,
`https://api.anthropic.com`, `x-api-key`, and the contract tests hold those
bytes.

**The handle stays `aep-<org>-anthropic` on every format.** A new handle is a
new provider, the publisher client has no delete scope to remove the old one,
and every bound agent would rebind. Only the display name
(`AEP <org> model connection`) is format-neutral.

**When it is written.** Only by a Settings save, the one moment the key is in
hand. Updating a provider redeploys every proxy bound to it, so nothing writes
it on a deploy. So:

- A Settings save that changes the key, URL, format or auth scheme publishes
  the connection post-commit (`organization.ModelProviderPublisher`), creating
  the provider when the org has none. A failed push answers `502
  agent_manager_not_updated` with the key still saved; saving it again retries.
  A model-only change writes nothing: the provider does not carry the model.
- The publish covers every environment of the org that has an AI gateway
  binding, whether or not a project deploys there yet, so a key saved at
  onboarding is published before the first project exists. Known gap: an
  environment or binding added after the save has no provider until the
  Default key is saved again; the deploy fails with `ErrProviderMissing`,
  whose message says so.
- A disconnect overwrites the provider's key with a value that authenticates
  nowhere, once, under the last connection's header. The provider itself stays:
  there is no delete scope.

**A connection switch is one PUT** carrying template, upstream, auth and key
together. It reaches the proxies bound to the provider; agents keep their keys
and proxy URLs, unless the base path moved (below).

## The four states of a one-time key

| AEP has it | AMP has it | Action |
|---|---|---|
| yes | yes | reuse — regenerating would invalidate a RUNNING agent's credential on every redeploy (unless the base path moved, below) |
| no | no | issue |
| no | yes | rotate — the only route back to a known state |
| yes | no | issue afresh; ours can never authenticate again |

Issuing and storing happen inside one call. Split across two retryable steps, a
crash between them strands a key neither side can recover.

**A moved base path rotates.** The secret holds the endpoint beside the key
(below), and the endpoint ends in the connection's base path. A switch from
`https://api.anthropic.com/v1` to `…/compatible-mode/v1` leaves the stored URL
asking the new upstream for a path it does not serve. The URL cannot be
rewritten alone: the secret store is write-only and a write replaces the whole
secret, so the key's value would be lost with it. So the endpoint written
beside each key is also recorded, durably and in the clear, in
`ai_agent_model_endpoints` (org, component, environment → endpoint,
`updated_at`; organization owns the table, the governor reaches it through its
`EndpointStore` port). When the recorded endpoint differs from the one composed
now, the "both hold it" row takes the rotate branch and stores the new key and
URL together — on the deploy path only, like every rotation. Durable, because
aep-api runs more than one replica and a deploy may land on one that never
stored the key. The record is written after the secret, never before, so a
failure between the two costs a spare rotation, never a stale URL.

**No record beside a stored key is treated as moved**: the URL stored beside
that key cannot be read, so the agent rotates once, on its next governed
deploy, and the record is written. Switches between hosts on the same base path
(most of them: `/v1`) move nothing.

## What the pod receives

The govern stage writes the key AND the proxy URL into one secret
(`amp-model-<component>-<environment>-secrets`). They are useless apart: the
key authenticates against that proxy alone. `projects.ModelAccessEnvVars` then
composes, with no Agent Manager call of its own:

| Variable | Source |
|---|---|
| `MODEL_ENDPOINT` | secretKeyRef → `url` |
| `MODEL_NAME` | the connection's model |
| `MODEL_API_FORMAT` | the connection's format |
| `MODEL_API_KEY` | secretKeyRef → `api-key` |
| `MODEL_API_KEY_HEADER` | literal `API-Key` — see below |

`MODEL_ENDPOINT` is the environment's **in-cluster** gateway address plus the
agent's own proxy path plus the connection's base path (`/v1`, `/api/v1`,
`/compatible-mode/v1`, or none). Each of the three has failed once:

- The gateway's admin URL is a different address for a different caller — the
  pod cannot reach it.
- The shared provider path is not attributable to one agent, so a guardrail on
  it is not per-agent.
- The gateway appends the request path to the upstream as is, and the upstream
  is the connection's origin. The SDK appends only the operation
  (`/messages`, `/chat/completions`), so without the base path the gateway asks
  for `/messages` and gets 404 — and with the path on the upstream as well, for
  `/v1/v1/…`, also 404. The ungoverned path hands the agent the connection's
  base URL, so both end in the same segment.

Composition reads `MODEL_ENDPOINT` from the secret's `url`, not from
`ai_agent_model_endpoints`: the secret is the one source the pod is composed
from.

Any doubt on the composition side resolves to "not governed", which falls back
to the org's key. That is the safe direction: an ungoverned agent works, while
an agent pointed at a gateway whose key was never stored reaches no model at
all. Fail-CLOSED lives in the govern stage, where a failure can still stop the
deploy.

## `MODEL_API_KEY_HEADER` — a workaround with an expiry date

Agent Manager's per-agent proxy authenticates on `API-Key` only (`X-API-Key`,
`x-api-key` and `Authorization: Bearer` all get 401), and that header name is not
configurable today. The SDKs an AEP agent is built on send `x-api-key`
(Anthropic) or `Authorization: Bearer` (OpenAI-compatible) and offer no way to
rename it, so a governed agent's request would arrive at the proxy
unauthenticated.

Until AMP makes the header configurable — which its team has confirmed it will
— the governed path sets `MODEL_API_KEY_HEADER`, and `skills/agent-building`
generates an agent that sends its key under whatever header that variable
names, falling back to the SDK's own default when it is unset. It covers every
format. A stray `Authorization: Bearer unused` beside `API-Key` (what an OpenAI
SDK sends when handed a placeholder key) was measured on an `openai`-template
provider and answers 200, so the proxy sets the upstream's own Authorization
over it. On an `anthropic`-template provider, whose upstream header is
`x-api-key`, a stray Authorization is not measured; the generated agent omits
it on the governed path.

**When the fix lands:** stop setting the variable. Agents written against the
branch revert with no code change. Then delete `modelAPIKeyHeaderEnvVar` and
`ampModelAPIKeyHeader` in `projects/component_service.go`, the entry that emits
them in `ai_agent_model_access.go`, and the override section in
`skills/agent-building/references/building.md`.

## The console's link to the agent

The Deployments page links a governed agent to its page in Agent Manager:
`{console}/org/{org}/project/{project}/agents/{AgentRecordName(project, component)}`.
The console URL is per ENVIRONMENT, like the admin URL: an optional binding
annotation, `aep.wso2.com/amp-console-url`, written by
`setup-environment-aigateway.sh` (`AMP_CONSOLE_URL`, default
`http://console.amp.localhost:8080`). The name is `AgentRecordName`, injected
into `projects` rather than recomputed, so the link and the registration cannot
disagree — a second copy of the hash would drift into links that open nothing.
An environment without the annotation governs exactly as before; its agents
just have no link.

