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

// The one genuinely end-to-end test in this package: it runs the real
// `bin/agent-eval.ts` as a child process against the real, locally
// installed promptfoo binary — no fake spawn. It exists because
// `process.exit(verdict.passed ? 0 : 1)` is a mutation of `bin/agent-eval.ts`
// itself, which no unit test importing `src/cli.ts` can ever execute; only a
// real run of the actual file can pin its exit code.
//
// No network call happens and no key is spent: the `--app` it points at
// holds no built agent, so the provider's boot fails before any model call
// and promptfoo records a per-scenario error rather than reaching a grader.
// That is also the property this test now pins twice over — a harness
// failure must still be reported and still exit 0.

import { describe, expect, it } from "vitest";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync, readFileSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const PACKAGE_ROOT = fileURLToPath(new URL("..", import.meta.url));
const TSX_BIN = join(PACKAGE_ROOT, "node_modules", ".bin", "tsx");
const BIN_ENTRY = join(PACKAGE_ROOT, "bin", "agent-eval.ts");

const SCENARIOS = {
  version: 1,
  component: "trip-agent",
  scenarios: [
    {
      id: "SC-001",
      brief: { goal: "Book a hotel.", facts: {}, withholds: [] },
      rubric: { mustCover: [{ id: "MC-1", must: "asks for dates", weight: 1 }], mustNot: [] },
    },
  ],
};

describe("bin/agent-eval.ts (real subprocess, real local promptfoo)", () => {
  it("exits 0 for a failing/errored verdict — pins mutation #4 at the actual entry point", () => {
    const dir = mkdtempSync(join(tmpdir(), "agent-eval-bin-e2e-"));
    try {
      const appDir = join(dir, "app");
      const outDir = join(dir, "out");
      mkdirSync(appDir, { recursive: true });
      writeFileSync(join(dir, "scenarios.json"), JSON.stringify(SCENARIOS));

      // A real agent document, at the location the convention fixes. This
      // one declares no tools, so nothing is stubbed and nothing is served.
      const afmPath = join(dir, "specs", "design", "components", "trip-agent", "agent.afm.md");
      mkdirSync(join(dir, "specs", "design", "components", "trip-agent"), { recursive: true });
      writeFileSync(afmPath, '---\nname: "trip-agent"\n---\n\n# Role\nBook hotels.\n');

      const result = spawnSync(
        TSX_BIN,
        [
          BIN_ENTRY,
          "--scenarios", join(dir, "scenarios.json"),
          "--app", appDir,
          "--out", outDir,
          "--afm", afmPath,
        ],
        {
          encoding: "utf8",
          timeout: 60_000,
          // An EXPLICIT environment, so "this test cannot spend the org's
          // key" is structural rather than a property of the fixture. The
          // CLI forwards ANTHROPIC_API_KEY to promptfoo by design; inheriting
          // this developer's environment would hand a real key to a real
          // promptfoo process and rely on the run erroring early to stay
          // free. PATH and HOME are what node and tsx need, and no more.
          env: {
            PATH: process.env.PATH ?? "",
            HOME: process.env.HOME ?? "",
          },
        },
      );

      expect(result.error).toBeUndefined();
      expect(result.status).toBe(0);

      const report = readFileSync(join(outDir, "report.md"), "utf8");
      // Genuinely graded (not a run-failure report): the real promptfoo run
      // produced a real, scored verdict, and it is that verdict's failing
      // score that must still exit 0.
      expect(report).toMatch(/Score: 0\.00/);
      expect(report).not.toMatch(/run itself failed/i);

      // And promptfoo really did reach OUR provider, which really did try to
      // boot the agent named by `--app`. Without this the test would pass
      // just as happily against a provider that was never wired in at all.
      const raw = readFileSync(join(outDir, "out.json"), "utf8");
      expect(raw).toContain("never became ready");
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  }, 60_000);
});
