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

/** The shapes the browser tests and the node-side commands exchange. */

/** Where a target lives: the host page, or the prototype app inside its sandboxed frame. */
export type Where = "host" | "app";

/** An element, found the way a person would: by role and name, label, text, or (Annotate) element id. */
export interface Target {
  where: Where;
  role?: string;
  name?: string;
  label?: string;
  text?: string;
  /** A kit element id (`data-proto-key`). */
  elementId?: string;
  /** Match `name`/`text` as a substring (default exact). */
  partial?: boolean;
}

export type Action = { type: "click" } | { type: "fill"; value: string } | { type: "select"; label: string } | { type: "press"; key: string };

export type Reading = "text" | "count" | "value" | "pressed" | "disabled" | "maxlength";

export interface Preview {
  id: string;
  url: string;
}
