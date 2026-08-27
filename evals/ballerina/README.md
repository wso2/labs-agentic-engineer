# @aep/ballerina-evals

One prompt → one Ballerina package → one `bal build`, measuring **the path the
agent took through `bal library`**. The loop for tuning the CLI
(`packages/bal-library-tool`) and the skill (`skills/ballerina/`) against
evidence instead of intuition.

Not a gate. Nothing here runs in CI, and a bad score is the output, not a
failure — the CLI exits nonzero only when the sweep could not run at all.

## Running

```bash
pnpm --filter @aep/ballerina-evals eval --list          # what exists
make eval-bal                                           # every case, once, at DEFAULTS.concurrency
make eval-bal ARGS="--suite narrow --repeats 3"         # the tuning loop
make eval-bal ARGS="--case aws-s3-submodule"            # one case while iterating
make eval-bal ARGS="--case xlsx-headers,catalog-redis"  # a handful, in one report
make eval-bal ARGS="--suite full --concurrency 2"       # the reality check
```

Flags: `--suite`, `--case`, `--repeats`, `--concurrency`, `--timeout` (minutes),
`--list`. `--suite` and `--case` each take a comma-separated list, because the
tuning loop runs a handful rather than one or all — separate sweeps would each
carry their own report and their own baseline diff. Every member is still matched
exactly, so a typo in one of them selects nothing rather than something else.

**Do not reinstall the tool while a sweep is running.** Preflight checks the jar
once, at the start; an install mid-sweep leaves attempts on either side of it
measuring different tools under one report.

## Every knob is in `src/config.ts`

One file. Paths, sweep defaults, the session (model, allowed and denied tools),
what counts as a filtered lookup, what counts as a signature error, and which
columns a report prints. Nothing tunable lives anywhere else — a change to the
detour threshold or the model should not require knowing which module counts
detours.

Precedence: **flag → env var → the default in `config.ts`**.

| env var | what it sets |
|---|---|
| `BAL_EVAL_REPEATS` | attempts per case |
| `BAL_EVAL_CONCURRENCY` | attempts in flight |
| `BAL_EVAL_TIMEOUT_MINUTES` | per-attempt ceiling |
| `BAL_EVAL_MODEL` | the model under test |

A malformed value falls back to the default rather than becoming `0` — a sweep
that silently ran once because `BAL_EVAL_REPEATS=three` is worse than one that
ignored the variable.

The one deliberate exception: `report.ts` keeps the *accessors* that read each
column off an attempt, because a closure over a type is code rather than
configuration. The column **names** are in `config.ts`, and a test pins the two
lists together so a column added to one and forgotten in the other fails there
instead of vanishing from every report.

## Cases are folders

`cases/<suite>/<name>.yaml`. **A folder IS a suite** — nothing enumerates them,
so `mkdir cases/connectors` makes `--suite connectors` work. The three that ship
are a cost distinction, not a kind:

| suite | shape | use |
|---|---|---|
| `narrow/` | one file, one package weakness, ~1-2 min | iterate |
| `service/` | an OpenAPI-generated HTTP service over connectors | the common shape |
| `full/` | a whole service across several packages, ~10-15 min | confirm |

### What `narrow/` already reaches for

Each of these exists to exercise a part of the CLI the `service/` cases never
touch, because every one of those is client-driven and names its package in the
prompt.

| case | verb it forces | what it stresses |
|---|---|---|
| `aws-s3-submodule` | `type` across a package edge | a submodule's config type, and the `aws.auth` detour |
| `xlsx-headers` | `find`, then `guide`, then `funcs` | the prompt names NO package, and `ballerina/xlsx` is far newer than any cut-off, so the case fails outright unless `find` is used. It has no client, and `@xlsx:Name` plus the `time:Date` cell binding appear in no signature — they are readme facts. **Measured: `find` 1, `overview` 1, `guide` 5, `funcs` 2** — the only case where the guide is the dominant verb |
| `kafka-listener-service` | attachment, and the commit contract | `service on <listener>`, the one shape nothing else in the suite writes, where an invented method name (`onMessage`, `onRecord`) compiles as an unattached service that consumes nothing — which `builds: true` cannot see. **It does not force `api`:** measured, the first run used `overview`/`type`/`client` and got `onConsumerRecord` from recall. The claim that it would was wrong |

### What `service/` already reaches for

Every case there is HTTP-triggered and generated from an `openapi.yaml`, so what
distinguishes one is the library behind it. Read this before adding the next one
— a case that repeats a row costs a sweep slot and measures nothing new.

