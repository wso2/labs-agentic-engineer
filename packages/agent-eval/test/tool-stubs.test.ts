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

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { readToolStubs } from "../src/agent-doc.js";
import { startToolStubs } from "../src/tool-stubs.js";

let dir: string;

const OPENAPI = `openapi: 3.0.3
info: { title: lunch-api, version: "1.0.0" }
paths:
  /rounds:
    get:
      operationId: listRounds
      responses:
        "200":
          content:
            application/json:
              example: [{ id: "r-1" }]
    post:
      operationId: createRound
      responses:
        "200":
          content:
            application/json:
              example: { id: "r-2" }
`;

/** Writes a components tree the way `specs/design/components/` is laid out. */
function writeDesign(afmFrontMatter: string, withContract = true): string {
  const components = join(dir, "specs", "design", "components");
  mkdirSync(join(components, "lunch-buddy"), { recursive: true });
  if (withContract) {
    mkdirSync(join(components, "lunch-api"), { recursive: true });
    writeFileSync(join(components, "lunch-api", "openapi.yaml"), OPENAPI);
  }
  const afmPath = join(components, "lunch-buddy", "agent.afm.md");
  writeFileSync(afmPath, `---\n${afmFrontMatter}---\n\n# Role\nHelp with lunch.\n`);
  return afmPath;
}

const WITH_TOOLS = `name: "lunch-buddy"
x-aep:
  tools:
    openapi:
      - component: "lunch-api"
        baseUrl: "\${env:LUNCH_API_URL}"
        allow: [listRounds]
`;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "agent-eval-stubs-"));
});

afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

describe("readToolStubs", () => {
  it("reads the env var, the contract path, and the allow-list from the front matter", () => {
    const stubs = readToolStubs(writeDesign(WITH_TOOLS));
    expect(stubs).toHaveLength(1);
    expect(stubs[0]!.envVar).toBe("LUNCH_API_URL");
    expect(stubs[0]!.specPath).toMatch(/lunch-api\/openapi\.yaml$/);
    // The allow-list is the security boundary; a harness that drops it on
    // the floor cannot hold the agent to it.
    expect(stubs[0]!.allow).toEqual(["listRounds"]);
  });

  it("refuses an entry with no allow-list, which grants nothing and says nothing", () => {
    const noAllow = WITH_TOOLS.replace("        allow: [listRounds]\n", "");
    expect(() => readToolStubs(writeDesign(noAllow))).toThrow(/allow/);
  });

  it("returns nothing for an agent that declares no openapi tools", () => {
    expect(readToolStubs(writeDesign(`name: "lunch-buddy"\n`))).toEqual([]);
  });

  // Each of these would otherwise become an agent booted without a tool
  // address, whose tool failures then look like the prompt's fault.
  it("refuses a component whose contract is not where the convention puts it", () => {
    expect(() => readToolStubs(writeDesign(WITH_TOOLS, false))).toThrow(/openapi\.yaml/);
  });

  it("refuses a literal baseUrl, which cannot be pointed at a stub", () => {
    const literal = WITH_TOOLS.replace("${env:LUNCH_API_URL}", "https://lunch.example.com");
    expect(() => readToolStubs(writeDesign(literal))).toThrow(/env:NAME/);
  });

  it("refuses a document with no front matter", () => {
    const path = join(dir, "agent.afm.md");
    writeFileSync(path, "# Role\nno front matter here\n");
    expect(() => readToolStubs(path)).toThrow(/front matter/);
  });
});

describe("startToolStubs", () => {
  it("serves each contract from its own address and names it in the env", async () => {
    const running = await startToolStubs(readToolStubs(writeDesign(WITH_TOOLS)));
    try {
      const url = running.env.LUNCH_API_URL!;
      expect(url).toMatch(/^http:\/\/127\.0\.0\.1:\d+$/);
      const body = await (await fetch(`${url}/rounds`)).json();
      expect(body).toEqual([{ id: "r-1" }]);
      expect(running.calls.LUNCH_API_URL).toHaveLength(1);
    } finally {
      await running.close();
    }
  });

  it("closes everything it started when one contract cannot be read", async () => {
    const good = readToolStubs(writeDesign(WITH_TOOLS))[0]!;
    const missing = { envVar: "OTHER_API_URL", specPath: join(dir, "nope.yaml") };
    // The first stub must not still be listening when the second fails: a
    // leaked port per scenario adds up across a three-round loop. Counting
    // the process's own handles cannot see it (vitest runs this in a worker),
    // so the closing is watched directly.
    let closed = 0;
    const start = () =>
      Promise.resolve({ url: "http://127.0.0.1:1", calls: [], close: async () => { closed += 1; } });
    await expect(startToolStubs([good, missing], start)).rejects.toThrow();
    expect(closed).toBe(1);
  });

  it("closes everything it started when a stub server itself fails to start", async () => {
    const good = readToolStubs(writeDesign(WITH_TOOLS))[0]!;
    let closed = 0;
    let started = 0;
    const start = (): Promise<never> | Promise<{ url: string; calls: never[]; close: () => Promise<void> }> => {
      started += 1;
      if (started === 2) return Promise.reject(new Error("port refused"));
      return Promise.resolve({ url: "http://127.0.0.1:1", calls: [], close: async () => { closed += 1; } });
    };
    await expect(startToolStubs([good, good], start)).rejects.toThrow(/port refused/);
    expect(closed).toBe(1);
  });

  // `allow` is the security boundary. The built agent only gets tools for
  // allow-listed operations, so this cannot widen anything — but a stub that
  // answers 200 to an operation the agent may not call makes an over-reach
  // invisible exactly where it would be cheapest to see.
  it("serves the allow-listed operation and refuses the rest of the contract", async () => {
    const running = await startToolStubs(readToolStubs(writeDesign(WITH_TOOLS)));
    try {
      const url = running.env.LUNCH_API_URL!;
      expect((await fetch(`${url}/rounds`)).status).toBe(200);
      const denied = await fetch(`${url}/rounds`, { method: "POST" });
      expect(denied.status).toBe(403);
      expect(await denied.text()).toContain("createRound");
    } finally {
      await running.close();
    }
  });

  it("names the operations that were reached for but not permitted", async () => {
    const running = await startToolStubs(readToolStubs(writeDesign(WITH_TOOLS)));
    try {
      expect(running.overReach()).toEqual([]);
      await fetch(`${running.env.LUNCH_API_URL!}/rounds`);
      expect(running.overReach()).toEqual([]);
      await fetch(`${running.env.LUNCH_API_URL!}/rounds`, { method: "POST" });
      expect(running.overReach()).toEqual(["LUNCH_API_URL: createRound"]);
    } finally {
      await running.close();
    }
  });

  // A typo in `allow` gives the agent no tool at all, and it would then score
  // badly for a reason no prompt fix can reach.
  it("refuses an allow entry the contract does not define", async () => {
    const typo = WITH_TOOLS.replace("[listRounds]", "[listRound]");
    await expect(startToolStubs(readToolStubs(writeDesign(typo)))).rejects.toThrow(/listRound\b/);
  });

  it("starts nothing, and closes cleanly, for an agent with no tools", async () => {
    const running = await startToolStubs([]);
    expect(running.env).toEqual({});
    await expect(running.close()).resolves.toBeUndefined();
  });
});
