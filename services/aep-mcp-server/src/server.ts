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

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

import { AepApiError, type AepClientOptions, createIssue, listIssues } from "./aepClient.js";
import { resolveHandoff } from "./handoffContext.js";
import { annotatePlatformIssues } from "./platformIssues.js";

function textResult(payload: unknown) {
  return { content: [{ type: "text" as const, text: JSON.stringify(payload) }] };
}

function errorResult(err: unknown) {
  const message = err instanceof AepApiError ? `aep-api ${err.status}: ${err.message}` : String(err);
  return { content: [{ type: "text" as const, text: message }], isError: true };
}

/**
 * Builds an McpServer bound to one caller's bearer token. Called once per
 * incoming HTTP request (see main.ts) — this server holds no credentials or
 * state of its own; every tool call forwards `client.bearer` straight
 * through to aep-api, which performs the actual org-scoped auth check.
 *
 * Two tools, not three. There is no dispatch tool: adoption moved into
 * create-issue, so filing an issue and handing it to the coding agent are one
 * call and cannot come apart. The other way to adopt an issue that already
 * exists is the `aep` arming GitHub label, which AE's event plane watches —
 * a human's route, not this server's. Adoption itself is entirely aep-api's
 * call (classification-driven — see CreateIssueRequest in
 * packages/contracts/api/v1/openapi.yaml, which has no adopt-like property):
 * there is no operator or caller override to forward here.
 */
export function createAepMcpServer(
  client: AepClientOptions,
  create: typeof createIssue = createIssue,
): McpServer {
  const server = new McpServer({ name: "aep-mcp-server", version: "0.0.0" });

  server.registerTool(
    "search_related_issues",
    {
      title: "Search related AE issues",
      description:
        "Search existing GitHub issues on a project's repo to find related/duplicate issues before filing a new one. " +
        "Keyword-ranked: pass space-separated keywords (component name + symptom terms), not a sentence; results come back ranked by keyword overlap for you to judge. " +
        "An issue marked `PlatformRecord: true` is AE's own plan for what to BUILD, not a defect report: read `ReadAs` on it before you treat it as evidence about whether something is broken.",
      inputSchema: {
        project: z.string().describe("OpenChoreo/AE project name"),
        query: z
          .string()
          .optional()
          .describe(
            "Space-separated keywords (e.g. 'service1 service2 timeout'), NOT a natural-language phrase. Tokenised and matched against issue title/body; issues are returned ranked by how many keywords they contain. Omit to list all issues.",
          ),
        labels: z.array(z.string()).optional().describe("Filter by GitHub labels"),
      },
    },
    async ({ project, query, labels }) => {
      try {
        const issues = await listIssues(client, project, {
          ...(query !== undefined ? { query } : {}),
          ...(labels !== undefined ? { labels } : {}),
        });
        return textResult(annotatePlatformIssues(issues));
      } catch (err) {
        return errorResult(err);
      }
    },
  );

  server.registerTool(
    "create_issue",
    {
      title: "Create a GitHub issue via AE",
      description:
        "Create a GitHub issue on a project's repo and hand it to AE for downstream handling. Creating the issue IS the hand-off — there is no second call. " +
        "File every report that reaches you. What the work IS, and whether a coding agent gets it, are not yours to decide: the platform derives both automatically and answers the classification it chose. " +
        "A `config-level` answer means the remediation agent already expressed every action as configuration — the issue is still filed, as a ledger entry, and nothing is dispatched over it. " +
        "Deduplication is automatic and server-owned: aep-api derives the stable incident key from the trusted request context and validated create fields, so if an OPEN issue for the same incident exists it is returned with `deduped: true`, nothing is created, and nothing is dispatched (the run that created that issue owns its dispatch). An issue already carrying a no-change verdict for this incident answers `suppressed: true`, and nothing is created. " +
        "If instead a CLOSED issue with that key is found — the same component's failure recurring after a fix was merged — it is reopened with this call's body appended as a `## Recurrence <n>` section, moved into the currently deployed version's milestone and handed back to the coding agent; the result then carries `reopened: true` and `recurrence` (which attempt this is). " +
        "The result's `adopted` says whether anything will actually work the issue, and `adoptionError` says why not when it will not — a project with no built version yet gets its issue recorded but not worked. Those outcomes are decided here and in aep-api code, not by the caller's skill.",
      inputSchema: {
        project: z.string().describe("OpenChoreo/AE project name"),
        title: z.string().describe("Issue title"),
        body: z.string().describe("Issue body (markdown)"),
        labels: z.array(z.string()).optional().describe("GitHub labels to apply"),
        componentName: z
          .string()
          .optional()
          .describe(
            "The component this issue is about. AE's design names it unprefixed ('service1'), and a name carrying its project prefix ('myproject-service1') is resolved to the design name for you, so pass whichever your world uses. Checked before the issue is filed — a name the design carries under neither form fails this call rather than surfacing later inside a coding cycle.",
          ),
        actionStatuses: z
          .array(z.enum(["revised", "suggested"]).nullable())
          .describe(
            "Your own remediation verdict for each of the RCA report's recommended_actions, in that same order: 'revised' when you expressed it as an OpenChoreo config change, 'suggested' when you could not, null for one you did not address. Required on every call — this is what AE classifies code-level vs config-level vs none from.",
          ),
      },
    },
    async ({ project, title, body, labels, componentName, actionStatuses }) => {
      try {
        const resolved = resolveHandoff({
          project,
          ...(componentName !== undefined ? { componentName } : {}),
          ...(labels !== undefined ? { labels } : {}),
          actionStatuses,
        });
        const issue = await create(client, resolved.project, {
          title,
          body,
          labels: resolved.labels,
          ...(resolved.componentName !== undefined ? { componentName: resolved.componentName } : {}),
          actionStatuses: resolved.actionStatuses,
        });
        return textResult(issue);
      } catch (err) {
        return errorResult(err);
      }
    },
  );

  return server;
}
