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
 * The kit's selectable-root props, for an Oxygen button (`ButtonBase`: a list
 * item, tab or step button). The kit leaves out a prop it has no value for,
 * so dropping undefined entries changes nothing at run time; it gives the
 * props the type of a component whose optional props do not take undefined.
 */

import type { SelectableRootProps } from "@wso2/prototype-kit";

type Defined<T> = { [K in keyof T]: Exclude<T[K], undefined> };

export function buttonRoot(root: SelectableRootProps): Defined<SelectableRootProps> {
  return Object.fromEntries(Object.entries(root).filter(([, value]) => value !== undefined)) as Defined<SelectableRootProps>;
}
