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
 * Who may join a Room, per listener (07 §11). One Hocuspocus instance serves
 * two listeners, and the listener a socket came in on (`context.listener`,
 * set by the listener, never by the client) decides who it is:
 *
 *   public  a Platform IdP user token of the pod's org (`userRule`). The
 *           participant is the token's user; a `credit` parameter is ignored.
 *   local   the Room socket, mounted only into ae-design-agent: the socket is
 *           the agent's identity and no token is read (Task 4.7a: the
 *           ae-studio client token never leaves ae-studio-tools). The
 *           participant is the user the `credit` parameter names
 *           (`{name, email}`), for whom the in-pod agent runs the turn.
 *
 * Then the room: `spec-<orgHandle>-<project>` with the pod's own handle, and a
 * project the Files socket knows (looked up once per connection). A user
 * token is kept only as its `exp`: the connection closes then (`expiry.ts`)
 * unless `onTokenSync` re-verifies a fresher user token of the org first. The
 * agent's connection has no deadline, and a token synced on it is ignored.
 *
 * A refusal is thrown with an empty message, so Hocuspocus logs nothing of
 * its own, and a `reason` the client reads: `permission-denied` is a verdict,
 * `upstream-unavailable` means retry. The log line is ours, and value-free.
 */

import type { onAuthenticatePayload, onTokenSyncPayload } from "@hocuspocus/server";
import { IdpUnavailableError, UnauthenticatedError, userRule, type TokenKind, type VerifiedToken } from "@aep/platform-idp-auth";
import { FilesDeniedError, type FilesClient } from "../files-client.js";
import { addParticipant, ensureRoomState } from "../rooms.js";
import { isSpecRoom } from "../room.js";
import type { ExpiryGuard } from "./expiry.js";
import type { PodLog, RefusalCause } from "./log.js";
import type { PodConfig } from "./config.js";

export type ListenerKind = "public" | "local";

export interface CollabUser {
  name: string;
  email: string;
  kind: "user" | "agent" | "dev";
}

/** A connection's context: set by `onAuthenticate`, read by the later hooks. */
export interface CollabContext {
  listener: ListenerKind;
  user: CollabUser;
  projectName: string | null;
  /** The verified user token's `exp` (seconds), kept current by `onTokenSync`; infinite for the agent and dev. */
  exp: number;
  /** The user token's `sub` at connect; null for the agent and dev. */
  subject: string | null;
}

/** `verify` from `@aep/platform-idp-auth`'s `createVerifier`. */
export type Verify = (token: string, kinds: TokenKind[]) => Promise<VerifiedToken>;

export type AuthConfig = Pick<PodConfig, "orgId" | "orgHandle" | "userAudiences">;

/** What the client is told, in `permission-denied` frames and close reasons. */
export const PERMISSION_DENIED = "permission-denied";
// Keep in step with useCollabSpec.ts and room-peer.ts, which spell it on their side.
export const UPSTREAM_UNAVAILABLE = "upstream-unavailable";

/** An error Hocuspocus forwards as `reason`; empty message, so nothing else is logged. */
export function refusal(reason: typeof PERMISSION_DENIED | typeof UPSTREAM_UNAVAILABLE): Error {
  return Object.assign(new Error(""), { reason });
}

/**
 * The close a connection gets when a synced token is refused. Hocuspocus
 * carries only the reason to the client; the code is for our side.
 */
export const TOKEN_REFUSED = { code: 4401, reason: PERMISSION_DENIED } as const;

/** The connection parameter that names the credited user on the Room socket. */
const CREDIT_PARAM = "credit";

type AuthPayload = Pick<onAuthenticatePayload<Partial<CollabContext>>, "token" | "documentName" | "context" | "requestParameters">;
type TokenSyncPayload = Pick<onTokenSyncPayload<CollabContext>, "token" | "connection">;

class Refused extends Error {
  constructor(readonly why: RefusalCause) {
    super(why);
  }
}

interface Checked {
  exp: number;
  user: CollabUser;
  subject: string;
}

/**
 * The public listener's token check: a user token of the pod's org, and only
 * that kind. Throws `Refused` for any other token, and `IdpUnavailableError`
 * when the IdP's keys could not be fetched (no verdict was reached).
 */
function userCheckerFor(cfg: AuthConfig, verify: Verify) {
  const pod = { orgId: cfg.orgId, orgHandle: cfg.orgHandle };
  const userKinds: TokenKind[] = [{ name: "user", audiences: cfg.userAudiences }];
  return async (token: string): Promise<Checked> => {
    let verified: VerifiedToken;
    try {
      verified = await verify(token, userKinds);
    } catch (err) {
      // IdpUnavailableError passes up; anything else is a wiring fault.
      if (!(err instanceof UnauthenticatedError)) throw err;
      throw new Refused("token");
    }
    // Only the user kind is offered, so anything else is a wiring fault.
    if (verified.kind !== "user") throw new Error("room auth: unexpected token kind");
    if (!userRule(verified.claims, pod)) throw new Refused("org");
    return { exp: verified.claims.exp, user: userOf(verified.claims), subject: verified.claims.sub };
  };
}

/** The participant a user token names: `name`, else given + family name, else `sub`. */
function userOf(claims: { sub: string; name?: string; given_name?: string; family_name?: string; email?: string }): CollabUser {
  const full = [claims.given_name, claims.family_name].filter((v) => v?.trim()).join(" ");
  const name = claims.name?.trim() || full || claims.sub;
  // An empty email becomes the noreply address in rooms.addParticipant.
  return { name, email: claims.email?.trim() ?? "", kind: "user" };
}

