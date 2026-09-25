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
import type { EnvironmentRow } from "./deploymentLedger";
import {
  componentLine,
  connectionsHeadline,
  deployHold,
  deployStep,
  deployedBehindBuild,
  deployedValidation,
  developmentConnections,
  holdNotice,
  holdSentence,
  productionConnections,
  productionLiveSentence,
  productionSentence,
  promoteStep,
  validationStep,
} from "./deploymentFlow";
import type { ConnectionRow } from "./promotion";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type DeployStage = components["schemas"]["DeployStage"];

function run(over: Partial<MilestoneRunView>): MilestoneRunView {
  return {
    id: "run-1",
    milestoneNumber: 1,
    milestoneTitle: "v1",
    kind: "dev",
    origin: "spec-build",
    state: "running",
    budgets: { cyclesTotal: 0, cycleCeiling: 8, fixCycles: 0, fixCeiling: 3, conflictCycles: 0, conflictCeiling: 2 },
    cycles: [],
    createdAt: "2026-09-10T08:00:00Z",
    ...over,
  } as MilestoneRunView;
}

const design: ComponentDependencies[] = [
  {
    componentName: "expense-web",
    dependencies: [{ kind: "component", name: "expense-api" }],
  },
  {
    componentName: "expense-api",
    dependencies: [
      { kind: "platform-resource", name: "expense-db", resourceType: "postgres-cnpg" },
      {
        kind: "external",
        name: "Currency-Service",
        config: [{ key: "API_KEY", secret: true }],
      },
    ],
  },
];

function row(over: Partial<EnvironmentRow> = {}): EnvironmentRow {
  return {
    environment: "development",
    label: "Development",
    version: "v1",
    cards: [
      {
        componentName: "expense-web",
        displayName: "expense-web",
        kind: "success",
        deployment: { componentName: "expense-web", environment: "development", status: "Ready", createdAt: "2026-09-10T08:15:00Z" },
      },
      {
        componentName: "expense-api",
        displayName: "expense-api",
        kind: "success",
        deployment: { componentName: "expense-api", environment: "development", status: "Ready", createdAt: "2026-09-10T08:14:00Z" },
      },
    ],
    status: { label: "Deployed", tone: "success", live: false },
    live: 2,
    total: 2,
    deployedAt: "2026-09-10T08:15:00Z",
    ...over,
  };
}

const production: EnvironmentRow = {
  environment: "production",
  label: "Production",
  cards: [],
  status: { label: "Nothing deployed", tone: "neutral", live: false },
  live: 0,
  total: 0,
};

const deployed: DeployStage = {
  version: "v1",
  status: "deployed",
  components: { total: 2, ready: 2 },
  validation: "passed",
};

const currency: ConnectionRow = {
  id: "external:Currency-Service",
  name: "Currency-Service",
  kind: "external",
  config: [{ key: "API_KEY", secret: true }],
  provisioned: false,
};
const db: ConnectionRow = {
  id: "platform-resource:postgres-cnpg:expense-db",
  name: "expense-db",
  kind: "platform-resource",
  detail: "postgres-cnpg",
  config: [],
  provisioned: true,
};

describe("deployHold", () => {
  it("is nothing while no run is parked at the deploy gate", () => {
    expect(deployHold([run({ state: "running" })], design)).toBeNull();
    expect(deployHold([run({ state: "waiting" })], design)).toBeNull();
    expect(deployHold(undefined, design)).toBeNull();
  });

  it("names the values owed in the design's spelling, and who waits on them", () => {
    const hold = deployHold(
      [run({ state: "waiting", waitingReason: "external-values", blockingDependencies: ["currency-service"] })],
      design,
    );
    expect(hold).toEqual({
      blocking: ["Currency-Service"],
      dependents: { "expense-api": ["Currency-Service"] },
    });
    expect(holdNotice(hold!)).toEqual({
      title: "Needs a value for Currency-Service before deploying",
      body: "expense-api depends on it. Deployment continues automatically once it is set.",
    });
  });

  it("reads only the NEWEST run — an older park is history", () => {
    const parked = run({ state: "waiting", waitingReason: "external-values", blockingDependencies: ["stripe"] });
    expect(deployHold([run({ state: "succeeded" }), parked], design)).toBeNull();
  });

  it("keeps a park the row could not name, rather than silencing it", () => {
    const hold = deployHold([run({ state: "waiting", waitingReason: "external-values" })], design);
    expect(hold).toEqual({ blocking: [], dependents: {} });
    expect(holdNotice(hold!).title).toBe("Needs a dependency value before deploying");
  });
});

