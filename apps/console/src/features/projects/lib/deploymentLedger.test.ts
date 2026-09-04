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
import type { DeploymentCard } from "./deploymentRows";
import {
  agoLabel,
  commitUrl,
  environmentRows,
  environmentStatus,
  latestDeployedAt,
  ledgerRows,
  milestoneFor,
  parseEnvironment,
  shortSha,
  validationCell,
} from "./deploymentLedger";

type DeployStage = components["schemas"]["DeployStage"];

const card = (
  over: Partial<DeploymentCard> & { kind: DeploymentCard["kind"] },
): DeploymentCard => ({
  componentName: "api",
  displayName: "API",
  ...(over.kind !== "notDeployed" && {
    deployment: {
      componentName: "api",
      environment: "development",
      status: "Ready",
      createdAt: "2026-08-14T16:54:00Z",
    },
  }),
  ...over,
});

const deploy = (over: Partial<DeployStage> = {}): DeployStage => ({
  version: "v1",
  status: "deployed",
  components: { total: 1, ready: 1 },
  validation: "passed",
  ...over,
});

describe("parseEnvironment", () => {
  it("accepts the two platform environments and nothing else", () => {
    expect(parseEnvironment("development")).toBe("development");
    expect(parseEnvironment("production")).toBe("production");
    expect(parseEnvironment("staging")).toBeNull();
    expect(parseEnvironment("")).toBeNull();
  });
});

describe("environmentStatus", () => {
  it("lets the deploy aggregate speak for development", () => {
    // Bindings still converging, but the platform says deployed — the
    // aggregate is the word the Builds ledger reads too, so it wins here.
    const cards = [card({ kind: "transitional" })];
    expect(environmentStatus("development", cards, deploy())).toEqual({
      label: "Deployed",
      tone: "success",
      live: false,
    });
    expect(
      environmentStatus("development", cards, deploy({ status: "deploying" })).live,
    ).toBe(true);
    expect(
      environmentStatus("development", cards, deploy({ status: "failed" })).label,
    ).toBe("Deploy failed");
    expect(
      environmentStatus("development", [], deploy({ status: "none", version: "" }))
        .label,
    ).toBe("Nothing deployed");
  });

  it("folds production from its bindings, loudest first", () => {
    expect(environmentStatus("production", []).label).toBe("Nothing deployed");
    expect(
      environmentStatus("production", [
        card({ kind: "success" }),
        card({ kind: "error" }),
        card({ kind: "transitional" }),
      ]),
    ).toEqual({ label: "Deploy failed", tone: "error", live: false });
    expect(
      environmentStatus("production", [
        card({ kind: "success" }),
        card({ kind: "transitional" }),
      ]),
    ).toEqual({ label: "Deploying", tone: "info", live: true });
    expect(environmentStatus("production", [card({ kind: "success" })]).label).toBe(
      "Deployed",
    );
    expect(
      environmentStatus("production", [card({ kind: "undeployed" })]).label,
    ).toBe("Undeployed");
  });

  it("falls back to the bindings for development while the poll is out", () => {
    expect(environmentStatus("development", [card({ kind: "success" })]).label).toBe(
      "Deployed",
    );
  });
});

describe("environmentRows / ledgerRows", () => {
  it("always seats development, and production only once bound", () => {
    const rows = environmentRows(
      { development: [card({ kind: "notDeployed" })], production: [] },
      deploy({ status: "none", version: "" }),
    );
    expect(rows.map((r) => r.environment)).toEqual(["development"]);
    // Nothing is bound anywhere, so the ledger has no row to show.
    expect(ledgerRows(rows)).toEqual([]);
  });

  it("carries the dev version, the live count and the newest stamp", () => {
    const rows = environmentRows(
      {
        development: [
          card({ kind: "success" }),
          card({
            kind: "transitional",
            componentName: "web",
            deployment: {
              componentName: "web",
              environment: "development",
              status: "Progressing",
              createdAt: "2026-08-14T17:10:00Z",
            },
          }),
          card({ kind: "notDeployed", componentName: "worker" }),
        ],
        production: [
          card({
            kind: "success",
            deployment: {
              componentName: "api",
              environment: "production",
              status: "Ready",
              createdAt: "2026-08-15T09:00:00Z",
            },
          }),
        ],
      },
      deploy({ status: "deploying" }),
    );
    expect(rows).toHaveLength(2);
    const [dev, prod] = rows;
    expect(dev?.version).toBe("v1");
    expect(dev?.live).toBe(1);
    expect(dev?.total).toBe(3);
    expect(dev?.deployedAt).toBe("2026-08-14T17:10:00Z");
    expect(dev?.status.live).toBe(true);
    // The aggregate names development's version only — production's is a
    // guess the console does not make.
    expect(prod?.version).toBeUndefined();
    expect(prod?.status.label).toBe("Deployed");
    expect(ledgerRows(rows).map((r) => r.environment)).toEqual([
      "development",
      "production",
    ]);
  });
});

