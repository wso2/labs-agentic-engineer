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
import { parseDesignCommand } from "@aep/contracts/commands";
import { client } from "../../api/client";
import { env } from "../../config/env";
import { useProjectChat } from "../agent-chat/useProjectChat";
import { useSpecDoc } from "../spec/collab/specDoc";
import { useRoomFiles } from "../spec/collab/useRoomFiles";
import { useMockDesignModel, type DesignDependency, type DesignModel, type DesignRun } from "./api/designModel";
import { designCatalog } from "./model/catalog";

// The design review (N7, E4). On the platform it is worked out from the room:
// the catalog from the design's files (model/catalog.ts), the dependencies
// that wait on a person from the build preflight, and the design turn running
// now from the chat. Commenting (pins, Address comments) is the mock's alone
// until it ships (E5, parked): on the platform there are no comments, and the
// card does not offer to pin one.
//
// In mock mode the whole review is the mock's, as approved.

const DESIGN_FILES = ["specs/design/", "specs/validation/acceptance/"] as const;
const TAUGHT_KEY = "aep:design-taught";

function taughtHere(): boolean {
  try {
    return localStorage.getItem(TAUGHT_KEY) === "1";
  } catch {
    return false;
  }
}

/** The dependencies a person must decide before their features build (E2): one per feature its component serves. */
function dependenciesOf(
  items: { component: string; dependency: string; kind: string; description: string }[],
  componentFeatures: ReadonlyMap<string, string[]>,
): DesignDependency[] {
  return items
    .filter((i) => i.kind === "external-unresolved")
    .flatMap((i) =>
      (componentFeatures.get(i.component) ?? []).map((featureId) => ({
        featureId,
        needs: i.dependency,
        question: i.description,
        why: `${featureId} cannot be built until ${i.dependency} is settled.`,
        options: [],
        answer: null,
      })),
    );
}

export function useDesignModel(projectName: string) {
  const mock = env.apiMode === "mock";
  const mocked = useMockDesignModel(projectName, mock);
  const files = useRoomFiles(useSpecDoc(projectName), DESIGN_FILES);
  const chat = useProjectChat(projectName);
  const preflight = useQuery({
    queryKey: ["projects", projectName, "build-preflight"],
    queryFn: async () => {
      const { data, error } = await client.GET("/projects/{projectName}/build/preflight", {
        params: { path: { projectName } },
      });
      if (error || data === undefined) throw new Error("Couldn't read the design's dependencies");
      return data;
    },
    enabled: !mock,
  });

  const platform = useMemo((): DesignModel | undefined => {
    if (mock) return undefined;
    const artifacts = designCatalog(files);
    const componentFeatures = new Map(
      artifacts.flatMap((a) => (a.source.kind === "contract" ? [[a.id, a.features] as const] : [])),
    );
    const instruction = chat.turn.phase === "idle" ? undefined : chat.turn.instruction;
    const design = instruction ? parseDesignCommand(instruction) : null;
    const running: DesignRun | null = design ? { kind: "design", features: design.featureIds } : null;
    return {
      revision: 0,
      running,
      artifacts,
      dependencies: dependenciesOf(preflight.data?.items ?? [], componentFeatures),
      comments: [],
      taught: taughtHere(),
      commenting: false,
    };
  }, [mock, files, chat.turn, preflight.data]);

  if (mock) return mocked;
  return {
    data: platform,
    isPending: false,
    isError: preflight.isError,
    error: preflight.error,
    refetch: preflight.refetch,
  };
}
