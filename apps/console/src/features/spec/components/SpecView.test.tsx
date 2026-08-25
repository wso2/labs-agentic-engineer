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

import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as Y from "yjs";
import type { components } from "../../../generated/aep-api";
import { START_COMMAND } from "@aep/contracts/commands";
import {
  chatKeyFor,
  consumePendingSeed,
  notifyTurnEnd,
  replaceMessages,
} from "../../agent-chat/chatStore";
import { SpecView } from "./SpecView";

type PreflightItem = components["schemas"]["PreflightItem"];
type BuildInputItem = components["schemas"]["BuildInputItem"];
type BuildResponse = components["schemas"]["BuildResponse"];

// --- Router -----------------------------------------------------------
const mockNavigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => mockNavigate,
  useSearch: () => ({}),
}));

// --- oxygen-ui: only useAppShell needs a stub (it throws outside an
// <AppShell> provider); every other export passes through untouched. -----
vi.mock("@wso2/oxygen-ui", async () => {
  const actual =
    await vi.importActual<typeof import("@wso2/oxygen-ui")>("@wso2/oxygen-ui");
  return {
    ...actual,
    useAppShell: () => ({
      actions: { collapseSidebar: vi.fn(), expandSidebar: vi.fn() },
    }),
  };
});

// --- Collab room: a mutable stub, solo/offline-shaped by default (status
// "offline" exercises the header's "solo session" metadata text — see the
// metadata-line test below). Tests that need a live room (the design.cell
// rewrite-navigation tests) reassign `mockCollab`; the global beforeEach
// resets it. --
const mockFlush = vi.fn().mockResolvedValue(undefined);
const soloCollab = () => ({
  status: "offline",
  // No room doc offline — tests that need one (the question-form block below)
  // assign a real Y.Doc over this.
  doc: null as Y.Doc | null,
  peers: [] as { clientId: number; name: string; color: string; kind: string }[],
  getFileText: (() => null) as (path: string) => Y.Text | null,
  getFileFragment: () => null,
  docPaths: [] as string[],
  provider: null,
  self: { name: "You", color: "#000000" },
  isLocalTransaction: () => false,
  version: 0,
  flush: mockFlush,
  flushError: null as string | null,
  clearFlushError: vi.fn(),
});
let mockCollab = soloCollab();
vi.mock("../collab/useCollabSpec", () => ({
  useCollabSpec: () => mockCollab,
}));

beforeEach(() => {
  mockCollab = soloCollab();
  mockSpecAgent = "";
  mockSpecFlow = "";
});

// --- CellDiagramPanel: its own behavior is covered by
// CellDiagramPanel.test.tsx; here a testid-only stub marks when SpecView's
// selection lands on the Architecture tab. ------------------------------
vi.mock("./CellDiagramPanel", () => ({
  CellDiagramPanel: () => <div data-testid="cell-diagram-panel" />,
}));

vi.mock("../../../auth/SessionContext", () => ({
  useSession: () => ({
    user: { name: "Test User", email: "test@example.com" },
    orgHandle: "acme",
    signOut: vi.fn(),
  }),
}));

// --- Turn-end flush (#252 Task 5): its own behavior is covered by
// useTurnEndFlush.test.tsx — here it's a stub so this file (which mocks every
// query hook wholesale, needing neither a QueryClientProvider nor MSW)
// doesn't have to grow a real QueryClient just to satisfy the real hook's
// useQueryClient() call. A dedicated test below checks SpecView wires it with
// the right chatKey/projectName/collab.
const mockUseTurnEndFlush = vi.fn();
vi.mock("../collab/useTurnEndFlush", () => ({
  useTurnEndFlush: (...args: unknown[]) => mockUseTurnEndFlush(...args),
}));

// --- "Resolve in chat" (#252 Task 9 seam, Task 5's plumbing): its own
// behavior is covered by useResolveDependencyViaChat.test.ts — here it's a
// stub so SpecView's own wiring (which componentName/dep it's called with)
// can be asserted directly. mockUseResolveDependencyViaChat records the
// (org, projectName) the hook itself is called with; mockResolveViaChat
// records the (componentName, dep) the RETURNED callback is invoked with.
const mockResolveViaChat = vi.fn();
const mockUseResolveDependencyViaChat = vi.fn();
mockUseResolveDependencyViaChat.mockReturnValue(mockResolveViaChat);
vi.mock("../../agent-chat/useResolveDependencyViaChat", () => ({
  useResolveDependencyViaChat: (...args: unknown[]) =>
    mockUseResolveDependencyViaChat(...args),
}));

// --- DesignView (#252 Task 9): its own card rendering is covered by
// @aep/ui-design-view's own DesignView.test.tsx — here it's a thin stub
// exposing the props SpecView derives, so this file only asserts SpecView's
// OWN wiring (componentName lookup, dependencyStatus map, callback), not
// DesignView's rendering.
vi.mock("@aep/ui-design-view", () => ({
  DesignView: ({
    design,
    dependencyStatus,
    dependencyUsedBy,
    onResolveDependency,
  }: {
    design: string;
    dependencyStatus?: Record<string, { status?: string; reason?: string }>;
    dependencyUsedBy?: Record<string, string[]>;
    onResolveDependency?: (name: string, intent: "resolve" | "reconsider") => void;
  }) => (
    <div data-testid="design-view">
      <div data-testid="design-view-content">{design}</div>
      <div data-testid="design-view-status">
        {JSON.stringify(dependencyStatus ?? {})}
      </div>
      <div data-testid="design-view-usedby">
        {JSON.stringify(dependencyUsedBy ?? {})}
      </div>
      <button onClick={() => onResolveDependency?.("stripe", "resolve")}>
        Resolve stripe
      </button>
      <button onClick={() => onResolveDependency?.("stripe", "reconsider")}>
        Reconsider stripe
      </button>
    </div>
  ),
}));

