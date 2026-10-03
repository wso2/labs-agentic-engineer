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
  agentsDefaultsFixture,
  agentsOpenCodeFixture,
  claudeCodeOnlyRuntimes,
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
  llmDisconnectedAtFixture,
  llmFormatsFor,
  llmOllamaFixture,
  MODEL_PROBE_PROVIDER_LIMIT_KEY,
  MODEL_PROBE_REJECTED_KEY,
  MODEL_PROBE_TEST_RATE_LIMIT_KEY,
  modelListings,
  ollamaVisionModels,
  pricedModels,
  sectionError,
  seedSkillUpdates,
  seedSkills,
  skillsLoadError,
  skillsSyncError,
  skillsRepoUrl,
  subscriptionFixture,
  subscriptionValidationError,
  type SettingsScenario,
} from "../fixtures/settings";

type AgentsProjection = components["schemas"]["AgentsProjection"];
type ConfigPatch = components["schemas"]["ConfigPatch"];
type ConfigProjection = components["schemas"]["ConfigProjection"];
type GitProviderProjection = components["schemas"]["GitProviderProjection"];
type LLMProjection = components["schemas"]["LLMProjection"];
type LLMPatch = components["schemas"]["LLMPatch"];
type LLMCheck = components["schemas"]["LLMCheck"];
type LLMCapabilities = components["schemas"]["LLMCapabilities"];
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

// A `claude setup-token` value is a subscription token; anything else shaped
// `sk-ant-` is an API key. The server classifies by the same prefix.
function isSubscriptionToken(key: string): boolean {
  return key.startsWith("sk-ant-oat");
}

// Session-local state layered on top of the scenario baseline, mirroring
// handlers/projects.ts's createdProjects pattern.
let gitProvider: GitProviderProjection | null = null;
let llm: LLMProjection | null = null;
// When the org's connection was last disconnected; null once one is saved.
let llmDisconnectedAt: string | null = null;
// Always present, unlike the connection: an org has an effective runtime from
// the moment it exists. Reset (agents:null) restores this very value INCLUDING
// the null stamps and no subscription — "reset to defaults" and "never
// touched" are the same observable state, which is what the contract says.
// `subscription` is the Claude subscription coding runs bill (null = they
// bill the connection's key).
let agents: AgentsProjection = { ...agentsDefaultsFixture };
let skills: SkillDetailBody[] = [];
let skillUpdates: SkillUpdate[] = [];
let initialized = false;

// Persist the org's connection state (GitHub, the model connection, the
// coding agent) across reloads so completing onboarding sticks — otherwise a
// refresh (or a Vite HMR update) drops the in-memory connection and the
// onboarding wizard re-gates every route, leaving project pages like Builds
// unreachable. Versioned: a value saved in the pre-connection shape is ignored.
const CONNECTION_KEY = "aep:mock:connection:v2";

function persistConnection(): void {
  try {
    localStorage.setItem(
      CONNECTION_KEY,
      JSON.stringify({ gitProvider, llm, llmDisconnectedAt, agents }),
    );
  } catch {
    /* quota — non-fatal in mock mode */
  }
}

