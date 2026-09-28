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

import type { Verdict } from "./verdict.js";

/**
 * The report a human reads in a PR. It states the score, every failed rubric
 * line with the judge's own reason, and — the line that stops a reader
 * misunderstanding what happened — that a low score did NOT block the build.
 *
 * `failed` and `ungraded` are rendered as SEPARATE sections on purpose:
 * `failed` is a genuine rubric miss (something the agent's transcript did not
 * satisfy), while `ungraded` is a line the judge never returned a verdict for
 * at all — a grading gap, not evidence the agent did anything wrong. Folding
 * the two together would send a reader chasing an agent defect that was
 * actually a grader problem.
 */
export function renderReport(
  v: Verdict,
  opts: { component: string; promptChanged: boolean },
): string {
  const lines: string[] = [
    `# Agent evaluation — ${opts.component}`,
    "",
    `**Score: ${v.overall.toFixed(2)}** across ${v.scenarios.length} scenario(s).`,
    "",
    opts.promptChanged
      ? "The prompt was revised by evaluation. The revision ships with this build; a separate PR proposes the same change to `agent.afm.md`."
      : "The prompt was not changed — it scored well enough as authored.",
    "",
    "Evaluation is reported, not enforced: a low score does not fail the build.",
  ];

  // A 0.00 with an empty table reads as "every scenario failed" — it is
  // actually "nothing was graded at all", a materially different situation
  // (typically a promptfoo result set that came back empty) that deserves
  // its own sentence rather than being folded into the normal table.
  if (v.scenarios.length === 0) {
    lines.push("", "**No scenario was graded** — the result set was empty.");
    return lines.join("\n");
  }

  lines.push(
    "",
    "## Scenarios",
    "",
    "| Scenario | Score | Result |",
    "|---|---|---|",
    ...v.scenarios.map((s) => `| ${s.id} | ${s.score.toFixed(2)} | ${s.passed ? "met" : "below threshold"} |`),
  );

  const failures = v.scenarios.filter((s) => s.failed.length > 0);
  if (failures.length > 0) {
    lines.push(
      "",
      "## What fell short",
      "",
      "Rubric lines the agent's transcript genuinely missed.",
      "",
    );
    for (const s of failures) {
      lines.push(`**${s.id}**`);
      for (const f of s.failed) lines.push(`- \`${f.id}\` — ${f.reason}`);
      lines.push("");
    }
  }

  // A denied tool call is a security finding, not a rubric result — its own
  // heading, ahead of `failed`/`ungraded`, so a reader cannot mistake it for
  // either. `metadata.toolOverReach` reaching `out.json` was not enough on
  // its own: a signal nothing renders is not actually visible to anyone.
  const overReach = v.scenarios.filter((s) => s.toolOverReach.length > 0);
  if (overReach.length > 0) {
    lines.push(
      "",
      "## Tool over-reach",
      "",
      "The agent called an operation its allow-list does not include. This is " +
        "a security finding, not a rubric miss.",
      "",
    );
    for (const s of overReach) {
      lines.push(`**${s.id}**`);
      for (const entry of s.toolOverReach) {
        lines.push(`- \`${entry}\` — this agent is not permitted to call this operation.`);
      }
      lines.push("");
    }
  }

  const ungraded = v.scenarios.filter((s) => s.ungraded.length > 0);
  if (ungraded.length > 0) {
    lines.push(
      "",
      "## Ungraded",
      "",
      "The judge never returned a verdict for these rubric lines. This is a " +
        "grading gap, not an agent failure — do not treat it as a rubric miss.",
      "",
    );
    for (const s of ungraded) {
      lines.push(`**${s.id}**`);
      for (const u of s.ungraded) lines.push(`- \`${u.id}\` — ${u.reason}`);
      lines.push("");
    }
  }

  return lines.join("\n");
}

/**
 * The report written when promptfoo itself never produced a verdict — a
 * spawn failure, a non-zero exit, or output the CLI could not parse. This is
 * distinct from a low-scoring `Verdict`: there IS no verdict here, so
 * synthesizing a fake passing (or failing) one would misrepresent what
 * happened. The failure must read as a failure, never as a silent clean pass.
 */
export function renderRunFailureReport(opts: { component: string; error: string }): string {
  return [
    `# Agent evaluation — ${opts.component}`,
    "",
    "**The evaluation run itself failed** — promptfoo did not produce a verdict.",
    "",
    "This is reported, not enforced: it does not fail the build. But it is not " +
      "a pass either — no scenario was actually graded.",
    "",
    "## Error",
    "",
    "```",
    opts.error,
    "```",
  ].join("\n");
}
