/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * The process wrapper: argv in, exit code out. Kept apart from `cli.ts` so the
 * CLI's behaviour is testable without a subprocess and without a module that
 * calls `process.exit` on import.
 */

import { run } from "./cli.js";

const code = await run(process.argv.slice(2), {
  out: (text) => process.stdout.write(text),
  errorOut: (text) => process.stderr.write(text),
}).catch((cause: unknown) => {
  // Nothing in the pipeline throws by design; if something does, it is a defect
  // in this package and the caller still needs a machine-readable line.
  const message = cause instanceof Error ? cause.message : String(cause);
  process.stderr.write(`${JSON.stringify({ kind: "internal", message })}\n`);
  return 1;
});

process.exitCode = code;
