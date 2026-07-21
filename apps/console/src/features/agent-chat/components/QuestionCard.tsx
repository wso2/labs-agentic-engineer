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

// The ask_question card (ADR-0012 / #270): the agent's structured question
// rendered natively in the chat panel — radio/checkbox options with the
// recommended answer badged, an "Other…" free-text row, and a submit that
// serializes the choice into the next turn. Read-only once answered or
// superseded (a typed composer reply is an equally valid answer path).

import { useState } from "react";
import {
  alpha,
  Box,
  Button,
  Checkbox,
  Chip,
  FormControlLabel,
  Radio,
  Stack,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { CircleQuestionMark } from "@wso2/oxygen-ui-icons-react";
import type { ChatMessage } from "../chatStore";

export type QuestionMessage = Extract<ChatMessage, { role: "question" }>;

export function QuestionCard({
  msg,
  answerable,
  busy,
  onAnswer,
}: {
  msg: QuestionMessage;
  /** Derived from the log: unanswered AND not superseded by a later user message. */
  answerable: boolean;
  /** A turn is in flight — keep the card visible but hold submissions. */
  busy: boolean;
  onAnswer: (selected: string[], freeText?: string) => void;
}) {
  const [selected, setSelected] = useState<string[]>([]);
  const [freeText, setFreeText] = useState("");
  const multi = msg.multiSelect === true;
  // An answered card renders the recorded choice; a live one renders the draft.
  const chosen = msg.answer ? msg.answer.selected : selected;
  const readOnly = !answerable;

  const toggle = (label: string) => {
    if (readOnly || busy) return;
    setSelected((prev) =>
      multi
        ? prev.includes(label)
          ? prev.filter((l) => l !== label)
          : [...prev, label]
        : [label],
    );
  };

  const canSubmit = !readOnly && !busy && (selected.length > 0 || freeText.trim().length > 0);

  return (
    <Box
      data-testid="question-card"
      sx={{
        px: 1.5,
        py: 1.25,
        borderRadius: 2,
        border: 1,
        borderColor: readOnly ? "divider" : "primary.main",
        bgcolor: "background.paper",
        opacity: readOnly && !msg.answer ? 0.7 : 1,
      }}
    >
      <Stack direction="row" spacing={1} sx={{ alignItems: "flex-start", mb: 1 }}>
        <CircleQuestionMark size={16} style={{ marginTop: 2, flexShrink: 0 }} />
        <Typography variant="body2" sx={{ fontWeight: 600 }}>
          {msg.question}
        </Typography>
      </Stack>

      <Stack spacing={0.5}>
        {msg.options.map((opt) => {
          const isChosen = chosen.includes(opt.label);
          return (
            <Box
              key={opt.label}
              sx={{
                px: 1,
                borderRadius: 1.5,
                border: 1,
                borderColor: isChosen ? "primary.main" : "divider",
                bgcolor: isChosen
                  ? (theme) => alpha(theme.palette.primary.main, 0.08)
                  : "transparent",
              }}
            >
              <FormControlLabel
                disabled={readOnly || busy}
                sx={{ width: "100%", m: 0, py: 0.25 }}
                control={
                  multi ? (
                    <Checkbox size="small" checked={isChosen} onChange={() => toggle(opt.label)} />
                  ) : (
                    <Radio size="small" checked={isChosen} onChange={() => toggle(opt.label)} />
                  )
                }
                label={
                  <Stack sx={{ py: 0.5 }}>
                    <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
                      <Typography variant="body2">{opt.label}</Typography>
                      {opt.recommended && (
                        <Chip size="small" color="primary" variant="outlined" label="Recommended" />
                      )}
                    </Stack>
                    {opt.description && (
                      <Typography variant="caption" color="text.secondary">
                        {opt.description}
                      </Typography>
                    )}
                  </Stack>
                }
              />
            </Box>
          );
        })}
      </Stack>

      {msg.answer?.freeText ? (
        <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: "block" }}>
          Note: {msg.answer.freeText}
        </Typography>
      ) : (
        !readOnly && (
          <Stack direction="row" spacing={1} sx={{ mt: 1, alignItems: "center" }}>
            <TextField
              fullWidth
              size="small"
              placeholder={msg.options.length ? "Other / add a note…" : "Your answer…"}
              value={freeText}
              disabled={busy}
              onChange={(e) => setFreeText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && canSubmit) {
                  e.preventDefault();
                  onAnswer(selected, freeText);
                }
              }}
            />
            <Button
              size="small"
              variant="contained"
              disabled={!canSubmit}
              onClick={() => onAnswer(selected, freeText)}
            >
              Answer
            </Button>
          </Stack>
        )
      )}
    </Box>
  );
}
