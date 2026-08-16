import type { components } from "../../generated/aep-api";

type BuildRunList = components["schemas"]["BuildRunList"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];
type DeployStage = components["schemas"]["DeployStage"];
// The six VERDICTS, which is a strict subset of the nine states the chip can
// show: `none`, `running` and `awaiting-fix` are lifecycle, and no run row or
// cycle record ever carries them.
type RunVerdict = NonNullable<components["schemas"]["RunValidation"]["verdict"]>;

// Scenario switch for the VALIDATION surface, orthogonal to the project scenario
// in ./project.ts. `deploy.validation` has nine values and only ONE of them is
// reachable from the project scenarios, so every other state of the Validation
// page — the four non-green verdicts, the two lifecycle states, the empty ones —
// could previously only be seen by hand-editing a fixture.
//
// Toggle in devtools, then reload:
//   localStorage.setItem('aep:mock:validation', 'failed')
//   localStorage.removeItem('aep:mock:validation')   // back to the project scenario
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
  "passed",
  "partial",
  "failed",
  "inconclusive",
  "unreported",
  "skipped",
];

// The paths this module owns. The handler drops them from the project scenario's
// file list before splicing in the ones a verdict override implies.
export const CRITERIA_PATH = "specs/validation/validation-criteria.json";
export const REPORT_PATH = "tests/validation/report.json";
export const VALIDATION_FILE_PATHS = [CRITERIA_PATH, REPORT_PATH];

// Same repo the project fixtures use. Duplicated rather than imported so this
// module stays a LEAF — ./project.ts imports the default artifacts from here, and
// a cycle between two fixture modules is a hazard nobody would expect to debug.
const REPO_URL = "https://github.com/acme-dev/demo-shop";

// ---------------------------------------------------------------------------
// The oracle and the report, built together from one catalogue
// ---------------------------------------------------------------------------

// Every scenario draws its criteria from this one list, so a reader comparing two
// verdicts sees only the OUTCOMES move: a criterion's id, requirement, wording and
// method are the same everywhere they appear.
interface CatalogueEntry {
  req: string;
  statement: string;
  id: string;
  must: string;
  method: "e2e" | "scenario" | "manual";
}

const REQ_001 = "Shoppers can browse and search the catalog by name and category.";
const REQ_002 = "Cart contents persist across browser sessions.";
const REQ_003 = "Checkout produces an order visible in the shopper's order history.";

const CATALOGUE: CatalogueEntry[] = [
  {
    req: "REQ-001",
    statement: REQ_001,
    id: "AC-001-a",
    must: "A shopper can search products by name and see matching results",
    method: "e2e",
  },
  {
    req: "REQ-001",
    statement: REQ_001,
    id: "AC-001-b",
    must: "A shopper can filter the catalog by category",
    method: "e2e",
  },
  {
    req: "REQ-002",
    statement: REQ_002,
    id: "AC-002-a",
    must: "A cart's contents survive a browser restart for the same shopper",
    method: "e2e",
  },
  {
    req: "REQ-002",
    statement: REQ_002,
    id: "AC-002-b",
    must: "The cart total updates promptly as items are added or removed",
    method: "scenario",
  },
  {
    req: "REQ-003",
    statement: REQ_003,
    id: "AC-003-a",
    must: "Completing checkout creates an order visible in order history",
    method: "e2e",
  },
  {
    req: "REQ-003",
    statement: REQ_003,
    id: "AC-003-b",
    must: "Payment details are transmitted over an encrypted connection",
    method: "manual",
  },
];

/** One criterion's outcome in a run report — the only thing a scenario varies. */
interface Outcome {
  status: "pass" | "fail" | "not_run" | "not_validated" | "manual";
  spec?: string;
  // The real shape the runner writes: an object, not a bare string. A
  // string-shaped mock is what let the view's failure block look fine while dead.
  failure?: { message: string; location: string };
  flaky?: boolean;
  healed?: boolean;
  durationMs?: number;
}

interface Artifacts {
  /** validation-criteria.json — absent when the project authored no oracle. */
  criteria?: string;
  /** report.json — absent when the run committed none (`unreported`). */
  report?: string;
}

