import { http, HttpResponse } from "msw";
import {
  issuesError,
  seedIssues,
  type IssuesScenario,
} from "../fixtures/issues";

function scenario(): IssuesScenario {
  return (
    (localStorage.getItem("aep:mock:issues") as IssuesScenario | null) ?? "some"
  );
}

export const issuesHandlers = [
  http.get("*/api/v1/projects/:projectName/issues", ({ request }) => {
    if (scenario() === "error") {
      return HttpResponse.json(issuesError, { status: 500 });
    }
    if (scenario() === "empty") {
      return HttpResponse.json([]);
    }
    const labels = new URL(request.url).searchParams.get("labels");
    if (!labels) {
      return HttpResponse.json(seedIssues);
    }
    const required = labels.split(",").map((label) => label.trim()).filter(Boolean);
    return HttpResponse.json(
      seedIssues.filter((issue) =>
        required.every((label) => (issue.Labels ?? []).includes(label)),
      ),
    );
  }),
];
