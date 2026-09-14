import type { components } from "../../generated/aep-api";

type ApiError = components["schemas"]["Error"];
type ApplyRequest = components["schemas"]["ApplyRequest"];
type ApplyResult = components["schemas"]["ApplyResult"];
type BuildRunList = components["schemas"]["BuildRunList"];
import { http, HttpResponse, type JsonBodyType } from "msw";
import {
  appliedFileContent,
  appliedFileMetas,
  applyFilesError,
  uploadReferencesError,
  componentDeployments,
  componentOpenApi,
  buildRunsForTag,
  projectBuildRuns,
  projectCycleBuilds,
  projectBuilds,
  projectComponents,
  projectDependencies,
  projectDependencyReadiness,
  projectSectionError,
  projectSpecFiles,
  projectStatuses,
  TRACK_SCENARIOS,
  trackOverrides,
  type TrackScenario,
  projectTags,
  projectTasks,
  recordAppliedFiles,
  specFileContent,
  specFileMetas,
  specFileNotFound,
  type ProjectScenario,
} from "../fixtures/project";
import {
  findTask,
  isSettledStatus,
  liveLine,
  streamFrames,
  taskDetailOf,
} from "../fixtures/task-log";
import {
  isTerminalRunState,
  runCancelledEvents,
  runCycleEvents,
  runCycleLines,
  runHeartbeatEvent,
} from "../fixtures/run-progress";
import {
  CRITERIA_PATH,
  VALIDATION_ATTEMPTS,
  VALIDATION_FILE_PATHS,
  VALIDATION_SCENARIOS,
  validationFiles,
  validationRuns,
  validationStatusThread,
  type ValidationAttempt,
  type ValidationScenario,
} from "../fixtures/validation";

function scenario(): ProjectScenario {
  const chosen = localStorage.getItem("aep:mock:project") as ProjectScenario | null;
  if (chosen) return chosen;
  // A verdict override is only reachable on a version whose run got that far, so
  // one devtools key is enough to see it: the base defaults to the deployed story
  // rather than the usual mid-build one.
  return validationScenario() ? "deployed" : "building";
}

// The track override (aep:mock:track): the spec/build/deploy combinations the
// scenario ladder cannot express, because each of its rungs has the three
// stages agreeing with each other. Unknown values are ignored, same as the
// validation override — a typo should not look like the switch is broken.
function trackScenario(): TrackScenario | null {
  const raw = localStorage.getItem("aep:mock:track");
  return raw && TRACK_SCENARIOS.includes(raw as TrackScenario)
    ? (raw as TrackScenario)
    : null;
}

// The validation override (aep:mock:validation), or null when the project
// scenario's own fixtures should stand. Unknown values are ignored rather than
// passed through — a typo would otherwise render as the `none` empty state and
// look like the switch is broken.
// How fast a mock progress stream plays a line. Paced for a HUMAN watching the
// feed animate, not for tests — nothing asserts on it, and the three streams
// below share the constant so one of them cannot quietly drift to a different
// speed from the others.
//
// A second rather than a fraction of one: the per-criterion rows on the
// Validation page step through five statuses each, and at 120ms a whole
// validation cycle replayed faster than a reader could follow which row had
// changed.
const MOCK_LINE_MS = 1_000;

function validationScenario(): ValidationScenario | null {
  const raw = localStorage.getItem("aep:mock:validation");
  return raw && VALIDATION_SCENARIOS.includes(raw as ValidationScenario)
    ? (raw as ValidationScenario)
    : null;
}

// Which attempt a `running` scenario is on (aep:mock:validation-attempt). It splits
// the one scenario the switch cannot: `deploy.validation` is `running` for both a
// first attempt and a repeat, and only the repeat carries a verdict to render.
// Ignored by every other scenario, and by an unknown value.
function validationAttempt(): ValidationAttempt {
  const raw = localStorage.getItem("aep:mock:validation-attempt");
  return raw && VALIDATION_ATTEMPTS.includes(raw as ValidationAttempt)
    ? (raw as ValidationAttempt)
    : "first";
}

