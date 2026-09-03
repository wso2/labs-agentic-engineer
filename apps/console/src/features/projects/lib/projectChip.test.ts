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

import { describe, expect, it } from "vitest";
import type { components } from "../../../generated/aep-api";
import { projectChip } from "./projectChip";

type ProjectStatus = components["schemas"]["ProjectStatus"];

// Every fixture carries phase "tasks" unless it is testing a repo rung: that
// is what the server emits for any project past the design, and pinning it
// here is the point — the chip must move while the phase does not.
function status(over: {
  phase?: string;
  spec?: Partial<ProjectStatus["spec"]>;
  build?: Partial<ProjectStatus["build"]>;
  deploy?: Partial<ProjectStatus["deploy"]>;
}): ProjectStatus {
  return {
    phase: over.phase ?? "tasks",
    repoStatus: "ready",
    repoUrl: "",
    hasSpec: true,
    hasDesign: true,
    hasTasks: false,
    specStatus: "",
    designStatus: "",
    spec: { exists: true, version: "v1", dirty: false, design: true, agent: "", ...over.spec },
    build: { version: "", status: "idle", ...over.build },
    deploy: {
      version: "",
      status: "none",
      components: { total: 0, ready: 0 },
      validation: "none",
      ...over.deploy,
    },
  };
}

describe("projectChip — repo lifecycle still comes from phase", () => {
  it("no repo → warning", () => {
    expect(projectChip(status({ phase: "no-repo" }))).toEqual({
      label: "No repository",
      tone: "warning",
      busy: false,
    });
  });
  it("cloning → info", () => {
    expect(projectChip(status({ phase: "repo-cloning" })).label).toBe(
      "Preparing repository",
    );
  });
  it("repo error → error", () => {
    expect(projectChip(status({ phase: "repo-error" })).tone).toBe("error");
  });
});

describe("projectChip — before the first build, the spec aggregate decides", () => {
  it("no spec yet → Starting", () => {
    const c = projectChip(status({ phase: "prompt", spec: { exists: false, version: "" } }));
    expect(c.label).toBe("Starting");
  });
  it("spec unpublished → Spec in progress", () => {
    expect(projectChip(status({ spec: { version: "" } })).label).toBe("Spec in progress");
  });
  it("published spec edited since → Spec in progress", () => {
    expect(projectChip(status({ spec: { dirty: true } })).label).toBe("Spec in progress");
  });
  it("published and clean, nothing built → Spec published", () => {
    expect(projectChip(status({}))).toEqual({ label: "Spec published", tone: "success", busy: false });
  });
});

