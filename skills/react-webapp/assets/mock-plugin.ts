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

// Mock mode — copied verbatim to <app-path>/mock/plugin.ts, never edited.
//
// The API itself is Mock Service Worker's job (./browser.ts, ./handlers.ts).
// This plugin covers the things a request interceptor structurally cannot, plus
// the one file MSW needs served:
//
//   /env-config.js          window._env_ AND the gateway's operation table:
//                           both must be set BEFORE the bundle runs, and the
//                           worker starts inside it
//   src/authz/session.ts    a module swap, not a request
//   /mockServiceWorker.js   served out of node_modules, so the worker script
//                           never enters public/ and never reaches dist/
//   the operation table     read out of the sibling's openapi.yaml, which is a
//                           file on disk the browser cannot open
//   the role list           read out of security.json, for the in-page role
//                           badge — same reason: a file, not a request
//
// And one mode: with AEP_WIRED_API set (the playground's `wire` verb) the app
// talks to a REAL service instead of MSW, and this plugin becomes the gateway in
// front of it — see ./wired.ts.
//
// It is added under `--mode mock` only and runs in Node at dev-server time, so
// nothing here reaches the production bundle.

import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import type { Plugin } from "vite";
import { mockEnv } from "./env";
import { projectOperations } from "./authz/contract";
import type { MockOperation, MockOperationTable } from "./authz/gateway";
import { securityPath, wiredFromEnv, wiredMiddleware, wiredProxy, type WiredOptions } from "./wired";

export interface MockModeOptions {
  /**
   * The OpenAPI contracts whose `security` blocks the mock gateway enforces.
   * Paths are resolved against the app root.
   *
   * Omitted — the normal case — every contract in the project is read:
   *   ../specs/design/components/{*}/openapi.yaml
   *   ../specs/design/components/{*}/dependencies/{*}.openapi.yaml
   */
  contracts?: string[];
  /** The path prefix the app calls its API through. Default `/api`. */
  apiPrefix?: string;
}

export function mockMode(options: MockModeOptions = {}): Plugin {
  let root = process.cwd();
  let operations: MockOperationTable | null = null;
  let roleNames: string[] | null = null;
  let wired: WiredOptions | null = null;
  return {
    name: "aep-mock-mode",
    enforce: "pre",

    // Read at CONFIG time, not on the first request: a contract this cannot
    // read has to stop `dev:mock` starting, where the message is the first
    // thing on screen, rather than surface later as a screen that mysteriously
    // serves everything.
    //
    // Async because the YAML parser is imported inside readOperationTable and
    // NOWHERE at module scope — a production `vite build` loads this file
    // (vite.config.ts imports it unconditionally) but never calls mockMode(),
    // so that dependency is never resolved outside mock mode.
    async config(userConfig) {
      root = path.resolve(userConfig.root ?? process.cwd());
      operations = await readOperationTable(root, options);
      roleNames = readRoleNames(root);

      // WIRED MODE. The dev server stops being the app's API and becomes the
      // gateway in front of the real one: this returns the proxy, and
      // configureServer below puts the scope check in front of it. Refused
      // rather than degraded when there is no table, because a wired run with
      // no gateway would forward every call unauthenticated and the service
      // would answer 401s that look like application bugs.
      wired = wiredFromEnv(root);
      if (wired && !operations) {
        throw new Error(
          "AEP_WIRED_API is set but no contract declares an `oauth2` scheme — " +
            "there is no operation table to enforce, so this app cannot stand in for the gateway.",
        );
      }
      if (wired && operations) {
        console.info(
          `[wired gateway] ${operations.prefix} -> ${wired.target}; ` +
            `${String(operations.operations.length)} operation(s) enforced, assertion signed with ${wired.keyPath}`,
        );
        return { server: { proxy: wiredProxy(operations, wired) } };
      }
      return undefined;
    },

    configResolved(config) {
      root = config.root;
    },

    // Sign-in without an IDP: every import that resolves to src/authz/session.ts
    // gets mock/authz/session.ts instead, so no module under src/ knows mock
    // mode exists.
    async resolveId(source, importer, options) {
      if (!importer || importer.includes(`${path.sep}mock${path.sep}`)) return null;
      const resolved = await this.resolve(source, importer, { ...options, skipSelf: true });
      if (!resolved) return null;
      const file = resolved.id.split("?")[0];
      return file === path.join(root, "src", "authz", "session.ts")
        ? path.join(root, "mock", "authz", "session.ts")
        : null;
    },

    configureServer(server) {
      // Four globals on one script, because they share one requirement: set
      // before the bundle runs. index.html loads this ahead of the module
      // script exactly as the platform's own /env-config.js is loaded in a pod.
      // `__AEP_WIRED__` is read by mock/browser.ts (do not start the worker —
      // the API is real) and by mock/badge.ts (say so on the badge).
      server.middlewares.use("/env-config.js", (_req, res) => {
        res.setHeader("Content-Type", "application/javascript; charset=utf-8");
        res.end(
          `window._env_ = ${JSON.stringify(mockEnv, null, 2)};\n` +
            `window.__AEP_MOCK_GATEWAY__ = ${JSON.stringify(operations)};\n` +
            `window.__AEP_MOCK_ROLES__ = ${JSON.stringify(roleNames)};\n` +
            `window.__AEP_WIRED__ = ${wired ? "true" : "false"};\n`,
        );
      });

      // Before Vite's own proxy middleware, which is what makes the scope check
      // a gate rather than a suggestion: a forwarded request has passed it.
      if (wired && operations) server.middlewares.use(wiredMiddleware(operations, wired));

      // `msw init` would copy this into public/, where it would be committed and
      // then shipped inside every production image. Resolving it from the
      // installed package instead keeps the repo and dist/ clean, and keeps the
      // script in lockstep with the msw version in package-lock.json.
      server.middlewares.use("/mockServiceWorker.js", (_req, res) => {
        const require = createRequire(path.join(root, "package.json"));
        res.setHeader("Content-Type", "application/javascript; charset=utf-8");
        res.setHeader("Service-Worker-Allowed", "/");
        res.end(fs.readFileSync(require.resolve("msw/mockServiceWorker.js"), "utf-8"));
      });
    },
  };
}

