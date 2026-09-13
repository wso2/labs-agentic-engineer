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

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";
import { SkillsSection } from "./SkillsSection";

// Router replaced the same way ResourcesSection.test.tsx does, since the
// not-connected state renders a Link.
vi.mock("@tanstack/react-router", () => ({
  Link: (props: Record<string, unknown>) => <a {...props} />,
}));

// ae:skill-view and ae:skill-config are independent grants (a real user can
// hold either, both, or neither) — the mock must distinguish them by the
// permission key requested, not return one shared flag for every call.
// Every existing test in this file assumes both are held; only the dedicated
// permission tests below flip one or the other off.
const skillPermissions = vi.hoisted(() => ({ view: true, config: true }));
vi.mock("../../../auth/permissions", () => ({
  useHasPermission: (permission: string) =>
    permission === "ae:skill-config" ? skillPermissions.config : skillPermissions.view,
}));

type SkillSummary = components["schemas"]["SkillSummary"];
type GitProviderProjection = components["schemas"]["GitProviderProjection"];

const githubConnected: GitProviderProjection = {
  kind: "github",
  mode: "pat",
  status: "connected",
  githubLogin: "acme-dev",
  identityLogin: "acme-dev",
  identityName: "Acme Dev",
  identityEmail: "dev@acme.example",
  connectedAt: "2026-06-01T12:00:00Z",
  lastValidatedAt: "2026-07-01T09:00:00Z",
  selectedRepos: [],
};

function skill(overrides: Partial<SkillSummary>): SkillSummary {
  return {
    name: "go",
    kind: "org",
    description: "How to build a Go service.",
    contentSha: "sha-go-1",
    editable: true,
    deletable: true,
    enabled: true,
    required: false,
    ...overrides,
  };
}

// setSkillEnabled is a vi.fn so the toggle-click assertions below can inspect
// exactly what was mutated; every other hook SkillsSection (and the dialogs
// it always mounts — EditSkillDialog, ImportSkillDialog, SkillViewerDialog)
// touches is stubbed to an inert default, mirroring ResourcesSection.test.tsx's
// wholesale "../api/queries" replacement.
const setSkillEnabled = {
  mutate: vi.fn(),
  isPending: false,
  isError: false,
  reset: vi.fn(),
} as unknown as ReturnType<typeof import("../api/queries").useSetSkillEnabled>;

let skillsData: { skills: SkillSummary[]; repoUrl?: string } = {
  skills: [],
  repoUrl: "https://github.com/acme-dev/org-skills",
};

