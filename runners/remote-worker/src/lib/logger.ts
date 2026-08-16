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

import fs from "node:fs";
import path from "node:path";

export interface TaskLog {
  write(data: unknown): void;
  close(): void;
  /**
   * Where this run's artifacts live. Exposed because the debug sinks land
   * beside `claude.log` and the two entrypoints do not agree on the base: a pod
   * logs into the workspace, the playground into its own run directory. One
   * decision about where a run writes, made here.
   */
  dir: string;
}

export function openTaskLog(workspacePath: string): TaskLog {
  const logDir = path.join(workspacePath, ".logs");
  fs.mkdirSync(logDir, { recursive: true, mode: 0o755 });
  const stream = fs.createWriteStream(path.join(logDir, "claude.log"), {
    flags: "w",
  });
  return {
    write(data: unknown) {
      stream.write(JSON.stringify(data) + "\n");
    },
    close() {
      stream.end();
    },
    dir: logDir,
  };
}

/**
 * The developer-only sinks: the SDK's own debug log, and the CLI's stderr.
 *
 * Files, never the feed — and that is what makes them developer-only in
 * practice as well as by policy. Nothing collects a pod's files (`claude.log`
 * has been written unconditionally for as long as it has existed and only the
 * playground has ever read one), so a sink here is by construction for someone
 * sitting in front of the run directory. Both can be large and the debug log
 * carries prompt text, which is the whole reason they stay off a build log the
 * console forwards to a browser.
 *
 * `claude-stderr.log` is written by us and scrubbed on the way in;
 * `debugFilePath` is handed to the SDK, which writes it directly and
 * unscrubbed. Some content overlaps and that is accepted — one of the two being
 * the complete record matters more than neither being redundant.
 *
 * The SDK also drops a `latest` symlink beside the debug file it is given. That
 * is its artifact, not ours; it is harmless here because this directory holds
 * nothing but one run's logs.
 */
export interface DebugSinks {
  /** Pass to the SDK's `debugFile` option. */
  debugFilePath: string;
  /** Pass to the SDK's `stderr` option. */
  onStderr(chunk: string): void;
  close(): void;
}

export function openDebugSinks(logDir: string, scrub: (line: string) => string): DebugSinks {
  fs.mkdirSync(logDir, { recursive: true, mode: 0o755 });
  const stream = fs.createWriteStream(path.join(logDir, "claude-stderr.log"), {
    flags: "w",
  });
  return {
    debugFilePath: path.join(logDir, "claude-debug.log"),
    onStderr(chunk: string) {
      stream.write(scrub(chunk));
    },
    close() {
      stream.end();
    },
  };
}

/**
 * The SDK options that exist only to be read by a developer afterwards.
 *
 * Split out as a pure function so the boundary is testable: the expensive,
 * prompt-bearing options must be provably absent from a run that did not ask
 * for them, and "absent" is not something an integration test of a live session
 * can assert.
 *
 * `includePartialMessages` is in here for volume, not secrecy — it multiplies
 * the message count by roughly the token count, and the run loop drops every
 * frame it produces on the floor after the watchdog has seen it. The other two
 * are in here for both reasons.
 *
 * `thinking` and `forwardSubagentText` are the reasoning pair, and they only
 * make sense together. ADR-0002 decision 6 rejected reasoning capture on the
 * measurement that it could not reach the subagents — where a coding run does
 * nearly all of its work — and `forwardSubagentText` is the SDK capability that
 * answers exactly that; see decision 16 for the reversal. Volume is why they
 * stay on this side of the gate: a pod's transcript grows by every subagent's
 * prose, and nothing collects a pod's files to read it back.
 */
export interface DebugQueryOptions {
  includePartialMessages?: true;
  debugFile?: string;
  stderr?: (data: string) => void;
  thinking?: { type: "adaptive"; display: "summarized" };
  forwardSubagentText?: true;
}

export function debugQueryOptions(sinks: DebugSinks | undefined): DebugQueryOptions {
  if (!sinks) return {};
  return {
    includePartialMessages: true,
    debugFile: sinks.debugFilePath,
    stderr: (data: string) => sinks.onStderr(data),
    // Without a display, thinking blocks arrive SIGNED AND EMPTY — measured on
    // the 2026-08-14 run: 7 blocks at the top level, every one `thinking: ""`,
    // while `system`/`thinking_tokens` counted the reasoning that produced them.
    // A transcript that records the reasoning happened but not what it was
    // cannot answer why a run took the path it took.
    thinking: { type: "adaptive", display: "summarized" },
    // The SDK forwards only tool_use/tool_result from a subagent by default,
    // which is why the same run held 120 subagent tool calls and zero words of
    // subagent reasoning. The progress feed is unaffected either way: the
    // translator reads `tool_use` blocks and ignores every other kind.
    forwardSubagentText: true,
  };
}

export function formatDuration(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) return `${minutes}m${seconds}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h${minutes % 60}m${seconds}s`;
}
