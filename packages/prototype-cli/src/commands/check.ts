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

/** `prototype check [dir] [--json] [--theme <package>]`. */

import { resolve } from "node:path";
import { checkPrototype } from "@wso2/prototype-kit/check";
import { parseCommandArgs } from "../args.js";
import { EXIT, type CliIO } from "../io.js";
import { formatHuman, formatJson } from "../report.js";
import { resolveCliTheme } from "../theme.js";

export async function runCheck(args: readonly string[], io: CliIO): Promise<number> {
  const { values, positionals } = parseCommandArgs(args, { json: { type: "boolean", default: false }, theme: { type: "string" } }, 1);
  const dir = resolve(io.cwd, positionals[0] ?? ".");
  const theme = resolveCliTheme(values.theme, dir, io.cwd);
  const findings = await checkPrototype(dir, { theme });
  io.stdout(values.json ? formatJson(findings) : formatHuman(findings));
  return findings.length === 0 ? EXIT.ok : EXIT.failed;
}
