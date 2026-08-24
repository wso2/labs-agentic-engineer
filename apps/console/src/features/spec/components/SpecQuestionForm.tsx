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

// Collab question FORM (2026-07-23 spike): EVERY agent question — one or many —
// is answered here. The chat panel only points at it and the spec body is taken
// over by this full-panel form, so questions always appear in one consistent
// place instead of splitting between two surfaces.
//
// The form is driven by the room's shared Yjs `questions` map, so EVERY collab
// participant sees the questions and each other's selections live — and ANY
// member submits, or hands the questions back for the agent's own recommended
// answers (#430 D5). The conversation is one shared project
// thread, so form-submit and a composer reply are the same act; the answer
// travels through the submitter's pendingSeed exactly like their typed reply
// would.

import { useRef } from "react";
import { Box, Button, Checkbox, Chip, CircularProgress, Radio, Stack, TextField, Typography } from "@wso2/oxygen-ui";
import { Sparkles, Users } from "@wso2/oxygen-ui-icons-react";
import type { Doc } from "yjs";
import type { AskQuestionInput, QuestionAnswer } from "@aep/agent-stream";
import {
  applyNote,
  applySelection,
  isFreeTextOption,
  isQuestionAnswered,
  normalizeAnswers,
  serializeQuestionAnswer,
} from "../../agent-chat/questionCards";
import { chatKeyFor, setPendingSeed } from "../../agent-chat/chatStore";
import {
  closeRoomQuestion,
  updateRoomAnswer,
  type RoomQuestion,
} from "../../agent-chat/questionRoom";

/**
 * One selectable option card: radio/checkbox, label, "Recommended" badge, and
 * the option's full description — always visible, so the user can weigh the
 * choices without hunting through tooltips.
 */
function OptionCard({
  opt,
  multi,
  isOn,
  disabled,
  onSelect,
}: {
  opt: AskQuestionInput["options"][number];
  multi: boolean;
  isOn: boolean;
  disabled: boolean;
  onSelect: () => void;
}) {
  const Control = multi ? Checkbox : Radio;
  return (
    <Box
      role={multi ? "checkbox" : "radio"}
      aria-checked={isOn}
      tabIndex={disabled ? -1 : 0}
      onClick={disabled ? undefined : onSelect}
      onKeyDown={
        disabled
          ? undefined
          : (e) => {
              if (e.key === " " || e.key === "Enter") {
                e.preventDefault();
                onSelect();
              }
            }
      }
      sx={{
        display: "flex",
        alignItems: "flex-start",
        gap: 1,
        p: 2,
        border: 1,
        borderColor: isOn ? "primary.main" : "divider",
        borderRadius: 2,
        bgcolor: isOn ? "action.selected" : "background.paper",
        cursor: disabled ? "default" : "pointer",
        transition: "border-color 120ms, background-color 120ms",
        "&:hover": disabled ? undefined : { borderColor: "primary.main" },
      }}
    >
      <Control size="small" checked={isOn} disabled={disabled} disableRipple sx={{ p: 0, mt: 0.25 }} />
      <Box sx={{ minWidth: 0 }}>
        <Stack direction="row" spacing={1} useFlexGap sx={{ alignItems: "center", flexWrap: "wrap" }}>
          <Typography variant="body1" sx={{ fontWeight: 600 }}>
            {opt.label}
          </Typography>
          {opt.recommended && (
            <Chip label="Recommended" size="small" color="primary" variant="outlined" sx={{ height: 20 }} />
          )}
        </Stack>
        {opt.description && (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            {opt.description}
          </Typography>
        )}
      </Box>
    </Box>
  );
}

