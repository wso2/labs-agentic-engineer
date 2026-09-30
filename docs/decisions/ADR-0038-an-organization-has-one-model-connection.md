# ADR-0038 — An organization has one model connection

**Status:** Accepted · 2026-09-26
**Amends:** [ADR-0028](ADR-0028-the-coding-agent-runtime-and-model-are-an-organization-setting.md)
(the runtime and model are an org setting; only priced models are offered) ·
[ADR-0036](ADR-0036-the-coding-credential-is-a-subscription.md) (the coding
credential is a subscription). Each carries a 2026-09-26 amendment.
**Related:** [`runners/remote-worker` ADR-0015](../../runners/remote-worker/design/decisions/ADR-0015-opencode-is-the-second-adapter.md)
(OpenCode is the second adapter; amended the same day)

## Context

Every agent the platform runs (requirements, design, task planning, coding)
called Anthropic's API with the org's Anthropic key. The model was a two-value
enum, the probe sent a one-token message to `claude-haiku-4-5`, and host checks
such as `isAnthropicModel` sat in the consumers. Orgs asked to run on other
providers: Ollama Cloud, OpenRouter, Alibaba, or a gateway of their own. Almost
all of them speak one of two wire formats, Anthropic Messages or
OpenAI-compatible chat completions.

## Decision

**An organization has exactly one model connection: an API format, a base URL,
a key and a model. Every agent uses it.**

1. **One connection for every agent.** The spec agents, task planning and the
   coding agent share one format, URL, key and model. The `/config` section
   `llm` is the connection, patched field by field (`{kind?, baseURL?, apiKey?,
   model?}`), so a model change never re-sends the key. `agents` keeps the
   runtime and the Claude subscription. Storage is `org_model_connections`,
   one row per org, absent while the org has none; the key's bytes live in
   `org_secrets` under `model/key` and mirror to SM-API under the entity
   `model-connection`. The card's save takes the per-org advisory lock
   `org_model:<org>`.

2. **No presets, and Anthropic is not special-cased.** A format prefills its
   default URL and model (`modelconn.Formats`, served as `llmFormats`):
   `https://api.anthropic.com/v1` and `claude-sonnet-5` for Anthropic, no URL
   and `glm-5.3` for OpenAI-compatible. The URL and the model stay free text.
   There is no context-window field: the probe resolves it (the provider's
   figure where the host states one, else 128,000, with a 64,000 output
   limit), and on Anthropic's own API both stay unset because the runtimes
   know Claude's.

3. **Capabilities are computed once, in aep-api.** `modelconn.CapabilitiesOf`
   is the one statement of what a connection supports: Claude Code, the Claude
   subscription, prompt cache, native PDFs, image input, generated agents, and
   the web-search strategy. It keys on format and host, never on "is
   Anthropic". Two hosts are named because features are bound to them:
   `api.anthropic.com` with the Anthropic format (the subscription, the
   `web_search` server tool, native PDFs, generated agents) and `ollama.com`
   (its web-search API). The result is sent to the agents service, the runner
   and the console, so Go, TypeScript and the UI cannot disagree.

4. **The probe decides at save, and nothing revalidates.** One prober per
   format (`organization/model_probe.go`) lists the host's models and proves
   the key with one `max_tokens=1` call when the listing is missing or public.
   A 401 on `x-api-key` earns one Bearer retry, and the scheme that worked is
   stored. The probe follows no redirects, because Go forwards `x-api-key`
   across one. On `ollama.com` it also reads `/api/show` for the context window
   and vision. A save is refused unless the probe passes, so a stored
   connection is usable by construction. An unlisted model is a warning, not a
   refusal. `POST /config/llm/test` runs the same probe without saving, 10
   calls per org per minute.

