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
 * The task-generation driver (#356): the one-shot Plan turn on the Turn
 * socket, mirroring the playground's `tasksCommand` component-for-component
 * (issues folded via `FsIssueStore` only when the turn completed) — but
 * through the eval's tracing capture.
 * No sim user: the section is one-shot by construction.
 */

import { openSession } from "@aep/playground/src/engine/session.js";
import { runPlanTurn } from "@aep/playground/src/engine/plan-turn.js";
import { tasksGate } from "@aep/playground/src/engine/gates.js";
import { FsIssueStore, type FoldOutcome, type Issue } from "@aep/playground/src/ports/issue-store.js";
import { saveProjectState } from "@aep/playground/src/state/project.js";
import type { PlanScope } from "@aep/agent-stream";
import { collectPart, newTurnRecord, reportTurnTrace, type TurnRecord } from "../tracing.js";

export interface TaskPlanRunResult {
  section: "tasks";
  records: TurnRecord[];
  fold?: FoldOutcome;
  issues: Issue[];
  error?: string;
}

/** `scope` is the milestone scope the platform would compute for the version (B3). */
export async function runTaskPlanSection(projectDir: string, scope?: PlanScope): Promise<TaskPlanRunResult> {
  const gate = tasksGate(projectDir);
  if (!gate.ok) {
    return { section: "tasks", records: [], issues: [], error: `gate: ${gate.reason ?? "blocked"}` };
  }

  const session = await openSession(projectDir, {});
  try {
    const store = new FsIssueStore(projectDir, session.state.slug);
    const rec = newTurnRecord("tasks", 1, "task-plan (one-shot)");
    const start = Date.now();
    // A Plan turn on the Turn socket, as aep-api starts one in production.
    // TODO(Task 50, AGT-22/AGT-13): pass `scope` on the Turn socket request once
    // runPlanTurn and the socket's PlanScope contract take main's shape.
    const result = await runPlanTurn(session, store.planContext(), {
      onPart: (part) => collectPart(rec, part),
    });
    rec.ms = Date.now() - start;
    reportTurnTrace(rec, start);

    const fold = store.fold(
      result,
      store.safeAllocator(
        () => session.state.nextIssueNumber,
        (advancedTo) => {
          session.state.nextIssueNumber = advancedTo;
        },
      ),
    );
    saveProjectState(projectDir, session.state);

    return {
      section: "tasks",
      records: [rec],
      fold,
      issues: store.list(),
      ...(result.error ? { error: result.error } : {}),
    };
  } finally {
    await session.close();
  }
}
