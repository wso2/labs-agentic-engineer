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

// A stand-in for a generated agent, honouring the same two routes
// (`/healthz`, `/chat`) and the same `PORT` override that `bootAgent` uses.
// It exists so every test in this package can boot and talk to "an agent"
// without a model call, an API key, or the network.
//
// `writeFakeAgent` emits it as the `dist/main.js` a real component would
// have — source, not a copy of a compiled file, because the thing under test
// is how `bootAgent` treats a child process, not how one is built.

import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

export type FakeAgentMode =
  /** Ready immediately: `/healthz` answers 200 and `/chat` replies. */
  | "ready"
  /** Never ready: `/healthz` answers 503 with an unset env var named. */
  | "missing-env"
  /** Never ready: fully configured, but the conversation store never comes up. */
  | "store-initialising"
  /** Exits before it ever listens, the way a component with no build does. */
  | "crash"
  /** Ready, but every `/chat` answers 500. */
  | "chat-error"
  /** Ready, but `/chat` answers 200 with a body that is not the contract. */
  | "chat-malformed";

const SOURCE = `
import { createServer } from "node:http";
import { randomUUID } from "node:crypto";

const mode = process.env.FAKE_AGENT_MODE ?? "ready";
if (mode === "crash") {
  console.error("Cannot find module 'dist/main.js'");
  process.exit(1);
}

// Conversation state, held server-side exactly as the real contract requires:
// the caller sends a message and an id, never history.
const conversations = new Map();

function send(res, status, body) {
  res.statusCode = status;
  res.setHeader("content-type", "application/json");
  res.end(JSON.stringify(body));
}

const server = createServer((req, res) => {
  const path = (req.url ?? "/").split("?")[0];
  if (path === "/healthz") {
    if (mode === "missing-env") {
      return send(res, 503, { ok: false, missing: ["LUNCH_API_URL"], store: "ready" });
    }
    if (mode === "store-initialising") {
      return send(res, 503, { ok: false, missing: [], store: "initialising" });
    }
    return send(res, 200, { ok: true });
  }
  // Not part of the agent contract — a window for the tests into what this
  // child was actually given, so "MEMORY_DB_* stays unset" is observable.
  if (path === "/debug/env") {
    return send(res, 200, {
      MEMORY_DB_HOST: process.env.MEMORY_DB_HOST ?? null,
      MODEL_API_KEY: process.env.MODEL_API_KEY ?? null,
      PORT: process.env.PORT ?? null,
      LUNCH_API_URL: process.env.LUNCH_API_URL ?? null,
    });
  }
  if (path === "/chat" && req.method === "POST") {
    if (mode === "chat-error") return send(res, 500, { error: "the model provider refused" });
    let raw = "";
    req.on("data", (c) => { raw += c; });
    req.on("end", async () => {
      const body = JSON.parse(raw || "{}");
      if (mode === "chat-malformed") return send(res, 200, { nothing: "useful" });
      let id = body.conversationId;
      if (id === undefined) {
        id = randomUUID();
        conversations.set(id, []);
      } else if (!conversations.has(id)) {
        return send(res, 404, { error: "conversation not found" });
      }
      const history = conversations.get(id);
      history.push(body.message);
      // Stands in for a tool call: if the harness wired a provider address,
      // reach it and put what came back in the reply, so the transcript
      // shows whether the stub was really connected.
      let tool = "";
      if (process.env.LUNCH_API_URL) {
        try {
          const r = await fetch(process.env.LUNCH_API_URL + "/rounds");
          tool = "; tool: " + (await r.text());
        } catch (e) {
          tool = "; tool failed: " + String(e);
        }
      }
      // The reply names the turn count and echoes the FIRST message of this
      // conversation, so a test can see whether memory survived the turn.
      send(res, 200, {
        conversationId: id,
        text: "turn " + history.length + " of " + id + "; port: " + process.env.PORT +
          "; first: " + history[0] + tool,
        toolCalls: [],
      });
    });
    return;
  }
  send(res, 404, { error: "not found" });
});

server.listen(Number(process.env.PORT ?? 9090), "127.0.0.1");
`;

/**
 * Writes the fake agent where `bootAgent` looks for a built component.
 *
 * `mode` is baked into the file as its default because a caller that boots
 * through the provider cannot reach the child's environment — the provider
 * decides what the agent is given, which is the point of it.
 */
export function writeFakeAgent(
  appDir: string,
  entry = join("dist", "main.js"),
  mode: FakeAgentMode = "ready",
): string {
  const target = join(appDir, entry);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, SOURCE.replace('?? "ready"', `?? "${mode}"`));
  // A generated component's own package.json declares ESM; without it Node
  // reads a `.js` entry as CommonJS and the fake agent would fail for a
  // reason that has nothing to do with what is being tested.
  writeFileSync(join(appDir, "package.json"), JSON.stringify({ type: "module" }));
  return target;
}
