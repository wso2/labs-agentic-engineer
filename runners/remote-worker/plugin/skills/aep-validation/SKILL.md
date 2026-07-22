---
name: aep-validation
description: Load when working a VALIDATION task dispatched by WSO2 Labs Agentic Engineer (the prompt says "validation task"; the issue is labelled `aep` + `validation`). The cwd is a clone of the project's repo on its default branch. You validate the deployed system against specs/validation/validation-criteria.json by authoring and running Playwright e2e tests, then open a PR containing the tests plus a validation report. This workflow REPLACES the implementation workflow in the `aep` skill; the auth model, git/gh conventions, and deny-list there still apply. The phase-specific discipline lives in this skill's `references/authoring.md` (explore + write specs) and `references/healing.md` (repair brittle specs); the `playwright-cli` companion skill carries the CLI mechanics.
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

**Reference files** used below live in this skill's `references/`
directory (in the platform runner: `/app/plugin/skills/aep-validation/references/`).

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
they are runtime inputs kept out of the public issue. You fetch them from
the platform in step 4.

Post a brief opening comment (`Starting validation: <one-line plan>`).

### 2. Branch

```bash
git checkout -b validation/issue-<N>
```

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

### 4. Fetch the validation context, then confirm the app is reachable

Deployed endpoint URLs come from the platform's secure validation-context
endpoint — never from the issue. Test credentials are a separate on-demand
request (below), made only when a criterion needs a login. `AEP_TASK_ID`
carries this run's execution id; the bearer rides a file.

```bash
curl -sf "$AEP_PLATFORM_URL/internal/v1/executions/$AEP_TASK_ID/validation-context" \
  -H "Authorization: Bearer $(cat "$AEP_BEARER_FILE")" > /tmp/validation-context.json
```

The response is `{ "endpoints": [{"component","url"}], "criteriaPath":
"..." }`. If the fetch fails or `AEP_PLATFORM_URL` is unset, post an issue
comment and exit with failure — do not guess endpoints.

- **Endpoints** become `tests/e2e/targets.json` (step 5) and your target
  list. Probe each URL (`curl -sf -o /dev/null <url>` or a playwright-cli
  visit) before authoring. Never start, build, or deploy the app — you
  validate what is already running; an unreachable endpoint → issue
  comment + exit failure.
- **Test credentials (on demand):** request them only when a criterion
  needs a login — POST the test-credentials endpoint with an optional
  `role` hint (the role the flow requires):

  ```bash
  curl -sf -X POST "$AEP_PLATFORM_URL/internal/v1/executions/$AEP_TASK_ID/test-credentials" \
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
  playwright.config.ts  copy references/playwright.config.template.ts
  targets.json          from the fetched validation-context endpoints (step 4)
  lib/targets.ts        copy references/targets.template.ts
  specs/                one spec file per criterion
  scripts/generate-report.mjs   copy references/generate-report.mjs VERBATIM
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
    "devDependencies": { "@playwright/test": "<value of $AEP_PLAYWRIGHT_VERSION>" }
  }
  ```
- `targets.json` shape: `{"targets": {"<component>": "<url>", ...},
  "primary": "<the web-facing component>"}`, filled from the step-4
  validation-context `endpoints`. On a re-validation run, refresh it from
  the newly fetched context.
- Install with `npm install` on first scaffold (commit the lockfile),
  `npm ci` on later runs.
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
- Load `aep:playwright-cli` with the Skill tool — the CLI's own skill
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

```bash
cd tests/e2e && npx playwright test
```

The config writes `test-results/results.json`. The run includes the
regression set — that's free regression coverage, not an accident.

### 8. HEAL (bounded)

For failures, **Read `references/healing.md` now and follow it as the
binding HEAL discipline** before touching any spec. Then: triage each
one against the live app, repair only *brittleness* (locators, waits,
setup), never weaken what a test asserts. Log each heal in
`tests/e2e/heal-log.json`. When the budget is exhausted, finish with
one final full run so `results.json` reflects the authoritative state.

### 9. REPORT

Generate the report deterministically — never hand-write it. Run the
**plugin's** copy of the script (always the current version, regardless
of what an earlier cycle committed into the repo):

```bash
node /app/plugin/skills/aep-validation/references/generate-report.mjs \
  --issue <N> --commit "$(git rev-parse HEAD)"
```

Then refresh the repo's committed copy so a human can reproduce the
report after checkout:

```bash
cp /app/plugin/skills/aep-validation/references/generate-report.mjs \
   tests/e2e/scripts/generate-report.mjs
```

This writes `tests/validation/report.md` + `report.json`. It reads the
oracle but never writes it — coverage is expressed by each criterion's
pass/fail in the report, not by a flag in the criteria file. Exit code 2
means a contract violation: spec titles that don't map to criterion ids
(fix titles, re-run tests from step 7), a spec file missing its
`// spec:` header (add the header, regenerate — no test re-run needed),
or a pre-existing spec modified without a heal-log entry (record the
heal per `references/healing.md`). Commit the report and tests together.

If `$AEP_PLATFORM_URL` is set, also push the report to the platform
(best-effort — do not fail the task if this call fails):

```bash
curl -sf -X POST "$AEP_PLATFORM_URL/internal/v1/executions/$AEP_TASK_ID/validation-report" \
  -H "Authorization: Bearer $(cat "$AEP_BEARER_FILE")" \
  -H "Content-Type: application/json" \
  --data-binary @tests/validation/report.json || true
```

### 10. PR

```bash
gh pr create \
  --title "Validation: <pass>/<total> e2e criteria passing (issue #<N>)" \
  --body $'Closes #<N>\n\n<summary table: pass/fail/not_run + manual/scenario counts>\n\nReport: tests/validation/report.md'
```

Open it **ready-for-review even when criteria fail** — the human reads
the report and decides. Post a closing issue comment with the summary
counts and the PR link.

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
- Everything in the `aep` skill's deny-list (no default-branch pushes,
  no force-push, one PR, no merging, no repo-settings changes).
