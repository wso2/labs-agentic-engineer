# Governed observability

How an AEP-built agent's traces reach Agent Manager. Companion to
`governed-model-access.md`, which covers the credential the same agent reaches
its model with. The two look alike and behave differently, and most of what
follows is that difference.

## The contract

An externally-hosted agent exports OTLP over HTTP to its environment's AI
gateway:

```
POST ${AMP_OTEL_ENDPOINT}/v1/traces
Header: x-amp-api-key: ${AMP_AGENT_API_KEY}
```

`AMP_OTEL_ENDPOINT` is **not the model gateway**. An environment runs two
gateways in the same namespace:

| gateway | serves |
|---|---|
| `ai-gateway-<org>-<env>-gw-gateway-runtime:8084` | the per-agent LLM proxies |
| `api-platform-<org>-<env>-gw-gateway-gateway-runtime:22893` | AMP's `/otel` route |

Trace ingest belongs to the second — the `…-otel-restapi` that
setup-environment-gateway.sh waits on is what serves it. Composing the OTLP
address from the AI gateway binding's endpoint yields a URL that answers **404**,
and an agent reports that as nothing at all. That shipped once and was caught
only by deploying a real agent and calling the endpoint from inside its pod.

So the address is RECORDED, not derived: setup-environment-gateway.sh writes
`aep.wso2.com/otel-endpoint` onto the Environment, beside the AI gateway and
Thunder bindings, and composition uses it verbatim. The route is part of the
recorded value, so aep-api never has to know AMP spells it `/otel`, and a chart
rename (the Service name really does contain `gateway-gateway`) changes one
script rather than a constant in Go.

The annotation is optional: an environment provisioned before it existed governs
model traffic correctly and runs untraced, which is the safe direction.

`/otel` on its own answers 404 and `/otel/v1/traces` without a key answers 401,
so both halves are load-bearing and a misconfiguration is legible rather than
silent.

The third variable, `TRACELOOP_TRACE_CONTENT`, is set to `false`: spans carry
model, token counts, latency and outcome, never prompts or completions.
Exporting message content is a decision with a privacy review behind it, not a
default inherited from an SDK.

## Minting: two endpoints, one of which is a trap

`POST …/agents/{agent}/tracing-token/regenerate` answers `200` with
`{environmentName, expiresAt, rotatedAt}` — expiry metadata and **no token**. A
mint pointed there succeeds, stores nothing, and leaves the agent exporting
spans it cannot authenticate.

`POST …/agents/{agent}/token?environment={env}` is the one that discloses the
value. `environment` is a query parameter and is required; omitted, amp-api
answers `500 "Failed to generate token"` — a server error for an incomplete
request, so nothing about the response suggests the caller left something out.

The scope is `amp:agent:token-manage`, which is a **third** key family: neither
`amp:llm-provider:api-key-manage` (the provider's key) nor
`amp:agent:api-key-manage` (the agent's model key) authorises it, and the 403
body says only "insufficient permissions".

## Why this reconciles differently from the model key

The tracing token is a **signed JWT, not a stored credential**. Agent Manager
signs it on demand and keeps no record of it, so minting a second one neither
revokes the first nor accumulates anything to clean up. What it cannot do is
outlive its own `exp`, around ninety days.

That inverts every rule the model key follows. A model key may be issued once
and must never be regenerated under a running agent; a tracing token may be
re-minted freely and must be replaced before it lapses. So:

* the model key reconciles on **presence** — does Agent Manager list a key of
  this name, and does AEP hold one;
* the tracing token reconciles on **expiry** — is the one this process minted
  inside the refresh window.

Expiry is held in memory on the `Governor`, alongside the provider-credential
fingerprint and for the same reason: the secret store is **write-only** (the
OpenBao provider implements no value read), so nothing can ask what token an
agent holds or when it lapses. The mint is the only moment the expiry is
knowable. A restart costs one extra mint per agent, which for a stateless JWT
costs nothing at all.

## Why the token gets its own SecretReference

Minting is allowed to fail. An agent with no tracing token runs correctly and is
merely unobserved, so a failure is logged loudly and the deploy continues —
unlike model access, where a missing key means the agent cannot answer at all.

That is only true if the deployment can avoid **referencing** a token that was
never written. OpenChoreo's `secretKeyRef` has no `optional` flag: naming a
SecretReference that does not exist does not degrade to "no traces", it stops the
container from starting. Sharing the model key's secret would have turned every
tracing failure into a crash-looping agent — strictly worse than the failure it
was meant to tolerate.

One secret per credential turns the question composition must answer into one it
can: *does this SecretReference exist?* All three tracing variables are composed
together or not at all.

The endpoint is composed as a **literal**, not held in the secret: it is not
secret, and the binding composition already reads carries it.

## What is not solved

**The pod-restart gap.** `secretKeyRef` injects at container start, so writing a
token into the secret does nothing to a pod already running — it takes effect on
the next restart. Agent Manager's own console hits this and says so ("Apply the
configuration to restart the agent so the new key takes effect"). A fresh deploy
is correct; an agent already running when a token is first minted stays
unobserved until it next restarts.

**Prose is not an implementation.** The first agent built against this contract
read all three variables into its config and never constructed an exporter — no
`@opentelemetry/*` dependency, no spans, a healthy pod and an empty trace store.
The skill had shown the exporter but left the provider registration as a
comment; an ellipsis where the hard part goes is an instruction to skip it. The
sample is now complete, runnable code, and says outright that reading the
variables without emitting spans is the failure it looks like.

**Node/TypeScript has no AMP instrumentation package.** AMP ships
`amp-instrumentation` for Python and a Ballerina module, and neither covers the
stack AEP's agents are built on. OpenLLMetry does not auto-instrument the Vercel
AI SDK — installing it yields a tracer that emits nothing, which reads as a
broken collector rather than as missing instrumentation. Agents therefore emit
`gen_ai.*` spans by hand; `skills/agent-building/references/building.md` carries
the contract.

**The collector is parked by default.** `setup.sh` scales the observability
plane's heavy workloads to zero, so a correctly configured agent authenticates
and then gets `503` from the gateway's upstream. `park-observability.sh up`
brings it back.
