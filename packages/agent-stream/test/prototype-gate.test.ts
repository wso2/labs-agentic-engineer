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
 * The prototype write gate: the manifest and the source are judged by the kit's
 * rules, the manifest first, and every refusal is INVALID_PROTOTYPE carrying
 * the kit's findings. The render check is the host's, so it is a stub here; the
 * agents service tests the real one. A manifest written beside an existing
 * source is judged against that source too.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  FileBundle,
  writeWithRenderCheck,
  type OpErr,
  type OpOk,
  type OpResult,
  type PrototypeFileTexts,
  type PrototypeFinding,
  type PrototypeRenderCheck,
} from "../src/index.js";

const MANIFEST_PATH = "specs/design/components/web/prototype.json";
const SOURCE_PATH = "specs/design/components/web/prototype.tsx";

const MANIFEST = JSON.stringify({
  schemaVersion: 3,
  name: "Demo",
  entryScreen: "screen.home",
  roles: [{ id: "user", name: "User" }],
  states: [{ id: "state.default", name: "Default" }],
  screens: [{ id: "screen.home", name: "Home", roleIds: ["user"] }],
  flows: [],
});

const SOURCE = `
import { Heading, Screen, defineApp } from "@wso2/prototype-kit";

function Home() {
  return (
    <Screen>
      <Heading id="heading.home" text="Home" />
    </Screen>
  );
}

export default defineApp({ screens: { "screen.home": Home } });
`;

function manifestWith(patch: Record<string, unknown>): string {
  return JSON.stringify({ ...JSON.parse(MANIFEST), ...patch });
}

function refused(r: OpResult): OpErr {
  assert.equal(r.ok, false, `expected a refusal, got ${JSON.stringify(r)}`);
  return r as OpErr;
}

function applied(r: OpResult): OpOk {
  assert.equal(r.ok, true, `expected ok, got ${JSON.stringify(r)}`);
  return r as OpOk;
}

function codes(r: OpErr): string[] {
  return (r.findings ?? []).map((f) => f.code);
}

test("a valid manifest then a valid source land", () => {
  const b = new FileBundle({});
  assert.equal(applied(b.addFile(MANIFEST_PATH, MANIFEST)).status, "applied");
  assert.equal(applied(b.addFile(SOURCE_PATH, SOURCE)).status, "applied");
  assert.equal(b.read(SOURCE_PATH), SOURCE.replace(/\r\n/g, "\n"));
});

const MANIFEST_REFUSALS: { name: string; content: string; code: string; location?: string }[] = [
  { name: "not JSON", content: "{ nope", code: "SCHEMA_VIOLATION", location: "(root)" },
  { name: "another schema version", content: manifestWith({ schemaVersion: 2 }), code: "UNSUPPORTED_VERSION", location: "schemaVersion" },
  { name: "a missing required key", content: manifestWith({ roles: undefined }), code: "SCHEMA_VIOLATION" },
  { name: "the retired component key", content: manifestWith({ component: "web" }), code: "SCHEMA_VIOLATION" },
  { name: "a duplicate id", content: manifestWith({ states: [{ id: "user", name: "Clash" }] }), code: "DUPLICATE_ID" },
  { name: "an unknown entry screen", content: manifestWith({ entryScreen: "screen.gone" }), code: "UNKNOWN_REFERENCE", location: "entryScreen" },
];

for (const row of MANIFEST_REFUSALS) {
  test(`prototype.json refused: ${row.name}`, () => {
    const b = new FileBundle({});
    const r = refused(b.addFile(MANIFEST_PATH, row.content));
    assert.equal(r.code, "INVALID_PROTOTYPE");
    assert.ok(codes(r).includes(row.code), `findings were ${JSON.stringify(r.findings)}`);
    if (row.location) assert.ok(r.findings!.some((f) => f.code === row.code && f.location === row.location));
    assert.ok(r.findings!.every((f) => f.file === "prototype.json"));
    assert.match(r.message, new RegExp(row.code));
    assert.equal(b.has(MANIFEST_PATH), false, "a refused write leaves nothing");
  });
}

test("prototype.tsx before its manifest is refused and says to write the manifest first", () => {
  const b = new FileBundle({});
  const r = refused(b.addFile(SOURCE_PATH, SOURCE));
  assert.equal(r.code, "INVALID_PROTOTYPE");
  assert.deepEqual(codes(r), ["MISSING_FILE"]);
  assert.match(r.message, /write it first/);
  assert.equal(b.has(SOURCE_PATH), false);
});

test("prototype.tsx against an invalid manifest in the bundle names the manifest's findings", () => {
  const b = new FileBundle({ [MANIFEST_PATH]: manifestWith({ entryScreen: "screen.gone" }) });
  const r = refused(b.addFile(SOURCE_PATH, SOURCE));
  assert.deepEqual(codes(r), ["UNKNOWN_REFERENCE"]);
  assert.match(r.message, /fix it first/);
});

