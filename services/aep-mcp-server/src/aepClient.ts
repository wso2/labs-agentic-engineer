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
 * Thin wrapper over aep-api's `/api/v1/projects/{projectName}/issues`
 * endpoints. Every call forwards the caller's bearer as-is — this server holds
 * no credentials of its own; aep-api's org-scoped JWT verification is the only
 * auth boundary. See AE-HANDOFF-DESIGN.md (openchoreo/agents/sre-agent) §4/§9.
 *
 * There is no separate dispatch call: creating an issue IS the dispatch, when
 * aep-api's own classification says it should be. aep-api files the issue into
 * the deployed version's milestone and starts (or wakes) the run in one write,
 * so there is no window in which the issue exists but nothing will work it.
 * Adoption has no caller-side override — CreateIssueRequest carries no such
 * property; forwarding one 400s the whole request.
 */

export interface AepClientOptions {
  baseUrl: string;
  bearer: string;
}

export interface IssueResult {
  number: number;
  url: string;
  nodeId: string;
  /** True when an open issue for the same server-owned incident key already existed — number/url refer to that issue and nothing was created. */
  deduped?: boolean;
  /** True when the issue was filed into a version's milestone as agent work and a run was started or woken over it. */
  adopted?: boolean;
  /** Why adoption did not happen, when it was asked for and did not. The issue still exists as a ledger entry. */
  adoptionError?: string;
  /** True when the dedupe key matched a CLOSED issue: the same incident recurring after a fix was merged. That issue was reopened with this call's body appended. */
  reopened?: boolean;
  /** Which attempt this is — 1 on a first filing, 2 on the first recurrence. Present with `reopened`, and on a dedupe onto an issue that already carries recurrences. */
  recurrence?: number;
  /** True when nothing was filed because an issue under this key already carries a no-change verdict: somebody with the repo in front of them already decided this signature needs no code change. */
  suppressed?: boolean;
  /** The handoff classification aep-api derived from `actionStatuses` — code-level, config-level, mixed, or none. Absent when the call sent no statuses. `config-level` is the one value that files without adopting. */
  classification?: string;
}

export interface IssueInfo {
  Number: number;
  Title: string;
  Body: string;
  URL: string;
  State: string;
  Labels: string[];
}

export class AepApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "AepApiError";
  }
}

async function request<T>(
  opts: AepClientOptions,
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  // Built incrementally (rather than `body: body === undefined ? undefined : ...`)
  // because exactOptionalPropertyTypes rejects explicitly assigning `undefined`
  // to RequestInit's optional `body` — omitting the key entirely is required.
  const init: RequestInit = {
    method,
    headers: {
      "content-type": "application/json",
      authorization: opts.bearer,
    },
  };
  if (body !== undefined) {
    init.body = JSON.stringify(body);
  }
  const res = await fetch(`${opts.baseUrl}/api/v1${path}`, init);

  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new AepApiError(res.status, text || `aep-api request failed: ${res.status}`);
  }

  // Some aep-api responses carry no body at all (204, and 202 for accepted-async
  // commands) — so key off actual content rather than a hardcoded status list.
  const text = await res.text();
  if (!text) return undefined as T;
  return JSON.parse(text) as T;
}

export function createIssue(
  opts: AepClientOptions,
  project: string,
  req: {
    title: string;
    body: string;
    labels?: string[];
    componentName?: string;
    /** One entry per recommended action on the RCA report, in its order, null where the remediation agent set no status. aep-api derives the classification and the adoption from these; omitting the field entirely leaves both alone. */
    actionStatuses?: (string | null)[];
  },
): Promise<IssueResult> {
  return request<IssueResult>(opts, "POST", `/projects/${encodeURIComponent(project)}/issues`, req);
}

export function listIssues(
  opts: AepClientOptions,
  project: string,
  filters: { labels?: string[]; query?: string } = {},
): Promise<IssueInfo[]> {
  const params = new URLSearchParams();
  if (filters.labels?.length) params.set("labels", filters.labels.join(","));
  if (filters.query) params.set("q", filters.query);
  const qs = params.toString();
  const path = `/projects/${encodeURIComponent(project)}/issues${qs ? `?${qs}` : ""}`;
  return request<IssueInfo[]>(opts, "GET", path);
}
