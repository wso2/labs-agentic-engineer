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
 * The sweep: every (case × config × repeat), N at a time, from one queue
 * (ADR-0005). SIGINT/SIGTERM abort one shared signal; each attempt tears down
 * through its normal path.
 */

import type { EvalCase, RunConfig } from "./case.js";
import { runAttempt, type AttemptRecord } from "./attempt.js";
import { failureText } from "./report.js";

export interface SweepOptions {
  sweepId: string;
  cases: { evalCase: EvalCase; roles: string[] }[];
  configs: RunConfig[];
  repeats: number;
  concurrency: number;
  token: string;
  dotenv: Record<string, string | undefined>;
  keep: boolean;
  signal: AbortSignal;
  say: (line: string) => void;
}

export async function runSweep(opts: SweepOptions): Promise<AttemptRecord[]> {
  const queue: { evalCase: EvalCase; roles: string[]; config: RunConfig; attempt: number }[] = [];
  for (const { evalCase, roles } of opts.cases) {
    for (const config of opts.configs) {
      for (let attempt = 1; attempt <= opts.repeats; attempt += 1) queue.push({ evalCase, roles, config, attempt });
    }
  }

  const records: AttemptRecord[] = [];
  let next = 0;
  const workers = Array.from({ length: Math.max(1, opts.concurrency) }, async () => {
    for (;;) {
      if (opts.signal.aborted) return;
      const item = queue[next];
      next += 1;
      if (!item) return;
      const label = `${item.evalCase.name} × ${item.config.id} #${String(item.attempt)}`;
      opts.say(`  ▶ ${label}`);
      const started = Date.now();
      const record = await runAttempt({
        sweepId: opts.sweepId,
        evalCase: item.evalCase,
        config: item.config,
        attempt: item.attempt,
        roles: item.roles,
        token: opts.token,
        dotenv: opts.dotenv,
        keep: opts.keep,
        signal: opts.signal,
        say: opts.say,
      });
      records.push(record);
      const minutes = Math.round((Date.now() - started) / 60_000);
      const score = record.score === null ? "" : ` ${String(record.score)} ${record.band ?? ""}`;
      opts.say(`  ${record.status === "scored" ? "✓" : "✗"} ${label} — ${record.status}${score}${failureText(record) ? ` · ${failureText(record)}` : ""} (${String(minutes)} min)`);
    }
  });
  await Promise.all(workers);

  return records.sort((a, b) =>
    `${a.case}/${a.config}/${String(a.attempt).padStart(3, "0")}`.localeCompare(`${b.case}/${b.config}/${String(b.attempt).padStart(3, "0")}`),
  );
}
