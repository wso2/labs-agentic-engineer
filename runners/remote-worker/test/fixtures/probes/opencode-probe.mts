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

// opencode-probe.mts — records one OpenCode session's bus as a fixture.
//
// The recording probe the four `opencode-*.jsonl` fixtures came from, rebuilt on
// the runner's own modules so a re-recording is made under the config and the
// close rule a run uses: `buildOpencodeConfig` (config.ts) with the guard plugin
// built by `buildGuardPlugin`, the runtime's environment (`childEnvironment`),
// the same launcher (server.ts), subscribe-before-create through the same queue
// (event_queue.ts), and the stream closed by the same rule (settle.ts). Every
// bus event is written as one JSON line stamped with `t` (ms since start) —
// the clock replay.test.ts replays durations against. Any permission ask is
// rejected, as the runtime does.
//
//   npx tsx test/fixtures/probes/opencode-probe.mts <out.jsonl> <projectDir> <promptFile> [overrides.json]
//
// `overrides.json` is deep-merged into the config (the S2c recording set the
// experimental flag in the ENVIRONMENT instead — export it to reproduce that).
// PROBE_PAST_CLOSE_MS records the raw bus PAST the close rule, until it has been
// quiet (heartbeats aside) that long: S2c's evidence is what arrives after the rule would close,
// which a recording that stops at the close cannot hold.
// Makes REAL model calls: needs ANTHROPIC_API_KEY and `opencode` on PATH; the
// Haiku recordings cost 1–5 cents each (S1c's Sonnet lead about 8). PROBE_MODEL picks
// the model (platform spelling).
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createOpencodeClient } from "@opencode-ai/sdk/client";

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../src");
const { buildOpencodeConfig } = await import(path.join(SRC, "runtime/opencode/config.ts"));
const { buildGuardPlugin } = await import(path.join(SRC, "runtime/opencode/plugin/build.ts"));
const { childEnvironment } = await import(path.join(SRC, "runtime/opencode/runtime.ts"));
const { freePort, startServer } = await import(path.join(SRC, "runtime/opencode/server.ts"));
const { pumpEvents } = await import(path.join(SRC, "runtime/opencode/event_queue.ts"));
const { createStreamCloser, sessionStream } = await import(path.join(SRC, "runtime/opencode/settle.ts"));
const { DENIED_CAPABILITIES } = await import(path.join(SRC, "runtime/port.ts"));

const [out, projectDir, promptFile, overridesFile] = process.argv.slice(2);
if (!out || !projectDir || !promptFile) {
  console.error("usage: tsx opencode-probe.mts <out.jsonl> <projectDir> <promptFile> [overrides.json]");
  process.exit(2);
}
if (!process.env.ANTHROPIC_API_KEY) {
  console.error("ANTHROPIC_API_KEY is required: this probe makes real model calls");
  process.exit(2);
}

type Json = Record<string, unknown>;
function deepMerge(a: Json, b: Json): Json {
  const o: Json = { ...a };
  for (const [k, v] of Object.entries(b)) {
    const prior = a[k];
    o[k] =
      v && typeof v === "object" && !Array.isArray(v) && prior && typeof prior === "object" && !Array.isArray(prior)
        ? deepMerge(prior as Json, v as Json)
        : v;
  }
  return o;
}

const workspace = path.resolve(projectDir);
const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "aep-oc-probe-"));
const pluginDir = await buildGuardPlugin(path.join(scratch, "aep-guard"));
const files = {
  instructions: path.join(scratch, "instructions.md"),
  secrets: path.join(scratch, "guard-secrets.json"),
  ready: path.join(scratch, "aep-guard.ready"),
  probe: path.join(scratch, "aep-guard.probe"),
};
fs.writeFileSync(files.instructions, process.env.PROBE_INSTRUCTIONS ? fs.readFileSync(process.env.PROBE_INSTRUCTIONS, "utf8") : "");
fs.writeFileSync(files.secrets, JSON.stringify(process.env.PROBE_SECRET ? [process.env.PROBE_SECRET] : []), { mode: 0o600 });

const skillsDir = path.join(workspace, ".claude", "skills");
let config: Json = buildOpencodeConfig({
  model: process.env.PROBE_MODEL || "claude-haiku-4-5",
  instructionsPath: files.instructions,
  pluginDir,
  skillAllow: fs.existsSync(skillsDir) ? fs.readdirSync(skillsDir) : [],
  deniedCapabilities: DENIED_CAPABILITIES,
  debug: false,
});
if (overridesFile) config = deepMerge(config, JSON.parse(fs.readFileSync(overridesFile, "utf8")) as Json);
fs.writeFileSync(out.replace(/\.jsonl$/, "") + ".config.json", JSON.stringify(config, null, 2) + "\n");

