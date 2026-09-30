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

import type { components } from "../../generated/aep-api";

type AgentsProjection = components["schemas"]["AgentsProjection"];
type SubscriptionProjection = components["schemas"]["SubscriptionProjection"];
type GitProviderProjection = components["schemas"]["GitProviderProjection"];
type LLMProjection = components["schemas"]["LLMProjection"];
type LLMFormatOption = components["schemas"]["LLMFormatOption"];
type SreLlmProjection = components["schemas"]["SreLlmProjection"];
type SreAgentProjection = components["schemas"]["SreAgentProjection"];
type AgentRuntime = components["schemas"]["AgentRuntime"];
type SkillDetailBody = components["schemas"]["SkillDetailBody"];
type SkillUpdate = components["schemas"]["SkillUpdate"];
type ApiError = components["schemas"]["Error"];

// Scenario switch for the Settings (#96) and Onboarding (#102) features.
// Toggle in devtools:
//   localStorage.setItem('aep:mock:settings',
//     'empty' | 'partial' | 'connected' | 'subscription' | 'ollama' | 'opencode'
//     | 'opencode-unavailable' | 'disconnected' | 'error' | 'sync-error')
// "empty": nothing connected yet (the default — triggers the onboarding
// gate; also exercises Settings' not-connected states).
// "partial": GitHub connected, no model connection — the onboarding wizard
// opens at "Connect a model" (resume-after-abandon, #102).
// "connected": GitHub + a model connection on Anthropic's API (no
// onboarding), Claude Code, no subscription.
// "subscription": as "connected", plus a Claude subscription token billing
// Claude Code.
// "ollama": GitHub + an OpenAI-compatible connection on Ollama Cloud
// (glm-5.3: text only, unpriced, Ollama web search), OpenCode.
// "opencode": as "connected", with OpenCode chosen as the coding agent.
// "opencode-unavailable": as "opencode", on an installation with no OpenCode
// runner image — the org is stranded on a runtime it can no longer choose,
// and no runtime runs the OpenAI-compatible format.
// "disconnected": GitHub connected and the model connection disconnected —
// the wizard opens at "Connect a model" and says so (`llmDisconnectedAt`).
// "error": GET /config and GET /skills fail (load-error state).
// "sync-error": config empty and POST /skills/sync fails — exercises the
// wizard's bootstrap-failure step (Retry / Continue anyway, #102).
//
// The model probe (Test connection, and every save that changes the
// connection) is simulated from what is typed; see the MODEL_PROBE_* sentinels
// below and `probeConnection` in handlers/settings.ts.
export type SettingsScenario =
  | "empty"
  | "partial"
  | "connected"
  | "subscription"
  | "ollama"
  | "opencode"
  | "opencode-unavailable"
  | "disconnected"
  | "error"
  | "sync-error";

// Typing this exact value into a PAT/API-key field simulates the BFF's
// synchronous probe-before-persist validation failing against the real
// provider (issue #96: PATCH /config validates before persisting).
export const INVALID_CREDENTIAL_VALUE = "invalid";

// Import sentinels (issue #96 re-grill: reject hard, warn soft). A file name
// or URL containing "invalid" simulates a structurally-broken skill (hard
// 422, nothing persisted); containing "warn" simulates an importable-but-
// imperfect skill (201 with a non-empty ImportResult.warnings).
export const IMPORT_INVALID_SENTINEL = "invalid";
export const IMPORT_WARN_SENTINEL = "warn";

export const importWarningsFixture = [
  "license: none declared — treated as unlicensed",
  "compatibility: references a tool ('browser') this platform does not provide",
];

// HTML URL of the org skills repo backing the catalogue (GET /skills
// envelope repoUrl — powers the Import dialog's via-pull-request guidance).
export const skillsRepoUrl = "https://github.com/acme-dev/org-skills";

export const importFileInvalidError: ApiError = {
  code: "bad_request",
  message:
    "skill validation failed: TARBALL_INVALID: not a valid gzip stream: unexpected EOF",
};

export const githubConnectedFixture: GitProviderProjection = {
  kind: "github",
  mode: "pat",
  status: "connected",
  githubLogin: "acme-dev",
  identityLogin: "acme-dev",
  identityName: "Acme Dev",
  identityEmail: "dev@acme.example",
  connectedAt: "2026-06-01T12:00:00Z",
  lastValidatedAt: "2026-07-01T09:00:00Z",
  selectedRepos: ["acme-dev/demo-shop"],
};

