# AGENTS.md — evals/ballerina

Measures the path an agent takes through `bal library`. The loop for tuning the
CLI (`packages/bal-library-tool`) and the skill (`skills/ballerina/`) — they are
tuned together because the agent does not distinguish them. What it measures and
why: [README.md](README.md).

## After a change — what is live, and what is not

A **skill** edit is live on the next run: the working tree is mirrored into each
attempt's scratch package. A **tool** the skill names by command is not — it is
installed, and where differs by mode:

| changed | to make it live |
|---|---|
| `skills/ballerina/**` | nothing |
| `packages/bal-library-tool` — host runs (`play --host`, these evals) | `packages/bal-library-tool/install-local.sh` |
| `packages/bal-library-tool` — docker `pnpm play` | `make bal-library-tool` — the jar is mounted |
| `packages/bal-library-tool` — dispatched/cluster runs | `make build-runner FORCE=1` — the image compiles the tool itself |

Then measure rather than eyeball:

```bash
make eval-bal ARGS="--case <name> --repeats 3"   # iterate
make eval-bal ARGS="--suite full"                # confirm
```

`preflight.ts` **refuses** to start against a missing or stale jar. That is the
point: a sweep on the old tool reports numbers, not an error, and nothing
downstream can tell.

## Reading a result

- **Never one number.** A delta smaller than the run-to-run spread prints
  `inconclusive`, and `--repeats 1` has no spread to read against. Measured: a
  32% drop in lookup tokens once accompanied a *worse* answer.
- **Signature errors are the tool's; other errors are the agent's.** A run has
  had 10 compile errors and zero signature errors — a blended count reads that
  as a 10x regression.
- **Wall clock is not comparable across `--concurrency` values.** Call, token and
  error counts are.

## Conventions

- Every knob is in `src/config.ts` — paths, defaults, model, tool lists, the
  filter regex, the error classifiers, the report columns. Precedence is
  flag → env → default. Add a tunable there, never beside its use.
- Cases are data under `cases/<suite>/`. **A folder IS a suite**; nothing
  enumerates them.
- **Build the import set before writing the case.** A package that resolves
  alone can still be unresolvable beside another: `ballerina/ftp` pins
  `data.csv:0.8.2` and refuses to share a graph with the current `0.10.0`. A
  scratch package importing every coordinate `as _` settles it in a minute,
  where the case would spend a 30-minute attempt failing at resolution and
  reporting it as the agent's score. Run `bal openapi -i` over a new contract
  too — the only errors should be the empty resource stubs.
- **Pin only what the compiler forces.** Three of the first five cases asserted
  an import that turned out optional — `ballerinax/aws.auth` and `ballerina/sql`
  are both inferred in idiomatic code *and* compile when written out. An
  expectation copied from a previous run's output encodes a style, and a case
  that fails on style reports noise as a regression. Cases carry no comments:
  rationale that outlives one run belongs here or in the README.
- The extractors read a transcript, so they score a **playground** run unchanged
  — that is where `test/__fixtures__` came from. Keep them pure: anything
  measured during a run cannot re-score a recorded one.
- **Host mode, `claude login`, with no opt-in** — not "by default", the only
  path. `hostEnv` deletes `ANTHROPIC_API_KEY` and `CLAUDE_CODE_OAUTH_TOKEN` from
  every session, because `make eval-bal` inherits the shell and Claude Code
  ranks a stray exported key above the keychain — which would bill the platform
  while the report claimed a subscription run. Do not add an `--api-key` flag: a
  harness whose credential varies between sweeps cannot compare them.
