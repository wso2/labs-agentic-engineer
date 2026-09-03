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

export { ValidationView } from "./ValidationView.js";
export type { LiveStatuses, ValidationViewProps } from "./ValidationView.js";
export { parseValidationCriteria } from "./parse.js";
export type {
  Criterion,
  CriterionMethod,
  Requirement,
  ValidationCriteria,
  ParseError,
  ParseResult,
} from "./parse.js";
export { parseValidationReport } from "./report.js";
export type {
  CriterionReport,
  CriterionRunState,
  ValidationReport,
  ReportParseResult,
} from "./report.js";
export {
  countOf,
  CRITERION_STATE_LABEL,
  METHOD_COLOR,
  METHOD_FALLBACK_COLOR,
  METHOD_LABEL,
  tallyCriterionMethods,
  tallyCriterionStates,
  uncoveredCount,
} from "./counts.js";
export type {
  CriterionMethodCount,
  CriterionStateCount,
  CriterionTally,
} from "./counts.js";
