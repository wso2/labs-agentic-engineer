import type { components } from "../../generated/aep-api";

type BuildRunList = components["schemas"]["BuildRunList"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];
type DeployStage = components["schemas"]["DeployStage"];
type IssueComment = components["schemas"]["IssueComment"];
// The six VERDICTS, which is a strict subset of the ten states the chip can
// show: `none`, `running`, `awaiting-fix` and `cancelled` are lifecycle, and no
// run row or cycle record ever carries them.
type RunVerdict = NonNullable<
  components["schemas"]["RunValidation"]["verdict"]
>;

// Scenario switch for the VALIDATION surface, orthogonal to the project scenario
// in ./project.ts. `deploy.validation` has ten values and only ONE of them is
// reachable from the project scenarios, so every other state of the Validation
// page — the four non-green verdicts, the three lifecycle states, the empty ones —
// could previously only be seen by hand-editing a fixture.
//
// Toggle in devtools, then reload:
//   localStorage.setItem('aep:mock:validation', 'failed')
//   localStorage.removeItem('aep:mock:validation')   // back to the project scenario
//
// Two companion keys narrow the `running` scenario further — which ATTEMPT is in
// flight (see ValidationAttempt below) and whether the repo has an oracle at all:
//   localStorage.setItem('aep:mock:validation-criteria', 'missing')
// which drops every specs/acceptance/*.feature from the file list, so the page sees
// a version whose spec authored none (handlers/project.ts). The same key also takes:
//   localStorage.setItem('aep:mock:validation-criteria', 'drifted')
// which adds a SCENARIO to the feature files that the pinned report does not speak
// for — see DRIFTED below.
//
// Setting it alone is enough: with no `aep:mock:project` chosen, the base scenario
// becomes `deployed` rather than the usual `building`, because a verdict only
// exists on a version whose run got that far (see handlers/project.ts).
export type ValidationScenario = DeployStage["validation"];

/** The switch's accepted values — also the list the handler validates against. */
export const VALIDATION_SCENARIOS: ValidationScenario[] = [
  "none",
  "running",
  "awaiting-fix",
  "cancelled",
  "passed",
  "partial",
  "failed",
  "inconclusive",
  "unreported",
  "skipped",
];

// The paths this module owns. The handler drops them from the project scenario's
// file list before splicing in the ones a verdict override implies.
export const REPORT_PATH = "tests/acceptance/report.json";

// Same repo the project fixtures use. Duplicated rather than imported so this
// module stays a LEAF — ./project.ts imports the default artifacts from here, and
// a cycle between two fixture modules is a hazard nobody would expect to debug.
const REPO_URL = "https://github.com/acme-dev/demo-shop";

// ---------------------------------------------------------------------------
// The oracle and the report, built together from one catalogue
// ---------------------------------------------------------------------------

// Every scenario draws from this one catalogue, so a reader comparing two verdicts
// sees only the OUTCOMES move: a feature's name, its rules, its wording and its
// steps are the same everywhere they appear.

interface ScenarioSpec {
  name: string;
  /** `@negative` — the product refuses, rejects or limits. */
  negative?: boolean;
  steps: { keyword: string; text: string }[];
}

interface RuleSpec {
  story: number;
  text: string;
  scenarios: ScenarioSpec[];
}

interface FeatureSpec {
  slug: string;
  name: string;
  rules: RuleSpec[];
}

