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
 * `FsIssueStore` — tasks as files (docs/design/playground.md §3/§5/§6):
 * `issues/<n>.md` in the production plan-context format
 * (`taskplan/context_file.go` ⇄ `parseTaskContextFile`), folded from the Plan
 * turn's task operations (the Turn socket's `task-op` lines: OK `planTask` /
 * `updateTask` results) with `plan_tap.go` semantics:
 *
 *  - existing issues preload before the turn (dedupe keys + `updateTask`
 *    {issueNumber} fencing, mirroring the frozen preloaded-context fence);
 *  - `planTask` ok → allocate the next number, compute the production dedupe
 *    key (lineage constant "local"), skip duplicates by key or normalized
 *    title, write the file;
 *  - `updateTask` ok → resolve by title (a Task planned THIS run) or by
 *    preloaded issueNumber, patch the file;
 *  - nothing is written unless the turn's result was `completed` (a failed
 *    or severed turn is unambiguously "do not commit" — D14).
 *
 * `renderTaskContextFile` mirrors the Go `TaskContextFile.Render` byte-for-byte
 * (field order, quoting) so files round-trip through `parseTaskContextFile`.
 */

import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { parseTaskContextFile, type PlanContextFile, type TaskContextFile } from "@aep/agent-stream";

// --- Rendering (mirror of taskplan/context_file.go) --------------------------

export interface IssueFileData {
  issueNumber: number;
  component: string;
  title: string;
  dependsOn: string[];
  origin?: string;
  key?: string;
  body?: string;
}

/** Mirrors Go yamlQuote: newlines flatten to spaces; backslash/quote escaped. */
function yamlQuote(s: string): string {
  const flat = s.replaceAll("\r\n", " ").replaceAll("\n", " ");
  return `"${flat.replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"`;
}

/** Mirrors Go yamlFlowList: `[]` or `["a", "b"]`. */
function yamlFlowList(items: string[]): string {
  return items.length === 0 ? "[]" : `[${items.map(yamlQuote).join(", ")}]`;
}

/**
 * Mirror of Go `TaskContextFile.Render` (field order fixed: issueNumber,
 * component, title, dependsOn, origin, then optionals), extended with the
 * playground's `key` line (extra frontmatter keys are ignored by
 * `parseTaskContextFile` — tolerated by design). No status field: an issue's
 * done-ness is never cached here — it is read fresh from the project tree
 * each run, not from a flag a prior run wrote.
 */
export function renderTaskContextFile(f: IssueFileData): string {
  let out = "---\n";
  out += `issueNumber: ${f.issueNumber}\n`;
  out += `component: ${yamlQuote(f.component)}\n`;
  out += `title: ${yamlQuote(f.title)}\n`;
  out += `dependsOn: ${yamlFlowList(f.dependsOn)}\n`;
  out += `origin: ${yamlQuote(f.origin ?? "spec-plan")}\n`;
  if (f.key) out += `key: ${yamlQuote(f.key)}\n`;
  out += "---\n";
  const body = (f.body ?? "").trim();
  if (body !== "") out += `\n${body}\n`;
  return out;
}

// --- Dedupe key (mirror of taskmeta.Key / taskmeta.TitleSlug) ----------------

/** The playground's lineage constant — no spec/design tags exist locally (§10). */
export const LOCAL_LINEAGE = "local";

function sha256HexFull(text: string): string {
  return createHash("sha256").update(text, "utf8").digest("hex");
}

/** Mirrors taskmeta.TitleSlug: lowercase, non-[a-z0-9] runs → "-", trimmed. */
export function titleSlug(title: string): string {
  return title.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
}

/** Mirrors taskmeta.Key: hex(sha256(project\nlineage\ntarget\ntitleSlug))[:12]. */
export function taskKey(project: string, lineageTag: string, target: string, title: string): string {
  let titleComponent = titleSlug(title);
  if (titleComponent === "") titleComponent = sha256HexFull(title.trim()).slice(0, 12);
  return sha256HexFull([project, lineageTag, target, titleComponent].join("\n")).slice(0, 12);
}

// --- The store ----------------------------------------------------------------

