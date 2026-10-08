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
 * The `agent-browser` a host process drives: this package's pinned
 * devDependency (equal to the image's `AGENT_BROWSER_VERSION`), never the
 * global CLI (ADR-0005).
 */

import { existsSync } from "node:fs";
import { delimiter, join } from "node:path";

/** Where pnpm put `packageRoot`'s `agent-browser` launcher. */
export function agentBrowserBinDir(packageRoot: string): string {
  return join(packageRoot, "node_modules", ".bin");
}

/** Why `binDir` cannot serve a walk, or undefined when it can. */
export function agentBrowserProblem(binDir: string): string | undefined {
  if (existsSync(join(binDir, "agent-browser"))) return undefined;
  return `the pinned agent-browser is not installed at ${binDir} — run \`make install\``;
}

/** `env` with `binDir` FIRST on PATH, so a bare `agent-browser` is the pinned copy. */
export function withAgentBrowserFirst(env: NodeJS.ProcessEnv, binDir: string): NodeJS.ProcessEnv {
  return { ...env, PATH: env.PATH ? `${binDir}${delimiter}${env.PATH}` : binDir };
}
