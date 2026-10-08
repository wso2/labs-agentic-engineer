# ADR-0001: What an attempt measures and whose failure it is

**Status:** Accepted (2026-10-05)

## Context

The codegen evals score the coding run end to end, as a user would meet its
output. An attempt can end badly for two different reasons: the generated app
is wrong, or the machine around it (docker, the runner, the model provider, the
browser, the harness) failed. A report that mixes the two says nothing about the
code. A score is also meaningless if attempts start from different inputs, or if
one attempt leaves state behind for the next.

## Decision

**A case is the pre-code state.** A case is a project's `specs/` and `issues/`
and nothing else, taken before its first coding run (`save.ts` falls back to the
oldest undo snapshot without agent-written issue sections). Every attempt
generates the whole app from scratch with `play code`. No attempt starts from
generated code.

**The target is `wire` from an empty database.** The app runs locally under
`play wire`. Only a project with a `web-application` component is a case. The
planner writes checklist items that can be walked from an empty database, and
teardown removes the database volume, so every attempt starts empty.

**Every attempt ends in one of three outcomes:**

| status | cause | meaning | in the statistics |
|---|---|---|---|
| `scored` | none | the app came up, was walked and judged | yes, the judge's score |
| `hard-fail` | `app` | the coding agent did not succeed or built nothing, or `wire` says the app would not come up | yes, as 0 |
| `harness-error` | `environment` | anything that is not the code: docker, the runner, the model provider, a held port, an unresponsive browser, a refused credential, a walker or judge with no answer, an interrupt, a harness crash | counted, never averaged in |

A failure is recorded as `failure: {phase, cause, reason}` in `attempt.json`.

**Each phase owns its classification.** `wire` classifies its own bring-up
failures and prints one line, `FAILED <app|environment> <reason>`
(`playground/src/engine/wire/failure.ts`), which `play.ts` parses. The coding
phase is classified from structured facts in `classify.ts`; the first matching
rule wins. A new failure mode picks a cause on purpose.

**Teardown always runs.** It runs in `finally`, whichever way the attempt ended:
every `play` child gets SIGTERM, then SIGKILL past the grace period, then
`compose down -v` by project name. A live child would hold the next attempt's
ports.

## Consequences

- A hard fail counts against the configuration that produced it. A harness
  error is visible in the report with its reason but does not move the median.
- A defect in the harness or the machine cannot lower a score, but it can hide
  one. A sweep with many harness errors has too little data to compare.
- Classification depends on the `FAILED` line contract with `wire`. A change to
  `wire`'s output must keep that line.
- Specs that need preconditions the UI cannot create from an empty database
  become planner *gaps* in `checklist.yaml`, not checklist items.