export interface Issue extends TaskContextFile {
  /** Repo-relative file, e.g. `issues/3.md`. */
  file: string;
  /** The dedupe key, when the frontmatter carries one. */
  key?: string;
}

export interface FoldOutcome {
  created: Issue[];
  updated: Issue[];
  /** planTask ops skipped as duplicates (by key or normalized title). */
  skippedDuplicates: string[];
  /** updateTask renames skipped because the new title was already taken (other set fields still applied). */
  skippedRenames: string[];
}

const normTitle = (t: string): string => t.trim().toLowerCase();

interface PlanTaskOkOutput {
  ok: true;
  op: "plan";
  component: string;
  title: string;
  dependsOn: string[];
  origin: string;
  rationale: string;
}

interface UpdateTaskOkOutput {
  ok: true;
  op: "update";
  ref: { issueNumber: number } | { title: string };
  set: { title?: string; dependsOn?: string[]; rationale?: string; body?: string };
}

/** One task operation of a Plan turn (a Turn socket `task-op` line). */
export interface TaskOperation {
  op: string;
  output: unknown;
}

/** A finished Plan turn, as the fold takes it. */
export interface PlanTurnOps {
  /** The turn's result was `completed`. */
  completed: boolean;
  taskOps: readonly TaskOperation[];
}

function asOkOutput(taskOp: TaskOperation): PlanTaskOkOutput | UpdateTaskOkOutput | null {
  const out = taskOp.output as { ok?: unknown; op?: unknown } | undefined;
  if (!out || out.ok !== true || out.op !== taskOp.op) return null;
  if (out.op === "plan") return out as PlanTaskOkOutput;
  if (out.op === "update") return out as UpdateTaskOkOutput;
  return null;
}

export class FsIssueStore {
  private readonly dir: string;

  constructor(
    private readonly projectDir: string,
    /** The dedupe key's `project` dimension (the playground slug). */
    private readonly projectKey: string,
  ) {
    this.dir = join(projectDir, "issues");
  }

  /** Every parseable `issues/<n>.md`, sorted by number. */
  list(): Issue[] {
    if (!existsSync(this.dir)) return [];
    const out: Issue[] = [];
    for (const e of readdirSync(this.dir, { withFileTypes: true })) {
      const m = /^(\d+)\.md$/.exec(e.name);
      if (!e.isFile() || !m) continue;
      const raw = readFileSync(join(this.dir, e.name), "utf8");
      // parseTaskContextFile keys off the production tasks/<n>.md path shape.
      const parsed = parseTaskContextFile(`tasks/${m[1]}.md`, raw);
      if (!parsed) continue; // malformed — not addressable, never crashes (production posture)
      const key = /^key: *"?([0-9a-f]{12})"?$/m.exec(raw)?.[1];
      out.push({ ...parsed, file: `issues/${e.name}`, ...(key ? { key } : {}) });
    }
    return out.sort((a, b) => a.issueNumber - b.issueNumber);
  }

  /**
   * The Plan turn's task context: a straight copy of each issue file under
   * its production `tasks/<n>.md` name (§6 — the file IS the context render),
   * sorted by path, as aep-api sends it (`planContextFor`).
   */
  planContext(): PlanContextFile[] {
    if (!existsSync(this.dir)) return [];
    const files: PlanContextFile[] = [];
    for (const e of readdirSync(this.dir, { withFileTypes: true })) {
      const m = /^(\d+)\.md$/.exec(e.name);
      if (!e.isFile() || !m) continue;
      files.push({ path: `tasks/${m[1]}.md`, body: readFileSync(join(this.dir, e.name), "utf8") });
    }
    return files.sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
  }

