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

// Project-scoped conversations (#430): the thread id is SERVER-minted and
// stored against the project — every member resolves the same one, which is
// what makes the chat a shared thread and `agentEngaged` project-accurate.
// The org's design agent (ae-design-agent /v1, through designAgent()) owns
// the threads.

import type { QueryClient } from "@tanstack/react-query";
import type { components } from "../../../generated/ae-design-agent";
import { designAgent } from "../../../api/aeStudio";
import { apiErrorCode, apiErrorMessage } from "../../../api/errors";

export type ProjectConversationView = components["schemas"]["ProjectConversationView"];

/**
 * Resolve the project's CURRENT thread id — created on the first read, so the
 * first visitor mints it and teammates converge on it.
 */
export async function fetchCurrentConversationId(projectName: string): Promise<string> {
  const { data, error } = await designAgent().GET("/projects/{projectName}/conversations/current", {
    params: { path: { projectName } },
  });
  if (error !== undefined || data === undefined) {
    throw new Error(apiErrorMessage(error, "Failed to resolve the project conversation"));
  }
  return data.conversationId;
}

/**
 * Start a fresh thread for the WHOLE project (D4): the current thread is
 * demoted for every member. Returns the new current id. The caller owns the
 * confirmation. Refused while a turn runs for the project, whose save would
 * recreate the demoted thread.
 */
export async function rotateConversation(projectName: string): Promise<string> {
  const { data, error, response } = await designAgent().POST("/projects/{projectName}/conversations", {
    params: { path: { projectName } },
  });
  if (error !== undefined || data === undefined) {
    if (response.status === 409 && apiErrorCode(error) === "turn_in_progress") {
      throw new Error("A turn is running for this project — start a new conversation once it finishes.");
    }
    throw new Error(apiErrorMessage(error, "Failed to start a new conversation"));
  }
  return data.conversationId;
}

/** Rotate the project's current thread and stamp the new id into the query cache. */
export async function rotateCurrentConversation(
  queryClient: QueryClient,
  projectName: string,
): Promise<string> {
  const fresh = await rotateConversation(projectName);
  queryClient.setQueryData(conversationKeys.current(projectName), fresh);
  return fresh;
}

/** Query keys for the thread (react-query). */
export const conversationKeys = {
  current: (projectName: string) => ["agent-conversation", projectName] as const,
  /**
   * The thread's server-side history. ONE cache entry shared by every surface
   * that rehydrates the log (#606) — the chat panel, the spec workspace and the
   * overview's spec card — so three mounted readers cost one request, not three.
   *
   * Keyed on the conversation id as well as the project: a rotation must not
   * serve the demoted thread's messages under the new thread's key.
   */
  messages: (projectName: string, conversationId: string) =>
    ["agent-conversation", projectName, "messages", conversationId] as const,
};
