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
    expect(projectChip(status({}))).toEqual({ label: "Spec published", tone: "success" });
  });
});

describe("projectChip — delivery state outranks the spec", () => {
  it("build running → Building", () => {
    expect(projectChip(status({ build: { version: "v1", status: "running" } }))).toEqual({
      label: "Building",
      tone: "info",
    });
  });
  it("build failed → Build failed", () => {
    expect(projectChip(status({ build: { version: "v1", status: "failed" } })).tone).toBe(
      "error",
    );
  });
  it("built, nothing deployed → Built", () => {
    expect(projectChip(status({ build: { version: "v1", status: "succeeded" } }))).toEqual({
      label: "Built",
      tone: "success",
    });
  });
  it("rollout underway → Deploying", () => {
    const c = projectChip(
      status({
        build: { version: "v1", status: "succeeded" },
        deploy: { version: "v1", status: "deploying", components: { total: 3, ready: 1 } },
      }),
    );
    expect(c).toEqual({ label: "Deploying", tone: "info" });
  });
  it("deploy failed → Deploy failed", () => {
    const c = projectChip(
      status({
        build: { version: "v1", status: "succeeded" },
        deploy: { version: "v1", status: "failed", components: { total: 3, ready: 1 } },
      }),
    );
    expect(c).toEqual({ label: "Deploy failed", tone: "error" });
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
    expect(c).toEqual({ label: "Active", tone: "success" });
  });

  it("a validating run reads as Active even while the binding read is empty", () => {
    const c = projectChip(
      status({
        build: { version: "v1", status: "succeeded" },
        deploy: { version: "v1", status: "none", validation: "running" },
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