5. **Public https endpoints only.** `platform/netguard` refuses non-https URLs
   and any host that resolves to a loopback, private, link-local, CGNAT, NAT64
   or cluster address, and dials the address it checked. The agents service
   repeats the check on every call (`shared/guarded-fetch.ts`). A host change
   needs a new key in the same save, so a stored key never follows the host.
   Keys under 12 characters are refused, because the runner's log scrubber
   cannot redact them.

6. **Each consumer maps the connection to its own shape.** The spec agents get
   the key in `X-Model-Key` and the connection in the turn body; `createModel`
   has one branch per format. A coding run gets the connection as plain
   `AEP_MODEL_*` env copied at dispatch, the host stamped on the cycle, and
   exactly one mounted credential (ADR-0036 still holds): a key on Anthropic's
   own API keeps `ANTHROPIC_API_KEY`, a key anywhere else is
   `AEP_MODEL_API_KEY`, and a subscription is `CLAUDE_CODE_OAUTH_TOKEN`. Each
   runtime adapter presents it under its own spelling. Claude Code runs on any
   Anthropic-format host; OpenCode runs on either format.

7. **Cost is priced per host and model.** `model_rates` is keyed on
   `(host, model_id)`, and every usage row records the host. A connection with
   no rate row shows tokens and "billed by `<host>`", never a guessed figure.
   The connection's `priced` reads the same lookup as the Usage page.

8. **A provider limit blocks the run.** One rule decides, in the agents service
   and in the runner: a 429 whose `retry-after` passes 5 minutes, or 5 minutes
   of 429s in total, is a provider limit; anything shorter is a wait, retried
   and reported. A spec turn ends with a `provider_limit` frame. A coding run
   settles BLOCKED with reason `model-provider-limit` and the reset time when
   known, spending no re-dispatch budget. The user starts it again. There is no
   org-wide limit state. The status alone cannot tell a rate limit from a
   spent plan, and Ollama Cloud was measured queueing concurrent requests
   rather than refusing them, so a long 429 there is most likely a spent plan.
   Left to the runtimes, a spent plan parked a coding run until the Job's
   two-hour deadline: OpenCode retries a long `retry-after` for as long as it
   says, and Claude Code settles a failure that reads like any other.

9. **History is filtered only across connections.** Each turn's journal entry
   records the connection that wrote it (`format@host`). Turns from the current
   connection replay byte for byte, so the prompt cache holds; turns another
   connection wrote lose their reasoning and provider-executed tool calls
   (`services/agents/src/conversation/history-for.ts`). The model is not part
   of the fingerprint: Anthropic's API accepts one Claude model's signed
   thinking replayed to another (checked `claude-haiku-4-5` ↔
   `claude-sonnet-5`, both ways), so a model change keeps the history as it
   is. On a model that reads no images, stored images are replaced by a text
   naming the file.

10. **One cut-over.** Storage, contract and console changed in one release
    (`migrate/phase19_model_connection.go`), deployed together. Existing orgs
    carried over with no re-save: same host, key bytes, model and vault
    reference. The migration deletes the old rows, so it is one-way: the
    rollback is a database backup taken before the deploy.

## Consequences

- **Features follow the connection.** Web search is per host: Anthropic's server
  tool on its own API, Ollama's search API through `packages/web-search` (a
  platform-executed tool in the spec agents, the `aep-web` MCP server in coding
  runs), and none elsewhere, which the card says. A PDF goes native only to
  Anthropic's own API and as extracted text everywhere else; an image goes
  unless the probe learned the model reads none. A part the model cannot read
  is refused when it is a chat attachment, and left out and named in the prompt
  when it is one of the project's reference documents, so a project whose org
  moves to a text-only model keeps working.
