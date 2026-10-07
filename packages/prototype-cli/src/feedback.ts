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
 * The feedback file a reviewer saves for the agent: `.prototype/feedback.json`,
 * overwritten on each save. Its requests are the kit's
 * (`@wso2/prototype-kit/feedback`: shape, limits, validator), which AEP's
 * prototype feedback shares, so a host can reuse them. Shared by the preview
 * server and the host app.
 */

import type { FeedbackSubmission } from "@wso2/prototype-kit/feedback";

export const FEEDBACK_SCHEMA_VERSION = 1;

/** Where the preview writes feedback, relative to the prototype folder. */
export const FEEDBACK_PATH = ".prototype/feedback.json";

/** What `.prototype/feedback.json` holds. */
export interface FeedbackFile extends FeedbackSubmission {
  schemaVersion: typeof FEEDBACK_SCHEMA_VERSION;
  /** ISO-8601 time of the save. */
  savedAt: string;
}