const CATALOGUE: FeatureSpec[] = [
  {
    slug: "browsing-the-catalog",
    name: "Browsing the catalog",
    rules: [
      {
        story: 1,
        text: "A shopper can find a product by name",
        scenarios: [
          {
            name: "Searching for a product by name",
            steps: [
              { keyword: "Given", text: 'the catalog has a product named "Cedar Desk Lamp"' },
              { keyword: "When", text: 'Priya searches for "Cedar Desk"' },
              { keyword: "Then", text: 'the results include "Cedar Desk Lamp"' },
            ],
          },
        ],
      },
      {
        story: 1,
        text: "A shopper can narrow the catalog to one category",
        scenarios: [
          {
            name: "Filtering the catalog by category",
            steps: [
              { keyword: "Given", text: "the catalog has products in Lighting and in Accessories" },
              { keyword: "When", text: "Priya filters the catalog to Accessories" },
              { keyword: "Then", text: "every product shown is in Accessories" },
            ],
          },
          // Deliberately LAST in the last rule of its feature — see DRIFTED below.
          {
            name: "A search that matches nothing explains itself",
            negative: true,
            steps: [
              { keyword: "Given", text: "the catalog has no product named \"Zeppelin\"" },
              { keyword: "When", text: 'Priya searches for "Zeppelin"' },
              { keyword: "Then", text: "she is shown that nothing matched, and no products" },
            ],
          },
        ],
      },
    ],
  },
  {
    slug: "the-cart",
    name: "The cart",
    rules: [
      {
        story: 2,
        text: "A cart's contents survive a browser restart",
        scenarios: [
          {
            name: "Reopening the shop with items in the cart",
            steps: [
              { keyword: "Given", text: 'Priya has "Cedar Desk Lamp" in her cart' },
              { keyword: "When", text: "she closes the browser and opens the shop again" },
              { keyword: "Then", text: 'her cart still holds "Cedar Desk Lamp"' },
            ],
          },
        ],
      },
      {
        story: 2,
        text: "The cart total tracks what is in it",
        scenarios: [
          {
            name: "Adding an item updates the total",
            steps: [
              { keyword: "Given", text: "Priya has an empty cart" },
              { keyword: "When", text: 'she adds "Cedar Desk Lamp" at "42.00"' },
              { keyword: "Then", text: 'the cart total is "42.00"' },
            ],
          },
          {
            name: "A cart cannot hold more of an item than the shop has",
            negative: true,
            steps: [
              { keyword: "Given", text: '"Cedar Desk Lamp" has 2 left in stock' },
              { keyword: "When", text: "Priya tries to add 3 of them to her cart" },
              { keyword: "Then", text: 'her cart holds 2 of "Cedar Desk Lamp"' },
            ],
          },
        ],
      },
    ],
  },
  {
    slug: "checkout",
    name: "Checkout",
    rules: [
      {
        story: 3,
        text: "Completing checkout creates an order in the shopper's history",
        scenarios: [
          {
            name: "Placing an order",
            steps: [
              { keyword: "Given", text: 'Priya has "Cedar Desk Lamp" in her cart' },
              { keyword: "When", text: "she completes checkout" },
              { keyword: "Then", text: 'her order history includes an order for "Cedar Desk Lamp"' },
            ],
          },
        ],
      },
      {
        story: 3,
        text: "Payment details are transmitted over an encrypted connection",
        scenarios: [
          {
            name: "The payment step is encrypted",
            steps: [
              { keyword: "Given", text: "Priya is at the payment step" },
              { keyword: "When", text: "she submits her card details" },
              { keyword: "Then", text: "the details leave the browser encrypted" },
            ],
          },
        ],
      },
    ],
  },
];

/**
 * A scenario the feature files carry and the pinned report does not.
 *
 * It is the one that sits LAST in the last rule of its feature, so including it
 * moves no other scenario's line number — the report's `line` values stay true of
 * the files shipped beside them, drifted or not.
 *
 * Not a contrived state: the console reads the feature files at the branch tip and
 * the report at the merge commit of the attempt that wrote it, so any scenario
 * authored since that attempt looks exactly like this. Asking the agent for one
 * more scenario after reading a failure is the ordinary way to get here.
 */
const DRIFTED = "A search that matches nothing explains itself";

/** What a run made of one scenario, and the evidence it recorded step by step. */
interface Step {
  command?: string;
  exit?: number;
  observed?: string;
}

/**
 * The failure-time capture — what the SYSTEM was doing when a scenario failed,
 * read while the page was still open. Only a `failed` outcome carries one, and
 * the checker requires it there: after the run neither half can be recovered.
 */
interface Capture {
  network: { method: string; url: string; status: number }[];
  console: string[];
  snapshot?: string;
}

interface Outcome {
  outcome: "passed" | "failed" | "blocked" | "unjudgeable";
  /**
   * Aligned to the scenario's steps, and allowed to be SHORTER: a blocked run
   * stops where it was stopped, and the steps past that point are specification
   * the run never reached.
   */
  steps: Step[];
  /** Required on `failed`, meaningless anywhere else. */
  capture?: Capture;
}

interface Artifacts {
  /** The outcome map, or undefined when the project authored no feature files. */
  outcomes?: Record<string, Outcome>;
  /** Whether the run committed a report — `unreported` is the absence of one. */
  reported: boolean;
}

/** `bought-items` -> `specs/acceptance/bought-items.feature`. */
function featurePath(slug: string): string {
  return `specs/acceptance/${slug}.feature`;
}

interface Located {
  file: string;
  feature: string;
  rule: string;
  line: number;
}

/**
 * The feature files for a set of scenario names, and where each one landed.
 *
 * Emitting the text and recording the line in one pass is what keeps the report's
 * `line` honest: typed line numbers go stale the moment a step is reworded, and a
 * stale one is exactly the breach `check-report.mjs` fails a real run for.
 */
