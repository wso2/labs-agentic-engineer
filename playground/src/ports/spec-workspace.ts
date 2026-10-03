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
 * `FsSpecWorkspace` — the project-dir side of a turn (docs/design/playground.md
 * §3/§5). The project folder is materialized into a fake immutable snapshot
 * on a temp mount, in the layout the design agent reads under
 * `AE_SNAPSHOTS_DIR` (`shared/snapshot-path.ts`), as ae-studio-tools writes it
 * in the pod:
 *
 *   <mount>/projects/<slug>/<sha>/…
 *   <mount>/skills/<sha>/skills/<name>/SKILL.md
 *
 * Snapshot "shas" are fake but content-addressed (40 hex), so identical states
 * share a dir and an EDITED skills library gets a fresh snapshot on the very
 * next turn (§8 hot-reload). `issues/` is excluded from SPEC-turn reads:
 * production spec turns never see tasks (they live in GitHub); issues enter
 * only the plan turn's task context (§5 phase 3). The in-process tools socket
 * (`engine/tools-fake.ts`) materializes on every project lookup.
 */

import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import { tmpdir } from "node:os";
import { readProjectFiles, resolveWithin } from "../kit/project-fs.js";
import { filesSnapshotSha, renderSkillFiles, skillsSnapshotSha } from "../kit/snapshot.js";
import type { RepoSkill } from "../kit/skills.js";
import { disabledSkillNames } from "../kit/skills.js";

/** The longest project name: a DNS label. */
const MAX_PROJECT_NAME = 63;

/**
 * The project's name on the design agent's `/v1` edge, from the directory
 * name: a DNS label, as the platform names projects (`isProjectName`).
 */
export function projectSlug(projectDir: string): string {
  const slug = basename(projectDir)
    .toLowerCase()
    .replace(/[^a-z0-9-]/g, "-")
    .replace(/-{2,}/g, "-")
    .slice(0, MAX_PROJECT_NAME)
    .replace(/^-+|-+$/g, "");
  return slug === "" ? "project" : slug;
}

export class FsSpecWorkspace {
  /** The temp snapshots mount the in-process design agent reads (its `snapshotsDir`). */
  readonly mountRoot: string;
  readonly slug: string;

  constructor(readonly projectDir: string) {
    this.slug = projectSlug(projectDir);
    this.mountRoot = mkdtempSync(join(tmpdir(), "aep-play-ws-"));
  }

  /**
   * Read the project into a spec-turn files map: dot-entries, binaries and
   * derived artifacts drop (project-fs walk), and `issues/` is excluded —
   * see the module doc. The server applies the production turn filter
   * (`keepInTurnSnapshot`) when it reads the snapshot back.
   */
  readSpecFiles(): Record<string, string> {
    const all = readProjectFiles(this.projectDir);
    const out: Record<string, string> = {};
    for (const [path, content] of Object.entries(all)) {
      if (path === "issues" || path.startsWith("issues/")) continue;
      out[path] = content;
    }
    return out;
  }

  /** Materialize one turn's files into a fake immutable `snapshots/<sha>/` dir. */
  materializeFiles(files: Record<string, string>): string {
    const sha = filesSnapshotSha(files);
    this.mirror(join(this.mountRoot, "projects", this.slug, sha), files);
    return sha;
  }

  /**
   * Materialize the skill library into the FLAT snapshot layout
   * (`skills/<name>/SKILL.md` + references). Content-addressed per call — an
   * edited library yields a new snapshot next turn; `skillsRef` is mandatory,
   * so an empty library still materializes an (empty) snapshot dir.
   */
  materializeSkills(skills: readonly RepoSkill[]): string {
    // AEP_DISABLED_SKILLS stands in for the org admin's availability toggle,
    // which the playground has no surface for; it lands in the snapshot's
    // manifest exactly as reconcile would write it.
    const disabled = disabledSkillNames();
    const sha = skillsSnapshotSha(skills, disabled);
    this.mirror(
      join(this.mountRoot, "skills", sha),
      renderSkillFiles(skills, disabled),
    );
    return sha;
  }

  private mirror(dir: string, files: Record<string, string>): void {
    if (existsSync(dir)) return;
    mkdirSync(dir, { recursive: true });
    for (const [path, content] of Object.entries(files)) this.write(dir, path, content);
  }

  private write(root: string, rel: string, content: string): void {
    const abs = resolveWithin(root, rel);
    mkdirSync(dirname(abs), { recursive: true });
    writeFileSync(abs, content, "utf8");
  }

  cleanup(): void {
    rmSync(this.mountRoot, { recursive: true, force: true });
  }
}
