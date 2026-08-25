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
  componentAction,
  componentChip,
  componentTally,
  currentDeployment,
  deploymentChip,
  isDeploymentLive,
  isPromotable,
  validationCell,
} from "./status";

type ProjectDeployment = components["schemas"]["ProjectDeployment"];
type DeploymentComponent = components["schemas"]["DeploymentComponent"];

const dep = (over: Partial<ProjectDeployment> = {}): ProjectDeployment => ({
  id: "dep-1",
  tag: "v1",
  environment: "development",
  status: "live",
  deployedAt: "2026-08-14T16:54:00Z",
  ...over,
});

const comp = (over: Partial<DeploymentComponent> = {}): DeploymentComponent => ({
  name: "claims-api",
  kind: "service",
  status: "Ready",
  ...over,
});

describe("deploymentChip", () => {
  it("maps each state to its pill", () => {
    expect(deploymentChip(dep()).label).toBe("Live");
    expect(deploymentChip(dep({ status: "failed" })).tone).toBe("error");
    expect(deploymentChip(dep({ status: "superseded" })).tone).toBe("neutral");
  });

  it("marks only the moving states live", () => {
    expect(deploymentChip(dep({ status: "validating" })).live).toBe(true);
    expect(deploymentChip(dep({ status: "deploying" })).live).toBe(true);
    expect(deploymentChip(dep()).live).toBe(false);
    expect(isDeploymentLive(dep({ status: "deploying" }))).toBe(true);
    expect(isDeploymentLive(dep({ status: "superseded" }))).toBe(false);
  });
});

describe("validationCell", () => {
  it("gives a verdict a pill", () => {
    expect(
      validationCell({ state: "passed", passed: 24, total: 24 }),
    ).toEqual({ label: "24 / 24 passed", tone: "success", chip: true });
  });

  it("shows a failed run's own tally rather than hiding it", () => {
    const cell = validationCell({ state: "failed", passed: 22, total: 24 });
    expect(cell.label).toBe("22 / 24 passed");
    expect(cell.tone).toBe("error");
  });

  it("keeps a run in flight as a sentence, not a pill", () => {
    const cell = validationCell({ state: "running", passed: 18, total: 24 });
    expect(cell.label).toBe("18 of 24 checked");
    expect(cell.chip).toBe(false);
  });

  it("does not print 0 of 0 before the criteria are known", () => {
    expect(validationCell({ state: "running" }).label).toBe("Validating…");
  });

  it("says Not run for an absent or unstarted verdict", () => {
    expect(validationCell(undefined).label).toBe("Not run");
    expect(validationCell({ state: "not_run" }).label).toBe("Not run");
  });
});

describe("isPromotable", () => {
  it("requires a live deployment whose validation passed", () => {
    expect(isPromotable(dep({ validation: { state: "passed" } }))).toBe(true);
  });

  it("refuses anything short of a pass", () => {
    expect(isPromotable(dep({ validation: { state: "running" } }))).toBe(false);
    expect(isPromotable(dep({ validation: { state: "failed" } }))).toBe(false);
    expect(isPromotable(dep({ validation: { state: "not_run" } }))).toBe(false);
    expect(isPromotable(dep())).toBe(false);
    expect(isPromotable(undefined)).toBe(false);
  });

  it("refuses a passed validation on a deployment that is no longer live", () => {
    expect(
      isPromotable(dep({ status: "superseded", validation: { state: "passed" } })),
    ).toBe(false);
  });
});

describe("componentAction", () => {
  it("offers the invitation the kind implies", () => {
    const url = "https://api.dev.example/claims";
    expect(componentAction(comp({ endpointUrl: url })).label).toBe("Try API");
    expect(componentAction(comp({ kind: "webapp", endpointUrl: url })).label).toBe("Visit");
    expect(componentAction(comp({ kind: "agent", endpointUrl: url })).label).toBe(
      "Try agent",
    );
  });

  it("offers nothing without an endpoint, whatever the kind", () => {
    // A dead button is worse than no button.
    expect(componentAction(comp()).label).toBeNull();
    expect(componentAction(comp({ kind: "webapp" })).label).toBeNull();
  });

  it("offers nothing for a worker, which has no front door", () => {
    expect(
      componentAction(comp({ kind: "worker", endpointUrl: "https://x" })).label,
    ).toBeNull();
  });
});

describe("componentChip", () => {
  it("keeps the platform's own word", () => {
    expect(componentChip(comp({ status: "Progressing" })).label).toBe("Progressing");
    expect(componentChip(comp({ status: "Ready" })).tone).toBe("success");
    expect(componentChip(comp({ status: "Failed" })).tone).toBe("error");
    expect(componentChip(comp({ status: "Progressing" })).live).toBe(true);
  });

  it("shows an unrecognised reason without asserting a tone", () => {
    const chip = componentChip(comp({ status: "SomethingNew" }));
    expect(chip.label).toBe("SomethingNew");
    expect(chip.tone).toBe("neutral");
  });

  it("falls back to Pending on an empty status", () => {
    expect(componentChip(comp({ status: "" })).label).toBe("Pending");
  });
});

describe("componentTally", () => {
  it("counts the ready ones against the total", () => {
    expect(
      componentTally([comp(), comp({ name: "web", status: "Progressing" })]),
    ).toEqual({ ready: 1, total: 2 });
  });

  it("is null when there is nothing to describe", () => {
    expect(componentTally([])).toBeNull();
    expect(componentTally(undefined)).toBeNull();
  });
});

describe("currentDeployment", () => {
  it("picks the newest in the environment", () => {
    const items = [
      dep({ id: "old", deployedAt: "2026-08-12T09:48:00Z" }),
      dep({ id: "new", deployedAt: "2026-08-14T16:54:00Z" }),
      dep({ id: "prod", environment: "production", deployedAt: "2026-08-15T10:00:00Z" }),
    ];
    expect(currentDeployment(items, "development")?.id).toBe("new");
    expect(currentDeployment(items, "production")?.id).toBe("prod");
  });

  it("does not lean on the server's ordering", () => {
    // Newest-first is the contract, but a card showing the wrong deployment
    // because the server reordered would be a silent, plausible-looking lie.
    const items = [
      dep({ id: "new", deployedAt: "2026-08-14T16:54:00Z" }),
      dep({ id: "old", deployedAt: "2026-08-12T09:48:00Z" }),
    ];
    expect(currentDeployment(items, "development")?.id).toBe("new");
  });

  it("is undefined for an environment with nothing in it", () => {
    expect(currentDeployment([dep()], "production")).toBeUndefined();
    expect(currentDeployment(undefined, "development")).toBeUndefined();
  });
});