function featureFiles(names: ReadonlySet<string>): {
  files: { path: string; content: string }[];
  located: Map<string, Located>;
} {
  const files: { path: string; content: string }[] = [];
  const located = new Map<string, Located>();

  for (const feature of CATALOGUE) {
    const lines: string[] = [`Feature: ${feature.name}`];
    let wrote = false;
    for (const rule of feature.rules) {
      const scenarios = rule.scenarios.filter((s) => names.has(s.name));
      if (scenarios.length === 0) continue;
      wrote = true;
      lines.push("", `  @story-${rule.story}`, `  Rule: ${rule.text}`);
      for (const scenario of scenarios) {
        lines.push("");
        if (scenario.negative) lines.push("    @negative");
        lines.push(`    Scenario: ${scenario.name}`);
        located.set(scenario.name, {
          file: featurePath(feature.slug),
          feature: feature.name,
          rule: rule.text,
          line: lines.length,
        });
        for (const step of scenario.steps) lines.push(`      ${step.keyword} ${step.text}`);
      }
    }
    if (wrote) files.push({ path: featurePath(feature.slug), content: `${lines.join("\n")}\n` });
  }
  return { files, located };
}

/** Every step a scenario declares, by name. */
const STEPS_OF = new Map<string, { keyword: string; text: string }[]>(
  CATALOGUE.flatMap((f) => f.rules.flatMap((r) => r.scenarios.map((s) => [s.name, s.steps] as const))),
);

/** Whether a scenario carries `@negative`, by name. */
const NEGATIVE_OF = new Set(
  CATALOGUE.flatMap((f) =>
    f.rules.flatMap((r) => r.scenarios.filter((s) => s.negative).map((s) => s.name)),
  ),
);

/** The isolation note. A paragraph, as a real one is — the view gives it room. */
const ISOLATION =
  "Every scenario creates the cart or the order it asserts on through the shop's own " +
  "interface, with a run-unique suffix on any product name it adds, and asserts only " +
  "within it. The two catalog scenarios have no container to own, so they assert on " +
  "the change in the result count rather than on an absolute total — sound because " +
  "the platform runs one validation at a time per version.";

/** schemaVersion 2, keyed by scenario, as `report.go` and the run's checker read it. */
function reportFor(
  outcomes: Record<string, Outcome>,
  located: Map<string, Located>,
): string {
  return JSON.stringify(
    {
      schemaVersion: 2,
      generatedAt: "2026-07-20T10:00:00.000Z",
      commit: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678",
      baseUrl: "https://demo-shop--development.openchoreoapis.localhost:19080/",
      isolation: ISOLATION,
      scenarios: Object.entries(outcomes).map(([name, outcome]) => {
        const where = located.get(name);
        const steps = STEPS_OF.get(name) ?? [];
        return {
          feature: where?.feature ?? "",
          featureFile: where?.file ?? "",
          line: where?.line ?? 0,
          rule: where?.rule ?? "",
          scenario: name,
          tags: NEGATIVE_OF.has(name) ? ["@negative"] : [],
          outcome: outcome.outcome,
          // Only as far as the run got. The steps past the end are specification,
          // and the view renders them as never reached.
          steps: outcome.steps.map((e, i) => ({
            text: steps[i]?.text ?? "",
            keyword: steps[i]?.keyword ?? "",
            ...(e.command !== undefined ? { command: e.command } : {}),
            ...(e.exit !== undefined ? { exit: e.exit } : {}),
            ...(e.observed !== undefined ? { observed: e.observed } : {}),
          })),
          ...(outcome.capture !== undefined ? { evidence: outcome.capture } : {}),
        };
      }),
    },
    null,
    2,
  );
}

// Settled three ways, so the evidence on screen is the shape a real run writes:
// a `wait` whose exit code IS the verdict, a `get count` that exits 0 because the
// command RAN and therefore has to record the value the agent read, and a step
// with no command at all, which has to say why.
const found = (what: string): Step => ({
  command: `agent-browser wait --text ${JSON.stringify(what)} --timeout 3000`,
  exit: 0,
});
const counted = (selector: string, observed: string): Step => ({
  command: `agent-browser get count ${JSON.stringify(selector)}`,
  exit: 0,
  observed,
});
const clicked = (name: string): Step => ({
  command: `agent-browser find role button click --name ${JSON.stringify(name)}`,
  exit: 0,
});

