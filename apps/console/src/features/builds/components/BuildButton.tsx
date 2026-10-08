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

import { Button } from "@wso2/oxygen-ui";
import { useBuildAction } from "../buildPicker";

/** "Build v1", where a build is offered: it opens the picker. Nothing while nothing is designed or a build runs. */
export function BuildButton({ projectName, size = "medium" }: { projectName: string; size?: "small" | "medium" }) {
  const build = useBuildAction(projectName);
  if (!build.label) return null;
  return (
    <Button size={size} variant="contained" onClick={build.open}>
      {build.label}
    </Button>
  );
}
