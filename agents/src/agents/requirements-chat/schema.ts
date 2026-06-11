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

import { z } from "zod";

// Each turn arrives with the full file map for the in-scope files (BFF pre-loads
// from the working tree) plus the chat history so the model can keep context
// across turns. The schema is shared with the BFF — keep field names stable.

export const ChatHistoryMessage = z.object({
  role: z.enum(["user", "assistant"]),
  content: z.string(),
});

export const RequirementsChatInput = z.object({
  message: z.string().min(1),
  history: z.array(ChatHistoryMessage).default([]),
  // Filename -> content. Files the BFF told the agent are in-scope for this
  // turn. The agent is also told to use `read_file` for anything outside
  // this set.
  files: z.record(z.string(), z.string()).default({}),
  // Read-only mode disables write tools server-side; the route omits them
  // from the tool list.
  mode: z.enum(["edit", "ask"]).default("edit"),
});

export type RequirementsChatInput = z.infer<typeof RequirementsChatInput>;
export type ChatHistoryMessage = z.infer<typeof ChatHistoryMessage>;

// Element shapes accepted by the canvas tools — mirrored byte-for-byte by
// the BFF schema (`asdlc-service/services/requirements_chat_*`) so the
// wire format is symmetrical.

export const WireframeElement = z.discriminatedUnion("kind", [
  z.object({
    kind: z.literal("rect"),
    label: z.string(),
    x: z.number().int().min(0).max(360),
    y: z.number().int().min(0).max(540),
    width: z.number().int().positive().optional(),
    height: z.number().int().positive().optional(),
  }),
  z.object({
    kind: z.literal("button"),
    label: z.string(),
    x: z.number().int().min(0).max(360),
    y: z.number().int().min(0).max(540),
    width: z.number().int().positive().optional(),
    height: z.number().int().positive().optional(),
  }),
  z.object({
    kind: z.literal("ellipse"),
    label: z.string(),
    x: z.number().int().min(0).max(360),
    y: z.number().int().min(0).max(540),
    width: z.number().int().positive().optional(),
    height: z.number().int().positive().optional(),
  }),
  z.object({
    kind: z.literal("text"),
    label: z.string(),
    x: z.number().int().min(0).max(360),
    y: z.number().int().min(0).max(540),
  }),
]);
export type WireframeElement = z.infer<typeof WireframeElement>;

export const DomainAttribute = z.object({
  name: z.string().min(1),
  type: z.string().min(1),
});
export type DomainAttribute = z.infer<typeof DomainAttribute>;
