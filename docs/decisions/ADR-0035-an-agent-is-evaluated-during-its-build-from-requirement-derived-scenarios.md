# ADR-0035 — An agent's behaviour is evaluated during its build, from requirement-derived scenarios

**Status:** Accepted · 2026-08-23 · **shipped dormant** as of 2026-09-23: the
harness and the scenario file exist, and every build skips evaluation because
nothing sets `AEP_AGENT_EVAL=on` (the `agent-building` skill's gate). Turning
it on, and proving a run end to end, is follow-up work.

## Context

A project whose only component is an `ai-agent` could not be validated.
`validation-criteria.json` gives every criterion `method: e2e | manual`, and
the validation phase discharges `e2e` by writing Playwright tests against the
deployed system. An agent has no browser surface — `/chat` is a POST with a JSON
body — so the oracle existed, the phase existed, and neither reached an agent.

The gap is not only mechanical. An agent's central question — *does it behave
well?* — is not pass/fail. "Refuses to invent a price", "asks before writing",
"says plainly when a tool failed" are judgements about a probabilistic system,
and grading them needs a rubric and a judge: a different instrument from an
e2e assertion.

## Decision

1. **Behavioural evaluation leads; contract checks ride along.** What a human
   cannot verify by reading the AFM is whether the agent behaves. Deterministic
   checks (`/chat` answers, a second turn remembers, an unknown conversation
   404s) sit in the same suite and need no judge.

2. **In the build, in a child process, with a real model and stubbed tools.**
   Iteration is only affordable where a retry costs seconds; evaluating the
   deployed agent would make every fix a rebuild and redeploy (20+ minutes).
   Tool providers are stubbed from their committed OpenAPI contracts, which
   keeps the world deterministic and rubric scores comparable between rounds.
   The stated cost: a stub cannot catch "the real API returns a shape the agent
   mishandles"; that is only reachable after deploy. The agent runs as a
   locally spawned child on an ephemeral port rather than in-process — a
   generated agent owns its own process lifecycle, and its HTTP contract is the
   thing under test (`packages/agent-eval/design/booting-the-agent.md`).

3. **The loop may change the prompt and nothing else.** The `agent.afm.md`
   body is the agent's own and where behavioural failures live.
   `x-aep.tools.openapi[].allow` is the security boundary — a loop that widened
   it to pass a scenario would be a machine granting itself permissions — so a
   scenario failing for a missing operation is a finding for a human, never a
   fix.

4. **Scenarios are written by the design turn, from the requirements only.**
   `specs/validation/agent-scenarios.json`: a brief (goal, facts, facts the
   simulated user withholds until asked) and a rubric (weighted `mustCover`,
   `mustNot`), each citing the criteria it exercises. Never from
   `agent.afm.md`: a loop that tunes a prompt until its own test passes is
   meaningful only if the test was written without seeing the prompt. The
   residual weakness is named: the same turn and model write both documents,
   and the separation is prose the model must follow — the exposure
   `validation-criteria` already carries.

5. **Evaluation reports; it does not block.** A binary gate on probabilistic
   behaviour produces flaky builds and gets switched off. Scenarios are scored;
   the fix loop has a hard cap of three rounds; the build completes either way,
   carrying `tests/agent-eval/report.md`; the best-scoring prompt ships, not
   the last one tried.

6. **The harness is promptfoo with a provider we own**, decided by spike: a
   `file://` provider runs the whole multi-turn conversation and returns the
   transcript as `output`, and promptfoo grades it — weighted rubrics,
   thresholds, grader pinning, JSON output — without us owning any of that.
   What we write is the driver: boot the agent, stub its tools, play the
   simulated user. Its optional provider SDKs are not installed in the runner
   image (`--omit=optional`, ~0.3 GB instead of ~2.5 GB). Agent Manager's
   evaluation was considered and is the wrong instrument here: retrospective,
   over traces of a deployed agent, with no file-based config or programmatic
   output — the right tool for post-deploy monitoring, once agents emit traces.

7. **The model key is the org's default Anthropic key**, the same key the
   deployed agent runs with, mounted on the pod as `AEP_EVAL_ANTHROPIC_API_KEY`
   with `AEP_EVAL_KEY_MANAGED=1`. Never the coding credential: that is the
   platform's coding budget, OAuth-shaped, and may be an override the org
   ring-fenced (ADR-0016). The spend — scenarios × turns × up to three rounds,
   plus grading — is a design decision, not a side effect.

## Consequences

- Every `ai-agent` design carries `agent-scenarios.json`; the console hides it
  (a build input, not a document) and shows the criteria it cites instead.
- The harness ships in the runner image and is reachable as `agent-eval` in a
  build pod; the skill runs it only when `AEP_AGENT_EVAL=on`. Off, the build
  PR says evaluation was skipped.
- Risks accepted and watched: cost per iteration; overfitting a suite that
  drifts toward the prompt (decision 4 is a rule, not a mechanism); judge
  variance (the report shows the score, never only a verdict); a worse agent
  that scores better (bounded by the best-scoring-prompt rule); and the window
  between the build PR merging and the spec PR merging, during which the
  deployed prompt is not the documented one.
- Out of scope, deliberately: evaluating the deployed agent; tool allow-list
  changes; agent `skills` (unsupported by the platform today); showing a spec
  deviation in the console; cross-project learning — a failure that recurs
  across projects is a signal a skill is wrong, and a human changes it.
- Known gap at the time of shipping dormant: the design turn does not reliably
  emit the harness's scenario schema (one run wrote its own field names);
  embedding the schema in the `validation-criteria` skill is the fix.
