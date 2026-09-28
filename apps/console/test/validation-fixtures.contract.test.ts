/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// OUTSIDE src/ deliberately. This is the only test in the console that needs
// node's own APIs — it shells out to the real checker — and `tsconfig.json` pins
// `types` to `vite/client` precisely so app code cannot reach for `process.env`
// and still typecheck. Adding node to that array (or referencing it from inside
// src/) widens the whole PROGRAM: node's `setTimeout` overload then beats the
// DOM's, and sibling packages start failing on `Timeout` vs `number`. So the
// node-side harness lives here, where `include: ["src"]` never sees it.

import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, describe, expect, it } from "vitest";
import {
  validationDetail,
  validationFiles,
  validationLedger,
  validationRuns,
  validationSnapshot,
  VALIDATION_SCENARIOS,
  type ValidationScenario,
  type ValidationStory,
} from "../src/mocks/fixtures/validation";

const REPO = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const CHECKER = join(REPO, "skills/validation-task/scripts/check-report.mjs");

let dir: string | undefined;
afterEach(() => {
  if (dir) rmSync(dir, { recursive: true, force: true });
  dir = undefined;
});

function layOut(files: { path: string; content: string }[]): string {
  const root = mkdtempSync(join(tmpdir(), "aep-mock-"));
  mkdirSync(join(root, "specs/validation/acceptance"), { recursive: true });
  mkdirSync(join(root, "tests/acceptance"), { recursive: true });
  for (const f of files) writeFileSync(join(root, f.path), f.content);
  return root;
}

/**
 * Every mock scenario that ships a report has to satisfy the contract a REAL run
 * is held to — the checker the run itself invokes, not a second opinion about it.
 *
 * Without this the fixtures only have to look plausible, and a mock that is
 * merely plausible is how a view comes to be designed against a shape nothing
 * writes: `line` numbers typed rather than counted, a `passed` scenario with no
 * command behind it, a `blocked` one that never says what stopped it. Each of
 * those renders perfectly well and is a lie.
 */
describe("the mock validation fixtures satisfy the run's own report contract", () => {
  const reported = VALIDATION_SCENARIOS.filter(
    (s) => validationFiles({ scenario: s }).some((f) => f.path === "tests/acceptance/report.json"),
  );

  it("covers the verdicts that ship a report", () => {
    expect([...reported].sort()).toEqual(
      ["awaiting-fix", "failed", "inconclusive", "partial", "passed"],
    );
  });

  it.each(reported)("%s passes check-report.mjs", (scenario) => {
    dir = layOut(validationFiles({ scenario }));
    // Throws on a nonzero exit, and the checker exits 2 on a contract breach with
    // every one of them printed — so a failure here names what is wrong.
    expect(() => execFileSync("node", [CHECKER, dir as string], { encoding: "utf8" })).not.toThrow();
  });

  // Drift is the one state where the two files are SUPPOSED to disagree: the
  // feature files carry a scenario the pinned report predates. The checker is
  // right to fail it, and it must fail for that reason alone — a drifted fixture
  // that also broke some other rule would be indistinguishable.
  it("drifts by exactly one uncovered scenario, and nothing else", () => {
    dir = layOut(validationFiles({ scenario: "partial" }, true));
    let output = "";
    expect(() => {
      try {
        execFileSync("node", [CHECKER, dir as string], { encoding: "utf8" });
      } catch (e) {
        output = String((e as { stdout?: string }).stdout ?? "");
        throw e;
      }
    }).toThrow();
    const breaches = output.split("\n").filter((l) => l.trim().startsWith("x "));
    expect(breaches).toHaveLength(1);
    expect(breaches[0]).toContain("has no entry in the report");
  });
});

/**
 * The read-model fixtures describe the SAME run as the run-story ones.
 *
 * They are derived from it rather than written beside it, and this is what
 * holds that true: a second hand-written set describing the same state is how
 * the validation page and the deployments board came to disagree about one run
 * in the first place (#423). A fixture that can contradict itself teaches the
 * UI to render a state the platform cannot produce.
 */
