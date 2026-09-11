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

import { http, HttpResponse } from "msw";
import type { components } from "../../generated/aep-api";

type ApiError = components["schemas"]["Error"];
import {
  codingAgentDefaultsFixture,
  codingAgentRuntimeUnavailable,
  codingLlmValidationError,
  codingLlmWithoutDefault,
  configLoadError,
  gitProviderDisconnectRejected,
  githubConnectedFixture,
  gitProviderValidationError,
  IMPORT_INVALID_SENTINEL,
  IMPORT_WARN_SENTINEL,
  importFileInvalidError,
  importWarningsFixture,
  INVALID_CREDENTIAL_VALUE,
  llmConnectedFixture,
  llmValidationError,
  seedSkillUpdates,
  seedSkills,
  skillsLoadError,
  skillsSyncError,
  skillsRepoUrl,
  type SettingsScenario,
} from "../fixtures/settings";

type CodingAgentProjection = components["schemas"]["CodingAgentProjection"];
type ConfigPatch = components["schemas"]["ConfigPatch"];
type ConfigProjection = components["schemas"]["ConfigProjection"];
type GitProviderProjection = components["schemas"]["GitProviderProjection"];
type LLMProjection = components["schemas"]["LLMProjection"];
type CreateSkillInput = components["schemas"]["CreateSkillInput"];
type UpdateSkillInput = components["schemas"]["UpdateSkillInput"];
type SkillUpdate = components["schemas"]["SkillUpdate"];
type SkillSummary = components["schemas"]["SkillSummary"];
type SkillDetailBody = components["schemas"]["SkillDetailBody"];

function scenario(): SettingsScenario {
  return (
    (localStorage.getItem("aep:mock:settings") as SettingsScenario | null) ??
    "empty"
  );
}

function errorJson(body: ApiError, status: number) {
  return HttpResponse.json(body, { status });
}

// Session-local state layered on top of the scenario baseline, mirroring
// handlers/projects.ts's createdProjects pattern.
let gitProvider: GitProviderProjection | null = null;
let llm: LLMProjection | null = null;
// null = the coding agent reuses `llm`'s key. Not a mode flag — the absence of
// a key IS "reuse", exactly as on the server (ADR-0016).
let codingLlm: LLMProjection | null = null;
// Always present, unlike the credentials: an org has an effective runtime and
// model from the moment it exists. Reset (codingAgent:null) restores this very
// value INCLUDING the null stamps — "reset to defaults" and "never touched"
// are the same observable state, which is what the contract says.
let codingAgent: CodingAgentProjection = { ...codingAgentDefaultsFixture };
let skills: SkillDetailBody[] = [];
let skillUpdates: SkillUpdate[] = [];
let initialized = false;

// Persist the org's connection state (GitHub + Anthropic) across reloads so
// completing onboarding sticks — otherwise a refresh (or a Vite HMR update)
// drops the in-memory connection and the onboarding wizard re-gates every
// route, leaving project pages like Builds unreachable.
const CONNECTION_KEY = "aep:mock:connection";

function persistConnection(): void {
  try {
    localStorage.setItem(CONNECTION_KEY, JSON.stringify({ gitProvider, llm }));
  } catch {
    /* quota — non-fatal in mock mode */
  }
}

function ensureInitialized() {
  if (initialized) return;
  initialized = true;
  if (scenario() === "connected") {
    gitProvider = { ...githubConnectedFixture };
    llm = { ...llmConnectedFixture };
  } else if (scenario() === "partial") {
    // Onboarding resume-after-abandon (#102): GitHub landed, Anthropic
    // didn't — the wizard must open at its first incomplete step.
    gitProvider = { ...githubConnectedFixture };
  }
  // A persisted connection (the user onboarded in an earlier page load) wins
  // over the scenario baseline so the wizard doesn't reappear on refresh.
  try {
    const raw = localStorage.getItem(CONNECTION_KEY);
    if (raw) {
      const saved = JSON.parse(raw) as {
        gitProvider: GitProviderProjection | null;
        llm: LLMProjection | null;
      };
      gitProvider = saved.gitProvider;
      llm = saved.llm;
    }
  } catch {
    /* ignore malformed persisted state */
  }
  codingAgent = { ...codingAgentDefaultsFixture };
  skills = seedSkills.map((s) => ({ ...s }));
  skillUpdates = seedSkillUpdates.map((u) => ({ ...u }));
}

