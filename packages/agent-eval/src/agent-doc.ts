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

import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { parse } from "yaml";

/** One provider contract to stub, and the env var that addresses it. */
export interface ToolStub {
  /** The agent's injected base-address variable, e.g. `LUNCH_API_URL`. */
  envVar: string;
  /** Absolute path to the provider's committed `openapi.yaml`. */
  specPath: string;
  /** The operationIds this agent may call — its security boundary. */
  allow: string[];
}

const FRONT_MATTER = /^---\r?\n([\s\S]*?)\r?\n---(\r?\n|$)/;
const ENV_REFERENCE = /^\$\{env:([A-Z0-9_]+)\}$/;

interface OpenApiTool {
  component?: unknown;
  baseUrl?: unknown;
  allow?: unknown;
}

/**
 * Reads an `agent.afm.md`'s FRONT MATTER only — never its body.
 *
 * The body is the prompt, and the plan forbids evaluation from deriving
 * anything from it: scenarios come from `specs/requirements/`, and reading
 * the prompt here would let the harness grade an agent against its own
 * wording. Only `x-aep.tools.openapi[]` is taken, because it says which
 * provider contracts must be stubbed and which variable addresses each.
 *
 * Every entry must resolve or this throws. A tool contract that quietly fails
 * to resolve becomes an agent booted without that address, whose failures
 * then read as bad behaviour rather than as a harness that did not wire it.
 */
export function readToolStubs(afmPath: string): ToolStub[] {
  const source = readFileSync(afmPath, "utf8");
  const matched = FRONT_MATTER.exec(source);
  if (matched === null) {
    throw new Error(`agent-eval: ${afmPath} has no YAML front matter`);
  }
  const doc = parse(matched[1]!) as { "x-aep"?: { tools?: { openapi?: unknown } } } | null;
  const declared = doc?.["x-aep"]?.tools?.openapi;
  if (declared === undefined) return [];
  if (!Array.isArray(declared)) {
    throw new Error(`agent-eval: ${afmPath} — x-aep.tools.openapi must be a list`);
  }

  // A component's contract lives at a FIXED location beside its own folder,
  // which is why the document names the component and never a path.
  const componentsDir = dirname(dirname(resolve(afmPath)));

  return (declared as OpenApiTool[]).map((tool) => {
    const component = tool.component;
    if (typeof component !== "string" || component === "") {
      throw new Error(`agent-eval: ${afmPath} — every x-aep.tools.openapi entry needs a component`);
    }
    if (typeof tool.baseUrl !== "string") {
      throw new Error(`agent-eval: ${afmPath} — ${component} has no baseUrl`);
    }
    const envVar = ENV_REFERENCE.exec(tool.baseUrl)?.[1];
    if (envVar === undefined) {
      throw new Error(
        `agent-eval: ${afmPath} — ${component}'s baseUrl must be an \${env:NAME} reference, ` +
          `got "${tool.baseUrl}". A literal address cannot be pointed at a stub.`,
      );
    }
    // Taken, never widened. The eval world must answer for exactly the tools
    // the built agent carries: a stub that served the whole contract would
    // let an over-reach succeed silently, in the one place it is cheap to
    // see. An entry that grants nothing is a document that says nothing.
    const allow = tool.allow;
    if (!Array.isArray(allow) || allow.length === 0 || allow.some((id) => typeof id !== "string")) {
      throw new Error(
        `agent-eval: ${afmPath} — ${component} needs a non-empty allow list of operationIds`,
      );
    }
    const specPath = join(componentsDir, component, "openapi.yaml");
    if (!existsSync(specPath)) {
      throw new Error(
        `agent-eval: ${afmPath} names the component "${component}", whose contract is not at ${specPath}`,
      );
    }
    return { envVar, specPath, allow: allow as string[] };
  });
}
