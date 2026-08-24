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
 * Deterministic full-route SSE integration test — boots the real Express app
 * with a MOCK model (no tokens) and drives it over `fetch` against an ephemeral
 * port. Every turn is the workspace shape (§12/D9): files + skills are read from
 * a per-test fixture mount. Exercises the always-on M2M gate (shared-secret
 * path) and the per-turn `X-Anthropic-Key`. This is the deterministic
 * end-to-end gate (the real-model eval is the report, Phase 8).
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import type { LanguageModel } from "ai";
import { SignJWT } from "jose";
import { createApp } from "../src/server.js";
import { listen0 } from "../src/shared/listen.js";
import { InMemoryConversationStore } from "../src/store/memory-store.js";
import { SEED_FILES } from "./seed-files.js";
import { sha256Hex } from "../src/shared/hash.js";
import { mockModel } from "../src/shared/mock-model.js";

const OPENAPI = "specs/design/components/hello-api/openapi.yaml";
const WORKLOAD_YAML = "specs/design/components/hello-api/workload.yaml";
const REQUIREMENTS = "specs/requirements/prd.md";
const AUD = "agents-service";
const SECRET = "test-secret";
const KEY = "sk-ant-test"; // the mock buildModel ignores it; presence is what the route checks

async function boot(model: LanguageModel, workspaceMountRoot?: string) {
  const store = new InMemoryConversationStore();
  const app = createApp({
    store,
    buildModel: () => model,
    auth: { audience: AUD, secret: SECRET },
    ...(workspaceMountRoot ? { workspaceMountRoot } : {}),
  });
  const { baseUrl, close } = await listen0(app.listen(0));
  return { store, baseUrl, close };
}

// --- Workspace-shape fixtures (§12/D9) ----------------------------------------

const WS_ORG = "org-a1";
const WS_PROJ = "proj-b2";
const WS_SLUG = "spec-repo";
const WS_REF = "1".repeat(40);
const WS_SKILLS_REF = "2".repeat(40);
const WS_CONV = `org_${WS_ORG}--proj_${WS_PROJ}--requirements-generate--conv1`;

/** Materialize a fake mount: one repo snapshot (given files) + one skills snapshot. */
function makeMountRoot(files: Record<string, string | Buffer>, skillMd?: { dir: string; content: string }): string {
  const root = mkdtempSync(join(tmpdir(), "aep-srv-ws-"));
  const snapDir = join(root, "repos", WS_ORG, WS_PROJ, WS_SLUG, "snapshots", WS_REF);
  for (const [rel, content] of Object.entries(files)) {
    const abs = join(snapDir, rel);
    mkdirSync(dirname(abs), { recursive: true });
    writeFileSync(abs, content);
  }
  const skillsDir = join(root, "repos", WS_ORG, "_skills", "org-skills", "snapshots", WS_SKILLS_REF);
  mkdirSync(skillsDir, { recursive: true });
  if (skillMd) {
    const abs = join(skillsDir, "skills", skillMd.dir, "SKILL.md");
    mkdirSync(dirname(abs), { recursive: true });
    writeFileSync(abs, skillMd.content, "utf8");
  }
  return root;
}

function wsBody(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    turn: { kind: "chat", text: "do the thing" },
    workspace: {
      conversationId: WS_CONV,
      turnId: "t-1",
      repoSlug: WS_SLUG,
      ref: WS_REF,
      skillsRef: WS_SKILLS_REF,
      ...(typeof overrides.workspace === "object" ? (overrides.workspace as Record<string, unknown>) : {}),
    },
    ...Object.fromEntries(Object.entries(overrides).filter(([k]) => k !== "workspace")),
  };
}

