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
 * Everything the Acceptance criteria rail entry needs, gathered in one place —
 * the same split `useSecurityEntry` makes, and for the same reason: `SpecView`
 * owns the rail and the pane ladder, and this owns how the documents are read.
 *
 * ONE ENTRY, MANY DOCUMENTS. It is the only rail entry that stands for a set,
 * so it is the only read that has to be plural: every
 * `specs/validation/acceptance/<capability>.feature`, live from the room with the
 * committed copy as the fallback, handed to the view as one set so a reader can
 * search across capabilities rather than guessing which holds the scenario.
 *
 * It deliberately does NOT reuse `features/validation`'s `useAcceptanceFeatures`,
 * which reads the same files. That one reads the BRANCH TIP with a short stale
 * time because the Validations page pairs it with a report pinned to a merge
 * commit, and the gap between the two is what makes drift visible there. This
 * one wants the live document and immutable per-sha reads. Same bytes, two
 * genuinely different reads — and sharing one would also make `spec` depend on
 * `validation`, the one direction the console does not have.
 */

import { useQueries } from "@tanstack/react-query";
import type { AcceptanceFeatureSource } from "@aep/ui-acceptance-view";
import { isAcceptanceCriteriaFile, type SpecFileEntry } from "../api/mapping";
import { specKeys } from "../api/keys";
import { fetchSpecFileContent } from "../api/queries";
import type { CollabSpec } from "../collab/useCollabSpec";
import { useYTextStrings } from "../collab/useYTextStrings";

export interface AcceptanceEntry {
  /** One per capability, path-ordered. Only the documents that have arrived. */
  features: AcceptanceFeatureSource[];
  /** True while a committed fallback this entry actually needs is in flight. */
  isPending: boolean;
  /** True only when every committed fallback it needed failed. */
  isError: boolean;
}

export function useAcceptanceEntry({
  projectName,
  active,
  files,
  collab,
  agentInRoom,
}: {
  projectName: string;
  /** False when this is not the current selection — every read is then skipped
   *  rather than fetched and thrown away. */
  active: boolean;
  files: SpecFileEntry[];
  collab: CollabSpec;
  agentInRoom: boolean;
}): AcceptanceEntry {
  // From `files`, which SpecView has already unioned with the live doc paths —
  // so a capability the agent has created but not yet committed is in here.
  // Deriving the list from the git listing alone would miss exactly the files
  // this pane exists to watch being written.
  const entries = active ? files.filter((f) => isAcceptanceCriteriaFile(f.path)) : [];

  const live = useYTextStrings(
    entries.map((f) => (active ? collab.getFileText(f.path) : null)),
  );

  // The committed copy is the solo fallback, per document. An agent in the room
  // suppresses it wholesale: the doc WILL deliver these files, and probing git
  // for a not-yet-committed path just sprays 404s.
  const fallbacks = entries.map((f, i) =>
    (live[i] ?? "").trim() === "" && !agentInRoom ? f : null,
  );

  const committed = useQueries({
    queries: entries.map((f, i) => ({
      queryKey: specKeys.file(projectName, f.path, f.sha),
      enabled: fallbacks[i] !== null,
      // Immutable per path+sha, so it never needs refetching.
      staleTime: Infinity,
      queryFn: () => fetchSpecFileContent(projectName, { path: f.path, sha: f.sha }),
    })),
  });

  const features = entries
    .map((f, i) => {
      const text = live[i] ?? committed[i]?.data?.content ?? null;
      return text !== null && text.trim() !== "" ? { path: f.path, content: text } : null;
    })
    .filter((f): f is AcceptanceFeatureSource => f !== null);

  const wanted = fallbacks.filter((f) => f !== null).length;
  const failed = committed.filter((q, i) => fallbacks[i] !== null && q.isError).length;

  return {
    features,
    isPending: committed.some((q, i) => fallbacks[i] !== null && q.isPending),
    // Only when NOTHING arrived. One capability that failed to read among five
    // that did is a thinner page, not an error page.
    isError: wanted > 0 && failed === wanted && features.length === 0,
  };
}
