# Authoring validation specs

**Binding authoring discipline for the aep-validation workflow (PLAN/GENERATE
steps). Follow every rule here.**

One rule above all: **the live app is the bar, not your reading of the
code**. A spec only counts once it has passed **twice consecutively**
against the live target. You may derive flows and locators from the
project's source when it's unambiguous — that's usually faster and the
live run catches stale assumptions — but the assertions themselves
always come from the criterion's `must`, never from what the source
happens to implement.

Use **playwright-cli** exploration whenever the live app is the only
trustworthy source of truth:

- the flow or locator is ambiguous from source + criteria alone;
- a spec you authored fails and you need to see what the app actually
  does (mandatory in healing triage — see `healing.md`);
- the app misbehaves: author the spec anyway so it fails honestly, and
  record what you observed in the test plan.

For playwright-cli command mechanics (sessions, refs, eval, storage
state), load the **`playwright-cli`** skill — especially its
`references/test-generation.md`. This doc only adds the AEP
validation discipline on top.

## When exploring: collect code, don't transcribe

Every playwright-cli action prints the Playwright code it ran
(`Ran Playwright code: await page.getByRole(...)...`). That output IS
your spec body — drive the criterion's flow end to end, collect the
emitted lines, and assemble the test from them instead of hand-writing
locators from memory:

```bash
playwright-cli open <url>
playwright-cli snapshot                 # refs: e1 [textbox "Name"], e2 [button "Say Hello"]
playwright-cli fill e1 "Ada"            # → await page.getByRole('textbox', { name: 'Name' }).fill('Ada');
playwright-cli click e2                 # → await page.getByRole('button', { name: 'Say Hello' }).click();
```

Actions generate code; **assertions you add yourself**, using the CLI
to capture stable locators and expected values:

```bash
playwright-cli --raw generate-locator e5      # locator for the assertion
playwright-cli --raw eval "el => el.textContent" e5   # expected text
playwright-cli --raw snapshot e5              # expected aria snapshot (region)
```

Assertion choices (from the CLI's own guidance):
- `toBeVisible()` — presence; prefer this when the locator itself is
  text-based (asserting text through a text-derived locator is
  circular).
- `toHaveText(...)` — pair with `getByLabel`/`getByTestId`-style
  locators that don't embed the asserted text.
- `toMatchAriaSnapshot(...)` — capture only what the criterion needs;
  use regular expressions for unstable values.

## The test plan (PLAN)

Write `tests/validation/test-plan.md` before any spec. One section per
criterion:

```markdown
## AC-001-a — A text box for entering a name is visible

- Target: hello-web (primary)
- Steps:
  1. Navigate to /
  2. Locate the name text box (role: textbox, name: "Name")
- Assert: the text box is visible
- Source of truth: hello-web/src/App.tsx (unambiguous) — or the
  playwright-cli snapshot ref if the flow was explored live
```

The plan is a reviewable artifact — a human should be able to check
your interpretation of each criterion before reading any code.

## Writing specs (GENERATE)

One criterion per file; the title prefix is the report's join key.
Every spec file MUST open with a `// spec:` header linking it to its
test-plan section — `generate-report.mjs` hard-fails without it, same
as a bad title. Step comments (one per plan step above the code that
executes it) are recommended for reviewability:

```ts
// spec: tests/validation/test-plan.md § AC-001-a
import { test, expect } from "@playwright/test";

test("AC-001-a: a name text box is visible", async ({ page }) => {
  // 1. Navigate to /
  await page.goto("/");
  // 2–3. Locate the name text box and assert visibility
  await expect(page.getByRole("textbox", { name: "Name" })).toBeVisible();
});
```

### UI discipline

- **Locators:** `getByRole` / `getByLabel` / `getByPlaceholder` /
  `getByText` — in that order of preference; the CLI's generated code
  already picks semantic locators, keep them. CSS/XPath only when no
  accessible locator exists (and say why in a comment). For inherently
  dynamic text (counters, timestamps), use a regex in the locator or
  assertion rather than the literal value.
- **Assertions:** web-first (`await expect(locator).toBeVisible()`,
  `.toHaveText()`, `.toHaveValue()` …) — they retry until timeout.
  Never `page.waitForTimeout(...)`, never wait for `networkidle` (a
  discouraged API); never assert on raw `page.content()` when a
  locator assertion exists.
- **Assert what the criterion claims — no more.** The `must` sentence
  is the contract. Extra assertions turn unrelated changes into false
  failures; missing assertions make the test vacuous.
- **Independence:** each spec must pass when run alone
  (`npx playwright test specs/AC-001-a.spec.ts`). No ordering
  dependencies, no state left behind for the next spec.
- **Unique test data:** the deployed environment persists between runs.
  Suffix created entities with a run marker
  (`` const name = `ada-${Date.now()}` ``) so re-runs don't collide
  with earlier data.

### API criteria (request fixture)

No browser needed — use the built-in `request` fixture against the
component's URL from the targets helper:

```ts
// spec: tests/validation/test-plan.md § AC-003-a
import { test, expect } from "@playwright/test";
import { target } from "../lib/targets";

test("AC-003-a: API returns Hello, <name>!", async ({ request }) => {
  const res = await request.post(`${target("hello-api")}/api/hello`, {
    data: { name: "Ada" },
  });
  expect(res.status()).toBe(200);
  expect(await res.json()).toMatchObject({ message: "Hello, Ada!" });
});
```

Verify the exact route/payload from the component's committed contract
(`specs/design/components/<name>/openapi.yaml`) and confirm it live
(`curl` or `request` exploration) before authoring.

### Authenticated apps

If the app requires login, use a Playwright **setup project** that logs
in once and saves `storageState` for the other specs, with credentials
from the environment only:

```ts
const user = process.env.AEP_E2E_USERNAME;
const pass = process.env.AEP_E2E_PASSWORD;
```

(For exploration sessions, `playwright-cli state-save` / `state-load`
serve the same purpose — see the `playwright-cli` skill's
`references/storage-state.md`.)

Never hardcode credentials in specs or commit a `storageState` file
(`.gitignore` it). If credentials are required but the env vars are
absent, don't fake a login: author the specs, let them land as
`not_run`/failing, and flag the blocker per the aep-validation workflow
(SKILL.md).

## Flakes

A spec that alternates pass/fail has not met the twice-consecutively
bar — it is brittle. Fix it now (see `healing.md` for the
brittleness taxonomy) rather than shipping a flake into the regression
set.
