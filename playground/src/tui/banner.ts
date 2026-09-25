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
 * The chat entry banner: printed once when `play <dir>` drops you into chat.
 * Chat is the home surface now, so this folds in what the phase menu used to
 * give at a glance — a compact per-phase status line — plus the active agent
 * name and the command guide. The menu still lives behind `/menu` for the rich
 * dashboard. Pure string builder: reads FS status, writes nothing.
 */

import { designStatus, listIssueSummaries, requirementsStatus } from "../state/status.js";

/** One compact status line summarizing how far the project has progressed. */
function statusLine(projectDir: string): string {
  const req = requirementsStatus(projectDir);
  const design = designStatus(projectDir);
  const issues = listIssueSummaries(projectDir);
  const resolved = issues.filter((i) => i.resolved).length;

  const parts = [
    req.present ? "requirements ✓" : "requirements —",
    design.components.length > 0 ? `design ✓ (${design.components.length} component${design.components.length === 1 ? "" : "s"})` : "design —",
    issues.length > 0 ? `tasks ✓ (${resolved}/${issues.length} resolved)` : "tasks —",
  ];
  return parts.join("  ·  ");
}

/** The command guide — shared by the entry banner and `/help`. */
export function commandGuide(): string {
  return [
    "  Commands:",
    "    /start [idea]          kick off the project — interview, then requirements",
    "    /spec /design [text]   generate the spec / design (load a skill and follow it)",
    "    /<skill> [text]        load any working-tree skill and follow it (e.g. /grilling)",
    "    /task                  plan implementation tasks",
    "    /code                  run the coding agent (one session, whole plan)",
    "    /wire [role]           run what was built, locally, as a role — browser + panel",
    "    /validate              check design artifacts against the schema",
    "    /undo                  restore the last pre-coding snapshot",
    "    /menu  /help  /quit    dashboard · this guide · leave",
  ].join("\n");
}

/** Build the multi-line entry banner for `slug` at `projectDir`. */
export function buildChatBanner(projectDir: string, slug: string, skillCount: number): string {
  return [
    `  ${slug} — engineering agent  ·  skills: ${skillCount} (working tree)`,
    `  ${statusLine(projectDir)}`,
    "",
    commandGuide(),
  ].join("\n");
}
