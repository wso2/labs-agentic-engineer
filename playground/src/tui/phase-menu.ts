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

/**
 * The home screen (docs/design/playground.md §7): one line per phase with its
 * FS-derived status; a phase with an unmet gate shows why. Selecting a phase
 * returns the action to the CLI driver — this module renders, it never runs.
 */

import * as clack from "@clack/prompts";
import { designGate, tasksGate, wireGate } from "../engine/gates.js";
import { designStatus, listIssueSummaries, requirementsStatus } from "../state/status.js";

export type MenuAction = "requirements" | "design" | "tasks" | "code" | "wire" | "chat" | "check" | "undo" | "quit";

function ago(d: Date | undefined): string {
  if (!d) return "";
  const mins = Math.round((Date.now() - d.getTime()) / 60_000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.round(mins / 60);
  return hours < 48 ? `${hours}h ago` : `${Math.round(hours / 24)}d ago`;
}

function kb(bytes: number): string {
  return `${(bytes / 1024).toFixed(1)} KB`;
}

export async function phaseMenu(projectDir: string, slug: string, skillCount: number): Promise<MenuAction> {
  const req = requirementsStatus(projectDir);
  const design = designStatus(projectDir);
  const issues = listIssueSummaries(projectDir);
  const dGate = designGate(projectDir);
  const tGate = tasksGate(projectDir);

  const reqLabel = req.present
    ? `1 Requirements   ✓ prd.md (${kb(req.sizeBytes)}, edited ${ago(req.modifiedAt)})`
    : "1 Requirements   — not generated yet";
  const designLabel = !dGate.ok
    ? `2 Design         ✗ blocked: ${dGate.reason}`
    : design.components.length > 0
      ? `2 Design         ✓ ${design.components.length} component(s)${design.skillsPinned.length ? ` · skillsPinned: ${design.skillsPinned.join(", ")}` : ""}`
      : "2 Design         — not generated yet";
  const pending = issues.filter((i) => !i.resolved).length;
  const issueCounts = issues.length ? ` (${issues.length - pending} resolved, ${pending} pending)` : "";
  const tasksLabel = !tGate.ok
    ? `3 Tasks          ✗ blocked: ${tGate.reason}`
    : issues.length > 0
      ? `3 Tasks          ✓ ${issues.length} issue(s)${issueCounts}`
      : "3 Tasks          — not planned yet";
  const codeLabel =
    issues.length === 0
      ? "4 Code           — needs issues"
      : pending > 0
        ? `4 Code           run — ${pending} pending, one session →`
        : "4 Code           ✓ all issues look resolved";

  const wGate = wireGate(projectDir);
  const wireLabel = wGate.ok
    ? "5 Wire           run it locally — pick a role, click through it with real data"
    : `5 Wire           ✗ blocked: ${wGate.reason}`;

  const choice = await clack.select<MenuAction>({
    message: `AEP playground — ${slug} (${projectDir}) · skills: ${skillCount} (working tree)`,
    options: [
      { value: "requirements", label: reqLabel },
      { value: "design", label: designLabel },
      { value: "tasks", label: tasksLabel },
      { value: "code", label: codeLabel },
      { value: "wire", label: wireLabel },
      { value: "chat", label: "chat — free turn in the spec conversation" },
      { value: "check", label: "check — validate design artifacts" },
      { value: "undo", label: "undo — restore the last pre-coding snapshot" },
      { value: "quit", label: "quit" },
    ],
  });
  return clack.isCancel(choice) ? "quit" : choice;
}