// --- Project/spec queries: replaced wholesale so the test needs neither a
// QueryClientProvider nor MSW — only the Build routing under test is real. -
const mockMutateAsync = vi.fn();
const mockPreflightRefetch = vi.fn();
let mockSpecAgent = "";
let mockSpecFlow = "";
vi.mock("../../projects/api/queries", () => ({
  useProject: () => ({ data: { displayName: "Test Project" } }),
  // `spec.agent` (#562) is what tells the workspace whether an agent is working
  // right now. Mutable so the kickoff block below can drive it; the global
  // beforeEach resets it to idle, which is what every other test wants.
  useProjectStatus: () => ({
    data: {
      specStatus: "approved",
      spec: { agent: mockSpecAgent, agentFlow: mockSpecFlow, designOutdated: false },
    },
  }),
  useProjectTags: () => ({ data: { latest: "v1", specDirty: false } }),
  useBuildProject: () => ({ mutateAsync: mockMutateAsync }),
  useBuildPreflight: () => ({ refetch: mockPreflightRefetch }),
}));

// --- Spec queries: delegated through vi.fn()s (rather than fixed inline
// factories) so individual describe blocks can override the fixture — the
// dependency-wiring tests below need a component design.json file + content,
// which the base Build-routing tests don't. The top-level beforeEach sets
// the shared baseline; each describe's own beforeEach layers overrides.
const mockUseSpecFiles = vi.fn();
const mockUseSpecFileContent = vi.fn();
const mockUseDesignDependencies = vi.fn();

vi.mock("../api/queries", () => ({
  useSpecFiles: (...args: unknown[]) => mockUseSpecFiles(...args),
  useSpecFileContent: (...args: unknown[]) => mockUseSpecFileContent(...args),
  useDesignDependencies: (...args: unknown[]) =>
    mockUseDesignDependencies(...args),
}));

// --- BuildDependencyDrawer: its own behavior is covered by
// BuildDependencyDrawer.test.tsx, so here it's a thin stub that exposes
// Continue/Cancel so tests can drive SpecView's routing without re-deriving
// real dependency-form state. ------------------------------------------
const STUB_INPUTS: BuildInputItem[] = [
  { component: "checkout-api", dependency: "postgres", kind: "platform-resource", approved: true },
];
vi.mock("./BuildDependencyDrawer", () => ({
  BuildDependencyDrawer: ({
    open,
    items,
    onClose,
    onContinue,
    onResolveDependency,
  }: {
    open: boolean;
    items: PreflightItem[];
    onClose: () => void;
    onContinue: (inputs: BuildInputItem[]) => void;
    onResolveDependency?: (
      item: PreflightItem,
      intent: "resolve" | "reconsider",
    ) => void;
  }) =>
    open ? (
      <div data-testid="dependency-drawer">
        <button onClick={() => onContinue(STUB_INPUTS)}>Drawer Continue</button>
        <button onClick={onClose}>Drawer Cancel</button>
        {items[0] ? (
          <button onClick={() => onResolveDependency?.(items[0]!, "resolve")}>
            Resolve drawer item
          </button>
        ) : null}
        {items[0] ? (
          <button onClick={() => onResolveDependency?.(items[0]!, "reconsider")}>
            Reconsider drawer item
          </button>
        ) : null}
      </div>
    ) : null,
}));

const PREFLIGHT_ITEMS: PreflightItem[] = [
  {
    component: "checkout-api",
    dependency: "postgres",
    kind: "platform-resource",
    description: "Postgres database",
    resourceType: "postgres",
  },
];

function clickBuild() {
  fireEvent.click(screen.getByRole("button", { name: "Build" }));
}

// Base fixture shared by every describe block below: a single non-component
// markdown file, no content loaded yet, no dependency data. Individual
// blocks override what they need (e.g. the dependency-wiring tests below
// swap in a component design.json + its loaded content).
const BASE_FILES = [
  { path: "specs/design/overview.md", sha: "abc", group: "designs" },
];

beforeEach(() => {
  vi.clearAllMocks();
  // clearAllMocks() clears recorded CALLS, not a queued mockResolvedValueOnce:
  // a one-shot value a test queued but never consumed survives into the NEXT
  // test and answers its first call. That turns one failure into two — the
  // drawer tests below queue one-shots, so a single missed refetch strands a
  // `needsInput:false`, which then tells the following test's Build click that
  // nothing is unresolved and its drawer never opens. Reset the queue instead,
  // so each test starts from an empty one and a failure stays where it began.
  mockPreflightRefetch.mockReset();
  mockUseSpecFiles.mockReturnValue({
    data: BASE_FILES,
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
  });
  mockUseSpecFileContent.mockReturnValue({
    data: undefined,
    isPending: true,
    isError: false,
    error: null,
    refetch: vi.fn(),
  });
  mockUseDesignDependencies.mockReturnValue({
    data: [],
    isPending: false,
    isError: false,
    error: null,
  });
});

