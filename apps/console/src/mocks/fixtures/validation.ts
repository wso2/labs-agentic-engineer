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
// Two companion keys narrow it — whether the newest attempt is a REPEAT over a
// failed one (see ValidationAttempt below) and whether the repo has an oracle at all:
//   localStorage.setItem('aep:mock:acceptance-criteria', 'missing')
// which drops every specs/validation/acceptance/*.feature from the file list, so the page sees
// a version whose spec authored none (handlers/project.ts). The same key also takes:
//   localStorage.setItem('aep:mock:acceptance-criteria', 'drifted')
// which adds a SCENARIO to the feature files that the pinned report does not speak
// for — see DRIFTED below.
//
// Setting it alone is enough: with no `aep:mock:project` chosen, the base scenario
// becomes `deployed` rather than the usual `building`, because a verdict only
// exists on a version whose run got that far (see handlers/project.ts).
export type ValidationScenario = DeployStage["validation"];

/**
 * The two keys read together: what state the version is in, and — where the
 * scenario alone cannot say — whether a failed attempt came before it. One value
 * rather than two arguments, because every read below must be told the same
 * story or the page and the board disagree about one run again (#423).
 */
export interface ValidationStory {
  scenario: ValidationScenario;
  /** Whether the newest attempt is the first or a repeat — see ValidationAttempt. */
  attempt?: ValidationAttempt;
}

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

/** `bought-items` -> `specs/validation/acceptance/bought-items.feature`. */
function featurePath(slug: string): string {
  return `specs/validation/acceptance/${slug}.feature`;
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
  story: ValidationStory,
  drifted = false,
): { path: string; content: string }[] {
  const { outcomes, reported } = previousReportStands(story)
    ? { outcomes: FAILED, reported: true }
    : ARTIFACTS[story.scenario];
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

// Validation is its own run. The DEV run delivers the version — coding cycles
// only — and mints its validation task at deployed-green, settling with an EMPTY
// verdict: delivered, not yet judged. A VALIDATION run (kind `validation`, origin
// `revalidate` whether the sweep started it off that task or a person clicked)
// judges it in one cycle, or two when the agent merged without a report and the
// platform dispatched again. A failed verdict files one repair issue per failed
// criterion, an ordinary TASK run works them, and the sweep judges again. So a
// version's story is its dev run followed by one validation run per attempt, and
// every read takes the version's answer off the newest of those.
//
// The cycles are not decoration: the page reads each attempt's report at its own
// cycle's mergeSha, the deployments chip is derived from whether the newest
// validation cycle is still open, and the attempt count is the number of
// validation runs. A verdict with the wrong runs renders the wrong page.

// The dev run's one coding cycle: the increment, merged.
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

// One merge SHA per cycle number, so two attempts never claim one commit.
const MERGE_SHA: Record<number, string> = {
  2: "5c0de1a77b3f2049",
  3: "c2f7e19b04d6a583",
  4: "7ab41c90ee31d5f0",
  5: "e3d9a04f6b17c825",
};

// A judging that landed: merged its pull request and recorded a verdict.
function validationCycle(
  n: number,
  verdict: RunVerdict,
  at: { createdAt: string; endedAt: string },
): RunCycleView {
  return {
    id: `cycle-${String(n)}`,
    kind: "validation",
    attempts: 1,
    branch: `aep/m1-c${String(n)}`,
    prNumber: n + 2,
    prUrl: `${REPO_URL}/pull/${String(n + 2)}`,
    mergeSha: MERGE_SHA[n] ?? "0000000000000000",
    validationVerdict: verdict,
    // `validationTask` in project.ts — the one issue list-tasks hides and get-task
    // still answers for. Every attempt carries the SAME number because a
    // validation run ADOPTS the version's task — the sweep reopens it after a
    // repair rather than minting a second one — so a per-attempt number here
    // would misdescribe the real thing.
    validationIssue: 30,
    ...at,
  };
}

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
      // A validation run has nothing to build, so nothing to retrigger.
      buildRetriggers: over.kind === "validation" ? 0 : 1,
      validationCycles: cycles.filter((c) => c.kind === "validation").length,
    },
    validation: {},
    cycles,
    createdAt: "2026-07-10T09:12:00Z",
    startedAt: "2026-07-10T09:13:00Z",
    endedAt: "2026-07-10T09:43:00Z",
    ...over,
  };
}