| case | beyond `ballerina/http` | what it stresses |
|---|---|---|
| `alerts-twilio` | `twilio` | one connector, an outcome recorded per recipient |
| `catalog-redis` | `redis` | cache-aside in front of an upstream API |
| `claims-fhir` | `health.fhir.r4.international401` | a very large generated type surface, and two shapes to hold apart — the API's and FHIR's |
| `crm-salesforce` | `salesforce` | a query-driven connector, and one call that creates three objects |
| `exports-sftp` | `ftp` | stdlib only: remote files as the store of record, listed and streamed back |
| `helpdesk-gmail-jira` | `googleapis.gmail`, `jira` | two connectors joined on one mail thread |
| `intake-fanout` | `googleapis.sheets`, `github`, `slack` | three-way fan-out with partial success |
| `knowledge-openai-pinecone` | `openai.embeddings`, `openai.chat`, `pinecone.vector` | embed → upsert → query → complete, with a score threshold |
| `orders-mongodb` | `mongodb` | a document store and a filtered listing |
| `partner-gateway-jwt` | `jwt` | an inbound token validated and read, an outbound one fetched and reused |
| `payments-stripe` | `stripe`, `crypto` | a third party's ids as the truth, plus an HMAC-verified webhook |
| `shipments-rabbitmq` | `rabbitmq` | a publish, and an HTTP client that retries and trips a breaker |
| `subscriptions-mysql` | `mysql` | a relational schema raised on startup and a job that sweeps it |
| `telemetry-kafka` | `kafka` | a keyed publish |
| `workorders-cosmos-asb` | `azure_cosmosdb`, `asb` | a partition key threaded through every read, then a queue handoff |

```yaml
prompt: |            # handed to the agent verbatim
  In this Ballerina package, write `s3_client.bal` containing: …
fixtures:            # copied in before the session: <path in package>: <source>
  openapi.yaml: openapi/orders-mongodb.yaml
expect:              # deterministic post-conditions
  builds: true
  imports: [ballerinax/aws.s3]      # also what gets pre-warmed before the sweep
  importsNot: [some/wrong.package]  # only for a package that cannot be right
```

A fixture's source is a **file**, resolved against the case's own directory —
`cases/service/openapi/` holds the contracts the `service/` cases hand over. A
contract is what `bal openapi -i openapi.yaml` reads, so it stays a file an
editor can validate and the tool can be run against by hand; inlined into the
case it becomes a block scalar that has to be re-indented on every edit. A
source that does not exist refuses the case at load, because the alternative is
a session that writes the service from the prompt alone and scores under the
same name.

**Pin only what the compiler forces.** Three of the five seeded cases originally
asserted an import that turned out to be optional — `ballerinax/aws.auth` and
`ballerina/sql` are both inferred in the idiomatic form, and both also compile
when written out (`auth:StaticAuthConfig cfg = {…}` needs the import, an inline
record literal does not). An expectation copied from what a previous run produced
encodes a style, and a case that fails on style reports noise as a regression.
Assert the connector the task requires; leave the rest to `builds`. `importsNot`
is for a package that is *wrong*, not one that is merely absent from one
idiomatic form.

## What it measures

Two axes, because neither means anything alone — a run can reach a green build
by luck, and take a clean path to the wrong answer.

**Path** (`src/metrics/transcript.ts`), from `claude.log`: lookup **tokens** —
the primary axis — then turns, invocations, how many were piped through a filter,
failures, how often `## Next` survived, verb census, and **worst detour**, the
longest run of consecutive zero-yield calls circling one package. That last one
is the `aws.auth` metric.

Tokens lead deliberately. The 2026-08-17 interface change buys bytes with calls —
`googleapis.sheets` goes from one lookup to two while `ballerina/crypto` goes from
64,310 bytes to about 3,500 — so a report led by call count would score that as a
regression on the axis it was not optimising. Calls are a diagnostic and sit below
the outcomes they explain.

**Turns and invocations are different numbers and both are printed.** A chained
`bal library type … ; bal library overview …` is one round trip and two questions:
a turn is what costs latency and context, an invocation is what was asked. One
column labelled "calls" hid which of the two it meant.

**`…piped` and `…piped AND cut` are also different numbers**, for the same reason.
Every document is bounded since 2026-08-17, so `| head -250` over a 42-line map
removes nothing, while a `grep` over the same document drops the trailing
`// Special Agent Note:` off every line it keeps. `truncated` counts only the pipes
that reached their window — content filters always, `head`/`tail` when the body
came back at or over the limit. The split immediately corrected a standing
misreading: the two runs recorded for months as "19 of 19 piped" had cut **9 and
5**. The headline was overstating its own finding by two to four times, which is
the direction that invites a fix to a problem half the size.

**Outcome** (`src/metrics/build.ts`): the harness's own `bal build` after the
session, plus the agent's own build cycles — with errors **split into signature
errors and everything else**. A run that fumbles a tuple destructuring and a run
that invents a field on `s3:ConnectionConfig` both report "compile errors", and
only the second is evidence about the tool. Measured: a real run had 10 errors
and zero signature errors.

