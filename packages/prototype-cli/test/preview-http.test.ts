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

/**
 * The preview server's HTTP surface, against a spawned `prototype preview`:
 * what it serves, and what it refuses — feedback that is not JSON, from
 * another origin, or of the wrong shape, a page that another site would frame,
 * a request addressed by another host name (DNS rebinding), and a busy port.
 */

import { copyFileSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { request, type IncomingHttpHeaders } from "node:http";
import { join } from "node:path";
import { createRequire } from "node:module";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { PACKAGE_ROOT, copyFixture, runCli, startPreview, type PreviewProcess } from "./harness.js";

let preview: PreviewProcess;

beforeAll(async () => {
  preview = await startPreview(copyFixture("valid/contacts"));
});

afterAll(async () => {
  await preview.stop();
});

/** A raw request, so the Host header can be anything. */
function send(path: string, init: { method?: string; headers?: Record<string, string>; body?: string } = {}): Promise<{ status: number; body: string; headers: IncomingHttpHeaders }> {
  const url = new URL(path, preview.url);
  return new Promise((resolve, reject) => {
    const req = request({ host: url.hostname, port: url.port, path: url.pathname, method: init.method ?? "GET", headers: init.headers ?? {} }, (res) => {
      let body = "";
      res.setEncoding("utf8");
      res.on("data", (c: string) => (body += c));
      res.on("end", () => resolve({ status: res.statusCode ?? 0, body, headers: res.headers }));
    });
    req.on("error", reject);
    req.end(init.body);
  });
}

const valid = JSON.stringify({ prototypeHash: "a".repeat(64), requests: [{ screenId: "screen.contacts", roleId: "editor", stateId: "state.default", elementIds: [], text: "Hello" }] });
const feedbackFile = () => join(preview.dir, ".prototype", "feedback.json");

describe("prototype preview (HTTP)", () => {
  it("serves the host page, its script, the frame runtime and the event stream", async () => {
    expect((await send("/")).body).toContain('id="proto-config"');
    expect((await send("/host.js")).status).toBe(200);
    expect((await send("/frame-runtime.js")).status).toBe(200);
    expect((await send("/nope")).status).toBe(404);
  });

  it("refuses to be framed by another site", async () => {
    const { headers } = await send("/");
    expect(headers["content-security-policy"]).toContain("frame-ancestors 'none'");
    expect(headers["x-frame-options"]).toBe("DENY");
  });

  it("refuses a request addressed by another host name", async () => {
    const res = await send("/", { headers: { host: "attacker.example" } });
    expect(res.status).toBe(403);
  });

  it("refuses feedback that is not JSON, from another origin, or of the wrong shape, and writes nothing", async () => {
    expect((await send("/feedback", { method: "POST", headers: { "content-type": "text/plain" }, body: valid })).status).toBe(415);
    expect((await send("/feedback", { method: "POST", headers: { "content-type": "application/json", origin: "http://attacker.example" }, body: valid })).status).toBe(403);
    expect((await send("/feedback", { method: "POST", headers: { "content-type": "application/json" }, body: "{" })).status).toBe(400);
    expect((await send("/feedback", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ prototypeHash: "x", requests: [] }) })).status).toBe(400);
    expect(existsSync(feedbackFile())).toBe(false);
  });

  it("answers 413 to feedback over 1 MiB, declared or streamed, and writes nothing", async () => {
    const big = JSON.stringify({ prototypeHash: "a".repeat(64), requests: [{ screenId: "s", roleId: "r", stateId: "t", elementIds: [], text: "x".repeat(1024 * 1024 + 1) }] });
    const post = { method: "POST", headers: { "content-type": "application/json" } };
    expect((await send("/feedback", { ...post, body: big })).status).toBe(413);
    const url = new URL("/feedback", preview.url);
    const streamed = await new Promise<number>((resolve, reject) => {
      const req = request({ host: url.hostname, port: url.port, path: url.pathname, method: "POST", headers: { "content-type": "application/json", "transfer-encoding": "chunked" } }, (res) => (res.resume(), resolve(res.statusCode ?? 0)));
      req.on("error", reject);
      req.write(big.slice(0, 1024 * 1024 + 100));
      req.end(big.slice(1024 * 1024 + 100));
    });
    expect(streamed).toBe(413);
    expect(existsSync(feedbackFile())).toBe(false);
  });

  it("answers every one of 50 concurrent oversized uploads with 413, none reset", async () => {
    const body = JSON.stringify({ prototypeHash: "a".repeat(64), requests: [{ screenId: "s", roleId: "r", stateId: "t", elementIds: [], text: "x".repeat(2 * 1024 * 1024) }] });
    const statuses = await Promise.all(Array.from({ length: 50 }, () => send("/feedback", { method: "POST", headers: { "content-type": "application/json" }, body }).then((r) => r.status)));
    expect(statuses.filter((s) => s !== 413)).toEqual([]);
    expect(existsSync(feedbackFile())).toBe(false);
  });

  it("says which rule a refused submission broke", async () => {
    const post = (requests: unknown[]) => send("/feedback", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ prototypeHash: "a".repeat(64), requests }) });
    const one = { screenId: "s", roleId: "r", stateId: "t", elementIds: [] as string[], text: "Hello" };
    const many = await post(Array.from({ length: 51 }, () => one));
    expect(many.status).toBe(400);
    expect(many.body).toContain("too many requests (max 50)");
    const long = await post([one, one, { ...one, text: "x".repeat(4001) }]);
    expect(long.status).toBe(400);
    expect(long.body).toContain("request 3 text exceeds 4000 characters");
    expect(existsSync(feedbackFile())).toBe(false);
  });

  it("writes valid feedback from its own origin to .prototype/feedback.json", async () => {
    const origin = preview.url.replace(/\/$/, "");
    const res = await send("/feedback", { method: "POST", headers: { "content-type": "application/json", origin }, body: valid });
    expect(res.status).toBe(200);
    const file = JSON.parse(readFileSync(feedbackFile(), "utf8")) as Record<string, unknown>;
    expect(file).toMatchObject({ schemaVersion: 1, prototypeHash: "a".repeat(64), requests: [{ screenId: "screen.contacts", text: "Hello" }] });
    expect(typeof file["savedAt"]).toBe("string");
  });
});

