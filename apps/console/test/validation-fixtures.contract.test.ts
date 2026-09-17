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
import { validationFiles, VALIDATION_SCENARIOS } from "../src/mocks/fixtures/validation";

const REPO = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const CHECKER = join(REPO, "skills/acceptance-run/scripts/check-report.mjs");

let dir: string | undefined;
afterEach(() => {
  if (dir) rmSync(dir, { recursive: true, force: true });
  dir = undefined;
});

function layOut(files: { path: string; content: string }[]): string {
  const root = mkdtempSync(join(tmpdir(), "aep-mock-"));
  mkdirSync(join(root, "specs/acceptance"), { recursive: true });
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
    (s) => validationFiles(s).some((f) => f.path === "tests/acceptance/report.json"),
  );

  it("covers the verdicts that ship a report", () => {
    expect([...reported].sort()).toEqual(
      ["awaiting-fix", "failed", "inconclusive", "partial", "passed"],
    );
  });

  it.each(reported)("%s passes check-report.mjs", (scenario) => {
    dir = layOut(validationFiles(scenario));
    // Throws on a nonzero exit, and the checker exits 2 on a contract breach with
    // every one of them printed — so a failure here names what is wrong.
    expect(() => execFileSync("node", [CHECKER, dir as string], { encoding: "utf8" })).not.toThrow();
  });

  // Drift is the one state where the two files are SUPPOSED to disagree: the
  // feature files carry a scenario the pinned report predates. The checker is
  // right to fail it, and it must fail for that reason alone — a drifted fixture
  // that also broke some other rule would be indistinguishable.
  it("drifts by exactly one uncovered scenario, and nothing else", () => {
    dir = layOut(validationFiles("partial", "first", true));
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