- **Generated ai-agents, Agent Manager and build evaluation stay on
  Anthropic** until their own follow-up. They act only when `GeneratedAgents`
  holds (Anthropic's own API). On any other connection ai-agent components
  start unconfigured, no key is published to Agent Manager (its copy of the
  previous key is cleared on the save that leaves Anthropic's API, by a move
  to another host or a disconnect), and builds run without
  evaluation. Lifting the gate is one line of `CapabilitiesOf` plus that
  follow-up's changes.
- **Coding pods have no egress guard yet.** The runtimes call the URL from
  inside the pod and take no injected guard, so coding runs rely on the
  save-time check. An egress NetworkPolicy for coding pods is its own issue.
- **A revoked key surfaces late.** Nothing revalidates, so it shows as a failed
  turn or run that names the host.
- **A smaller context window rotates the conversation.** Past 80% of a stated
  window, aep-api rotates the spec conversation; connections on Anthropic's own
  API state none and skip it.
- **Provider terms are the org's responsibility.** The card states where prompts
  go; no host is special-cased or vetted by the platform.
- **Skills migrated as they are.** They were tuned on Claude; quality on other
  models is measured, not gated.

## Alternatives rejected

- **A per-provider catalog, one form per provider.** Every new provider would be
  a contract change and a console form, and most providers differ only in URL.
  Two formats cover them; capabilities cover the differences that matter.
- **Presets** (named providers that fill the URL and model). A preset is a
  catalog under another name and goes stale as providers move endpoints. The
  format's default URL is the one prefill, and it stays editable.
- **A context-window field.** Few users know the figure, a wrong one breaks
  compaction silently in both runtimes, and the probe can read it or fall back
  to a safe default.
- **An expand/contract rollout** (old and new `/config` shapes served side by
  side). It doubles the contract, the card rule and the storage for a window
  nobody needs: aep-api, the console, the agents service and the runner images
  ship together from one chart release, and a backup is a simpler rollback.
- **An operator allowlist of hosts.** It makes the platform vet providers and
  every new one an operator request. Public https hosts only, with the SSRF
  guard, is the rule; an allowlist can be revisited if a customer asks.

## Amendment 2026-09-26 — generated ai-agents run on the connection

The interim gate is lifted: `CapabilitiesOf` reports `GeneratedAgents` on every
format, and the console card no longer names it.

- **ai-agent components** take the connection's model and format
  (`MODEL_NAME`, `MODEL_API_FORMAT`) on both paths. Direct, they also take its
  base URL and auth scheme (`MODEL_ENDPOINT`, `MODEL_API_AUTH_SCHEME`, `x-api-key`
  or `bearer`). Governed, they keep the per-agent proxy URL and
  `MODEL_API_KEY_HEADER`, and Agent Manager's provider takes its template,
  upstream and auth from the connection.
- **Build evaluation** mounts the connection key as `AEP_EVAL_MODEL_API_KEY`,
  with `AEP_EVAL_MODEL_FORMAT`, `AEP_EVAL_MODEL_BASE_URL`, `AEP_EVAL_MODEL_NAME`
  and `AEP_EVAL_MODEL_AUTH_SCHEME` beside it: all five or none.

## Amendment 2026-09-29 — the SRE agent may use its own connection

The OpenChoreo SRE (RCA) agent is one Deployment per plane, not dispatched
per-org, so "the" connection above does not quite fit it: an org's default
connection may be Anthropic-format, which the stock agent (OpenAI-compatible
only) cannot call. An org may now save a second, optional connection just for
it — the **SRE model connection** (`/config` section `sreLlm`,
`org_sre_model_connections`) — OpenAI-compatible and Bearer only, under the
same probe-before-save and host-change-needs-a-key rules as the org's main
connection.

Resolving what the agent runs on checks, in order: the SRE model connection,
if saved; else the org's own model connection, if it carries the `SREAgent`
capability (any `openai-compatible` connection); else unconfigured, and the
agent is scaled to zero. This is not a second peer connection in the sense
§1 rejects — every other agent still reads exactly one connection — it is a
narrow, single-purpose override for the one workload this platform runs
outside the per-org dispatch model. See
[`services/aep-api/design/sre-model-connection.md`](../../services/aep-api/design/sre-model-connection.md)
for delivery (aep-api pushes the resolved connection into the agent's
Secret; the agent never reads AE's database).