/** One question block: heading, context detail, option cards, and an "Other…" free-text row. */
function QuestionBlock({
  q,
  answer,
  disabled,
  onSelect,
  onNote,
}: {
  q: AskQuestionInput;
  answer: QuestionAnswer | undefined;
  disabled: boolean;
  onSelect: (label: string) => void;
  onNote: (note: string) => void;
}) {
  const multi = q.multiSelect === true;
  const selected = answer?.selected ?? [];
  // A question with NO options is a free-text question: only the text field
  // renders — no radio that selects nothing. A question with ONE option is
  // (in practice) an agent-invented "type my own answer" card: keep it
  // selectable, but checking it moves the caret straight into the text field.
  const freeTextOnly = q.options.length === 0;
  // A selected escape-hatch option ("Something else", "Other", explicit
  // freeText flag) means the REAL answer is the text — surface that: the
  // field grows, gains a helper line, and submit stays gated until typed.
  const needsText =
    !freeTextOnly &&
    selected.some((label) => {
      const opt = q.options.find((o) => o.label === label);
      return opt !== undefined && isFreeTextOption(opt);
    }) &&
    (answer?.freeText ?? "").trim() === "";
  const noteRef = useRef<HTMLTextAreaElement | null>(null);
  return (
    <Box sx={{ mb: 5 }}>
      <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
        {q.question}
      </Typography>
      {q.detail && (
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
          {q.detail}
        </Typography>
      )}
      {multi && (
        <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: 0.5 }}>
          Pick as many as apply
        </Typography>
      )}
      <Stack
        spacing={1.5}
        {...(freeTextOnly ? {} : { role: multi ? "group" : "radiogroup", "aria-label": q.question })}
        sx={{ mt: 1.5 }}
      >
        {q.options.map((opt) => (
          <OptionCard
            key={opt.label}
            opt={opt}
            multi={multi}
            isOn={selected.includes(opt.label)}
            disabled={disabled}
            onSelect={() => {
              const turningOn = !selected.includes(opt.label);
              onSelect(opt.label);
              // Checking an option whose real input is the text (an "Other" /
              // "Something else" escape hatch, or a lone option) moves the
              // caret straight into the field.
              if (turningOn && (isFreeTextOption(opt) || q.options.length === 1)) {
                noteRef.current?.focus();
              }
            }}
          />
        ))}
        <TextField
          size="small"
          label={freeTextOnly ? "Your answer" : "Other — your own answer or extra context"}
          placeholder={freeTextOnly || needsText ? "Type your answer…" : "Add a note…"}
          value={answer?.freeText ?? ""}
          disabled={disabled}
          multiline
          {...(freeTextOnly || needsText ? { minRows: 2 } : {})}
          {...(needsText
            ? { helperText: "This choice needs a typed answer — describe it here to continue." }
            : {})}
          inputRef={noteRef}
          onChange={(e) => onNote(e.target.value)}
          sx={{
            "& .MuiOutlinedInput-root": { borderRadius: 2 },
            ...(needsText ? { "& .MuiOutlinedInput-notchedOutline": { borderColor: "primary.main" } } : {}),
          }}
        />
      </Stack>
    </Box>
  );
}