vi.mock("../api/queries", () => ({
  useConfig: () => ({
    data: { gitProvider: githubConnected, llm: null, idp: undefined },
    isLoading: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
  useSkills: () => ({
    data: skillsData,
    isLoading: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
  useSkillUpdates: () => ({ data: [] }),
  useDeleteSkill: () => ({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
    reset: vi.fn(),
  }),
  useSyncSkills: () => ({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
    isSuccess: false,
    error: null,
    data: undefined,
    reset: vi.fn(),
  }),
  useSetSkillEnabled: () => setSkillEnabled,
  // Looks up the row by name so the viewer dialog (opened via "View") sees
  // that skill's own editable/deletable flags, matching the real hook.
  useSkill: (name: string) => ({
    data: skillsData.skills.find((s) => s.name === name),
    isLoading: false,
    isError: false,
    error: null,
  }),
  useUpdateSkill: () => ({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
    reset: vi.fn(),
  }),
  useCreateSkill: () => ({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
    reset: vi.fn(),
  }),
  useImportSkill: () => ({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
    reset: vi.fn(),
  }),
}));

function resetMocks() {
  setSkillEnabled.mutate = vi.fn();
  setSkillEnabled.isPending = false;
  setSkillEnabled.isError = false;
  skillsData = {
    skills: [],
    repoUrl: "https://github.com/acme-dev/org-skills",
  };
  skillPermissions.view = true;
  skillPermissions.config = true;
}

describe("SkillsSection — availability toggle", () => {
  it("renders a switch per row reflecting enabled", () => {
    resetMocks();
    skillsData = {
      skills: [
        skill({ name: "go", enabled: true }),
        skill({ name: "python-service", enabled: false }),
      ],
    };

    render(<SkillsSection />);

    const goSwitch = screen.getByRole("switch", { name: "Disable go" });
    const pythonSwitch = screen.getByRole("switch", {
      name: "Enable python-service",
    });
    expect(goSwitch).toBeChecked();
    expect(pythonSwitch).not.toBeChecked();
  });

  it("fires the PATCH mutation with the inverted enabled value on click", () => {
    resetMocks();
    skillsData = { skills: [skill({ name: "go", enabled: true })] };

    render(<SkillsSection />);

    fireEvent.click(screen.getByRole("switch", { name: "Disable go" }));

    expect(setSkillEnabled.mutate).toHaveBeenCalledWith({
      name: "go",
      enabled: false,
    });
  });

  // The coding runner reads `aep` out of the project mirror on every run and
  // refuses to start without it, and the mirror only copies enabled skills — so
  // the server rejects this PATCH with 409. A toggle that can only fail is worse
  // than one that renders as unavailable and says why, so `required` (the
  // server's own flag, never a name match here) takes the control out of play.
  it("takes the toggle out of play for a required skill, per row", () => {
    resetMocks();
    skillsData = {
      skills: [
        skill({ name: "aep", kind: "platform", enabled: true, required: true }),
        skill({ name: "go", enabled: true }),
      ],
    };

    render(<SkillsSection />);

    // `disabled` IS the mechanism — a real user cannot reach the change handler
    // through it. Asserted rather than clicked: jsdom's fireEvent dispatches
    // synthetically and so bypasses the disabled check, which would make a
    // "mutation not fired" assertion a statement about the harness. This package
    // has no @testing-library/user-event to respect it properly.
    const aepSwitch = screen.getByRole("switch", { name: "Disable aep" });
    expect(aepSwitch).toBeDisabled();
    // Still shown as ON: it IS enabled, and rendering it off would misreport
    // what every build actually loads.
    expect(aepSwitch).toBeChecked();

    // Per row, not a mode the whole section drops into — an ordinary skill
    // beside it stays operable, mutation and all.
    const goSwitch = screen.getByRole("switch", { name: "Disable go" });
    expect(goSwitch).not.toBeDisabled();
    fireEvent.click(goSwitch);
    expect(setSkillEnabled.mutate).toHaveBeenCalledWith({
      name: "go",
      enabled: false,
    });
  });

  it("renders a disabled row muted while keeping its kind chip", () => {
    resetMocks();
    skillsData = {
      skills: [
        skill({ name: "go", enabled: true, kind: "org" }),
        skill({ name: "python-service", enabled: false, kind: "org" }),
      ],
    };

    render(<SkillsSection />);

    const enabledName = screen.getByText("go");
    const disabledName = screen.getByText("python-service");
    // The muted row's name resolves to a visibly different (dimmer) color
    // than the active row's — the exact hex is theme-owned, so this only
    // asserts the two are NOT rendered identically.
    expect(getComputedStyle(disabledName).color).not.toBe(
      getComputedStyle(enabledName).color,
    );
    // The kind chip stays present and legible on both rows — availability
    // and kind are independent signals, so disabling never hides it.
    expect(screen.getAllByText("Org")).toHaveLength(2);
  });

  // Lacking ae:skill-config takes every row's toggle out of play, regardless
  // of that row's own required/enabled state — permission is checked first.
  // ae:skill-view stays held here (this is the view-only role, not "no
  // access" — the page itself must still render).
  it("disables every toggle when the user lacks ae:skill-config", () => {
    resetMocks();
    skillPermissions.config = false;
    skillsData = {
      skills: [
        skill({ name: "go", enabled: true }),
        skill({ name: "aep", kind: "platform", enabled: true, required: true }),
      ],
    };

    render(<SkillsSection />);

    expect(screen.getByRole("switch", { name: "Disable go" })).toBeDisabled();
    expect(screen.getByRole("switch", { name: "Disable aep" })).toBeDisabled();
  });

  // Beyond the toggle: Import, and Edit/Delete inside the viewer, are the
  // only other ways to change something on this page — view stays open.
  it("disables Import and the viewer's Edit/Delete when the user lacks ae:skill-config", () => {
    resetMocks();
    skillPermissions.config = false;
    skillsData = { skills: [skill({ name: "go" })] };

    render(<SkillsSection />);

    expect(screen.getByRole("button", { name: "Import" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "View" }));
    expect(screen.getByRole("button", { name: "Edit" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Delete" })).toBeDisabled();
  });
});

describe("SkillsSection — view/config permission gate", () => {
  it("renders the page for a view-only user (ae:skill-view, no ae:skill-config)", () => {
    resetMocks();
    skillPermissions.config = false;
    skillsData = { skills: [skill({ name: "go" })] };

    render(<SkillsSection />);

    expect(screen.getByText("go")).toBeInTheDocument();
    expect(
      screen.queryByText("You don't have permission to view skills."),
    ).not.toBeInTheDocument();
  });

  it("renders the page for a config-only user (ae:skill-config, no ae:skill-view — config implies view)", () => {
    resetMocks();
    skillPermissions.view = false;
    skillsData = { skills: [skill({ name: "go" })] };

    render(<SkillsSection />);

    expect(screen.getByText("go")).toBeInTheDocument();
    // Config holds every capability view does, plus mutation — nothing here
    // should be disabled just because the view grant itself is absent.
    expect(screen.getByRole("button", { name: "Import" })).not.toBeDisabled();
  });

  it("shows an insufficient-permissions message and renders no skill content with neither permission", () => {
    resetMocks();
    skillPermissions.view = false;
    skillPermissions.config = false;
    skillsData = { skills: [skill({ name: "go" })] };

    render(<SkillsSection />);

    expect(
      screen.getByText("You don't have permission to view skills."),
    ).toBeInTheDocument();
    expect(screen.queryByText("go")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Import" })).not.toBeInTheDocument();
  });
});
