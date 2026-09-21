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
 * The wire plan, against two real design bundles: the one-service app the
 * prototype was validated on, and a two-service project where one calls the
 * other.
 *
 * What is pinned is the promise the design makes — every value comes from the
 * design files and nothing is guessed. An env var this planner invents is a
 * service that starts with the wrong wiring and fails in a way that looks like
 * application code.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { assignHostPorts, buildWirePlan, describePlan, planBlockers, readWireSpecs } from "../src/engine/wire/plan.js";
import { composeDocument, maskedPlan } from "../src/engine/wire/compose.js";
import { parse } from "yaml";

const FIXTURES = join(fileURLToPath(new URL(".", import.meta.url)), "fixtures", "wire");
const ONBOARDING = join(FIXTURES, "onboarding");
const TWO = join(FIXTURES, "two-services");

/** Ports without touching the machine: everything from 19090 up is free. */
const allFree = async (): Promise<boolean> => true;

function plan(dir: string, slug: string) {
  return buildWirePlan(readWireSpecs(dir, slug), { secret: () => "s3cret" });
}

test("one service, its database, its Thunder app and the webapp beside them", async () => {
  const wire = await assignHostPorts(plan(ONBOARDING, "onboarding"), allFree);

  assert.equal(wire.composeProject, "aep-wire-onboarding");
  assert.deepEqual(
    wire.services.map((service) => service.name),
    ["onboarding-api"],
  );
  assert.deepEqual(
    wire.databases.map((database) => [database.name, database.database, database.volume]),
    [["onboarding-db", "onboarding_db", "onboarding-onboarding-db"]],
  );
  assert.deepEqual(wire.webapp, {
    name: "onboarding-webapp",
    appPath: "onboarding-webapp",
    apiService: "onboarding-api",
  });
  assert.equal(wire.unresolved.length, 0, "this fixture resolves completely");
});

test("the database's env var names are the design's, not this planner's", async () => {
  const wire = await assignHostPorts(plan(ONBOARDING, "onboarding"), allFree);
  const api = wire.services[0];
  assert.ok(api);
  assert.equal(api.env.ONBOARDING_DB_HOST, "onboarding-db", "the host is the compose service name");
  assert.equal(api.env.ONBOARDING_DB_PORT, "5432");
  assert.equal(api.env.ONBOARDING_DB_USER, "onboarding_db");
  assert.equal(api.env.ONBOARDING_DB_DBNAME, "onboarding_db");
  assert.equal(api.env.ONBOARDING_DB_PASSWORD, "s3cret");
  // Thunder is not stood up, but the service panics on an empty value, so each
  // binding gets something.
  assert.ok(api.env.USER_AUTH_ISSUER?.startsWith("https://"));
  assert.match(api.env.USER_AUTH_SCOPES ?? "", /new-hires:create/);
  assert.deepEqual(api.dependsOn, ["onboarding-db"]);
});

test("the roles carry the login name the assertion will claim", async () => {
  const wire = await assignHostPorts(plan(ONBOARDING, "onboarding"), allFree);
  assert.deepEqual(
    wire.roles.map((role) => [role.name, role.username, role.usernameGuessed]),
    [
      ["HRCoordinator", "test-hrcoordinator", false],
      ["ITOnboardingStaff", "test-itonboardingstaff", false],
      ["FacilitiesOnboardingStaff", "test-facilitiesonboardingstaff", false],
      ["HiringManagerRole", "test-hiringmanagerrole", false],
    ],
  );
});

test("a role with no testUsers row is named as a finding, not silently guessed", async () => {
  const wire = await assignHostPorts(plan(TWO, "two-services"), allFree);
  const desk = wire.roles.find((role) => role.name === "OrderDesk");
  assert.ok(desk);
  assert.equal(desk.username, "OrderDesk");
  assert.equal(desk.usernameGuessed, true);
  assert.ok(describePlan(wire).some((line) => line.includes("OrderDesk has no testUsers row")));
});

test("two services: each on its own host port, reaching the other by service name", async () => {
  const wire = await assignHostPorts(plan(TWO, "two-services"), allFree);
  const orders = wire.services.find((service) => service.name === "orders-api");
  const billing = wire.services.find((service) => service.name === "billing-api");
  assert.ok(orders && billing);
  assert.notEqual(orders.hostPort, billing.hostPort);
  assert.equal(orders.env.BILLING_API_URL, "http://billing-api:9090", "siblings share one port inside the network");
  assert.ok(orders.dependsOn.includes("billing-api"));
  assert.equal(wire.databases.length, 2, "one database container per postgres dependency");
});