/** The Room socket's `credit` parameter: `{name, email}` JSON with a name. */
function creditOf(params: URLSearchParams): CollabUser {
  const raw = params.get(CREDIT_PARAM);
  if (raw === null) throw new Refused("credit");
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Refused("credit");
  }
  if (typeof parsed !== "object" || parsed === null) throw new Refused("credit");
  const { name, email } = parsed as { name?: unknown; email?: unknown };
  if (typeof name !== "string" || !name.trim() || (email !== undefined && typeof email !== "string")) {
    throw new Refused("credit");
  }
  return { name: name.trim(), email: email?.trim() ?? "", kind: "agent" };
}

/** The project of `spec-<orgHandle>-<project>`, or null for any other room. */
function projectOf(documentName: string, orgHandle: string): string | null {
  const prefix = `spec-${orgHandle}-`;
  if (!isSpecRoom(documentName) || !documentName.startsWith(prefix)) return null;
  return documentName.slice(prefix.length) || null;
}

function listenerOf(context: Partial<CollabContext> | undefined): ListenerKind {
  const listener = context?.listener;
  // Set by the listener on every socket; anything else is a wiring fault.
  if (listener !== "public" && listener !== "local") throw new Error("room auth: connection has no listener");
  return listener;
}

/**
 * `onAuthenticate`: who by listener (a user token on the public listener, the
 * `credit` on the Room socket), then the room, then the project lookup.
 */
export function authenticateFor(
  cfg: AuthConfig,
  verify: Verify,
  files: FilesClient,
  log: PodLog,
): (data: AuthPayload) => Promise<CollabContext> {
  const check = userCheckerFor(cfg, verify);
  return async (data) => {
    const listener = listenerOf(data.context);
    const refuse = (cause: RefusalCause, reason: Parameters<typeof refusal>[0] = PERMISSION_DENIED): Error => {
      log({ msg: "room_auth_refused", source: "ae-collab", listener, cause });
      return refusal(reason);
    };
    let who: Pick<CollabContext, "user" | "exp" | "subject">;
    try {
      // The public listener never reads `credit`: a user is credited as
      // themself. The Room socket never reads a token: the socket is the agent.
      who =
        listener === "public"
          ? await check(data.token)
          : { user: creditOf(data.requestParameters), exp: Number.POSITIVE_INFINITY, subject: null };
    } catch (err) {
      if (err instanceof Refused) throw refuse(err.why);
      if (err instanceof IdpUnavailableError) throw refuse("idp_unavailable", UPSTREAM_UNAVAILABLE);
      throw err;
    }
    const projectName = projectOf(data.documentName, cfg.orgHandle);
    if (!projectName) throw refuse("room");
    try {
      await files.lookup(projectName);
    } catch (err) {
      if (err instanceof FilesDeniedError) throw refuse(err.status === 404 ? "project_unknown" : "files_denied");
      throw refuse("files_unavailable", UPSTREAM_UNAVAILABLE);
    }
    ensureRoomState(data.documentName, projectName);
    addParticipant(data.documentName, who.user);
    return { listener, projectName, ...who };
  };
}

/**
 * `onTokenSync`: the client pushed a token. On the public listener it must
 * pass the same check as at connect (signature, issuer, user kind, the pod's
 * org); then the deadline moves to its `exp`. A refused token closes the
 * connection at once. An IdP that cannot be reached decides nothing: the old
 * deadline stands. A user token for another subject of the org is accepted
 * (it passes the same rule a fresh connection would) and logged without the
 * subjects. On the Room socket no token is read, so a sync changes nothing.
 */
export function onTokenSyncFor(
  cfg: AuthConfig,
  verify: Verify,
  expiry: ExpiryGuard,
  log: PodLog,
): (data: TokenSyncPayload) => Promise<void> {
  const check = userCheckerFor(cfg, verify);
  return async ({ token, connection }) => {
    const context = connection.context;
    const listener = listenerOf(context);
    if (listener === "local") return;
    try {
      const { exp, subject } = await check(token);
      if (subject !== context.subject) log({ msg: "room_token_subject_changed", source: "ae-collab", listener });
      context.exp = exp;
      expiry.arm(connection, exp);
      log({ msg: "room_token_refreshed", source: "ae-collab", listener });
    } catch (err) {
      if (err instanceof IdpUnavailableError) {
        log({ msg: "room_token_unverified", source: "ae-collab", listener, cause: "idp_unavailable" });
        return;
      }
      if (!(err instanceof Refused)) throw err;
      // Closed here, not thrown: Hocuspocus would print the error object.
      connection.close(TOKEN_REFUSED);
      log({ msg: "room_token_refused", source: "ae-collab", listener, cause: err.why });
    }
  };
}

/**
 * Dev mode (`COLLAB_DEV=1`, never in a pod): every connection is the dev user,
 * the project is the room name after `spec-`, and no token is read or expires.
 */
export function devAuthenticate(data: Pick<AuthPayload, "documentName" | "context">): Promise<CollabContext> {
  const listener = listenerOf(data.context);
  if (!isSpecRoom(data.documentName)) return Promise.reject(refusal(PERMISSION_DENIED));
  const projectName = data.documentName.slice("spec-".length);
  const user: CollabUser = { name: "Dev User", email: "dev@localhost", kind: "dev" };
  ensureRoomState(data.documentName, projectName);
  addParticipant(data.documentName, user);
  return Promise.resolve({ listener, user, projectName, exp: Number.POSITIVE_INFINITY, subject: null });
}
