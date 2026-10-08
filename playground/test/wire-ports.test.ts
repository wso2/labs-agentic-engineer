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
 * Port leases: two wired sessions started together must not be handed one port.
 *
 * Every probe here says "free", because that is the race: neither session has
 * bound anything yet when the other asks. Only the lease can tell them apart.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { portLeases } from "../src/engine/wire/ports.js";
import { assignHostPorts, buildWirePlan, FIRST_HOST_PORT, readWireSpecs } from "../src/engine/wire/plan.js";
import { findFreePort } from "../src/engine/wire/runtime.js";

const allFree: (port: number) => Promise<boolean> = () => Promise.resolve(true);

function registry(): string {
  return mkdtempSync(join(tmpdir(), "wire-ports-"));
}

/** Two `wire` processes on one machine: distinct pids, both alive. */
function twoSessions(dir: string, isAvailable: (port: number) => Promise<boolean> = allFree) {
  const alive = new Set([1001, 1002]);
  const isAlive = (pid: number): boolean => alive.has(pid);
  return {
    alive,
    first: portLeases({ dir, isAvailable, pid: 1001, isAlive }),
    second: portLeases({ dir, isAvailable, pid: 1002, isAlive }),
  };
}

/** The two-services fixture: each session needs two host ports. */
function twoServicePlan() {
  const dir = join(fileURLToPath(new URL(".", import.meta.url)), "fixtures", "wire", "two-services");
  return buildWirePlan(readWireSpecs(dir, "two-services"), { secret: () => "s3cret" });
}

test("two sessions assigning at the same moment get disjoint host ports", async () => {
  const { first, second } = twoSessions(registry());
  const [a, b] = await Promise.all([
    assignHostPorts(twoServicePlan(), first.take),
    assignHostPorts(twoServicePlan(), second.take),
  ]);
  const ports = [...a.services, ...b.services].map((service) => service.hostPort);
  assert.equal(ports.length, 4, "the fixture has two services");
  assert.equal(new Set(ports).size, 4, `every port granted once: ${ports.join(", ")}`);
  assert.ok(ports.includes(FIRST_HOST_PORT), "the first port still goes to someone");
});

test("the dev-server port is leased the same way", async () => {
  const { first, second } = twoSessions(registry());
  const [a, b] = await Promise.all([findFreePort(5173, first.take), findFreePort(5173, second.take)]);
  assert.notEqual(a, b);
});

test("a released port is offered again; an unreleased one is not", async () => {
  const { first, second } = twoSessions(registry());
  assert.equal(await first.take(19090), true);
  assert.equal(await second.take(19090), false, "held by a live session");
  await first.release();
  assert.equal(await second.take(19090), true, "given back at teardown");
});

test("a lease whose process is gone is taken over", async () => {
  const { alive, first, second } = twoSessions(registry());
  assert.equal(await first.take(19090), true);
  alive.delete(1001); // SIGKILLed before its teardown ran
  assert.equal(await second.take(19090), true);
});

test("a port the probe refuses is not kept leased", async () => {
  const dir = registry();
  const busy = new Set([19090]);
  const { first, second } = twoSessions(dir, (port) => Promise.resolve(!busy.has(port)));
  assert.equal(await first.take(19090), false, "something that is not a session holds it");
  busy.clear();
  assert.equal(await second.take(19090), true, "and the refused take left no lease behind");
});

test("release gives back only this session's ports", async () => {
  const { first, second } = twoSessions(registry());
  await first.take(19090);
  await second.take(19091);
  await first.release();
  assert.equal(await first.take(19091), false, "the other session's port stays its own");
});

test("a lock left by a dead process does not wedge the next session", async () => {
  const dir = registry();
  writeFileSync(join(dir, "leases.lock"), "1003"); // 1003 is not alive
  const { first } = twoSessions(dir);
  assert.equal(await first.take(19090), true);
  assert.equal(existsSync(join(dir, "leases.lock")), false, "and the lock is gone after");
});

test("a lock held by a live process is waited for, then reported by name", async () => {
  const dir = registry();
  writeFileSync(join(dir, "leases.lock"), "1002"); // the other session, mid-assignment forever
  const stuck = portLeases({ dir, isAvailable: allFree, pid: 1001, isAlive: () => true, lockTimeoutMs: 50 });
  await assert.rejects(stuck.take(19090), /leases\.lock/);
});
