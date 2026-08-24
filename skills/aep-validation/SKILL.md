---
name: aep-validation
description: Load when working a VALIDATION task dispatched by WSO2 Labs Agentic Engineer (the prompt says "validation task"; the issue is labelled `aep` + `validation`). The cwd is a clone of the project's repo on its default branch. You validate the deployed system against specs/validation/validation-criteria.json by authoring and running Playwright e2e tests, then open a PR containing the tests plus a validation report. This workflow REPLACES the implementation workflow in the `aep` skill; the auth model, git/gh conventions, and deny-list there still apply. The phase-specific discipline lives in this skill's `references/authoring.md` (explore + write specs) and `references/healing.md` (repair brittle specs); the `playwright-cli` companion skill carries the CLI mechanics.
metadata:
  aep:
    kind: platform
    audience: [coding]
---

# WSO2 Labs Agentic Engineer validation task

You are validating a deployed system against its acceptance criteria.
Deliverable: **one PR** containing committed e2e tests and a validation
report — everything under `tests/`. A failing criterion is report
*content*, not a task failure — you open the PR either way.

The acceptance oracle (`specs/validation/validation-criteria.json`) is
**read-only input**: you read it to know what to test, but you never
modify it or anything else under `specs/`. All artifacts you produce
live under `tests/`.

The `aep` skill's authentication model (preconfigured `git`/`gh`),
comment conventions, and deny-list all still apply. Its
implementation-specific sections (build verification, workload.yaml,
App Path structure) do NOT apply here.

The workflow docs `references/authoring.md` and `references/healing.md`
are referenced below by relative path, and resolve inside this skill's own
directory (templates under `assets/`, the report generator under
`scripts/`). Where that directory IS on disk varies by run, so anything you
must invoke by absolute path is under `$AEP_SKILLS_DIR/aep-validation/` —
the runner sets it.

**Every command below runs from the repo root, and none of them needs a
`cd`.** One shell serves the whole run, so a `cd` you make persists into
every later call and a relative one is right only once. `Read`, `Write`
and `Edit` never move with the shell — their relative paths always
resolve from the repo root.

## Workflow

### 1. Read the issue

The prompt carries the issue URL. Read it with comments:

```bash
gh issue view <url> --comments
```

The body must contain: the **acceptance oracle** section (criteria file
path + per-criterion tables), a **Test layout** section, and **Report**
requirements. If a required section is missing, post an issue comment
naming what's missing and exit with failure.

Deployed endpoint URLs and any test credentials are NOT in the issue —
they are runtime inputs kept out of the public issue. The endpoints are
already on disk for you (step 4); credentials you request on demand.

Post a brief opening comment (`Starting validation: <one-line plan>`).

### 2. Branch

The `aep/m<milestone#>-` prefix is a CONTRACT, not a style: the platform keys
your merged pull request back to this run by the branch name. A branch outside
that shape reads as a stranger's pull request — it is refused auto-merge, and
your tests and report never reach the run. The milestone is the one your
validation issue is filed under:

```bash
MILESTONE=$(gh issue view <N> --repo <owner/repo> --json milestone -q .milestone.number)
git checkout -b "aep/m${MILESTONE}-validation"
```

If `MILESTONE` comes back empty the issue was filed without one — a platform
fault, not something to work around. Say so in an issue comment and stop; a
branch without the prefix would strand everything you are about to do.

### 3. Partition the criteria

Read the criteria file (default
`specs/validation/validation-criteria.json`) — read-only. Coverage is
determined by **committed-spec presence**, not by any flag in the file.
For each criterion:

- `method: e2e`, no spec at `tests/e2e/specs/<AC-ID>.spec.ts` → **author**
  a spec (steps 5–6).
- `method: e2e`, a committed spec already exists at
  `tests/e2e/specs/<AC-ID>.spec.ts` → **regression set**: do not
  re-author it, just run it (heal only per `references/healing.md`).
- `method: manual` → no automation; the report renders these as a human
  checklist.
