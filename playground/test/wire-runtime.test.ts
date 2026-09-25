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
 * Port assignment, which is what lets two wired sessions coexist.
 *
 * The case that matters is the one a loopback bind cannot see: Docker
 * publishes a container port on the WILDCARD address, and binding
 * `127.0.0.1:<same port>` still succeeds while it does. A picker that asks
 * only "can I bind loopback" therefore hands out a port compose will be
 * refused, and the refusal arrives after the image has been built.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer, type Server } from "node:net";

import { findFreePort, isPortAvailable, isPortBusy, isPortFree } from "../src/engine/wire/runtime.js";

/** Hold a port the way Docker does: listening on every interface. */
function listenWildcard(port: number): Promise<Server> {
  return new Promise((resolve, reject) => {
    const server = createServer();
    server.once("error", reject);
    server.listen(port, () => {
      resolve(server);
    });
  });
}

function close(server: Server): Promise<void> {
  return new Promise((resolve) => {
    server.close(() => {
      resolve();
    });
  });
}

test("findFreePort skips a port already published on the wildcard address", async () => {
  // A high port this suite owns, so a developer's own services cannot flake it.
  const taken = 41090;
  const held = await listenWildcard(taken);
  try {
    // The precondition, asserted rather than assumed — if this ever stops
    // holding, the bug this test guards has changed shape and the test should
    // say so instead of passing for the wrong reason.
    //
    // It is per-KERNEL, and asserted per kernel rather than skipped. BSD (macOS)
    // lets `127.0.0.1:P` bind while another process holds `0.0.0.0:P` — that is
    // the trap this picker exists for, and where a developer meets it. Linux
    // refuses the same bind with EADDRINUSE, so the trap cannot arise in CI at
    // all. Both answers are correct for their kernel; an UNEXPECTED one is what
    // must fail, on either. What the test actually guards — that the picker
    // never hands out a port compose will be refused — is asserted
    // unconditionally below, because it has to hold on both.
    const loopbackBindsUnderWildcardHolder = process.platform !== "linux";
    assert.equal(
      await isPortFree(taken),
      loopbackBindsUnderWildcardHolder,
      loopbackBindsUnderWildcardHolder
        ? "a loopback bind still succeeds against a wildcard holder — that is the whole trap"
        : "this kernel refuses a loopback bind under a wildcard holder, so the trap cannot arise here",
    );
    assert.equal(await isPortBusy(taken), true, "but something IS listening there");

    // The shared predicate is what BOTH the dev port and every service port
    // resolve through, so pin it directly as well as through the picker.
    assert.equal(await isPortAvailable(taken), false, "the port is not available to take");

    const picked = await findFreePort(taken);
    assert.notEqual(picked, taken, "the picker must not hand out a port compose will be refused");
    assert.ok(picked > taken, `expected a port past ${String(taken)}, got ${String(picked)}`);
  } finally {
    await close(held);
  }
});

test("findFreePort skips a port held on the IPv6 loopback only", async () => {
  // Vite binds `::1`. A probe that knows only `127.0.0.1` reads the port as
  // free, the second session's dev server is told to use it, and the READY url
  // it prints then serves the FIRST project's app — the tester browses the
  // wrong application without any error being raised anywhere.
  const taken = 41290;
  const held = await new Promise<Server>((resolve, reject) => {
    const server = createServer();
    server.once("error", reject);
    server.listen(taken, "::1", () => {
      resolve(server);
    });
  });
  try {
    assert.equal(await isPortBusy(taken), true, "a v6-only listener still makes the port busy");
    assert.notEqual(await findFreePort(taken), taken);
  } finally {
    await close(held);
  }
});

test("findFreePort returns the first port when nothing holds it", async () => {
  const free = 41190;
  assert.equal(await findFreePort(free), free);
});
