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

import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  useSearch,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import * as Y from "yjs";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { DESIGN_REVIEW_PATH, type DesignReview } from "@aep/agent-stream";
import { readDocFile, setDocFile } from "@aep/collab-doc";
import { sha256Text } from "../../../lib/sha256";
import type { ProjectChat } from "../../agent-chat/chatStore";
import type { BuildHold } from "../../builds/model/picker";
import type { DesignModel } from "../api/designModel";
import { withoutAssumedTags } from "../model/decisions";
import { readReview } from "../model/review";
import { readSpecLines } from "../../spec/collab/useSpecLines";

// The Design card as a reviewer uses it, on a room (a Y.Doc here) holding a
// design's files: the rail of files, each file's Checked and 💬, the
// Overview's one primary, and Address comments, which sends the review.

const DECISIONS = "specs/design/decisions.md";
const FILES: Record<string, string> = {
  "specs/design/design.cell": "title Acme\n\ncomponent expense-api as \"Expense API\" service\n",
  "specs/design/domain-model.md": "# Data model\n\nClaims and their expenses.\n",
  [DECISIONS]: "# Technical decisions\n\n- [data-model] Amounts are kept in cents. *assumed* (F1)\n",
  "specs/validation/acceptance/F1-submit-expenses.feature": [
    "Feature: F1 Submit expenses",
    "",
    "  @story-F1.1",
    "  Rule: The amount is read from a receipt photo",
    "",
    "    Scenario: A clear photo fills in the expense",
    "      Given Priya has a photo of a $30.00 taxi receipt",
    "      When she adds it to a new claim",
    "      Then the expense reads $30.00",
    "",
  ].join("\n"),
};

let doc: Y.Doc;
vi.mock("../../spec/collab/specDoc", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../spec/collab/specDoc")>()),
  useSpecDoc: () => doc,
}));
vi.mock("../../../auth/SessionContext", () => ({
  useSession: () => ({ user: { id: "u-priya", name: "Priya", email: "priya@acme.example" }, orgHandle: "acme", signOut: () => {} }),
}));

const model: DesignModel = { revision: null, running: null, artifacts: [], dependencies: [] };
vi.mock("../useDesignModel", () => ({ useDesignModel: () => ({ data: model, isError: false, error: null, refetch: vi.fn() }) }));

let label: string | null = null;
const F1_PATH = "specs/requirements/features/F1-submit-expenses.md";
/** Whether F1 has been designed: only a designed feature's open questions are the Design card's. */
let designedFrom: Record<string, string> = {};
/** The features the spec is ahead of the design for (the workspace works it out from the live documents). */
let outOfDate: string[] = [];
vi.mock("../../spec/useSpecWorkspace", () => ({
  useSpecWorkspace: () => ({
    model: {
      data: { features: [{ id: "F1", name: "Submit expenses", path: F1_PATH }], design: { designedFrom } },
      isError: false,
      error: null,
      refetch: vi.fn(),
    },
    lines: readSpecLines(doc),
    workspace: { design: { label, toDesign: label ? ["F1"] : [], outOfDate } },
  }),
}));

let revisions: Record<string, number> = {};
vi.mock("../../spec/api/specModel", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../spec/api/specModel")>()),
  useSpecState: () => ({ data: { artifactRevisions: revisions } }),
}));

const chat: ProjectChat = { status: "ready", error: null, items: [], turn: { phase: "idle" } };
const send = vi.fn<(projectName: string, text: string, scope: unknown) => Promise<boolean>>(async () => true);
vi.mock("../../agent-chat/useProjectChat", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../agent-chat/useProjectChat")>()),
  useProjectChat: () => chat,
  chatStore: { send: (projectName: string, text: string, scope: unknown) => send(projectName, text, scope) },
}));
vi.mock("../../shell/chatPanel", () => ({ useChatPanel: () => ({ open: vi.fn() }) }));

const openBuild = vi.fn();
/** Why no build can start (null: Build v1 is offered). */
let hold: BuildHold | null = null;
vi.mock("../../builds/buildPicker", () => ({ useBuildAction: () => ({ offer: null, label: hold ? null : "Build v1", hold, open: openBuild }) }));

const { DesignActions, DesignWorkspace } = await import("./DesignWorkspace");

