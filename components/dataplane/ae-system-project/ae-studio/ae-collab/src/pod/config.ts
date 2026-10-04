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
 * The AE Studio pod's env for this container (07 §11, 08 §2): the org the pod
 * serves, the Platform IdP that signs its users' tokens, the user audiences,
 * the browser origins allowed to open a room, the Files socket of
 * ae-studio-tools, the Room socket the in-pod agent joins on, and the public
 * and health ports. Only the pod renders `AE_ORG_ID`; the
 * chart Deployment never does. This container mounts no Secret, so there is
 * no secret revision to check. Dev mode (`COLLAB_DEV=1`, no `AE_ORG_ID`) runs
 * the same listeners from `loadDevConfig`.
 */

import { tmpdir } from "node:os";
import { join } from "node:path";

/** What the listeners need, in pod and in dev mode alike. */
export interface ListenerConfig {
  /** Exact browser origins (`scheme://host[:port]`) a public room upgrade may come from. */
  allowedOrigins: string[];
  listenPort: number;
  healthPort: number;
  /**
   * The in-pod agent's listener: a Unix socket (mode 0660) on an emptyDir
   * shared with ae-design-agent only. The mount is the gate: no token.
   */
  roomSocket: string;
}

export interface PodConfig extends ListenerConfig {
  orgId: string;
  orgHandle: string;
  issuer: string;
  jwksUrl: string;
  userAudiences: string[];
  /** ae-studio-tools' Files socket: spec content and commits, no token. */
  filesSocket: string;
}

/** Dev mode: the pod's listeners with auth bypassed and a fake Files socket. */
export type DevConfig = ListenerConfig;

type Env = Readonly<Record<string, string | undefined>>;

const DEFAULT_LISTEN_PORT = 8081;
const DEFAULT_HEALTH_PORT = 9081;
/** Dev mode's Room socket when `AE_ROOM_SOCKET` is unset (the pod always renders it). */
const DEV_ROOM_SOCKET = join(tmpdir(), "ae-collab-room.sock");

function list(raw: string | undefined): string[] {
  return (raw ?? "")
    .split(",")
    .map((v) => v.trim())
    .filter((v) => v !== "");
}

/** A bare origin, as a browser sends it: no path, no trailing slash. */
function isOrigin(value: string): boolean {
  try {
    return new URL(value).origin === value;
  } catch {
    return false;
  }
}

/**
 * Reads the pod env. `null` when `AE_ORG_ID` is unset: not a pod. Once it is
 * set every other key is required, and one error names each missing or
 * invalid key (never a value), so a misconfigured pod says everything wrong
 * with it in one restart. Ports are 1-65535: 0 (any free port) is never a
 * pod's port, since its probes and Service name fixed ones.
 */
export function loadPodConfig(env: Env): PodConfig | null {
  const orgId = env.AE_ORG_ID?.trim();
  if (!orgId) return null;
  const { problems, required, requiredList, port } = reader(env);
  const orgHandle = required("AE_ORG_HANDLE");
  const issuer = required("AE_IDP_ISSUER");
  const jwksUrl = required("AE_IDP_JWKS_URL");
  const userAudiences = requiredList("AE_USER_AUDIENCES");
  const allowedOrigins = requiredList("AE_ALLOWED_ORIGINS");
  // A listed value that is not a bare origin never equals an Origin header:
  // fail loudly instead of refusing every browser in silence.
  if (!allowedOrigins.every(isOrigin)) problems.push("invalid AE_ALLOWED_ORIGINS");
  const filesSocket = required("AE_FILES_SOCKET");
  const roomSocket = required("AE_ROOM_SOCKET");
  const listenPort = port("AE_LISTEN_PORT", DEFAULT_LISTEN_PORT);
  const healthPort = port("AE_HEALTH_PORT", DEFAULT_HEALTH_PORT);
  if (problems.length > 0) throw new Error(`ae-collab pod env: ${problems.join(", ")}`);
  return {
    orgId,
    orgHandle,
    issuer,
    jwksUrl,
    userAudiences,
    allowedOrigins,
    filesSocket,
    roomSocket,
    listenPort,
    healthPort,
  };
}

/**
 * Dev mode's listeners (`pnpm dev`): the pod's ports, `AE_ALLOWED_ORIGINS`
 * optional (empty: a browser Origin on the public port is refused), and the
 * Room socket at `AE_ROOM_SOCKET`, else a fixed path in the temp dir. One
 * error names every invalid key, as in the pod.
 */
export function loadDevConfig(env: Env): DevConfig {
  const { problems, port } = reader(env);
  const allowedOrigins = list(env.AE_ALLOWED_ORIGINS);
  if (!allowedOrigins.every(isOrigin)) problems.push("invalid AE_ALLOWED_ORIGINS");
  const listenPort = port("AE_LISTEN_PORT", DEFAULT_LISTEN_PORT);
  const healthPort = port("AE_HEALTH_PORT", DEFAULT_HEALTH_PORT);
  if (problems.length > 0) throw new Error(`ae-collab dev env: ${problems.join(", ")}`);
  const roomSocket = env.AE_ROOM_SOCKET?.trim() || DEV_ROOM_SOCKET;
  return { allowedOrigins, listenPort, healthPort, roomSocket };
}

/** Env readers that collect every problem (key names only) instead of throwing at the first. */
function reader(env: Env) {
  const problems: string[] = [];
  const required = (key: string): string => {
    const value = env[key]?.trim() ?? "";
    if (!value) problems.push(`missing ${key}`);
    return value;
  };
  const requiredList = (key: string): string[] => {
    const values = list(env[key]);
    if (values.length === 0) problems.push(`missing ${key}`);
    return values;
  };
  const port = (key: string, fallback: number): number => {
    const raw = env[key]?.trim();
    if (!raw) return fallback;
    const n = Number(raw);
    if (!/^\d+$/.test(raw) || n < 1 || n > 65535) problems.push(`invalid ${key}`);
    return n;
  };
  return { problems, required, requiredList, port };
}
