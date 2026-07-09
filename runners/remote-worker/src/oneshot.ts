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

// One-shot entrypoint for the per-task coding-agent pod.
//
// The Argo Workflow renders a pod from aep-coding-agent ClusterWorkflow,
// passing the dispatch payload via AEP_* env vars (no HTTP, no token in body).
// We reuse the same provisionWorkspace + runClaudeQuery code that the legacy
// HTTP server used; only the wrapper changes shape.
//
// Exit codes:
//   0 — agent reported success
//   1 — agent reported failure (commit/push/PR-ready issue, etc.)
//   2 — provisioning or unexpected error before the agent ran
//
// Argo treats any non-zero exit as a Failed step, which the BFF coding-agent
// watcher picks up via the WorkflowRun terminal status.

import { randomUUID } from "node:crypto";
import os from "node:os";
import path from "node:path";
import { provisionWorkspace, refreshGitToken } from "./lib/workspace.js";
import { runClaudeQuery } from "./lib/runner.js";
import { openTaskLog } from "./lib/logger.js";
import { isUUID, isSlug } from "./lib/uuid.js";
import type { DispatchRequest } from "./lib/types.js";
import { emit, primeScrubber } from "./lib/progress/emitter.js";
import { resolveTaskSkills } from "./lib/skills_resolver.js";
import { materializeSkills } from "./lib/skills_materializer.js";
import { ClientCredentialsTokenProvider } from "./lib/oauth.js";

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
  };
}

async function main(): Promise<number> {
  let req: DispatchRequest;
  try {
    req = readDispatchFromEnv();
  } catch (err) {
    // Pre-scrubber stderr is the only safe channel here — the bearer
    // hasn't been read yet so we can't enroll it as a redaction literal.
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

  primeScrubber([process.env.ANTHROPIC_API_KEY, req.bearer, publisherClientSecret]);

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

  // Per-task skills — clone the org's org-skills repo (URL stamped by the BFF
  // as AEP_SKILLS_REPO_URL), resolve the design's applied skills locally, and
  // materialise them into the AgentSkills plugin tree under .aep/skills-plugin/.
  // On any failure we log LOUDLY and continue without the per-task plugin
  // (runner falls back to the base aep plugin only).
  // See docs/design/coding-runner-skills-clone.md.
  let preloadSkillNames: string[] = [];
  let skillsPluginDir: string | undefined;
  const skillsRepoURL = process.env.AEP_SKILLS_REPO_URL ?? "";
  if (skillsRepoURL) {
    try {
      const skillsBearer = ccProvider ? await ccProvider.getToken() : req.bearer;
      const pat = await refreshGitToken(req, skillsBearer);
      const resolutions = await resolveTaskSkills({
        workspace: layout.workspace,
        skillsRepoURL,
        pat,
        // Clone OUTSIDE the work tree so its nested .git never enters the
        // agent's git status / commits.
        scratchDir: path.join(os.tmpdir(), "aep-skills", req.taskId),
        log: (l) => console.log(l),
      });
      const result = await materializeSkills(layout.workspace, resolutions);
      if (result) {
        skillsPluginDir = result.pluginDir;
        preloadSkillNames = result.preloadNames;
        console.log(
          `[oneshot] materialised ${resolutions.length} skill(s); preload=${preloadSkillNames.length} org skill(s)`,
        );
      } else {
        console.log("[oneshot] no per-task skills to materialise");
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.warn(
        `[oneshot] ⚠️  SKILLS UNAVAILABLE — coding agent proceeding WITHOUT its per-task skill plugin (guidance degraded; if this project has skills the output may be wrong): ${msg}`,
      );
    }
  } else {
    console.log("[oneshot] AEP_SKILLS_REPO_URL not set — skipping per-task skills");
  }

  const log = openTaskLog(layout.workspace);
  const { completion } = runClaudeQuery(req, layout, log, {
    skillsPluginDir,
    preloadSkillNames,
  });
  const result = await completion;
  return result.exitCode;
}

main()
  .then((code) => process.exit(code))
  .catch((err) => {
    console.error("[oneshot] unhandled error:", err);
    process.exit(2);
  });