/** The Design card at `?art=`, its header's actions above its body, as the card route draws them. */
function renderCard(search = "") {
  const root = createRootRoute({ component: () => <Outlet /> });
  const card = createRoute({
    getParentRoute: () => root,
    path: "/projects/$projectName/design",
    validateSearch: (s: Record<string, unknown>) => ({
      ...(typeof s.art === "string" ? { art: s.art } : {}),
      ...(typeof s.review === "string" ? { review: s.review } : {}),
    }),
    component: function Card() {
      const { art, review } = useSearch({ strict: false }) as { art?: string; review?: string };
      return (
        <>
          <header aria-label="Card actions">
            <DesignActions projectName="acme-expenses" art={art} />
          </header>
          <DesignWorkspace projectName="acme-expenses" art={art} review={review} />
        </>
      );
    },
  });
  const router = createRouter({
    routeTree: root.addChildren([card]),
    history: createMemoryHistory({ initialEntries: [`/projects/acme-expenses/design${search}`] }),
  });
  render(
    <QueryClientProvider client={new QueryClient()}>
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <RouterProvider router={router} />
      </OxygenUIThemeProvider>
    </QueryClientProvider>,
  );
  return router;
}

function review(): DesignReview {
  return readReview(readDocFile(doc, DESIGN_REVIEW_PATH)).review;
}

function seed(files: Record<string, string>) {
  doc = new Y.Doc();
  for (const [path, content] of Object.entries(files)) setDocFile(doc, path, content, "test");
}

/** The Overview's buttons and links, by whether each is the primary (contained). */
function primaries(): string[] {
  return [...document.querySelectorAll(".MuiButton-contained")].map((e) => e.getAttribute("aria-label") ?? e.textContent ?? "");
}

beforeAll(() => {
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
});
beforeEach(() => {
  seed(FILES);
  label = null;
  designedFrom = {};
  outOfDate = [];
  revisions = {};
  hold = null;
  send.mockReset();
  send.mockResolvedValue(true);
});
afterEach(cleanup);