describe("latestDeployedAt / agoLabel", () => {
  it("picks the newest binding and ignores cards without one", () => {
    expect(
      latestDeployedAt([
        card({ kind: "notDeployed" }),
        card({ kind: "success" }),
      ]),
    ).toBe("2026-08-14T16:54:00Z");
    expect(latestDeployedAt([card({ kind: "notDeployed" })])).toBeUndefined();
  });

  it("says the age coarsely", () => {
    const now = Date.parse("2026-08-14T18:54:00Z");
    expect(agoLabel("2026-08-14T18:53:40Z", now)).toBe("just now");
    expect(agoLabel("2026-08-14T18:30:00Z", now)).toBe("24m ago");
    expect(agoLabel("2026-08-14T16:54:00Z", now)).toBe("2h ago");
    expect(agoLabel("2026-08-11T16:54:00Z", now)).toBe("3d ago");
    expect(agoLabel("not a date", now)).toBe("");
  });
});

describe("milestoneFor", () => {
  it("reads the version's milestone off the ledger the layout already holds", () => {
    const builds = [
      { tag: "v2", milestoneNumber: 5, status: "completed" as const, startedAt: "" },
      { tag: "v1", milestoneNumber: 3, status: "completed" as const, startedAt: "" },
    ];
    expect(milestoneFor("v1", builds)).toBe("Milestone #3");
    expect(milestoneFor("v9", builds)).toBeUndefined();
    expect(milestoneFor(undefined, builds)).toBeUndefined();
    expect(milestoneFor("v1", undefined)).toBeUndefined();
  });
});

describe("validationCell", () => {
  it("is development's alone", () => {
    expect(validationCell("production", "passed")).toBeNull();
  });

  it("prefers the counts once the join resolved them", () => {
    expect(
      validationCell("development", "passed", {
        passed: 24,
        failed: 0,
        uncovered: 0,
        total: 24,
      }),
    ).toEqual({ label: "24 / 24 passed", tone: "success", live: false });
  });

  it("keeps the hedge audible on a partial verdict", () => {
    const cell = validationCell("development", "partial", {
      passed: 22,
      failed: 0,
      uncovered: 2,
      total: 24,
    });
    expect(cell?.label).toBe("22 / 24 passed");
    expect(cell?.spoken).toBe("validated, partially, 22 of 24 passed");
  });

  it("names the lifecycle while a cycle is in flight, counts or not", () => {
    expect(
      validationCell("development", "running", {
        passed: 18,
        failed: 0,
        uncovered: 6,
        total: 24,
      }),
    ).toEqual({ label: "validating", tone: "info", live: true });
    expect(validationCell("development", "awaiting-fix")).toEqual({
      label: "awaiting fix",
      tone: "warning",
      live: false,
    });
  });

  it("reads Not run before anything has been asked", () => {
    expect(validationCell("development", "none")).toEqual({
      label: "Not run",
      tone: "neutral",
      live: false,
    });
    expect(validationCell("development", undefined)?.label).toBe("Not run");
  });
});

describe("shortSha / commitUrl", () => {
  it("prints the short SHA and links it on the repo's web root", () => {
    expect(shortSha("4e8a0d6f1c2b3a4d5e6f")).toBe("4e8a0d6");
    expect(commitUrl("https://github.com/acme/demo.git", "4e8a0d6f")).toBe(
      "https://github.com/acme/demo/commit/4e8a0d6f",
    );
    expect(commitUrl("https://github.com/acme/demo/", "4e8a0d6f")).toBe(
      "https://github.com/acme/demo/commit/4e8a0d6f",
    );
    expect(commitUrl(undefined, "4e8a0d6f")).toBeUndefined();
    expect(commitUrl("https://github.com/acme/demo", "")).toBeUndefined();
  });
});
