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
 * `prototype check --json` over fixture folders: one valid folder per
 * shipped example, one invalid folder per finding. Each row asserts the exit
 * code and the exact codes, files and locations an agent would act on.
 */

import { copyFileSync, mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { checkJson, fixturePath, tempDir } from "./harness.js";

type Expected = [code: string, file: string, location: string];

interface Row {
  fixture: string;
  findings: Expected[];
  /** A pattern the first finding's message must match. */
  message?: RegExp;
}

const valid = ["baseline", "local-names", "contacts", "expense-approval", "integration-monitor", "app-shell", "team-leave", "row-actions"];

const invalid: Row[] = [
  { fixture: "missing-source", findings: [["MISSING_FILE", "prototype.tsx", "(file)"]] },
  // Manifest: shape, then references.
  { fixture: "manifest-not-json", findings: [["SCHEMA_VIOLATION", "prototype.json", "(root)"]], message: /not valid JSON/ },
  { fixture: "manifest-missing-roles", findings: [["SCHEMA_VIOLATION", "prototype.json", "roles"]] },
  { fixture: "manifest-legacy-component", findings: [["SCHEMA_VIOLATION", "prototype.json", "(root)"]], message: /component/ },
  { fixture: "manifest-version-2", findings: [["UNSUPPORTED_VERSION", "prototype.json", "schemaVersion"]] },
  { fixture: "manifest-duplicate-id", findings: [["DUPLICATE_ID", "prototype.json", "screens[1].id"]] },
  { fixture: "manifest-unknown-role", findings: [["UNKNOWN_REFERENCE", "prototype.json", "screens[1].roleIds[1]"]] },
  { fixture: "manifest-unknown-entry", findings: [["UNKNOWN_REFERENCE", "prototype.json", "entryScreen"]] },
  { fixture: "manifest-flow-unknown-screen", findings: [["UNKNOWN_REFERENCE", "prototype.json", "flows[0].screenIds[1]"]] },
  { fixture: "manifest-flow-unreachable", findings: [["UNKNOWN_REFERENCE", "prototype.json", "flows[0].screenIds[1]"]], message: /role "user" reaches/ },
  // Source: the static rules.
  { fixture: "source-syntax-error", findings: [["SYNTAX_ERROR", "prototype.tsx", "line 27"]] },
  { fixture: "source-import-other-package", findings: [["FORBIDDEN_IMPORT", "prototype.tsx", "line 22"]], message: /lodash/ },
  { fixture: "source-dynamic-import", findings: [["FORBIDDEN_IMPORT", "prototype.tsx", "line 23"]], message: /dynamic import/ },
  { fixture: "source-require", findings: [["FORBIDDEN_IMPORT", "prototype.tsx", "line 23"]], message: /^require/ },
  { fixture: "source-fetch", findings: [["FORBIDDEN_API", "prototype.tsx", "line 27"]], message: /^fetch/ },
  { fixture: "source-local-storage", findings: [["FORBIDDEN_API", "prototype.tsx", "line 27"]], message: /^localStorage/ },
  { fixture: "source-eval", findings: [["FORBIDDEN_API", "prototype.tsx", "line 27"]], message: /^eval/ },
  { fixture: "source-window", findings: [["FORBIDDEN_API", "prototype.tsx", "line 27"]], message: /^window/ },
  { fixture: "source-raw-element", findings: [["FORBIDDEN_ELEMENT", "prototype.tsx", "line 27"]], message: /<div>/ },
  { fixture: "source-inner-html", findings: [["FORBIDDEN_API", "prototype.tsx", "line 27"]], message: /dangerouslySetInnerHTML/ },
  { fixture: "source-math-random", findings: [["FORBIDDEN_API", "prototype.tsx", "line 27"]], message: /Math\.random/ },
  { fixture: "source-date-now", findings: [["FORBIDDEN_API", "prototype.tsx", "line 27"]], message: /Date\.now/ },
  { fixture: "source-new-date", findings: [["FORBIDDEN_API", "prototype.tsx", "line 27"]], message: /new Date\(\)/ },
  // Source: literal navigation targets against the manifest.
  { fixture: "source-unknown-to", findings: [["UNKNOWN_NAV_TARGET", "prototype.tsx", "line 27"]], message: /screen\.reports/ },
  { fixture: "source-unknown-go", findings: [["UNKNOWN_NAV_TARGET", "prototype.tsx", "line 28"]], message: /screen\.audit/ },
  // Render: every screen × role × state, isolated.
  { fixture: "render-throws-in-one-state", findings: [["RENDER_FAILED", "prototype.tsx", "screen.home as user in state.empty"]], message: /reading 'join'/ },
  { fixture: "render-throws-for-one-role", findings: [["RENDER_FAILED", "prototype.tsx", "screen.home as admin in state.default"]], message: /reading 'name'/ },
  { fixture: "render-duplicate-element-id", findings: [["DUPLICATE_ELEMENT_ID", "prototype.tsx", "screen.admin as admin in state.default"]], message: /"heading\.admin"/ },
  { fixture: "render-duplicate-id-special-chars", findings: [["DUPLICATE_ELEMENT_ID", "prototype.tsx", "screen.admin as admin in state.default"]], message: /"say \\"hi\\" & bye"/ },
  { fixture: "render-unreachable-to", findings: [["UNKNOWN_NAV_TARGET", "prototype.tsx", "screen.home as user in state.default"]], message: /role "user" does not reach/ },
  // The app shell: its user menu's targets are checked while the menu is closed, and each is required.
  { fixture: "render-shell-unknown-target", findings: [["UNKNOWN_NAV_TARGET", "prototype.tsx", "screen.home as user in state.default"]], message: /screen\.goodbye/ },
  { fixture: "render-shell-missing-target", findings: [["RENDER_FAILED", "prototype.tsx", "screen.home as user in state.default"]], message: /<AppShell> needs signOut/ },
  // Stats and tables: an icon outside the kit's set, a status with no column for it, and a row action's target per role.
  { fixture: "render-stat-unknown-icon", findings: [["RENDER_FAILED", "prototype.tsx", "screen.home as user in state.default"]], message: /<Stat> icon "Calender" is not one of the kit's icons/ },
  { fixture: "render-table-status-without-column", findings: [["RENDER_FAILED", "prototype.tsx", "screen.home as user in state.default"]], message: /no column is \{ kind: "status" \}/ },
  { fixture: "render-table-action-unreachable-to", findings: [["UNKNOWN_NAV_TARGET", "prototype.tsx", "screen.home as user in state.default"]], message: /role "user" does not reach/ },
  { fixture: "render-missing-element-id", findings: [["RENDER_FAILED", "prototype.tsx", "screen.admin as admin in state.default"]], message: /<Text> needs an id/ },
  { fixture: "render-no-default-export", findings: [["NO_APP", "prototype.tsx", "module"]] },
  {
    fixture: "render-screen-mismatch",
    findings: [
      ["SCREEN_MISMATCH", "prototype.tsx", "screen.extra"],
      ["SCREEN_MISMATCH", "prototype.tsx", "screen.admin"],
    ],
  },
  { fixture: "render-unknown-collection", findings: [["RENDER_FAILED", "prototype.tsx", "screen.home as user in state.default"]], message: /no collection named "contacts"/ },
  { fixture: "render-data-not-json", findings: [["RENDER_FAILED", "prototype.tsx", "module"]], message: /data\.lookup is not JSON/ },
  { fixture: "form-invalid-pattern", findings: [["RENDER_FAILED", "prototype.tsx", "screen.home as user in state.default"]], message: /not a valid regular expression/ },
  // Sandbox-escape attempts: refused by the isolated render, not just the static rules.
  { fixture: "escape-computed-constructor", findings: [["RENDER_FAILED", "prototype.tsx", "module"]], message: /Code generation from strings disallowed/ },
  { fixture: "escape-prototype-pollution", findings: [["RENDER_FAILED", "prototype.tsx", "module"]], message: /not extensible/ },
  { fixture: "escape-kit-function", findings: [["RENDER_FAILED", "prototype.tsx", "module"]], message: /Code generation from strings disallowed/ },
];

describe("prototype check --json", () => {
  it.each(valid)("passes the valid fixture %s with exit 0", (name) => {
    const { status, ok, findings } = checkJson(fixturePath(`valid/${name}`));
    expect(findings, JSON.stringify(findings, null, 2)).toEqual([]);
    expect(ok).toBe(true);
    expect(status).toBe(0);
  });

  it.each(invalid)("reports $fixture", ({ fixture, findings: expected, message }) => {
    const { status, ok, findings } = checkJson(fixturePath(`invalid/${fixture}`));
    expect(findings.map((f): Expected => [f.code, f.file, f.location]), JSON.stringify(findings, null, 2)).toEqual(expected);
    if (message) expect(findings[0]?.message).toMatch(message);
    expect(ok).toBe(false);
    expect(status).toBe(1);
  });

  it("reports both files of a folder that has neither", () => {
    const { status, findings } = checkJson(join(tempDir(), "nothing-here"));
    expect(findings.map((f): Expected => [f.code, f.file, f.location])).toEqual([
      ["MISSING_FILE", "prototype.json", "(file)"],
      ["MISSING_FILE", "prototype.tsx", "(file)"],
    ]);
    expect(status).toBe(1);
  });

  it("reports a source over the size cap without parsing it", () => {
    const dir = tempDir();
    mkdirSync(dir, { recursive: true });
    copyFileSync(fixturePath("valid/baseline/prototype.json"), join(dir, "prototype.json"));
    writeFileSync(join(dir, "prototype.tsx"), `// ${"x".repeat(256 * 1024)}\n`);
    const { status, findings } = checkJson(dir);
    expect(findings.map((f): Expected => [f.code, f.file, f.location])).toEqual([["SOURCE_TOO_LARGE", "prototype.tsx", "(file)"]]);
    expect(status).toBe(1);
  });

  // Review Focus: a screen that never finishes rendering must not hang the agent's check loop.
  it("ends a render that never finishes with RENDER_FAILED inside the time cap", () => {
    const started = Date.now();
    const { status, findings } = checkJson(fixturePath("invalid/render-hangs"), 40_000);
    expect(findings.map((f): Expected => [f.code, f.file, f.location])).toEqual([["RENDER_FAILED", "prototype.tsx", "module"]]);
    expect(findings[0]?.message).toMatch(/took longer than 15s/);
    expect(status).toBe(1);
    expect(Date.now() - started).toBeLessThan(30_000);
  }, 45_000);
});
