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

import type { DocumentGenerationSkill } from "./types.js";
import { systemPrompt as baSystemPrompt } from "../../agents/business-analyst/prompt.js";

/**
 * Bootstrap `requirements.md` from a free-text user prompt. This is the
 * starting point of the spec — every other document derives from it.
 *
 * The system prompt is the existing business-analyst prompt (lifted from
 * `agents/business-analyst/prompt.ts`); future tweaks happen in that file.
 */
export const requirementsFromPrompt: DocumentGenerationSkill = {
  id: "requirements-from-prompt",
  label: "Requirements from prompt",
  systemPrompt: baSystemPrompt,
  buildUserPrompt: ({ prompt }) => {
    return prompt?.trim() ?? "";
  },
};
