---
name: aep-validation
description: Load when working a VALIDATION task dispatched by WSO2 Labs Agentic Engineer (the prompt says "validation task"; the issue is labelled `aep` + `validation`). The cwd is a clone of the project's repo on its default branch. You validate the deployed system against specs/validation/validation-criteria.json by authoring and running Playwright e2e tests, then open a PR containing the tests plus a validation report. This workflow REPLACES the implementation workflow in the `aep` skill; the auth model, git/gh conventions, and deny-list there still apply. The phase-specific discipline lives in this skill's `references/authoring.md` (explore + write specs) and `references/healing.md` (repair brittle specs); the `playwright-cli` companion skill carries the CLI mechanics.
metadata:
  aep:
    kind: platform
    audience: [coding]
---

# WSO2 Labs Agentic Engineer validation task

You are validating a deployed system against its validation criteria.
Deliverable: **one PR** containing committed e2e tests and a validation
report — everything under `tests/`. A failing criterion is report
*content*, not a task failure — you open the PR either way.

The validation criteria (`specs/validation/validation-criteria.json`) are
**read-only input**: you read them to know what to test, but you never
modify them or anything else under `specs/`. All artifacts you produce
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

## The status line

Keep your validation issue's status line current, per the `aep` skill — the
issue in your prompt, and no other.

Say what the criterion rows cannot. The console already draws a row per
criterion and repaints it as you work, with the counts beside it; a line
repeating those tells the reader what they are already looking at. Yours
carries what nothing else can see — where you are in this workflow, and what
you decided.

Steps 1, 2, 4 and 10 ask for comments; those are this line's ends, and step
10's summary is the one that carries counts. A line that sends you back a step
is the ordinary path here, not a fault to explain.

## Workflow

### 1. Read the issue

The prompt carries the issue URL. Read it with comments:

```bash
gh issue view <url> --comments
```

The body must contain: the **Validation criteria** section (criteria file
path + per-criterion tables), a **Test layout** section, and **Report**
requirements. If a required section is missing, post an issue comment
naming what's missing and exit with failure.

