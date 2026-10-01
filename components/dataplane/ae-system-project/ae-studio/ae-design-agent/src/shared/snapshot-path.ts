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
 * The structural tenancy fence (shared-workspace-volume, D9).
 *
 * A workspace-shape turn carries IDs + shas ONLY — never a filesystem path —
 * and this module is the single place they become paths:
 *
 *   repo:   $WORKSPACE_MOUNT_ROOT/repos/<orgId>/<projectId>/<repoSlug>/snapshots/<ref>/
 *   skills: $WORKSPACE_MOUNT_ROOT/repos/<orgId>/_skills/org-skills/snapshots/<skillsRef>/
 *
 * orgId/projectId come from the namespaced conversation id
 * (`org_<o>--proj_<p>--<useCase>--<uuid>`), and the org segment must equal the
 * caller's `X-Org-Id` claim (403 otherwise — the cross-tenant IDOR fence; the
 * header is load-bearing for this shape). Every field is charset-fenced with NO
 * path separators and NO `..` anywhere, so traversal is impossible by
 * construction; there is no canonicalize-untrusted-path logic because no path
 * is ever accepted. All failures are PRE-STREAM: 400 (shape/format/unknown
 * snapshot) or 403 (org mismatch).
 */

import { statSync } from "node:fs";
import { join } from "node:path";
import type { WorkspaceRef } from "@aep/agent-stream";
import { config } from "./config.js";

/** A pre-stream workspace rejection: `status` is the HTTP code the route returns. */
export class WorkspaceRefError extends Error {
  constructor(
    public readonly status: 400 | 403,
    message: string,
  ) {
    super(message);
    this.name = "WorkspaceRefError";
  }
}

// Segment grammars. Deliberately strict: the Go side composes these values from
// DB rows/uuids, so anything outside the expected alphabet is hostile or a bug —
// reject either way. None admit `/`, `\` or whitespace.
const ORG_SEGMENT_RE = /^org_[A-Za-z0-9_.-]{1,128}$/;
const PROJ_SEGMENT_RE = /^proj_[A-Za-z0-9_.-]{1,128}$/;
const USE_CASE_RE = /^[a-z][a-z-]{0,63}$/;
const UUID_SEGMENT_RE = /^[A-Za-z0-9_.-]{1,128}$/;
const TURN_ID_RE = /^[A-Za-z0-9_.-]{1,128}$/;
const REPO_SLUG_RE = /^[a-z0-9][a-z0-9._-]{0,99}$/;
const SHA_RE = /^[0-9a-f]{40}$/;
const MAX_CONVERSATION_ID_LEN = 300;

/** Belt-and-braces: no separators, no NUL, no `..`, and no dot-only values — anywhere. */
function isHostile(value: string): boolean {
  return (
    value.includes("/") ||
    value.includes("\\") ||
    value.includes("\0") ||
    value.includes("..") ||
    value === "." ||
    value.startsWith(".")
  );
}

function bad(message: string): never {
  throw new WorkspaceRefError(400, message);
}

/** The validated, derived workspace a turn runs against. */
export interface ResolvedWorkspace {
  orgId: string;
  projectId: string;
  useCase: string;
  conversationId: string;
  turnId: string;
  repoSlug: string;
  ref: string;
  skillsRef: string;
  /** Immutable per-SHA repo snapshot dir (stat-checked to exist). */
  snapshotDir: string;
  /** Immutable `_skills` snapshot dir (stat-checked to exist). */
  skillsSnapshotDir: string;
}

/**
 * The org segment of a namespaced conversation id — the get-conversation
 * read's cross-tenant fence (#463): the same parse the turn POST fences with,
 * so the two routes can never disagree on an id's org. Throws
 * `WorkspaceRefError` (400) on a malformed id.
 */
export function conversationOrgId(id: string): string {
  return parseConversationId(id).orgId;
}

/**
 * Parse a namespaced conversation id into its four segments, defensively.
 * Splitting on `--` FIRST guarantees no segment can smuggle a `--` through the
 * (dash-admitting) per-segment charsets.
 */
