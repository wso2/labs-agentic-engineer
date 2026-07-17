import { http, HttpResponse, type JsonBodyType } from "msw";
import {
  componentDeployments,
  componentOpenApi,
  projectBuilds,
  projectComponents,
  projectSectionError,
  projectSpecFiles,
  projectStatuses,
  projectTags,
  projectTasks,
  specFileContent,
  specFileMetas,
  specFileNotFound,
  type ProjectScenario,
} from "../fixtures/project";
import {
  isSettledStatus,
  liveLine,
  streamFrames,
  taskDetailOf,
} from "../fixtures/task-log";
import { estimateFor, projectUsage } from "../fixtures/usage";

function scenario(): ProjectScenario {
  return (
    (localStorage.getItem("aep:mock:project") as ProjectScenario | null) ??
    "building"
  );
}

function respond<T extends JsonBodyType>(
  pick: (s: Exclude<ProjectScenario, "error">) => T,
) {
  const s = scenario();
  if (s === "error") {
    return HttpResponse.json(projectSectionError, {
      status: 500,
      headers: { "Content-Type": "application/problem+json" },
    });
  }
  return HttpResponse.json(pick(s));
}

// Project-scoped reads backing the overview page (issue #77). The project
// itself (GET /projects/:projectName) is served by handlers/projects.ts.
export const projectHandlers = [
  http.get("*/api/v1/projects/:projectName/status", () =>
    respond((s) => projectStatuses[s]),
  ),
  http.get("*/api/v1/projects/:projectName/components", () =>
    respond((s) => projectComponents[s]),
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
  // Builds page (#185): the per-tag build history.
  http.get("*/api/v1/projects/:projectName/builds", () =>
    respond((s) => projectBuilds[s]),
  ),
  // Cost visibility (#245): per-project usage rollups…
  http.get("*/api/v1/projects/:projectName/usage", () =>
    respond((s) => projectUsage[s]),
  ),
  // …and the pre-action historical-average estimate (null on cold start).
  http.get("*/api/v1/projects/:projectName/estimates", ({ request }) => {
    const action = new URL(request.url).searchParams.get("action");
    return respond((s) => estimateFor(s, action));
  }),
  // Task page (#173): one task with its execution history…
  http.get("*/api/v1/projects/:projectName/tasks/:issueNumber", ({ params }) => {
    const s = scenario();
    if (s === "error") {
      return HttpResponse.json(projectSectionError, {
        status: 500,
        headers: { "Content-Type": "application/problem+json" },
      });
    }
    const detail = taskDetailOf(s, Number(params.issueNumber));
    if (!detail) {
      return HttpResponse.json(
        {
          type: "about:blank",
          status: 404,
          title: "Not Found",
          detail: `no task #${String(params.issueNumber)}`,
        },
        { status: 404, headers: { "Content-Type": "application/problem+json" } },
      );
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
          headers: { "Content-Type": "application/problem+json" },
        });
      }
      const issueNumber = Number(params.issueNumber);
      const frames = streamFrames(s, issueNumber);
      const task = projectTasks[s].find((t) => t.issueNumber === issueNumber);
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
            await delay(120);
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
  // as aep-api serves them (repo-relative specs/ paths).
  http.get("*/api/v1/projects/:projectName/files", () =>
    respond((s) => specFileMetas(projectSpecFiles[s])),
  ),
  http.get("*/api/v1/projects/:projectName/files/*", ({ request }) => {
    const s = scenario();
    if (s === "error") {
      return HttpResponse.json(projectSectionError, {
        status: 500,
        headers: { "Content-Type": "application/problem+json" },
      });
    }
    const pathname = new URL(request.url).pathname;
    const path = decodeURIComponent(pathname.replace(/^.*\/files\//, ""));
    const file = specFileContent(projectSpecFiles[s], path);
    if (!file) {
      return HttpResponse.json(specFileNotFound(path), {
        status: 404,
        headers: { "Content-Type": "application/problem+json" },
      });
    }
    return HttpResponse.json(file);
  }),
];
