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
 * The committer over the Files socket (07 §11): a room's live doc lands as
 * one commit per flush, with no token. Every test runs against the fake Files
 * socket, so the baseline shas are the real git blob shas a bundle returns.
 */

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";
import type { Document } from "@hocuspocus/server";
import { deleteDocFile, readDocFile, setDocFile, setDocFileAsAgent } from "@aep/collab-doc";
import { flushAllRooms, flushRoom, pendingChanges, seedBaseline } from "./committer.js";
import {
  ApplyConflictError,
  createFilesClient,
  FilesDeniedError,
  FilesUnavailableError,
  type ApplyBatch,
  type ApplyWarning,
  type FilesClient,
} from "./files-client.js";
import { startFakeFilesSocket, type FakeFilesSocket } from "./fake-files-socket.js";
import type { PodLogLine } from "./pod/log.js";
import { addParticipant, dropRoomState, ensureRoomState, roomState } from "./rooms.js";
import { seedDocument } from "./seed.js";

const open: { fake: FakeFilesSocket; rooms: string[] }[] = [];

afterEach(async () => {
  for (const { fake, rooms } of open.splice(0)) {
    for (const room of rooms) dropRoomState(room);
    await fake.close();
  }
});

async function fakeSocket(files: Record<string, string>, warnings?: ApplyWarning[]): Promise<FakeFilesSocket> {
  const fake = await startFakeFilesSocket({ files, ...(warnings ? { warnings } : {}) });
  open.push({ fake, rooms: [] });
  return fake;
}

interface SeededRoom {
  name: string;
  doc: Document;
  edit(path: string, content: string): void;
  text(path: string): string | undefined;
}

/** A room seeded from the socket's bundle, as the Room's load hook seeds it. */
async function seededRoom(files: FilesClient, name: string, project: string): Promise<SeededRoom> {
  const doc = new Y.Doc() as Document;
  const bundle = await files.bundle(project);
  seedDocument(doc, bundle);
  seedBaseline(ensureRoomState(name, project), doc, bundle);
  open.at(-1)?.rooms.push(name);
  return {
    name,
    doc,
    edit: (path, content) => setDocFile(doc, path, content),
    text: (path) => readDocFile(doc, path),
  };
}

/** The batches the committer sent, in order. */
function recording(files: FilesClient): { files: FilesClient; batches: ApplyBatch[] } {
  const batches: ApplyBatch[] = [];
  return {
    batches,
    files: {
      ...files,
      apply: (project, batch) => {
        batches.push(batch);
        return files.apply(project, batch);
      },
    },
  };
}

const PRD = "specs/requirements/prd.md";
const ARCH = "specs/design/arch.excalidraw";
// The PRD ends in a newline, as git files do; the room's serializer drops it.
const SEED = { [PRD]: "# PRD\n\nSeeded.\n", [ARCH]: '{"v":1}' };

test("flush commits with no token, broadcasts warnings, and keeps the doc live on 503", async () => {
  const fake = await startFakeFilesSocket({ files: { "specs/a.md": "a" }, warnings: [{ path: "specs/a.md", message: "soft" }] });
  open.push({ fake, rooms: [] });
  const room = await seededRoom(createFilesClient(fake.path), "spec-acme-greeter", "greeter");
  room.edit("specs/a.md", "b");
  const sent: string[] = [];
  await flushRoom({ files: createFilesClient(fake.path), onWarnings: (w) => sent.push(JSON.stringify(w)) }, room.name, room.doc);
  assert.equal(fake.commits().length, 1);
  assert.deepEqual(JSON.parse(sent[0]!), [{ path: "specs/a.md", message: "soft" }]);

  room.edit("specs/a.md", "c");
  fake.failNext(503, "disk_full");
  await assert.rejects(flushRoom({ files: createFilesClient(fake.path) }, room.name, room.doc), FilesUnavailableError);
  assert.equal(room.text("specs/a.md"), "c", "doc stays live; the next debounce retries");
  await flushRoom({ files: createFilesClient(fake.path) }, room.name, room.doc);
  assert.equal(fake.commits().length, 2);
});

test("a clean room sends no apply and reports no warnings", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const room = await seededRoom(files, "spec-acme-shop", "shop");
  const sent: ApplyWarning[][] = [];
  await flushRoom({ files, onWarnings: (w) => sent.push(w) }, room.name, room.doc);
  assert.equal(fake.requests.filter((r) => r.method === "POST").length, 0);
  assert.deepEqual(sent, []);
});

