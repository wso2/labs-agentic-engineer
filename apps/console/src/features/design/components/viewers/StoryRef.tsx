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

import { createLink } from "@tanstack/react-router";
import { ButtonBase } from "@wso2/oxygen-ui";
import { soft } from "../../../spec/components/Tag";

const StoryLink = createLink(ButtonBase);

/** A story an artifact walks, "F2.2": opens the line in the spec. `tag` shows it as a scenario's tag, "@story-F2.2". */
export function StoryRef({ projectName, id, tag = false }: { projectName: string; id: string; tag?: boolean }) {
  return (
    <StoryLink
      to="/projects/$projectName/spec"
      params={{ projectName }}
      search={{ file: id.split(".")[0]!, at: id }}
      title="Open in the spec"
      sx={{
        fontFamily: "monospace",
        fontSize: "0.72rem",
        color: "primary.main",
        bgcolor: soft("primary"),
        border: 1,
        borderColor: "transparent",
        borderRadius: 1,
        px: 0.625,
        verticalAlign: "baseline",
        whiteSpace: "nowrap",
        flexShrink: 0,
        "&:hover": { borderColor: "primary.main" },
      }}
    >
      {tag ? `@story-${id}` : id}
    </StoryLink>
  );
}
