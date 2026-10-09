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
 * The playground's in-process `ToolsSocket`: what ae-studio-tools is
 * to the pod, played from the project folder.
 *
 * - `lookup(<the project>)` materializes the folder and the working-tree skill
 *   library into `FsSpecWorkspace`'s snapshots and answers their shas, the
 *   reference document names and the descriptor's idea, as the socket does
 *   from git. Any other project name is not this run's (`null`).
 * - `skills()` materializes the skill library alone (marketplace turns).
 * - MCP is the design agent's catalog stubs (`FakeToolsSocket`): the eleven
 *   design tools are listed, and every call answers that a local run has no
 *   platform catalog, so the agent treats the org as empty.
 * - There is no Room (the session wires none) and no usage ledger: usage
 *   records are accepted and dropped.
 */

import { FakeToolsSocket } from "@aep/ae-design-agent/tools-socket/fake";
import {
  ToolsSocketError,
  type ProjectSnapshot,
  type SkillsSnapshot,
  type ToolsSocket,
} from "@aep/ae-design-agent/tools-socket/client";
import { loadRepoSkills } from "../kit/skills.js";
import type { FsSpecWorkspace } from "../ports/spec-workspace.js";
import { readIdea } from "../state/descriptor.js";
import { readReferences } from "../state/references.js";

/** Every catalog tool's answer in a local run. */
export const NO_CATALOG = "The local playground has no platform catalog: nothing is registered in this organization.";

export class PlaygroundToolsSocket implements ToolsSocket {
  readonly mcpFetch = new FakeToolsSocket({ callTool: () => NO_CATALOG }).mcpFetch;

  constructor(
    private readonly ws: FsSpecWorkspace,
    /** Repo-root `skills/`, read fresh on every lookup (§8 hot-reload). */
    private readonly skillsDir: string,
  ) {}

  async lookup(project: string, at?: string): Promise<ProjectSnapshot | null> {
    if (project !== this.ws.slug) return null;
    const headSha = this.ws.materializeFiles(this.ws.readSpecFiles());
    // The folder has no history: only its current state is a commit.
    if (at !== undefined && at !== headSha) {
      throw new ToolsSocketError("ref_not_found", 404, "at names no commit of the repository");
    }
    const idea = readIdea(this.ws.projectDir);
    return {
      headSha,
      skillsSha: this.materializeSkills(),
      references: readReferences(this.ws.projectDir),
      ...(idea !== null ? { idea } : {}),
    };
  }

  async skills(): Promise<SkillsSnapshot> {
    return { skillsSha: this.materializeSkills() };
  }

  async postUsage(): Promise<void> {
    // No usage ledger in a local run.
  }

  private materializeSkills(): string {
    return this.ws.materializeSkills(loadRepoSkills(this.skillsDir));
  }
}
