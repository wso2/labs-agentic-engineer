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
import {
  parsePrototypeModel,
  prototypeArtifactPath,
  type PrototypeIssueCode,
  type PrototypeModelV1,
} from "@aep/prototype-model";
import { fetchSpecFileContent } from "../../spec/api/queries";
import { ApiRequestError } from "../../../api/errors";
import { prototypeKeys } from "./keys";

/** A finding that keeps the prototype from rendering. */
export interface PrototypeReadIssue {
  /** The shared model's code, or `INVALID_JSON` when the file is not JSON at all. */
  code: PrototypeIssueCode | "INVALID_JSON";
  /** Where, as `screens[0].content[2].id`; empty for the document root. */
  path: string;
  message: string;
}

/**
 * A prototype file that exists: either a model to render or the coded issues
 * that stop it from rendering. Never both — a file that fails any check
 * renders nothing.
 */
export type PrototypeRead =
  | { ok: true; model: PrototypeModelV1; sha: string }
  | { ok: false; issues: PrototypeReadIssue[]; sha: string };

/**
 * A web-application component's `prototype.json`, read through the existing
 * read-file operation (which already returns the blob sha) and parsed with the
 * shared model — the same checks the agent's write gate and the save gate run,
 * including that the file's `component` matches its directory.
 *
 * Resolves to `null` when the component has no prototype (the read answers
 * `not_found`); any other failed read is the query's error.
 */
export function usePrototype(projectName: string, component: string) {
  return useQuery({
    queryKey: prototypeKeys.file(projectName, component),
    queryFn: async (): Promise<PrototypeRead | null> => {
      let file;
      try {
        // The sha is only the spec cache's key; an empty one reads the tip.
        file = await fetchSpecFileContent(projectName, { path: prototypeArtifactPath(component), sha: "" });
      } catch (e) {
        if (e instanceof ApiRequestError && e.code === "not_found") return null;
        throw e;
      }
      return readPrototype(file.content, file.sha, component);
    },
    // Freshness is driven by the feedback turn's invalidation, not by polling.
    staleTime: Infinity,
  });
}

function readPrototype(content: string, sha: string, component: string): PrototypeRead {
  let value: unknown;
  try {
    value = JSON.parse(content);
  } catch (e) {
    return {
      ok: false,
      sha,
      issues: [{ code: "INVALID_JSON", path: "", message: e instanceof Error ? e.message : String(e) }],
    };
  }
  const result = parsePrototypeModel(value, { component });
  return result.ok ? { ok: true, model: result.model, sha } : { ok: false, issues: result.issues, sha };
}