test("every successful apply reports its warnings, an empty list included", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const room = await seededRoom(files, "spec-acme-shop", "shop");
  room.edit(ARCH, '{"v":2}');
  const sent: ApplyWarning[][] = [];
  await flushRoom({ files, onWarnings: (w) => sent.push(w) }, room.name, room.doc);
  assert.deepEqual(sent, [[]], "an empty list clears the console's Alert");
});

test("flushes only the changed files, preconditioned on the seeded shas, with co-author trailers", async () => {
  const fake = await fakeSocket(SEED);
  const rec = recording(createFilesClient(fake.path));
  const room = await seededRoom(rec.files, "spec-acme-shop", "shop");
  const seeded = new Map((await rec.files.bundle("shop")).map((f) => [f.path, f.sha]));
  addParticipant(room.name, { name: "Mark", email: "mark@x.io" });
  addParticipant(room.name, { name: "John", email: "john@x.io" });
  room.edit(ARCH, '{"v":2}');
  room.edit("specs/validation/plan.txt", "check\n");

  await flushRoom({ files: rec.files }, room.name, room.doc);

  assert.equal(rec.batches.length, 1);
  const batch = rec.batches[0]!;
  assert.deepEqual(batch.writes.map((w) => w.path).sort(), [ARCH, "specs/validation/plan.txt"]);
  assert.equal(batch.writes.find((w) => w.path === ARCH)!.baseSha, seeded.get(ARCH));
  assert.equal(batch.writes.find((w) => w.path === "specs/validation/plan.txt")!.baseSha, "", "a new file must not exist yet");
  assert.match(batch.message, /^collab session\n\nCo-authored-by: John <john@x\.io>\nCo-authored-by: Mark <mark@x\.io>$/);
  assert.equal(fake.commits()[0]!.message, batch.message, "the trailers reach the commit");
});

test("the baseline advances after a flush: the next one is a no-op", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const room = await seededRoom(files, "spec-acme-shop", "shop");
  room.edit(ARCH, '{"v":2}');
  await flushRoom({ files }, room.name, room.doc);
  await flushRoom({ files }, room.name, room.doc);
  assert.equal(fake.commits().length, 1);
  // And the advanced sha is the one git holds: the next change applies cleanly.
  room.edit(ARCH, '{"v":3}');
  await flushRoom({ files }, room.name, room.doc);
  assert.equal(fake.commits().length, 2);
  assert.equal(fake.file(ARCH), '{"v":3}');
});

// A room that already holds reference-document entries (seeded before the
// exclusion existed) must not write them back, and a reference in the
// baseline must never be deleted for being absent from the doc.
test("reference documents are never written or deleted", async () => {
  const fake = await fakeSocket(SEED);
  const rec = recording(createFilesClient(fake.path));
  const room = await seededRoom(rec.files, "spec-acme-shop", "shop");
  roomState(room.name)!.baseline.set("specs/requirements/references/old.pdf", { content: "JVBERi0xLjQK", sha: "sha-pdf" });
  room.edit("specs/requirements/references/rfp.pdf", "JVBERi0xLjQK");
  room.edit(ARCH, '{"v":2}');

  await flushRoom({ files: rec.files }, room.name, room.doc);

  assert.deepEqual(rec.batches[0]!.writes.map((w) => w.path), [ARCH]);
  assert.deepEqual(rec.batches[0]!.deletes, []);
});

test("a non-markdown file removed from the doc is deleted against its seeded sha", async () => {
  const fake = await fakeSocket(SEED);
  const rec = recording(createFilesClient(fake.path));
  const room = await seededRoom(rec.files, "spec-acme-shop", "shop");
  const archSha = roomState(room.name)!.baseline.get(ARCH)!.sha;
  deleteDocFile(room.doc, ARCH);
  await flushRoom({ files: rec.files }, room.name, room.doc);
  assert.deepEqual(rec.batches[0]!.deletes, [{ path: ARCH, baseSha: archSha }]);
  assert.equal(fake.file(ARCH), undefined);
});

test("participants without an email get the noreply trailer address", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const room = await seededRoom(files, "spec-acme-shop", "shop");
  addParticipant(room.name, { name: "Chris", email: "" });
  room.edit(ARCH, '{"v":2}');
  await flushRoom({ files }, room.name, room.doc);
  assert.match(fake.commits()[0]!.message, /Co-authored-by: Chris <Chris@users\.noreply\.aep\.dev>/);
});