const PASS_SEARCH: Outcome = {
  outcome: "passed",
  steps: [
    { command: 'POST /products {"name":"Cedar Desk Lamp-vr8821","category":"Lighting"}', exit: 0 },
    { command: 'agent-browser find label "Search" fill "Cedar Desk"; agent-browser press Enter', exit: 0 },
    found("Cedar Desk Lamp-vr8821"),
  ],
};
const PASS_FILTER: Outcome = {
  outcome: "passed",
  steps: [
    { command: "POST /products x2 (Lighting, Accessories)", exit: 0 },
    clicked("Accessories"),
    counted('[data-testid="product"]:not([data-category="Accessories"])', "0 — every row shown is in Accessories"),
  ],
};
const PASS_PERSIST: Outcome = {
  outcome: "passed",
  steps: [
    { command: "POST /cart/items {\"name\":\"Cedar Desk Lamp-vr8821\"}", exit: 0 },
    { command: "agent-browser close; agent-browser open https://demo-shop…/", exit: 0 },
    found("Cedar Desk Lamp-vr8821"),
  ],
};
const PASS_TOTAL: Outcome = {
  outcome: "passed",
  steps: [
    { command: "agent-browser get count \"[data-testid=cart-row]\"", exit: 0, observed: "0 — the cart starts empty" },
    clicked("Add to cart"),
    found("42.00"),
  ],
};
const PASS_ORDER: Outcome = {
  outcome: "passed",
  steps: [
    { command: 'POST /cart/items {"name":"Cedar Desk Lamp-vr8821"}', exit: 0 },
    clicked("Place order"),
    found("Cedar Desk Lamp-vr8821"),
  ],
};

// A refusal the shop gets WRONG: it accepts three of a product it has two of.
const FAIL_STOCK: Outcome = {
  outcome: "failed",
  steps: [
    { command: 'POST /products {"name":"Cedar Desk Lamp-vr8821","stock":2}', exit: 0 },
    {
      command: 'agent-browser find label "Quantity" fill "3"; agent-browser find role button click --name "Add to cart"',
      exit: 0,
      observed: "the form accepted the quantity and returned to the cart",
    },
    {
      command: 'agent-browser get value "[data-testid=cart-qty]"',
      exit: 1,
      observed: '3 — the cart holds three of a product the shop has two of',
    },
  ],
  // The request LEFT and the server said yes. That is what makes this a stock
  // rule the shop does not enforce, rather than a form that failed to submit —
  // and the step trace above reads identically for both.
  capture: {
    network: [{ method: "POST", url: "/cart/items", status: 201 }],
    console: [],
    snapshot: '- row "Cedar Desk Lamp-vr8821"\n  - textbox "Quantity": "3"',
  },
};

// Blocked, not failed: the control the When needs is ABSENT, so the behaviour was
// never reached. A person has to say whether that is the shop correctly refusing
// or the shop being broken, which is why no repair is filed for it.
const BLOCKED_STOCK: Outcome = {
  outcome: "blocked",
  steps: [
    { command: 'POST /products {"name":"Cedar Desk Lamp-vr8821","stock":2}', exit: 0 },
    {
      command: 'agent-browser snapshot -i',
      observed:
        'The quantity control on the product page is a select whose options stop at the stock count — it offers "1" and "2" and nothing else, and the free-text box the desktop layout used is absent here. There is no enabled control through which a quantity of 3 can be entered, so the action this step describes cannot be attempted through the UI. `agent-browser network requests` confirms no POST /cart/items left the page.',
    },
  ],
};

// Unjudgeable: the answer lives outside the running app. Honest, and not a defect.
const UNJUDGEABLE_TLS: Outcome = {
  outcome: "unjudgeable",
  steps: [
    { command: "agent-browser open https://demo-shop…/checkout/payment", exit: 0 },
    clicked("Pay"),
    {
      observed:
        "Whether the details left the browser encrypted is a property of the transport, not of anything the shop renders: the page shows a confirmation either way, and the development deployment terminates TLS at the gateway ahead of the app. Nothing in the running system can settle this, so it is reported rather than guessed at.",
    },
  ],
};

// Everything settled, everything green. The two scenarios a run cannot settle are
// absent from the oracle entirely — a `passed` verdict over a spec containing one
// is unreachable, because an unsettled scenario is precisely what makes a run
// `partial`.
const PASSED: Record<string, Outcome> = {
  "Searching for a product by name": PASS_SEARCH,
  "Filtering the catalog by category": PASS_FILTER,
  "Reopening the shop with items in the cart": PASS_PERSIST,
  "Adding an item updates the total": PASS_TOTAL,
  "Placing an order": PASS_ORDER,
};