- `method: scenario` → no automation in this run; the report lists them
  as not validated.

### 4. Read the validation context, then confirm the app is reachable

Deployed endpoint URLs come from the platform, never from the issue. The
platform fetched them **before you started** and left them at
`/tmp/validation-context.json`:

```bash
cat /tmp/validation-context.json
```

The content is `{ "endpoints": [{"component","url"}], "criteriaPath":
"..." }`. It is present and non-empty whenever you are running: the runner
exits before starting you if the platform could not resolve it, so there is
no failure mode here for you to recover from. If the file were somehow
missing, that is a platform fault — say so in one line and stop. Do not
probe, scan, or infer endpoints, and do not call the platform for them
yourself; the URL is not something you can work out from inside the cluster.

- **Endpoints** become `tests/e2e/targets.json` (step 5) and your target
  list. Each one is reachable exactly as written: the runner probed them all
  and exits before starting you if any did not answer, so there is no
  unreachable-endpoint case for you to detect or report. Use the URL as
  given — do not rewrite it to an IP or a cluster-internal name, which would
  route around the gateway and stop testing what a user actually reaches.
  Never start, build, or deploy the app; you validate what is already
  running. If a request fails once you are authoring, that is a finding
  about the app, not a target to go re-derive.
- **Test credentials (on demand):** request them only when a criterion
  needs a login — POST the test-credentials endpoint with an optional
  `role` hint (the role the flow requires). `AEP_TASK_ID` is this run's
  validation cycle id; the bearer rides a file:

  ```bash
  curl -sf -X POST "$AEP_PLATFORM_URL/internal/v1/validation/$AEP_TASK_ID/test-credentials" \
    -H "Authorization: Bearer $(cat "$AEP_BEARER_FILE")" \
    -H "Content-Type: application/json" \
    --data '{"role":"admin"}' > /tmp/creds.json
  ```

  The response is `{ "username", "password", "mock": true|false, "note":
  "..." }`. Export it in-session and never commit it:
  `export AEP_E2E_USERNAME=… AEP_E2E_PASSWORD=…`. When `mock` is true the
  account is a shared stand-in (real user provisioning isn't implemented
  yet) — use it, and state in your PR description and closing comment that
  auth-gated criteria ran against mock credentials, since such a login may
  legitimately fail against a generated app that doesn't recognise it.
  Only if the request itself errors do you let the affected criterion land
  `not_run`, blocker noted.
- **Local dev servers (experimental runs only):** if the fetched
  endpoints are `localhost` dev servers you must start (the local
  harness), this overrides the base "never start servers" rule: start
  each per the repo's README, backgrounded with logs to a file (e.g.
  `nohup go run . > /tmp/api.log 2>&1 &`), then poll until it answers.
  These die with your session; never commit their artifacts or logs.

### 5. Scaffold the test harness (skip pieces that already exist)

The Playwright package lives at repo root `tests/e2e/` — outside every
component's App Path, so committing tests never triggers a component
rebuild. Never touch application source or the root `package.json`.

```
tests/e2e/
  package.json          see below
  package-lock.json     generated by npm install; commit it
  playwright.config.ts  copy assets/playwright.config.template.ts
  targets.json          from the fetched validation-context endpoints (step 4)
  lib/targets.ts        copy assets/targets.template.ts
  specs/                one spec file per criterion
  scripts/generate-report.mjs   copy the plugin's scripts/generate-report.mjs VERBATIM
  .gitignore            node_modules/ test-results/ playwright-report/ heal-log.json
```

