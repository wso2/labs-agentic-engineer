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

// Thin client for the BFF calls this service makes. The BFF is the only
// authority: room access (validate-collab-access) and spec content (the
// Files API, #114). Git, credentials, and tenancy all stay its monopoly.

export interface CollabIdentity {
  name: string;
  email: string;
  /**
   * Project resolved from the room ID by the oracle — only the BFF can split
   * `spec-<org>-<project>` (it knows the caller's org).
   */
  projectName: string;
}

/**
 * One spec file ready for seeding. `path` is the ROOM KEY — the repo path
 * with the `specs/` prefix stripped (e.g. requirements/prd.md), matching what
 * the console looks up (#113 decision 2). The strip happens here, at the
 * Files API boundary, and nowhere else.
 */
export interface SpecFile {
  path: string;
  content: string;
}

/** The Files API serves repo-relative paths; rooms key by the remainder. */
export const SPECS_PREFIX = "specs/";

export function toRoomPath(repoPath: string): string | null {
  return repoPath.startsWith(SPECS_PREFIX)
    ? repoPath.slice(SPECS_PREFIX.length)
    : null;
}

export interface BffClient {
  /** Resolves to the caller's display identity, or throws on deny. */
  validateAccess(token: string, roomId: string): Promise<CollabIdentity>;
  /** Spec files for seeding, read via the Files API as the connecting user. */
  fetchSpecFiles(token: string, projectName: string): Promise<SpecFile[]>;
}

export class BffAccessDeniedError extends Error {
  constructor(status: number) {
    super(`BFF denied collab access (${status})`);
    this.name = "BffAccessDeniedError";
  }
}

export function createBffClient(
  base: string,
  fetchImpl: typeof fetch = fetch,
): BffClient {
  return {
    async validateAccess(token, roomId) {
      const res = await fetchImpl(`${base}/collab/validate`, {
        headers: {
          Authorization: `Bearer ${token}`,
          "X-Room-Id": roomId,
        },
      });
      if (!res.ok) throw new BffAccessDeniedError(res.status);
      const body = (await res.json()) as CollabIdentity;
      return {
        name: body.name,
        email: body.email,
        projectName: body.projectName,
      };
    },

    async fetchSpecFiles(token, projectName) {
      const project = encodeURIComponent(projectName);
      const headers = { Authorization: `Bearer ${token}` };

      const listRes = await fetchImpl(`${base}/projects/${project}/files`, {
        headers,
      });
      if (!listRes.ok) {
        throw new Error(
          `Failed to list spec files for ${projectName} (${listRes.status})`,
        );
      }
      const metas = ((await listRes.json()) ?? []) as { path: string }[];

      return Promise.all(
        metas.flatMap((meta) => {
          const roomPath = toRoomPath(meta.path);
          if (roomPath === null) return [];
          const encoded = meta.path
            .split("/")
            .map(encodeURIComponent)
            .join("/");
          return [
            (async (): Promise<SpecFile> => {
              const res = await fetchImpl(
                `${base}/projects/${project}/files/${encoded}`,
                { headers },
              );
              if (!res.ok) {
                throw new Error(
                  `Failed to read ${meta.path} for ${projectName} (${res.status})`,
                );
              }
              const body = (await res.json()) as { content: string };
              return { path: roomPath, content: body.content };
            })(),
          ];
        }),
      );
    },
  };
}