// Everything that ran passed, and two scenarios could not be settled — one blocked
// by an absent control, one whose truth lives outside the app. Exercises every
// outcome chip except `Failed`.
const PARTIAL: Record<string, Outcome> = {
  ...PASSED,
  "A cart cannot hold more of an item than the shop has": BLOCKED_STOCK,
  "The payment step is encrypted": UNJUDGEABLE_TLS,
};

// One real defect, beside one scenario nobody could settle — the pair the page has
// to keep apart, because one says the behaviour is wrong and the other says it was
// never reached.
const FAILED: Record<string, Outcome> = {
  ...PASSED,
  "A cart cannot hold more of an item than the shop has": FAIL_STOCK,
  "The payment step is encrypted": UNJUDGEABLE_TLS,
};

// Nothing passed: every scenario was blocked or unanswerable, which is what
// `inconclusive` means — the run produced no verdict anyone can act on.
const INCONCLUSIVE: Record<string, Outcome> = {
  "A cart cannot hold more of an item than the shop has": BLOCKED_STOCK,
  "The payment step is encrypted": UNJUDGEABLE_TLS,
};

/**
 * The default oracle: the widest set, which the Spec view previews with no report.
 */
export const DEFAULT_ACCEPTANCE_FEATURES = featureFiles(
  new Set([...Object.keys(PARTIAL), DRIFTED]),
).files;

/**
 * The default report, paired with the default verdict on ./project.ts's settled
 * run. It is the `partial` one because that is the only verdict this oracle can
 * honestly reach: two of its scenarios are ones a run cannot settle, so a green
 * report over it would be claiming a result nobody produced.
 */
export const DEFAULT_VALIDATION_REPORT = (() => {
  const { located } = featureFiles(new Set(Object.keys(PARTIAL)));
  return reportFor(PARTIAL, located);
})();

// Per verdict: what a project in that state actually has on disk. Chosen together,
// because a verdict is a statement ABOUT the pair — `unreported` is not an empty
// report, it is the absence of one, and `skipped` has no oracle at all, which is
// why it was skipped.
const ARTIFACTS: Record<ValidationScenario, Artifacts> = {
  passed: { outcomes: PASSED, reported: true },
  partial: { outcomes: PARTIAL, reported: true },
  failed: { outcomes: FAILED, reported: true },
  inconclusive: { outcomes: INCONCLUSIVE, reported: true },
  // The run reached its validation cycle's merge commit and found nothing there.
  unreported: { outcomes: PARTIAL, reported: false },
  // No acceptance criteria were ever authored — that IS the reason it was skipped.
  skipped: { reported: false },
  // The oracle exists; the report does not yet, because the attempt is still
  // running or has not started. A REPEAT attempt does have one — see below.
  running: { outcomes: PARTIAL, reported: false },
  none: { outcomes: PARTIAL, reported: false },
  // Same pair as `none`: the criteria were authored, and the attempt that would
  // have written a report against them was stopped before it committed one. The
  // absence here is why the page must not read `cancelled` as "no criteria" — they
  // are right there, unanswered.
  cancelled: { outcomes: PARTIAL, reported: false },
  // Mid-repair: the failed attempt's report is committed and stays readable, which
  // is what lets the page show WHAT is being fixed while the fix is in flight.
  "awaiting-fix": { outcomes: FAILED, reported: true },
};

/** The validation artifacts a scenario puts in the repo, as Files-API entries. */
export function validationFiles(
  scenario: ValidationScenario,
  attempt: ValidationAttempt = "first",
  drifted = false,
): { path: string; content: string }[] {
  // A repeat attempt is running OVER a failed one whose report is still committed —
  // which is what its copy counts. A first attempt has the oracle and nothing else.
  const { outcomes, reported } = isRepeat(scenario, attempt)
    ? { outcomes: FAILED, reported: true }
    : ARTIFACTS[scenario];
  if (!outcomes) return [];

  // Only the SPECIFICATION moves: a report is written once and pinned, so drift can
  // only ever come from the feature-file side.
  const names = new Set(Object.keys(outcomes));
  const { located } = featureFiles(names);
  if (drifted) names.add(DRIFTED);
  const { files } = featureFiles(names);

  return [
    ...files,
    ...(reported ? [{ path: REPORT_PATH, content: reportFor(outcomes, located) }] : []),
  ];
}

/**
 * Every path this module can put in the repo — what the handler swaps out before
 * splicing in the ones a verdict override implies.
 */
export const ACCEPTANCE_PATHS = CATALOGUE.map((f) => featurePath(f.slug));
export const VALIDATION_FILE_PATHS = [...ACCEPTANCE_PATHS, REPORT_PATH];

