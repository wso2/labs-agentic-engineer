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
