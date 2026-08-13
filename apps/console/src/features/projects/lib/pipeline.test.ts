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
import {
  buildStageView,
  deployStageView,
  specStageView,
  validationState,
  validationView,
} from "./pipeline";

type ProjectStatus = components["schemas"]["ProjectStatus"];

function status(over: {
  spec?: Partial<ProjectStatus["spec"]>;
  build?: Partial<ProjectStatus["build"]>;
  deploy?: Partial<ProjectStatus["deploy"]>;
}): ProjectStatus {
  return {
    phase: "spec",
    repoStatus: "ready",
    repoUrl: "",
    hasSpec: true,
    hasDesign: false,
    hasTasks: false,
    specStatus: "",
    designStatus: "",
    spec: { exists: true, version: "", dirty: false, design: false, ...over.spec },
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

describe("specStageView — derived, no stored status", () => {
  it("no spec at all → the Generate CTA", () => {
    expect(specStageView(status({ spec: { exists: false } })).cta).toBe(true);
  });
  it("exists but never published → draft", () => {
    const v = specStageView(status({}));
    expect(v.line).toContain("draft");
    expect(v.version).toBe("");
  });
  it("published and clean → vN approved", () => {
    const v = specStageView(status({ spec: { version: "v2" } }));
    expect(v.version).toBe("v2");
    expect(v.tone).toBe("success");
  });
  it("published and dirty → vN+", () => {
    const v = specStageView(status({ spec: { version: "v2", dirty: true } }));
    expect(v.version).toBe("v2+");
    expect(v.tone).toBe("warning");
  });
});

// The build stage is deliberately count-free: a per-version task tally can only
// come from GitHub and this aggregate is polled at 5s, so it carries the run's
// state and its version and nothing else.
describe("buildStageView", () => {
  it("idle → ghosted waiting, no version claimed", () => {
    const v = buildStageView(status({}));
    expect(v.tone).toBe("ghost");
    expect(v.version).toBe("");
  });
  it("running → the version being built", () => {
    const v = buildStageView(
      status({ build: { version: "v1", status: "running" } }),
    );
    expect(v.line).toBe("building");
    expect(v.tone).toBe("info");
    expect(v.version).toBe("v1");
  });
  it("failed → error tone", () => {
    const v = buildStageView(
      status({ build: { version: "v1", status: "failed" } }),
    );
    expect(v.tone).toBe("error");
    expect(v.line).toBe("build failed");
  });
  it("succeeded → success", () => {
    const v = buildStageView(
      status({ build: { version: "v1", status: "succeeded" } }),
    );
    expect(v.tone).toBe("success");
    expect(v.line).toBe("built");
  });
});

describe("deployStageView", () => {
  it("none → ghosted", () => {
    expect(deployStageView(status({})).tone).toBe("ghost");
  });
  it("deploying → component rollout progress", () => {
    const v = deployStageView(
      status({
        deploy: {
          version: "v1",
          status: "deploying",
          components: { total: 5, ready: 3 },
        },
      }),
    );
    expect(v.line).toBe("deploying · 3/5 components");
  });
  it("deployed with no validation → plain live in dev", () => {
    const v = deployStageView(
      status({
        deploy: {
          version: "v1",
          status: "deployed",
          components: { total: 5, ready: 5 },
        },
      }),
    );
    expect(v.tone).toBe("success");
    expect(v.version).toBe("v1");
    expect(v.line).toBe("live in dev");
  });
  it("deployed while validating → appends the validation state", () => {
    const v = deployStageView(
      status({
        deploy: {
          version: "v1",
          status: "deployed",
          components: { total: 5, ready: 5 },
          validation: "running",
        },
      }),
    );
    expect(v.line).toBe("live in dev · validating");
  });
  it("deployed and validation ran → appends validation complete", () => {
    const v = deployStageView(
      status({
        deploy: {
          version: "v1",
          status: "deployed",
          components: { total: 5, ready: 5 },
          validation: "passed",
        },
      }),
    );
    expect(v.line).toBe("live in dev · validated");
  });
  it("failed → error", () => {
    const v = deployStageView(
      status({
        deploy: {
          version: "v1",
          status: "failed",
          components: { total: 5, ready: 1 },
        },
      }),
    );
    expect(v.tone).toBe("error");
  });
  it("status none but validation ran → still surfaces live-in-dev + validation", () => {
    // Validation runs only post-deploy, so a non-none validation means the app
    // is live even if the deploy-status read lagged to "none".
    const v = deployStageView(
      status({
        deploy: {
          version: "v1",
          status: "none",
          components: { total: 1, ready: 0 },
          validation: "passed",
        },
      }),
    );
    expect(v.line).toBe("live in dev · validated");
    expect(v.tone).toBe("success");
  });
  it("status none and no validation → nothing deployed", () => {
    const v = deployStageView(status({}));
    expect(v.line).toBe("nothing deployed");
    expect(v.tone).toBe("ghost");
  });
});

describe("validationView", () => {
  it("none / empty / unknown → null (nothing to show)", () => {
    expect(validationView("none")).toBeNull();
    expect(validationView("")).toBeNull();
    expect(validationView("bogus")).toBeNull();
  });
  it("running → validating (info)", () => {
    expect(validationView("running")).toEqual({
      label: "validating",
      tone: "info",
    });
  });
  // The repairing state stays terse and says nothing about WHAT is being fixed — the
  // tile and the cycle feed say that in full. What it must not do is read as repairing
  // validation, when the cycle in flight is ordinary coding work on the defect
  // validation found. Warning, not error: the verdict is real but not final, and
  // sharing `failed`'s tone would read as terminal mid-repair.
  it("awaiting-fix → awaiting fix (warning, never error)", () => {
    expect(validationView("awaiting-fix")).toEqual({
      label: "awaiting fix",
      tone: "warning",
    });
    expect(validationView("awaiting-fix")?.tone).not.toBe(
      validationView("failed")?.tone,
    );
  });
  // deploy.validation MIRRORS the verdict, so every label names the outcome
  // rather than naming an artifact the reader would have to open to find it.
  it("passed → validated (success)", () => {
    expect(validationView("passed")).toEqual({
      label: "validated",
      tone: "success",
    });
  });
  it("failed → validation failed (error)", () => {
    expect(validationView("failed")).toEqual({
      label: "validation failed",
      tone: "error",
    });
  });
  // Something passed and nothing failed, but criteria were left uncovered.
  // Visually this shares `passed`'s green label (#401 review — the deployments
  // surface prints the counts beside it, which carry the hedge); the SPOKEN
  // form is what still distinguishes it, so that is what this test pins.
  it("partial → validated (success), spoken form keeps the distinction", () => {
    expect(validationView("partial")).toEqual({
      label: "validated",
      tone: "success",
      spoken: "validated, partially",
    });
  });
  it("inconclusive → validation? (warning)", () => {
    expect(validationView("inconclusive")).toEqual({
      label: "validation?",
      tone: "warning",
      spoken: "validation inconclusive",
    });
  });
  // A validation failure that fails the run — so error, not warning, and pointed at
  // validation itself rather than at anything the criteria concluded: no criterion
  // produced a result here.
  it("unreported → validation error (error)", () => {
    expect(validationView("unreported")).toEqual({
      label: "validation error",
      tone: "error",
    });
  });
  // Surfaced rather than folded into null: this run reached validation and passed
  // over it, where null means it has not got there yet.
  it("skipped → validation skipped (neutral)", () => {
    expect(validationView("skipped")).toEqual({
      label: "validation skipped",
      tone: "neutral",
    });
  });
  // `spoken` is opt-in, and that is what makes "the accessible name is the visible
  // label" the default: a state that gained one by accident would announce itself
  // differently from what it shows, for no reason a reader could see.
  it("carries a spoken form ONLY where a mark carries the meaning", () => {
    for (const v of ["running", "awaiting-fix", "passed", "failed", "unreported", "skipped"]) {
      expect(validationView(v)?.spoken, `${v} should not need a spoken form`)
        .toBeUndefined();
    }
    // The two whose labels differ from a neighbour's by punctuation alone.
    expect(validationView("partial")?.spoken).toBeTruthy();
    expect(validationView("inconclusive")?.spoken).toBeTruthy();
  });
  // Every value the contract can send must map to something — a new verdict that
  // silently rendered nothing would be invisible on every surface.
  it("maps every verdict the contract can send", () => {
    for (const v of [
      "running",
      "passed",
      "partial",
      "failed",
      "inconclusive",
      "unreported",
      "skipped",
    ]) {
      expect(validationView(v), `no mapping for ${v}`).not.toBeNull();
    }
  });
});

// The join the Validation page was missing. `RunValidation.verdict` is a COLUMN —
// six verdicts, no lifecycle — so a surface reading it alone renders `failed` as
// terminal while the platform is repairing it and about to validate again, which is
// exactly what the page did while the deployments board beside it read "awaiting
// fix" for the same run.
describe("validationState", () => {
  // The two values that exist ONLY on deploy.validation. Nothing else can supply
  // them: no run row and no cycle record ever carries either.
  it("takes the lifecycle from deploy.validation over a repeatable verdict", () => {
    expect(validationState("awaiting-fix", "failed")).toBe("awaiting-fix");
    expect(validationState("awaiting-fix", "unreported")).toBe("awaiting-fix");
    expect(validationState("running", "failed")).toBe("running");
  });

  it("takes the lifecycle when no verdict exists yet — the first attempt", () => {
    expect(validationState("running", "")).toBe("running");
  });

  // The verdict itself always comes from the run row, which the page scopes more
  // precisely than the status read does (deploy.validation answers for the newest
  // validating run on the DEPLOY version's milestone).
  it("keeps the run's verdict for every settled state", () => {
    for (const v of ["passed", "partial", "inconclusive", "failed", "unreported", "skipped"]) {
      expect(validationState(v, v), `${v} should pass straight through`).toBe(v);
    }
  });

  // The two are separate polls, so they can disagree by one interval. A lifecycle
  // value only means anything over a verdict the loop actually repeats — pairing a
  // stale `awaiting-fix` with the newer poll's green verdict would announce a repair
  // of something that passed.
  it("ignores a stale lifecycle over a verdict the loop never repeats", () => {
    expect(validationState("awaiting-fix", "passed")).toBe("passed");
    expect(validationState("running", "partial")).toBe("partial");
    expect(validationState("running", "inconclusive")).toBe("inconclusive");
    expect(validationState("awaiting-fix", "skipped")).toBe("skipped");
  });

  it("has nothing to say with neither half", () => {
    expect(validationState("none", "")).toBe("");
    expect(validationState("", "")).toBe("");
  });
});
