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

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import type {
  ProjectRoleState,
  ProjectRolesLiveState,
  ProjectTestUserState,
} from "../api/roles";
import { serializeSecurityDesign, type SecurityDesign } from "../api/securityDesign";
import { SecurityPanel } from "./SecurityPanel";

afterEach(cleanup);

function role(name: string): SecurityDesign["roles"][number] {
  return {
    name,
    description: `What ${name} may do`,
    stories: [1],
    grants: ["orders:read"],
  };
}

function design(over: Partial<SecurityDesign> = {}): string {
  return serializeSecurityDesign({
    version: 2,
    permissions: [
      {
        resource: "orders",
        component: "orders-api",
        actions: [
          { handle: "read", ownership: "own", description: "See own orders" },
        ],
      },
    ],
    groups: [],
    roles: [role("Admin")],
    screens: [],
    testUsers: [],
    ...over,
  });
}

/** A complete version-1 document — the previous schema, not a half-written one. */
const V1_DOCUMENT = JSON.stringify({
  version: 1,
  coldStartRole: null,
  publicComponents: [],
  roles: [
    {
      name: "Admin",
      description: "What Admin may do",
      stories: [1],
      grantedBy: "an administrator",
      permissions: [{ component: "orders-api", actions: ["read"] }],
    },
  ],
  testUsers: [{ username: "ada", role: "Admin" }],
  thunder: { name: "orders-app", type: "browser" },
});

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

function setup(props: Partial<React.ComponentProps<typeof SecurityPanel>> = {}) {
  render(
    <SecurityPanel securityJson={design()} live={undefined} {...props} />,
  );
}

