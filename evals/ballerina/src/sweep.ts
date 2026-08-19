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
 * The sweep: every selected case, every repeat, N at a time.
 *
 * Attempts are the unit of concurrency rather than cases, so three repeats of
 * one case fill the lanes just as well as three different cases — which matters
 * when you are iterating on the one case a change was meant to fix.
 */

import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import type { EvalCase } from "./cases.js";
import { runSession } from "./driver.js";
import { ensureToolRegistered } from "./preflight.js";
import { readAgentBuilds, readBuildAttempt, summarize, type BuildMetrics } from "./metrics/build.js";
import { parseTranscript, readPathMetrics, toolResults, type PathMetrics } from "./metrics/transcript.js";
import { archive, prepareScratch, readSources, verify } from "./scratch.js";

export interface SweepOptions {
  cases: EvalCase[];
  repeats: number;
  concurrency: number;
  /** Per-attempt wall-clock ceiling. */
  timeoutMs: number;
  /** Where attempt directories are laid out. */
  runsRoot: string;
  skillsDir: string;
  onEvent(event: SweepEvent): void;
}

export type SweepEvent =
  | { kind: "start"; case: string; attempt: number }
  | { kind: "done"; case: string; attempt: number; green: boolean; lookups: number; ms: number }
  | { kind: "failed"; case: string; attempt: number; reason: string }
  /**
   * Something about the ENVIRONMENT was wrong and was put right, which the attempt
   * itself survives. Separate from "failed" because it is not the attempt that
   * failed, and separate from silence because a sweep that quietly repaired its own
   * toolchain is a sweep whose earlier attempts may already be worthless.
   */
  | { kind: "repaired"; case: string; attempt: number; what: string };

/** One attempt's full record — the row every report is built from. */
export interface Attempt {
  case: string;
  suite: string;
  attempt: number;
  path: PathMetrics;
  build: BuildMetrics;
  /** The harness's own verifying build, which is what decides `green`. */
  verified: { green: boolean; errors: number; signatureErrors: number };
  imports: string[];
  /** Declared expectations that did not hold. Empty is a pass. */
  violations: string[];
  timedOut: boolean;
  error?: string;
  /** Where the workspace and logs were left, for a human to open. */
  dir: string;
}

export async function runSweep(opts: SweepOptions): Promise<Attempt[]> {
  const queue: { evalCase: EvalCase; attempt: number }[] = [];
  for (const evalCase of opts.cases) {
    for (let attempt = 1; attempt <= opts.repeats; attempt += 1) queue.push({ evalCase, attempt });
  }

  const results: Attempt[] = [];
  let next = 0;
  // A fixed pool of workers pulling from one queue, rather than chunking into
  // batches of `concurrency`: a batch runs at the speed of its slowest member
  // and leaves lanes idle, and case durations here differ by 10x between the
  // narrow and full suites.
  const workers = Array.from({ length: Math.max(1, opts.concurrency) }, async () => {
    for (;;) {
      const index = next;
      next += 1;
      const item = queue[index];
      if (!item) return;
      results.push(await runAttempt(item.evalCase, item.attempt, opts));
    }
  });
  await Promise.all(workers);

  return results.sort(
    (a, b) => `${a.suite}/${a.case}/${a.attempt}`.localeCompare(`${b.suite}/${b.case}/${b.attempt}`),
  );
}

