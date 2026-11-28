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
 * the console, the evals, the playground, and the design agent all consume
 * the wire through ONE definition. Moving `FileBundle` here brings the component
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
  PrototypeFinding,
  AddFileInput,
  EditFileInput,
  RemoveFileInput,
  AskQuestionOption,
  QuestionOptionAction,
  AskQuestionInput,
  AskQuestionsInput,
  QuestionAnswer,
  DeclarePlanInput,
  HandOffInput,
  HandOffResult,
  LoadSkillInput,
  LoadSkillResult,
  LoadedSkill,
  LoadSkillReferenceInput,
  LoadSkillReferenceResult,
  Change,
  TurnSpec,
  TurnKind,
  PlanScope,
  TurnAttachment,
  TurnConnection,
  ModelCapabilities,
  ModelFormat,
  ModelAuthScheme,
  WebSearchStrategy,
  Tristate,
  TurnErrorPart,
  ProviderWaitPart,
  TurnAnchor,
  TurnAnchorNode,
  TurnAim,
  TurnScope,
  TurnAimIntent,
  ManifestPart,
  TurnUsage,
  Toolset,
  Surface,
  AreaId,
  FocusKind,
  Focus,
  AgentSseEventType,
} from "./contracts/sse-events.js";
export {
  AGENT_SSE_EVENT_TYPES,
  SSE_DONE,
  TOOLSETS,
  SURFACES,
  TURN_KINDS,
  ASK_QUESTION_TOOL,
  isQuestionTool,
  isErrorToolOutput,
  ASK_QUESTIONS_TOOL,
  HAND_OFF_TOOL,
  isHandOffTool,
  AREAS,
  isAreaId,
  areaLabel,
  areaCommand,
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
  isTurnConnection,
  isTurnAim,
  isTurnScope,
  TURN_AIM_LIMITS,
} from "./contracts/sse-events.js";
export type {
  SecurityDesign,
  Permission,
  Action,
  Group,
  Enrolment,
  RoleKind,
  Role,
  TestUser,
} from "./contracts/security-design.js";
export type {
  ComponentDesign,
  Dependency,
  DependencyKind,
  DependencyStyle,
  ConfigKey,
  ExposesAPI,
} from "./contracts/component-design.js";

// --- Build-plan tool contract --------------------------------------------------
export { PLAN_BUILD, FOUNDATION_ITEM, WHOLE_SPEC_ITEM } from "./contracts/task-tools.js";
export type {
  TaskOp,
  TaskToolErrCode,
  BuildPlanItem,
  BuildPlanInput,
  BuildPlanOk,
  TaskToolErr,
  BuildPlanResult,
} from "./contracts/task-tools.js";
export { buildPlanInputSchema } from "./task-tools-schema.js";
export { parseKnownComponents } from "./task-context.js";

// --- The stream-part seam ----------------------------------------------------
export type { StreamPart } from "./stream-types.js";

// --- The compile-time drift-guard primitive (schema ⇄ wire-type checks) ------
export type { Equal } from "./type-equal.js";

// --- The fold surface --------------------------------------------------------
export { FileBundle, lf, FRONTMATTER_RE } from "./bundle.js";
export type { PlannedWrite, WritePlan } from "./bundle.js";
export { isPrototypePath, writeWithRenderCheck } from "./prototype-gate.js";
export type { FileWrite, PrototypeFileTexts, PrototypeRenderCheck } from "./prototype-gate.js";
export { toChange, applyToolCall, isFileMutationTool, opForTool, readToolInputPath } from "./change.js";

// --- The component design.json write-gate (travels with FileBundle) ----------
export {
  checkComponentDesign,
  componentDesignSchema,
  COMPONENT_DESIGN_JSON_RE,
  SIGN_IN_RESOURCE_TYPE,
} from "./component-design-schema.js";
export type { ComponentDesignProblem } from "./component-design-schema.js";
export { checkDesignDiagram, cellNodeIds, prdActors, DOMAIN_MODEL_PATH, PRD_PATH } from "./design-diagrams.js";
export type { DesignDiagramProblem, DiagramBundleReader, CellNodes } from "./design-diagrams.js";
export { checkComponentDependencies } from "./component-dependencies.js";
export type { ComponentDependencyProblem } from "./component-dependencies.js";

