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
 * The half of the matrix's baseline rows that `security.json` cannot answer.
 *
 * `baselineScreens` in `securityDesign.ts` reads the SCREENS with no handle;
 * an OPERATION's protection is authored in its component's `openapi.yaml`, so
 * it has to be read from there. Both halves land on the same two rows —
 * "any signed-in user" and "public" — which is the point: the design wants the
 * reader to see the baseline in the same grid as the grants, not to go looking
 * for it in the API view.
 *
 * A component whose contract the bundle does not hold contributes nothing, in
 * silence. The design lineup writes `security.json` before some sibling files
 * exist, and an empty row is the honest rendering of "not authored yet" — an
 * error here would be a false alarm on every project mid-design.
 */

import { parseOpenApi, type Protection } from "@aep/ui-openapi-view";
import type { SecurityReferenceContext } from "@aep/agent-stream";

/** Operations reachable without holding a catalog handle, as `GET /me` lines. */
export interface BaselineOperations {
  /** Any signed-in caller reaches these. */
  signedIn: string[];
  /** Reachable before sign-in. */
  open: string[];
}

const EMPTY: BaselineOperations = { signedIn: [], open: [] };

function contractOf(
  references: SecurityReferenceContext,
  component: string,
): string | undefined {
  const dir = `specs/design/components/${component}`;
  return (
    references.read(`${dir}/openapi.yaml`) ?? references.read(`${dir}/openapi.yml`)
  );
}

/**
 * Every unscoped operation the named components declare, component order
 * preserved so the row reads in the order the cell draws the services.
 */
export function baselineOperations(
  components: readonly string[],
  references: SecurityReferenceContext | undefined,
): BaselineOperations {
  if (!references) return EMPTY;
  const signedIn: string[] = [];
  const open: string[] = [];
  for (const component of new Set(components)) {
    const source = contractOf(references, component);
    if (source === undefined) continue;
    const parsed = parseOpenApi(source);
    if ("kind" in parsed) continue; // a malformed contract is the API view's story
    for (const section of parsed.sections) {
      for (const operation of section.endpoints) {
        const bucket = bucketFor(operation.protection);
        if (bucket === null) continue;
        (bucket === "signedIn" ? signedIn : open).push(
          `${operation.method} ${operation.path}`,
        );
      }
    }
  }
  return { signedIn, open };
}

/** Which baseline row an operation belongs on, or null when it needs a handle. */
function bucketFor(protection: Protection): "signedIn" | "open" | null {
  if (protection.kind === "signedIn") return "signedIn";
  if (protection.kind === "public") return "open";
  return null;
}
