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

// The guard's DECISION, separate from the plugin plumbing so it is testable in
// plain node against plain objects.
//
// Every rule and every sentence here is the platform's own, imported from the
// modules the Claude Code hooks are built from — `authoredPathDenial` and
// `allowsWriteOutsideProject` (workspace_guard.ts), `webSearchDenial`
// (websearch_dlp.ts), `webFetchDenial` (webfetch_guard.ts). The plugin build
// bundles them into one dependency-free file, so the two runtimes cannot
// disagree about what is refused or what the agent is told. The only thing
// written here is how OpenCode's tools spell their arguments.

import { obj, str } from "../../fields.js";
import { allowsWriteOutsideProject, authoredPathDenial } from "../../../lib/workspace_guard.js";
import { webFetchDenial } from "../../../lib/webfetch_guard.js";
import { webSearchDenial } from "../../../lib/websearch_dlp.js";
import { patchPaths } from "../tools.js";
import { WORKSPACE_GUARD_MARKER } from "./protocol.js";

/** Everything the decision depends on, captured once when the plugin loads. */
export interface GuardInputs {
  workspace: string;
  secrets: readonly string[];
}

/** One tool call's arguments → the sentence to deny it with, or null to let it run. */
export type GuardDecision = (tool: string, args: unknown) => string | null;

/** The paths one authoring call writes, by OpenCode's argument spelling. */
function authoredPaths(tool: string, args: Record<string, unknown>): string[] {
  if (tool === "apply_patch") return patchPaths(str(args.patchText));
  const target = str(args.filePath);
  return target ? [target] : [];
}

/**
 * Build the decision for one run.
 *
 * The write rule is `allowsWriteOutsideProject` evaluated in the plugin's own
 * process — the SAME function the runner puts on `RuntimePolicy.write`, reading
 * the same `HOME` and temp directory, because the server inherits the run's
 * environment. The two egress rules are rebuilt from the staged-secret values
 * the runner's own predicates were built from (`stagedSecretValues` over the
 * same env), handed over in a file because a closure cannot cross a process.
 */
export function createGuardDecision(inputs: GuardInputs): GuardDecision {
  const searchDenial = webSearchDenial(inputs.secrets);
  const fetchDenial = webFetchDenial(inputs.secrets);
  return (tool, rawArgs) => {
    const args = obj(rawArgs);
    switch (tool) {
      case "edit":
      case "write":
      case "apply_patch": {
        for (const target of authoredPaths(tool, args)) {
          const reason = authoredPathDenial(tool, target, inputs.workspace, allowsWriteOutsideProject);
          if (reason) return `${WORKSPACE_GUARD_MARKER} ${reason}`;
        }
        return null;
      }
      case "websearch":
        return searchDenial(str(args.query));
      case "webfetch":
        return fetchDenial(str(args.url));
      default:
        return null;
    }
  };
}