describe("deployStep", () => {
  it("is done with its stamp when the environment is green", () => {
    const step = deployStep(row(), null);
    expect(step.state).toBe("done");
    expect(step.title).toBe("Deployed");
    expect(step.chip?.tone).toBe("neutral");
  });

  it("is on hold whatever the bindings say, when a run is parked", () => {
    const step = deployStep(row(), { blocking: ["stripe"], dependents: {} });
    expect(step).toMatchObject({ state: "hold", title: "Deploy", chip: { label: "On hold", tone: "warning" } });
  });

  it("moves while the rollout converges, breaks when a release fails, waits before any build", () => {
    expect(deployStep(row({ status: { label: "Deploying", tone: "info", live: true }, live: 1 }), null))
      .toMatchObject({ state: "active", chip: { label: "1 of 2 live", tone: "info", live: true } });
    expect(deployStep(row({ status: { label: "Deploy failed", tone: "error", live: false } }), null))
      .toMatchObject({ state: "error", title: "Deploy", chip: { label: "Deploy failed", tone: "error" } });
    expect(deployStep(row({ status: { label: "Nothing deployed", tone: "neutral", live: false }, cards: [], live: 0, total: 0 }), null))
      .toMatchObject({
        state: "pending",
        // The design's empty card: a noun for a title, a chip that says what
        // is there, and one sentence about what would fill it.
        title: "Deployment",
        chip: { label: "Nothing deployed", tone: "neutral" },
        note: "Nothing running yet. Deploys automatically when a build merges.",
      });
  });

  // An empty environment says what would land in it — off the row of the
  // environment that promotes INTO it, and never a version nobody reported.
  it("names the version waiting upstream, or what it would take to have one", () => {
    const empty = row({ status: { label: "Nothing deployed", tone: "neutral", live: false }, cards: [], live: 0, total: 0 });
    expect(deployStep(empty, null, { label: "Staging", version: "v3" }).note).toBe(
      "Nothing running yet. v3 on Staging is ready to promote here.",
    );
    expect(deployStep(empty, null, { label: "UAT", version: "" }).note).toBe(
      "Nothing running yet. Only a version that reached UAT can be promoted here.",
    );
  });
});

describe("componentLine", () => {
  const card = row().cards[0]!;
  it("says Live for a serving component, in the card's own word", () => {
    expect(componentLine(card, "web-application", null)).toMatchObject({ kind: "web app", label: "Live", tone: "success" });
  });
  it("names what a held component waits on, and Waiting for the rest", () => {
    const hold = { blocking: ["Currency-Service"], dependents: { "expense-api": ["Currency-Service"] } };
    expect(componentLine(row().cards[1]!, "service", hold)).toMatchObject({ label: "Needs Currency-Service", tone: "warning" });
    expect(componentLine(card, "web-application", hold)).toMatchObject({ label: "Waiting", tone: "neutral" });
  });
  it("keeps the shared chip for every other binding state", () => {
    expect(componentLine({ ...card, kind: "notDeployed" }, "service", null)).toMatchObject({ label: "Not deployed" });
  });
});

