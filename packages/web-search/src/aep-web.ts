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
 * `aep-web`: the `ollama-api` search strategy as a coding run's MCP server.
 *
 *   aep-web <config.json>
 *
 * The runner writes the config, 0600, in a private directory, and names it as
 * the one argument (`runners/remote-worker/src/lib/aep_web.ts`). A file rather
 * than env or argv, because a runtime's MCP config is not a secret store: Claude
 * Code hands its server list to the CLI on the command line, and an MCP child
 * does not reliably inherit its parent's environment.
 *
 * The key is sent only to the connection's own host (`searchOllama`), and a
 * query holding any of the run's staged-secret values is refused before it is
 * sent: the runner's WebSearch rule, whose values and wording the runner hands
 * over, so the sentence the model reads is the same on every search path.
 */

import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { createWebSearchServer, serveStdio } from "./mcp.js";
import { searchOllama } from "./ollama.js";

/** What the runner hands the server. */
export interface AepWebConfig {
  /** The connection's base URL (`https://ollama.com/v1`); only its origin is used. */
  baseURL: string;
  /** The connection key, sent as `Authorization: Bearer` to that origin only. */
  apiKey: string;
  /** Values no query may contain: the run's staged secrets. */
  deniedValues: string[];
  /** The sentence a refused query is answered with. */
  denialMessage: string;
}

/** Read and check the config; a malformed one is an error, never a server with no guard. */
export function readAepWebConfig(path: string): AepWebConfig {
  let raw: Partial<AepWebConfig> | null;
  try {
    raw = JSON.parse(readFileSync(path, "utf8")) as Partial<AepWebConfig> | null;
  } catch {
    // Never the parser's own message: it quotes the file, which holds the key.
    throw new Error(`aep-web: ${path} is not readable JSON`);
  }
  const strings = (v: unknown): v is string[] => Array.isArray(v) && v.every((s) => typeof s === "string");
  if (
    !raw ||
    typeof raw.baseURL !== "string" ||
    typeof raw.apiKey !== "string" ||
    raw.apiKey === "" ||
    !strings(raw.deniedValues) ||
    typeof raw.denialMessage !== "string"
  ) {
    throw new Error(`aep-web: ${path} is not an aep-web config`);
  }
  return { baseURL: raw.baseURL, apiKey: raw.apiKey, deniedValues: raw.deniedValues, denialMessage: raw.denialMessage };
}

/** The staged-secret rule: refuse a query that contains any denied value. */
export function deniedValuesRule(values: readonly string[], message: string): (query: string) => string | null {
  return (query) => (values.some((v) => v !== "" && query.includes(v)) ? message : null);
}

async function main(configPath: string | undefined): Promise<void> {
  if (!configPath) throw new Error("usage: aep-web <config.json>");
  const config = readAepWebConfig(configPath);
  const handle = createWebSearchServer({
    name: "aep-web",
    version: "1.0.0",
    search: (query) => searchOllama(query, { baseURL: config.baseURL, apiKey: config.apiKey }),
    deny: deniedValuesRule(config.deniedValues, config.denialMessage),
  });
  await serveStdio(handle, process.stdin, process.stdout);
}

// Run as a program (the image's bundle, or `tsx src/aep-web.ts` from source);
// imported by a test, it only exports.
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main(process.argv[2]).catch((err: unknown) => {
    // stderr, never stdout: stdout is the protocol channel.
    process.stderr.write(`${err instanceof Error ? err.message : String(err)}\n`);
    process.exit(1);
  });
}