test("an interim flush holds markdown with pending agent marks; a forced flush commits it", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const room = await seededRoom(files, "spec-acme-shop", "shop");
  setDocFileAsAgent(
    room.doc as unknown as Parameters<typeof setDocFileAsAgent>[0],
    PRD,
    "# PRD\n\nSeeded. Agent addition.",
    "agent",
    { agent: "Spec Agent", at: "2026-07-08T00:00:00Z" },
  );

  await flushRoom({ files }, room.name, room.doc);
  assert.equal(fake.commits().length, 0, "unreviewed agent text never reaches git mid-session");

  await flushRoom({ files }, room.name, room.doc, true);
  assert.equal(fake.commits().length, 1);
  assert.match(fake.file(PRD)!, /Agent addition\./);
  assert.ok(!fake.file(PRD)!.includes("agentInsertion"));
});

test("a verdict (4xx) is thrown as a denial and the doc keeps its edit", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const room = await seededRoom(files, "spec-acme-shop", "shop");
  room.edit(ARCH, '{"v":2}');
  fake.failNext(400, "path_invalid", "apply");
  await assert.rejects(flushRoom({ files }, room.name, room.doc), FilesDeniedError);
  assert.equal(room.text(ARCH), '{"v":2}');
  assert.equal(pendingChanges(room.doc, roomState(room.name)!, false).writes.length, 1, "the baseline did not move");
});

// ---------------------------------------------------------------------------
// Conflicts: doc wins over the paths it changed, reported; nothing else is touched

test("not_fast_forward is retryable; the retry meets an external commit and doc-wins reports the path", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const rec = recording(files);
  const room = await seededRoom(rec.files, "spec-acme-shop", "shop");
  room.edit(PRD, "# PRD\n\nRoom version.");

  // The branch moved during the save: the pod says re-read and retry later.
  fake.failNext(409, "not_fast_forward", "apply");
  await assert.rejects(flushRoom({ files: rec.files }, room.name, room.doc), FilesUnavailableError);
  assert.equal(fake.commits().length, 0);

  // What moved it: a commit made outside the room, on the file the room edited.
  fake.pushExternal(PRD, "# PRD\n\nExternal version.");
  const externalSha = (await files.bundle("shop")).find((f) => f.path === PRD)!.sha;
  const sent: ApplyWarning[][] = [];
  await flushRoom({ files: rec.files, onWarnings: (w) => sent.push(w) }, room.name, room.doc);

  // The next debounce: conflict → refetch through the bundle → re-apply on HEAD's sha.
  assert.equal(rec.batches.length, 3);
  assert.equal(rec.batches[2]!.writes[0]!.baseSha, externalSha);
  assert.equal(fake.file(PRD), "# PRD\n\nRoom version.");
  assert.equal(sent.length, 1);
  assert.deepEqual(
    sent[0]!.map((w) => w.path),
    [PRD],
    "saving over a commit made outside the room is never silent",
  );
  assert.ok(fake.requests.some((r) => r.url.startsWith("/projects/shop/bundle")));
});

test("an external commit to a file the room did not edit is not clobbered: it is re-seeded into the room", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const rec = recording(files);
  const room = await seededRoom(rec.files, "spec-acme-shop", "shop");
  room.edit(PRD, "# PRD\n\nRoom version.");
  // One commit outside the room touches both files; the room edited only PRD.
  fake.pushExternal(PRD, "# PRD\n\nExternal version.");
  fake.pushExternal(ARCH, '{"v":"external"}');
  const sent: ApplyWarning[][] = [];

  await flushRoom({ files: rec.files, onWarnings: (w) => sent.push(w) }, room.name, room.doc);

  assert.deepEqual(rec.batches.at(-1)!.writes.map((w) => w.path), [PRD], "the retry writes only what the room changed");
  assert.equal(fake.file(ARCH), '{"v":"external"}', "the external change survives");
  assert.equal(room.text(ARCH), '{"v":"external"}', "and the room now shows it");
  assert.deepEqual(sent[0]!.map((w) => w.path), [PRD]);

  // Editing the re-seeded file later preconditions on the external commit: no conflict, nothing lost.
  room.edit(ARCH, '{"v":"external","room":1}');
  await flushRoom({ files: rec.files, onWarnings: (w) => sent.push(w) }, room.name, room.doc);
  assert.equal(fake.file(ARCH), '{"v":"external","room":1}');
  assert.deepEqual(sent.at(-1), []);
});

