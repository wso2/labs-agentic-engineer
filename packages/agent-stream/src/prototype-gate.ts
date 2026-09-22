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
 * The write gate for a web-application component's
 * `specs/design/components/<component>/prototype.json`.
 *
 * The rules are `@aep/prototype-model`'s — the same `parsePrototypeModel` the
 * console reads with, and the same shape and codes the Go save gate applies —
 * so this module only decides how a refusal reads to the model:
 *
 *  - `INVALID_JSON` — the body does not parse;
 *  - `PROTOTYPE_COMPONENT_MISMATCH` — `component` disagrees with the directory
 *    (its own code, because the fix is a different move: the file is probably
 *    at the wrong path, not wrong inside);
 *  - `INVALID_PROTOTYPE` — anything else the validator refuses, with each
 *    finding's own code and JSON path in the message.
 */

import {
  parsePrototypeModel,
  prototypeArtifactComponent,
  type PrototypeValidationIssue,
} from "@aep/prototype-model";

export interface PrototypeProblem {
  code: "INVALID_JSON" | "INVALID_PROTOTYPE" | "PROTOTYPE_COMPONENT_MISMATCH";
  message: string;
}

/** Same move as every other gate: a refused write created nothing to edit. */
const REMEDY =
  "The file is unchanged — the write was refused, so this path holds nothing new to edit. " +
  "Fix what this names and re-emit the WHOLE corrected document in ONE retry with addFile " +
  "(removeFile first only if the file already existed).";

/** How many findings a refusal lists before summarizing the rest. */
const MAX_LISTED = 8;

/**
 * Validate a candidate prototype body for `path`. Returns null when `path` is
 * not a component prototype or the content is valid; otherwise the problem,
 * phrased for the model's self-correction.
 */
export function checkPrototype(path: string, content: string): PrototypeProblem | null {
  const component = prototypeArtifactComponent(path);
  if (component === undefined) return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(content);
  } catch (e) {
    return {
      code: "INVALID_JSON",
      message: `${path} is not valid JSON: ${e instanceof Error ? e.message : String(e)}. ${REMEDY}`,
    };
  }

  const result = parsePrototypeModel(parsed, { component });
  if (result.ok) return null;

  const mismatch = result.issues.some((i) => i.code === "PROTOTYPE_COMPONENT_MISMATCH");
  return {
    code: mismatch ? "PROTOTYPE_COMPONENT_MISMATCH" : "INVALID_PROTOTYPE",
    message: `${path} is not a valid prototype — ${describe(result.issues)}. ${REMEDY}`,
  };
}

function describe(issues: PrototypeValidationIssue[]): string {
  const listed = issues.slice(0, MAX_LISTED).map((i) => `${i.code} at ${i.path || "(root)"}: ${i.message}`);
  const more = issues.length - listed.length;
  return more > 0 ? `${listed.join("; ")}; and ${more} more` : listed.join("; ");
}
