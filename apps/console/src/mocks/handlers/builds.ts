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

import { http, HttpResponse } from "msw";
import type { ProjectBuild } from "../../features/builds/api/builds";
import { repairOfBody, selectionOfBody, type BuildBody } from "../../features/builds/buildSelection";
import { builtLines } from "../../features/builds/model/changes";
import { buildOffer, type PickRow } from "../../features/builds/model/picker";
import { projectSpecDoc } from "../../features/spec/collab/specDoc";
import { readSpecLines } from "../../features/spec/collab/useSpecLines";
import { deriveWorkspace } from "../../features/spec/model/workspace";
import type { components } from "../../generated/aep-api";
import { addBuild, elapsed, mockBuilds, projectBuilds, scriptOf, type MockBuild } from "../buildsState";
import { designView } from "../designState";
import { framesUntil, runAt, snapshotAt, summaryAt } from "../fixtures/buildRun";
import { specView } from "../specState";

type BuildResponse = components["schemas"]["BuildResponse"];
type BuildList = components["schemas"]["BuildList"];
type SpecVersionList = components["schemas"]["SpecVersionList"];
type BuildRunList = components["schemas"]["BuildRunList"];
type ApiError = components["schemas"]["Error"];

// What each version built answers list-project-versions, off the builds the
// mock keeps. Starting a build answers the contract's POST
// /projects/{projectName}/build, reading the selection (or the provisional
// repair) off its body (buildSelection.ts). The server checks the selection
// against the spec and the design as they are now, as the platform's build
// gate would, and refuses one it cannot build with the gate's 422 detail rows.
//
// The run itself is today's API: the ledger (list-project-builds), the run
// rows (list-build-runs), the run's progress stream (stream-run-progress,
// SSE) and the validation report (get-validation-report), all read off the
// build's scripted run (fixtures/buildRun.ts).
//
// Acme Expenses' first build fails one scenario, the deputy's (F2.4), so Fix
// has something to fix; a repair build passes. For a first build that passes:
//   localStorage.setItem("aep:mock:build-result", "pass")

const FAILING_STORY: Record<string, string> = { "acme-expenses": "F2.4" };

function refuse(status: number, body: ApiError): Response {
  return HttpResponse.json<ApiError>(body, { status });
}

function firstBuildFails(): boolean {
  return localStorage.getItem("aep:mock:build-result") !== "pass";
}

/** Why a picked row cannot be built, as a gate detail row; null when it can. */
function gateProblem(rows: PickRow[], id: string): { field: string; message: string } | null {
  const row = rows.find((r) => r.id === id);
  if (row?.state === "offered") return null;
  const message = row ? `${row.kind === "feature" ? row.name : row.id}: ${row.detail.join(", ")}` : `${id}: not in the spec`;
  return { field: `body.selection.${id}`, message };
}

/** The acceptance files the design wrote for these features: what validation judges them by. */
function criteriaFor(projectName: string, features: string[]): { path: string; content: string }[] {
  return designView(projectName).artifacts.flatMap((a) =>
    a.source.kind === "acceptance" && a.features.some((f) => features.includes(f))
      ? [{ path: a.source.path, content: a.source.content }]
      : [],
  );
}

function newRun(projectName: string, build: Omit<ProjectBuild, "status">, failing: string | null, repair: MockBuild["run"]["repair"]): MockBuild {
  const n = mockBuilds(projectName).length + 1;
  return {
    projectName,
    build,
    run: {
      version: build.version,
      runId: `run-${projectName}-${n}-${Date.now().toString(36)}`,
      milestoneNumber: n,
      features: build.features.map((f) => ({ id: f.id, name: f.name })),
      repair,
      criteria: criteriaFor(projectName, build.features.map((f) => f.id)),
      failing,
      startedAt: Date.now(),
    },
  };
}

/**
 * A repair build of `of`: the same features, working what `of` failed, which
 * re-runs every scenario. Like the server, it refuses a version with nothing
 * failing, and a repair of a repair fixes the version it fixed.
 */
function startRepair(projectName: string, repair: { of: string }): Response {
  const builds = mockBuilds(projectName);
  const fixed = builds.find((b) => b.build.version === repair.of);
  if (!fixed) return refuse(404, { code: "not_found", message: `No version named ${repair.of}.` });
  const failing = fixed.run.failing;
  if (!failing) return refuse(409, { code: "conflict", message: `${repair.of} has no failing scenario to fix` });
  const of = fixed.build.fixes ?? repair.of;
  const version = `${of}.${builds.filter((b) => b.build.fixes === of).length + 1}`;
  addBuild(newRun(projectName, { ...fixed.build, version, fixes: of }, null, { of, stories: [failing] }));
  return HttpResponse.json<BuildResponse>({ tag: version });
}

/** The build whose run this is, and the build asked for by tag. */
function byRun(projectName: string, runId: string): MockBuild | undefined {
  return mockBuilds(projectName).find((b) => b.run.runId === runId);
}

function byTag(projectName: string, tag: string): MockBuild | undefined {
  return mockBuilds(projectName).find((b) => b.build.version === tag);
}

