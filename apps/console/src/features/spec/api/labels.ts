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
import { DOMAIN_MODEL_PATH, SECURITY_JSON_PATH, isDependencyDefinition } from "./designTree";

const OPENAPI_RE = /\/openapi\.ya?ml$/;
const GRAPHQL_SCHEMA_RE = /^specs\/design\/dependencies\/[^/]+\/schema\.graphql$/;
const COMPONENT_DESIGN_RE = /^specs\/design\/components\/[^/]+\/design\.json$/;
const SDK_MANIFEST_RE = /^specs\/design\/dependencies\/[^/]+\/sdk\.json$/;
const AGENT_AFM_RE = /^specs\/design\/components\/[^/]+\/agent\.afm\.md$/;

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
  // An ai-agent's definition. Named like its siblings — the file is
  // `agent.afm.md`, but what the reader is opening is the agent's spec.
  if (AGENT_AFM_RE.test(path)) return "Agent spec";
  // A dependency's directory reads the same way under its own header —
  // `stripe › Definition · API · SDK` — the interface file taking the name
  // a component's does, whichever style wrote it.
  if (isDependencyDefinition(path)) return "Definition";
  if (GRAPHQL_SCHEMA_RE.test(path)) return "API";
  if (SDK_MANIFEST_RE.test(path)) return "SDK";
  // A document nothing above names — a feature file most of the time, where
  // the filename IS the feature's name once the extension is off it. Keeping
  // the extension would leave the one surface the user reads throughout still
  // showing them a file.
  //
  // Both senses of "feature file" land here and want the same treatment: a
  // requirement's `<slug>.md` depth document, and a `<capability>.feature` of
  // acceptance criteria. Each is named for what it covers, under a section
  // header that already says which phase it belongs to.
  const name = basename(path).replace(/\.(md|feature)$/, "");
  // A `.feature` slug is a capability name the agent chose (`bought-items`), so
  // it is title-cased. The RAIL no longer shows these — one "Acceptance criteria"
  // entry stands for the set, and the pane names each capability from its own
  // `Feature:` heading — but a path still needs a label wherever one is asked
  // for, such as the pane's "Waiting for the agent to write …". A requirement's
  // `<slug>.md` keeps its verbatim name: those are referenced by name in the PRD,
  // and re-casing one would stop the two matching.
  return path.endsWith(".feature") ? titleCase(name) : name;
}

// `bought-items` -> `Bought items`. First word only: a capability is a phrase, not
// a heading, and Title Casing Every Word reads as a product name.
function titleCase(slug: string): string {
  const words = slug.split(/[-_]+/).filter(Boolean).join(" ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

