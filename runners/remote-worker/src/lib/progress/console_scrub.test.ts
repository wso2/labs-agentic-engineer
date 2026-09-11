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

import { test, beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { installConsoleScrubber, installLogRedaction, type ConsoleLike } from "./console_scrub.js";
import { _resetEmitterForTesting } from "./emitter.js";
import { scrubber } from "./scrubber.js";

// A token whose SHAPE the scrubber does not recognise, so these tests prove the
// literal-enrollment path rather than the regex path.
const OPAQUE_TOKEN = "aQ7fL2mZ9xR4tY6uP1sD3gH5jK8nB0vC";

// The bridge writes to the real stdout (that IS the progress feed), so the
// tests capture the fd rather than a fake sink — which also proves the output
// is parseable NDJSON, the property the whole change is about.
let captured: string[] = [];
let restore: (() => void) | undefined;

function captureStdout(): void {
  const original = process.stdout.write.bind(process.stdout);
  restore = () => {
    process.stdout.write = original;
  };
  process.stdout.write = ((chunk: string | Uint8Array): boolean => {
    captured.push(typeof chunk === "string" ? chunk : Buffer.from(chunk).toString("utf8"));
    return true;
  }) as typeof process.stdout.write;
}

/** Every emitted event, parsed — a parse failure fails the test by design. */
function events(): Array<Record<string, unknown>> {
  return captured
    .join("")
    .split("\n")
    .filter((l) => l.trim() !== "")
    .map((l) => JSON.parse(l) as Record<string, unknown>);
}

function summaries(): string[] {
  return events().map((e) => String(e.detail));
}

/** A console with all five methods, so the bridge has something to wrap. */
function fakeConsole(): ConsoleLike {
  const noop = (): void => {};
  return { log: noop, info: noop, warn: noop, error: noop, debug: noop };
}

beforeEach(() => {
  captured = [];
  _resetEmitterForTesting();
  captureStdout();
});

afterEach(() => {
  restore?.();
});

test("installConsoleScrubber: console output lands on the feed as typed, parseable log events", () => {
  const c = fakeConsole();
  installConsoleScrubber(c);

  c.log("[local] materialised 6 skill(s); preload=4");
  c.warn("careful");
  c.error("broken");

  const out = events();
  assert.equal(out.length, 3);
  // The envelope every other event carries — this is what a bare stdout line
  // used to be missing, and what made the feed not-NDJSON.
  for (const e of out) {
    assert.equal(e.kind, "notice");
    assert.equal(e.v, 2);
    assert.equal(e.agentId, "lead", "a runner line is the lead's unless somebody says otherwise");
    assert.ok(typeof e.ts === "string" && e.ts !== "");
    assert.ok(typeof e.seq === "number");
  }
  assert.deepEqual(out.map((e) => e.level), ["info", "warn", "error"]);
  assert.equal(out[0]?.detail, "[local] materialised 6 skill(s); preload=4");
  // seq is monotonic across the bridge and the rest of the feed alike.
  assert.deepEqual(out.map((e) => e.seq), [1, 2, 3]);
});

test("installConsoleScrubber: redacts an enrolled literal on every method", () => {
  const c = fakeConsole();
  installConsoleScrubber(c);
  scrubber.addLiteral(OPAQUE_TOKEN);

  c.log(`clone used ${OPAQUE_TOKEN}`);
  c.warn(`warn ${OPAQUE_TOKEN}`);
  c.error(`error ${OPAQUE_TOKEN}`);
  c.info(`info ${OPAQUE_TOKEN}`);
  c.debug(`debug ${OPAQUE_TOKEN}`);

  const lines = summaries();
  assert.equal(lines.length, 5);
  for (const line of lines) {
    assert.ok(!line.includes(OPAQUE_TOKEN), `leaked: ${line}`);
    assert.match(line, /\[REDACTED\]/);
  }
});

test("installConsoleScrubber: literals enrolled after install still redact", () => {
  // The git token is minted mid-run, well after the wrapper is installed.
  const c = fakeConsole();
  installConsoleScrubber(c);

  c.log(`before ${OPAQUE_TOKEN}`);
  scrubber.addLiteral(OPAQUE_TOKEN);
  c.log(`after ${OPAQUE_TOKEN}`);

  const lines = summaries();
  assert.ok(lines[0]?.includes(OPAQUE_TOKEN), "pre-enrollment line is not covered by a literal");
  assert.ok(!lines[1]?.includes(OPAQUE_TOKEN), `leaked after enrollment: ${lines[1]}`);
});

test("installConsoleScrubber: scrubs a token inside an Error's stack", () => {
  const c = fakeConsole();
  installConsoleScrubber(c);
  scrubber.addLiteral(OPAQUE_TOKEN);

  // The shape console.error("...:", err) takes at the runner's entry points.
  c.error("[oneshot] unhandled error:", new Error(`Command failed: git clone ${OPAQUE_TOKEN}`));

  const lines = summaries();
  assert.equal(lines.length, 1);
  assert.ok(!lines[0]?.includes(OPAQUE_TOKEN), `leaked: ${lines[0]}`);
  assert.match(String(lines[0]), /\[oneshot\] unhandled error:/);
  // The stack survives — redaction must not cost us the diagnostic.
  assert.match(String(lines[0]), /Error: Command failed/);
  // A multi-line stack stays ONE event: a newline inside the summary is
  // JSON-escaped, so it cannot split the feed into unparseable fragments.
  assert.equal(events().length, 1);
});

test("installConsoleScrubber: preserves printf-style formatting", () => {
  const c = fakeConsole();
  installConsoleScrubber(c);
  c.log("materialised %d skill(s) for %s", 3, "api");
  assert.equal(summaries()[0], "materialised 3 skill(s) for api");
});

test("installConsoleScrubber: leaves ordinary lines untouched", () => {
  const c = fakeConsole();
  installConsoleScrubber(c);
  scrubber.addLiteral(OPAQUE_TOKEN);
  c.log("[oneshot] no per-task skills to materialise");
  assert.equal(summaries()[0], "[oneshot] no per-task skills to materialise");
});

test("installConsoleScrubber: is idempotent — no double wrapping", () => {
  const c = fakeConsole();
  installConsoleScrubber(c);
  installConsoleScrubber(c);
  scrubber.addLiteral(OPAQUE_TOKEN);

  c.log(`x ${OPAQUE_TOKEN} y`);
  const lines = summaries();
  assert.equal(lines.length, 1);
  assert.equal(lines[0], "x [REDACTED] y");
});

test("installLogRedaction: enrolls the mounted credentials and says nothing when all are covered", () => {
  const c = fakeConsole();
  installLogRedaction(c, { GITHUB_TOKEN: OPAQUE_TOKEN });

  // No warning: everything mounted is enrolled, so a healthy run stays quiet.
  assert.equal(events().length, 0);
  // And the enrollment is real — the shape of this token matches no pattern.
  c.log(`remote: ${OPAQUE_TOKEN}`);
  assert.ok(!summaries()[0]?.includes(OPAQUE_TOKEN));
});

test("installLogRedaction: warns by NAME when a mounted credential is too short to enroll", () => {
  const c = fakeConsole();
  installLogRedaction(c, { GITHUB_TOKEN: "short-tok" });

  const warned = events();
  assert.equal(warned.length, 1);
  assert.equal(warned[0]?.kind, "notice");
  assert.equal(warned[0]?.level, "warn");
  // Code-LESS on purpose: `code` names closed conditions a consumer branches
  // on, and this is prose for whoever reads the log.
  assert.equal(warned[0]?.code, undefined);
  const detail = String(warned[0]?.detail);
  // The NAME is what makes the warning actionable...
  assert.match(detail, /GITHUB_TOKEN/);
  // ...and the value must never ride along: this line goes to the build log,
  // so putting it there would be the disclosure the module exists to prevent.
  assert.ok(!detail.includes("short-tok"));
});