describe("projectChip — delivery state outranks the spec", () => {
  it("build running → Building", () => {
    expect(projectChip(status({ build: { version: "v1", status: "running" } }))).toEqual({
      label: "Building",
      tone: "info",
      busy: true,
    });
  });
  it("build failed → Build failed", () => {
    expect(projectChip(status({ build: { version: "v1", status: "failed" } })).tone).toBe(
      "error",
    );
  });
  // A cancel is not a failure. The toolbar said "Build failed" over a build
  // somebody had deliberately stopped — and once the build page header learned
  // the word Cancelled, the two surfaces contradicted each other in one glance.
  it("build cancelled → Build cancelled, neutral rather than error", () => {
    expect(projectChip(status({ build: { version: "v1", status: "cancelled" } }))).toEqual({
      label: "Build cancelled",
      tone: "neutral",
      busy: false,
    });
  });
  // It keeps `failed`'s precedence over what settled behind it: a cancel the
  // reader just performed is the newest thing that happened to the project, and
  // the previous version still serving is background rather than news.
  it("a cancelled build outranks a deployed previous version", () => {
    expect(
      projectChip(
        status({
          build: { version: "v2", status: "cancelled" },
          deploy: { version: "v1", status: "deployed" },
        }),
      ).label,
    ).toBe("Build cancelled");
  });
  it("built, nothing deployed → Built", () => {
    expect(projectChip(status({ build: { version: "v1", status: "succeeded" } }))).toEqual({
      label: "Built",
      tone: "success",
      busy: false,
    });
  });
  it("rollout underway → Deploying", () => {
    const c = projectChip(
      status({
        build: { version: "v1", status: "succeeded" },
        deploy: { version: "v1", status: "deploying", components: { total: 3, ready: 1 } },
      }),
    );
    expect(c).toEqual({ label: "Deploying", tone: "info", busy: true });
  });
  it("deploy failed → Deploy failed", () => {
    const c = projectChip(
      status({
        build: { version: "v1", status: "succeeded" },
        deploy: { version: "v1", status: "failed", components: { total: 3, ready: 1 } },
      }),
    );
    expect(c).toEqual({ label: "Deploy failed", tone: "error", busy: false });
  });

  // The regression this whole file exists for: a settled build plus live
  // components used to render "Building" forever, because phase never leaves
  // "tasks".
  it("built and live in dev → Active, though phase is still tasks", () => {
    const c = projectChip(
      status({
        build: { version: "v1", status: "succeeded" },
        deploy: {
          version: "v1",
          status: "deployed",
          components: { total: 1, ready: 1 },
          validation: "passed",
        },
      }),
    );
    expect(c).toEqual({ label: "Active", tone: "success", busy: false });
  });

  // The allowance the binding read needs: a validation cycle in flight means
  // the components ARE up, whatever the slower deploy read currently says. What
  // it does NOT mean is that the project has settled — this used to read
  // "Active", so a run finished in the toolbar that the toolbar never admitted
  // had started.
  it.each(["running", "awaiting-fix"] as const)(
    "reads as Validating while validation is %s, binding read or no binding read",
    (validation) => {
      const c = projectChip(
        status({
          build: { version: "v1", status: "succeeded" },
          deploy: { version: "v1", status: "none", validation },
        }),
      );
      expect(c).toEqual({ label: "Validating", tone: "info", busy: true });
    },
  );

  // A verdict settles it. Only the phase in flight outranks Active.
  it("reads as Active once the verdict is in", () => {
    const c = projectChip(
      status({
        build: { version: "v1", status: "succeeded" },
        deploy: { version: "v1", status: "none", validation: "passed" },
      }),
    );
    expect(c.label).toBe("Active");
  });

  // build.version is the NEWEST run, so a v2 in flight over a live v1 is the
  // headline — the deploy card keeps showing v1.
  it("a new build over a live version → Building", () => {
    const c = projectChip(
      status({
        build: { version: "v2", status: "running" },
        deploy: {
          version: "v1",
          status: "deployed",
          components: { total: 1, ready: 1 },
        },
      }),
    );
    expect(c.label).toBe("Building");
  });
});

// `busy` drives the toolbar badge's pulse: it means the platform is still
// working and the label will change on its own, as opposed to a settled state
// that waits for somebody to act. Asserted as a set so a new state cannot be
// added without deciding which side of the line it falls on.
describe("projectChip — busy marks the states that move by themselves", () => {
  const cases: [string, ReturnType<typeof status>, boolean][] = [
    ["Preparing repository", status({ phase: "repo-cloning" }), true],
    ["Starting", status({ phase: "prompt", spec: { exists: false, version: "" } }), true],
    ["Spec in progress", status({ spec: { version: "" } }), true],
    ["Building", status({ build: { version: "v1", status: "running" } }), true],
    [
      "Deploying",
      status({
        build: { version: "v1", status: "succeeded" },
        deploy: { version: "v1", status: "deploying", components: { total: 3, ready: 1 } },
      }),
      true,
    ],
    ["No repository", status({ phase: "no-repo" }), false],
    ["Repository error", status({ phase: "repo-error" }), false],
    ["Spec published", status({}), false],
    ["Built", status({ build: { version: "v1", status: "succeeded" } }), false],
    ["Build failed", status({ build: { version: "v1", status: "failed" } }), false],
    [
      "Deploy failed",
      status({
        build: { version: "v1", status: "succeeded" },
        deploy: { version: "v1", status: "failed", components: { total: 3, ready: 1 } },
      }),
      false,
    ],
    [
      "Active",
      status({
        build: { version: "v1", status: "succeeded" },
        deploy: {
          version: "v1",
          status: "deployed",
          components: { total: 1, ready: 1 },
          validation: "passed",
        },
      }),
      false,
    ],
  ];

  it.each(cases)("%s → busy %s", (label, input, busy) => {
    const chip = projectChip(input);
    expect(chip.label).toBe(label);
    expect(chip.busy).toBe(busy);
  });
});