describe("the validation read-model fixtures agree with the run story", () => {
  it.each(VALIDATION_SCENARIOS)("%s reports the same state everywhere", (scenario) => {
    const ledger = validationLedger({ scenario });
    const detail = validationDetail({ scenario });
    const current = ledger.validations.find((v) => v.tag === "v1");

    expect(current?.state).toBe(scenario);
    expect(detail.state).toBe(scenario);
    expect(detail.milestoneNumber).toBe(current?.milestoneNumber);
  });

  // The ledger exists to make older versions reachable, so a fixture with one
  // row would hide the feature it is there to show.
  it("always offers more than the scenario's own version", () => {
    const rows = validationLedger({ scenario: "passed" }).validations;
    expect(rows.length).toBeGreaterThan(1);
    // Including one never validated — the row a reader most needs to find.
    expect(rows.some((r) => r.state === "none")).toBe(true);
  });

  it("carries only validation cycles, on runs that attempted one", () => {
    for (const scenario of VALIDATION_SCENARIOS) {
      for (const attempt of ["first", "repeat"] as const) {
        for (const run of validationDetail({ scenario, attempt }).runs) {
          expect(run.cycles.length).toBeGreaterThan(0);
          for (const c of run.cycles) expect(c.kind).toBe("validation");
        }
      }
    }
  });

  // A snapshot pairs a report with the criteria AT THE SAME COMMIT. An attempt
  // still running has no commit, so it has criteria and no report — which is a
  // different fact from an empty report and renders as a different screen.
  // The ledger's older rows exist so an old version is READABLE; `deployed`
  // marks the one that is also revalidatable. A fixture set where every version
  // is deployed would leave the gate untestable by hand.
  it("marks exactly the scenario's own version as deployed", () => {
    expect(validationDetail({ scenario: "passed" }).deployed).toBe(true);
    expect(validationDetail({ scenario: "passed" }, "v0.2").deployed).toBe(false);
  });

  // Every row is a link. A ledger whose rows all open the same page would be
  // three copies of one version, and the older rows exist precisely to prove
  // they are not — so each row's page has to answer for the row.
  it("opens each ledger row onto that version's own history", () => {
    for (const scenario of VALIDATION_SCENARIOS) {
      for (const row of validationLedger({ scenario }).validations) {
        const detail = validationDetail({ scenario }, row.tag);
        expect(detail.tag).toBe(row.tag);
        expect(detail.state).toBe(row.state);
        expect(detail.milestoneNumber).toBe(row.milestoneNumber);
      }
    }
  });

  // An older version's attempt is read at ITS commit, so its report is the one
  // its own verdict implies — not whatever the scenario key currently says.
  it("answers an older version's attempt with that version's report", () => {
    for (const scenario of VALIDATION_SCENARIOS) {
      const snap = validationSnapshot({ scenario }, false, "cycle-v0.2-2", "v0.2");
      expect(snap.report).toBe(validationSnapshot({ scenario: "passed" }).report);
    }
  });

  it("pairs a report with a commit, or has neither", () => {
    for (const scenario of VALIDATION_SCENARIOS) {
      const snap = validationSnapshot({ scenario });
      expect(Boolean(snap.report)).toBe(Boolean(snap.commit));
    }
  });
});

/**
 * The run story is the SPLIT platform's: a dev run delivers and never judges,
 * and every attempt is a validation run of its own. A repeat attempt is the one
 * story with a history — dev run, failed attempt, repair, the scenario's attempt
 * — and what the page's two-block cards and their attempt numbers exist for.
 */