// Model probe sentinels, read from what the reader types (host, key, model):
// - a key containing "invalid" is rejected by the endpoint (401);
// - a key containing "ratelimit" answers Test connection with 429
//   `llm_test_rate_limited` (the platform's own 10-a-minute limit);
// - a key containing "limit" proves the key but hits the provider's usage
//   limit (`warning: provider_limit`);
// - a host containing "internal", "localhost" or ending ".local" is refused
//   as private; a host containing "unreachable" does not answer;
// - a model missing from a known host's listing is `modelListed: no`, and a
//   host with no listing here (anything but the three below) is `unknown`.
export const MODEL_PROBE_REJECTED_KEY = "invalid";
export const MODEL_PROBE_TEST_RATE_LIMIT_KEY = "ratelimit";
export const MODEL_PROBE_PROVIDER_LIMIT_KEY = "limit";

/** Each host's model listing, as its `GET /models` would answer. */
export const modelListings: Record<string, string[]> = {
  "api.anthropic.com": ["claude-sonnet-5", "claude-haiku-4-5", "claude-opus-5"],
  "ollama.com": ["glm-5.3", "kimi-k3", "deepseek-v4-pro:0813", "gpt-oss:20b", "gpt-oss:120b"],
  "openrouter.ai": ["z-ai/glm-5.3", "moonshotai/kimi-k3", "anthropic/claude-sonnet-5"],
};

/** Models Ollama's `/api/show` reports with `vision`. */
export const ollamaVisionModels = ["kimi-k3"];

/** The (host, model) pairs the platform holds a rate for. */
export const pricedModels: Record<string, string[]> = {
  "api.anthropic.com": ["claude-sonnet-5", "claude-haiku-4-5"],
};

const ANTHROPIC_URL = "https://api.anthropic.com/v1";

/**
 * The formats a connection may speak on an installation that runs
 * `availableRuntimes`: Claude Code takes only the Anthropic format.
 */
export function llmFormatsFor(availableRuntimes: AgentRuntime[]): LLMFormatOption[] {
  return [
    {
      kind: "anthropic",
      defaultBaseURL: ANTHROPIC_URL,
      defaultModel: "claude-sonnet-5",
      runtimes: availableRuntimes.filter((r) => r === "claude-code" || r === "opencode"),
    },
    {
      kind: "openai-compatible",
      defaultBaseURL: null,
      defaultModel: "glm-5.3",
      runtimes: availableRuntimes.filter((r) => r === "opencode"),
    },
  ];
}

export const llmConnectedFixture: LLMProjection = {
  kind: "anthropic",
  baseURL: ANTHROPIC_URL,
  model: "claude-sonnet-5",
  keyPreview: "sk-a…wxyz",
  connectedAt: "2026-06-01T12:05:00Z",
  updatedAt: "2026-09-25T13:53:00Z",
  updatedBy: "dev@acme.example",
  priced: true,
  capabilities: {
    claudeSubscription: true,
    webSearch: "anthropic-server-tool",
    imageInput: "yes",
    nativePdf: true,
    generatedAgents: true,
    sreAgent: false,
  },
};

export const llmOllamaFixture: LLMProjection = {
  kind: "openai-compatible",
  baseURL: "https://ollama.com/v1",
  model: "glm-5.3",
  keyPreview: "3f9a…c2d1",
  connectedAt: "2026-09-25T13:50:00Z",
  updatedAt: "2026-09-25T13:53:00Z",
  updatedBy: "dev@acme.example",
  priced: false,
  capabilities: {
    claudeSubscription: false,
    webSearch: "ollama-api",
    imageInput: "no",
    nativePdf: false,
    generatedAgents: true,
    sreAgent: false,
  },
};

// An OpenAI-compatible, Bearer-authenticated org connection — the one shape
// `LLMCapabilities.sreAgent` is true for, so the SRE agent model row can
// inherit it (the "organization" source).
export const openaiOrgFixture: LLMProjection = {
  kind: "openai-compatible",
  baseURL: "https://api.openai.com/v1",
  model: "gpt-5.4",
  keyPreview: "sk-o…f00d",
  connectedAt: "2026-09-20T08:00:00Z",
  updatedAt: "2026-09-20T08:00:00Z",
  updatedBy: "dev@acme.example",
  priced: false,
  capabilities: {
    claudeSubscription: false,
    webSearch: "none",
    imageInput: "unknown",
    nativePdf: false,
    generatedAgents: true,
    sreAgent: true,
  },
};