// ---------------------------------------------------------------------------
// The run story behind each verdict
// ---------------------------------------------------------------------------

// The cycles are not decoration: the page reads the report at the LAST validation
// cycle's mergeSha, the deployments chip is derived from whether the LATEST cycle
// is an in-flight validation one, and `awaiting-fix` exists only because a coding
// cycle follows a failed attempt. A verdict with the wrong cycles renders the
// wrong page.

const CODING_1: RunCycleView = {
  id: "cycle-1",
  kind: "coding",
  attempts: 1,
  branch: "aep/m1-c1",
  prNumber: 3,
  prUrl: `${REPO_URL}/pull/3`,
  resolves: [9],
  mergeSha: "dcb1edc5fe0417b2",
  createdAt: "2026-07-10T09:14:00Z",
  endedAt: "2026-07-10T09:41:00Z",
};

// The repair: an ordinary coding cycle over the issues the failed attempt filed.
// There is no "repair" kind, because a repair is ordinary work.
const CODING_3: RunCycleView = {
  id: "cycle-3",
  kind: "coding",
  attempts: 1,
  branch: "aep/m1-c3",
  prNumber: 5,
  prUrl: `${REPO_URL}/pull/5`,
  resolves: [13],
  mergeSha: "9f2ab4c81de60357",
  createdAt: "2026-07-10T10:05:00Z",
  endedAt: "2026-07-10T10:21:00Z",
};

// In flight: branch, PR and merge SHA are LEARNED FROM WEBHOOKS, so a cycle that
// has not opened a pull request yet carries none of them.
const CODING_IN_FLIGHT: RunCycleView = {
  id: "cycle-3",
  kind: "coding",
  attempts: 1,
  createdAt: "2026-07-10T10:05:00Z",
};

function validationCycle(
  n: number,
  verdict: RunVerdict,
  over: Partial<RunCycleView> = {},
): RunCycleView {
  return {
    id: `cycle-${String(n)}`,
    kind: "validation",
    attempts: 1,
    branch: `aep/m1-c${String(n)}`,
    prNumber: n + 2,
    prUrl: `${REPO_URL}/pull/${String(n + 2)}`,
    mergeSha: n === 2 ? "5c0de1a77b3f2049" : "7ab41c90ee31d5f0",
    validationVerdict: verdict,
    // `validationTask` in project.ts — the one issue list-tasks hides and get-task
    // still answers for. Every validation cycle carries the SAME number because the
    // platform reopens the version's issue for a repeat attempt rather than minting
    // a second one, so a per-cycle number here would misdescribe the real thing.
    validationIssue: 30,
    createdAt: "2026-07-10T09:45:00Z",
    endedAt: "2026-07-10T10:02:00Z",
    ...over,
  };
}

const VALIDATION_IN_FLIGHT: RunCycleView = {
  id: "cycle-2",
  kind: "validation",
  attempts: 1,
  createdAt: "2026-07-10T09:45:00Z",
};

// The two counters the server DERIVES from the cycle ledger — the supervisor bumps
// them as it appends cycles, so a fixture that states them by hand states them
// wrong the moment its cycle list changes. Counting instead is what keeps every
// scenario self-consistent: `skipped` has one cycle and no validation, `none` has
// two coding cycles and no validation, and neither can drift again.
//
// The rest of the budgets stay literal: they are spend against ceilings, which no
// cycle list implies.
function run(over: Partial<MilestoneRunView>): MilestoneRunView {
  const cycles = over.cycles ?? [CODING_1];
  return {
    id: "run-v1-1",
    milestoneNumber: 1,
    milestoneTitle: "v1",
    kind: "dev",
    origin: "spec-build",
    state: "succeeded",
    budgets: {
      cyclesTotal: cycles.length,
      cycleCeiling: 8,
      fixCycles: 0,
      conflictCycles: 0,
      buildRetriggers: 1,
      validationCycles: cycles.filter((c) => c.kind === "validation").length,
    },
    validation: {},
    cycles,
    createdAt: "2026-07-10T09:12:00Z",
    startedAt: "2026-07-10T09:13:00Z",
    endedAt: "2026-07-10T10:41:00Z",
    ...over,
  };
}

// A run that answered on its first attempt.
function firstAttemptRun(verdict: RunVerdict): MilestoneRunView {
  return run({
    validation: { verdict, issue: 30, reportPath: REPORT_PATH },
    cycles: [CODING_1, validationCycle(2, verdict)],
  });
}