test("a file HEAD gained outside the room is not the doc's to delete", async () => {
  const fake = await fakeSocket(SEED);
  const rec = recording(createFilesClient(fake.path));
  const room = await seededRoom(rec.files, "spec-acme-shop", "shop");
  room.edit(PRD, "# PRD\n\nRoom version.");
  fake.pushExternal(PRD, "# PRD\n\nExternal version.");
  // The platform committed a dependency's interface beside its definition.
  fake.pushExternal("specs/design/dependencies/stripe/openapi.yaml", "openapi: 3.0.0");

  await flushRoom({ files: rec.files }, room.name, room.doc);

  assert.deepEqual(rec.batches.at(-1)!.deletes, []);
  assert.equal(fake.file("specs/design/dependencies/stripe/openapi.yaml"), "openapi: 3.0.0");
  assert.deepEqual(pendingChanges(room.doc, roomState(room.name)!, false).deletes, []);
});

test("a conflict whose content already landed (a lost reply, a racing flush) adopts HEAD silently", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const room = await seededRoom(files, "spec-acme-shop", "shop");
  room.edit(ARCH, '{"v":2}');
  // Git already holds exactly what the room would write.
  fake.pushExternal(ARCH, '{"v":2}');
  const sent: ApplyWarning[][] = [];
  await flushRoom({ files, onWarnings: (w) => sent.push(w) }, room.name, room.doc);
  assert.deepEqual(sent, [], "nothing was saved over, so nothing to report and nothing to apply");
  assert.equal(pendingChanges(room.doc, roomState(room.name)!, false).writes.length, 0);
});

test("conflicts that keep coming give up after the bounded retries", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const room = await seededRoom(files, "spec-acme-shop", "shop");
  room.edit(ARCH, '{"v":2}');
  let n = 0;
  const racing: FilesClient = {
    ...files,
    apply: async (project, batch) => {
      // Someone commits ARCH again before every apply.
      fake.pushExternal(ARCH, `{"v":"race-${++n}"}`);
      return files.apply(project, batch);
    },
  };
  await assert.rejects(flushRoom({ files: racing }, room.name, room.doc), ApplyConflictError);
  assert.equal(n, 3, "one apply and two doc-wins retries");
});

test("a room with no committer state is skipped", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const doc = new Y.Doc() as Document;
  setDocFile(doc, ARCH, "x");
  await flushRoom({ files }, "spec-acme-nobody", doc);
  assert.equal(fake.requests.length, 0);
});

test("concurrent flushes of one room run one at a time: one commit, and the room's own edit is never reported", async () => {
  const fake = await fakeSocket(SEED);
  const rec = recording(createFilesClient(fake.path));
  const room = await seededRoom(rec.files, "spec-acme-shop", "shop");
  room.edit(ARCH, '{"v":2}');
  const sent: ApplyWarning[][] = [];
  const deps = { files: rec.files, onWarnings: (w: ApplyWarning[]) => sent.push(w) };
  // The debounced store, a console flush and the shutdown drain, all at once.
  await Promise.all([
    flushRoom(deps, room.name, room.doc),
    flushRoom(deps, room.name, room.doc, true),
    flushRoom(deps, room.name, room.doc, true),
  ]);
  assert.equal(rec.batches.length, 1, "the later flushes found nothing left to write");
  assert.equal(fake.commits().length, 1);
  assert.deepEqual(sent, [[]]);
});

test("a flush after a failed one still runs: the chain survives a rejection", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const room = await seededRoom(files, "spec-acme-shop", "shop");
  room.edit(ARCH, '{"v":2}');
  fake.failNext(503, "disk_full", "apply");
  const [first, second] = await Promise.allSettled([
    flushRoom({ files }, room.name, room.doc),
    flushRoom({ files }, room.name, room.doc),
  ]);
  assert.equal(first.status, "rejected");
  assert.equal(second.status, "fulfilled");
  assert.equal(fake.commits().length, 1);
});

test("a conflict on a blob this room committed itself is not reported as an outside change", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const room = await seededRoom(files, "spec-acme-shop", "shop");
  const seededSha = roomState(room.name)!.baseline.get(ARCH)!.sha;
  room.edit(ARCH, '{"v":2}');
  await flushRoom({ files }, room.name, room.doc);
  // A stale precondition the room's own commit left behind (e.g. an apply whose
  // baseline update was lost): git holds the room's v2, the room sends v1's sha.
  roomState(room.name)!.baseline.set(ARCH, { content: '{"v":2}', sha: seededSha });
  room.edit(ARCH, '{"v":3}');
  const sent: ApplyWarning[][] = [];
  await flushRoom({ files, onWarnings: (w) => sent.push(w) }, room.name, room.doc);
  assert.equal(fake.file(ARCH), '{"v":3}');
  assert.deepEqual(sent, [[]]);
});

