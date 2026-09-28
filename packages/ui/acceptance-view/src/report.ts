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
 * Reads `tests/acceptance/report.json`, schemaVersion 2.
 *
 * Tolerant on purpose: the file is written by an agent, and a view that throws
 * on one malformed entry shows nothing for the fifteen good ones. Anything
 * unreadable is dropped and the rest renders; `parseAcceptanceReport` only
 * fails when there is no `scenarios` array at all.
 */

/**
 * The four the run answers. Deliberately NOT a union type: an outcome word the
 * console does not know renders verbatim and neutral rather than falling
 * through to a wrong label, which agrees — without either side knowing about
 * the other — with the Go ladder counting an unrecognised outcome as a gap
 * rather than as coverage (`validation/report.go`).
 */
export const OUTCOMES = ["passed", "failed", "blocked", "unjudgeable"] as const;
export type KnownOutcome = (typeof OUTCOMES)[number];

export interface ReportStep {
  readonly keyword: string;
  readonly text: string;
  /** Not always a command: `n/a`, `(already signed in from prior scenario)`. */
  readonly command?: string;
  /**
   * Absent is NOT zero. A blocked `Then` has no exit at all, and the Go struct
   * uses a pointer for exactly this reason — collapsing the two would render a
   * step that never ran as one that succeeded.
   */
  readonly exit?: number;
  readonly observed?: string;
}

export interface ReportScenario {
  readonly feature: string;
  readonly featureFile?: string;
  readonly line?: number;
  readonly rule: string;
  readonly scenario: string;
  readonly outcome: string;
  readonly steps: readonly ReportStep[];
}

export interface AcceptanceReport {
  readonly schemaVersion?: number;
  readonly generatedAt?: string;
  readonly commit?: string;
  readonly baseUrl?: string;
  /** How the scenarios were kept independent. A paragraph, not a tag. */
  readonly isolation?: string;
  readonly scenarios: readonly ReportScenario[];
  /** Keyed by {@link scenarioKey}. */
  readonly byKey: ReadonlyMap<string, ReportScenario>;
}

export interface ReportParseError {
  readonly error: string;
}

export type ReportParseResult = AcceptanceReport | ReportParseError;

export function isReportParseError(r: ReportParseResult): r is ReportParseError {
  return "error" in r;
}

/**
 * Identity, and deliberately not the line number.
 *
 * The features are read at the branch tip and the report at the merge commit of
 * the attempt that wrote it, so any edit above a scenario moves its line while
 * leaving the scenario itself untouched. Feature + rule + scenario is what
 * `check-report.mjs` keys on and what `reportScenario.id()` builds in Go, so
 * all three agree on what "the same scenario" means.
 */
export function scenarioKey(feature: string, rule: string, scenario: string): string {
  return [feature, rule, scenario].join(" ▸ ");
}

function str(v: unknown): string | undefined {
  return typeof v === "string" && v !== "" ? v : undefined;
}

function readStep(raw: unknown): ReportStep | null {
  if (typeof raw !== "object" || raw === null) return null;
  const s = raw as Record<string, unknown>;
  const text = str(s["text"]);
  if (text === undefined) return null;
  return {
    keyword: str(s["keyword"]) ?? "",
    text,
    ...(str(s["command"]) !== undefined ? { command: str(s["command"]) as string } : {}),
    ...(typeof s["exit"] === "number" ? { exit: s["exit"] } : {}),
    ...(str(s["observed"]) !== undefined ? { observed: str(s["observed"]) as string } : {}),
  };
}

function readScenario(raw: unknown): ReportScenario | null {
  if (typeof raw !== "object" || raw === null) return null;
  const s = raw as Record<string, unknown>;
  const scenario = str(s["scenario"]);
  if (scenario === undefined) return null;
  const steps = Array.isArray(s["steps"])
    ? (s["steps"].map(readStep).filter((x): x is ReportStep => x !== null) as ReportStep[])
    : [];
  return {
    feature: str(s["feature"]) ?? "",
    ...(str(s["featureFile"]) !== undefined ? { featureFile: str(s["featureFile"]) as string } : {}),
    ...(typeof s["line"] === "number" ? { line: s["line"] } : {}),
    rule: str(s["rule"]) ?? "",
    scenario,
    outcome: str(s["outcome"]) ?? "",
    steps,
  };
}

export function parseAcceptanceReport(raw: string): ReportParseResult {
  let doc: unknown;
  try {
    doc = JSON.parse(raw);
  } catch (e) {
    return { error: e instanceof Error ? e.message : "the report is not valid JSON" };
  }
  if (typeof doc !== "object" || doc === null) return { error: "the report is not an object" };
  const d = doc as Record<string, unknown>;
  if (!Array.isArray(d["scenarios"])) return { error: "the report has no scenarios" };

  const scenarios = d["scenarios"]
    .map(readScenario)
    .filter((s): s is ReportScenario => s !== null);
  const byKey = new Map<string, ReportScenario>();
  for (const s of scenarios) byKey.set(scenarioKey(s.feature, s.rule, s.scenario), s);

  return {
    ...(typeof d["schemaVersion"] === "number" ? { schemaVersion: d["schemaVersion"] } : {}),
    ...(str(d["generatedAt"]) !== undefined ? { generatedAt: str(d["generatedAt"]) as string } : {}),
    ...(str(d["commit"]) !== undefined ? { commit: str(d["commit"]) as string } : {}),
    ...(str(d["baseUrl"]) !== undefined ? { baseUrl: str(d["baseUrl"]) as string } : {}),
    ...(str(d["isolation"]) !== undefined ? { isolation: str(d["isolation"]) as string } : {}),
    scenarios,
    byKey,
  };
}

/**
 * The step that settled the scenario — the one whose reason is worth showing
 * on a collapsed row.
 *
 * Same rule as `deciding()` in `validation/report.go`, so the console's
 * one-line summary and a repair issue quote the same step: the first step that
 * exited nonzero, else the first that recorded anything observed.
 */
export function decidingStep(scenario: ReportScenario): ReportStep | undefined {
  return (
    scenario.steps.find((s) => s.exit !== undefined && s.exit !== 0) ??
    scenario.steps.find((s) => s.observed !== undefined)
  );
}
