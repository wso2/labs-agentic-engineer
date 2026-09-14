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

// Dev CLI (`make workflow-skill`) — prints the `aep` workflow skill exactly as a
// session reads it, followed by the tool glossary the runner appends after it,
// on stdout:
//
//   make workflow-skill             # github mode: the authored trunk, verbatim
//   MODE=local make workflow-skill  # what a playground run reads
//
// It exists because local mode's text is DERIVED — the authored `SKILL.md` plus
// `overlays/local.md` — so "what is the agent actually steered by?" has no file
// on disk to open. This runs the same composer a run runs, which is the whole
// point: a second hand-maintained copy would drift (ADR-0005).
//
// Answering that question used to be free, because a local run wrote its composed
// plugin under the run dir. It cannot any more: composing into that dir is a
// bind mount in docker mode, where `fs.cpSync` fails EACCES. So the question
// moved here, off a run's critical path.
//
// Never shipped in the production image (`.dockerignore`), and never on a run's
// path: `local_skill_mirror.ts` composes for a session.
//
// Env:
//   AEP_LIBRARY_DIR  the skill library  (default: the checkout's repo-root
//                    skills/ — this CLI runs from a checkout, never in the image
//                    where the library is baked at /app/skills)
//   MODE             github | local     (default: github)

import path from "node:path";
import { fileURLToPath } from "node:url";
import { composeWorkflowSkill, type AgentMode } from "./lib/workflow_skill.js";
import { toolGlossary } from "./lib/tool_glossary.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

function readMode(): AgentMode {
  const raw = process.env.MODE ?? "github";
  if (raw !== "github" && raw !== "local") {
    throw new Error(`MODE must be "github" or "local", got ${JSON.stringify(raw)}`);
  }
  return raw;
}

const libraryDir = process.env.AEP_LIBRARY_DIR ?? path.resolve(__dirname, "../../../skills");
// stdout carries what the agent is steered by and nothing else, so it pipes into
// a diff or a pager. The glossary rides last here for the same reason it does in
// `startCodingRun`: the skill's roles are unresolved without it, so a reader
// checking "what does the agent actually receive?" has to see both.
process.stdout.write(`${composeWorkflowSkill(libraryDir, readMode())}\n\n${toolGlossary()}\n`);
