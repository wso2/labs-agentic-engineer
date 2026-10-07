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

import { Box, Button, Typography } from "@wso2/oxygen-ui";
import { useStartInterview } from "../../agent-chat/useStartInterview";
import { useChatPanel } from "../../shell/chatPanel";
import type { SpecFeature } from "../api/specModel";

/**
 * A feature that has a name and a purpose only. Its interview runs in the
 * chat, beside this document, which fills in as the questions are answered.
 */
export function StubNote({
  projectName,
  feature,
}: {
  projectName: string;
  feature: Pick<SpecFeature, "id" | "name" | "path" | "stage">;
}) {
  const interview = useStartInterview(projectName);
  const chat = useChatPanel();
  const interviewing = feature.stage === "Interviewing";
  return (
    <Box
      sx={{
        mt: 2.25,
        maxWidth: "72ch",
        border: "1px dashed",
        borderColor: "divider",
        borderRadius: 2.5,
        px: 1.75,
        py: 1.5,
        display: "flex",
        flexDirection: "column",
        alignItems: "flex-start",
        gap: 1,
      }}
    >
      <Typography variant="body2" color="text.secondary">
        {interviewing
          ? "The interview is under way in the chat. This page fills in once the questions are answered."
          : "Not interviewed yet. This feature has a name and a purpose only, so it can't be built yet."}
      </Typography>
      {interviewing ? (
        <Button size="small" variant="outlined" onClick={chat.open}>
          Open the chat
        </Button>
      ) : (
        <Button
          size="small"
          variant="contained"
          disabled={!interview.ready}
          onClick={() => interview.start(feature)}
        >
          Start interview
        </Button>
      )}
      {!interviewing && interview.waiting && (
        <Typography variant="caption" color="text.secondary">
          The agent is working. You can start once it's done.
        </Typography>
      )}
    </Box>
  );
}
