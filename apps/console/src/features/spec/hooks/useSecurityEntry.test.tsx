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

// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as Y from "yjs";

import { DESIGN_CELL_PATH, SECURITY_JSON_PATH } from "../api/designTree";
import type { SpecFileEntry } from "../api/mapping";
import { serializeSecurityDesign, type SecurityDesign } from "../api/securityDesign";
import type { CollabSpec } from "../collab/useCollabSpec";
import { useSecurityEntry } from "./useSecurityEntry";

const mockContent = vi.fn();
const mockContents = vi.fn();
vi.mock("../api/queries", () => ({
  useSpecFileContent: (...args: unknown[]) => mockContent(...args),
  useSpecFileContents: (...args: unknown[]) => mockContents(...args),
}));

vi.mock("../api/roles", () => ({
  useProjectRoles: () => ({ data: undefined, isPending: false, isError: false }),
}));

// `useYTextString` and `applyTextEdit` are NOT mocked: the room here is a
// real `Y.Doc`, so the read path, the write path and the CRDT merge behaviour
// under test are the ones that ship. Only the two network reads are doubles.

const OPENAPI_PATH = "specs/design/components/orders-api/openapi.yaml";
const WIREFRAMES_PATH = "specs/design/components/storefront/wireframes.dsl";

function entry(path: string, sha = "abc"): SpecFileEntry {
  return { path, sha, group: "designs" } as SpecFileEntry;
}

const FILE = entry(SECURITY_JSON_PATH);

/** A v2 document naming one owning component and one screen component. */
function designText(): string {
  const doc: SecurityDesign = {
    version: 2,
    permissions: [
      {
        resource: "orders",
        component: "orders-api",
        actions: [{ handle: "read", ownership: "own" }],
      },
    ],
    groups: [],
    roles: [
      {
        name: "Admin",
        description: "What Admin may do",
        stories: [1],
        grants: ["orders:read"],
      },
    ],
    screens: [{ component: "storefront", screen: "Orders", requires: "orders:read" }],
    testUsers: [],
  };
  return serializeSecurityDesign(doc);
}

/**
 * A room holding the given files, as a real `Y.Doc` — `getFileText` reads the
 * `files` map exactly as `useCollabSpec` does, and a read never creates.
 */
function room(files: Record<string, string> | null): {
  collab: CollabSpec;
  text: (path: string) => Y.Text | null;
} {
  const doc = new Y.Doc();
  const map = doc.getMap<Y.Text>("files");
  for (const [path, content] of Object.entries(files ?? {})) {
    const ytext = new Y.Text();
    ytext.insert(0, content);
    map.set(path, ytext);
  }
  const text = (path: string) => map.get(path) ?? null;
  return {
    collab: { peers: [], doc, getFileText: text } as unknown as CollabSpec,
    text,
  };
}

beforeEach(() => {
  mockContent.mockReset();
  mockContent.mockReturnValue({
    data: undefined,
    isPending: false,
    isError: false,
  });
  mockContents.mockReset();
  mockContents.mockReturnValue({});
});

function run(
  over: {
    active?: boolean;
    agentInRoom?: boolean;
    roomFiles?: Record<string, string> | null;
    files?: SpecFileEntry[];
  } = {},
) {
  const { collab, text } = room(over.roomFiles ?? null);
  const rendered = renderHook(() =>
    useSecurityEntry({
      projectName: "p",
      active: over.active ?? true,
      files: over.files ?? [FILE],
      collab,
      agentInRoom: over.agentInRoom ?? false,
    }),
  );
  return { ...rendered, text, result: rendered.result };
}

/** The room holding only `security.json`, at the given text. */
function withDocument(content: string) {
  return { [SECURITY_JSON_PATH]: content };
}

