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
 * `save`: turn a playground project into a committed case.
 *
 * A case is the PRE-CODE state of a project: `specs/` + `issues/` and nothing
 * else (ADR-0001). The work here is finding that state, which a project that has already been
 * coded no longer shows at its top level: the undo snapshots
 * (`playground/src/state/undo.ts`) are where it survives, and the OLDEST of
 * them is the one taken before the first coding run.
 *
 * Then the planner derives the checklist from the copied specs, and both are
 * written. Into a temporary sibling first, renamed into place at the end, so a
 * planner that fails leaves no half-saved case for the next sweep to load.
 */

import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { basename, join } from "node:path";
import { Document } from "yaml";
import { buildWirePlan, readWireSpecs } from "@aep/playground/src/engine/wire/plan.js";
import { listUndoSnapshots } from "@aep/playground/src/state/undo.js";
import { CaseSchema, ChecklistSchema, parseYaml, type CaseFile, type Checklist } from "./case.js";
import { PATHS, SAVE } from "./config.js";
import { readOAuthToken, sdkEnv } from "./credentials.js";
import { planChecklist, type Plan } from "./planner.js";

/** What `save` needs to know about one candidate tree — gathered by `readTreeFacts`, judged by `chooseSource`. */
export interface TreeFacts {
  dir: string;
  /** Non-dot top-level entries. */
  entries: string[];
  /** Issue files carrying an agent-written section (`SAVE.agentWrittenSections`). */
  agentWritten: string[];
}

export type SourceChoice = { ok: true; dir: string; snapshot: string } | { ok: false; reason: string };

/** A compiled rendering, which a case leaves out (`SAVE.renderedSuffixes`). */
export function isRendering(path: string): boolean {
  return SAVE.renderedSuffixes.some((suffix) => path.endsWith(suffix));
}

/**
 * Which tree is the pre-code state. Snapshots must also pass the issue check:
 * one taken before a second coding run carries the first run's `## Progress`. Pure.
 */
export function chooseSource(current: TreeFacts, snapshots: TreeFacts[]): SourceChoice {
  if (isPreCode(current)) return { ok: true, dir: current.dir, snapshot: "current" };
  const oldestFirst = [...snapshots].sort((a, b) => a.dir.localeCompare(b.dir));
  const snapshot = oldestFirst.find(isPreCode);
  if (snapshot) return { ok: true, dir: snapshot.dir, snapshot: lastSegment(snapshot.dir) };
  const why = isCaseShaped(current)
    ? `its issues carry agent-written sections (${current.agentWritten.join(", ")})`
    : `its top level is ${current.entries.join(", ") || "(empty)"}, not exactly specs/ + issues/`;
  return {
    ok: false,
    reason:
      `${current.dir} is not a pre-code project — ${why} — and none of its ` +
      `${String(snapshots.length)} undo snapshot(s) is either. Save from a project before its first coding run.`,
  };
}

function isCaseShaped(facts: TreeFacts): boolean {
  return [...facts.entries].sort().join(",") === [...SAVE.caseDirs].sort().join(",");
}

function isPreCode(facts: TreeFacts): boolean {
  return isCaseShaped(facts) && facts.agentWritten.length === 0;
}

function lastSegment(path: string): string {
  return path.split("/").filter(Boolean).pop() ?? path;
}

export function readTreeFacts(dir: string): TreeFacts {
  const entries = readdirSync(dir).filter((name) => !name.startsWith("."));
  const issuesDir = join(dir, "issues");
  const agentWritten = existsSync(issuesDir)
    ? readdirSync(issuesDir)
        .filter((name) => name.endsWith(".md"))
        .filter((name) => {
          const text = readFileSync(join(issuesDir, name), "utf8");
          return SAVE.agentWrittenSections.some((pattern) => pattern.test(text));
        })
        .map((name) => `issues/${name}`)
    : [];
  return { dir, entries, agentWritten };
}

/**
 * The role names `play wire --role` accepts for a project, from `wire`'s own
 * derivation, so the checklist and the session cannot disagree.
 */
export function caseRoles(dir: string): string[] {
  return buildWirePlan(readWireSpecs(dir, "case")).roles.map((role) => role.name);
}

/** True when any component's `design.json` is a web application — the only kind `wire` puts a browser on. */
export function hasWebapp(dir: string): boolean {
  return readWireSpecs(dir, "case").designs.some((design) => design.type === "web-application");
}

export interface SaveOptions {
  from: string;
  name: string;
  force: boolean;
  say: (line: string) => void;
}

