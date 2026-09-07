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
  ASK_QUESTION_TOOL,
  ASK_QUESTIONS_TOOL,
  DECLARE_PLAN_TOOL,
  type AddFileInput,
  type AskQuestionInput,
  type AskQuestionsInput,
  type DeclarePlanInput,
  type EditFileInput,
  type Equal,
  type OpResult,
  type RemoveFileInput,
} from "@aep/agent-stream";
import { buildSkillTools } from "./skill-tools.js";
import { WriteLedger, type WriteOp } from "./write-ledger.js";
import type { SkillSource } from "../skill-source.js";

export const ADD_FILE = "addFile" as const;
export const EDIT_FILE = "editFile" as const;
export const REMOVE_FILE = "removeFile" as const;

// The HITL question tools' NAMES are owned by the wire contract
// (@aep/agent-stream) so the producer (this service) and the renderers
// (console, playground) can never split on a rename. Re-exported so the call
// site's `hasToolCall` stop conditions read one definition.
export { ASK_QUESTION_TOOL, ASK_QUESTIONS_TOOL, DECLARE_PLAN_TOOL } from "@aep/agent-stream";

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

// --- ask_question / ask_questions (HITL, console ADR-0012 / #270) -------------
// Structured multiple-choice questions the agent asks when it needs a human
// decision. Each tool HAS an execute() returning a RESOLVED placeholder, so the
// turn ends fully-resolved (no dangling tool_use → no MissingToolResultsError on
// persist/replay). Registered on the `files` tool set (see buildFileToolSet) and
// paired with a `hasToolCall` stop condition at the call site
// (run-conversation-turn.ts) so the turn ENDS at the call; the user's answer
// arrives as the NEXT turn's plain user message (`buildAnswerInstruction` /
// `buildAnswersInstruction`). PROPERTY ORDER is load-bearing: `question` first so
// a consumer can render the card header the instant it resolves; the `options`
// list streams after.
//
// The description keeps the tool RESTRICTIVE ("only when you cannot proceed
// safely") for ordinary generation turns; the platform `grilling` skill loosens
// that during interviews. Always registered — no per-turn gating (#270 decision 6).

const askQuestionOptionSchema = z.object({
  label: z.string().describe("Short display text for this choice — the exact value echoed back in the answer. Keep labels unique within a question."),
  description: z.string().optional().describe(
    "The full explanation of this choice, shown on the option card. Write 2–4 sentences: what picking it " +
    "means concretely, what it implies or rules out, and its trade-offs versus the other options — enough " +
    "for a non-expert to decide confidently. Always provide it unless the label is truly self-explanatory.",
  ),
  recommended: z.boolean().optional().describe("Mark the ONE option you recommend (at most one per question)."),
  freeText: z.boolean().optional().describe(
    "Set true on an option whose selection requires the user to TYPE their answer (an 'Other' / 'Something else' " +
    "escape hatch): the form focuses the text field and blocks submit until text is entered. When the ENTIRE " +
    "question needs a typed answer, prefer an empty options array instead.",
  ),
});

export const askQuestionInputSchema = z.object({
  question: z.string().describe("The clarifying question to ask the user — specific and self-contained, not a vague topic."),
  detail: z.string().optional().describe(
    "Context shown under the question: why you are asking, what part of the spec or design this decision " +
    "affects, and any background the user needs to answer well. 1–3 sentences; plain language.",
  ),
  options: z
    .array(askQuestionOptionSchema)
    .max(5)
    .refine((opts) => opts.filter((o) => o.recommended).length <= 1, {
      message: "At most one option may be marked recommended.",
    })
    .describe(
      "0–5 answer options the user picks from; labels are the selection identity. Pass an EMPTY array when " +
      "the answer must be typed (no sensible presets) — the form always offers a free-text field, so never " +
      "invent placeholder options like 'Type my own answer' or 'Other'.",
    ),
  multiSelect: z
    .boolean()
    .optional()
    .describe("True → the user may pick several options together (checkboxes). Defaults to false (a single choice)."),
});

export const askQuestionsInputSchema = z.object({
  questions: z
    .array(askQuestionInputSchema)
    .min(1)
    .max(8)
    .describe("1–8 questions rendered together as one form; each answered independently."),
});

// --- declare_plan (fire-and-forget UI, console ADR-0022 / #576) -------------
//
// The agent says which bundle paths it is ABOUT to write, so the console's spec
// rail can show a checklist and an honest count instead of only a log of what
// already happened. Unlike the question tools above this does NOT end the turn:
// `execute` resolves immediately and the agent keeps working, so the call site
// pairs it with no `hasToolCall` stop condition.

