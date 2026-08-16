# @aep/spec-agent-evals

Evaluation framework for the spec-flow agents: each SDLC section —
**requirements**, **design**, **task generation** — evaluable separately and in
combination (a **chain**), against the *real* agents service booted in-process
through the playground's production-parity waist. Decision record: the
[wayfinder map #351](https://github.com/wso2/labs-agentic-engineer/issues/351)
and its closed tickets (#352–#357).

## How a run works

1. **Scenario** (`scenarios/<section>/*.yaml`) — data, not code. The `brief` is
   the simulated user's world (idea, persona, facts, fallback, optional
   `traits`); the `rubric` is the evaluation side (`mustCover` with optional
   weights + declared `mustNot`). The sim never sees the rubric. Design/tasks
   scenarios also name a `fixture` — a captured-then-curated `specs/` state
   under `scenarios/fixtures/`.
2. **Driver** (`src/drivers/`) — conversational sections run the `/start` or
   design instruction through `runSpecTurn`, with the **sim user**
   (`src/sim-user.ts`) answering `ask_question` pauses via the production
   answer serializers: adjacent-only volunteering (tagged), per-answer
   `source: fact | persona-fallback | improvised`, and a fatigue curve that
   degrades answers as questions accumulate (reset per section). The loop
   answers however many rounds the agent asks — a one-form `/start` pass and
   a multi-round grilling session (#486) both fit; the turn cap bounds
   runaway sessions, and the fatigue curve prices over-asking. Task
   generation is the detached one-shot `task-plan` toolset turn, folded to
   `issues/*.md` via the playground's `FsIssueStore`.
3. **Scoring** (`src/scoring/`) — runs inside the task, not in evalite
   scorers, so chains can band a section before deciding to continue:
   - *structural*: deterministic checks binding the LIVE workspace validators
     (`checkComponentDesign`, the DSL compilers, the issue fold). A validator
     tightening is an explainable score shift, not drift.
   - *rubric judge*: one sonnet call at temperature 0, seeing the artifact,
     the rubric, and the sim's decisions digest — user-decided scope is never
     penalized as invention, and `*assumed*`-tagged recommendations (the
     finish valve's visible-by-design output) are judged on substance, never
     as defects.
   - *bands*: combined score → **pass ≥75 / review 50–75 / fail <50**; a
     `mustNot` violation forces at least review.
4. **Chain** (`evals/chain.eval.ts`) — requirements → design continue ONE
   conversation (the console flow), tasks runs detached; a fail-band section
   halts the chain (downstream marked skipped); the result is a per-section
   **verdict vector**, never a blended number.
5. **Artifacts per run**: evalite traces (serve UI), a readable
   `<run>.transcript.md` + raw `<run>.trace.json` next to the throwaway
   project under `playground/.projects/spec-agent-evals/`, and — for any
   review-or-worse run — a **review sheet** in `eval-reviews/` (the human
   review queue: fill in the sheet, commit it).

## Running

```bash
make eval                                  # everything (real model calls)
make eval EVAL=evals/requirements.eval.ts  # one section
make eval-ui                               # run once + local results UI
EVAL_REPEATS=3 make eval EVAL=...          # score-spread mode (#355)
```

Requires `ANTHROPIC_API_KEY` (env or `deployments/.env`). On-demand only —
never wired into CI. Score history lives in evalite's local sqlite store;
`evalite export` produces a static bundle.

## Authoring scenarios

Add a YAML under `scenarios/<section>/` — no code changes. Capture a fixture
by running the upstream section once, reviewing/hand-tuning the produced
`specs/`, and freezing it under `scenarios/fixtures/<name>/` (refresh
deliberately; it's a conscious event, not silent drift). The models for the
sim and judge are pinned in `src/config.ts` (sonnet, temperature 0) so score
variance attributes to the agent under eval.