// Whether the repo should read as having no acceptance oracle at all
// (aep:mock:validation-criteria=missing). A separate key from the two above because
// it names what is IN THE REPO rather than which run or which attempt: the page
// treats a `not_found` on the criteria as "none were authored" — the state a version
// eventually settles as `skipped` for — and nothing else can produce it, since every
// scenario that has a verdict also has an oracle.
function criteriaMissing(): boolean {
  return localStorage.getItem("aep:mock:validation-criteria") === "missing";
}

// Whether the oracle should carry a criterion the pinned report predates
// (aep:mock:validation-criteria=drifted). Shares the key with `missing` because both
// describe the criteria FILE rather than a run, and the two are mutually exclusive:
// a file that is absent cannot also have drifted.
function criteriaDrifted(): boolean {
  return localStorage.getItem("aep:mock:validation-criteria") === "drifted";
}

// The project's files with the two validation artifacts swapped for the ones the
// overridden verdict implies. Dropping them first is what makes `unreported` and
// `skipped` reachable: those scenarios contribute FEWER files, not different ones.
function specFiles(s: Exclude<ProjectScenario, "error">) {
  const v = validationScenario();
  if (!v) return projectSpecFiles[s];
  return [
    ...projectSpecFiles[s].filter((f) => !VALIDATION_FILE_PATHS.includes(f.path)),
    ...validationFiles(v, validationAttempt(), criteriaDrifted()).filter(
      (f) => !(criteriaMissing() && f.path === CRITERIA_PATH),
    ),
  ];
}

function respond<T extends JsonBodyType>(
  pick: (s: Exclude<ProjectScenario, "error">) => T,
) {
  const s = scenario();
  if (s === "error") {
    return HttpResponse.json(projectSectionError, {
      status: 500,
    });
  }
  return HttpResponse.json(pick(s));
}

// Project-scoped reads backing the overview page (issue #77). The project
// itself (GET /projects/:projectName) is served by handlers/projects.ts.
// Runs the reader cancelled in THIS browser session, by id.
//
// Session-scoped and deliberately not persisted: a mock scenario is a story you
// start over by reloading, and a cancellation that outlived the page would make
// the `building` scenario permanently uncancellable.
const cancelledRuns = new Set<string>();

/**
 * The run story with the reader's cancellations applied.
 *
 * The real supervisor acts on the signal and the row flips; the mock has no
 * supervisor, so this is where "somebody stopped it" becomes true. Applied to
 * BOTH the run list and the progress stream, because a feed saying cancelled
 * under a header saying running is a fixture contradicting itself.
 */
function withCancellations(story: BuildRunList): BuildRunList {
  if (cancelledRuns.size === 0) return story;
  return {
    ...story,
    runs: story.runs.map((run) =>
      cancelledRuns.has(run.id)
        ? {
            ...run,
            state: "cancelled" as const,
            terminalReason: "cancelled" as const,
            endedAt: new Date().toISOString(),
          }
        : run,
    ),
  };
}