function ensureInitialized() {
  if (initialized) return;
  initialized = true;
  const anthropicScenarios: SettingsScenario[] = [
    "connected",
    "subscription",
    "opencode",
    "opencode-unavailable",
  ];
  if (anthropicScenarios.includes(scenario())) {
    gitProvider = { ...githubConnectedFixture };
    llm = { ...llmConnectedFixture };
  } else if (scenario() === "ollama") {
    gitProvider = { ...githubConnectedFixture };
    llm = { ...llmOllamaFixture };
  } else if (scenario() === "partial") {
    // Onboarding resume-after-abandon (#102): GitHub landed, the model
    // connection didn't — the wizard must open at its first incomplete step.
    gitProvider = { ...githubConnectedFixture };
  } else if (scenario() === "disconnected") {
    gitProvider = { ...githubConnectedFixture };
    llmDisconnectedAt = llmDisconnectedAtFixture;
  }
  agents =
    scenario() === "opencode" || scenario() === "ollama"
      ? { ...agentsOpenCodeFixture }
      : scenario() === "opencode-unavailable"
        ? { ...agentsOpenCodeFixture, availableRuntimes: claudeCodeOnlyRuntimes }
      : scenario() === "subscription"
        ? { ...agentsDefaultsFixture, subscription: { ...subscriptionFixture } }
        : { ...agentsDefaultsFixture };
  // A persisted connection (the user onboarded in an earlier page load) wins
  // over the scenario baseline so the wizard doesn't reappear on refresh.
  try {
    const raw = localStorage.getItem(CONNECTION_KEY);
    if (raw) {
      const saved = JSON.parse(raw) as {
        gitProvider: GitProviderProjection | null;
        llm: LLMProjection | null;
        llmDisconnectedAt?: string | null;
        agents?: AgentsProjection;
      };
      gitProvider = saved.gitProvider;
      llm = saved.llm;
      llmDisconnectedAt = saved.llmDisconnectedAt ?? null;
      if (saved.agents) agents = saved.agents;
    }
  } catch {
    /* ignore malformed persisted state */
  }
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
    ...(llm === null && llmDisconnectedAt ? { llmDisconnectedAt } : {}),
    agents,
    llmFormats: llmFormatsFor(agents.availableRuntimes),
    idp: {
      kind: "platform",
      issuer: "https://idp.aep.local",
      jwksUrl: "https://idp.aep.local/.well-known/jwks.json",
      hasClientSecret: false,
      publisherClientId: "aep-console",
    },
  };
}

// ---- The model connection: merge and probe, as the server's judge does ----

interface MergedConnection {
  kind: LLMProjection["kind"];
  baseURL: string;
  model: string;
  /** The key the probe presents: typed, or "" for the stored one. */
  apiKey: string;
}

type Refusal = { error: ApiError; status: number };

function hostOf(url: string): string {
  try {
    return new URL(url).host.toLowerCase();
  } catch {
    return "";
  }
}

/** The server's preview: the last 4, prefixed by the first 4 on a key of 24 or more. */
function keyPreview(key: string): string {
  return key.length >= 24 ? `${key.slice(0, 4)}…${key.slice(-4)}` : key.slice(-4);
}

/**
 * The patch merged over the saved connection (a first connect fills the
 * format's defaults), with the rules that need no network: required fields, a
 * key for a new host, the key's length.
 */
function mergeConnection(patch: LLMPatch, test: boolean): MergedConnection | Refusal {
  const formats = llmFormatsFor(agents.availableRuntimes);
  const kind = patch.kind ?? llm?.kind;
  if (!kind || (llm === null && !patch.apiKey && !test)) {
    return { status: 400, error: sectionError("llm", "llm_field_required", "a first connection needs its API format and key") };
  }
  const format = formats.find((f) => f.kind === kind);
  const baseURL = patch.baseURL ?? llm?.baseURL ?? format?.defaultBaseURL ?? "";
  if (baseURL === "") {
    return { status: 400, error: sectionError("llm", "llm_field_required", `the ${kind} format has no default URL; enter the base URL`) };
  }
  const model = patch.model ?? llm?.model ?? format?.defaultModel ?? "";
  const apiKey = patch.apiKey ?? "";
  let url: URL;
  try {
    url = new URL(baseURL);
  } catch {
    return { status: 400, error: sectionError("llm", "llm_base_url_invalid", "Use an https URL.") };
  }
  if (url.protocol !== "https:" || url.username || url.search || url.hash) {
    return { status: 400, error: sectionError("llm", "llm_base_url_invalid", "Use an https URL, with no credentials, query or fragment.") };
  }
  const host = url.host.toLowerCase();
  if (host.includes("internal") || host.includes("localhost") || host.endsWith(".local")) {
    return {
      status: 400,
      error: sectionError("llm", "llm_host_refused", `Refused: ${host} resolves to a private address. Only public https endpoints are allowed.`),
    };
  }
  const newHost = llm === null || hostOf(llm.baseURL) !== host;
  if (newHost && apiKey === "") {
    return {
      status: 400,
      error: sectionError("llm", llm === null ? "llm_field_required" : "llm_key_required_for_new_host", `a new host needs its own key: paste the API key for ${host}`),
    };
  }
  if (apiKey !== "" && apiKey.length < 12) {
    return { status: 400, error: sectionError("llm", "llm_key_too_short", "an API key has at least 12 characters; check it was copied whole") };
  }
  return { kind, baseURL, model, apiKey };
}

