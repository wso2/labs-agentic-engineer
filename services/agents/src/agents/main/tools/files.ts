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
 * The file-mutation tools the main agent calls (the `files` tool set), built over
 * a FileBundle. The skill loaders (`loadSkill`/`loadSkillReference`) are shared
 * across tool sets and live in `./skill-tools.ts`.
 *
 * PROPERTY ORDER IS LOAD-BEARING. `path` is the first property in every schema
 * so the provider streams it first and a consumer can render the file header the
 * instant it resolves; the large string (`content` / `newString`) is last so it
 * streams delta-by-delta. The execute() return value IS what the model reads to
 * decide its next step.
 *
 * The Zod `inputSchema`s are the runtime validators; the corresponding wire
 * `*Input` types live in `@aep/agent-stream` (the source of truth). A compile-time
 * drift guard below asserts `z.infer<schema>` stays equal to each wire type.
 */

import { tool } from "ai";
import type { Tool } from "ai";
import { z } from "zod";
import {
  FileBundle,
  type AddFileInput,
  type EditFileInput,
  type Equal,
  type RemoveFileInput,
} from "@aep/agent-stream";
import { buildSkillTools } from "./skill-tools.js";
import type { SkillSource } from "../skill-source.js";

export const ADD_FILE = "addFile" as const;
export const EDIT_FILE = "editFile" as const;
export const REMOVE_FILE = "removeFile" as const;
/** A tool for human-in-the-loop questions — implemented, but disabled. */
export const ASK_QUESTION = "ask_question" as const;

// Re-export the shared skill-loader names so existing importers keep one entry point.
export { LOAD_SKILL, LOAD_SKILL_REFERENCE } from "./skill-tools.js";

// --- Input schemas (runtime validators; their types are the wire `*Input`) ---

export const addFileInputSchema = z.object({
  path: z
    .string()
    .describe('New bundle path, e.g. "specs/design/components/foo/openapi.yaml". Must not already exist.'),
  content: z.string().describe("The full initial file body."),
});

export const editFileInputSchema = z.object({
  path: z.string().describe("Existing bundle path to edit."),
  oldString: z
    .string()
    .min(1)
    .describe("Verbatim snippet to replace, including its exact leading whitespace. Must occur exactly once."),
  newString: z.string().describe("Replacement text (may be empty to delete the snippet)."),
});

export const removeFileInputSchema = z.object({
  path: z.string().describe("Existing bundle path to delete."),
});

// --- Drift guard: Zod schema ⇄ sse-events wire type -------------------------
// Compile-time only. If a schema's inferred input diverges from its wire type,
// the corresponding `true` is no longer assignable and this fails to compile,
// forcing the schema and contract back in sync. No meaningful runtime effect.
const _drift: [
  Equal<z.infer<typeof addFileInputSchema>, AddFileInput>,
  Equal<z.infer<typeof editFileInputSchema>, EditFileInput>,
  Equal<z.infer<typeof removeFileInputSchema>, RemoveFileInput>,
] = [true, true, true];
void _drift;

// --- ask_question (HITL, Option B) — implemented but NOT registered ----------
// HAS an execute() returning a RESOLVED placeholder, so the transcript ends
// fully-resolved (no dangling tool_use → no MissingToolResultsError on
// persist/replay). Enabling HITL = uncomment the registration line in
// buildFileTools AND the paired `hasToolCall("ask_question")` stop condition at
// the call site. The user's answer arrives as the NEXT turn's plain user message.
// No test covers this path while disabled (§5).
export const askQuestionInputSchema = z.object({
  question: z.string().describe("The clarifying question to ask the user."),
});

/** @knipkeep Unwired HITL tool — enabled by uncommenting its registration in buildFileTools (see the note above). */
export const askQuestionTool: Tool = tool({
  description:
    "Ask the user a clarifying question when the instruction is ambiguous and you cannot proceed safely. " +
    "Ends your turn; the user's answer arrives as the next message.",
  inputSchema: askQuestionInputSchema,
  execute: async ({ question }) => ({ status: "awaiting_user_response" as const, question }),
});

/**
 * Build the file tool set bound to one bundle for the duration of a turn. When
 * `skills` (a `SkillSource`) is non-empty, also registers the shared skill
 * loaders (ADR-0002). No skills → no `loadSkill` (the catalog is likewise
 * omitted from the prompt), so a skill-free turn is byte-identical.
 */
export function buildFileTools(bundle: FileBundle, skills?: SkillSource): Record<string, Tool> {
  const tools: Record<string, Tool> = {
    [ADD_FILE]: tool({
      description:
        "Create a NEW file. The only tool that emits a whole body — use it for files that do not exist yet, " +
        "or (after removeFile) to replace a file wholesale. Errors with ALREADY_EXISTS if the path is already present.",
      inputSchema: addFileInputSchema,
      execute: async ({ path, content }) => bundle.addFile(path, content),
    }),

    [EDIT_FILE]: tool({
      description:
        "Change part of an existing file by replacing oldString with newString. oldString must be copied VERBATIM " +
        "from the file (including leading indentation and newlines) and must match EXACTLY ONE location. On NOT_UNIQUE, " +
        "broaden the anchor with surrounding lines; on NOT_FOUND, re-copy the snippet exactly. Use this for prose AND " +
        "openapi.yaml.",
      inputSchema: editFileInputSchema,
      execute: async ({ path, oldString, newString }) => bundle.editFile(path, oldString, newString),
    }),

    [REMOVE_FILE]: tool({
      description:
        "Delete a file. Idempotent (deleting an absent path is a NOOP success). Refuses to delete the structural roots " +
        "(requirements.md, design.md) with PROTECTED_PATH.",
      inputSchema: removeFileInputSchema,
      execute: async ({ path }) => bundle.removeFile(path),
    }),

    // ask_question — human-in-the-loop follow-up. Implemented (Phase 5) but NOT
    // registered: enabling HITL = uncomment the line below AND the paired
    // hasToolCall("ask_question") stop condition at the call site. See §5/§10.
    // [ASK_QUESTION]: askQuestionTool,
  };

  return { ...tools, ...buildSkillTools(skills) };
}