test("a path the room undid while the bundle was read is re-seeded, not written by sha alone", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const room = await seededRoom(files, "spec-acme-shop", "shop");
  const seeded = room.text(ARCH)!;
  room.edit(ARCH, '{"v":"room"}');
  fake.pushExternal(ARCH, '{"v":"external"}');
  const undoing: FilesClient = {
    ...files,
    bundle: async (project) => {
      const head = await files.bundle(project);
      room.edit(ARCH, seeded); // the user undoes their edit meanwhile
      return head;
    },
  };
  const sent: ApplyWarning[][] = [];
  await flushRoom({ files: undoing, onWarnings: (w) => sent.push(w) }, room.name, room.doc);
  assert.equal(fake.file(ARCH), '{"v":"external"}', "the outside change survives");
  assert.equal(room.text(ARCH), '{"v":"external"}', "and the room shows it");
  assert.deepEqual(sent, [], "nothing was applied, nothing reported");
  assert.equal(pendingChanges(room.doc, roomState(room.name)!, false).writes.length, 0);
});

test("a file the room created and undid during the refetch, also created outside, is adopted and never deleted", async () => {
  const fake = await fakeSocket(SEED);
  const rec = recording(createFilesClient(fake.path));
  const room = await seededRoom(rec.files, "spec-acme-shop", "shop");
  const X = "specs/validation/plan.txt";
  room.edit(X, "room's plan");
  fake.pushExternal(X, "outside plan");
  const undoing: FilesClient = {
    ...rec.files,
    bundle: async (project) => {
      const head = await rec.files.bundle(project);
      deleteDocFile(room.doc, X); // the user undoes the create meanwhile
      return head;
    },
  };
  const sent: ApplyWarning[][] = [];
  await flushRoom({ files: undoing, onWarnings: (w) => sent.push(w) }, room.name, room.doc);
  assert.equal(rec.batches.length, 1, "only the conflicted apply: nothing left to write after the undo");
  assert.ok(rec.batches.every((b) => b.deletes.length === 0), "no delete is ever sent");
  assert.equal(fake.file(X), "outside plan", "the outside file stays on origin");
  assert.equal(room.text(X), "outside plan", "and the room gains HEAD's copy");
  assert.deepEqual(sent, []);
  assert.deepEqual(pendingChanges(room.doc, roomState(room.name)!, true), { writes: [], deletes: [], held: [] });
});

// ---------------------------------------------------------------------------
// Shutdown

test("flushAllRooms force-flushes every room, and one failing room does not stop the others", async () => {
  const fake = await fakeSocket({ ...SEED, "specs/b.txt": "b" });
  const files = createFilesClient(fake.path);
  const a = await seededRoom(files, "spec-acme-a", "a");
  const b = await seededRoom(files, "spec-acme-b", "b");
  a.edit(ARCH, '{"v":"a"}');
  b.edit("specs/b.txt", "b2");
  fake.failNext(503, "disk_full", "apply");
  const lines: PodLogLine[] = [];
  await flushAllRooms(
    { files, log: (l) => lines.push(l) },
    new Map([
      [a.name, a.doc],
      [b.name, b.doc],
    ]),
    { concurrency: 1, force: true },
  );
  assert.equal(fake.commits().length, 1, "the room after the failed one still committed");
  assert.deepEqual(
    lines.filter((l) => l.msg === "room_flush_failed"),
    [{ msg: "room_flush_failed", source: "ae-collab", cause: "files_unavailable" }],
  );
});

test("flushAllRooms returns at its budget when the socket stalls, naming only the count", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const room = await seededRoom(files, "spec-acme-shop", "shop");
  room.edit(ARCH, '{"v":2}');
  fake.stallNext("apply");
  const lines: PodLogLine[] = [];
  const started = Date.now();
  await flushAllRooms({ files, log: (l) => lines.push(l) }, new Map([[room.name, room.doc]]), {
    concurrency: 8,
    force: true,
    budgetMs: 100,
  });
  assert.ok(Date.now() - started < 2_000);
  assert.deepEqual(lines.at(-1), { msg: "room_shutdown_flush_over_budget", source: "ae-collab", rooms: 1 });
});

test("no committer log line carries a room name, a path or content", async () => {
  const fake = await fakeSocket(SEED);
  const files = createFilesClient(fake.path);
  const room = await seededRoom(files, "spec-acme-shop", "shop");
  room.edit(PRD, "# PRD\n\nRoom version.");
  fake.pushExternal(PRD, "# PRD\n\nExternal version.");
  const lines: PodLogLine[] = [];
  await flushRoom({ files, log: (l) => lines.push(l) }, room.name, room.doc);
  assert.ok(lines.some((l) => l.msg === "room_flush_committed"));
  assert.doesNotMatch(JSON.stringify(lines), /acme|shop|prd|PRD|specs\//);
});
