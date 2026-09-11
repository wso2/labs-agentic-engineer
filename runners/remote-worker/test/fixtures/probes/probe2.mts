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

// probe2.mts
// Probe 2: the lead ends its turn while ONE background subagent is still running.
// Records every SDK message as one JSON line. Run: npx tsx probe2.mts out.jsonl
import { query } from "@anthropic-ai/claude-agent-sdk";
import { mkdtempSync, writeFileSync, appendFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
const out = process.argv[2];
const cwd = mkdtempSync(join(tmpdir(), "aep-probe-"));
writeFileSync(join(cwd, "README.md"), "probe workspace\n");
writeFileSync(out, "");
const prompt = `You are a probe. Do exactly this: call the Agent tool ONCE with run_in_background: true, description "probe slow", prompt "Run the Bash command: sleep 25; then run Bash: echo slow-done; then reply REPORT: slow done". Then IMMEDIATELY reply with the single line LEAD DONE and end your turn. Do NOT wait for the agent and do NOT call TaskOutput.`;
const q = query({ prompt, options: {
  cwd, model: "claude-haiku-4-5-20251001",
  allowedTools: ["Bash", "Write", "Agent", "TaskOutput"],
  permissionMode: "bypassPermissions", allowDangerouslySkipPermissions: true,
  persistSession: false, settingSources: [], forwardSubagentText: true, maxTurns: 30,
}});
for await (const m of q) appendFileSync(out, JSON.stringify(m) + "\n");
console.error("probe done, cwd=" + cwd);
