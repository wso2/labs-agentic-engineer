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

import { splitAfm } from "@aep/agent-stream";

/**
 * Parser for a component's `agent.afm.md` (Agent-Flavored Markdown). Turns the
 * raw file text into a tolerant, UI-friendly model the React view can render
 * without knowing the schema.
 *
 * The document split is delegated to `@aep/agent-stream`'s `splitAfm` — the
 * same reader the design-save gate uses — so the view can never disagree with
 * the gate about where the front matter ends.
 *
 * Deliberately NOT reused: that package's `agentAfmFrontMatterSchema`. It is a
 * zod `strictObject`, correct for a write-time gate that must reject an
 * unsupported key outright, and wrong for a viewer, which must still render a
 * partial draft or a document written against a newer spec version. So every
 * field here is read defensively: missing ones are omitted, unknown ones are
 * ignored, and only a document with no readable front matter at all degrades
 * to a ParseError the view shows as an alert instead of throwing.
 *
 * Tool RESOLUTION (does this `allow` entry name a real operationId?) is the one
 * thing deliberately not computed here. It is derived server-side by
 * spec.ComputeAgentToolStatus at design-read and reaches the console on
 * `Dependency.operations`; recomputing it from this file would need the
 * provider's openapi.yaml and would drift from that single authority.
 * AgentView's optional `toolStatus` prop is the only source for it.
 */

/** One `# Heading` section of the prompt body, in document order. */
export interface PromptSection {
  heading: string;
  body: string;
}

/** An entry of `x-aep.tools.openapi[]` — one provider component's allow-list. */
export interface AgentToolGroup {
  component: string;
  /** Always an `${env:NAME}` reference in a valid document; kept raw. */
  baseUrl?: string | undefined;
  operations: string[];
}

export interface AgentInterface {
  /** "webchat" today; kept as a raw string so an unknown type still renders. */
  type: string;
  /** The HTTP path this interface is exposed on, when declared. */
  path?: string | undefined;
}

export interface AgentModel {
  /** "anthropic" | "openai"; raw so an unknown provider still renders. */
  provider?: string | undefined;
  name?: string | undefined;
  url?: string | undefined;
  /** `model.authentication.type` — "api-key" today. */
  authType?: string | undefined;
  /** `model.authentication.api_key`, an `${env:NAME}` reference. */
  authKey?: string | undefined;
}

export interface AgentSpec {
  name: string;
  description?: string | undefined;
  specVersion?: string | undefined;
  version?: string | undefined;
  maxIterations?: number | undefined;
  model?: AgentModel | undefined;
  interfaces: AgentInterface[];
  tools: AgentToolGroup[];
  /** `x-aep.memory.type` — "server" (the default) or "client". */
  memory?: string | undefined;
  /** `x-aep.identity.mode` — "on-behalf-of" | "agent". */
  identity?: string | undefined;
  prompt: PromptSection[];
  /**
   * The prompt body VERBATIM, exactly as it sits after the front matter.
   * `prompt` above is a reading of it; this is the thing itself, and it is
   * what an editor must write back — a body rebuilt from the sections would
   * silently normalise spacing and drop anything the section reader does not
   * recognise as a heading.
   */
  body: string;
}

export interface ParseError {
  error: string;
}

export type ParseResult = AgentSpec | ParseError;

export function isParseError(result: ParseResult): result is ParseError {
  return "error" in result;
}

function record(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function str(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() !== "" ? value.trim() : undefined;
}

function num(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function list(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function readModel(value: unknown): AgentModel | undefined {
  const model = record(value);
  if (!model) return undefined;
  const auth = record(model.authentication);
  const parsed: AgentModel = {
    provider: str(model.provider),
    name: str(model.name),
    url: str(model.url),
    authType: str(auth?.type),
    authKey: str(auth?.api_key),
  };
  // An entirely empty `model:` block carries nothing the view can show.
  return Object.values(parsed).some((v) => v !== undefined) ? parsed : undefined;
}

function readInterfaces(value: unknown): AgentInterface[] {
  const out: AgentInterface[] = [];
  for (const entry of list(value)) {
    const iface = record(entry);
    const type = str(iface?.type);
    if (!type) continue;
    const path = str(record(record(iface?.exposure)?.http)?.path);
    out.push(path ? { type, path } : { type });
  }
  return out;
}

/**
 * Split the prompt body on its level-1 headings — the `# Role` / `# Instructions`
 * / `# Style` structure agent-building authors. Deeper headings stay inside their
 * section's body so a nested outline survives; a body with no heading at all
 * becomes one unheaded section rather than disappearing.
 */
function readPrompt(body: string): PromptSection[] {
  const trimmed = body.trim();
  if (trimmed === "") return [];

  const sections: PromptSection[] = [];
  let heading = "";
  let lines: string[] = [];

  const flush = () => {
    const text = lines.join("\n").trim();
    if (heading !== "" || text !== "") sections.push({ heading, body: text });
    lines = [];
  };

  let inFence = false;
  for (const line of trimmed.split("\n")) {
    // Track fences first: inside one, a leading "# " is code (a comment, a
    // shell prompt), never a heading, and treating it as one both invents a
    // section and truncates the real one it interrupts.
    if (/^\s*(```|~~~)/.test(line)) {
      inFence = !inFence;
      lines.push(line);
      continue;
    }
    const match = inFence ? null : /^#\s+(.*)$/.exec(line);
    if (match) {
      flush();
      heading = match[1]!.trim();
      continue;
    }
    lines.push(line);
  }
  flush();

  return sections;
}

function readTools(value: unknown): AgentToolGroup[] {
  const out: AgentToolGroup[] = [];
  for (const entry of list(record(value)?.openapi)) {
    const tool = record(entry);
    const component = str(tool?.component);
    if (!component) continue;
    const operations = list(tool?.allow)
      .map((op) => str(op))
      .filter((op): op is string => op !== undefined);
    const baseUrl = str(tool?.baseUrl);
    out.push(baseUrl ? { component, baseUrl, operations } : { component, operations });
  }
  return out;
}

export function parseAgentAfm(raw: string): ParseResult {
  const split = splitAfm(raw);
  if (!split) {
    return {
      error:
        "This file has no readable YAML front matter. An agent definition starts with a `---` block.",
    };
  }
  const front = record(split.frontMatter);
  if (!front) {
    return { error: "The front matter of this file is not a YAML mapping." };
  }

  const aep = record(front["x-aep"]);

  return {
    name: str(front.name) ?? "",
    description: str(front.description),
    specVersion: str(front.spec_version),
    version: str(front.version),
    maxIterations: num(front.max_iterations),
    model: readModel(front.model),
    interfaces: readInterfaces(front.interfaces),
    tools: readTools(aep?.tools),
    memory: str(record(aep?.memory)?.type),
    identity: str(record(aep?.identity)?.mode),
    prompt: readPrompt(split.body),
    body: split.body,
  };
}