// The version's dev run: delivered on one coding cycle, deployed green, its
// validation task minted, settled with no verdict of its own. Every judged
// scenario stands on it.
const DEV_RUN = run({});

// Where an attempt sits in the version's morning. The first is judged as soon
// as the dev run settles; the second comes after the repair, so a repeat story
// reads dev → attempt 1 → repair → attempt 2 down the page.
interface AttemptSlot {
  runId: string;
  /** The cycle number its judging takes — the next number is the re-dispatch. */
  cycle: number;
  createdAt: string;
  startedAt: string;
  cycleStart: string;
  cycleEnd: string;
  endedAt: string;
  /** When a person stopped it, for `cancelled`. */
  stoppedAt: string;
  /** The re-dispatch after `unreported`, and when the run gave up. */
  againStart: string;
  againEnd: string;
  againEndedAt: string;
}

const ATTEMPT_1: AttemptSlot = {
  runId: "run-v1-2",
  cycle: 2,
  createdAt: "2026-07-10T09:44:00Z",
  startedAt: "2026-07-10T09:45:00Z",
  cycleStart: "2026-07-10T09:45:00Z",
  cycleEnd: "2026-07-10T10:02:00Z",
  endedAt: "2026-07-10T10:03:00Z",
  stoppedAt: "2026-07-10T09:52:00Z",
  againStart: "2026-07-10T10:05:00Z",
  againEnd: "2026-07-10T10:20:00Z",
  againEndedAt: "2026-07-10T10:21:00Z",
};

const ATTEMPT_2: AttemptSlot = {
  runId: "run-v1-4",
  cycle: 4,
  createdAt: "2026-07-10T10:23:00Z",
  startedAt: "2026-07-10T10:24:00Z",
  cycleStart: "2026-07-10T10:24:00Z",
  cycleEnd: "2026-07-10T10:40:00Z",
  endedAt: "2026-07-10T10:41:00Z",
  stoppedAt: "2026-07-10T10:31:00Z",
  againStart: "2026-07-10T10:43:00Z",
  againEnd: "2026-07-10T10:58:00Z",
  againEndedAt: "2026-07-10T10:59:00Z",
};

// The scenarios a validation run can settle in — every verdict, plus the two
// lifecycle states that are the run's own. `none` and `skipped` are the dev
// run's to say, and `awaiting-fix` is a repair, not a judging.
type Judged = Exclude<ValidationScenario, "none" | "skipped" | "awaiting-fix">;
const JUDGED: readonly ValidationScenario[] = [
  "passed",
  "partial",
  "inconclusive",
  "failed",
  "unreported",
  "running",
  "cancelled",
];

function isJudged(scenario: ValidationScenario): scenario is Judged {
  return JUDGED.includes(scenario);
}

/**
 * One attempt: the validation run that reached `scenario`, in its slot. The
 * shapes are ValidationRunWorkflow's, not the dev loop's.
 */
function attempt(scenario: Judged, slot: AttemptSlot): MilestoneRunView {
  const base: Partial<MilestoneRunView> = {
    id: slot.runId,
    kind: "validation",
    origin: "revalidate",
    createdAt: slot.createdAt,
    startedAt: slot.startedAt,
    endedAt: slot.endedAt,
  };
  const judging = (verdict: RunVerdict) =>
    validationCycle(slot.cycle, verdict, { createdAt: slot.cycleStart, endedAt: slot.cycleEnd });

  switch (scenario) {
    case "passed":
    case "partial":
    case "inconclusive":
      // Honest reports, incomplete or not: the run settles green on them.
      return run({
        ...base,
        validation: { verdict: scenario, issue: 30, reportPath: REPORT_PATH },
        cycles: [judging(scenario)],
      });
    case "failed":
      // A real assertion loss. ONE cycle: the run files the failure as repair
      // work and stops, where the dev loop used to work it in place.
      return run({
        ...base,
        state: "failed",
        terminalReason: "validation-failed",
        validation: { verdict: "failed", issue: 30, reportPath: REPORT_PATH },
        cycles: [judging("failed")],
      });
    case "unreported":
      // The one verdict the run remedies itself, once: the agent merged without
      // a report, so it was dispatched again, and a second silence settled it.
      // The server omits the path: advertising one would send the client to a
      // 404 to rediscover what the verdict already said.
      return run({
        ...base,
        state: "failed",
        terminalReason: "validation-unreported",
        validation: { verdict: "unreported", issue: 30 },
        cycles: [
          judging("unreported"),
          validationCycle(slot.cycle + 1, "unreported", {
            createdAt: slot.againStart,
            endedAt: slot.againEnd,
          }),
        ],
        endedAt: slot.againEndedAt,
      });
    case "running":
      // Live, the judging itself in flight. No verdict yet — the chip is
      // `running` because the newest validation cycle is unfinished, which is
      // the only place that fact is knowable.
      return run({
        ...base,
        state: "running",
        endedAt: null,
        cycles: [
          { id: `cycle-${String(slot.cycle)}`, kind: "validation", attempts: 1, createdAt: slot.cycleStart },
        ],
      });
    case "cancelled":
      // A person STOPPED the judging. The cycle was opened and closed with no
      // merge SHA — what the agent stage records for a dispatch that produced
      // nothing — so the run settles carrying no verdict at all.
      return run({
        ...base,
        state: "cancelled",
        cycles: [
          {
            id: `cycle-${String(slot.cycle)}`,
            kind: "validation",
            attempts: 1,
            validationIssue: 30,
            createdAt: slot.cycleStart,
            endedAt: slot.stoppedAt,
          },
        ],
        endedAt: slot.stoppedAt,
      });
  }
}

