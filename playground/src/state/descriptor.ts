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
 * The project descriptor — `specs/.agentic-engineer.toml`, the playground's
 * half of what aep-api writes on project create
 * (services/aep-api/internal/spec/descriptor.go). It MARKS the directory as an
 * Agentic Engineer project and carries the idea the user gave at creation,
 * which `/start` turns into requirements.
 *
 * Note the boundary this file draws against `project.ts`: that module owns
 * `.aep-playground/` — playground-local state that never leaves the machine and
 * is invisible to turns. The descriptor is different in kind: it is project
 * CONTENT, living under `specs/` exactly where production commits it, so the
 * same file means the same thing on both surfaces.
 *
 * The agent still cannot read it. Dot-led path segments are stripped from every
 * turn snapshot (`readProjectFiles`, mirroring agentfold.InTurnSnapshot), so
 * the idea reaches a turn ONLY through the project lookup
 * (`engine/tools-fake.ts`) and the design agent's `/start` — never by the
 * model opening the file.
 */

import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { parse, stringify } from "smol-toml";

/** Repo-relative descriptor path — identical to Go's `spec.DescriptorPath`. */
export const DESCRIPTOR_PATH = "specs/.agentic-engineer.toml";

/** Stamped into every descriptor written here — matches Go's `DescriptorAPIVersion`. */
export const DESCRIPTOR_API_VERSION = "agentic-engineer/v1";

export interface Descriptor {
  apiVersion: string;
  name: string;
  createdAt: string;
  idea: string;
}

export function descriptorFile(projectDir: string): string {
  return join(projectDir, DESCRIPTOR_PATH);
}

/**
 * The captured idea, or null. Best-effort exactly like the Go reader: a missing
 * or malformed descriptor yields null and the caller carries on — losing the
 * idea costs one question from the start skill, and there is nothing here worth
 * failing a run over.
 */
export function readIdea(projectDir: string): string | null {
  const file = descriptorFile(projectDir);
  if (!existsSync(file)) return null;
  let parsed: unknown;
  try {
    parsed = parse(readFileSync(file, "utf8"));
  } catch {
    return null; // hand-edited into invalid TOML — treat as "no idea captured"
  }
  const idea = (parsed as Partial<Descriptor> | null)?.idea;
  if (typeof idea !== "string") return null;
  const trimmed = idea.trim();
  return trimmed === "" ? null : trimmed;
}

/**
 * Write the descriptor for `projectDir`. Called at project creation — from
 * `--idea`, or from the prompt shown when the flag is absent — so the marker
 * and the idea land together, before any turn runs.
 *
 * A real TOML encoder rather than hand-rolled key writing: the idea is free
 * text a user typed, so quotes, backslashes and newlines have to survive.
 */
export function writeDescriptor(projectDir: string, name: string, idea: string, createdAt = new Date().toISOString()): void {
  const descriptor: Descriptor = {
    apiVersion: DESCRIPTOR_API_VERSION,
    name,
    createdAt,
    idea: idea.trim(),
  };
  const file = descriptorFile(projectDir);
  mkdirSync(dirname(file), { recursive: true });
  // Same write-then-rename as project.ts: a crash mid-write must not leave a
  // half-written descriptor that later parses as "no idea".
  const tmp = `${file}.tmp`;
  writeFileSync(tmp, stringify(descriptor) + "\n", "utf8");
  renameSync(tmp, file);
}
