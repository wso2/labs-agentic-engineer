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
import { Box, Button, Stack, Typography } from "@wso2/oxygen-ui";
import { Sparkles } from "@wso2/oxygen-ui-icons-react";
import type { AskQuestionInput, QuestionAnswer } from "@aep/agent-stream";
import { chatKeyFor, setPendingSeed } from "../../agent-chat/chatStore";
import {
  applyNote,
  applySelection,
  isQuestionAnswered,
  normalizeAnswers,
  serializeQuestionAnswer,
} from "../../agent-chat/questionCards";
import { QuestionBlock } from "./SpecQuestionForm";

/**
 * HITL questions for a surface that is NOT a spec room (Marketplace register).
 * Same widgets as SpecQuestionForm; answers travel as the next chat turn
 * (ADR-0012 pendingSeed). No Yjs, no collab caption, no "use recommended"
 * (that path flags *assumed* in a spec document).
 */
export function ChatQuestionForm({
  org,
  projectName,
  questions,
  streaming = false,
  submitting = false,
  onSubmitted,
}: {
  org: string;
  projectName: string;
  questions: AskQuestionInput[];
  streaming?: boolean;
  /** Answers are in flight — keep this pane, freeze the widgets. */
  submitting?: boolean;
  onSubmitted?: () => void;
}) {
  const [answers, setAnswers] = useState<QuestionAnswer[]>(() =>
    normalizeAnswers(questions, null),
  );
  const aligned = normalizeAnswers(questions, answers);
  const allAnswered = questions.every((q, i) => isQuestionAnswered(q, aligned[i]));
  const frozen = streaming || submitting;
  const canSubmit = allAnswered && !frozen;

  const submit = () => {
    if (!canSubmit) return;
    const cleaned = aligned.map((a) => ({
      selected: a.selected,
      ...(a.freeText?.trim() ? { freeText: a.freeText.trim() } : {}),
    }));
    onSubmitted?.();
    setPendingSeed(chatKeyFor(org, projectName), serializeQuestionAnswer(questions, cleaned));
  };

  const skip = () => {
    if (frozen) return;
    onSubmitted?.();
    setPendingSeed(
      chatKeyFor(org, projectName),
      "Skip these questions — stop interviewing and proceed with your best assumptions, stating them.",
    );
  };

  return (
    <Box
      data-testid="chat-question-form"
      sx={{ flexGrow: 1, minWidth: 0, minHeight: 0, display: "flex", flexDirection: "column" }}
    >
      <Box sx={{ flexGrow: 1, overflowY: "auto", px: 1, py: 1 }}>
        <Box sx={{ maxWidth: 820 }}>
          <Stack direction="row" spacing={1} sx={{ alignItems: "center", mb: 0.5 }}>
            <Sparkles size={18} color="var(--oxygen-palette-primary-main, currentColor)" />
            <Typography variant="h5" sx={{ fontWeight: 700 }}>
              Quick questions
            </Typography>
          </Stack>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 4 }}>
            {submitting
              ? "Drafting the catalog form from your answers."
              : "Answer so the agent can draft the catalog form. Environment values stay on the form, never in chat."}
          </Typography>
          {questions.map((q, qi) => (
            <QuestionBlock
              key={qi}
              q={q}
              answer={aligned[qi]}
              disabled={frozen}
              onSelect={(label) => setAnswers(applySelection(questions, aligned, qi, label))}
              onNote={(text) => setAnswers(applyNote(questions, aligned, qi, text))}
            />
          ))}
        </Box>
      </Box>
      <Stack
        direction="row"
        spacing={2}
        sx={{
          alignItems: "center",
          justifyContent: "flex-end",
          py: 2,
          borderTop: 1,
          borderColor: "divider",
          flexShrink: 0,
        }}
      >
        <Button variant="text" color="inherit" disabled={frozen} onClick={skip}>
          Skip
        </Button>
        <Button
          variant="contained"
          disabled={!canSubmit}
          loading={submitting}
          onClick={submit}
        >
          {submitting ? "Working…" : "Continue"}
        </Button>
      </Stack>
    </Box>
  );
}
