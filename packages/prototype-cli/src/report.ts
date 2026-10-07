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

/** How `check` (and `export`, on findings) prints findings: JSON for agents, lines for people. */

import type { Finding } from "@wso2/prototype-kit/check";

export function formatJson(findings: readonly Finding[]): string {
  return `${JSON.stringify({ ok: findings.length === 0, findings }, null, 2)}\n`;
}

export function formatHuman(findings: readonly Finding[]): string {
  if (findings.length === 0) return "No findings.\n";
  const lines = findings.map((f) => `${f.file} ${f.location}: ${f.code} — ${f.message}`);
  return `${lines.join("\n")}\n\n${findings.length} finding${findings.length === 1 ? "" : "s"}.\n`;
}
