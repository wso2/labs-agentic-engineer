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

/** The `prototype` CLI: dispatch to a command; 0 ok, 1 findings or a failed operation, 2 a usage error. */

import { packageVersion } from "./assets.js";
import { runCheck } from "./commands/check.js";
import { runExport } from "./commands/export.js";
import { runInit } from "./commands/init.js";
import { runPreview } from "./commands/preview.js";
import { EXIT, type CliIO } from "./io.js";
import { USAGE, UsageError } from "./usage.js";

export async function main(argv: readonly string[], io: CliIO): Promise<number> {
  const [command, ...rest] = argv;
  try {
    switch (command) {
      case undefined:
      case "help":
      case "-h":
      case "--help":
        io.stdout(USAGE);
        return EXIT.ok;
      case "--version":
        io.stdout(`${packageVersion()}\n`);
        return EXIT.ok;
      case "init":
        return runInit(rest, io);
      case "check":
        return await runCheck(rest, io);
      case "preview":
        return await runPreview(rest, io);
      case "export":
        return await runExport(rest, io);
      default:
        throw new UsageError(`unknown command ${JSON.stringify(command)}`);
    }
  } catch (e) {
    if (e instanceof UsageError) {
      io.stderr(`prototype: ${e.message}\n\n${USAGE}`);
      return EXIT.usage;
    }
    io.stderr(`prototype: ${e instanceof Error ? e.message : String(e)}\n`);
    return EXIT.failed;
  }
}