// build derives BOTH files from one outcome map, which is what keeps a scenario
// honest: the oracle can only list criteria the report speaks for, and the
// report's totals are counted rather than typed. Criteria absent from the map are
// absent from the oracle — that is how `passed` gets an all-automatable oracle
// while `inconclusive` gets one with nothing automatable in it.
function build(outcomes: Record<string, Outcome>): { criteria: string; report: string } {
  const chosen: { entry: CatalogueEntry; outcome: Outcome }[] = [];
  for (const entry of CATALOGUE) {
    const outcome = outcomes[entry.id];
    if (outcome) chosen.push({ entry, outcome });
  }

  const byReq = new Map<string, { statement: string; entries: CatalogueEntry[] }>();
  for (const { entry } of chosen) {
    const group = byReq.get(entry.req);
    if (group) group.entries.push(entry);
    else byReq.set(entry.req, { statement: entry.statement, entries: [entry] });
  }

  const criteria = JSON.stringify(
    {
      requirements: [...byReq].map(([id, group]) => ({
        id,
        statement: group.statement,
        criteria: group.entries.map((c) => ({
          id: c.id,
          must: c.must,
          method: c.method,
        })),
      })),
    },
    null,
    2,
  );

  const e2e = { total: 0, pass: 0, fail: 0, notRun: 0 };
  let manual = 0;
  let scenario = 0;
  for (const { entry, outcome } of chosen) {
    if (entry.method === "manual") manual += 1;
    else if (entry.method === "scenario") scenario += 1;
    else {
      e2e.total += 1;
      if (outcome.status === "pass") e2e.pass += 1;
      else if (outcome.status === "fail") e2e.fail += 1;
      else e2e.notRun += 1;
    }
  }

  const report = JSON.stringify(
    {
      schemaVersion: 1,
      issue: 30,
      commit: "a1b2c3d",
      generatedAt: "2026-07-20T10:00:00.000Z",
      playwrightVersion: "1.55.0",
      totals: { e2e, manual, scenario },
      criteria: chosen.map(({ entry, outcome }) => ({
        id: entry.id,
        requirementId: entry.req,
        // The generator echoes the `must` into the report, so a reader (and a
        // repair issue) never needs the oracle to know what the criterion demanded.
        must: entry.must,
        method: entry.method,
        status: outcome.status,
        spec: outcome.spec ?? null,
        healed: outcome.healed ?? false,
        healAttempts: outcome.healed ? 1 : 0,
        flaky: outcome.flaky ?? false,
        durationMs: outcome.durationMs ?? 0,
        failure: outcome.failure ?? null,
      })),
    },
    null,
    2,
  );

  return { criteria, report };
}

// A representative Playwright failure: multi-line, with the locator that timed
// out, so the failure block is exercised at a realistic size.
const TIMEOUT_FAILURE = {
  message:
    "TimeoutError: locator.click: Timeout 5000ms exceeded.\n  waiting for getByRole('option', { name: 'Accessories' })",
  location: "tests/e2e/specs/AC-001-b.spec.ts:31",
};

// Everything automated, everything green. The oracle is the four e2e criteria
// ONLY: a `passed` verdict over an oracle carrying a manual criterion is
// unreachable, because the runner reports that criterion `manual` and an uncovered
// criterion is precisely what makes a run `partial`.
const PASSED = build({
  "AC-001-a": { status: "pass", spec: "tests/e2e/specs/AC-001-a.spec.ts", durationMs: 1840 },
  "AC-001-b": {
    status: "pass",
    spec: "tests/e2e/specs/AC-001-b.spec.ts",
    healed: true,
    durationMs: 2210,
  },
  "AC-002-a": { status: "pass", spec: "tests/e2e/specs/AC-002-a.spec.ts", durationMs: 980 },
  "AC-003-a": {
    status: "pass",
    spec: "tests/e2e/specs/AC-003-a.spec.ts",
    flaky: true,
    durationMs: 3120,
  },
});

// Something passed, nothing failed, and three criteria were never answered — one
// e2e that never ran plus the two methods no runner executes. Exercises every
// state chip except `fail`.
const PARTIAL = build({
  "AC-001-a": { status: "pass", spec: "tests/e2e/specs/AC-001-a.spec.ts", durationMs: 1840 },
  "AC-001-b": { status: "pass", spec: "tests/e2e/specs/AC-001-b.spec.ts", durationMs: 2600 },
  "AC-002-a": { status: "not_run" },
  "AC-002-b": { status: "not_validated" },
  "AC-003-a": {
    status: "pass",
    spec: "tests/e2e/specs/AC-003-a.spec.ts",
    healed: true,
    durationMs: 3120,
  },
  "AC-003-b": { status: "manual" },
});