// Opening the spec before the interview has asked anything (#562). The kickoff
// fires at project creation, so this is a real arrival — the user clicks
// through from the overview while the agent is still writing.
describe("SpecView while the kickoff is still writing", () => {
  const withFiles = (data: { path: string; sha: string; group: string }[]) =>
    mockUseSpecFiles.mockReturnValue({
      data,
      isPending: false,
      isError: false,
      error: null,
      refetch: vi.fn(),
    });
  // Nothing written yet — the state a kickoff occupies for its whole first turn.
  const empty = () => withFiles([]);
  // A project past its kickoff: the PRD exists, so nothing here is about
  // requirements any more.
  const published = () =>
    withFiles([{ path: "specs/requirements/prd.md", sha: "abc", group: "requirements" }]);

  it("says what is happening instead of offering an empty picker", () => {
    mockSpecAgent = "working";
    mockSpecFlow = "start";
    empty();
    render(<SpecView projectName="proj1" />);

    expect(
      screen.getByText("Agent is working on the requirements document"),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Select a file to view its content."),
    ).not.toBeInTheDocument();
  });

  // The body says an agent is working ONLY when the rail pulses for the same
  // reason. It used to claim it whenever the workspace was empty, so between
  // turns the rail showed Requirements as not started while the body beside it
  // insisted an agent was writing — the two surfaces contradicting each other.
  it("does not claim work the rail is not showing", () => {
    mockSpecAgent = "";
    mockSpecFlow = "";
    empty();
    render(<SpecView projectName="proj1" />);

    expect(
      screen.queryByText("Agent is working on the requirements document"),
    ).not.toBeInTheDocument();
    expect(screen.getByText("Nothing written yet")).toBeInTheDocument();
  });

  // A failure has its own banner with its own way out; spinning underneath it
  // would promise work that already stopped.
  it("stops spinning once the turn has failed", () => {
    mockSpecAgent = "failed";
    empty();
    render(<SpecView projectName="proj1" />);

    expect(
      screen.queryByText("Agent is working on the requirements document"),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText("The agent couldn't write your requirements"),
    ).toBeInTheDocument();
  });


  // `spec.agent` is PROJECT-wide — the newest turn of any flow. A design pass
  // on a project whose PRD shipped months ago is an agent working, but not on
  // the requirements, and not on anything this workspace should re-explain.
  it("does not claim requirements work while a later flow runs", () => {
    mockSpecAgent = "working";
    published();
    render(<SpecView projectName="proj1" />);

    expect(
      screen.queryByText("Agent is working on the requirements document"),
    ).not.toBeInTheDocument();
  });

  // The failure banner was unreachable before #562 wired a real signal into it.
  // Unscoped it would pin a red alert across a healthy published spec after any
  // The alert owns the failed case, so the body must add nothing to it. It used
  // to fall through to "Select a file to view its content." — `nothingToShow`
  // excludes `failed`, and an empty workspace has no file to select.
  it("leaves the body empty under the failure banner", () => {
    mockSpecAgent = "failed";
    empty();
    render(<SpecView projectName="proj1" />);

    expect(screen.getByText("The agent couldn't write your requirements")).toBeInTheDocument();
    expect(screen.queryByText("Select a file to view its content.")).not.toBeInTheDocument();
    expect(screen.queryByText("Nothing written yet")).not.toBeInTheDocument();
  });

  // turn failed; a kickoff that died is the one failure leaving nothing behind.
  // The one state that can be KNOWN rather than inferred: a turn that started
  // and then died. So it is the only one carrying a way out.
  it("banners a failed kickoff, and offers Retry there", () => {
    mockSpecAgent = "failed";
    empty();
    const { unmount } = render(<SpecView projectName="proj1" />);
    expect(
      screen.getByText("The agent couldn't write your requirements"),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    // GUARDED: this surface cannot see an interview that ended on a question.
    // The panel re-decides after it has rehydrated, which is the first moment
    // that is knowable.
    expect(consumePendingSeed(chatKeyFor("acme", "proj1"))).toEqual({
      message: START_COMMAND,
      guarded: true,
    });
    unmount();

    mockSpecAgent = "failed";
    published();
    render(<SpecView projectName="proj1" />);
    expect(
      screen.queryByText("The agent couldn't write your requirements"),
    ).not.toBeInTheDocument();
  });

  // The dead end this closed (#562 review): a dispatch that never reached the
  // turn guard — no Anthropic key, an unreachable skills repo — or an abandoned
  // reference upload the create held the kickoff for. There is no turn row, so
  // nothing "failed", and the spinner promised work that was never coming with
  // nothing to click.
  it("offers a way out when no turn has ever run", () => {
    mockSpecAgent = "never-started";
    mockSpecFlow = "";
    empty();
    render(<SpecView projectName="proj1" />);

    expect(
      screen.queryByText("Agent is working on the requirements document"),
    ).not.toBeInTheDocument();
    expect(screen.getByText("Nothing written yet")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(consumePendingSeed(chatKeyFor("acme", "proj1"))).toEqual({
      message: START_COMMAND,
      guarded: true,
    });
  });

  // A design run is not requirements work, so the requirements body says
  // nothing about it.
  it("does not claim requirements work during a design run", () => {
    mockSpecAgent = "working";
    mockSpecFlow = "design";
    empty();
    render(<SpecView projectName="proj1" />);

    expect(
      screen.queryByText("Agent is working on the requirements document"),
    ).not.toBeInTheDocument();
  });

  // An empty workspace offers NOTHING (#562 retest). It used to carry a Start
  // button, which appeared during the kickoff itself — the moment the user must
  // not be invited to restart it — because "the workspace looks empty" is true
  // for a while before the agent's first write lands.
  it("offers no action while an agent is actually writing", () => {
    mockSpecAgent = "working";
    mockSpecFlow = "start";
    empty();
    render(<SpecView projectName="proj1" />);

    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  });
});

describe("SpecView never opens a reference document (#383)", () => {
  // References are transient turn inputs and are never committed (ADR-0017),
  // so `toSpecEntry` drops them and they cannot reach the file list from git.
  // The room is the other way in: a project created under the feature's v1 has
  // them in git, so a room seeded from that HEAD still carries the paths. This
  // exercises SpecView's own collab-union call to `toSpecEntry` — the union is
  // where a room path becomes an entry — and pins that a reference path never
  // becomes one, and so never reaches the pane as base64 editor text.
  it("drops a reference path arriving from the collab room", () => {
    mockCollab = {
      ...soloCollab(),
      docPaths: ["specs/requirements/references/claim-form.pdf"],
    };
    mockUseSpecFiles.mockReturnValue({
      data: [],
      isPending: false,
      isError: false,
      error: null,
      refetch: vi.fn(),
    });
    render(<SpecView projectName="proj1" />);

    expect(
      screen.queryByText("claim-form.pdf"),
    ).not.toBeInTheDocument();
    // The content hook is disabled (empty path) rather than pointed at it.
    const requestedPaths = mockUseSpecFileContent.mock.calls.map(
      (c) => (c[1] as { path: string } | null)?.path ?? null,
    );
    expect(requestedPaths).not.toContain(
      "specs/requirements/references/claim-form.pdf",
    );
  });

  it("a requirements file still wins the default as before", () => {
    mockUseSpecFiles.mockReturnValue({
      data: [
        {
          path: "specs/requirements/references/claim-form.pdf",
          sha: "r1",
          group: "references",
          size: 868409,
        },
        { path: "specs/requirements/prd.md", sha: "p1", group: "requirements" },
      ],
      isPending: false,
      isError: false,
      error: null,
      refetch: vi.fn(),
    });
    render(<SpecView projectName="proj1" />);
    const requestedPaths = mockUseSpecFileContent.mock.calls.map(
      (c) => (c[1] as { path: string } | null)?.path ?? null,
    );
    expect(requestedPaths).toContain("specs/requirements/prd.md");
    expect(requestedPaths).not.toContain(
      "specs/requirements/references/claim-form.pdf",
    );
  });
});

describe("SpecView onBuild routing (#164)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockFlush.mockResolvedValue(undefined);
  });

  it("wires useTurnEndFlush with the chat's key, the project name, and the live collab (#252 Task 5)", () => {
    render(<SpecView projectName="proj1" />);
    expect(mockUseTurnEndFlush).toHaveBeenCalledWith(
      "aep.chat.v1.acme.proj1",
      "proj1",
      expect.objectContaining({ status: "offline", flush: mockFlush }),
    );
  });

  it("needsInput:false — shows the Cut-version ceremony; confirming builds and navigates (#370/#372)", async () => {
    mockPreflightRefetch.mockResolvedValue({ data: { needsInput: false, items: [] } });
    mockMutateAsync.mockResolvedValue({ tag: "v1" } satisfies BuildResponse);

    render(<SpecView projectName="proj1" />);
    clickBuild();

    // The ceremony intervenes: nothing POSTs until the user confirms — the
    // BACKEND cuts the real tag on confirm.
    const dialog = await screen.findByTestId("cut-version-dialog");
    const confirm = within(dialog).getByRole("button", { name: /cut v\d+ & build/i, hidden: true });
    expect(mockMutateAsync).not.toHaveBeenCalled();
    fireEvent.click(confirm);

    await waitFor(() =>
      expect(mockMutateAsync).toHaveBeenCalledWith({ inputs: [] }),
    );
    expect(mockNavigate).toHaveBeenCalledWith({
      to: "/projects/$projectName",
      params: { projectName: "proj1" },
    });
    expect(screen.queryByTestId("dependency-drawer")).not.toBeInTheDocument();
  });

  it("preflight refetch errors — surfaces the failure and does not build or navigate", async () => {
    mockPreflightRefetch.mockResolvedValue({
      data: undefined,
      isError: true,
      error: new Error("boom"),
    });

    render(<SpecView projectName="proj1" />);
    clickBuild();

    await waitFor(() => expect(screen.getByText("boom")).toBeInTheDocument());
    expect(mockMutateAsync).not.toHaveBeenCalled();
    expect(mockNavigate).not.toHaveBeenCalled();
    expect(screen.queryByTestId("dependency-drawer")).not.toBeInTheDocument();
  });

  it("needsInput:true — opens the dependency drawer and does not build", async () => {
    mockPreflightRefetch.mockResolvedValue({
      data: { needsInput: true, items: PREFLIGHT_ITEMS },
    });

    render(<SpecView projectName="proj1" />);
    clickBuild();

    await waitFor(() =>
      expect(screen.getByTestId("dependency-drawer")).toBeInTheDocument(),
    );
    expect(mockMutateAsync).not.toHaveBeenCalled();
  });

  it("drawer Continue with a clean BuildResponse — builds with the drawer's inputs, closes, navigates", async () => {
    mockPreflightRefetch.mockResolvedValue({
      data: { needsInput: true, items: PREFLIGHT_ITEMS },
    });
    mockMutateAsync.mockResolvedValue({ tag: "v2" } satisfies BuildResponse);

    render(<SpecView projectName="proj1" />);
    clickBuild();
    await waitFor(() =>
      expect(screen.getByTestId("dependency-drawer")).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByText("Drawer Continue"));

    await waitFor(() =>
      expect(mockMutateAsync).toHaveBeenCalledWith({ inputs: STUB_INPUTS }),
    );
    expect(mockNavigate).toHaveBeenCalledWith({
      to: "/projects/$projectName",
      params: { projectName: "proj1" },
    });
    expect(screen.queryByTestId("dependency-drawer")).not.toBeInTheDocument();
  });

  it("drawer Continue with failures — keeps the drawer open and surfaces the failure reasons", async () => {
    mockPreflightRefetch.mockResolvedValue({
      data: { needsInput: true, items: PREFLIGHT_ITEMS },
    });
    mockMutateAsync.mockResolvedValue({
      failures: [{ dependency: "postgres", reason: "provisioning timed out" }],
    } satisfies BuildResponse);

    render(<SpecView projectName="proj1" />);
    clickBuild();
    await waitFor(() =>
      expect(screen.getByTestId("dependency-drawer")).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByText("Drawer Continue"));

    await waitFor(() =>
      expect(
        screen.getByText(/postgres: provisioning timed out/i),
      ).toBeInTheDocument(),
    );
    expect(screen.getByTestId("dependency-drawer")).toBeInTheDocument();
    expect(mockNavigate).not.toHaveBeenCalled();
  });
});