/** Where a project keeps the contracts this app might call. */
const CONTRACT_ROOTS = [
  { dir: ["..", "specs", "design", "components"], depth: 1, suffix: "openapi.yaml" },
  { dir: ["..", "specs", "design", "components"], depth: 2, suffix: ".openapi.yaml" },
];

/** Every file under `dir` at exactly `depth` levels down whose name ends in `suffix`. */
function findContracts(dir: string, depth: number, suffix: string): string[] {
  let entries: fs.Dirent[];
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true });
  } catch {
    return [];
  }
  const found: string[] = [];
  for (const entry of entries) {
    const full = path.join(dir, entry.name);
    if (depth > 1) {
      if (entry.isDirectory()) found.push(...findContracts(full, depth - 1, suffix));
    } else if (entry.isFile() && entry.name.endsWith(suffix)) {
      found.push(full);
    }
  }
  return found.sort();
}

/**
 * The operation table mock/authz/gateway.ts enforces, read off the project's own
 * contracts.
 *
 * Returns null when nothing declares an `oauth2` scheme — an app with no
 * sign-in — and SAYS so, because "no gateway" and "a gateway that lets
 * everything through" look identical from a screen and only one of them is
 * correct.
 */
async function readOperationTable(
  root: string,
  options: MockModeOptions,
): Promise<MockOperationTable | null> {
  const files = options.contracts
    ? options.contracts.map((file) => path.resolve(root, file))
    : CONTRACT_ROOTS.flatMap(({ dir, depth, suffix }) =>
        findContracts(path.resolve(root, ...dir), depth + 1, suffix),
      );
  if (files.length === 0) {
    console.warn(
      "[mock gateway] no OpenAPI contract found under ../specs/design/components — " +
        "NO operation's scope is enforced in mock mode. Pass mockMode({ contracts: [...] }) " +
        "if they live elsewhere.",
    );
    return null;
  }

  // Imported here, never at module scope: vite.config.ts loads this file on a
  // production build too, and that build must not need a mock-only dependency.
  const { parse } = await import("yaml");
  const operations: MockOperation[] = [];
  const seen = new Map<string, string>();
  const read: string[] = [];

  for (const file of files) {
    const source = path.relative(root, file);
    const projected = projectOperations(parse(fs.readFileSync(file, "utf-8")), source);
    if (projected === null) continue; // no oauth2 scheme: not a gateway-fronted API
    read.push(source);
    for (const operation of projected) {
      const key = `${operation.method} ${operation.path}`;
      const first = seen.get(key);
      if (first !== undefined) {
        // Two contracts claiming one route: the app proxies /api to ONE of
        // them, so the second is noise at best and the wrong scope at worst.
        console.warn(`[mock gateway] ${key} is declared in both ${first} and ${source}; keeping ${first}`);
        continue;
      }
      seen.set(key, source);
      operations.push(operation);
    }
  }

  if (operations.length === 0) {
    console.info(
      "[mock gateway] disabled — no contract declares an `oauth2` scheme, so this app has no " +
        "sign-in to enforce.",
    );
    return null;
  }
  console.info(
    `[mock gateway] enforcing ${operations.length} operation(s) from ${read.join(", ")}`,
  );
  return { prefix: options.apiPrefix ?? "/api", operations };
}

/**
 * The role names the in-page badge offers, straight from the design.
 *
 * `mock/authz/roles.gen.ts` carries the same names with their grants, but only
 * apps with an auth dependency have that file, and the badge ships with
 * `mock/browser.ts` to every app. Reading `security.json` here is the same move
 * the operation table makes: the file is on disk, the browser cannot open it,
 * and the plugin already serves the one script that runs before the bundle.
 *
 * Null when there is no security.json or it names no roles — then there is no
 * role to switch to and no badge is mounted.
 */
function readRoleNames(root: string): string[] | null {
  try {
    const security = JSON.parse(fs.readFileSync(securityPath(root), "utf-8")) as { roles?: { name?: unknown }[] };
    const names = (security.roles ?? [])
      .map((role) => role.name)
      .filter((name): name is string => typeof name === "string" && name !== "");
    return names.length > 0 ? names : null;
  } catch {
    return null;
  }
}
