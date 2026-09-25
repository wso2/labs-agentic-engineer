# Governed model access

An `ai-agent` deployed into an environment that has an **AI gateway binding**
reaches its model through WSO2 Agent Manager instead of calling Anthropic
directly. This note is the shipped shape of that path.

## Two entry points, and why

| Entry point | When | Does |
|---|---|---|
| `EnsureRegistration` | the version's **`provision` gate**, at planning | provider · agent record · model binding |
| `GovernAgent` | **every deploy**, and the converge sweep | the same, plus the key |

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
the agent deploys on the org's own key, as it always did.

## What it makes true, in order

The order is forced by the API: a provider must exist before a binding can name
it, and a binding before a key can be issued against it.

| Step | Object | Identity |
|---|---|---|
| 1 | LLM provider | `aep-<org>-anthropic`, holding the ORG's Anthropic key |
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

Step 1 re-asserts the org's key on every deploy rather than returning early on
"it exists": a rotated org key that AMP never learned about fails every
governed agent in the org at Anthropic, and the only symptom is 401s from a
provider nobody touched. It sends no `policies` field, so guardrails an
operator attached survive.

**The binding (step 4) is the whole point.** It is what gives Agent Manager a
per-agent view: the console shows the provider under the agent, a guardrail
attaches to THIS agent's traffic, and usage is attributed to it. AMP answers
with a proxy of its own for each binding — a GENERATED URL, read back and never
constructed.

## The four states of a one-time key

| AEP has it | AMP has it | Action |
|---|---|---|
| yes | yes | reuse — regenerating would invalidate a RUNNING agent's credential on every redeploy |
| no | no | issue |
| no | yes | rotate — the only route back to a known state |
| yes | no | issue afresh; ours can never authenticate again |

Issuing and storing happen inside one call. Split across two retryable steps, a
crash between them strands a key neither side can recover.

## What the pod receives

The govern stage writes the key AND the proxy URL into one secret
(`amp-model-<component>-<environment>-secrets`). They are useless apart: the
key authenticates against that proxy alone. `projects.ModelAccessEnvVars` then
composes, with no Agent Manager call of its own:

| Variable | Source |
|---|---|
| `MODEL_ENDPOINT` | secretKeyRef → `url` |
| `MODEL_NAME` | literal |
| `MODEL_API_KEY` | secretKeyRef → `api-key` |
| `MODEL_API_KEY_HEADER` | literal `API-Key` — see below |

`MODEL_ENDPOINT` is the environment's **in-cluster** gateway address plus the
agent's own proxy path plus `/v1`. Each of the three has failed once:

- The gateway's admin URL is a different address for a different caller — the
  pod cannot reach it.
- The shared provider path is not attributable to one agent, so a guardrail on
  it is not per-agent.
- The Anthropic SDK requests `<base>/messages`, so a base without the version
  segment asks for `/aep-…/messages` and the gateway answers 404.

Any doubt on the composition side resolves to "not governed", which falls back
to the org's key. That is the safe direction: an ungoverned agent works, while
an agent pointed at a gateway whose key was never stored reaches no model at
all. Fail-CLOSED lives in the govern stage, where a failure can still stop the
deploy.

## `MODEL_API_KEY_HEADER` — a workaround with an expiry date

Agent Manager's per-agent proxy authenticates on `API-Key`, and that header
name is not configurable today. The Anthropic SDK an AEP agent is built on
hardcodes `x-api-key` and offers no way to rename it, so a governed agent's
request arrives at the proxy unauthenticated.

Until AMP makes the header configurable — which its team has confirmed it will
— the governed path sets `MODEL_API_KEY_HEADER`, and `skills/agent-building`
generates an agent that sends its key under whatever header that variable
names, falling back to the SDK's own default when it is unset.

**When the fix lands:** stop setting the variable. Agents written against the
branch revert with no code change. Then delete `modelAPIKeyHeaderEnvVar` and
`ampModelAPIKeyHeader` in `projects/component_service.go`, the entry that emits
them in `ai_agent_model_access.go`, and the override section in
`skills/agent-building/references/building.md`.