describe("connections on the cards", () => {
  it("reads Set and Missing off the readiness read, and Provisioned off the design", () => {
    const lines = developmentConnections(
      [currency, db],
      { configured: false, dependencies: [{ name: "currency-service", state: "unset", missingKeys: ["API_KEY"] }] },
      null,
      new Set(),
      false,
    );
    expect(lines.map((l) => [l.row.name, l.state, l.label, l.configure])).toEqual([
      ["Currency-Service", "missing", "Missing", true],
      ["expense-db", "provisioned", "Provisioned", false],
    ]);
    expect(connectionsHeadline(lines)).toBe("1 of 2 set");
  });

  it("lets a parked run's word win over readiness", () => {
    const lines = developmentConnections(
      [currency],
      { configured: true, dependencies: [{ name: "currency-service", state: "configured", missingKeys: [] }] },
      { blocking: ["Currency-Service"], dependents: {} },
      new Set(),
      false,
    );
    expect(lines[0]).toMatchObject({ state: "missing" });
  });

  it("offers no Configure on a Registered external, or while the catalog is unknown", () => {
    expect(developmentConnections([currency], undefined, null, new Set(["Currency-Service"]), false)[0]?.configure).toBe(false);
    // The catalog's spelling of the name is its own — a case-only difference
    // is the same Registered External, not one to re-author.
    expect(developmentConnections([currency], undefined, null, new Set(["currency-service"]), false)[0]?.configure).toBe(false);
    expect(developmentConnections([currency], undefined, null, new Set(), true)[0]?.configure).toBe(false);
    expect(developmentConnections([currency], undefined, null, new Set(), false)[0]).toMatchObject({ state: "unknown", label: "", configure: true });
  });

  it("reads production off the values entered so far", () => {
    expect(productionConnections([currency, db], {})[0]).toMatchObject({ state: "missing", configure: true });
    expect(productionConnections([currency, db], { [currency.id]: { API_KEY: "x" } })[0]).toMatchObject({ state: "set", configure: false });
    expect(productionConnections([currency, db], {})[1]).toMatchObject({ state: "provisioned" });
  });
});

describe("validationStep", () => {
  const done = deployStep(row(), null);
  it("waits until the deployment is live, and says why", () => {
    expect(validationStep("none", undefined, deployStep(row(), { blocking: [], dependents: {} })))
      .toMatchObject({
        state: "pending",
        chip: { label: "Not run", tone: "neutral" },
        note: "Runs once something is deployed here.",
      });
    expect(validationStep("none", undefined, done))
      .toMatchObject({
        state: "pending",
        chip: { label: "Not run", tone: "neutral" },
        note: "Starts automatically now that the deployment is live.",
      });
  });
  it("runs without the last attempt's numbers under this attempt's heading", () => {
    expect(validationStep("running", { total: 25, passed: 24, failed: 0, uncovered: 1 }, done))
      .toMatchObject({ state: "active", chip: { label: "Running", tone: "info", live: true } });
  });
  it("settles with its counts", () => {
    expect(validationStep("passed", { total: 25, passed: 25, failed: 0, uncovered: 0 }, done))
      .toMatchObject({ state: "done", chip: { label: "Passed · 25 of 25", tone: "success" } });
    expect(validationStep("partial", { total: 25, passed: 24, failed: 0, uncovered: 1 }, done).chip)
      .toMatchObject({ label: "Passed* · 24 of 25", spoken: "passed, partially, 24 of 25" });
    expect(validationStep("failed", undefined, done)).toMatchObject({ state: "error", chip: { label: "Failed" } });
    expect(validationStep("cancelled", undefined, done)).toMatchObject({ state: "settled", chip: { tone: "neutral" } });
    expect(validationStep("skipped", undefined, done)).toMatchObject({ state: "settled", chip: { label: "Skipped" } });
  });
});

describe("promoteStep", () => {
  it("is absent once production runs something, or before there is a version", () => {
    expect(promoteStep(deployed, { ...production, cards: row().cards }, [], {}, null)).toBeNull();
    expect(promoteStep({ ...deployed, version: "" }, production, [], {}, null)).toBeNull();
  });

  it("names a held version by its build, before anything is deployed", () => {
    const none: DeployStage = { version: "", status: "none", components: { total: 2, ready: 0 }, validation: "none" };
    const hold = { blocking: ["stripe"], dependents: {} };
    expect(promoteStep(none, production, [], {}, hold, "v1"))
      .toMatchObject({ state: "pending", reason: "Unavailable until v1 deploys and validates" });
    expect(productionSentence(none, null, hold, "v1"))
      .toBe("Nothing running yet. v1 must deploy and validate in Development first.");
  });

  it("waits on the deployment, then on validation, each with its reason", () => {
    expect(promoteStep(deployed, production, [], {}, { blocking: [], dependents: {} }))
      .toMatchObject({ state: "pending", enabled: false, reason: "Unavailable until v1 deploys and validates" });
    expect(promoteStep({ ...deployed, validation: "running" }, production, [], {}, null))
      .toMatchObject({ state: "pending", enabled: false, reason: "Enabled when validation passes" });
    expect(promoteStep({ ...deployed, validation: "failed" }, production, [], {}, null))
      .toMatchObject({ enabled: false, reason: "Blocked — validation failed" });
  });

  it("is the active step with one blocker per missing value, then enables", () => {
    const blocked = promoteStep(deployed, production, [currency, db], {}, null);
    expect(blocked).toMatchObject({
      state: "active",
      enabled: false,
      chip: { label: "1 value missing", tone: "warning" },
      reason: "Enabled once the value is set",
    });
    expect(blocked?.missing.map((r) => r.name)).toEqual(["Currency-Service"]);
    expect(promoteStep(deployed, production, [currency, db], { [currency.id]: { API_KEY: "k" } }, null))
      .toMatchObject({ state: "active", enabled: true, missing: [] });
  });

  it("gives the Production card its sentence for each of those", () => {
    expect(productionSentence(deployed, promoteStep(deployed, production, [currency], {}, null), null))
      .toBe("Nothing running yet. v1 is ready to promote once the missing value is set.");
    expect(productionSentence({ ...deployed, validation: "running" }, promoteStep({ ...deployed, validation: "running" }, production, [], {}, null), null))
      .toBe("Nothing running yet. v1 can be promoted here once it passes validation.");
    expect(productionSentence(deployed, null, { blocking: [], dependents: {} }))
      .toBe("Nothing running yet. v1 must deploy and validate in Development first.");
    expect(productionSentence(undefined, null, null)).toMatch(/promote a validated version/);
  });
});

