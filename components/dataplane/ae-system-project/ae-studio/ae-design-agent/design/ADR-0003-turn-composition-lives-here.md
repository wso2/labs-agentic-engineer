# ADR-0003 — Turn instructions are composed by this service, from facts

## Context

A turn's instruction text used to be assembled by whoever dispatched the turn.
In production that was `aep-api` (Go); in the playground and the evals it was
`playground/src/engine/compose.ts` (TypeScript). Both produced a finished string
and POSTed it as `TurnRequest.instruction`; this service treated it as opaque
and ran it.

Two composers in two languages have to agree, so the shared sentences were
authored once in `packages/contracts/prompts/strings.json` and compiled by
`prompts/gen.mjs` into `strings.gen.ts` and `aep-api/internal/prompts/strings_gen.go`
on every `make gen`. The Go↔TS parity test was retired when that landed: the
generator was supposed to make drift impossible.

It did not hold.

- **Four prompt strings had already escaped the generator.** The D20 failure
  note (`turn_runner.go`), the flow skill pointer (`start_command.go` — a
  byte-for-byte duplicate of the TS `slashSkillInstruction`), the milestone-scope
  block and the plan-context fences (`delivery/task/plan.go`) were hand-written
  Go. The generator covered eight strings while at least four more sat outside
  it, with nothing watching them.
- **The generator did not run.** `turbo.json` declared `inputs` for the `gen`
  task that matched none of the files it read, so editing `strings.json` hashed
  identically and turbo replayed a cache hit. `make gen` printed success in 70ms
  and regenerated nothing, and no CI step compared the outputs.
- **The two composers were not equal anyway.** The eager-skill map lived only in
  `aep-api`; the playground never sent `eagerSkills` at all, so a playground run
  had not reproduced a production turn since that feature landed.

The whole apparatus existed to keep two copies in step, and the copies were out
of step.

## Decision

**The dispatcher sends facts. This service decides wording.**

`TurnRequest.turn` is a `TurnSpec` (`@aep/agent-stream`): a discriminated union
over `chat` · `flow` · `start` · `plan`, carrying only what the caller knows —
the user's text, the flow's skill, the captured project idea, the paths of any
reference documents attached at project create, the milestone scope and
existing-Task renders. Turn-level facts ride beside it: `target`,
`previousTurnFailed`, `headless`. `src/prompts/turn.ts` turns that into
instruction text and is the only place any of that text exists.

Consequences that follow from the split rather than being chosen separately:

- **The tool set and the eager-skill map are derived here**, from `turn.kind`.
  Which guidance a flow needs is a property of the flow, not of the call, so a
  console CTA, a typed command and a playground run reach the same map.
- **The `organization` skill joins the standing system instructions** of both
  the editing agent and the task planner, rather than being an eager skill named
  per flow. Org defaults answer interview questions the agent would otherwise
  put to the user, and pin providers at design time, on every turn regardless of
  flow.
- **`aep-api` holds no prompt text.** `internal/prompts` is deleted along with
  `ideaSteer`, `targetSuffix`, `renderScopeContext`, `renderPlanContext` and the
  divergence note. It still *parses* `/<skill>`, because the token decides
  whether to read the project descriptor and whether the turn gets a BFF-signed
  MCP bearer — but it emits a classification, not a sentence.
- **`packages/contracts/prompts` is gone.** The JSON, the generator and both
  generated files are deleted; what remains is the command grammar, at
  `packages/contracts/commands`.
- **`instruction`, `toolset` and `eagerSkills` are rejected with a 400** naming
  the replacement, as the pre-§12 inline `files`/`skills` shape already is.

### Why here, and not in the BFF

The BFF has the database, git credentials and the project descriptor; this
service has the model. Prompt text is an input to a model, so it belongs beside
the model. The practical test: every fact on a `TurnSpec` is something this
service *cannot* obtain — the descriptor's dot-led path is stripped from the
turn snapshot, and Tasks are platform state that never reaches a repository. If
a field could be read from the workspace, it would not need to be on the wire.

## Alternatives rejected

- **Fix the generator and keep two composers.** Correcting the turbo `inputs`
  and adding a CI drift gate would make the eight covered strings safe, and do
  nothing for the four that were never in the JSON — the failure mode was not
  the pipeline breaking but text growing outside it. (The turbo bug was fixed on
  its own merits; codegen tasks are now uncacheable.)
- **Move the facts too** — unstrip dot-led paths so the agent reads
  `specs/.agentic-engineer.toml` itself, and pull open Tasks over MCP. Rejected:
  it weakens the snapshot fence for a file that is platform-only by design, and
  converts a composed turn into agent round-trips.
- **Have the playground call `aep-api`** so there is only one composer.
  Rejected: running without the platform is the playground's reason to exist.
- **Author the prompts in TOML for readability.** This was the request that
  started the investigation. Rejected because it solves the smaller problem: a
  cross-language source file only needs a human-friendly format because it is
  cross-language. With one consumer, the prompts are ordinary TypeScript
  template strings in the service that sends them.

## Consequences

- Editing a prompt is editing one file, with no regeneration step and no way for
  a second copy to disagree.
- A playground run and a production turn are the same turn by construction — the
  same service composes both — instead of by two composers being checked against
  each other.
- `aep-api` and the agents service must deploy together; the wire break is not
  backward compatible. A turn dispatched during a rolling upgrade fails with a
  clear 400 and is retryable.
- Go has no discriminated unions, so `agentsvc.TurnSpec` is a flat struct with a
  `Kind` tag. The TS side validates the combination (`isTurnSpec`) and rejects a
  mismatch pre-stream, which is where that asymmetry is absorbed.
- The `organization` body is now in every turn's system prompt, so its cost
  scales with how much the org has settled. Prompt caching is not configured
  today, so it is paid per turn until it is. The skill is excluded from the
  catalog for the same reason — listed *and* inlined would charge for its
  description twice and offer a load that returns text already in the window.
- The slash grammar is duplicated in Go and TS. Deliberate: a regex cannot be
  shared across languages without a generator, and the two never see the same
  input — a divergence shows up as the playground routing a line differently
  from production, never as wrong prompt text.