// A run that spent BOTH attempts on the same answer: attempt 1 failed, the
// platform filed the failure as ordinary work, a coding cycle worked it, and
// attempt 2 came back the same. Spending the attempts is what settles the run —
// the first failure alone does not.
function exhaustedRun(
  verdict: RunVerdict,
  terminalReason: string,
  reportPath: boolean,
): MilestoneRunView {
  return run({
    state: "failed",
    terminalReason,
    validation: {
      verdict,
      issue: 30,
      // The server omits the path for `unreported`: advertising one would send the
      // client to a 404 to rediscover what the verdict already said.
      ...(reportPath ? { reportPath: REPORT_PATH } : {}),
    },
    cycles: [
      CODING_1,
      validationCycle(2, verdict),
      CODING_3,
      validationCycle(4, verdict, {
        createdAt: "2026-07-10T10:24:00Z",
        endedAt: "2026-07-10T10:40:00Z",
      }),
    ],
  });
}

const RUNS: Record<ValidationScenario, MilestoneRunView> = {
  passed: firstAttemptRun("passed"),
  partial: firstAttemptRun("partial"),
  inconclusive: firstAttemptRun("inconclusive"),
  failed: exhaustedRun("failed", "validation-failed", true),
  unreported: exhaustedRun("unreported", "validation-unreported", false),
  // Nothing to validate: the run never dispatched a validation cycle, so there is
  // no cycle to read a report at and the verdict is the workflow's, not a report's.
  skipped: run({ validation: { verdict: "skipped" } }),
  // Live, with the validation cycle itself in flight. No verdict yet — the chip is
  // `running` because the LATEST cycle is an unfinished validation one, which is
  // the only place that fact is knowable.
  running: run({
    state: "running",
    endedAt: null,
    cycles: [CODING_1, VALIDATION_IN_FLIGHT],
  }),
  // Live, mid self-heal: a real `failed` verdict from attempt 1, an attempt still
  // in budget, and an ordinary coding cycle in flight against the repair issues.
  // The verdict is deliberately NOT hidden — the run row carries it, which is what
  // the deployments board turns into `awaiting-fix` rather than a terminal `failed`.
  "awaiting-fix": run({
    state: "running",
    endedAt: null,
    validation: { verdict: "failed", issue: 30, reportPath: REPORT_PATH },
    cycles: [CODING_1, validationCycle(2, "failed"), CODING_IN_FLIGHT],
  }),
  // A person STOPPED the judging. The validation cycle was opened and closed with
  // no merge SHA — what the agent stage records for a dispatch that produced
  // nothing — so the run settles carrying no verdict at all. Kind and origin are
  // the validation run's own, and that is the whole distinction this scenario
  // exists to show: only a run of THAT kind reads as `cancelled`, because a
  // cancelled dev run is an abandoned increment and means something else.
  cancelled: run({
    kind: "validation",
    origin: "revalidate",
    state: "cancelled",
    validation: {},
    cycles: [
      {
        id: "cycle-2",
        kind: "validation",
        attempts: 1,
        validationIssue: 30,
        createdAt: "2026-07-10T09:45:00Z",
        endedAt: "2026-07-10T09:52:00Z",
      },
    ],
  }),
  // The run is live and has not reached validation at all — the state every run
  // spends most of its life in.
  none: run({
    state: "running",
    endedAt: null,
    cycles: [CODING_1, CODING_IN_FLIGHT],
  }),
};

// `running` is the one scenario with TWO honest shapes, because the loop repeats:
// a first attempt (no verdict yet, nothing to report) and a repeat attempt (the
// previous attempt's verdict still on the row, its report still committed). They
// render differently — only the repeat has a verdict tile, and only its copy marks
// its numbers as the last attempt's — and `deploy.validation` is `running` for both,
// so no value of the scenario switch can tell them apart.
//
// Hence a second devtools key rather than an eleventh scenario:
//   localStorage.setItem('aep:mock:validation', 'running')
//   localStorage.setItem('aep:mock:validation-attempt', 'repeat')
//
// It is read only for `running`; every other scenario has one shape and ignores it.
export type ValidationAttempt = "first" | "repeat";

/** The key's accepted values — also the list the handler validates against. */
export const VALIDATION_ATTEMPTS: ValidationAttempt[] = ["first", "repeat"];

