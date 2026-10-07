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
 * Reading a prototype folder: its two files, or `null` for one that is not
 * there. A missing file is a finding, not an error, so an agent learns the
 * folder's shape from `prototype check` itself.
 */

import { readFileSync } from "node:fs";
import { join } from "node:path";
import { MANIFEST_FILE, SOURCE_FILE, type Finding, type FindingFile } from "../findings.js";

export interface PrototypeFiles {
  /** `prototype.json`'s text, or null when the folder has none. */
  manifest: string | null;
  /** `prototype.tsx`'s text, or null when the folder has none. */
  source: string | null;
}

export function readPrototypeFiles(dir: string): PrototypeFiles {
  return { manifest: readIfPresent(join(dir, MANIFEST_FILE)), source: readIfPresent(join(dir, SOURCE_FILE)) };
}

function readIfPresent(path: string): string | null {
  try {
    return readFileSync(path, "utf8");
  } catch (e) {
    const code = (e as NodeJS.ErrnoException).code;
    if (code === "ENOENT" || code === "ENOTDIR") return null;
    throw e;
  }
}

/** One `MISSING_FILE` per absent file; empty when both are there. */
export function missingFileFindings(files: PrototypeFiles): Finding[] {
  const missing: FindingFile[] = [];
  if (files.manifest === null) missing.push(MANIFEST_FILE);
  if (files.source === null) missing.push(SOURCE_FILE);
  return missing.map((file) => ({
    code: "MISSING_FILE",
    file,
    location: "(file)",
    message: `${file} is missing: a prototype folder holds prototype.json (the manifest) and prototype.tsx (the screens)`,
  }));
}
