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
 * Everything the Security rail entry needs, gathered in one place.
 *
 * It lives here rather than inline in `SpecView` because the two are edited for
 * different reasons: `SpecView` owns the rail and the pane ladder, and this owns
 * how the security design is read and written. Threading four hooks, two
 * document reads and a write path through the page component made one file
 * change for two unrelated reasons.
 *
 * The two halves are read from two different places on purpose. The DESIGN
 * (`roles.json`) comes from the collab room, so an edit shows the instant it is
 * made. The LIVE state comes from the platform, and is the world as the last
 * Build left it. Rendering them together is what makes "new at Build" legible
 * rather than a guess.
 */

import { setDocFile } from "@aep/collab-doc";

import type { AccountActions } from "../components/SecurityPanel";
import { ROLES_JSON_PATH, SECURITY_MD_PATH } from "../api/designTree";
import type { SpecFileEntry } from "../api/mapping";
import { useSpecFileContent } from "../api/queries";
import {
  useDeleteTestUser,
  useProjectRoles,
  useRevealTestUserPassword,
  useRotateTestUserPassword,
  type ProjectRolesLiveState,
} from "../api/roles";
import type { CollabSpec } from "../collab/useCollabSpec";
import { useYTextString } from "../collab/useYTextString";

export interface SecurityEntry {
  /** The roles document — live from the room, else the committed copy. */
  rolesJson: string | null;
  /** The live directory state, undefined while it loads. */
  live: ProjectRolesLiveState | undefined;
  /**
   * Apply an edited roles document, or undefined when there is nothing safe to
   * write to. The panel renders read-only in that case rather than offering
   * controls that would silently do nothing.
   */
  onRolesChange: ((next: string) => void) | undefined;
  /** The prose half's collaborative fragment, null when the room is down. */
  proseFragment: ReturnType<CollabSpec["getFileFragment"]>;
  /** Reveal / rotate / delete on an owned account. */
  actions: AccountActions;
}

export function useSecurityEntry({
  projectName,
  active,
  files,
  collab,
  agentInRoom,
}: {
  projectName: string;
  /** False when the Security entry is not the current selection — every read
   *  below is then skipped rather than fetched and thrown away. */
  active: boolean;
  files: SpecFileEntry[];
  collab: CollabSpec;
  agentInRoom: boolean;
}): SecurityEntry {
  const rolesLiveText = useYTextString(
    active ? collab.getFileText(ROLES_JSON_PATH) : null,
  );
  // The committed copy is the solo fallback only. An agent in the room also
  // suppresses it: the doc WILL deliver the file, and probing git for a
  // not-yet-committed path just sprays 404s.
  const rolesCommitted = useSpecFileContent(
    projectName,
    active && rolesLiveText === null && !agentInRoom
      ? (files.find((f) => f.path === ROLES_JSON_PATH) ?? null)
      : null,
  );

  const live = useProjectRoles(projectName, active);
  const reveal = useRevealTestUserPassword(projectName);
  const rotate = useRotateTestUserPassword(projectName);
  const remove = useDeleteTestUser(projectName);

  // The panel patches the room's Y.Text and the room's committer lands it — one
  // writer to committed truth, so the design agent and the panel cannot race.
  const doc = collab.doc;
  const onRolesChange =
    doc && collab.status === "connected"
      ? (next: string) => setDocFile(doc, ROLES_JSON_PATH, next)
      : undefined;

  return {
    rolesJson: rolesLiveText ?? rolesCommitted.data?.content ?? null,
    live: live.data,
    onRolesChange,
    proseFragment: active ? collab.getFileFragment(SECURITY_MD_PATH) : null,
    actions: {
      reveal: async (u) => (await reveal.mutateAsync(u)).password,
      rotate: async (u) => (await rotate.mutateAsync(u)).password,
      remove: (u) => remove.mutateAsync(u),
    },
  };
}
