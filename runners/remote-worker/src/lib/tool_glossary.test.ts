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

import { test } from "node:test";
import assert from "node:assert/strict";
import { toolGlossary } from "./tool_glossary.js";

test("toolGlossary: Claude Code waits by ending its turn; OpenCode's task call is the wait", () => {
  const claude = toolGlossary("claude-code");
  assert.ok(claude.includes("- **wait tool**: none. End your turn. A finished agent wakes you with its report."));
  assert.doesNotMatch(claude, /TaskOutput|block: true/);

  const opencode = toolGlossary("opencode");
  assert.match(opencode, /wait tool\*\*: none\. Each `task` call IS the wait/);
  assert.match(opencode, /fan-out is foreground, so ending your turn does not apply here/);
});
