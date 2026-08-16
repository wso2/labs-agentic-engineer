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

/**
 * The conversational-section driver: requirements and design run through the
 * SAME loop, differing only in first instruction — production-parity bytes via
 * the playground waist (`runSpecTurn`), the sim user answering `ask_question`
 * pauses through the production `buildAnswer(s)Instruction` channel. Every
 * StreamPart is kept (tracing decision on ticket #353).
 */

import { runSpecTurn } from "@aep/playground/src/engine/turn.js";
import { chatSpec } from "@aep/playground/src/engine/turn-spec.js";
import type { TurnSpec } from "@aep/agent-stream";
import { pendingQuestions } from "@aep/playground/src/engine/questions.js";
import type { PlaygroundSession } from "@aep/playground/src/engine/session.js";
import { buildAnswerInstruction, buildAnswersInstruction } from "@aep/agent-stream";
import { DEFAULT_MAX_TURNS } from "../config.js";
import type { ScenarioBrief } from "../scenario.js";
import { simAnswers, type SimAnswer } from "../sim-user.js";
import { collectPart, newTurnRecord, reportTurnTrace, type TurnRecord } from "../tracing.js";

export interface SectionRunResult {
  section: string;
  records: TurnRecord[];
  questionsAsked: number;
  /** The section reached a natural end (not the turn cap, not an error). */
  finishedInterview: boolean;
  answers: SimAnswer[];
  error?: string;
}

/** A short human label for the trace record — never what is sent to the model. */
function turnLabel(turn: TurnSpec): string {
  switch (turn.kind) {
    case "chat":
      return turn.text;
    case "flow":
      return turn.text ? `/${turn.skill} ${turn.text}` : `/${turn.skill}`;
    case "start":
      return turn.idea ? `/start ${turn.idea}` : "/start";
    case "plan":
      return "task-plan (one-shot)";
  }
}

/**
 * Run one HITL section to completion inside an open session. The fatigue
 * counter starts at zero — per-section reset (#357).
 */
export async function runConversationalSection(
  session: PlaygroundSession,
  section: "requirements" | "design",
  firstTurn: TurnSpec,
  brief: ScenarioBrief,
): Promise<SectionRunResult> {
  const maxTurns = brief.maxTurns ?? DEFAULT_MAX_TURNS;
  const records: TurnRecord[] = [];
  const answers: SimAnswer[] = [];
  let questionsAsked = 0;
  let finishedInterview = false;
  let error: string | undefined;

  let turn = firstTurn;
  let label = turnLabel(firstTurn);
  while (records.length < maxTurns) {
    const rec = newTurnRecord(section, records.length + 1, label);
    const start = Date.now();
    const result = await runSpecTurn(session, turn, {
      onPart: (part) => collectPart(rec, part),
    });
    rec.ms = Date.now() - start;
    records.push(rec);

    if (result.error) {
      error = result.error;
      reportTurnTrace(rec, start);
      break;
    }

    const pending = pendingQuestions(result.toolCalls);
    if (!pending) {
      finishedInterview = true;
      reportTurnTrace(rec, start);
      break;
    }

    rec.questions = pending.questions.map((q) => q.question);
    if (pending.session) rec.session = pending.session;
    const batch = await simAnswers(brief, pending.questions, questionsAsked);
    questionsAsked += pending.questions.length;
    rec.answers = batch;
    answers.push(...batch);
    reportTurnTrace(rec, start);

    // An answer is ordinary chat, whatever kind of turn asked the question.
    label = pending.batch
      ? buildAnswersInstruction(batch)
      : buildAnswerInstruction(batch[0]!.question, batch[0]!.selected, batch[0]!.freeText);
    turn = chatSpec(label);
  }

  return {
    section,
    records,
    questionsAsked,
    finishedInterview,
    answers,
    ...(error ? { error } : {}),
  };
}