// A pod's HOME holds only the image's pre-warmed OpenCode cache and config
// (opencode-prewarm.sh). The developer's own HOME is not that: OpenCode also
// reads skills from ~/.claude/skills, and a recording made there carried the
// host's skill paths. So the server gets a scratch HOME with just those two,
// and a plain bash with none of the developer's shell startup files (a zsh
// ZDOTDIR put the host's own ~/.zshenv errors into a recorded bash output).
const home = path.join(scratch, "home");
for (const dir of [".cache/opencode", ".config/opencode"]) {
  const from = path.join(os.homedir(), dir);
  if (!fs.existsSync(from)) continue;
  fs.mkdirSync(path.dirname(path.join(home, dir)), { recursive: true });
  fs.symlinkSync(from, path.join(home, dir));
}
const hostEnv = Object.fromEntries(
  Object.entries(process.env).filter(
    ([k, v]) => v !== undefined && !k.startsWith("XDG_") && !["ZDOTDIR", "BASH_ENV", "ENV"].includes(k),
  ),
) as Record<string, string>;
const env = childEnvironment({ workspace, env: { ...hostEnv, HOME: home, SHELL: "/bin/bash" }, logDir: scratch }, files);
const t0 = Date.now();
const server = await startServer({
  command: "opencode",
  hostname: "127.0.0.1",
  port: await freePort(),
  config,
  env,
  cwd: workspace,
  debug: false,
  timeoutMs: 60_000,
});
const client = createOpencodeClient({ baseUrl: server.url, throwOnError: true, directory: workspace });
const abort = new AbortController();
const rec = fs.createWriteStream(out);
let outcome = "settled";
try {
  await client.config.get();
  const subscription = await client.event.subscribe({ signal: abort.signal });
  const queue = pumpEvents(subscription.stream, { tickMs: 0 });
  await queue.connected;
  const created = await client.session.create({ body: { title: `probe ${path.basename(out)}` } });
  const rootId = created.data?.id ?? "";
  await client.session.promptAsync({
    path: { id: rootId },
    body: {
      agent: "aep",
      model: { providerID: "anthropic", modelID: process.env.PROBE_MODEL || "claude-haiku-4-5" },
      parts: [{ type: "text", text: fs.readFileSync(promptFile, "utf8") }],
    },
  });
  const deadline = setTimeout(() => {
    outcome = "timeout";
    abort.abort();
  }, Number(process.env.PROBE_TIMEOUT_MS || 600_000));
  const pastCloseMs = Number(process.env.PROBE_PAST_CLOSE_MS || 0);
  const closer = pastCloseMs > 0 ? { observe: () => false, busySessions: () => [] } : createStreamCloser();
  let quiet: NodeJS.Timeout | undefined;
  const armQuiet = (): void => {
    if (pastCloseMs <= 0) return;
    clearTimeout(quiet);
    quiet = setTimeout(() => {
      outcome = "quiet";
      abort.abort();
    }, pastCloseMs);
  };
  const reject = (p: Json): void => {
    void client
      .postSessionIdPermissionsPermissionId({
        path: { id: String(p.sessionID), permissionID: String(p.id) },
        body: { response: "reject" },
      })
      .catch(() => {});
  };
  try {
    for await (const message of sessionStream(queue.messages, closer, { skills: [], onPermissionAsked: reject })) {
      if (String((message as Json).type).startsWith("aep.")) continue;
      // A heartbeat is the server alive, not the session working.
      if ((message as Json).type !== "server.heartbeat") armQuiet();
      rec.write(JSON.stringify({ t: Date.now() - t0, ...(message as Json) }) + "\n");
    }
  } catch (err) {
    // The quiet timer ends a past-close recording by aborting the subscription.
    if (outcome !== "quiet") throw err;
  }
  clearTimeout(deadline);
  clearTimeout(quiet);
  queue.stop();
} finally {
  abort.abort();
  rec.end();
  await server.close();
  fs.rmSync(scratch, { recursive: true, force: true });
}
console.log(`[probe] ${outcome} after ${Date.now() - t0}ms → ${out}`);
process.exit(outcome === "settled" || outcome === "quiet" ? 0 : 3);
