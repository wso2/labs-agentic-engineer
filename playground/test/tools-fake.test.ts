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
 * The playground's in-process tools socket (`engine/tools-fake.ts`): a lookup
 * writes the snapshots where the design agent reads them in the pod
 * (`shared/snapshot-path.ts`), and answers what ae-studio-tools would.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { projectSnapshotDir, skillsSnapshotDir } from "@aep/ae-design-agent/shared/snapshot-path";
import { ToolsSocketError } from "@aep/ae-design-agent/tools-socket/client";
import { DESIGN_TOOL_NAMES } from "@aep/ae-design-agent/tools-socket/fake";
import { NO_CATALOG, PlaygroundToolsSocket } from "../src/engine/tools-fake.js";
import { FsSpecWorkspace, projectSlug } from "../src/ports/spec-workspace.js";
import { writeDescriptor } from "../src/state/descriptor.js";
import { REFERENCES_DIR } from "../src/state/references.js";

function fixture(t: { after: (fn: () => void) => void }): { projectDir: string; skillsDir: string; ws: FsSpecWorkspace } {
  const projectDir = mkdtempSync(join(tmpdir(), "aep-play-tools-"));
  const skillsDir = mkdtempSync(join(tmpdir(), "aep-play-tools-skills-"));
  mkdirSync(join(projectDir, "specs/requirements"), { recursive: true });
  writeFileSync(join(projectDir, "specs/requirements/prd.md"), "# PRD\n");
  mkdirSync(join(projectDir, "issues"), { recursive: true });
  writeFileSync(join(projectDir, "issues/1.md"), "a task\n");
  mkdirSync(join(skillsDir, "demo"), { recursive: true });
  writeFileSync(join(skillsDir, "demo", "SKILL.md"), "---\nname: demo\ndescription: demo\n---\n\nBody.\n");
  const ws = new FsSpecWorkspace(projectDir);
  t.after(() => {
    ws.cleanup();
    rmSync(projectDir, { recursive: true, force: true });
    rmSync(skillsDir, { recursive: true, force: true });
  });
  return { projectDir, skillsDir, ws };
}

test("projectSlug is a DNS label, as the /v1 edge names projects", () => {
  assert.equal(projectSlug("/x/My_Project.v2"), "my-project-v2");
  assert.equal(projectSlug("/x/--a--b--"), "a-b");
  assert.equal(projectSlug("/x/___"), "project");
  assert.equal(projectSlug(`/x/${"a".repeat(70)}`).length, 63);
});

test("lookup materializes the project and its skills in the pod's snapshot layout", async (t) => {
  const { projectDir, skillsDir, ws } = fixture(t);
  writeDescriptor(projectDir, ws.slug, "a rota planner");
  mkdirSync(join(projectDir, REFERENCES_DIR), { recursive: true });
  writeFileSync(join(projectDir, REFERENCES_DIR, "rfp.md"), "# RFP\n");
  writeFileSync(join(projectDir, REFERENCES_DIR, "brief.md"), "# Brief\n");
  const tools = new PlaygroundToolsSocket(ws, skillsDir);

  const found = await tools.lookup(ws.slug);
  assert.ok(found);
  assert.equal(found.idea, "a rota planner");
  assert.deepEqual(found.references, ["brief.md", "rfp.md"]);
  const snapshot = projectSnapshotDir(ws.mountRoot, ws.slug, found.headSha);
  assert.equal(readFileSync(join(snapshot, "specs/requirements/prd.md"), "utf8"), "# PRD\n");
  assert.ok(!existsSync(join(snapshot, "issues")), "issues/ never enters a spec snapshot");
  const skills = skillsSnapshotDir(ws.mountRoot, found.skillsSha);
  assert.match(readFileSync(join(skills, "skills/demo/SKILL.md"), "utf8"), /Body\./);
  assert.deepEqual(await tools.skills(), { skillsSha: found.skillsSha });

  // An edit is a new snapshot; the current one is the only `at` there is.
  writeFileSync(join(projectDir, "specs/requirements/prd.md"), "# PRD v2\n");
  const next = await tools.lookup(ws.slug);
  assert.notEqual(next?.headSha, found.headSha);
  assert.equal((await tools.lookup(ws.slug, next!.headSha))?.headSha, next!.headSha);
  await assert.rejects(tools.lookup(ws.slug, found.headSha), (e: unknown) => e instanceof ToolsSocketError && e.code === "ref_not_found");
});

test("another project is not this run's; a project with no descriptor has no idea", async (t) => {
  const { skillsDir, ws } = fixture(t);
  const tools = new PlaygroundToolsSocket(ws, skillsDir);
  assert.equal(await tools.lookup("someone-else"), null);
  const found = await tools.lookup(ws.slug);
  assert.equal(found?.idea, undefined);
  assert.deepEqual(found?.references, []);
});

test("MCP lists the design tools and every call says the local run has no catalog; no Room, usage dropped", async (t) => {
  const { skillsDir, ws } = fixture(t);
  const tools = new PlaygroundToolsSocket(ws, skillsDir);
  const rpc = async (body: unknown) => (await tools.mcpFetch("http://mcp.sock/mcp", { method: "POST", body: JSON.stringify(body) })).json();
  const list = (await rpc({ jsonrpc: "2.0", id: 1, method: "tools/list" })) as { result: { tools: { name: string }[] } };
  assert.deepEqual(
    list.result.tools.map((x) => x.name),
    [...DESIGN_TOOL_NAMES],
  );
  const call = (await rpc({ jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "list_external_resources", arguments: {} } })) as {
    result: { content: { text: string }[] };
  };
  assert.equal(call.result.content[0]?.text, NO_CATALOG);
  await tools.postUsage();
});
