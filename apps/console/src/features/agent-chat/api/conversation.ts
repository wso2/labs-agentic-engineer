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

// The project conversation's two reads, copied from the old console's agent-chat
// (api/conversations.ts, api/turns.ts): resolve the project's current thread,
// then read its messages.

import type { components } from "../../../generated/aep-api";
import { client } from "../../../api/client";
import { apiErrorMessage } from "../../../api/errors";

export type ConversationMessage = components["schemas"]["ConversationMessage"];

/**
 * The project's CURRENT thread id (#430): server-minted and stored against the
 * project, so every member resolves the same one.
 */
export async function fetchCurrentConversationId(projectName: string): Promise<string> {
  const { data, error } = await client.GET("/projects/{projectName}/agents/conversations", {
    params: { path: { projectName } },
  });
  if (error || data === undefined) {
    throw new Error(apiErrorMessage(error, "Couldn't open the project conversation"));
  }
  const current = data.conversations.find((c) => c.current) ?? data.conversations[0];
  if (!current) throw new Error("The project has no conversation thread.");
  return current.conversationId;
}

/**
 * The thread's history, as the server persisted it. A turn's messages are
 * persisted when it ends, so a running turn is not in it: the chat attaches
 * to that one's stream instead.
 */
export async function fetchConversationMessages(
  projectName: string,
  conversationId: string,
): Promise<ConversationMessage[]> {
  const { data, error } = await client.GET("/projects/{projectName}/agents/{conversationId}/messages", {
    params: { path: { projectName, conversationId } },
  });
  if (error || data === undefined) {
    throw new Error(apiErrorMessage(error, "Couldn't load the conversation"));
  }
  return data.messages;
}
