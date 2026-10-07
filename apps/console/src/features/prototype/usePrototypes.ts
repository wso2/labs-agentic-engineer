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
import { useProjectChat } from "../agent-chat/useProjectChat";
import { useDesignModel } from "../design/useDesignModel";
import { useSpecDoc } from "../spec/collab/specDoc";
import { useRoomFiles } from "../spec/collab/useRoomFiles";
import { appPrototypes, COMPONENTS_PREFIX, revisingIn, webApplications, type AppPrototype } from "./model/prototypes";

const PROTOTYPE_FILES = [COMPONENTS_PREFIX] as const;

/**
 * The project's prototypes, one per web application the design has, read
 * from the room as it changes (an agent's write shows on the next render),
 * with the running turn's. Undefined until the design is known.
 */
export function usePrototypes(projectName: string): AppPrototype[] | undefined {
  const design = useDesignModel(projectName).data;
  const files = useRoomFiles(useSpecDoc(projectName), PROTOTYPE_FILES);
  const chat = useProjectChat(projectName);
  const instruction = chat.turn.phase === "idle" ? undefined : chat.turn.instruction;
  return useMemo(
    () => (design ? appPrototypes(webApplications(design.artifacts), files, revisingIn(instruction)) : undefined),
    [design, files, instruction],
  );
}