const SOURCE_REFUSALS: { name: string; content: string; code: string }[] = [
  { name: "a syntax error", content: SOURCE + "\nconst = ;", code: "SYNTAX_ERROR" },
  { name: "an import outside the kit", content: SOURCE + 'import fs from "node:fs";', code: "FORBIDDEN_IMPORT" },
  { name: "a network call", content: SOURCE + '\nfetch("/x");', code: "FORBIDDEN_API" },
  { name: "a raw element", content: SOURCE.replace("<Screen>", "<div>").replace("</Screen>", "</div>"), code: "FORBIDDEN_ELEMENT" },
  { name: "a size over the cap", content: SOURCE + "\n// " + "x".repeat(262144), code: "SOURCE_TOO_LARGE" },
];

for (const row of SOURCE_REFUSALS) {
  test(`prototype.tsx refused: ${row.name}`, () => {
    const b = new FileBundle({ [MANIFEST_PATH]: MANIFEST });
    const r = refused(b.addFile(SOURCE_PATH, row.content));
    assert.equal(r.code, "INVALID_PROTOTYPE");
    assert.ok(codes(r).includes(row.code), `findings were ${JSON.stringify(r.findings)}`);
    assert.ok(r.findings!.every((f) => f.file === "prototype.tsx"));
    assert.equal(b.has(SOURCE_PATH), false);
  });
}

test("a literal navigation target the manifest does not have is refused with its line", () => {
  const b = new FileBundle({ [MANIFEST_PATH]: MANIFEST });
  const bad = SOURCE.replace('import { Heading,', 'import { Button, Heading,').replace("</Screen>", '<Button id="btn.go" label="Go" to="screen.nowhere" /></Screen>');
  const r = refused(b.addFile(SOURCE_PATH, bad));
  assert.ok(codes(r).includes("UNKNOWN_NAV_TARGET"), JSON.stringify(r.findings));
  assert.match(r.findings![0]!.location, /^line \d+$/);
});

test("an editFile that breaks the source is refused and leaves it unchanged", () => {
  const b = new FileBundle({ [MANIFEST_PATH]: MANIFEST, [SOURCE_PATH]: SOURCE });
  const r = refused(b.editFile(SOURCE_PATH, "<Screen>", '<Screen onClick={() => fetch("/x")}>'));
  assert.equal(r.code, "INVALID_PROTOTYPE");
  assert.equal(b.read(SOURCE_PATH), SOURCE);
});

test("a listing over eight findings is summarized in the message but complete in findings", () => {
  const b = new FileBundle({});
  const states = Array.from({ length: 10 }, (_, i) => ({ id: "user", name: `Clash ${i}` }));
  const r = refused(b.addFile(MANIFEST_PATH, manifestWith({ states })));
  assert.ok(r.findings!.length > 8);
  assert.match(r.message, /and \d+ more/);
});

const RENDER_FINDING: PrototypeFinding = {
  code: "RENDER_FAILED",
  file: "prototype.tsx",
  location: "screen.home as user in state.default",
  message: "boom",
};

/** A stub render check: records the pairs it was asked to draw, answers `findings`. */
function stubRender(findings: PrototypeFinding[] = []): { render: PrototypeRenderCheck; seen: PrototypeFileTexts[] } {
  const seen: PrototypeFileTexts[] = [];
  return {
    seen,
    render: async (files) => {
      seen.push(files);
      return findings;
    },
  };
}

test("the render check runs after the static stages, with the manifest and source texts, and its findings refuse the write", async () => {
  const { render, seen } = stubRender([RENDER_FINDING]);
  const b = new FileBundle({ [MANIFEST_PATH]: MANIFEST });
  const r = refused(await writeWithRenderCheck(b, { op: "add", path: SOURCE_PATH, content: SOURCE }, render));
  assert.equal(r.code, "INVALID_PROTOTYPE");
  assert.deepEqual(r.findings, [RENDER_FINDING]);
  assert.match(r.message, /RENDER_FAILED in prototype\.tsx at screen\.home as user in state\.default: boom/);
  assert.deepEqual(seen, [{ manifest: MANIFEST, source: SOURCE }]);
  assert.equal(b.has(SOURCE_PATH), false);
});

test("the render check is not asked when a static stage already refused, nor for a manifest with no source beside it", async () => {
  const { render, seen } = stubRender();
  const b = new FileBundle({});
  applied(await writeWithRenderCheck(b, { op: "add", path: MANIFEST_PATH, content: MANIFEST }, render));
  refused(await writeWithRenderCheck(b, { op: "add", path: SOURCE_PATH, content: SOURCE + '\nfetch("/x");' }, render));
  assert.equal(seen.length, 0);
  applied(await writeWithRenderCheck(b, { op: "add", path: SOURCE_PATH, content: SOURCE }, render));
  assert.equal(seen.length, 1);
});

