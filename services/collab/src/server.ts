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

import { Server } from "@hocuspocus/server";
import type {
  onAuthenticatePayload,
  onLoadDocumentPayload,
} from "@hocuspocus/server";
import type { CollabConfig } from "./env.js";
import type { BffClient } from "./bff.js";
import { isSpecRoom } from "./room.js";
import { seedDocument } from "./seed.js";
import { devSeedFiles } from "./fixtures.js";

// Connection context established by onAuthenticate and consumed by later
// hooks. The token is retained for the seed read (performed as the first
// joiner) — it never leaves this process except toward the BFF.
export interface CollabContext {
  user: { name: string; email: string; kind: "user" | "dev" };
  token: string | null;
  /** Resolved by the oracle from the room ID (only the BFF can split
   *  `spec-<org>-<project>` — it knows the caller's org). Null in dev mode. */
  projectName: string | null;
}

export interface CollabDeps {
  bff: BffClient | null;
  log?: (message: string) => void;
}

export function buildAuthenticateHook(config: CollabConfig, deps: CollabDeps) {
  return async (
    data: Pick<onAuthenticatePayload, "token" | "documentName">,
  ): Promise<CollabContext> => {
    const { token, documentName } = data;
    if (!isSpecRoom(documentName)) {
      throw new Error(`unknown room: ${documentName}`);
    }

    if (config.devMode) {
      return {
        user: { name: "Dev User", email: "dev@localhost", kind: "dev" },
        token: null,
        projectName: null,
      };
    }

    if (!deps.bff) throw new Error("no BFF configured");
    if (!token) throw new Error("missing token");
    // The oracle does both halves: JWT verification (Thunder JWKS) and the
    // room's project-ownership/tenancy check. This service verifies nothing
    // itself (#86: identity stays the BFF's problem). It also resolves the
    // room into a project name for the seed read.
    const identity = await deps.bff.validateAccess(token, documentName);
    return {
      user: { name: identity.name, email: identity.email, kind: "user" },
      token,
      projectName: identity.projectName,
    };
  };
}

export function buildLoadDocumentHook(config: CollabConfig, deps: CollabDeps) {
  return async (
    data: Pick<onLoadDocumentPayload, "document" | "documentName"> & {
      context: CollabContext;
    },
  ) => {
    const { document, documentName, context } = data;

    if (config.devMode) {
      seedDocument(document, devSeedFiles);
      deps.log?.(`seeded ${documentName} from dev fixtures`);
      return document;
    }

    // Real path: read the spec files as the first joiner (the oracle
    // resolved the room into context.projectName).
    if (!deps.bff || !context.token || !context.projectName) {
      deps.log?.(
        `cannot seed ${documentName}: missing bff/token/project — opening empty`,
      );
      return document;
    }
    // A failed seed must not kill the room: access was already authorized
    // by the oracle, and an unseeded-but-live doc beats a dead connection
    // (transient BFF errors would otherwise hard-fail every join).
    try {
      const files = await deps.bff.fetchSpecFiles(
        context.token,
        context.projectName,
      );
      seedDocument(document, files);
      deps.log?.(`seeded ${documentName} (${files.length} files) from BFF`);
    } catch (err) {
      deps.log?.(
        `seed failed for ${documentName} (${String(err)}) — opening empty`,
      );
    }
    return document;
  };
}

export function createCollabServer(
  config: CollabConfig,
  deps: CollabDeps,
): Server<CollabContext> {
  return new Server<CollabContext>({
    name: "aep-collab",
    // No persistence extension yet: the committer worker is #86 phase 3.
    // Until then a doc's life is its room's life; the seed is the recovery
    // story. Keep docs loaded only while connections exist.
    unloadImmediately: true,
    onAuthenticate: buildAuthenticateHook(config, deps),
    onLoadDocument: buildLoadDocumentHook(config, deps) as (
      data: onLoadDocumentPayload<CollabContext>,
    ) => Promise<unknown>,
  });
}