test("a dependency wired mode cannot stand in for stops the run, and --skip is the way past it", async () => {
  const wire = await assignHostPorts(plan(TWO, "two-services"), allFree);
  assert.deepEqual(wire.unresolved, [{ component: "orders-api", dependency: "stripe", kind: "external" }]);

  const blocked = planBlockers(wire, TWO);
  assert.equal(blocked.length, 1);
  assert.match(blocked[0] ?? "", /stripe .*--skip stripe/s);
  assert.deepEqual(planBlockers(wire, TWO, ["stripe"]), []);
});

test("an app whose mock mode predates wired mode is refused, not quietly left on MSW", async () => {
  const wire = await assignHostPorts(plan(ONBOARDING, "onboarding"), allFree);
  assert.deepEqual(planBlockers(wire, ONBOARDING), [], "the fixture's app carries the wired asset");

  // The same plan against a tree whose app has no `mock/wired.ts`. Without this
  // the dev server answers `/api` from MSW, the service runs untouched, and
  // every screen looks right — the one failure wired mode exists to catch, and
  // the one every app generated before this existed would hit.
  const blockers = planBlockers({ ...wire, services: [] }, join(FIXTURES, "two-services"));
  assert.equal(blockers.length, 1);
  assert.match(blockers[0] ?? "", /predates wired mode/);
  assert.match(blockers[0] ?? "", /mock-wired\.ts/);
});

test("a service with no Dockerfile has not been built yet, and the plan says so", async () => {
  const wire = await assignHostPorts(plan(ONBOARDING, "onboarding"), allFree);
  // Against a tree with nothing in it, so the app is missing too; this is about
  // the service half.
  const blockers = planBlockers(wire, join(FIXTURES, "nothing-here"));
  assert.equal(blockers.filter((b) => /no Dockerfile.*coding phase/.test(b)).length, 1);
});

test("ports skip what is already taken", async () => {
  const taken = new Set([19090, 19091]);
  const wire = await assignHostPorts(plan(TWO, "two-services"), async (port) => !taken.has(port));
  assert.deepEqual(
    wire.services.map((service) => service.hostPort).sort(),
    [19092, 19093],
  );
});

test("nothing secret is written down or shown", async () => {
  const wire = await assignHostPorts(plan(ONBOARDING, "onboarding"), allFree);
  const masked = maskedPlan(wire);
  assert.equal(masked.databases[0]?.password, "***");
  assert.equal(masked.services[0]?.env.ONBOARDING_DB_PASSWORD, "***");
  assert.equal(masked.services[0]?.env.ONBOARDING_DB_HOST, "onboarding-db", "only the secrets are masked");
  assert.ok(!JSON.stringify(masked).includes("s3cret"));
  assert.ok(!describePlan(wire).join("\n").includes("s3cret"));
});

test("a password is masked by where it came from, not by what the design named it", async () => {
  // THE BINDING NAME BELONGS TO THE DESIGN, and nothing obliges it to look like
  // a secret: `DB_PASS` carries no "PASSWORD", and a lowercase name does not
  // match a case-sensitive pattern at all. Both are legal, and both used to put
  // the real password into `plan.json` and into the triage agent's prompt. The
  // planner knows better than any pattern can — it read the value out of the
  // dependency's `password` output — so that is what decides.
  const specs = readWireSpecs(ONBOARDING, "onboarding");
  const api = specs.workloads["onboarding-api"];
  assert.ok(api?.["onboarding-db"], "the fixture binds a database, or this test proves nothing");
  api["onboarding-db"] = { ...api["onboarding-db"], password: "db_pass" };
  const wire = await assignHostPorts(buildWirePlan(specs, { secret: () => "s3cret" }), allFree);

  assert.equal(wire.services[0]?.env.db_pass, "s3cret", "the container still gets the real value");
  assert.deepEqual(wire.services[0]?.secretEnv, ["db_pass"], "and the plan says which key holds it");

  const masked = maskedPlan(wire);
  assert.equal(masked.services[0]?.env.db_pass, "***");
  assert.ok(!JSON.stringify(masked).includes("s3cret"), "nothing written down carries it");
  assert.ok(!describePlan(wire).join("\n").includes("s3cret"), "and nothing shown does either");
});

