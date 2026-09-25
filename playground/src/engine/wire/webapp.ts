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

import { existsSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { findFreePort, run, startGroup, waitForHttp, type ProcessGroup } from "./runtime.js";

/** Where the dev server is searched for a port, the same window `walk.sh` uses. */
const FIRST_DEV_PORT = 5173;

export interface WiredEnv {
  /** `http://localhost:<the API's mapped port>`. */
  api: string;
  keyPath: string;
  issuer: string;
  header: string;
}

/**
 * Whether `npm ci` has to run before the dev server will start.
 *
 * The coding run installs `node_modules` INSIDE the Linux runner image, so a
 * project that has never been touched on the host carries Linux binaries: Vite's
 * rollup resolves a platform-specific optional dependency, finds none for this
 * machine, and dies with an error about a missing module that reads as a broken
 * app rather than a foreign install. Checking `@rollup` for this platform's
 * package is the cheapest true test of "were these deps installed here".
 */
export function needsInstall(
  appPath: string,
  hostPlatform: string = process.platform,
  hostArch: string = process.arch,
): boolean {
  if (!existsSync(join(appPath, "node_modules"))) return true;
  const rollup = join(appPath, "node_modules", "@rollup");
  if (!existsSync(rollup)) return false; // not a rollup app: nothing platform-specific to get wrong
  try {
    // PLATFORM AND ARCHITECTURE, because rollup's packages are named for both
    // (`rollup-darwin-arm64`, `rollup-linux-x64-gnu`) and the platform alone
    // matches across the pair: an x64 install on an arm64 Mac reads as this
    // machine's, and Vite then dies on exactly the missing module this check is
    // here to pre-empt.
    return !readdirSync(rollup).some((entry) => entry.includes(`${hostPlatform}-${hostArch}`));
  } catch {
    return true;
  }
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
    throw new Error(`npm ci failed in ${appPath} (exit ${String(result.code)}):\n${result.output.slice(-2000)}`);
  }
  return true;
}

export interface DevServer {
  url: string;
  port: number;
  group: ProcessGroup;
}

/**
 * Start `npm run dev:mock` wired to the real API, on a free port, as its own
 * process group.
 *
 * `--strictPort` on purpose: the port is in the URL that is about to be opened
 * and printed, and a Vite that quietly moves to the next one would leave a
 * session pointing at nothing.
 */
export async function startWiredDevServer(
  appPath: string,
  wired: WiredEnv,
  onLine?: (line: string) => void,
): Promise<DevServer> {
  const port = await findFreePort(FIRST_DEV_PORT);
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
  const up = await waitForHttp(`${url}/`, 90_000);
  if (!up) {
    await group.stop();
    throw new Error(`the dev server did not answer on ${url} — see the log in .aep-playground/wire/logs/`);
  }

  // The dev server itself says whether the wired branch engaged. Asking it is
  // the only check that cannot be satisfied by a file that looks right: if this
  // is false the page is being answered by MSW and the service is running for
  // nobody, which is indistinguishable from working until the data is wrong.
  if (!(await servesWiredEnv(url))) {
    await group.stop();
    throw new Error(
      `${appPath} came up in plain mock mode — /env-config.js does not set window.__AEP_WIRED__. ` +
        `Its mock/ files are older than wired mode; re-copy them from the react-webapp skill's assets.`,
    );
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
