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

// `aep-web`: the platform's web search for a run whose connection's strategy is
// one no runtime runs itself (`ollama-api`). The server is
// `packages/web-search/src/aep-web.ts` — ONE implementation, shared with the
// spec agents' `web_search` tool — and this module is what a run needs to
// offer it: where the server is, and the private file it reads.
//
// Where the server is, in the order a run can meet it:
//   - the image: `AEP_WEB_SEARCH_SERVER`, the bundle the Dockerfile builds from
//     the `web-search` named build context (a pod holds no monorepo);
//   - the monorepo: the package's source, run with this package's own `tsx` —
//     a host playground run, and the tests.
// Neither → no server, and the run says so on its feed rather than searching
// through a runtime tool that would send the query somewhere else.
//
// The server's inputs go in a 0600 file in a fresh 0700 directory, never in env
// or argv: the file holds the connection key and the run's staged-secret
// values, and a runtime's MCP config reaches a command line (Claude Code passes
// its server list to the CLI as an argument).

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { ModelConnection } from "./model_connection.js";
import { connectionKey } from "./model_connection.js";
import type { WebSearchServer } from "../runtime/port.js";

/** The server's key in a runtime's MCP config. */
export const AEP_WEB_SERVER = "aep-web";

/** The one tool it offers (`WEB_SEARCH_TOOL` in `packages/web-search/src/mcp.ts`). */
export const AEP_WEB_TOOL = "web_search";

/** This package's root: `src/lib/` is two levels down, in the image and in the monorepo alike. */
const RUNNER_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");

/** The server's source in the monorepo, beside this package. */
const MONOREPO_SERVER = path.resolve(RUNNER_ROOT, "..", "..", "packages", "web-search", "src", "aep-web.ts");

/** How to start the server, or undefined when this run has none. */
export function aepWebCommand(env: NodeJS.ProcessEnv = process.env): { command: string; args: string[] } | undefined {
  const bundled = (env.AEP_WEB_SEARCH_SERVER ?? "").trim();
  if (bundled !== "") return { command: process.execPath, args: [bundled] };
  if (fs.existsSync(MONOREPO_SERVER)) {
    return { command: path.join(RUNNER_ROOT, "node_modules", ".bin", "tsx"), args: [MONOREPO_SERVER] };
  }
  return undefined;
}

/** A mounted server: the policy clause, and the teardown that removes its private file. */
export interface AepWebMount {
  server: WebSearchServer;
  close(): void;
}

export interface AepWebInputs {
  connection: ModelConnection;
  /** The run's child env: where the connection key is read from. */
  env: Readonly<Record<string, string | undefined>>;
  /** The run's staged-secret values — the WebSearch rule's inputs. */
  deniedValues: readonly string[];
  /** The WebSearch rule's sentence, so every search path refuses in the same words. */
  denialMessage: string;
  /** Where the server is; `aepWebCommand()` by default. */
  command?: { command: string; args: string[] } | undefined;
}

/**
 * The `aep-web` server for this run, or why there is none.
 *
 * `reason` is set when the connection's strategy asks for the server and it
 * cannot be offered, so the caller can say so on the feed; neither is set when
 * the strategy is not `ollama-api` (the runtime searches, or nothing does).
 */
export function mountAepWeb(inputs: AepWebInputs): { mount?: AepWebMount; reason?: string } {
  if (inputs.connection.webSearch !== "ollama-api") return {};
  const key = connectionKey(inputs.env);
  if (!key) return { reason: "the run has no connection key to search with" };
  const command = "command" in inputs ? inputs.command : aepWebCommand();
  if (!command) return { reason: "the aep-web server is not installed in this image" };

  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "aep-web-"));
  const configFile = path.join(dir, "config.json");
  const config = {
    baseURL: inputs.connection.baseURL,
    apiKey: key,
    deniedValues: [...inputs.deniedValues],
    denialMessage: inputs.denialMessage,
  };
  fs.writeFileSync(configFile, JSON.stringify(config), { mode: 0o600 });
  return {
    mount: {
      server: { name: AEP_WEB_SERVER, command: command.command, args: [...command.args, configFile], tool: AEP_WEB_TOOL },
      close: () => fs.rmSync(dir, { recursive: true, force: true }),
    },
  };
}