// --- #252 Task 9: dependency-status wiring ---------------------------------
type ComponentDependencies = components["schemas"]["ComponentDependencies"];

const CHECKOUT_DESIGN_JSON = JSON.stringify({
  name: "checkout-api",
  dependencies: [{ kind: "external", name: "stripe" }],
});

const CHECKOUT_DEPS: ComponentDependencies[] = [
  {
    componentName: "checkout-api",
    dependencies: [
      { kind: "external", name: "stripe", status: "unresolved", reason: "needs-input" },
    ],
  },
];

describe("SpecView dependency wiring (#252 Task 9)", () => {
  beforeEach(() => {
    mockUseSpecFiles.mockReturnValue({
      data: [
        {
          path: "specs/design/components/checkout-api/design.json",
          sha: "abc",
          group: "designs",
        },
      ],
      isPending: false,
      isError: false,
      error: null,
      refetch: vi.fn(),
    });
    mockUseSpecFileContent.mockReturnValue({
      data: { sha: "abc", content: CHECKOUT_DESIGN_JSON },
      isPending: false,
      isError: false,
      error: null,
      refetch: vi.fn(),
    });
    mockUseDesignDependencies.mockReturnValue({
      data: CHECKOUT_DEPS,
      isPending: false,
      isError: false,
      error: null,
    });
  });

  it("wires useDesignDependencies with the project name", () => {
    render(<SpecView projectName="proj1" />);
    expect(mockUseDesignDependencies).toHaveBeenCalledWith("proj1");
  });

  it("wires useResolveDependencyViaChat with the default-fallback org and the project name", () => {
    render(<SpecView projectName="proj1" />);
    // useSession's mock above sets orgHandle: "acme" explicitly, so the
    // "default" fallback never actually kicks in here — but the call proves
    // SpecView passes orgHandle through rather than hardcoding a value.
    expect(mockUseResolveDependencyViaChat).toHaveBeenCalledWith("acme", "proj1");
  });

  it("passes the selected component's dependency status map to DesignView, keyed by dependency name", () => {
    render(<SpecView projectName="proj1" />);
    const status = JSON.parse(
      screen.getByTestId("design-view-status").textContent ?? "{}",
    );
    expect(status).toEqual({
      stripe: { status: "unresolved", reason: "needs-input" },
    });
  });

  it('"Resolve in chat" fires the resolve callback with the component name, the full endpoint dependency entry, and the RESOLVE intent', () => {
    render(<SpecView projectName="proj1" />);
    fireEvent.click(screen.getByText("Resolve stripe"));
    expect(mockResolveViaChat).toHaveBeenCalledWith(
      "checkout-api",
      CHECKOUT_DEPS[0]!.dependencies![0],
      "resolve",
    );
  });

  // #252 Task 17: the hamburger's "Discuss in chat & modify" — same lookup,
  // but the RECONSIDER intent.
  it('"Discuss in chat & modify" fires the resolve callback with the RECONSIDER intent', () => {
    render(<SpecView projectName="proj1" />);
    fireEvent.click(screen.getByText("Reconsider stripe"));
    expect(mockResolveViaChat).toHaveBeenCalledWith(
      "checkout-api",
      CHECKOUT_DEPS[0]!.dependencies![0],
      "reconsider",
    );
  });

  // #252 Task 15: cross-component "Used by", computed across EVERY
  // component's dependencies (not just the selected one) and keyed by the
  // selected component's own dependency names.
  it("passes a cross-component 'Used by' map to DesignView for a dependency shared with another component", () => {
    const SHARED_DEPS: ComponentDependencies[] = [
      {
        componentName: "checkout-api",
        dependencies: [
          { kind: "external", name: "stripe", status: "unresolved", reason: "needs-input" },
          { kind: "platform-resource", name: "thunder-app", resourceType: "auth" },
        ],
      },
      {
        componentName: "checkout-web",
        dependencies: [
          { kind: "platform-resource", name: "thunder-app", resourceType: "auth" },
        ],
      },
    ];
    mockUseDesignDependencies.mockReturnValue({
      data: SHARED_DEPS,
      isPending: false,
      isError: false,
      error: null,
    });

    render(<SpecView projectName="proj1" />);
    const usedBy = JSON.parse(
      screen.getByTestId("design-view-usedby").textContent ?? "{}",
    );
    expect(usedBy).toEqual({
      "thunder-app": ["checkout-api", "checkout-web"],
    });
    // stripe is component-local (only checkout-api declares it) — no entry.
    expect(usedBy).not.toHaveProperty("stripe");
  });
});