/** Mint a shared-secret HS256 M2M token (defaults valid; override to exercise 401s). */
async function mintToken(opts: { audience?: string; secret?: string; expired?: boolean } = {}): Promise<string> {
  const jwt = new SignJWT({})
    .setProtectedHeader({ alg: "HS256" })
    .setAudience(opts.audience ?? AUD)
    .setIssuedAt()
    .setExpirationTime(opts.expired ? Math.floor(Date.now() / 1000) - 60 : "1h");
  return jwt.sign(new TextEncoder().encode(opts.secret ?? SECRET));
}

/** A turn POST carrying the M2M token and (unless omitted) the Anthropic key. */
function turnPost(body: unknown, opts: { token: string; key?: string | null; org?: string }) {
  const headers: Record<string, string> = {
    "content-type": "application/json",
    Authorization: `Bearer ${opts.token}`,
  };
  if (opts.key !== null) headers["X-Anthropic-Key"] = opts.key ?? KEY;
  if (opts.org !== undefined) headers["X-Org-Id"] = opts.org;
  return { method: "POST", headers, body: JSON.stringify(body) };
}

const delay = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

test("GET /healthz is 200 and unauthenticated", async () => {
  const { baseUrl, close } = await boot(mockModel([{ kind: "text", text: "ok" }]));
  try {
    const res = await fetch(`${baseUrl}/healthz`);
    assert.equal(res.status, 200);
    assert.deepEqual(await res.json(), { status: "ok" });
  } finally {
    await close();
  }
});

