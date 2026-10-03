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
 * Running a prototype module: the transpiled `prototype.tsx` is CommonJS, and
 * its `require` answers from the one copy of React and the kit the runtime
 * bundles, so the module and the kit share one React. Shared by the frame and
 * the render check.
 */

import * as React from "react";
import * as JsxRuntime from "react/jsx-runtime";
import { isPrototypeApp, type PrototypeApp } from "../app.js";
import * as Kit from "../index.js";

const MODULES: Readonly<Record<string, unknown>> = Object.freeze({
  react: React,
  "react/jsx-runtime": JsxRuntime,
  "@wso2/prototype-kit": Kit,
});

/** The function a transpiled module body is wrapped in. */
export type ModuleFactory = (require: (name: string) => unknown, module: { exports: Record<string, unknown> }, exports: Record<string, unknown>) => void;

/** Run a module factory and return its `defineApp` default export; throws with the reason otherwise. */
export function runPrototypeModule(factory: ModuleFactory): PrototypeApp {
  const module = { exports: {} as Record<string, unknown> };
  const require = (name: string) => {
    if (!Object.hasOwn(MODULES, name)) throw new Error(`prototype.tsx may import only react and @wso2/prototype-kit, not ${JSON.stringify(name)}`);
    return MODULES[name];
  };
  factory(require, module, module.exports);
  const app = module.exports["default"];
  if (!isPrototypeApp(app)) throw new Error("prototype.tsx must `export default defineApp({ screens, data })`");
  return app;
}