/** The run's progress stream: what has happened at once, then the rest as it happens, then `done`. */
function progressStream(build: MockBuild, signal: AbortSignal): ReadableStream<Uint8Array> {
  const script = scriptOf(build);
  const encoder = new TextEncoder();
  const timers: ReturnType<typeof setTimeout>[] = [];
  return new ReadableStream<Uint8Array>({
    start(controller) {
      let closed = false;
      const send = (data: string) => {
        if (!closed && !signal.aborted) controller.enqueue(encoder.encode(`data: ${data}\n\n`));
      };
      const close = () => {
        if (closed) return;
        closed = true;
        timers.forEach(clearTimeout);
        controller.close();
      };
      signal.addEventListener("abort", close);
      const now = elapsed(build);
      for (const { frame } of framesUntil(script, now)) send(JSON.stringify(frame));
      for (const { at, frame } of script.frames.filter((f) => f.at > now)) {
        timers.push(setTimeout(() => send(JSON.stringify(frame)), at - now));
      }
      timers.push(
        setTimeout(() => {
          send("[DONE]");
          close();
        }, Math.max(0, script.end - now) + 10),
      );
    },
    cancel() {
      timers.forEach(clearTimeout);
    },
  });
}

export const buildsHandlers = [
  http.get("*/api/v1/projects/:projectName/versions", ({ params }) =>
    HttpResponse.json<SpecVersionList>({
      versions: mockBuilds(String(params.projectName)).map(({ build }) => ({
        name: build.version,
        features: build.features.map((f) => ({
          id: f.id,
          name: f.name,
          lines: f.lines.map((l) => (l.id ? { id: l.id, words: l.words } : { words: l.words })),
        })),
        productWide: build.productWide,
        heldBack: [],
        ...(build.fixes ? { fixes: build.fixes } : {}),
      })),
    }),
  ),

  http.get("*/api/v1/projects/:projectName/builds", ({ params }) => {
    const builds = mockBuilds(String(params.projectName));
    const summaries = builds.map((b) => summaryAt(scriptOf(b), elapsed(b))).reverse();
    return HttpResponse.json<BuildList>({ builds: summaries });
  }),

  http.get("*/api/v1/projects/:projectName/builds/:tag/runs", ({ params }) => {
    const build = byTag(String(params.projectName), String(params.tag));
    if (!build) return refuse(404, { code: "not_found", message: `No version named ${String(params.tag)}.` });
    const script = scriptOf(build);
    return HttpResponse.json<BuildRunList>({
      tag: build.build.version,
      milestoneNumber: build.run.milestoneNumber,
      runs: [runAt(script, elapsed(build))],
    });
  }),

  http.get("*/api/v1/projects/:projectName/runs/:runId/progress", ({ params, request }) => {
    const build = byRun(String(params.projectName), String(params.runId));
    if (!build) return refuse(404, { code: "not_found", message: "No such run." });
    return new HttpResponse(progressStream(build, request.signal), {
      headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" },
    });
  }),

  http.get("*/api/v1/projects/:projectName/validations/:tag/cycles/:cycleId/report", ({ params }) => {
    const build = byTag(String(params.projectName), String(params.tag));
    const snapshot = build ? snapshotAt(scriptOf(build), elapsed(build), String(params.cycleId)) : null;
    if (!build || !snapshot) return refuse(404, { code: "not_found", message: "No report for this attempt." });
    // The version validates every feature built in it or before (B4).
    const builds = mockBuilds(String(params.projectName));
    const upTo = builds.slice(0, builds.findIndex((b) => b.build.version === build.build.version) + 1);
    const features = [...new Set(upTo.flatMap((b) => b.build.features.map((f) => f.id)))];
    return HttpResponse.json({ ...snapshot, scope: { features, heldBack: [] } });
  }),

  http.post("*/api/v1/projects/:projectName/build", async ({ params, request }): Promise<Response> => {
    const projectName = String(params.projectName);
    const body = (await request.json()) as BuildBody;
    const builds = projectBuilds(projectName);
    if (builds.some((b) => b.status === "building")) {
      return refuse(409, { code: "build_in_progress", message: "A build is already running for this project." });
    }
    const repair = repairOfBody(body);
    if (repair) return startRepair(projectName, repair);
    const selection = selectionOfBody(body);
    if (!selection || selection.features.length === 0) {
      return refuse(400, { code: "invalid_request", message: "Pick at least one feature to build." });
    }
    const model = specView(projectName);
    const lines = readSpecLines(projectSpecDoc(projectName, model));
    const workspace = deriveWorkspace(model, lines);
    const design = designView(projectName);
    const offer = buildOffer({
      features: workspace.features,
      designedFrom: model.design.designedFrom,
      productWide: workspace.productWide,
      lines,
      dependencies: design.dependencies,
      comments: design.comments,
      artifacts: design.artifacts,
      builds,
    });
    const held = [...selection.features, ...selection.productWide].flatMap((id) => gateProblem(offer.rows, id) ?? []);
    if (held.length > 0) {
      return refuse(422, { code: "validation_failed", message: "Not ready to build yet.", details: held });
    }
    const picked = workspace.features.filter((f) => selection.features.includes(f.id));
    const carried = workspace.productWide
      .filter((p) => p.appliesTo === "all" || picked.some((f) => (p.appliesTo as string[]).includes(f.id)))
      .map((p) => p.id);
    const failing = offer.firstBuild && firstBuildFails() ? (FAILING_STORY[projectName] ?? null) : null;
    const build = {
      version: offer.version,
      features: picked.map((f) => ({ id: f.id, name: f.name, lines: builtLines(lines.get(f.path) ?? []) })),
      productWide: carried,
    };
    addBuild(newRun(projectName, build, failing && picked.some((f) => failing.startsWith(`${f.id}.`)) ? failing : null, null));
    return HttpResponse.json<BuildResponse>({ tag: offer.version });
  }),
];
