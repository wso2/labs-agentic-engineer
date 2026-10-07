# @aep/codegen-evals

Measures the **coding run** end to end, the way a user would meet its output:
a saved case (`specs/` + `issues/`, nothing generated) → `play code` builds the
whole app from scratch → `play wire` runs it on this machine → a walker agent
clicks through the running app against a frozen checklist and records evidence
→ a judge scores what a user would see. Everything an attempt produced is
archived, so a later root-cause pass can read the code, the coding transcripts,
the walk and the wire logs side by side.

Not a gate. Nothing here runs in CI, and a bad score is the output, not a
failure: the CLI exits nonzero only when it could not do what was asked.

## Saving a case

```bash
pnpm play <project dir> eval-save <name>      # from a playground project
pnpm --filter @aep/codegen-evals eval save --from <abs dir> --name <name> [--force]
```

`save` finds the project's **pre-code** state — the project itself when its top
level is exactly `specs/` + `issues/` and no issue carries an agent-written
section (`## Progress`, `## Mock verification`), otherwise the oldest undo
snapshot that is — copies it to `cases/<name>/` without the compiled renderings
(`*.excalidraw`, `*.gen.json`, which code generation never reads), and asks a planner (read-only
tools) to derive `checklist.yaml` from the specs: one item per user-visible
behaviour in the wireframe flows and PRD stories, each walkable from an empty
database, under a role `wire --role` accepts. A project with no
`web-application` component is refused: `wire` is the only target.

**Review `checklist.yaml` before the first sweep.** It is hand-editable and
frozen — every attempt walks the same list, and nothing re-derives it. Its
header lists the planner's *gaps*: preconditions the UI cannot create from an
empty database (a manager/report link nobody can set, say). Those are spec
facts, not app defects; add items for them by hand only if you know the
precondition holds. `extras.mustCover` items are walked and scored like
`items`; an `extras.mustNot` that the judge finds violated caps the band at
`review`.

Cases are committed. Planner transcripts land in `.runs/planner/`.

## Running

```bash
make eval-codegen ARGS="list"
make eval-codegen ARGS="run --case expense-claims"                  # first config, once
make eval-codegen ARGS="run --case expense-claims --repeats 3"      # before believing a delta
make eval-codegen ARGS="run --config sonnet --config <other id>"    # a matrix
make eval-codegen ARGS="report"                                     # re-render the newest sweep
make eval-codegen ARGS="rewalk --attempt .runs/<sweep>/<case>/<config>/attempt-1"   # no coding run
make eval-codegen ARGS="replan --case expense-claims"               # re-derive a checklist
```

**Tuning the checklist or the walker** does not need a coding run: `rewalk`
copies an archived attempt's `project/` to a fresh staging dir and runs wire →
walk → judge against the CURRENT checklist, into `attempt-<n>/rewalk-<k>/`
(~5 minutes). `--from-sweep <id> [--case <name>]` rewalks every attempt of a
sweep. Rewalks are listed in that sweep's `report.md` under their own heading
and never enter its statistics. `replan` re-runs the planner over a saved
case's committed specs (same snapshot, hand-added `extras` kept).

`run` flags: `--case` and `--config` (both repeatable; defaults: every case,
the first entry of `configs.yaml`), `--repeats`, `--concurrency` (default 1 —
an attempt is a container, a compose project and a browser), `--keep` (keep
the staged project under `~/.aep-evals/codegen/`), `--list`. Ctrl-C stops every
live attempt through its normal teardown and still writes the report.

An attempt is 30-90 minutes: the coding run dominates, then a cold compose
build, then a walk of up to 30 minutes. Docker (Colima) must be up and the
runner image built.

`configs.yaml` is the matrix: `{id, runtime, model}` becomes `AEP_AGENT_RUNTIME`
and `AEP_AGENT_MODEL` for the coding run. An `opencode` entry also needs a
`connection` (literal format and base URL, plus the NAME of the env var holding
its key — read from `deployments/.env`, then the shell).

### Every knob is in `src/config.ts`

Paths, phase timeouts, the harness's own models, the walker's command guard,
the bands. Flag → env var → default:

