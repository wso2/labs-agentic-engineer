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
 * The check runtime's entry, bundled per theme by `buildThemeRuntimes`. It
 * publishes `__protoPrototypeCheck(input)` in the isolated context: JSON in
 * (the manifest), JSON out (the findings), so nothing but strings crosses.
 * The module factory is put in the context as `__protoModuleFactory` by the
 * check process.
 */

import type { PrototypeManifest } from "../manifest/types.js";
import type { ModuleFactory } from "../runtime/modules.js";
import type { PrototypeTheme } from "../theme/contract.js";
import { hardenIntrinsics } from "./harden.js";
import { checkRender } from "./render-check.js";

export function startCheck(theme: PrototypeTheme): void {
  const g = globalThis as Record<string, unknown>;
  g["__protoPrototypeCheck"] = (input: string): string => {
    const { manifest } = JSON.parse(input) as { manifest: PrototypeManifest };
    const factory = g["__protoModuleFactory"] as ModuleFactory;
    hardenIntrinsics();
    return JSON.stringify(checkRender(manifest, factory, theme));
  };
}