// The repair between two attempts: the failed verdict filed one issue per failed
// criterion, and an ordinary task run worked them. What routes a repair back to
// a judging is the version's validation task, which the sweep reopens once the
// repair lands.
const REPAIR_RUN = run({
  id: "run-v1-3",
  kind: "task",
  origin: "incident-adoption",
  cycles: [CODING_3],
  createdAt: "2026-07-10T10:04:00Z",
  startedAt: "2026-07-10T10:04:00Z",
  endedAt: "2026-07-10T10:22:00Z",
});

// The first attempt failed and its repair was worked. Every repeat story stands
// on these, and every read that walks the older attempts finds the failed one.
const FAILED_ATTEMPT_1 = attempt("failed", ATTEMPT_1);

/** A version's runs, newest first as the server lists them, on a first attempt. */
const STORIES: Record<ValidationScenario, MilestoneRunView[]> = {
  passed: [attempt("passed", ATTEMPT_1), DEV_RUN],
  partial: [attempt("partial", ATTEMPT_1), DEV_RUN],
  inconclusive: [attempt("inconclusive", ATTEMPT_1), DEV_RUN],
  failed: [FAILED_ATTEMPT_1, DEV_RUN],
  unreported: [attempt("unreported", ATTEMPT_1), DEV_RUN],
  running: [attempt("running", ATTEMPT_1), DEV_RUN],
  cancelled: [attempt("cancelled", ATTEMPT_1), DEV_RUN],
  // Nothing to validate: no acceptance oracle, so no validation task was minted
  // and nothing will ever judge this version. The dev run says so itself — the
  // one verdict it records — because an empty one would read as "any moment now"
  // forever.
  skipped: [run({ validation: { verdict: "skipped" } })],
  // The dev run is live and has not delivered yet — the state every version
  // spends most of its life in. No validation run exists to be judged by.
  none: [
    run({
      state: "running",
      endedAt: null,
      cycles: [CODING_1, CODING_IN_FLIGHT],
    }),
  ],
  // Mid-repair: the first attempt failed, and the task run working its repair
  // issues is in flight. The failed report stays committed, which is what lets
  // the page show WHAT is being fixed while the fix is.
  //
  // What the platform's own derivation says of this shape today is `failed`:
  // the aggregate reads the newest VALIDATION run, which is terminal, and only
  // a live run holding a fatal verdict reads `awaiting-fix` — a shape the dev
  // loop had before validation became its own run. So this state reaches the
  // console only through the scenario override. Whether the platform should
  // say `awaiting-fix` here again is an open question on its side; the run
  // story is honest either way.
  "awaiting-fix": [
    run({
      id: "run-v1-3",
      kind: "task",
      origin: "incident-adoption",
      state: "running",
      endedAt: null,
      cycles: [CODING_IN_FLIGHT],
      createdAt: "2026-07-10T10:04:00Z",
      startedAt: "2026-07-10T10:04:00Z",
    }),
    FAILED_ATTEMPT_1,
    DEV_RUN,
  ],
};

