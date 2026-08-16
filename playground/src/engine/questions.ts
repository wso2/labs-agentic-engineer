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
 * The playground half of the HITL question loop (console ADR-0012 / #270). When
 * a turn ends on an `ask_question` / `ask_questions` tool-call the agents
 * service leaves the conversation `awaiting-human`; the chat screen detects that
 * here, prompts the user in the terminal (`tui/questions.ts`), and sends the
 * answer back as the NEXT turn's plain instruction via the wire serializers
 * (`buildAnswerInstruction` / `buildAnswersInstruction`) — exactly the console's
 * flow, no new channel.
 */

import {
  ASK_QUESTION_TOOL,
  ASK_QUESTIONS_TOOL,
  type AskQuestionInput,
  type AskQuestionsInput,
  type QuestionSessionInfo,
  type StreamPart,
} from "@aep/agent-stream";

/** The structured questions a turn ended on (HITL). */
export interface PendingQuestions {
  /** `ask_questions` (a form) vs a single `ask_question` — decides how the answer serializes. */
  batch: boolean;
  /** The questions to render; a single `ask_question` is a one-element list. */
  questions: AskQuestionInput[];
  /**
   * The grilling session this round belongs to (#486), when the agent sent one.
   * Absent on an ordinary one-form interview — and its absence is exactly how a
   * session that failed to open is spotted, here and in the eval transcripts.
   */
  session?: QuestionSessionInfo;
}

/**
 * Detect an awaiting-human turn from its streamed tool-calls: the LAST question
 * tool-call wins (a turn ends at the first such call, so there is normally one).
 * Returns undefined when the turn didn't end on a question — the caller then
 * proceeds as an ordinary chat turn.
 */
export function pendingQuestions(toolCalls: StreamPart[]): PendingQuestions | undefined {
  for (let i = toolCalls.length - 1; i >= 0; i--) {
    const tc = toolCalls[i];
    if (!tc || tc.type !== "tool-call") continue;
    if (tc.toolName === ASK_QUESTION_TOOL) {
      return { batch: false, questions: [tc.input as AskQuestionInput] };
    }
    if (tc.toolName === ASK_QUESTIONS_TOOL) {
      const input = tc.input as AskQuestionsInput;
      // The checklist is chrome: a round whose `session` is malformed still has
      // its questions answered, exactly as the console treats it.
      const session = Array.isArray(input.session?.areas) ? input.session : undefined;
      return { batch: true, questions: input.questions, ...(session ? { session } : {}) };
    }
  }
  return undefined;
}
