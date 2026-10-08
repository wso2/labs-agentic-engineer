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

/** `prototype init`: the scaffold passes `check`, and init never overwrites. */

import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { checkJson, runCli, tempDir } from "./harness.js";

describe("prototype init", () => {
  it("scaffolds a prototype that passes check", () => {
    const dir = join(tempDir(), "app");
    const run = runCli(["init", dir]);
    expect(run.status).toBe(0);
    expect(run.stdout).toContain("prototype.json");
    const { status, findings } = checkJson(dir);
    expect(findings).toEqual([]);
    expect(status).toBe(0);
  });

  it("scaffolds into the current directory by default", () => {
    const dir = tempDir();
    expect(runCli(["init"], { cwd: dir }).status).toBe(0);
    expect(checkJson(dir).status).toBe(0);
  });

  it("refuses to overwrite an existing prototype and leaves it unchanged", () => {
    const dir = join(tempDir(), "app");
    runCli(["init", dir]);
    const before = readFileSync(join(dir, "prototype.tsx"), "utf8");
    const run = runCli(["init", dir]);
    expect(run.status).toBe(1);
    expect(run.stderr).toContain("already exist");
    expect(readFileSync(join(dir, "prototype.tsx"), "utf8")).toBe(before);
  });

  it("is a usage error with two directories", () => {
    expect(runCli(["init", "a", "b"]).status).toBe(2);
  });
});
