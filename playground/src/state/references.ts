/*
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
 * Reference documents — the files a user attaches to a project, which the
 * `/start` kickoff names so the start skill reads them as the primary brief.
 *
 * This is the playground's half of aep-api's `listReferenceDocs`
 * (internal/spec/start_command.go): the platform lists the folder out of git
 * at the turn's base commit, the playground lists it off disk. Both hand the
 * turn PATHS only — the documents are ordinary spec content the agent reads
 * from the project itself.
 */

import { existsSync, readdirSync } from "node:fs";
import { join } from "node:path";

/** Where attached documents live, repo-relative. Mirrors aep-api's `ReferencesDir`. */
export const REFERENCES_DIR = "specs/requirements/references";

/**
 * The reference documents in `projectDir`, as repo-relative paths, sorted so a
 * given folder always produces the same turn. No folder (the ordinary case) is
 * not an error: it yields an empty list and the kickoff simply says nothing
 * about documents — same best-effort posture as the captured idea, where
 * losing the steer costs a question, never the run.
 */
export function readReferences(projectDir: string): string[] {
  const dir = join(projectDir, REFERENCES_DIR);
  if (!existsSync(dir)) return [];
  let names: string[];
  try {
    names = readdirSync(dir, { withFileTypes: true })
      .filter((e) => e.isFile())
      .map((e) => e.name);
  } catch {
    return []; // unreadable folder — degrade to "no references", never throw
  }
  return names.sort().map((n) => `${REFERENCES_DIR}/${n}`);
}
