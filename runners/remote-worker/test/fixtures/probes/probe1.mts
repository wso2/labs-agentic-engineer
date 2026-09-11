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

// probe1.mts
// Probe 1: three BACKGROUND subagents in one turn, one of them spawning a depth-2 child.
// Records every SDK message as one JSON line. Run: npx tsx probe1.mts out.jsonl
import { query } from "@anthropic-ai/claude-agent-sdk";
import { mkdtempSync, writeFileSync, appendFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
const out = process.argv[2];
const cwd = mkdtempSync(join(tmpdir(), "aep-probe-"));
writeFileSync(join(cwd, "README.md"), "probe workspace\n");
writeFileSync(out, "");
const prompt = `You are a probe. Do exactly this, nothing else:
1. In ONE assistant turn, call the Agent tool TWICE with run_in_background: true. Give them descriptions "probe alpha" and "probe beta". Each subagent's prompt: "Run the Bash command: echo alpha-1 (or beta-1); then echo alpha-2; then create a file named alpha.txt containing 'ok' with the Write tool; then reply with exactly one line: 'REPORT: <your name> done, 1 file'". Adjust names per agent.
2. Also in that same first turn, call Agent a THIRD time with run_in_background: true, description "probe gamma", whose prompt is: "You MUST call the Agent tool once yourself with description 'probe gamma-child' and run_in_background: false, prompt 'Run Bash: echo child; reply DONE'. Then reply 'REPORT: gamma done'."
3. Then wait for all three with TaskOutput (block: true, timeout 300000) and reply with one line: 'ALL DONE'.`;
const q = query({ prompt, options: {
  cwd, model: "claude-haiku-4-5-20251001",
  allowedTools: ["Bash", "Write", "Agent", "TaskOutput"],
  permissionMode: "bypassPermissions", allowDangerouslySkipPermissions: true,
  persistSession: false, settingSources: [], forwardSubagentText: true, maxTurns: 30,
}});
for await (const m of q) appendFileSync(out, JSON.stringify(m) + "\n");
console.error("probe done, cwd=" + cwd);
