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

// opencode-probe-appendix.mts — MODEL-FREE proof that the prompt appendix
// reaches the lead's system prompt and no subagent's.
//
// Starts the real server with the run's config and the guard plugin, plus a
// second throwaway plugin that dumps every system prompt AFTER the guard's
// transform (plugins run in config order). One prompt carries a `subtask` part,
// which spawns a `general` child without the model deciding to; the child's
// model call and then the lead's each fail at the provider on a dummy key, but
// both system prompts are dumped first.
//
//   npx tsx test/fixtures/probes/opencode-probe-appendix.mts [pluginDir]
//
// pluginDir defaults to a fresh build of src/runtime/opencode/plugin. Prints a
// JSON verdict; exit 0 when the lead has the appendix and the child does not.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../src");
const { createOpencodeClient } = await import("@opencode-ai/sdk/client");
const { buildOpencodeConfig } = await import(path.join(SRC, "runtime/opencode/config.ts"));
const { childEnvironment } = await import(path.join(SRC, "runtime/opencode/runtime.ts"));
const { freePort, startServer } = await import(path.join(SRC, "runtime/opencode/server.ts"));
const { buildGuardPlugin } = await import(path.join(SRC, "runtime/opencode/plugin/build.ts"));
const { DENIED_CAPABILITIES } = await import(path.join(SRC, "runtime/port.ts"));
const { SESSION_CONTEXT_FILE } = await import(path.join(SRC, "lib/run_context.ts"));

const MARKER = "APPENDIX-END-MARKER-7f3a";
const model = process.env.PROBE_MODEL || "claude-haiku-4-5";
const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "aep-oc-appendix-"));
const workspace = path.join(scratch, "proj");
fs.mkdirSync(path.join(workspace, ".claude", "skills", "demo"), { recursive: true });
fs.writeFileSync(
  path.join(workspace, ".claude", "skills", "demo", "SKILL.md"),
  "---\nname: demo\ndescription: A demo skill for the probe.\n---\nDemo body.\n",
);

const pluginDir = process.argv[2] ? path.resolve(process.argv[2]) : await buildGuardPlugin(path.join(scratch, "aep-guard"));
const dumpDir = path.join(scratch, "dump-plugin");
const dumpFile = path.join(scratch, "system-dump.jsonl");
fs.mkdirSync(dumpDir);
fs.writeFileSync(path.join(dumpDir, "package.json"), JSON.stringify({ name: "dump", type: "module", main: "index.js" }));
fs.writeFileSync(
  path.join(dumpDir, "index.js"),
  `import fs from "node:fs";
export const Dump = async () => ({
  "experimental.chat.system.transform": async (input, output) => {
    fs.appendFileSync(process.env.PROBE_DUMP, JSON.stringify({ session: input.sessionID, system: output.system }) + "\\n");
  },
});
`,
);

const files = {
  instructions: path.join(scratch, "instructions.md"),
  secrets: path.join(scratch, "guard-secrets.json"),
  ready: path.join(scratch, "aep-guard.ready"),
  probe: path.join(scratch, "aep-guard.probe"),
};
fs.writeFileSync(files.instructions, `# Your workflow\n\nProbe workflow.\n\n## Tool glossary\n${MARKER}\n`);
fs.writeFileSync(files.secrets, "[]", { mode: 0o600 });
const logDir = path.join(scratch, "logs");

const config = buildOpencodeConfig({
  model,
  instructionsPath: files.instructions,
  pluginDir,
  skillAllow: ["demo"],
  deniedCapabilities: DENIED_CAPABILITIES,
  debug: false,
}) as Record<string, unknown>;
config.plugin = [pluginDir, dumpDir];

const env = {
  ...childEnvironment(
    { workspace, env: { ...(process.env as Record<string, string>), ANTHROPIC_API_KEY: "dummy-no-model-call" }, logDir },
    files,
  ),
  PROBE_DUMP: dumpFile,
};

const server = await startServer({
  command: process.env.OPENCODE_BIN || "opencode",
  hostname: "127.0.0.1",
  port: await freePort(),
  config,
  env,
  cwd: workspace,
  debug: false,
  timeoutMs: 60_000,
});

type Dumped = { session: string; system: string[] };
const read = (file: string): Record<string, unknown>[] =>
  fs.existsSync(file)
    ? fs.readFileSync(file, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l) as Record<string, unknown>)
    : [];

let verdict: Record<string, unknown>;
try {
  const client = createOpencodeClient({ baseUrl: server.url, throwOnError: true, directory: workspace });
  await client.config.get();
  const created = await client.session.create({ body: { title: "appendix probe" } });
  const rootId = created.data?.id ?? "";
  await client.session.promptAsync({
    path: { id: rootId },
    body: {
      agent: "aep",
      model: { providerID: "anthropic", modelID: model },
      parts: [
        { type: "text", text: "probe" },
        { type: "subtask", agent: "general", description: "probe child", prompt: "probe child task" },
      ],
    },
  });

  const deadline = Date.now() + Number(process.env.PROBE_TIMEOUT_MS || 60_000);
  let dumped: Dumped[] = [];
  while (Date.now() < deadline) {
    dumped = read(dumpFile) as unknown as Dumped[];
    if (dumped.some((d) => d.session === rootId) && dumped.some((d) => d.session !== rootId)) break;
    await new Promise((r) => setTimeout(r, 250));
  }

  const records = read(path.join(logDir, SESSION_CONTEXT_FILE));
  const firstCall = new Map<string, Dumped>();
  for (const d of dumped) if (!firstCall.has(d.session)) firstCall.set(d.session, d);
  const sessions = [...firstCall.values()].map((d) => {
    const text = d.system.join("\n");
    return {
      session: d.session,
      role: d.session === rootId ? "lead" : "subagent",
      agent: records.find((r) => r.session === d.session && "agent" in r)?.agent,
      appendixInSystemPrompt: text.includes(MARKER),
      basePromptKept: text.startsWith("You are OpenCode"),
      envBlock: text.includes("<env>"),
      skillCatalog: text.includes("<available_skills>") || text.includes("Use the skill tool"),
      systemChars: text.length,
      head: text.slice(0, 80),
    };
  });
  const lead = sessions.find((s) => s.role === "lead");
  const child = sessions.find((s) => s.role === "subagent");
  verdict = {
    ok: Boolean(lead?.appendixInSystemPrompt && child && !child.appendixInSystemPrompt),
    sessions,
    sessionContext: records,
  };
} catch (err) {
  verdict = { ok: false, error: err instanceof Error ? err.message : String(err) };
} finally {
  await server.close();
}
console.log(JSON.stringify(verdict, null, 2));
fs.rmSync(scratch, { recursive: true, force: true });
process.exit(verdict.ok ? 0 : 1);