export async function saveCase(opts: SaveOptions): Promise<string> {
  if (!SAVE.namePattern.test(opts.name)) {
    throw new Error(`case name "${opts.name}" must match ${String(SAVE.namePattern)} — it becomes part of a compose project name`);
  }
  const target = join(PATHS.casesDir, opts.name);
  if (existsSync(target) && !opts.force) throw new Error(`case ${opts.name} already exists — pass --force to replace it`);
  if (!existsSync(opts.from)) throw new Error(`${opts.from} does not exist`);

  const choice = chooseSource(
    readTreeFacts(opts.from),
    listUndoSnapshots(opts.from).map((dir) => readTreeFacts(dir)),
  );
  if (!choice.ok) throw new Error(choice.reason);
  opts.say(`  source: ${choice.snapshot === "current" ? opts.from : `undo snapshot ${choice.snapshot}`}`);

  if (!hasWebapp(choice.dir)) {
    throw new Error("no component in specs/design/components/*/design.json is a web-application — `wire` is the only target, so there is nothing to walk");
  }
  // Read before anything is written: a refused credential should cost nothing.
  const token = readOAuthToken(PATHS.envFile);

  const staging = join(PATHS.casesDir, `.saving-${opts.name}`);
  rmSync(staging, { recursive: true, force: true });
  mkdirSync(staging, { recursive: true });
  try {
    for (const name of SAVE.caseDirs) {
      cpSync(join(choice.dir, name), join(staging, name), { recursive: true, filter: (src) => !isRendering(src) });
    }
    const planned = await planInto(staging, opts.name, token, opts.say);
    const meta: CaseFile = CaseSchema.parse({
      name: opts.name,
      description: describe(choice.dir),
      source: { project: basename(opts.from), snapshot: choice.snapshot, savedAt: new Date().toISOString() },
      tags: [],
    });
    writeFileSync(join(staging, "case.yaml"), new Document(meta).toString());

    rmSync(target, { recursive: true, force: true });
    renameSync(staging, target);
    sayPlanned(planned, opts.say);
    return target;
  } catch (e) {
    rmSync(staging, { recursive: true, force: true });
    throw e;
  }
}

interface Planned {
  items: number;
  gaps: number;
  costUsd: number | null;
  transcriptFile: string;
}

/**
 * Plan a checklist for the case at `caseDir` and write `checklist.yaml` there.
 * Hand-added `extras` already in the file are kept: they are a person's, and
 * re-planning the derived items is no reason to lose them.
 */
async function planInto(caseDir: string, name: string, token: string, say: (line: string) => void): Promise<Planned> {
  const roles = caseRoles(caseDir);
  if (roles.length === 0) throw new Error("specs/design/security.json declares no roles — there is no one to walk the app as");
  say(`  roles: ${roles.join(", ")}`);

  const transcriptDir = join(PATHS.runsDir, "planner");
  mkdirSync(transcriptDir, { recursive: true });
  const transcriptFile = join(transcriptDir, `${name}-${new Date().toISOString().replace(/[:.]/g, "-")}.jsonl`);
  say("  planning the checklist (reads the specs; a few minutes)…");
  const plan = await planChecklist({ caseDir, roles, env: sdkEnv(process.env, token), transcriptFile });

  const file = join(caseDir, "checklist.yaml");
  const extras = existsSync(file) ? parseYaml(readFileSync(file, "utf8"), ChecklistSchema, file).extras : undefined;
  const checklist: Checklist = ChecklistSchema.parse({ items: plan.items, ...(extras ? { extras } : {}) });
  writeFileSync(file, renderChecklist(checklist, plan, roles));
  return { items: checklist.items.length, gaps: plan.gaps.length, costUsd: plan.costUsd, transcriptFile };
}

function sayPlanned(planned: Planned, say: (line: string) => void): void {
  const cost = planned.costUsd === null ? "unknown" : `$${planned.costUsd.toFixed(2)}`;
  say(`  ${String(planned.items)} items, ${String(planned.gaps)} gap(s); planner cost ${cost}`);
  say(`  planner transcript: ${planned.transcriptFile}`);
}

/**
 * Re-plan a saved case's checklist from its committed specs; other files
 * untouched, hand edits overwritten, extras kept.
 */
export async function replanCase(name: string, say: (line: string) => void): Promise<string> {
  const dir = join(PATHS.casesDir, name);
  if (!existsSync(join(dir, "case.yaml"))) throw new Error(`no case ${name} under ${PATHS.casesDir}`);
  const token = readOAuthToken(PATHS.envFile);
  sayPlanned(await planInto(dir, name, token, say), say);
  return join(dir, "checklist.yaml");
}

/** The project's idea, as the case's description — `specs/.agentic-engineer.toml`'s one line, when it has one. */
function describe(dir: string): string {
  const descriptor = join(dir, "specs", ".agentic-engineer.toml");
  if (!existsSync(descriptor)) return "";
  const match = /^\s*idea\s*=\s*"((?:[^"\\]|\\.)*)"/m.exec(readFileSync(descriptor, "utf8"));
  return match?.[1]?.replace(/\\"/g, '"') ?? "";
}

/**
 * `checklist.yaml`, with a header that says it may be edited and how, and
 * the planner's gaps as comments — information for the person reading the
 * list, never input to a score.
 */
export function renderChecklist(checklist: Checklist, plan: Pick<Plan, "gaps">, roles: string[]): string {
  const doc = new Document({ items: checklist.items, extras: checklist.extras });
  const header = [
    " Derived by the planner (save / replan) and frozen: every attempt walks this",
    " list, in this order, from an empty database. HAND-EDITABLE — fix, reorder,",
    " reweight or delete items freely; only an explicit replan re-derives it.",
    "",
    ` role: one of ${[...roles, "no role", "signed out"].join(", ")}`,
    " weight: default 1. extras.mustCover items are walked and scored like items;",
    " extras.mustNot entries ({id, description}) cap the band at review when violated.",
  ];
  if (plan.gaps.length > 0) {
    header.push("", " Planner gaps — what the walk cannot cover, and where the PRD and the wireframes disagree:");
    for (const gap of plan.gaps) header.push(`  - ${gap}`);
  }
  doc.commentBefore = header.join("\n");
  return doc.toString({ lineWidth: 0 });
}
