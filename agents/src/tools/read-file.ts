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

import { tool } from "ai";
import { z } from "zod";
import { readFile as fsReadFile } from "node:fs/promises";

export const readFile = tool({
  description:
    "Read the contents of a file at the given absolute path. Returns the file text or an error message if the file cannot be read.",
  inputSchema: z.object({
    path: z.string().describe("Absolute path to the file to read"),
  }),
  execute: async ({ path }) => {
    try {
      const content = await fsReadFile(path, "utf-8");
      return { success: true as const, content };
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      return { success: false as const, error: message };
    }
  },
});