Deployed endpoint URLs are NOT in this issue — they are runtime inputs the
platform resolved for you and left on disk (step 4). Test-user logins are
not here either: they are published in this milestone's roles gate ticket,
which is a different issue (step 4).

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
- **Test users and their passwords** come from the **roles gate ticket**,
  not from the context file and not from a spec file. A project whose design
  declares roles gets one issue per version titled *Provision roles and test
  users*, and the platform posts a comment on it carrying every test account's
  login. Find that ticket in your own milestone:

  ```bash
  # newest gate in THIS milestone — only its table is current, and a rebuild can
  # leave an older ticket beside it
  GATE=$(gh issue list --repo <owner/repo> --label "aep:gate/roles" \
    --milestone "$MILESTONE" --state all --json number,createdAt \
    --jq 'sort_by(.createdAt) | .[-1].number // empty')
  [ -n "$GATE" ] && gh issue view "$GATE" --repo <owner/repo> --comments
  ```

  It is normally CLOSED — the platform resolves this gate itself — so
  `--state all` is required, and the logins are a comment, so `--comments` is
  too. `$MILESTONE` is the one you read in step 2; if it is empty here, read it
  again rather than running the query without it — unfiltered, it returns
  another version's ticket.

  The logins are the markdown table under the `<!-- aep:test-users -->`
  marker — the LAST such comment if the ticket carries more than one, since an
  earlier one is a superseded build's. One row per account:

  | Username | Password | Role | Cold start |
  |---|---|---|---|
  | `test-trainer` | `tdyjkfmq5t` | Trainer | no |
  | `test-team-member` | `n3pe5cw8s4` | Team Member | yes |

  Read the table, never the prose around it — a human may rewrite that at any
  time, and the marker is what the platform guarantees.

  **Which row.** Match the criterion's role to the `Role` column and use that
  row. For a criterion that needs *a* signed-in user but names no role, use
  the row with **Cold start: yes** — that is the role a person holds before
  anyone grants them one. Do not reuse one role's login to exercise another
  role's screens; that is the difference between judging a permission and
  judging a page.

  Export the pair in-session, per role, as you need it:

  ```bash
  export AEP_E2E_USERNAME='test-trainer' AEP_E2E_PASSWORD='…'
  ```

  **Never write a password into anything you commit or post** — not the
  specs, not `targets.json`, not the report, not the PR body, not an issue
  comment. Read it from the ticket into the environment and leave it there.
  Playwright specs take it from `process.env`, never as a literal.

  These accounts are the platform's own, created for this purpose. They hold
  only the project's application roles, so a criterion judged with one is
  judged against a real sign-in.

  **When a login is missing.** Never improvise one, and never fall back to a
  guess like `admin`/`admin` — a verdict from a login the app does not
  recognise is worth less than no verdict. Land the affected criteria
  `not_run` and say WHICH of these you hit, in the report and in your closing
  comment; they mean different things and only some are a problem:

  | What you see | What it means | Report it as |
  |---|---|---|
  | No gate ticket in the milestone | The design declares no roles — this system has no sign-in | Expected; no finding |
  | A ticket, but no login table | Every role the design declares is one the platform does not own, so it could provision no usable account | A provisioning problem — say so |
  | The ticket is OPEN and carries a failure comment | Provisioning failed; quote the cause | A provisioning problem — say so |
  | A table, but no row for the role you need | That account was refused or could not be enrolled — the ticket's other comment says which | A provisioning problem — name the role |
  | A row whose password says *unavailable* | The platform holds the account but could not publish its password | A platform problem — name the account |
  | No row has **Cold start: yes** | The design says a caller with no role reaches nothing. Use the least-privileged role the criterion implies; if it implies none, the criterion is unreachable by design | Expected; explain the reasoning |
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
  the lockfile), `npm ci --prefix tests/e2e` on later runs. That is the only
  install this run needs: the browsers are already in the image, so
  `playwright install` is never one of your steps.
- `scripts/generate-report.mjs` is platform-owned: the REPORT step
  always executes the plugin's copy directly and refreshes this
  committed copy, which exists only so humans can reproduce the report
  after checkout.
- `playwright.config.ts` is platform-owned too, and is the one scaffold
  file to re-copy from the template on EVERY run rather than skip when
  present. It carries the launch args that make deployed endpoints
  reachable and the per-run results layout the report generator merges;
  a repo scaffolded before either landed keeps the old behaviour
  silently, which is exactly the class of bug those two exist to fix.

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
- **Your solo runs are the results.** The independence check below — each
  spec passing alone, twice consecutively — writes a results file under
  `tests/e2e/test-results/runs/` every time, and the report merges them.
  So the work of proving a spec is also the work of recording it; step 7
  is not repeating it.
- **Create the file before you explore for it.** For each criterion you
  are authoring fresh, write `tests/e2e/specs/<AC-ID>.spec.ts` with its
  `// spec:` header and nothing else, THEN explore, THEN fill in the
  body. The header is mandatory either way — `generate-report.mjs`
  hard-fails without it — so this changes only when you write it, and a
  header-only spec is how the platform knows that criterion has been
  picked up. Exploration is the longest stretch of this phase and the
  only one nothing else can see into.

  Only for specs you are CREATING. Never blank an existing spec back to
  its header: that registers as a pre-existing spec modified with no
  heal-log entry and fails the report.

### 7. RUN

Every run writes its **own** results file under
`tests/e2e/test-results/runs/`, and the report is the merge of them —
newest result per criterion wins. So no single command has to cover the
whole suite, and nothing you have already proved is thrown away by a
later call. In particular the solo runs from step 6 already count: each
spec you authored has a result before you get here.

Run the whole suite, and treat it as **best effort**:

```bash
npm test --prefix tests/e2e   # Bash timeout: use the max your Bash tool states
```

