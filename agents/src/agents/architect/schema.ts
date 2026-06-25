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

import { z } from "zod";

// ── Unified dependency model ───────────────────────────────────────────────
// A Dependency is a single, kind-discriminated entry on a component. It
// subsumes the legacy `dependsOn` (sibling components) and `dependentApis`
// (external HTTP APIs) into ONE list with one gating path. Four kinds:
//   - component         — a sibling component built by THIS project (internal)
//   - org-service       — a service deployed by another project in the org (P3)
//   - external          — an off-platform API / SaaS / DB the user supplies
//                         values for; the only kind that needs a connection +
//                         value collection
//   - platform-resource — a platform-PROVISIONED resource (db/queue/cache/idp);
//                         forward-declared here, wired in P5
// See docs/design (dependency management).

export const DependencyStatus = z.enum(["resolved", "ambiguous", "unresolved"]);
export type DependencyStatus = z.infer<typeof DependencyStatus>;

// A single config key the consuming component reads at runtime. For an
// `external` connection these keys ARE the connection's schema and drive the
// OpenChoreo ResourceType in P2. `secret: true` routes the value through the
// secret path (SM-API → ESO); `false` is plain config (URLs, ids, regions).
export const ConfigKey = z.object({
  key: z
    .string()
    .describe(
      "Env var name the component reads, UPPER_SNAKE_CASE, e.g. 'SALESFORCE_CLIENT_ID' or 'OPENWEATHER_BASE_URL'.",
    ),
  secret: z
    .boolean()
    .describe(
      "True if the value is a credential / token / password (secret path); false for plain config (URLs, ids, regions).",
    ),
  credentialClass: z
    .enum(["publishable", "secret"])
    .optional()
    .describe(
      "Only meaningful for a secret key consumed by a web-app: 'publishable' = safe to expose in the browser (window._env_); 'secret' = must stay server-side. Defaults to 'secret' when unknown.",
    ),
});
export type ConfigKey = z.infer<typeof ConfigKey>;

// A candidate attached to an `ambiguous` dependency for the resolution UI.
export const DependencyCandidate = z.object({
  label: z.string().describe("Human-readable candidate name."),
  description: z.string().optional(),
  url: z.string().optional().describe("Spec or homepage URL, when known."),
});
export type DependencyCandidate = z.infer<typeof DependencyCandidate>;

// kind: component — a sibling component built by this same project. The
// platform resolves its URL (deploy-gated) and wires an OC Connection.
const ComponentDependency = z.object({
  kind: z.literal("component"),
  name: z
    .string()
    .describe(
      "Exact `name` of the sibling component this one depends on (must match another component verbatim).",
    ),
  status: DependencyStatus.optional(),
});

// kind: org-service — a service deployed by ANOTHER project in the same org,
// consumed via the org catalog + an OC Connection (P3). Declare by name only.
const OrgServiceDependency = z.object({
  kind: z.literal("org-service"),
  name: z
    .string()
    .describe("Catalog name of the org service this component calls."),
  description: z.string().optional(),
  status: DependencyStatus.optional(),
  candidates: z.array(DependencyCandidate).optional(),
});

// kind: external — an off-platform service the user supplies values for: a
// SaaS (Salesforce, GitHub), a public/corporate REST API (OpenWeather), or a
// user-managed DB. ONE generic kind — the integration style (which SDK, which
// auth, where the spec lives) rides in `description`, not a sub-kind enum.
const ExternalDependency = z.object({
  kind: z.literal("external"),
  name: z
    .string()
    .describe(
      "Stable key; the connection's registry + Resource name, e.g. 'salesforce', 'openweather'. Lowercase kebab-case.",
    ),
  description: z
    .string()
    .describe(
      "What the config is for + how to use it (which SDK to initialise, which auth scheme, where the API spec lives). Agent-facing, free-form.",
    ),
  config: z
    .array(ConfigKey)
    .describe(
      "The config key SCHEMA the agent codes against (which keys, which are secret). Values are collected later from the user, NOT here. A URL is a config key (it varies per env), not metadata.",
    ),
  status: DependencyStatus.optional(),
  candidates: z.array(DependencyCandidate).optional(),
});

