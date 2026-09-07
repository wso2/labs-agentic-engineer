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

import { PRD_PATH } from "./mapping";
import { DOMAIN_MODEL_PATH, SECURITY_JSON_PATH } from "./designTree";

const OPENAPI_RE = /\/openapi\.ya?ml$/;
const COMPONENT_DESIGN_RE = /^specs\/design\/components\/[^/]+\/design\.json$/;
const VALIDATION_CRITERIA_RE = /^specs\/validation\/validation-criteria\.json$/;

function basename(path: string): string {
  return path.split("/").at(-1) ?? path;
}

/**
 * A document's NAME, never its filename (#575).
 *
 * The user is reading a document tree, not a repository — `prd.md` and
 * `security.json` are storage details that leaked into the one surface they
 * read throughout the journey. The repo paths deliberately do not change; this
 * is the mapping, and the lexicon holds the same table in words.
 *
 * A file with no entry here falls back to its filename, which keeps an
 * agent-invented document readable rather than blank. Feature files land there
 * on purpose: their filename IS the feature's name, so the fallback is already
 * the right answer.
 */
const TITLES: Record<string, string> = {
  [PRD_PATH]: "Product requirements",
  [DOMAIN_MODEL_PATH]: "Domain model",
  [SECURITY_JSON_PATH]: "Security",
};

export function fileLabel(path: string): string {
  if (Object.hasOwn(TITLES, path)) return TITLES[path] as string;
  if (OPENAPI_RE.test(path)) return "API";
  // Under the component's own header, so the label adds the artifact and
  // never repeats the subject — `orders › Design · API · Wireframe`.
  if (COMPONENT_DESIGN_RE.test(path)) return "Design";
  if (VALIDATION_CRITERIA_RE.test(path)) return "Validation criteria";
  // A document nothing above names — a feature file most of the time, where
  // the filename IS the feature's name once the extension is off it. Keeping
  // `.md` would leave the one surface the user reads throughout still showing
  // them a file.
  return basename(path).replace(/\.md$/, "");
}

