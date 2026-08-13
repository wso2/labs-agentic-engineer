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

// One-shot entrypoint for the per-cycle coding-agent pod.
//
// OpenChoreo renders a batch/v1 Job from the coding-agent ComponentType,
// passing the dispatch payload via AEP_* env vars (no HTTP, no token in body).
// We reuse the same provisionWorkspace + runClaudeQuery code that the legacy
// HTTP server used; only the wrapper changes shape.
//
// Exit codes:
//   0 — agent reported success
//   1 — agent reported failure (commit/push/PR-ready issue, etc.)
//   2 — provisioning or unexpected error before the agent ran
//
// The BFF cycle watcher classifies the Job's pod truth (exit, timeout,
// startup failure) and settles the run cycle from it.

import { randomUUID } from "node:crypto";
import { provisionWorkspace } from "./lib/workspace.js";
import { runClaudeQuery } from "./lib/runner.js";
import { openTaskLog } from "./lib/logger.js";
import { isUUID, isSlug } from "./lib/uuid.js";
import type { DispatchRequest } from "./lib/types.js";
import { emit, primeScrubber } from "./lib/progress/emitter.js";
import { installConsoleScrubber } from "./lib/progress/console_scrub.js";
import { resolveTaskSkills } from "./lib/skills_resolver.js";
import { listMirroredSkills, readSkillBodies, resolvePinnedSkills } from "./lib/skills_presence.js";
import { ClientCredentialsTokenProvider } from "./lib/oauth.js";
import {
  fetchValidationContext,
  VALIDATION_CONTEXT_FILE,
} from "./lib/validation_context.js";

function requireEnv(name: string): string {
  const v = process.env[name];
  if (v === undefined || v === "") {
    throw new Error(`missing required env var: ${name}`);
  }
  return v;
}

function readDispatchFromEnv(): DispatchRequest {
  const taskId = requireEnv("AEP_TASK_ID");
  const orgId = requireEnv("AEP_ORG_ID");
  const projectId = requireEnv("AEP_PROJECT_ID");
  const componentName = requireEnv("AEP_COMPONENT_NAME");
  const repoUrl = requireEnv("AEP_REPO_URL");
  // WS2.4 — Bearer is optional when publisher cc creds are present;
  // required otherwise. Validated below after we peek at publisher envs.
  const bearer = process.env.AEP_BEARER ?? "";
  const gitServiceUrl = requireEnv("AEP_GIT_SERVICE_URL");
  const prompt = requireEnv("AEP_PROMPT");
  const identityName = requireEnv("AEP_IDENTITY_NAME");
  const identityEmail = requireEnv("AEP_IDENTITY_EMAIL");
  const identityLogin = process.env.AEP_IDENTITY_LOGIN || "";
  const correlationId = process.env.AEP_CORRELATION_ID || randomUUID();
  // Endpoint Spec Discovery (B1/B2) — BFF MCP server coordinates. The BFF
  // stamps AEP_MCP_URL unconditionally but only stamps AEP_MCP_TOKEN when
  // minting succeeded, so both are optional here; runner.ts guards on BOTH
  // being present before registering the in-process mcpServers entry.
  const mcpUrl = process.env.AEP_MCP_URL || "";
  const mcpToken = process.env.AEP_MCP_TOKEN || "";

  const taskKind = process.env.AEP_TASK_KIND || "implementation";
  if (taskKind !== "implementation" && taskKind !== "validation") {
    throw new Error(`AEP_TASK_KIND must be "implementation" or "validation": ${taskKind}`);
  }

  const publisherClientId = process.env.PUBLISHER_CLIENT_ID ?? "";
  const publisherClientSecret = process.env.PUBLISHER_CLIENT_SECRET ?? "";
  const publisherTokenUrl = process.env.PUBLISHER_TOKEN_URL ?? "";
  const hasPublisher =
    publisherClientId !== "" && publisherClientSecret !== "" && publisherTokenUrl !== "";
  if (!hasPublisher && bearer === "") {
    throw new Error(
      "neither AEP_BEARER nor PUBLISHER_CLIENT_ID/SECRET/TOKEN_URL set — runner has no auth material",
    );
  }

  if (!isUUID(taskId)) throw new Error(`AEP_TASK_ID is not a valid UUID: ${taskId}`);
  if (!isSlug(orgId)) throw new Error(`AEP_ORG_ID is not a valid slug: ${orgId}`);
  if (!isSlug(projectId)) throw new Error(`AEP_PROJECT_ID is not a valid slug: ${projectId}`);
  if (componentName.includes("/") || componentName.includes("..")) {
    throw new Error(`AEP_COMPONENT_NAME must not contain '/' or '..': ${componentName}`);
  }

  return {
    taskId,
    orgId,
    projectId,
    componentName,
    repoUrl,
    bearer,
    identity: { name: identityName, email: identityEmail, login: identityLogin || undefined },
    gitServiceUrl,
    prompt,
    correlationId,
    mcpUrl: mcpUrl || undefined,
    mcpToken: mcpToken || undefined,
    taskKind,
    // OFF unless a human opts this pod in. The sinks are files in a workspace
    // nothing collects, so in the cluster they are write-only — and the debug
    // log holds prompt text. The opt-in exists because a stall that only
    // reproduces here would otherwise be undiagnosable: set AEP_RUNNER_DEBUG=1
    // on the Job and read the files off the pod before it exits.
    debug: process.env.AEP_RUNNER_DEBUG === "1",
  };
}