export const declarePlanInputSchema = z.object({
  paths: z
    .array(z.string())
    .describe(
      "The bundle paths you are about to write, in the order you intend to write them — full repo-relative " +
      'paths ("specs/design/design.md"). Names are the console\'s to choose; send paths only.',
    ),
});

// --- Drift guard: Zod schema ⇄ sse-events wire type -------------------------
// Compile-time only. If a schema's inferred input diverges from its wire type,
// the corresponding `true` is no longer assignable and this fails to compile,
// forcing the schema and contract back in sync. No meaningful runtime effect.
const _drift: [
  Equal<z.infer<typeof addFileInputSchema>, AddFileInput>,
  Equal<z.infer<typeof editFileInputSchema>, EditFileInput>,
  Equal<z.infer<typeof removeFileInputSchema>, RemoveFileInput>,
  Equal<z.infer<typeof askQuestionInputSchema>, AskQuestionInput>,
  Equal<z.infer<typeof askQuestionsInputSchema>, AskQuestionsInput>,
  Equal<z.infer<typeof declarePlanInputSchema>, DeclarePlanInput>,
] = [true, true, true, true, true, true];
void _drift;

/** Ask ONE structured question (a single card); ends the turn (HITL). */
export const askQuestionTool: Tool = tool({
  description:
    "Ask the user ONE structured multiple-choice question when you cannot proceed safely without their decision. " +
    "Make it decidable without prior context: put the why/what-it-affects in `detail`, and explain every option's " +
    "meaning and trade-offs in its `description`. Give 0–5 options (0 = a typed answer is required; mark at most one recommended); set multiSelect " +
    "when several may apply together. Ends your turn; the user's answer arrives as the next message.",
  inputSchema: askQuestionInputSchema,
  execute: async (input) => ({ status: "awaiting_user_response" as const, ...input }),
});

/** Ask a LIST of structured questions answered as one form; ends the turn (HITL). */
export const askQuestionsTool: Tool = tool({
  description:
    "Ask the user SEVERAL structured questions at once, rendered as a single form — use it when you have multiple " +
    "independent decisions to gather in one round instead of asking them one turn at a time. Each entry is a full " +
    "question (1–5 options, optional recommended, optional multiSelect). Ends your turn; the answers arrive as the next message.",
  inputSchema: askQuestionsInputSchema,
  execute: async (input) => ({ status: "awaiting_user_response" as const, ...input }),
});

/**
 * Declare the artifacts this turn is about to write. Fire-and-forget: the
 * tool-call input IS the payload, `execute` only acknowledges, and the turn
 * continues (ADR-0022). Declaring again ADDS to the plan — the console takes
 * the union, so a restated path is ignored rather than duplicated.
 */
export const declarePlanTool: Tool = tool({
  description:
    "Declare the files you are about to write, BEFORE you write them, so the user can see the whole shape of " +
    "the work and how much is left instead of watching files appear one at a time. Call it as soon as you know " +
    "part of the plan, and call it AGAIN whenever the plan grows — declaring the cell first and the " +
    "per-component files once the component set exists is the expected pattern, not a mistake. Restating a path " +
    "already declared is harmless. Does NOT end your turn: declare, then get on with the writing.",
  inputSchema: declarePlanInputSchema,
  execute: async (input) => ({ status: "ok" as const, ...input }),
});

/**
 * The write ops the ledger applies, bound to one bundle: the tool's OWN
 * `inputSchema` is the validator, so a call validated at input-end is validated
 * exactly as the SDK would validate it — no second copy of the rules.
 */
function buildWriteOps(bundle: FileBundle): Record<string, WriteOp> {
  /** Tie a schema to its op, so the validated shape IS the applied shape. */
  const writeOp = <T>(schema: z.ZodType<T>, apply: (input: T) => OpResult): WriteOp => ({
    validate: (args) => {
      const parsed = schema.safeParse(args);
      return parsed.success ? parsed.data : undefined;
    },
    apply: (input) => apply(input as T),
  });
  return {
    [ADD_FILE]: writeOp(addFileInputSchema, ({ path, content }) => bundle.addFile(path, content)),
    [EDIT_FILE]: writeOp(editFileInputSchema, ({ path, oldString, newString }) =>
      bundle.editFile(path, oldString, newString),
    ),
    [REMOVE_FILE]: writeOp(removeFileInputSchema, ({ path }) => bundle.removeFile(path)),
  };
}