| env var | what it sets |
|---|---|
| `CODEGEN_EVAL_REPEATS`, `CODEGEN_EVAL_CONCURRENCY` | sweep shape |
| `CODEGEN_EVAL_CODING_TIMEOUT_MINUTES` (90), `CODEGEN_EVAL_WIRE_TIMEOUT_MINUTES` (20), `CODEGEN_EVAL_WALK_TIMEOUT_MINUTES` (30) | phase ceilings; a case's `timeoutMinutes` overrides the coding one |
| `CODEGEN_EVAL_PLANNER_MODEL`, `CODEGEN_EVAL_WALKER_MODEL`, `CODEGEN_EVAL_JUDGE_MODEL` | the harness's agents (default `claude-sonnet-5-5`) |
| `CODEGEN_EVAL_WALK_MAX_TURNS` | the walker's turn budget |
| `CODEGEN_EVAL_STAGE_ROOT` | where attempts are staged (must stay under `$HOME`: Colima shares nothing else) |

## Reading results

Each sweep writes `.runs/<sweepId>/`:

- `report.md` — one row per case × config: n, median / min / max / spread of the
  score, hard fails, harness errors, median coding minutes and cost, and the
  delta against a baseline (the most recent earlier sweep with the same row and
  no harness errors). Then one line per attempt: status, score, top failing
  items, archive path.
- `summary.json`, `attempts.json`, `facts.json` — the same, as data.
- `<case>/<config>/attempt-<n>/` — the archive:
  `project/` (the generated tree, minus `node_modules`, build output, wire
  secrets and undo snapshots), `coding/` (`play.log` and the whole run dir: `progress.ndjson`,
  `.logs/runtime.log`, `agent-sessions/`), `wire/` (`wire.log`, `plan.json`,
  `compose.yaml`, `logs/`), `walk/` (`transcript.jsonl`, `result.json`,
  `shots/`), `judge/verdict.json`, `metrics.json`, `attempt.json`.
  `wire/logs/services.log` is every compose service's own stdout
  (`docker compose logs --timestamps`), taken before teardown removes the
  containers. `provenance.json` is what the attempt ran against: HEAD sha and
  branch, uncommitted paths under `skills/`, `runners/remote-worker/`,
  `playground/` and `evals/codegen/`, the runner image's ID, the coding
  runtime and model, and the planner/walker/judge models — paths and ids,
  never env values. `skills.diff` is `git diff HEAD -- skills/` plus every
  untracked file under `skills/` as a new-file hunk (empty when clean).
  `checklist.yaml` is the case's checklist as it stood when the attempt
  started — the case's own file changes on every hand edit or `replan`.
  `log --attempt <dir> --usage` gives tokens and tool calls per agent (the
  lead and each fan-out subagent), which the progress feed does not carry.
  A rewalk (`attempt-<n>/rewalk-<k>/`) holds the same minus `project/` and
  `coding/`.

Read an archived coding run with the playground's own developer view:
`make eval-codegen ARGS="log --attempt <attempt dir> [--slow|--thinking]"`.

**Statuses.** `scored` — the app was walked and judged. `hard-fail` — the code
failed: the coding agent did not succeed or built nothing, or `wire` says the
app would not come up; scored 0 and included in the median. `harness-error` —
anything that is not the code (docker, the runner, the model provider, a held
port, an unresponsive browser, a refused credential, a walker or judge with no
answer, an interrupt): counted, excluded from every statistic.

**Every failure names its cause.** `attempt.json` carries `failure: {phase,
cause, reason}` and `report.md` prints it on the attempt's line: cause `app`
is a hard fail, `environment` a harness error. `wire` classifies its own
failures (`FAILED <cause> <reason>`, see `playground/src/engine/wire/failure.ts`),
the coding phase is classified in `src/classify.ts`, and a walk stops as
`browser unresponsive` after three `agent-browser` commands in a row time out.

**Never one number.** A delta inside the wider of the two spreads prints
`inconclusive`, and a row with n=1 on either side always does. Score bands:
pass ≥ 75, review 50–75, fail < 50.

**Symptoms, not causes.** The judge says what a user would see. Why it happened
is for whoever opens the archive.
