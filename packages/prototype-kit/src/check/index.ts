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
 * `@wso2/prototype-kit/check`: the whole prototype check, in the order an
 * author fixes things — the files, the manifest's shape and references, the
 * source's static rules, the source's literal references into the manifest,
 * then the isolated render of every screen × role × state. Each stage runs
 * only when the earlier ones pass.
 */

import type { Finding } from "../findings.js";
import { parseManifestJson } from "../manifest/parse.js";
import { sourceReferenceFindings } from "../source/references.js";
import { checkSource } from "../source/static-check.js";
import { missingFileFindings, readPrototypeFiles, type PrototypeFiles } from "./files.js";
import { checkRenderIsolated } from "./render.js";
import type { ThemeRuntimes } from "./theme.js";

export { FINDING_CODES, MANIFEST_FILE, SOURCE_FILE, type Finding, type FindingCode, type FindingFile } from "../findings.js";
export { readPrototypeFiles, type PrototypeFiles } from "./files.js";
export { RENDER_TIMEOUT_MS } from "./render.js";
export { ThemeNotFoundError, resolveTheme, type ThemeRuntimes } from "./theme.js";

export interface CheckOptions {
  /** The theme whose check runtime renders the screens. */
  theme: ThemeRuntimes;
}

/** Every finding for these two files; empty when they are ready to preview. The render stage runs in a child process, off the caller's event loop. */
export async function checkPrototypeFiles(files: PrototypeFiles, options: CheckOptions): Promise<Finding[]> {
  if (files.manifest === null || files.source === null) return missingFileFindings(files);
  const manifest = parseManifestJson(files.manifest);
  if (!manifest.ok) return manifest.findings;
  const staticFindings = checkSource(files.source);
  if (staticFindings.length > 0) return staticFindings;
  const references = sourceReferenceFindings(files.source, manifest.manifest);
  if (references.length > 0) return references;
  return checkRenderIsolated(manifest.manifest, files.source, options.theme);
}

/** Every finding for the prototype folder `dir`; empty when it is ready to preview. */
export function checkPrototype(dir: string, options: CheckOptions): Promise<Finding[]> {
  return checkPrototypeFiles(readPrototypeFiles(dir), options);
}
