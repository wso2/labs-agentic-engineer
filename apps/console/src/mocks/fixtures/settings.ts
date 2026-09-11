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

type CodingAgentProjection = components["schemas"]["CodingAgentProjection"];
type GitProviderProjection = components["schemas"]["GitProviderProjection"];
type LLMProjection = components["schemas"]["LLMProjection"];
type SkillDetailBody = components["schemas"]["SkillDetailBody"];
type SkillUpdate = components["schemas"]["SkillUpdate"];
type ApiError = components["schemas"]["Error"];

// Scenario switch for the Settings (#96) and Onboarding (#102) features.
// Toggle in devtools:
//   localStorage.setItem('aep:mock:settings',
//     'empty' | 'partial' | 'connected' | 'error' | 'sync-error')
// "empty": nothing connected yet (the default — triggers the onboarding
// gate; also exercises Settings' not-connected states).
// "partial": GitHub connected, Anthropic not — the onboarding wizard opens
// at its first incomplete step (resume-after-abandon, #102).
// "connected": GitHub + Anthropic already connected (no onboarding).
// "error": GET /config and GET /skills fail (load-error state).
// "sync-error": config empty and POST /skills/sync fails — exercises the
// wizard's bootstrap-failure step (Retry / Continue anyway, #102).
export type SettingsScenario =
  | "empty"
  | "partial"
  | "connected"
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

export const llmConnectedFixture: LLMProjection = {
  kind: "anthropic",
  credentialKind: "api_key",
  status: "connected",
  keyPrefix: "sk-ant-",
  keyLast4: "wxyz",
  connectedAt: "2026-06-01T12:05:00Z",
  lastValidatedAt: "2026-07-01T09:00:00Z",
};

// Every org has an effective runtime and model, so this section is never
// absent — unlike the credential sections. Null updatedAt/updatedBy is the
// platform-defaults state: nobody has ever chosen, which the console must not
// render as somebody having picked these very values.
export const codingAgentDefaultsFixture: CodingAgentProjection = {
  runtime: "claude-code",
  model: "claude-sonnet-5",
  updatedAt: null,
  updatedBy: null,
};

// `opencode` is in the contract's enum but the platform ships no adapter for
// it, so the API rejects it by name rather than silently substituting the
// runtime it can run. 422 + body.codingAgent, matching the real rejection.
export const codingAgentRuntimeUnavailable: ApiError = {
  code: "validation_failed",
  message:
    "coding agent: runtime \"opencode\" is not available on this platform — no adapter is installed for it",
  details: [
    {
      field: "body.codingAgent",
      message:
        "coding agent: runtime \"opencode\" is not available on this platform — no adapter is installed for it",
    },
  ],
};

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

export const llmValidationError: ApiError = {
  code: "validation_failed",
  message: "the provided API key was rejected by Anthropic",
  details: [
    {
      field: "body.llm",
      message: "the provided API key was rejected by Anthropic",
    },
  ],
};

export const codingLlmValidationError: ApiError = {
  code: "validation_failed",
  message: "the provided API key was rejected by Anthropic",
  details: [
    {
      field: "body.codingLlm",
      message: "the provided API key was rejected by Anthropic",
    },
  ],
};

// The coding agent key overrides the org's key, so it cannot be set before
// there is one. Same shape the BFF returns (sectionErrorFrom → patchconfig).
export const codingLlmWithoutDefault: ApiError = {
  code: "validation_failed",
  message:
    "anthropic: connect the organization's Anthropic key before setting a coding-agent key",
  details: [
    {
      field: "body.codingLlm",
      message:
        "anthropic: connect the organization's Anthropic key before setting a coding-agent key",
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
