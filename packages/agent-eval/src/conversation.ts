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

import { simAnswer } from "./sim-user.js";
import type { Scenario } from "./scenario.js";

export interface Message {
  role: "user" | "assistant";
  content: string;
}
export type AskFn = (messages: Message[]) => Promise<string>;
export interface Transcript {
  text: string;
  turns: number;
  messages: Message[];
}

const DEFAULT_MAX_TURNS = 6;

/**
 * One scenario, start to finish.
 *
 * `ask` is injected rather than built here so this file has no opinion about
 * HOW the agent is reached — in a build it is an in-process handler; in a test
 * it is a function. That seam is what lets the conversation logic be tested
 * without a model.
 */
export async function runConversation(opts: {
  scenario: Scenario;
  ask: AskFn;
  maxTurns?: number;
}): Promise<Transcript> {
  const max = opts.maxTurns ?? DEFAULT_MAX_TURNS;
  const messages: Message[] = [];
  let agentSaid = "";
  let turns = 0;

  for (let i = 0; i < max; i++) {
    const userSays = simAnswer(opts.scenario.brief, agentSaid, i);
    messages.push({ role: "user", content: userSays });
    agentSaid = await opts.ask(messages);
    messages.push({ role: "assistant", content: agentSaid });
    turns = i + 1;
    if (i > 0 && /that's all, thanks/i.test(userSays)) break;
  }

  const text = messages
    .map((m) => `${m.role === "user" ? "User" : "Agent"}: ${m.content}`)
    .join("\n");
  return { text, turns, messages };
}