// Review Focus: a second preview on a busy port.
describe("prototype preview (ports)", () => {
  it("refuses an explicit --port that is in use, with exit 2", () => {
    const port = new URL(preview.url).port;
    const run = runCli(["preview", preview.dir, "--port", port], { timeoutMs: 20_000 });
    expect(run.status).toBe(2);
    expect(run.stderr).toContain(`port ${port} is in use`);
  });
});

// Review Focus: a preview must not crash on its own files. A theme's runtime that vanishes while it runs (a rebuild) is a 500, not a dead server.
describe("prototype preview (a runtime file that disappears)", () => {
  it("answers 500 for the frame runtime and keeps serving", async () => {
    const dir = copyFixture("valid/contacts");
    const themeDir = join(dir, "node_modules", "fake-theme");
    mkdirSync(themeDir, { recursive: true });
    writeFileSync(join(themeDir, "package.json"), JSON.stringify({ name: "fake-theme", version: "0.0.0", exports: { "./frame-runtime.js": "./frame-runtime.js", "./check-runtime.js": "./check-runtime.js" } }));
    writeFileSync(join(themeDir, "frame-runtime.js"), "");
    copyFileSync(createRequire(join(PACKAGE_ROOT, "x.js")).resolve("@wso2/prototype-theme-default/check-runtime.js"), join(themeDir, "check-runtime.js"));
    const running = await startPreview(dir, ["--theme", "fake-theme"]);
    try {
      const get = (path: string) => {
        const url = new URL(path, running.url);
        return new Promise<number>((resolve, reject) => request({ host: url.hostname, port: url.port, path, headers: {} }, (res) => (res.resume(), resolve(res.statusCode ?? 0))).on("error", reject).end());
      };
      expect(await get("/frame-runtime.js")).toBe(200);
      rmSync(join(themeDir, "frame-runtime.js"));
      expect(await get("/frame-runtime.js")).toBe(500);
      expect(await get("/")).toBe(200);
    } finally {
      await running.stop();
      rmSync(dir, { recursive: true, force: true });
    }
  });
});
