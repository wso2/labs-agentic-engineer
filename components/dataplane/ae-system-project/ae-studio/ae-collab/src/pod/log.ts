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
 * The pod's log lines: one JSON object per line on stdout, value-free. A line
 * names an event and where it happened, never a token, a claim value or a
 * room's content.
 */

export interface PodLogLine {
  msg:
    | "pod_health_listening"
    | "pod_public_listening"
    | "pod_local_listening"
    | "pod_listeners_stopped"
    | "pod_request_failed"
    | "pod_dev_mode"
    | "room_auth_refused"
    | "room_seed_failed"
    | "room_seed_anomaly"
    | "room_token_refreshed"
    | "room_token_refused"
    | "room_token_expired";
  source: "ae-collab";
  port?: number;
  listener?: "public" | "local";
  /** Why a room or a token was refused: a fixed word, never input. */
  cause?: RefusalCause;
}

export type RefusalCause =
  | "token"
  | "org"
  | "room"
  | "project_unknown"
  | "credit"
  | "files_unavailable"
  | "files_denied";

export type PodLog = (line: PodLogLine) => void;

export const stdoutLog: PodLog = (line) => {
  process.stdout.write(`${JSON.stringify(line)}\n`);
};
