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

import { tool } from "ai";
import type { Tool } from "ai";
import { z } from "zod";
import { DesignDoc } from "./doc.js";
import { Dependency, DependencyStatus, SlimComponent } from "./schema.js";
import { validate, type ValidationIssue } from "./validator.js";

// Side-channel for tools to push SSE events to the client. Each tool emits at
// most one event; events are name-keyed and idempotent under reorder, so we
// don't need a mutex around execute().
export interface SseSink {
  send(event: string, data: unknown): void;
  // True after the response stream is closed (client disconnect). Tools
  // short-circuit when the socket is gone so we stop burning model time.
  isClosed(): boolean;
}

// FinalizeResolver — the route waits on this. When finalize() validates and
// emits data-finish, it also resolves so streamText loop can exit cleanly.
export interface FinalizeResolver {
  finalized: boolean;
  resolve(): void;
}

// `result` shape returned to the model. Keep these stable strings — the
// system prompt instructs the model to react to specific values.
type ToolResult = Record<string, unknown>;

const CLIENT_DISCONNECTED: ToolResult = {
  error: "client-disconnected",
};

export function buildTools(
  doc: DesignDoc,
  sse: SseSink,
  finalizer: FinalizeResolver,
  wireframes: Record<string, string> = {},
): Record<string, Tool> {
  const guard = (run: () => ToolResult): ToolResult => {
    if (sse.isClosed()) return CLIENT_DISCONNECTED;
    try {
      return run();
    } catch (err) {
      return {
        error: "tool-failed",
        message: err instanceof Error ? err.message : String(err),
      };
    }
  };

  return {
    set_overview: tool({
      description: "Replace the project overview text.",
      inputSchema: z.object({
        text: z
          .string()
          .describe(
            "2-3 sentence architecture overview summarizing system design and component structure",
          ),
      }),
      execute: async ({ text }) =>
        guard(() => {
          doc.setOverview(text);
          sse.send("overview", { text });
          return { ok: true };
        }),
    }),

    add_component: tool({
      description:
        "Add a new component. Fails if a component with the same name already exists.",
      inputSchema: SlimComponent,
      execute: async (slim) =>
        guard(() => {
          if (doc.hasComponent(slim.name)) {
            return {
              error: "name-exists",
              message:
                "To modify, use the surgical setters; to replace, call remove_component first.",
            };
          }
          doc.addComponent(slim);
          sse.send("component-added", { component: slim });
          return { ok: true };
        }),
    }),

    remove_component: tool({
      description: "Remove a component by name. Clears its pending spec.",
      inputSchema: z.object({ name: z.string() }),
      execute: async ({ name }) =>
        guard(() => {
          if (!doc.hasComponent(name)) {
            return { error: "not-found", message: `${name} does not exist` };
          }
          doc.removeComponent(name);
          sse.send("component-removed", { name });
          return { ok: true };
        }),
    }),

    set_agent_instructions: tool({
      description:
        "Replace componentAgentInstructions for a component. Does NOT invalidate openapi (instruction-only edits do not require a spec re-emit).",
      inputSchema: z.object({
        name: z.string(),
        text: z.string(),
      }),
      execute: async ({ name, text }) =>
        guard(() => {
          if (!doc.hasComponent(name)) {
            return { error: "not-found" };
          }
          doc.setAgentInstructions(name, text);
          sse.send("component-updated", {
            name,
            patch: { componentAgentInstructions: text },
            openapiInvalidated: false,
          });
          return { ok: true, openapiInvalidated: false };
        }),
    }),

    set_language: tool({
      description: "Set the language/framework for a component. Invalidates its openapi.",
      inputSchema: z.object({
        name: z.string(),
        language: z.string(),
      }),
      execute: async ({ name, language }) =>
        guard(() => {
          if (!doc.hasComponent(name)) {
            return { error: "not-found" };
          }
          doc.setLanguage(name, language);
          sse.send("component-updated", {
            name,
            patch: { language },
            openapiInvalidated: true,
          });
          return { ok: true, openapiInvalidated: true };
        }),
    }),

    add_dependency: tool({
      description:
        "Add (or replace by name) a dependency on a component. A dependency is anything the component needs from outside itself, discriminated by `kind`:\n" +
        "  - 'component': a sibling component built by THIS project (set `name` to the sibling's exact name). Invalidates this component's openapi (wire-contract drift).\n" +
        "  - 'org-service': a service published by ANOTHER project in the org, declared by name (P3).\n" +
        "  - 'external': an off-platform API / SaaS / DB the user supplies values for — carry a free-form `description` (which SDK / auth / where the spec lives) and a `config` key schema (which env-var keys, which are secret).\n" +
        "  - 'platform-resource': a platform-provisioned resource (database / queue / cache / identity-provider), named by `resourceType` (P5).\n" +
        "Idempotent on the dependency's `name` (a second add with the same name replaces it).",
      inputSchema: z.object({
        name: z
          .string()
          .describe("Name of the component that has this dependency."),
        dependency: Dependency,
      }),
      execute: async ({ name, dependency }) =>
        guard(() => {
          if (!doc.hasComponent(name)) {
            return { error: "not-found" };
          }
          doc.upsertDependency(name, dependency);
          const after = doc.getComponent(name);
          const invalidated = dependency.kind === "component";
          sse.send("component-updated", {
            name,
            patch: { dependencies: after.slim.dependencies },
            openapiInvalidated: invalidated,
          });
          return { ok: true, openapiInvalidated: invalidated };
        }),
    }),

    remove_dependency: tool({
      description:
        "Remove a dependency from a component by its `name` (any kind). No-op if not present. Invalidates openapi only when the removed dependency was a sibling component.",
      inputSchema: z.object({
        name: z.string().describe("Name of the component."),
        dependencyName: z
          .string()
          .describe("`name` of the dependency to remove."),
      }),
      execute: async ({ name, dependencyName }) =>
        guard(() => {
          if (!doc.hasComponent(name)) {
            return { error: "not-found" };
          }
          const before = doc.getComponent(name).slim.dependencies ?? [];
          const removed = before.find((d) => d.name === dependencyName);
          doc.removeDependency(name, dependencyName);
          const after = doc.getComponent(name);
          const invalidated = removed?.kind === "component";
          sse.send("component-updated", {
            name,
            patch: { dependencies: after.slim.dependencies },
            openapiInvalidated: invalidated,
          });
          return { ok: true, openapiInvalidated: invalidated };
        }),
    }),

    resolve_dependency: tool({
      description:
        "Set the resolution status of a dependency: 'resolved' (ready to wire), 'ambiguous' (candidates attached, needs a user pick), or 'unresolved' (user input required). Use this when you cannot fully resolve an external / org-service dependency on your own — the console surfaces non-resolved entries to the user, and a design with unresolved/ambiguous dependencies cannot be saved.",
      inputSchema: z.object({
        name: z.string().describe("Name of the component."),
        dependencyName: z.string().describe("`name` of the dependency."),
        status: DependencyStatus,
      }),
      execute: async ({ name, dependencyName, status }) =>
        guard(() => {
          if (!doc.hasComponent(name)) {
            return { error: "not-found" };
          }
          try {
            doc.resolveDependency(name, dependencyName, status);
          } catch (err) {
            return {
              error: "not-found",
              message: err instanceof Error ? err.message : String(err),
            };
          }
          const after = doc.getComponent(name);
          sse.send("component-updated", {
            name,
            patch: { dependencies: after.slim.dependencies },
            openapiInvalidated: false,
          });
          return { ok: true };
        }),
    }),

    set_openapi: tool({
      description:
        "Set the OpenAPI 3.0.3 YAML for a 'service' component. Rejected with {error:'not-applicable'} for 'web-app' components — frontends have no wire contract. If the new spec is semantically equal to the current one, returns {changed: false} and emits no SSE event — do not retry in that case.",
      inputSchema: z.object({
        name: z.string(),
        contents: z.string().describe("Full OpenAPI 3.0.3 YAML"),
      }),
      execute: async ({ name, contents }) =>
        guard(() => {
          if (!doc.hasComponent(name)) {
            return { error: "not-found" };
          }
          const entry = doc.getComponent(name);
          if (entry.slim.componentType === "web-app") {
            return {
              error: "not-applicable",
              message:
                "web-app components do not get an OpenAPI spec — describe screens / flows / which services they call in componentAgentInstructions instead.",
            };
          }
          const result = doc.setOpenApi(name, contents);
          if (result.changed) {
            sse.send("component-spec-updating", { name });
            return { changed: true };
          }
          return { changed: false, reason: result.reason };
        }),
    }),

    get_component: tool({
      description:
        "Read a component's current state. Returns slim metadata and the raw OpenAPI YAML (or null if pending).",
      inputSchema: z.object({ name: z.string() }),
      execute: async ({ name }) =>
        guard(() => {
          if (!doc.hasComponent(name)) {
            return { error: "not-found" };
          }
          return doc.getComponent(name);
        }),
    }),

    read_wireframe: tool({
      description:
        "Read a wireframe / domain-model canvas as DSL text. Use this to fetch the screen flows or entity model when designing components — only call when the spec mentions a relevant canvas. Returns {dsl: string} on success or {error:'not-found'} if no canvas with that name was supplied.",
      inputSchema: z.object({
        name: z
          .string()
          .describe(
            "Canvas name without extension, e.g. 'wireframes' or 'domain-model'",
          ),
      }),
      execute: async ({ name }) =>
        guard(() => {
          const dsl = wireframes[name];
          if (!dsl) {
            return {
              error: "not-found",
              message: `No wireframe DSL named ${name}. Available: ${Object.keys(wireframes).join(", ") || "(none)"}`,
            };
          }
          return { name, dsl };
        }),
    }),

    finalize: tool({
      description:
        "End the session. Runs the validator. On validation failure, returns {error:'validation', issues:[...]} for you to address. On success, emits data-finish.",
      inputSchema: z.object({}),
      execute: async () =>
        guard(() => {
          const issues: ValidationIssue[] = validate(doc);
          if (issues.length > 0) {
            return { error: "validation", issues };
          }
          if (!finalizer.finalized) {
            finalizer.finalized = true;
            sse.send("finish", { design: doc.materialize() });
            finalizer.resolve();
          }
          return { ok: true, finalized: true };
        }),
    }),
  };
}
