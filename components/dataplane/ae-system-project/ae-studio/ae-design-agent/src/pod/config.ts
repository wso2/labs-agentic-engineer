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
 * The AE Studio pod's env for this container (08 §2/§3, 07 §9): the org the
 * pod serves, the Platform IdP that signs its users' tokens, the user
 * audiences, the public and health ports, the tools socket, the Turn socket,
 * ae-collab's Room socket, the snapshot mount, and the secret revisions of
 * 08 §7. The model connection is read
 * apart (`shared/connection-env.ts`): the pod boots without a key.
 */

export interface PodConfig {
  orgId: string;
  orgHandle: string;
  issuer: string;
  jwksUrl: string;
  userAudiences: string[];
  listenPort: number;
  healthPort: number;
  /** `AE_MCP_SOCKET`: ae-studio-tools' MCP socket (`tools-socket/client.ts`). */
  mcpSocket: string;
  /** `AE_TURN_SOCKET`: where this container serves the Turn socket (`edge/turn-socket.ts`). */
  turnSocket: string;
  /** `AE_ROOM_SOCKET`: ae-collab's Room socket, where the agent joins Rooms (no token: the mount is the gate). */
  roomSocket: string;
  /** `AE_SNAPSHOTS_DIR`: the read-only snapshot mount. */
  snapshotsDir: string;
  /** The revision the pod spec was rendered for; `""` when the org has no key. */
  expectedSecretRev: string;
  /** The revision of the mounted Secret; `""` when there is none. */
  secretRev: string;
}

type Env = Readonly<Record<string, string | undefined>>;

const DEFAULT_LISTEN_PORT = 8080;
const DEFAULT_HEALTH_PORT = 9080;

/**
 * Reads the pod env. `null` when `AE_ORG_ID` is unset: not a pod. Once it is
 * set every other key is required, and one error names each missing or
 * invalid key (never a value), so a misconfigured pod says everything wrong
 * with it in one restart. A port of 0 binds a free port (tests).
 */
export function loadPodConfig(env: Env): PodConfig | null {
  const orgId = env.AE_ORG_ID?.trim();
  if (!orgId) return null;
  const problems: string[] = [];
  const required = (key: string): string => {
    const value = env[key]?.trim() ?? "";
    if (!value) problems.push(`missing ${key}`);
    return value;
  };
  const port = (key: string, fallback: number): number => {
    const raw = env[key]?.trim();
    if (!raw) return fallback;
    const n = Number(raw);
    if (!/^\d+$/.test(raw) || n > 65535) problems.push(`invalid ${key}`);
    return n;
  };
  const orgHandle = required("AE_ORG_HANDLE");
  const issuer = required("AE_IDP_ISSUER");
  const jwksUrl = required("AE_IDP_JWKS_URL");
  const userAudiences = (env.AE_USER_AUDIENCES ?? "")
    .split(",")
    .map((a) => a.trim())
    .filter((a) => a !== "");
  if (userAudiences.length === 0) problems.push("missing AE_USER_AUDIENCES");
  const mcpSocket = required("AE_MCP_SOCKET");
  const turnSocket = required("AE_TURN_SOCKET");
  const roomSocket = required("AE_ROOM_SOCKET");
  const snapshotsDir = required("AE_SNAPSHOTS_DIR");
  const listenPort = port("AE_LISTEN_PORT", DEFAULT_LISTEN_PORT);
  const healthPort = port("AE_HEALTH_PORT", DEFAULT_HEALTH_PORT);
  if (problems.length > 0) throw new Error(`ae-design-agent pod env: ${problems.join(", ")}`);
  return {
    orgId,
    orgHandle,
    issuer,
    jwksUrl,
    userAudiences,
    listenPort,
    healthPort,
    mcpSocket,
    turnSocket,
    roomSocket,
    snapshotsDir,
    expectedSecretRev: env.AE_EXPECTED_SECRET_REV ?? "",
    secretRev: env.AE_SECRET_REV ?? "",
  };
}
