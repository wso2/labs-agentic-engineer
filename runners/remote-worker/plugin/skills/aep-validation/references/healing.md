# Healing validation specs

**Binding HEAL discipline for the aep-validation workflow. Follow every rule;
items marked MUST are enforced by `generate-report.mjs` and fail the run.**

**The rule, before anything else:** a heal may change *how the test
drives the app* — never *what the test claims the app does*. If a fix
would touch an expected value, a status code, or an assertion, it is
not a heal. The criterion fails, and that failure goes into the report
verbatim. A validation suite that heals genuine failures is worthless;
a red criterion is the phase working as designed.

## Triage: brittle or genuine?

For every failure, re-drive the exact steps with playwright-cli against
the live app before touching the spec:

- App behaves correctly live, but the spec fails → **brittle** (the
  test is wrong about *how* to drive/observe the app). Heal it.
- App itself misbehaves — wrong text, error page, 500, missing
  element the criterion requires → **genuine**. Do not touch the spec.

| Classification | Signature | Verdict |
|---|---|---|
| Locator drift | `locator not found`, strict-mode violation, timeout on a locator whose element IS in the live snapshot under a different role/name | brittle — heal |
| Timing | passes on manual re-drive; failure is a navigation/race (missing web-first assertion, asserting before a request settles) | brittle — heal |
| Data collision | failure mentions duplicate/existing entities from a previous run | brittle — heal (make test data unique) |
| Setup/session | expired or missing `storageState`, login step broke while the app's login works live | brittle — heal |
| Assertion mismatch | element/response found, but value differs from what the criterion expects | **genuine — report** |
| App error | 4xx/5xx, error page, crash, endpoint absent | **genuine — report** |

When unsure after re-driving: treat as genuine. A false red gets
caught by the human reviewer; a false green silently corrupts the
validation phase.

## Heal technique

- **One fix at a time, then retest.** When a spec has multiple errors,
  fix the first, re-run the spec, and only then look at the next —
  batched fixes hide which change mattered.
- Re-derive locators from a fresh live snapshot
  (`playwright-cli snapshot`, `playwright-cli --raw generate-locator eN`
  — see the `playwright-cli` skill), don't guess corrections.
- For inherently dynamic data (counters, timestamps, generated names),
  heal to a **regular-expression locator/assertion** that pins what the
  criterion actually requires — never to today's literal value.
- Never introduce `waitForTimeout`, `networkidle` waits, or other
  discouraged/deprecated APIs as a "fix" — replace bad waits with
  web-first assertions on the state the test actually depends on.

## Budget

- Max **2 heal attempts per criterion**; each followed by a focused
  re-run: `npx playwright test specs/<AC-ID>.spec.ts`.
- Max **2 focused re-run waves** after the initial full run.
- Then **one final full run** (`npx playwright test`) so
  `test-results/results.json` — the input to the report — reflects the
  authoritative end state.
- Still failing after the budget: leave it failing. In the plan/PR
  notes, mark it `genuine` or `unresolved (possibly brittle)`.

## Record every heal

The heal-log is mechanically enforced: `generate-report.mjs` hard-fails
when a pre-existing spec (one that existed at the base ref) was modified
this run without a matching heal-log entry. A heal that isn't logged
does not exist as far as the report is concerned.

One commit per heal, and one entry appended to
`tests/e2e/heal-log.json` (an array; create it if absent — it is
gitignored, per-run, and folded into the report by
`generate-report.mjs`):

```bash
git commit -m "heal(AC-001-b): locator drift: button renamed 'Say Hello' -> 'Greet'"
```

```json
{
  "criterionId": "AC-001-b",
  "spec": "specs/AC-001-b.spec.ts",
  "classification": "locator drift",
  "change": "getByRole('button', { name: 'Say Hello' }) -> { name: 'Greet' }",
  "commit": "<sha of the heal commit>"
}
```

## Forbidden moves

Never, under any classification:

- Edit an `expect(...)` expected value to match observed behavior.
- Delete or comment out an assertion.
- Convert `expect` to `expect.soft`.
- Add `.skip`, `.fixme`, or conditional early-returns around failures.
  (Playwright's own healer agent marks stubborn tests `test.fixme()` —
  that is test-suite maintenance, not validation. Here a stubborn
  failure stays red and goes in the report.)
- Wrap assertions in `try/catch`.
- Add retries in the config (retries stay 0 — they mask brittleness).
- Raise a timeout to "fix" a hang: >15s expect timeouts need the app to
  be genuinely slow, and that observation belongs in the report, not
  buried in config.
