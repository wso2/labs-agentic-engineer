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

// Which components the design cell declares as web-applications (#813) — the
// one fact that decides whether the Spec rail has a Prototype stage at all, and
// which prototype entries it lists.
//
// Derived from the cell every time, never stored: the cell IS the architecture,
// read by the same compiler the Architecture diagram renders it with, so the
// rail and the diagram cannot disagree about what the design declares.

import { compileProject } from "@aep/ui-cell-diagram-react/compiler";

/** The one spelling the design and prototype skills key on. */
const WEB_APPLICATION = "web-application";

/**
 * The web-application component ids in `source`, in declaration order.
 *
 * Tolerant: a cell streaming in mid-turn carries a half-written last line, and
 * the complete lines above it still declare what they declare.
 */
export function webApplicationsOf(source: string | null | undefined): string[] {
  if (!source?.trim()) return [];
  const { model } = compileProject(source, { tolerant: true });
  return (model?.cells ?? []).flatMap((cell) =>
    cell.components.filter((c) => c.type === WEB_APPLICATION).map((c) => c.id),
  );
}
