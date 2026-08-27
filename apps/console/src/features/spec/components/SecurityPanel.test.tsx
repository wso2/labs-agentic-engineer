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

import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type {
  ProjectRoleState,
  ProjectRolesLiveState,
  ProjectTestUserState,
} from "../api/roles";
import { serializeRolesDesign, type RolesDesign } from "../api/rolesDesign";
import { SecurityPanel, type AccountActions } from "./SecurityPanel";

afterEach(cleanup);

function role(name: string): RolesDesign["roles"][number] {
  return {
    name,
    description: `What ${name} may do`,
    stories: [1],
    grantedBy: "an administrator",
    permissions: [{ component: "orders-api", actions: ["read"] }],
  };
}

function design(over: Partial<RolesDesign> = {}): string {
  return serializeRolesDesign({
    version: 1,
    coldStartRole: null,
    publicComponents: [],
    roles: [role("Admin")],
    testUsers: [],
    ...over,
  });
}

function liveRole(
  name: string,
  over: Partial<ProjectRoleState> = {},
): ProjectRoleState {
  return { name, platformCreated: true, ...over };
}

function liveUser(
  username: string,
  over: Partial<ProjectTestUserState> = {},
): ProjectTestUserState {
  return {
    username,
    roleName: "Admin",
    coldStart: false,
    exists: true,
    owned: true,
    supplied: false,
    ...over,
  };
}

function live(
  over: Partial<ProjectRolesLiveState> = {},
): ProjectRolesLiveState {
  return { directoryAvailable: true, roles: [], testUsers: [], ...over };
}

/**
 * Renders the panel and, by default, opens **Roles & users**.
 *
 * The panel itself lands on Security architecture — the prose half leads — but
 * most of what there is to assert here is the roster, so `setup` walks to it
 * rather than making every test click. Pass `{ stayOnDefaultTab: true }` for the
 * few tests that are about the landing tab itself.
 */
function setup(
  props: Partial<React.ComponentProps<typeof SecurityPanel>> = {},
  { stayOnDefaultTab = false }: { stayOnDefaultTab?: boolean } = {},
) {
  const onRolesChange = vi.fn();
  const merged = {
    rolesJson: design(),
    prose: <div data-testid="prose">the security architecture editor</div>,
    ...props,
  };
  render(
    <SecurityPanel
      {...merged}
      onRolesChange={
        "onRolesChange" in props ? props.onRolesChange : onRolesChange
      }
    />,
  );
  if (!stayOnDefaultTab) {
    fireEvent.click(screen.getByRole("tab", { name: "Roles & users" }));
  }
  return { onRolesChange };
}

/** The single argument a one-shot `onRolesChange` was called with, reparsed. */
function editedDoc(onRolesChange: ReturnType<typeof vi.fn>): RolesDesign {
  expect(onRolesChange).toHaveBeenCalledTimes(1);
  return JSON.parse(String(onRolesChange.mock.calls[0]![0])) as RolesDesign;
}

describe("SecurityPanel — the two halves of the security design", () => {
  // The mechanism is the decision and the roster follows from it, so the prose
  // half leads and is what a reader lands on.
  it("opens on Security architecture, with Roles & users beside it", () => {
    setup({}, { stayOnDefaultTab: true });

    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((t) => t.textContent)).toEqual([
      "Security architecture",
      "Roles & users",
    ]);
    expect(tabs[0]).toHaveAttribute("aria-selected", "true");
    expect(screen.getByTestId("prose")).toBeInTheDocument();
  });

  it("hands the prose pane over to the caller's editor on the first tab", () => {
    setup({}, { stayOnDefaultTab: true });

    expect(screen.getByTestId("prose")).toBeInTheDocument();
    // The table is not merely hidden behind it — the pane belongs to the editor.
    expect(screen.queryByText("Admin")).not.toBeInTheDocument();
  });
});

