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
 * how the security design is read. Threading the document read and live
 * directory query through the page component made one file change for two
 * unrelated reasons.
 *
 * One document (`security.json`) from the collab room (committed fallback when
 * the room has not delivered it yet), the sibling specs its cross-checks read,
 * and the write back into the room. Live directory chips come from the platform
 * as the last Build left them.
 *
 * Spec edits the DOCUMENT and nothing else: a grant is a design decision and
 * lives in the file, while the accounts the platform created are somebody's
 * credentials — reveal, rotate and delete stay on Deploy (ticket 15).
 */

import { useCallback, useMemo } from "react";

import { SECURITY_JSON_PATH } from "../api/designTree";
import type { SpecFileEntry } from "../api/mapping";
import { useSpecFileContent } from "../api/queries";
import {
  useProjectRoles,
  type ProjectRolesLiveState,
} from "../api/roles";
import {
  parseSecurityDesign,
  referencePaths,
} from "../api/securityDesign";
import type { CollabSpec } from "../collab/useCollabSpec";
import { applyTextareaValue } from "../collab/textareaBinding";
import { useYTextString } from "../collab/useYTextString";
import { useSpecReferences, type SpecReferences } from "./useSpecReferences";

export interface SecurityEntry {
  /** The security document — live from the room, else the committed copy. */
  securityJson: string | null;
  /** The live directory state, undefined while it loads. */
  live: ProjectRolesLiveState | undefined;
  /**
   * True only while the committed `security.json` fallback is in flight.
   * A disabled query still reports `isPending` in react-query, so this is
   * gated on the fallback actually being used — same rule as Architecture
   * and Wireframes. Live directory chips (`GET …/roles`) fill in after paint
   * and do not block the page.
   */
  isPending: boolean;
  /** True only when that same committed fallback failed. */
  isError: boolean;
  /**
   * The sibling spec files the document's cross-checks read — `design.cell`,
   * the owning components' OpenAPI, the screens' wireframes — resolved room
   * first and committed copy second. It satisfies
   * `SecurityReferenceContext` from `@aep/agent-stream`, which is what
   * `securityReferenceFindings(doc, ctx)` wants.
   *
   * ALWAYS present, never undefined: "which files can I see?" is a per-path
   * question, and a whole-context `undefined` would make a caller branch on
   * something that is never true — the object simply answers `undefined` for
   * a path nothing holds.
   */
  references: SpecReferences;
  /**
   * True when the ROOM holds `security.json` — which is the same thing as "an
   * edit is possible here". False means the page is showing the committed copy
   * from git, and `writeSecurityJson` has nowhere to put a change.
   *
   * This is the editability signal, and the only one: the console models no
   * "this version is tagged" state anywhere, so nothing else can be asked.
   */
  roomLive: boolean;
  /**
   * Write the WHOLE document back into the room, as a minimal CRDT edit
   * relative to what is there — so a concurrent edit by the design agent merges
   * instead of being clobbered. Serialise with `serializeSecurityDesign` first.
   *
   * Synchronous, and a no-op when `roomLive` is false. It deliberately does not
   * flush: the room commits to git on its own schedule, and `collab.flush()`
   * belongs to the Build path, which needs HEAD to be current before it tags.
   */
  writeSecurityJson: (next: string) => void;
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
  // The room's copy IS the editable one, so the Y.Text is held rather than only
  // read: `roomLive` and the writer are both answers about this object.
  const securityYText = active ? collab.getFileText(SECURITY_JSON_PATH) : null;
  const securityLiveText = useYTextString(securityYText);
  // The committed copy is the solo fallback only. An agent in the room also
  // suppresses it: the doc WILL deliver the file, and probing git for a
  // not-yet-committed path just sprays 404s.
  const restFallback =
    active && securityLiveText === null && !agentInRoom
      ? (files.find((f) => f.path === SECURITY_JSON_PATH) ?? null)
      : null;
  const securityCommitted = useSpecFileContent(projectName, restFallback);

  const live = useProjectRoles(projectName, active);

  const securityJson =
    securityLiveText ?? securityCommitted.data?.content ?? null;

  // Which siblings to read is a property of the DOCUMENT — the components that
  // own a resource, the components a screen names — so the paths are derived
  // from the parsed copy rather than from the whole design tree. An unreadable
  // document asks for nothing, which is right: there is nothing to cross-check.
  const paths = useMemo(() => {
    const parsed = parseSecurityDesign(securityJson);
    return parsed.kind === "ok" ? referencePaths(parsed.doc) : [];
  }, [securityJson]);

  const references = useSpecReferences({
    projectName,
    paths,
    files,
    collab,
    active,
  });

  const writeSecurityJson = useCallback(
    (next: string) => {
      if (!securityYText) return;
      applyTextareaValue(securityYText, next);
    },
    [securityYText],
  );

  return {
    securityJson,
    live: live.data,
    isPending: restFallback !== null && securityCommitted.isPending,
    isError: restFallback !== null && securityCommitted.isError,
    references,
    roomLive: securityYText !== null,
    writeSecurityJson,
  };
}
