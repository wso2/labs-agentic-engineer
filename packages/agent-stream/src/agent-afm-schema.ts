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
 * The `agent.afm.md` write-gate — the STRUCTURAL half of the schema
 * (docs/design/draft/agent-afm-schema.md §5). Everything checkable from the
 * document alone lives here and hard-fails the write; the cross-file checks
 * (does `.component` name a declared dependency, is every `allow` entry a real
 * operationId) run at design-save instead, because writes have no guaranteed
 * order.
 */
import { z } from "zod";
import { parse as parseYaml } from "yaml";

export interface AfmProblem {
  code: "INVALID_AFM" | "SCHEMA_VIOLATION";
  message: string;
}

/** A value the platform injects. A literal here reaches git. */
const envRef = z
  .string()
  .regex(/^\$\{env:[A-Za-z_][A-Za-z0-9_]*\}$/, "must be an ${env:...} reference");

const modelSchema = z.strictObject({
  // Required by the schema but selects nothing: a generated agent picks its AI
  // SDK adapter at runtime from MODEL_API_FORMAT, the org's model connection.
  provider: z.enum(["anthropic", "openai"]),
  name: z.string().min(1),
  url: envRef,
  authentication: z.strictObject({
    type: z.literal("api-key"),
    api_key: envRef,
  }),
});

const interfaceSchema = z.strictObject({
  // webchat is the only interface the platform carries today. Rejecting the
  // others beats accepting a design the platform cannot deliver.
  type: z.literal("webchat"),
  exposure: z.strictObject({ http: z.strictObject({ path: z.string().min(1) }) }).optional(),
});

const openApiToolSchema = z.strictObject({
  // Names a `component` dependency. Its contract's location is fixed at
  // specs/design/components/<component>/openapi.yaml — never a path field.
  component: z.string().min(1),
  baseUrl: envRef,
  allow: z.array(z.string().min(1)).min(1),
});

/**
 * What an agent may be sent with a message, and the platform ceilings any
 * declaration must stay within. The ceilings are the console chat's (ADR-0019),
 * derived from the model's per-request budget; the Go gate (afmgate.go) mirrors
 * them and must give the same verdict.
 */
export const ATTACHMENT_TYPES = ["application/pdf", "image/png", "image/jpeg", "image/gif", "image/webp"] as const;
export const ATTACHMENT_CEILINGS = { maxFiles: 10, maxFileSizeMB: 5, maxTotalMB: 15 } as const;

const attachmentsSchema = z.strictObject({
  types: z
    .array(z.enum(ATTACHMENT_TYPES))
    .min(1)
    .refine((types) => new Set(types).size === types.length, "must not repeat a type"),
  maxFiles: z.number().int().min(1).max(ATTACHMENT_CEILINGS.maxFiles),
  maxFileSizeMB: z.number().int().min(1).max(ATTACHMENT_CEILINGS.maxFileSizeMB),
});

/**
 * `x-aep.guardrails` — AI-gateway policies applied to this agent's own model
 * traffic at deploy (agentgovernance/guardrails.go). Any policy in the
 * gateway's catalog may be named; its params are checked against the catalog
 * at deploy, because this gate runs offline. The spec carries the use case
 * only: the platform sets the policy version and every JSONPath, so a spec
 * that sets one is refused. The Go gate (afmgate.go) mirrors this.
 */
export const GUARDRAIL_LIMITS = { maxGuardrails: 10, maxWhy: 300 } as const;
export const PLATFORM_OWNED_GUARDRAIL_KEYS = ["jsonPath", "streamingJsonPath", "version", "paths"] as const;

/** The first platform-owned key in params, at the top level or in request/response. */
function ownedGuardrailKey(params: Record<string, unknown>): string | undefined {
  for (const scope of [params, params.request, params.response]) {
    if (scope && typeof scope === "object" && !Array.isArray(scope)) {
      const hit = PLATFORM_OWNED_GUARDRAIL_KEYS.find((key) => key in scope);
      if (hit) return hit;
    }
  }
  return undefined;
}

