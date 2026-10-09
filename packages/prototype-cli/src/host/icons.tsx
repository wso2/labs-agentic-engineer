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

/** The host's icons, drawn in the text's colour. */

/** An icon from its SVG path data. */
export function Icon({ d }: { d: readonly string[] }) {
  return (
    <svg className="ph-icon" viewBox="0 0 24 24" aria-hidden>
      {d.map((path) => (
        <path key={path} d={path} />
      ))}
    </svg>
  );
}

/** A pointer: Preview. */
export const POINTER = ["M4 3l7 17 2.5-7.5L21 10z"];
/** A speech bubble with a "+": Comment. */
export const COMMENT = ["M7.9 20A9 9 0 1 0 4 16.1L2 22z", "M8 12h8M12 8v8"];
/** A circling arrow: Reset data. */
export const RESET = ["M3 12a9 9 0 1 0 3-6.7L3 8", "M3 3v5h5"];
/** A screen with a "+": Comment on this screen. */
export const SCREEN_COMMENT = ["M4 4h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1z", "M12 8v6M9 11h6"];
