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

import { useEffect, useMemo, useReducer } from "react";
import { prototypeHash } from "@wso2/prototype-kit/feedback";
import { subscribeReviewed, unreviewed } from "./model/reviewed";
import { usePrototypes } from "./usePrototypes";

/**
 * Whether the Prototype tab has news: a valid prototype whose current
 * revision this browser has not opened in a review yet, the same condition
 * the chat's Open prototype note follows. A revision the agent writes later
 * shows it again; one still being written (a turn running) does not yet.
 */
export function usePrototypeDot(projectName: string): boolean {
  const prototypes = usePrototypes(projectName);
  // The revisions on offer, hashed once per change of the prototypes.
  const hashes = useMemo(
    () =>
      Object.fromEntries(
        (prototypes ?? []).flatMap((p) =>
          p.status === "ready" && p.files ? [[p.component, prototypeHash(p.files.manifestText, p.files.source)] as const] : [],
        ),
      ),
    [prototypes],
  );
  const [, recheck] = useReducer((n: number) => n + 1, 0);
  useEffect(() => subscribeReviewed(recheck), []);

  return unreviewed(projectName, hashes).length > 0;
}
