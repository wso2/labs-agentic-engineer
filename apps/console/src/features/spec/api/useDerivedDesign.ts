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

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { type PrototypeModel } from "@aep/excalidraw-dsl";
import { deriveWireframeScene } from "../derive/deriveWireframe";
import { derivePrototypeModel } from "../derive/derivePrototype";
import { fetchSpecFileContent } from "./queries";
import { specKeys } from "./keys";

/**
 * Internal hook for shared spec file query logic. Returns the raw query result
 * with no derivation applied — consumers apply their own derive function
 * (e.g. deriveWireframeScene, derivePrototypeModel).
 *
 * The Files API reads by PATH at HEAD — `sha` is never sent to the server
 * (see fetchSpecFileContent); it is only a cache key. So fetch whenever we
 * have a path, even before the committed file-list catches up with an
 * agent-created file (whose sha is still unknown). Gating `enabled` on `sha`
 * left the query permanently disabled, so `isPending` stayed true forever —
 * an infinite spinner on a file that was perfectly fetchable by path.
 *
 * A content sha pins immutable content; without one we're reading HEAD, so
 * revalidate instead of caching a pre-commit read forever. The identical
 * cache key across useDerivedWireframe and useDerivedPrototype guarantees
 * toggling view modes never refetches.
 */
function useSpecFileQuery(
  projectName: string,
  dslPath: string,
  sha: string | undefined,
) {
  return useQuery({
    queryKey: specKeys.file(projectName, dslPath, sha ?? "head"),
    enabled: Boolean(dslPath),
    staleTime: sha ? Infinity : 0,
    queryFn: () => fetchSpecFileContent(projectName, { path: dslPath, sha: sha ?? "" }),
  });
}

export function useDerivedWireframe(
  projectName: string,
  dslPath: string,
  sha: string | undefined,
): { scene: string | null; isPending: boolean; isError: boolean } {
  const q = useSpecFileQuery(projectName, dslPath, sha);
  const scene =
    q.data?.content != null ? deriveWireframeScene(dslPath, q.data.content) : null;
  return { scene, isPending: q.isPending, isError: q.isError };
}

export function useDerivedPrototype(
  projectName: string,
  dslPath: string,
  sha: string | undefined,
): { model: PrototypeModel | null; isPending: boolean; isError: boolean } {
  const q = useSpecFileQuery(projectName, dslPath, sha);
  const model = useMemo(
    () =>
      q.data?.content != null ? derivePrototypeModel(dslPath, q.data.content) : null,
    [dslPath, q.data?.content],
  );
  return { model, isPending: q.isPending, isError: q.isError };
}