describe("SecurityPanel — reading the document", () => {
  // No sign-in is a legitimate design, so an absent document is explained, not
  // reported as a failure.
  it("says a design with no roles document simply declares none", () => {
    setup({ rolesJson: null });

    expect(screen.getByText(/declares no roles yet/i)).toBeInTheDocument();
    expect(screen.queryByRole("alert")?.className).toContain(
      "MuiAlert-colorInfo",
    );
  });

  it("shows a malformed document as an error instead of crashing", () => {
    setup({ rolesJson: '{"version": 1,' });

    expect(
      screen.getByText(/Couldn't read the roles document/i),
    ).toBeInTheDocument();
  });

  it("renders each role with its description and permissions", () => {
    setup({ rolesJson: design({ roles: [role("Admin"), role("Viewer")] }) });

    expect(screen.getByText("Admin")).toBeInTheDocument();
    expect(screen.getByText("Viewer")).toBeInTheDocument();
    expect(screen.getAllByText("orders-api")).toHaveLength(2);
  });
});

describe("SecurityPanel — a role against the shared directory", () => {
  it('reads "Reused" for a role the platform already created', () => {
    setup({ live: live({ roles: [liveRole("Admin")] }) });

    expect(screen.getByText("Reused")).toBeInTheDocument();
  });

  it('reads "New at Build" for a role the directory does not have', () => {
    setup({ live: live({ roles: [liveRole("Something Else")] }) });

    expect(screen.getByText("New at Build")).toBeInTheDocument();
  });

  // The Administrators safety property, surfaced: a group the platform did not
  // create is somebody else's, and the build must not enrol test users into it.
  it('reads "Not ours" for an existing group the platform did not create, and promises to leave it alone', async () => {
    setup({
      live: live({ roles: [liveRole("Admin", { platformCreated: false })] }),
    });

    const chip = screen.getByText("Not ours");
    expect(chip).toBeInTheDocument();
    expect(screen.queryByText("Reused")).not.toBeInTheDocument();

    fireEvent.mouseOver(chip);
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip).toHaveTextContent(/left\s+alone/i);
    expect(tooltip).toHaveTextContent(/no test user is added to it/i);
  });

  it("matches the design's role to the directory's case-insensitively", () => {
    setup({ live: live({ roles: [liveRole("admin")] }) });

    expect(screen.getByText("Reused")).toBeInTheDocument();
  });

  it("says nothing about the live world while the directory is unreachable", () => {
    setup({
      live: live({
        directoryAvailable: false,
        roles: [liveRole("Admin", { platformCreated: false })],
      }),
    });

    expect(
      screen.getByText(/identity provider could not be reached/i),
    ).toBeInTheDocument();
    for (const label of ["Reused", "New at Build", "Not ours"]) {
      expect(screen.queryByText(label)).not.toBeInTheDocument();
    }
  });
});

describe("SecurityPanel — what a test user costs to name", () => {
  // The build publishes each login onto the roles gate ticket so the
  // validation agent can sign in, so the password is readable by everyone with
  // repository access. The panel is the only place the user learns that before
  // they type a colleague's account name in.
  it("warns that the platform publishes each login where the repository can read it", () => {
    setup();

    const warning = screen.getByText(
      /Disposable accounts for agents, not for real people/i,
    );
    expect(warning).toBeInTheDocument();

    const body = warning.parentElement!;
    expect(body).toHaveTextContent(/the platform generates the password/i);
    expect(body).toHaveTextContent(
      /publishes the username and password in that build's roles gate ticket/i,
    );
    expect(body).toHaveTextContent(
      /anyone who can read this project's repository can read them/i,
    );
    expect(body).toHaveTextContent(/Never name a real person's account here/i);
    expect(body).toHaveTextContent(
      /no working login rather than a password reset/i,
    );
  });

  // One standing fact about every test user: repeated per card it would read as
  // several separate problems.
  it("states it once however many roles and users the design carries", () => {
    setup({
      rolesJson: design({
        roles: [role("Admin"), role("Viewer"), role("Auditor")],
        testUsers: [
          { username: "ada", role: "Admin" },
          { username: "grace", role: "Admin" },
          { username: "linus", role: "Viewer" },
        ],
      }),
    });

    expect(screen.getAllByText("orders-api")).toHaveLength(3);
    expect(
      screen.getAllByText(
        /Disposable accounts for agents, not for real people/i,
      ),
    ).toHaveLength(1);
  });

  // It belongs to the roster, so the tab a reader LANDS on carries none of it.
  // It belongs to the roster, so the tab a reader LANDS on carries none of it.
  it("does not carry the warning on the Security architecture tab", () => {
    setup({}, { stayOnDefaultTab: true });

    expect(
      screen.queryByText(
        /Disposable accounts for agents, not for real people/i,
      ),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: "Roles & users" }));
    expect(
      screen.getByText(/Disposable accounts for agents, not for real people/i),
    ).toBeInTheDocument();
  });

  // A document declaring no roles has no test users to warn about, and the
  // absent-roles explanation is the only thing that should be on screen.
  it("says nothing about test users when the design declares no roles", () => {
    setup({ rolesJson: null });

    expect(
      screen.queryByText(
        /Disposable accounts for agents, not for real people/i,
      ),
    ).not.toBeInTheDocument();
  });
});