// --- #252 Task 10: build dependency drawer wiring --------------------------
describe("SpecView build dependency drawer (#252 Task 10)", () => {
  const DRAWER_PREFLIGHT_ITEMS: PreflightItem[] = [
    {
      component: "checkout-api",
      dependency: "stripe",
      kind: "external-unresolved",
      description: "Needs information only you can provide.",
    },
  ];

  beforeEach(() => {
    mockFlush.mockResolvedValue(undefined);
    mockUseDesignDependencies.mockReturnValue({
      data: CHECKOUT_DEPS,
      isPending: false,
      isError: false,
      error: null,
    });
  });

  it("resolves a drawer blocker item to its full Dependency entry and fires the seeded chat flow", async () => {
    mockPreflightRefetch.mockResolvedValue({
      data: { needsInput: true, items: DRAWER_PREFLIGHT_ITEMS },
    });

    render(<SpecView projectName="proj1" />);
    clickBuild();
    await waitFor(() =>
      expect(screen.getByTestId("dependency-drawer")).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByText("Resolve drawer item"));

    // Same lookup precedent as "Resolve in chat" above: the FULL endpoint
    // entry (status/reason included), never a hand-built partial object —
    // looked up by (item.component, item.dependency), not the currently
    // selected file's component (the drawer can span any component). The
    // RESOLVE intent, since this is the blocker panel's chat button.
    expect(mockResolveViaChat).toHaveBeenCalledWith(
      "checkout-api",
      CHECKOUT_DEPS[0]!.dependencies![0],
      "resolve",
    );
  });

  // #252 Task 17: the drawer's hamburger ("Discuss in chat & modify") — same
  // lookup, but the RECONSIDER intent, and it also closes the drawer.
  it("resolves a drawer hamburger action to its full Dependency entry, fires the RECONSIDER intent, and closes the drawer", async () => {
    mockPreflightRefetch.mockResolvedValue({
      data: { needsInput: true, items: DRAWER_PREFLIGHT_ITEMS },
    });

    render(<SpecView projectName="proj1" />);
    clickBuild();
    await waitFor(() =>
      expect(screen.getByTestId("dependency-drawer")).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByText("Reconsider drawer item"));

    expect(mockResolveViaChat).toHaveBeenCalledWith(
      "checkout-api",
      CHECKOUT_DEPS[0]!.dependencies![0],
      "reconsider",
    );
    await waitFor(() =>
      expect(screen.queryByTestId("dependency-drawer")).not.toBeInTheDocument(),
    );
  });

  it("refetches preflight and updates the still-open drawer's items when a chat turn ends", async () => {
    mockPreflightRefetch
      .mockResolvedValueOnce({
        data: { needsInput: true, items: DRAWER_PREFLIGHT_ITEMS },
      })
      .mockResolvedValueOnce({ data: { needsInput: false, items: [] } });

    render(<SpecView projectName="proj1" />);
    clickBuild();
    await waitFor(() =>
      expect(screen.getByText("Resolve drawer item")).toBeInTheDocument(),
    );
    // PAINTED is not SUBSCRIBED. The drawer's items land in a commit, but the
    // effect that registers SpecView's turn-end listener is a passive effect
    // React flushes after that commit — and `waitFor` resolves on the DOM
    // mutation itself. Notifying in that gap dispatches to nobody: no refetch,
    // no state change, and the assertion below then burns its whole budget
    // staring at an unchanged drawer. Drain the pending effects first so the
    // listener is provably live before the turn ends.
    await act(async () => {});

    // The seeded chat turn ends — same chatKey SpecView computes
    // (orgHandle "acme" from the useSession mock, matching AppLayout's
    // "default" fallback convention when there's no claim).
    // Build already flushed once on its way here, so the listener's own flush
    // has to be counted, not merely observed.
    const flushesBeforeTurnEnd = mockFlush.mock.calls.length;
    notifyTurnEnd(chatKeyFor("acme", "proj1"), "completed");
    // notifyTurnEnd dispatches SYNCHRONOUSLY and the listener's first act is
    // that flush, so this pins "a listener actually ran" right here — rather
    // than leaving it to be inferred from an unchanged drawer five seconds on.
    expect(mockFlush).toHaveBeenCalledTimes(flushesBeforeTurnEnd + 1);

    await waitFor(() =>
      expect(screen.queryByText("Resolve drawer item")).not.toBeInTheDocument(),
    );
    expect(mockPreflightRefetch).toHaveBeenCalledTimes(2);
  });

  it("does not touch preflight on a chat turn ending while the drawer is closed", () => {
    render(<SpecView projectName="proj1" />);

    notifyTurnEnd(chatKeyFor("acme", "proj1"), "completed");

    expect(mockPreflightRefetch).not.toHaveBeenCalled();
    expect(mockFlush).not.toHaveBeenCalled();
  });

  // #252 Task 15: the drawer is a MUI overlay — left open after "Resolve via
  // chat" it covers the chat panel the seeded message just opened, so the
  // user can't see what they're meant to respond to. Closing it is this
  // handler's job, alongside firing the seeded chat flow.
  it('closes the dependency drawer when "Resolve via chat" is clicked, so the seeded chat is visible', async () => {
    mockPreflightRefetch.mockResolvedValue({
      data: { needsInput: true, items: DRAWER_PREFLIGHT_ITEMS },
    });

    render(<SpecView projectName="proj1" />);
    clickBuild();
    await waitFor(() =>
      expect(screen.getByTestId("dependency-drawer")).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByText("Resolve drawer item"));

    // The seeded chat flow still fires (same lookup as the test above)...
    expect(mockResolveViaChat).toHaveBeenCalledWith(
      "checkout-api",
      CHECKOUT_DEPS[0]!.dependencies![0],
      "resolve",
    );
    // ...and the drawer closes so the chat panel it opens is actually visible.
    await waitFor(() =>
      expect(screen.queryByTestId("dependency-drawer")).not.toBeInTheDocument(),
    );
  });
});

