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

import type { IssueAttentionReason } from "./api/queries";

export function attentionLabel(reason: IssueAttentionReason): string {
  switch (reason) {
    case "unverified_fix":
      return "Needs review";
    case "no_change_verdict":
      return "No code fix";
    case "escalated":
      return "Escalated";
  }
}

export function attentionDescription(reason: IssueAttentionReason): string {
  switch (reason) {
    case "unverified_fix":
      return "The coding agent was not confident enough to close this issue. Review the fix and close it when verified.";
    case "no_change_verdict":
      return "The coding agent closed this as not planned because no code fix was warranted.";
    case "escalated":
      return "This incident has recurred repeatedly and needs human attention.";
  }
}
