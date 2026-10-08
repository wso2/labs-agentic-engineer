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

import { useRef } from "react";
import { Box, ButtonBase, Checkbox, Chip, InputBase, Radio, Typography } from "@wso2/oxygen-ui";
import type { AskQuestionInput, AskQuestionOption, QuestionAnswer } from "@aep/agent-stream";
import { isFreeTextOption } from "../questionCards";

// One of the agent's questions: its options, and a free answer in the user's
// own words. Widgets after the old console's SpecQuestionForm (QuestionBlock,
// OptionCard). The Questions card lists one per question the agent asked.

/**
 * One option as a card, after the classic console's OptionCard: a radio (or a
 * checkbox, when several may be picked), the label, a Recommended chip, and
 * the description, always shown so the choices can be weighed side by side.
 * The whole card is the control; the radio or checkbox in it only shows the
 * state, so assistive tech reads the card once. A div, not a button: a button
 * may not hold the input.
 */
function OptionButton({
  opt,
  multi,
  on,
  disabled,
  onSelect,
}: {
  opt: AskQuestionOption;
  multi: boolean;
  on: boolean;
  disabled: boolean;
  onSelect: () => void;
}) {
  const Control = multi ? Checkbox : Radio;
  return (
    <ButtonBase
      component="div"
      role={multi ? "checkbox" : "radio"}
      aria-checked={on}
      disabled={disabled}
      onClick={onSelect}
      sx={{
        width: "100%",
        display: "flex",
        alignItems: "flex-start",
        justifyContent: "flex-start",
        gap: 1,
        textAlign: "start",
        px: 1.25,
        py: 1,
        border: 1,
        borderRadius: 2,
        borderColor: on ? "primary.main" : "divider",
        bgcolor: on ? "rgba(var(--oxygen-palette-primary-mainChannel) / 0.08)" : "transparent",
        "&:hover": { borderColor: "primary.main" },
      }}
    >
      <Control
        size="small"
        checked={on}
        disabled={disabled}
        disableRipple
        tabIndex={-1}
        slotProps={{ input: { "aria-hidden": true, tabIndex: -1 } }}
        sx={{ p: 0, mt: 0.125, pointerEvents: "none" }}
      />
      <Box sx={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column", gap: 0.25 }}>
        <Box sx={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: 0.75 }}>
          <Typography variant="body2" sx={{ fontWeight: 600 }}>
            {opt.label}
          </Typography>
          {opt.recommended && (
            <Chip label="Recommended" size="small" color="primary" variant="outlined" sx={{ height: 20 }} />
          )}
        </Box>
        {opt.description && (
          <Typography variant="caption" color="text.secondary">
            {opt.description}
          </Typography>
        )}
      </Box>
    </ButtonBase>
  );
}

export function QuestionBlock({
  q,
  answer,
  disabled,
  errorId,
  onSelect,
  onNote,
}: {
  q: AskQuestionInput;
  answer: QuestionAnswer;
  disabled: boolean;
  /**
   * The id of the text saying this question still owes an answer, while it
   * does: its controls are marked invalid and point at that text.
   */
  errorId?: string | undefined;
  onSelect: (label: string) => void;
  onNote: (text: string) => void;
}) {
  const note = useRef<HTMLTextAreaElement | null>(null);
  const invalid = errorId ? { "aria-invalid": true, "aria-errormessage": errorId } : {};
  const multi = q.multiSelect === true;
  const freeOnly = q.options.length === 0;
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
      <Typography variant="body2" sx={{ fontWeight: 600 }}>
        {q.question}
      </Typography>
      {q.detail && (
        <Typography variant="caption" color="text.secondary">
          {q.detail}
        </Typography>
      )}
      {!freeOnly && (
        <Box
          role={multi ? "group" : "radiogroup"}
          aria-label={q.question}
          {...invalid}
          sx={{ display: "flex", flexDirection: "column", gap: 0.625 }}
        >
          {q.options.map((opt) => (
            <OptionButton
              key={opt.label}
              opt={opt}
              multi={multi}
              on={answer.selected.includes(opt.label)}
              disabled={disabled}
              onSelect={() => {
                const turningOn = !answer.selected.includes(opt.label);
                onSelect(opt.label);
                // An "Other" option's real answer is the text: go there.
                if (turningOn && isFreeTextOption(opt)) note.current?.focus();
              }}
            />
          ))}
        </Box>
      )}
      <InputBase
        multiline
        maxRows={4}
        disabled={disabled}
        value={answer.freeText ?? ""}
        onChange={(e) => onNote(e.target.value)}
        inputRef={note}
        placeholder={freeOnly ? "Your answer…" : "Or answer in your own words…"}
        inputProps={{ "aria-label": freeOnly ? q.question : `Your own answer to: ${q.question}`, ...invalid }}
        sx={{
          fontSize: "0.8125rem",
          border: 1,
          borderColor: "divider",
          borderRadius: 2,
          px: 1.25,
          py: 0.625,
          bgcolor: "background.paper",
        }}
      />
    </Box>
  );
}