/** What the platform would compute for this connection (modelconn.CapabilitiesOf + the probe). */
function capabilitiesOf(kind: LLMProjection["kind"], host: string, model: string): LLMCapabilities {
  const anthropicApi = host === "api.anthropic.com";
  const listed = modelListings[host];
  return {
    claudeSubscription: anthropicApi && kind === "anthropic",
    webSearch: anthropicApi ? "anthropic-server-tool" : host === "ollama.com" ? "ollama-api" : "none",
    imageInput: anthropicApi
      ? "yes"
      : host === "ollama.com" && listed?.includes(model)
        ? ollamaVisionModels.includes(model) ? "yes" : "no"
        : "unknown",
    nativePdf: anthropicApi,
    generatedAgents: true,
  };
}

/** The probe: the endpoint's answer to the key, and whether it lists the model. */
function probeConnection(c: MergedConnection): { check: LLMCheck } | Refusal {
  const host = hostOf(c.baseURL);
  if (host.includes("unreachable")) {
    return { status: 400, error: sectionError("llm", "llm_unreachable", `could not reach ${host}: the connection timed out`) };
  }
  if (c.apiKey.includes(MODEL_PROBE_REJECTED_KEY)) {
    return { status: 400, error: sectionError("llm", "llm_key_rejected", `${host} answered 401: the key was rejected. Check it was copied whole.`) };
  }
  // A path-less Anthropic-format URL gains /v1, as the SDKs expect.
  const url = new URL(c.baseURL);
  const baseURL = c.kind === "anthropic" && (url.pathname === "/" || url.pathname === "")
    ? `${url.origin}/v1`
    : c.baseURL.replace(/\/$/, "");
  const listing = modelListings[host];
  const check: LLMCheck = {
    kind: c.kind,
    baseURL,
    model: c.model,
    modelListed: listing ? (listing.includes(c.model) ? "yes" : "no") : "unknown",
    priced: pricedModels[host]?.includes(c.model) ?? false,
    capabilities: capabilitiesOf(c.kind, host, c.model),
    ...(c.apiKey.includes(MODEL_PROBE_PROVIDER_LIMIT_KEY) ? { warning: "provider_limit" as const } : {}),
  };
  return { check };
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
    const formats = llmFormatsFor(agents.availableRuntimes);

    // Reject phase — every section is judged BEFORE any of them is applied,
    // mirroring the real PATCH: a rejected section must never leave an earlier
    // one half-written, or the console would render state the server never had.
    let next: MergedConnection | null | undefined;
    let check: LLMCheck | undefined;
    if (body.llm === null) {
      next = null;
    } else if (body.llm !== undefined) {
      const merged = mergeConnection(body.llm, false);
      if ("error" in merged) return errorJson(merged.error, merged.status);
      const runtimes = formats.find((f) => f.kind === merged.kind)?.runtimes ?? [];
      if (runtimes.length === 0) {
        return errorJson(
          sectionError("llm", "llm_format_has_no_runtime", `no coding agent on this installation runs the ${merged.kind} format`),
          400,
        );
      }
      const probed = probeConnection(merged);
      if ("error" in probed) return errorJson(probed.error, probed.status);
      next = merged;
      check = probed.check;
    }
    const connectionAfter: { kind: string; capabilities: LLMCapabilities } | null =
      next === undefined
        ? llm
        : next === null
          ? null
          : { kind: next.kind, capabilities: check!.capabilities };

    // Only a runtime this installation can run may be chosen.
    const runtime = body.agents?.runtime;
    if (runtime !== undefined && !agents.availableRuntimes.includes(runtime)) {
      return errorJson(
        sectionError("agents", "agents_runtime_unavailable", `runtime "${runtime}" is not available on this installation, which has no runner image for it (available: ${agents.availableRuntimes.join(", ")})`),
        400,
      );
    }
    const runtimeAfter = runtime ?? agents.runtime;
    if (runtimeAfter === "claude-code" && connectionAfter?.kind === "openai-compatible") {
      return errorJson(
        sectionError("agents", "agents_runtime_requires_anthropic_format", "Claude Code speaks only the Anthropic Messages format; choose OpenCode in the same save that switches the format"),
        400,
      );
    }
    // The AI agents card, judged as one on the state the patch leaves, as on
    // the server: a subscription needs Claude Code and a connection that
    // takes one.
    const newToken = body.agents?.subscription?.token;
    if (newToken !== undefined) {
      if (newToken === INVALID_CREDENTIAL_VALUE) {
        return errorJson(subscriptionValidationError, 400);
      }
      if (!isSubscriptionToken(newToken)) {
        return errorJson(
          sectionError("agents", "agents_subscription_token_required", "a Claude subscription takes a token from `claude setup-token` (sk-ant-oat…); the connection's API key belongs in the API key field"),
          400,
        );
      }
      if (connectionAfter === null) {
        return errorJson(
          sectionError("agents", "agents_subscription_requires_connection", "a Claude subscription needs a model connection on Anthropic's API; save the connection in the same save, or first"),
          400,
        );
      }
      if (!connectionAfter.capabilities.claudeSubscription) {
        return errorJson(
          sectionError("agents", "agents_subscription_requires_anthropic_host", "a Claude subscription bills only against Anthropic's own API, and this connection is elsewhere"),
          400,
        );
      }
      if (runtimeAfter !== "claude-code") {
        return errorJson(
          sectionError("agents", "agents_subscription_requires_claude_code", "a Claude subscription bills the coding agent only on Claude Code, and this save leaves the runtime on OpenCode. Choose Claude Code to use the subscription, or save OpenCode without one"),
          400,
        );
      }
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
    const now = new Date().toISOString();
    if (next === null) {
      llm = null;
      llmDisconnectedAt = now;
    } else if (next !== undefined && check) {
      llmDisconnectedAt = null;
      const sameHost = llm !== null && hostOf(llm.baseURL) === hostOf(next.baseURL);
      llm = {
        kind: next.kind,
        baseURL: check.baseURL,
        model: next.model,
        keyPreview: next.apiKey ? keyPreview(next.apiKey) : (llm?.keyPreview ?? ""),
        connectedAt: sameHost && llm ? llm.connectedAt : now,
        updatedAt: now,
        updatedBy: "dev@acme.example",
        priced: check.priced,
        capabilities: check.capabilities,
      };
    }

    if (body.agents !== undefined) {
      if (body.agents === null) {
        // The installation's runtimes are not the org's to reset.
        agents = { ...agentsDefaultsFixture, availableRuntimes: agents.availableRuntimes };
      } else {
        // Every field is optional so a client can move one without restating
        // the others — merge, never replace.
        const { runtime: nextRuntime, subscription } = body.agents;
        if (nextRuntime !== undefined) {
          agents = { ...agents, runtime: nextRuntime, updatedAt: now, updatedBy: "dev@acme.example" };
        }
        if (subscription === null) {
          agents = { ...agents, subscription: null };
        } else if (subscription !== undefined) {
          agents = {
            ...agents,
            subscription: {
              kind: "claude",
              status: "connected",
              keyPrefix: subscription.token.slice(0, 13),
              keyLast4: subscription.token.slice(-4),
              connectedAt: now,
              lastValidatedAt: now,
            },
          };
        }
      }
    }
    // A subscription the end state cannot use goes in the same save: OpenCode
    // cannot present one, and only Anthropic's own API takes one.
    if (agents.runtime !== "claude-code" || !llm?.capabilities.claudeSubscription) {
      agents = { ...agents, subscription: null };
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
        connectedAt: now,
        lastValidatedAt: now,
        selectedRepos: ["acme-dev/demo-shop"],
      };
    }

    persistConnection();
    return HttpResponse.json({ ...configProjection(), ...(check ? { llmCheck: check } : {}) });
  }),

  // Test connection: the same merge and probe as a save, nothing written.
  http.post("*/api/v1/config/llm/test", async ({ request }) => {
    ensureInitialized();
    const body = (await request.json()) as LLMPatch;
    if (body.apiKey?.includes(MODEL_PROBE_TEST_RATE_LIMIT_KEY)) {
      return errorJson(
        sectionError("llm", "llm_test_rate_limited", "too many connection tests from this organization; try again in a minute"),
        429,
      );
    }
    const merged = mergeConnection(body, true);
    if ("error" in merged) return errorJson(merged.error, merged.status);
    const probed = probeConnection(merged);
    if ("error" in probed) return errorJson(probed.error, probed.status);
    return HttpResponse.json(probed.check);
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
