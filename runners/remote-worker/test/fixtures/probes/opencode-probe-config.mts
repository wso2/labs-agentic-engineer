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

// opencode-probe-config.mts — the MODEL-FREE check.
//
// Boots an OpenCode server for a project exactly as a run does
// (`bootOpencode`, runtime/opencode/runtime.ts: the config builder's output, the
// runtime's env flags, the guard plugin, the four start-time assertions) and
// prints what the server reports — its merged config, the tools the `aep` agent
// is offered, the skills it discovered — then stops it. The only session is the
// startup probe, which the guard refuses before any model call, so it costs
// nothing; a dummy key is enough.
//
//   npx tsx test/fixtures/probes/opencode-probe-config.mts <projectDir> [pluginDir]
//
// pluginDir defaults to the image's /app/runtime/opencode/aep-guard. On a host,
// build one first: `npx tsx src/runtime/opencode/plugin/build.ts <dir>`.
// Exit 0 when every assertion held, 1 with the refusal otherwise.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../src");
const { bootOpencode } = await import(path.join(SRC, "runtime/opencode/runtime.ts"));
const { DENIED_CAPABILITIES } = await import(path.join(SRC, "runtime/port.ts"));

const [projectDir, pluginDir] = process.argv.slice(2);
if (!projectDir) {
  console.error("usage: tsx opencode-probe-config.mts <projectDir> [pluginDir]");
  process.exit(2);
}
const workspace = path.resolve(projectDir);
const skillsDir = path.join(workspace, ".claude", "skills");
const model = process.env.PROBE_MODEL || "claude-haiku-4-5";

const policy = {
  workspace,
  env: { ...process.env, ANTHROPIC_API_KEY: process.env.ANTHROPIC_API_KEY || "dummy-no-model-call" } as Record<string, string>,
  model,
  taskKind: "implementation",
  debug: false,
  logDir: path.join(workspace, ".logs"),
  write: { allowOutsideProject: () => false },
  deniedCapabilities: DENIED_CAPABILITIES,
  webSearch: { deny: () => null },
  webFetch: { deny: () => null },
  skills: {
    dir: skillsDir,
    allow: fs.existsSync(skillsDir) ? fs.readdirSync(skillsDir) : [],
    preloadBodies: "## Tool glossary (probe)\n",
  },
};

const t0 = Date.now();
try {
  const booted = await bootOpencode(policy, pluginDir ? { pluginDir: path.resolve(pluginDir) } : {});
  const cfg = (await booted.client.config.get()).data ?? {};
  const tools = (await booted.client.tool.list({ query: { provider: "anthropic", model } })).data ?? [];
  console.log(
    JSON.stringify(
      {
        ok: true,
        bootMs: Date.now() - t0,
        assertions: "guard plugin loaded; task/skill/todowrite/edit/write/bash visible; question hidden; task has no background; system transform live",
        skillsDiscovered: booted.skills,
        config: {
          model: cfg.model,
          small_model: cfg.small_model,
          plugin: cfg.plugin,
          permission: cfg.permission,
          agents: Object.keys(cfg.agent ?? {}),
        },
        toolIds: tools.map((t: { id: string }) => t.id),
      },
      null,
      2,
    ),
  );
  await booted.close();
  process.exit(0);
} catch (err) {
  console.log(JSON.stringify({ ok: false, bootMs: Date.now() - t0, error: err instanceof Error ? err.message : String(err) }, null, 2));
  process.exit(1);
}
