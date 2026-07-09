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

import { useQuery } from "@tanstack/react-query";
import { client } from "../../../api/client";
import { specKeys } from "./keys";
import { toRepoPath, toSpecEntries } from "./mapping";

// The file list fills in while agents derive the spec, so it polls at the
// same cadence as the overview's reads (#77 decision: 10s).
const SPEC_POLL_MS = 10_000;

function toError(error: unknown, fallback: string): Error {
  const e = error as { detail?: string; title?: string } | undefined;
  return new Error(e?.detail ?? e?.title ?? fallback);
}

/** Spec file metadata at HEAD (#113): list-files, mapped to the view model. */
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
    refetchInterval: SPEC_POLL_MS,
  });
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
    queryFn: async () => {
      if (!file) throw new Error("no file selected");
      // openapi-fetch can't substitute Huma's `{path...}` wildcard (its
      // serializer only knows `{name}`/`{name*}`, and both percent-encode
      // the slashes the wildcard exists to keep). Build the concrete URL,
      // cast to the contract path so the response stays typed.
      const repoPath = toRepoPath(file.path)
        .split("/")
        .map(encodeURIComponent)
        .join("/");
      const { data, error } = await client.GET(
        `/projects/${encodeURIComponent(projectName)}/files/${repoPath}` as "/projects/{projectName}/files/{path...}",
        { params: { path: { projectName, path: toRepoPath(file.path) } } },
      );
      if (error || data === undefined)
        throw toError(error, `Failed to load ${file.path}`);
      return data;
    },
  });
}
