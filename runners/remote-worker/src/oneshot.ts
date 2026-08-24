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
import { onDemandSkills, runClaudeQuery, type McpAuthOpts } from "./lib/runner.js";
import { openTaskLog } from "./lib/logger.js";
import { isUUID, isSlug } from "./lib/uuid.js";
import type { DispatchRequest } from "./lib/types.js";
import { emit, primeScrubber } from "./lib/progress/emitter.js";
import { installConsoleScrubber } from "./lib/progress/console_scrub.js";
import { resolveTaskSkills } from "./lib/skills_resolver.js";
import { listMirroredSkills, readSkillBodies, resolveSkillPresence } from "./lib/skills_presence.js";
import { ClientCredentialsTokenProvider } from "./lib/oauth.js";
import {
  fetchValidationContext,
  VALIDATION_CONTEXT_FILE,
} from "./lib/validation_context.js";
import type { ComponentEndpoint } from "./lib/validation_context.js";
import {
  curlConfigHome,
  curlResolveEntries,
  playwrightCliConfigHome,
  probeEndpoints,
  writeCurlResolveConfig,
  writePlaywrightCliConfig,
} from "./lib/endpoint_access.js";

function requireEnv(name: string): string {
  const v = process.env[name];
  if (v === undefined || v === "") {
    throw new Error(`missing required env var: ${name}`);
  }
  return v;
}

type PublisherCreds = { clientId: string; clientSecret: string; tokenUrl: string };

function requirePublisherCreds(): PublisherCreds {
  const clientId = process.env.PUBLISHER_CLIENT_ID ?? "";
  const clientSecret = process.env.PUBLISHER_CLIENT_SECRET ?? "";
  const tokenUrl = process.env.PUBLISHER_TOKEN_URL ?? "";
  if (clientId === "" || clientSecret === "" || tokenUrl === "") {
    throw new Error("PUBLISHER_CLIENT_ID/SECRET/TOKEN_URL required — runner has no platform credential");
  }
  return { clientId, clientSecret, tokenUrl };
}

function readDispatchFromEnv(): { req: DispatchRequest; publisher: PublisherCreds } {
  const taskId = requireEnv("AEP_TASK_ID");
  const orgId = requireEnv("AEP_ORG_ID");
  const projectId = requireEnv("AEP_PROJECT_ID");
  const componentName = requireEnv("AEP_COMPONENT_NAME");
  const repoUrl = requireEnv("AEP_REPO_URL");
  // Publisher CC is the Job's only platform credential (local and cloud).
  const gitServiceUrl = requireEnv("AEP_GIT_SERVICE_URL");
  const prompt = requireEnv("AEP_PROMPT");
  const identityName = requireEnv("AEP_IDENTITY_NAME");
  const identityEmail = requireEnv("AEP_IDENTITY_EMAIL");
  const identityLogin = process.env.AEP_IDENTITY_LOGIN || "";
  const correlationId = process.env.AEP_CORRELATION_ID || randomUUID();
  // Endpoint Spec Discovery (B1/B2) — BFF MCP server coordinates. The BFF
  // stamps AEP_MCP_URL; the runner presents the publisher CC token (minted
  // below), not a BFF MCP token. Design agent MCP stays a different caller.
  const mcpUrl = process.env.AEP_MCP_URL || "";

  const taskKind = process.env.AEP_TASK_KIND || "implementation";
  if (taskKind !== "implementation" && taskKind !== "validation") {
    throw new Error(`AEP_TASK_KIND must be "implementation" or "validation": ${taskKind}`);
  }

  const publisher = requirePublisherCreds();

  if (!isUUID(taskId)) throw new Error(`AEP_TASK_ID is not a valid UUID: ${taskId}`);
  if (!isSlug(orgId)) throw new Error(`AEP_ORG_ID is not a valid slug: ${orgId}`);
  if (!isSlug(projectId)) throw new Error(`AEP_PROJECT_ID is not a valid slug: ${projectId}`);
  if (componentName.includes("/") || componentName.includes("..")) {
    throw new Error(`AEP_COMPONENT_NAME must not contain '/' or '..': ${componentName}`);
  }

  return {
    req: {
      taskId,
      orgId,
      projectId,
      componentName,
      repoUrl,
      bearer: "",
      identity: { name: identityName, email: identityEmail, login: identityLogin || undefined },
      gitServiceUrl,
      prompt,
      correlationId,
      mcpUrl: mcpUrl || undefined,
      mcpToken: undefined,
      taskKind,
      // OFF unless a human opts this pod in. The sinks are files in a workspace
      // nothing collects, so in the cluster they are write-only — and the debug
      // log holds prompt text. The opt-in exists because a stall that only
      // reproduces here would otherwise be undiagnosable: set AEP_RUNNER_DEBUG=1
      // on the Job and read the files off the pod before it exits.
      debug: process.env.AEP_RUNNER_DEBUG === "1",
    },
    publisher,
  };
}

