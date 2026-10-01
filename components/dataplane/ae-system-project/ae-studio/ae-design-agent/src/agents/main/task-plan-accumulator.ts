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
 * TaskPlan — the plan-turn accumulator (the FileBundle analogue). Constructed per
 * turn from the read-only snapshot: it derives the KNOWN COMPONENTS and parses the
 * EXISTING Tasks (`tasks/<issueNumber>.md`), then validates each `planTask` /
 * `updateTask` against that context and tracks the resulting plan in order.
 *
 * Like the file tools, validation runs SERVER-SIDE inside the agent loop so a
 * rejection (unknown component / unknown ref / duplicate title / dependency
 * cycle) returns a self-correction payload the model acts on in the same turn
 * (the `design.json` gate pattern). The service NEVER touches GitHub — a
 * successful op only records intent + echoes the normalized operation; the BFF
 * plan tap performs the actual issue writes off the stream.
 *
 * Pure, no I/O, fully testable.
 */

import {
  parseKnownComponents,
  parseTaskContextFile,
  TASK_CONTEXT_FILE_RE,
  type PlanTaskInput,
  type UpdateTaskInput,
  type PlanTaskResult,
  type UpdateTaskResult,
  type TaskToolErr,
  type TaskContextFile,
  type TaskOrigin,
  type TaskRef,
} from "@aep/agent-stream";

/** A Task planned earlier this turn (title/dependsOn are mutable via updateTask). */
export interface PlannedTask {
  component: string;
  title: string;
  dependsOn: string[];
  origin: TaskOrigin;
  rationale: string;
  body?: string;
}

/** The patchable fields of an existing Task, overlaying its rendered values. */
interface ExistingPatch {
  title?: string;
  dependsOn?: string[];
}

const norm = (title: string): string => title.trim().toLowerCase();

export class TaskPlan {
  private readonly known: Set<string>;
  private readonly existing: Map<number, TaskContextFile>;
  private readonly existingPatch = new Map<number, ExistingPatch>();
  private readonly planned: PlannedTask[] = [];

  constructor(files: Record<string, string>) {
    this.known = new Set(parseKnownComponents(files));
    this.existing = new Map();
    for (const [path, content] of Object.entries(files)) {
      if (!TASK_CONTEXT_FILE_RE.test(path)) continue;
      const task = parseTaskContextFile(path, content);
      if (task) this.existing.set(task.issueNumber, task); // malformed → skipped (not addressable)
    }
  }

  // -- Reads (for the eval fold + tests) ----------------------------------

  knownComponents(): string[] {
    return [...this.known].sort();
  }

  plannedTasks(): readonly PlannedTask[] {
    return this.planned;
  }

  existingIssueNumbers(): number[] {
    return [...this.existing.keys()].sort((a, b) => a - b);
  }

  // -- Ops ----------------------------------------------------------------

  planTask(input: PlanTaskInput): PlanTaskResult {
    const op = "plan" as const;

    if (!this.known.has(input.component)) {
      return this.unknownComponent(op, `unknown component "${input.component}".`);
    }
    const unknownDeps = input.dependsOn.filter((d) => !this.known.has(d));
    if (unknownDeps.length > 0) {
      return this.unknownComponent(op, `dependsOn names unknown component(s): ${unknownDeps.join(", ")}.`);
    }
    const titleClash = this.titleTaken(input.title);
    if (titleClash) return this.duplicateTitle(op, input.title);

    const cycle = this.cycleThroughComponent(input.component, input.dependsOn);
    if (cycle) return this.dependencyCycle(op, cycle);

    const origin: TaskOrigin = input.origin ?? "spec-plan";
    this.planned.push({
      component: input.component,
      title: input.title,
      dependsOn: [...input.dependsOn],
      origin,
      rationale: input.rationale,
    });
    return { ok: true, op, component: input.component, title: input.title, dependsOn: [...input.dependsOn], origin, rationale: input.rationale };
  }

  updateTask(input: UpdateTaskInput): UpdateTaskResult {
    const op = "update" as const;
    const target = this.resolve(input.ref);
    if (!target) return this.unknownRef(op, input.ref);

    const set = input.set;
    if (set.title !== undefined && this.titleTaken(set.title, target.key)) {
      return this.duplicateTitle(op, set.title);
    }
    if (set.dependsOn !== undefined) {
      const unknownDeps = set.dependsOn.filter((d) => !this.known.has(d));
      if (unknownDeps.length > 0) {
        return this.unknownComponent(op, `dependsOn names unknown component(s): ${unknownDeps.join(", ")}.`);
      }
      const cycle = this.cycleThroughComponent(target.component, set.dependsOn);
      if (cycle) return this.dependencyCycle(op, cycle);
    }

    const resolvedRef = this.commitPatch(target, set);
    return { ok: true, op, ref: resolvedRef, set };
  }

  // -- Resolution + patching ----------------------------------------------

  private resolve(ref: TaskRef): { key: string; component: string; resolvedRef: TaskRef } | undefined {
    if ("issueNumber" in ref) {
      const existing = this.existing.get(ref.issueNumber);
      if (!existing) return undefined; // issueNumber refs pre-existing Tasks only
      return { key: `existing:${ref.issueNumber}`, component: existing.component, resolvedRef: { issueNumber: ref.issueNumber } };
    }
    // A title refs a Task planned earlier THIS turn (never an existing one).
    const idx = this.planned.findIndex((p) => norm(p.title) === norm(ref.title));
    if (idx < 0) return undefined;
    const p = this.planned[idx]!;
    return { key: `planned:${idx}`, component: p.component, resolvedRef: { title: p.title } };
  }