function toSummary(s: SkillDetailBody): SkillSummary {
  return {
    name: s.name,
    kind: s.kind,
    description: s.description,
    contentSha: s.contentSha,
    editable: s.editable,
    deletable: s.deletable,
    enabled: s.enabled,
    required: s.required,
  };
}

function nextContentSha(name: string): string {
  return `sha-${name}-${Date.now()}`;
}

function extractDescription(skillMd: string): string {
  const match = /^description:\s*(.+)$/m.exec(skillMd);
  return match?.[1]?.trim() ?? "Custom skill";
}

function slugFromFileName(fileName: string): string {
  return fileName
    .replace(/\.(md|tgz|tar\.gz)$/i, "")
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

type ImportResult = components["schemas"]["ImportResult"];

// Shared by the tarball and URL import handlers: lands the skill in the
// session catalogue and builds the ImportResult (warn-sentinel names get
// the soft-warnings outcome — see fixtures/settings.ts).
function importSkill(name: string, source: string): ImportResult {
  const withWarnings = name.includes(IMPORT_WARN_SENTINEL);
  const existing = skills.find((s) => s.name === name);
  if (existing) {
    existing.contentSha = nextContentSha(name);
    existing.updatedAt = new Date().toISOString();
  } else {
    skills.push({
      orgId: "org-1",
      name,
      kind: "imported",
      editable: true,
      deletable: true,
      enabled: true,
      required: false,
      description: `Imported from ${source}.`,
      skillMd: `---\nname: ${name}\ndescription: Imported from ${source}.\n---\n\nImported skill body.`,
      references: {},
      binaryReferences: [],
      contentSha: nextContentSha(name),
      updatedAt: new Date().toISOString(),
    });
  }
  return {
    name,
    kind: "imported",
    ...(withWarnings ? {} : { license: "Apache-2.0" }),
    compatibility: withWarnings ? "partial" : "full",
    warnings: withWarnings ? [...importWarningsFixture] : [],
  };
}

function configProjection(): ConfigProjection {
  return {
    gitProvider,
    llm,
    codingLlm,
    codingAgent,
    idp: {
      kind: "platform",
      issuer: "https://idp.aep.local",
      jwksUrl: "https://idp.aep.local/.well-known/jwks.json",
      hasClientSecret: false,
      publisherClientId: "aep-console",
    },
  };
}

export const settingsHandlers = [
  http.get("*/api/v1/config", () => {
    ensureInitialized();
    if (scenario() === "error") return errorJson(configLoadError, 500);
    return HttpResponse.json(configProjection());
  }),

  http.patch("*/api/v1/config", async ({ request }) => {
    ensureInitialized();
    const body = (await request.json()) as ConfigPatch;

    // Reject phase — every section is judged BEFORE any of them is applied,
    // mirroring the real PATCH: a rejected section must never leave an earlier
    // one half-written, or the console would render state the server never had.
    if (body.llm != null && body.llm.apiKey === INVALID_CREDENTIAL_VALUE) {
      return errorJson(llmValidationError, 400);
    }
    if (body.codingLlm != null) {
      if (body.codingLlm.apiKey === INVALID_CREDENTIAL_VALUE) {
        return errorJson(codingLlmValidationError, 400);
      }
      // An override with nothing to override — including the case where this
      // very patch clears the key it would override.
      const defaultAfterPatch = body.llm === undefined ? llm : body.llm;
      if (defaultAfterPatch === null) {
        return errorJson(codingLlmWithoutDefault, 400);
      }
    }
    // The runtime enum carries more than the platform can run: an unavailable
    // one is rejected with a reason naming what is missing, never quietly
    // swapped for the one that works.
    if (
      body.codingAgent != null &&
      body.codingAgent.runtime === "opencode"
    ) {
      return errorJson(codingAgentRuntimeUnavailable, 422);
    }
    if (body.gitProvider !== undefined) {
      if (body.gitProvider === null) {
        return errorJson(gitProviderDisconnectRejected, 400);
      }
      if (body.gitProvider.pat === INVALID_CREDENTIAL_VALUE) {
        return errorJson(gitProviderValidationError, 400);
      }
    }

    // Persist phase.
    if (body.llm !== undefined) {
      if (body.llm === null) {
        llm = null;
        // The coding key overrides `llm` and cannot outlive it.
        codingLlm = null;
      } else {
        llm = {
          kind: "anthropic",
          credentialKind: "api_key",
          status: "connected",
          keyPrefix: body.llm.apiKey.slice(0, 7),
          keyLast4: body.llm.apiKey.slice(-4),
          connectedAt: new Date().toISOString(),
          lastValidatedAt: new Date().toISOString(),
        };
      }
    }

    if (body.codingLlm !== undefined) {
      if (body.codingLlm === null) {
        codingLlm = null;
      } else {
        codingLlm = {
          kind: "anthropic",
          // The coding agent may bill a Claude subscription instead: a
          // `claude setup-token` value is an oauth_token, not an api_key.
          credentialKind: body.codingLlm.apiKey.startsWith("sk-ant-oat")
            ? "oauth_token"
            : "api_key",
          status: "connected",
          keyPrefix: body.codingLlm.apiKey.slice(0, 7),
          keyLast4: body.codingLlm.apiKey.slice(-4),
          connectedAt: new Date().toISOString(),
          lastValidatedAt: new Date().toISOString(),
        };
      }
    }

    if (body.codingAgent !== undefined) {
      if (body.codingAgent === null) {
        codingAgent = { ...codingAgentDefaultsFixture };
      } else {
        // Both fields are optional so a client can move one without restating
        // the other — merge, never replace.
        codingAgent = {
          runtime: body.codingAgent.runtime ?? codingAgent.runtime,
          model: body.codingAgent.model ?? codingAgent.model,
          updatedAt: new Date().toISOString(),
          updatedBy: "dev@acme.example",
        };
      }
    }

    if (body.gitProvider != null) {
      const login = body.gitProvider.githubLogin || "acme-dev";
      gitProvider = {
        kind: "github",
        mode: "pat",
        status: "connected",
        githubLogin: login,
        identityLogin: login,
        identityName: "Acme Dev",
        identityEmail: "dev@acme.example",
        connectedAt: new Date().toISOString(),
        lastValidatedAt: new Date().toISOString(),
        selectedRepos: ["acme-dev/demo-shop"],
      };
    }

    persistConnection();
    return HttpResponse.json(configProjection());
  }),

  http.post("*/api/v1/config/git-provider/disconnect", () => {
    ensureInitialized();
    gitProvider = null;
    persistConnection();
    return HttpResponse.json({ status: "disconnected" });
  }),

  // Static /skills/* routes must register before the dynamic /skills/:name
  // handler below, or MSW would match "updates"/"import"/"sync" as a name.
  http.post("*/api/v1/skills/import", async ({ request }) => {
    ensureInitialized();
    let fileName = "";
    try {
      const formData = await request.formData();
      const file = formData.get("file");
      fileName = file instanceof File ? file.name : "";
    } catch {
      fileName = "";
    }
    if (!fileName || fileName.includes(IMPORT_INVALID_SENTINEL)) {
      return errorJson(importFileInvalidError, 400);
    }
    const name =
      slugFromFileName(fileName) || `imported-skill-${skills.length + 1}`;
    return HttpResponse.json(importSkill(name, "an AgentSkills tarball"), {
      status: 201,
    });
  }),

  // All-or-nothing, mirroring the BE: no request body, reconcile everything.
  // Per #102 the BE creates the org's skills repo first when it's missing;
  // the mock treats that as part of the same opaque call.
  http.post("*/api/v1/skills/sync", () => {
    ensureInitialized();
    if (scenario() === "sync-error") return errorJson(skillsSyncError, 502);
    const targets = skillUpdates;

    for (const t of targets) {
      const existing = skills.find((s) => s.name === t.name);
      if (existing) {
        existing.contentSha = nextContentSha(t.name);
        existing.updatedAt = new Date().toISOString();
      } else {
        // A synced-in skill the repo didn't have yet. Unmarked frontmatter is
        // an `org` skill, matching the BE's frontmatterKind default —
        // deletable = editable for org-kind skills.
        skills.push({
          orgId: "org-1",
          name: t.name,
          kind: "org",
          editable: true,
          deletable: true,
          enabled: true,
          required: false,
          description: `${t.name} (platform-shipped)`,
          skillMd: `---\nname: ${t.name}\ndescription: ${t.name} (platform-shipped)\n---\n\nPlatform-shipped skill body.`,
          references: {},
          binaryReferences: [],
          contentSha: nextContentSha(t.name),
          updatedAt: new Date().toISOString(),
        });
      }
    }
    const updated = targets.length;
    skillUpdates = [];

    return HttpResponse.json({ status: "synced", updated });
  }),

  http.get("*/api/v1/skills/updates", () => {
    ensureInitialized();
    if (scenario() === "error") return errorJson(skillsLoadError, 500);
    return HttpResponse.json({ updates: skillUpdates, count: skillUpdates.length });
  }),

  http.get("*/api/v1/skills", () => {
    ensureInitialized();
    if (scenario() === "error") return errorJson(skillsLoadError, 500);
    return HttpResponse.json({
      skills: skills.map(toSummary),
      repoUrl: skillsRepoUrl,
    });
  }),

  http.post("*/api/v1/skills", async ({ request }) => {
    ensureInitialized();
    const body = (await request.json()) as CreateSkillInput;
    if (skills.some((s) => s.name === body.name)) {
      return errorJson(
        {
          code: "conflict",
          message: `A skill named ${body.name} already exists`,
        },
        409,
      );
    }
    const created: SkillDetailBody = {
      orgId: "org-1",
      name: body.name,
      kind: "org",
      // A freshly created skill is always org-kind, so deletable = editable.
      editable: true,
      deletable: true,
      enabled: true,
      required: false,
      description: extractDescription(body.skillMd),
      skillMd: body.skillMd,
      references: body.references ?? {},
      binaryReferences: [],
      contentSha: nextContentSha(body.name),
      updatedAt: new Date().toISOString(),
    };
    skills.push(created);
    return HttpResponse.json(created, { status: 201 });
  }),

  http.get("*/api/v1/skills/:name", ({ params }) => {
    ensureInitialized();
    const skill = skills.find((s) => s.name === params.name);
    if (!skill) {
      return errorJson(
        {
          code: "not_found",
          message: `Skill ${String(params.name)} not found`,
        },
        404,
      );
    }
    return HttpResponse.json(skill);
  }),

  // Non-destructive availability toggle (ADR-0014): flips the fixture's
  // `enabled` flag in place — content, kind, and editable/deletable are
  // untouched.
  http.patch("*/api/v1/skills/:name", async ({ params, request }) => {
    ensureInitialized();
    const skill = skills.find((s) => s.name === params.name);
    if (!skill) {
      return errorJson(
        {
          code: "not_found",
          message: `Skill ${String(params.name)} not found`,
        },
        404,
      );
    }
    const body = (await request.json()) as { enabled: boolean };
    skill.enabled = body.enabled;
    return HttpResponse.json(skill);
  }),

  http.put("*/api/v1/skills/:name", async ({ params, request }) => {
    ensureInitialized();
    const skill = skills.find((s) => s.name === params.name && s.editable);
    if (!skill) {
      return errorJson(
        {
          code: "not_found",
          message: `Editable skill ${String(params.name)} not found`,
        },
        404,
      );
    }
    const body = (await request.json()) as UpdateSkillInput;
    skill.skillMd = body.skillMd;
    skill.references = body.references ?? {};
    skill.description = extractDescription(body.skillMd);
    skill.contentSha = nextContentSha(skill.name);
    skill.updatedAt = new Date().toISOString();
    return HttpResponse.json(skill);
  }),

  http.delete("*/api/v1/skills/:name", ({ params }) => {
    ensureInitialized();
    const skill = skills.find((s) => s.name === params.name);
    if (!skill) {
      return errorJson(
        {
          code: "not_found",
          message: `Skill ${String(params.name)} not found`,
        },
        404,
      );
    }
    // Mirrors the real BE guard: deletable = editable — a platform-kind
    // skill (always reconcile-managed) is neither.
    if (!skill.deletable) {
      return errorJson(
        { code: "forbidden", message: "built-in skills are read-only" },
        403,
      );
    }
    skills = skills.filter((s) => s.name !== params.name);
    return HttpResponse.json({ status: "deleted" });
  }),
];
