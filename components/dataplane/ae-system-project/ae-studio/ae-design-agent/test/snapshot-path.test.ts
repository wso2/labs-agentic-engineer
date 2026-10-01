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
 * The structural-fence unit gate (§12/§17.5): ID fuzz (traversal, separators,
 * oversize, wrong charset) → 400; org-segment ≠ claim → 403; unknown snapshot
 * sha (stat fail) → 400 — all pre-stream, all before any fs read.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync } from "node:fs";
import { join, sep } from "node:path";
import { tmpdir } from "node:os";
import { resolveWorkspace, WorkspaceRefError } from "../src/shared/snapshot-path.js";

const ORG = "org-1a2b";
const PROJ = "proj-3c4d";
const SLUG = "spec-repo";
const REF = "a".repeat(40);
const SKILLS_REF = "b".repeat(40);
const CONV = `org_${ORG}--proj_${PROJ}--requirements-generate--u-123`;

/** A mount root holding exactly the two snapshot dirs the valid request needs. */
function makeRoot(): string {
  const root = mkdtempSync(join(tmpdir(), "aep-fence-"));
  mkdirSync(join(root, "repos", ORG, PROJ, SLUG, "snapshots", REF), { recursive: true });
  mkdirSync(join(root, "repos", ORG, "_skills", "org-skills", "snapshots", SKILLS_REF), { recursive: true });
  return root;
}

function validWorkspace(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    conversationId: CONV,
    turnId: "turn-1",
    repoSlug: SLUG,
    ref: REF,
    skillsRef: SKILLS_REF,
    ...overrides,
  };
}

function resolve(root: string, opts: { workspace?: Record<string, unknown>; idParam?: string; org?: string } = {}) {
  return resolveWorkspace({
    conversationIdParam: opts.idParam ?? ((opts.workspace?.conversationId as string) ?? CONV),
    workspace: opts.workspace ?? validWorkspace(),
    orgIdClaim: opts.org ?? ORG,
    mountRoot: root,
  });
}

function expectStatus(root: string, status: 400 | 403, opts: Parameters<typeof resolve>[1]): void {
  try {
    resolve(root, opts);
    assert.fail(`expected WorkspaceRefError(${status})`);
  } catch (err) {
    assert.ok(err instanceof WorkspaceRefError, `expected WorkspaceRefError, got ${String(err)}`);
    assert.equal(err.status, status, `wrong status for ${JSON.stringify(opts)}: ${err.message}`);
  }
}