describe("useSecurityEntry — committed fallback loading", () => {
  it("reports pending only while the solo committed read is in flight", () => {
    mockContent.mockReturnValue({
      data: undefined,
      isPending: true,
      isError: false,
    });
    const { result } = run();

    expect(mockContent).toHaveBeenLastCalledWith("p", FILE);
    expect(result.current.isPending).toBe(true);
    expect(result.current.isError).toBe(false);
    expect(result.current.securityJson).toBeNull();
  });

  it("does not spin when the room already has the document (disabled query may still be pending)", () => {
    mockContent.mockReturnValue({
      data: undefined,
      isPending: true,
      isError: true,
    });
    const { result } = run({ roomFiles: withDocument('{"version":1}') });

    expect(mockContent).toHaveBeenLastCalledWith("p", null);
    expect(result.current.isPending).toBe(false);
    expect(result.current.isError).toBe(false);
    expect(result.current.securityJson).toBe('{"version":1}');
  });

  it("uses committed data.content on the solo path", () => {
    mockContent.mockReturnValue({
      data: { content: '{"version":1,"thunder":{"name":"orders-app","type":"browser"}}' },
      isPending: false,
      isError: false,
    });
    const { result } = run();

    expect(mockContent).toHaveBeenLastCalledWith("p", FILE);
    expect(result.current.isPending).toBe(false);
    expect(result.current.isError).toBe(false);
    expect(result.current.securityJson).toBe(
      '{"version":1,"thunder":{"name":"orders-app","type":"browser"}}',
    );
  });

  it("does not spin when an agent is in the room and the committed read is suppressed", () => {
    mockContent.mockReturnValue({
      data: undefined,
      isPending: true,
      isError: false,
    });
    const { result } = run({ agentInRoom: true });

    expect(mockContent).toHaveBeenLastCalledWith("p", null);
    expect(result.current.isPending).toBe(false);
  });

  it("surfaces a committed-read failure", () => {
    mockContent.mockReturnValue({
      data: undefined,
      isPending: false,
      isError: true,
    });
    const { result } = run();

    expect(result.current.isPending).toBe(false);
    expect(result.current.isError).toBe(true);
  });
});

describe("useSecurityEntry — roomLive", () => {
  it("is true exactly when the room holds the document", () => {
    expect(run({ roomFiles: withDocument(designText()) }).result.current.roomLive).toBe(
      true,
    );
  });

  // The committed copy is a READ. There is nowhere to put an edit, and the page
  // has to be able to say so rather than dropping a click on the floor.
  it("is false when the page is showing the committed copy from git", () => {
    mockContent.mockReturnValue({
      data: { content: designText() },
      isPending: false,
      isError: false,
    });
    const { result } = run();

    expect(result.current.securityJson).toBe(designText());
    expect(result.current.roomLive).toBe(false);
  });

  it("is false while the entry is not the current selection", () => {
    const { result } = run({
      active: false,
      roomFiles: withDocument(designText()),
    });
    expect(result.current.roomLive).toBe(false);
  });
});

