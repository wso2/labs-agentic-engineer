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

/** The theme a command renders with: `--theme <package>`, else the default theme, found from the prototype, the cwd, then the CLI itself. */

import { ThemeNotFoundError, resolveTheme, type ThemeRuntimes } from "@wso2/prototype-kit/check";
import { PACKAGE_ROOT } from "./assets.js";
import { UsageError } from "./usage.js";

export const DEFAULT_THEME = "@wso2/prototype-theme-default";

export function resolveCliTheme(specifier: string | undefined, dir: string, cwd: string): ThemeRuntimes {
  try {
    return resolveTheme(specifier ?? DEFAULT_THEME, [dir, cwd, PACKAGE_ROOT]);
  } catch (e) {
    if (e instanceof ThemeNotFoundError) throw new UsageError(e.message);
    throw e;
  }
}
