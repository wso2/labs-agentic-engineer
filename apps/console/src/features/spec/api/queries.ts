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

import {
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { components } from "../../../generated/aep-api";
import { client } from "../../../api/client";
import { specKeys } from "./keys";
import { toSpecEntries } from "./mapping";
import { scheduleFreshnessPoll } from "./dependencyFreshness";
import { ApiRequestError } from "../../../api/errors";

type FileContent = components["schemas"]["FileContent"];
type ComponentDependencies = components["schemas"]["ComponentDependencies"];

// ApiRequestError, not Error: it keeps the envelope's `code` alongside the same
// message every existing caller already reads. The Validation page needs it to tell
// a file that is genuinely ABSENT (`not_found` — a version whose spec authored no
// criteria) from a read that merely failed, which decides whether the page explains
// itself or offers a retry. Branching on the code rather than the message, which the
// BFF owns and may reword.
function toError(error: unknown, fallback: string): Error {
  return new ApiRequestError(error, fallback);
}

/**
 * Spec file metadata at HEAD (#113): list-files, mapped to the view model.
 * A ONE-SHOT load — the committed snapshot for first paint and the fallback
 * while collab is offline / a room seed failed. Live changes (agent-created
 * files, edits) arrive through the collab doc, not this query, so there is no
 * poll (SpecView unions this with the live doc list). Out-of-room commits
 * won't reflect until reload — the parked external-merge concern (#86).
 */
export function useSpecFiles(projectName: string) {
  return useQuery({
    queryKey: specKeys.files(projectName),
    queryFn: async () => {
      const { data, error } = await client.GET(
        "/projects/{projectName}/files",
        { params: { path: { projectName } } },
      );
      if (error) throw toError(error, "Failed to load the spec files");
      return toSpecEntries(data ?? []);
    },
    staleTime: Infinity,
  });
}

/**
 * Read-time dependency status for every component in the current design
 * (#252 Task 2's `GET /projects/{p}/design/dependencies` — the console's
 * single dependency-status read model). Keyed off `specKeys.dependencies` —
 * the EXACT key Task 5's turn-end freshness wiring
 * (`dependencyFreshness.ts`/`useTurnEndFlush`) invalidates after a "Resolve
 * via chat" turn commits, so a different key here would silently break that
 * refresh.
 *
 * Resolves to `[]` for a project with no design yet — a real, empty answer.
 * A FETCH ERROR is not that answer and no longer pretends to be: it surfaces as
 * `isError`, because "this project declares no dependencies" and "the console
 * could not find out" are different facts and at least one caller acts on the
 * difference. The Builds page's External resources section (ADR-0023) is told
 * to collect values a parked deploy is waiting on; swallowing the error there
 * made the whole section vanish, so a run held on missing values pointed at a
 * section that was not on the page.
 *
 * Callers that only decorate — the Spec view's dependency status chips, the
 * Deployments page's promotion readiness — degrade at their own use sites
 * (`data ?? []`), so a failed read still renders their content without the
 * decoration. That is a per-caller choice now, not one this hook makes for
 * everybody. Surfacing the error also means the query gets the client's default
 * retries, so a transient failure recovers instead of being cached as "[]".
 *
 * `staleTime: Infinity` keeps it from refetching on remount/refocus; freshness
 * is driven entirely by the explicit turn-end invalidation, not polling.
 */
export function useDesignDependencies(projectName: string) {
  return useQuery({
    queryKey: specKeys.dependencies(projectName),
    queryFn: async (): Promise<ComponentDependencies[]> => {
      const { data, error } = await client.GET(
        "/projects/{projectName}/design/dependencies",
        { params: { path: { projectName } } },
      );
      if (error) throw toError(error, "Failed to load the design dependencies");
      // A design-less project answers with no body — that IS "no dependencies".
      return data ?? [];
    },
    staleTime: Infinity,
  });
}

/**
 * Fetch one spec file's content. Shared by the lazy selection hook below, the
 * derived wireframe hook (useDerivedWireframe), and the cell-diagram panel's
 * solo/offline design.cell read — reads outside a single "selected file"
 * context.
 */
export async function fetchSpecFileContent(
  projectName: string,
  file: { path: string; sha: string; ref?: string },
): Promise<FileContent> {
  // The contract's {path} param is a trailing wildcard — it spans multiple
  // segments, and openapi-fetch's serializer would percent-encode the very
  // slashes the wildcard exists to keep. Build the concrete URL, cast to
  // the contract path so the response stays typed. `file.path` is already
  // the full repo path (specs/…) the Files API expects.
  //
  // `file.ref` pins the read to one commit. Omitted reads the branch tip, which is
  // what spec reads want; the validation report needs the pin, because it sits at a
  // fixed path every run overwrites and the tip would answer for the newest run.
  const repoPath = file.path
    .split("/")
    .map(encodeURIComponent)
    .join("/");
  const { data, error } = await client.GET(
    `/projects/${encodeURIComponent(projectName)}/files/${repoPath}` as "/projects/{projectName}/files/{path}",
    {
      params: {
        path: { projectName, path: file.path },
        ...(file.ref ? { query: { ref: file.ref } } : {}),
      },
    },
  );
  if (error || data === undefined)
    throw toError(error, `Failed to load ${file.path}`);
  return data;
}

/**
 * Content of one spec file, fetched lazily for the selection (#113
 * decision 4). Immutable per (path, sha) — see specKeys.file.
 */
export function useSpecFileContent(
  projectName: string,
  file: { path: string; sha: string } | null,
) {
  return useQuery({
    queryKey: specKeys.file(projectName, file?.path ?? "", file?.sha ?? ""),
    enabled: file !== null,
    staleTime: Infinity,
    queryFn: () => {
      if (!file) throw new Error("no file selected");
      return fetchSpecFileContent(projectName, file);
    },
  });
}

/**
 * Content of SEVERAL spec files at once, as `path → content` for the ones that
 * have arrived.
 *
 * The plural exists because some readers need a set of files whose SIZE depends
 * on what they are reading — the Security page's cross-checks read the OpenAPI
 * and wireframes of whichever components the security document names — and a
 * hook cannot be called a variable number of times. Same key, same fetch and
 * the same immutable-per-(path, sha) caching as `useSpecFileContent`, so a file
 * one of these pulls in is free for the single-file hook and vice versa.
 *
 * Files still loading or failed are simply absent from the map: every caller so
 * far degrades (a cross-check that cannot read a sibling spec is skipped, not
 * failed), and a per-file status nobody reads would be state to keep correct
 * for nothing.
 */
export function useSpecFileContents(
  projectName: string,
  files: { path: string; sha: string }[],
): Record<string, string> {
  return useQueries({
    queries: files.map((file) => ({
      queryKey: specKeys.file(projectName, file.path, file.sha),
      staleTime: Infinity,
      queryFn: () => fetchSpecFileContent(projectName, file),
    })),
    // `combine` runs inside react-query's own memo over the results array, so
    // the identity below is stable while the answers are — which is what lets a
    // caller put the map in a dependency array.
    combine: (results) => {
      const out: Record<string, string> = {};
      results.forEach((result, i) => {
        const path = files[i]?.path;
        if (path !== undefined && result.data) out[path] = result.data.content;
      });
      return out;
    },
  });
}

/**
 * The definition view's "provide the interface": a URL the platform fetches, or
 * the document itself (pasted or dropped). The platform validates, normalizes
 * and commits it into the dependency's directory and records it in
 * dependency.json. Both the file list and the dependency read model change,
 * so both refresh.
 */
export function useProvideDependencyContract(projectName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: { depName: string; url?: string; content?: string }) => {
      const { data, error } = await client.POST(
        "/projects/{projectName}/dependencies/{depName}/contract",
        {
          params: { path: { projectName, depName: input.depName } },
          body: {
            ...(input.url ? { url: input.url } : {}),
            ...(input.content ? { content: input.content } : {}),
          },
        },
      );
      if (error || data === undefined) throw toError(error, "Failed to provide the contract");
      return data;
    },
    onSuccess: () => {
      // The write lands through the Files API and the read model is served
      // from the repo's HEAD, which follows a moment later — the same shape a
      // turn end has, so the same immediate-then-later refresh.
      scheduleFreshnessPoll(queryClient, projectName);
      void queryClient.invalidateQueries({ queryKey: specKeys.files(projectName) });
    },
  });
}

/** The user's permission to build against a contract the agent wrote. */
export function useAcceptDependencyAssumption(projectName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: { depName: string; note?: string }) => {
      const { error } = await client.POST(
        "/projects/{projectName}/dependencies/{depName}/assumption",
        {
          params: { path: { projectName, depName: input.depName } },
          body: input.note ? { note: input.note } : {},
        },
      );
      if (error) throw toError(error, "Failed to accept the assumption");
    },
    onSuccess: () => {
      scheduleFreshnessPoll(queryClient, projectName);
      void queryClient.invalidateQueries({ queryKey: specKeys.files(projectName) });
    },
  });
}