describe("useSecurityEntry — writeSecurityJson", () => {
  it("writes the whole document back into the room", () => {
    const { result, text } = run({ roomFiles: withDocument(designText()) });
    const next = designText().replace("orders:read", "orders:read-all");

    result.current.writeSecurityJson(() => next);

    expect(text(SECURITY_JSON_PATH)?.toString()).toBe(next);
  });

  it("hands the updater the room's text", () => {
    const { result } = run({ roomFiles: withDocument(designText()) });
    const seen: string[] = [];

    result.current.writeSecurityJson((current) => {
      seen.push(current);
      return null;
    });

    expect(seen).toEqual([designText()]);
  });

  // The point of going through `applyTextEdit`: the design agent may be
  // writing the same file, and a delete-all-then-insert-all would clobber its
  // edit at the CRDT level even though the text looks right locally.
  it("lands as a minimal edit, not a whole-document replace", () => {
    const { result, text } = run({ roomFiles: withDocument(designText()) });
    const ytext = text(SECURITY_JSON_PATH);
    const deltas: unknown[] = [];
    ytext?.observe((event) => deltas.push(event.changes.delta));

    result.current.writeSecurityJson(() =>
      designText().replace("orders:read", "orders:read-all"),
    );

    // One retain over the untouched prefix, then the four inserted characters —
    // and no delete of the rest of the file.
    expect(deltas).toEqual([
      [{ retain: designText().indexOf("orders:read") + "orders:read".length }, { insert: "-all" }],
    ]);
  });

  // The real diff, not the placeholder binding's single prefix/suffix trim.
  // Nothing guarantees the room's copy is in the two-space form the patch
  // re-serialises to, and a trim over two differences spans everything between
  // them — deleting, and re-inserting, every line in the middle.
  it("changes only the lines that differ in a differently-formatted document", () => {
    const before = `{\n  "a": 1,\n  "MIDDLE": "untouched",\n  "z": 2\n}\n`;
    const after = `{\n  "a": 9,\n  "MIDDLE": "untouched",\n  "z": 8\n}\n`;
    const { result, text } = run({ roomFiles: withDocument(before) });
    const ytext = text(SECURITY_JSON_PATH)!;
    const deltas: { retain?: number; insert?: string; delete?: number }[] = [];
    ytext.observe((event) => deltas.push(...(event.changes.delta as never[])));

    result.current.writeSecurityJson(() => after);

    expect(ytext.toString()).toBe(after);
    // Two characters changed, so two characters are deleted and two inserted.
    // A single prefix/suffix trim would span both differences and rewrite the
    // whole untouched middle — deleting the line between them and any
    // concurrent edit inside it.
    expect(deltas.filter((d) => d.delete !== undefined)).toEqual([
      { delete: 1 },
      { delete: 1 },
    ]);
    expect(deltas.filter((d) => d.insert !== undefined)).toEqual([
      { insert: "9" },
      { insert: "8" },
    ]);
  });

  // F2: the patch and the diff must see ONE text. A caller handing over a
  // string computed from the last render applies it as a positional diff
  // against whatever the room holds NOW — and when the design agent flushed in
  // between, the single changed span covers its insertion and deletes it.
  it("patches the room's CURRENT text, so an edit that lands mid-click survives", () => {
    const { result, text } = run({ roomFiles: withDocument(designText()) });
    const ytext = text(SECURITY_JSON_PATH)!;
    const rendered = ytext.toString();

    // The agent appends a second role while the click is in flight.
    const agentEdit = '\n{ "the agent was here" }\n';
    ytext.insert(ytext.length, agentEdit);

    // The updater is handed the text as it is NOW, not the text above.
    result.current.writeSecurityJson((current) => {
      expect(current).not.toBe(rendered);
      return current.replace("orders:read", "orders:read-all");
    });

    const after = ytext.toString();
    expect(after).toContain("orders:read-all");
    expect(after).toContain("the agent was here");
  });

  it("is a no-op when the room does not hold the document", () => {
    mockContent.mockReturnValue({
      data: { content: designText() },
      isPending: false,
      isError: false,
    });
    const { result } = run();

    expect(result.current.roomLive).toBe(false);
    expect(() => result.current.writeSecurityJson(() => "{}")).not.toThrow();
    // The committed copy is untouched: the hook never writes to git.
    expect(result.current.securityJson).toBe(designText());
  });

  it("writes nothing when the text is unchanged", () => {
    const { result, text } = run({ roomFiles: withDocument(designText()) });
    const ytext = text(SECURITY_JSON_PATH);
    const deltas: unknown[] = [];
    ytext?.observe((event) => deltas.push(event.changes.delta));

    result.current.writeSecurityJson(() => designText());

    expect(deltas).toEqual([]);
  });

  // The updater says "nothing to write" — a refused patch, which the panel
  // turns into a banner rather than a version in the room's history.
  it("writes nothing when the updater answers null", () => {
    const { result, text } = run({ roomFiles: withDocument(designText()) });
    const ytext = text(SECURITY_JSON_PATH);
    const deltas: unknown[] = [];
    ytext?.observe((event) => deltas.push(event.changes.delta));

    result.current.writeSecurityJson(() => null);

    expect(deltas).toEqual([]);
    expect(ytext?.toString()).toBe(designText());
  });
});