// The org's own SRE model connection (an override of the org connection).
export const sreLlmFixture: SreLlmProjection = {
  baseURL: "https://api.openai.com/v1",
  host: "api.openai.com",
  model: "gpt-5.4-mini",
  keyPreview: "sk-p…abcd",
  connectedAt: "2026-09-20T08:00:00Z",
  updatedAt: "2026-09-20T08:00:00Z",
  updatedBy: "dev@acme.example",
};

/** An `SreAgentProjection`, defaulted to "no model, not running"; override what a test needs. */
export function sreAgentProjectionFixture(over: Partial<SreAgentProjection> = {}): SreAgentProjection {
  return { enabled: true, source: "none", host: "", model: "", status: "unconfigured", ...over };
}

// When the "disconnected" scenario's connection was removed.
export const llmDisconnectedAtFixture = "2026-09-20T08:00:00Z";

// A `claude setup-token` value stored as the org's Claude subscription.
export const subscriptionFixture: SubscriptionProjection = {
  kind: "claude",
  status: "connected",
  keyPrefix: "sk-ant-oat01-",
  keyLast4: "9f2c",
  connectedAt: "2026-09-01T10:00:00Z",
  lastValidatedAt: "2026-09-01T10:00:00Z",
};

export const agentsOpenCodeFixture: AgentsProjection = {
  runtime: "opencode",
  availableRuntimes: ["claude-code", "opencode"],
  subscription: null,
  updatedAt: "2026-09-20T08:30:00Z",
  updatedBy: "dev@acme.example",
};

// An installation deployed without the OpenCode runner image.
export const claudeCodeOnlyRuntimes: AgentsProjection["availableRuntimes"] = ["claude-code"];

// Every org has an effective runtime, so this section is never absent —
// unlike the connection. Null updatedAt/updatedBy is the platform-defaults
// state: nobody has ever chosen, which the console must not render as
// somebody having picked these very values.
export const agentsDefaultsFixture: AgentsProjection = {
  runtime: "claude-code",
  availableRuntimes: ["claude-code", "opencode"],
  subscription: null,
  updatedAt: null,
  updatedBy: null,
};

/** A refusal on one `/config` section, as the server shapes it. */
export function sectionError(section: "llm" | "agents", code: string, message: string): ApiError {
  return { code, message, details: [{ field: `body.${section}`, message }] };
}

export const gitProviderValidationError: ApiError = {
  code: "validation_failed",
  message: "the provided PAT could not be validated against GitHub",
  details: [
    {
      field: "body.gitProvider",
      message: "the provided PAT could not be validated against GitHub",
    },
  ],
};

export const subscriptionValidationError: ApiError = {
  code: "anthropic_key_invalid",
  message: "Anthropic rejected the key (401 Unauthorized)",
  details: [
    {
      field: "body.agents",
      message: "Anthropic rejected the key (401 Unauthorized)",
    },
  ],
};

export const gitProviderDisconnectRejected: ApiError = {
  code: "validation_failed",
  message:
    "use POST /config/git-provider/disconnect to disconnect the git provider",
  details: [
    {
      field: "body.gitProvider",
      message:
        "use POST /config/git-provider/disconnect to disconnect the git provider",
    },
  ],
};

export const configLoadError: ApiError = {
  code: "internal_error",
  message: "Failed to load organization configuration",
};

export const skillsLoadError: ApiError = {
  code: "internal_error",
  message: "Failed to load skills",
};

// Bootstrap failure for the onboarding wizard (#102): repo creation or the
// built-ins push failed. Sync is idempotent, so the remedy is retry.
export const skillsSyncError: ApiError = {
  code: "bad_gateway",
  message: "Failed to create the skills repository on GitHub",
};