/**
 * The `files` tool set plus the turn's write ledger, as ONE object: every file
 * write goes through the ledger, so a caller that also taps the stream
 * (`tapWrites`) settles each write at its own `tool-input-end` instead of at the
 * step's tail, and neither half can drift from the other. A caller with no
 * stream to tap simply never pre-applies — the ledger's memo stays empty and
 * `execute()` applies exactly as it always did.
 */
export function buildFileToolSet(
  bundle: FileBundle,
  skills?: SkillSource,
): { tools: Record<string, Tool>; writes: WriteLedger } {
  const writes = new WriteLedger(buildWriteOps(bundle));
  const tools: Record<string, Tool> = {
    [ADD_FILE]: tool({
      description:
        "Create a NEW file. The only tool that emits a whole body — use it for files that do not exist yet, " +
        "or (after removeFile) to replace a file wholesale. Errors with ALREADY_EXISTS if the path is already present.",
      inputSchema: addFileInputSchema,
      execute: async (input, { toolCallId }) => writes.apply(toolCallId, ADD_FILE, input),
    }),

    [EDIT_FILE]: tool({
      description:
        "Change part of an existing file by replacing oldString with newString. oldString must be copied VERBATIM " +
        "from the file (including leading indentation and newlines) and must match EXACTLY ONE location. On NOT_UNIQUE, " +
        "broaden the anchor with surrounding lines; on NOT_FOUND, re-copy the snippet exactly. Use this for prose AND " +
        "openapi.yaml.",
      inputSchema: editFileInputSchema,
      execute: async (input, { toolCallId }) => writes.apply(toolCallId, EDIT_FILE, input),
    }),

    [REMOVE_FILE]: tool({
      description:
        "Delete a file. Idempotent (deleting an absent path is a NOOP success). Refuses to delete the structural roots " +
        "(prd.md, design.cell) with PROTECTED_PATH.",
      inputSchema: removeFileInputSchema,
      execute: async (input, { toolCallId }) => writes.apply(toolCallId, REMOVE_FILE, input),
    }),

    // Human-in-the-loop questions (console ADR-0012 / #270). Registered on the
    // `files` set only; the call site pairs each with a `hasToolCall` stop
    // condition so the turn ends awaiting the user's answer.
    [ASK_QUESTION_TOOL]: askQuestionTool,
    [ASK_QUESTIONS_TOOL]: askQuestionsTool,

    // Fire-and-forget UI (#576): deliberately NOT paired with a stop condition
    // at the call site — the agent declares and keeps working.
    [DECLARE_PLAN_TOOL]: declarePlanTool,
  };

  return { tools: { ...tools, ...buildSkillTools(skills) }, writes };
}

export const DRAFT_EXTERNAL_RESOURCE_TOOL = "draftExternalResource" as const;

const draftExternalResourceInputSchema = z.object({
  name: z.string().optional().describe("Resource identity (logical name)."),
  description: z.string().optional().describe("What the resource is."),
  consumptionInstructions: z
    .string()
    .optional()
    .describe("How a consuming project should use it — not a restatement of description."),
  config: z
    .array(
      z.object({
        key: z.string().describe("Config key identity."),
        description: z.string().describe("What this key is for."),
        secret: z.boolean().describe("True when the value must never appear in chat."),
      }),
    )
    .optional()
    .describe("Config keys. Never include env values or secret bytes."),
  resourceDocs: z
    .array(
      z.object({
        type: z.enum(["documentation", "openapi", "graphql", "asyncapi", "protobuf"]),
        url: z.string().describe("Remote docs URL. Never a file body."),
      }),
    )
    .optional()
    .describe("Optional URL-only resource-docs. Never env values."),
});

/**
 * Console-folded draft for Marketplace register. Execute is a no-op: the
 * tool-call input IS the draft. Only registered on `/register-external-resource`
 * turns — never on spec/project files turns.
 */
export function buildRegisterDraftTools(): Record<string, Tool> {
  return {
    [DRAFT_EXTERNAL_RESOURCE_TOOL]: tool({
      description:
        "Draft the Registered External resource form (name, description, consumption instructions, " +
        "config keys, optional URL resource-docs). Never include environment values or secret bytes. " +
        "Call this when you know enough to fill the form; ask_question first when you do not.",
      inputSchema: draftExternalResourceInputSchema,
      execute: async (input) => ({ status: "ok" as const, ...input }),
    }),
  };
}
