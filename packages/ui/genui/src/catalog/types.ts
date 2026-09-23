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

import type { z } from "zod";

/**
 * One component a model may place in a generated UI.
 *
 * Library-neutral on purpose: nothing here names a renderer, so the same
 * definition feeds whichever adapter turns specs into React (json-render
 * today). An adapter translates this shape into its library's catalog format.
 */
export interface GenUiComponentDef {
  /**
   * The props contract. Both the model's output and the implementation are
   * held to it. Always an object schema, so adapters can reason per prop.
   */
  readonly props: z.ZodObject;
  /** What the component is for. Goes verbatim into the model's instructions. */
  readonly description: string;
  /** Whether the component renders child elements. */
  readonly hasChildren: boolean;
  /** Events the component can emit; a spec binds each one to an action. */
  readonly events: readonly string[];
}

/** One action a generated UI may trigger. The host app supplies the handler. */
export interface GenUiActionDef {
  /** The params contract, checked before the host's handler is called. */
  readonly params: z.ZodType;
  /** What the action does. Goes verbatim into the model's instructions. */
  readonly description: string;
}
