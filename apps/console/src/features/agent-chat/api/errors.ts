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

// The chat API's errors that more than one of its calls raise.

/**
 * The issue whose thread was addressed is closed (409 `issue_closed`): a closed
 * issue has no thread, and closing one removes the one it had. Resolving the
 * thread and starting a turn in it both say so.
 */
export class IssueClosedError extends Error {
  constructor() {
    super("This issue is closed. Its chat was removed.");
    this.name = "IssueClosedError";
  }
}