  /**
   * Fold one finished Plan turn; a turn that did not complete writes nothing. `nextIssueNumber` is read+advanced via
   * the callbacks so the caller's project state stays the single counter owner.
   */
  fold(
    turn: PlanTurnOps,
    allocateIssueNumber: () => number,
  ): FoldOutcome {
    const outcome: FoldOutcome = { created: [], updated: [], skippedDuplicates: [], skippedRenames: [] };
    if (!turn.completed) return outcome; // not completed → do not commit (D14)

    // Frozen preload — the anti-hallucination fence plan_tap enforces.
    const preloaded = new Map<number, Issue>();
    const existingKeys = new Set<string>();
    const takenTitles = new Set<string>();
    for (const issue of this.list()) {
      preloaded.set(issue.issueNumber, issue);
      if (issue.key) existingKeys.add(issue.key);
      takenTitles.add(normTitle(issue.title));
    }
    const createdByTitle = new Map<string, Issue>();

    for (const taskOp of turn.taskOps) {
      const op = asOkOutput(taskOp);
      if (!op) continue;

      if (op.op === "plan") {
        const key = taskKey(this.projectKey, LOCAL_LINEAGE, op.component, op.title);
        if (existingKeys.has(key) || takenTitles.has(normTitle(op.title))) {
          outcome.skippedDuplicates.push(op.title);
          continue;
        }
        const issueNumber = allocateIssueNumber();
        const data: IssueFileData = {
          issueNumber,
          component: op.component,
          title: op.title,
          dependsOn: op.dependsOn,
          origin: op.origin,
          key,
          body: `> **Rationale:** ${op.rationale}`,
        };
        this.write(data);
        const issue: Issue = {
          issueNumber,
          component: data.component,
          title: data.title,
          dependsOn: data.dependsOn,
          origin: "spec-plan",
          body: data.body ?? "",
          file: `issues/${issueNumber}.md`,
          key,
        };
        existingKeys.add(key);
        takenTitles.add(normTitle(op.title));
        createdByTitle.set(normTitle(op.title), issue);
        outcome.created.push(issue);
        continue;
      }

      // updateTask: title → a Task created THIS run; issueNumber → preloaded only.
      let target: Issue | undefined;
      if ("title" in op.ref) target = createdByTitle.get(normTitle(op.ref.title));
      else target = preloaded.get(op.ref.issueNumber);
      if (!target) continue; // out-of-context ref — never written (the fence)

      if (op.set.title !== undefined) {
        const renamed = normTitle(op.set.title);
        if (renamed !== normTitle(target.title) && takenTitles.has(renamed)) {
          // Colliding rename: skip ONLY the rename (surfaced to the caller);
          // the op's dependsOn/body changes below must still land.
          outcome.skippedRenames.push(`#${target.issueNumber} "${target.title}" → "${op.set.title}"`);
        } else {
          takenTitles.delete(normTitle(target.title));
          if ("title" in op.ref) {
            createdByTitle.delete(normTitle(target.title));
            createdByTitle.set(renamed, target);
          }
          target.title = op.set.title;
          takenTitles.add(renamed);
        }
      }
      if (op.set.dependsOn !== undefined) target.dependsOn = [...op.set.dependsOn];
      if (op.set.body !== undefined) {
        const rationale = /^> \*\*Rationale:\*\*.*$/m.exec(target.body)?.[0];
        target.body = rationale ? `${rationale}\n\n${op.set.body}` : op.set.body;
      }
      this.write({
        issueNumber: target.issueNumber,
        component: target.component,
        title: target.title,
        dependsOn: target.dependsOn,
        origin: target.origin,
        ...(target.key ? { key: target.key } : {}),
        body: target.body,
      });
      if (!outcome.created.includes(target) && !outcome.updated.includes(target)) outcome.updated.push(target);
    }

    return outcome;
  }

  private write(data: IssueFileData): void {
    mkdirSync(this.dir, { recursive: true });
    writeFileSync(join(this.dir, `${data.issueNumber}.md`), renderTaskContextFile(data), "utf8");
  }

  /**
   * Wrap a raw counter allocator so it never hands out a number whose file
   * already exists — a project dir copied WITHOUT its `.aep-playground/`
   * state starts the counter at 1 while `issues/1.md` may exist; clobbering
   * a planner-authored issue is never acceptable. Advances the caller's
   * counter past the allocated number via `commit`.
   */
  safeAllocator(next: () => number, commit: (advancedTo: number) => void): () => number {
    return () => {
      let n = next();
      while (existsSync(join(this.dir, `${n}.md`))) n += 1;
      commit(n + 1);
      return n;
    };
  }
}