// --- The agent.afm.md structural write-gate (the ai-agent component kind) ---
export {
  ATTACHMENT_CEILINGS,
  GUARDRAIL_LIMITS,
  PLATFORM_OWNED_GUARDRAIL_KEYS,
  ATTACHMENT_TYPES,
  checkAgentAfm,
  splitAfm,
  type AfmProblem,
  type AgentAfmFrontMatter,
} from "./agent-afm-schema.js";

// --- The dependency.json write-gate (one dependency, one definition) --------
export {
  checkDependencyDesign,
  preservePlatformFields,
  dependencyDesignSchema,
  dependencySuggestionSchema,
  resourceDefinitionSchema,
  resourceContractSchema,
  resourceProvenanceSchema,
  sdkManifestSchema,
  dependencyDir,
  dependencyDesignPath,
  DEPENDENCY_DESIGN_JSON_RE,
  SDK_MANIFEST_JSON_RE,
  CONTRACT_FILES_BY_TYPE,
  SDK_MANIFEST_FILE,
} from "./dependency-design-schema.js";
export type { DependencyDesignProblem } from "./dependency-design-schema.js";
export type {
  DependencyDesign,
  DependencySuggestion,
  ResourceDefinition,
  ResourceContract,
  ResourceContractType,
  ContractOrigin,
  ResourceProvenance,
  DependencyAssumption,
  SdkManifest,
} from "./contracts/dependency-design.js";

// --- The security.json write-gate ------------------------------------------
export {
  checkSecurityDesign,
  securityDesignSchema,
  SECURITY_DESIGN_JSON_RE,
  TEST_USERNAME_RE,
  HANDLE_SEGMENT_RE,
  isHandle,
} from "./security-design-schema.js";
export type { SecurityDesignProblem } from "./security-design-schema.js";
export {
  catalogHandles,
  roleGrants,
  OIDC_RESERVED_SCOPES,
} from "./security-design-catalog.js";
export {
  checkSecurityReferences,
  securityReferenceFindings,
} from "./security-design-references.js";
export type {
  SecurityReferenceFinding,
  SecurityReferenceContext,
  SecurityFindingSeverity,
} from "./security-design-references.js";
export {
  SECURITY_DESIGN_MESSAGES,
  securityMessage,
} from "./security-design-messages.js";
export type { SecurityMessageKey } from "./security-design-messages.js";

// --- The review.json write-gate (the Design step's comments and checks) -----
export {
  checkDesignReview,
  designReviewSchema,
  reviewedArtifactSchema,
  reviewCommentSchema,
  reviewCommentStatusSchema,
  reviewAuthorSchema,
  reviewAnchorSchema,
  DESIGN_REVIEW_PATH,
  REVIEW_ARTIFACT_PATH_RE,
  PROTOTYPE_ARTIFACT_RE,
} from "./design-review-schema.js";
export type { DesignReviewProblem } from "./design-review-schema.js";
export type {
  DesignReview,
  ReviewedArtifact,
  ReviewComment,
  ReviewCommentStatus,
  ReviewAuthor,
  ReviewAnchor,
} from "./contracts/design-review.js";

// --- JSON Schema publication (the BFF validates the same definitions) --------
export {
  componentDesignJsonSchema,
  securityDesignJsonSchema,
  designReviewJsonSchema,
  buildPlanJsonSchema,
  agentAfmJsonSchema,
} from "./json-schema.js";

// --- The reference SSE reader ------------------------------------------------
// `startAndStreamTurn` = start a `/v1` turn + stream it (server-side callers:
// evals, playground).
// `parseSseFrames` = parse only, with each frame's resume id, for a caller that
// owns its own fetch (the console adds auth + pre-stream status mapping and
// resumes with `?from=<last id + 1>`); `parseSseStream` = the same, parts only.
export { startAndStreamTurn, parseSseFrames, parseSseStream, TurnRefusedError } from "./sse-client.js";
export type { SseFrame, SseStreamEnd, TurnStartBody } from "./sse-client.js";