describe("the run story is one validation run per attempt", () => {
  // Where a validation run can settle in the scenario's state.
  const judged: ValidationScenario[] = [
    "passed",
    "partial",
    "inconclusive",
    "failed",
    "unreported",
    "running",
    "cancelled",
  ];
  const repeat = (scenario: ValidationScenario): ValidationStory => ({ scenario, attempt: "repeat" });

  it.each(judged)("%s is judged by a validation run over a dev run that never was", (scenario) => {
    const runs = validationRuns({ scenario }).runs ?? [];
    expect(runs).toHaveLength(2);
    const [newest, dev] = runs;
    expect(newest?.kind).toBe("validation");
    expect(newest?.origin).toBe("revalidate");
    // A validation run builds nothing, so its cycles are all judgements.
    for (const c of newest?.cycles ?? []) expect(c.kind).toBe("validation");
    // The dev run delivered and settled with no verdict of its own.
    expect(dev?.kind).toBe("dev");
    expect(dev?.validation.verdict).toBeUndefined();
    expect(dev?.cycles.some((c) => c.kind === "validation")).toBe(false);
    expect(String(newest?.createdAt) > String(dev?.createdAt)).toBe(true);
  });

  // The only scenarios where a run holds two judgings: `unreported` is the one
  // verdict the run remedies itself, by dispatching once more.
  it("dispatches again only after an unreported attempt", () => {
    for (const scenario of judged) {
      const [newest] = validationRuns({ scenario }).runs ?? [];
      expect(newest?.cycles.length).toBe(scenario === "unreported" ? 2 : 1);
    }
  });

  it.each(judged)("%s on a repeat stacks dev, failed attempt, repair, then the attempt", (scenario) => {
    const runs = validationRuns(repeat(scenario)).runs ?? [];
    expect(runs.map((r) => r.kind)).toEqual(["validation", "task", "validation", "dev"]);
    expect(runs[2]?.validation.verdict).toBe("failed");
    // Newest first, and the dates agree with the order.
    const created = runs.map((r) => String(r.createdAt));
    expect([...created].sort().reverse()).toEqual(created);
    // The state is still the scenario's — it is the newest attempt's to set.
    expect(validationDetail(repeat(scenario)).state).toBe(scenario);
  });

  // The console keys sections and the one-open rule on the cycle id, so two
  // runs sharing one would open two attempts at once.
  it.each(judged)("%s never reuses a cycle id across its runs", (scenario) => {
    const ids = (validationRuns(repeat(scenario)).runs ?? []).flatMap((r) =>
      r.cycles.map((c) => c.id),
    );
    expect(new Set(ids).size).toBe(ids.length);
  });

  // Each attempt reads its report at ITS commit. The first attempt failed, the
  // repeat reached the scenario's verdict, and a reader opening both must see
  // two different reports — or the history is decoration.
  it("reads the failed first attempt at its own commit, not the branch tip", () => {
    const story = repeat("passed");
    const newest = validationSnapshot(story, false, "cycle-4");
    const first = validationSnapshot(story, false, "cycle-2");
    expect(newest.report).toBe(validationSnapshot(story).report);
    expect(first.report).toBeDefined();
    expect(first.report).not.toBe(newest.report);
    expect(first.report).toBe(validationSnapshot({ scenario: "failed" }).report);
  });

  // The ledger row dates the version's LATEST attempt, which is the repeat's —
  // not the first's, which happens to be listed after it.
  it("dates the ledger row by the newest attempt", () => {
    const row = validationLedger(repeat("passed")).validations.find((v) => v.tag === "v1");
    expect(row?.endedAt).toBe("2026-07-10T10:40:00Z");
  });

  // An attempt that committed nothing leaves the first attempt's report at the
  // tip, which is what the Spec view then shows.
  it("keeps the failed report at the tip while the repeat is unsettled", () => {
    for (const scenario of ["running", "cancelled"] as const) {
      const report = validationFiles(repeat(scenario)).find((f) => f.path === "tests/acceptance/report.json");
      expect(report?.content).toBe(validationSnapshot({ scenario: "failed" }).report);
    }
    // A first attempt in flight has the oracle and nothing else.
    expect(validationFiles({ scenario: "running" }).some((f) => f.path === "tests/acceptance/report.json")).toBe(false);
  });

  // The repeat reads at its OWN commit, and an attempt in flight or stopped has
  // committed nothing. The first attempt's failed report still stands at the
  // tip — that is the Spec view's answer (above), and not this one's.
  it("gives an unsettled repeat attempt no report of its own", () => {
    for (const scenario of ["running", "cancelled"] as const) {
      const snap = validationSnapshot(repeat(scenario), false, "cycle-4");
      expect(snap.report).toBeUndefined();
      expect(snap.commit).toBe("");
    }
  });

  // The dev loop's own shapes and a version the trigger refuses: none is a state
  // a validation run can be in, so the key is ignored rather than honoured with
  // a run the platform could never produce.
  it("ignores the attempt key where no validation run has a shape", () => {
    for (const scenario of ["none", "skipped", "awaiting-fix"] as const) {
      expect(validationRuns(repeat(scenario)).runs).toEqual(validationRuns({ scenario }).runs);
    }
  });

  // Mid-repair is a live TASK run over a failed attempt, not a dev run with a
  // verdict on it — the shape the platform has since validation became its own
  // run, whatever its derivation currently calls it.
  it("shapes awaiting-fix as a live repair over a failed attempt", () => {
    const runs = validationRuns({ scenario: "awaiting-fix" }).runs ?? [];
    expect(runs.map((r) => [r.kind, r.state])).toEqual([
      ["task", "running"],
      ["validation", "failed"],
      ["dev", "succeeded"],
    ]);
  });
});