- `package.json` — exactly this shape (no `"type": "module"`: the
  templates rely on Playwright's default CommonJS transpilation), with
  `@playwright/test` pinned to the exact value of the
  `$AEP_PLAYWRIGHT_VERSION` env var (the image's baked browsers match
  that version — a floating `^` would download browsers at run time):

  ```json
  {
    "name": "e2e",
    "private": true,
    "scripts": { "test": "playwright test" },
    "devDependencies": { "@playwright/test": "<value of $AEP_PLAYWRIGHT_VERSION>" }
  }
  ```

  The `test` script is what lets every command below run from the repo
  root: `npm --prefix` executes a script with the package as its working
  directory, so Playwright finds this config without you moving your
  shell.
- `targets.json` shape: `{"targets": {"<component>": "<url>", ...},
  "primary": "<the web-facing component>"}`, filled from the step-4
  validation-context `endpoints`. On a re-validation run, refresh it from
  the context file — a committed `targets.json` may name URLs from an
  earlier deployment.
- Install with `npm install --prefix tests/e2e` on first scaffold (commit
  the lockfile), `npm ci --prefix tests/e2e` on later runs.
- `scripts/generate-report.mjs` is platform-owned: the REPORT step
  always executes the plugin's copy directly and refreshes this
  committed copy, which exists only so humans can reproduce the report
  after checkout.

### 6. PLAN, then GENERATE

Before this step, pull in the phase discipline — do not proceed from
memory:

- **Read `references/authoring.md` now and follow it as the binding
  authoring discipline** (plan format, collect-generated-code loop,
  assertion rules, criterion↔spec contract).
- Load `playwright-cli` with the Skill tool — the CLI's own skill
  (commands, refs, eval, storage state; vendored from @playwright/cli).

Then: write the test plan, author one spec per uncovered e2e criterion
(source-informed where unambiguous; playwright-cli exploration when the
live app is the only trustworthy source), and remember the bar: a spec
counts only after passing twice consecutively against the live app.

- Plan artifact: `tests/validation/test-plan.md` — one section per
  criterion (id, must, target, numbered steps, expected assertion).
  Commit it before writing specs. On re-validation, append new sections;
  don't rewrite history.
- Spec naming (the report's join key — get this exactly right):
  - file: `tests/e2e/specs/<AC-ID>.spec.ts` (e.g. `AC-001-a.spec.ts`)
  - title: `test('<AC-ID>: <short form of the must>', ...)`

### 7. RUN

The suite outlives the Bash tool's DEFAULT timeout (120s), so ask for the
time up front — `timeout` is a parameter on the Bash call, max `600000`:

```bash
rm -f tests/e2e/test-results/results.json   # never read a previous run's verdict
npm test --prefix tests/e2e                 # Bash timeout: 600000
```

Never `npx playwright test` from the repo root. It finds the specs and
passes anyway, without loading the config — so no reporter, no
`results.json`, and none of the launch args the endpoints need. Exit 0,
nothing written.

Two things about this step will mislead you if you let them:

- **A timed-out command still reports success.** Past the timeout the
  harness detaches the command and hands back an OK result with no
  output — identical, from where you sit, to a suite that finished. So
  never infer the run completed from the call returning. Confirm
  `tests/e2e/test-results/results.json` exists and is NEWER than the moment you
  started the run; if it is missing or stale, the run was severed and
  its results do not exist.
- **You cannot wait for a detached run.** `sleep` is blocked, and
  `Monitor`/`TaskOutput` are not available to you, so a severed run is
  unrecoverable — there is no way to attach to it or read its output
  later. Getting the timeout right up front is the whole game.

If the suite is too big for one window, **shard it** — never let one
call run past the limit:

```bash
# the `--` is what passes the filter through npm to Playwright
npm test --prefix tests/e2e -- specs/AC-001-a.spec.ts specs/AC-001-b.spec.ts
```

Merge each batch's results yourself and keep the per-criterion verdicts;
sharding changes how the suite is run, never what the report claims. A
batch that severs is a batch you re-run smaller, not one you skip.

The config writes `tests/e2e/test-results/results.json`. The run includes the
regression set — that's free regression coverage, not an accident.

### 8. HEAL (bounded)

For failures, **Read `references/healing.md` now and follow it as the
binding HEAL discipline** before touching any spec. Then: triage each
one against the live app, repair only *brittleness* (locators, waits,
setup), never weaken what a test asserts. Log each heal in
`tests/e2e/heal-log.json`. When the budget is exhausted, finish with
one final full run so `results.json` reflects the authoritative state —
under the same timeout discipline as step 7, sharded if that is what it
takes. A final run that severs leaves you with no authoritative state at
all, which is worse than a slower one that lands.

### 9. REPORT

Generate the report deterministically — never hand-write it. Run the
**platform's** copy of the script (always the current version, regardless
of what an earlier cycle committed into the repo):

```bash
node "$AEP_SKILLS_DIR/aep-validation/scripts/generate-report.mjs" \
  --issue <N> --commit "$(git rev-parse HEAD)"
```

Then refresh the repo's committed copy so a human can reproduce the
report after checkout:

```bash
cp "$AEP_SKILLS_DIR/aep-validation/scripts/generate-report.mjs" \
   tests/e2e/scripts/generate-report.mjs
```

This writes `tests/validation/report.md` + `report.json`. It reads the
oracle but never writes it — coverage is expressed by each criterion's
pass/fail in the report, not by a flag in the criteria file. Exit code 2
means a contract violation: spec titles that don't map to criterion ids
(fix titles, re-run tests from step 7), a spec file missing its
`// spec:` header (add the header, regenerate — no test re-run needed),
or a pre-existing spec modified without a heal-log entry (record the
heal per `references/healing.md`).

**`tests/validation/report.json` is REQUIRED. Commit it with the tests.**

The platform reads it at your pull request's merge commit to decide the
run's verdict. A merged PR without it fails the whole run with
`validation-unreported` — the platform cannot tell "nothing was wrong"
from "nobody looked", so it assumes nothing was learned. This is not
best-effort.

So on exit code 2: fix the violation and regenerate. Never open the PR
without the report, and never hand-write one to get past the error —
a fabricated report is worse than a failed run.

A **failing criterion is different**: that is report *content*, it
belongs in the report, and you still open the PR (step 10).

### 10. PR

```bash
# always the lease: this branch name repeats every cycle, so a re-validation
# diverges from what the last one left on it
git push --force-with-lease -u origin "aep/m${MILESTONE}-validation"

gh pr create \
  --title "Validation: <pass>/<total> e2e criteria passing (issue #<N>)" \
  --body $'Validates #<N>\n\n<summary table: pass/fail/not_run + manual/scenario counts>\n\nReport: tests/validation/report.md'
```

**`Validates #<N>`, never `Closes` / `Fixes` / `Resolves`.** The
platform owns this task's close: it reopens the task when a version is
judged again, and it closes the task even on a run that never merged a
PR at all. A GitHub closing keyword would put two owners on one issue.

The reference still has to be there — the platform only auto-merges a
PR that names an armed issue in the milestone, so a body referencing
nothing sits unmerged until the run's deadline and the version reports
`validation-unreported`.

Open it **ready-for-review even when criteria fail** — the human reads
the report and decides. Post an issue comment with the summary counts
and the PR link; the platform closes the issue itself.

## Do not

- Modify application source, component App Paths, or the root
  `package.json` — tests and report only.
- Write or modify anything under `specs/` — the acceptance oracle is
  read-only input; all validation artifacts live under `tests/`.
- Delete or skip a previously committed spec. If one is obsolete, say
  so in the PR description and report — a human removes it.
- Leave `.only` / `.skip` / `.fixme` in committed specs.
- Hand-edit `report.md` / `report.json` — regenerate via the script.
- Commit playwright-cli session state, server logs, `test-results/`,
  or credentials. Credentials come only from the step-4 test-credentials
  request (exported in-session as `AEP_E2E_USERNAME` / `AEP_E2E_PASSWORD`,
  never written to a file); if the request errors and a criterion needs
  login, mark it blocked in an issue comment and let it land `not_run`.
- Everything in the `aep` skill's deny-list (no default-branch pushes, one
  PR, no merging, no repo-settings changes). Its force-push exception is
  yours: `--force-with-lease` on your own branch, per step 10.