If it completes, it supersedes the per-spec results with one pass over
the deployed system in sequence — which is the only thing that catches a
spec depending on state another spec left behind. If it severs, it costs
you nothing: step 6's results still cover every spec you authored. A
severed call is detached rather than killed — it keeps running, so its
results may still land in `runs/` minutes later, which is a bonus rather
than something to wait on.

Do **not** delete anything under `test-results/runs/` first. Those are the
accumulated results; removing them is how you lose coverage you already
have.

Never `npx playwright test` from the repo root. It finds the specs and
passes anyway, without loading the config — so no reporter, nothing
written, and none of the launch args the endpoints need. Exit 0, nothing
written.

**A timed-out command still reports success.** Past the timeout the
harness detaches the command and hands back an OK result with no output —
identical, from where you sit, to a suite that finished. Never infer the
run completed from the call returning; check whether a new file appeared
under `test-results/runs/`.

If the suite will not fit one call, **cover it in pieces** — that is what
the merge is for:

```bash
npm test --prefix tests/e2e -- specs/AC-001-a.spec.ts specs/AC-001-b.spec.ts
```

You do not have to guess the right size. Run what you think fits, then
let step 9 tell you what is still missing: the report generator names
every spec on disk that has no result and refuses to emit until they all
do. A batch that severs is a batch you re-run smaller — its specs show up
in that list until its results land, if they ever do.

The suite includes the regression set — that's free regression coverage,
not an accident.

### 8. HEAL (bounded)

For failures, **Read `references/healing.md` now and follow it as the
binding HEAL discipline** before touching any spec. Then: triage each
one against the live app, repair only *brittleness* (locators, waits,
setup), never weaken what a test asserts. Log each heal in
`tests/e2e/heal-log.json`.

Re-run each healed spec on its own. That result is newer than the failure
it replaces, so it supersedes it in the report — you do not need a
closing full-suite run to make the repair count:

```bash
npm test --prefix tests/e2e -- specs/<AC-ID>.spec.ts
```

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
criteria but never writes them — coverage is expressed by each criterion's
pass/fail in the report, not by a flag in the criteria file. Exit code 2
means a contract violation:

- **a spec on disk with no result** — the generator names every one of
  them and refuses to emit. This is the ordinary loop, not a defect: run
  exactly those specs and regenerate. A severed batch lands here, which is
  how you find out which of its specs you still owe — though a severed
  call is detached, not killed, so look at `runs/` once more before
  re-running: its results may have arrived in the meantime.
- spec titles that don't map to criterion ids (fix titles, re-run those
  specs), or two spec files claiming one criterion inside a single run.
- a spec file missing its `// spec:` header (add the header, regenerate
  — no test re-run needed).
- a pre-existing spec modified without a heal-log entry (record the heal
  per `references/healing.md`).
- results recorded under different `rootDir` values or by different
  Playwright versions — those results describe two different checkouts.

The generator IS the coverage check, so there is no separate command to
run: generate, read what it names, cover that, generate again.

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
- Write or modify anything under `specs/` — the validation criteria are
  read-only input; all validation artifacts live under `tests/`.
- Delete or skip a previously committed spec. If one is obsolete, say
  so in the PR description and report — a human removes it.
- Leave `.only` / `.skip` / `.fixme` in committed specs.
- Hand-edit `report.md` / `report.json` — regenerate via the script.
- Commit playwright-cli session state, server logs, `test-results/`,
  or credentials. A login comes only from the step-4 roles gate ticket,
  exported in-session as `AEP_E2E_USERNAME` / `AEP_E2E_PASSWORD` and never
  written to a file, a spec, a report or a comment; if there is no login to
  read and a criterion needs one, mark it blocked in an issue comment and let
  it land `not_run`.
- Everything in the `aep` skill's deny-list (no default-branch pushes, one
  PR, no merging, no repo-settings changes). Its force-push exception is
  yours: `--force-with-lease` on your own branch, per step 10.