// kind: platform-resource — a resource the PLATFORM provisions (database,
// message-queue, cache, identity-provider …), sub-typed by `resourceType`.
// Forward-declared in P1; provisioning is wired in P5.
const PlatformResourceDependency = z.object({
  kind: z.literal("platform-resource"),
  name: z.string().describe("Logical name for this resource on the component."),
  resourceType: z
    .string()
    .describe(
      "Registered OpenChoreo (Cluster)ResourceType name, e.g. 'database'. OPEN STRING — the available set is discovered from the cluster at runtime, not an enum.",
    ),
  parameters: z
    .record(z.string(), z.string())
    .optional()
    .describe("Provisioning parameters (open key/value). Wired in P5."),
  description: z.string().optional(),
  status: DependencyStatus.optional(),
});

export const Dependency = z.discriminatedUnion("kind", [
  ComponentDependency,
  OrgServiceDependency,
  ExternalDependency,
  PlatformResourceDependency,
]);
export type Dependency = z.infer<typeof Dependency>;

// SlimComponent — shape metadata only, no openAPISpec. The architect emits
// these via add_component / set_* tools so the UI can render component cards
// before the (large) OpenAPI YAML has streamed.
export const SlimComponent = z.object({
  name: z
    .string()
    .describe("Lowercase kebab-case component name, e.g. 'user-api'"),
  componentType: z
    .enum(["service", "web-app"])
    .describe(
      "Component type: 'web-app' for frontends, 'service' for backend APIs.",
    ),
  language: z
    .string()
    .describe(
      "Primary programming language and framework, e.g. 'Go', 'TypeScript / React', 'Ballerina'",
    ),
  dependencies: z
    .array(Dependency)
    .describe(
      "Everything this component needs from outside itself, as ONE kind-discriminated list: sibling components (kind 'component'), org services (kind 'org-service'), external connections (kind 'external'), and platform resources (kind 'platform-resource'). Empty array when the component is self-contained. Replaces the legacy dependsOn + dependentApis fields.",
    ),
  origin: z
    .enum(["source", "image"])
    .optional()
    .describe(
      "How this component is produced: 'source' = the agent writes + builds it (the default; omit for source); 'image' = a prebuilt container (e.g. Keycloak) the agent does NOT write source for. Image components are a later phase.",
    ),
  image: z
    .string()
    .optional()
    .describe(
      "Container image ref with tag when origin = 'image', e.g. 'quay.io/keycloak/keycloak:25.0'. Omit for source components.",
    ),
  config: z
    .array(ConfigKey)
    .optional()
    .describe(
      "User-provided runtime config vars on THIS component (plain settings and/or secrets) that are NOT an external service — e.g. a feature flag, a target repo name, an admin password. Collected from the user via the same value form as connections and injected into the component's own ReleaseBinding. Omit when the component needs no user-supplied config.",
    ),
  entrypoint: z
    .enum(["deployment/service", "deployment/web-application"])
    .describe(
      "OpenChoreo component type: 'deployment/service' for backend APIs, 'deployment/web-application' for frontends/SPAs",
    ),
  buildpack: z.literal("docker").describe("Build strategy"),
  appPath: z
    .string()
    .describe(
      "Folder (directory) within the monorepo where this component's source code lives, relative to the repo root. This is NOT an HTTP route or API path — it is a filesystem path. Must NOT start with a leading slash. Examples: 'user-api', 'services/auth'. The coding agent will create files like '<appPath>/main.go', '<appPath>/Dockerfile', '<appPath>/workload.yaml'.",
    ),
  componentAgentInstructions: z
    .string()
    .describe(
      "Detailed implementation instructions for the Generator agent",
    ),
  exposesAPI: z
    .object({
      managed: z
        .boolean()
        .optional()
        .describe(
          "True when the service should be routed through the WSO2 API Platform gateway (CORS + JWT validation + rate limiting). Default true when 'auth' is set.",
        ),
      auth: z
        .enum(["end-user-required", "service-required", "none"])
        .describe(
          "Caller authentication policy. 'end-user-required' = the gateway validates an end-user JWT and injects X-User-Id. 'service-required' = the gateway validates a service-to-service JWT (no end-user). 'none' = public.",
        ),
    })
    .optional()
    .describe(
      "API exposure policy (services only). Omit for public APIs. Set 'auth: end-user-required' when callers are end users; the gateway validates the JWT and injects X-User-Id.",
    ),
  callerIdentity: z
    .object({
      mode: z
        .enum(["end-user", "service-account", "none"])
        .describe(
          "How the component identifies its caller. 'end-user' = SPA signs in users via the platform IDP (OIDC + PKCE). 'service-account' = workload-to-workload auth. 'none' = no auth.",
        ),
    })
    .optional()
    .describe(
      "Caller-identity intent. Set 'mode: end-user' on web-app components that sign users in via the platform IDP; the platform handles OIDC provisioning + runtime config injection.",
    ),
});