// The same oracle with one criterion lost. `fail` wins the verdict outright, which
// is why this scenario keeps the uncovered criteria: the tile has to count the
// failure against the whole set, not against the criteria that ran.
const FAILED = build({
  "AC-001-a": { status: "pass", spec: "tests/e2e/specs/AC-001-a.spec.ts", durationMs: 1840 },
  "AC-001-b": {
    status: "fail",
    spec: "tests/e2e/specs/AC-001-b.spec.ts",
    failure: TIMEOUT_FAILURE,
    flaky: true,
    durationMs: 2600,
  },
  "AC-002-a": { status: "not_run" },
  "AC-002-b": { status: "not_validated" },
  "AC-003-a": {
    status: "pass",
    spec: "tests/e2e/specs/AC-003-a.spec.ts",
    healed: true,
    durationMs: 3120,
  },
  "AC-003-b": { status: "manual" },
});

// Nothing failed because nothing ran: an oracle whose criteria are all manual or
// scenario, plus an e2e whose spec was never written. Zero passes is what makes it
// inconclusive rather than partial.
const INCONCLUSIVE = build({
  "AC-002-a": { status: "not_run" },
  "AC-002-b": { status: "not_validated" },
  "AC-003-b": { status: "manual" },
});

/**
 * The default oracle: the full six, mixed across all three methods. It is what the
 * Spec view previews (criteria only, no report), so it stays the widest one.
 */
export const DEFAULT_VALIDATION_CRITERIA = PARTIAL.criteria;

/**
 * The default report, paired with the default verdict on ./project.ts's settled
 * run. It is the `partial` one because that is the only verdict this oracle can
 * honestly reach: two of its six criteria are methods no runner executes, so a
 * green report over it would be claiming a result for criteria nobody checked.
 */
export const DEFAULT_VALIDATION_REPORT = PARTIAL.report;

// Per verdict: the pair of files a project in that state actually has on disk.
// Chosen together, because a verdict is a statement ABOUT the pair — `unreported`
// is not an empty report, it is the absence of one, and `skipped` has no oracle at
// all, which is why it was skipped.
const ARTIFACTS: Record<ValidationScenario, Artifacts> = {
  passed: PASSED,
  partial: PARTIAL,
  failed: FAILED,
  inconclusive: INCONCLUSIVE,
  // The run reached its validation cycle's merge commit and found nothing there.
  unreported: { criteria: PARTIAL.criteria },
  // No acceptance oracle was ever authored — that IS the reason it was skipped.
  skipped: {},
  // The oracle exists; the report does not yet, because the attempt is still
  // running or has not started. A REPEAT attempt does have one — see REPEAT_ARTIFACTS.
  running: { criteria: PARTIAL.criteria },
  none: { criteria: PARTIAL.criteria },
  // Mid-repair: the failed attempt's report is committed and stays readable, which
  // is what lets the page show WHAT is being fixed while the fix is in flight.
  "awaiting-fix": FAILED,
};

/** The validation artifacts a scenario puts in the repo, as Files-API entries. */
export function validationFiles(
  scenario: ValidationScenario,
  attempt: ValidationAttempt = "first",
): { path: string; content: string }[] {
  // A repeat attempt is running OVER a failed one whose report is still committed —
  // which is what its copy counts. A first attempt has the oracle and nothing else.
  const { criteria, report } = isRepeat(scenario, attempt)
    ? FAILED
    : ARTIFACTS[scenario];
  return [
    ...(criteria ? [{ path: CRITERIA_PATH, content: criteria }] : []),
    ...(report ? [{ path: REPORT_PATH, content: report }] : []),
  ];
}

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
    validationIssue: 12,
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
    validation: { verdict, issue: 12, reportPath: REPORT_PATH },
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
      issue: 12,
      // The server omits the path for `unreported`: advertising one would send the
      // client to a 404 to rediscover what the verdict already said.
      ...(reportPath ? { reportPath: REPORT_PATH } : {}),
    },
    cycles: [
      CODING_1,
      validationCycle(2, verdict),
      CODING_3,
      validationCycle(4, verdict, { createdAt: "2026-07-10T10:24:00Z", endedAt: "2026-07-10T10:40:00Z" }),
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
    validation: { verdict: "failed", issue: 12, reportPath: REPORT_PATH },
    cycles: [CODING_1, validationCycle(2, "failed"), CODING_IN_FLIGHT],
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
// Hence a second devtools key rather than a tenth scenario:
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
  validation: { verdict: "failed", issue: 12, reportPath: REPORT_PATH },
  cycles: [
    CODING_1,
    validationCycle(2, "failed"),
    CODING_3,
    { ...VALIDATION_IN_FLIGHT, id: "cycle-4", createdAt: "2026-07-10T10:24:00Z" },
  ],
});

/** True when the scenario/attempt pair is the repeat-attempt shape. */
function isRepeat(scenario: ValidationScenario, attempt: ValidationAttempt): boolean {
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
