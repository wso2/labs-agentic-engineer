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

import { applyTextEdit } from "@aep/collab-doc";

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
   * Edit the document in the room: the updater is handed the room's text AS IT
   * IS NOW and returns the whole next document, or `null` to write nothing.
   *
   * It takes an UPDATER rather than a string because the caller cannot be
   * trusted with the timing. A patch computed from the text of the last React
   * render is then applied as a diff against the room's current text, and the
   * two are not the same string whenever the design agent flushed in between —
   * the diff's single changed span then covers the agent's insertion, and
   * applying it deletes that insertion. Reading the text and computing the next
   * one inside this call makes the patch and the diff see one text, so a
   * concurrent edit merges at the CRDT level, which is the whole point of
   * writing into a room instead of over a file.
   *
   * Synchronous, and a no-op when `roomLive` is false. It deliberately does not
   * flush: the room commits to git on its own schedule, and `collab.flush()`
   * belongs to the Build path, which needs HEAD to be current before it tags.
   */
  writeSecurityJson: (update: (current: string) => string | null) => void;
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
    (update: (current: string) => string | null) => {
      if (!securityYText) return;
      // One read, one diff, one transaction. `applyTextEdit` is the real
      // character diff every other writer into this room uses (the design
      // agent's own writes go through it), rather than the placeholder
      // textarea binding's single prefix/suffix trim: a trim assumes the two
      // texts differ in exactly one span, which a re-serialised JSON document
      // cannot promise — the room's copy need not be in the two-space form
      // `patchGrants` emits, and one differently-indented line makes the trim
      // delete and re-insert everything between the first and last difference.
      const current = securityYText.toString();
      const next = update(current);
      if (next === null || next === current) return;
      applyTextEdit(securityYText, next);
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
