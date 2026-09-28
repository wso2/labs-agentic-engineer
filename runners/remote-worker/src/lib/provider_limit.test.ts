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
import type { ApiRetryInfo } from "../runtime/port.js";
import {
  PROVIDER_DETAIL_MAX,
  PROVIDER_LIMIT_MS,
  createProviderLimits,
  providerLimit,
  type ProviderLimitHit,
} from "./provider_limit.js";

const SECOND = 1000;
const MINUTE = 60 * SECOND;

/** A 429 as a runtime reports it, the delay it read off the provider's headers. */
function tooMany(retryDelayMs: number, overrides: Partial<ApiRetryInfo> = {}): ApiRetryInfo {
  return {
    attempt: 1,
    maxRetries: null,
    retryDelayMs,
    errorStatus: 429,
    error: "rate_limit",
    providerText: "Too Many Requests: weekly usage limit reached",
    ...overrides,
  };
}

/**
 * Feed a sequence of retries through one tracker on a fake clock: each step
 * says how far the clock moved BEFORE that retry arrived. Returns every
 * verdict, in order.
 */
function play(steps: { after: number; info: ApiRetryInfo }[]): (ProviderLimitHit | undefined)[] {
  let clock = Date.parse("2026-09-26T10:00:00.000Z");
  const limits = createProviderLimits({ host: "ollama.com", now: () => clock });
  return steps.map(({ after, info }) => {
    clock += after;
    return limits.observe(info);
  });
}

// The rule on its own: the table the design states, boundaries included.
test("providerLimit: the rule, cell by cell", () => {
  const cases: { name: string; retryAfterMs: number | null; waitedMs: number; want: string }[] = [
    { name: "a short wait, early", retryAfterMs: 20 * SECOND, waitedMs: 0, want: "wait" },
    { name: "a short wait, late in a streak", retryAfterMs: 20 * SECOND, waitedMs: 4 * MINUTE, want: "wait" },
    { name: "a retry-after just under five minutes", retryAfterMs: PROVIDER_LIMIT_MS - 1, waitedMs: 0, want: "wait" },
    { name: "a retry-after of exactly five minutes", retryAfterMs: PROVIDER_LIMIT_MS, waitedMs: 0, want: "provider_limit" },
    { name: "a long retry-after on the first 429", retryAfterMs: 4 * 60 * MINUTE, waitedMs: 0, want: "provider_limit" },
    { name: "five minutes of short retries", retryAfterMs: 20 * SECOND, waitedMs: PROVIDER_LIMIT_MS, want: "provider_limit" },
    { name: "no retry-after, early", retryAfterMs: null, waitedMs: 30 * SECOND, want: "wait" },
    { name: "no retry-after, five minutes in", retryAfterMs: null, waitedMs: PROVIDER_LIMIT_MS, want: "provider_limit" },
  ];
  for (const c of cases) {
    assert.equal(providerLimit(c.retryAfterMs, c.waitedMs), c.want, c.name);
  }
});

test("createProviderLimits: short waits are waits, and carry no reset time", () => {
  const hits = play([
    { after: 0, info: tooMany(2 * SECOND) },
    { after: 2 * SECOND, info: tooMany(4 * SECOND) },
    { after: 4 * SECOND, info: tooMany(8 * SECOND) },
  ]);
  assert.deepEqual(
    hits.map((h) => h?.verdict),
    ["wait", "wait", "wait"],
  );
  assert.deepEqual(
    hits.map((h) => h?.waitedMs),
    [0, 2 * SECOND, 6 * SECOND],
    "waited is measured from the streak's first 429",
  );
  assert.ok(
    hits.every((h) => h?.resetAt === undefined),
    "a backoff delay is not a reset time the provider promised",
  );
});

