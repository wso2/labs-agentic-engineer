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
 * @aep/prototype-model — the versioned model behind every web-application
 * component's `prototype.json`, shared by the agent's write gate, the console,
 * and (through the published JSON Schema) the Go save gate.
 *
 * Realistic fixtures ship beside the code as
 * `@aep/prototype-model/fixtures/expense-approval.json` and
 * `@aep/prototype-model/fixtures/integration-monitor.json`.
 */

export { PROTOTYPE_SCHEMA_VERSION } from "./model.js";
export type {
  PrototypeAction,
  PrototypeAlertNode,
  PrototypeApprovalPanelNode,
  PrototypeBadgeNode,
  PrototypeBreadcrumb,
  PrototypeBreadcrumbsNode,
  PrototypeButton,
  PrototypeButtonNode,
  PrototypeDetailField,
  PrototypeDetailNode,
  PrototypeDialog,
  PrototypeDisplayState,
  PrototypeDrawer,
  PrototypeEmptyStateNode,
  PrototypeField,
  PrototypeFiltersNode,
  PrototypeFlow,
  PrototypeFormNode,
  PrototypeGridNode,
  PrototypeHeadingNode,
  PrototypeLinkNode,
  PrototypeModelV1,
  PrototypeNavigation,
  PrototypeNavigationItem,
  PrototypeNode,
  PrototypeNodeKind,
  PrototypeOverlay,
  PrototypeRole,
  PrototypeRow,
  PrototypeScreen,
  PrototypeSplitNode,
  PrototypeStackNode,
  PrototypeStatNode,
  PrototypeStep,
  PrototypeStepperNode,
  PrototypeTab,
  PrototypeTableNode,
  PrototypeTabsNode,
  PrototypeTaskQueueNode,
  PrototypeTextNode,
  PrototypeTimelineEntry,
  PrototypeTimelineNode,
  PrototypeTone,
  PrototypeValidationSummaryNode,
} from "./model.js";
export type { PrototypeIssueCode, PrototypeValidationIssue } from "./issues.js";
export { prototypeModelSchema } from "./schema.js";
export {
  parsePrototypeModel,
  stablePrototypeJson,
  type PrototypeParseOptions,
  type PrototypeParseResult,
} from "./parse.js";
export { prototypeModelJsonSchema } from "./json-schema.js";
export {
  isPrototypeArtifactPath,
  prototypeArtifactComponent,
  prototypeArtifactPath,
} from "./artifact-path.js";
