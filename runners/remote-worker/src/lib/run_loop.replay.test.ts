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

// The loop's WHOLE transcript over each recording, pinned byte for byte.
//
// `run_loop.test.ts` asserts the rules one at a time. This asserts that nothing
// else moved: every run event the feed got, every call the watchdog got (which
// kind of observation, with how many events), every message written to the raw
// log, and the moment input ended — in order, stamped with the message being
// read. It exists because the loop stopped reading message shapes and started
// reading a runtime's `MessageClass` (ADR-0012's amendment), and "the watchdog
// is told exactly what it was told before" is a claim about all of those at
// once, which no single-rule test can make.
//
// The golden files were captured from the loop BEFORE that change and are the
// evidence it changed nothing. A diff here is a behaviour change: if it is
// intended, regenerate with `AEP_UPDATE_GOLDEN=1 pnpm test` and say why in the
// change that does it — never edit a golden by hand.

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { consumeRun } from "./run_loop.js";
import { createClaudeClassifier } from "../runtime/claude/classify.js";
import { createClaudeAdapter } from "../runtime/claude/translate.js";
import type { RunWatchdog } from "./progress/watchdog.js";

type Cursor = number | "closed";

const FIXTURES = new URL("../../test/fixtures/", import.meta.url);

function recording(name: string): unknown[] {
  return fs
    .readFileSync(new URL(name, FIXTURES), "utf8")
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line) as unknown);
}

/**
 * Replay one recording and return the loop's transcript, one JSON line per side
 * effect.
 *
 * Deterministic by construction: the adapter's clock advances a fixed step per
 * reading (so heartbeat budgets and durations are reproducible, and a change in
 * how often the clock is read shows up here), and the source holds open after
 * its last message the way a real CLI does until its input ends — so the input
 * rule is part of the transcript, whether a turn end or the grace ends it.
 */
async function transcript(messages: unknown[], requestedSkills?: string[]): Promise<string> {
  const lines: string[] = [];
  const log = (o: unknown): void => {
    lines.push(JSON.stringify(o));
  };
  let cursor: Cursor = 0;
  let clock = 1_000_000;
  let release!: () => void;
  const released = new Promise<void>((resolve) => {
    release = resolve;
  });
  async function* source(): AsyncGenerator<unknown> {
    try {
      for (let i = 0; i < messages.length; i++) {
        cursor = i + 1;
        yield messages[i];
      }
      // A run whose input never ends would hang a real pod until its deadline;
      // here it is released and SAID, so the golden records it.
      const fallback = setTimeout(() => {
        log({ fallbackRelease: true });
        release();
      }, 300);
      await released;
      clearTimeout(fallback);
    } finally {
      cursor = "closed";
    }
  }
  const watchdog: RunWatchdog = {
    observe: (events) => log({ at: cursor, wd: "observe", n: events.length }),
    observeRetry: (info) => log({ at: cursor, wd: "observeRetry", info }),
    observeStream: () => log({ at: cursor, wd: "observeStream" }),
    check: () => {},
    describe: () => "WD",
    start: () => () => {},
  };
  const result = await consumeRun(
    {
      messages: source(),
      stopTask: async (id) => log({ at: cursor, stopTask: id }),
      endInput: () => {
        log({ at: cursor, endInput: true });
        release();
      },
    },
    {
      translate: createClaudeAdapter({ taskKind: "implementation", now: () => (clock += 3000) }).translate,
      classify: createClaudeClassifier(),
      watchdog,
      emit: (event) => log({ at: cursor, event }),
      record: (m) => log({ at: cursor, record: m && typeof m === "object" ? ((m as { type?: string }).type ?? "?") : String(m) }),
      ...(requestedSkills ? { requestedSkills } : {}),
      inputGraceMs: 20,
    },
  );
  log({ result });
  return lines.join("\n") + "\n";
}

async function assertGolden(actual: string, golden: string): Promise<void> {
  const file = new URL(golden, FIXTURES);
  if (process.env.AEP_UPDATE_GOLDEN === "1") {
    fs.writeFileSync(file, actual);
    return;
  }
  assert.equal(actual, fs.readFileSync(file, "utf8"), `the loop's transcript moved — see ${golden}`);
}

test("replay golden: probe 1's loop transcript is unchanged", async () => {
  // A requested skill the session does not resolve, so the preload warning is
  // part of the transcript too.
  const actual = await transcript(recording("probe1-background-fanout.jsonl"), ["aep", "nope"]);
  await assertGolden(actual, "probe1-background-fanout.loop.ndjson");
});

test("replay golden: probe 2's loop transcript is unchanged", async () => {
  const actual = await transcript(recording("probe2-lead-ends-early.jsonl"));
  await assertGolden(actual, "probe2-lead-ends-early.loop.ndjson");
});
