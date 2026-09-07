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
 * `@aep/agent-stream` — the single client-side consumption surface for the spec
 * agent's turn stream. Zero server-side dependencies (no Express, no AI SDK), so
 * the console, the evals, the playground, and the agents service all fold the
 * wire through ONE definition. Moving `FileBundle` here brings the component
 * `design.json` write-gate with it, so any fold enforces the same schema for
 * free (§3/§12.4 of the migration decision record).
 */

// --- Wire contracts (the turn request + the SSE payload shapes) --------------
export type {
  Op,
  ErrCode,
  MatchCandidate,
  OpOk,
  OpErr,
  OpResult,
  AddFileInput,
  EditFileInput,
  RemoveFileInput,
  AskQuestionOption,
  AskQuestionInput,
  AskQuestionsInput,
  QuestionAnswer,
  DeclarePlanInput,
  LoadSkillInput,
  LoadSkillResult,
  LoadedSkill,
  LoadSkillReferenceInput,
  LoadSkillReferenceResult,
  Change,
  TurnRequest,
  TurnSpec,
  TurnKind,
  PlanScope,
  PlanContextFile,
  TurnJournal,
  TurnAttachment,
  TurnAnchor,
  TurnAnchorNode,
  TurnAim,
  TurnAimIntent,
  WorkspaceRef,
  McpConfig,
  CollabConfig,
  ManifestPart,
  TurnUsage,
  Toolset,
  Surface,
  AgentSseEventType,
} from "./contracts/sse-events.js";
export {
  AGENT_SSE_EVENT_TYPES,
  SSE_DONE,
  TOOLSETS,
  SURFACES,
  TURN_KINDS,
  ASK_QUESTION_TOOL,
  ASK_QUESTIONS_TOOL,
  DECLARE_PLAN_TOOL,
  ANSWER_PREFIX,
  ANSWERS_PREFIX,
  buildAnswerInstruction,
  buildAnswersInstruction,
  isToolset,
  isSurface,
  isTurnSpec,
  isTurnAttachment,
  isTurnAttachmentsOrAbsent,
  isTurnAim,
  TURN_AIM_LIMITS,
  isCollabConfig,
} from "./contracts/sse-events.js";
export type {
  SecurityDesign,
  ThunderClient,
  RoleDeclaration,
  RolePermission,
  TestUserDeclaration,
} from "./contracts/security-design.js";
export type {
  ComponentDesign,
  Dependency,
  DependencyKind,
  DependencyStyle,
  DependencyCandidate,
  ConfigKey,
  ExposesAPI,
} from "./contracts/component-design.js";

// --- Task-plan tool contract (tasks-github-native §9.3/§10.3) ----------------
export { PLAN_TASK, UPDATE_TASK } from "./contracts/task-tools.js";
export type {
  TaskOrigin,
  TaskOp,
  TaskToolErrCode,
  PlanTaskInput,
  UpdateTaskInput,
  UpdateTaskSet,
  TaskRef,
  PlanTaskOk,
  UpdateTaskOk,
  TaskToolErr,
  AddressableRefs,
  PlanTaskResult,
  UpdateTaskResult,
  TaskContextFile,
} from "./contracts/task-tools.js";
export { planTaskInputSchema, updateTaskInputSchema } from "./task-tools-schema.js";
export { parseKnownComponents, parseTaskContextFile, TASK_CONTEXT_FILE_RE } from "./task-context.js";

// --- The stream-part seam ----------------------------------------------------
export type { StreamPart } from "./stream-types.js";

// --- The compile-time drift-guard primitive (schema ⇄ wire-type checks) ------
export type { Equal } from "./type-equal.js";

// --- The fold surface --------------------------------------------------------
export { FileBundle, lf, FRONTMATTER_RE } from "./bundle.js";
export { toChange, applyToolCall, isFileMutationTool, opForTool, readToolInputPath } from "./change.js";

// --- The component design.json write-gate (travels with FileBundle) ----------
export {
  checkComponentDesign,
  componentDesignSchema,
  COMPONENT_DESIGN_JSON_RE,
} from "./component-design-schema.js";
export type { ComponentDesignProblem } from "./component-design-schema.js";
export { checkDesignDiagram, cellNodeIds, prdActors, DOMAIN_MODEL_PATH } from "./design-diagrams.js";
export type { DesignDiagramProblem, DiagramBundleReader, CellNodes } from "./design-diagrams.js";
export { checkComponentDependencies } from "./component-dependencies.js";
export type { ComponentDependencyProblem } from "./component-dependencies.js";

// --- The security.json write-gate ------------------------------------------
export {
  checkSecurityDesign,
  checkSecurityReferences,
  securityDesignSchema,
  SECURITY_DESIGN_JSON_RE,
  TEST_USERNAME_RE,
} from "./security-design-schema.js";
export type { SecurityDesignProblem } from "./security-design-schema.js";

// --- JSON Schema publication (the BFF validates the same definitions) --------
export {
  componentDesignJsonSchema,
  securityDesignJsonSchema,
  planTaskJsonSchema,
  updateTaskJsonSchema,
} from "./json-schema.js";

// --- The reference SSE reader ------------------------------------------------
// `streamTurn` = fetch + parse (server-side callers: evals, playground).
// `parseSseStream` = parse only, for a caller that owns its own fetch (the
// console adds auth + a `{useCase,...}` body + pre-stream status mapping).
export { streamTurn, parseSseStream } from "./sse-client.js";
export type { SseStreamEnd, StreamTurnOptions } from "./sse-client.js";
