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

import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Box, Button, Link, Typography } from "@wso2/oxygen-ui";
import { useChatPanel } from "../../shell/chatPanel";
import type { HandOffItem } from "../chatLog";
import { continueInIssues, getHandOff, setHandOff, type HandOffState } from "../handOffState";
import { chatStoreFor, useProjectChat } from "../useProjectChat";

/**
 * The main chat's announcement that a request belongs in Issues. New Issue
 * opens the Issues page and takes the request on to its chat; Stay here keeps
 * the user where they are, and nothing is sent. Once chosen, the card reads
 * as what was chosen, here and after a reload; a request already on the
 * Issues thread reads as continued wherever it was moved from.
 */
export function HandOffCard({ projectName, item }: { projectName: string; item: HandOffItem }) {
  const navigate = useNavigate();
  const chatPanel = useChatPanel();
  const issues = useProjectChat(projectName, "issues");
  const [chosen, setChosen] = useState<HandOffState>(() => getHandOff(projectName, item.toolCallId));
  const request = item.request.trim();
  const onIssuesThread = issues.items.some((i) => i.kind === "user" && i.text.trim() === request);
  const state: HandOffState = chosen === "pending" && onIssuesThread ? "continued" : chosen;

  const choose = (next: HandOffState) => {
    setHandOff(projectName, item.toolCallId, next);
    setChosen(next);
  };
  const openIssues = () => navigate({ to: "/projects/$projectName/issues", params: { projectName } });

  const newIssue = async () => {
    choose("continued");
    await openIssues();
    chatPanel.open();
    await continueInIssues(projectName, item.request, {
      store: chatStoreFor("issues"),
      compose: (text) => chatPanel.compose(text, { view: "issues", projectName }),
    });
  };

  if (state === "stayed") {
    return (
      <Typography variant="caption" color="text.secondary" sx={{ pl: 4 }}>
        Stayed here instead of opening Issues
      </Typography>
    );
  }
  if (state === "continued") {
    return (
      <Typography variant="caption" color="text.secondary" sx={{ pl: 4 }}>
        Continued in Issues ·{" "}
        <Link component="button" type="button" variant="caption" onClick={() => void openIssues()} sx={{ verticalAlign: "baseline" }}>
          Open
        </Link>
      </Typography>
    );
  }
  return (
    <Box sx={{ pl: 4, display: "flex", flexDirection: "column", gap: 0.75 }}>
      <Typography variant="body2">
        This belongs in <strong>Issues</strong>. I&apos;ll open it and draft the issue, in its own chat on top of this one.
      </Typography>
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.75 }}>
        <Button size="small" variant="contained" onClick={() => void newIssue()}>
          New Issue
        </Button>
        <Button size="small" variant="outlined" onClick={() => choose("stayed")}>
          Stay here
        </Button>
      </Box>
    </Box>
  );
}
