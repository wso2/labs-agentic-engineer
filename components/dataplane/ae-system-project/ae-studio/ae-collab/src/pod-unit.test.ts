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
 * Unit tests of the pod's token deadline pieces that the socket-level tests
 * cannot reach: `onTokenSync` while the IdP is down, and the system clock past
 * setTimeout's 2^31-1 ms cap.
 */

import { mock, test } from "node:test";
import assert from "node:assert/strict";
import type { Connection } from "@hocuspocus/server";
import { IdpUnavailableError, type VerifiedToken } from "@aep/platform-idp-auth";
import { onTokenSyncFor, type CollabContext, type Verify } from "./pod/auth.js";
import { systemClock, type ExpiryGuard } from "./pod/expiry.js";
import type { PodLogLine } from "./pod/log.js";

const cfg = { orgId: "ou-acme", orgHandle: "acme", userAudiences: ["aep-console-client"], agentClientId: "ae-studio-acme" };

function fakeConnection(context: CollabContext) {
  const closes: unknown[] = [];
  const connection = { context, close: (e: unknown) => closes.push(e), onClose: () => connection } as unknown as Connection<CollabContext>;
  return { connection, closes };
}

test("onTokenSync: an IdP that cannot be reached closes nothing and moves no deadline", async () => {
  const armed: number[] = [];
  const expiry: ExpiryGuard = { arm: (_c, exp) => armed.push(exp) };
  const lines: PodLogLine[] = [];
  const verify: Verify = () => Promise.reject(new IdpUnavailableError("down"));
  const sync = onTokenSyncFor(cfg, verify, expiry, (l) => lines.push(l));
  const context: CollabContext = {
    listener: "public",
    user: { name: "Ann", email: "", kind: "user" },
    projectName: "greeter",
    exp: 100,
    subject: "u-ann",
  };
  const { connection, closes } = fakeConnection(context);
  await sync({ token: "t", connection });
  assert.deepEqual(closes, []);
  assert.deepEqual(armed, []);
  assert.equal(context.exp, 100);
  assert.deepEqual(lines, [{ msg: "room_token_unverified", source: "ae-collab", listener: "public", cause: "idp_unavailable" }]);
});

test("onTokenSync: a verified token of the same subject moves the deadline, logging no subject change", async () => {
  const armed: number[] = [];
  const lines: PodLogLine[] = [];
  const verified: VerifiedToken = { kind: "user", claims: { sub: "u-ann", ouId: "ou-acme", ouHandle: "acme", exp: 500 } };
  const sync = onTokenSyncFor(cfg, () => Promise.resolve(verified), { arm: (_c, exp) => armed.push(exp) }, (l) => lines.push(l));
  const context: CollabContext = {
    listener: "public",
    user: { name: "Ann", email: "", kind: "user" },
    projectName: "greeter",
    exp: 100,
    subject: "u-ann",
  };
  await sync({ token: "t", connection: fakeConnection(context).connection });
  assert.deepEqual(armed, [500]);
  assert.equal(context.exp, 500);
  assert.deepEqual(lines.map((l) => l.msg), ["room_token_refreshed"]);
});

test("systemClock: a deadline past setTimeout's cap is reached in steps", () => {
  mock.timers.enable({ apis: ["setTimeout", "Date"], now: 0 });
  try {
    const cap = 2 ** 31 - 1;
    let fired = 0;
    systemClock.schedule(cap + 60_000, () => fired++);
    mock.timers.tick(cap);
    assert.equal(fired, 0, "the first step ends at the cap, short of the deadline");
    mock.timers.tick(59_999);
    assert.equal(fired, 0);
    mock.timers.tick(1);
    assert.equal(fired, 1);
    // A cancel stops a pending step.
    const cancel = systemClock.schedule(Date.now() + cap * 2, () => fired++);
    mock.timers.tick(cap);
    cancel();
    mock.timers.tick(cap);
    assert.equal(fired, 1);
  } finally {
    mock.timers.reset();
  }
});

test("systemClock: a deadline already past fires on the next turn, never synchronously", () => {
  mock.timers.enable({ apis: ["setTimeout", "Date"], now: 10_000 });
  try {
    let fired = 0;
    systemClock.schedule(0, () => fired++);
    assert.equal(fired, 0);
    mock.timers.tick(0);
    assert.equal(fired, 1);
  } finally {
    mock.timers.reset();
  }
});