// A version is judged as many times as it takes, and the scenario switch only
// says how the NEWEST attempt ended. The attempt key says whether that was the
// version's first judging or a repeat over a failed one whose repair was worked
// — the story that gives the page a history to stack, and the only one the
// platform produces beyond a single attempt:
//   localStorage.setItem('aep:mock:validation', 'passed')
//   localStorage.setItem('aep:mock:validation-attempt', 'repeat')
//
// A repeat is dev run → attempt 1 (failed) → repair → attempt 2 (the scenario).
// Read for every scenario a validation run can settle in; `none`, `skipped` and
// `awaiting-fix` have one shape each and ignore it.
export type ValidationAttempt = "first" | "repeat";

/** The key's accepted values — also the list the handler validates against. */
export const VALIDATION_ATTEMPTS: ValidationAttempt[] = ["first", "repeat"];

/** True when the story is a repeat attempt — which only a judged scenario can be. */
function isRepeat(story: ValidationStory): boolean {
  return story.attempt === "repeat" && isJudged(story.scenario);
}

// A report is overwritten by the next attempt to commit one, so a repeat attempt
// that has committed nothing yet — in flight, or stopped — leaves the FIRST
// attempt's failed report at the branch tip. Not `unreported`: that verdict IS
// the tip having been read and no report found there.
function previousReportStands(story: ValidationStory): boolean {
  return isRepeat(story) && (story.scenario === "running" || story.scenario === "cancelled");
}