test("POST streams raw StreamPart frames + [DONE], runs execute, persists", async () => {
  const root = makeMountRoot({ [REQUIREMENTS]: "# Req\n\nHello, World!\n" });
  const { store, baseUrl, close } = await boot(
    mockModel([
      {
        kind: "toolCall",
        toolCallId: "c1",
        toolName: "editFile",
        input: { path: REQUIREMENTS, oldString: "Hello, World!", newString: "Hi there!" },
        text: "Updating.",
      },
      { kind: "text", text: "Done." },
    ]),
    root,
  );
  try {
    const token = await mintToken();
    const res = await fetch(`${baseUrl}/conversations/${WS_CONV}/turns`, turnPost(wsBody({ turn: { kind: "chat", text: "rename" } }), { token, org: WS_ORG }));
    assert.equal(res.status, 200);
    assert.match(res.headers.get("content-type") ?? "", /text\/event-stream/);

    const text = await res.text();
    assert.match(text, /"type":"tool-call"/);
    assert.match(text, /"type":"tool-result"/);
    assert.match(text, /data: \[DONE\]/);

    const stored = await store.get(WS_CONV);
    assert.ok(stored);
    assert.equal(stored.status, "done");
    assert.ok(stored.messages.some((m) => m.role === "tool"));
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});

test("GET rehydrates the aggregate; org-fenced; 404 for an unknown id", async () => {
  const root = makeMountRoot({ [REQUIREMENTS]: "# Req\n" });
  const { baseUrl, close } = await boot(mockModel([{ kind: "text", text: "ok" }]), root);
  try {
    const token = await mintToken();
    await (await fetch(`${baseUrl}/conversations/${WS_CONV}/turns`, turnPost(wsBody(), { token, org: WS_ORG }))).text();

    const headers = { Authorization: `Bearer ${token}`, "X-Org-Id": WS_ORG };
    const got = await fetch(`${baseUrl}/conversations/${WS_CONV}`, { headers });
    assert.equal(got.status, 200);
    const body = (await got.json()) as { status: string; messages: unknown[] };
    assert.equal(body.status, "done");
    assert.ok(Array.isArray(body.messages) && body.messages.length >= 2);

    // The read carries the same cross-tenant fence as the turn POST (#463):
    // the shared M2M token alone must not read another org's thread.
    const noOrg = await fetch(`${baseUrl}/conversations/${WS_CONV}`, { headers: { Authorization: `Bearer ${token}` } });
    assert.equal(noOrg.status, 403);
    const wrongOrg = await fetch(`${baseUrl}/conversations/${WS_CONV}`, {
      headers: { Authorization: `Bearer ${token}`, "X-Org-Id": "other-org" },
    });
    assert.equal(wrongOrg.status, 403);
    const malformed = await fetch(`${baseUrl}/conversations/does-not-exist`, { headers });
    assert.equal(malformed.status, 400);

    const missing = await fetch(`${baseUrl}/conversations/${WS_CONV.replace(/conv1$/, "conv9")}`, { headers });
    assert.equal(missing.status, 404);
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});

test("401 when the M2M token is missing, malformed, wrong-secret, or wrong-aud", async () => {
  const { baseUrl, close } = await boot(mockModel([{ kind: "text", text: "ok" }]));
  try {
    const url = `${baseUrl}/conversations/${WS_CONV}/turns`;
    const body = JSON.stringify(wsBody());
    const post = (headers: Record<string, string>) => fetch(url, { method: "POST", headers: { "content-type": "application/json", ...headers }, body });

    const noAuth = await post({ "X-Anthropic-Key": KEY });
    assert.equal(noAuth.status, 401);
    assert.match(noAuth.headers.get("www-authenticate") ?? "", /Bearer realm="agents-service"/);

    const malformed = await post({ Authorization: "NotBearer xyz", "X-Anthropic-Key": KEY });
    assert.equal(malformed.status, 401);

    const wrongSecret = await mintToken({ secret: "not-the-secret" });
    assert.equal((await post({ Authorization: `Bearer ${wrongSecret}`, "X-Anthropic-Key": KEY })).status, 401);

    const wrongAud = await mintToken({ audience: "some-other-service" });
    assert.equal((await post({ Authorization: `Bearer ${wrongAud}`, "X-Anthropic-Key": KEY })).status, 401);

    // GET is gated too.
    assert.equal((await fetch(`${baseUrl}/conversations/c`)).status, 401);
  } finally {
    await close();
  }
});

test("400 when X-Anthropic-Key is missing (authenticated but no key)", async () => {
  const { baseUrl, close } = await boot(mockModel([{ kind: "text", text: "ok" }]));
  try {
    const token = await mintToken();
    const res = await fetch(`${baseUrl}/conversations/${WS_CONV}/turns`, turnPost(wsBody(), { token, key: null, org: WS_ORG }));
    assert.equal(res.status, 400);
    assert.match(((await res.json()) as { error: string }).error, /X-Anthropic-Key/);
  } finally {
    await close();
  }
});

test("400 when the turn or workspace is missing; retired body shapes are rejected", async () => {
  const { baseUrl, close } = await boot(mockModel([{ kind: "text", text: "ok" }]));
  try {
    const token = await mintToken();
    const post = (body: unknown) => fetch(`${baseUrl}/conversations/${WS_CONV}/turns`, turnPost(body, { token, org: WS_ORG }));

    const noTurn = await post({ workspace: wsBody().workspace });
    assert.equal(noTurn.status, 400);
    assert.match(((await noTurn.json()) as { error: string }).error, /turn is required/);

    // The retired pre-composition contract: a stale caller must never run a
    // turn this service did not compose, nor pick its own tool set.
    const preComposed = await post(wsBody({ instruction: "Load the design skill and follow it." }));
    assert.equal(preComposed.status, 400);
    assert.match(((await preComposed.json()) as { error: string }).error, /instruction is no longer accepted/);

    const ownToolset = await post(wsBody({ toolset: "task-plan" }));
    assert.equal(ownToolset.status, 400);
    assert.match(((await ownToolset.json()) as { error: string }).error, /toolset is no longer accepted/);

    const ownEager = await post(wsBody({ eagerSkills: ["grilling"] }));
    assert.equal(ownEager.status, 400);
    assert.match(((await ownEager.json()) as { error: string }).error, /eagerSkills is no longer accepted/);

    // The deleted pre-§12 inline contract: a stale caller gets a loud 400, never a silent turn.
    const inlineFiles = await post(wsBody({ files: { "a.md": "x" } }));
    assert.equal(inlineFiles.status, 400);
    assert.match(((await inlineFiles.json()) as { error: string }).error, /files is no longer accepted/);

    const inlineSkills = await post(wsBody({ skills: [{ name: "a", description: "b", content: "c" }] }));
    assert.equal(inlineSkills.status, 400);
    assert.match(((await inlineSkills.json()) as { error: string }).error, /skills is no longer accepted/);

    const noWorkspace = await post({ turn: { kind: "chat", text: "x" } });
    assert.equal(noWorkspace.status, 400);
    assert.match(((await noWorkspace.json()) as { error: string }).error, /workspace is required/);

    // An unknown surface is a 400, never a silent fallback: the wrong answer
    // here narrates repo paths at someone who cannot see a file tree.
    const badSurface = await post(wsBody({ surface: "terminal" }));
    assert.equal(badSurface.status, 400);
    assert.match(((await badSurface.json()) as { error: string }).error, /surface must be one of: console/);
  } finally {
    await close();
  }
});

test("400 on an unparseable JSON body (the body-parser catch-all)", async () => {
  const { baseUrl, close } = await boot(mockModel([{ kind: "text", text: "ok" }]));
  try {
    const token = await mintToken();
    const res = await fetch(`${baseUrl}/conversations/${WS_CONV}/turns`, {
      method: "POST",
      headers: { "content-type": "application/json", Authorization: `Bearer ${token}`, "X-Anthropic-Key": KEY },
      body: "{not json",
    });
    assert.equal(res.status, 400);
    assert.match(((await res.json()) as { error: string }).error, /invalid request body/);
  } finally {
    await close();
  }
});


test("400 on a malformed mcp value (missing/wrong-typed url or token)", async () => {
  const root = makeMountRoot({ [REQUIREMENTS]: "# Req\n" });
  const { baseUrl, close } = await boot(mockModel([{ kind: "text", text: "ok" }]), root);
  try {
    const token = await mintToken();
    const post = (mcp: unknown) =>
      fetch(`${baseUrl}/conversations/${WS_CONV}/turns`, turnPost(wsBody({ mcp }), { token, org: WS_ORG }));

    for (const bad of [{ url: "http://x" }, { token: "t" }, { url: 1, token: "t" }, "not-an-object", null]) {
      const res = await post(bad);
      assert.equal(res.status, 400);
      assert.match(((await res.json()) as { error: string }).error, /mcp must be/);
    }
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});

// --- The turn spec (composition lives here, not in the caller) ------------------

test("a turn spec is composed server-side and streams like any turn", async () => {
  const root = makeMountRoot(SEED_FILES);
  const { baseUrl, close } = await boot(mockModel([{ kind: "text", text: "done." }]), root);
  try {
    const token = await mintToken();
    const res = await fetch(
      `${baseUrl}/conversations/${WS_CONV}/turns`,
      turnPost(
        // The caller states facts only — there is no instruction field to send.
        wsBody({ turn: { kind: "start", idea: "an expense tracker" } }),
        { token, org: WS_ORG },
      ),
    );
    assert.equal(res.status, 200);
    assert.match(await res.text(), /done\./);
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});

// --- Reference PDF attachments (#384) ----------------------------------------

test("start turn: a .pdf reference is attached to the model as a native file part", async () => {
  const pdfBytes = Buffer.from("%PDF-1.4 minimal pdf\n");
  const refPath = "specs/requirements/references/brief.pdf";
  const root = makeMountRoot({ [REQUIREMENTS]: "# Req\n", [refPath]: pdfBytes });
  const { baseUrl, close, store } = await boot(mockModel([{ kind: "text", text: "ok" }]), root);
  try {
    const token = await mintToken();
    const res = await fetch(
      `${baseUrl}/conversations/${WS_CONV}/turns`,
      turnPost(wsBody({ turn: { kind: "start", idea: "an app", references: [refPath] } }), { token, org: WS_ORG }),
    );
    assert.equal(res.status, 200);
    await res.text();

    const stored = await store.get(WS_CONV);
    const firstUser = stored!.messages.find((m) => m.role === "user")!;
    assert.ok(Array.isArray(firstUser.content), "the user message became a content array");
    const parts = firstUser.content as unknown as Array<Record<string, unknown>>;
    const filePart = parts.find((p) => p.type === "file");
    assert.ok(filePart, "expected a file part on the user message");
    assert.equal(filePart!.mediaType, "application/pdf");
    assert.equal(Buffer.from(filePart!.data as string, "base64").toString("hex"), pdfBytes.toString("hex"));
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});

test("start turn: only .md references produce no file parts (byte-identical message shape)", async () => {
  const root = makeMountRoot({ [REQUIREMENTS]: "# Req\n" });
  const { baseUrl, close, store } = await boot(mockModel([{ kind: "text", text: "ok" }]), root);
  try {
    const token = await mintToken();
    const res = await fetch(
      `${baseUrl}/conversations/${WS_CONV}/turns`,
      turnPost(wsBody({ turn: { kind: "start", idea: "an app", references: [REQUIREMENTS] } }), { token, org: WS_ORG }),
    );
    assert.equal(res.status, 200);
    await res.text();

    const stored = await store.get(WS_CONV);
    const firstUser = stored!.messages.find((m) => m.role === "user")!;
    assert.equal(typeof firstUser.content, "string", "no PDF references ⇒ the message content stays a plain string");
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});

// --- Chat attachments (#428) -------------------------------------------------

test("chat turn: an attachment rides the user message and the journal records its name", async () => {
  const root = makeMountRoot({ [REQUIREMENTS]: "# Req\n" });
  const { baseUrl, close, store } = await boot(mockModel([{ kind: "text", text: "ok" }]), root);
  try {
    const token = await mintToken();
    const res = await fetch(
      `${baseUrl}/conversations/${WS_CONV}/turns`,
      turnPost(
        wsBody({
          turn: { kind: "chat", text: "add this form as well" },
          journal: { text: "add this form as well", attachments: ["claim-form.pdf"] },
          attachments: [
            { name: "claim-form.pdf", mediaType: "application/pdf", data: Buffer.from("%PDF-1.7 x").toString("base64") },
          ],
        }),
        { token, org: WS_ORG },
      ),
    );
    assert.equal(res.status, 200);
    await res.text();

    const stored = await store.get(WS_CONV);
    const firstUser = stored!.messages.find((m) => m.role === "user")!;
    const parts = firstUser.content as unknown as Array<Record<string, unknown>>;
    const filePart = parts.find((p) => p.type === "file");
    assert.ok(filePart, "the attachment must reach the model as a file part");
    assert.equal(filePart!.filename, "claim-form.pdf");
    assert.equal(filePart!.mediaType, "application/pdf");
    // And the chip's source of truth.
    assert.deepEqual(stored!.turns[0]?.attachments, ["claim-form.pdf"]);
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});

test("chat turn: a name is journaled ONLY if its bytes actually reached the model", async () => {
  // The failure this pins: the journal used to record whatever names the caller
  // sent. When the shared encoded budget skips an attachment, that puts a chip on
  // a file the model never received — the confusion chips exist to prevent,
  // inverted. Names are derived from the surviving parts instead.
  const root = makeMountRoot({ [REQUIREMENTS]: "# Req\n" });
  const { baseUrl, close, store } = await boot(mockModel([{ kind: "text", text: "ok" }]), root);
  try {
    const token = await mintToken();
    // One byte past the whole per-turn encoded budget, so it cannot be included.
    const tooBig = "A".repeat(20 * 1024 * 1024 + 4);
    const res = await fetch(
      `${baseUrl}/conversations/${WS_CONV}/turns`,
      turnPost(
        wsBody({
          turn: { kind: "chat", text: "read this" },
          journal: { text: "read this", attachments: ["huge.pdf"] },
          attachments: [{ name: "huge.pdf", mediaType: "application/pdf", data: tooBig }],
        }),
        { token, org: WS_ORG },
      ),
    );
    assert.equal(res.status, 200);
    await res.text();

    const stored = await store.get(WS_CONV);
    const firstUser = stored!.messages.find((m) => m.role === "user")!;
    assert.equal(typeof firstUser.content, "string", "nothing was attachable, so the message stays a plain string");
    assert.equal(
      stored!.turns[0]?.attachments,
      undefined,
      "and no chip is promised for a file the model never got",
    );
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});

test("a malformed turn spec is a clean pre-stream 400, never a composed nonsense turn", async () => {
  const { baseUrl, close } = await boot(mockModel([{ kind: "text", text: "ok" }]));
  try {
    const token = await mintToken();
    const post = (turn: unknown) =>
      fetch(`${baseUrl}/conversations/${WS_CONV}/turns`, turnPost(wsBody({ turn }), { token, org: WS_ORG }));

    for (const bad of [{ kind: "flow" }, { kind: "chat" }, { kind: "generate" }, {}, "start", null]) {
      const res = await post(bad);
      assert.equal(res.status, 400, `${JSON.stringify(bad)} must not reach the model`);
      assert.match(((await res.json()) as { error: string }).error, /turn must be a valid turn spec/);
    }
  } finally {
    await close();
  }
});

test("a plan turn derives the task-plan tool set — the caller never sends toolset", async () => {
  const root = makeMountRoot(SEED_FILES);
  const { baseUrl, close } = await boot(
    mockModel([
      {
        kind: "toolCall",
        toolCallId: "p1",
        toolName: "planTask",
        input: { component: "hello-api", title: "Build hello-api", dependsOn: [], rationale: "the core service." },
      },
      { kind: "text", text: "planned." },
    ]),
    root,
  );
  try {
    const token = await mintToken();
    const res = await fetch(
      `${baseUrl}/conversations/${WS_CONV}/turns`,
      turnPost(wsBody({ turn: { kind: "plan" } }), { token, org: WS_ORG }),
    );
    assert.equal(res.status, 200);
    const text = await res.text();
    assert.match(text, /"toolName":"planTask"/, "kind:plan registered the task tools on its own");
    assert.doesNotMatch(text, /"toolName":"addFile"/);
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});


// --- Workspace reads (§12/D9) ---------------------------------------------------

test("workspace turn: reads the snapshot + skills from the mount, streams, ends with a manifest", async () => {
  const root = makeMountRoot(
    { [REQUIREMENTS]: "# Req\n\nHello, World!\n" },
    { dir: "custom/house-style", content: "---\nname: house-style\ndescription: our style\n---\n\nUSE OUR TONE.\n" },
  );
  const { baseUrl, close } = await boot(
    mockModel([
      { kind: "toolCall", toolCallId: "s1", toolName: "loadSkill", input: { names: ["house-style"] } },
      {
        kind: "toolCall",
        toolCallId: "c1",
        toolName: "editFile",
        input: { path: REQUIREMENTS, oldString: "Hello, World!", newString: "Hi there!" },
      },
      { kind: "text", text: "done" },
    ]),
    root,
  );
  try {
    const token = await mintToken();
    const res = await fetch(`${baseUrl}/conversations/${WS_CONV}/turns`, turnPost(wsBody(), { token, org: WS_ORG }));
    assert.equal(res.status, 200);
    assert.match(res.headers.get("content-type") ?? "", /text\/event-stream/);

    const text = await res.text();
    assert.match(text, /"toolName":"loadSkill"/);
    assert.match(text, /USE OUR TONE/); // the skill body was read from DISK and streamed in the tool-result
    assert.match(text, /"type":"tool-result"/);
    // The terminal manifest: touched path → sha256 of the final content, then [DONE].
    const expectedSha = sha256Hex("# Req\n\nHi there!\n");
    assert.match(text, new RegExp(`"type":"manifest".*${expectedSha}`));
    assert.match(text, /data: \[DONE\]/);
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});

test("workspace turn: the snapshot filter drops non-spec yaml (workload.yaml is NOT in CURRENT STATE)", async () => {
  const root = makeMountRoot({
    [REQUIREMENTS]: "# Req\n",
    [WORKLOAD_YAML]: "kind: Workload\n", // present on disk, filtered from the turn — arbitrary yaml stays excluded
  });
  const { baseUrl, close } = await boot(
    mockModel([
      { kind: "toolCall", toolCallId: "c1", toolName: "editFile", input: { path: WORKLOAD_YAML, oldString: "Workload", newString: "Job" } },
      { kind: "text", text: "done" },
    ]),
    root,
  );
  try {
    const token = await mintToken();
    const res = await fetch(`${baseUrl}/conversations/${WS_CONV}/turns`, turnPost(wsBody(), { token, org: WS_ORG }));
    assert.equal(res.status, 200);
    const text = await res.text();
    assert.match(text, /NO_SUCH_FILE/); // the filtered file is not editable — not in the bundle
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});

test("workspace turn: the snapshot filter admits a stored openapi.yaml (editable, IS in CURRENT STATE)", async () => {
  const root = makeMountRoot({
    [REQUIREMENTS]: "# Req\n",
    // A turn must be able to read back a spec it just stored. Complete enough to
    // clear the openapi.yaml write gate (3.x + a path + an operation) — an edit
    // against a stub with no paths is rejected on its content, which would say
    // nothing about the snapshot filter this test is about.
    [OPENAPI]:
      'openapi: 3.0.3\ninfo:\n  title: X\n  version: 0.1.0\npaths:\n  /things:\n    get:\n      responses:\n        "200":\n          description: ok\n',
  });
  const { baseUrl, close } = await boot(
    mockModel([
      { kind: "toolCall", toolCallId: "c1", toolName: "editFile", input: { path: OPENAPI, oldString: "3.0.3", newString: "3.1.0" } },
      { kind: "text", text: "done" },
    ]),
    root,
  );
  try {
    const token = await mintToken();
    const res = await fetch(`${baseUrl}/conversations/${WS_CONV}/turns`, turnPost(wsBody(), { token, org: WS_ORG }));
    assert.equal(res.status, 200);
    const text = await res.text();
    assert.doesNotMatch(text, /NO_SUCH_FILE/);
    assert.match(text, /"status":"applied"/); // the edit against the admitted spec actually applied
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});

test("workspace fence at the route: org mismatch/missing → 403; fuzz/unknown-sha/skills-in-body → 400", async () => {
  const root = makeMountRoot({ [REQUIREMENTS]: "# Req\n" });
  const { baseUrl, close } = await boot(mockModel([{ kind: "text", text: "ok" }]), root);
  try {
    const token = await mintToken();
    const post = (body: unknown, org?: string, urlId = WS_CONV) =>
      fetch(`${baseUrl}/conversations/${urlId}/turns`, turnPost(body, { token, ...(org !== undefined ? { org } : {}) }));

    // The IDOR fence: X-Org-Id is load-bearing.
    assert.equal((await post(wsBody(), "another-org")).status, 403);
    assert.equal((await post(wsBody())).status, 403); // header missing entirely

    // ID fuzz → pre-stream 400.
    const traversal = `org_..--proj_${WS_PROJ}--chat--u`;
    assert.equal((await post(wsBody({ workspace: { conversationId: traversal } }), "..", traversal)).status, 400);
    assert.equal((await post(wsBody({ workspace: { repoSlug: "../escape" } }), WS_ORG)).status, 400);
    assert.equal((await post(wsBody({ workspace: { ref: "zz" } }), WS_ORG)).status, 400);

    // Well-formed but unknown sha → 400 (stat fail).
    assert.equal((await post(wsBody({ workspace: { ref: "f".repeat(40) } }), WS_ORG)).status, 400);

    // URL :id ≠ body conversationId → 400.
    assert.equal((await post(wsBody(), WS_ORG, `org_${WS_ORG}--proj_${WS_PROJ}--chat--other`)).status, 400);

    // skills cannot ride along in the body (loaded from the skills snapshot).
    const withSkills = wsBody({ skills: [{ name: "a", description: "b", content: "c" }] });
    assert.equal((await post(withSkills, WS_ORG)).status, 400);
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});

test("409 when a turn is already in flight for the id", async () => {
  const root = makeMountRoot({ [REQUIREMENTS]: "# Req\n" });
  // delayMs keeps turn 1 in-flight so turn 2 hits the in-flight guard.
  const { baseUrl, close } = await boot(
    mockModel([{ kind: "text", text: "a" }, { kind: "text", text: "b" }], { delayMs: 80 }),
    root,
  );
  try {
    const token = await mintToken();
    const opts = turnPost(wsBody(), { token, org: WS_ORG });
    const p1 = fetch(`${baseUrl}/conversations/${WS_CONV}/turns`, opts).then(async (r) => {
      await r.text();
      return r.status;
    });
    await delay(15); // ensure turn 1 acquires the lock first
    const p2 = fetch(`${baseUrl}/conversations/${WS_CONV}/turns`, opts).then(async (r) => {
      await r.text();
      return r.status;
    });

    const [s1, s2] = await Promise.all([p1, p2]);
    assert.deepEqual([s1, s2].sort(), [200, 409]);
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
});

// --- Narration policy (#580) ---------------------------------------------------

const CONSOLE_SKILL_MD = `---
name: console
description: How the agent speaks to someone working in the console.
metadata:
  aep:
    kind: platform
    audience: [design]
---

## Never quote a repo path

Name the artifact instead.
`;

/** The system instructions the model actually received for the turn's first step. */
function systemPrompt(model: ReturnType<typeof mockModel>): string {
  const prompt = model.doStreamCalls[0]!.prompt as unknown as { role: string; content: unknown }[];
  const system = prompt.find((m) => m.role === "system");
  return typeof system?.content === "string" ? system.content : JSON.stringify(system?.content ?? "");
}

test("a console turn carries the narration policy; the same turn without a surface does not", async () => {
  const root = makeMountRoot({ [REQUIREMENTS]: "# Req\n" }, { dir: "console", content: CONSOLE_SKILL_MD });
  const consoleModel = mockModel([{ kind: "text", text: "ok" }]);
  const localModel = mockModel([{ kind: "text", text: "ok" }]);
  const consoleRun = await boot(consoleModel, root);
  const localRun = await boot(localModel, root);
  try {
    const token = await mintToken();
    const post = (baseUrl: string, body: unknown) =>
      fetch(`${baseUrl}/conversations/${WS_CONV}/turns`, turnPost(body, { token, org: WS_ORG }));

    assert.equal((await post(consoleRun.baseUrl, wsBody({ surface: "console" }))).status, 200);
    const consolePrompt = systemPrompt(consoleModel);
    assert.match(consolePrompt, /# Narration policy/);
    assert.match(consolePrompt, /Never quote a repo path/, "the BODY rides the prompt — nothing loads it");
    // Standing policy, not a task: offering it in the catalog would invite a
    // round-trip returning text the agent is already holding.
    assert.doesNotMatch(consolePrompt, /- console:/);

    // The local run reads the SAME skills snapshot and names no surface, so the
    // rules stay absent — which is what keeps the shared flow skills usable in
    // a terminal, where a repo path is the right word.
    assert.equal((await post(localRun.baseUrl, wsBody())).status, 200);
    assert.doesNotMatch(systemPrompt(localModel), /Narration policy/);
  } finally {
    await consoleRun.close();
    await localRun.close();
    rmSync(root, { recursive: true, force: true });
  }
});
