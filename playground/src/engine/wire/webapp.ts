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
 * THE APP, on the host.
 *
 * The one component wired mode does NOT put in a container. It runs under Vite
 * because that is where mock mode's two levers live: `?role=` switches who is
 * signed in with no IDP, and the dev server can be the gateway in front of the
 * real service (mock/wired.ts). A production nginx image can do neither.
 */

import { existsSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { classifyDevServer, WireStepFailed } from "./failure.js";
import { findFreePort, isPortBusy, run, startGroup, waitForHttp, type ProcessGroup } from "./runtime.js";

/** Where the dev server is searched for a port, the same window `walk.sh` uses. */
const FIRST_DEV_PORT = 5173;

export interface WiredEnv {
  /** `http://localhost:<the API's mapped port>`. */
  api: string;
  keyPath: string;
  issuer: string;
  header: string;
}

/** The facts a `node_modules` tree's native binaries were resolved for. */
export interface InstallHost {
  platform: string;
  arch: string;
  /** Node's module ABI (`process.versions.modules`): what a compiled addon is built against. */
  abi: string;
}

export function thisHost(): InstallHost {
  return { platform: process.platform, arch: process.arch, abi: process.versions.modules };
}

/**
 * What `installIfNeeded` writes into the tree after its own `npm ci`: the host
 * it installed for, and npm's hidden lockfile as it left it. npm rewrites
 * `node_modules/.package-lock.json` on every install, so a tree some other
 * install has touched since no longer matches, whoever did it.
 */
interface InstallStamp extends InstallHost {
  lockfile: { mtimeMs: number; size: number } | null;
}

const STAMP = ".aep-host-install.json";

function hiddenLockfile(modules: string): InstallStamp["lockfile"] {
  try {
    const stat = statSync(join(modules, ".package-lock.json"));
    return { mtimeMs: stat.mtimeMs, size: stat.size };
  } catch {
    return null;
  }
}

function currentStamp(modules: string, host: InstallHost): InstallStamp {
  return { ...host, lockfile: hiddenLockfile(modules) };
}

/**
 * Whether `npm ci` has to run before the dev server will start.
 *
 * The coding run installs `node_modules` inside the Linux runner image, whose
 * bundler bindings fail here as a missing module. Only a host install writes the
 * stamp; a container install never does, and `npm ci` deletes it (ADR-0002).
 */
export function needsInstall(appPath: string, host: InstallHost = thisHost()): boolean {
  const modules = join(appPath, "node_modules");
  if (!existsSync(modules)) return true;
  let recorded: InstallStamp;
  try {
    recorded = JSON.parse(readFileSync(join(modules, STAMP), "utf8")) as InstallStamp;
  } catch {
    return true; // no stamp: installed somewhere else, or by something else
  }
  const now = currentStamp(modules, host);
  return !(
    recorded.platform === now.platform &&
    recorded.arch === now.arch &&
    recorded.abi === now.abi &&
    recorded.lockfile?.mtimeMs === now.lockfile?.mtimeMs &&
    recorded.lockfile?.size === now.lockfile?.size
  );
}

/** Record that this host installed the tree, as it stands right after the install. */
export function stampHostInstall(appPath: string, host: InstallHost = thisHost()): void {
  const modules = join(appPath, "node_modules");
  writeFileSync(join(modules, STAMP), JSON.stringify(currentStamp(modules, host)), "utf8");
}

/** `npm ci`, quietly, when the tree was installed somewhere else. */
export async function installIfNeeded(appPath: string, onLine?: (line: string) => void): Promise<boolean> {
  if (!needsInstall(appPath)) return false;
  const result = await run("npm", ["ci", "--no-audit", "--no-fund"], {
    cwd: appPath,
    capture: true,
    ...(onLine ? { onLine } : {}),
  });
  if (result.code !== 0) {
    // The environment's: the coding run already installed this exact lockfile
    // inside the runner image, so what failed here is this host's install (its
    // network, its registry, its toolchain), not the app's dependency set.
    throw new WireStepFailed({
      cause: "environment",
      reason: `npm ci failed on this host in ${appPath} (exit ${String(result.code)}) — see .aep-playground/wire/logs/webapp.log`,
    });
  }
  stampHostInstall(appPath);
  return true;
}

export interface DevServer {
  url: string;
  port: number;
  group: ProcessGroup;
}

/**
 * Start `npm run dev:mock` wired to the real API, on a port `take` leased to
 * this session, as its own process group.
 *
 * `--strictPort` on purpose: the port is in the URL that is about to be opened
 * and printed, and a Vite that quietly moves to the next one would leave a
 * session pointing at nothing.
 */
export async function startWiredDevServer(
  appPath: string,
  wired: WiredEnv,
  take: (port: number) => Promise<boolean>,
  onLine?: (line: string) => void,
): Promise<DevServer> {
  let port: number;
  try {
    port = await findFreePort(FIRST_DEV_PORT, take);
  } catch (e) {
    throw new WireStepFailed({ cause: "environment", reason: e instanceof Error ? e.message : String(e) });
  }
  const group = startGroup("npm", ["run", "dev:mock", "--", "--port", String(port), "--strictPort"], {
    cwd: appPath,
    capture: true,
    env: {
      ...process.env,
      AEP_WIRED_API: wired.api,
      AEP_WIRED_KEY: wired.keyPath,
      AEP_WIRED_ISSUER: wired.issuer,
      AEP_WIRED_HEADER: wired.header,
    },
    ...(onLine ? { onLine } : {}),
  });

  const url = `http://localhost:${String(port)}`;
  const up = await waitForHttp(`${url}/`, 90_000, () => group.exitCode() !== null);
  if (!up) {
    const exitCode = group.exitCode();
    await group.stop();
    // Asked only once ours is gone: with --strictPort, Vite exits rather than
    // move when its port is taken, and what is listening there now is not it.
    const portHeldByOther = exitCode !== null && (await isPortBusy(port));
    throw new WireStepFailed(classifyDevServer({ exitCode, portHeldByOther, url }));
  }

  // The dev server itself says whether the wired branch engaged. Asking it is
  // the only check that cannot be satisfied by a file that looks right: if this
  // is false the page is being answered by MSW and the service is running for
  // nobody, which is indistinguishable from working until the data is wrong.
  if (!(await servesWiredEnv(url))) {
    await group.stop();
    throw new WireStepFailed({
      cause: "app",
      reason:
        `${appPath} came up in plain mock mode — /env-config.js does not set window.__AEP_WIRED__. ` +
        `Its mock/ files are older than wired mode; re-copy them from the react-webapp skill's assets.`,
    });
  }
  return { url, port, group };
}

/** Whether `/env-config.js` declares wired mode — the plugin's own answer. */
async function servesWiredEnv(url: string): Promise<boolean> {
  try {
    const response = await fetch(`${url}/env-config.js`);
    return /__AEP_WIRED__\s*=\s*true/.test(await response.text());
  } catch {
    return false;
  }
}
