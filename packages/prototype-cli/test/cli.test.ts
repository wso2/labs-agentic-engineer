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

/** The CLI's surface: help, version, usage errors (exit 2) and the human output. */

import { describe, expect, it } from "vitest";
import { fixturePath, runCli } from "./harness.js";

describe("prototype (usage)", () => {
  it("prints help and exits 0 with no command, help, -h or --help", () => {
    for (const args of [[], ["help"], ["-h"], ["--help"]]) {
      const run = runCli(args);
      expect(run.status).toBe(0);
      expect(run.stdout).toContain("Usage: prototype <command>");
    }
  });

  it("prints its version", () => {
    expect(runCli(["--version"]).stdout).toBe("0.1.0\n");
  });

  it.each([
    [["frobnicate"], 'unknown command "frobnicate"'],
    [["check", "--nope"], "Unknown option '--nope'"],
    [["check", "a", "b"], 'unexpected argument "b"'],
    [["check", fixturePath("valid/baseline"), "--theme", "@wso2/no-such-theme"], 'theme "@wso2/no-such-theme" was not found'],
  ])("exits 2 with usage on %j", (args, message) => {
    const run = runCli(args);
    expect(run.status).toBe(2);
    expect(run.stderr).toContain(message);
    expect(run.stderr).toContain("Usage: prototype <command>");
  });

  it("prints findings for people without --json", () => {
    const run = runCli(["check", fixturePath("invalid/missing-source")]);
    expect(run.status).toBe(1);
    expect(run.stdout).toContain("prototype.tsx (file): MISSING_FILE");
    expect(run.stdout).toContain("1 finding.");
  });

  it("prints No findings. for a clean prototype", () => {
    const run = runCli(["check", fixturePath("valid/baseline")]);
    expect(run.status).toBe(0);
    expect(run.stdout).toBe("No findings.\n");
  });
});
