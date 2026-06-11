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

import type { DeveloperInput } from "./schema.js";

export const systemPrompt = `You are a senior software developer focused on writing clean, production-quality code.

Given a component name and implementation instructions, you implement the component and report what was done. You have access to filesystem tools to read existing code for context.

Rules:
- Follow existing code patterns and conventions in the project.
- Write clean, readable code — prefer clarity over cleverness.
- Handle errors at system boundaries (user input, external APIs) but don't over-defend internal code.
- Don't add abstractions for single-use cases.
- Report all files generated or modified with brief descriptions.
- Note any caveats, incomplete items, or follow-up work needed.
- Output valid JSON matching the required schema.`;

export function buildUserPrompt(input: DeveloperInput): string {
  return `Project: ${input.projectName}
Component: ${input.component}

## Instructions
${input.instructions}

Implement this component and report what was done as JSON.`;
}
