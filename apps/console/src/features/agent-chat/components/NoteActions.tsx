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

import { useNavigate } from "@tanstack/react-router";
import { Box, Button } from "@wso2/oxygen-ui";
import { useSpecModel } from "../../spec/useSpecWorkspace";
import type { NoteAction } from "../chatLog";
import { useStartInterview } from "../useStartInterview";

/**
 * A note's next steps, under it: open a version on the Builds card, review a
 * prototype, or start the next feature's interview. The same steps the card offers, so the chat
 * and the card never disagree about what comes next.
 */
export function NoteActions({ projectName, actions }: { projectName: string; actions: NoteAction[] }) {
  const navigate = useNavigate();
  const interview = useStartInterview(projectName);
  const features = useSpecModel(projectName).data?.features;

  const act = (action: NoteAction) => {
    if (action.kind === "open-build") {
      void navigate({ to: "/projects/$projectName/builds/$version", params: { projectName, version: action.version } });
      return;
    }
    if (action.kind === "open-prototype") {
      void navigate({
        to: "/projects/$projectName/prototype",
        params: { projectName },
        search: action.component ? { review: action.component } : {},
      });
      return;
    }
    const feature = features?.find((f) => f.id === action.featureId);
    if (feature) interview.start(feature);
  };

  return (
    <Box sx={{ pl: 4, display: "flex", flexWrap: "wrap", gap: 0.75 }}>
      {actions.map((action) => (
        <Button
          key={action.label}
          size="small"
          variant="outlined"
          disabled={action.kind === "interview" && !interview.ready}
          onClick={() => act(action)}
        >
          {action.label}
        </Button>
      ))}
    </Box>
  );
}