export type SlimComponent = z.infer<typeof SlimComponent>;

// DesignComponent — slim + openAPISpec. This is the wire shape the BFF and
// console expect at data-finish; produced by DesignDoc.materialize().
export const DesignComponent = SlimComponent.extend({
  openAPISpec: z
    .string()
    .describe("Complete OpenAPI 3.0 YAML spec for this component"),
});

export type DesignComponent = z.infer<typeof DesignComponent>;

export const ArchitectOutput = z.object({
  overview: z
    .string()
    .describe(
      "A 2-3 sentence architecture overview summarizing the system design, component structure, and communication patterns",
    ),
  components: z
    .array(DesignComponent)
    .describe("Deployable service components"),
});

export type ArchitectOutput = z.infer<typeof ArchitectOutput>;

// SkillDescription is the lightweight per-skill catalog row shipped on
// the architect input — name + description only (no body). The architect
// uses these to decide whether to read the full body (org skills) or
// already has the body inlined (built-ins, via SkillRecord).
export const SkillDescription = z.object({
  name: z.string(),
  description: z.string(),
});
export type SkillDescription = z.infer<typeof SkillDescription>;

// SkillRecord is name + description + full SKILL.md body. Used by the
// architect input for built-ins (inlined under "Platform skills — MUST
// consult") and by the tech-lead detail input for every attached skill.
export const SkillRecord = SkillDescription.extend({
  body: z.string(),
});
export type SkillRecord = z.infer<typeof SkillRecord>;

export const ArchitectInput = z.object({
  projectName: z.string(),
  spec: z.string().describe("Specification document to design against"),
  previousDesign: ArchitectOutput.optional().describe(
    "Existing design to evolve — preserve component names and structure where possible",
  ),
  // Platform skills — full bodies inlined into the prompt under "MUST
  // consult" framing. The architect sees these regardless of project
  // state because they encode best practices the platform requires.
  builtinSkills: z
    .array(SkillRecord)
    .optional()
    .describe(
      "Built-in platform skills — bodies are inlined into the prompt under 'Platform skills — MUST consult'.",
    ),
  // Org skills — manifest-only (name + description). The architect
  // loads bodies on demand via read_skill (PR 3) if any apply.
  orgSkills: z
    .array(SkillDescription)
    .optional()
    .describe(
      "Org-authored skills (custom + imported) — manifest only; bodies load via read_skill on demand.",
    ),
  // Currently-attached skill names on the project's design.md root
  // frontmatter. Optional; surfaced so the architect can decide to
  // attach / detach via tools (PR 3). PR 1 sets this from
  // seedDefaultSkillsApplied on every finalize.
  skillsApplied: z
    .array(z.string())
    .optional()
    .describe(
      "Skill names currently attached to the project. The architect may keep or change this set in the design.",
    ),
  // Wireframes / domain-models live alongside the spec under
  // `specs/requirements/`. The BFF passes the raw DSL keyed by canvas
  // name (without extension); the architect calls `read_wireframe(name)`
  // on demand to pull in the DSL when a screen flow is relevant.
  wireframes: z
    .record(z.string(), z.string())
    .optional()
    .describe(
      "Map of canvas name (e.g. 'wireframes', 'domain-model') to DSL text",
    ),
  availableWireframes: z
    .array(z.string())
    .optional()
    .describe(
      "List of canvas names available via the read_wireframe tool. Mentioned in the system prompt so the model knows what to fetch.",
    ),
});

export type ArchitectInput = z.infer<typeof ArchitectInput>;
