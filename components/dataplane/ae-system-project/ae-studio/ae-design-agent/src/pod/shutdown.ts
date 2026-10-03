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
 * The design agent's SIGTERM (07 §10). Kubernetes signals the pod's three
 * containers at once, and each step here is bounded by what its peer in
 * ae-studio-tools waits for:
 *
 * 1. Refuse new turns: `/v1` and the Turn socket answer `503 shutting_down`.
 *    First, because the desk does not refuse a later start by itself.
 * 2. End every running turn at once (`desk.abortAll`): browsers read
 *    `turn-failed {reason: shutdown}`, the Turn socket `result {status:
 *    failed, code: shutdown}`, and Temporal retries the same `turnId` on the
 *    new pod. A relayed turn is a request in flight on ae-studio-tools'
 *    public listener, which drains for only 5 s, so this ends well inside it
 *    (`abortAll` returns within 2 s even when a run ignores its abort).
 * 3. Hand the finished turns' records over (`outbox.drain`), within
 *    `OUTBOX_DRAIN_MS`: ae-studio-tools keeps its sockets accepting for a
 *    10 s window after SIGTERM (`socketDrainWindow`, ae-studio-tools
 *    `cmd/ae-studio-tools/main.go`), which assumes this drain takes ≤ 8 s.
 * 4. Close the listeners (the Turn socket file goes with them).
 *
 * In all ≤ 2 + 8 + 1 s, inside the pod's ≥ 30 s termination grace.
 */

/** The longest the usage outbox may drain (ae-studio-tools' 10 s socket window assumes ≤ 8 s). */
export const OUTBOX_DRAIN_MS = 8_000;

/** One structured, value-free log line. */
export interface ShutdownLogLine {
  msg: "pod_shutdown_started" | "usage_drain_incomplete";
  source: "ae-design-agent";
}

export interface ShutdownDeps {
  turns: { refuse(): void };
  desk: { abortAll(reason: "shutdown"): Promise<void> };
  outbox: { drain(timeoutMs: number): Promise<boolean> };
  listeners: { close(): Promise<void> };
  log?: (line: ShutdownLogLine) => void;
}

const stdoutLog = (line: ShutdownLogLine): void => {
  process.stdout.write(`${JSON.stringify(line)}\n`);
};

/** Run the shutdown steps in order; resolves once the listeners are closed. */
export async function shutdown(deps: ShutdownDeps): Promise<void> {
  const log = deps.log ?? stdoutLog;
  log({ msg: "pod_shutdown_started", source: "ae-design-agent" });
  deps.turns.refuse();
  await deps.desk.abortAll("shutdown");
  // A record still queued after the bound is lost with the pod (07 §7).
  if (!(await deps.outbox.drain(OUTBOX_DRAIN_MS))) log({ msg: "usage_drain_incomplete", source: "ae-design-agent" });
  await deps.listeners.close();
}
