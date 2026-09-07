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
 * Scenario runners: drivers + scoring composed per section, and the chain
 * (#357: live production-shaped handoff — requirements→design continue ONE
 * conversation, tasks runs detached; halt on fail-band with downstream marked
 * skipped; per-section verdicts, never a blended number).
 *
 * Scoring runs INSIDE the task (not in evalite scorers) so the chain can band
 * a section before deciding to continue; the .eval.ts scorers only extract.
 */

import { readdirSync, readFileSync, existsSync } from "node:fs";
import { join } from "node:path";
import { openSession } from "@aep/playground/src/engine/session.js";
import { flowSpec, startSpec } from "@aep/playground/src/engine/turn-spec.js";

// The design flow, as a fact. In production aep-api classifies the /design
// token; the evals run without aep-api, so they state the same thing directly —
// and the agents service composes identical wording for both.
const designTurn = flowSpec("design");
import { PROJECTS_HOME } from "./config.js";
import { prepareProject, readProjectFile } from "./project.js";
import type { ChainScenario, DesignScenario, RequirementsScenario, Rubric, TasksScenario } from "./scenario.js";
import { decisionsDigest, type SimAnswer } from "./sim-user.js";
import { runConversationalSection, type SectionRunResult } from "./drivers/conversational.js";
import { runTaskPlanSection, type TaskPlanRunResult } from "./drivers/task-plan.js";
import { sectionVerdict, type SectionVerdict } from "./scoring/bands.js";
import { judgeArtifact, type JudgeReport } from "./scoring/judge.js";
import { designChecks, requirementsChecks, tasksChecks, type StructuralReport } from "./scoring/structural.js";
import { emitReviewSheet, type ReviewSection } from "./scoring/review-sheet.js";
import { sumUsage, writeRunArtifacts, type TurnRecord, type TurnUsage } from "./tracing.js";
import { listFlows } from "@aep/playground/src/engine/gates.js";

export interface SectionOutcome {
  section: string;
  verdict: SectionVerdict;
  structural: StructuralReport;
  judge: JudgeReport | null;
  turns: number;
  questionsAsked: number;
  skipped: boolean;
}

export interface EvalRunOutput {
  scenario: string;
  outcomes: SectionOutcome[];
  usage: TurnUsage;
  transcriptPath: string;
  tracePath: string;
  reviewSheetPath: string | null;
}

/** The artifact each section's judge reads, clipped defensively. */
const CLIP = 80_000;
const clip = (s: string): string =>
  s.length > CLIP ? `${s.slice(0, CLIP)}\n…(clipped at ${CLIP} characters — later files were NOT judged)` : s;

function requirementsArtifact(projectDir: string): string {
  return readProjectFile(projectDir, "specs/requirements/prd.md");
}

function designArtifact(projectDir: string): string {
  const labeled = (rel: string): string => {
    const content = readProjectFile(projectDir, rel);
    return content ? `--- ${rel} ---\n${content}` : "";
  };
  const parts: string[] = [
    labeled("specs/design/design.cell"),
    labeled("specs/design/domain-model.md"),
  ];
  for (const f of listFlows(projectDir)) {
    parts.push(labeled(`specs/design/flows/${f}`));
  }
  const componentsDir = join(projectDir, "specs/design/components");
  if (existsSync(componentsDir)) {
    for (const c of readdirSync(componentsDir).sort()) {
      for (const f of ["design.json", "openapi.yaml"]) {
        const rel = `specs/design/components/${c}/${f}`;
        const content = readProjectFile(projectDir, rel);
        if (content) parts.push(`--- ${rel} ---\n${content}`);
      }
    }
  }
  return parts.filter(Boolean).join("\n\n");
}

function tasksArtifact(projectDir: string): string {
  const dir = join(projectDir, "issues");
  if (!existsSync(dir)) return "";
  return readdirSync(dir)
    .filter((f) => /^\d+\.md$/.test(f))
    .sort((a, b) => Number.parseInt(a) - Number.parseInt(b))
    .map((f) => `--- issues/${f} ---\n${readFileSync(join(dir, f), "utf8")}`)
    .join("\n\n");
}

async function scoreConversational(
  projectDir: string,
  run: SectionRunResult,
  rubric: Rubric,
  priorAnswers: SimAnswer[],
): Promise<SectionOutcome> {
  const isDesign = run.section === "design";
  const structural = isDesign ? designChecks(projectDir, run) : requirementsChecks(projectDir, run);
  const artifact = isDesign ? designArtifact(projectDir) : requirementsArtifact(projectDir);
  const digest = decisionsDigest([...priorAnswers, ...run.answers]);
  const judge = artifact.trim()
    ? await judgeArtifact({
        rubric,
        artifactLabel: isDesign ? "software design (design.cell + domain-model.md + flows/ + per-component design.json/openapi.yaml)" : "requirements document",
        artifact: clip(artifact),
        ...(digest ? { decisionsDigest: digest } : {}),
      })
    : null;
  return {
    section: run.section,
    verdict: sectionVerdict(run.section, structural.score, judge?.weightedScore ?? null, (judge?.mustNotViolations.length ?? 0) > 0),
    structural,
    judge,
    turns: run.records.length,
    questionsAsked: run.questionsAsked,
    skipped: false,
  };
}