// Covers all three kinds (org | platform | imported — the BE's real
// vocabulary; custom/builtin/flow are retired) so the catalogue's kind chips,
// read-only vs editable vs deletable actions, and the updates-available list
// all exercise. deletable = editable per the real BE contract: org-kind
// skills — whether platform-seeded (go, react-webapp, node-service,
// python-service, postgres-schema), user-authored (acme-deploy-checklist,
// acme-api-style), or imported (find-skills, commit-conventions) — are
// editable:true, deletable:true; platform-kind skills (high-level-
// architecture, security-design, wireframes, validation-files) are always
// managed by reconcile and are editable:false, deletable:false. Both Delete
// states render in mock mode. More than one page of skills (10/page, issue
// #172) so the flat list's pagination is exercisable in mock mode.
export const seedSkills: SkillDetailBody[] = [
  {
    orgId: "org-1",
    name: "go",
    kind: "org",
    // Platform-seeded org skill: editable and deletable like any org skill
    // (deletable = editable) — reconcile only re-seeds it on ongoing sync if
    // it's absent, never overwrites a present copy.
    editable: true,
    deletable: true,
    enabled: true,
    required: false,
    description:
      "How to build a Go service on the platform — layout, port 9090, multi-stage Dockerfile.",
    skillMd: `---
name: go
description: How to build a Go service on the platform.
---

# Go services

Pin \`golang:1.25-alpine\` as the builder; the build pod runs with
\`GOTOOLCHAIN=local\`.

## Layout

- \`cmd/\` — entrypoints
- \`internal/\` — everything else

Expose \`GET /health\` for liveness on port **9090**.`,
    references: {},
    binaryReferences: [],
    contentSha: "sha-go-1",
    updatedAt: "2026-05-01T00:00:00Z",
  },
  {
    orgId: "org-1",
    name: "react-webapp",
    kind: "org",
    // Platform-seeded org skill: editable and deletable (deletable = editable).
    editable: true,
    deletable: true,
    enabled: true,
    required: false,
    description:
      "How to build a React SPA on the platform — Vite layout, nginx runtime, window._env_ config.",
    skillMd: `---
name: react-webapp
description: How to build a React SPA on the platform.
---

# React web apps

Load \`/env-config.js\` synchronously **before** the bundle, then read runtime
config from \`window._env_\`. Throw on a missing key rather than defaulting.`,
    references: {},
    binaryReferences: [],
    contentSha: "sha-rw-1",
    updatedAt: "2026-05-02T00:00:00Z",
  },
  {
    orgId: "org-1",
    name: "architecture",
    kind: "platform",
    editable: false,
    deletable: false,
    enabled: true,
    required: false,
    description: "Derives component architecture from requirements.",
    skillMd: `---
name: architecture
description: Derives component architecture from requirements.
---

Derive the component architecture from the approved requirements.`,
    references: {},
    binaryReferences: [],
    contentSha: "sha-hla-1",
    updatedAt: "2026-05-01T00:00:00Z",
  },
  {
    orgId: "org-1",
    name: "security-design",
    kind: "platform",
    editable: false,
    deletable: false,
    enabled: true,
    required: false,
    description: "Designs roles, permissions, and Thunder auth.",
    skillMd: `---
name: security-design
description: Designs roles, permissions, and Thunder auth.
---

Break the approved design into a sequence of buildable tasks.`,
    references: {},
    binaryReferences: [],
    contentSha: "sha-tb-1",
    updatedAt: "2026-05-01T00:00:00Z",
  },
  {
    orgId: "org-1",
    name: "acme-deploy-checklist",
    kind: "org",
    // User-authored org skill (no platform manifest entry): editable AND
    // deletable — this is what exercises the Delete button in mock mode.
    editable: true,
    deletable: true,
    enabled: true,
    required: false,
    description: "Acme's internal pre-deploy checklist.",
    skillMd: `---
name: acme-deploy-checklist
description: Acme's internal pre-deploy checklist.
---

# Pre-deploy checklist

1. Migrations applied
2. Feature flags reviewed
3. Rollback plan written`,
    references: {
      "references/rollback.md": "# Rollback\n\nRevert the release tag.",
    },
    binaryReferences: [],
    contentSha: "sha-adc-1",
    updatedAt: "2026-06-20T00:00:00Z",
  },
  {
    orgId: "org-1",
    name: "find-skills",
    kind: "imported",
    editable: true,
    deletable: true,
    enabled: true,
    required: false,
    description: "Discover and evaluate community AgentSkills before adopting.",
    skillMd: `---
name: find-skills
description: Discover and evaluate community AgentSkills before adopting.
---

Search the registry, read the SKILL.md, and check the declared license.`,
    references: {},
    binaryReferences: [],
    contentSha: "sha-fs-1",
    updatedAt: "2026-07-01T00:00:00Z",
  },
  {
    orgId: "org-1",
    name: "node-service",
    kind: "org",
    // Platform-seeded org skill: editable and deletable (deletable = editable).
    editable: true,
    deletable: true,
    enabled: true,
    required: false,
    description: "How to build a Node.js service on the platform.",
    skillMd: `---
name: node-service
description: How to build a Node.js service on the platform.
---

Pin the LTS base image; expose \`GET /health\` on port **9090**.`,
    references: {},
    binaryReferences: [],
    contentSha: "sha-ns-1",
    updatedAt: "2026-05-03T00:00:00Z",
  },
  {
    orgId: "org-1",
    name: "python-service",
    kind: "org",
    // Platform-seeded org skill: editable and deletable (deletable = editable).
    editable: true,
    deletable: true,
    enabled: true,
    required: false,
    description: "How to build a Python service on the platform.",
    skillMd: `---
name: python-service
description: How to build a Python service on the platform.
---

Use \`uv\` for dependency management; expose \`GET /health\` on port **9090**.`,
    references: {},
    binaryReferences: [],
    contentSha: "sha-ps-1",
    updatedAt: "2026-05-03T00:00:00Z",
  },
  {
    orgId: "org-1",
    name: "postgres-schema",
    kind: "org",
    // Platform-seeded org skill: editable and deletable (deletable = editable).
    editable: true,
    deletable: true,
    enabled: true,
    required: false,
    description: "Schema and migration conventions for platform databases.",
    skillMd: `---
name: postgres-schema
description: Schema and migration conventions for platform databases.
---

One migration per change; never edit an applied migration.`,
    references: {},
    binaryReferences: [],
    contentSha: "sha-pg-1",
    updatedAt: "2026-05-04T00:00:00Z",
  },
  {
    orgId: "org-1",
    name: "wireframes",
    kind: "platform",
    editable: false,
    deletable: false,
    enabled: true,
    required: false,
    description: "Derives per-component wireframes from the design file.",
    skillMd: `---
name: wireframes
description: Derives per-component wireframes from the design file.
---

Derive one wireframe per user-facing component in the approved design.`,
    references: {},
    binaryReferences: [],
    contentSha: "sha-wf-1",
    updatedAt: "2026-05-01T00:00:00Z",
  },
  {
    orgId: "org-1",
    name: "validation-files",
    kind: "platform",
    editable: false,
    deletable: false,
    enabled: true,
    required: false,
    description: "Derives validation files from approved requirements.",
    skillMd: `---
name: validation-files
description: Derives validation files from approved requirements.
---

Every requirement gets at least one validation criterion.`,
    references: {},
    binaryReferences: [],
    contentSha: "sha-vf-1",
    updatedAt: "2026-05-01T00:00:00Z",
  },
  {
    orgId: "org-1",
    name: "acme-api-style",
    kind: "org",
    // User-authored org skill: editable AND deletable.
    editable: true,
    deletable: true,
    enabled: true,
    required: false,
    description: "Acme's REST API naming and versioning conventions.",
    skillMd: `---
name: acme-api-style
description: Acme's REST API naming and versioning conventions.
---

Plural nouns, kebab-case paths, \`/v1\` prefix, RFC 9457 errors.`,
    references: {},
    binaryReferences: [],
    contentSha: "sha-aas-1",
    updatedAt: "2026-06-22T00:00:00Z",
  },
  {
    orgId: "org-1",
    name: "commit-conventions",
    kind: "imported",
    editable: true,
    deletable: true,
    enabled: true,
    required: false,
    description: "Conventional-commit message rules for agent-authored PRs.",
    skillMd: `---
name: commit-conventions
description: Conventional-commit message rules for agent-authored PRs.
---

\`type(scope): summary\` — imperative, no trailing period.`,
    references: {},
    binaryReferences: [],
    contentSha: "sha-cc-1",
    updatedAt: "2026-07-02T00:00:00Z",
  },
];

// Embedded content differs from the org repo copy — surfaces in GET
// /skills/updates until synced. "code-review" is absent from the repo. All
// seeds are state "update" (clean copies the sync will refresh); the
// overridden/conflict states get their own fixtures with the review UI.
export const seedSkillUpdates: SkillUpdate[] = [
  { name: "security-design", state: "update" },
  { name: "go", state: "update" },
  { name: "code-review", state: "update" },
];
