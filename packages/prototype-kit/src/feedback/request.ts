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

/** What a reviewer's request is, and the limits a submission of them keeps. */

/** The most requests one submission takes. */
export const MAX_FEEDBACK_REQUESTS = 50;

/** The longest request text, in UTF-16 code units. */
export const MAX_FEEDBACK_TEXT = 4000;

/** The longest screen, flow, role, state, element or component id, in UTF-16 code units. */
export const MAX_FEEDBACK_ID = 200;

/** A revision hash: 64 lowercase hex characters. */
export const PROTOTYPE_HASH_PATTERN = /^[0-9a-f]{64}$/;

export interface FeedbackRequest {
  screenId: string;
  flowId?: string;
  roleId: string;
  stateId: string;
  /** The selected elements' ids, in selection order; empty for the whole screen. */
  elementIds: string[];
  text: string;
}

/** A batch of requests on one revision. */
export interface FeedbackSubmission {
  /** The revision the requests were made on (`prototypeHash`). */
  prototypeHash: string;
  requests: FeedbackRequest[];
}
