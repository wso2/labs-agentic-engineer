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
 * The data a sweep reads — a case, its checklist, the run matrix — as zod
 * schemas, and the loaders that hold every file to them.
 *
 * Strict: these files are hand-edited (ADR-0004), so a misspelled key must
 * fail, not parse.
 */

import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { parse } from "yaml";
import { z } from "zod";

const kebab = z.string().regex(/^[a-z0-9]+(?:-[a-z0-9]+)*$/, "kebab-case");

/**
 * One checklist item: one user-visible behaviour, walked under one role.
 * `role` is a name `play wire --role` accepts, or one of the two callers no
 * role name expresses — `no role` and `signed out` (`roleEntries` in
 * `playground/src/engine/wire/roles.ts`, which is where both labels come from).
 */
export const ItemSchema = z
  .object({
    id: kebab,
    role: z.string(),
    screen: z.string().min(1),
    flow: z.string().min(1),
    steps: z.string().min(1),
    expect: z.string().min(1),
    weight: z.number().positive().default(1),
  })
  .strict();
export type Item = z.infer<typeof ItemSchema>;

/** Something that must NOT happen in any attempt (a role reading another's data, a raw error page). */
export const MustNotSchema = z
  .object({
    id: kebab,
    description: z.string().min(1),
  })
  .strict();
export type MustNot = z.infer<typeof MustNotSchema>;

export const ChecklistSchema = z
  .object({
    items: z.array(ItemSchema).min(1),
    extras: z
      .object({
        /** Hand-added items, walked and scored exactly like `items`. */
        mustCover: z.array(ItemSchema).default([]),
        mustNot: z.array(MustNotSchema).default([]),
      })
      .strict()
      .default({ mustCover: [], mustNot: [] }),
  })
  .strict()
  .superRefine((checklist, ctx) => {
    const seen = new Set<string>();
    for (const item of [...checklist.items, ...checklist.extras.mustCover, ...checklist.extras.mustNot]) {
      if (seen.has(item.id)) ctx.addIssue({ code: "custom", message: `duplicate id "${item.id}"` });
      seen.add(item.id);
    }
  });
export type Checklist = z.infer<typeof ChecklistSchema>;

export const CaseSchema = z
  .object({
    name: kebab,
    description: z.string(),
    source: z
      .object({
        /** Directory name, never a path (paths do not resolve across machines). Informational. */
        project: z.string(),
        /** `current` or the undo snapshot's directory name. */
        snapshot: z.string(),
        savedAt: z.string(),
      })
      .strict(),
    tags: z.array(z.string()).default([]),
    /** Overrides `TIMEOUTS.codingMinutes` for this case's coding run. */
    timeoutMinutes: z.number().int().positive().optional(),
  })
  .strict();
export type CaseFile = z.infer<typeof CaseSchema>;

/** A model connection, for runtimes that do not run on the OAuth token (see `credentials.ts`). */
const ConnectionSchema = z
  .object({
    format: z.string().min(1),
    baseUrl: z.string().min(1),
    authScheme: z.string().optional(),
    /** The NAME of the env var holding the key — never the key itself, this file is committed. */
    apiKeyEnv: z.string().regex(/^[A-Z][A-Z0-9_]*$/),
  })
  .strict();

export const RunConfigSchema = z
  .object({
    id: kebab,
    /** `AEP_AGENT_RUNTIME` — the runner's own parser refuses an unknown one. */
    runtime: z.enum(["claude-code", "opencode"]),
    /** `AEP_AGENT_MODEL`. */
    model: z.string().min(1),
    connection: ConnectionSchema.optional(),
  })
  .strict()
  .superRefine((config, ctx) => {
    // OpenCode cannot run on the OAuth token, the only Anthropic credential held.
    if (config.runtime === "opencode" && !config.connection) {
      ctx.addIssue({ code: "custom", message: `${config.id}: an opencode config needs a connection` });
    }
    if (config.runtime === "claude-code" && config.connection) {
      ctx.addIssue({
        code: "custom",
        message: `${config.id}: a claude-code config runs on the OAuth token — drop the connection`,
      });
    }
  });
export type RunConfig = z.infer<typeof RunConfigSchema>;

const ConfigsSchema = z.array(RunConfigSchema).min(1);

/** A loaded case: its files' contents plus where it lives. */
export interface EvalCase {
  name: string;
  dir: string;
  meta: CaseFile;
  checklist: Checklist;
}

export function listCases(casesDir: string): string[] {
  if (!existsSync(casesDir)) return [];
  return readdirSync(casesDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && !entry.name.startsWith(".") && existsSync(join(casesDir, entry.name, "case.yaml")))
    .map((entry) => entry.name)
    .sort();
}

export function loadCase(casesDir: string, name: string): EvalCase {
  const dir = join(casesDir, name);
  const meta = parseFile(join(dir, "case.yaml"), CaseSchema);
  if (meta.name !== name) throw new Error(`${dir}/case.yaml: name "${meta.name}" does not match its directory`);
  const checklist = parseFile(join(dir, "checklist.yaml"), ChecklistSchema);
  return { name, dir, meta, checklist };
}

export function loadConfigs(file: string): RunConfig[] {
  const configs = parseFile(file, ConfigsSchema);
  const ids = configs.map((config) => config.id);
  const duplicate = ids.find((id, index) => ids.indexOf(id) !== index);
  if (duplicate) throw new Error(`${file}: duplicate config id "${duplicate}"`);
  return configs;
}

/**
 * Every item an attempt walks and scores, in walking order: the checklist,
 * then the hand-added `mustCover` extras.
 */
export function scoredItems(checklist: Checklist): Item[] {
  return [...checklist.items, ...checklist.extras.mustCover];
}

/** Items whose role `wire` would not accept. `roles` is the case's own list (`caseRoles` in `save.ts`). */
export function unknownRoles(checklist: Checklist, roles: string[]): string[] {
  const known = new Set([...roles, ...SPECIAL_ROLES]);
  return scoredItems(checklist)
    .filter((item) => !known.has(item.role))
    .map((item) => `${item.id}: role "${item.role}"`);
}

/** The two callers `roleEntries` adds to the design's roles, by the labels it gives them. */
export const SPECIAL_ROLES = ["no role", "signed out"] as const;

/** Parse YAML text against a schema, naming the file and the path of every issue. */
export function parseYaml<T>(text: string, schema: z.ZodType<T>, label: string): T {
  let raw: unknown;
  try {
    raw = parse(text);
  } catch (e) {
    throw new Error(`${label}: not valid YAML — ${e instanceof Error ? e.message : String(e)}`);
  }
  const result = schema.safeParse(raw);
  if (!result.success) {
    const issues = result.error.issues.map((issue) => `${issue.path.join(".") || "(root)"}: ${issue.message}`);
    throw new Error(`${label}:\n  ${issues.join("\n  ")}`);
  }
  return result.data;
}

function parseFile<T>(file: string, schema: z.ZodType<T>): T {
  if (!existsSync(file)) throw new Error(`${file} is missing`);
  return parseYaml(readFileSync(file, "utf8"), schema, file);
}