describe("SecurityPanel — test users", () => {
  it("shows the name the build will supply for a role the design gave none", () => {
    setup({ rolesJson: design({ roles: [role("Compliance Admin")] }) });

    expect(screen.getByText("test-compliance-admin")).toBeInTheDocument();
    expect(screen.getByText("Platform-supplied")).toBeInTheDocument();
  });

  it("does not badge an authored user as platform-supplied", () => {
    setup({
      rolesJson: design({ testUsers: [{ username: "ada", role: "Admin" }] }),
    });

    expect(screen.getByText("ada")).toBeInTheDocument();
    expect(screen.queryByText("Platform-supplied")).not.toBeInTheDocument();
  });

  // The real-person-account guard: the username belongs to somebody else, so
  // the platform sets no password and adds it to no role.
  it("warns that a name is already taken when the account exists but is not the platform's", async () => {
    setup({
      rolesJson: design({ testUsers: [{ username: "ada", role: "Admin" }] }),
      live: live({ testUsers: [liveUser("ada", { owned: false })] }),
    });

    const chip = screen.getByText("Name already taken");
    fireEvent.mouseOver(chip);
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip).toHaveTextContent(/left untouched/i);
    expect(tooltip).toHaveTextContent(/no password is set/i);
  });

  it('marks an account that does not exist yet as "Created at Build"', () => {
    setup({
      rolesJson: design({ testUsers: [{ username: "ada", role: "Admin" }] }),
      live: live({ testUsers: [liveUser("ada", { exists: false })] }),
    });

    expect(screen.getByText("Created at Build")).toBeInTheDocument();
  });
});