function parseConversationId(id: string): { orgId: string; projectId: string; useCase: string } {
  if (id.length > MAX_CONVERSATION_ID_LEN) bad("workspace.conversationId is too long");
  if (isHostile(id)) bad("workspace.conversationId contains an illegal sequence");
  const segments = id.split("--");
  if (segments.length !== 4) {
    bad("workspace.conversationId must be org_<org>--proj_<project>--<useCase>--<uuid>");
  }
  const [orgSeg, projSeg, useCase, uuid] = segments as [string, string, string, string];
  if (!ORG_SEGMENT_RE.test(orgSeg)) bad("workspace.conversationId has an invalid org segment");
  if (!PROJ_SEGMENT_RE.test(projSeg)) bad("workspace.conversationId has an invalid project segment");
  if (!USE_CASE_RE.test(useCase)) bad("workspace.conversationId has an invalid use-case segment");
  if (!UUID_SEGMENT_RE.test(uuid)) bad("workspace.conversationId has an invalid uuid segment");
  const orgId = orgSeg.slice("org_".length);
  const projectId = projSeg.slice("proj_".length);
  // The payloads become directory names: re-fence them standalone (dot-leading
  // names are also rejected — org/project ids are uuid-ish, never dotfiles).
  if (isHostile(orgId)) bad("workspace.conversationId has an illegal org id");
  if (isHostile(projectId)) bad("workspace.conversationId has an illegal project id");
  return { orgId, projectId, useCase };
}

function requireString(v: unknown, field: string): string {
  if (typeof v !== "string" || v === "") bad(`workspace.${field} is required`);
  return v;
}

export interface ResolveWorkspaceRequest {
  /** The URL `:id` — must equal the body's `workspace.conversationId`. */
  conversationIdParam: string;
  /** The untrusted `body.workspace` value. */
  workspace: unknown;
  /** The caller's `X-Org-Id` claim (load-bearing on this shape). */
  orgIdClaim: string | undefined;
  /** Override for tests/evals; defaults to `config.workspaceMountRoot`. */
  mountRoot?: string;
}

/**
 * Validate a workspace-shape turn and derive its snapshot dirs. Throws
 * `WorkspaceRefError` (400/403) on any violation — always before the stream
 * starts. This is the ONLY path from a `WorkspaceRef` to the filesystem.
 */
export function resolveWorkspace(req: ResolveWorkspaceRequest): ResolvedWorkspace {
  const w = req.workspace;
  if (w === null || typeof w !== "object" || Array.isArray(w)) {
    bad("workspace must be an object");
  }
  const ref = w as Partial<Record<keyof WorkspaceRef, unknown>>;
  const conversationId = requireString(ref.conversationId, "conversationId");
  const turnId = requireString(ref.turnId, "turnId");
  const repoSlug = requireString(ref.repoSlug, "repoSlug");
  const commitRef = requireString(ref.ref, "ref");
  const skillsRef = requireString(ref.skillsRef, "skillsRef");

  const { orgId, projectId, useCase } = parseConversationId(conversationId);
  if (req.conversationIdParam !== conversationId) {
    bad("workspace.conversationId must match the conversation id in the URL");
  }
  if (!TURN_ID_RE.test(turnId) || isHostile(turnId)) bad("workspace.turnId is invalid");
  if (!REPO_SLUG_RE.test(repoSlug) || isHostile(repoSlug)) bad("workspace.repoSlug is invalid");
  if (!SHA_RE.test(commitRef)) bad("workspace.ref must be a 40-hex commit sha");
  if (!SHA_RE.test(skillsRef)) bad("workspace.skillsRef must be a 40-hex commit sha");

  // The cross-tenant IDOR fence: the org segment must equal the verified claim.
  if (!req.orgIdClaim || req.orgIdClaim !== orgId) {
    throw new WorkspaceRefError(403, "workspace org does not match the caller's organization");
  }

  const root = req.mountRoot ?? config.workspaceMountRoot;
  const snapshotDir = join(root, "repos", orgId, projectId, repoSlug, "snapshots", commitRef);
  const skillsSnapshotDir = join(root, "repos", orgId, "_skills", "org-skills", "snapshots", skillsRef);
  if (!isDirectory(snapshotDir)) bad(`unknown snapshot ref ${commitRef} for ${repoSlug}`);
  if (!isDirectory(skillsSnapshotDir)) bad(`unknown skills snapshot ref ${skillsRef}`);

  return {
    orgId,
    projectId,
    useCase,
    conversationId,
    turnId,
    repoSlug,
    ref: commitRef,
    skillsRef,
    snapshotDir,
    skillsSnapshotDir,
  };
}

function isDirectory(path: string): boolean {
  try {
    return statSync(path).isDirectory();
  } catch {
    return false;
  }
}