async function main(): Promise<number> {
  // Before anything logs: the BFF forwards this pod's console output into the
  // user-visible build log, so every line has to pass the scrubber.
  installConsoleScrubber();

  let req: DispatchRequest;
  let publisher: PublisherCreds;
  try {
    ({ req, publisher } = readDispatchFromEnv());
  } catch (err) {
    // Nothing is enrolled as a literal yet (the bearer hasn't been read), so
    // this line is covered only by the scrubber's token-shape patterns.
    console.error("[oneshot] env validation failed:", err instanceof Error ? err.message : String(err));
    return 2;
  }

  const ccProvider = new ClientCredentialsTokenProvider({
    tokenUrl: publisher.tokenUrl,
    clientId: publisher.clientId,
    clientSecret: publisher.clientSecret,
  });

  const platformURL = process.env.AEP_PLATFORM_URL ?? "";
  if (platformURL) {
    const base = platformURL.endsWith("/") ? platformURL.slice(0, -1) : platformURL;
    req.refreshUrl = `${base}/internal/v1/executions/${encodeURIComponent(req.taskId)}/credentials/refresh`;
  }

  try {
    const ccToken = await ccProvider.getToken();
    req.bearer = ccToken;
    req.mcpToken = ccToken;
    primeScrubber([ccToken]);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    console.error("[oneshot] cc token mint failed:", msg);
    return 2;
  }

  // BOTH credential variables: a run authenticates with exactly one of them
  // (an org may bill its coding agent to a Claude Code OAuth token instead of
  // an API key), and priming only the one that happens to be unset would leave
  // the other unredacted in the progress feed. Unset entries are skipped.
  primeScrubber([
    process.env.ANTHROPIC_API_KEY,
    process.env.CLAUDE_CODE_OAUTH_TOKEN,
    req.bearer,
    publisher.clientSecret,
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
  // must not be used to pick a design file.
  //
  // A validation run (AEP_TASK_KIND) applies no DESIGN skills at all — it is
  // black-box verification and builds nothing — so `pinnedBodies` stays empty
  // for it. Its workflow arrives another way: alwaysOnSkills() names
  // `aep-validation` and requireWorkflowBodies() injects the whole SKILL.md into
  // the system prompt, in context from the first token rather than invocable.
  //
  // The ALLOWLIST is not empty though, and that distinction cost a release.
  // Pinning nothing is not the same as allowing nothing: `skills:` gates the
  // Skill tool, so an empty array made the `playwright-cli` load that
  // `aep-validation` instructs impossible, and the agent read the mirror's files
  // by hand instead. onDemandSkills() names what the phase may load.
  let availableSkillNames: string[] = [];
  let pinnedBodies = "";
  if (req.taskKind === "validation") {
    console.log(
      "[oneshot] validation run — no design skills apply; aep-validation is injected as this run's workflow",
    );
    // PREFLIGHT: where the deployed system is, fetched by the platform before the
    // agent starts. Fatal on purpose — an agent that cannot learn its targets has
    // no honest way forward, and when this was the skill's own `curl` a 404 sent
    // it scanning the pod network for half an hour instead of stopping.
    let endpoints: ComponentEndpoint[];
    try {
      const ctx = await fetchValidationContext({
        platformUrl: platformURL,
        cycleId: req.taskId,
        bearer: req.bearer,
        source: ccProvider,
        canRefresh: true,
      });
      endpoints = ctx.endpoints;
      console.log(
        `[oneshot] validation context: ${ctx.endpoints.length} deployed endpoint(s) → ${VALIDATION_CONTEXT_FILE}`,
      );
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      emit({ kind: "result", status: "failure", error: `validation_context: ${msg}` });
      console.error(`[oneshot] validation context unavailable — not starting the agent: ${msg}`);
      return 2;
    }

    // Make those URLs work for curl before the agent can reach for it. Fatal for
    // the same reason the fetch above is: without the override a plain `curl`
    // against a `.localhost` endpoint is refused by loopback (RFC 6761 — see
    // endpoint_access.ts), and the skill no longer carries the ~20 lines that used
    // to explain that away. An agent handed a URL it cannot dial and no
    // explanation is exactly the state that sent the last one hunting.
    try {
      const entries = await curlResolveEntries(endpoints, undefined, (l) => console.log(l));
      const written = await writeCurlResolveConfig(curlConfigHome(), entries);
      // The same override for the exploration browser. Separate file because
      // `.curlrc` is a curl mechanism and reaches no browser, and separate from
      // the project's playwright.config.ts because playwright-cli does not read
      // that either — it was the one client still dialling loopback.
      const browserConfig = await writePlaywrightCliConfig(playwrightCliConfigHome(), entries);
      if (written === undefined) {
        // No `.localhost` endpoints — a cloud plane resolves them normally and
        // there is nothing to pin. Logged so the absence is a decision on the
        // record rather than a step that looks like it did not run.
        console.log("[oneshot] endpoints need no curl resolve override");
      } else {
        console.log(`[oneshot] pinned ${entries.length} endpoint host(s) for curl → ${written}`);
      }
      if (browserConfig !== undefined) {
        console.log(
          `[oneshot] pinned ${entries.length} endpoint host(s) for playwright-cli → ${browserConfig}`,
        );
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      emit({ kind: "result", status: "failure", error: `endpoint_access: ${msg}` });
      console.error(`[oneshot] cannot make the endpoints reachable — not starting the agent: ${msg}`);
      return 2;
    }

    // PREFLIGHT: does the deployed system actually answer? Also a platform fact,
    // and one the agent used to be told to establish itself — which it got wrong
    // in the one way that matters, reading a resolver quirk as a dead deployment.
    // Proving it here means an unreachable endpoint costs no agent tokens and is
    // reported as a platform fault instead of a validation verdict.
    const unreachable = await probeEndpoints(endpoints);
    if (unreachable.length > 0) {
      const detail = unreachable.map((u) => `${u.component} (${u.url}): ${u.reason}`).join("; ");
      emit({ kind: "result", status: "failure", error: `endpoint_unreachable: ${detail}` });
      console.error(`[oneshot] deployed endpoint(s) did not answer — not starting the agent: ${detail}`);
      return 2;
    }
    console.log(`[oneshot] ${endpoints.length} deployed endpoint(s) answered`);

    // Resolved against the mirror rather than passed straight through: the BFF
    // copies a skill only when the org has it enabled, so a name here can be
    // absent, and this defect already survived weeks of green runs on silence.
    // A miss is named now, with the path it looked for, instead of arriving as a
    // rejected Skill call mid-run.
    const { present } = await resolveSkillPresence(
      layout.workspace,
      onDemandSkills(req.taskKind),
      (l) => console.log(l),
    );
    availableSkillNames = present;
    console.log(
      `[oneshot] ${availableSkillNames.length} skill(s) loadable on demand: ${availableSkillNames.join(", ") || "none"}`,
    );
  } else {
    const pinned = await resolveTaskSkills({
      workspace: layout.workspace,
      scope: { kind: "project" },
      log: (l) => console.log(l),
    });
    const { present, dangling } = await resolveSkillPresence(layout.workspace, pinned, (l) => console.log(l));
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
    pinnedBodies = await readSkillBodies(layout.workspace, present);
    console.log(
      `[oneshot] ${availableSkillNames.length} skill(s) available, ${present.length} pinned into context`,
    );
  }

  const log = openTaskLog(layout.workspace);
  const mcpAuth: McpAuthOpts | undefined =
    req.mcpUrl && req.mcpToken
      ? {
          source: ccProvider,
          canRefresh: true,
        }
      : undefined;
  let completion: Promise<{ exitCode: number }>;
  try {
    ({ completion } = await runClaudeQuery(req, layout, log, { availableSkillNames, pinnedBodies }, mcpAuth));
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