// Attempt 1 merged and failed, a coding cycle repaired it, attempt 2 is in flight
// against the fixed system. The in-flight cycle is re-id'd because
// VALIDATION_IN_FLIGHT is hardcoded `cycle-2`, which the merged attempt owns here.
const RUNNING_REPEAT: MilestoneRunView = run({
  state: "running",
  endedAt: null,
  validation: { verdict: "failed", issue: 30, reportPath: REPORT_PATH },
  cycles: [
    CODING_1,
    validationCycle(2, "failed"),
    CODING_3,
    {
      ...VALIDATION_IN_FLIGHT,
      id: "cycle-4",
      createdAt: "2026-07-10T10:24:00Z",
    },
  ],
});

/** True when the scenario/attempt pair is the repeat-attempt shape. */
function isRepeat(
  scenario: ValidationScenario,
  attempt: ValidationAttempt,
): boolean {
  return scenario === "running" && attempt === "repeat";
}

/** The version's run story for a validation scenario. */
export function validationRuns(
  scenario: ValidationScenario,
  attempt: ValidationAttempt = "first",
): BuildRunList {
  const row = isRepeat(scenario, attempt) ? RUNNING_REPEAT : RUNS[scenario];
  return { tag: "v1", milestoneNumber: 1, runs: [row] };
}

// ---------------------------------------------------------------------------
// The agent's STATUS LINE — the validation issue's comment thread.
//
// The agent keeps its issue's newest comment current while it works
// (`skills/aep/SKILL.md`, "The status line"), and the tile renders that line's
// first row. It is the only run-wide narration that survives a reload, so the
// fixture's job is to show a line the derived sentence could not have produced:
// the middle of a run, where the derived sentence can only count criteria.
//
// Oldest first, matching the contract — the tile reads the LAST one.
//
// Only `running` renders: the page shows this line while validation is running
// and at no other time, because a comment outlives its run and the closing
// summary would otherwise narrate a finished attempt forever. The three settled
// and repairing threads below are therefore NOT dead fixture — they are how the
// gate is seen to work, by switching the scenario and watching the line go away.
type StatusPost = { body: string; observed?: boolean };

const STATUS_THREAD: Partial<Record<ValidationScenario, StatusPost[]>> = {
  // The shape a real run takes: the agent's opener, the platform's rungs as it
  // watches the run work, and the agent speaking again only for the thing no
  // command shows. `running` ends on the platform's line, so the tile renders
  // the unlabelled common case.
  running: [
    { body: "Starting validation: 12 criteria, 9 need new specs." },
    { body: "Setting up the test harness…", observed: true },
    { body: "Exploring the deployed app to author automated tests…", observed: true },
    { body: "Authoring automated tests…", observed: true },
    { body: "Running automated tests against the deployed system…", observed: true },
  ],
  // Ends on the AGENT's line, which is what renders the "The agent:" label — the
  // two scenarios are how the attribution is seen to work, by switching between
  // them and watching the prefix appear.
  "awaiting-fix": [
    { body: "Starting validation: 12 criteria, 9 need new specs." },
    { body: "Running automated tests against the deployed system…", observed: true },
    { body: "3 of 12 failed — report committed, PR #14 open for review." },
  ],
  passed: [
    { body: "Starting validation: 12 criteria, 9 need new specs." },
    { body: "Generating the validation report from the automated test results…", observed: true },
    { body: "All 12 covered and passing. Report committed, PR #14 open." },
  ],
  failed: [
    { body: "Starting validation: 12 criteria, 9 need new specs." },
    { body: "Running automated tests against the deployed system…", observed: true },
    { body: "AC-004-b blocked: the roles gate published no second login." },
    { body: "3 of 12 failed — report committed, PR #14 open for review." },
  ],
};

/**
 * The validation issue's comments for a scenario, or undefined when the agent
 * has posted nothing.
 *
 * Undefined rather than `[]` on purpose: the contract omits the field for every
 * empty case, and a scenario with no thread is what exercises the tile's
 * FALLBACK to the derived sentence — the path a run takes when its posts could
 * not reach GitHub at all.
 */
export function validationStatusThread(
  scenario: ValidationScenario,
): IssueComment[] | undefined {
  const bodies = STATUS_THREAD[scenario];
  if (!bodies) return undefined;
  // Fifteen minutes apart, inside the window the run's own cycles occupy, so the
  // thread reads as one run's narration rather than as history from another day.
  return bodies.map((post, i) => ({
    id: `vc-${String(i + 1)}`,
    author: "aep-bot",
    body: post.body,
    createdAt: `2026-07-10T09:${String(45 + i * 5).padStart(2, "0")}:00Z`,
    url: `${REPO_URL}/issues/30#issuecomment-${String(i + 1)}`,
    // Author cannot separate these — the platform and the runner share one
    // credential — so the brand is the only thing that can, here as on the wire.
    ...(post.observed ? { observed: true } : {}),
  }));
}