describe("SpecView architecture-tab navigation on design.cell change", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockFlush.mockResolvedValue(undefined);
  });

  // A connected room whose doc carries design.cell. An agent editFile lands as
  // an in-place Y.Text patch (useYTextString observes it directly); a
  // restructure's removeFile deletes the Y.Map entry, whose re-render the real
  // hook receives via useCollabSpec's version bump — simulated here with a
  // reassignment + rerender.
  function connectedRoom(withAgent: boolean) {
    const doc = new Y.Doc();
    const files = doc.getMap<Y.Text>("files");
    const ytext = new Y.Text();
    files.set("specs/design/design.cell", ytext);
    ytext.insert(0, "title X\ncomponent api service\n");
    const collab = {
      ...soloCollab(),
      status: "connected",
      peers: withAgent
        ? [{ clientId: 1, name: "Agent", color: "#000000", kind: "agent" }]
        : [],
      getFileText: (path: string) => files.get(path) ?? null,
    };
    return { files, ytext, collab };
  }

  it("navigates to the Architecture tab when the agent patches design.cell in place", () => {
    const room = connectedRoom(true);
    mockCollab = room.collab;

    render(<SpecView projectName="proj1" />);
    expect(screen.queryByTestId("cell-diagram-panel")).not.toBeInTheDocument();

    act(() => {
      room.ytext.insert(room.ytext.length, "south email-provider service\n");
    });

    expect(screen.getByTestId("cell-diagram-panel")).toBeInTheDocument();
  });

  it("navigates on a restructure (removeFile deletes the doc entry)", () => {
    const room = connectedRoom(true);
    mockCollab = room.collab;

    const { rerender } = render(<SpecView projectName="proj1" />);
    expect(screen.queryByTestId("cell-diagram-panel")).not.toBeInTheDocument();

    room.files.delete("specs/design/design.cell");
    mockCollab = { ...room.collab, version: 1 };
    rerender(<SpecView projectName="proj1" />);

    expect(screen.getByTestId("cell-diagram-panel")).toBeInTheDocument();
  });

  it("does not navigate when no agent peer is in the room", () => {
    const room = connectedRoom(false);
    mockCollab = room.collab;

    render(<SpecView projectName="proj1" />);

    act(() => {
      room.ytext.insert(room.ytext.length, "south email-provider service\n");
    });

    expect(screen.queryByTestId("cell-diagram-panel")).not.toBeInTheDocument();
  });
});

