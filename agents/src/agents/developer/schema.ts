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

export const DeveloperInput = z.object({
  projectName: z.string(),
  component: z.string().describe("Name of the component to implement"),
  instructions: z.string().describe("Implementation instructions and context"),
});

export type DeveloperInput = z.infer<typeof DeveloperInput>;

export const DeveloperOutput = z.object({
  summary: z.string().describe("Summary of what was implemented"),
  filesGenerated: z
    .array(
      z.object({
        path: z.string(),
        description: z.string(),
      }),
    )
    .describe("Files that were generated or modified"),
  notes: z
    .array(z.string())
    .describe("Implementation notes, caveats, or follow-up items"),
});

export type DeveloperOutput = z.infer<typeof DeveloperOutput>;