test("createProviderLimits: a long retry-after is a provider limit at once, with its reset time", () => {
  const [hit] = play([{ after: 0, info: tooMany(4 * 60 * MINUTE) }]);
  assert.equal(hit?.verdict, "provider_limit");
  assert.equal(hit?.waitedMs, 0, "no need to wait five minutes to learn what the provider already said");
  assert.equal(hit?.resetAt, "2026-09-26T14:00:00.000Z", "the reset is now plus the delay the provider asked for");
  assert.equal(hit?.status, 429);
  assert.equal(hit?.detail, "Too Many Requests: weekly usage limit reached");
});

test("createProviderLimits: five minutes of short retries is a provider limit, with no reset time", () => {
  // OpenCode with no retry headers: backoff capped at 30s, for as long as it takes.
  const steps = [{ after: 0, info: tooMany(30 * SECOND) }];
  for (let i = 0; i < 10; i++) steps.push({ after: 30 * SECOND, info: tooMany(30 * SECOND) });
  const hits = play(steps);
  const verdicts = hits.map((h) => h?.verdict);
  assert.deepEqual(verdicts.slice(0, 10), Array(10).fill("wait"), "4.5 minutes in, still a wait");
  assert.equal(verdicts[10], "provider_limit", "the retry that lands at five minutes trips the rule");
  assert.equal(hits[10]?.waitedMs, PROVIDER_LIMIT_MS);
  assert.equal(hits[10]?.resetAt, undefined, "the provider never said when it resets");
});

test("createProviderLimits: no retry-after at all trips on time alone", () => {
  // OpenCode's status with no `next` reports a delay of zero, which is "none
  // stated" and must not read as a provider asking for no delay at all.
  const hits = play([
    { after: 0, info: tooMany(0, { errorStatus: null }) },
    { after: 2 * MINUTE, info: tooMany(0, { errorStatus: null }) },
    { after: 3 * MINUTE, info: tooMany(0, { errorStatus: null }) },
  ]);
  assert.deepEqual(
    hits.map((h) => h?.verdict),
    ["wait", "wait", "provider_limit"],
  );
  assert.equal(hits[2]?.status, null, "a rate-limit class with no status is still a 429, and says so honestly");
  assert.equal(hits[2]?.resetAt, undefined);
});

test("createProviderLimits: other retries are not 429s and do not touch the streak", () => {
  let clock = 0;
  const limits = createProviderLimits({ host: "ollama.com", now: () => clock });
  assert.ok(limits.observe(tooMany(20 * SECOND)));
  clock += 4 * MINUTE;
  assert.equal(limits.observe(tooMany(20 * SECOND, { errorStatus: 529, error: "overloaded" })), undefined);
  assert.equal(limits.observe(tooMany(1 * SECOND, { errorStatus: null, error: "connection" })), undefined);
  clock += 1 * MINUTE;
  assert.equal(limits.observe(tooMany(20 * SECOND))?.verdict, "provider_limit", "the overload in between neither reset nor paused it");
});

test("createProviderLimits: the model answering ends the streak", () => {
  let clock = 0;
  const limits = createProviderLimits({ host: "ollama.com", now: () => clock });
  limits.observe(tooMany(20 * SECOND));
  assert.equal(limits.open()?.verdict, "wait", "a streak in progress is open");
  clock += 4 * MINUTE;
  limits.progress();
  assert.equal(limits.open(), undefined, "an answer closes it");
  clock += 2 * MINUTE;
  const hit = limits.observe(tooMany(20 * SECOND));
  assert.equal(hit?.verdict, "wait", "a new streak starts from zero, not from the first 429 of the run");
  assert.equal(hit?.waitedMs, 0);
});

test("createProviderLimits: the provider's text is capped for the settle", () => {
  const [hit] = play([{ after: 0, info: tooMany(20 * SECOND, { providerText: "x".repeat(1000) }) }]);
  assert.equal(hit?.detail.length, PROVIDER_DETAIL_MAX);
  assert.ok(hit?.detail.endsWith("…"));
});
