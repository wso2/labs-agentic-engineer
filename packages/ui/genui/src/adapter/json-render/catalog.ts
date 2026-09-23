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

import { defineCatalog } from "@json-render/core";
import { schema } from "@json-render/react/schema";
import {
  genUiActions,
  genUiComponents,
  type GenUiComponentDef,
} from "../../catalog/index.js";

// json-render has no catalog field for events, so they ride in the
// description the model reads; the spec's `on` field binds them.
function describe(def: GenUiComponentDef): string {
  return def.events.length > 0
    ? `${def.description} Events: ${def.events.join(", ")}.`
    : def.description;
}

/** AEP's catalog translated into json-render's catalog format. */
export const jsonRenderCatalog = defineCatalog(schema, {
  components: Object.fromEntries(
    Object.entries(genUiComponents).map(([name, def]) => [
      name,
      {
        props: def.props,
        description: describe(def),
        slots: def.hasChildren ? ["default"] : [],
      },
    ]),
  ),
  actions: Object.fromEntries(
    Object.entries(genUiActions).map(([name, def]) => [
      name,
      { params: def.params, description: def.description },
    ]),
  ),
});