async function scoreTasks(projectDir: string, run: TaskPlanRunResult, rubric: Rubric): Promise<SectionOutcome> {
  const structural = tasksChecks(projectDir, run);
  const artifact = tasksArtifact(projectDir);
  const judge = artifact.trim()
    ? await judgeArtifact({
        rubric,
        artifactLabel: "task plan (the folded issues/*.md set)",
        artifact: clip(artifact),
      })
    : null;
  return {
    section: "tasks",
    verdict: sectionVerdict("tasks", structural.score, judge?.weightedScore ?? null, (judge?.mustNotViolations.length ?? 0) > 0),
    structural,
    judge,
    turns: run.records.length,
    questionsAsked: 0,
    skipped: false,
  };
}

function skippedOutcome(section: string): SectionOutcome {
  return {
    section,
    verdict: { section, structural: 0, judge: null, score: 0, band: "fail", forcedReview: false },
    structural: { score: 0, checks: [] },
    judge: null,
    turns: 0,
    questionsAsked: 0,
    skipped: true,
  };
}

function finishRun(
  evalName: string,
  scenarioName: string,
  runName: string,
  records: TurnRecord[],
  outcomes: SectionOutcome[],
  artifacts: Record<string, string>,
): EvalRunOutput {
  const { transcriptPath, tracePath } = writeRunArtifacts(PROJECTS_HOME, runName, records, artifacts);
  const sections: ReviewSection[] = outcomes.map((o) => ({
    verdict: o.verdict,
    structural: o.structural,
    judge: o.judge,
    ...(o.skipped ? { skipped: true } : {}),
  }));
  const reviewSheetPath = emitReviewSheet({ scenario: scenarioName, evalName, sections, transcriptPath, tracePath });
  return {
    scenario: scenarioName,
    outcomes,
    usage: sumUsage(records),
    transcriptPath,
    tracePath,
    reviewSheetPath,
  };
}

export async function runRequirementsScenario(sc: RequirementsScenario, runName: string): Promise<EvalRunOutput> {
  const projectDir = prepareProject(runName);
  const session = await openSession(projectDir, {});
  let run: SectionRunResult;
  try {
    run = await runConversationalSection(session, "requirements", startSpec(sc.brief.idea), sc.brief);
  } finally {
    await session.close();
  }
  const outcome = await scoreConversational(projectDir, run, sc.rubric, []);
  return finishRun("requirements-section", sc.brief.name, runName, run.records, [outcome], {
    "specs/requirements/prd.md": requirementsArtifact(projectDir),
  });
}

export async function runDesignScenario(sc: DesignScenario, runName: string): Promise<EvalRunOutput> {
  const projectDir = prepareProject(runName, sc.fixture);
  const session = await openSession(projectDir, {});
  let run: SectionRunResult;
  try {
    run = await runConversationalSection(session, "design", designTurn, sc.brief);
  } finally {
    await session.close();
  }
  const outcome = await scoreConversational(projectDir, run, sc.rubric, []);
  return finishRun("design-section", sc.brief.name, runName, run.records, [outcome], {
    "specs/design/": designArtifact(projectDir),
  });
}

export async function runTasksScenario(sc: TasksScenario, runName: string): Promise<EvalRunOutput> {
  const projectDir = prepareProject(runName, sc.fixture);
  const run = await runTaskPlanSection(projectDir);
  const outcome = await scoreTasks(projectDir, run, sc.rubric);
  return finishRun("tasks-section", sc.name, runName, run.records, [outcome], {
    "issues/": tasksArtifact(projectDir),
  });
}

export async function runChainScenario(sc: ChainScenario, runName: string): Promise<EvalRunOutput> {
  const projectDir = prepareProject(runName);
  const records: TurnRecord[] = [];
  const outcomes: SectionOutcome[] = [];

  // Requirements → design share ONE conversation (the console flow); the
  // session stays open across both.
  const session = await openSession(projectDir, {});
  let designSkipped = false;
  let requirementsAnswers: SimAnswer[] = [];
  try {
    const req = await runConversationalSection(session, "requirements", startSpec(sc.brief.idea), sc.brief);
    records.push(...req.records);
    requirementsAnswers = req.answers;
    const reqOutcome = await scoreConversational(projectDir, req, sc.rubrics.requirements, []);
    outcomes.push(reqOutcome);

    if (reqOutcome.verdict.band === "fail") {
      designSkipped = true;
    } else {
      const design = await runConversationalSection(session, "design", designTurn, sc.brief);
      records.push(...design.records);
      const designOutcome = await scoreConversational(projectDir, design, sc.rubrics.design, requirementsAnswers);
      outcomes.push(designOutcome);
    }
  } finally {
    await session.close();
  }

  if (designSkipped) {
    outcomes.push(skippedOutcome("design"), skippedOutcome("tasks"));
  } else if (outcomes[1]!.verdict.band === "fail") {
    outcomes.push(skippedOutcome("tasks"));
  } else {
    // Tasks runs detached (its own session), exactly as production's build click.
    const tasks = await runTaskPlanSection(projectDir);
    records.push(...tasks.records);
    outcomes.push(await scoreTasks(projectDir, tasks, sc.rubrics.tasks));
  }

  return finishRun("chain", sc.brief.name, runName, records, outcomes, {
    "specs/requirements/prd.md": requirementsArtifact(projectDir),
    "specs/design/": designArtifact(projectDir),
    "issues/": tasksArtifact(projectDir),
  });
}

/** The chain's headline: the per-section verdict vector (#357). */
export function verdictVector(out: EvalRunOutput): string {
  return out.outcomes
    .map((o) => `${o.section}:${o.skipped ? "skipped" : o.verdict.band}`)
    .join(" / ");
}
