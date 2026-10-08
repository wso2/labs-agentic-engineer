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
 * Seam 2: the packed tarballs, installed where the workspace cannot help —
 * an empty directory outside it — and driven through the installed bin. It
 * catches a broken `exports` map, a file missing from `files`, a missing
 * prebuilt runtime and a workspace-only dependency, before anything is
 * published. Needs the npm registry for the packages' own dependencies.
 */

import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { afterAll, describe, expect, it } from "vitest";
import { CHILD_ENV, PACKAGE_ROOT, tempDir } from "./harness.js";

const PACKAGES = ["prototype-kit", "prototype-theme-default", "prototype-cli"];
const consumer = tempDir("prototype-consumer-");

afterAll(() => rmSync(consumer, { recursive: true, force: true }));

/** `pnpm pack` one workspace package into the consumer directory; the tarball's file name. */
function pack(name: string): string {
  const out = execFileSync("pnpm", ["pack", "--pack-destination", consumer], { cwd: join(PACKAGE_ROOT, "..", name), encoding: "utf8", env: { ...process.env } });
  const tarball = out.trim().split("\n").pop()!.trim();
  return tarball.slice(tarball.lastIndexOf("/") + 1);
}

function run(args: string[], cwd = consumer) {
  const result = spawnSync(join(consumer, "node_modules", ".bin", "prototype"), args, { cwd, encoding: "utf8", env: CHILD_ENV, timeout: 60_000 });
  return { status: result.status, stdout: result.stdout, stderr: result.stderr };
}

describe("the packed packages, installed outside the workspace", () => {
  it("install with npm and run init, check and export through the installed bin", () => {
    const [kit, theme, cli] = PACKAGES.map(pack) as [string, string, string];
    writeFileSync(
      join(consumer, "package.json"),
      JSON.stringify({
        name: "prototype-consumer",
        private: true,
        dependencies: { "@wso2/prototype-cli": `file:./${cli}` },
        overrides: { "@wso2/prototype-kit": `file:./${kit}`, "@wso2/prototype-theme-default": `file:./${theme}` },
      }),
    );
    execFileSync("npm", ["install", "--no-audit", "--no-fund", "--loglevel=error"], { cwd: consumer, encoding: "utf8", env: { ...process.env } });

    expect(run(["init", "app"]).status).toBe(0);
    const check = run(["check", "--json", "app"]);
    expect(JSON.parse(check.stdout)).toEqual({ ok: true, findings: [] });
    expect(check.status).toBe(0);
    expect(run(["export", "app", "-o", "app.html"]).status).toBe(0);
    expect(existsSync(join(consumer, "app.html"))).toBe(true);
    expect(readFileSync(join(consumer, "app.html"), "utf8")).toContain('id="proto-config"');
    expect(existsSync(join(consumer, "node_modules", "@wso2", "prototype-kit", "schema", "prototype-manifest.schema.json"))).toBe(true);
    expect(existsSync(join(consumer, "node_modules", "@wso2", "prototype-kit", "reference.md"))).toBe(true);
  }, 300_000);
});