export const projectHandlers = [
  http.get("*/api/v1/projects/:projectName/status", () =>
    respond((s) => {
      const v = validationScenario();
      const track = trackScenario();
      const scenarioBase = projectStatuses[s];
      // The track override replaces all three aggregates together — they only
      // mean anything as a set.
      const base = track ? { ...scenarioBase, ...trackOverrides[track] } : scenarioBase;
      // Only deploy.validation moves: the rest of the status is the project
      // scenario's, so the override can be read against any of them.
      return v ? { ...base, deploy: { ...base.deploy, validation: v } } : base;
    }),
  ),
  http.get("*/api/v1/projects/:projectName/components", () =>
    respond((s) => projectComponents[s]),
  ),
  // Read-time dependency status for the whole design (#252) — the Spec view's
  // status chips and the Deployments page's promotion connections.
  http.get("*/api/v1/projects/:projectName/design/dependencies", () =>
    respond((s) => projectDependencies(s)),
  ),
  // Whether the platform holds real values for each external dependency in an
  // environment — the Builds page's External resources section (ADR-0023).
  // Environment-scoped on the wire; the mock ignores it, since the console only
  // ever asks about development.
  http.get("*/api/v1/projects/:projectName/dependencies/readiness", () =>
    respond((s) => projectDependencyReadiness(s)),
  ),
  // The dependency definition view's two writes (ADR-0028): a document lands in the
  // dependency's directory; an assumption is accepted. Neither echoes anything
  // the page needs beyond success, so the mock acknowledges and the page
  // refetches the dependencies read model.
  http.post("*/api/v1/projects/:projectName/dependencies/:name/contract", ({ params }) => {
    if (scenario() === "error") {
      return HttpResponse.json(projectSectionError, { status: 500 });
    }
    return HttpResponse.json({
      contract: `specs/design/dependencies/${String(params["name"])}/openapi.yaml`,
    });
  }),
  http.post("*/api/v1/projects/:projectName/dependencies/:name/assumption", () => {
    if (scenario() === "error") {
      return HttpResponse.json(projectSectionError, { status: 500 });
    }
    return HttpResponse.json({ status: "accepted" });
  }),
  // Re-collect an external connection's values (#395 follow-up). Values are
  // write-only on the real platform (secrets go to the secret manager and
  // never echo), so the mock just acknowledges.
  http.post(
    "*/api/v1/projects/:projectName/dependencies/external-resources/:name/values",
    () => {
      if (scenario() === "error") {
        return HttpResponse.json(projectSectionError, { status: 500 });
      }
      return HttpResponse.json({ status: "provisioned" });
    },
  ),
  // The component's OpenAPI contract for the in-app viewer dialog. Errors
  // follow the section scenario; otherwise a spec keyed to the component name.
  http.get(
    "*/api/v1/projects/:projectName/components/:componentName/openapi",
    ({ params }) => respond(() => componentOpenApi(String(params.componentName))),
  ),
  // Per-component release bindings — the overview's "Open app" link (#196)
  // and the Deployments board's fan-out (#216).
  http.get(
    "*/api/v1/projects/:projectName/components/:componentName/deployments",
    ({ params }) =>
      respond((s) => componentDeployments(s, String(params.componentName))),
  ),
  http.get("*/api/v1/projects/:projectName/tasks", ({ request }) => {
    // ?tag=vN scopes to one build's lineage, mirroring the aep:spec/<tag>
    // label read (#185); absent = all versions.
    const tag = new URL(request.url).searchParams.get("tag");
    return respond((s) =>
      tag
        ? projectTasks[s].filter((t) => t.lineage?.specTag === tag)
        : projectTasks[s],
    );
  }),
  // Builds page: the version ledger (also feeds the overview's version menu).
  http.get("*/api/v1/projects/:projectName/builds", () =>
    respond((s) => projectBuilds[s]),
  ),
  // …and one version's whole run story: run rows + cycle records, DB-only.
  http.get("*/api/v1/projects/:projectName/builds/:tag/runs", ({ params }) =>
    respond((s) => {
      const v = validationScenario();
      // The verdict lives on the RUN, and its cycles are what the page reads the
      // report at — so an override has to replace the whole story, not patch a
      // field onto the project scenario's.
      const tag = String(params.tag);
      // Keyed BY TAG: a run story stamped with another version's identity is a
      // fixture that contradicts its own envelope.
      const story = v
        ? { ...validationRuns(v, validationAttempt()), tag }
        : buildRunsForTag(s, tag);
      return withCancellations(story);
    }),
  ),
  // A build session's fan-out. Derived from the cluster on the real server, so
  // the console only ever asks for a session whose merge landed — and asks per
  // session, which is why the fixture is keyed by scenario rather than by cycle.
  http.get(
    "*/api/v1/projects/:projectName/builds/:tag/cycles/:cycleId/builds",
    () => respond((s) => ({ items: projectCycleBuilds[s] })),
  ),
  // Cancel: 202 means the SIGNAL was sent — the run row flips to `cancelled`
  // when the supervisor acts on it, which is why there is no body to return.
  //
  // The mock then ACTS on it (see `cancelledRuns`). It used to answer 202 and
  // change nothing, so the one button on this page that stops a run had no
  // observable effect in mock mode — and the cancelled ending, which is a
  // distinct thing from a failure everywhere else in the product, could not be
  // seen at all.
  http.post("*/api/v1/projects/:projectName/runs/:runId/cancel", ({ params }) => {
    if (scenario() === "error") {
      return HttpResponse.json(projectSectionError, { status: 503 });
    }
    cancelledRuns.add(String(params.runId));
    return new HttpResponse(null, { status: 202 });
  }),
  // The run feed: ONE SSE stream for the whole run, frames grouped by cycle.
  // ONLY a terminal run settles it — a live run's stream stays open, which is
  // the property the console's reconnect logic is written against.
  http.get(
    "*/api/v1/projects/:projectName/runs/:runId/progress",
    ({ request, params }) => {
      const s = scenario();
      if (s === "error") {
        return HttpResponse.json(projectSectionError, { status: 500 });
      }
      // The same runs list-build-runs answers with. Without this the feed
      // streamed the PROJECT scenario's runs while the page's rows came from the
      // validation override — two answers about one run, and the validation
      // cycle a reader had selected was not the one narrating itself.
      const v = validationScenario();
      const runs = v
        ? validationRuns(v, validationAttempt()).runs
        : projectBuildRuns[s].runs;
      const run = runs[0];
      // Cancellation is checked against the id the CLIENT asked for, not the
      // fixture's own: `buildRunsForTag` restamps run ids per version so a run
      // story cannot contradict its envelope, and the console therefore cancels
      // an id this list has never heard of.
      const cancelled = cancelledRuns.has(String(params.runId));
      const encoder = new TextEncoder();
      let timer: ReturnType<typeof setInterval> | undefined;

      const stream = new ReadableStream<Uint8Array>({
        async start(controller) {
          const send = (data: string) =>
            controller.enqueue(encoder.encode(`data: ${data}\n\n`));
          const delay = (ms: number) =>
            new Promise((resolve) => setTimeout(resolve, ms));

          // `event` frames, the v2 feed. The cycle and the attempt are stamped
          // by the SERVER as it relays — a runner knows what it is doing but not
          // which cycle of which run it turned out to be — so the mock stamps
          // them here rather than baking them into the fixture.
          let seq = 0;
          for (const cycle of run?.cycles ?? []) {
            if (request.signal.aborted) return controller.close();
            send(JSON.stringify({ type: "cycle", cycle }));
            for (const event of runCycleEvents(cycle, seq)) {
              if (request.signal.aborted) return controller.close();
              send(
                JSON.stringify({
                  type: "event",
                  cycleId: cycle.id,
                  attempt: cycle.attempts,
                  event,
                }),
              );
              seq = (event.seq ?? seq) + 1;
              await delay(MOCK_LINE_MS);
            }
          }
          if (!run || cancelled || isTerminalRunState(run.state)) {
            if (cancelled) {
              for (const event of runCancelledEvents(seq)) {
                const last = run?.cycles[run.cycles.length - 1];
                if (!last) break;
                send(JSON.stringify({ type: "event", cycleId: last.id, attempt: last.attempts, event }));
                seq = (event.seq ?? seq) + 1;
              }
            }
            send(JSON.stringify({ type: "done", state: cancelled ? "cancelled" : (run?.state ?? "succeeded") }));
            send("[DONE]");
            controller.close();
            return;
          }
          // Live run: heartbeats on the newest cycle until disconnect. They paint
          // no row — the silence explained is the agent's status line — which is
          // exactly the property a mock should keep exercising.
          //
          // …unless the reader cancels it, which is the ONE thing that ends a
          // live mock run. The ending is a fact about the run, so it is appended
          // here rather than baked into a cycle's fixture: the agent it
          // interrupted stops (`stopped`, a cancellation and not a failure) and
          // the run settles `cancelled`.
          const last = run.cycles[run.cycles.length - 1];
          let tick = 1;
          timer = setInterval(() => {
            if (request.signal.aborted || !last) {
              clearInterval(timer);
              controller.close();
              return;
            }
            if (cancelledRuns.has(String(params.runId))) {
              clearInterval(timer);
              for (const event of runCancelledEvents(seq)) {
                send(
                  JSON.stringify({
                    type: "event",
                    cycleId: last.id,
                    attempt: last.attempts,
                    event,
                  }),
                );
                seq = (event.seq ?? seq) + 1;
              }
              send(JSON.stringify({ type: "done", state: "cancelled" }));
              send("[DONE]");
              controller.close();
              return;
            }
            send(
              JSON.stringify({
                type: "event",
                cycleId: last.id,
                attempt: last.attempts,
                event: runHeartbeatEvent(seq++, tick++),
              }),
            );
          }, 4000);
        },
        cancel() {
          if (timer) clearInterval(timer);
        },
      });

      return new HttpResponse(stream, {
        headers: {
          "Content-Type": "text/event-stream",
          "Cache-Control": "no-cache",
        },
      });
    },
  ),
  // The VERSION feed: ONE SSE stream over every run the milestone has seen, in
  // chronological order, each frame carrying the run it belongs to. It settles
  // whenever no run is live — which is NOT "the version is finished", so the
  // frame says `reason`, never a run state.
  //
  // Still `line` frames: this stream has not moved to the v2 envelope, and a
  // mock that moved ahead of it would be testing a contract nothing serves.
  http.get(
    "*/api/v1/projects/:projectName/builds/:tag/progress",
    ({ request }) => {
      const s = scenario();
      if (s === "error") {
        return HttpResponse.json(projectSectionError, { status: 500 });
      }
      // Oldest first: the run list is newest-first, and the narrative reads the
      // other way.
      const runs = [...projectBuildRuns[s].runs].reverse();
      const encoder = new TextEncoder();

      const stream = new ReadableStream<Uint8Array>({
        async start(controller) {
          const send = (data: string) =>
            controller.enqueue(encoder.encode(`data: ${data}\n\n`));
          const delay = (ms: number) =>
            new Promise((resolve) => setTimeout(resolve, ms));

          let seq = 0;
          for (const [r, run] of runs.entries()) {
            const attribution = { id: run.id, kind: run.kind, index: r + 1 };
            for (const [i, cycle] of run.cycles.entries()) {
              if (request.signal.aborted) return controller.close();
              send(JSON.stringify({ type: "cycle", run: attribution, cycle }));
              for (const line of runCycleLines(cycle, i, seq)) {
                if (request.signal.aborted) return controller.close();
                send(JSON.stringify({ type: "line", run: attribution, line }));
                seq = (line.seq ?? seq) + 1;
                await delay(MOCK_LINE_MS);
              }
            }
          }
          if (runs.some((run) => !isTerminalRunState(run.state))) {
            // A live run holds the stream open, exactly as the server does — the
            // property the console's reconnect logic is written against.
            return;
          }
          send(JSON.stringify({ type: "done", reason: "no_live_run" }));
          send("[DONE]");
          controller.close();
        },
      });

      return new HttpResponse(stream, {
        headers: {
          "Content-Type": "text/event-stream",
          "Cache-Control": "no-cache",
        },
      });
    },
  ),
  // Task page (#173): one task with its execution history…
  http.get("*/api/v1/projects/:projectName/tasks/:issueNumber", ({ params }) => {
    const s = scenario();
    if (s === "error") {
      return HttpResponse.json(projectSectionError, {
        status: 500,
      });
    }
    const detail = taskDetailOf(s, Number(params.issueNumber));
    if (!detail) {
      return HttpResponse.json(
        {
          code: "not_found",
          message: `no task #${String(params.issueNumber)}`,
        } satisfies ApiError,
        { status: 404 },
      );
    }
    // The validation issue's thread is the agent's status line, and it is the
    // one thing on this read the validation scenario owns rather than the
    // project scenario — so it is spliced here rather than baked into the task
    // fixture, which serves every scenario alike.
    const v = validationScenario();
    if (detail.executorClass === "validation" && v) {
      const comments = validationStatusThread(v);
      if (comments) return HttpResponse.json({ ...detail, comments });
    }
    return HttpResponse.json(detail);
  }),
  // …and its SSE log: replay the timeline as TaskStreamEvent frames, then
  // either settle with `done` + [DONE] or keep ticking heartbeat lines for a
  // live task (matches the contract's wire format, keep-alives included).
  http.get(
    "*/api/v1/projects/:projectName/tasks/:issueNumber/log",
    ({ params, request }) => {
      const s = scenario();
      if (s === "error") {
        return HttpResponse.json(projectSectionError, {
          status: 500,
        });
      }
      const issueNumber = Number(params.issueNumber);
      const frames = streamFrames(s, issueNumber);
      const task = findTask(s, issueNumber);
      const settled = !task || isSettledStatus(task.derivedStatus);
      const encoder = new TextEncoder();
      let timer: ReturnType<typeof setInterval> | undefined;

      const stream = new ReadableStream<Uint8Array>({
        async start(controller) {
          const send = (data: string) =>
            controller.enqueue(encoder.encode(`data: ${data}\n\n`));
          const delay = (ms: number) =>
            new Promise((resolve) => setTimeout(resolve, ms));

          let seq = 0;
          for (const frame of frames) {
            if (request.signal.aborted) return controller.close();
            send(JSON.stringify(frame));
            if (frame.type === "line") seq = (frame.line?.seq ?? seq) + 1;
            await delay(MOCK_LINE_MS);
          }
          if (settled) {
            send("[DONE]");
            controller.close();
            return;
          }
          // Live task: heartbeat lines until the client disconnects.
          let tick = 1;
          timer = setInterval(() => {
            if (request.signal.aborted) {
              clearInterval(timer);
              controller.close();
              return;
            }
            send(
              JSON.stringify({
                type: "line",
                line: liveLine(issueNumber, seq++, tick++),
              }),
            );
          }, 4000);
        },
        cancel() {
          if (timer) clearInterval(timer);
        },
      });

      return new HttpResponse(stream, {
        headers: {
          "Content-Type": "text/event-stream",
          "Cache-Control": "no-cache",
        },
      });
    },
  ),
  http.get("*/api/v1/projects/:projectName/tags", () =>
    respond((s) => projectTags[s]),
  ),
  // Files API (#113): list-files metadata + per-file content reads, exactly
  // as aep-api serves them (repo-relative specs/ paths). Files applied through
  // the mock files/apply (#383's reference uploads) are merged in per project.
  http.get("*/api/v1/projects/:projectName/files", ({ params }) =>
    respond((s) => [
      ...specFileMetas(specFiles(s)),
      ...appliedFileMetas(String(params.projectName)),
    ]),
  ),
  http.get(
    "*/api/v1/projects/:projectName/files/*",
    ({ request, params }) => {
      const s = scenario();
      if (s === "error") {
        return HttpResponse.json(projectSectionError, {
          status: 500,
        });
      }
      const pathname = new URL(request.url).pathname;
      const path = decodeURIComponent(pathname.replace(/^.*\/files\//, ""));
      const file =
        specFileContent(specFiles(s), path) ??
        appliedFileContent(String(params.projectName), path);
      if (!file) {
        return HttpResponse.json(specFileNotFound(path), {
          status: 404,
        });
      }
      return HttpResponse.json(file);
    },
  ),
  // The create flow's reference upload (#383), fired right after POST
  // /projects. Nothing is committed and nothing becomes a spec file — the real
  // server stores the bytes off-git (ADR-0017) — so the mock only asserts the
  // request shape and answers 204. Error state (the confirm step's Retry /
  // Continue-without-documents surface) via
  // localStorage.setItem('aep:mock:project:references', 'error').
  http.post("*/api/v1/projects/:projectName/references", async ({ request }) => {
    if (localStorage.getItem("aep:mock:project:references") === "error") {
      return HttpResponse.json(uploadReferencesError, { status: 500 });
    }
    const form = await request.formData();
    const files = form.getAll("files");
    if (files.length === 0) {
      return HttpResponse.json(
        { code: "invalid_request", message: "no reference documents" } satisfies ApiError,
        { status: 400 },
      );
    }
    return new HttpResponse(null, { status: 204 });
  }),
  // apply-files. Error state via
  // localStorage.setItem('aep:mock:project:apply', 'error').
  http.post(
    "*/api/v1/projects/:projectName/files/apply",
    async ({ request, params }) => {
      if (localStorage.getItem("aep:mock:project:apply") === "error") {
        return HttpResponse.json(applyFilesError, { status: 500 });
      }
      const body = (await request.json()) as ApplyRequest;
      const writes = body.writes ?? [];
      const invalid =
        writes.length === 0
          ? "empty apply (no writes or deletes)"
          : writes.find((w) => !w.path.startsWith("specs/"))
            ? "only specs/ paths are accessible via this API"
            : null;
      if (invalid) {
        return HttpResponse.json(
          { code: "invalid_path", message: invalid } satisfies ApiError,
          { status: 400 },
        );
      }
      const files = recordAppliedFiles(String(params.projectName), writes);
      return HttpResponse.json({
        commitSha: files[0]?.sha ?? "0000000000000000000000000000000000000000",
        files,
      } satisfies ApplyResult);
    },
  ),
];
