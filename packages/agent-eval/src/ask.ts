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

import type { AskFn, Message } from "./conversation.js";

/** Every scenario runs as one caller; the agent scopes its memory by this. */
const EVAL_USER_ID = "agent-eval";

interface ChatReply {
  conversationId?: unknown;
  text?: unknown;
}

/**
 * The conversation loop's `ask`, speaking the agent's real `/chat` contract.
 *
 * `POST /chat` takes `{ conversationId?, message }` and answers
 * `{ conversationId, text, toolCalls }`. History lives in the agent's own
 * store, so only the newest user message goes on the wire — and the id
 * returned by the first turn is threaded through every later turn, which is
 * exactly what makes "a second turn must remember" observable.
 *
 * ONE conversation per returned function: each scenario builds its own `ask`,
 * so no scenario can inherit — or be blamed for — another's history.
 *
 * Every failure raises. Turning a 500 or a body that is not the contract into
 * an empty answer would put a blank line in front of the grader and score an
 * outage as bad behaviour.
 */
export function askViaHttp(baseUrl: string, userId: string = EVAL_USER_ID): AskFn {
  let conversationId: string | undefined;

  return async (messages: Message[]): Promise<string> => {
    const last = messages[messages.length - 1];
    if (last === undefined || last.role !== "user") {
      throw new Error("askViaHttp: the last message must be the user's turn");
    }

    const res = await fetch(new URL("/chat", baseUrl), {
      method: "POST",
      headers: { "content-type": "application/json", "x-user-id": userId },
      body: JSON.stringify({
        ...(conversationId === undefined ? {} : { conversationId }),
        message: last.content,
      }),
    });
    const raw = await res.text();
    if (!res.ok) {
      throw new Error(`askViaHttp: the agent's /chat answered ${res.status}: ${raw}`);
    }

    let body: ChatReply;
    try {
      body = JSON.parse(raw) as ChatReply;
    } catch {
      throw new Error(`askViaHttp: the agent's /chat returned a malformed body (${res.status}): ${raw}`);
    }
    if (typeof body.conversationId !== "string" || typeof body.text !== "string") {
      throw new Error(
        `askViaHttp: the agent's /chat returned a malformed body (${res.status}), ` +
          `expected { conversationId, text }: ${raw}`,
      );
    }

    conversationId = body.conversationId;
    return body.text;
  };
}