test("valid workspace derives both snapshot dirs under the mount root", () => {
  const root = makeRoot();
  try {
    const ws = resolve(root);
    assert.equal(ws.orgId, ORG);
    assert.equal(ws.projectId, PROJ);
    assert.equal(ws.useCase, "requirements-generate");
    assert.equal(ws.snapshotDir, join(root, "repos", ORG, PROJ, SLUG, "snapshots", REF));
    assert.equal(ws.skillsSnapshotDir, join(root, "repos", ORG, "_skills", "org-skills", "snapshots", SKILLS_REF));
    // The derivation never leaves the root.
    assert.ok(ws.snapshotDir.startsWith(root + sep));
    assert.ok(ws.skillsSnapshotDir.startsWith(root + sep));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("conversationId fuzz → 400 (shape, traversal, separators, charset, oversize)", () => {
  const root = makeRoot();
  try {
    const badIds = [
      "", // empty
      "plain-legacy-id", // no segments
      `proj_${PROJ}--org_${ORG}--chat--u`, // segments swapped
      `org_${ORG}--proj_${PROJ}--chat`, // 3 segments
      `org_${ORG}--proj_${PROJ}--chat--u--extra`, // 5 segments
      `org_..--proj_${PROJ}--chat--u`, // org traversal
      `org_${ORG}--proj_..--chat--u`, // proj traversal
      `org_a/b--proj_${PROJ}--chat--u`, // separator in org
      `org_${ORG}--proj_a\\b--chat--u`, // backslash in proj
      `org_${ORG}--proj_${PROJ}--Chat--u`, // uppercase use case
      `org_${ORG}--proj_${PROJ}--chat_x--u`, // underscore in use case
      `org_${ORG}--proj_${PROJ}--chat--u!`, // charset in uuid
      `org_--proj_${PROJ}--chat--u`, // empty org payload
      `org_${ORG}--proj_--chat--u`, // empty proj payload
      `org_.--proj_${PROJ}--chat--u`, // dot-only org
      `org_.hidden--proj_${PROJ}--chat--u`, // dot-led org
      `org_${"x".repeat(200)}--proj_${PROJ}--chat--u`, // oversized org segment
      `org_${ORG}--proj_${PROJ}--chat--${"u".repeat(400)}`, // oversized total
      `org_${ORG}\0--proj_${PROJ}--chat--u`, // NUL
    ];
    for (const conversationId of badIds) {
      expectStatus(root, 400, { workspace: validWorkspace({ conversationId }), idParam: conversationId });
    }
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("URL :id must equal the body conversationId → 400", () => {
  const root = makeRoot();
  try {
    expectStatus(root, 400, { workspace: validWorkspace(), idParam: `org_${ORG}--proj_${PROJ}--chat--other` });
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("turnId / repoSlug / refs fuzz → 400", () => {
  const root = makeRoot();
  try {
    const cases: Record<string, unknown>[] = [
      { turnId: "" },
      { turnId: "a/b" },
      { turnId: ".." },
      { turnId: "x".repeat(200) },
      { repoSlug: "" },
      { repoSlug: "UPPER" },
      { repoSlug: "-leads-dash" },
      { repoSlug: "a/b" },
      { repoSlug: "a..b" },
      { repoSlug: ".hidden" },
      { repoSlug: "x".repeat(200) },
      { ref: "not-a-sha" },
      { ref: REF.slice(0, 39) }, // 39 hex
      { ref: `${REF.slice(0, 39)}G` }, // non-hex char
      { ref: REF.toUpperCase() }, // uppercase hex rejected
      { ref: `../${REF.slice(4)}` },
      { skillsRef: "1234" },
      { skillsRef: SKILLS_REF.slice(0, 39) },
      // missing / non-string fields
      { turnId: undefined },
      { ref: undefined },
      { skillsRef: 42 },
    ];
    for (const overrides of cases) {
      expectStatus(root, 400, { workspace: validWorkspace(overrides) });
    }
    // Non-object workspace values.
    for (const workspace of [null, [], "str", 7]) {
      try {
        resolveWorkspace({ conversationIdParam: CONV, workspace, orgIdClaim: ORG, mountRoot: root });
        assert.fail("expected WorkspaceRefError(400)");
      } catch (err) {
        assert.ok(err instanceof WorkspaceRefError);
        assert.equal(err.status, 400);
      }
    }
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("org segment ≠ X-Org-Id claim → 403 (the cross-tenant fence); missing claim → 403", () => {
  const root = makeRoot();
  try {
    expectStatus(root, 403, { org: "some-other-org" });
    expectStatus(root, 403, { org: "" });
    // undefined claim
    try {
      resolveWorkspace({ conversationIdParam: CONV, workspace: validWorkspace(), orgIdClaim: undefined, mountRoot: root });
      assert.fail("expected 403");
    } catch (err) {
      assert.ok(err instanceof WorkspaceRefError);
      assert.equal(err.status, 403);
    }
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("well-formed but unknown snapshot shas (stat fail) → 400, for repo AND skills", () => {
  const root = makeRoot();
  try {
    expectStatus(root, 400, { workspace: validWorkspace({ ref: "c".repeat(40) }) });
    expectStatus(root, 400, { workspace: validWorkspace({ skillsRef: "d".repeat(40) }) });
    // Unknown repoSlug/org/proj all reduce to a stat fail too.
    expectStatus(root, 400, { workspace: validWorkspace({ repoSlug: "other-repo" }) });
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
