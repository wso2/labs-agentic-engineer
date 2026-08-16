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
  developmentStage,
  productionStage,
  validationStage,
} from "./deploymentStory";

type DeployStage = components["schemas"]["DeployStage"];

function deploy(over: Partial<DeployStage> = {}): DeployStage {
  return {
    version: "v1",
    status: "deployed",
    components: { total: 2, ready: 2 },
    validation: "passed",
    ...over,
  };
}

function card(kind: DeploymentCard["kind"], name = "api"): DeploymentCard {
  return {
    componentName: name,
    displayName: name,
    kind,
    ...(kind !== "notDeployed" && {
      deployment: { componentName: name, environment: "development" },
    }),
  };
}

describe("developmentStage", () => {
  it("is done when everything is ready, with the version as its fact", () => {
    const s = developmentStage([card("success")], deploy());
    expect(s.state).toBe("done");
    expect(s.fact).toBe("v1");
    // Counted from the CARDS, not the aggregate's tally (which says 2/2
    // here) — the note must agree with the rows rendered under it.
    expect(s.note).toBe("1 of 1 components ready");
  });

  it("fails loudly when any binding failed", () => {
    const s = developmentStage([card("success"), card("error", "web")], deploy());
    expect(s.state).toBe("failed");
  });

  it("waits when nothing has a binding yet", () => {
    const s = developmentStage([card("notDeployed")], deploy({ version: "", status: "none" }));
    expect(s.state).toBe("waiting");
    expect(s.fact).toBeUndefined();
  });
});

describe("validationStage", () => {
  it("maps the verdict's tone onto the rail vocabulary", () => {
    expect(validationStage("passed").state).toBe("done");
    expect(validationStage("failed").state).toBe("failed");
    expect(validationStage("running").state).toBe("active");
    expect(validationStage("none").state).toBe("waiting");
  });

  it("treats skipped as settled, with a note that claims no check happened", () => {
    const s = validationStage("skipped");
    expect(s.state).toBe("done");
    expect(s.fact).toBe("validation skipped");
    expect(s.note).toContain("no validation criteria");
  });

  it("carries the verdict label as the stage fact", () => {
    expect(validationStage("passed").fact).toBe("validated");
    expect(validationStage("none").fact).toBeUndefined();
  });

  it("upgrades the fact to criteria counts when the join resolved them", () => {
    expect(validationStage("passed", { passed: 12, failed: 0, uncovered: 0, total: 12 }).fact).toBe(
      "12/12 passed",
    );
    expect(validationStage("partial", { passed: 8, failed: 0, uncovered: 4, total: 12 }).fact).toBe(
      "8/12 passed",
    );
  });
});

describe("productionStage", () => {
  it("renders what production is running when there are bindings", () => {
    const s = productionStage([card("success", "api")], deploy(), 3);
    expect(s.state).toBe("done");
    expect(s.note).toBe("1 of 1 components ready");
    expect(s.actor).toBe("OpenChoreo");
  });

  it("is the reader's move when promotion is open, naming the connection count", () => {
    const s = productionStage([], deploy(), 3);
    expect(s.state).toBe("attention");
    expect(s.actor).toBe("You");
    expect(s.note).toContain("3 connections");
  });

  it("waits while validation is still failing or running", () => {
    const s = productionStage([], deploy({ validation: "running" }), 3);
    expect(s.state).toBe("waiting");
  });
});