export function SpecQuestionForm({
  doc,
  entry,
  org,
  projectName,
}: {
  /** The live room Y.Doc — selections write straight into its shared map. */
  doc: Doc;
  /** The pending question entry from the room (one question or many). */
  entry: RoomQuestion;
  org: string;
  projectName: string;
}) {
  // Re-aligned to the CURRENT question count on every render: while the batch
  // streams the stored array is shorter than the list, and editing through a
  // short array drops writes for the later questions (#335).
  const answers: QuestionAnswer[] = normalizeAnswers(entry.questions, entry.answers);

  // Edits resolve against the LIVE room entry (never this render's snapshot):
  // successive clicks between renders must not clobber each other (#335).
  const select = (qi: number, label: string) => {
    updateRoomAnswer(doc, entry.toolCallId, (live) =>
      applySelection(live.questions, live.answers, qi, label),
    );
  };

  const note = (qi: number, freeText: string) => {
    updateRoomAnswer(doc, entry.toolCallId, (live) => applyNote(live.questions, live.answers, qi, freeText));
  };

  const allAnswered = entry.questions.every((q, i) => isQuestionAnswered(q, answers[i]));
  // While the batch is still streaming (#270 latency), the form is readable and
  // selectable but cannot submit or skip: the turn is still running, and more
  // questions may yet arrive. The final mirror clears the gate.
  const streaming = entry.streaming === true;
  const canSubmit = allAnswered && !streaming;

  const submit = () => {
    if (!canSubmit) return;
    const cleaned = answers.map((a) => ({
      selected: a.selected,
      ...(a.freeText?.trim() ? { freeText: a.freeText.trim() } : {}),
    }));
    setPendingSeed(chatKeyFor(org, projectName), serializeQuestionAnswer(entry.questions, cleaned));
    closeRoomQuestion(doc, entry.toolCallId);
  };

  // The other exit: close the form for the WHOLE room and hand the questions
  // back to the agent to answer itself — otherwise the turn sits waiting on an
  // answer that isn't coming. Any member, same as submit (#430 D5).
  //
  // It is NOT "stop interviewing" (#578): the interview is uncapped, so this
  // defers THESE questions rather than ending the conversation. What it asks
  // for is the flag — an assumption the user can see in the document is a
  // decision they can overturn; a silent one is an invention.
  //
  // It hands back only what is still UNANSWERED. The caption invites partial
  // use — answer the ones you have opinions about, let the agent take the rest
  // — and a room can have co-authored several answers already; sending them all
  // back would overwrite real decisions with guesses and flag them `*assumed*`,
  // which reads as the agent's invention rather than the user's overwritten
  // answer. The `grilling` skill sets the same bound: "apply your recommended
  // answer to every REMAINING decision".
  const useRecommended = () => {
    const answered = entry.questions.filter((q, i) => isQuestionAnswered(q, answers[i]));
    const remaining = entry.questions.filter((q, i) => !isQuestionAnswered(q, answers[i]));
    // Nothing left to hand back is the Continue case exactly: asking the agent
    // to "decide the rest" of an empty set invites it to invent decisions and
    // flag them `*assumed*`, which is the one thing this exit must never cause.
    const ask =
      remaining.length === 0
        ? ""
        : remaining.length === entry.questions.length
          ? "Use your recommended answers for these questions — decide them yourself and flag each one as assumed where it lands in the document, so I can change it later."
          : `Decide the rest yourself using your recommended answers, and flag each one as assumed where it lands in the document, so I can change it later:\n` +
            remaining.map((q) => `- "${q.question}"`).join("\n");
    // The answered half rides first, serialized exactly as Continue would send
    // it, so the agent reads one message carrying both decisions and deferrals.
    const decided = answered.length
      ? serializeQuestionAnswer(
          answered,
          entry.questions
            .map((q, i) => ({ q, a: answers[i] }))
            .filter(({ q, a }) => isQuestionAnswered(q, a))
            .map(({ a }) => ({
              selected: a?.selected ?? [],
              ...(a?.freeText?.trim() ? { freeText: a.freeText.trim() } : {}),
            })),
        ) + "\n\n"
      : "";
    setPendingSeed(chatKeyFor(org, projectName), (decided + ask).trimEnd());
    closeRoomQuestion(doc, entry.toolCallId);
  };

  return (
    <Box
      data-testid="spec-question-form"
      sx={{ flexGrow: 1, minWidth: 0, minHeight: 0, display: "flex", flexDirection: "column" }}
    >
      {/* Scrollable question list — a long interview scrolls here, the footer stays put. */}
      <Box sx={{ flexGrow: 1, overflowY: "auto", px: 5, py: 4 }}>
        <Box sx={{ maxWidth: 820, mx: "auto" }}>
          <Stack direction="row" spacing={1} sx={{ alignItems: "center", mb: 0.5 }}>
            <Sparkles size={18} color="var(--oxygen-palette-primary-main, currentColor)" />
            <Typography variant="h5" sx={{ fontWeight: 700 }}>
              Quick questions
            </Typography>
          </Stack>
          <Stack direction="row" spacing={0.75} sx={{ alignItems: "center", mb: 4 }}>
            <Users size={14} />
            <Typography variant="body2" color="text.secondary">
              Everyone on this project can answer together — anyone can send the
              answers.
            </Typography>
          </Stack>

          {entry.questions.map((q, qi) => (
            <QuestionBlock
              key={qi}
              q={q}
              answer={answers[qi]}
              disabled={false}
              onSelect={(label) => select(qi, label)}
              onNote={(text) => note(qi, text)}
            />
          ))}
          {streaming && (
            <Stack direction="row" spacing={1} sx={{ alignItems: "center", mb: 4 }}>
              <CircularProgress size={14} aria-label="More questions arriving" />
              <Typography variant="body2" color="text.secondary">
                The agent is still writing questions — you can start answering.
              </Typography>
            </Stack>
          )}
        </Box>
      </Box>

      {/* Sticky footer — Continue sits bottom-right, open to any member. The
          caption is the other exit's meaning, stated before it is clicked:
          the control decides nothing away, it hands the decision to the agent
          and keeps it flagged. */}
      <Stack
        direction="row"
        spacing={2}
        sx={{
          alignItems: "center",
          justifyContent: "space-between",
          px: 5,
          py: 2,
          borderTop: 1,
          borderColor: "divider",
          flexShrink: 0,
        }}
      >
        <Typography variant="caption" color="text.secondary" sx={{ minWidth: 0 }}>
          The agent can answer these itself — each decision lands flagged <em>assumed</em> in your
          spec, so you can change it later.
        </Typography>
        <Stack direction="row" spacing={2} sx={{ alignItems: "center", flexShrink: 0 }}>
          <Button variant="text" color="inherit" disabled={streaming} onClick={useRecommended}>
            Use recommended answers
          </Button>
          <Button variant="contained" disabled={!canSubmit} onClick={submit}>
            Continue
          </Button>
        </Stack>
      </Stack>
    </Box>
  );
}