describe("SpecView header metadata (soft version chips)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockFlush.mockResolvedValue(undefined);
  });

  it("renders session/version info as soft status chips (not buttons) and drops 'Approved'", () => {
    render(<SpecView projectName="proj1" />);

    // Version + session state render as soft status chips beside the title
    // (consistent with the builds/deployments headers): "v1 · published"
    // (tags.latest) and "solo session" (offline collab).
    expect(screen.getByText("v1 · published")).toBeInTheDocument();
    expect(screen.getByText("solo session")).toBeInTheDocument();

    // The old "Approved" status chip is gone entirely (specStatus is
    // "approved" in this test's project-status mock).
    expect(screen.queryByText("Approved")).not.toBeInTheDocument();

    // Build remains the header's only button-like control — the soft chips
    // are Chips, not buttons.
    expect(screen.getByRole("button", { name: "Build" })).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /solo|published/i }),
    ).not.toBeInTheDocument();
  });
});

// The interview is uncapped (#578), so the PRD is written in the FIRST round
// and every later round asks over a populated spec view. That flipped a state
// that used to be unreachable: requirements files exist while a question form
// is standing, which is exactly when the header's requirements-gated launchers
// light up. Firing one supersedes the live questions — the agent then answers
// its own questions from assumptions — so they stand down until the form is
// answered. `agentBusy` cannot cover this: the agent's turn ENDED on the
// question, so no agent peer is in the room.
describe("SpecView while the agent is waiting on answers", () => {
  const CHAT_KEY = chatKeyFor("acme", "proj1");
  const REQUIREMENTS_FILES = [
    { path: "specs/requirements/prd.md", sha: "p1", group: "requirements" },
  ];

  /** Seed the log with one live question card and give the room a real doc. */
  function askQuestion(): void {
    mockCollab = { ...soloCollab(), doc: new Y.Doc() };
    replaceMessages(CHAT_KEY, [
      {
        id: "m1",
        role: "question",
        turnId: "t1",
        toolCallId: "call-1",
        questions: [{ question: "Which of these did I get wrong?", options: [] }],
      },
    ]);
  }

  beforeEach(() => {
    replaceMessages(CHAT_KEY, []);
    mockUseSpecFiles.mockReturnValue({
      data: REQUIREMENTS_FILES,
      isPending: false,
      isError: false,
      error: null,
      refetch: vi.fn(),
    });
  });

  it("offers the launchers and Generate design once the questions are answered", () => {
    render(<SpecView projectName="proj1" />);

    expect(screen.getByRole("button", { name: "+ Feature" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Generate design/ })).toBeEnabled();
  });

  it("stands them down while a question form is open", () => {
    askQuestion();
    render(<SpecView projectName="proj1" />);

    expect(screen.getByTestId("spec-question-form")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "+ Feature" })).toBeNull();
    expect(screen.getByRole("button", { name: /Generate design/ })).toBeDisabled();
  });

  // A seeded command does NOT go through the composer: `AgentChatPanel` sends
  // a pending seed as soon as the conversation is ready, without the
  // `inputDisabled` guard that stops a user typing mid-turn. So an ungated
  // launcher delivers `/feature` into a running turn — the thing the composer
  // beside it refuses.
  it("stands + Feature down while an agent holds the turn", () => {
    mockCollab = {
      ...soloCollab(),
      peers: [{ clientId: 1, name: "Agent", color: "#fff", kind: "agent" }],
    };
    render(<SpecView projectName="proj1" />);

    expect(screen.getByRole("button", { name: "+ Feature" })).toBeDisabled();
    expect(screen.getByRole("button", { name: /Generate design/ })).toBeDisabled();
  });
});

