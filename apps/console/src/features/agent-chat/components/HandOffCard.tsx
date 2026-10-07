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
import { Box, Button, Link, Typography } from "@wso2/oxygen-ui";
import { useChatPanel } from "../../shell/chatPanel";
import type { HandOffItem } from "../chatLog";
import { continueInIssues, getHandOff, issueReport, setHandOff, type HandOffState } from "../handOffState";
import { useOpenIssuesChat } from "../useOpenIssuesChat";
import { chatStoreFor, useProjectChat } from "../useProjectChat";

/** How much of the request the card shows before New Issue is chosen. */
const PREVIEW_CHARS = 200;

function preview(request: string): string {
  const words = request.trim();
  return words.length > PREVIEW_CHARS ? `${words.slice(0, PREVIEW_CHARS)}…` : words;
}

/**
 * The main chat's announcement that a request belongs in Issues. New Issue
 * opens the Issues page, starts its chat over the main one, and takes the
 * request on to it as an `/issue` report (`continueInIssues`); Stay here keeps the user where they are, and
 * nothing is sent. Once chosen, the card reads as what was chosen, here and
 * after a reload.
 */
export function HandOffCard({ projectName, item }: { projectName: string; item: HandOffItem }) {
  const [chosen, setChosen] = useState<HandOffState>(() => getHandOff(projectName, item.toolCallId));
  const choose = (next: HandOffState) => {
    setHandOff(projectName, item.toolCallId, next);
    setChosen(next);
  };
  const openIssuesChat = useOpenIssuesChat(projectName);

  if (chosen === "stayed") {
    return (
      <Typography variant="caption" color="text.secondary" sx={{ pl: 4 }}>
        Stayed here instead of opening Issues
      </Typography>
    );
  }
  if (chosen === "continued") return <Continued onOpen={() => void openIssuesChat()} />;
  return <Pending projectName={projectName} item={item} openIssuesChat={openIssuesChat} choose={choose} />;
}

function Continued({ onOpen }: { onOpen: () => void }) {
  return (
    <Typography variant="caption" color="text.secondary" sx={{ pl: 4 }}>
      Continued in Issues ·{" "}
      <Link component="button" type="button" variant="caption" onClick={onOpen} sx={{ verticalAlign: "baseline" }}>
        Open
      </Link>
    </Typography>
  );
}

/**
 * The choice still to make. Only an undecided card watches the Issues chat:
 * once its thread carries the report, the request went on from somewhere (a
 * teammate, another tab) and the card reads as continued.
 */
function Pending({
  projectName,
  item,
  openIssuesChat,
  choose,
}: {
  projectName: string;
  item: HandOffItem;
  openIssuesChat: () => Promise<void>;
  choose: (next: HandOffState) => void;
}) {
  const chatPanel = useChatPanel();
  const issues = useProjectChat(projectName, "issues");
  const [moving, setMoving] = useState(false);
  const report = issueReport(item.request);
  if (issues.items.some((i) => i.kind === "user" && i.text.trim() === report.trim())) {
    return <Continued onOpen={() => void openIssuesChat()} />;
  }

  const newIssue = async () => {
    setMoving(true);
    try {
      // The Issues chat comes up over the main one, so the request is seen
      // going there (sent, or waiting in its composer).
      await openIssuesChat();
    } catch {
      // The move did not happen: nothing was sent, and the choice is offered again.
      setMoving(false);
      return;
    }
    choose("continued");
    chatPanel.open();
    await continueInIssues(projectName, item.request, {
      store: chatStoreFor("issues"),
      compose: (text) => chatPanel.compose(text, { view: "issues", projectName }),
    });
  };

  return (
    <Box sx={{ pl: 4, display: "flex", flexDirection: "column", gap: 0.75 }}>
      <Typography variant="body2">
        This belongs in <strong>Issues</strong>. I&apos;ll open it and draft the issue, in its own chat on top of this one.
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
        {`I'll pass on: “${preview(item.request)}”`}
      </Typography>
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.75 }}>
        <Button size="small" variant="contained" disabled={moving} onClick={() => void newIssue()}>
          New Issue
        </Button>
        <Button size="small" variant="outlined" disabled={moving} onClick={() => choose("stayed")}>
          Stay here
        </Button>
      </Box>
    </Box>
  );
}