async function main(): Promise<number> {
  // Before anything logs: the BFF forwards this pod's console output into the
  // user-visible build log, so every line has to pass the scrubber.
  installConsoleScrubber();

  let req: DispatchRequest;
  try {
    req = readDispatchFromEnv();
  } catch (err) {
    // Nothing is enrolled as a literal yet (the bearer hasn't been read), so
    // this line is covered only by the scrubber's token-shape patterns.
    console.error("[oneshot] env validation failed:", err instanceof Error ? err.message : String(err));
    return 2;
  }

  // WS2.4 — when publisher cc envs are set, mint a token via the cc helper
  // and prefer it for runner→BFF callbacks. Falls back to AEP_BEARER
  // (legacy TaskJWT) when cc envs are absent.
  const publisherClientId = process.env.PUBLISHER_CLIENT_ID ?? "";
  const publisherClientSecret = process.env.PUBLISHER_CLIENT_SECRET ?? "";
  const publisherTokenUrl = process.env.PUBLISHER_TOKEN_URL ?? "";
  let ccProvider: ClientCredentialsTokenProvider | undefined;
  if (publisherClientId !== "" && publisherClientSecret !== "" && publisherTokenUrl !== "") {
    ccProvider = new ClientCredentialsTokenProvider({
      tokenUrl: publisherTokenUrl,
      clientId: publisherClientId,
      clientSecret: publisherClientSecret,
    });
    console.log("[oneshot] publisher cc creds present — using client_credentials for runner callbacks");
  }

  // WS2.6 — always target the path-scoped refresh endpoint. It accepts BOTH
  // the publisher-cc token AND the legacy AEP_BEARER Task-JWT (verified first),
  // so the unscoped /internal/v1/credentials/refresh route is retired.
  const platformURL = process.env.AEP_PLATFORM_URL ?? "";
  if (platformURL) {
    const base = platformURL.endsWith("/") ? platformURL.slice(0, -1) : platformURL;
    req.refreshUrl = `${base}/internal/v1/executions/${encodeURIComponent(req.taskId)}/credentials/refresh`;
  }

  // When publisher cc is in play, pre-mint a token and stuff it into req.bearer.
  // provisionWorkspace doesn't otherwise need to know which auth mode is active.
  if (ccProvider) {
    try {
      const ccToken = await ccProvider.getToken();
      req.bearer = ccToken;
      primeScrubber([ccToken]);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error("[oneshot] cc token mint failed:", msg);
      return 2;
    }
  }

  // BOTH credential variables: a run authenticates with exactly one of them
  // (an org may bill its coding agent to a Claude Code OAuth token instead of
  // an API key), and priming only the one that happens to be unset would leave
  // the other unredacted in the progress feed. Unset entries are skipped.
  primeScrubber([
    process.env.ANTHROPIC_API_KEY,
    process.env.CLAUDE_CODE_OAUTH_TOKEN,
    req.bearer,
    publisherClientSecret,
    req.mcpToken,
  ]);

  emit({
    kind: "phase",
    phase: "workspace_provisioning",
  });

  let layout;
  try {
    layout = await provisionWorkspace(req);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    emit({ kind: "result", status: "failure", error: `workspace_provisioning: ${msg}` });
    console.error("[oneshot] provisionWorkspace failed:", msg);
    return 2;
  }

  emit({ kind: "phase", phase: "workspace_ready" });

  // Per-task skills — read the design's pinned skill names from the project
  // clone (no network: `.claude/skills/` is already the BFF-mirrored, filtered
  // set of the org's coding-relevant skills), then resolve those pins against
  // the copies actually on disk. A dangling pin is warned about and skipped,
  // never fatal — the guidance is missing, which degrades the build, but
  // aborting loses it entirely. If `.claude/skills/` is absent altogether the
  // run proceeds with the workflow skill only.
  //
  // Scope: an implementation run is a MILESTONE cycle — one branch, one PR,
  // any component in the milestone — so it takes the union of every component's
  // skillsPinned. Its AEP_COMPONENT_NAME is the `aep-milestone` sentinel and
  // must not be used to pick a design file. A validation run applies no design
  // skills at all: it is black-box verification driven by the `aep-validation`
  // skill (AEP_TASK_KIND), and builds nothing.
  // A validation run applies no DESIGN skills: nothing is pinned and nothing
  // is pinned, so the allowlist below leaves the design skills out entirely.
  let availableSkillNames: string[] = [];
  let pinnedBodies = "";
  if (req.taskKind === "validation") {
    console.log("[oneshot] validation run — no design skills apply; using the aep-validation skill only");
    // PREFLIGHT: where the deployed system is, fetched by the platform before the
    // agent starts. Fatal on purpose — an agent that cannot learn its targets has
    // no honest way forward, and when this was the skill's own `curl` a 404 sent
    // it scanning the pod network for half an hour instead of stopping.
    try {
      const ctx = await fetchValidationContext({
        platformUrl: platformURL,
        cycleId: req.taskId,
        bearer: req.bearer,
      });
      console.log(
        `[oneshot] validation context: ${ctx.endpoints.length} deployed endpoint(s) → ${VALIDATION_CONTEXT_FILE}`,
      );
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      emit({ kind: "result", status: "failure", error: `validation_context: ${msg}` });
      console.error(`[oneshot] validation context unavailable — not starting the agent: ${msg}`);
      return 2;
    }
  } else {
    const pinned = await resolveTaskSkills({
      workspace: layout.workspace,
      scope: { kind: "project" },
      log: (l) => console.log(l),
    });
    const { preload, dangling } = await resolvePinnedSkills(layout.workspace, pinned, (l) => console.log(l));
    if (dangling.length > 0) {
      console.warn(
        `[oneshot] ⚠️  ${dangling.length} pinned skill(s) missing from .claude/skills/ — proceeding without them: ${dangling.join(", ")}`,
      );
    }
    // Every mirrored skill is allowed — the SDK's `skills:` is an allowlist, so
    // anything omitted here cannot be invoked at all. The pinned subset also
    // goes into the system prompt, which is the only thing that actually
    // preloads guidance.
    availableSkillNames = await listMirroredSkills(layout.workspace);
    pinnedBodies = await readSkillBodies(layout.workspace, preload);
    console.log(
      `[oneshot] ${availableSkillNames.length} skill(s) available, ${preload.length} pinned into context`,
    );
  }

  const log = openTaskLog(layout.workspace);
  let completion: Promise<{ exitCode: number }>;
  try {
    ({ completion } = runClaudeQuery(req, layout, log, { availableSkillNames, pinnedBodies }));
  } catch (err) {
    // The mirror carries no workflow skill (see requireWorkflowBodies), so this
    // run has no procedure to follow. Fail the build rather than let the agent
    // improvise one and report success — the mirror's writes are best-effort by
    // design, and this is the point where the cause is still obvious.
    const msg = err instanceof Error ? err.message : String(err);
    emit({ kind: "result", status: "failure", error: `skills: ${msg}` });
    console.error(`[oneshot] ${msg}`);
    return 2;
  }
  const result = await completion;
  return result.exitCode;
}

main()
  .then((code) => process.exit(code))
  .catch((err) => {
    console.error("[oneshot] unhandled error:", err);
    process.exit(2);
  });