describe("SecurityPanel — one read-only page", () => {
  it("has no tabs; Roles & users is a heading", () => {
    setup();

    expect(screen.queryByRole("tab")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("tab", { name: "Security architecture" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Roles & users" }),
    ).toBeInTheDocument();
  });

  it("shows no Reveal / Rotate / Delete / Add / Hide controls", () => {
    setup({
      securityJson: design({ testUsers: [{ username: "ada", roles: ["Admin"] }] }),
      live: live({ testUsers: [liveUser("ada")] }),
    });

    for (const name of [
      /Reveal/i,
      /Rotate/i,
      /^Delete$/i,
      /Add a test user/i,
      /^Hide$/i,
    ]) {
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    }
    expect(screen.queryByText("correct-horse")).not.toBeInTheDocument();
  });
});

describe("SecurityPanel — reading the document", () => {
  it("shows a spinner while the committed document is loading, not the empty copy", () => {
    setup({ securityJson: null, isPending: true });

    expect(screen.getByLabelText("Loading security")).toBeInTheDocument();
    expect(
      screen.queryByText(/This Security document is empty or incomplete/i),
    ).not.toBeInTheDocument();
  });

  it("surfaces a committed-document read failure", () => {
    setup({ securityJson: null, isError: true });

    expect(
      screen.getByText(/Failed to load the Security document/i),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/This Security document is empty or incomplete/i),
    ).not.toBeInTheDocument();
  });

  it("explains an empty or null document with the mock info copy", () => {
    setup({ securityJson: null });

    expect(
      screen.getByText(
        /This Security document is empty or incomplete\. Ask in chat — the design agent can finish it\./,
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(
        /Disposable accounts for agents, not for real people/i,
      ),
    ).not.toBeInTheDocument();
  });

  it("explains an empty JSON object with the same info copy", () => {
    setup({ securityJson: "{}" });

    expect(
      screen.getByText(
        /This Security document is empty or incomplete\. Ask in chat — the design agent can finish it\./,
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(
        /Disposable accounts for agents, not for real people/i,
      ),
    ).not.toBeInTheDocument();
  });

  it("shows a malformed document as an error with the Security prefix", () => {
    setup({ securityJson: '{"version": 2,' });

    expect(
      screen.getByText(/Couldn't read the Security document:/i),
    ).toBeInTheDocument();
  });

  // A project whose last design turn predates v2 has a complete v1 file. The
  // panel must say so — not render it as an unfinished draft.
  it("shows a version-1 document as an error naming what v2 removed", () => {
    setup({ securityJson: V1_DOCUMENT });

    const alert = screen.getByText(/Couldn't read the Security document:/i);
    expect(alert).toHaveTextContent(/v1 is not accepted/i);
    expect(alert).toHaveTextContent(/coldStartRole/);
    expect(
      screen.queryByRole("heading", { name: "Roles & users" }),
    ).not.toBeInTheDocument();
  });

  it("renders the permission catalog with each handle and what rows it reaches", () => {
    setup({
      securityJson: design({
        permissions: [
          {
            resource: "orders",
            component: "orders-api",
            description: "Customer orders",
            actions: [
              { handle: "read", ownership: "own", description: "See own orders" },
              { handle: "read-all", ownership: "any" },
            ],
          },
        ],
      }),
    });

    // Once in the catalog, once as the default role's grant.
    expect(screen.getAllByText("orders:read")).toHaveLength(2);
    expect(screen.getByText("orders:read-all")).toBeInTheDocument();
    expect(screen.getByText("owned by orders-api")).toBeInTheDocument();
    expect(screen.getByText("Customer orders")).toBeInTheDocument();
    expect(screen.getByText("own rows")).toBeInTheDocument();
    expect(screen.getByText("any row")).toBeInTheDocument();
  });

  it("lists the org groups the project introduces, and nothing when it introduces none", () => {
    setup({
      securityJson: design({
        groups: [{ name: "Finance", description: "Approves what we pay for" }],
      }),
    });

    expect(
      screen.getByText(/Approves what we pay for/),
    ).toBeInTheDocument();

    cleanup();
    setup();
    expect(screen.queryByText("New org groups")).not.toBeInTheDocument();
  });

  it("renders each role with description, enrolment, grants, and usernames", () => {
    setup({
      securityJson: design({
        roles: [
          { ...role("Admin"), assignTo: ["Compliance"] },
          { ...role("Viewer"), enrolment: "self-service" },
        ],
        testUsers: [{ username: "ada", roles: ["Admin"] }],
      }),
    });

    expect(screen.getByText("Admin")).toBeInTheDocument();
    expect(screen.getByText("Viewer")).toBeInTheDocument();
    expect(screen.getByText("What Admin may do")).toBeInTheDocument();
    expect(
      screen.getByText("Assigned to everyone in Compliance."),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Self-service — the application assigns it when an account is created.",
      ),
    ).toBeInTheDocument();
    // The grant, and the catalog's sentence for the handle it names.
    expect(screen.getAllByText("orders:read")).toHaveLength(3);
    expect(screen.getAllByText("See own orders")).toHaveLength(3);
    expect(screen.getByText("ada")).toBeInTheDocument();
  });

  it("marks a service role and says nobody is enrolled in it", () => {
    setup({
      securityJson: design({
        roles: [{ ...role("Ledger Writer"), kind: "service" }],
      }),
    });

    expect(screen.getByText("Service")).toBeInTheDocument();
    expect(
      screen.getByText("Held by a service, not by a person."),
    ).toBeInTheDocument();
  });

  it("names the roles that may hand a role out", () => {
    setup({
      securityJson: design({
        roles: [{ ...role("Admin"), assignableBy: ["Admin"] }],
      }),
    });

    expect(screen.getByText("Handed out by Admin.")).toBeInTheDocument();
  });

  it("shows the Security heading and subtitle", () => {
    setup();

    expect(screen.getByRole("heading", { name: "Security" })).toBeInTheDocument();
    expect(
      screen.getByText(
        /What this project protects, what each role may do with it, and the accounts the validation agent signs in with\./,
      ),
    ).toBeInTheDocument();
  });
});

describe("SecurityPanel — a role's groups against the shared directory", () => {
  // The live half is the directory's GROUP catalog. A project role is not an
  // object on the directory — it reaches an app through the groups it is
  // assigned to — so every chip is about one `assignTo` group, never about the
  // role's own name.
  function assigned(...groups: string[]): string {
    return design({ roles: [{ ...role("Admin"), assignTo: groups }] });
  }

  it('reads "Reused" for an assignTo group the platform already created', () => {
    setup({
      securityJson: assigned("Administrators"),
      live: live({ roles: [liveRole("Administrators")] }),
    });

    expect(screen.getByText("Administrators: Reused")).toBeInTheDocument();
  });

  it('reads "New at Build" for an assignTo group the directory does not have', () => {
    setup({
      securityJson: assigned("Administrators"),
      live: live({ roles: [liveRole("Something Else")] }),
    });

    expect(screen.getByText("Administrators: New at Build")).toBeInTheDocument();
  });

  it('reads "Not ours" with the leave-alone tooltip', async () => {
    setup({
      securityJson: assigned("Administrators"),
      live: live({ roles: [liveRole("Administrators", { platformCreated: false })] }),
    });

    const chip = screen.getByText("Administrators: Not ours");
    expect(chip).toBeInTheDocument();
    expect(screen.queryByText("Administrators: Reused")).not.toBeInTheDocument();

    fireEvent.mouseOver(chip);
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip).toHaveTextContent(
      /This group already exists and the platform did not create it, so it will be left alone\./,
    );
    expect(tooltip.textContent).not.toMatch(/no test user is added/i);
  });

  it("matches the design's group to the directory's case-insensitively", () => {
    setup({
      securityJson: assigned("Administrators"),
      live: live({ roles: [liveRole("administrators")] }),
    });

    expect(screen.getByText("Administrators: Reused")).toBeInTheDocument();
  });

  it("gives one chip per assignTo group, each judged on its own", () => {
    setup({
      securityJson: assigned("Administrators", "Finance"),
      live: live({ roles: [liveRole("Administrators")] }),
    });

    expect(screen.getByText("Administrators: Reused")).toBeInTheDocument();
    expect(screen.getByText("Finance: New at Build")).toBeInTheDocument();
  });

  // The role's own name is NOT a directory object: a project role that happens
  // to be spelled like an org group must not read as one already there.
  it("never judges the role's own name against the group catalog", () => {
    setup({
      securityJson: assigned("Administrators"),
      live: live({ roles: [liveRole("Admin"), liveRole("Administrators")] }),
    });

    expect(screen.queryByText("Admin: Reused")).not.toBeInTheDocument();
    expect(screen.getByText("Administrators: Reused")).toBeInTheDocument();
  });

  it("shows no directory chip for a role with no assignTo", () => {
    // A service role's principal is an application and a self-service role's
    // accounts come from the app's registration flow: neither is assigned to a
    // group, so there is nothing for the directory to already hold.
    setup({
      securityJson: design({
        roles: [
          { ...role("Ledger Sync"), kind: "service" },
          { ...role("Shopper"), enrolment: "self-service" },
        ],
      }),
      live: live({ roles: [liveRole("Ledger Sync"), liveRole("Shopper")] }),
    });

    for (const label of [/Reused/, /New at Build/, /Not ours/]) {
      expect(screen.queryByText(label)).not.toBeInTheDocument();
    }
  });

  it("omits live chips when the directory is unreachable — no IDP alert", () => {
    setup({
      securityJson: assigned("Administrators"),
      live: live({
        directoryAvailable: false,
        roles: [liveRole("Administrators", { platformCreated: false })],
      }),
    });

    expect(
      screen.queryByText(/identity provider could not be reached/i),
    ).not.toBeInTheDocument();
    for (const label of [/Reused/, /New at Build/, /Not ours/]) {
      expect(screen.queryByText(label)).not.toBeInTheDocument();
    }
  });

  it("omits live chips when live is missing", () => {
    setup({ securityJson: assigned("Administrators"), live: undefined });

    for (const label of [/Reused/, /New at Build/, /Not ours/]) {
      expect(screen.queryByText(label)).not.toBeInTheDocument();
    }
  });
});

describe("SecurityPanel — disposable accounts warning", () => {
  it("uses the mock Deploy body, once however many roles", () => {
    setup({
      securityJson: design({
        roles: [role("Admin"), role("Viewer"), role("Auditor")],
        testUsers: [
          { username: "ada", roles: ["Admin"] },
          { username: "grace", roles: ["Admin"] },
          { username: "linus", roles: ["Viewer"] },
        ],
      }),
    });

    expect(
      screen.getAllByText(
        /Disposable accounts for agents, not for real people/i,
      ),
    ).toHaveLength(1);

    const warning = screen.getByText(
      /Disposable accounts for agents, not for real people/i,
    );
    const body = warning.parentElement!;
    expect(body).toHaveTextContent(
      /passwords are shown on Deploy after Build publishes them/i,
    );
    expect(body).toHaveTextContent(/never name a real person/i);
    expect(body).not.toHaveTextContent(/roles gate ticket/i);
  });

  it("says nothing about test users when the design is empty", () => {
    setup({ securityJson: null });

    expect(
      screen.queryByText(
        /Disposable accounts for agents, not for real people/i,
      ),
    ).not.toBeInTheDocument();
  });
});

describe("SecurityPanel — test users", () => {
  it("shows the name the build will supply for a role the design gave none", () => {
    setup({ securityJson: design({ roles: [role("Compliance Admin")] }) });

    expect(screen.getByText("test-compliance-admin")).toBeInTheDocument();
    expect(screen.getByText("Platform-supplied")).toBeInTheDocument();
  });

  it("does not badge an authored user as platform-supplied", () => {
    setup({
      securityJson: design({ testUsers: [{ username: "ada", roles: ["Admin"] }] }),
    });

    expect(screen.getByText("ada")).toBeInTheDocument();
    expect(screen.queryByText("Platform-supplied")).not.toBeInTheDocument();
  });

  // One account, two roles: it satisfies both cards, so neither role gets a
  // platform-supplied name.
  it("shows an account holding several roles under each of them", () => {
    setup({
      securityJson: design({
        roles: [role("Admin"), role("Viewer")],
        testUsers: [{ username: "ada", roles: ["Admin", "Viewer"] }],
      }),
    });

    expect(screen.getAllByText("ada")).toHaveLength(2);
    expect(screen.queryByText("Platform-supplied")).not.toBeInTheDocument();
  });

  it("does not show Name already taken or Created at Build chips", () => {
    setup({
      securityJson: design({ testUsers: [{ username: "ada", roles: ["Admin"] }] }),
      live: live({
        testUsers: [
          liveUser("ada", { owned: false }),
          liveUser("grace", { exists: false, username: "grace" }),
        ],
      }),
    });

    expect(screen.queryByText("Name already taken")).not.toBeInTheDocument();
    expect(screen.queryByText("Created at Build")).not.toBeInTheDocument();
  });
});

describe("SecurityPanel — screens", () => {
  it("shows what each screen takes to reach, including public and signed-in", () => {
    setup({
      securityJson: design({
        screens: [
          { component: "storefront", screen: "Orders", requires: "orders:read" },
          { component: "storefront", screen: "Catalog", requires: "public" },
          { component: "storefront", screen: "My account", requires: null },
        ],
      }),
    });

    expect(screen.getByText("Orders")).toBeInTheDocument();
    expect(screen.getAllByText("orders:read")).toHaveLength(3);
    expect(
      screen.getByText("Open to everyone, no sign-in"),
    ).toBeInTheDocument();
    expect(screen.getByText("Any signed-in person")).toBeInTheDocument();
  });

  it("omits the screens block for an API-only project", () => {
    setup();

    expect(screen.queryByText("Screens")).not.toBeInTheDocument();
  });
});
