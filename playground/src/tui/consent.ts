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
 * First-run consent for the two verbs that reach outside the spec files: the
 * coding agent (§12) runs bypassPermissions ON THE HOST and writes the project,
 * and `wire` builds the project's container images and runs them. Both are
 * confirmed interactively once per project (a restorable undo snapshot is taken
 * for a coding run regardless). Shared by the CLI driver and the chat loop so
 * the prompt wording can't drift; both refuse in headless mode, where `--yes`
 * is the only consent channel.
 */

import * as clack from "@clack/prompts";

/** A `confirmDir` callback for `codeCommand`. */
export function confirmCodingDir(projectDir: string): () => Promise<boolean> {
  return async () => {
    if (!process.stdin.isTTY) return false;
    const ok = await clack.confirm({
      message: `The coding agent runs with permissions BYPASSED and will write inside ${projectDir}. A restorable undo snapshot is taken first. Continue?`,
    });
    return !clack.isCancel(ok) && ok;
  };
}

/** A `confirmDir` callback for `wireCommand`. */
export function confirmWireDir(projectDir: string): () => Promise<boolean> {
  return async () => {
    if (!process.stdin.isTTY) return false;
    const ok = await clack.confirm({
      message: `wire builds this project's images with Docker and runs them on your machine, with a database container beside them. Nothing is deployed and nothing outside ${projectDir} is written. Continue?`,
    });
    return !clack.isCancel(ok) && ok;
  };
}
