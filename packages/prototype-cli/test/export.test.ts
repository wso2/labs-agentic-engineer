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

/** `prototype export`: one self-contained HTML file, written only for a clean prototype. */

import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { copyFixture, runCli, tempDir } from "./harness.js";

describe("prototype export", () => {
  it("writes <dir>/prototype.html by default, with everything inline and a no-network CSP", () => {
    const dir = copyFixture("valid/contacts");
    const run = runCli(["export", dir]);
    expect(run.status).toBe(0);
    const html = readFileSync(join(dir, "prototype.html"), "utf8");
    expect(html).toContain("connect-src 'none'");
    expect(html).toContain('id="proto-config"');
    expect(html).not.toMatch(/<script[^>]+src=/);
    expect(html).not.toMatch(/<link[^>]+href=/);
  });

  it("writes to -o", () => {
    const dir = copyFixture("valid/contacts");
    const out = join(tempDir(), "contacts.html");
    expect(runCli(["export", dir, "-o", out]).status).toBe(0);
    expect(existsSync(out)).toBe(true);
  });

  it("refuses a prototype with findings, prints them, and writes nothing", () => {
    const dir = copyFixture("invalid/source-fetch");
    const run = runCli(["export", dir]);
    expect(run.status).toBe(1);
    expect(run.stderr).toContain("FORBIDDEN_API");
    expect(existsSync(join(dir, "prototype.html"))).toBe(false);
  });

  it("keeps hostile text in the name and source from breaking out of the page's script elements", () => {
    const hostile = "</script><img src=x onerror=alert(1)><!-- <script>";
    const dir = copyFixture("valid/contacts");
    const manifestPath = join(dir, "prototype.json");
    const sourcePath = join(dir, "prototype.tsx");
    writeFileSync(manifestPath, readFileSync(manifestPath, "utf8").replace('"name": "Contacts"', `"name": ${JSON.stringify(hostile)}`));
    writeFileSync(sourcePath, readFileSync(sourcePath, "utf8").replace('text="New contact"', `text=${JSON.stringify(hostile)}`));
    expect(runCli(["check", dir]).status).toBe(0);

    expect(runCli(["export", dir]).status).toBe(0);
    const html = readFileSync(join(dir, "prototype.html"), "utf8");

    // The page has exactly two script elements (the config JSON and the host app): hostile text closes none early.
    expect(html.match(/<\/script/gi)).toHaveLength(2);
    // The config JSON holds no raw "<", and the hostile text is only there escaped.
    const config = html.match(/<script type="application\/json" id="proto-config">([\s\S]*?)<\/script>/)?.[1] ?? "";
    expect(config).not.toContain("<");
    expect(config).toContain("\\u003c/script>\\u003cimg src=x onerror=alert(1)>\\u003c!-- \\u003cscript>");
    // Nowhere in the file does the hostile markup appear raw.
    expect(html).not.toContain("<img src=x");
    expect(html).not.toContain("<!-- <script>");
    expect(html).toContain("&lt;/script&gt;&lt;img src=x onerror=alert(1)&gt;&lt;!-- &lt;script&gt;");
  });
});