Reported as **median and spread** per metric, never a bare number. A delta
smaller than the wider of the two spreads prints as `inconclusive`. That rule
exists because between two real playground runs the library-token total fell 32%
while the answers got worse — one figure per metric is what made that look like
progress. Use `--repeats 3` before believing anything.

## Host mode, your own login

Every session runs on the host against your own `claude login`. This is not a
default — it is the only path, because a harness whose credential can vary
between sweeps cannot compare them.

`ANTHROPIC_API_KEY` and `CLAUDE_CODE_OAUTH_TOKEN` are **stripped** from the
session environment (`hostEnv`). This package loads no `.env` itself, but
`make eval-bal` inherits your shell, and Claude Code ranks an exported key above
the keychain — so a stray one would bill the platform while the report claimed a
subscription run. If either variable is set, the run says it withheld it rather
than removing it silently. There is no `--api-key` opt-in.

## The stale-jar refusal

Host mode resolves `bal library` out of `~/.ballerina`, so **a CLI change is not
live until you install it**:

```bash
(cd packages/bal-library-tool && ./install-local.sh)
```

`src/preflight.ts` refuses to start when the tool is missing, or when the
working-tree jar is newer than the installed one. It refuses rather than warns
because the failure it prevents produces *numbers*, not an error: a sweep
against a stale tool reports the old CLI's behaviour under the new CLI's name,
and nothing downstream can tell. The skill needs no such step — it is read from
the working tree and copied into each scratch package per attempt.

### And the registration can be dropped *during* a sweep

Preflight cannot catch this one, because it happens inside the sessions.
`bal tool pull openapi` — which every `service/` case legitimately runs — rewrites
`~/.ballerina/.config/bal-tools.toml` from `bal`'s own view of installed tools,
and the local `library` entry carries `repository = "local"`, which that view does
not include. The entry is dropped, and from that moment **every lookup in every
concurrent attempt** answers `unknown command 'library'`.

Measured on 2026-08-17: three sessions pulled openapi, `catalog-redis` failed four
lookups, and one session tried to help itself with `bal tool pull library` —
installing the *published* tool over the working-tree build, so the sweep carried
on measuring a different tool under the name of the one under test.

Two defences, because either alone is insufficient:

- **`ensureToolRegistered()` runs before every attempt** and re-installs when the
  command has gone missing, printing `⚠ <case> #n` when it did. Safe mid-sweep: it
  restores the tool the sweep began against rather than changing it.
- **`toolMissing` is a reported metric, and it leads the table.** A nonzero value
  means the attempt is *not evidence*, and the report says so in those words,
  above the numbers. That matters more than the repair — an attempt already
  spoiled cannot be un-spoiled, and the failure mode being defended against is
  averaging it in with attempts that had a tool.

A spoiled sweep is also **never used as a baseline**, and one spoiled case
disqualifies the whole run rather than its own row — the attempts shared one
`bal-tools.toml`, so a neighbour's clean row is not evidence the neighbour was
unaffected. Measured why: the contaminated sweep reported `telemetry-kafka` at 16
lookup tokens and 1 turn, and the next clean sweep diffed 6,500 against that 16 and
printed `+6484`, which reads as a 400× regression when it is the tool coming back.

## Why not the runner

`runners/remote-worker` throws without the `aep` workflow skill and carries
issue discovery, component contracts and workload authoring. That is a procedure
for building a project; this measures how one agent reads one library, and
putting the coding workflow between a skill edit and its number would defeat the
point. What *is* shared is the option shape — `debugQueryOptions` and the log
sinks come from `remote-worker/src/lib/logger.js`, so a transcript recorded here
has the same fields as one from a real build, including the reasoning pair
(ADR-0002 decision 16).

That sharing runs both ways: the extractors score a **playground** run
unchanged, which is where `test/__fixtures__` came from — two real runs of
`staff-report-maintenance621`, trimmed to the messages the metrics read. Those
fixtures caught two errors in the hand analysis they were taken from.

## Artifacts

`.runs/<timestamp>/` (gitignored): `report.md`, `summary.json`, `attempts.json`,
and per attempt the scratch package itself plus `run/.logs/claude.log`. The
package is left buildable — `cd` into it and run `bal build` by hand. Each sweep
diffs against the previous one automatically.

Per attempt, `run/` also keeps the two **sources** every number is derived from:
`transcript.jsonl` (the session, NDJSON) and `build.log` (the verification
build). Keep them, because a count is not a diagnosis: a sweep reporting "2
signature errors" with no record of which two can only be understood by running
the sweep again, and a re-run is a different sample.
