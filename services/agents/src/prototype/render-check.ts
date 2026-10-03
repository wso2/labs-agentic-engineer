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
 * The prototype write gate's render check: draws every screen of a generated
 * prototype, for every role and display state, against the Oxygen theme's check
 * runtime. `@wso2/prototype-kit/check` runs it in an isolated Node child (the
 * permission model, no network, no filesystem, a timeout), because the module
 * being drawn is model output.
 *
 * Both packages are workspace dependencies whose `dist` the image builds; the
 * theme is resolved once, at import, so an image without its runtimes fails to
 * boot instead of failing every prototype write.
 */

import { dirname } from "node:path";
import { fileURLToPath } from "node:url";
import type { PrototypeRenderCheck } from "@aep/agent-stream";
import { checkPrototypeFiles, resolveTheme } from "@wso2/prototype-kit/check";

const THEME = resolveTheme("@wso2/prototype-theme-oxygen", [dirname(fileURLToPath(import.meta.url))]);

export const checkPrototypeRender: PrototypeRenderCheck = (files) => checkPrototypeFiles(files, { theme: THEME });