// Designing against the agent's own guesses is ordinary use, not a mistake —
// gating it was tried and removed (#539). But it has a cost the button does not
// show: the design is derived from those guesses, and overturning one later
// means deriving again. So the click warns and lets the user go on.
describe("SpecView warns before designing against unsettled requirements", () => {
  const REQUIREMENTS_FILES = [
    { path: "specs/requirements/prd.md", sha: "p1", group: "requirements" },
  ];
  const UNSETTLED = [
    "## User Stories",
    "",
    "1. As a manager, I approve claims *assumed* single approver",
    "",
    "## Open Questions",
    "",
    "- Which payroll vendor?",
  ].join("\n");
  const SETTLED = ["## User Stories", "", "1. As a manager, I approve claims"].join("\n");

  function seed(prd: string): void {
    mockUseSpecFiles.mockReturnValue({
      data: REQUIREMENTS_FILES,
      isPending: false,
      isError: false,
      error: null,
      refetch: vi.fn(),
    });
    mockUseSpecFileContent.mockReturnValue({
      data: { sha: "p1", content: prd },
      isPending: false,
      isError: false,
      error: null,
      refetch: vi.fn(),
    });
  }

  const clickGenerate = () =>
    fireEvent.click(screen.getByRole("button", { name: /Generate design/ }));

  it("goes straight to the design run when nothing is unsettled", () => {
    seed(SETTLED);
    render(<SpecView projectName="proj1" />);
    clickGenerate();

    expect(mockNavigate).toHaveBeenCalledWith(
      expect.objectContaining({ search: { generate: "design" } }),
    );
  });

  // The count is what the rail already shows, said in the same words — one
  // account of what is unsettled, not two that can drift apart.
  it("names what is unsettled instead of designing", () => {
    seed(UNSETTLED);
    render(<SpecView projectName="proj1" />);
    clickGenerate();

    expect(screen.getByText("1 open question")).toBeInTheDocument();
    expect(screen.getByText("1 assumption to challenge")).toBeInTheDocument();
    expect(mockNavigate).not.toHaveBeenCalled();
  });

  // A warning, not a gate: the way past is the primary action.
  it("designs anyway when the user says so", () => {
    seed(UNSETTLED);
    render(<SpecView projectName="proj1" />);
    clickGenerate();
    fireEvent.click(screen.getByRole("button", { name: "Generate anyway" }));

    expect(mockNavigate).toHaveBeenCalledWith(
      expect.objectContaining({ search: { generate: "design" } }),
    );
  });

  // The other way out gets out of the user's way — it does not leave them on a
  // dialog they have already answered, and it starts no design run.
  it("stands down without designing when the user goes to resolve", async () => {
    seed(UNSETTLED);
    render(<SpecView projectName="proj1" />);
    clickGenerate();
    fireEvent.click(screen.getByRole("button", { name: "Resolve issues" }));

    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Generate anyway" })).toBeNull(),
    );
    expect(mockNavigate).not.toHaveBeenCalled();
  });
});
