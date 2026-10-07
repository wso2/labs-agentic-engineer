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

import type { QuestionAnswer } from "@aep/agent-stream";

// The answers given on the Questions card and not yet sent, per batch, for
// as long as the page lives: closing the card and opening it again finds them
// where they were; a reload starts over, and a send that went through clears
// them (ADR-0002). Keyed by the batch's tool call, which names one ask across
// conversations, not by its place in the log: a replaced conversation can put
// another batch at the same place. Each user's own: nothing here is shared.

const drafts = new Map<string, QuestionAnswer[]>();

const key = (projectName: string, toolCallId: string) => `${projectName}\u0000${toolCallId}`;

export function questionDraft(projectName: string, toolCallId: string): QuestionAnswer[] {
  return drafts.get(key(projectName, toolCallId)) ?? [];
}

export function saveQuestionDraft(projectName: string, toolCallId: string, answers: QuestionAnswer[]): void {
  drafts.set(key(projectName, toolCallId), answers);
}

export function clearQuestionDraft(projectName: string, toolCallId: string): void {
  drafts.delete(key(projectName, toolCallId));
}