  private commitPatch(
    target: { key: string; resolvedRef: TaskRef },
    set: UpdateTaskInput["set"],
  ): TaskRef {
    if (target.key.startsWith("planned:")) {
      const idx = Number(target.key.slice("planned:".length));
      const p = this.planned[idx]!;
      if (set.title !== undefined) p.title = set.title;
      if (set.dependsOn !== undefined) p.dependsOn = [...set.dependsOn];
      if (set.rationale !== undefined) p.rationale = set.rationale;
      if (set.body !== undefined) p.body = set.body;
      return target.resolvedRef; // { title } — the pre-rename identity the BFF holds
    }
    // Existing Task: track title/dependsOn for later validation; body/rationale are
    // applied to the issue by the BFF and need no server-side state.
    const issueNumber = Number(target.key.slice("existing:".length));
    const patch = this.existingPatch.get(issueNumber) ?? {};
    if (set.title !== undefined) patch.title = set.title;
    if (set.dependsOn !== undefined) patch.dependsOn = [...set.dependsOn];
    this.existingPatch.set(issueNumber, patch);
    return target.resolvedRef; // { issueNumber }
  }

  // -- The unified current view (existing patched + planned) ---------------

  /** The current title of every task identity, paired with its identity key. */
  private currentTitles(): { key: string; title: string }[] {
    const out: { key: string; title: string }[] = [];
    for (const [n, base] of this.existing) {
      out.push({ key: `existing:${n}`, title: this.existingPatch.get(n)?.title ?? base.title });
    }
    this.planned.forEach((p, i) => out.push({ key: `planned:${i}`, title: p.title }));
    return out;
  }

  /** True when `title` collides (case-insensitive, trimmed) with any OTHER identity. */
  private titleTaken(title: string, exceptKey?: string): boolean {
    const n = norm(title);
    return this.currentTitles().some((t) => t.key !== exceptKey && norm(t.title) === n);
  }

  private takenTitles(): string[] {
    return this.currentTitles().map((t) => t.title);
  }

  // -- Cycle detection over the component graph ----------------------------

  /**
   * The current component dependency graph: `component → dependsOn components`.
   * Each component's edges come from its LATEST task — for existing renderings
   * that is the highest issueNumber (§5's "latest task for that component"
   * gating), and a planned task for the same component overrides the existing one
   * (later plan order wins). This kills the stale-edge false positive where an old
   * rendering's dependsOn would fabricate a cycle the current plan doesn't have.
   */
  private effectiveGraph(): Map<string, string[]> {
    const latestByComponent = new Map<string, number>();
    for (const [n, base] of this.existing) {
      const prev = latestByComponent.get(base.component);
      if (prev === undefined || n > prev) latestByComponent.set(base.component, n);
    }
    const graph = new Map<string, string[]>();
    for (const [component, n] of latestByComponent) {
      const base = this.existing.get(n)!;
      graph.set(component, this.existingPatch.get(n)?.dependsOn ?? base.dependsOn);
    }
    for (const p of this.planned) graph.set(p.component, p.dependsOn); // planned overrides existing; later planned wins
    return graph;
  }

  /**
   * Cycle check SCOPED to the op under change: with `component`'s edges set to the
   * proposed `deps`, is `component` reachable from itself? Only then does the op's
   * own edge participate in a cycle. A pre-existing cycle among OTHER (untouched)
   * components is NOT the planner's concern — the reconciliation sweep flags it
   * `aep:attention` (design §5) — so it never blocks this op. Returns the cycle
   * path THROUGH `component` (e.g. `[a, b, a]`), or null.
   */
  private cycleThroughComponent(component: string, deps: string[]): string[] | null {
    const graph = this.effectiveGraph();
    graph.set(component, [...deps]);

    const visited = new Set<string>([component]);
    const path: string[] = [];
    const dfs = (node: string): string[] | null => {
      path.push(node);
      for (const next of graph.get(node) ?? []) {
        if (next === component) return [...path, component]; // closed a loop back to the op's component
        if (!visited.has(next)) {
          visited.add(next);
          const found = dfs(next);
          if (found) return found;
        }
      }
      path.pop();
      return null;
    };
    return dfs(component);
  }

  // -- Error builders (self-correction payloads) ---------------------------

  private unknownComponent(op: TaskToolErr["op"], message: string): TaskToolErr {
    return { ok: false, op, code: "UNKNOWN_COMPONENT", message: `${message} Known components: ${this.knownComponents().join(", ") || "(none)"}.`, knownComponents: this.knownComponents() };
  }

  private unknownRef(op: TaskToolErr["op"], ref: TaskRef): TaskToolErr {
    const addressable = { issueNumbers: this.existingIssueNumbers(), plannedTitles: this.planned.map((p) => p.title) };
    const what = "issueNumber" in ref ? `issueNumber ${ref.issueNumber}` : `title "${ref.title}"`;
    return {
      ok: false,
      op,
      code: "UNKNOWN_REF",
      message: `cannot resolve ${what}. Reference an existing Task by issueNumber (${addressable.issueNumbers.join(", ") || "none"}) or a Task planned this turn by title (${addressable.plannedTitles.map((t) => `"${t}"`).join(", ") || "none"}).`,
      addressable,
    };
  }

  private duplicateTitle(op: TaskToolErr["op"], title: string): TaskToolErr {
    return { ok: false, op, code: "DUPLICATE_TITLE", message: `a Task titled "${title}" already exists — choose a distinct title.`, takenTitles: this.takenTitles() };
  }

  private dependencyCycle(op: TaskToolErr["op"], cyclePath: string[]): TaskToolErr {
    return { ok: false, op, code: "DEPENDENCY_CYCLE", message: `dependsOn would create a cycle: ${cyclePath.join(" → ")}. Break the cycle before planning.`, cyclePath };
  }
}