async function runAttempt(evalCase: EvalCase, attempt: number, opts: SweepOptions): Promise<Attempt> {
  opts.onEvent({ kind: "start", case: `${evalCase.suite}/${evalCase.name}`, attempt });
  const started = Date.now();
  const base = join(opts.runsRoot, evalCase.suite, evalCase.name, `attempt-${attempt}`);
  mkdirSync(base, { recursive: true });

  try {
    // Before EVERY attempt, not once per sweep: a `bal tool pull` inside any
    // concurrent session can drop the local `library` registration, and from that
    // moment every lookup in every attempt answers `unknown command 'library'`.
    // See ensureToolRegistered — this restores the tool the sweep began against.
    const repaired = ensureToolRegistered();
    if (repaired) {
      opts.onEvent({ kind: "repaired", case: `${evalCase.suite}/${evalCase.name}`, attempt, what: repaired });
    }

    const scratch = prepareScratch(opts.runsRoot, evalCase, attempt, opts.skillsDir);
    const session = await runSession({
      workspace: scratch.workspace,
      runDir: scratch.runDir,
      prompt: evalCase.prompt,
      timeoutMs: opts.timeoutMs,
    });

    const messages = parseTranscript(session.transcript);
    const path = readPathMetrics(session.transcript);
    const build = summarize(readAgentBuilds(bashCalls(messages)));
    const verified = verify(scratch.workspace);
    const verifiedAttempt = readBuildAttempt(verified.output, verified.exitCode);
    const sources = readSources(scratch.workspace);

    const record: Attempt = {
      case: evalCase.name,
      suite: evalCase.suite,
      attempt,
      path,
      build,
      verified: {
        green: verified.exitCode === 0,
        errors: verifiedAttempt.errors.length,
        signatureErrors: verifiedAttempt.signatureErrors.length,
      },
      imports: verified.imports,
      violations: checkExpectations(evalCase, verified.exitCode === 0, verified.imports, sources),
      timedOut: session.timedOut,
      ...(session.error !== undefined ? { error: session.error } : {}),
      dir: scratch.archiveDir,
    };
    writeFileSync(join(scratch.runDir, "metrics.json"), JSON.stringify(record, null, 2));
    // The transcript, kept. Every metric above is DERIVED from it, so without the source a number
    // that moved cannot be explained afterwards — a sweep reporting "2 signature errors" with no
    // record of which two is a measurement you have to re-run the sweep to read.
    writeFileSync(join(scratch.runDir, "transcript.jsonl"), session.transcript);
    writeFileSync(join(scratch.runDir, "build.log"), verified.output);
    // The package moves from staging into `.runs/` only now that it has been
    // scored, so the artifact a human opens is the one that was measured. Losing
    // the copy is not worth failing an attempt that already has its numbers.
    try {
      archive(scratch);
    } catch {
      /* the measurement stands; only the readable copy is gone */
    }
    opts.onEvent({
      kind: "done",
      case: `${evalCase.suite}/${evalCase.name}`,
      attempt,
      green: record.verified.green,
      lookups: path.lookups,
      ms: Date.now() - started,
    });
    return record;
  } catch (e) {
    const reason = e instanceof Error ? e.message : String(e);
    opts.onEvent({ kind: "failed", case: `${evalCase.suite}/${evalCase.name}`, attempt, reason });
    // A harness failure is recorded as an attempt rather than thrown, so one
    // broken case cannot abandon a sweep that has already spent an hour.
    return {
      case: evalCase.name,
      suite: evalCase.suite,
      attempt,
      path: readPathMetrics(""),
      build: summarize([]),
      verified: { green: false, errors: 0, signatureErrors: 0 },
      imports: [],
      violations: [`harness error: ${reason}`],
      timedOut: false,
      error: reason,
      dir: base,
    };
  }
}

/** Every Bash command the session ran, paired with what it printed. */
function bashCalls(messages: Record<string, unknown>[]): { command: string; output: string }[] {
  const results = toolResults(messages);
  const out: { command: string; output: string }[] = [];
  for (const message of messages) {
    if (message.type !== "assistant") continue;
    const inner = message.message;
    if (!inner || typeof inner !== "object") continue;
    const content = (inner as { content?: unknown }).content;
    if (!Array.isArray(content)) continue;
    for (const raw of content) {
      if (!raw || typeof raw !== "object") continue;
      const block = raw as Record<string, unknown>;
      if (block.type !== "tool_use" || block.name !== "Bash") continue;
      const command = (block.input as { command?: unknown } | undefined)?.command;
      if (typeof command !== "string") continue;
      out.push({ command, output: results.get(String(block.id)) ?? "" });
    }
  }
  return out;
}

function checkExpectations(
  evalCase: EvalCase,
  green: boolean,
  imports: string[],
  sources: string,
): string[] {
  const expect = evalCase.expect ?? {};
  const violations: string[] = [];
  if (expect.builds !== false && !green) violations.push("bal build failed");
  for (const required of expect.imports ?? []) {
    if (!imports.includes(required)) violations.push(`missing import ${required}`);
  }
  for (const forbidden of expect.importsNot ?? []) {
    if (imports.includes(forbidden)) violations.push(`unexpected import ${forbidden}`);
  }
  // The behavioural axis. Patterns were compiled once at load, so a bad one has
  // already failed the run rather than one attempt of it.
  for (const pattern of expect.mustContain ?? []) {
    if (!new RegExp(pattern).test(sources)) violations.push(`source does not match ${pattern}`);
  }
  for (const pattern of expect.mustNotContain ?? []) {
    if (new RegExp(pattern).test(sources)) violations.push(`source matches forbidden ${pattern}`);
  }
  return violations;
}
