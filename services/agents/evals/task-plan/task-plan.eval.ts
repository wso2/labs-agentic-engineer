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

/**
 * The plan-turn eval suite: its seeds (a small real design), its skill dir, and
 * two fixtures — `fresh` (empty `tasks/`, plan every component) and `incremental`
 * (existing Tasks + one new component, plan only the delta). Fixtures are TS (not
 * JSON like the file suite) because the seeds are large and the incremental seed
 * extends the fresh one — one definition, no copy-pasted design blob.
 *
 * The design's dependency chain is a DAG: order-webapp → order-service →
 * user-service, so a correct plan carries a real (acyclic) dependsOn.
 */

import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type { TaskPlanFixture, TaskPlanSuite } from "./harness.js";

const here = dirname(fileURLToPath(import.meta.url));

const PLAN_PROMPT = "Plan the implementation Tasks for this project — one Task per component, with dependencies and a body for each.";

function designJson(name: string, type: string, connections: { to: string; type: string; onPlatform?: boolean }[]): string {
  return `${JSON.stringify(
    {
      name,
      type,
      version: "0.1.0",
      language: type === "web-application" ? "TypeScript" : "Go",
      buildpack: "docker",
      appPath: name,
      entrypoint: type === "web-application" ? "deployment/web-application" : "deployment/service",
      exposure: type === "web-application" ? "internet" : "intranet",
      connections,
      description: `The ${name} ${type}.`,
    },
    null,
    2,
  )}\n`;
}

const DESIGN_MD = `---
skillsApplied:
  - high-level-architecture
---

# Overview

A small order-management system: shoppers browse a catalog and place orders.

## Components

- user-service (service) — accounts and authentication.
- order-service (service) — places and tracks orders.
- order-webapp (web-application) — the shopper-facing UI.

## Interactions

- order-service → user-service (validate the ordering user).
- order-webapp → order-service (place and view orders).
`;

/** The FRESH design: three components, empty tasks/. */
export const FRESH_SEED: Record<string, string> = {
  "specs/requirements/requirements.md":
    "# Requirements\n\n- Shoppers have accounts.\n- Shoppers place orders.\n- Shoppers view their orders in a web UI.\n",
  "specs/design/design.md": DESIGN_MD,
  "specs/design/components/user-service/design.json": designJson("user-service", "service", []),
  "specs/design/components/order-service/design.json": designJson("order-service", "service", [
    { to: "user-service", type: "http" },
    { to: "postgres", type: "datastore" },
  ]),
  "specs/design/components/order-webapp/design.json": designJson("order-webapp", "web-application", [{ to: "order-service", type: "http" }]),
};

function existingTask(issueNumber: number, component: string, title: string, dependsOn: string[]): string {
  const deps = dependsOn.length === 0 ? "dependsOn: []\n" : `dependsOn:\n${dependsOn.map((d) => `  - ${d}`).join("\n")}\n`;
  return (
    `---\nissueNumber: ${issueNumber}\ncomponent: ${component}\ntitle: ${title}\n${deps}` +
    `origin: spec-plan\nderivedStatus: pending\nspecTag: requirements-v1\ndesignTag: design-v1\n---\n` +
    `## Scope\n\nImplement ${component}.\n\n## Acceptance criteria\n\n- ${component} works.\n`
  );
}

/**
 * The INCREMENTAL design: the three original components already have Tasks, and a
 * NEW `notification-service` component (depending on user-service) was added — so
 * a correct incremental plan adds ONLY notification-service and does not re-plan
 * the covered, unchanged components.
 */
export const INCREMENTAL_SEED: Record<string, string> = {
  ...FRESH_SEED,
  "specs/design/design.md": DESIGN_MD.replace(
    "- order-webapp (web-application) — the shopper-facing UI.",
    "- order-webapp (web-application) — the shopper-facing UI.\n- notification-service (service) — emails order confirmations.",
  ).replace(
    "- order-webapp → order-service (place and view orders).",
    "- order-webapp → order-service (place and view orders).\n- notification-service → user-service (look up the recipient).",
  ),
  "specs/design/components/notification-service/design.json": designJson("notification-service", "service", [{ to: "user-service", type: "http" }]),
  "tasks/1.md": existingTask(1, "user-service", "Implement user-service", []),
  "tasks/2.md": existingTask(2, "order-service", "Implement order-service", ["user-service"]),
  "tasks/3.md": existingTask(3, "order-webapp", "Implement order-webapp", ["order-service"]),
};

export const taskPlanSuite: TaskPlanSuite = {
  agent: "task-plan",
  defaultSeed: FRESH_SEED,
  // services/agents/evals/task-plan → up four to the repo root's skills/ (ADR-0002).
  skillsDir: join(here, "..", "..", "..", "..", "skills"),
};

export const taskPlanFixtures: TaskPlanFixture[] = [
  {
    name: "fresh",
    description: "Empty tasks/: pick up the skill and plan one Task per component with valid deps + bodies.",
    difficulty: "medium",
    turns: [
      {
        prompt: PLAN_PROMPT,
        expect: {
          loadedSkills: ["task-planning"],
          plannedComponents: ["user-service", "order-service", "order-webapp"],
          minPlanned: 3,
          someDependsOn: true,
          bodyForEveryPlan: true,
          noToolErrors: true,
        },
      },
    ],
  },
  {
    name: "incremental",
    description: "Existing Tasks + one new component: plan only the delta, do not re-plan unchanged components.",
    difficulty: "hard",
    seed: INCREMENTAL_SEED,
    turns: [
      {
        prompt: "The design changed. Update the plan for what changed — do not re-plan components that already have a Task and did not change.",
        expect: {
          loadedSkills: ["task-planning"],
          plannedComponents: ["notification-service"],
          singlePlanFor: ["notification-service"], // exactly one plan — no double-plan with a distinct title
          noPlanForComponents: ["user-service", "order-service", "order-webapp"],
          bodyForEveryPlan: true,
          noToolErrors: true,
        },
      },
    ],
  },
];

export default taskPlanSuite;