const guardrailSchema = z.strictObject({
  policy: z.string().regex(/^[a-z0-9-]{1,64}$/, "must be a lowercase policy name"),
  params: z.record(z.string(), z.unknown()).superRefine((params, ctx) => {
    const owned = ownedGuardrailKey(params);
    if (owned) ctx.addIssue({ code: "custom", message: `must not set ${owned} — the platform sets it` });
  }),
  why: z.string().min(1).max(GUARDRAIL_LIMITS.maxWhy),
});

const frontMatterSchema = z.strictObject({
  spec_version: z.literal("0.4.0"),
  name: z.string().min(1),
  description: z.string().min(1),
  version: z.string().min(1).optional(),
  max_iterations: z.number().int().min(1).optional(),
  model: modelSchema,
  interfaces: z.array(interfaceSchema).min(1),
  "x-aep": z
    .strictObject({
      tools: z.strictObject({ openapi: z.array(openApiToolSchema).min(1) }).optional(),
      memory: z.strictObject({ type: z.enum(["client", "server"]) }).optional(),
      identity: z.strictObject({ mode: z.enum(["on-behalf-of", "agent"]) }).optional(),
      attachments: attachmentsSchema.optional(),
      guardrails: z
        .array(guardrailSchema)
        .min(1)
        .max(GUARDRAIL_LIMITS.maxGuardrails)
        .refine((list) => new Set(list.map((g) => g.policy)).size === list.length, "must not repeat a policy")
        .optional(),
    })
    .optional(),
});

export type AgentAfmFrontMatter = z.infer<typeof frontMatterSchema>;

const FRONT_MATTER = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/;

/** Split an AFM document. Exported so the design-save check reuses it. */
export function splitAfm(content: string): { frontMatter: unknown; body: string } | null {
  const match = FRONT_MATTER.exec(content);
  if (!match) return null;
  try {
    return { frontMatter: parseYaml(match[1]!), body: match[2]!.trim() };
  } catch {
    return null;
  }
}

/**
 * AFM keys the spec defines but this platform does not carry yet. `strictObject`
 * would reject them anyway, as "unknown property" — which reads like a typo. A
 * named message says the truth: the field is real, we do not support it.
 */
const UNSUPPORTED: Record<string, string> = {
  tools: "MCP tools are not supported — an agent reaches our services over their OpenAPI contracts (x-aep.tools.openapi)",
  skills: "agent skills are not supported in this version",
};

export function checkAgentAfm(content: string, dirName: string): AfmProblem | null {
  const split = splitAfm(content);
  if (!split) {
    return { code: "INVALID_AFM", message: "no parseable YAML front matter — not an AFM document" };
  }

  for (const [key, message] of Object.entries(UNSUPPORTED)) {
    if (split.frontMatter && typeof split.frontMatter === "object" && key in split.frontMatter) {
      return { code: "SCHEMA_VIOLATION", message: `${key}: ${message}` };
    }
  }

  const parsed = frontMatterSchema.safeParse(split.frontMatter);
  if (!parsed.success) {
    const issue = parsed.error.issues[0]!;
    // zod v4's default `unrecognized_keys` message ("Unrecognized key: ...")
    // reads like an internal detail; "unknown property" says what a spec
    // author needs to hear — the field is not part of the AFM schema.
    if (issue.code === "unrecognized_keys") {
      const path = [...issue.path, issue.keys[0]].join(".");
      return { code: "SCHEMA_VIOLATION", message: `${path}: unknown property` };
    }
    const path = issue.path.join(".");
    return { code: "SCHEMA_VIOLATION", message: path ? `${path}: ${issue.message}` : issue.message };
  }

  if (parsed.data.name !== dirName) {
    return {
      code: "SCHEMA_VIOLATION",
      message: `name ${JSON.stringify(parsed.data.name)} must equal the component directory name ${JSON.stringify(dirName)}`,
    };
  }

  for (const section of ["# Role", "# Instructions"]) {
    if (!split.body.includes(section)) {
      return { code: "SCHEMA_VIOLATION", message: `body must contain a ${section} section` };
    }
  }
  return null;
}

/** JSON Schema projection of the front matter, for the published artifact. */
export const agentAfmFrontMatterSchema = frontMatterSchema;