describe("holdSentence", () => {
  it("counts the values owed, and says a park that named none without a count", () => {
    expect(holdSentence({ blocking: ["stripe"], dependents: {} }))
      .toBe("Deployment is on hold until one dependency value is set. It continues automatically.");
    expect(holdSentence({ blocking: ["stripe", "sendgrid"], dependents: {} }))
      .toBe("Deployment is on hold until 2 dependency values are set. It continues automatically.");
    // An older row, or a lost write: parked, and nothing named — not "0 values".
    expect(holdSentence({ blocking: [], dependents: {} }))
      .toBe("Deployment is on hold until its dependency values are set. It continues automatically.");
  });
});

describe("productionLiveSentence", () => {
  it("says Running only when production's own status does", () => {
    const serving = row({ environment: "production", label: "Production", live: 2, total: 2 });
    expect(productionLiveSentence(serving)).toBe("Running · 2 of 2 components live");
    expect(
      productionLiveSentence({
        ...serving,
        live: 0,
        status: { label: "Deploy failed", tone: "error", live: false },
      }),
    ).toBe("Deploy failed · 0 of 2 components live");
    expect(
      productionLiveSentence({
        ...serving,
        live: 1,
        status: { label: "Deploying", tone: "info", live: true },
      }),
    ).toBe("Deploying · 1 of 2 components live");
  });
});

describe("deployedValidation", () => {
  const judged = run({
    id: "run-v1-1",
    state: "succeeded",
    validation: { verdict: "partial" },
  } as Partial<MilestoneRunView>);

  it("is the aggregate's word while the deployed version is the build's", () => {
    expect(deployedBehindBuild(deployed, "v1")).toBe(false);
    expect(deployedValidation(deployed, "v1", [judged])).toBe("passed");
    // Nothing deployed yet: the build's own version, and its own word.
    const none: DeployStage = { version: "", status: "none", components: { total: 1, ready: 0 }, validation: "none" };
    expect(deployedBehindBuild(none, "v2")).toBe(false);
    expect(deployedValidation(none, "v2", [])).toBe("none");
  });

  it("answers for an older deployed version off its own run story", () => {
    // v1 serves while v2 builds: the aggregate's `none` is v2's, not v1's.
    const behind: DeployStage = { ...deployed, validation: "none" };
    expect(deployedBehindBuild(behind, "v2")).toBe(true);
    expect(deployedValidation(behind, "v2", [judged])).toBe("partial");
    // A newer non-validating run on v1 (an adopted incident) does not hide the answer.
    expect(deployedValidation(behind, "v2", [run({ id: "run-v1-2", kind: "task", state: "succeeded" }), judged])).toBe("partial");
    // …and a version nothing ever judged reads `none`, as the aggregate would.
    expect(deployedValidation(behind, "v2", [run({ state: "succeeded" })])).toBe("none");
    expect(deployedValidation(behind, "v2", undefined)).toBe("none");
  });
});
