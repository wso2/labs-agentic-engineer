#!/usr/bin/env node
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

// Wiring only: every decision lives in src/cli.ts, where it can be driven by
// a fake spawn in tests. This file supplies the one piece that can't be
// faked in a unit test — the real, locally installed promptfoo binary
// (never `npx promptfoo@latest`) — and calls it.

import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { runCli, type SpawnResult } from "../src/cli.js";

/**
 * The locally installed promptfoo binary, resolved through this package's
 * OWN pinned dependency rather than a hard-coded node_modules/.bin path —
 * so it keeps working whether this file runs from source or from a built
 * package, and it is never whatever `npx` happens to fetch on the day a
 * build runs.
 */
function resolvePromptfooBin(): string {
  const req = createRequire(import.meta.url);
  const entry = req.resolve("promptfoo");
  return join(dirname(entry), "entrypoint.js");
}

function spawnPromptfoo(args: string[], opts: { cwd: string; env: NodeJS.ProcessEnv }): SpawnResult {
  const result = spawnSync(process.execPath, [resolvePromptfooBin(), ...args], {
    cwd: opts.cwd,
    encoding: "utf8",
    env: opts.env,
  });
  return {
    status: result.status,
    stderr: result.stderr ?? "",
    ...(result.error === undefined ? {} : { error: result.error }),
  };
}

runCli({ argv: process.argv.slice(2), env: process.env, cwd: process.cwd(), spawnPromptfoo });

// Exit 0 regardless: a failing scenario — or a failed run — is report
// content, never a build failure. `runCli` never throws and never returns a
// pass/fail decision, so there is nothing here to branch on.
process.exit(0);
