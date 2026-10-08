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

/** Parsing one command's arguments; any problem is a UsageError. */

import { parseArgs, type ParseArgsConfig } from "node:util";
import { UsageError } from "./usage.js";

type Options = NonNullable<ParseArgsConfig["options"]>;

export function parseCommandArgs<const O extends Options>(args: readonly string[], options: O, maxPositionals: number) {
  let parsed;
  try {
    parsed = parseArgs({ args: [...args], options, allowPositionals: true, strict: true });
  } catch (e) {
    throw new UsageError(e instanceof Error ? e.message : String(e));
  }
  if (parsed.positionals.length > maxPositionals) {
    throw new UsageError(`unexpected argument ${JSON.stringify(parsed.positionals[maxPositionals])}`);
  }
  return parsed;
}
