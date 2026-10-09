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

import { STATUS_CODES } from "node:http";

/** An `application/problem+json` body (RFC 9457) with the platform's `code`. */
export interface ProblemBody {
  type: "about:blank";
  title: string;
  status: number;
  detail: string;
  code: string;
}

/**
 * The status and problem body of an error response; same shape as
 * ae-studio-tools' problem.Write (`detail` is always present, "" by default).
 */
export function problem(status: number, code: string, detail = ""): { status: number; body: ProblemBody } {
  return { status, body: { type: "about:blank", title: STATUS_CODES[status] ?? "Error", status, detail, code } };
}
