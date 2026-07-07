import { http, HttpResponse } from "msw";
import type { components } from "../../generated/aep-api";
import {
  deleteProjectError,
  duplicateProjectError,
  emptyProjects,
  orgConfig,
  projectsError,
  PROJECTS_PAGE_SIZE,
  seedProjects,
  type ProjectsScenario,
} from "../fixtures/projects";

type Project = components["schemas"]["Project"];
type CreateProjectRequest = components["schemas"]["CreateProjectRequest"];

function scenario(): ProjectsScenario {
  return (
    (localStorage.getItem("aep:mock:projects") as ProjectsScenario | null) ??
    "empty"
  );
}

// Projects created through the UI in this session; layered on top of the
// scenario's seed so the create flow behaves statefully in mock mode.
const createdProjects: Project[] = [];

// Names deleted through the UI in this session — masks seed AND created
// entries so the delete flow behaves statefully in mock mode (#107).
const deletedProjects = new Set<string>();

function currentProjects(): Project[] {
  const seed = scenario() === "some" ? seedProjects : [];
  return [...seed, ...createdProjects].filter(
    (p) => !deletedProjects.has(p.name),
  );
}

export const projectsHandlers = [
  // Wildcard prefix: matches whatever runtime API base URL sits in front of
  // /api/v1.
  http.get("*/api/v1/projects", ({ request }) => {
    if (scenario() === "error") {
      return HttpResponse.json(projectsError, {
        status: 500,
        headers: { "Content-Type": "application/problem+json" },
      });
    }
    const params = new URL(request.url).searchParams;
    const search = params.get("search")?.trim();
    const matches = currentProjects().filter((p) => {
      if (!search) return true;
      const q = search.toLowerCase();
      return (
        p.name?.toLowerCase().includes(q) ||
        p.displayName?.toLowerCase().includes(q)
      );
    });
    if (matches.length === 0 && !search) {
      return HttpResponse.json(emptyProjects);
    }
    // The cursor is opaque to the client; the mock's is a plain offset.
    const offset = Number(params.get("cursor") ?? 0) || 0;
    const limit = Number(params.get("limit")) || PROJECTS_PAGE_SIZE;
    const items = matches.slice(offset, offset + limit);
    const next = offset + limit;
    return HttpResponse.json({
      items,
      ...(next < matches.length && { nextCursor: String(next) }),
    });
  }),

  http.post("*/api/v1/projects", async ({ request }) => {
    const body = (await request.json()) as CreateProjectRequest;
    if (currentProjects().some((p) => p.name === body.name)) {
      return HttpResponse.json(duplicateProjectError, {
        status: 409,
        headers: { "Content-Type": "application/problem+json" },
      });
    }
    const project: Project = {
      name: body.name,
      displayName: body.displayName ?? body.name,
      ...(body.description !== undefined && { description: body.description }),
      status: "active",
      createdAt: new Date().toISOString(),
    };
    createdProjects.push(project);
    return HttpResponse.json(project, { status: 201 });
  }),

  http.get("*/api/v1/projects/:projectName", ({ params }) => {
    const project = currentProjects().find(
      (p) => p.name === params.projectName,
    );
    if (!project) {
      return HttpResponse.json(
        {
          type: "about:blank",
          status: 404,
          title: "Not Found",
          detail: `Project ${String(params.projectName)} not found`,
        },
        { status: 404, headers: { "Content-Type": "application/problem+json" } },
      );
    }
    return HttpResponse.json(project);
  }),

  // delete-project (#107). Error state via
  // localStorage.setItem('aep:mock:projects:delete', 'error').
  http.delete("*/api/v1/projects/:projectName", ({ params }) => {
    if (localStorage.getItem("aep:mock:projects:delete") === "error") {
      return HttpResponse.json(deleteProjectError, {
        status: 500,
        headers: { "Content-Type": "application/problem+json" },
      });
    }
    const name = String(params.projectName);
    if (!currentProjects().some((p) => p.name === name)) {
      return HttpResponse.json(
        {
          type: "about:blank",
          status: 404,
          title: "Not Found",
          detail: `Project ${name} not found`,
        },
        { status: 404, headers: { "Content-Type": "application/problem+json" } },
      );
    }
    deletedProjects.add(name);
    return new HttpResponse(null, { status: 204 });
  }),

  // get-config: the create flow reads the connected GitHub org for the
  // repo-URL preview from the org-config projection.
  http.get("*/api/v1/config", () => HttpResponse.json(orgConfig)),
];