describe("the Design card before anything is designed", () => {
  it("says what Design makes, and offers it", async () => {
    seed({});
    label = "Design 1 feature";
    renderCard();
    expect(await screen.findByText(/Nothing is designed yet/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Design 1 feature" })).toBeInTheDocument();
    expect(within(screen.getByRole("banner", { name: "Card actions" })).queryByRole("button")).toBeNull();
  });
});

describe("the rail", () => {
  it("lists Overview, Comments, then the files in the order Design makes them, with no filter", async () => {
    renderCard();
    const rail = await screen.findByRole("navigation", { name: "Design" });
    expect(within(rail).getAllByRole("link").map((l) => l.textContent)).toEqual([
      "Overview",
      "Comments",
      "Architectureto check",
      "Data modelto check",
      "Technical decisionsto check",
      "F1 Submit expenses1 scenario",
    ]);
    expect(within(rail).getByText("Acceptance tests")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Business" })).toBeNull();
  });
});

describe("an agent's file", () => {
  it("reads in Instructions, Tools and Configuration tabs, not as its raw frontmatter", async () => {
    const dir = "specs/design/components/claims-assistant/";
    seed({
      ...FILES,
      [`${dir}design.json`]: JSON.stringify({ name: "claims-assistant", type: "ai-agent", stories: ["F1.1"] }),
      [`${dir}agent.afm.md`]: [
        "---",
        'spec_version: "0.4.0"',
        'name: "claims-assistant"',
        "model:",
        '  provider: "anthropic"',
        "x-aep:",
        "  tools:",
        "    openapi:",
        '      - component: "expense-api"',
        "        allow: [listClaims]",
        "---",
        "",
        "# Role",
        "Answers questions about a claim.",
        "",
      ].join("\n"),
    });
    renderCard("?art=agent-claims-assistant");
    expect(await screen.findByRole("tab", { name: "Instructions" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: /Tools/ })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Configuration" })).toBeInTheDocument();
    expect(screen.getByText("Answers questions about a claim.")).toBeInTheDocument();
    expect(screen.queryByText(/spec_version/)).toBeNull();
  });
});

describe("Checked and comments on a file's title line", () => {
  it("ticks Checked at the file's hash, and shows it unticked once the file changes", async () => {
    renderCard("?art=decisions");
    const box = await screen.findByRole("checkbox", { name: "Checked: Technical decisions" });
    fireEvent.click(box);
    await waitFor(() => expect(box).toBeChecked());
    // The technical decisions' hash leaves out their assumed tags (ADR-0006).
    expect(review().artifacts).toEqual([{ path: DECISIONS, checkedHash: sha256Text(withoutAssumedTags(readDocFile(doc, DECISIONS)!)) }]);

    act(() => setDocFile(doc, DECISIONS, `${FILES[DECISIONS]}- [contract] Oldest first. (F2)\n`, "agent"));
    await waitFor(() => expect(box).not.toBeChecked());
    expect(screen.getByText("Changed since you checked it")).toBeInTheDocument();
  });

  it("names the file's revision once a design run has changed it", async () => {
    revisions = { [DECISIONS]: 2 };
    renderCard("?art=decisions");
    expect(await screen.findByText("Revision 2")).toBeInTheDocument();
  });

  it("adds a Pending draft from the 💬, edits and deletes it", async () => {
    renderCard("?art=decisions");
    fireEvent.click(await screen.findByRole("button", { name: "Comments on Technical decisions: 0" }));
    fireEvent.change(screen.getByLabelText("Your comment on Technical decisions"), { target: { value: "Why cents?" } });
    fireEvent.click(screen.getByRole("button", { name: "Add comment" }));
    await waitFor(() => expect(review().artifacts[0]?.comments?.map((c) => [c.text, c.status, c.author.name])).toEqual([["Why cents?", "draft", "Priya"]]));
    expect(screen.getByRole("button", { name: "Comments on Technical decisions: 1", hidden: true })).toBeInTheDocument();
    expect(screen.getByText("Pending")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByLabelText("Edit your comment: Why cents?"), { target: { value: "Why cents, not decimals?" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(review().artifacts[0]?.comments?.[0]?.text).toBe("Why cents, not decimals?"));
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(review().artifacts).toEqual([]));
  });

  it("gives the acceptance tests a 💬 but no Checked, and keeps each scenario's steps in a closed details", async () => {
    renderCard("?art=acceptance-F1");
    expect(await screen.findByRole("button", { name: /^Comments on Acceptance tests: F1 Submit expenses/ })).toBeInTheDocument();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(screen.getByText("F1.1")).toBeInTheDocument();
    const scenario = screen.getByText("A clear photo fills in the expense").closest("details")!;
    expect(scenario).not.toHaveAttribute("open");
    expect(scenario).toHaveTextContent("Given Priya has a photo of a $30.00 taxi receipt");
  });
});

describe("the Overview's one primary", () => {
  it("is Open on the first unchecked row, then Build once every file is checked", async () => {
    renderCard();
    await screen.findByRole("region", { name: "For technical review" });
    expect(primaries()).toEqual(["Open Architecture"]);
    for (const name of ["Architecture", "Data model", "Technical decisions"]) {
      fireEvent.click(screen.getByRole("checkbox", { name: `Checked: ${name}` }));
    }
    // One primary per screen: the Overview's own Build, the header's outlined beside it.
    await waitFor(() => expect(primaries()).toEqual(["Build v1"]));
    const header = () => within(screen.getByRole("banner", { name: "Card actions" })).getByRole("button", { name: "Build v1" });
    expect(header()).toHaveClass("MuiButton-outlined");
  });

  it("says why Build is held while a build runs, linking to it, never only that everything is checked", async () => {
    hold = { kind: "running", version: "v2" };
    renderCard();
    await screen.findByRole("region", { name: "For technical review" });
    for (const name of ["Architecture", "Data model", "Technical decisions"]) {
      fireEvent.click(screen.getByRole("checkbox", { name: `Checked: ${name}` }));
    }
    const bottom = screen.getByRole("region", { name: "Send and build" });
    await waitFor(() => expect(within(bottom).getByRole("button", { name: "Build" })).toBeDisabled());
    expect(bottom).toHaveTextContent("v2 is building — open it on Builds");
    expect(bottom).not.toHaveTextContent("Everything is checked.");
    expect(within(bottom).getByRole("link", { name: "open it on Builds" })).toHaveAttribute("href", "/projects/acme-expenses/builds/v2");
  });

  it("holds Build while the agent works, on the Overview and in the header, and asks to wait", async () => {
    hold = { kind: "agent-working" };
    renderCard();
    await screen.findByRole("region", { name: "For technical review" });
    for (const name of ["Architecture", "Data model", "Technical decisions"]) {
      fireEvent.click(screen.getByRole("checkbox", { name: `Checked: ${name}` }));
    }
    const bottom = screen.getByRole("region", { name: "Send and build" });
    await waitFor(() => expect(within(bottom).getByRole("button", { name: "Build" })).toBeDisabled());
    expect(bottom).toHaveTextContent("Wait for the agent to finish.");
    expect(bottom).not.toHaveTextContent("Everything is checked.");
    expect(within(screen.getByRole("banner", { name: "Card actions" })).queryByRole("button", { name: /Build/ })).toBeNull();
  });

  it("makes the header's Build the primary on a file once every file is checked", async () => {
    const router = renderCard();
    await screen.findByRole("region", { name: "For technical review" });
    for (const name of ["Architecture", "Data model", "Technical decisions"]) {
      fireEvent.click(screen.getByRole("checkbox", { name: `Checked: ${name}` }));
    }
    await act(() => router.navigate({ to: "/projects/$projectName/design", params: { projectName: "acme-expenses" }, search: { art: "decisions" } }));
    const header = within(screen.getByRole("banner", { name: "Card actions" })).getByRole("button", { name: "Build v1" });
    await waitFor(() => expect(header).toHaveClass("MuiButton-contained"));
    expect(primaries()).toEqual(["Build v1"]);
  });

  it("is Address comments while a comment waits, which sends them all, then starts the turn", async () => {
    setDocFile(
      doc,
      DESIGN_REVIEW_PATH,
      JSON.stringify({
        version: 1,
        artifacts: [{ path: DECISIONS, comments: [{ id: "c1", author: { id: "u-sam", name: "Sam" }, text: "Why cents?", createdAt: "2026-10-10T09:00:00Z", status: "draft" }] }],
      }),
      "test",
    );
    renderCard();
    const address = await screen.findByRole("button", { name: "Address comments (1)" });
    expect(primaries()).toEqual(["Address comments (1)"]);
    fireEvent.click(address);
    await waitFor(() => expect(send).toHaveBeenCalledWith("acme-expenses", "/address-comments", { kind: "design" }));
    expect(review().artifacts[0]?.comments?.[0]).toMatchObject({ status: "sent", sentAt: expect.any(String) });
  });

  it("puts the comments back as they were when the turn does not start", async () => {
    send.mockResolvedValue(false);
    renderCard("?art=decisions");
    fireEvent.click(await screen.findByRole("button", { name: "Comments on Technical decisions: 0" }));
    fireEvent.change(screen.getByLabelText("Your comment on Technical decisions"), { target: { value: "Why cents?" } });
    fireEvent.click(screen.getByRole("button", { name: "Add comment" }));
    cleanup();
    renderCard();
    fireEvent.click(await screen.findByRole("button", { name: "Address comments (1)" }));
    await waitFor(() => expect(send).toHaveBeenCalled());
    await waitFor(() => expect(review().artifacts[0]?.comments?.[0]).toMatchObject({ status: "draft" }));
    expect(review().artifacts[0]?.comments?.[0]).not.toHaveProperty("sentAt");
  });
});

describe("questions waiting on the user", () => {
  const asked = (status: "sent" | "addressed" = "sent") =>
    JSON.stringify({
      version: 1,
      artifacts: [
        {
          path: DECISIONS,
          comments: [
            { id: "c1", author: { id: "u-sam", name: "Sam" }, text: "Cents?", createdAt: "x", status, sentAt: "y", question: "Cents for every currency, or the currency's own minor unit?" },
          ],
        },
      ],
    });

  it("makes Answer 1 question the primary; the answer reopens the comment, and Address comments is next", async () => {
    setDocFile(doc, DESIGN_REVIEW_PATH, asked(), "test");
    renderCard();
    const answer = await screen.findByRole("button", { name: "Answer 1 question" });
    expect(primaries()).toEqual(["Answer 1 question"]);
    fireEvent.click(answer);
    const panel = await screen.findByRole("dialog", { name: "Questions for you" });
    expect(panel).toHaveTextContent("On Technical decisions · Sam's comment: “Cents?”");
    fireEvent.change(within(panel).getByLabelText(/^Your answer: Cents for every currency/), { target: { value: "The currency's own minor unit." } });
    fireEvent.click(within(panel).getByRole("button", { name: "Answer" }));
    await waitFor(() =>
      expect(review().artifacts[0]?.comments?.[0]).toMatchObject({ status: "reopened", answer: "The currency's own minor unit." }),
    );
    await waitFor(() => expect(primaries()).toEqual(["Address comments (1)"]));
    expect(screen.queryByRole("button", { name: /^Answer \d/ })).toBeNull();
  });

  it("asks a designed feature's open question, and the answer goes into the feature's Decisions", async () => {
    setDocFile(doc, F1_PATH, "# Submit expenses\n\n## Open Questions\n\n1. Can a claim mix currencies?\n   - No, one currency per claim.\n", "test");
    designedFrom = { F1: "basis" };
    renderCard();
    fireEvent.click(await screen.findByRole("button", { name: "Answer 1 question" }));
    const panel = await screen.findByRole("dialog", { name: "Questions for you" });
    expect(panel).toHaveTextContent("Submit expenses (F1)");
    fireEvent.click(within(panel).getByRole("button", { name: "No, one currency per claim." }));
    await waitFor(() => expect(readDocFile(doc, F1_PATH)).toContain("## Decisions\n\n- No, one currency per claim."));
    expect(readDocFile(doc, F1_PATH)).not.toContain("Can a claim mix currencies?");
  });

  it("says the answer changed the feature, not a Spec edit, once its question is answered here", async () => {
    setDocFile(doc, F1_PATH, "# Submit expenses\n\n## Open Questions\n\n1. Can a claim mix currencies?\n   - No, one currency per claim.\n", "test");
    // A basis of its own: an answer an earlier test gave here was to a design F1 no longer has.
    designedFrom = { F1: "designed again since" };
    // The workspace reads the feature as out of date once the answer edits its file; here, from the start.
    label = "Update design · 1 feature";
    outOfDate = ["F1"];
    renderCard();
    expect(await screen.findByText("You changed Submit expenses in Spec since it was designed.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Answer 1 question" }));
    const panel = await screen.findByRole("dialog", { name: "Questions for you" });
    fireEvent.click(within(panel).getByRole("button", { name: "No, one currency per claim." }));
    expect(await screen.findByText("Your answer changed Submit expenses: update the design.")).toBeInTheDocument();
  });

  it("does not ask an undesigned feature's open questions, or a question already answered", async () => {
    setDocFile(doc, F1_PATH, "# Submit expenses\n\n## Open Questions\n\n1. Can a claim mix currencies?\n", "test");
    setDocFile(doc, DESIGN_REVIEW_PATH, asked("addressed").replace('"status":"addressed"', '"status":"addressed","reply":"Done.","answer":"Minor unit."'), "test");
    renderCard();
    await screen.findByRole("region", { name: "For technical review" });
    expect(screen.queryByRole("button", { name: /^Answer \d/ })).toBeNull();
  });

  it("makes Design the primary while the spec is ahead of the design", async () => {
    label = "Update design · 1 feature";
    renderCard();
    expect(await screen.findByRole("button", { name: "Update design · 1 feature" })).toHaveClass("MuiButton-contained");
    expect(primaries()).toEqual(["Update design · 1 feature"]);
  });
});

describe("the Comments item", () => {
  it("lists every comment by file, linking to it, with the agent's reply and Reopen", async () => {
    setDocFile(
      doc,
      DESIGN_REVIEW_PATH,
      JSON.stringify({
        version: 1,
        artifacts: [
          {
            path: DECISIONS,
            comments: [
              { id: "c1", author: { id: "u-sam", name: "Sam" }, text: "Why cents?", createdAt: "x", status: "addressed", reply: "Totals never round.", revision: 2 },
            ],
          },
        ],
      }),
      "test",
    );
    renderCard("?art=comments");
    const group = await screen.findByRole("region", { name: "Technical decisions" });
    expect(within(group).getByRole("link", { name: "Technical decisions" })).toHaveAttribute("href", "/projects/acme-expenses/design?art=decisions");
    // The comment's own revision is not shown: the file's title line numbers its revisions.
    expect(group).toHaveTextContent("✓ Addressed: Totals never round.");
    expect(group).not.toHaveTextContent(/revision/i);
    fireEvent.click(within(group).getByRole("button", { name: "Reopen" }));
    await waitFor(() => expect(review().artifacts[0]?.comments?.[0]?.status).toBe("reopened"));
    expect(screen.getByRole("navigation", { name: "Design" })).toHaveTextContent("Comments1 pending");
  });
});

describe("the technical decisions", () => {
  const KEPT = "- \\[data-model\\] Amounts are kept in cents. (F1)";

  it("counts the assumed decisions on the Overview's row", async () => {
    renderCard();
    const technical = await screen.findByRole("region", { name: "For technical review" });
    expect(within(technical).getByLabelText("1 assumed decision")).toHaveTextContent("1 assumed");
  });

  it("keeps an assumed decision in the room, says it was you, and leaves the file checked", async () => {
    renderCard("?art=decisions");
    fireEvent.click(await screen.findByRole("checkbox", { name: "Checked: Technical decisions" }));
    fireEvent.click(screen.getByRole("button", { name: "Keep: [data-model] Amounts are kept in cents. (F1)" }));
    expect(readDocFile(doc, DECISIONS)).toContain(KEPT);
    expect(await screen.findByText("by you")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Keep:/ })).toBeNull();
    expect(screen.getByRole("checkbox", { name: "Checked: Technical decisions" })).toBeChecked();
    expect(screen.queryByText("Changed since you checked it")).toBeNull();
  });

  it("opens the file's 💬 on the decision, quoted read-only, and adds the quote and the change as a Pending draft", async () => {
    renderCard("?art=decisions");
    fireEvent.click(await screen.findByRole("button", { name: "Change: [data-model] Amounts are kept in cents. (F1)" }));
    expect(screen.getByRole("blockquote", { name: "The decision this comment changes" })).toHaveTextContent("[data-model] Amounts are kept in cents. (F1)");
    expect(screen.getByText("Write what you want instead under the quote.")).toBeInTheDocument();
    const box = screen.getByLabelText("Your comment on Technical decisions");
    expect(box).toHaveValue("");
    fireEvent.change(box, { target: { value: "Keep them as decimals." } });
    fireEvent.click(screen.getByRole("button", { name: "Add comment" }));
    expect(review().artifacts.find((a) => a.path === DECISIONS)?.comments?.[0]).toMatchObject({
      text: "> [data-model] Amounts are kept in cents. (F1)\n\nKeep them as decimals.",
      status: "draft",
    });
    expect(readDocFile(doc, DECISIONS)).toContain("*assumed*");
    expect(await screen.findByText("Pending")).toBeInTheDocument();
  });

  it("keeps the quote read-only when its draft is edited", async () => {
    renderCard("?art=decisions");
    fireEvent.click(await screen.findByRole("button", { name: "Change: [data-model] Amounts are kept in cents. (F1)" }));
    fireEvent.change(screen.getByLabelText("Your comment on Technical decisions"), { target: { value: "Keep them as decimals." } });
    fireEvent.click(screen.getByRole("button", { name: "Add comment" }));
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const box = screen.getByLabelText(/^Edit your comment/);
    expect(box).toHaveValue("Keep them as decimals.");
    expect(screen.getByRole("blockquote", { name: "The decision this comment changes" })).toHaveTextContent("[data-model] Amounts are kept in cents. (F1)");
    fireEvent.change(box, { target: { value: "Keep them as decimals, two places." } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(review().artifacts.find((a) => a.path === DECISIONS)?.comments?.[0]?.text).toBe(
      "> [data-model] Amounts are kept in cents. (F1)\n\nKeep them as decimals, two places.",
    );
  });

  it("offers nothing on a decision already confirmed", async () => {
    seed({ ...FILES, [DECISIONS]: "# Technical decisions\n\n- [data-model] Amounts are kept in cents. (F1)\n" });
    renderCard("?art=decisions");
    expect(await screen.findByText("[data-model] Amounts are kept in cents. (F1)")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^(Keep|Change):/ })).toBeNull();
  });
});
