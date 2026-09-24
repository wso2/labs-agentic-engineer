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

import { test } from "node:test";
import assert from "node:assert/strict";
import { apiRetryLine } from "../../lib/run_loop.js";
import { createOpencodeClassifier } from "./classify.js";

const ev = (type: string, properties: Record<string, unknown> = {}) => ({ type, properties });
const created = (id: string, parentID?: string) => ev("session.created", { info: { id, ...(parentID ? { parentID } : {}) } });

test("classify: the root's idle ends a turn; a child's life is task bookkeeping by session id", () => {
  const classify = createOpencodeClassifier();
  assert.deepEqual(classify(created("root")), { kind: "activity" });
  assert.deepEqual(classify(created("kid", "root")), { kind: "task_bookkeeping", started: "kid" });
  assert.deepEqual(classify(ev("session.idle", { sessionID: "kid" })), { kind: "task_bookkeeping", ended: "kid" });
  assert.deepEqual(classify(ev("session.idle", { sessionID: "root" })), { kind: "turn_end" });
  // The status pair's idle is bookkeeping for the translator and the close
  // rule; only `session.idle` ends anything, so nothing counts twice.
  assert.deepEqual(classify(ev("session.status", { sessionID: "root", status: { type: "idle" } })), { kind: "activity" });
});

test("classify: a busy session is a model wait, never activity", () => {
  const classify = createOpencodeClassifier();
  assert.deepEqual(classify(ev("session.status", { sessionID: "kid", status: { type: "busy" } })), { kind: "model_wait", streaming: false });
});

test("classify: a retry is a RETRY, with a closed error word and no invented ceiling", () => {
  const classify = createOpencodeClassifier({ now: () => 1_000 });
  const cls = classify(
    ev("session.status", {
      sessionID: "root",
      status: { type: "retry", attempt: 2, message: "Overloaded: the provider said no (529)", next: 6_000 },
    }),
  );
  assert.deepEqual(cls, {
    kind: "retry",
    info: { attempt: 2, maxRetries: null, retryDelayMs: 5_000, errorStatus: 529, error: "overloaded" },
  });
  // The provider's own text never reaches the line.
  if (cls.kind !== "retry") throw new Error("unreachable");
  assert.equal(apiRetryLine(cls.info), "[api] retry 2 after overloaded (HTTP 529) — next attempt in 5s");
  const other = classify(ev("session.status", { status: { type: "retry", attempt: 1, message: "who knows", next: 0 } }));
  assert.deepEqual(other, {
    kind: "retry",
    info: { attempt: 1, maxRetries: null, retryDelayMs: 0, errorStatus: null, error: "unknown" },
  });
});

test("classify: a permission ask is a denial the run says out loud", () => {
  const cls = createOpencodeClassifier()(
    ev("permission.asked", { id: "per_1", sessionID: "root", permission: "external_directory", patterns: ["/etc/*"] }),
  );
  assert.equal(cls.kind, "stall_signal");
  if (cls.kind !== "stall_signal") return;
  assert.equal(cls.signal.code, "permission_denied");
  assert.equal(cls.signal.level, "warn");
  assert.match(cls.signal.detail, /external_directory on \/etc\/\* asked for approval and was rejected/);
});

test("classify: liveness, init, compaction, noise", () => {
  const classify = createOpencodeClassifier();
  assert.deepEqual(classify(ev("message.part.delta", { sessionID: "root", delta: "hi" })), {
    kind: "model_wait",
    streaming: true,
  });
  assert.deepEqual(classify({ type: "aep.tick" }), { kind: "tool_progress" });
  assert.deepEqual(classify({ type: "aep.skills", skills: ["aep", 3, "go"] }), {
    kind: "init",
    resolvedSkills: ["aep", "go"],
  });
  assert.equal(classify(ev("session.compacted", { sessionID: "root" })).kind, "stall_signal");
  // The bus's chatter must not reset the watchdog's idle clock.
  for (const type of ["server.heartbeat", "server.connected", "plugin.added", "catalog.updated", "file.watcher.updated", "session.diff"]) {
    assert.deepEqual(classify(ev(type)), { kind: "noise" }, type);
  }
  assert.deepEqual(classify(ev("message.part.updated", { part: { type: "text" } })), { kind: "activity" });
  assert.deepEqual(classify(ev("something.new")), { kind: "activity" });
  assert.deepEqual(classify("not an event"), { kind: "activity" });
});
