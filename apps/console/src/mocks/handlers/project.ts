import { http, HttpResponse, type JsonBodyType } from "msw";
import {
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
  http.get("*/api/v1/projects/:projectName/tasks", () =>
    respond((s) => projectTasks[s]),
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
