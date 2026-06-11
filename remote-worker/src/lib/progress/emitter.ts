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

// Single-owner stdout writer for runner progress NDJSON.
// All progress events flow through emit(); nothing else writes to stdout
// in the runner code path. Stamps ts + seq so callers can't forget,
// and routes every line through the scrubber.

import { scrubber } from "./scrubber.js";
import { PROGRESS_SCHEMA_VERSION, type ProgressEvent, type ProgressEventInput } from "./schema.js";

let seqCounter = 0;

export function emit(event: ProgressEventInput): void {
  seqCounter += 1;
  const enriched = {
    schemaVersion: PROGRESS_SCHEMA_VERSION,
    ts: new Date().toISOString(),
    seq: seqCounter,
    ...event,
  } as ProgressEvent;
  const line = scrubber.scrub(JSON.stringify(enriched));
  process.stdout.write(line + "\n");
}

export function primeScrubber(secrets: Iterable<string | undefined | null>): void {
  for (const s of secrets) scrubber.addLiteral(s ?? undefined);
}

// Test seam.
export function _resetEmitterForTesting(): void {
  seqCounter = 0;
  scrubber.reset();
}