test("the compose file the plan becomes", async () => {
  const wire = await assignHostPorts(plan(ONBOARDING, "onboarding"), allFree);
  const yaml = composeDocument(
    wire,
    { certificate: "-----BEGIN CERTIFICATE-----\nPEM\n-----END CERTIFICATE-----\n", issuer: "aep-playground-wire", header: "x-jwt-assertion" },
    "/tmp/project",
  );
  const document = parse(yaml) as {
    name: string;
    services: Record<string, Record<string, unknown>>;
    volumes: Record<string, unknown>;
  };

  assert.equal(document.name, "aep-wire-onboarding");
  assert.deepEqual(Object.keys(document.services).sort(), ["onboarding-api", "onboarding-db"]);

  const api = document.services["onboarding-api"] as {
    build: { context: string };
    ports: string[];
    environment: Record<string, string>;
    depends_on: Record<string, { condition: string }>;
    healthcheck: { test: string[]; start_period: string };
  };
  assert.deepEqual(api.build, { context: "/tmp/project/onboarding-api" }, "built from the agent's own Dockerfile");
  assert.deepEqual(api.ports, ["19090:9090"], "the container port is the contract's; only the host side is mapped");
  assert.equal(api.environment.GATEWAY_ASSERTION_ISSUER, "aep-playground-wire");
  assert.equal(api.environment.GATEWAY_ASSERTION_HEADER, "x-jwt-assertion");
  assert.match(api.environment.GATEWAY_ASSERTION_CERTIFICATE ?? "", /BEGIN CERTIFICATE/);
  assert.deepEqual(api.depends_on, { "onboarding-db": { condition: "service_healthy" } });
  // bash, not curl: the JRE images these services build on have no curl, so an
  // HTTP probe would fail for a reason that is not about the service.
  assert.deepEqual(api.healthcheck.test, ["CMD", "bash", "-c", "exec 3<>/dev/tcp/127.0.0.1/9090"]);
  assert.equal(api.healthcheck.start_period, "15s");

  const db = document.services["onboarding-db"] as { image: string; volumes: string[] };
  assert.equal(db.image, "postgres:16");
  assert.deepEqual(db.volumes, ["onboarding-onboarding-db:/var/lib/postgresql/data"]);
  assert.deepEqual(Object.keys(document.volumes), ["onboarding-onboarding-db"]);
});

test("the same bundle plans the same way twice", async () => {
  const first = await assignHostPorts(plan(ONBOARDING, "onboarding"), allFree);
  const second = await assignHostPorts(plan(ONBOARDING, "onboarding"), allFree);
  assert.deepEqual(first, second);
});

/**
 * The invariant that turns a 25-minute hunt into a one-line refusal.
 *
 * A component whose `workload.yaml` names no variable for a dependency wired
 * mode STANDS UP is unresolved, not quietly fine. Without this, compose starts
 * the database, `dependsOn` orders it, the service boots against its own
 * defaults, its health check passes because nothing there touches storage, and
 * the first real query 500s against `localhost:5432` — with every symptom
 * pointing at the generated app and none of them pointing at the plan.
 *
 * Measured: both projects generated on 2026-09-17 declared their dependencies
 * with no `wiring` key at all, so this is the live case, not a hypothetical.
 */
test("a dependency that binds no variable is unresolved, not a silently unconfigured service", async () => {
  const specs = readWireSpecs(ONBOARDING, "onboarding");
  // The component ships no bindings — what an unbuilt component, or one whose
  // workload never declared them, looks like to the planner.
  specs.workloads["onboarding-api"] = {};

  const wire = await assignHostPorts(buildWirePlan(specs, { secret: () => "s3cret" }), allFree);

  assert.deepEqual(
    wire.unresolved.map((entry) => [entry.component, entry.dependency, entry.resourceType]),
    [
      ["onboarding-api", "onboarding-db", "postgres-cnpg"],
      ["onboarding-api", "user-auth", "thunder-app"],
    ],
    "both stood-up dependencies must be reported, so the message names what to fix",
  );
  assert.deepEqual(
    wire.services[0]?.env,
    {},
    "and nothing may be invented to fill the gap",
  );
});

test("bindings come from workload.yaml, the file the platform itself projects from", async () => {
  const specs = readWireSpecs(ONBOARDING, "onboarding");
  const wire = await assignHostPorts(buildWirePlan(specs, { secret: () => "s3cret" }), allFree);

  // The host is the compose service name, not localhost: the value is the
  // plan's, the NAME it lands in is the workload's.
  assert.equal(wire.services[0]?.env.ONBOARDING_DB_HOST, "onboarding-db");
  assert.equal(wire.services[0]?.env.ONBOARDING_DB_DBNAME, "onboarding_db");
  assert.equal(wire.services[0]?.env.USER_AUTH_ISSUER !== undefined, true);
});
