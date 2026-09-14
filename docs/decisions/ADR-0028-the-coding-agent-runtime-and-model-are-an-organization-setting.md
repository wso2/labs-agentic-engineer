# ADR-0028 — The coding agent's runtime and model are an organization setting

**Status:** accepted · 2026-09-07
**Related:** ADR-0016 (the coding-agent key is an override, not a peer) ·
`runners/remote-worker/design/decisions/ADR-0012` (the runtime is a port) ·
ADR-0027 (run recordings are observability, not ledger)

## Context

Which coding-agent runtime a build runs on, and which model it bills, were both
facts of the platform: `model: "claude-sonnet-5"` was a literal in the runner,
and there was no notion of a runtime at all. The motivation for changing that is
other model providers — an organization that wants its builds on something other
than Claude Code, or on a cheaper model for routine work, has no way to say so.

Three things are already per-org here, which is what makes a fourth natural: the
Anthropic key (`llm`), the coding agent's optional override key (`codingLlm`,
ADR-0016), and the git provider. All three live on one `/config` document.

## Decision

**Runtime and model are one `codingAgent` section on the existing org-config
document, and the dispatcher copies them onto each cycle's Workload.**

- Contract: `AgentRuntime` (`claude-code` | `opencode`), `CodingAgentModel`, and
  `CodingAgentProjection` in `packages/contracts/api/v1/openapi.yaml`;
  `ConfigPatch.codingAgent` and `ConfigProjection.codingAgent`.
- Storage: `org_coding_agent_settings`, one row per org. **Its absence is the
  platform defaults** — the same shape ADR-0016 chose, and for the same reason: a
  "using the defaults" flag can disagree with the values beside it.
- Dispatch: `AEP_AGENT_RUNTIME` and `AEP_AGENT_MODEL` on the coding Job's env,
  beside the credential ref that ADR-0016 already puts there.
- Defaults: `claude-code` and `claude-sonnet-5` — today's behaviour exactly, so
  an organization that never opens the page sees no change at all.

**The credential is NOT part of this section.** It is already an org
coding-agent setting (`codingLlm`), and a second place to set it would be a
second answer to one question. The console groups the three into one card; the
API keeps them as the two sections they are.

### The setting is COPIED onto a run, not referenced by it

A change applies from the **next cycle**. A run in flight keeps the runtime and
model it was launched with, because a feed is read back long after the setting
may have moved and the two runtimes emit different agent ids, model names and
tool names. Re-reading mid-run would produce usage lines whose model names
disagree with the tokens they were billed for. This is the same reason
`RunEvent.runtime` is recorded on the event rather than looked up from the run.

### Only runtimes the platform can RUN are selectable

`opencode` is in the contract's enum because the design carries it and a client
should be able to render the choice. It is refused — with a reason naming what is
missing — at two layers: the API when an organization chooses it, and the runner
if a value somehow reaches a pod. **Never substituted.** Silently running the one
runtime we do have would bill an organization for a runtime it did not choose and
never tell it, and the org would go on believing it had switched.

### Only models the platform can PRICE are offered

`modelcost.SumCost` is all-or-nothing across a cycle's usage capture: a single
model with no `model_rates` row blanks the cost of the **whole cycle**, not just
its own share. So the `CodingAgentModel` enum is exactly the set with rate rows
(`claude-sonnet-5`, `claude-haiku-4-5`), and adding a model is a rate row and a
contract change in one commit, never one without the other.

This does not close the hazard, it only stops the SETTING from opening it: a
lead may still fan work out to a subagent on a model with no rate row, and the
tool glossary names one (`opus`). That is a pre-existing gap this decision
deliberately does not widen.

## Consequences

- `services/aep-api/internal/organization` gains a service, an entity and a
  repository; the `/config` orchestrator gains a section in every phase.
- The section is the first on that document that is not credential-shaped: no
  secret, no external probe, a projection that echoes what was written, and
  individually optional fields — an org tunes its model far more often than it
  moves runtime, and restating the runtime on every model change would let a
  stale read overwrite it.
- `codingAgent` is **never null on the wire**. Every org has an effective runtime
  and model, so the section carries the defaults until somebody chooses;
  `updatedBy` distinguishes "on the defaults" from "chose the defaults", and a
  reset DELETES the row so that distinction survives.
- Authorization is unchanged: any authenticated member of the org may change it,
  compensated by the section-level `orgconfig.patched` audit line — which now
  names `codingAgent` — and by `updated_by` on the row itself.