describe("SecurityPanel — editing the design", () => {
  it("adds a test user through the inline field", () => {
    const { onRolesChange } = setup({
      rolesJson: design({ roles: [role("Viewer")] }),
    });

    fireEvent.click(screen.getByRole("button", { name: /add a test user/i }));
    // Seeded with the name the build would have supplied.
    const field = screen.getByLabelText("Test user name");
    expect(field).toHaveValue("test-viewer");

    fireEvent.change(field, { target: { value: "qa-viewer" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(editedDoc(onRolesChange).testUsers).toEqual([
      { username: "qa-viewer", role: "Viewer" },
    ]);
  });

  it("renames an authored user", () => {
    const { onRolesChange } = setup({
      rolesJson: design({ testUsers: [{ username: "ada", role: "Admin" }] }),
    });

    fireEvent.click(screen.getByRole("button", { name: "Rename ada" }));
    fireEvent.change(screen.getByLabelText("Test user name"), {
      target: { value: "grace" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(editedDoc(onRolesChange).testUsers).toEqual([
      { username: "grace", role: "Admin" },
    ]);
  });

  // Typing over the promised name is the user choosing one, so it lands in the
  // document as an authored user rather than doing nothing.
  it("turns a rename of the platform-supplied name into an authored user", () => {
    const { onRolesChange } = setup({
      rolesJson: design({ roles: [role("Viewer")] }),
    });

    fireEvent.click(screen.getByRole("button", { name: "Rename test-viewer" }));
    fireEvent.change(screen.getByLabelText("Test user name"), {
      target: { value: "qa-viewer" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(editedDoc(onRolesChange).testUsers).toEqual([
      { username: "qa-viewer", role: "Viewer" },
    ]);
  });

  it("removes an authored user from the design", () => {
    const { onRolesChange } = setup({
      rolesJson: design({
        testUsers: [
          { username: "ada", role: "Admin" },
          { username: "grace", role: "Admin" },
        ],
      }),
    });

    fireEvent.click(screen.getByRole("button", { name: "Remove ada" }));

    expect(editedDoc(onRolesChange).testUsers).toEqual([
      { username: "grace", role: "Admin" },
    ]);
  });

  // A supplied name is not in the document, so there is nothing to remove — the
  // only way to be rid of it is to author a different one.
  it("offers no Remove for a platform-supplied name", () => {
    setup({ rolesJson: design({ roles: [role("Viewer")] }) });

    expect(
      screen.queryByRole("button", { name: "Remove test-viewer" }),
    ).not.toBeInTheDocument();
  });

  it("renders no edit controls at all when nothing can be written", () => {
    setup({
      rolesJson: design({ testUsers: [{ username: "ada", role: "Admin" }] }),
      onRolesChange: undefined,
    });

    expect(screen.getByText("ada")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /add a test user/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Rename ada" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Remove ada" }),
    ).not.toBeInTheDocument();
  });
});

describe("SecurityPanel — account actions on an owned account", () => {
  const ROLES = design({ testUsers: [{ username: "ada", role: "Admin" }] });

  function withActions(
    over: Partial<AccountActions> = {},
    userOver: Partial<ProjectTestUserState> = {},
  ) {
    const actions: AccountActions = {
      reveal: vi.fn().mockResolvedValue("correct-horse"),
      rotate: vi.fn().mockResolvedValue("battery-staple"),
      remove: vi.fn().mockResolvedValue(undefined),
      ...over,
    };
    setup({
      rolesJson: ROLES,
      live: live({ testUsers: [liveUser("ada", userOver)] }),
      actions,
    });
    return actions;
  }

  it("keeps the password out of the DOM until reveal is clicked", async () => {
    const actions = withActions();

    expect(screen.queryByText("correct-horse")).not.toBeInTheDocument();
    expect(actions.reveal).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", { name: "Reveal the password for ada" }),
      );
    });

    expect(actions.reveal).toHaveBeenCalledWith("ada");
    expect(screen.getByText("correct-horse")).toBeInTheDocument();
  });

  it("offers a Hide control once the password is on screen", async () => {
    withActions();

    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", { name: "Reveal the password for ada" }),
      );
    });
    fireEvent.click(screen.getByRole("button", { name: "Hide" }));

    expect(screen.queryByText("correct-horse")).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Reveal the password for ada" }),
    ).toBeInTheDocument();
  });

  it("rotates the password and shows the new one", async () => {
    const actions = withActions();

    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", { name: "Rotate the password for ada" }),
      );
    });

    expect(actions.rotate).toHaveBeenCalledWith("ada");
    expect(screen.getByText("battery-staple")).toBeInTheDocument();
  });

  it("deletes the account", async () => {
    const actions = withActions();

    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", { name: "Delete the account ada" }),
      );
    });

    expect(actions.remove).toHaveBeenCalledWith("ada");
  });

  it("surfaces a failed action beside the row rather than throwing", async () => {
    withActions({ reveal: vi.fn().mockRejectedValue(new Error("403 nope")) });

    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", { name: "Reveal the password for ada" }),
      );
    });

    expect(screen.getByText("403 nope")).toBeInTheDocument();
  });

  // Shared accounts outlive the project that named them, so the warning carries
  // the real number instead of a generic "are you sure".
  it("warns with the real number of other projects before a delete", async () => {
    withActions({}, { referencingCount: 3 });

    const button = screen.getByRole("button", {
      name: "Delete the account ada",
    });
    fireEvent.mouseOver(button.parentElement!);

    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip).toHaveTextContent("2 other projects use it");
  });

  it("says nothing about other projects when this is the only one", async () => {
    withActions({}, { referencingCount: 1 });

    const button = screen.getByRole("button", {
      name: "Delete the account ada",
    });
    fireEvent.mouseOver(button.parentElement!);

    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip).toHaveTextContent(/The role is left standing/i);
    expect(tooltip.textContent).not.toMatch(/other project/i);
  });

  it("renders no account controls when the caller supplied no actions", () => {
    setup({
      rolesJson: ROLES,
      live: live({ testUsers: [liveUser("ada")] }),
      actions: undefined,
    });

    for (const name of [
      "Reveal the password for ada",
      "Rotate the password for ada",
      "Delete the account ada",
    ]) {
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    }
  });

  it("renders no account controls for an account the platform does not own", () => {
    setup({
      rolesJson: ROLES,
      live: live({ testUsers: [liveUser("ada", { owned: false })] }),
      actions: {
        reveal: vi.fn(),
        rotate: vi.fn(),
        remove: vi.fn(),
      },
    });

    for (const name of [
      "Reveal the password for ada",
      "Rotate the password for ada",
      "Delete the account ada",
    ]) {
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    }
  });
});

describe("SecurityPanel — the document's standing rules", () => {
  it("names the role a freshly signed-in person holds", () => {
    setup({ rolesJson: design({ coldStartRole: "Admin" }) });

    expect(
      screen.getByText(/has just signed in and been granted nothing holds/i),
    ).toBeInTheDocument();
  });

  it("says a person with no role reaches nothing when there is no cold-start role", () => {
    setup({ rolesJson: design({ coldStartRole: null }) });

    expect(screen.getByText(/reaches nothing/i)).toBeInTheDocument();
  });

  it("lists the components open without sign-in", () => {
    setup({ rolesJson: design({ publicComponents: ["docs-site"] }) });

    expect(
      screen.getByText(/Open to everyone, no sign-in: docs-site\./i),
    ).toBeInTheDocument();
  });
});