describe("useSecurityEntry — references", () => {
  it("is always an object, even with no document to cross-check", () => {
    const { result } = run();
    expect(typeof result.current.references.read).toBe("function");
    expect(result.current.references.read(DESIGN_CELL_PATH)).toBeUndefined();
  });

  it("reads a sibling spec from the room", () => {
    const { result } = run({
      roomFiles: {
        ...withDocument(designText()),
        [DESIGN_CELL_PATH]: 'component "orders-api" service',
        [WIREFRAMES_PATH]: "screen Orders",
      },
    });

    expect(result.current.references.read(DESIGN_CELL_PATH)).toBe(
      'component "orders-api" service',
    );
    expect(result.current.references.read(WIREFRAMES_PATH)).toBe("screen Orders");
  });

  it("falls back to the committed copy for a sibling the room does not hold", () => {
    mockContents.mockReturnValue({ [OPENAPI_PATH]: "openapi: 3.1.0" });
    const { result } = run({
      roomFiles: withDocument(designText()),
      files: [FILE, entry(OPENAPI_PATH)],
    });

    expect(result.current.references.read(OPENAPI_PATH)).toBe("openapi: 3.1.0");
  });

  it("prefers the room's copy over the committed one", () => {
    mockContents.mockReturnValue({ [OPENAPI_PATH]: "committed" });
    const { result } = run({
      roomFiles: { ...withDocument(designText()), [OPENAPI_PATH]: "live" },
      files: [FILE, entry(OPENAPI_PATH)],
    });

    expect(result.current.references.read(OPENAPI_PATH)).toBe("live");
  });

  it("answers undefined for a path nothing holds", () => {
    const { result } = run({ roomFiles: withDocument(designText()) });
    expect(result.current.references.read(OPENAPI_PATH)).toBeUndefined();
  });

  // Only what the DOCUMENT names and git actually lists: no 404-probing for a
  // component whose specs were never written, and no request for a file the
  // room is already streaming.
  it("fetches only the document's siblings that the committed tree lists", () => {
    run({
      roomFiles: withDocument(designText()),
      files: [
        FILE,
        entry(DESIGN_CELL_PATH),
        entry(OPENAPI_PATH),
        entry("specs/design/components/other-api/openapi.yaml"),
        entry("specs/requirements/prd.md"),
      ],
    });

    expect(mockContents).toHaveBeenLastCalledWith("p", [
      entry(DESIGN_CELL_PATH),
      entry(OPENAPI_PATH),
    ]);
  });

  it("does not fetch a sibling the room already holds", () => {
    run({
      roomFiles: {
        ...withDocument(designText()),
        [DESIGN_CELL_PATH]: 'component "orders-api" service',
      },
      files: [FILE, entry(DESIGN_CELL_PATH), entry(OPENAPI_PATH)],
    });

    expect(mockContents).toHaveBeenLastCalledWith("p", [entry(OPENAPI_PATH)]);
  });

  // An unreadable document names no components, so there is nothing to
  // cross-check and nothing to fetch.
  it("asks for nothing when the document does not parse", () => {
    run({
      roomFiles: withDocument('{"version":1}'),
      files: [FILE, entry(DESIGN_CELL_PATH), entry(OPENAPI_PATH)],
    });

    expect(mockContents).toHaveBeenLastCalledWith("p", []);
  });

  // The room's `update` event fires for EVERY file in it, so an agent streaming
  // an unrelated markdown document bumps this hook on every flush. The answers
  // have not changed, so the object must not either: the Security page feeds it
  // straight into `securityReferenceFindings` and into a parse of every owning
  // component's `openapi.yaml`, and a fresh object re-runs both, several times a
  // second, for as long as the tab is open.
  it("keeps the same references object when an unwatched file changes", () => {
    const { result, rerender, text } = run({
      roomFiles: {
        ...withDocument(designText()),
        [DESIGN_CELL_PATH]: 'component "orders-api" service',
        "specs/requirements/prd.md": "# PRD",
      },
      files: [FILE, entry(DESIGN_CELL_PATH)],
    });
    const before = result.current.references;

    act(() => {
      text("specs/requirements/prd.md")?.insert(5, " more prose");
    });
    rerender();

    expect(result.current.references).toBe(before);
  });

  it("answers a new object when a file it IS watching changes", () => {
    const { result, rerender, text } = run({
      roomFiles: {
        ...withDocument(designText()),
        [DESIGN_CELL_PATH]: 'component "orders-api" service',
      },
      files: [FILE, entry(DESIGN_CELL_PATH)],
    });
    const before = result.current.references;

    act(() => {
      text(DESIGN_CELL_PATH)?.insert(0, "// changed\n");
    });
    rerender();

    expect(result.current.references).not.toBe(before);
    expect(result.current.references.read(DESIGN_CELL_PATH)).toContain(
      "// changed",
    );
  });

  it("reads nothing while the entry is not the current selection", () => {
    const { result } = run({
      active: false,
      roomFiles: {
        ...withDocument(designText()),
        [DESIGN_CELL_PATH]: 'component "orders-api" service',
      },
      files: [FILE, entry(DESIGN_CELL_PATH)],
    });

    expect(mockContents).toHaveBeenLastCalledWith("p", []);
    expect(result.current.references.read(DESIGN_CELL_PATH)).toBeUndefined();
  });
});