/** The version's run story, newest run first as the server lists them. */
export function validationRuns(story: ValidationStory): BuildRunList {
  const runs =
    isRepeat(story) && isJudged(story.scenario)
      ? [attempt(story.scenario, ATTEMPT_2), REPAIR_RUN, FAILED_ATTEMPT_1, DEV_RUN]
      : STORIES[story.scenario];
  return { tag: "v1", milestoneNumber: 1, runs };
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

// ---------------------------------------------------------------------------
// The validation READ MODEL — the ledger, a version's history, one attempt's
// evidence.
//
// Derived from the same scenario switch the run story uses, so flipping
// `aep:mock:validation` moves every validation surface together. The alternative
// — a second set of rows describing the same state — is how the page and the
// board came to disagree about the same run in the first place.

type ValidationList = components["schemas"]["ValidationList"];
type ValidationSummary = components["schemas"]["ValidationSummary"];
type ValidationDetail = components["schemas"]["ValidationDetail"];
type ValidationSnapshot = components["schemas"]["ValidationSnapshot"];

// v0.2's one judging, months before the morning the scenario describes. Its own
// literal rather than `attempt()`, whose slots, cycle numbers and branch names
// all belong to v1 — two versions sharing a cycle id would open two attempts at
// once, since the page keys its sections on it.
const OLDER_PASSED_ATTEMPT: MilestoneRunView = {
  id: "run-v0.2-2",
  milestoneNumber: 2,
  milestoneTitle: "v0.2",
  kind: "validation",
  origin: "revalidate",
  state: "succeeded",
  budgets: {
    cyclesTotal: 1,
    cycleCeiling: 8,
    fixCycles: 0,
    conflictCycles: 0,
    buildRetriggers: 0,
    validationCycles: 1,
  },
  validation: { verdict: "passed", issue: 22, reportPath: REPORT_PATH },
  cycles: [
    {
      id: "cycle-v0.2-2",
      kind: "validation",
      attempts: 1,
      branch: "aep/m2-c2",
      prNumber: 2,
      prUrl: `${REPO_URL}/pull/2`,
      mergeSha: "b71c4e08d95a2f36",
      validationVerdict: "passed",
      validationIssue: 22,
      createdAt: "2026-04-02T09:12:00Z",
      endedAt: "2026-04-02T09:31:00Z",
    },
  ],
  createdAt: "2026-04-02T09:11:00Z",
  startedAt: "2026-04-02T09:12:00Z",
  endedAt: "2026-04-02T09:32:00Z",
};

/**
 * The versions behind the scenario's own, with the history each one has.
 *
 * They are the point of the page: a ledger exists so an older version is
 * reachable, and the revalidate gate can only be seen to work against a version
 * that is not deployed. Their runs are here rather than only their summary rows
 * because the rows are CLICKABLE — a row whose page answered with the current
 * version's attempts would make the fixture contradict itself on the one
 * feature the page was built for.
 *
 * v0.1 has no runs at all — a version built and never judged. Its dev run holds
 * no validation cycle, which is exactly what the detail read filters out.
 */
const OLDER_VERSIONS: {
  tag: string;
  milestoneNumber: number;
  scenario: ValidationScenario;
  runs: MilestoneRunView[];
}[] = [
  { tag: "v0.2", milestoneNumber: 2, scenario: "passed", runs: [OLDER_PASSED_ATTEMPT] },
  { tag: "v0.1", milestoneNumber: 3, scenario: "none", runs: [] },
];

/** The attempts a run list holds, oldest first — the runs arrive newest first. */
function attemptsIn(runs: readonly MilestoneRunView[]) {
  return [...runs]
    .reverse()
    .flatMap((r) => (r.cycles ?? []).filter((c) => c.kind === "validation"));
}

/** One ledger row, dated by the version's NEWEST attempt as the server dates it. */
function summaryOf(
  tag: string,
  milestoneNumber: number,
  state: ValidationScenario,
  runs: readonly MilestoneRunView[],
): ValidationSummary {
  const cycles = attemptsIn(runs);
  const newest = cycles[cycles.length - 1];
  return {
    tag,
    milestoneNumber,
    state,
    ...(newest?.createdAt ? { startedAt: newest.createdAt } : {}),
    ...(newest?.endedAt ? { endedAt: newest.endedAt } : {}),
  };
}

/** The ledger. The scenario's own version, then the ones before it. */
export function validationLedger(story: ValidationStory): ValidationList {
  return {
    validations: [
      summaryOf("v1", 1, story.scenario, validationRuns(story).runs ?? []),
      ...OLDER_VERSIONS.map((v) => summaryOf(v.tag, v.milestoneNumber, v.scenario, v.runs)),
    ],
  };
}

/** One version's validation history, filtered as the server filters it. */
export function validationDetail(story: ValidationStory, tag = "v1"): ValidationDetail {
  const older = OLDER_VERSIONS.find((v) => v.tag === tag);
  const list = validationRuns(story);
  const runs = (older ? older.runs : (list.runs ?? []))
    .map((r) => ({ ...r, cycles: (r.cycles ?? []).filter((c) => c.kind === "validation") }))
    .filter((r) => r.cycles.length > 0);
  return {
    tag,
    milestoneNumber: older?.milestoneNumber ?? list.milestoneNumber,
    state: older?.scenario ?? story.scenario,
    live: runs.some((r) => !TERMINAL_RUN_STATES.has(r.state)),
    // The scenario's own version is the deployed one; the older rows are not.
    // Without a NOT-deployed version in the fixtures the revalidate gate cannot
    // be seen to work at all — every page would offer the trigger and the
    // disabled state would exist only in tests.
    deployed: tag === "v1",
    runs,
  };
}

const TERMINAL_RUN_STATES = new Set(["succeeded", "failed", "cancelled", "blocked"]);

/**
 * One attempt's evidence: the report, and the criteria at the same commit.
 *
 * A running attempt has no commit and therefore no report — which is the state
 * the scenario list's `running` first attempt puts the page in.
 *
 * Read at the ATTEMPT's own commit, never at the branch tip: on a repeat the
 * first attempt is answered with what it committed, the failed report, and the
 * repeat itself only with what it has committed — nothing, while it is in
 * flight. That is what `previousReportStands` is NOT for: it models the tip,
 * which is the Spec view's question and not this one.
 *
 * An older version answers from its own verdict, and drift does not reach it:
 * its commit is pinned, and only the tip can move.
 */
export function validationSnapshot(
  story: ValidationStory,
  drifted = false,
  cycleId?: string,
  tag = "v1",
): ValidationSnapshot {
  const older = OLDER_VERSIONS.find((v) => v.tag === tag);
  const historical =
    isRepeat(story) && FAILED_ATTEMPT_1.cycles.some((c) => c.id === cycleId);
  // Whose artifacts this attempt committed. A pinned commit is also the one
  // drift cannot reach — only the tip moves.
  const pinned: { scenario: ValidationScenario } | undefined =
    older ?? (historical ? { scenario: "failed" } : undefined);
  const files = pinned
    ? validationFiles({ scenario: pinned.scenario })
    : validationFiles({ scenario: story.scenario }, drifted);
  const report = files.find((f) => f.path === REPORT_PATH);
  return {
    commit: report ? "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c" : "",
    criteria: files.filter((f) => f.path !== REPORT_PATH),
    ...(report ? { report: report.content } : {}),
  };
}
