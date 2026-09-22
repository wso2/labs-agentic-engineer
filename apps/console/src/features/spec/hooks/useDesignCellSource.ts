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

import { useSpecFileContent } from "../api/queries";
import { DESIGN_CELL_PATH } from "../api/designTree";
import type { SpecFileEntry } from "../api/mapping";
import type { CollabSpec } from "../collab/useCollabSpec";
import { useYTextString } from "../collab/useYTextString";

export interface DesignCellSource {
  /** The cell's text, or null while there is none to read. */
  source: string | null;
  /** The committed-copy fallback is loading. */
  isPending: boolean;
  /** The committed-copy fallback failed. */
  isError: boolean;
}

/**
 * The design cell's text, from wherever it is freshest.
 *
 * design.cell IS the architecture, so everything that reads it — the diagram,
 * the rail's Prototype stage — reads it here. Connected, the collab doc
 * supplies it live (committed content is seeded into the room; an agent
 * editFile lands in place, a restructure's removeFile + addFile re-streams
 * line by line). Solo/offline, the committed git blob is fetched over REST.
 */
export function useDesignCellSource(
  projectName: string,
  files: SpecFileEntry[],
  collab: CollabSpec,
): DesignCellSource {
  const liveSource = useYTextString(collab.getFileText(DESIGN_CELL_PATH));
  const committed =
    files.find((f) => f.path === DESIGN_CELL_PATH && f.sha !== "") ?? null;
  const restFallback = liveSource === null ? committed : null;
  const rest = useSpecFileContent(projectName, restFallback);
  return {
    source: liveSource ?? (restFallback ? (rest.data?.content ?? null) : null),
    isPending: restFallback !== null && rest.isPending,
    isError: restFallback !== null && rest.isError,
  };
}
