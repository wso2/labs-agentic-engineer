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
import { Box, ButtonBase, Typography } from "@wso2/oxygen-ui";
import { CircleQuestionMark } from "@wso2/oxygen-ui-icons-react";
import type { QuestionItem } from "../chatLog";

// The agent's questions, as the chat shows them (ADR-0002 / #879): every
// question is answered on the Questions card, so while one is open the chat
// only points there, and the click is what opens it; nothing moves the user
// by itself. Once answered, or superseded by a message, the questions fold to
// their texts, and the answer reads as the user's message below them.

export function QuestionsPointer({
  projectName,
  item,
  open,
}: {
  projectName: string;
  item: QuestionItem;
  /** The conversation is waiting on these questions (or still being asked them). */
  open: boolean;
}) {
  const navigate = useNavigate();

  if (!open || item.answers) {
    return (
      <Box sx={{ borderLeft: 2, borderColor: "divider", pl: 1.25, display: "flex", flexDirection: "column", gap: 0.25 }}>
        {item.questions.map((q, i) => (
          <Typography key={i} variant="caption" color="text.secondary" component="p">
            {q.question}
          </Typography>
        ))}
      </Box>
    );
  }

  const one = item.questions.length === 1;
  const said = item.streaming
    ? "The agent is asking questions…"
    : one
      ? "The agent has a question"
      : `The agent has ${item.questions.length} questions`;
  return (
    <ButtonBase
      onClick={() => void navigate({ to: "/projects/$projectName/questions", params: { projectName } })}
      sx={{
        width: "100%",
        display: "flex",
        alignItems: "center",
        gap: 1,
        textAlign: "start",
        px: 1.5,
        py: 1,
        border: 1,
        borderColor: "primary.main",
        borderRadius: 2.5,
        bgcolor: "rgba(var(--oxygen-palette-primary-mainChannel) / 0.06)",
        "&:hover": { bgcolor: "rgba(var(--oxygen-palette-primary-mainChannel) / 0.12)" },
      }}
    >
      <Box component="span" sx={{ color: "primary.main", display: "inline-flex" }}>
        <CircleQuestionMark size={18} aria-hidden />
      </Box>
      <Typography variant="body2" sx={{ fontWeight: 600, flex: 1 }}>
        {said}
      </Typography>
      <Typography variant="body2" sx={{ color: "primary.main", flexShrink: 0 }}>
        {one && !item.streaming ? "Answer it →" : "Answer them →"}
      </Typography>
    </ButtonBase>
  );
}
