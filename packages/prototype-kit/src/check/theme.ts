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

/** Finding a theme's prebuilt runtimes from its package name. */

import { createRequire } from "node:module";
import { join, resolve } from "node:path";

export interface ThemeRuntimes {
  /** The theme's package specifier, as given. */
  name: string;
  /** Absolute path of the theme's `frame-runtime.js`. */
  frameRuntimePath: string;
  /** Absolute path of the theme's `check-runtime.js`. */
  checkRuntimePath: string;
}

export class ThemeNotFoundError extends Error {}

/** Resolves `${specifier}/frame-runtime.js` and `${specifier}/check-runtime.js` from the first directory that has the package. */
export function resolveTheme(specifier: string, searchFrom: readonly string[]): ThemeRuntimes {
  for (const from of searchFrom) {
    const require = createRequire(join(resolve(from), "noop.js"));
    try {
      return {
        name: specifier,
        frameRuntimePath: require.resolve(`${specifier}/frame-runtime.js`),
        checkRuntimePath: require.resolve(`${specifier}/check-runtime.js`),
      };
    } catch {
      continue;
    }
  }
  throw new ThemeNotFoundError(`theme ${JSON.stringify(specifier)} was not found (looked from ${searchFrom.join(", ")}); install it, or pass an installed theme package to --theme`);
}