test("an edit is drawn as the file it would leave", async () => {
  const { render, seen } = stubRender();
  const b = new FileBundle({ [MANIFEST_PATH]: MANIFEST, [SOURCE_PATH]: SOURCE });
  applied(await writeWithRenderCheck(b, { op: "edit", path: SOURCE_PATH, oldString: 'text="Home"', newString: 'text="Start"' }, render));
  assert.equal(seen[0]!.source, SOURCE.replace('text="Home"', 'text="Start"'));
  assert.equal(b.read(SOURCE_PATH), seen[0]!.source);
});

test("a write the bundle answers without storing (a no-op, a failed anchor) is never drawn", async () => {
  const { render, seen } = stubRender([RENDER_FINDING]);
  const b = new FileBundle({ [MANIFEST_PATH]: MANIFEST, [SOURCE_PATH]: SOURCE });
  assert.equal(applied(await writeWithRenderCheck(b, { op: "add", path: SOURCE_PATH, content: SOURCE }, render)).status, "noop");
  assert.equal(refused(await writeWithRenderCheck(b, { op: "edit", path: SOURCE_PATH, oldString: "absent", newString: "x" }, render)).code, "NOT_FOUND");
  assert.equal(seen.length, 0);
});

test("without a render check the source gets its static checks only", () => {
  const b = new FileBundle({ [MANIFEST_PATH]: MANIFEST });
  applied(b.addFile(SOURCE_PATH, SOURCE));
});

// --- A manifest written beside an existing source -------------------------

const LINKED_MANIFEST = manifestWith({
  screens: [
    { id: "screen.home", name: "Home", roleIds: ["user"] },
    { id: "screen.about", name: "About", roleIds: ["user"] },
  ],
});
const LINKED_SOURCE = SOURCE.replace('<Heading id="heading.home" text="Home" />', '<Heading id="heading.home" text="Home" />\n      <Button id="btn.about" label="About" to="screen.about" />')
  .replace("import { Heading,", "import { Button, Heading,");

test("a manifest that drops a screen the existing source links to is refused with the source's findings", () => {
  const b = new FileBundle({ [MANIFEST_PATH]: LINKED_MANIFEST, [SOURCE_PATH]: LINKED_SOURCE });
  applied(b.removeFile(MANIFEST_PATH));
  const r = refused(b.addFile(MANIFEST_PATH, MANIFEST));
  assert.equal(r.code, "INVALID_PROTOTYPE");
  assert.equal(r.findings?.[0]?.file, "prototype.tsx");
  assert.match(r.message, /prototype\.json would break specs\/design\/components\/web\/prototype\.tsx/);
  assert.match(r.message, /screen\.about/);
});

test("an edit to the manifest is judged against the existing source too", () => {
  const b = new FileBundle({ [MANIFEST_PATH]: LINKED_MANIFEST, [SOURCE_PATH]: LINKED_SOURCE });
  refused(b.editFile(MANIFEST_PATH, '{"id":"screen.about","name":"About","roleIds":["user"]}', '{"id":"screen.help","name":"Help","roleIds":["user"]}'));
  assert.equal(b.read(MANIFEST_PATH), LINKED_MANIFEST);
});

test("a manifest change that keeps the source's links lands", () => {
  const b = new FileBundle({ [MANIFEST_PATH]: LINKED_MANIFEST, [SOURCE_PATH]: LINKED_SOURCE });
  applied(b.editFile(MANIFEST_PATH, '"name":"About"', '"name":"About us"'));
});

test("a manifest written beside an existing source is drawn with it, and refused when the pair fails to draw", async () => {
  const { render, seen } = stubRender([{ ...RENDER_FINDING, code: "SCREEN_MISMATCH", location: "module" }]);
  const b = new FileBundle({ [MANIFEST_PATH]: MANIFEST, [SOURCE_PATH]: SOURCE });
  const changed = manifestWith({ name: "Renamed" });
  const r = refused(await writeWithRenderCheck(b, { op: "edit", path: MANIFEST_PATH, oldString: '"name":"Demo"', newString: '"name":"Renamed"' }, render));
  assert.deepEqual(seen, [{ manifest: changed, source: SOURCE }]);
  assert.match(r.message, /prototype\.json would break/);
  assert.equal(r.findings?.[0]?.code, "SCREEN_MISMATCH");
  assert.equal(b.read(MANIFEST_PATH), MANIFEST);
});

const NOT_PROTOTYPE_PATHS = [
  "specs/design/components/web/prototype.json.bak",
  "specs/design/components/web/sub/prototype.tsx",
  "specs/design/prototype.json",
  "specs/design/components/web/notes.tsx",
];

for (const path of NOT_PROTOTYPE_PATHS) {
  test(`${path} is not a prototype file, so the gate leaves it alone`, () => {
    const b = new FileBundle({});
    assert.equal(applied(b.addFile(path, "not a prototype")).status, "applied");
  });
}
