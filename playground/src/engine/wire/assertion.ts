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
 * THE GATEWAY'S IDENTITY, for one project.
 *
 * A generated service verifies the assertion in front of every request against
 * ONE certificate and nothing else — no JWKS, no introspection, no network call
 * (see the `ballerina` skill's gateway_assertion.bal). So standing in for the
 * gateway is exactly: hold the private key whose certificate the service was
 * given. This mints that pair, once per project, and hands the certificate to
 * compose and the key path to the dev server.
 *
 * The key never enters the app tree and never enters a prompt. It lives 0600 in
 * the project's state dir beside the compose file, and a developer running
 * `npm run dev:mock` by hand without `wire` gets plain mock mode because
 * `AEP_WIRED_KEY` is not set.
 *
 * openssl rather than a library: what the service needs is an X.509
 * CERTIFICATE, not a public key, and no JavaScript dependency in this repo
 * issues one. `jose` signs the tokens below, which is the half it is good at.
 */

import { chmodSync, existsSync, mkdirSync, readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { join } from "node:path";
import { SignJWT, importPKCS8 } from "jose";
import type { WireRole } from "./plan.js";

/** The `iss` claim every assertion carries, and the service's `GATEWAY_ASSERTION_ISSUER`. */
export const WIRE_ISSUER = "aep-playground-wire";

/** The header the assertion travels in, and the service's `GATEWAY_ASSERTION_HEADER`. */
export const WIRE_HEADER = "x-jwt-assertion";

/** The OIDC scopes the platform requests beside the project handles; the mock session mints the same. */
const BASE_SCOPES = ["openid", "profile", "email", "group", "ou"];

export interface Keypair {
  keyPath: string;
  certPath: string;
  /** The certificate's PEM text — what the services are given. */
  certificate: string;
}

/**
 * The project's keypair, generated on first use and reused after.
 *
 * Reused rather than regenerated because the volume outlives the session: rows
 * created by a role yesterday are owned by the `username` in the assertion, and
 * a new identity would not change that — but a developer comparing two runs
 * should not have to wonder whether the key did.
 */
export function ensureKeypair(stateDir: string): Keypair {
  mkdirSync(stateDir, { recursive: true });
  const keyPath = join(stateDir, "key.pem");
  const certPath = join(stateDir, "cert.pem");
  if (!existsSync(keyPath) || !existsSync(certPath)) {
    execFileSync(
      "openssl",
      [
        "req",
        "-x509",
        "-newkey",
        "rsa:2048",
        "-nodes",
        "-keyout",
        keyPath,
        "-out",
        certPath,
        "-subj",
        `/CN=${WIRE_ISSUER}`,
        "-days",
        "365",
      ],
      { stdio: "ignore" },
    );
    chmodSync(keyPath, 0o600);
  }
  return { keyPath, certPath, certificate: readFileSync(certPath, "utf8") };
}

/**
 * One mock bearer per role, in the exact shape `mock/authz/session.ts` mints in
 * the browser — `mock:<role>;<grants>` — so the wired gateway decodes a scripted
 * call and a clicked one with the same code.
 *
 * The empty-string key is the `?role=` caller: signed in, holding nothing. It is
 * a case a walk has to cover (NoAccess) and no role name expresses it.
 */
export function roleTokens(roles: WireRole[]): Record<string, string> {
  const tokens: Record<string, string> = {};
  for (const role of roles) {
    tokens[role.name] = `mock:${encodeURIComponent(role.name)};${role.grants.map(encodeURIComponent).join(",")}`;
  }
  tokens[""] = "mock:;";
  return tokens;
}

/**
 * The `sub` a role gets: derived from the name so it is the same on every run,
 * shaped as a UUID because a real one is a directory id. The same derivation
 * `mock/wired.ts` uses, so a curl and a click are the same caller.
 */
export function subjectFor(role: string): string {
  const digest = createHash("sha1").update(`aep-wire:${role}`).digest("hex");
  return `${digest.slice(0, 8)}-${digest.slice(8, 12)}-${digest.slice(12, 16)}-${digest.slice(16, 20)}-${digest.slice(20, 32)}`;
}

export interface AssertionOptions {
  issuer?: string;
  /** Seconds the assertion stays valid. */
  ttlSeconds?: number;
}

/**
 * An assertion for a role, signed with the project's key.
 *
 * Used for the printed `curl` table a project with no web application gets —
 * there the person IS the client, and a token that expires in a minute is a
 * token that has expired by the time it is pasted. In the browser the dev
 * server mints per request instead, with a minute's life.
 */
export async function mintAssertion(role: WireRole, keyPath: string, options: AssertionOptions = {}): Promise<string> {
  const key = await importPKCS8(readFileSync(keyPath, "utf8"), "RS256");
  const issuedAt = Math.floor(Date.now() / 1000);
  return new SignJWT({
    username: role.username,
    scope: [...BASE_SCOPES, ...role.grants].join(" "),
    ouHandle: "local",
  })
    .setProtectedHeader({ alg: "RS256", typ: "JWT" })
    .setIssuer(options.issuer ?? WIRE_ISSUER)
    .setSubject(subjectFor(role.name))
    .setIssuedAt(issuedAt)
    .setExpirationTime(issuedAt + (options.ttlSeconds ?? 7200))
    .sign(key);
}
